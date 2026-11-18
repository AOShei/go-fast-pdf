package gofastpdf_test

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// TestLayoutCost: no arrangement of text on a page makes the layout
// take time out of proportion to the size of the page. Each of these
// pages has some hundred thousand pieces of text, arranged so as to
// meet a step of the layout at its worst.
func TestLayoutCost(t *testing.T) {
	if testing.Short() {
		t.Skip("large pages")
	}
	const n = 120000
	pages := map[string]func(sb *strings.Builder){
		"everything at one point": func(sb *strings.Builder) {
			for i := 0; i < n; i++ {
				fmt.Fprintf(sb, "1 0 0 1 100 500 Tm (w%d) Tj\n", i%977)
			}
		},
		"the same word at one point": func(sb *strings.Builder) {
			for i := 0; i < n; i++ {
				sb.WriteString("1 0 0 1 100 500 Tm (same) Tj\n")
			}
		},
		"single letters along one line": func(sb *strings.Builder) {
			for i := 0; i < n; i++ {
				fmt.Fprintf(sb, "1 0 0 1 %d 500 Tm (a) Tj\n", i*9)
			}
		},
		"a grid of single words": func(sb *strings.Builder) {
			for i := 0; i < n; i++ {
				fmt.Fprintf(sb, "1 0 0 1 %d %d Tm (cell) Tj\n", 20+(i%300)*40, 20+(i/300)*14)
			}
		},
		"words scattered": func(sb *strings.Builder) {
			x, y := uint32(1), uint32(7)
			for i := 0; i < n; i++ {
				x, y = x*1664525+1013904223, y*22695477+1
				fmt.Fprintf(sb, "1 0 0 1 %d %d Tm (w%d) Tj\n", x%20000, y%20000, i%977)
			}
		},
		"a staircase of blocks": func(sb *strings.Builder) {
			// Each block lower and further right than the last: every
			// cut takes off one block.
			for i := 0; i < 6000; i++ {
				fmt.Fprintf(sb, "1 0 0 1 %d %d Tm (step) Tj\n", 10+i*30, 200000-i*30)
			}
		},
		"lines one under the other": func(sb *strings.Builder) {
			for i := 0; i < n; i++ {
				fmt.Fprintf(sb, "1 0 0 1 72 %d Tm (line %d of a long column) Tj\n", 2000000-i*12, i)
			}
		},
		"every piece turned a little": func(sb *strings.Builder) {
			for i := 0; i < n; i++ {
				fmt.Fprintf(sb, "1 0.0%d -0.0%d 1 %d %d Tm (turn) Tj\n", i%90, i%90, 72+(i%50)*40, 72+(i/50)*3)
			}
		},
		"hidden and visible by turns": func(sb *strings.Builder) {
			for i := 0; i < n; i++ {
				fmt.Fprintf(sb, "%d Tr 1 0 0 1 %d %d Tm (word) Tj\n", (i%2)*3, 72+(i%100)*30, 72+(i/100)*12)
			}
		},
	}
	for name, fill := range pages {
		t.Run(name, func(t *testing.T) {
			var sb strings.Builder
			sb.WriteString("BT /F1 10 Tf\n")
			fill(&sb)
			sb.WriteString("ET")
			data := synthDoc{Font: fontCourier, Pages: []synthPage{{Content: sb.String(), MediaBox: "[0 0 20000 2000000]"}}}.bytes()
			start := time.Now()
			doc := mustExtract(t, data, nil)
			took := time.Since(start)
			words := len(strings.Fields(doc.Text()))
			t.Logf("%d blocks, %d words, %v", len(doc.Parts[0].Blocks), words, took.Round(time.Millisecond))
			if words == 0 {
				t.Error("no text")
			}
			// The contract check takes part of this; the bound is loose
			// on purpose and still far below what a quadratic step needs.
			if took > 20*time.Second {
				t.Errorf("took %v", took)
			}
		})
	}
}
