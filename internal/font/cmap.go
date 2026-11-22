package font

import (
	"errors"
	"io"
	"sort"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/AOShei/go-fast-pdf/internal/charset"
	"github.com/AOShei/go-fast-pdf/internal/syntax"
)

// A CMap maps character codes to something: a ToUnicode CMap maps them
// to text, the CMap named by a composite font's /Encoding maps them to
// character identifiers (CIDs). Both also say how many bytes a code has,
// in their codespace ranges.
//
// Mappings are kept as the ranges the file states and are looked up by
// binary search. Nothing is expanded, so a range that claims four
// billion codes costs one entry.
type cmap struct {
	space   codespace
	entries []entry // sorted by length of code, then first code
	text    []byte  // the texts of the entries, UTF-8
	// vertical is set by /WMode 1.
	vertical bool
	// parent is the name given to usecmap, if any.
	parent string
	// truncated is set when the CMap had more entries, or more text,
	// than are kept: the rest of it is not read.
	truncated bool
	// cutTexts counts the texts of codes cut at maxEntryText, and
	// cutBytes the bytes left out of them.
	cutTexts int
	cutBytes int64
	// parentMap is the predefined CMap that parent names, and
	// collection the character collection whose CIDs a predefined CMap
	// gives (cjk.go).
	parentMap  *cmap
	collection string
}

// Limits on what one CMap may hold.
const (
	maxEntries   = 1 << 19
	maxCMapText  = 16 << 20
	maxEntryText = 1 << 12
)

type entry struct {
	lo, hi uint32
	// For text: the position of the text in cmap.text. For a CID: the
	// CID of lo.
	val  uint32
	tlen uint16
	n    uint8 // bytes in the code
	kind uint8
	// outer is the index of the nearest earlier entry that contains lo,
	// or -1. A range that lies inside another is found through it.
	outer int32
}

const (
	kindText uint8 = iota // every code of the range has this text
	kindIncr              // the last character of the text grows with the code
	kindCID
)

// find returns the entry for a code of n bytes, or nil.
func (c *cmap) find(code uint32, n int) *entry {
	es := c.entries
	i := sort.Search(len(es), func(i int) bool {
		e := &es[i]
		return int(e.n) > n || int(e.n) == n && e.lo > code
	}) - 1
	for i >= 0 {
		e := &es[i]
		if int(e.n) != n {
			return nil
		}
		if code <= e.hi {
			return e
		}
		i = int(e.outer)
	}
	return nil
}

// findAny is find, and when the code has no entry of its own length, the
// entry of another length with the same value. Files disagree with
// themselves about the length of codes often enough to matter: a font
// read with two-byte codes whose ToUnicode CMap lists one-byte ones.
func (c *cmap) findAny(code uint32, n int) *entry {
	if e := c.find(code, n); e != nil {
		return e
	}
	for other := 1; other <= 4; other++ {
		if other != n {
			if e := c.find(code, other); e != nil {
				return e
			}
		}
	}
	return nil
}

// appendText appends the text of a code to dst. ok is false when the
// CMap has no text for it.
func (c *cmap) appendText(dst []byte, code uint32, n int) (out []byte, ok bool) {
	e := c.findAny(code, n)
	if e == nil || e.kind == kindCID {
		return dst, false
	}
	text := c.text[e.val : e.val+uint32(e.tlen)]
	if e.kind == kindText || code == e.lo {
		return append(dst, text...), true
	}
	// The last character counts up from the first code of the range.
	r, size := utf8.DecodeLastRune(text)
	if next := int64(r) + int64(code-e.lo); next > utf8.MaxRune || next >= 0xD800 && next <= 0xDFFF {
		r = utf8.RuneError
	} else {
		r = rune(next)
	}
	dst = append(dst, text[:len(text)-size]...)
	return utf8.AppendRune(dst, r), true
}

// cid returns the CID of a code, or 0, the CID of the glyph for an
// undefined character.
func (c *cmap) cid(code uint32, n int) uint32 {
	if e := c.find(code, n); e != nil && e.kind == kindCID {
		return e.val + (code - e.lo)
	}
	return 0
}

// codeFor returns a code whose text is exactly text, for finding the
// width of the space. Only single codes are searched.
func (c *cmap) codeFor(text string) (code uint32, n int, ok bool) {
	for i := range c.entries {
		e := &c.entries[i]
		if e.kind != kindCID && e.lo == e.hi && string(c.text[e.val:e.val+uint32(e.tlen)]) == text {
			return e.lo, int(e.n), true
		}
	}
	return 0, 0, false
}

func (c *cmap) add(e entry) {
	if len(c.entries) >= maxEntries {
		c.truncated = true
		return
	}
	c.entries = append(c.entries, e)
}

func (c *cmap) addText(lo, hi uint32, n int, text []byte, kind uint8) {
	if len(text) > maxEntryText {
		// The text of one code is kept up to the limit, at the end of a
		// character: cut, it is still the beginning of the right text,
		// where dropped the code would read by the font's encoding.
		cut := maxEntryText
		for cut > 0 && !utf8.RuneStart(text[cut]) {
			cut--
		}
		c.cutTexts++
		c.cutBytes += int64(len(text) - cut)
		text = text[:cut]
	}
	if len(c.text)+len(text) > maxCMapText {
		c.truncated = true
		return
	}
	c.add(entry{lo: lo, hi: hi, n: uint8(n), kind: kind, val: uint32(len(c.text)), tlen: uint16(len(text))})
	c.text = append(c.text, text...)
}

// finish sorts the entries and links each to the range around it.
func (c *cmap) finish() {
	es := c.entries
	// Stable: of two entries for the same code, the later one in the
	// file is found first.
	sort.SliceStable(es, func(i, j int) bool {
		if es[i].n != es[j].n {
			return es[i].n < es[j].n
		}
		return es[i].lo < es[j].lo
	})
	var open []int32 // entries that may contain the ones that follow
	for i := range es {
		e := &es[i]
		for len(open) > 0 {
			top := &es[open[len(open)-1]]
			if top.n == e.n && top.hi >= e.lo {
				break
			}
			open = open[:len(open)-1]
		}
		e.outer = -1
		if len(open) > 0 {
			e.outer = open[len(open)-1]
		}
		open = append(open, int32(i))
	}
	c.space.finish()
}

// --- codespace ---

// codespace says how many bytes a character code has, from its first
// bytes.
type codespace struct {
	ranges []codeRange
	// byFirst is the length of a code that starts with a given byte when
	// that byte alone decides it; 0 when no range starts with it and
	// mixed when the following bytes must be looked at.
	byFirst [256]uint8
	// shortest is the length taken when a code matches no range.
	shortest uint8
}

type codeRange struct {
	n      uint8
	lo, hi [4]byte
}

const mixed = 0xFF

func (s *codespace) add(lo, hi []byte) {
	if len(lo) != len(hi) || len(lo) < 1 || len(lo) > 4 || len(s.ranges) >= 256 {
		return
	}
	r := codeRange{n: uint8(len(lo))}
	copy(r.lo[:], lo)
	copy(r.hi[:], hi)
	s.ranges = append(s.ranges, r)
}

func (s *codespace) finish() {
	s.byFirst = [256]uint8{}
	s.shortest = 0
	for _, r := range s.ranges {
		if s.shortest == 0 || r.n < s.shortest {
			s.shortest = r.n
		}
		for b := int(r.lo[0]); b <= int(r.hi[0]); b++ {
			switch s.byFirst[b] {
			case 0:
				s.byFirst[b] = r.n
			case r.n:
			default:
				s.byFirst[b] = mixed
			}
		}
	}
}

// fixed returns a codespace in which every code has n bytes.
func fixed(n int) codespace {
	var s codespace
	s.add(make([]byte, n), []byte{0xFF, 0xFF, 0xFF, 0xFF}[:n])
	s.finish()
	return s
}

// next returns the first character code of b and its length in bytes.
// b must not be empty. The length is never more than len(b).
func (s *codespace) next(b []byte) (code uint32, n int) {
	n = int(s.byFirst[b[0]])
	switch n {
	case 0:
		// No range starts like this. The specification has the reader
		// take as many bytes as the shortest code.
		n = int(s.shortest)
		if n == 0 {
			n = 1
		}
	case mixed:
		n = 0
		for i := range s.ranges {
			r := &s.ranges[i]
			if int(r.n) > len(b) {
				continue
			}
			match := true
			for k := 0; k < int(r.n); k++ {
				if b[k] < r.lo[k] || b[k] > r.hi[k] {
					match = false
					break
				}
			}
			// Of two ranges that both match, the longer code is meant:
			// the shorter one would never leave it a chance.
			if match && int(r.n) > n {
				n = int(r.n)
			}
		}
		if n == 0 {
			n = int(s.shortest)
		}
	}
	n = min(n, len(b))
	for _, c := range b[:n] {
		code = code<<8 | uint32(c)
	}
	return code, n
}

// --- parsing ---

func codeValue(b []byte) (uint32, bool) {
	if len(b) < 1 || len(b) > 4 {
		return 0, false
	}
	var v uint32
	for _, c := range b {
		v = v<<8 | uint32(c)
	}
	return v, true
}

// decodeUTF16BE appends the UTF-8 form of UTF-16BE text. Half of a
// surrogate pair without the other half becomes U+FFFD. A single byte
// is taken as that character, which is what the files that write one
// mean; any other odd byte at the end is dropped.
func decodeUTF16BE(dst, b []byte) []byte {
	if len(b) == 1 {
		return utf8.AppendRune(dst, rune(b[0]))
	}
	for i := 0; i+1 < len(b); i += 2 {
		r := rune(b[i])<<8 | rune(b[i+1])
		if utf16.IsSurrogate(r) {
			var r2 rune = -1
			if i+3 < len(b) {
				r2 = rune(b[i+2])<<8 | rune(b[i+3])
			}
			if pair := utf16.DecodeRune(r, r2); pair != utf8.RuneError {
				r = pair
				i += 2
			} else {
				r = utf8.RuneError
			}
		}
		dst = utf8.AppendRune(dst, r)
	}
	return dst
}

// parseCMap reads the codespace ranges and the mappings of a CMap. A
// CMap is a PostScript program; everything but the sections that hold
// data is stepped over.
func parseCMap(data []byte) *cmap {
	c := &cmap{}
	p := syntax.NewParser(data, 0, 0)
	// next returns the next object, stepping over bytes that start none,
	// such as the braces of a PostScript procedure.
	next := func() (syntax.Value, bool) {
		for {
			v, err := p.Object()
			if err == nil {
				return v, true
			}
			if err == io.EOF || !errors.Is(err, syntax.ErrSyntax) {
				return syntax.Value{}, false
			}
			p.SetPos(p.Pos() + 1)
		}
	}
	isCode := func(v syntax.Value) bool { return v.Kind == syntax.String && len(v.Data) >= 1 && len(v.Data) <= 4 }
	var scratch []byte
	// text returns the text a destination stands for: UTF-16BE in a
	// string, or a glyph name.
	text := func(v syntax.Value) ([]byte, bool) {
		switch v.Kind {
		case syntax.String:
			scratch = decodeUTF16BE(scratch[:0], v.Data)
			return scratch, true
		case syntax.Name:
			if t, ok := charset.GlyphText(string(v.Data)); ok {
				scratch = append(scratch[:0], t...)
				return scratch, true
			}
		}
		return nil, false
	}
	// section reads the items of a section up to its end keyword. Each
	// item has count objects; fn receives them.
	section := func(end string, count int, fn func(item []syntax.Value)) bool {
		var item [3]syntax.Value
		for {
			for i := 0; i < count; i++ {
				v, ok := next()
				if !ok {
					return false
				}
				if v.Kind == syntax.Keyword {
					if v.Equal(end) {
						return true
					}
					// A stray keyword: start the item again.
					i = -1
					continue
				}
				item[i] = v
			}
			fn(item[:count])
		}
	}

	var prev, prev2 syntax.Value // the two objects before the current one
	for {
		v, ok := next()
		if !ok {
			break
		}
		if v.Kind != syntax.Keyword {
			prev2, prev = prev, v
			continue
		}
		switch string(v.Data) {
		case "begincodespacerange":
			ok = section("endcodespacerange", 2, func(it []syntax.Value) {
				if it[0].Kind == syntax.String && it[1].Kind == syntax.String {
					c.space.add(it[0].Data, it[1].Data)
				}
			})
		case "beginbfchar":
			ok = section("endbfchar", 2, func(it []syntax.Value) {
				if !isCode(it[0]) {
					return
				}
				code, _ := codeValue(it[0].Data)
				if t, ok := text(it[1]); ok {
					c.addText(code, code, len(it[0].Data), t, kindText)
				}
			})
		case "beginbfrange":
			ok = section("endbfrange", 3, func(it []syntax.Value) {
				if !isCode(it[0]) || !isCode(it[1]) || len(it[0].Data) != len(it[1].Data) {
					return
				}
				lo, _ := codeValue(it[0].Data)
				hi, _ := codeValue(it[1].Data)
				if hi < lo {
					return
				}
				n := len(it[0].Data)
				if it[2].Kind == syntax.Array {
					// One text per code, for as many as the array has.
					for i, elem := range it[2].List {
						code := lo + uint32(i)
						if code > hi || code < lo {
							break
						}
						if t, ok := text(elem); ok {
							c.addText(code, code, n, t, kindText)
						}
					}
					return
				}
				if t, ok := text(it[2]); ok {
					kind := kindIncr
					if lo == hi || len(t) == 0 {
						kind = kindText
					}
					c.addText(lo, hi, n, t, kind)
				}
			})
		case "begincidchar":
			ok = section("endcidchar", 2, func(it []syntax.Value) {
				if cid, isInt := it[1].Integer(); isCode(it[0]) && isInt && cid >= 0 && cid <= 0xFFFFFF {
					code, _ := codeValue(it[0].Data)
					c.add(entry{lo: code, hi: code, n: uint8(len(it[0].Data)), kind: kindCID, val: uint32(cid)})
				}
			})
		case "begincidrange":
			ok = section("endcidrange", 3, func(it []syntax.Value) {
				cid, isInt := it[2].Integer()
				if !isCode(it[0]) || !isCode(it[1]) || len(it[0].Data) != len(it[1].Data) || !isInt || cid < 0 || cid > 0xFFFFFF {
					return
				}
				lo, _ := codeValue(it[0].Data)
				hi, _ := codeValue(it[1].Data)
				if hi >= lo {
					c.add(entry{lo: lo, hi: hi, n: uint8(len(it[0].Data)), kind: kindCID, val: uint32(cid)})
				}
			})
		case "usecmap":
			c.parent = prev.NameString()
		case "def":
			if prev2.IsName("WMode") {
				if mode, isInt := prev.Integer(); isInt {
					c.vertical = mode == 1
				}
			}
		}
		if !ok {
			break
		}
		prev, prev2 = syntax.Value{}, syntax.Value{}
	}
	c.finish()
	return c
}

// predefined returns what is known about a CMap the specification
// names, without the CMap itself: the length of its codes, and whether
// its codes are Unicode already.
//
// The CMaps for the legacy encodings of Chinese, Japanese and Korean map
// codes to CIDs by tables of some megabytes that this library does not
// carry; their text comes from a ToUnicode CMap or not at all.
func predefined(name string) (space codespace, unicode unicodeCodes, vertical, known bool) {
	if name == "" {
		return space, 0, false, false
	}
	vertical = strings.HasSuffix(name, "-V") || name == "V"
	leadByte := func(lo byte) codespace {
		var s codespace
		s.add([]byte{0x00}, []byte{lo - 1})
		s.add([]byte{lo, 0x00}, []byte{0xFF, 0xFF})
		s.finish()
		return s
	}
	switch {
	case name == "Identity-H" || name == "Identity-V":
		return fixed(2), 0, vertical, true
	case strings.Contains(name, "UCS2"):
		return fixed(2), unicodeUCS2, vertical, true
	case strings.Contains(name, "UTF16"):
		var s codespace
		s.add([]byte{0x00, 0x00}, []byte{0xD7, 0xFF})
		s.add([]byte{0xD8, 0x00, 0xDC, 0x00}, []byte{0xDB, 0xFF, 0xDF, 0xFF})
		s.add([]byte{0xE0, 0x00}, []byte{0xFF, 0xFF})
		s.finish()
		return s, unicodeUTF16, vertical, true
	case strings.Contains(name, "UTF32"):
		return fixed(4), unicodeUTF32, vertical, true
	case strings.Contains(name, "RKSJ"):
		// Shift-JIS: 81 to 9F and E0 to FC start a two-byte code.
		var s codespace
		s.add([]byte{0x00}, []byte{0x80})
		s.add([]byte{0x81, 0x40}, []byte{0x9F, 0xFC})
		s.add([]byte{0xA0}, []byte{0xDF})
		s.add([]byte{0xE0, 0x40}, []byte{0xFC, 0xFC})
		s.add([]byte{0xFD}, []byte{0xFF})
		s.finish()
		return s, asciiOneByte, vertical, true
	case strings.Contains(name, "EUC"), strings.Contains(name, "B5"), strings.Contains(name, "UHC"),
		strings.Contains(name, "GBK"), strings.Contains(name, "Johab"), strings.Contains(name, "GBpc"):
		// One byte up to 80, two bytes from 81.
		return leadByte(0x81), asciiOneByte, vertical, true
	case strings.HasSuffix(name, "-H"), strings.HasSuffix(name, "-V"), name == "H", name == "V":
		return fixed(2), 0, vertical, true
	}
	return space, 0, false, false
}

// unicodeCodes says that the character codes of a font are Unicode in
// one of its forms.
type unicodeCodes uint8

const (
	unicodeUCS2 unicodeCodes = iota + 1
	unicodeUTF16
	unicodeUTF32
	// asciiOneByte: the one-byte codes from 20 to 7E are ASCII, as they
	// are in every legacy encoding of Chinese, Japanese and Korean. The
	// two-byte codes need tables this library does not carry.
	asciiOneByte
)
