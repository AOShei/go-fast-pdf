package gofastpdf_test

import (
	"fmt"
	"strings"
	"testing"

	gofastcore "github.com/AOShei/go-fast-core"
	gofastpdf "github.com/AOShei/go-fast-pdf"
)

// anchorsOf lists the blocks of a part that have an anchor.
func anchorsOf(part gofastcore.Part) (out []string) {
	for _, b := range part.Blocks {
		if b.Loc.Anchor != "" {
			out = append(out, fmt.Sprintf("%s: %s", b.Loc.Anchor, b.Text))
		}
	}
	return out
}

// anchorsDoc is a document of two pages with named destinations in a
// name tree and in the dictionary of PDF 1.1.
func anchorsDoc() synthDoc {
	// Objects 1 to 3; the catalog is 4, the page tree 5, the font 6, and
	// the pages 8 and 10 after their content.
	objects := []string{
		"<< /Kids [2 0 R] >>",
		"<< /Limits [(alias) (\xfe\xff\x00s\x00e\x00c\x00o\x00n\x00d)] /Names [" +
			"(alias) [8 0 R /XYZ 72 740 0] " + // the heading too, but further from it than intro
			"(inside) [8 0 R /XYZ 72 670 0] " + // inside a paragraph, though another begins near
			"(intro) [8 0 R /XYZ 72 735 0] " +
			"(near) [8 0 R /XYZ 72 700 0] " + // two paragraphs begin below it: the nearer
			"(fit) [8 0 R /Fit] " + // no place on the page
			"(page.2) [10 0 R /XYZ 0 792 0] " + // the top of the page: the page, though a block begins near
			"(\xfe\xff\x00s\x00e\x00c\x00o\x00n\x00d) [1 /XYZ 72 715 null]" + // a page by its index, a name in UTF-16
			"] >>",
		"<< /old [10 0 R /FitH 615] >>", // the dictionary of PDF 1.1
	}
	return synthDoc{Font: fontCourier, Objects: objects, Catalog: "/Names << /Dests 1 0 R >> /Dests 3 0 R",
		Pages: []synthPage{
			{Content: set("F1", 14, 72, 720, "Introduction") + para(690, "Body text") + para(645, "More text")},
			{Content: set("F1", 10, 72, 775, "Top line of the second page.") +
				set("F1", 10, 72, 700, "Second page text that runs on.") + set("F1", 10, 72, 600, "Another part of the page.")},
		}}
}

// Named destinations give their names to the blocks that begin where
// they point, with Options.Spans.
func TestNamedDestinationAnchors(t *testing.T) {
	d := anchorsDoc()
	want := [][]string{
		{"intro: Introduction", "near: Body text and some more words of the body that go on for a second line of the same paragraph and end on a third line here."},
		{"second: Second page text that runs on.", "old: Another part of the page."},
	}
	spans := &gofastcore.Options{Spans: true}
	doc := mustExtract(t, d.bytes(), spans)
	for i, p := range doc.Parts {
		if got := anchorsOf(p); strings.Join(got, "\n") != strings.Join(want[i], "\n") {
			t.Errorf("page %d: got\n%s\nwant\n%s", p.Number, strings.Join(got, "\n"), strings.Join(want[i], "\n"))
		}
	}

	t.Run("a page read by itself", func(t *testing.T) {
		f, err := gofastpdf.OpenBytes(d.bytes(), spans)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		p, err := f.Page(2)
		if err != nil {
			t.Fatal(err)
		}
		part, err := p.Part()
		if err != nil {
			t.Fatal(err)
		}
		if got := anchorsOf(part); strings.Join(got, "\n") != strings.Join(want[1], "\n") {
			t.Errorf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want[1], "\n"))
		}
	})

	t.Run("without spans", func(t *testing.T) {
		doc := mustExtract(t, d.bytes(), nil)
		for _, p := range doc.Parts {
			if got := anchorsOf(p); len(got) > 0 {
				t.Errorf("page %d: %q", p.Number, got)
			}
		}
	})

	t.Run("a name longer than a name can be", func(t *testing.T) {
		long := strings.Repeat("n", 600)
		d := d
		d.Objects = append([]string{}, d.Objects...)
		d.Objects[2] = "<< /" + long + " [10 0 R /FitH 615] >>"
		doc := mustExtract(t, d.bytes(), spans)
		if got := anchorsOf(doc.Parts[1]); len(got) != 1 || !strings.HasPrefix(got[0], "second:") {
			t.Errorf("got %q", got)
		}
	})
}

// Running headers have no anchors: a name of the top of each page goes to
// the first block below them.
func TestAnchorsSkipHeaders(t *testing.T) {
	var pages []synthPage
	var names []string
	for i := range 4 {
		pages = append(pages, synthPage{Content: set("F1", 9, 72, 770, "A Running Title") +
			set("F1", 14, 72, 700, fmt.Sprintf("Chapter %d Begins", i+1)) + para(670, "Body text")})
		names = append(names, fmt.Sprintf("(top.%d) [%d /XYZ 72 785 null] (chap.%d) [%d /XYZ 72 717 null]", i+1, i, i+1, i))
	}
	d := synthDoc{Font: fontCourier, Objects: []string{"<< /Names [" + strings.Join(names, " ") + "] >>"},
		Catalog: "/Names << /Dests 1 0 R >>", Pages: pages}
	doc := mustExtract(t, d.bytes(), &gofastcore.Options{Spans: true})
	for _, p := range doc.Parts {
		got := anchorsOf(p)
		want := fmt.Sprintf("chap.%d: Chapter %d Begins", p.Number, p.Number)
		if len(got) != 1 || got[0] != want {
			t.Errorf("page %d: got %q, want %q", p.Number, got, want)
		}
		if p.Blocks[0].Kind != gofastcore.KindHeader {
			t.Errorf("page %d: the running title is %s", p.Number, p.Blocks[0].Kind)
		}
	}
}
