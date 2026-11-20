package gofastpdf_test

import (
	"fmt"
	"strings"
	"testing"

	gofastcore "github.com/AOShei/go-fast-core"
	gofastpdf "github.com/AOShei/go-fast-pdf"
)

// A tagged PDF: what its structure tree says of its content.
func TestTaggedPDF(t *testing.T) {
	objects := []string{
		"<< /Type /StructTreeRoot /K 2 0 R /ParentTree 9 0 R /RoleMap << /Title /H1 /Heading /Title >> >>",
		"<< /Type /StructElem /S /Document /P 1 0 R /K [3 0 R 4 0 R 5 0 R 6 0 R] >>",
		"<< /Type /StructElem /S /Heading /P 2 0 R /K 0 >>", // Heading is Title is H1
		"<< /Type /StructElem /S /P /P 2 0 R /K 1 >>",
		"<< /Type /StructElem /S /Figure /P 2 0 R /Alt (A red square on white) /K 2 >>",
		"<< /Type /StructElem /S /Sect /P 2 0 R /K 7 0 R >>",
		"<< /Type /StructElem /S /Sect /P 6 0 R /K 8 0 R >>",
		"<< /Type /StructElem /S /H /P 7 0 R /K 3 >>", // in two sections: level 2
		"<< /Nums [0 [3 0 R 4 0 R 5 0 R 8 0 R]] >>",
	}
	content := "/Heading << /MCID 0 >> BDC " + set("F1", 10, 72, 720, "The Title Set Plain") + "EMC\n" +
		"/P << /MCID 1 >> BDC " + set("F1", 20, 72, 680, "Large Text Tagged as Text") + para(640, "The body") + "EMC\n" +
		"/Figure << /MCID 2 >> BDC q 50 0 0 50 300 500 cm BI /W 1 /H 1 /CS /G /BPC 8 ID x EI Q EMC\n" +
		"/H << /MCID 3 >> BDC " + set("F1", 10, 72, 450, "A Subsection") + "EMC\n" +
		set("F1", 10, 72, 420, "Untagged text at the end.")
	d := synthDoc{Font: fontCourier, Objects: objects, Catalog: "/StructTreeRoot 1 0 R /MarkInfo << /Marked true >>",
		Pages: []synthPage{{Content: content, Extra: "/StructParents 0"}}}

	check := func(t *testing.T, part gofastcore.Part) {
		t.Helper()
		var got []string
		for _, b := range part.Blocks {
			switch {
			case b.Kind == gofastcore.KindHeading:
				got = append(got, fmt.Sprintf("H%d %s %s", b.Level, b.Origin, b.Text))
			case b.Kind == gofastcore.KindImage:
				got = append(got, "image "+b.Text)
			}
		}
		want := []string{"H1 native The Title Set Plain", "image A red square on white", "H2 native A Subsection"}
		if strings.Join(got, "\n") != strings.Join(want, "\n") {
			t.Errorf("got:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
		}
	}
	doc := mustExtract(t, d.bytes(), nil)
	check(t, doc.Parts[0])
	if md := doc.Markdown(); !strings.Contains(md, "# The Title Set Plain") || !strings.Contains(md, "## A Subsection") {
		t.Errorf("markdown %q", md)
	}

	t.Run("a page read by itself", func(t *testing.T) {
		f, err := gofastpdf.OpenBytes(d.bytes(), nil)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		p, err := f.Page(1)
		if err != nil {
			t.Fatal(err)
		}
		part, err := p.Part()
		if err != nil {
			t.Fatal(err)
		}
		check(t, part)
	})

	t.Run("a tree that leads back to itself", func(t *testing.T) {
		loop := append([]string{}, objects...)
		loop[1] = "<< /Type /StructElem /S /Document /P 8 0 R /K [3 0 R 4 0 R 5 0 R 6 0 R] >>"
		d := d
		d.Objects = loop
		mustExtract(t, d.bytes(), nil) // ends, and keeps the contract
	})
}
