package gofastpdf_test

import (
	"fmt"
	"strings"
	"testing"

	gofastcore "github.com/AOShei/go-fast-core"
)

// listDoc is a tagged page with a list of four items: one with a label,
// one whose bullet is drawn and not written, one with a list in it, and
// one whose bullet is in its body, a mark that is a marker only because
// the tree says the text is an item.
func listDoc() synthDoc {
	objects := []string{
		"<< /Type /StructTreeRoot /K 2 0 R /ParentTree 3 0 R >>",
		"<< /Type /StructElem /S /Document /P 1 0 R /K [4 0 R 5 0 R 15 0 R] >>",
		"<< /Nums [0 [4 0 R 8 0 R 9 0 R 10 0 R 12 0 R 13 0 R 17 0 R 18 0 R 14 0 R 15 0 R]] >>",
		"<< /Type /StructElem /S /P /P 2 0 R /K 0 >>",
		"<< /Type /StructElem /S /L /P 2 0 R /K [6 0 R 7 0 R 11 0 R 19 0 R] /A << /O /List /ListNumbering /Disc >> >>",
		"<< /Type /StructElem /S /LI /P 5 0 R /K [8 0 R 9 0 R] >>",
		"<< /Type /StructElem /S /LI /P 5 0 R /K 10 0 R >>",
		"<< /Type /StructElem /S /Lbl /P 6 0 R /K 1 >>",
		"<< /Type /StructElem /S /LBody /P 6 0 R /K 2 >>",
		"<< /Type /StructElem /S /LBody /P 7 0 R /K 3 >>",
		"<< /Type /StructElem /S /LI /P 5 0 R /K [12 0 R 13 0 R] >>",
		"<< /Type /StructElem /S /Lbl /P 11 0 R /K 4 >>",
		"<< /Type /StructElem /S /LBody /P 11 0 R /K [5 16 0 R] >>",
		"<< /Type /StructElem /S /LBody /P 19 0 R /K 8 >>",
		"<< /Type /StructElem /S /P /P 2 0 R /K 9 >>",
		"<< /Type /StructElem /S /L /P 13 0 R /K 20 0 R /A [<< /O /List /ListNumbering /Decimal >> 0] >>",
		"<< /Type /StructElem /S /Lbl /P 20 0 R /K 6 >>",
		"<< /Type /StructElem /S /LBody /P 20 0 R /K 7 >>",
		"<< /Type /StructElem /S /LI /P 5 0 R /K 14 0 R >>",
		"<< /Type /StructElem /S /LI /P 16 0 R /K [17 0 R 18 0 R] >>",
	}
	content := marked(0, 72, 720, "Text before the list.") +
		marked(1, 72, 690, `\267`) + marked(2, 90, 690, "First item, labelled.") +
		marked(3, 90, 678, "Second item, its bullet drawn.") +
		marked(4, 72, 666, `\267`) + marked(5, 90, 666, "Third item, with a list.") +
		marked(6, 108, 654, "1.") + marked(7, 126, 654, "The nested item.") +
		marked(8, 72, 642, "* Fourth item.") +
		marked(9, 72, 600, "Text after the list.")
	return synthDoc{Font: fontCourier, Objects: objects, Catalog: "/StructTreeRoot 1 0 R /MarkInfo << /Marked true >>",
		Pages: []synthPage{{Content: content, Extra: "/StructParents 0"}}}
}

func TestTaggedList(t *testing.T) {
	doc := mustExtract(t, listDoc().bytes(), nil)
	var got []string
	for _, b := range doc.Parts[0].Blocks {
		got = append(got, fmt.Sprintf("%s %s level=%d ordered=%v marker=%q %q", b.Kind, b.Origin, b.Level, b.Ordered, b.Marker, b.Text))
	}
	want := []string{
		`paragraph inferred level=0 ordered=false marker="" "Text before the list."`,
		`list_item native level=1 ordered=false marker="•" "First item, labelled."`,
		`list_item native level=1 ordered=false marker="" "Second item, its bullet drawn."`,
		`list_item native level=1 ordered=false marker="•" "Third item, with a list."`,
		`list_item native level=2 ordered=true marker="1." "The nested item."`,
		`list_item native level=1 ordered=false marker="*" "Fourth item."`,
		`paragraph inferred level=0 ordered=false marker="" "Text after the list."`,
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("got:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	// The renderers put the markers back, and a dash where the bullet
	// was drawn.
	if text := doc.Text(); !strings.Contains(text, "• First item") || !strings.Contains(text, "- Second item") || !strings.Contains(text, "  1. The nested item.") {
		t.Errorf("text:\n%s", text)
	}

	t.Run("without the tree", func(t *testing.T) {
		// The same page untagged: the items found by their markers. The
		// second is not one, and the fourth runs into the item above it.
		d := listDoc()
		d.Catalog = ""
		var items []string
		for _, b := range mustExtract(t, d.bytes(), nil).Parts[0].Blocks {
			if b.Kind == gofastcore.KindListItem {
				items = append(items, b.Text)
			}
		}
		if strings.Join(items, "|") != "First item, labelled.|Third item, with a list.|The nested item. * Fourth item." {
			t.Errorf("items %q", items)
		}
	})
}

// gridDoc is a tagged page of four blocks in two rows of two, drawn row
// by row, and a title above them that the tree gives last, as a word
// processor gives a text box where it is anchored. The blocks on the
// right are set a little lower, so that a row is not one line.
func gridDoc(byColumns bool) synthDoc {
	// The tree's order of the four blocks: by rows, or by columns.
	order := "4 0 R 5 0 R 6 0 R 7 0 R"
	if byColumns {
		order = "4 0 R 6 0 R 5 0 R 7 0 R"
	}
	objects := []string{
		"<< /Type /StructTreeRoot /K 2 0 R /ParentTree 3 0 R >>",
		"<< /Type /StructElem /S /Document /P 1 0 R /K [" + order + " 8 0 R] >>",
		"<< /Nums [0 [8 0 R 4 0 R 5 0 R 6 0 R 7 0 R]] >>",
		"<< /Type /StructElem /S /P /P 2 0 R /K 1 >>",
		"<< /Type /StructElem /S /P /P 2 0 R /K 2 >>",
		"<< /Type /StructElem /S /P /P 2 0 R /K 3 >>",
		"<< /Type /StructElem /S /P /P 2 0 R /K 4 >>",
		"<< /Type /StructElem /S /P /P 2 0 R /K 0 >>",
	}
	content := marked(0, 72, 740, "The title of the page") +
		marked(1, 72, 700, "Top left.") + marked(2, 320, 694, "Top right.") +
		marked(3, 72, 640, "Bottom left.") + marked(4, 320, 634, "Bottom right.")
	return synthDoc{Font: fontCourier, Objects: objects, Catalog: "/StructTreeRoot 1 0 R /MarkInfo << /Marked true >>",
		Pages: []synthPage{{Content: content, Extra: "/StructParents 0"}}}
}

// The order of the tree decides how a page is read where its white space
// leaves the choice open, as the order of drawing does in an untagged
// file; where white space separates blocks, they are read by it.
func TestTaggedOrder(t *testing.T) {
	for _, c := range []struct {
		byColumns bool
		want      string
	}{
		{false, "The title of the page|Top left.|Top right.|Bottom left.|Bottom right."},
		{true, "The title of the page|Top left.|Bottom left.|Top right.|Bottom right."},
	} {
		var got []string
		for _, b := range mustExtract(t, gridDoc(c.byColumns).bytes(), nil).Parts[0].Blocks {
			got = append(got, b.Text)
		}
		if strings.Join(got, "|") != c.want {
			t.Errorf("by columns %v: %s\nwant %s", c.byColumns, strings.Join(got, "|"), c.want)
		}
	}
}
