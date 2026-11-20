package gofastpdf_test

import (
	"fmt"
	"strings"
	"testing"

	gofastcore "github.com/AOShei/go-fast-core"
	gofastpdf "github.com/AOShei/go-fast-pdf"
)

// marked is text under a marked-content identifier, at a place.
func marked(mcid, x, y int, text string) string {
	return fmt.Sprintf("/Span << /MCID %d >> BDC BT /F1 10 Tf %d %d Td (%s) Tj ET EMC\n", mcid, x, y, text)
}

// tablesOf describes the blocks of a part, with the grid of each table.
func tablesOf(part gofastcore.Part) string {
	var sb strings.Builder
	for _, b := range part.Blocks {
		switch {
		case b.Kind == gofastcore.KindTable:
			t := b.Table
			fmt.Fprintf(&sb, "table %s continued=%v header=%d merges=%v\n", b.Origin, b.Continued, t.HeaderRows, t.Merges)
			for _, row := range t.Rows {
				fmt.Fprintf(&sb, "  | %s |\n", strings.Join(row, " | "))
			}
		case b.Hidden != 0:
			fmt.Fprintf(&sb, "%s hidden %q\n", b.Kind, b.Text)
		default:
			fmt.Fprintf(&sb, "%s %q\n", b.Kind, b.Text)
		}
	}
	return sb.String()
}

// tableDoc is a tagged page with a table: a row of headers, a row of
// two cells, one of them with hidden text, and a cell over both
// columns.
func tableDoc() synthDoc {
	objects := []string{
		"<< /Type /StructTreeRoot /K 2 0 R /ParentTree 3 0 R >>",
		"<< /Type /StructElem /S /Document /P 1 0 R /K [4 0 R 5 0 R 14 0 R] >>",
		"<< /Nums [0 [4 0 R 9 0 R 10 0 R 11 0 R 12 0 R 13 0 R 14 0 R 12 0 R]] >>",
		"<< /Type /StructElem /S /P /P 2 0 R /K 0 >>",
		"<< /Type /StructElem /S /Table /P 2 0 R /K [6 0 R 7 0 R] >>",
		"<< /Type /StructElem /S /THead /P 5 0 R /K 8 0 R >>",
		"<< /Type /StructElem /S /TBody /P 5 0 R /K [15 0 R 16 0 R] >>",
		"<< /Type /StructElem /S /TR /P 6 0 R /K [9 0 R 10 0 R] >>",
		"<< /Type /StructElem /S /TH /P 8 0 R /K 1 >>",
		"<< /Type /StructElem /S /TH /P 8 0 R /K 2 >>",
		"<< /Type /StructElem /S /TD /P 15 0 R /K 3 >>",
		"<< /Type /StructElem /S /TD /P 15 0 R /K [4 7] >>",
		"<< /Type /StructElem /S /TD /P 16 0 R /K 5 /A [<< /O /Layout /Placement /Block >> << /O /Table /ColSpan 2 >> 0] >>",
		"<< /Type /StructElem /S /P /P 2 0 R /K 6 >>",
		"<< /Type /StructElem /S /TR /P 7 0 R /K [11 0 R 12 0 R] >>",
		"<< /Type /StructElem /S /TR /P 7 0 R /K 13 0 R >>",
	}
	content := marked(0, 72, 720, "Text before the table.") +
		marked(1, 72, 680, "Name") + marked(2, 300, 680, "Value") +
		marked(3, 72, 660, "alpha") + marked(4, 300, 660, "1") +
		"/Span << /MCID 7 >> BDC BT 3 Tr /F1 10 Tf 300 648 Td (secret) Tj 0 Tr ET EMC\n" +
		marked(5, 72, 636, "spanning both") +
		marked(6, 72, 600, "Text after the table.")
	return synthDoc{Font: fontCourier, Objects: objects, Catalog: "/StructTreeRoot 1 0 R /MarkInfo << /Marked true >>",
		Pages: []synthPage{{Content: content, Extra: "/StructParents 0"}}}
}

func TestTaggedTable(t *testing.T) {
	want := `paragraph "Text before the table."
table native continued=false header=1 merges=[{2 0 1 2}]
  | Name | Value |
  | alpha | 1 |
  | spanning both |  |
paragraph hidden "secret"
paragraph "Text after the table."
`
	doc := mustExtract(t, tableDoc().bytes(), nil)
	if got := tablesOf(doc.Parts[0]); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
	if md := doc.Markdown(); !strings.Contains(md, "| Name | Value |") {
		t.Errorf("markdown:\n%s", md)
	}

	t.Run("a page read by itself", func(t *testing.T) {
		f, err := gofastpdf.OpenBytes(tableDoc().bytes(), nil)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		p, _ := f.Page(1)
		part, err := p.Part()
		if err != nil {
			t.Fatal(err)
		}
		if got := tablesOf(part); got != want {
			t.Errorf("got:\n%s\nwant:\n%s", got, want)
		}
	})

	t.Run("the lines of the page", func(t *testing.T) {
		f, err := gofastpdf.OpenBytes(tableDoc().bytes(), nil)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		p, _ := f.Page(1)
		lines, err := p.Lines()
		if err != nil {
			t.Fatal(err)
		}
		var texts []string
		for _, l := range lines {
			texts = append(texts, l.Text)
		}
		for _, w := range []string{"Name", "alpha", "spanning both"} {
			if !strings.Contains(strings.Join(texts, "|"), w) {
				t.Errorf("no line %q in %q", w, texts)
			}
		}
	})

	t.Run("a row without cells", func(t *testing.T) {
		// A table whose rows hold paragraphs and no cells is no table:
		// its text is paragraphs, as without the tree.
		d := tableDoc()
		d.Objects = append([]string{}, d.Objects...)
		for _, i := range []int{8, 9, 10, 11, 12} {
			d.Objects[i] = strings.Replace(strings.Replace(d.Objects[i], "/S /TD", "/S /P", 1), "/S /TH", "/S /P", 1)
		}
		doc := mustExtract(t, d.bytes(), nil)
		for _, b := range doc.Parts[0].Blocks {
			if b.Kind == gofastcore.KindTable {
				t.Errorf("a table:\n%s", tablesOf(doc.Parts[0]))
			}
		}
		if text := doc.Text(); !strings.Contains(text, "alpha") || !strings.Contains(text, "spanning both") {
			t.Errorf("text lost: %q", text)
		}
	})

	t.Run("a tree that leads back to itself", func(t *testing.T) {
		d := tableDoc()
		d.Objects = append([]string{}, d.Objects...)
		d.Objects[6] = "<< /Type /StructElem /S /TBody /P 5 0 R /K [15 0 R 16 0 R 5 0 R 7 0 R] >>"
		d.Objects[14] = "<< /Type /StructElem /S /TR /P 7 0 R /K [11 0 R 12 0 R 15 0 R] >>"
		mustExtract(t, d.bytes(), nil) // ends, and keeps the contract
	})
}

// A table over two pages is a block on each, the second continued.
func TestTaggedTableOverPages(t *testing.T) {
	objects := []string{
		"<< /Type /StructTreeRoot /K 2 0 R /ParentTree 3 0 R >>",
		"<< /Type /StructElem /S /Table /P 1 0 R /K [4 0 R 5 0 R 6 0 R] >>",
		"<< /Nums [0 [7 0 R 8 0 R 9 0 R 10 0 R] 1 [11 0 R 12 0 R]] >>",
		"<< /Type /StructElem /S /TR /P 2 0 R /K [7 0 R 8 0 R] >>",
		"<< /Type /StructElem /S /TR /P 2 0 R /K [9 0 R 10 0 R] >>",
		"<< /Type /StructElem /S /TR /P 2 0 R /K [11 0 R 12 0 R] >>",
		"<< /Type /StructElem /S /TH /P 4 0 R /K 0 >>",
		"<< /Type /StructElem /S /TH /P 4 0 R /K 1 >>",
		"<< /Type /StructElem /S /TD /P 5 0 R /K 2 >>",
		"<< /Type /StructElem /S /TD /P 5 0 R /K 3 >>",
		"<< /Type /StructElem /S /TD /P 6 0 R /K 0 >>",
		"<< /Type /StructElem /S /TD /P 6 0 R /K 1 >>",
	}
	d := synthDoc{Font: fontCourier, Objects: objects, Catalog: "/StructTreeRoot 1 0 R /MarkInfo << /Marked true >>",
		Pages: []synthPage{
			{Content: marked(0, 72, 700, "Year") + marked(1, 300, 700, "Count") + marked(2, 72, 680, "2024") + marked(3, 300, 680, "17"), Extra: "/StructParents 0"},
			{Content: marked(0, 72, 700, "2025") + marked(1, 300, 700, "21"), Extra: "/StructParents 1"},
		}}
	doc := mustExtract(t, d.bytes(), nil)
	want := []string{
		"table native continued=false header=1 merges=[]\n  | Year | Count |\n  | 2024 | 17 |\n",
		"table native continued=true header=0 merges=[]\n  | 2025 | 21 |\n",
	}
	for i, p := range doc.Parts {
		if got := tablesOf(p); got != want[i] {
			t.Errorf("page %d:\n%s\nwant:\n%s", p.Number, got, want[i])
		}
	}
	// The renderers run the second block on from the first.
	if md := doc.Markdown(); strings.Count(md, "| --- |") != 1 {
		t.Errorf("markdown:\n%s", md)
	}
}
