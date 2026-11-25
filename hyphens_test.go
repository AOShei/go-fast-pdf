package gofastpdf_test

import (
	"strings"
	"testing"

	gofastcore "github.com/AOShei/go-fast-core"
	gofastpdf "github.com/AOShei/go-fast-pdf"
)

// lines sets lines of body text one under the other, from y down. Each
// fills its measure: Courier at 10 points sets 78 letters in 468 points.
func lines(y int, ls ...string) string {
	var sb strings.Builder
	for i, l := range ls {
		sb.WriteString(set("F1", 10, 72, y-12*i, l))
	}
	return sb.String()
}

func hyphenTexts(doc *gofastcore.Document) string {
	var sb strings.Builder
	for _, p := range doc.Parts {
		for _, b := range p.Blocks {
			sb.WriteString(b.Text)
			sb.WriteByte('\n')
		}
	}
	return sb.String()
}

func TestHyphenAtLineEnd(t *testing.T) {
	page1 := lines(700,
		"The declaration of the year is made by every household that has an income decla-",
		"ration to make, and the well-known rule of the year before applies to it as well-",
		"known rules do, while a word that the document writes nowhere else, as a hippo-",
		"potamus is, keeps the hyphen that the line gave it, since nothing says more here.")
	page2 := lines(700,
		"On the second page the administration answers what the household asked, and an-",
		"swers in writing and within the month that the law gives it, as the administra-",
		"tion does: answers come by post.")
	pdf := synthDoc{Font: fontCourier, Pages: []synthPage{{Content: page1}, {Content: page2}}}.bytes()
	doc := mustExtract(t, pdf, nil)
	got := hyphenTexts(doc)
	for _, want := range []string{
		"an income declaration to make",            // the word is whole in the document
		"applies to it as well-known rules do",     // it is there with the hyphen inside a line
		"as a hippo-potamus is",                    // nothing says
		"asked, and answers in writing and within", // the whole word comes later on the page
		"as the administration does",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("text has no %q:\n%s", want, got)
		}
	}

	// The same for any number of workers, and page by page.
	for _, workers := range []int{1, 2, 8} {
		d := mustExtract(t, pdf, &gofastcore.Options{Workers: workers})
		if hyphenTexts(d) != got {
			t.Errorf("%d workers: %q", workers, hyphenTexts(d))
		}
	}

	// A page read by itself has its own words: the first page says
	// "declaration", the second has "administration" and "answers".
	f, err := gofastpdf.OpenBytes(pdf, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	for n, want := range map[int]string{1: "an income declaration to make", 2: "as the administration does"} {
		page, err := f.Page(n)
		if err != nil {
			t.Fatal(err)
		}
		part, _ := page.Part()
		if text := part.Text(); !strings.Contains(text, want) {
			t.Errorf("page %d by itself has no %q: %q", n, want, text)
		}
	}
}

// The spans of a block follow the text when a hyphen is left out of it.
func TestHyphenAtLineEndSpans(t *testing.T) {
	page := lines(700,
		"The declaration of the year is made by every household that has an income decla-") +
		set("F2", 10, 72, 688, "ration") + set("F1", 10, 114, 688, "to make, and no household is without one.")
	doc := mustExtract(t, synthDoc{Font: fontCourier, Pages: []synthPage{{Content: page, Fonts: boldFont}}}.bytes(), &gofastcore.Options{Spans: true})
	for _, b := range doc.Parts[0].Blocks {
		if !strings.Contains(b.Text, "income declaration to make") {
			t.Fatalf("text %q", b.Text)
		}
		bold := ""
		for _, s := range b.Spans {
			if s.Style&gofastcore.StyleBold != 0 {
				bold += b.Text[s.Start:s.End]
			}
		}
		if bold != "ration" {
			t.Errorf("bold span is %q, want %q; spans %+v", bold, "ration", b.Spans)
		}
	}
}
