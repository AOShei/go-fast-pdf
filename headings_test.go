package gofastpdf_test

import (
	"fmt"
	"strings"
	"testing"

	gofastcore "github.com/AOShei/go-fast-core"
	gofastpdf "github.com/AOShei/go-fast-pdf"
)

// set is text in font fn at size pt, at a place on the page.
func set(fn string, pt, x, y int, text string) string {
	return fmt.Sprintf("BT /%s %d Tf %d %d Td (%s) Tj ET\n", fn, pt, x, y, text)
}

// para is a paragraph of body text: three lines of 10 points.
func para(y int, words string) string {
	return set("F1", 10, 72, y, words+" and some more words of the body") +
		set("F1", 10, 72, y-12, "that go on for a second line of the same paragraph") +
		set("F1", 10, 72, y-24, "and end on a third line here.")
}

const boldFont = "/F2 << /Type /Font /Subtype /Type1 /BaseFont /Courier-Bold >>"

func headingsOf(doc *gofastcore.Document) (out []string) {
	for _, p := range doc.Parts {
		for _, b := range p.Blocks {
			if b.Kind == gofastcore.KindHeading {
				s := fmt.Sprintf("%d L%d %q %s", p.Number, b.Level, b.Text, b.Origin)
				if l := b.Attrs[gofastcore.AttrLevel]; l != "" {
					s += " level=" + l
				}
				out = append(out, s)
			}
		}
	}
	return out
}

func TestHeadingsByType(t *testing.T) {
	page1 := set("F1", 24, 72, 720, "A Report on Things") +
		para(680, "The opening paragraph") +
		set("F1", 16, 72, 620, "1 Introduction") +
		para(590, "The first section") +
		set("F1", 13, 72, 530, "1.1 Background") +
		para(500, "The first subsection") +
		set("F2", 10, 72, 440, "A Run-in Heading") +
		para(420, "Under the bold heading") +
		set("F2", 10, 72, 360, "Bold text that ends as a sentence.") +
		para(340, "Another paragraph") +
		set("F1", 16, 72, 280, "Too") +
		para(260, "Below a short large word")
	doc := mustExtract(t, synthDoc{Font: fontCourier, Pages: []synthPage{{Content: page1, Fonts: boldFont}}}.bytes(), nil)
	want := []string{
		`1 L1 "A Report on Things" inferred`,
		`1 L2 "1 Introduction" inferred`,
		`1 L3 "1.1 Background" inferred`,
		`1 L4 "A Run-in Heading" inferred`,
	}
	if got := headingsOf(doc); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("headings:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	// Markdown writes them as headings.
	if md := doc.Markdown(); !strings.Contains(md, "# A Report on Things") || !strings.Contains(md, "### 1.1 Background") {
		t.Errorf("markdown %q", md)
	}

	t.Run("not on a map", func(t *testing.T) {
		var labels strings.Builder
		for i := 0; i < 12; i++ {
			// Scattered, each at a height of its own.
			labels.WriteString(set("F1", 16, 72+(i%3)*170, 720-i*45, fmt.Sprintf("Lake Number %c", 'A'+i)))
		}
		labels.WriteString(para(150, "A legend under the map"))
		d := mustExtract(t, synthDoc{Font: fontCourier, Pages: []synthPage{{Content: labels.String()}, {Content: page1, Fonts: boldFont}}}.bytes(), nil)
		for _, h := range headingsOf(d) {
			if strings.HasPrefix(h, "1 ") {
				t.Errorf("on the map: %s", h)
			}
		}
	})

	t.Run("not the entries of a table of contents", func(t *testing.T) {
		content := set("F1", 16, 72, 720, "Contents") +
			set("F2", 10, 72, 690, "Introduction .......... 1") +
			set("F2", 10, 72, 660, "Background ............ 4") +
			set("F1", 14, 72, 630, "Preface ......... iv") +
			para(600, "A paragraph under the contents")
		d := mustExtract(t, synthDoc{Font: fontCourier, Pages: []synthPage{{Content: content, Fonts: boldFont}, {Content: para(700, "More") + para(600, "And more")}}}.bytes(), nil)
		for _, h := range headingsOf(d) {
			if strings.Contains(h, "....") {
				t.Errorf("a heading: %s", h)
			}
		}
	})

	t.Run("not hidden text, not when the body is bold", func(t *testing.T) {
		hidden := "3 Tr " + set("F1", 20, 72, 700, "Hidden Large Title") + "0 Tr " + para(650, "Body")
		d := mustExtract(t, synthDoc{Font: fontCourier, Pages: []synthPage{{Content: hidden}}}.bytes(), nil)
		if got := headingsOf(d); len(got) != 0 {
			t.Errorf("hidden: %q", got)
		}
		boldBody := strings.ReplaceAll(para(700, "All bold")+para(600, "Still bold"), "/F1", "/F2") + set("F2", 10, 72, 500, "A Bold Line")
		d = mustExtract(t, synthDoc{Font: fontCourier, Pages: []synthPage{{Content: boldBody, Fonts: boldFont}}}.bytes(), nil)
		if got := headingsOf(d); len(got) != 0 {
			t.Errorf("bold body: %q", got)
		}
	})
}

// A block that an entry of the outline names is a heading at the
// entry's level, and the file says so.
func TestHeadingsFromTheOutline(t *testing.T) {
	content := para(720, "Opening") +
		set("F1", 10, 72, 650, "Methods and Materials") + // at the size of the body
		para(630, "Under it") +
		set("F1", 10, 72, 560, "A Deep Section") +
		para(540, "Under the deep one")
	objects := []string{
		"<< /Type /Outlines /First 2 0 R /Last 2 0 R >>",
		"<< /Title (2. Methods and Materials) /Parent 1 0 R /First 3 0 R /Last 3 0 R /Dest [12 0 R /XYZ 0 650 0] >>",
		"<< /Title (A Deep Section) /Parent 2 0 R /First 4 0 R /Last 4 0 R /Dest [12 0 R /XYZ 0 560 0] >>",
		"<< /Title (Level three) /Parent 3 0 R /First 5 0 R /Last 5 0 R /Dest [12 0 R /XYZ 0 100 0] >>",
		"<< /Title (Level four) /Parent 4 0 R /First 6 0 R /Last 6 0 R /Dest [12 0 R /XYZ 0 100 0] >>",
		"<< /Title (Level five) /Parent 5 0 R /First 7 0 R /Last 7 0 R /Dest [12 0 R /XYZ 0 100 0] >>",
		"<< /Title (Level six) /Parent 6 0 R /Dest [12 0 R /XYZ 0 100 0] >>",
	}
	// The page is object 12: seven objects, then the catalog, the tree,
	// the font and the page's content.
	d := synthDoc{Font: fontCourier, Objects: objects, Catalog: "/Outlines 1 0 R", Pages: []synthPage{{Content: content}}}
	doc := mustExtract(t, d.bytes(), nil)
	want := []string{
		`1 L1 "Methods and Materials" native`,
		`1 L2 "A Deep Section" native`,
	}
	if got := headingsOf(doc); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("headings:\n%s\nwant:\n%s\noutline %+v", strings.Join(got, "\n"), strings.Join(want, "\n"), doc.Outline)
	}

	t.Run("deeper than six", func(t *testing.T) {
		deep := []string{"<< /Type /Outlines /First 2 0 R /Last 2 0 R >>"}
		for i := 2; i <= 8; i++ {
			kid := ""
			if i < 8 {
				kid = fmt.Sprintf("/First %d 0 R /Last %d 0 R", i+1, i+1)
			}
			deep = append(deep, fmt.Sprintf("<< /Title (Level %d) /Parent %d 0 R %s /Dest [13 0 R /XYZ 0 700 0] >>", i-1, i-1, kid))
		}
		content := set("F1", 10, 72, 700, "Level 7") + para(650, "Under level seven")
		doc := mustExtract(t, synthDoc{Font: fontCourier, Objects: deep, Catalog: "/Outlines 1 0 R", Pages: []synthPage{{Content: content}}}.bytes(), nil)
		if got := headingsOf(doc); len(got) != 1 || got[0] != `1 L6 "Level 7" native level=7` {
			t.Errorf("%q; outline %+v", got, doc.Outline)
		}
		if !doc.Parts[0].Has(gofastcore.WarnHeadingLevelClamped) {
			t.Errorf("no %s on the part: %+v", gofastcore.WarnHeadingLevelClamped, doc.Parts[0].Warnings)
		}
	})

	t.Run("a page read by itself", func(t *testing.T) {
		f, err := gofastpdf.OpenBytes(d.bytes(), nil)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		p, _ := f.Page(1)
		part, _ := p.Part()
		n := 0
		for _, b := range part.Blocks {
			if b.Kind == gofastcore.KindHeading {
				n++
			}
		}
		if n != 2 {
			t.Errorf("%d headings on the page read by itself", n)
		}
	})
}
