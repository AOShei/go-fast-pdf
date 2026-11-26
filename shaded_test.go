package gofastpdf_test

import (
	"fmt"
	"strings"
	"testing"

	gofastcore "github.com/AOShei/go-fast-core"
)

// shadedRow fills the ground of each cell of a row, between ys[r] and
// ys[r+1], one box a cell.
func shadedRow(xs, ys []int, r int) string {
	var sb strings.Builder
	sb.WriteString("0.9 g\n")
	for c := 0; c+1 < len(xs); c++ {
		fmt.Fprintf(&sb, "%d %d %d %d re f\n", xs[c], ys[r+1], xs[c+1]-xs[c], ys[r]-ys[r+1])
	}
	sb.WriteString("0 g\n")
	return sb.String()
}

// TestShadedTable: a table with no rules, the cells of every other row
// drawn as boxes filled with a shade. The edges of the boxes are the
// lines of the table: a cell of two lines is one cell, and a row that is
// not shaded stands between two that are. (A last row that is not shaded
// has no edge under it, and is not known to be the table's.)
func TestShadedTable(t *testing.T) {
	xs, ys := []int{72, 200, 300, 400}, []int{700, 680, 648, 628, 608, 588}
	rows := []row{
		{false, []string{"Station", "Depth", "Count"}},
		{false, []string{"North Bay", "12.5", "140"}},
		{false, []string{"South Cove", "8.0", "97"}},
		{false, []string{"East Point", "3.5", "12"}},
		{false, []string{"West Reef", "21.0", "8"}},
	}
	content := shadedRow(xs, ys, 0) + shadedRow(xs, ys, 2) + shadedRow(xs, ys, 4) +
		ruledCells(xs, ys, rows) + set("F1", 10, xs[0]+4, ys[1]-24, "upper reach")
	doc := mustExtract(t, ruledDoc(content).bytes(), nil)
	got := tablesOf(doc.Parts[0])
	for _, want := range []string{"| Station | Depth | Count |", "| North Bay upper reach | 12.5 | 140 |", "| South Cove | 8.0 | 97 |", "| East Point | 3.5 | 12 |", "| West Reef | 21.0 | 8 |"} {
		if !strings.Contains(got, want) {
			t.Errorf("no row %q in:\n%s", want, got)
		}
	}
}

// TestShadedNotTables: boxes that are filled and are not the cells of a
// row make no table.
func TestShadedNotTables(t *testing.T) {
	prose := "The committee met on the first day of the month to discuss the budget for the coming year and its plans for the new building. Members asked about the costs of the work and the schedule for its completion."
	var strips strings.Builder
	strips.WriteString("0.9 g\n")
	for i := 0; i < 6; i++ {
		// One box a line of a shaded paragraph, each under the last.
		fmt.Fprintf(&strips, "72 %d 400 12 re f\n", 688-12*i)
	}
	strips.WriteString("0 g\n")
	cases := map[string]string{
		"a panel behind a paragraph":      "0.9 g\n72 600 400 100 re f\n0 g\n" + proseIn(76, 690, prose),
		"the lines of a shaded paragraph": strips.String() + proseIn(76, 690, prose),
		// Bars of a chart: side by side, each of its own height.
		"the bars of a chart": "0.5 g\n100 600 40 30 re f\n140 600 40 55 re f\n180 600 40 80 re f\n0 g\n" +
			set("F1", 10, 104, 588, "2003") + set("F1", 10, 144, 588, "2004") + set("F1", 10, 184, 588, "2005"),
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
