package font

import (
	"bytes"
	"encoding/binary"
	"math"
	"sync"
	"unicode/utf16"
)

// A Face is a TrueType or OpenType program read by itself: the font of
// an XPS document, which has nothing but the program. A PDF says the
// advance of each glyph itself; here it is the program's (hmtx, vmtx),
// and so is which glyph a character has when the document does not say
// (cmap), what the font is called (name) and whether it is bold or
// italic (OS/2, head).
//
// A Face is safe for several goroutines.
type Face struct {
	// Name is the full name of the font, "" when the program has none.
	Name string
	// Bold and Italic are what the program says of itself.
	Bold, Italic bool

	units        float64
	hmtx, vmtx   []byte
	nH, nV       int
	glyphs       int
	height       float64 // ascender less descender, a fraction of the em
	uni, sym     []byte  // cmap subtables: Unicode, and a symbol font's own
	cmap         []byte
	latin        [256]uint16 // the glyphs of U+0000 to U+00FF
	once         sync.Once
	chars        []rune // by glyph
	mu           sync.Mutex
	styled       [4]*Font
	spaceAdvance float64
}

// maxFaceBytes bounds the font program of an XPS document.
const maxFaceBytes = maxProgramBytes

// MaxFaceBytes is the longest font program NewFace reads.
const MaxFaceBytes = maxFaceBytes

// NewFace reads the program data, the face index of it when it is a
// collection. It returns nil when data is no program with the tables
// that say how wide its glyphs are. data is not kept.
func NewFace(data []byte, index int) *Face {
	if len(data) > maxFaceBytes {
		data = data[:maxFaceBytes]
	}
	at := 0
	if len(data) >= 12 && string(data[:4]) == "ttcf" {
		n := int(binary.BigEndian.Uint32(data[8:]) & 0x7FFFFFFF)
		if index < 0 || index >= n || 12+4*index+4 > len(data) {
			return nil
		}
		at = int(binary.BigEndian.Uint32(data[12+4*index:]) & 0x7FFFFFFF)
	}
	tables := sfntTablesAt(data, at)
	head, hhea := tables["head"], tables["hhea"]
	if len(head) < 54 || len(hhea) < 36 {
		return nil
	}
	f := &Face{units: float64(binary.BigEndian.Uint16(head[18:]))}
	if f.units <= 0 {
		return nil
	}
	// The tables that are kept are copied: the program, with the outlines
	// of its glyphs, is not held for them.
	f.hmtx, f.nH = bytes.Clone(tables["hmtx"]), int(binary.BigEndian.Uint16(hhea[34:]))
	if maxp := tables["maxp"]; len(maxp) >= 6 {
		f.glyphs = int(binary.BigEndian.Uint16(maxp[4:]))
	}
	if f.nH == 0 || len(f.hmtx) < 4 {
		return nil
	}
	f.nH = min(f.nH, len(f.hmtx)/4)
	ascent, descent := int16(binary.BigEndian.Uint16(hhea[4:])), int16(binary.BigEndian.Uint16(hhea[6:]))
	f.height = float64(int(ascent)-int(descent)) / f.units
	if vhea := tables["vhea"]; len(vhea) >= 36 {
		f.vmtx, f.nV = bytes.Clone(tables["vmtx"]), int(binary.BigEndian.Uint16(vhea[34:]))
		f.nV = min(f.nV, len(f.vmtx)/4)
	}
	f.cmap = bytes.Clone(tables["cmap"])
	f.uni, f.sym = unicodeSubtable(f.cmap), cmapSubtable(f.cmap, 3, 0)
	for c := range f.latin {
		f.latin[c] = uint16(f.lookup(rune(c)))
	}
	f.Name = sfntName(tables["name"])
	f.Bold, f.Italic = binary.BigEndian.Uint16(head[44:])&1 != 0, binary.BigEndian.Uint16(head[44:])&2 != 0
	if os2 := tables["OS/2"]; len(os2) >= 64 {
		sel := binary.BigEndian.Uint16(os2[62:])
		f.Bold, f.Italic = sel&0x20 != 0, sel&0x01 != 0
	}
	f.spaceAdvance = 0.25
	if g := f.Glyph(' '); g > 0 {
		if a := f.Advance(g); a > 0 {
			f.spaceAdvance = a
		}
	}
	return f
}

// sfntTablesAt is sfntTables for the face whose table directory is at
// offset at of data: that of a collection.
func sfntTablesAt(data []byte, at int) map[string][]byte {
	if at == 0 {
		return sfntTables(data)
	}
	if at < 0 || at+12 > len(data) {
		return nil
	}
	n := int(binary.BigEndian.Uint16(data[at+4:]))
	out := map[string][]byte{}
	for i := 0; i < n && i < 256; i++ {
		rec := at + 12 + 16*i
		if rec+16 > len(data) {
			break
		}
		off := int(binary.BigEndian.Uint32(data[rec+8:]) & 0x7FFFFFFF)
		length := int(binary.BigEndian.Uint32(data[rec+12:]) & 0x7FFFFFFF)
		if off >= len(data) {
			continue
		}
		out[string(data[rec:rec+4])] = data[off:min(len(data), off+length)]
	}
	return out
}

// sfntName returns the full name of a font by its name table, or its
// family and style; "" for none. The names for Windows in English are
// preferred, then any for Windows, then those for the Macintosh.
func sfntName(table []byte) string {
	if len(table) < 6 {
		return ""
	}
	count, strings := int(binary.BigEndian.Uint16(table[2:])), int(binary.BigEndian.Uint16(table[4:]))
	var best [5]string // by name identifier
	var rank [5]int
	for i := 0; i < count && i < 1024; i++ {
		rec := 6 + 12*i
		if rec+12 > len(table) {
			break
		}
		u16 := func(k int) int { return int(binary.BigEndian.Uint16(table[rec+k:])) }
		platform, encoding, language, id, length, off := u16(0), u16(2), u16(4), u16(6), u16(8), u16(10)
		if id != 1 && id != 2 && id != 4 || length == 0 || length > 512 || strings+off+length > len(table) {
			continue
		}
		raw := table[strings+off : strings+off+length]
		var r int
		var s string
		switch {
		case platform == 3 && (encoding == 1 || encoding == 0 || encoding == 10), platform == 0:
			r = 2
			if platform == 3 && language == 0x409 {
				r = 3
			}
			units := make([]uint16, len(raw)/2)
			for k := range units {
				units[k] = binary.BigEndian.Uint16(raw[2*k:])
			}
			s = string(utf16.Decode(units))
		case platform == 1 && encoding == 0:
			r = 1
			b := make([]rune, len(raw))
			for k, c := range raw {
				b[k] = rune(c)
				if c >= 0x80 {
					b[k] = '?'
				}
			}
			s = string(b)
		}
		if r > rank[id] {
			rank[id], best[id] = r, s
		}
	}
	if best[4] != "" {
		return best[4]
	}
	if best[1] != "" && best[2] != "" && best[2] != "Regular" {
		return best[1] + " " + best[2]
	}
	return best[1]
}

func (f *Face) lookup(r rune) int {
	g := 0
	if f.uni != nil {
		g = lookupGlyph(f.uni, int(r))
	}
	if g <= 0 && f.sym != nil {
		// A symbol font has its glyphs at U+F000 and on, and is written
		// with those codes or with the low byte alone.
		if g = lookupGlyph(f.sym, int(r)); g <= 0 && r < 0x100 {
			g = lookupGlyph(f.sym, int(r)|0xF000)
		}
	}
	if g < 0 || g > 0xFFFF {
		return 0
	}
	return g
}

// Glyph returns the glyph of a character by the program's cmap, 0 for
// none.
func (f *Face) Glyph(r rune) int {
	if r >= 0 && r < 0x100 {
		return int(f.latin[r])
	}
	return f.lookup(r)
}

// Advance returns how far glyph g advances along the line, as a
// fraction of the font size.
func (f *Face) Advance(g int) float64 {
	if g < 0 {
		g = 0
	}
	if g >= f.nH {
		g = f.nH - 1
	}
	return float64(binary.BigEndian.Uint16(f.hmtx[4*g:])) / f.units
}

// Height returns how far glyph g advances when it is set sideways: its
// advance height, or the height of the font's line when the program has
// no vertical metrics.
func (f *Face) Height(g int) float64 {
	if f.nV == 0 {
		return f.height
	}
	if g < 0 {
		g = 0
	}
	if g >= f.nV {
		g = f.nV - 1
	}
	return float64(binary.BigEndian.Uint16(f.vmtx[4*g:])) / f.units
}

// Rune returns the character glyph g shows by the program's cmap turned
// round, 0 for none.
func (f *Face) Rune(g int) rune {
	f.once.Do(func() {
		f.chars = glyphChars(f.cmap)
		if f.chars == nil && f.sym != nil {
			// A symbol font: its codes are its own, U+F000 and on.
			eachMapping(f.sym, func(c, gid int) {
				if gid <= 0 || gid > 0xFFFF || c < 0xF020 || c > 0xF0FF {
					return
				}
				for len(f.chars) <= gid {
					f.chars = append(f.chars, 0)
				}
				if f.chars[gid] == 0 {
					f.chars[gid] = rune(c - 0xF000)
				}
			})
		}
	})
	if g <= 0 || g >= len(f.chars) {
		return 0
	}
	return f.chars[g]
}

// Font returns the font of the face as the runs of a page name it, made
// bold or italic by the document when the program is not (the style
// simulations of XPS). The same face and style give the same font.
func (f *Face) Font(bold, italic bool) *Font {
	k := 0
	if bold {
		k |= 1
	}
	if italic {
		k |= 2
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.styled[k] == nil {
		f.styled[k] = NewPlaced(f.Name, f.spaceAdvance, f.Bold || bold, f.Italic || italic)
	}
	return f.styled[k]
}

// A placed font is the font of text that comes as text and not as
// character codes: that of an XPS document, which gives the characters
// of its glyphs itself. A string shown in it is not codes to look up but
// a record (PlacedString): how far its glyphs advance and how many they
// are, and then its text. The interpreter shows such a string as it
// shows any other, so that all it does with a string of a PDF it does
// with this one.

// placedHeader is the length of what stands before the text of a string
// of a placed font.
const placedHeader = 16

// NewPlaced returns a placed font with the name and the style given;
// space is the advance of its space, as a fraction of the font size.
func NewPlaced(name string, space float64, bold, italic bool) *Font {
	f := &Font{BaseFont: name, Composite: true, SpaceWidth: space, Bold: bold, Italic: italic, placed: true}
	// Its text is any text: letters written from right to left too.
	f.rtl.Store(2)
	return f
}

// Placed reports whether f is a placed font.
func (f *Font) Placed() bool { return f.placed }

// PlacedString appends to dst the string of a placed font that shows
// text: glyphs glyphs, unmapped of them without text of their own, that
// advance by advance, a fraction of the font size, in all.
func PlacedString(dst []byte, text []byte, advance float64, glyphs, unmapped int) []byte {
	dst = binary.LittleEndian.AppendUint64(dst, math.Float64bits(advance))
	dst = binary.LittleEndian.AppendUint32(dst, uint32(glyphs))
	dst = binary.LittleEndian.AppendUint32(dst, uint32(unmapped))
	return append(dst, text...)
}

// appendPlaced is Append for a string of a placed font.
func appendPlaced(dst, s []byte) ([]byte, Shown) {
	if len(s) < placedHeader {
		return dst, Shown{}
	}
	sh := Shown{
		Advance:  math.Float64frombits(binary.LittleEndian.Uint64(s)),
		Glyphs:   int(binary.LittleEndian.Uint32(s[8:])),
		Unmapped: int(binary.LittleEndian.Uint32(s[12:])),
	}
	if sh.Advance != sh.Advance {
		sh.Advance = 0
	}
	return append(dst, s[placedHeader:]...), sh
}
