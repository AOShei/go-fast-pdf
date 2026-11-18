package font

import (
	"fmt"
	"math"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/AOShei/go-fast-pdf/internal/file"
	"github.com/AOShei/go-fast-pdf/internal/pdftest"
	"github.com/AOShei/go-fast-pdf/internal/syntax"
)

// text returns what a CMap gives a code of n bytes.
func text(c *cmap, code uint32, n int) string {
	out, ok := c.appendText(nil, code, n)
	if !ok {
		return "(none)"
	}
	return string(out)
}

func TestCMap(t *testing.T) {
	c := parseCMap([]byte(`/CIDInit /ProcSet findresource begin 12 dict begin begincmap
1 begincodespacerange <00> <FF> endcodespacerange
4 beginbfchar
<01> <0041>
<02> <00480069>
<0003> <00E9>
<04> <D83DDE00>
endbfchar
3 beginbfrange
<10> <12> <0061>
<20> <22> [<0058> <0059> <005A>]
<30> <32> <D83DDE00>
endbfrange
endcmap`))
	cases := []struct {
		code uint32
		n    int
		want string
	}{
		{0x01, 1, "A"}, {0x02, 1, "Hi"}, {0x04, 1, "😀"},
		{0x10, 1, "a"}, {0x11, 1, "b"}, {0x12, 1, "c"}, {0x13, 1, "(none)"},
		{0x20, 1, "X"}, {0x21, 1, "Y"}, {0x22, 1, "Z"}, {0x23, 1, "(none)"},
		// A range whose text is outside the basic plane counts up as a
		// character, not as a UTF-16 unit.
		{0x30, 1, "😀"}, {0x31, 1, "😁"}, {0x32, 1, "😂"},
		// Two bytes.
		{0x0003, 2, "é"},
		// A code asked for with another length than the CMap lists it
		// with is found by its value.
		{0x03, 1, "é"}, {0x0001, 2, "A"}, {0x0011, 2, "b"},
		{0x05, 1, "(none)"}, {0x0005, 2, "(none)"},
	}
	for _, tc := range cases {
		if got := text(c, tc.code, tc.n); got != tc.want {
			t.Errorf("code %#x (%d bytes): %q, want %q", tc.code, tc.n, got, tc.want)
		}
	}
}

func TestCMapRanges(t *testing.T) {
	t.Run("a range inside a range", func(t *testing.T) {
		// The specific entries win inside the wide range, and the wide
		// range is still found after them.
		c := parseCMap([]byte(`1 beginbfrange <0000> <FFFF> <0000> endbfrange
3 beginbfchar <0041> <005A> <0043> <0059> <0100> <0058> endbfchar
1 beginbfrange <0050> <0052> <0031> endbfrange`))
		for code, want := range map[uint32]string{0x40: "@", 0x41: "Z", 0x42: "B", 0x43: "Y", 0x44: "D",
			0x4F: "O", 0x50: "1", 0x52: "3", 0x53: "S", 0x100: "X", 0x101: "ā", 0xFFFF: "￿"} {
			if got := text(c, code, 2); got != want {
				t.Errorf("code %#x: %q, want %q", code, got, want)
			}
		}
	})
	t.Run("the later of two entries for a code wins", func(t *testing.T) {
		c := parseCMap([]byte("2 beginbfchar <41> <0058> <41> <0059> endbfchar"))
		if got := text(c, 0x41, 1); got != "Y" {
			t.Errorf("%q", got)
		}
	})
	t.Run("a huge range costs one entry", func(t *testing.T) {
		// Defect C6: this used to be expanded code by code.
		start := time.Now()
		c := parseCMap([]byte("1 begincodespacerange <00000000> <FFFFFFFF> endcodespacerange\n1 beginbfrange <00000000> <FFFFFFFF> <0041> endbfrange"))
		if len(c.entries) != 1 || time.Since(start) > time.Second {
			t.Fatalf("%d entries in %v", len(c.entries), time.Since(start))
		}
		if got := text(c, 0, 4); got != "A" {
			t.Errorf("first code: %q", got)
		}
		if got := text(c, 3, 4); got != "D" {
			t.Errorf("code 3: %q", got)
		}
		// Past the end of Unicode the text is the replacement character.
		if got := text(c, 0xFFFFFFFF, 4); got != "�" {
			t.Errorf("last code: %q", got)
		}
	})
	t.Run("text that counts into the surrogates", func(t *testing.T) {
		c := parseCMap([]byte("1 beginbfrange <00> <FF> <D7FE> endbfrange"))
		for code, want := range map[uint32]string{0: "퟾", 1: "퟿", 2: "�", 0xFF: "�"} {
			if got := text(c, code, 1); got != want {
				t.Errorf("code %d: %q, want %q", code, got, want)
			}
		}
	})
	t.Run("a glyph name as the text", func(t *testing.T) {
		c := parseCMap([]byte("2 beginbfchar <01> /eacute <02> /nosuchglyphname endbfchar"))
		if text(c, 1, 1) != "é" || text(c, 2, 1) != "(none)" {
			t.Errorf("%q, %q", text(c, 1, 1), text(c, 2, 1))
		}
	})
	t.Run("CIDs", func(t *testing.T) {
		c := parseCMap([]byte(`/WMode 1 def /Parent usecmap
1 begincodespacerange <0000> <FFFF> endcodespacerange
1 begincidchar <0041> 7 endcidchar
1 begincidrange <0100> <01FF> 1000 endcidrange`))
		if c.cid(0x41, 2) != 7 || c.cid(0x100, 2) != 1000 || c.cid(0x1FF, 2) != 1255 || c.cid(0x200, 2) != 0 {
			t.Errorf("%d %d %d %d", c.cid(0x41, 2), c.cid(0x100, 2), c.cid(0x1FF, 2), c.cid(0x200, 2))
		}
		if !c.vertical || c.parent != "Parent" {
			t.Errorf("vertical %v, parent %q", c.vertical, c.parent)
		}
		if _, ok := c.appendText(nil, 0x41, 2); ok {
			t.Error("a CID entry gave text")
		}
	})
}

func TestCMapDamage(t *testing.T) {
	t.Run("PostScript procedure", func(t *testing.T) {
		// Defect C4: a brace must not stop the parser.
		c := parseCMap([]byte("/CMapName /X def { pop } bind def\n1 beginbfchar <01> <0041> endbfchar"))
		if text(c, 1, 1) != "A" {
			t.Error("the mapping after the procedure is lost")
		}
	})
	t.Run("ends inside a section", func(t *testing.T) {
		c := parseCMap([]byte("3 beginbfchar <01> <0041> <02>"))
		if text(c, 1, 1) != "A" {
			t.Error("the mapping before the end is lost")
		}
	})
	t.Run("surrogates and odd lengths", func(t *testing.T) {
		c := parseCMap([]byte("5 beginbfchar <01> <D800> <02> <00E9E9> <03> <DC00D800> <04> <41> <05> <> endbfchar"))
		for code, want := range map[uint32]string{1: "�", 2: "é", 3: "��", 4: "A", 5: ""} {
			if got := text(c, code, 1); got != want || !utf8.ValidString(got) {
				t.Errorf("code %d: %q, want %q", code, got, want)
			}
		}
	})
	t.Run("inverted, mismatched and oversized", func(t *testing.T) {
		c := parseCMap([]byte(`4 beginbfrange
<20> <10> <0041>
<10> <0020> <0041>
<0102030405> <0102030406> <0041>
<30> <31> [<0041> <0042> <0043> <0044>]
endbfrange
1 beginbfchar <0102030405> <0041> endbfchar`))
		if len(c.entries) != 2 || text(c, 0x30, 1) != "A" || text(c, 0x31, 1) != "B" || text(c, 0x32, 1) != "(none)" {
			t.Errorf("%d entries; %q %q %q", len(c.entries), text(c, 0x30, 1), text(c, 0x31, 1), text(c, 0x32, 1))
		}
	})
	t.Run("more entries than are kept", func(t *testing.T) {
		var sb strings.Builder
		sb.WriteString("beginbfchar\n")
		for i := 0; i < maxEntries+10; i++ {
			fmt.Fprintf(&sb, "<%06X> <0041>\n", i)
		}
		sb.WriteString("endbfchar")
		c := parseCMap([]byte(sb.String()))
		if len(c.entries) != maxEntries || !c.truncated {
			t.Errorf("%d entries, truncated %v", len(c.entries), c.truncated)
		}
	})
}

func TestCodespace(t *testing.T) {
	split := func(s *codespace, in string) string {
		var out []string
		for b := []byte(in); len(b) > 0; {
			code, n := s.next(b)
			out = append(out, fmt.Sprintf("%0*X", 2*n, code))
			b = b[n:]
		}
		return strings.Join(out, " ")
	}
	parse := func(ranges string) *codespace {
		return &parseCMap([]byte("begincodespacerange " + ranges + " endcodespacerange")).space
	}
	one, two := parse("<00> <FF>"), parse("<0000> <FFFF>")
	if got := split(one, "\x41\x42\xFF"); got != "41 42 FF" {
		t.Errorf("one byte: %s", got)
	}
	// Defect F2: codes stay aligned whatever their values are.
	if got := split(two, "\x00\x41\xFF\x42\x00\x42"); got != "0041 FF42 0042" {
		t.Errorf("two bytes: %s", got)
	}
	if got := split(two, "\x00\x41\x42"); got != "0041 42" {
		t.Errorf("an odd byte at the end: %s", got)
	}
	// One and two bytes, decided by the first byte.
	mixed := parse("<00> <80> <8140> <9FFC> <A0> <DF> <E040> <FCFC>")
	if got := split(mixed, "A\x81\x40B\xA1\xE0\x41\x7F"); got != "41 8140 42 A1 E041 7F" {
		t.Errorf("mixed: %s", got)
	}
	// One and two bytes that start with the same byte: the second byte
	// decides.
	shared := parse("<80> <80> <8041> <80FF>")
	if got := split(shared, "\x80\x41\x80\x20\x80"); got != "8041 80 20 80" {
		t.Errorf("shared first byte: %s", got)
	}
	// A byte that starts no range is taken with the shortest length.
	sparse := parse("<2000> <20FF> <300000> <30FFFF>")
	if got := split(sparse, "\x20\x41\x99\x98\x30\x01\x02"); got != "2041 9998 300102" {
		t.Errorf("sparse: %s", got)
	}
	var empty codespace
	empty.finish()
	if got := split(&empty, "AB"); got != "41 42" {
		t.Errorf("no ranges: %s", got)
	}
	// The predefined CMaps.
	for name, want := range map[string]string{
		"Identity-H":    "4181 4042 E041",
		"UniJIS-UCS2-H": "4181 4042 E041",
		"90ms-RKSJ-H":   "41 8140 42 E041",
		"GBK-EUC-H":     "41 8140 42 E041",
		"ETen-B5-V":     "41 8140 42 E041",
		"KSCms-UHC-H":   "41 8140 42 E041",
		"UniGB-UTF16-H": "4181 4042 E041",
		"UniGB-UTF32-H": "41814042 E041",
		"GB-H":          "4181 4042 E041",
	} {
		s, _, _, known := predefined(name)
		if got := split(&s, "A\x81\x40B\xE0\x41"); !known || got != want {
			t.Errorf("%s: %s (known %v), want %s", name, got, known, want)
		}
	}
	if _, _, vertical, _ := predefined("Identity-V"); !vertical {
		t.Error("Identity-V is not vertical")
	}
	if _, _, _, known := predefined("MyOwnCMap"); known {
		t.Error("an unknown name is known")
	}
	s, form, _, _ := predefined("UniJIS-UTF16-H")
	if got := split(&s, "\x00A\xD8\x3D\xDE\x00\xFF\xFF"); got != "0041 D83DDE00 FFFF" || form != unicodeUTF16 {
		t.Errorf("UTF-16: %s", got)
	}
}

// load builds a file with the given objects (numbered from 1) and loads
// the last one as a font.
func load(t testing.TB, dict string, extra ...string) *Font {
	t.Helper()
	b := pdftest.New()
	for _, e := range extra {
		b.Add(e)
	}
	font := b.Add(dict)
	data := pdftest.Doc(pdftest.DocOptions{Base: b}, pdftest.PageSpec{}).Bytes()
	f, err := file.Open(data, file.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	return Load(f, syntax.Value{Kind: syntax.Ref, N: int64(font)})
}

// show returns the text of a string in a font, with its advance.
func show(f *Font, s string) (string, Shown) {
	out, sh := f.Append(nil, []byte(s))
	return string(out), sh
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-4 }

func allBytes() string {
	b := make([]byte, 256)
	for i := range b {
		b[i] = byte(i)
	}
	return string(b)
}

func TestSimpleFontEncodings(t *testing.T) {
	// Every code of every base encoding: the text of code c is what the
	// encoding says, and a code with no character is U+FFFD.
	t.Run("the full byte range of each base encoding", func(t *testing.T) {
		for _, tc := range []struct {
			dict string
			want map[byte]string
		}{
			{"/Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding", map[byte]string{'A': "A", 0xE9: "é", 0x93: "“", 0x94: "”", 0x96: "–", 0x80: "€", 0xEF: "ï", 0x00: ""}},
			{"/Subtype /Type1 /BaseFont /Helvetica /Encoding /MacRomanEncoding", map[byte]string{'A': "A", 0x8E: "é", 0xD2: "“", 0xD0: "–", 0xDB: "¤"}},
			{"/Subtype /Type1 /BaseFont /Helvetica /Encoding /StandardEncoding", map[byte]string{'A': "A", 0x27: "’", 0xAE: "fi", 0xE1: "Æ", 0xE9: "Ø", 0x80: ""}},
			{"/Subtype /Type1 /BaseFont /Helvetica /Encoding /MacExpertEncoding", map[byte]string{0x61: "a", 0x30: "0", 0x57: "fi"}},
			// No /Encoding: the built-in one of a standard font, the
			// usual one of the font's kind otherwise.
			{"/Subtype /Type1 /BaseFont /Helvetica", map[byte]string{'A': "A", 0x27: "’", 0xE1: "Æ"}},
			{"/Subtype /Type1 /BaseFont /Symbol", map[byte]string{0x61: "α", 0xA5: "∞"}},
			{"/Subtype /Type1 /BaseFont /ZapfDingbats", map[byte]string{0x21: "✁"}},
			{"/Subtype /TrueType /BaseFont /Garamond", map[byte]string{'A': "A", 0xE9: "é", 0x93: "“"}},
			{"/Subtype /Type1 /BaseFont /Garamond", map[byte]string{'A': "A", 0xE9: "Ø"}},
			{"/Subtype /Type1 /BaseFont /Garamond /Encoding << /BaseEncoding /MacRomanEncoding >>", map[byte]string{0x8E: "é"}},
			{"/Subtype /Type1 /BaseFont /Garamond /Encoding /NoSuchEncoding", map[byte]string{0xE9: "Ø"}},
			// A code with no character: in a named encoding, or in a
			// font that is not embedded, it has no glyph and is nothing.
			// In an embedded font that names no encoding it may have
			// one, and is a character that cannot be told.
			{"/Subtype /Type1 /BaseFont /Garamond", map[byte]string{0x80: "", 0x01: ""}},
			{"/Subtype /Type1 /BaseFont /Garamond /FontDescriptor << /FontFile 1 0 R >>", map[byte]string{'A': "A", 0x80: "�", 0x01: "�"}},
			{"/Subtype /Type1 /BaseFont /Garamond /Encoding /WinAnsiEncoding /FontDescriptor << /FontFile 1 0 R >>", map[byte]string{0x01: ""}},
		} {
			f := load(t, "<< /Type /Font "+tc.dict+" >>")
			got, sh := show(f, allBytes())
			if sh.Glyphs != 256 || !utf8.ValidString(got) {
				t.Errorf("%s: %d glyphs, valid %v", tc.dict, sh.Glyphs, utf8.ValidString(got))
			}
			if n := strings.Count(got, "�"); n != sh.Unmapped {
				t.Errorf("%s: %d replacement characters, %d unmapped", tc.dict, n, sh.Unmapped)
			}
			for code, want := range tc.want {
				if got, _ := show(f, string([]byte{code})); got != want {
					t.Errorf("%s: code %#x is %q, want %q", tc.dict, code, got, want)
				}
			}
		}
	})
	t.Run("differences", func(t *testing.T) {
		f := load(t, "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding << /BaseEncoding /WinAnsiEncoding /Differences [65 /B /fi 200 /eacute /x /NoSuchGlyph /.notdef /a100 1 /uni00E9 /u1F600 /Lslash /Euro /afii10017 /a.alt /f_f_i] >> >>")
		// 202 is a glyph with a name that says nothing, 203 is left as it
		// was by .notdef, 204 has the name of a dingbat in a font that is
		// not ZapfDingbats.
		for code, want := range map[byte]string{64: "@", 65: "B", 66: "fi", 67: "C", 200: "é", 201: "x", 202: "�", 203: "Ë", 204: "�", 205: "Í",
			1: "é", 2: "😀", 3: "Ł", 4: "€", 5: "А", 6: "a", 7: "ffi"} {
			if got, _ := show(f, string([]byte{code})); got != want {
				t.Errorf("code %d: %q, want %q", code, got, want)
			}
		}
	})
	t.Run("Type3 has only its differences", func(t *testing.T) {
		f := load(t, "<< /Type /Font /Subtype /Type3 /FontMatrix [0.01 0 0 0.01 0 0] /FirstChar 65 /LastChar 66 /Widths [50 70] /Encoding << /Differences [65 /A /g17] >> >>")
		got, sh := show(f, "ABC")
		// B is a glyph with a made-up name and C has no glyph at all:
		// both are read by their codes, and counted.
		if got != "ABC" || sh.Unmapped != 0 || sh.Assumed != 2 || !near(sh.Advance, 0.5+0.7) || !f.Type3 {
			t.Errorf("%q, %+v", got, sh)
		}
		// A code that is not ASCII and has no glyph is nothing.
		if got, sh := show(f, "\x01\xc0"); got != "" || sh.Unmapped != 0 || sh.Assumed != 0 {
			t.Errorf("%q, %+v", got, sh)
		}
	})
	t.Run("glyphs the font says nothing about", func(t *testing.T) {
		// Names a program made up. A code that is ASCII is read as
		// ASCII; one that is not is read as Latin-1 only when the name
		// is the number of the code; anything else is unmapped.
		f := load(t, "<< /Type /Font /Subtype /Type1 /BaseFont /ABCDEF+Custom /FontDescriptor << /FontFile 1 0 R >> /Encoding << /Differences [65 /g1 /g2 192 /a192 /a999 /g7 1 /g8] >> >>")
		got, sh := show(f, "AB\xc0\xc1\xc2\x01C")
		if got != "ABÀ���C" || sh.Unmapped != 3 || sh.Assumed != 3 {
			t.Errorf("%q, %+v", got, sh)
		}
		// ToUnicode settles it, and nothing is assumed any more.
		f = load(t, "<< /Type /Font /Subtype /Type1 /ToUnicode 1 0 R /Encoding << /Differences [65 /g1 /g2] >> >>",
			pdftest.Stream("", "2 beginbfchar <41> <0058> <42> <0059> endbfchar"))
		if got, sh := show(f, "AB"); got != "XY" || sh.Assumed != 0 {
			t.Errorf("%q, %+v", got, sh)
		}
	})
	t.Run("dingbat names in the dingbat font", func(t *testing.T) {
		f := load(t, "<< /Type /Font /Subtype /Type1 /BaseFont /ZapfDingbats /Encoding << /Differences [65 /a100 /a1] >> >>")
		if got, _ := show(f, "AB\x21"); got != "❞✁✁" {
			t.Errorf("%q", got)
		}
	})
	t.Run("ToUnicode wins, code by code", func(t *testing.T) {
		f := load(t, "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /ToUnicode 1 0 R /Encoding << /Differences [65 /B 67 /X] >> >>",
			pdftest.Stream("", "2 beginbfchar <41> <005A> <0044> <0051> endbfchar"))
		// 65 from ToUnicode, 66 from the base encoding, 67 from the
		// differences, 68 from a ToUnicode entry written with two bytes.
		if got, _ := show(f, "ABCD"); got != "ZBXQ" {
			t.Errorf("%q", got)
		}
	})
}

func TestSimpleFontWidths(t *testing.T) {
	t.Run("stated widths", func(t *testing.T) {
		f := load(t, "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /FirstChar 32 /LastChar 34 /Widths [278 300 355] /FontDescriptor << /MissingWidth 123 >> >>")
		_, sh := show(f, " !\"")
		if !near(sh.Advance, 0.278+0.300+0.355) || sh.Spaces != 1 || !near(f.SpaceWidth, 0.278) {
			t.Errorf("%+v, space %v", sh, f.SpaceWidth)
		}
		// Outside /FirstChar to /LastChar: /MissingWidth, not the
		// standard metrics.
		if _, sh := show(f, "W"); !near(sh.Advance, 0.123) {
			t.Errorf("missing width: %v", sh.Advance)
		}
		// A font whose widths do not cover the space has no space.
		f = load(t, "<< /Type /Font /Subtype /Type1 /BaseFont /CMR10 /FirstChar 40 /Widths [388 388] /FontDescriptor << /MissingWidth 500 >> >>")
		if !near(f.SpaceWidth, 0.25) {
			t.Errorf("space width %v from /MissingWidth", f.SpaceWidth)
		}
	})
	t.Run("standard fonts without widths", func(t *testing.T) {
		// Defect F3.
		for _, tc := range []struct {
			font, s string
			want    float64
		}{
			{"Helvetica", "iiii", 0.888}, {"Helvetica", "WWWW", 3.776}, {"Helvetica", " ", 0.278},
			{"Helvetica-Bold", "i", 0.278}, {"Times-Roman", "a", 0.444}, {"Courier", "iW", 1.2},
			{"Arial", "W", 0.944}, {"ABCDEF+TimesNewRomanPS-BoldMT", "a", 0.5},
			{"Helvetica", "\xe9", 0.556}, // é through WinAnsi
		} {
			f := load(t, "<< /Type /Font /Subtype /Type1 /BaseFont /"+tc.font+" /Encoding /WinAnsiEncoding >>")
			if _, sh := show(f, tc.s); !near(sh.Advance, tc.want) {
				t.Errorf("%s %q: %v, want %v", tc.font, tc.s, sh.Advance, tc.want)
			}
		}
		// A glyph placed by /Differences has its own width.
		f := load(t, "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding << /Differences [1 /W] >> >>")
		if _, sh := show(f, "\x01"); !near(sh.Advance, 0.944) {
			t.Errorf("differences: %v", sh.Advance)
		}
		if f := load(t, "<< /Type /Font /Subtype /Type1 /BaseFont /Symbol >>"); !near(float64(f.widths[0x61]), 0.631) {
			t.Errorf("Symbol alpha: %v", f.widths[0x61])
		}
	})
	t.Run("no widths and not a standard font", func(t *testing.T) {
		f := load(t, "<< /Type /Font /Subtype /Type1 /BaseFont /Garamond >>")
		// The width that stands in for a missing one is not taken for
		// the width of a space.
		if _, sh := show(f, "ab"); !near(sh.Advance, 1.0) || !near(f.SpaceWidth, 0.25) {
			t.Errorf("%v, space %v", sh.Advance, f.SpaceWidth)
		}
		f = load(t, "<< /Type /Font /Subtype /Type1 /BaseFont /Garamond /FontDescriptor << /Flags 1 >> >>")
		if _, sh := show(f, "ab"); !near(sh.Advance, 1.2) {
			t.Errorf("fixed pitch: %v", sh.Advance)
		}
	})
	t.Run("absurd widths are bounded", func(t *testing.T) {
		f := load(t, "<< /Type /Font /Subtype /Type1 /FirstChar 65 /Widths [1e30 -1e30 99999999999999999999999999999999999999999999] >>")
		_, sh := show(f, "ABC")
		if math.IsNaN(sh.Advance) || math.IsInf(sh.Advance, 0) || math.Abs(sh.Advance) > 3*maxAdvance {
			t.Errorf("advance %v", sh.Advance)
		}
	})
}

// type0 returns the objects of a composite font: the descendant font,
// the ToUnicode CMap (empty for none), and the font dictionary last.
func type0(encoding, widths, toUnicode string) (dict string, extra []string) {
	extra = append(extra, "<< /Type /Font /Subtype /CIDFontType2 /BaseFont /Test /CIDSystemInfo << /Registry (Adobe) /Ordering (Identity) /Supplement 0 >> "+widths+" >>")
	dict = "<< /Type /Font /Subtype /Type0 /BaseFont /Test /Encoding " + encoding + " /DescendantFonts [1 0 R]"
	if toUnicode != "" {
		extra = append(extra, pdftest.Stream("", toUnicode))
		dict += " /ToUnicode 2 0 R"
	}
	return dict + " >>", extra
}

func TestCompositeFont(t *testing.T) {
	abcd := "1 begincodespacerange <0000> <FFFF> endcodespacerange 4 beginbfchar <0001> <0041> <0002> <0042> <0003> <0043> <0020> <0020> endbfchar"
	t.Run("default width", func(t *testing.T) {
		// Defect F1.
		dict, extra := type0("/Identity-H", "/DW 800", abcd)
		f := load(t, dict, extra...)
		got, sh := show(f, "\x00\x01\x00\x02")
		if got != "AB" || sh.Glyphs != 2 || !near(sh.Advance, 1.6) || !f.Composite {
			t.Errorf("%q, %+v", got, sh)
		}
		dict, extra = type0("/Identity-H", "", abcd)
		if _, sh := show(load(t, dict, extra...), "\x00\x01"); !near(sh.Advance, 1) {
			t.Errorf("no /DW: %v", sh.Advance)
		}
	})
	t.Run("both forms of /W", func(t *testing.T) {
		dict, extra := type0("/Identity-H", "/DW 1000 /W [1 [200 300] 3 5 400 32 32 250 10 [600]]", abcd)
		f := load(t, dict, extra...)
		for cid, want := range map[int]float64{0: 1, 1: 0.2, 2: 0.3, 3: 0.4, 5: 0.4, 6: 1, 10: 0.6, 11: 1, 32: 0.25} {
			if _, sh := show(f, string([]byte{byte(cid >> 8), byte(cid)})); !near(sh.Advance, want) {
				t.Errorf("CID %d: %v, want %v", cid, sh.Advance, want)
			}
		}
		// The space is the code whose text is a space.
		if !near(f.SpaceWidth, 0.25) {
			t.Errorf("space width %v", f.SpaceWidth)
		}
		// Word spacing applies to a one-byte 32 only.
		if _, sh := show(f, "\x00\x20"); sh.Spaces != 0 {
			t.Error("a two-byte code counts as a space for word spacing")
		}
	})
	t.Run("codes stay aligned", func(t *testing.T) {
		// Defect F2.
		dict, extra := type0("/Identity-H", "/DW 500", "1 begincodespacerange <0000> <FFFF> endcodespacerange 2 beginbfchar <0041> <0041> <0042> <0042> endbfchar")
		got, sh := show(load(t, dict, extra...), "\x00\x41\xFF\x42\x00\x42")
		if got != "A�B" || sh.Unmapped != 1 || sh.Glyphs != 3 {
			t.Errorf("%q, %+v", got, sh)
		}
	})
	t.Run("no ToUnicode", func(t *testing.T) {
		// Defect F5: nothing but valid UTF-8, and every code counted.
		dict, extra := type0("/Identity-H", "/DW 500", "")
		got, sh := show(load(t, dict, extra...), "\xE9\xFF\x00\x41\xC3")
		if got != "���" || sh.Unmapped != 3 || !near(sh.Advance, 1.5) {
			t.Errorf("%q, %+v", got, sh)
		}
	})
	t.Run("codes that are Unicode", func(t *testing.T) {
		dict, extra := type0("/UniJIS-UCS2-H", "/DW 1000", "")
		if got, sh := show(load(t, dict, extra...), "\x00A\x65\xE5\x67\x2C\xD8\x00"); got != "A日本�" || sh.Unmapped != 1 {
			t.Errorf("UCS-2: %q, %+v", got, sh)
		}
		dict, extra = type0("/UniJIS-UTF16-H", "/DW 1000", "")
		if got, sh := show(load(t, dict, extra...), "\x00A\xD8\x3D\xDE\x00\x65\xE5"); got != "A😀日" || sh.Glyphs != 3 {
			t.Errorf("UTF-16: %q, %+v", got, sh)
		}
		dict, extra = type0("/UniGB-UTF32-H", "/DW 1000", "")
		if got, _ := show(load(t, dict, extra...), "\x00\x01\xF6\x00\x00\x00\x00A"); got != "😀A" {
			t.Errorf("UTF-32: %q", got)
		}
	})
	t.Run("a legacy encoding takes its text from ToUnicode", func(t *testing.T) {
		tu := "2 begincodespacerange <00> <80> <8140> <FCFC> endcodespacerange 2 beginbfchar <41> <0041> <93FA> <65E5> endbfchar"
		dict, extra := type0("/90ms-RKSJ-H", "/DW 1000", tu)
		got, sh := show(load(t, dict, extra...), "A\x93\xFA")
		if got != "A日" || sh.Glyphs != 2 || !near(sh.Advance, 2) {
			t.Errorf("%q, %+v", got, sh)
		}
		// Without one, the one-byte codes are still ASCII; the two-byte
		// codes need the tables of the encoding.
		for _, enc := range []string{"/90ms-RKSJ-H", "/GBK-EUC-H", "/ETen-B5-H", "/KSCms-UHC-V", "/EUC-H"} {
			dict, extra = type0(enc, "/DW 1000", "")
			if got, sh := show(load(t, dict, extra...), "OK \x93\xFA\x7f"); got != "OK ��" || sh.Unmapped != 2 {
				t.Errorf("%s: %q, %+v", enc, got, sh)
			}
		}
	})
	t.Run("an embedded CMap", func(t *testing.T) {
		cm := "begincodespacerange <00> <7F> <8000> <FFFF> endcodespacerange begincidrange <00> <7F> 100 <8000> <80FF> 500 endcidrange"
		extra := []string{
			"<< /Type /Font /Subtype /CIDFontType0 /BaseFont /Test /DW 1000 /W [100 227 500 500 [250]] >>",
			pdftest.Stream("/Type /CMap /CMapName /Custom", cm),
			pdftest.Stream("", "begincodespacerange <00> <7F> <8000> <FFFF> endcodespacerange beginbfrange <00> <7F> <0000> <8000> <80FF> <4E00> endbfrange"),
		}
		f := load(t, "<< /Type /Font /Subtype /Type0 /BaseFont /Test /Encoding 2 0 R /ToUnicode 3 0 R /DescendantFonts [1 0 R] >>", extra...)
		got, sh := show(f, "AB\x80\x00\x80\x01")
		if got != "AB一丁" || sh.Glyphs != 4 || !near(sh.Advance, 0.5+0.5+0.25+1) {
			t.Errorf("%q, %+v", got, sh)
		}
	})
	t.Run("vertical", func(t *testing.T) {
		dict, extra := type0("/Identity-V", "/DW 500", abcd)
		f := load(t, dict, extra...)
		if _, sh := show(f, "\x00\x01\x00\x02"); !f.Vertical || !near(sh.Advance, -2) {
			t.Errorf("vertical %v, advance %v", f.Vertical, sh.Advance)
		}
		dict, extra = type0("/Identity-V", "/DW2 [880 -500]", abcd)
		if _, sh := show(load(t, dict, extra...), "\x00\x01\x00\x02"); !near(sh.Advance, -1) {
			t.Errorf("/DW2: %v", sh.Advance)
		}
	})
	t.Run("an unknown encoding uses the ToUnicode codespace", func(t *testing.T) {
		dict, extra := type0("/MyOwnCMap", "/DW 500", "begincodespacerange <00> <FF> endcodespacerange beginbfchar <41> <0058> endbfchar")
		if got, sh := show(load(t, dict, extra...), "AA"); got != "XX" || sh.Glyphs != 2 {
			t.Errorf("%q, %+v", got, sh)
		}
	})
}

func TestLoad(t *testing.T) {
	t.Run("wrong types", func(t *testing.T) {
		// Defect C7: none of these may panic, and whatever loads must
		// produce valid text.
		for _, dict := range []string{
			"12", "(font)", "[1 2]",
			"<< /FirstChar /A /Widths 5 >>",
			"<< /FirstChar 0 /Widths [/a (b) [1]] >>",
			"<< /FirstChar -1000000 /Widths [1 2 3] >>",
			"<< /FirstChar 1e15 /Widths [1 2 3] >>",
			"<< /ToUnicode 12 /Encoding 5 >>",
			"<< /Encoding << /Differences << /A 1 >> >> >>",
			"<< /Encoding << /Differences [-5 /a 1e12 /b (s) [1] /c 255 /d /e /f] >> >>",
			"<< /BaseFont 12 /FontDescriptor 7 >>",
			"<< /Subtype /Type3 /FontMatrix [0 0] /Widths [1] /FirstChar 0 >>",
			"<< /Subtype /Type3 /FontMatrix [(a) 0 0 1 0 0] >>",
			"<< /Subtype /Type0 >>",
			"<< /Subtype /Type0 /Encoding 12 /DescendantFonts 5 >>",
			"<< /Subtype /Type0 /Encoding /Identity-H /DescendantFonts [<< /W 5 /DW /x >>] >>",
			"<< /Subtype /Type0 /Encoding /Identity-H /DescendantFonts [<< /W [1 2] /DW2 [1] >>] >>",
			"<< /Subtype /Type0 /Encoding /Identity-H /DescendantFonts [<< /W [1 [(a) /b] -5 3 2 9 1 2 1e12 [5] 7] >>] >>",
			"<< /Subtype /Type0 /Encoding /Identity-V /DescendantFonts [<< /DW2 [(a) (b)] >>] >>",
		} {
			if f := load(t, dict); f != nil {
				if out, _ := show(f, allBytes()+allBytes()); !utf8.ValidString(out) {
					t.Errorf("%s: invalid text", dict)
				}
			}
		}
		if f := load(t, "12"); f != nil {
			t.Errorf("a number loaded as a font: %+v", f)
		}
	})
	t.Run("parsed once per file", func(t *testing.T) {
		b := pdftest.New()
		font := b.Add(pdftest.FontHelvetica)
		data := pdftest.Doc(pdftest.DocOptions{Base: b}, pdftest.PageSpec{}).Bytes()
		f, _ := file.Open(data, file.Limits{})
		ref := syntax.Value{Kind: syntax.Ref, N: int64(font)}
		if Load(f, ref) != Load(f, ref) {
			t.Error("two loads of one font object gave two fonts")
		}
	})
	t.Run("the default font", func(t *testing.T) {
		if got, sh := show(Default, "A b\xff"); got != "A b�" || !near(sh.Advance, 2) || sh.Spaces != 1 {
			t.Errorf("%q, %+v", got, sh)
		}
	})
}

func TestAppendDoesNotAllocate(t *testing.T) {
	simple := load(t, pdftest.FontHelvetica)
	dict, extra := type0("/Identity-H", "/DW 1000 /W [1 [200 300]]", "begincodespacerange <0000> <FFFF> endcodespacerange beginbfrange <0000> <FFFF> <0000> endbfrange")
	composite := load(t, dict, extra...)
	buf := make([]byte, 0, 1024)
	s1, s2 := []byte("The quick brown fox"), []byte("\x00T\x00h\x00e\x00 \x4e\x00")
	if n := testing.AllocsPerRun(100, func() {
		simple.Append(buf[:0], s1)
		composite.Append(buf[:0], s2)
	}); n != 0 {
		t.Errorf("%v allocations per string", n)
	}
}

func FuzzCMap(f *testing.F) {
	f.Add([]byte("1 begincodespacerange <0000> <FFFF> endcodespacerange\n2 beginbfchar <0001> <0041> <0002> <00480069> endbfchar"), []byte("\x00\x01\x00\x02"))
	f.Add([]byte("1 beginbfrange <0010> <0020> <0041> endbfrange\n1 beginbfrange <0030> <0031> [<0061> <0062>] endbfrange"), []byte("\x00\x10"))
	f.Add([]byte("/CMapName /X def { pop } bind def 1 beginbfchar <01> <D800> endbfchar"), []byte("\x01"))
	f.Add([]byte("begincodespacerange <00> <80> <8140> <FCFC> endcodespacerange begincidrange <00> <7F> 100 endcidrange /WMode 1 def /X usecmap"), []byte("A\x81\x40"))
	f.Add([]byte("1 beginbfrange <00000000> <FFFFFFFF> <D7FE> endbfrange"), []byte("\xff\xff\xff\xff"))
	f.Fuzz(func(t *testing.T, data, shown []byte) {
		start := time.Now()
		c := parseCMap(data)
		if d := time.Since(start); d > 5*time.Second {
			t.Fatalf("%d bytes took %v", len(data), d)
		}
		// The entries are sorted and their links point backwards.
		for i := range c.entries {
			e := &c.entries[i]
			if e.hi < e.lo || e.n < 1 || e.n > 4 || int(e.outer) >= i {
				t.Fatalf("entry %d: %+v", i, *e)
			}
			if e.kind != kindCID && int(e.val)+int(e.tlen) > len(c.text) {
				t.Fatalf("entry %d: text out of range", i)
			}
		}
		// Used as both tables of a composite font, it yields valid text
		// and consumes the whole string.
		font := &Font{Composite: true, toUni: c, cids: c, space: c.space, dw: 1}
		out, sh := font.Append(nil, shown)
		if !utf8.Valid(out) {
			t.Fatalf("invalid UTF-8: %q", out)
		}
		if sh.Glyphs > len(shown) || len(shown) > 0 && sh.Glyphs == 0 {
			t.Fatalf("%d glyphs for %d bytes", sh.Glyphs, len(shown))
		}
		if math.IsNaN(sh.Advance) || math.IsInf(sh.Advance, 0) {
			t.Fatalf("advance %v", sh.Advance)
		}
	})
}

// FuzzFont loads a font dictionary made from the input and shows a
// string in it.
func FuzzFont(f *testing.F) {
	f.Add("/Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding", "", []byte("Hello"))
	f.Add("/Subtype /Type1 /Encoding << /BaseEncoding /MacRomanEncoding /Differences [65 /B /uni00E9 /f_f_i] >> /FirstChar 65 /Widths [500 600]", "", []byte("AB"))
	f.Add("/Subtype /Type0 /Encoding /Identity-H /DescendantFonts [<< /DW 500 /W [1 [200 300] 5 9 400] >>] /ToUnicode 1 0 R", "beginbfrange <0000> <FFFF> <0000> endbfrange", []byte("\x00A\x00B"))
	f.Add("/Subtype /Type0 /Encoding 1 0 R /DescendantFonts [<< /DW2 [880 -900] >>]", "begincodespacerange <00> <80> <8140> <FCFC> endcodespacerange /WMode 1 def", []byte("A\x81\x40"))
	f.Add("/Subtype /Type3 /FontMatrix [0.01 0 0 0.01 0 0] /FirstChar 0 /Widths [1e30] /Encoding << /Differences [0 /a] >>", "", []byte("\x00"))
	f.Add("/Subtype /Type0 /Encoding /UniJIS-UTF16-H /DescendantFonts [1 0 R]", "", []byte("\xd8\x3d\xde\x00"))
	f.Fuzz(func(t *testing.T, dict, stream string, shown []byte) {
		if strings.Contains(dict, "endobj") || strings.Contains(stream, "endstream") {
			t.Skip()
		}
		b := pdftest.New()
		b.Add(pdftest.Stream("", stream))
		font := b.Add("<< /Type /Font " + dict + " >>")
		data := pdftest.Doc(pdftest.DocOptions{Base: b}, pdftest.PageSpec{}).Bytes()
		file, err := file.Open(data, file.Limits{})
		if err != nil {
			t.Skip()
		}
		fnt := Load(file, syntax.Value{Kind: syntax.Ref, N: int64(font)})
		if fnt == nil {
			return
		}
		out, sh := fnt.Append(nil, shown)
		if !utf8.Valid(out) {
			t.Fatalf("invalid UTF-8: %q", out)
		}
		if sh.Glyphs > len(shown) || len(shown) > 0 && sh.Glyphs == 0 {
			t.Fatalf("%d glyphs for %d bytes", sh.Glyphs, len(shown))
		}
		if math.IsNaN(sh.Advance) || math.IsInf(sh.Advance, 0) || math.Abs(sh.Advance) > float64(len(shown))*maxAdvance+1 {
			t.Fatalf("advance %v for %d bytes", sh.Advance, len(shown))
		}
		if math.IsNaN(fnt.SpaceWidth) || fnt.SpaceWidth <= 0 || fnt.SpaceWidth > maxAdvance {
			t.Fatalf("space width %v", fnt.SpaceWidth)
		}
	})
}
