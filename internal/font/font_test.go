package font

import (
	"fmt"
	"testing"
	"time"

	"github.com/AOShei/go-fast-pdf/internal/file"
	"github.com/AOShei/go-fast-pdf/internal/pdftest"
	"github.com/AOShei/go-fast-pdf/internal/syntax"
)

func cmapFont(src string) *Font {
	f := &Font{}
	f.parseCMap([]byte(src))
	return f
}

func TestCMap(t *testing.T) {
	f := cmapFont(`/CIDInit /ProcSet findresource begin 12 dict begin begincmap
1 begincodespacerange <00> <FF> endcodespacerange
3 beginbfchar
<01> <0041>
<02> <00480069>
<0003> <00E9>
endbfchar
2 beginbfrange
<10> <12> <0061>
<20> <22> [<0058> <0059> <005A>]
endbfrange
endcmap`)
	one := map[byte]string{0x01: "A", 0x02: "Hi", 0x10: "a", 0x11: "b", 0x12: "c", 0x20: "X", 0x21: "Y", 0x22: "Z"}
	for code, want := range one {
		if got, ok := f.One(code); !ok || got != want {
			t.Errorf("code %#x: %q, %v; want %q", code, got, ok, want)
		}
	}
	if got, ok := f.Two(0x00, 0x03); !ok || got != "é" {
		t.Errorf("two-byte code: %q, %v", got, ok)
	}
	if _, ok := f.One(0x13); ok {
		t.Error("a code after the end of a range is mapped")
	}
	if !f.HasCMap {
		t.Error("HasCMap is false")
	}
}

func TestCMapDamage(t *testing.T) {
	t.Run("PostScript procedure", func(t *testing.T) {
		// Defect C4: a brace used to hang the parser.
		f := cmapFont("/CMapName /X def { pop } bind def\n1 beginbfchar <01> <0041> endbfchar")
		if got, _ := f.One(1); got != "A" {
			t.Errorf("mapping after a procedure: %q", got)
		}
	})
	t.Run("ends inside a section", func(t *testing.T) {
		f := cmapFont("2 beginbfchar <01> <0041> <02>")
		if got, _ := f.One(1); got != "A" {
			t.Errorf("the entry before the end is lost: %q", got)
		}
	})
	t.Run("surrogates and odd lengths", func(t *testing.T) {
		f := cmapFont("3 beginbfchar <01> <D83DDE00> <02> <D800> <03> <41> endbfchar")
		if got, _ := f.One(1); got != "😀" {
			t.Errorf("surrogate pair: %q", got)
		}
		if got, _ := f.One(2); got != "�" {
			t.Errorf("lone surrogate: %q", got)
		}
	})
	t.Run("huge ranges are bounded", func(t *testing.T) {
		// Defect C6: four billion codes in one range.
		start := time.Now()
		f := cmapFont("1 beginbfrange <00000000> <FFFFFFFF> <0041> endbfrange\n" +
			"1 beginbfrange <0000> <FFFF> <0000> endbfrange\n1 beginbfrange <00> <FF> [<0041>] endbfrange")
		if d := time.Since(start); d > 2*time.Second {
			t.Errorf("took %v", d)
		}
		if got, ok := f.Two(0x00, 0x41); !ok || got != "A" {
			t.Errorf("identity range: %q, %v", got, ok)
		}
		if len(f.two) != 65536 {
			t.Errorf("%d two-byte entries, want 65536", len(f.two))
		}
	})
	t.Run("inverted and oversized ranges", func(t *testing.T) {
		f := cmapFont("3 beginbfrange <20> <10> <0041> <F0> <FFFF> <0041> <0102030405060708> <0102030405060709> <0041> endbfrange")
		if _, ok := f.One(0x20); ok {
			t.Error("an inverted range produced entries")
		}
	})
}

func load(t *testing.T, dict string, extra ...string) *Font {
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

func TestLoad(t *testing.T) {
	t.Run("widths", func(t *testing.T) {
		f := load(t, "<< /Type /Font /Subtype /Type1 /FirstChar 32 /LastChar 34 /Widths [278 300 355] >>")
		if w, ok := f.Width(33); !ok || w != 300 || f.SpaceWidth != 278 || !f.HasWidths {
			t.Errorf("width of 33 is %v (%v), space %v", w, ok, f.SpaceWidth)
		}
		if _, ok := f.Width(35); ok {
			t.Error("a code after /LastChar has a width")
		}
	})
	t.Run("no widths", func(t *testing.T) {
		f := load(t, "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>")
		if f.HasWidths || f.SpaceWidth != 250 || f.BaseFont != "Helvetica" {
			t.Errorf("%+v", f.SpaceWidth)
		}
	})
	t.Run("differences", func(t *testing.T) {
		f := load(t, "<< /Type /Font /Subtype /Type1 /Encoding << /Differences [65 /B /fi 200 /eacute /x /NoSuchGlyph] >> >>")
		for code, want := range map[byte]string{65: "B", 66: "fi", 201: "x", 202: string([]byte{202})} {
			if got, ok := f.Encoded(code); !ok || got != want {
				t.Errorf("code %d: %q, %v; want %q", code, got, ok, want)
			}
		}
		if _, ok := f.Encoded(67); ok || !f.HasEncoding {
			t.Error("a code without an entry is mapped, or HasEncoding is false")
		}
	})
	t.Run("ToUnicode wins over differences", func(t *testing.T) {
		f := load(t, "<< /Type /Font /Subtype /Type1 /ToUnicode 1 0 R /Encoding << /Differences [65 /B] >> >>",
			pdftest.Stream("", "1 beginbfchar <41> <005A> endbfchar"))
		if got, _ := f.One(65); got != "Z" || f.HasEncoding {
			t.Errorf("code 65 is %q, HasEncoding %v", got, f.HasEncoding)
		}
	})
	t.Run("wrong types", func(t *testing.T) {
		// Defect C7: none of these may panic.
		for _, dict := range []string{
			"12", "(font)", "[1 2]",
			"<< /FirstChar /A /Widths 5 >>",
			"<< /FirstChar 0 /Widths [/a (b) [1]] >>",
			"<< /FirstChar -1000000 /Widths [1 2 3] >>",
			"<< /FirstChar 1e15 /Widths [1 2 3] >>",
			"<< /ToUnicode 12 /Encoding 5 >>",
			"<< /Encoding << /Differences << /A 1 >> >> >>",
			"<< /Encoding << /Differences [-5 /a 1e12 /b (s) [1] /c] >> >>",
			"<< /BaseFont 12 >>",
		} {
			load(t, dict)
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
}

func FuzzCMap(f *testing.F) {
	f.Add([]byte("1 begincodespacerange <0000> <FFFF> endcodespacerange\n2 beginbfchar <0001> <0041> <0002> <00480069> endbfchar"))
	f.Add([]byte("1 beginbfrange <0010> <0020> <0041> endbfrange\n1 beginbfrange <0030> <0031> [<0061> <0062>] endbfrange"))
	f.Add([]byte("/CMapName /X def { pop } bind def 1 beginbfchar <01> <D800> endbfchar"))
	f.Fuzz(func(t *testing.T, data []byte) {
		start := time.Now()
		font := &Font{}
		font.parseCMap(data)
		if d := time.Since(start); d > 5*time.Second {
			t.Fatalf("%d bytes took %v", len(data), d)
		}
		_ = fmt.Sprint(font.HasCMap)
	})
}
