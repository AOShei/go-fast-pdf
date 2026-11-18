package charset

import (
	"sort"
	"testing"
	"unicode/utf8"
)

func TestGlyphText(t *testing.T) {
	cases := []struct{ name, want string }{
		// Names of the list.
		{"A", "A"}, {"Lslash", "Ł"}, {"Euro", "€"}, {"afii10017", "А"}, {"space", " "},
		{"fi", "fi"}, {"ffl", "ffl"}, {"quotedblleft", "“"}, {"dalethatafpatah", "דֲ"},
		// uniXXXX and uXXXXXX.
		{"uni00E9", "é"}, {"uni00410042", "AB"}, {"uniD83DDE00", "😀"}, {"u1F600", "😀"}, {"u00E9", "é"},
		// Suffixes and compositions.
		{"a.alt", "a"}, {"f_f_i", "ffi"}, {"f_f_i.liga", "ffi"}, {"T_h", "Th"}, {"uni0041_uni0042.x", "AB"},
		// Variants of a character.
		{"Asmall", "a"}, {"Eacutesmall", "é"}, {"exclamsmall", "!"}, {"threeoldstyle", "3"},
		{"onesuperior", "¹"}, {"asuperior", "a"}, {"dollarinferior", "$"}, {"onefitted", "1"},
	}
	for _, c := range cases {
		if got, ok := GlyphText(c.name); !ok || got != c.want {
			t.Errorf("GlyphText(%q) = %q, %v; want %q", c.name, got, ok, c.want)
		}
	}
	// The names of the Zapf Dingbats glyphs mean something in that font
	// only.
	if got, ok := DingbatText("a100"); !ok || got != "❞" {
		t.Errorf("DingbatText(a100) = %q, %v", got, ok)
	}
	if got, ok := DingbatText("eacute"); !ok || got != "é" {
		t.Errorf("DingbatText(eacute) = %q, %v", got, ok)
	}
	// Names that say nothing about the text.
	for _, name := range []string{"", ".notdef", "g23", "a100", "a1", "a1_a2", "cid1234", "G00", "uni12", "uni00G9", "uniD800_x", "u110000", "uD800", "foo_bar", "a_", "small"} {
		if got, ok := GlyphText(name); ok {
			t.Errorf("GlyphText(%q) = %q, want no text", name, got)
		}
	}
}

func TestGlyphListIsSortedAndValid(t *testing.T) {
	n := len(glyphVals)
	if n < 4400 || len(glyphOffs) != n+1 {
		t.Fatalf("%d names, %d offsets", n, len(glyphOffs))
	}
	names := make([]string, n)
	for i := range names {
		names[i] = glyphNames[glyphOffs[i]:glyphOffs[i+1]]
		text, ok := lookup(names[i])
		if !ok || text == "" || !utf8.ValidString(text) {
			t.Errorf("%q: %q, %v", names[i], text, ok)
		}
	}
	if !sort.StringsAreSorted(names) {
		t.Error("the names are not sorted")
	}
}

func TestEncodings(t *testing.T) {
	check := func(name string, e *Encoding, want map[byte]string) {
		t.Helper()
		for code, text := range want {
			if e[code] != text {
				t.Errorf("%s: code %#x is %q, want %q", name, code, e[code], text)
			}
		}
	}
	// ASCII is the same in the three Latin encodings.
	for _, e := range []*Encoding{WinAnsi(), MacRoman()} {
		for c := byte(0x20); c < 0x7F; c++ {
			if e[c] != string(rune(c)) {
				t.Errorf("code %#x is %q", c, e[c])
			}
		}
	}
	check("Standard", Standard(), map[byte]string{'A': "A", 0x27: "’", 0x60: "‘", 0xAE: "fi", 0xE1: "Æ", 0xFA: "œ", 0x80: "", 0x00: ""})
	check("WinAnsi", WinAnsi(), map[byte]string{0x80: "€", 0x93: "“", 0x94: "”", 0x96: "–", 0xE9: "é", 0xEF: "ï", 0x27: "'", 0x81: "•", 0xA0: " ", 0xAD: "-", 0x1F: ""})
	check("MacRoman", MacRoman(), map[byte]string{0x80: "Ä", 0x8E: "é", 0xA5: "•", 0xD2: "“", 0xDB: "¤", 0xDE: "fi", 0xCA: " "})
	check("MacExpert", MacExpert(), map[byte]string{0x20: " ", 0x21: "!", 0x30: "0", 0x56: "ff", 0x57: "fi", 0x61: "a", 0x7A: "z", 0x47: "¼", 0x23: "¢"})
	check("Symbol", Symbol(), map[byte]string{0x61: "α", 0x44: "∆", 0xA5: "∞", 0x20: " "})
	check("ZapfDingbats", ZapfDingbats(), map[byte]string{0x21: "✁", 0x6C: "●", 0x20: " "})
	if Named("WinAnsiEncoding") != WinAnsi() || Named("Identity-H") != nil {
		t.Error("Named")
	}
}

func TestCore(t *testing.T) {
	width := func(base string, code byte, text string) float64 {
		t.Helper()
		c := Core(base)
		if c == nil {
			t.Fatalf("%s is not recognised", base)
		}
		w, _ := c.Width(code, text)
		return w
	}
	cases := []struct {
		base string
		code byte
		text string
		want float64
	}{
		{"Helvetica", 'i', "i", 222}, {"Helvetica", 'W', "W", 944}, {"Helvetica", ' ', " ", 278},
		{"Helvetica-Oblique", 'W', "W", 944}, {"Helvetica-Bold", 'i', "i", 278}, {"ABCDEF+Helvetica-BoldOblique", 'i', "i", 278},
		{"Arial", 'W', "W", 944}, {"Arial,Bold", 'i', "i", 278}, {"Arial-BoldMT", 'i', "i", 278}, {"ArialMT", 'i', "i", 222},
		{"Times-Roman", 'a', "a", 444}, {"TimesNewRomanPSMT", 'a', "a", 444}, {"Times New Roman,BoldItalic", 'a', "a", 500},
		{"Times-Bold", 'a', "a", 500}, {"Times-Italic", 'a', "a", 500},
		{"Helvetica", 0xE9, "é", 556}, {"Helvetica", 0x80, "€", 556},
		{"Courier", 'i', "i", 600}, {"CourierNewPS-BoldMT", 'W', "W", 600},
		{"Symbol", 0x61, "α", 631}, {"ZapfDingbats", 0x21, "✁", 974},
	}
	for _, c := range cases {
		if got := width(c.base, c.code, c.text); got != c.want {
			t.Errorf("%s %q: width %v, want %v", c.base, c.text, got, c.want)
		}
	}
	if _, ok := Core("Helvetica").Width(1, "中"); ok {
		t.Error("a width for a glyph Helvetica does not have")
	}
	for _, base := range []string{"", "Garamond", "ArialNarrow", "Helvetica-Condensed", "Arial-Black", "ABCDEF+", "CMR10"} {
		if Core(base) != nil {
			t.Errorf("%q is taken for a standard font", base)
		}
	}
	if !Core("Symbol").Symbolic || Core("Times-Roman").Symbolic || Core("Symbol").Builtin != Symbol() {
		t.Error("Symbolic or Builtin is wrong")
	}
}

func TestTextString(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"plain", "plain"},
		{"R\xe9sum\xe9", "Résumé"},
		{"\x8d quoted \x8e \xa0", "“ quoted ” €"},
		{"\xfe\xff\x00Z\x00o\x00\xeb", "Zoë"},
		{"\xff\xfeZ\x00o\x00\xeb\x00", "Zoë"},
		{"\xef\xbb\xbfZo\xc3\xab", "Zoë"},
		{"\xfe\xff\xd8\x3d\xde\x00", "😀"},
		{"\xfe\xff\xd8\x3d", "�"},   // half a pair
		{"\xfe\xff\x00A\x00", "A�"}, // an odd byte
		{"\xef\xbb\xbf\xff", "�"},   // not UTF-8 after its mark
		{"a\x00b\x9f", "a�b�"},      // bytes with no character
		{"line\nnext\ttab", "line\nnext\ttab"},
	}
	for _, c := range cases {
		got := TextString([]byte(c.in))
		if got != c.want || !utf8.ValidString(got) {
			t.Errorf("TextString(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func FuzzTextString(f *testing.F) {
	f.Add([]byte("\xfe\xff\xd8\x3d"))
	f.Add([]byte("R\xe9sum\xe9"))
	f.Fuzz(func(t *testing.T, b []byte) {
		if s := TextString(b); !utf8.ValidString(s) {
			t.Fatalf("not valid UTF-8: %q", s)
		}
		if s, ok := GlyphText(string(b)); ok && (s == "" || !utf8.ValidString(s)) {
			t.Fatalf("glyph %q: %q", b, s)
		}
	})
}
