package gofastpdf

import (
	"sort"
	"strings"

	"github.com/AOShei/go-fast-pdf/internal/file"
	"github.com/AOShei/go-fast-pdf/internal/layout"
	"github.com/AOShei/go-fast-pdf/internal/syntax"

	gofastcore "github.com/AOShei/go-fast-core"
)

// Tables of a tagged PDF.
//
// The structure tree of a tagged PDF can say that content is a table:
// an element Table has rows, TR, directly or in THead, TBody and TFoot,
// and a row has cells, TD and TH. A cell can span rows and columns, by
// the RowSpan and ColSpan of its table attributes (/A with /O /Table).
// The grid of a table is read from the tree once, as an HTML table is
// laid out: each cell takes the first column of its row that no cell
// above it still covers.
//
// The text of each cell, without a display reason, is laid out by itself, and the table is
// one block of kind table with OriginNative, among the other blocks by
// where it is on the page (internal/layout/tables.go). A table over
// several pages is a block on each, with the rows that have content on
// that page and all the columns of the table; the block on a later page
// is Continued. Its header rows are the leading rows that the tree says
// are headers: in THead, or all of TH. A cell that spans rows or
// columns is a merge, cut to the rows of the page; a merge that would
// cover text or another merge is left out, its text staying where it is.
// A table whose grid is larger than maxTableCells, or that the tree
// gives no rows or no cells, is not one: its text is paragraphs, as
// without the tree.

const (
	// maxTableCells bounds the cells of the grid of a table, and
	// maxTableSpan the rows or columns one cell can span.
	maxTableCells = 1 << 18
	maxTableSpan  = 1 << 10
)

// gridCell is where a cell of a table is in its grid.
type gridCell struct {
	row, col, rows, cols int32
}

// tableGrid is the grid of a table as the structure tree gives it.
type tableGrid struct {
	cells      map[int64]gridCell
	rows, cols int32
	// header says of each row whether it is a row of headers.
	header []bool
}

// grid returns the grid of the table with object number n, read once;
// nil when it has no rows or cells, or too many.
func (s *structure) grid(n int64) *tableGrid {
	s.mu.Lock()
	defer s.mu.Unlock()
	if g, ok := s.grids[n]; ok {
		return g
	}
	g := s.readGrid(n)
	if s.grids == nil {
		s.grids = map[int64]*tableGrid{}
	}
	s.grids[n] = g
	return g
}

// readGrid reads the grid of a table from the structure tree.
func (s *structure) readGrid(n int64) *tableGrid {
	f := s.f
	table := f.Object(n)
	if !table.IsDict() {
		return nil
	}
	g := &tableGrid{cells: map[int64]gridCell{}}
	// covered[c] is the first row that the cells above leave column c
	// free from.
	var covered []int32
	addRow := func(row syntax.Value, head bool) bool {
		r := g.rows
		g.rows++
		allTH, any := true, false
		col := int32(0)
		for _, k := range s.kids(row) {
			el := k.el
			t := s.role(f.Resolve(el.Get("S")).NameString())
			if t != "TD" && t != "TH" {
				continue
			}
			for int(col) < len(covered) && covered[col] > r {
				col++
			}
			rows, cols := s.span(el)
			if int64(col+cols)*int64(max(g.rows, r+rows)) > maxTableCells {
				return false
			}
			for len(covered) < int(col+cols) {
				covered = append(covered, 0)
			}
			for c := col; c < col+cols; c++ {
				covered[c] = r + rows
			}
			if num := file.RefNumber(k.ref); num != 0 {
				if _, dup := g.cells[num]; !dup {
					g.cells[num] = gridCell{row: r, col: col, rows: rows, cols: cols}
				}
			}
			any, allTH = true, allTH && t == "TH"
			col += cols
			g.cols = max(g.cols, col)
		}
		g.header = append(g.header, head || any && allTH)
		return int64(g.rows)*int64(max(g.cols, 1)) <= maxTableCells
	}
	for _, k := range s.kids(table) {
		switch part := s.role(f.Resolve(k.el.Get("S")).NameString()); part {
		case "TR":
			if !addRow(k.el, false) {
				return nil
			}
		case "THead", "TBody", "TFoot":
			for _, r := range s.kids(k.el) {
				if s.role(f.Resolve(r.el.Get("S")).NameString()) == "TR" {
					if !addRow(r.el, part == "THead") {
						return nil
					}
				}
			}
		}
	}
	// Rows that cells above reach into are rows of the grid too.
	for _, c := range covered {
		for g.rows < c {
			g.rows++
			g.header = append(g.header, false)
		}
	}
	if g.rows == 0 || g.cols == 0 || len(g.cells) == 0 || int64(g.rows)*int64(g.cols) > maxTableCells {
		return nil
	}
	return g
}

// kid is a kid of a structure element that is an element: the value
// that refers to it, and the element.
type kid struct {
	ref, el syntax.Value
}

// kids returns the kids of a structure element that are elements, not
// content: an element's /K is one item or an array of them.
func (s *structure) kids(el syntax.Value) []kid {
	f := s.f
	k := el.Get("K")
	list := []syntax.Value{k}
	if v := f.Resolve(k); v.Kind == syntax.Array {
		list = v.List
	}
	var out []kid
	for _, item := range list {
		if v := f.Resolve(item); v.IsDict() && !v.Get("S").IsNull() {
			out = append(out, kid{item, v})
		}
	}
	return out
}

// span returns the rows and the columns that a cell spans, by its table
// attributes; 1 where they say nothing.
func (s *structure) span(el syntax.Value) (rows, cols int32) {
	f := s.f
	rows, cols = 1, 1
	a := f.Resolve(el.Get("A"))
	list := []syntax.Value{a}
	if a.Kind == syntax.Array {
		list = a.List // attribute dictionaries, each maybe with a revision number after it
	}
	for _, item := range list {
		d := f.Resolve(item)
		if !d.IsDict() || !f.Resolve(d.Get("O")).IsName("Table") {
			continue
		}
		if n, ok := f.Resolve(d.Get("RowSpan")).Integer(); ok && n > 1 {
			rows = int32(min(n, maxTableSpan))
		}
		if n, ok := f.Resolve(d.Get("ColSpan")).Integer(); ok && n > 1 {
			cols = int32(min(n, maxTableSpan))
		}
	}
	return rows, cols
}

// pageTables is what the layout of a page needs of its tables: the
// cells that its content is in, and the table of each.
type pageTables struct {
	cellOf    map[int64]int32 // a cell's index, by its object number
	cells     []int64         // the object number of each cell
	cellTable []int32         // the table of each cell
	tables    []int64         // the object number of each table
	grids     []*tableGrid
}

// pageTables finds the tables that the content of a page is in; nil for
// none.
func (s *structure) pageTables(tags []tag) *pageTables {
	var pt *pageTables
	var tableOf map[int64]int32 // made at the first cell: most pages have none
	for i := range tags {
		t := &tags[i]
		if t.cell == 0 {
			continue
		}
		if tableOf == nil {
			tableOf = map[int64]int32{}
		}
		if pt != nil {
			if _, ok := pt.cellOf[t.cell]; ok {
				continue
			}
		}
		k, ok := tableOf[t.table]
		if !ok {
			g := s.grid(t.table)
			k = -1
			if g != nil {
				if pt == nil {
					pt = &pageTables{cellOf: map[int64]int32{}}
				}
				k = int32(len(pt.tables))
				pt.tables, pt.grids = append(pt.tables, t.table), append(pt.grids, g)
			}
			tableOf[t.table] = k
		}
		if k < 0 {
			continue
		}
		if _, in := pt.grids[k].cells[t.cell]; !in {
			continue // a cell the grid does not have: its text is paragraphs
		}
		pt.cellOf[t.cell] = int32(len(pt.cells))
		pt.cells, pt.cellTable = append(pt.cells, t.cell), append(pt.cellTable, k)
	}
	if pt == nil {
		return nil
	}
	// The cells in the order of their tables, and of their rows and
	// columns: the lines of a table are given row by row.
	order := make([]int, len(pt.cells))
	for i := range order {
		order[i] = i
	}
	at := func(i int) gridCell { return pt.grids[pt.cellTable[i]].cells[pt.cells[i]] }
	sort.SliceStable(order, func(i, j int) bool {
		a, b := order[i], order[j]
		if pt.cellTable[a] != pt.cellTable[b] {
			return pt.cellTable[a] < pt.cellTable[b]
		}
		ga, gb := at(a), at(b)
		return ga.row < gb.row || ga.row == gb.row && ga.col < gb.col
	})
	cells, cellTable := make([]int64, len(order)), make([]int32, len(order))
	for k, i := range order {
		cells[k], cellTable[k] = pt.cells[i], pt.cellTable[i]
		pt.cellOf[cells[k]] = int32(k)
	}
	pt.cells, pt.cellTable = cells, cellTable
	return pt
}

// options gives the layout the cells of the page's tables.
func (pt *pageTables) options(tags []tag, opts *layout.Options) {
	if pt == nil {
		return
	}
	opts.Cell = func(mcid int32) int32 {
		if mcid >= 0 && int(mcid) < len(tags) && tags[mcid].cell != 0 {
			if k, ok := pt.cellOf[tags[mcid].cell]; ok {
				return k
			}
		}
		return -1
	}
	opts.CellTable, opts.Tables = pt.cellTable, len(pt.tables)
}

// drop leaves out the tables whose cells are not where the grid says:
// a row whose text reaches below the top of a row after it, or columns
// that do not follow each other one way across the page. A structure
// tree can be made badly, by a program that tags what it guessed, and
// its text then reads better as the page sets it. It reports whether it
// left out any: the page is then laid out again, its text as if the
// tree had no tables there.
func (pt *pageTables) drop(lay *layout.Result) bool {
	if pt == nil || len(lay.CellBox) != len(pt.cells) {
		return false
	}
	bad := make([]bool, len(pt.tables))
	any := false
	for t := range pt.tables {
		bad[t] = !pt.agrees(int32(t), lay)
		any = any || bad[t]
	}
	if !any {
		return false
	}
	for c, t := range pt.cellTable {
		if t >= 0 && bad[t] {
			pt.cellTable[c] = -1
		}
	}
	return true
}

// agrees reports whether the cells of table t that have text stand in
// the rows and the columns of its grid: each row below the one before
// it, none reaching into another, and in each row the cells one after
// the other across the page, the same way in all rows. A row that the
// tree gives fewer cells than it shows, so that its cells are a column
// off, still agrees: its text is in the row it is in.
func (pt *pageTables) agrees(t int32, lay *layout.Result) bool {
	g := pt.grids[t]
	type band struct{ lo, hi, size float64 }
	rows := map[int32]*band{}
	type placed struct {
		row, col int32
		x0, x1   float64
		size     float64
	}
	var cells []placed
	for c, tc := range pt.cellTable {
		if tc != t || lay.CellText[c] == "" {
			continue
		}
		at, r, size := g.cells[pt.cells[c]], lay.CellBox[c], lay.CellSize[c]
		if at.rows == 1 {
			if b := rows[at.row]; b == nil {
				rows[at.row] = &band{r.Y0, r.Y1, size}
			} else {
				b.lo, b.hi, b.size = min(b.lo, r.Y0), max(b.hi, r.Y1), max(b.size, size)
			}
		}
		cells = append(cells, placed{at.row, at.col, r.X0, r.X1, size})
	}
	// Rows down the page, none reaching into the next.
	keys := make([]int32, 0, len(rows))
	for k := range rows {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	for i := 1; i < len(keys); i++ {
		a, b := rows[keys[i-1]], rows[keys[i]]
		if a.hi-b.lo > 0.5*max(a.size, b.size) {
			return false
		}
	}
	// In each row, the cells across the page one way.
	sort.Slice(cells, func(i, j int) bool {
		return cells[i].row < cells[j].row || cells[i].row == cells[j].row && cells[i].col < cells[j].col
	})
	way := 0 // 1 left to right, -1 right to left
	for i := 1; i < len(cells); i++ {
		a, b := &cells[i-1], &cells[i]
		if a.row != b.row {
			continue
		}
		// By their middles: the box of a cell's text can take in the
		// spaces after it, and reach over the next cell's edge.
		step, ma, mb := 0, (a.x0+a.x1)/2, (b.x0+b.x1)/2
		switch tol := 0.5 * max(a.size, b.size); {
		case mb-ma > tol && b.x1 > a.x1:
			step = 1
		case ma-mb > tol && b.x0 < a.x0:
			step = -1
		default:
			return false // one over the other
		}
		if way != 0 && step != way {
			return false
		}
		way = step
	}
	return true
}

// fill makes the blocks of kind table that the layout made into tables:
// the rows of each that have content on the page, and the columns of
// the whole table.
func (pt *pageTables) fill(blocks []gofastcore.Block, lay *layout.Result) {
	if pt == nil {
		return
	}
	j := 0
	for k := range blocks {
		b := &blocks[k]
		if b.Kind != gofastcore.KindTable || b.Table != nil || j >= len(lay.Tables) {
			continue // a table found by alignment has its grid
		}
		t := lay.Tables[j]
		j++
		if !pt.table(b, t, lay.CellText) {
			// No grid to give: the text of its cells, row by row, as a
			// paragraph.
			var rows []string
			for c, tc := range pt.cellTable {
				if tc == t && lay.CellText[c] != "" {
					rows = append(rows, lay.CellText[c])
				}
			}
			b.Kind, b.Text, b.Table = gofastcore.KindParagraph, strings.Join(rows, "\n"), nil
		}
	}
}

// table makes block b the table t of the page from the text of its
// cells. It reports false when there is no grid to give.
func (pt *pageTables) table(b *gofastcore.Block, t int32, text []string) bool {
	g := pt.grids[t]
	lo, hi := g.rows, int32(-1)
	for c, tc := range pt.cellTable {
		if tc != t {
			continue
		}
		at := g.cells[pt.cells[c]]
		lo, hi = min(lo, at.row), max(hi, at.row+at.rows-1)
	}
	hi = min(hi, g.rows-1)
	if hi < lo || int64(hi-lo+1)*int64(g.cols) > maxTableCells {
		return false
	}
	rows := make([][]string, hi-lo+1)
	for r := range rows {
		rows[r] = make([]string, g.cols)
	}
	type span struct{ row, col, rows, cols int32 }
	var spans []span
	for c, tc := range pt.cellTable {
		if tc != t || text[c] == "" {
			continue
		}
		at := g.cells[pt.cells[c]]
		r, col := at.row-lo, at.col
		if cell := &rows[r][col]; *cell == "" {
			*cell = text[c]
		} else {
			*cell += "\n" + text[c]
		}
		if at.rows > 1 || at.cols > 1 {
			spans = append(spans, span{r, col, min(at.rows, hi-at.row+1), min(at.cols, g.cols-col)})
		}
	}
	// Merges, where they cover no text and no other merge.
	var merges []gofastcore.Merge
	taken := map[[2]int32]bool{}
	for _, m := range spans {
		if m.rows*m.cols <= 1 {
			continue
		}
		free := true
		for r := m.row; r < m.row+m.rows && free; r++ {
			for c := m.col; c < m.col+m.cols && free; c++ {
				free = !taken[[2]int32{r, c}] && (r == m.row && c == m.col || rows[r][c] == "")
			}
		}
		if !free {
			continue
		}
		for r := m.row; r < m.row+m.rows; r++ {
			for c := m.col; c < m.col+m.cols; c++ {
				taken[[2]int32{r, c}] = true
			}
		}
		merges = append(merges, gofastcore.Merge{Row: int(m.row), Col: int(m.col), RowSpan: int(m.rows), ColSpan: int(m.cols)})
	}
	head := 0
	for r := lo; r <= hi && g.header[r]; r++ {
		head++
	}
	// A table whose first rows are on a page before this one goes on
	// from there; rows of headers before it are what a page repeats.
	continued := false
	for r := int32(0); r < lo; r++ {
		continued = continued || !g.header[r]
	}
	b.Kind, b.Text, b.Origin, b.Continued = gofastcore.KindTable, "", gofastcore.OriginNative, continued
	b.Table = &gofastcore.Table{HeaderRows: head, Rows: rows, Merges: merges}
	return true
}
