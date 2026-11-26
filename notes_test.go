package gofastpdf_test

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	gofastcore "github.com/AOShei/go-fast-core"

	gofastpdf "github.com/AOShei/go-fast-pdf"
)

// Footnotes (notes.go).

// sized is text set at a size and a place on the page.
func sized(x, y, size int, text string) string {
	return fmt.Sprintf("BT /F1 %d Tf %d %d Td (%s) Tj ET\n", size, x, y, text)
}

const (
	noteText1 = "The first paragraph of the page, long enough to be its text."
	noteText2 = "A second paragraph of the page, which stands far below the first."
)

// notesOf lists the footnotes of a document as page, marker and text.
func notesOf(doc *gofastcore.Document) (out []string) {
	for _, p := range doc.Parts {
		for _, b := range p.Blocks {
			if b.Kind == gofastcore.KindFootnote {
				out = append(out, fmt.Sprintf("%d %q %q", p.Number, b.Marker, b.Text))
			}
		}
	}
	return out
}

func TestFootnotes(t *testing.T) {
	text := sized(72, 700, 10, noteText1) + sized(72, 500, 10, noteText2)
	for _, c := range []struct {
		name, content string
		want          []string
	}{
		{"a number", text + sized(72, 120, 8, "1 See the report of 2019."), []string{`1 "1" "See the report of 2019."`}},
		{"a number set against its note", text + sized(72, 120, 8, "12See the report."), []string{`1 "12" "See the report."`}},
		{"signs", text + sized(72, 120, 8, "* As of June.") + sized(72, 90, 8, "** Estimated."), []string{`1 "*" "As of June."`, `1 "**" "Estimated."`}},
		{"a quote after the number", text + sized(72, 120, 8, `3 "Report", 2019.`), []string{`1 "3" "\"Report\", 2019."`}},
		{"as large as the text", text + sized(72, 120, 10, "1 See the report of 2019."), nil},
		{"no mark", text + sized(72, 120, 8, "See the report of 2019."), nil},
		{"a small letter after the number", text + sized(72, 120, 8, "5 bolts of steel"), nil},
		{"a number of four digits", text + sized(72, 120, 8, "2019 Annual report"), nil},
		{"a number with a full stop", text + sized(72, 120, 8, "1. See the report."), nil},
		{"text of the page below it", sized(72, 700, 10, noteText1) + sized(72, 600, 8, "1 See the report of 2019.") + sized(72, 500, 10, noteText2), nil},
		{"no text of the page above it", sized(72, 760, 8, "1 See the report of 2019.") + text, nil},
		{"under another column", text + sized(500, 120, 8, "1 See the report."), nil},
	} {
		doc := mustExtract(t, synthDoc{Font: fontCourier, Pages: []synthPage{{Content: c.content}}}.bytes(), nil)
		if got := notesOf(doc); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}

	t.Run("rendered with its mark", func(t *testing.T) {
		doc := mustExtract(t, synthDoc{Font: fontCourier, Pages: []synthPage{{Content: text + sized(72, 120, 8, "1See the report.")}}}.bytes(), nil)
		if got := doc.Text(); !strings.HasSuffix(got, "\n\n1 See the report.\n") {
			t.Errorf("text %q", got)
		}
	})

	t.Run("a page read by itself", func(t *testing.T) {
		f, err := gofastpdf.OpenBytes(synthDoc{Font: fontCourier, Pages: []synthPage{{Content: text + sized(72, 120, 8, "1 See the report.")}}}.bytes(), nil)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		p, _ := f.Page(1)
		part, _ := p.Part()
		if b := part.Blocks[len(part.Blocks)-1]; b.Kind != gofastcore.KindFootnote || b.Marker != "1" {
			t.Errorf("%s %q %q", b.Kind, b.Marker, b.Text)
		}
	})
}

// A paragraph that goes on to the next page is continued past the
// footnotes under it: it continues the paragraph, not the note.
func TestParagraphAcrossPagesPastFootnote(t *testing.T) {
	doc := mustExtract(t, synthDoc{Font: fontCourier, Pages: []synthPage{
		{Content: sized(72, 700, 10, noteText1) + sized(72, 500, 10, "and here is a sentence of the page that goes on to the") + sized(72, 120, 8, "1 A note that ends without a full stop")},
		{Content: sized(72, 700, 10, "next page, where it ends after some more words of it.") + sized(72, 500, 10, noteText2)},
	}}.bytes(), nil)
	var got []string
	for _, s := range kinds(doc) {
		if strings.HasSuffix(s, "continued") || strings.Contains(s, " footnote ") {
			got = append(got, s)
		}
	}
	want := []string{
		`1 footnote "A note that ends without a full stop"`,
		`2 paragraph "next page, where it ends after some more words of it." continued`,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%q, want %q", got, want)
	}
}
