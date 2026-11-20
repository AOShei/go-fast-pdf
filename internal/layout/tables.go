package layout

import (
	"strings"

	gofastcore "github.com/AOShei/go-fast-core"
)

// Tables of a tagged PDF.
//
// The structure tree of a tagged PDF can say which content is in which
// cell of a table (Options.Cell). The text of each cell is laid out by
// itself, as a little page, and each table is one block of kind table,
// as large as its cells together, put among the other blocks by where it
// is. What the cells say is in Result.CellText; the grid, which the
// structure tree gives, is made of it by the caller. Text in a cell that
// has a display reason is not the table's: it is laid out with the other
// text that has its reasons, as anywhere else, so that a block of a
// table has none.

// tables makes the blocks of the tables from b.blocks[from:to], the
// blocks of the cells, which are in order of cell. It returns the text,
// the box and the size of the text of each cell, and the new blocks, one
// for each table with text.
func (b *builder) tables(from, to int) (text []string, boxes []gofastcore.Rect, sizes []float64, set []int32) {
	text = make([]string, len(b.opts.CellTable))
	boxes = make([]gofastcore.Rect, len(b.opts.CellTable))
	sizes = make([]float64, len(b.opts.CellTable))
	at := make([]int32, b.opts.Tables) // one more than the block of each table
	var sb strings.Builder
	for lo := from; lo < to; {
		hi := lo + 1
		cell := b.blocks[lo].cell
		for hi < to && b.blocks[hi].cell == cell {
			hi++
		}
		// The paragraphs of the cell, in reading order, one to a line.
		group := make([]int32, hi-lo)
		for i := range group {
			group[i] = int32(lo + i)
		}
		if len(group) > 1 {
			b.readingOrder(group)
		}
		sb.Reset()
		for i, bi := range group {
			if i > 0 {
				sb.WriteByte('\n')
			}
			sb.WriteString(b.blocks[bi].text)
		}
		text[cell-1] = sb.String()
		first := &b.blocks[group[0]]
		x0, y0, x1, y1, size := first.x0, first.y0, first.x1, first.y1, first.size
		for _, bi := range group {
			bl := &b.blocks[bi]
			x0, y0, x1, y1, size = min(x0, bl.x0), min(y0, bl.y0), max(x1, bl.x1), max(y1, bl.y1), max(size, bl.size)
		}
		boxes[cell-1], sizes[cell-1] = rect(x0, y0, x1, y1), size

		t := b.opts.CellTable[cell-1]
		if at[t] == 0 {
			first := &b.blocks[lo]
			b.blocks = append(b.blocks, blk{table: t + 1, line: -1, key: first.key, size: first.size,
				x0: first.x0, y0: first.y0, x1: first.x1, y1: first.y1})
			at[t] = int32(len(b.blocks))
			set = append(set, at[t]-1)
		}
		tb := &b.blocks[at[t]-1]
		for _, bi := range group {
			bl := &b.blocks[bi]
			tb.x0, tb.y0, tb.x1, tb.y1 = min(tb.x0, bl.x0), min(tb.y0, bl.y0), max(tb.x1, bl.x1), max(tb.y1, bl.y1)
			tb.key, tb.size = min(tb.key, bl.key), max(tb.size, bl.size)
		}
		tb.members = append(tb.members, group...)
		lo = hi
	}
	return text, boxes, sizes, set
}
