package font

import (
	"unicode/utf8"

	"github.com/AOShei/go-fast-pdf/internal/charset"
	"github.com/AOShei/go-fast-pdf/internal/file"
	"github.com/AOShei/go-fast-pdf/internal/syntax"
)

// What the embedded program of a simple font says of its codes.
//
// A simple font that names no encoding has the one built into its
// program; a Type 1 program states it in clear text (type1.go). A CFF
// program (FontFile3 of subtype Type1C, or the CFF table of an OpenType
// one) has an encoding of its own that gives each code a glyph name
// (cff.go). A TrueType program reaches its glyphs from codes through its
// cmap: for a symbolic font, the subtable of platform 3, encoding 0, at
// the code or at F000, F100 or F200 plus it, else the Macintosh one
// (platform 1, encoding 0), unless it has a subtable for Unicode: a
// font marked symbolic that reaches its glyphs only by Unicode has the
// codes of WinAnsiEncoding, as the programs that write such fonts mean.
// The character of a glyph is the one the program's Unicode subtable
// reaches it from, or else what its name in the 'post' table says.
//
// The names of a CFF program's encoding are not taken when most of its
// letters and digits that are ASCII are not the characters of their own
// codes: the names are scrambled, as some programs make them, and the
// codes are the text.
//
// The names of /Differences that say nothing by themselves ("g23") are
// looked up in the program too: a TrueType glyph of that name in 'post'
// has the character its Unicode subtable gives it.
//
// The program is read only for a font that leaves something unknown:
// one without a ToUnicode CMap that names no encoding, or whose
// differences have such a name.

// simpleProgram is what a simple font's program says.
type simpleProgram struct {
	// names are the glyph names of the codes by a CFF program's
	// encoding; standard is set when that is the Standard encoding.
	names         [256]string
	cff, standard bool
	// text is the text of each code by a TrueType program, and mapped
	// whether it says which glyph the codes are; windows is set when
	// it reaches its glyphs by Unicode only, and its codes are those of
	// WinAnsiEncoding.
	text            [256]string
	mapped, windows bool
	// tables are those of a TrueType program, and byName the characters
	// of its named glyphs, made when a name is first asked for.
	tables map[string][]byte
	byName map[string]rune
}

// readSimpleProgram reads the program of a simple font, as its font
// descriptor gives it; nil for none that says anything.
func readSimpleProgram(f *file.File, descriptor syntax.Value) *simpleProgram {
	p := &simpleProgram{}
	var tables map[string][]byte
	switch {
	case !descriptor.Get("FontFile3").IsNull():
		stm := f.Resolve(descriptor.Get("FontFile3"))
		data, _ := f.Decode(stm, nil, maxProgramBytes)
		switch {
		case f.Resolve(stm.Get("Subtype")).IsName("OpenType"):
			tables = sfntTables(data)
			if cff := tables["CFF "]; cff != nil {
				p.names, p.standard, p.cff = cffEncoding(cff)
			}
		default:
			p.names, p.standard, p.cff = cffEncoding(data)
		}
	case !descriptor.Get("FontFile2").IsNull():
		data, _ := f.Decode(f.Resolve(descriptor.Get("FontFile2")), nil, maxProgramBytes)
		tables = sfntTables(data)
	}
	if tables != nil {
		p.readTrueType(tables)
	}
	if p.cff && scrambled(&p.names) {
		p.cff = false
	}
	if !p.cff && !p.mapped && !p.windows && p.tables == nil {
		return nil
	}
	return p
}

// readTrueType reads the codes of a TrueType program's tables: the glyph
// of each code, and its character. Only the glyphs of codes are looked
// at; the names of the others are read when a difference asks for one.
func (p *simpleProgram) readTrueType(tables map[string][]byte) {
	cmap := tables["cmap"]
	p.tables = tables
	var gids [256]int
	switch {
	case cmapSubtable(cmap, 3, 0) != nil:
		sub := cmapSubtable(cmap, 3, 0)
		for c := range 256 {
			for _, at := range [...]int{c, 0xF000 + c, 0xF100 + c, 0xF200 + c} {
				if g := lookupGlyph(sub, at); g > 0 {
					gids[c] = g
					break
				}
			}
		}
		p.mapped = true
	case unicodeSubtable(cmap) != nil:
		// No symbol subtable, and one for Unicode: the codes are those of
		// an encoding for Windows, whatever the font's flags say.
		p.windows = true
		return
	case cmapSubtable(cmap, 1, 0) != nil:
		sub := cmapSubtable(cmap, 1, 0)
		for c := range 256 {
			gids[c] = lookupGlyph(sub, c)
		}
		p.mapped = true
	default:
		return
	}
	// The characters of those glyphs, in one pass over the Unicode
	// subtable.
	var need [1 << 10]uint64
	for _, g := range gids {
		if g > 0 && g < 1<<16 {
			need[g>>6] |= 1 << (g & 63)
		}
	}
	chars := map[int]rune{}
	private := func(r rune) bool { return r >= 0xE000 && r <= 0xF8FF }
	if sub := unicodeSubtable(cmap); sub != nil {
		eachMapping(sub, func(c, g int) {
			if g <= 0 || g >= 1<<16 || need[g>>6]&(1<<(g&63)) == 0 || c <= 0 || c > utf8.MaxRune || c >= 0xD800 && c <= 0xDFFF {
				return
			}
			if old := chars[g]; old == 0 || private(old) && !private(rune(c)) {
				chars[g] = rune(c)
			}
		})
	}
	post := readPost(tables["post"])
	for c, g := range gids {
		switch {
		case g <= 0:
		case chars[g] != 0:
			p.text[c] = string(chars[g])
		default:
			if t, ok := charset.GlyphText(post.name(g)); ok && post.name(g) != ".notdef" {
				p.text[c] = t
			}
		}
	}
}

// nameChar returns the character of the glyph a TrueType program names
// so in its 'post' table, by its Unicode subtable; ok is false for none.
// The names are read at the first asking.
func (p *simpleProgram) nameChar(name string) (rune, bool) {
	if p.tables == nil {
		return 0, false
	}
	if p.byName == nil {
		p.byName = map[string]rune{}
		chars := glyphChars(p.tables["cmap"])
		post := readPost(p.tables["post"])
		for g := 1; g < post.n && g < len(chars); g++ {
			if n := post.name(g); n != "" && n != ".notdef" && chars[g] != 0 {
				if _, dup := p.byName[n]; !dup {
					p.byName[n] = chars[g]
				}
			}
		}
	}
	r, ok := p.byName[name]
	return r, ok
}

// scrambled reports whether the glyph names of an encoding are not the
// characters of their codes: of the codes of ASCII letters and digits
// whose names are ASCII characters too, most name another one.
func scrambled(names *[256]string) bool {
	total, same := 0, 0
	for c := '0'; c <= 'z'; c++ {
		if !(c >= '0' && c <= '9' || c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z') || names[c] == "" {
			continue
		}
		t, ok := charset.GlyphText(names[c])
		if !ok || len(t) != 1 || t[0] < 0x21 || t[0] > 0x7E {
			continue
		}
		total++
		if rune(t[0]) == c {
			same++
		}
	}
	return total >= 8 && 2*same < total
}
