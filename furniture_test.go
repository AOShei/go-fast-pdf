package gofastpdf_test

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"

	gofastcore "github.com/AOShei/go-fast-core"
	gofastpdf "github.com/AOShei/go-fast-pdf"
)

// Running headers and footers, and paragraphs split by a page break
// (furniture.go).

// line is text set at a place on the page, from the bottom left.
func line(x, y int, text string) string {
	return fmt.Sprintf("BT /F1 10 Tf %d %d Td (%s) Tj ET\n", x, y, text)
}

// body is the text of a page between its margins.
func body(n int) string {
	return line(72, 600, fmt.Sprintf("The text of page %d, which is different on every page.", n)) +
		line(72, 400, fmt.Sprintf("A second paragraph of page %d, far below the first.", n))
}

// kinds lists the blocks of a document as kind and text, page by page.
func kinds(doc *gofastcore.Document) []string {
	var out []string
	for _, p := range doc.Parts {
		for _, b := range p.Blocks {
			s := fmt.Sprintf("%d %s %q", p.Number, b.Kind, b.Text)
			if b.Continued {
				s += " continued"
			}
			out = append(out, s)
		}
	}
	return out
}

func tagged(doc *gofastcore.Document) (out []string) {
	for _, s := range kinds(doc) {
		if strings.Contains(s, " header ") || strings.Contains(s, " footer ") || strings.HasSuffix(s, "continued") {
			out = append(out, s)
		}
	}
	return out
}

func TestRunningHeadersAndFooters(t *testing.T) {
	const n = 8
	pages := make([]synthPage, n)
	roman := []string{"i", "ii", "iii", "iv", "v", "vi", "vii", "viii"}
	for i := range pages {
		p := i + 1
		head := line(72, 760, "Annual Report 2024")
		if p%2 == 0 {
			head = line(400, 760, "Chapter One: Results") // the other side on even pages
		}
		pages[i].Content = head + body(p) + line(290, 30, fmt.Sprintf("Page %d of %d", p, n)) + line(500, 30, roman[i])
	}
	data := synthDoc{Font: fontCourier, Pages: pages}.bytes()
	doc := mustExtract(t, data, nil)
	var want []string
	for p := 1; p <= n; p++ {
		head := "Annual Report 2024"
		if p%2 == 0 {
			head = "Chapter One: Results"
		}
		// The page number and the roman one are on one line: one block.
		want = append(want, fmt.Sprintf("%d header %q", p, head), fmt.Sprintf("%d footer %q", p, fmt.Sprintf("Page %d of %d\t%s", p, n, roman[p-1])))
	}
	if got := tagged(doc); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("tagged:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}

	t.Run("left out of rendered text, there when asked for", func(t *testing.T) {
		if text := doc.Text(); strings.Contains(text, "Annual Report") || strings.Contains(text, "Page 3 of") || !strings.Contains(text, "The text of page 3") {
			t.Errorf("text %q", text)
		}
		var sb strings.Builder
		doc.WriteText(&sb, &gofastcore.RenderOptions{Furniture: true})
		if !strings.Contains(sb.String(), "Annual Report") || !strings.Contains(sb.String(), "Page 3 of 8") {
			t.Errorf("with furniture: %q", sb.String())
		}
	})

	t.Run("the same for any number of workers, from the iterator, and for a range", func(t *testing.T) {
		if diff := sameDocument(doc, mustExtract(t, data, &gofastcore.Options{Workers: 8})); diff != "" {
			t.Error(diff)
		}
		f, err := gofastpdf.OpenBytes(data, &gofastcore.Options{Workers: 3})
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		var parts []gofastcore.Part
		for p, err := range f.Parts(context.Background()) {
			if err != nil {
				t.Fatal(err)
			}
			parts = append(parts, p)
		}
		if !reflect.DeepEqual(parts, doc.Parts) {
			t.Error("the iterator's parts are not the document's")
		}
		// Pages 3 to 5: each is compared with the others of the range.
		// The footers are on all three. A header of one side is on one
		// other page of the three, and two are asked for: not tagged.
		r := mustExtract(t, data, &gofastcore.Options{Parts: gofastcore.PartRange{First: 3, Last: 5}})
		if got := len(tagged(r)); got != 3 {
			t.Errorf("pages 3 to 5: %d tagged\n%s", got, strings.Join(tagged(r), "\n"))
		}
	})
}

func TestNotHeadersOrFooters(t *testing.T) {
	cases := map[string]func(p int) string{
		// A row of a table that every page repeats at its top.
		"a repeated table head": func(p int) string {
			return "BT /F1 10 Tf 72 760 Td (Code) Tj 100 0 Td (Name) Tj 100 0 Td (Unit) Tj 100 0 Td (Amount) Tj ET\n" + body(p)
		},
		// The scale of a map at the foot of every page.
		"a repeated scale": func(p int) string {
			return body(p) + "BT /F1 10 Tf 72 30 Td (0) Tj 50 0 Td (1.5) Tj 50 0 Td (3) Tj ET\n"
		},
		// Text at the top that is not the same on other pages.
		"different text at the top": func(p int) string {
			return line(72, 760, fmt.Sprintf("Section %c begins here", 'A'+p)) + body(p)
		},
		// A page whose only text is at its top, as a slide's title.
		"text only in the margin": func(p int) string { return line(72, 760, fmt.Sprintf("Slide %d", p)) },
		// Long text at the top, the same on every page.
		"a long repeated paragraph": func(p int) string {
			return line(72, 760, strings.Repeat("A long notice repeated on every page. ", 5)) + body(p)
		},
		// Hidden text in the margin.
		"hidden in the margin": func(p int) string { return "3 Tr " + line(72, 760, "Hidden title") + "0 Tr " + body(p) },
	}
	for name, page := range cases {
		pages := make([]synthPage, 5)
		for i := range pages {
			pages[i].Content = page(i + 1)
		}
		doc := mustExtract(t, synthDoc{Font: fontCourier, Pages: pages}.bytes(), nil)
		for _, s := range tagged(doc) {
			if !strings.HasSuffix(s, "continued") {
				t.Errorf("%s: %s", name, s)
			}
		}
	}

	t.Run("one page; two pages", func(t *testing.T) {
		one := mustExtract(t, synthDoc{Font: fontCourier, Pages: []synthPage{{Content: line(72, 760, "Report") + body(1)}}}.bytes(), nil)
		if got := tagged(one); len(got) != 0 {
			t.Errorf("one page: %q", got)
		}
		two := mustExtract(t, synthDoc{Font: fontCourier, Pages: []synthPage{{Content: line(72, 760, "Report") + body(1)}, {Content: line(72, 760, "Report") + body(2)}}}.bytes(), nil)
		if got := tagged(two); len(got) != 2 {
			t.Errorf("two pages: %q", got)
		}
	})
}

func TestParagraphsAcrossPages(t *testing.T) {
	page := func(first, last string) synthPage {
		return synthPage{Content: line(72, 760, "Running head") + line(72, 650, first) + line(72, 400, last) + line(290, 30, "7")}
	}
	doc := mustExtract(t, synthDoc{Font: fontCourier, Pages: []synthPage{
		page("A first paragraph that ends.", "and here a sentence that goes on to the"),
		page("next page where it ends.", "This one ends with a full stop."),
		page("so this one is not continued.", "Nor is the next, which ends without one"),
		page("But starts with a capital letter.", "A last line."),
	}}.bytes(), nil)
	var continued []string
	for _, s := range kinds(doc) {
		if strings.HasSuffix(s, "continued") {
			continued = append(continued, s)
		}
	}
	if want := []string{`2 paragraph "next page where it ends." continued`}; !reflect.DeepEqual(continued, want) {
		t.Errorf("continued: %q", continued)
	}
	// The renderers run the two pieces into one paragraph.
	if text := doc.Text(); !strings.Contains(text, "goes on to the next page where it ends.") {
		t.Errorf("text %q", text)
	}
	// A page read by itself knows nothing of the page before it.
	f, _ := gofastpdf.OpenBytes(synthDoc{Font: fontCourier, Pages: []synthPage{page("a", "b"), page("c", "d")}}.bytes(), nil)
	defer f.Close()
	p, _ := f.Page(2)
	part, _ := p.Part()
	for _, b := range part.Blocks {
		if b.Continued || b.Kind.Furniture() {
			t.Errorf("page 2 read by itself: %s %q continued %v", b.Kind, b.Text, b.Continued)
		}
	}
}
