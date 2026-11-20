package font

import (
	"encoding/binary"
	"sync"
	"unicode/utf8"

	"github.com/AOShei/go-fast-pdf/internal/charset"
	"github.com/AOShei/go-fast-pdf/internal/file"
	"github.com/AOShei/go-fast-pdf/internal/syntax"
)

// The characters of the glyphs of an embedded TrueType program.
//
// A TrueType program says which glyph shows each character, in its cmap
// table. Turned round, that says which character a glyph shows: what a
// CIDFont of type 2 needs when nothing else says what its codes are. The
// CID gives the glyph through /CIDToGIDMap (the same number when it is
// /Identity), and the glyph the character. A glyph that several
// characters reach shows the first of them, other than one for private
// use. Subtables for Unicode are read, in formats 0, 4, 6 and 12; one of
// a symbol font (platform 3, encoding 0) gives codes of its own and no
// characters. The program is read the first time a code of the font has
// no text otherwise, not when the font is loaded.

const (
	// maxProgramBytes bounds the font program that is read for its cmap.
	maxProgramBytes = 32 << 20
	// maxCMapChars bounds the characters a cmap subtable is read for.
	maxCMapChars = 1 << 20
	// maxGIDMapBytes bounds a /CIDToGIDMap stream: two bytes for each of
	// 65,536 CIDs.
	maxGIDMapBytes = 1 << 17
)

// trueTypeText gives the character of each CID of a CIDFont of type 2,
// from its program, read once when first asked.
type trueTypeText struct {
	f       *file.File
	program syntax.Value // FontFile2
	gidMap  syntax.Value // CIDToGIDMap: a stream, or anything else for Identity

	once  sync.Once
	chars []rune // by glyph
	gids  []byte // the CIDToGIDMap stream, two bytes a CID; nil for Identity
}

// char returns the character of a CID, 0 for none.
func (t *trueTypeText) char(cid uint32) rune {
	t.once.Do(t.read)
	gid := cid
	if t.gids != nil {
		if int(cid)*2+1 >= len(t.gids) {
			return 0
		}
		gid = uint32(binary.BigEndian.Uint16(t.gids[2*cid:]))
	}
	if int(gid) >= len(t.chars) {
		return 0
	}
	return t.chars[gid]
}

func (t *trueTypeText) read() {
	if t.gidMap.Kind == syntax.Stream {
		t.gids, _ = t.f.Decode(t.gidMap, nil, maxGIDMapBytes)
		if t.gids == nil {
			t.gids = []byte{}
		}
	}
	data, _ := t.f.Decode(t.program, nil, maxProgramBytes)
	tables := sfntTables(data)
	t.chars = glyphChars(tables["cmap"])
	// A glyph the cmap does not reach has the character its name says.
	post := readPost(tables["post"])
	for gid := 1; gid < post.n; gid++ {
		if gid < len(t.chars) && t.chars[gid] != 0 {
			continue
		}
		name := post.name(gid)
		if name == "" || name == ".notdef" {
			continue
		}
		if txt, ok := charset.GlyphText(name); ok {
			if r, n := utf8.DecodeRuneInString(txt); n == len(txt) {
				for len(t.chars) <= gid {
					t.chars = append(t.chars, 0)
				}
				t.chars[gid] = r
			}
		}
	}
	t.f, t.program, t.gidMap = nil, syntax.Value{}, syntax.Value{}
}

// sfntTables returns the tables of a TrueType or OpenType program by
// their tags; nil when it is not one.
func sfntTables(data []byte) map[string][]byte {
	if len(data) < 12 {
		return nil
	}
	n := int(binary.BigEndian.Uint16(data[4:]))
	out := map[string][]byte{}
	for i := 0; i < n && i < 256; i++ {
		rec := 12 + 16*i
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

// cmapSubtable returns the subtable of a cmap table for a platform and
// an encoding; nil for none.
func cmapSubtable(table []byte, platform, encoding int) []byte {
	if len(table) < 4 {
		return nil
	}
	count := int(binary.BigEndian.Uint16(table[2:]))
	for i := 0; i < count && i < 256; i++ {
		rec := 4 + 8*i
		if rec+8 > len(table) {
			break
		}
		if int(binary.BigEndian.Uint16(table[rec:])) == platform && int(binary.BigEndian.Uint16(table[rec+2:])) == encoding {
			if off := int(binary.BigEndian.Uint32(table[rec+4:]) & 0x7FFFFFFF); off < len(table) {
				return table[off:]
			}
		}
	}
	return nil
}

// unicodeSubtable returns the subtable of a cmap table that says the
// most about Unicode; nil for none.
func unicodeSubtable(table []byte) []byte {
	for _, pe := range [][2]int{{3, 10}, {0, 6}, {0, 4}, {3, 1}, {0, 3}, {0, 2}, {0, 1}, {0, 0}} {
		if sub := cmapSubtable(table, pe[0], pe[1]); sub != nil {
			return sub
		}
	}
	return nil
}

// eachMapping calls fn with each code of a cmap subtable and its glyph,
// in formats 0, 4, 6 and 12, at most maxCMapChars of them.
func eachMapping(sub []byte, fn func(code, gid int)) {
	u16 := func(i int) (int, bool) {
		if i < 0 || i+2 > len(sub) {
			return 0, false
		}
		return int(binary.BigEndian.Uint16(sub[i:])), true
	}
	u32 := func(i int) (int, bool) {
		if i < 0 || i+4 > len(sub) {
			return 0, false
		}
		return int(binary.BigEndian.Uint32(sub[i:]) & 0x7FFFFFFF), true
	}
	format, _ := u16(0)
	switch format {
	case 0:
		for c := 0; c < 256 && 6+c < len(sub); c++ {
			fn(c, int(sub[6+c]))
		}
	case 4:
		segX2, ok := u16(6)
		if !ok {
			return
		}
		ends, starts, deltas, ranges := 14, 16+segX2, 16+2*segX2, 16+3*segX2
		done := 0
		for s := 0; s < segX2/2; s++ {
			end, ok1 := u16(ends + 2*s)
			start, ok2 := u16(starts + 2*s)
			delta, ok3 := u16(deltas + 2*s)
			ro, ok4 := u16(ranges + 2*s)
			if !ok1 || !ok2 || !ok3 || !ok4 || start > end {
				continue
			}
			for c := start; c <= end && c != 0xFFFF && done < maxCMapChars; c++ {
				done++
				g := c
				if ro != 0 {
					var ok bool
					if g, ok = u16(ranges + 2*s + ro + 2*(c-start)); !ok || g == 0 {
						continue
					}
				}
				fn(c, (g+delta)&0xFFFF)
			}
		}
	case 6:
		first, ok1 := u16(6)
		n, ok2 := u16(8)
		if !ok1 || !ok2 {
			return
		}
		for i := 0; i < n; i++ {
			if g, ok := u16(10 + 2*i); ok {
				fn(first+i, g)
			}
		}
	case 12:
		groups, ok := u32(12)
		if !ok {
			return
		}
		done := 0
		for i := 0; i < groups && done < maxCMapChars; i++ {
			start, ok1 := u32(16 + 12*i)
			end, ok2 := u32(20 + 12*i)
			gid, ok3 := u32(24 + 12*i)
			if !ok1 || !ok2 || !ok3 {
				break
			}
			for c := start; c <= end && done < maxCMapChars; c++ {
				done++
				fn(c, gid+(c-start))
			}
		}
	}
}

// cmapChars reads the cmap table of a TrueType program and returns the
// character each glyph shows; nil when it says nothing.
func cmapChars(data []byte) []rune {
	return glyphChars(sfntTables(data)["cmap"])
}

// glyphChars returns the character each glyph shows by the Unicode
// subtable of a cmap table; nil for none.
func glyphChars(table []byte) []rune {
	sub := unicodeSubtable(table)
	if sub == nil {
		return nil
	}
	var chars []rune
	private := func(r rune) bool { return r >= 0xE000 && r <= 0xF8FF }
	eachMapping(sub, func(c, gid int) {
		if gid <= 0 || gid > 0xFFFF || c <= 0 || c > utf8.MaxRune || c >= 0xD800 && c <= 0xDFFF {
			return
		}
		for len(chars) <= gid {
			chars = append(chars, 0)
		}
		if old := chars[gid]; old == 0 || private(old) && !private(rune(c)) {
			chars[gid] = rune(c)
		}
	})
	return chars
}

// lookupGlyph returns the glyph of a code by a cmap subtable of format
// 0, 4, 6 or 12; 0 for none.
func lookupGlyph(sub []byte, code int) int {
	u16 := func(i int) int {
		if i < 0 || i+2 > len(sub) {
			return -1
		}
		return int(binary.BigEndian.Uint16(sub[i:]))
	}
	u32 := func(i int) int {
		if i < 0 || i+4 > len(sub) {
			return -1
		}
		return int(binary.BigEndian.Uint32(sub[i:]) & 0x7FFFFFFF)
	}
	switch u16(0) {
	case 0:
		if code < 256 && 6+code < len(sub) {
			return int(sub[6+code])
		}
	case 4:
		segX2 := u16(6)
		if segX2 <= 0 || code > 0xFFFF {
			return 0
		}
		n := segX2 / 2
		ends, starts, deltas, ranges := 14, 16+segX2, 16+2*segX2, 16+3*segX2
		lo, hi := 0, n
		for lo < hi { // the first segment that ends at or after the code
			mid := (lo + hi) / 2
			if e := u16(ends + 2*mid); e < 0 || e >= code {
				hi = mid
			} else {
				lo = mid + 1
			}
		}
		if lo >= n {
			return 0
		}
		start, delta, ro := u16(starts+2*lo), u16(deltas+2*lo), u16(ranges+2*lo)
		if start < 0 || delta < 0 || ro < 0 || code < start {
			return 0
		}
		g := code
		if ro != 0 {
			if g = u16(ranges + 2*lo + ro + 2*(code-start)); g <= 0 {
				return 0
			}
		}
		return (g + delta) & 0xFFFF
	case 6:
		first, n := u16(6), u16(8)
		if first >= 0 && code >= first && code < first+n {
			return max(u16(10+2*(code-first)), 0)
		}
	case 12:
		groups := u32(12)
		lo, hi := 0, max(groups, 0)
		for lo < hi {
			mid := (lo + hi) / 2
			start, end := u32(16+12*mid), u32(20+12*mid)
			switch {
			case start < 0 || end < 0:
				return 0
			case code < start:
				hi = mid
			case code > end:
				lo = mid + 1
			default:
				return max(u32(24+12*mid)+code-start, 0)
			}
		}
	}
	return 0
}

// post is a 'post' table, whose names are read one at a time.
type post struct {
	n     int      // glyphs it names
	index []byte   // the number of each glyph's name
	own   [][2]int // where the names of the program's own are, in the table
	table []byte
	mac   bool // format 1: the glyphs are those of the Macintosh order
}

// readPost reads a 'post' table of format 1 or 2; one of another
// format names no glyph.
func readPost(table []byte) post {
	if len(table) < 32 {
		return post{}
	}
	switch binary.BigEndian.Uint32(table) {
	case 0x00010000:
		return post{n: len(macGlyphNames), mac: true}
	case 0x00020000:
	default:
		return post{}
	}
	if len(table) < 34 {
		return post{}
	}
	n := int(binary.BigEndian.Uint16(table[32:]))
	if 34+2*n > len(table) {
		return post{}
	}
	p := post{n: n, index: table[34 : 34+2*n], table: table}
	for at := 34 + 2*n; at < len(table) && len(p.own) < 1<<16; {
		l := int(table[at])
		if at+1+l > len(table) {
			break
		}
		p.own = append(p.own, [2]int{at + 1, at + 1 + l})
		at += 1 + l
	}
	return p
}

// name returns the name of a glyph; "" for none.
func (p *post) name(g int) string {
	switch {
	case g < 0 || g >= p.n:
		return ""
	case p.mac:
		return macGlyphNames[g]
	}
	i := int(binary.BigEndian.Uint16(p.index[2*g:]))
	switch {
	case i < len(macGlyphNames):
		return macGlyphNames[i]
	case i-len(macGlyphNames) < len(p.own):
		r := p.own[i-len(macGlyphNames)]
		return string(p.table[r[0]:r[1]])
	}
	return ""
}
