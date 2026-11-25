package gofastpdf_test

import (
	"os"
	"strings"
	"testing"

	gofastcore "github.com/AOShei/go-fast-core"
	gofastpdf "github.com/AOShei/go-fast-pdf"
)

// The fixtures of testdata/gen-rtl.sh: Hebrew, Arabic, and English with
// Hebrew and Arabic in it, set by LibreOffice (which draws a line's words
// in the order of reading, each from left to right, and its spaces and
// punctuation apart) and by Firefox (which draws glyph by glyph from
// right to left, marks apart from their letters, and Arabic letters in
// pieces that its subsets of the Noto fonts give no character). Each
// reads as its source is written: the same paragraphs, the same
// characters, in the order of reading.
func TestRightToLeftFixtures(t *testing.T) {
	// Firefox gives the final alef maksura (U+0649) of the word for "on"
	// the text of the Persian yeh (U+FBFD, its final form), whose glyph
	// is the same: that is what the file says.
	firefoxYeh := strings.NewReplacer("\u0639\u0644\u0649 ", "\u0639\u0644\u06CC ")
	// Firefox's Arabic is in part read by the outlines of its glyphs,
	// which the page's warning says (byOutline).
	for _, c := range []struct {
		fixture, source string
		declared        *strings.Replacer
		byOutline       bool
	}{
		{"rtl-he-libreoffice", "rtl-he", nil, false},
		{"rtl-he-firefox", "rtl-he", nil, false},
		{"rtl-ar-libreoffice", "rtl-ar", nil, false},
		{"rtl-ar-firefox", "rtl-ar", firefoxYeh, true},
		{"rtl-ar-naskh-firefox", "rtl-ar", firefoxYeh, true},
		{"rtl-mixed-libreoffice", "rtl-mixed", nil, false},
		// Set narrow (testdata/gen-rtl-narrow.sh): lines that by
		// themselves read in the other direction than their paragraph,
		// lines mostly in the font of the Latin words, and brackets
		// whose partner is on another line.
		{"rtl-he-narrow-firefox", "rtl-he", nil, false},
		{"rtl-he-narrow-libreoffice", "rtl-he", nil, false},
		{"rtl-ar-narrow-libreoffice", "rtl-ar", nil, false},
		{"rtl-ar-narrow-firefox", "rtl-ar", firefoxYeh, true},
		{"rtl-mixed-narrow-libreoffice", "rtl-mixed", nil, false},
	} {
		t.Run(c.fixture, func(t *testing.T) {
			src, err := os.ReadFile("testdata/src/" + c.source + ".txt")
			if err != nil {
				t.Fatal(err)
			}
			text := strings.TrimSpace(strings.ReplaceAll(string(src), "\r\n", "\n"))
			if c.declared != nil {
				text = c.declared.Replace(text)
			}
			want := strings.Split(text, "\n\n")
			data, err := os.ReadFile("testdata/fixtures/" + c.fixture + ".pdf")
			if err != nil {
				t.Fatal(err)
			}
			doc := mustExtract(t, data, nil)
			var got []string
			for i := range doc.Parts {
				got = append(got, texts(&doc.Parts[i])...)
			}
			if strings.Join(got, "\n\n") != strings.Join(want, "\n\n") {
				t.Errorf("got:\n%s\nwant:\n%s", strings.Join(got, "\n\n"), strings.Join(want, "\n\n"))
			}
			// With spans, the words of a line are found in the text it
			// was written again with (mustExtract holds the result to
			// the contract).
			spans := mustExtract(t, data, &gofastcore.Options{Spans: true})
			var with []string
			for i := range spans.Parts {
				with = append(with, texts(&spans.Parts[i])...)
			}
			if strings.Join(got, "\n\n") != strings.Join(with, "\n\n") {
				t.Errorf("the text with spans differs:\n%s\nwithout:\n%s", strings.Join(with, "\n\n"), strings.Join(got, "\n\n"))
			}
			for i := range doc.Parts {
				ws := doc.Parts[i].Warnings
				if c.byOutline && (len(ws) != 1 || ws[0].Code != gofastpdf.WarnGlyphTextByOutline || ws[0].Severity != gofastcore.SeverityInfo) ||
					!c.byOutline && len(ws) != 0 {
					t.Errorf("page %d: warnings %v", i+1, ws)
				}
			}
		})
	}
}

// The fixtures of testdata/gen-rtl-columns.sh: the Hebrew and the Arabic
// of TestRightToLeftFixtures under a heading across the page, in two
// columns, the first on the right, set by LibreOffice and by Firefox; and
// the English with Hebrew and Arabic in it, the first column on the left.
// The words are read in the order of the source: the right column
// before the left, and in the English the left before the right. A paragraph that goes on into the next column is two
// blocks there, so the text is compared word by word.
func TestRightToLeftColumns(t *testing.T) {
	firefoxYeh := strings.NewReplacer("على ", "علی ")
	// Firefox breaks the Arabic between "بنسبة" and "75%", two lines of
	// the left column: the Arabic letter before the number, on the line
	// before, makes it an Arabic number, and the percent sign after it
	// stands left of it.
	for _, c := range []struct {
		fixture, source string
		declared        []*strings.Replacer
		byOutline       bool
	}{
		{"rtl-he-columns-libreoffice", "rtl-he", nil, false},
		{"rtl-he-columns-firefox", "rtl-he", nil, false},
		{"rtl-ar-columns-libreoffice", "rtl-ar", nil, false},
		{"rtl-ar-columns-firefox", "rtl-ar", []*strings.Replacer{firefoxYeh}, true},
		{"rtl-mixed-columns-libreoffice", "rtl-mixed", nil, false},
	} {
		t.Run(c.fixture, func(t *testing.T) {
			src, err := os.ReadFile("testdata/src/" + c.source + ".txt")
			if err != nil {
				t.Fatal(err)
			}
			want := strings.Join(strings.Fields(string(src)), " ")
			for _, r := range c.declared {
				want = r.Replace(want)
			}
			data, err := os.ReadFile("testdata/fixtures/" + c.fixture + ".pdf")
			if err != nil {
				t.Fatal(err)
			}
			doc := mustExtract(t, data, nil)
			var got []string
			for i := range doc.Parts {
				got = append(got, texts(&doc.Parts[i])...)
			}
			if g := strings.Join(strings.Fields(strings.Join(got, " ")), " "); g != want {
				t.Errorf("got:\n%s\nwant:\n%s", strings.Join(got, "\n\n"), want)
			}
			for i := range doc.Parts {
				ws := doc.Parts[i].Warnings
				if c.byOutline && (len(ws) != 1 || ws[0].Code != gofastpdf.WarnGlyphTextByOutline || ws[0].Severity != gofastcore.SeverityInfo) ||
					!c.byOutline && len(ws) != 0 {
					t.Errorf("page %d: warnings %v", i+1, ws)
				}
			}
		})
	}
}

// The last paragraph of testdata/src/rtl-ar.txt, which Firefox breaks
// between "بنسبة" and "75%" (testdata/gen-rtl-columns.sh): the Arabic
// letter at the end of the line before makes the number at the start of
// the next an Arabic number, and the percent sign after it stands at its
// left. It reads as the source, "75%", and not "%75".
func TestRightToLeftNumberAtLineStart(t *testing.T) {
	src, err := os.ReadFile("testdata/src/rtl-ar.txt")
	if err != nil {
		t.Fatal(err)
	}
	paras := strings.Split(strings.TrimSpace(strings.ReplaceAll(string(src), "\r\n", "\n")), "\n\n")
	want := paras[len(paras)-1]
	data, err := os.ReadFile("testdata/fixtures/rtl-ar-line-number-firefox.pdf")
	if err != nil {
		t.Fatal(err)
	}
	doc := mustExtract(t, data, nil)
	if len(doc.Parts) != 1 {
		t.Fatalf("%d parts", len(doc.Parts))
	}
	if got := texts(&doc.Parts[0]); len(got) != 1 || got[0] != want {
		t.Errorf("got:\n%s\nwant:\n%s", strings.Join(got, "\n\n"), want)
	}
	// With spans, the words of the line are found in the text it was
	// written again with (mustExtract holds the result to the contract).
	for _, name := range []string{"rtl-ar-line-number-firefox", "rtl-ar-columns-firefox"} {
		data, err := os.ReadFile("testdata/fixtures/" + name + ".pdf")
		if err != nil {
			t.Fatal(err)
		}
		plain := mustExtract(t, data, nil)
		spans := mustExtract(t, data, &gofastcore.Options{Spans: true})
		if a, b := texts(&plain.Parts[0]), texts(&spans.Parts[0]); strings.Join(a, "\n") != strings.Join(b, "\n") {
			t.Errorf("%s: the text with spans differs:\n%s\nwithout:\n%s", name, strings.Join(b, "\n"), strings.Join(a, "\n"))
		}
	}
}

// The Arabic set by LibreOffice in Noto Sans Arabic (testdata/gen-rtl.sh),
// which gives each dotted letter as ActualText and draws its dots before
// its body. Every word is whole: a letter reaches over all its glyphs,
// and does not begin at its dots. (The lines are further apart than the
// paragraph step joins, so the text is compared word by word.)
func TestRightToLeftActualText(t *testing.T) {
	src, err := os.ReadFile("testdata/src/rtl-ar.txt")
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Join(strings.Fields(string(src)), " ")
	data, err := os.ReadFile("testdata/fixtures/rtl-ar-noto-libreoffice.pdf")
	if err != nil {
		t.Fatal(err)
	}
	doc := mustExtract(t, data, nil)
	var got []string
	for i := range doc.Parts {
		got = append(got, texts(&doc.Parts[i])...)
		if ws := doc.Parts[i].Warnings; len(ws) != 0 {
			t.Errorf("page %d: warnings %v", i+1, ws)
		}
	}
	if g := strings.Join(strings.Fields(strings.Join(got, " ")), " "); g != want {
		t.Errorf("got:\n%s\nwant:\n%s", g, want)
	}
}

// Two paragraphs of Hebrew with one between them that is of neither
// (testdata/gen-rtl-narrow.sh). A heading that has another font than the
// paragraph above, of Latin words for the most part or of Hebrew in bold,
// stays apart from it, and reads as it is set, though its lines by
// themselves read in two directions; and a sentence of English set from
// left to right keeps its full stop at its end, though it is taken for
// the end of the paragraph above, with which it shares its right edge.
func TestRightToLeftBetween(t *testing.T) {
	src, err := os.ReadFile("testdata/src/rtl-he.txt")
	if err != nil {
		t.Fatal(err)
	}
	paras := strings.Split(strings.TrimSpace(strings.ReplaceAll(string(src), "\r\n", "\n")), "\n\n")
	for _, c := range []struct {
		fixture string
		want    []string
	}{
		{"rtl-he-between-heading-libreoffice", []string{paras[1], "PDF Tolstoy Austen Library Books הספרייה", paras[2]}},
		{"rtl-he-between-bold-firefox", []string{paras[1], "הספרייה העירונית Tolstoy הספרייה", paras[2]}},
		{"rtl-he-between-latin-firefox", []string{paras[1] + " Contact the library for details.", paras[2]}},
	} {
		data, err := os.ReadFile("testdata/fixtures/" + c.fixture + ".pdf")
		if err != nil {
			t.Fatal(err)
		}
		doc := mustExtract(t, data, nil)
		if len(doc.Parts) != 1 {
			t.Fatalf("%s: %d parts", c.fixture, len(doc.Parts))
		}
		if got := texts(&doc.Parts[0]); strings.Join(got, "\n\n") != strings.Join(c.want, "\n\n") {
			t.Errorf("%s: got:\n%s\nwant:\n%s", c.fixture, strings.Join(got, "\n\n"), strings.Join(c.want, "\n\n"))
		}
	}
}
