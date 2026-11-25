package gofastpdf_test

import (
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

// The fixtures of testdata/gen-contents.sh: a table of contents with dot
// leaders, set by LibreOffice. Each entry is a block of its own, the one
// of two lines too.
func TestTablesOfContents(t *testing.T) {
	leader := regexp.MustCompile(`\.{4,}`)
	for _, lang := range []string{"en", "fr", "ar"} {
		t.Run(lang, func(t *testing.T) {
			src, err := os.ReadFile("testdata/src/contents-" + lang + ".txt")
			if err != nil {
				t.Fatal(err)
			}
			var want []string
			for _, s := range strings.Split(strings.TrimSpace(strings.ReplaceAll(string(src), "\r\n", "\n")), "\n") {
				if s != "" {
					want = append(want, s)
				}
			}
			data, err := os.ReadFile("testdata/fixtures/contents-" + lang + "-libreoffice.pdf")
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, s := range texts(&mustExtract(t, data, nil).Parts[0]) {
				got = append(got, leader.ReplaceAllString(s, "\t"))
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("blocks:\n  %s\nwant:\n  %s", strings.Join(got, "\n  "), strings.Join(want, "\n  "))
			}
		})
	}
}
