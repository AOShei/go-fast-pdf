package gofastpdf_test

import (
	"os"
	"strings"
	"testing"
)

// The fixtures of testdata/gen-rtl.sh: Hebrew, Arabic, and English with
// Hebrew and Arabic in it, set by LibreOffice (which draws a line's words
// in the order of reading, each from left to right, and its spaces and
// punctuation apart) and by Firefox (which draws glyph by glyph from
// right to left). Each reads as its source is written: the same
// paragraphs, the same characters, in the order of reading.
func TestRightToLeftFixtures(t *testing.T) {
	for _, c := range []struct{ fixture, source string }{
		{"rtl-he-libreoffice", "rtl-he"},
		{"rtl-he-firefox", "rtl-he"},
		{"rtl-ar-libreoffice", "rtl-ar"},
		{"rtl-mixed-libreoffice", "rtl-mixed"},
	} {
		t.Run(c.fixture, func(t *testing.T) {
			src, err := os.ReadFile("testdata/src/" + c.source + ".txt")
			if err != nil {
				t.Fatal(err)
			}
			want := strings.Split(strings.TrimSpace(strings.ReplaceAll(string(src), "\r\n", "\n")), "\n\n")
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
				if len(doc.Parts[i].Warnings) != 0 {
					t.Errorf("page %d: warnings %v", i+1, doc.Parts[i].Warnings)
				}
			}
		})
	}
}
