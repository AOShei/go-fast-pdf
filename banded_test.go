package gofastpdf_test

import (
	"fmt"
	"strings"
	"testing"
)

// levelRules draws a rule from x0 to x1 at each y.
func levelRules(x0, x1 int, ys ...int) string {
	var sb strings.Builder
	for _, y := range ys {
		fmt.Fprintf(&sb, "%d %d %d 0.5 re f\n", x0, y, x1-x0)
	}
	return sb.String()
}

// A table with rules between its rows and none between its columns: a
// row is what stands between two rules, and a cell of two lines is one
// cell. The rules make no grid, and the lines of the cell run on as a
// sentence does, which is no table by alignment alone.
func TestTableBetweenLevelRules(t *testing.T) {
	content := para(760, "Text before the table") +
		levelRules(72, 420, 712, 694, 664, 646, 616) +
		set("F1", 10, 76, 700, "Name") + set("F1", 10, 200, 700, "What it is for") +
		set("F1", 10, 76, 682, "Alpha") + set("F1", 10, 200, 682, "the first line of a note") +
		set("F1", 10, 200, 670, "that goes on to a second") +
		set("F1", 10, 76, 652, "Beta") + set("F1", 10, 200, 652, "a short note") +
		set("F1", 10, 76, 634, "Gamma") + set("F1", 10, 200, 634, "a note of three lines,") +
		set("F1", 10, 200, 622, "of which this is the last") +
		para(580, "Text after the table")
	got := tableBlocks(t, synthDoc{Font: fontCourier, Pages: []synthPage{{Content: content}}})
	for _, want := range []string{
		"Name | What it is for",
		"Alpha | the first line of a note that goes on to a second",
		"Beta | a short note",
		"Gamma | a note of three lines, of which this is the last",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("%q is not in:\n%s", want, got)
		}
	}
	if strings.Count(got, "table inferred") != 1 || !strings.Contains(got, "Text after the table") {
		t.Errorf("one table and the text after it:\n%s", got)
	}
}

// A table with a rule over it, one under its header and one at its end:
// the rules bound it, and the rows between two of them are rows of text.
// It stands beside a column of prose, which is none of it.
func TestTableBetweenThreeRules(t *testing.T) {
	var sb strings.Builder
	sb.WriteString(levelRules(72, 300, 712, 694, 636))
	rows := [][3]string{{"Station", "Depth", "Count"}, {"North", "12.5", "140"}, {"South", "8.0", "97"}, {"East", "15.2", "211"}, {"West", "4.4", "36"}}
	for i, r := range rows {
		y := 700 - 14*i
		if i > 0 {
			y -= 4
		}
		sb.WriteString(set("F1", 10, 76, y, r[0]) + set("F1", 10, 160, y, r[1]) + set("F1", 10, 240, y, r[2]))
	}
	// The column beside it, its lines on other baselines.
	for i := 0; i < 8; i++ {
		sb.WriteString(set("F1", 10, 330, 705-12*i, "prose that runs down the page beside"))
	}
	got := tableBlocks(t, synthDoc{Font: fontCourier, Pages: []synthPage{{Content: sb.String()}}})
	for _, want := range []string{"Station | Depth | Count", "North | 12.5 | 140", "West | 4.4 | 36"} {
		if !strings.Contains(got, want) {
			t.Fatalf("%q is not in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "| prose") || strings.Contains(got, "36 | prose") {
		t.Errorf("the prose beside the table is in it:\n%s", got)
	}
}
