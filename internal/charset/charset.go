// Package charset holds the fixed tables that turn what a PDF file says
// about text into Unicode: glyph names, the encodings the specification
// names, the widths of the standard fonts, and the encoding of text
// strings outside content streams.
//
// The tables are generated (see gen/gen.go) and are read in place: no
// map is built at start-up.
package charset

import (
	"bytes"
	"sort"
	"strings"
	"sync"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"
)

// --- glyph names ---

// lookup returns the text the Adobe Glyph List gives a name.
func lookup(name string) (string, bool) {
	n := len(glyphVals)
	i := sort.Search(n, func(i int) bool { return glyphNames[glyphOffs[i]:glyphOffs[i+1]] >= name })
	if i == n || glyphNames[glyphOffs[i]:glyphOffs[i+1]] != name {
		return "", false
	}
	v := glyphVals[i]
	if v&(1<<31) == 0 {
		return letters(rune(v)), true
	}
	pos, count := int(v&^(1<<31))>>4, int(v&15)
	return string(glyphSeqs[pos : pos+count]), true
}

// --- ligatures and presentation forms ---

// ligatures are the letters of the Latin ligatures U+FB00 to U+FB06.
// Text leaves this library with the letters and never with the ligature
// character, whichever of the two a file states: "fi" is what a reader
// sees and what a search looks for, and the single glyph is how a font
// happens to draw it.
//
// So with the Arabic presentation forms, U+FB50 to U+FDFF and U+FE70 to
// U+FEFF (arabicforms_gen.go): the form of a letter at the start, in the
// middle or at the end of a word is the letter, and a ligature its
// letters. They are the shapes a font draws, which some files give as
// the text of their glyphs.
var ligatures = [...]string{"ff", "fi", "fl", "ffi", "ffl", "st", "st"}

const ligatureFirst, ligatureLast = 0xFB00, 0xFB06

// formLetters returns the letters of a ligature or a presentation form;
// false for any other character.
func formLetters(r rune) (string, bool) {
	switch {
	case r >= ligatureFirst && r <= ligatureLast:
		return ligatures[r-ligatureFirst], true
	case r >= arabicFormsFirst && r <= arabicFormsLast:
		i := r - arabicFormsFirst
		if t := arabicFormText[arabicFormOffs[i]:arabicFormOffs[i+1]]; t != "" {
			return t, true
		}
	}
	return "", false
}

// letters returns a character as text, a ligature or a presentation
// form as its letters.
func letters(r rune) string {
	if t, ok := formLetters(r); ok {
		return t
	}
	return string(r)
}

// formAt returns the letters of the ligature or presentation form that
// b starts with. All are three bytes in UTF-8, the first of them 0xEF.
func formAt[T string | []byte](b T) (string, bool) {
	if len(b) < 3 || b[0] != 0xEF || b[1]&0xC0 != 0x80 || b[2]&0xC0 != 0x80 {
		return "", false
	}
	return formLetters(rune(b[1]&0x3F)<<6 | rune(b[2]&0x3F) | 0xF000)
}

// Letters returns s with the ligatures and presentation forms written as
// their letters.
func Letters(s string) string {
	i := 0
	for {
		k := strings.IndexByte(s[i:], 0xEF)
		if k < 0 {
			return s
		}
		i += k
		if _, ok := formAt(s[i:]); ok {
			break
		}
		i++
	}
	var sb strings.Builder
	sb.Grow(len(s) + 8)
	sb.WriteString(s[:i])
	for i < len(s) {
		if t, ok := formAt(s[i:]); ok {
			sb.WriteString(t)
			i += 3
			continue
		}
		sb.WriteByte(s[i])
		i++
	}
	return sb.String()
}

// LettersIn does to b[from:] what Letters does to a string and returns
// b, which can have become shorter or longer.
func LettersIn(b []byte, from int) []byte {
	i := from
	for {
		k := bytes.IndexByte(b[i:], 0xEF)
		if k < 0 {
			return b
		}
		i += k
		if _, ok := formAt(b[i:]); ok {
			break
		}
		i++
	}
	// The letters of a form can be longer than it: they are written
	// apart and copied back.
	tail := make([]byte, 0, len(b)-i+16)
	for j := i; j < len(b); {
		if t, ok := formAt(b[j:]); ok {
			tail = append(tail, t...)
			j += 3
			continue
		}
		tail = append(tail, b[j])
		j++
	}
	return append(b[:i], tail...)
}

// isPrivateUse reports whether text is in the private use area, where
// the glyph list puts glyphs that are a variant of a character and have
// no character of their own: small capitals, old-style figures.
func isPrivateUse(text string) bool {
	r, _ := utf8.DecodeRuneInString(text)
	return r >= 0xE000 && r <= 0xF8FF
}

// variantSuffixes are the endings of glyph names for a variant of a
// character. The text of the variant is the text of the character.
var variantSuffixes = [...]string{"small", "oldstyle", "superior", "inferior", "fitted"}

// component returns the text of one component of a glyph name: a name
// of the glyph list, uniXXXX (one or more UTF-16 units), or uXXXX to
// uXXXXXX (one code point).
func component(name string, dingbats bool) (string, bool) {
	if !dingbats && isDingbatName(name) {
		return "", false
	}
	text, listed := lookup(name)
	if listed && !isPrivateUse(text) {
		return text, true
	}
	if rest, ok := strings.CutPrefix(name, "uni"); ok && len(rest) >= 4 && len(rest)%4 == 0 {
		units := make([]uint16, 0, len(rest)/4)
		for i := 0; i < len(rest); i += 4 {
			v, ok := upperHex(rest[i : i+4])
			if !ok {
				units = nil
				break
			}
			units = append(units, uint16(v))
		}
		if units != nil {
			// Half of a surrogate pair by itself is no character.
			if text := string(utf16.Decode(units)); !strings.ContainsRune(text, utf8.RuneError) {
				return Letters(text), true
			}
		}
	}
	if rest, ok := strings.CutPrefix(name, "u"); ok && len(rest) >= 4 && len(rest) <= 6 {
		if v, ok := upperHex(rest); ok && v <= unicode.MaxRune && (v < 0xD800 || v > 0xDFFF) {
			return letters(rune(v)), true
		}
	}
	// A variant of a character: "Asmall" is a small capital A, set in
	// place of a lower-case a; "threeoldstyle" is the figure 3.
	for _, suffix := range variantSuffixes {
		base, ok := strings.CutSuffix(name, suffix)
		if !ok || base == "" {
			continue
		}
		if text, ok := lookup(base); ok && !isPrivateUse(text) {
			if suffix == "small" {
				text = strings.ToLower(text)
			}
			return text, true
		}
	}
	if listed {
		return text, true // a private use character is all there is
	}
	return "", false
}

func upperHex(s string) (rune, bool) {
	var v rune
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= '0' && c <= '9':
			v = v<<4 | rune(c-'0')
		case c >= 'A' && c <= 'F':
			v = v<<4 | rune(c-'A'+10)
		default:
			return 0, false
		}
	}
	return v, true
}

// isDingbatName reports whether name has the form of the glyph names of
// ITC Zapf Dingbats: a1 to a206. They mean something in that font only;
// elsewhere "a12" is a name some program made up.
func isDingbatName(name string) bool {
	if len(name) < 2 || name[0] != 'a' {
		return false
	}
	for i := 1; i < len(name); i++ {
		if name[i] < '0' || name[i] > '9' {
			return false
		}
	}
	return true
}

// GlyphText returns the text a glyph name stands for, following the
// Adobe Glyph List specification: what follows the first period is
// dropped ("a.alt" is "a"), the rest is split at underscores ("f_f_i" is
// "ffi"), and each part is a name of the list, uniXXXX or uXXXXXX.
//
// ok is false when the name says nothing about the text, as "g23" or
// "cid1234" do. The names of the Zapf Dingbats glyphs are read by
// DingbatText only.
func GlyphText(name string) (text string, ok bool) { return glyphText(name, false) }

// DingbatText is GlyphText for a glyph of the ZapfDingbats font, whose
// names are a1 to a206.
func DingbatText(name string) (text string, ok bool) { return glyphText(name, true) }

func glyphText(name string, dingbats bool) (text string, ok bool) {
	if i := strings.IndexByte(name, '.'); i >= 0 {
		name = name[:i]
	}
	if name == "" {
		return "", false
	}
	if !strings.Contains(name, "_") {
		return component(name, dingbats)
	}
	var sb strings.Builder
	for _, part := range strings.Split(name, "_") {
		t, ok := component(part, dingbats)
		if !ok {
			return "", false
		}
		sb.WriteString(t)
	}
	return sb.String(), true
}

// --- encodings ---

// Encoding gives the text of each of the 256 codes of a simple font;
// an empty string is a code with no character.
type Encoding [256]string

// Dingbats reports whether the font's glyphs are named as those of
// ZapfDingbats are.
func (c *CoreFont) Dingbats() bool { return c.byCode == &widthsZapfDingbats }

func fromRunes(table *[256]uint16) *Encoding {
	var e Encoding
	for code, r := range table {
		if r != 0 {
			e[code] = letters(rune(r))
		}
	}
	return &e
}

var (
	// Standard is StandardEncoding, WinAnsi is WinAnsiEncoding and
	// MacRoman is MacRomanEncoding. Symbol and ZapfDingbats are the
	// built-in encodings of the two standard fonts of those names.
	Standard     = sync.OnceValue(func() *Encoding { return fromRunes(&standardEncoding) })
	WinAnsi      = sync.OnceValue(func() *Encoding { return fromRunes(&winAnsiEncoding) })
	MacRoman     = sync.OnceValue(func() *Encoding { return fromRunes(&macRomanEncoding) })
	Symbol       = sync.OnceValue(func() *Encoding { return fromRunes(&symbolEncoding) })
	ZapfDingbats = sync.OnceValue(func() *Encoding { return fromRunes(&zapfDingbatsEncoding) })
	// MacExpert is MacExpertEncoding. Its glyphs are mostly variants of
	// characters; see GlyphText for what they become.
	MacExpert = sync.OnceValue(func() *Encoding {
		var e Encoding
		for code, name := range macExpertNames {
			if name != "" {
				e[code], _ = GlyphText(name)
			}
		}
		return &e
	})
)

// Named returns the encoding with the given PDF name, or nil.
func Named(name string) *Encoding {
	switch name {
	case "StandardEncoding":
		return Standard()
	case "WinAnsiEncoding":
		return WinAnsi()
	case "MacRomanEncoding":
		return MacRoman()
	case "MacExpertEncoding":
		return MacExpert()
	}
	return nil
}

// --- standard fonts ---

// CoreFont describes one of the 14 standard fonts, which a file may use
// without stating their widths.
type CoreFont struct {
	// Builtin is the font's own encoding: Symbol or ZapfDingbats for
	// those two, StandardEncoding for the others.
	Builtin *Encoding
	// Symbolic is true for Symbol and ZapfDingbats.
	Symbolic bool

	fixed  uint16       // the one width of a fixed-pitch font, or 0
	byRune []runeWidth  // widths by character, sorted
	byCode *[256]uint16 // widths by code in the built-in encoding
}

// Width returns the width of the glyph for a code whose text is text, in
// thousandths of the font size. ok is false when the font has no such
// glyph.
func (c *CoreFont) Width(code byte, text string) (width float64, ok bool) {
	switch {
	case c.fixed != 0:
		return float64(c.fixed), true
	case c.byCode != nil:
		w := c.byCode[code]
		return float64(w), w != 0
	}
	r, size := utf8.DecodeRuneInString(text)
	if size == 0 || size != len(text) {
		return 0, false
	}
	i := sort.Search(len(c.byRune), func(i int) bool { return c.byRune[i].r >= r })
	if i < len(c.byRune) && c.byRune[i].r == r {
		return float64(c.byRune[i].w), true
	}
	return 0, false
}

// Core returns the standard font a /BaseFont name refers to, or nil. A
// subset tag is ignored, and so are the differences between the usual
// spellings: "Arial,Bold", "Arial-BoldMT" and "Helvetica-Bold" have the
// same metrics.
func Core(baseFont string) *CoreFont {
	name := baseFont
	if len(name) > 7 && name[6] == '+' {
		name = name[7:]
	}
	name = strings.ToLower(name)
	bold := strings.Contains(name, "bold")
	italic := strings.Contains(name, "italic") || strings.Contains(name, "oblique")
	name = strings.NewReplacer(" ", "", "-", "", ",", "", "_", "").Replace(name)
	switch {
	case strings.HasPrefix(name, "helvetica"), strings.HasPrefix(name, "arial"):
		if strings.Contains(name, "narrow") || strings.Contains(name, "condensed") || strings.Contains(name, "black") {
			return nil
		}
		if bold {
			return &CoreFont{Builtin: Standard(), byRune: widthsHelveticaBold[:]}
		}
		return &CoreFont{Builtin: Standard(), byRune: widthsHelvetica[:]}
	case strings.HasPrefix(name, "times"):
		table := widthsTimesRoman[:]
		switch {
		case bold && italic:
			table = widthsTimesBoldItalic[:]
		case bold:
			table = widthsTimesBold[:]
		case italic:
			table = widthsTimesItalic[:]
		}
		return &CoreFont{Builtin: Standard(), byRune: table}
	case strings.HasPrefix(name, "courier"):
		return &CoreFont{Builtin: Standard(), fixed: 600}
	case strings.HasPrefix(name, "symbol"):
		return &CoreFont{Builtin: Symbol(), Symbolic: true, byCode: &widthsSymbol}
	case strings.HasPrefix(name, "zapfdingbats"):
		return &CoreFont{Builtin: ZapfDingbats(), Symbolic: true, byCode: &widthsZapfDingbats}
	}
	return nil
}

// --- text strings ---

// TextString decodes a text string as PDF stores it outside content
// streams, in the document information and in /ActualText: UTF-16 with a
// byte order mark, UTF-8 with one, or PDFDocEncoding. The result is
// valid UTF-8; a byte with no character becomes U+FFFD, and a ligature
// is written as its letters.
func TextString(b []byte) string {
	return Letters(textString(b))
}

func textString(b []byte) string {
	switch {
	case len(b) >= 2 && b[0] == 0xFE && b[1] == 0xFF:
		return decodeUTF16(b[2:], true)
	case len(b) >= 2 && b[0] == 0xFF && b[1] == 0xFE:
		return decodeUTF16(b[2:], false)
	case len(b) >= 3 && b[0] == 0xEF && b[1] == 0xBB && b[2] == 0xBF:
		return strings.ToValidUTF8(string(b[3:]), "�")
	}
	ascii := true
	for _, c := range b {
		if c >= 0x7F || c < 0x20 && c != '\t' && c != '\n' && c != '\r' {
			ascii = false
			break
		}
	}
	if ascii {
		return string(b)
	}
	var sb strings.Builder
	sb.Grow(len(b) + 8)
	for _, c := range b {
		if r := pdfDocEncoding[c]; r != 0 {
			sb.WriteRune(rune(r))
		} else {
			sb.WriteRune(utf8.RuneError)
		}
	}
	return sb.String()
}

// decodeUTF16 decodes UTF-16 text. A unit that is half of a pair without
// its other half becomes U+FFFD, and so does a trailing odd byte.
func decodeUTF16(b []byte, bigEndian bool) string {
	units := make([]uint16, 0, len(b)/2+1)
	for i := 0; i+1 < len(b); i += 2 {
		if bigEndian {
			units = append(units, uint16(b[i])<<8|uint16(b[i+1]))
		} else {
			units = append(units, uint16(b[i+1])<<8|uint16(b[i]))
		}
	}
	s := string(utf16.Decode(units))
	if len(b)%2 != 0 {
		s += "�"
	}
	return s
}
