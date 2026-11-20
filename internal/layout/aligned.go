package layout

import (
	"cmp"
	"math"
	"slices"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	gofastcore "github.com/AOShei/go-fast-core"
)

// Tables found by how their text lines up.
//
// A table without tags and without rules is text set in columns: in
// each row, its cells stand apart, and the white space between them is
// at the same place in every row, a gutter running down the table. That
// is what is looked for, in the lines of one group as they are before
// they are joined into paragraphs.
//
// The lines are put in rows, those on one baseline together, and each
// row in segments: stretches of text with no space between them as wide
// as segGap ems. A region is a run of rows one under the other, close
// together, set at about one size; its gutters are the stretches across
// it, at least gutterMin ems wide, that no segment of its rows covers,
// or only a few of them: a row that crosses a gutter is a cell over
// several columns, as a heading of a group of rows is. A region grows a
// row at a time and ends where a row is too far from the one before, or
// where rows cover the gutters, as the lines of a paragraph after the
// table do.
//
// A region is a table when it looks like one and nothing else:
//
//   - two columns or more, and at least tableMinRows rows;
//   - most rows have text in two columns or more;
//   - the cells are short: a column of prose beside another, as on a
//     page set in two columns, is not a table;
//   - its cells are lined up in their columns, at the left, the right
//     or the middle, and at least two in five of them have text: the
//     labels of a figure are scattered, and it does not lie on a
//     picture;
//   - its text does not run on from row to row as sentences do, and
//     its cells do not start with bullets;
//   - its rows do not start with the markers of list items, they do
//     not end in dot leaders, as a table of contents does, nor in the
//     comments of a listing of code.
//
// A row whose first cell is empty, close under the row before and with
// text only in columns that row has text in, continues the cells of
// that row: a cell of several lines. The header rows are the first rows
// of the table, up to three, when each has two cells or more, all set
// in bold, and the rows under them are not, but for a fifth of their
// cells at most: bold used for emphasis here and there is not a header,
// nor is a label in bold over a group of rows.

const (
	// segGap is the narrowest space, in ems, between two segments of a
	// row: wider than any space between words.
	segGap = 0.4
	// gutterMin is the narrowest gutter, in ems.
	gutterMin = 0.4
	// tableMinRows is the fewest rows of a table.
	tableMinRows = 3
	// rowPitchMax is the farthest apart, in sizes of the text, that two
	// rows of one table are.
	rowPitchMax = 2.6
	// cellWordsMax is the most words that the cells of a table have, at
	// the median; a fifth of them at most have more than cellWordsLong.
	cellWordsMax  = 6
	cellWordsLong = 10
	// maxTableRows bounds the rows of a region.
	maxTableRows = 4096
)

// seg is a stretch of a line between wide spaces.
type seg struct {
	line       int32 // in b.lines
	lo, hi     int32 // its pieces are b.lidx[lo:hi]
	u0, u1, em float64
	bold       bool
	words      int32 // as the text of its runs has them
}

// trow is a row of text: the lines on one baseline, as segments.
type trow struct {
	v, size float64
	segs    []seg
	lo, hi  int32 // where segs is in the builder's buffer, while it grows
}

// tcell is a cell of a region weighed as a table: the segments of a row
// in a column.
type tcell struct {
	segs   []seg
	span   int // the columns it reaches over, besides its own
	words  int
	bold   bool
	u0, u1 float64 // where its first segment is
	em     float64
	text   string
	first  int // the index of its first segment in its row
}

// tgrow is a row of such a region.
type tgrow struct {
	cells []tcell
	v     float64
	size  float64
}

// findTables finds the tables among the lines b.lines[first:]. It adds
// a block for each, takes their lines out of b.lines and returns them,
// in the order of the tables: they are not for paragraphs. The tables'
// blocks have, in tlines, where the lines are among those returned.
func (b *builder) findTables(first int) (taken []line, blocks []int32) {
	lines := b.lines[first:]
	if len(lines) < tableMinRows || !b.mayHaveTable(first) {
		return nil, nil
	}
	rows := b.tableRows(first)
	if len(rows) < tableMinRows {
		return nil, nil
	}
	var used []bool
	for i := 0; i < len(rows); {
		if len(rows[i].segs) < 2 {
			i++
			continue
		}
		j, g := tableRegion(rows, i)
		add := func(rows []trow, g []gutter, split bool) int {
			bl, period, ok := b.makeTable(rows, g, split)
			if !ok {
				return period
			}
			if len(used) < len(lines) {
				used = append(used, make([]bool, len(lines)-len(used))...)
			}
			b.blocks = append(b.blocks, bl)
			blocks = append(blocks, int32(len(b.blocks)-1))
			for _, r := range rows {
				for _, s := range r.segs {
					used[int(s.line)-first] = true
				}
			}
			return 0
		}
		if period := add(rows[i:j], g, true); period > 0 {
			// Tables set side by side: each is weighed by itself, and a
			// line with text in two of them is two lines.
			b.cutAcross(rows[i:j], g, period)
			lines = b.lines[first:]
			for m := 0; m < (len(g)+1)/period; m++ {
				part, pg := sideBySidePart(rows[i:j], g, period, m)
				add(part, pg, false)
			}
		}
		// A region that is no table has none in it either: what made it
		// a region holds for the rows after its first as well.
		i = max(j, i+1)
	}
	if len(blocks) == 0 {
		return nil, nil
	}
	return b.takeLines(first, lines, used, blocks), blocks
}

// takeLines takes the lines of tables, those of lines (b.lines[first:])
// that used marks, out of the lines for paragraphs and returns them, in
// their order. Their places change: the tables' blocks, whose tlines say
// where their lines are in b.lines, are told where they go among those
// returned.
func (b *builder) takeLines(first int, lines []line, used []bool, blocks []int32) (taken []line) {
	at := make([]int32, len(lines)) // new index of each line taken, among those taken
	keep := first
	for i := range lines {
		if used[i] {
			at[i] = int32(len(taken))
			taken = append(taken, lines[i])
		} else {
			b.lines[keep] = lines[i]
			keep++
		}
	}
	b.lines = b.lines[:keep]
	for _, k := range blocks {
		bl := &b.blocks[k]
		for n, li := range bl.tlines {
			bl.tlines[n] = at[int(li)-first]
		}
	}
	return taken
}

// mayHaveTable reports, cheaply, whether the lines b.lines[first:] can
// hold a table: whether enough of them have a wide space in them, or
// stand beside another line, for the rows of one.
func (b *builder) mayHaveTable(first int) bool {
	n := 0
	for i := first; i < len(b.lines); i++ {
		ln := &b.lines[i]
		for k := ln.lo + 1; k < ln.hi; k++ {
			if p := &b.pieces[b.lidx[k]]; (p.sep == ' ' || p.sep == '\t') && float64(p.gap) >= segGap*p.em {
				n++
				break
			}
		}
		if n >= tableMinRows {
			return true
		}
	}
	// Lines beside each other, as the cells of a row that are not drawn
	// one after the other are: two lines whose baselines meet.
	vs := b.tV[:0]
	for i := first; i < len(b.lines); i++ {
		vs = append(vs, b.lines[i].v)
	}
	b.tV = vs
	slices.Sort(vs)
	beside := 0
	for i := 1; i < len(vs); i++ {
		if vs[i]-vs[i-1] <= baselineSlack*b.lines[first].size {
			beside++
		}
	}
	return n+beside >= tableMinRows
}

// tableRows puts the lines b.lines[first:] in rows, from the top, and
// each row in segments, along the line.
//
// The rows and their segments are in buffers of the builder, kept from
// page to page: most pages have no table, and looking costs them little.
func (b *builder) tableRows(first int) []trow {
	order := b.tOrder[:0]
	for i := first; i < len(b.lines); i++ {
		if b.lines[i].hi > b.lines[i].lo {
			order = append(order, int32(i))
		}
	}
	b.tOrder = order
	slices.SortStableFunc(order, func(i, j int32) int {
		a, c := &b.lines[i], &b.lines[j]
		if a.v != c.v {
			return cmp.Compare(a.v, c.v)
		}
		return cmp.Compare(a.u0, c.u0)
	})
	rows, segs := b.tRows[:0], b.tSegs[:0]
	for i := 0; i < len(order); {
		head := &b.lines[order[i]]
		j := i + 1
		for j < len(order) && b.lines[order[j]].v-head.v <= baselineSlack*head.size {
			j++
		}
		r := trow{v: head.v, size: head.size, lo: int32(len(segs))}
		for _, li := range order[i:j] {
			ln := &b.lines[li]
			r.size = max(r.size, ln.size)
			start := ln.lo
			for k := ln.lo + 1; k <= ln.hi; k++ {
				if k < ln.hi {
					p := &b.pieces[b.lidx[k]]
					if !(p.sep == ' ' || p.sep == '\t') || float64(p.gap) < segGap*p.em {
						continue
					}
				}
				segs = append(segs, b.segment(li, start, k))
				start = k
			}
		}
		r.hi = int32(len(segs))
		rows = append(rows, r)
		i = j
	}
	// The segments of each row, now that the buffer stays where it is,
	// along the row. A row's slice cannot grow into the next row's.
	for x := range rows {
		r := &rows[x]
		r.segs = segs[r.lo:r.hi:r.hi]
		slices.SortStableFunc(r.segs, func(a, c seg) int { return cmp.Compare(a.u0, c.u0) })
	}
	b.tRows, b.tSegs = rows, segs
	return rows
}

// segment makes the segment of the pieces b.lidx[lo:hi] of line li,
// with its words counted from the text of its runs.
func (b *builder) segment(li, lo, hi int32) seg {
	s := seg{line: li, lo: lo, hi: hi}
	for q := lo; q < hi; q++ {
		p := &b.pieces[b.lidx[q]]
		r := &b.c.Runs[p.run]
		if q == lo {
			s.u0, s.u1, s.em = p.u0, p.u1, p.em
			s.bold = r.Font != nil && r.Font.Bold
		} else if p.sep == ' ' {
			s.words++
		}
		s.u0, s.u1 = min(s.u0, p.u0), max(s.u1, p.u1)
		inWord := false
		for _, c := range b.c.Text[r.Off : r.Off+r.Len] {
			if c == ' ' {
				inWord = false
			} else if !inWord {
				inWord = true
				s.words++
			}
		}
	}
	return s
}

// gutter is a stretch across a region that its rows leave white.
type gutter struct{ u0, u1 float64 }

// tableRegion returns where the region that starts at row i ends, and
// its gutters. The gutters are the spaces between the segments of row
// i; each row after it narrows them, or crosses some, as a cell over
// several columns does. The region ends at a row that is far from the
// one before or set at another size, at the second row in a row that
// crosses, or when more than a fifth of its rows cross; rows that cross
// at its end are not its own.
func tableRegion(rows []trow, i int) (int, []gutter) {
	r := &rows[i]
	var g []gutter
	for k := 1; k < len(r.segs); k++ {
		if lo, hi := r.segs[k-1].u1, r.segs[k].u0; hi-lo >= gutterMin*r.size {
			g = append(g, gutter{lo, hi})
		}
	}
	if len(g) == 0 {
		return i + 1, nil
	}
	j, crossing, before := i+1, 0, false
	next := make([]gutter, len(g))
	for ; j < len(rows) && j-i < maxTableRows; j++ {
		a, c := &rows[j-1], &rows[j]
		if c.v-a.v > rowPitchMax*max(a.size, c.size) || c.size > 1.35*a.size || a.size > 1.35*c.size {
			break
		}
		// Each gutter as the row leaves it: narrowed from its sides,
		// still wide enough, or crossed.
		crossed := false
		copy(next, g)
		for x := range next {
			for _, s := range c.segs {
				gx := &next[x]
				if s.u1 <= gx.u0 || s.u0 >= gx.u1 {
					continue
				}
				switch {
				case s.u0 <= gx.u0:
					gx.u0 = s.u1
				case s.u1 >= gx.u1:
					gx.u1 = s.u0
				default:
					gx.u0 = gx.u1 // text inside the gutter
				}
			}
			if next[x].u1-next[x].u0 < gutterMin*c.size {
				crossed = true
			}
		}
		if crossed {
			crossing++
			if before || 5*crossing > j-i+1 {
				break
			}
		} else {
			copy(g, next)
		}
		before = crossed
	}
	for j > i+1 && crosses(rows[j-1], g) {
		j--
	}
	return j, g
}

// crosses reports whether a segment of a row reaches over a gutter.
func crosses(r trow, g []gutter) bool {
	for _, s := range r.segs {
		for _, x := range g {
			if s.u0 < x.u1 && s.u1 > x.u0 {
				return true
			}
		}
	}
	return false
}

// makeTable makes a table of rows with the columns between gutters,
// if it is one. What the place of the text says is weighed first, and
// the text is written only for a region that passes.
//
// With split, a region that is tables set side by side is none, and the
// number of columns of each is returned (sideBySide).
func (b *builder) makeTable(rows []trow, g []gutter, split bool) (blk, int, bool) {
	cols := len(g) + 1
	if len(g) == 0 || len(rows) < tableMinRows {
		return blk{}, 0, false
	}
	colOf := func(u float64) int {
		return sort.Search(len(g), func(k int) bool { return g[k].u0 >= u })
	}
	// A cell is the segments of a row in a column. The segments of a row
	// are in order along it, and so are the columns: those of one cell
	// stand together, and the cell is a slice of the row's, which cannot
	// grow into the next cell's. The cells are in a buffer of the
	// builder: most regions are no table.
	if n := len(rows) * cols; cap(b.tCells) < n {
		b.tCells = make([]tcell, n)
	} else {
		b.tCells = b.tCells[:n]
		clear(b.tCells)
	}
	grid := b.tGrid[:0]
	for i, r := range rows {
		cells := b.tCells[i*cols : (i+1)*cols : (i+1)*cols]
		for x, s := range r.segs {
			c0, c1 := colOf(s.u0), colOf(s.u1)
			c := &cells[c0]
			if len(c.segs) == 0 {
				c.bold, c.u0, c.u1, c.em, c.first = s.bold, s.u0, s.u1, s.em, x
			}
			c.segs = r.segs[c.first : x+1 : x+1]
			c.span = max(c.span, c1-c0)
			c.words += int(s.words)
		}
		grid = append(grid, tgrow{cells, r.v, r.size})
	}
	b.tGrid = grid
	set := func(c *tcell) bool { return len(c.segs) > 0 }

	// A row that continues the cells of the row over it: a cell of
	// several lines. Its line is set closer under the row than the rows
	// are to each other, at the median: in a region whose rows are all as
	// far apart, as lists of different lengths side by side are, a row
	// with its first cell empty is a row.
	pitches := b.tV[:0]
	for i := 1; i < len(grid); i++ {
		pitches = append(pitches, grid[i].v-grid[i-1].v)
	}
	b.tV = pitches
	slices.Sort(pitches)
	pitch := pitches[len(pitches)/2]
	out := grid[:1]
	for _, r := range grid[1:] {
		up := &out[len(out)-1]
		cont := !set(&r.cells[0]) && r.v-up.v <= 1.5*max(r.size, up.size) && r.v-up.v < 0.85*pitch
		for k := range r.cells {
			c := &r.cells[k]
			cont = cont && (!set(c) || set(&up.cells[k]) && c.span == 0)
		}
		if !cont {
			out = append(out, r)
			continue
		}
		for k := range r.cells {
			if c := &r.cells[k]; set(c) {
				u := &up.cells[k]
				u.segs = append(u.segs, c.segs...)
				u.words += c.words
				u.bold = u.bold && c.bold
			}
		}
		up.v = r.v
	}
	grid = out
	if len(grid) < tableMinRows {
		return blk{}, 0, false
	}

	// Where the text is: short cells, most rows with several, most
	// cells filled, and lined up in their columns.
	words := b.tWords[:0]
	multi, long, filled := 0, 0, 0
	for _, r := range grid {
		n := 0
		for k := range r.cells {
			if c := &r.cells[k]; set(c) {
				n++
				words = append(words, c.words)
				if c.words > cellWordsLong {
					long++
				}
			}
		}
		if n >= 2 {
			multi++
		}
		filled += n
	}
	lined, columns := 0, 0
	for k := 0; k < cols; k++ {
		var lo, hi [3]float64
		n, em := 0, 0.0
		for _, r := range grid {
			c := &r.cells[k]
			if !set(c) || c.span > 0 {
				continue
			}
			at := [3]float64{c.u0, c.u1, (c.u0 + c.u1) / 2}
			if n == 0 {
				lo, hi = at, at
			}
			for x := range at {
				lo[x], hi[x] = min(lo[x], at[x]), max(hi[x], at[x])
			}
			n++
			em = max(em, c.em)
		}
		if n < 2 {
			continue
		}
		columns++
		if min(hi[0]-lo[0], hi[1]-lo[1], hi[2]-lo[2]) <= em {
			lined++
		}
	}
	b.tWords = words
	slices.Sort(words)
	switch {
	case len(words) == 0 || words[len(words)/2] > cellWordsMax || 5*long > len(words):
		return blk{}, 0, false // columns of prose
	case 2*multi < len(grid):
		return blk{}, 0, false // rows of one cell
	case 5*filled < 2*len(grid)*cols:
		return blk{}, 0, false // more empty than not: labels over a figure
	case 3*lined < 2*columns || 3*columns < 2*cols:
		return blk{}, 0, false // columns not lined up, or hardly columns
	}

	// The text, and what it says.
	var tlines []int32
	seen := map[int32]bool{}
	texts := make([][]string, len(grid))
	for i := range grid {
		texts[i] = make([]string, cols)
		for k := range grid[i].cells {
			c := &grid[i].cells[k]
			var parts []string
			for _, s := range c.segs {
				if !seen[s.line] {
					seen[s.line] = true
					tlines = append(tlines, s.line)
				}
				if t := strings.ReplaceAll(b.segText(s), "\t", " "); t != "" {
					parts = append(parts, t)
				}
			}
			c.text = strings.Join(parts, " ")
			texts[i][k] = c.text
		}
	}
	if split {
		if period := b.sideBySide(texts, grid, cols); period > 0 {
			return blk{}, period, false
		}
	}
	markers, leaders, code, bullets, lower, lettered := 0, 0, 0, 0, 0, 0
	for _, r := range grid {
		first, last := "", ""
		for k := range r.cells {
			c := &r.cells[k]
			if c.text == "" {
				continue
			}
			if first == "" {
				first = c.text
			}
			if k > 0 {
				last = c.text
			}
			if bulleted(c.text) {
				bullets++
			}
			if f, _ := utf8.DecodeRuneInString(c.text); unicode.IsLetter(f) && c.words >= 3 {
				lettered++ // of a few words: a word alone in small letters is a value
				if unicode.IsLower(f) {
					lower++
				}
			}
		}
		if _, rest, _, ok := ListMarker(first + " x"); ok && rest == "x" || symbol(first) {
			markers++ // the marker of a list item, or a mark that stands for one
		}
		for k := range r.cells {
			if t := r.cells[k].text; strings.Contains(t, "....") || strings.Contains(t, ". . .") {
				leaders++
				break
			}
		}
		if comment(last) {
			code++
		}
	}
	// A row whose cells mostly say the same thing is one label set again
	// beside itself, as the time scale of a schedule drawn for several
	// stages side by side is: no row of a table.
	repeated := 0
	for _, r := range texts {
		most, n := 0, 0
		for x, t := range r {
			if t == "" {
				continue
			}
			n++
			same := 0
			for _, u := range r[x:] {
				if u == t {
					same++
				}
			}
			most = max(most, same)
		}
		if most >= 2 && 2*most > n {
			repeated++
		}
	}
	// Text that runs on, down a column of words, as the lines of a
	// paragraph do.
	pairs, running := 0, 0
	for k := 0; k < cols; k++ {
		n, w := 0, 0
		for _, r := range grid {
			if c := &r.cells[k]; c.text != "" {
				n++
				if c.words >= 3 {
					w++
				}
			}
		}
		if n == 0 || 2*w <= n {
			continue // not a column of words
		}
		for i := 1; i < len(grid); i++ {
			if up, c := grid[i-1].cells[k].text, grid[i].cells[k].text; up != "" && c != "" {
				pairs++
				if runsOn(up, c) {
					running++
				}
			}
		}
	}
	switch {
	case 2*markers >= len(grid):
		return blk{}, 0, false // a list: its markers and its items
	case 3*bullets >= filled:
		return blk{}, 0, false // items of lists side by side
	case pairs > 0 && 3*running >= pairs:
		return blk{}, 0, false // sentences that run on from row to row
	case lettered > 0 && 4*lower >= lettered:
		return blk{}, 0, false // pieces of sentences
	case 2*leaders >= len(grid):
		return blk{}, 0, false // a table of contents
	case 2*code >= len(grid):
		return blk{}, 0, false // code, with its comments lined up
	case sameColumns(texts):
		return blk{}, 0, false // a list set in columns, read down each
	case 2*repeated >= len(grid):
		return blk{}, 0, false // labels repeated side by side
	}

	// The header rows: the first, all in bold, over rows that are not.
	// A row of one cell in bold is the label of the rows under it, a
	// group of them, and no header.
	head := 0
	for head < min(3, len(grid)-1) {
		all, n := true, 0
		for k := range grid[head].cells {
			if c := &grid[head].cells[k]; c.text != "" {
				all = all && c.bold
				n++
			}
		}
		if !all || n < 2 {
			break
		}
		head++
	}
	if head > 0 {
		body, bold := 0, 0
		for _, r := range grid[head:] {
			for k := range r.cells {
				if c := &r.cells[k]; c.text != "" {
					body++
					if c.bold {
						bold++
					}
				}
			}
		}
		if body == 0 || 5*bold > body {
			head = 0 // bold throughout: no header by weight
		}
	}

	t := &gofastcore.Table{HeaderRows: head, Rows: texts}
	for r, row := range grid {
		for k := range row.cells {
			c := &row.cells[k]
			if c.text == "" || c.span == 0 || k+c.span >= cols {
				continue
			}
			free := true
			for x := k + 1; x <= k+c.span; x++ {
				free = free && row.cells[x].text == ""
			}
			if free {
				t.Merges = append(t.Merges, gofastcore.Merge{Row: r, Col: k, RowSpan: 1, ColSpan: c.span + 1})
			}
		}
	}
	firstLine := &b.lines[tlines[0]]
	bl := blk{grid: t, tlines: tlines, line: -1, key: firstLine.key, size: firstLine.size,
		x0: firstLine.x0, y0: firstLine.y0, x1: firstLine.x1, y1: firstLine.y1}
	for _, li := range tlines {
		ln := &b.lines[li]
		bl.x0, bl.y0, bl.x1, bl.y1 = min(bl.x0, ln.x0), min(bl.y0, ln.y0), max(bl.x1, ln.x1), max(bl.y1, ln.y1)
		bl.key, bl.size = min(bl.key, ln.key), max(bl.size, ln.size)
	}
	if b.overPicture(box{bl.x0, bl.y0, bl.x1, bl.y1}) {
		return blk{}, 0, false // labels on a picture, a map or a diagram
	}
	return bl, 0, true
}

// sideBySide returns the number of columns of each of the tables that a
// grid is, when it is tables of the same columns set side by side, and
// 0 when it is one: as the line-ups of the stages of a festival, a name
// and a time for each, are set beside each other. The kinds of the
// columns, of numbers or of words, repeat from table to table, and do
// not all repeat within one. That alone would split a table of a name,
// an age, a town and a number, so one of these holds too: the tables are
// lists of different lengths, one ending where another goes on, or they
// all start with the same header. A line with text in two of them is cut
// in two (cutAcross), which is done for lines written from left to right
// only: another keeps the grid one table.
func (b *builder) sideBySide(texts [][]string, grid []tgrow, cols int) int {
	if len(texts) == 0 || cols < 4 {
		return 0
	}
	numeric := make([]bool, cols)
	for k := 0; k < cols; k++ {
		digits, chars := 0, 0
		for _, r := range texts {
			for _, c := range r[k] {
				chars++
				if unicode.IsDigit(c) {
					digits++
				}
			}
		}
		if chars == 0 {
			return 0
		}
		numeric[k] = 4*digits >= chars
	}
	period := 0
	for p := 2; p <= cols/2 && period == 0; p++ {
		if cols%p != 0 || !slices.Contains(numeric[:p], !numeric[0]) {
			continue
		}
		same := true
		for k := p; k < cols; k++ {
			same = same && numeric[k] == numeric[k%p]
		}
		if same {
			period = p
		}
	}
	if period == 0 {
		return 0
	}
	n := cols / period
	// Where each ends, and whether all start alike.
	last := make([]int, n)
	header := true
	for i, r := range texts {
		for m := 0; m < n; m++ {
			part := r[m*period : (m+1)*period]
			if slices.ContainsFunc(part, func(t string) bool { return t != "" }) {
				last[m] = i
			}
			if i == 0 && m > 0 && !slices.Equal(part, r[:period]) {
				header = false
			}
		}
	}
	if slices.Min(last) == slices.Max(last) && !header {
		return 0
	}
	// A line in two of them is written from left to right.
	for _, r := range grid {
		owner := map[int32]int{}
		for k := range r.cells {
			for _, s := range r.cells[k].segs {
				if o, ok := owner[s.line]; ok && o != k/period && b.lines[s.line].class != 0 {
					return 0
				}
				owner[s.line] = k / period
			}
		}
	}
	return period
}

// cutAcross cuts each line of the rows of tables set side by side
// (sideBySide) that has text in two of them, at the start of the text of
// the next: a line belongs to one block. The pieces of a line are in
// order along it, and its segments are pieces from one wide space to the
// next. The part after a cut is a new line, at the end of b.lines, and
// the segments in it say so.
func (b *builder) cutAcross(rows []trow, g []gutter, period int) {
	table := func(u float64) int {
		m := 0
		for (m+1)*period-1 < len(g) && u >= g[(m+1)*period-1].u0 {
			m++
		}
		return m
	}
	stats := b.stats // the line is counted once, as it was
	for _, r := range rows {
		for x := 1; x < len(r.segs); x++ {
			s, prev := &r.segs[x], &r.segs[x-1]
			if s.line != prev.line || table(s.u0) == table(prev.u0) {
				continue
			}
			old, at := b.lines[s.line], s.line
			head := old
			head.hi = s.lo
			tail := line{lo: s.lo, hi: old.hi, class: old.class, pred: -1}
			b.measure(&head)
			b.lineText(&head)
			b.measure(&tail)
			b.lineText(&tail)
			b.lines[at] = head
			b.lines = append(b.lines, tail)
			for y := x; y < len(r.segs); y++ {
				if r.segs[y].line == at {
					r.segs[y].line = int32(len(b.lines) - 1)
				}
			}
		}
	}
	b.stats = stats
}

// sideBySidePart returns the rows of the m-th of the tables of period
// columns that rows are (sideBySide), and its gutters: the segments of
// each row that begin in its columns, and the rows that have some.
func sideBySidePart(rows []trow, g []gutter, period, m int) ([]trow, []gutter) {
	lo, hi := math.Inf(-1), math.Inf(1)
	if m > 0 {
		lo = g[m*period-1].u0
	}
	if (m+1)*period-1 < len(g) {
		hi = g[(m+1)*period-1].u0
	}
	var out []trow
	for _, r := range rows {
		a := 0
		for a < len(r.segs) && r.segs[a].u0 < lo {
			a++
		}
		z := a
		for z < len(r.segs) && r.segs[z].u0 < hi {
			z++
		}
		if z > a {
			r.segs = r.segs[a:z:z]
			out = append(out, r)
		}
	}
	return out, g[m*period : (m+1)*period-1]
}

// sameColumns reports whether the columns of a grid are all of one
// kind: none of numbers, and their cells about as long in each. Such a
// grid is one list set in columns, to be read down each, as names or
// countries are; the columns of a table hold different things.
func sameColumns(rows [][]string) bool {
	if len(rows) == 0 {
		return false
	}
	cols := len(rows[0])
	lo, hi := 0.0, 0.0
	for k := 0; k < cols; k++ {
		n, length, digits, chars := 0, 0, 0, 0
		for _, r := range rows {
			if r[k] == "" {
				continue
			}
			n++
			for _, c := range r[k] {
				chars++
				length++
				if unicode.IsDigit(c) {
					digits++
				}
			}
		}
		if n == 0 {
			continue
		}
		if 4*digits >= chars {
			return false // a column of numbers: a table
		}
		mean := float64(length) / float64(n)
		if k == 0 || lo == 0 {
			lo, hi = mean, mean
		}
		lo, hi = min(lo, mean), max(hi, mean)
	}
	return lo > 0 && hi <= 1.33*lo
}

// overPicture reports whether most of a box lies on an image that does
// not fill the page: a picture with labels set on it.
func (b *builder) overPicture(x box) bool {
	area := (x.x1 - x.x0) * (x.y1 - x.y0)
	page := b.page.Width * b.page.Height
	for i := range b.c.Images {
		_, bb := b.quadBox(b.c.Images[i].Quad)
		if a := (bb.x1 - bb.x0) * (bb.y1 - bb.y0); page > 0 && a > 0.8*page {
			continue // the ground of the page
		}
		w, h := min(x.x1, bb.x1)-max(x.x0, bb.x0), min(x.y1, bb.y1)-max(x.y0, bb.y0)
		if w > 0 && h > 0 && w*h > area/2 {
			return true
		}
	}
	return false
}

// symbol reports whether text is one mark that is not a letter or a
// digit: a bullet, a check mark, a character that could not be read.
func symbol(text string) bool {
	r := []rune(text)
	return len(r) == 1 && !unicode.IsLetter(r[0]) && !unicode.IsDigit(r[0])
}

// bulleted reports whether text starts with a bullet, or a mark that
// could not be read where a bullet would be, and goes on with words.
func bulleted(text string) bool {
	r, n := utf8.DecodeRuneInString(text)
	return n < len(text) && text[n] == ' ' && strings.ContainsRune("•·▪◦‣⁃∙●○■□❑➢►✓✔-–\uFFFD", r)
}

// runsOn reports whether the text of a line goes on in the line under
// it, as a sentence does: the lower starts with a small letter, or the
// upper ends with a hyphen, a comma or one of the little words that
// do not end a sentence.
func runsOn(upper, lower string) bool {
	if r, _ := utf8.DecodeRuneInString(lower); unicode.IsLower(r) {
		return true
	}
	return endsOpen(upper)
}

// endsOpen reports whether a line ends as no sentence does: with a
// hyphen, a comma or one of the little words that do not end one.
func endsOpen(upper string) bool {
	upper = strings.TrimSpace(upper)
	if strings.HasSuffix(upper, "-") || strings.HasSuffix(upper, ",") {
		return true
	}
	last := upper[strings.LastIndexByte(upper, ' ')+1:]
	switch strings.ToLower(last) {
	case "a", "an", "and", "as", "at", "by", "for", "from", "in", "of", "on", "or", "the", "to", "with":
		return true
	}
	return false
}

// comment reports whether text starts as a comment of a program does.
func comment(text string) bool {
	for _, m := range [...]string{"//", "/*", "#", "--", ";"} {
		if strings.HasPrefix(text, m) {
			return true
		}
	}
	return false
}

// segText writes the text of a segment, as the text of a line of its
// pieces alone.
func (b *builder) segText(s seg) string {
	ln := b.lines[s.line]
	ln.lo, ln.hi = s.lo, s.hi
	stats := b.stats
	b.lineText(&ln)
	b.stats = stats
	return strings.TrimSpace(b.clean(b.text[ln.tlo:ln.thi]))
}
