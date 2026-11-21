package gofastpdf_test

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	gofastcore "github.com/AOShei/go-fast-core"
	gofastpdf "github.com/AOShei/go-fast-pdf"
)

// ruledGrid draws the rules of a grid: a level rule at each of ys from
// xs[0] to the last of xs, and an upright one at each of xs from ys[0]
// to the last of ys, stroked half a point wide; skip leaves out the
// pieces of rules it names, as "v1r0" for the upright rule xs[1] beside
// row 0, or "h2c1" for the level rule ys[2] over column 1.
func ruledGrid(xs, ys []int, skip ...string) string {
	var sb strings.Builder
	sb.WriteString("0.5 w\n")
	skipped := map[string]bool{}
	for _, s := range skip {
		skipped[s] = true
	}
	for i, y := range ys {
		for c := 0; c+1 < len(xs); c++ {
			if !skipped[fmt.Sprintf("h%dc%d", i, c)] {
				fmt.Fprintf(&sb, "%d %d m %d %d l S\n", xs[c], y, xs[c+1], y)
			}
		}
	}
	for i, x := range xs {
		for r := 0; r+1 < len(ys); r++ {
			if !skipped[fmt.Sprintf("v%dr%d", i, r)] {
				fmt.Fprintf(&sb, "%d %d m %d %d l S\n", x, ys[r], x, ys[r+1])
			}
		}
	}
	return sb.String()
}

// ruledCells sets the text of cells, each 4 points in from the left of
// its cell and 12 under its top, in /F2 (bold) where a row says so.
func ruledCells(xs, ys []int, rows []row) string {
	var sb strings.Builder
	for r, rw := range rows {
		font := "F1"
		if rw.bold {
			font = "F2"
		}
		for c, text := range rw.cells {
			if text != "" {
				sb.WriteString(set(font, 10, xs[c]+4, ys[r]-12, text))
			}
		}
	}
	return sb.String()
}

func ruledDoc(content string) synthDoc {
	return synthDoc{Font: fontCourier, Pages: []synthPage{{Content: content, Fonts: boldFont}}}
}

func TestRuledTable(t *testing.T) {
	xs, ys := []int{72, 200, 300, 400}, []int{700, 680, 660, 640}
	rows := []row{
		{true, []string{"Station", "Depth", "Count"}},
		{false, []string{"North Bay", "12.5", "140"}},
		{false, []string{"South Cove", "8.0", "97"}},
	}
	t.Run("a grid with a header", func(t *testing.T) {
		got := tableBlocks(t, ruledDoc(ruledGrid(xs, ys)+ruledCells(xs, ys, rows)))
		want := `table inferred continued=false header=1 merges=[]
  | Station | Depth | Count |
  | North Bay | 12.5 | 140 |
  | South Cove | 8.0 | 97 |
`
		if got != want {
			t.Errorf("got:\n%s\nwant:\n%s", got, want)
		}
	})

	t.Run("a rule missing inside the grid is a merge", func(t *testing.T) {
		// No rule between the last two columns in the first row, and
		// none between the first two rows in the first column.
		rows := []row{
			{true, []string{"Station", "Depth and count", ""}},
			{false, []string{"", "12.5", "140"}},
			{false, []string{"South Cove", "8.0", "97"}},
		}
		got := tableBlocks(t, ruledDoc(ruledGrid(xs, ys, "v2r0", "h1c0")+ruledCells(xs, ys, rows)))
		want := `table inferred continued=false header=1 merges=[{0 0 2 1} {0 1 1 2}]
  | Station | Depth and count |  |
  |  | 12.5 | 140 |
  | South Cove | 8.0 | 97 |
`
		if got != want {
			t.Errorf("got:\n%s\nwant:\n%s", got, want)
		}
	})

	t.Run("a cell of two lines", func(t *testing.T) {
		ys := []int{700, 680, 650, 630}
		content := ruledGrid(xs, ys) + ruledCells(xs, ys, rows[:1]) +
			set("F1", 10, 76, 668, "North") + set("F1", 10, 76, 656, "Bay") +
			set("F1", 10, 204, 668, "12.5") + set("F1", 10, 304, 668, "140") +
			ruledCells(xs, ys, []row{{}, {}, rows[2]})
		got := tableBlocks(t, ruledDoc(content))
		if !strings.Contains(got, "| North Bay | 12.5 | 140 |") || strings.Count(got, "\n  |") != 3 {
			t.Errorf("got:\n%s", got)
		}
	})

	t.Run("rules between the columns only", func(t *testing.T) {
		// A rule over and under the header and under the table, and
		// rules between the columns: the body is one row of the grid,
		// and a row of the table on each of its lines.
		ys := []int{700, 680, 620}
		content := ruledGrid(xs, ys) + ruledCells(xs, ys, rows[:1])
		for i, r := range [][]string{{"North Bay", "12.5", "140"}, {"South Cove", "8.0", "97"}, {"East Reef", "15.2", "211"}, {"West Point", "4.4", "36"}} {
			for c, text := range r {
				content += set("F1", 10, xs[c]+4, 668-12*i, text)
			}
		}
		got := tableBlocks(t, ruledDoc(content))
		want := `table inferred continued=false header=1 merges=[]
  | Station | Depth | Count |
  | North Bay | 12.5 | 140 |
  | South Cove | 8.0 | 97 |
  | East Reef | 15.2 | 211 |
  | West Point | 4.4 | 36 |
`
		if got != want {
			t.Errorf("got:\n%s\nwant:\n%s", got, want)
		}
	})

	t.Run("one string across a column rule", func(t *testing.T) {
		// The entries of the last two columns are one run: strings that
		// stand less than a font size apart, the room between them across
		// the rule at 300. One goes on after a gap, the other after a
		// space it ends with and a kern.
		content := ruledGrid(xs, ys) + ruledCells(xs, ys, rows[:1]) +
			set("F1", 10, 76, 668, "North Bay") + "BT /F1 10 Tf 272 668 Td [(12.5) -800 (140)] TJ ET\n" +
			set("F1", 10, 76, 648, "South Cove") + "BT /F1 10 Tf 276 648 Td [(8.0 ) -400 (97)] TJ ET\n"
		got := tableBlocks(t, ruledDoc(content))
		want := `table inferred continued=false header=1 merges=[]
  | Station | Depth | Count |
  | North Bay | 12.5 | 140 |
  | South Cove | 8.0 | 97 |
`
		if got != want {
			t.Errorf("got:\n%s\nwant:\n%s", got, want)
		}
	})

	t.Run("rows that begin at each code", func(t *testing.T) {
		// Rules between the columns only, and entries of one line or
		// two: a row begins at each line of the first column, and the
		// unit stands on the last line of its entry.
		xs, ys := []int{72, 140, 330, 430}, []int{700, 680, 620}
		content := ruledGrid(xs, ys) + ruledCells(xs, ys, []row{{true, []string{"Code", "Description", "Unit"}}}) +
			set("F1", 10, 76, 668, "3241") + set("F1", 10, 144, 668, "All petroleum products") +
			set("F1", 10, 76, 656, "32411") + set("F1", 10, 144, 656, "Refined products, made") +
			set("F1", 10, 144, 646, "in a refinery") + set("F1", 10, 334, 646, "1,000 bbl") +
			set("F1", 10, 76, 634, "32412") + set("F1", 10, 144, 634, "Asphalt paving") + set("F1", 10, 334, 634, "short tons")
		got := tableBlocks(t, ruledDoc(content))
		want := `table inferred continued=false header=1 merges=[]
  | Code | Description | Unit |
  | 3241 | All petroleum products |  |
  | 32411 | Refined products, made in a refinery | 1,000 bbl |
  | 32412 | Asphalt paving | short tons |
`
		if got != want {
			t.Errorf("got:\n%s\nwant:\n%s", got, want)
		}
	})

	t.Run("a name of two lines is one row", func(t *testing.T) {
		// The first column has two lines, but the last goes on from its
		// first line, which fills the cell, to its second: one row.
		xs, ys := []int{72, 180, 300, 500}, []int{700, 680, 640}
		content := ruledGrid(xs, ys) + ruledCells(xs, ys, []row{{true, []string{"Product", "Maker", "Contents"}}}) +
			set("F1", 10, 76, 668, "Natures Nutrition") + set("F1", 10, 76, 656, "Formula One") +
			set("F1", 10, 184, 668, "Affiliated") + set("F1", 10, 184, 656, "International") +
			set("F1", 10, 304, 668, "Ma-huang, cola nut, hawthorn") + set("F1", 10, 304, 656, "berry, saw palmetto")
		got := tableBlocks(t, ruledDoc(content))
		want := `table inferred continued=false header=1 merges=[]
  | Product | Maker | Contents |
  | Natures Nutrition Formula One | Affiliated International | Ma-huang, cola nut, hawthorn berry, saw palmetto |
`
		if got != want {
			t.Errorf("got:\n%s\nwant:\n%s", got, want)
		}
	})

	t.Run("bars filled for rules", func(t *testing.T) {
		var sb strings.Builder
		for _, y := range ys {
			fmt.Fprintf(&sb, "72 %d 328 0.6 re f\n", y)
		}
		for _, x := range xs {
			fmt.Fprintf(&sb, "%d 640 0.6 60 re f\n", x)
		}
		got := tableBlocks(t, ruledDoc(sb.String()+ruledCells(xs, ys, rows)))
		if !strings.Contains(got, "table inferred continued=false header=1") || !strings.Contains(got, "| South Cove | 8.0 | 97 |") {
			t.Errorf("got:\n%s", got)
		}
	})

	t.Run("cells drawn each as a rectangle", func(t *testing.T) {
		var sb strings.Builder
		sb.WriteString("0.5 w\n")
		for r := 0; r+1 < len(ys); r++ {
			for c := 0; c+1 < len(xs); c++ {
				fmt.Fprintf(&sb, "%d %d %d %d re S\n", xs[c], ys[r+1], xs[c+1]-xs[c], ys[r]-ys[r+1])
			}
		}
		got := tableBlocks(t, ruledDoc(sb.String()+ruledCells(xs, ys, rows)))
		if !strings.Contains(got, "| North Bay | 12.5 | 140 |") {
			t.Errorf("got:\n%s", got)
		}
	})

	t.Run("a line across the edge stays out of the table", func(t *testing.T) {
		rows := []row{
			{false, []string{"Station", "Depth", "Count"}},
			{false, []string{"North Bay", "12.5", "140"}},
			{false, []string{"South Cove", "8.0", "97"}},
			{false, []string{"East Reef", "15.2", "211"}},
		}
		ys := []int{700, 680, 660, 640, 620}
		content := ruledGrid(xs, ys) + ruledCells(xs, ys, rows) + set("F1", 10, 60, 625, "7 S")
		got := tableBlocks(t, ruledDoc(content))
		if !strings.Contains(got, "| East Reef | 15.2 | 211 |") || !strings.Contains(got, `paragraph "7 S"`) {
			t.Errorf("got:\n%s", got)
		}
	})

	t.Run("text drawn with no ink stays out of the cells", func(t *testing.T) {
		content := ruledGrid(xs, ys) + ruledCells(xs, ys, rows[:2]) +
			set("F1", 10, 76, 648, "South Cove") + set("F1", 10, 204, 648, "8.0") +
			"BT 3 Tr /F1 10 Tf 304 648 Td (97) Tj 0 Tr ET\n"
		got := tableBlocks(t, ruledDoc(content))
		if !strings.Contains(got, "| South Cove | 8.0 |  |") || !strings.Contains(got, `paragraph no_ink "97"`) {
			t.Errorf("got:\n%s", got)
		}
	})
}

// A ruled table that is also lined up is found by its rules: one table,
// with the merge only the rules show.
func TestRuledBeforeAligned(t *testing.T) {
	xs, ys := []int{72, 200, 300, 400}, []int{700, 680, 660, 640, 620}
	rows := []row{
		{false, []string{"Station", "Depth", "Count"}},
		{false, []string{"North Bay", "12.5", "140"}},
		{false, []string{"", "8.0", "97"}},
		{false, []string{"East Reef", "15.2", "211"}},
	}
	got := tableBlocks(t, ruledDoc(ruledGrid(xs, ys, "h2c0")+ruledCells(xs, ys, rows)))
	if strings.Count(got, "table inferred") != 1 || !strings.Contains(got, "merges=[{1 0 2 1}]") {
		t.Errorf("got:\n%s", got)
	}
}

func TestRuledNotTables(t *testing.T) {
	xs, ys := []int{72, 200, 300, 400}, []int{700, 680, 660, 640}
	rows := []row{
		{false, []string{"Station", "Depth", "Count"}},
		{false, []string{"North Bay", "12.5", "140"}},
		{false, []string{"South Cove", "8.0", "97"}},
	}
	prose := "The committee met on the first day of the month to discuss the budget for the coming year and its plans for the new building. Members asked about the costs of the work and the schedule for its completion, and the chair said that a report would follow in the spring."
	cases := map[string]string{
		// Boxes around paragraphs: the cells are prose.
		"boxes around prose": ruledGrid([]int{72, 300, 540}, []int{700, 620, 540}) +
			proseIn(76, 690, prose) + proseIn(304, 690, prose) + proseIn(76, 610, prose) + proseIn(304, 610, prose),
		// The grid of a chart, its labels outside it.
		"the grid of a chart": ruledGrid(xs, ys) + set("F1", 10, 72, 625, "0") + set("F1", 10, 200, 625, "10") +
			set("F1", 10, 300, 625, "20") + set("F1", 10, 400, 625, "30") + set("F1", 10, 40, 700, "100") + set("F1", 10, 40, 660, "50"),
		// Lines that run across the edge of the grid: the rules frame
		// something else.
		"lines across the edge": ruledGrid(xs, ys) + ruledCells(xs, ys, rows) +
			set("F1", 10, 150, 685, "a note set across the edge of the grid and beyond it") +
			set("F1", 10, 150, 665, "a note set across the edge of the grid and well beyond it"),
		// A form of boxes, most of them empty: four labels in twenty
		// boxes.
		"a form of empty boxes": ruledGrid([]int{72, 150, 230, 310, 390, 470}, []int{700, 680, 660, 640, 620}) +
			set("F1", 10, 76, 688, "Name") + set("F1", 10, 154, 688, "Date") +
			set("F1", 10, 76, 668, "Address") + set("F1", 10, 154, 668, "Phone"),
		// One column: a box with rows.
		"one column": ruledGrid([]int{72, 400}, ys) + ruledCells([]int{72, 400}, ys, []row{{false, []string{"One"}}, {false, []string{"Two"}}, {false, []string{"Three"}}}),
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			doc := mustExtract(t, ruledDoc(content).bytes(), nil)
			for _, b := range doc.Parts[0].Blocks {
				if b.Kind == gofastcore.KindTable {
					t.Errorf("a table:\n%s", tablesOf(doc.Parts[0]))
					break
				}
			}
		})
	}
}

// A run cut by a rule of a grid that is no table reads as it did: the
// letters of a label set apart, a line of the grid between two of them.
func TestRuledCutKeepsText(t *testing.T) {
	xs, ys := []int{72, 200, 300, 400, 450, 500}, []int{700, 680, 660, 640, 620, 600}
	content := ruledGrid(xs, ys) + set("F1", 10, 72, 625, "0") + set("F1", 10, 200, 625, "10") +
		"BT /F1 10 Tf 170 690 Td [(C) -400 (o) -400 (u) -400 (n) -400 (t) -400 (y)] TJ ET\n" +
		"BT /F1 10 Tf 230 670 Td [(Harbour) -600 (and) -600 (coast)] TJ ET\n"
	data := ruledDoc(content).bytes()
	var texts [2]string
	for i, off := range []bool{false, true} {
		e := &gofastpdf.Extractor{NoRuledTables: off}
		doc, err := e.Extract(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, b := range doc.Parts[0].Blocks {
			if b.Kind == gofastcore.KindTable {
				t.Fatalf("a table:\n%s", tablesOf(doc.Parts[0]))
			}
			texts[i] += b.Text + "\n"
		}
	}
	if texts[0] != texts[1] || !strings.Contains(texts[0], "Harbour and coast") {
		t.Errorf("with rules:\n%s\nwithout:\n%s", texts[0], texts[1])
	}
}

// proseIn sets text as lines of 40 characters from x, y down.
func proseIn(x, y int, text string) string {
	var sb strings.Builder
	for len(text) > 0 {
		n := min(40, len(text))
		sb.WriteString(set("F1", 8, x, y, text[:n]))
		text, y = text[n:], y-10
	}
	return sb.String()
}

// Extractor.NoRuledTables leaves ruled tables out: two rows with rules
// are a table, and without the rules too few for one by alignment.
func TestNoRuledTables(t *testing.T) {
	xs, ys := []int{72, 200, 300, 400}, []int{700, 680, 660}
	rows := []row{
		{false, []string{"Station", "Depth", "Count"}},
		{false, []string{"North Bay", "12.5", "140"}},
	}
	data := ruledDoc(ruledGrid(xs, ys) + ruledCells(xs, ys, rows)).bytes()
	for _, off := range []bool{false, true} {
		e := &gofastpdf.Extractor{NoRuledTables: off}
		doc, err := e.Extract(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
		if err != nil {
			t.Fatal(err)
		}
		tables := 0
		for _, b := range doc.Parts[0].Blocks {
			if b.Kind == gofastcore.KindTable {
				tables++
			}
		}
		if want := btoi(!off); tables != want {
			t.Errorf("NoRuledTables %v: %d tables\n%s", off, tables, tablesOf(doc.Parts[0]))
		}
	}
}
