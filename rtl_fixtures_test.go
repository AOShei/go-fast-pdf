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
