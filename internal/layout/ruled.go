package layout

import (
	"cmp"
	"slices"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	gofastcore "github.com/AOShei/go-fast-core"

	"github.com/AOShei/go-fast-pdf/internal/extract"
)

// Tables found by their rules.
//
// A table drawn with borders is a grid of rules: lines along the axes
// that meet. The rules a page paints (extract.Content.Rules) are joined
// where they continue each other, and those that meet make up a grid.
// Its columns are between the places of its upright rules, its rows
// between those of its level ones; where the outermost rules of one
// direction reach further than those of the other, as in a table with
// rules between its rows and none at its sides, their ends close the
// grid. A rule missing inside the grid joins the cells on its two sides:
// a cell over several columns or rows. A grid whose joined cells are not
// rectangles is no table.
//
// Text goes to the cell that holds the middle of each of its pieces. A
// grid is a table when it has two columns or more and two rows or more,
// at least two rows have text in two cells, a quarter of its cells or
// more have text, the text of a cell is no paragraph of prose, and it
// does not lie on a picture. A line that the grid's edge cuts through
// stays out of the table; where a quarter of the lines in the grid or
// more are such, the rules frame something else and are no table. Text
// drawn in one go across an upright rule is cut there first (cutRuns).
// Rows and columns with no text are left out. The header rows are found as in
// tables found by alignment: the first rows, all in bold, over rows that
// are not.
//
// Tables by their rules are looked for before tables by alignment, among
// the same lines: text without a display reason, in no cell of a tagged
// table.

const (
	// ruleSnap is how close, in points, the rules at one place are.
	ruleSnap = 2.0
	// maxRuleWork bounds the comparisons made to find the grids of a
	// page: a page past it is a drawing of many lines, a map or a chart,
	// and is not searched for ruled tables.
	maxRuleWork = 1 << 18
	// maxRuledCells bounds the cells of a grid.
	maxRuledCells = 1 << 14
	// ruledWordsMax is the most words that the cells of a ruled table
	// have, at the median.
	ruledWordsMax = 30
)

// arule is a rule along one axis of displayed space: at its place across
// that axis, from lo to hi along it.
type arule struct{ at, lo, hi float64 }

// findRuledTables finds the tables drawn with rules among the lines
// b.lines[first:], as findTables does those found by alignment.
func (b *builder) findRuledTables(first int) (taken []line, blocks []int32) {
	if len(b.c.Rules) < 4 || len(b.lines) == first {
		return nil, nil
	}
	if !b.gridsDone {
		// Once for the page, whatever groups of its text look.
		b.gridsDone = true
		if hs, vs := b.displayRules(); len(hs) >= 2 && len(vs) >= 2 {
			b.grids = ruleGrids(hs, vs)
		}
	}
	lines := b.lines[first:]
	var used []bool
	for _, g := range b.grids {
		bl, mine, ok := b.ruledTable(first, g)
		if !ok {
			continue
		}
		if used == nil {
			used = make([]bool, len(lines))
		}
		clash := false
		for _, li := range mine {
			clash = clash || used[int(li)-first]
		}
		if clash {
			continue // a grid inside another's
		}
		for _, li := range mine {
			used[int(li)-first] = true
		}
		b.blocks = append(b.blocks, bl)
		blocks = append(blocks, int32(len(b.blocks)-1))
	}
	if len(blocks) == 0 {
		return nil, nil
	}
	return b.takeLines(first, lines, used, blocks), blocks
}

// displayRules returns the page's rules in displayed space, the level
// ones and the upright ones, each joined with those that continue it.
func (b *builder) displayRules() (hs, vs []arule) {
	for _, r := range b.c.Rules {
		x0, y0 := b.toDisplay(r[0], r[1])
		x1, y1 := b.toDisplay(r[2], r[3])
		x0, x1 = min(x0, x1), max(x0, x1)
		y0, y1 = min(y0, y1), max(y0, y1)
		if x1-x0 >= y1-y0 {
			hs = append(hs, arule{(y0 + y1) / 2, x0, x1})
		} else {
			vs = append(vs, arule{(x0 + x1) / 2, y0, y1})
		}
	}
	return joinRules(hs), joinRules(vs)
}

// joinRules joins the rules at one place that overlap or nearly touch:
// a rule drawn in pieces, or twice, as the shared border of two cells
// drawn each as a rectangle is.
func joinRules(rs []arule) []arule {
	slices.SortFunc(rs, func(a, c arule) int {
		if d := cmp.Compare(a.at, c.at); d != 0 {
			return d
		}
		return cmp.Compare(a.lo, c.lo)
	})
	out := rs[:0]
	for _, r := range rs {
		if n := len(out); n > 0 {
			if p := &out[n-1]; r.at-p.at <= ruleSnap/2 && r.lo <= p.hi+ruleSnap {
				p.lo, p.hi = min(p.lo, r.lo), max(p.hi, r.hi)
				continue
			}
		}
		out = append(out, r)
	}
	return out
}

// Runs cut by the rules of a grid.
//
// A run is text that the page draws in one go, and it can hold the
// entries of two cells of a row: the interpreter joins strings that
// stand less than a font size apart. Where a string goes on from the
// one before it across an upright rule of a grid, the run is cut there:
// what stands on the two sides of a drawn line is not one piece of text.
// Where the two strings end and begin is known (extract.Join), and the
// rule must lie in the room between them, where nothing is drawn. The
// spaces of the file there go with the second part; a space that the
// interpreter put in goes with neither, and the layout weighs the gap
// between the two parts as it weighed that space. So the text of the
// line is what it was, and the line is laid out as before unless the
// grid is a table.

// cutRuns finds the grids of the page and cuts the runs that their
// upright rules pass through. It cuts a copy: the content is the
// caller's, and may be laid out again.
func (b *builder) cutRuns() {
	c := b.c
	if len(c.Rules) < 4 || len(c.Runs) == 0 {
		return
	}
	b.gridsDone = true
	hs, vs := b.displayRules()
	if len(hs) < 2 || len(vs) < 2 {
		return
	}
	b.grids = ruleGrids(hs, vs)
	if len(b.grids) == 0 {
		return
	}
	var ups []arule
	for _, g := range b.grids {
		ups = append(ups, g.vs...)
	}
	slices.SortFunc(ups, func(a, c arule) int { return cmp.Compare(a.at, c.at) })

	var out []extract.Run // the runs, once one is cut
	var cuts []runCut
	for i := range c.Runs {
		r := &c.Runs[i]
		cuts = b.runCuts(cuts[:0], r, ups)
		if len(cuts) == 0 {
			if out != nil {
				out = append(out, *r)
			}
			continue
		}
		if out == nil {
			out = append(make([]extract.Run, 0, len(c.Runs)+len(cuts)), c.Runs[:i]...)
		}
		out = cutRun(out, r, cuts)
	}
	if out != nil {
		cc := *c
		cc.Runs = out
		b.c = &cc
	}
}

// runCut is a place where a run is cut: the first byte of the spaces
// there, in the run's text, and where they begin and end on the line in
// the page's coordinates. put is set for a space that the interpreter
// put in.
type runCut struct {
	at             int32
	x0, y0, x1, y1 float64
	put            bool
}

// runCuts appends the places where the upright rules ups, in the order
// of their places, pass between two strings of the run r.
func (b *builder) runCuts(dst []runCut, r *extract.Run, ups []arule) []runCut {
	if r.Len < 3 || r.Back || r.RTL || r.ActualText || r.Vertical() {
		return dst
	}
	if dx, _ := b.direction(r.Dx, r.Dy); dx < 0.9999 {
		return dst // written along the displayed page's x axis only
	}
	sx, sy := b.toDisplay(r.X0, r.Y0)
	ex, _ := b.toDisplay(r.X1, r.Y1)
	k0 := sort.Search(len(ups), func(k int) bool { return ups[k].at > sx })
	if k0 == len(ups) || ups[k0].at >= ex {
		return dst
	}
	joins, gaps := b.c.Joins, b.c.Gaps
	j := sort.Search(len(joins), func(i int) bool { return joins[i].Off > r.Off })
	if j == len(joins) || joins[j].Off >= r.Off+r.Len {
		return dst
	}
	text := b.c.Text[r.Off : r.Off+r.Len]
	_, ny := b.direction(r.Nx, r.Ny)
	mid := sy + ny*0.3*r.Size*b.scale // the middle of the glyphs
	for ; j < len(joins) && joins[j].Off < r.Off+r.Len; j++ {
		jn := &joins[j]
		i := jn.Off - r.Off
		// The spaces at the join: those the string before ends with,
		// or the space put in, or those the string begins with.
		s, e := i, i
		for s > 0 && text[s-1] == ' ' {
			s--
		}
		for e < r.Len && text[e] == ' ' {
			e++
		}
		if s == 0 || e == r.Len || s == e {
			continue
		}
		// The room between the two strings: from the last glyph before
		// the spaces of the string before to the first glyph after those
		// of the string after.
		before, after := float64(i-s), float64(e-i)
		put := false
		if g := sort.Search(len(gaps), func(k int) bool { return gaps[k].Off >= jn.Off-1 }); g < len(gaps) && gaps[g].Off == jn.Off-1 {
			before, put = before-1, true // put in: it has no width
		}
		w := r.SpaceWidth
		c := runCut{at: s, put: put && e-s == 1,
			x0: jn.X0 - before*w*r.Dx, y0: jn.Y0 - before*w*r.Dy,
			x1: jn.X1 + after*w*r.Dx, y1: jn.Y1 + after*w*r.Dy}
		d0, _ := b.toDisplay(c.x0, c.y0)
		d1, _ := b.toDisplay(c.x1, c.y1)
		for k := k0; k < len(ups) && ups[k].at <= d1; k++ {
			if v := &ups[k]; v.at >= d0 && mid >= v.lo-ruleSnap && mid <= v.hi+ruleSnap {
				if n := len(dst); n == 0 || dst[n-1].at < c.at {
					dst = append(dst, c)
				}
				break
			}
		}
	}
	return dst
}

// cutRun appends the parts of r cut at cuts, which are in order. Each
// part after the first begins with the spaces of the file it was cut at,
// and where the text after them begins.
func cutRun(dst []extract.Run, r *extract.Run, cuts []runCut) []extract.Run {
	along := func(x, y float64) float64 { return (x-r.X0)*r.Dx + (y-r.Y0)*r.Dy }
	last := along(r.LX, r.LY)
	for k := 0; k <= len(cuts); k++ {
		p := *r
		start, end := int32(0), r.Len
		if k > 0 {
			c := &cuts[k-1]
			start = c.at
			if c.put {
				start++
			}
			p.X0, p.Y0 = c.x1, c.y1
			p.Glyphs, p.Unmapped, p.Assumed = 0, 0, 0
		}
		if k < len(cuts) {
			end = cuts[k].at
			p.X1, p.Y1 = cuts[k].x0, cuts[k].y0
		}
		p.Off, p.Len = r.Off+start, end-start
		// Where its last string begins: the run's, if that is in it.
		if a := along(p.X0, p.Y0); last < a || last > along(p.X1, p.Y1) {
			p.LX, p.LY = p.X0, p.Y0
		}
		dst = append(dst, p)
	}
	return dst
}

// grid is the rules of one table: those that meet each other.
type grid struct{ hs, vs []arule }

// ruleGrids returns the groups of rules that meet, each with two level
// rules and two upright ones at least.
func ruleGrids(hs, vs []arule) []grid {
	parent := make([]int32, len(hs)+len(vs))
	for i := range parent {
		parent[i] = int32(i)
	}
	var find func(int32) int32
	find = func(i int32) int32 {
		for parent[i] != i {
			parent[i] = parent[parent[i]]
			i = parent[i]
		}
		return i
	}
	// The upright rules are in the order of their places (joinRules):
	// those a level rule reaches are found by a search.
	work := 0
	for i, h := range hs {
		k := sort.Search(len(vs), func(k int) bool { return vs[k].at >= h.lo-ruleSnap })
		for ; k < len(vs) && vs[k].at <= h.hi+ruleSnap; k++ {
			if work++; work > maxRuleWork {
				return nil
			}
			if v := &vs[k]; h.at >= v.lo-ruleSnap && h.at <= v.hi+ruleSnap {
				a, c := find(int32(i)), find(int32(len(hs)+k))
				parent[a] = c
			}
		}
	}
	groups := map[int32]*grid{}
	var roots []int32
	for i := range parent {
		r := find(int32(i))
		g := groups[r]
		if g == nil {
			g = &grid{}
			groups[r] = g
			roots = append(roots, r)
		}
		if i < len(hs) {
			g.hs = append(g.hs, hs[i])
		} else {
			g.vs = append(g.vs, vs[i-len(hs)])
		}
	}
	var out []grid
	for _, r := range roots {
		if g := groups[r]; len(g.hs) >= 2 && len(g.vs) >= 2 {
			out = append(out, *g)
		}
	}
	return out
}

// places returns the places of rules, those within ruleSnap of each
// other as one, and closes them at lo and hi where the rules of the
// other direction reach further: those ends are edges of the grid
// without a rule, and open reports which they are.
func places(rs []arule, lo, hi float64) (at []float64, open []bool) {
	for _, r := range rs {
		if n := len(at); n > 0 && r.at-at[n-1] <= ruleSnap {
			continue
		}
		at = append(at, r.at)
	}
	open = make([]bool, len(at))
	if len(at) > 0 && lo < at[0]-ruleSnap {
		at, open = append([]float64{lo}, at...), append([]bool{true}, open...)
	}
	if len(at) > 0 && hi > at[len(at)-1]+ruleSnap {
		at, open = append(at, hi), append(open, true)
	}
	return at, open
}

// covers reports whether a rule of rs, which are in the order of their
// places, at the place p reaches over the middle m of the stretch
// between two rules of the other direction.
func covers(rs []arule, p, m float64) bool {
	k := sort.Search(len(rs), func(k int) bool { return rs[k].at >= p-ruleSnap })
	for ; k < len(rs) && rs[k].at <= p+ruleSnap; k++ {
		if r := &rs[k]; r.lo-ruleSnap <= m && m <= r.hi+ruleSnap {
			return true
		}
	}
	return false
}

// ruledCell is a cell of a ruled table: cells of the grid joined where a
// rule between them is missing.
type ruledCell struct {
	r0, r1, c0, c1 int // the rows and columns it covers, both ends in
	segs           []seg
	text           string
	words          int
	bold           bool
}

// ruledTable makes a table of a grid, if it is one, and returns the
// lines it takes.
//
// The rules of g are in the order of their places, as ruleGrids leaves
// them.
func (b *builder) ruledTable(first int, g grid) (blk, []int32, bool) {
	hlo, hhi, vlo, vhi := g.hs[0].lo, g.hs[0].hi, g.vs[0].lo, g.vs[0].hi
	for _, h := range g.hs {
		hlo, hhi = min(hlo, h.lo), max(hhi, h.hi)
	}
	for _, v := range g.vs {
		vlo, vhi = min(vlo, v.lo), max(vhi, v.hi)
	}
	xs, xOpen := places(g.vs, hlo, hhi)
	ys, yOpen := places(g.hs, vlo, vhi)
	cols, rows := len(xs)-1, len(ys)-1
	if cols < 2 || rows < 2 || cols*rows > maxRuledCells {
		return blk{}, nil, false
	}

	// The cells of the grid, joined where no rule parts them.
	parent := make([]int32, rows*cols)
	for i := range parent {
		parent[i] = int32(i)
	}
	find := func(i int32) int32 {
		for parent[i] != i {
			parent[i] = parent[parent[i]]
			i = parent[i]
		}
		return i
	}
	join := func(a, c int) { parent[find(int32(a))] = find(int32(c)) }
	for r := 0; r < rows; r++ {
		m := (ys[r] + ys[r+1]) / 2
		for c := 0; c+1 < cols; c++ {
			if !xOpen[c+1] && !covers(g.vs, xs[c+1], m) {
				join(r*cols+c, r*cols+c+1)
			}
		}
	}
	for c := 0; c < cols; c++ {
		m := (xs[c] + xs[c+1]) / 2
		for r := 0; r+1 < rows; r++ {
			if !yOpen[r+1] && !covers(g.hs, ys[r+1], m) {
				join(r*cols+c, (r+1)*cols+c)
			}
		}
	}
	at := make([]int32, rows*cols) // the root of a cell to one more than its index in cells
	var cells []ruledCell
	of := make([]int, rows*cols)
	for r := 0; r < rows; r++ {
		for c := 0; c < cols; c++ {
			root := find(int32(r*cols + c))
			k := int(at[root]) - 1
			if k < 0 {
				k = len(cells)
				at[root] = int32(k + 1)
				cells = append(cells, ruledCell{r0: r, r1: r, c0: c, c1: c, bold: true})
			}
			x := &cells[k]
			x.r0, x.r1, x.c0, x.c1 = min(x.r0, r), max(x.r1, r), min(x.c0, c), max(x.c1, c)
			of[r*cols+c] = k
		}
	}
	for k := range cells {
		x := &cells[k]
		for r := x.r0; r <= x.r1; r++ {
			for c := x.c0; c <= x.c1; c++ {
				if of[r*cols+c] != k {
					return blk{}, nil, false // joined into no rectangle
				}
			}
		}
	}

	// A quarter of the cells at least must have text: there are not
	// enough pieces of text in the grid for that, it is no table, and
	// the text need not be placed.
	X0, X1, Y0, Y1 := xs[0], xs[cols], ys[0], ys[rows]
	pieces := 0
	for i := first; i < len(b.lines) && 4*pieces < len(cells); i++ {
		if ln := &b.lines[i]; ln.x1 > X0 && ln.x0 < X1 && ln.y1 > Y0 && ln.y0 < Y1 {
			pieces += int(ln.hi - ln.lo)
		}
	}
	if 4*pieces < len(cells) {
		return blk{}, nil, false
	}

	// The text: each piece goes to the cell that holds its middle; a
	// line goes to the table whole, or not at all. A line that the edge
	// of the grid cuts through stays out of it; when there are many, the
	// rules frame something else.
	inGrid := func(x, y float64) bool { return x > X0 && x < X1 && y > Y0 && y < Y1 }
	var mine []int32
	across := 0
	for i := first; i < len(b.lines); i++ {
		ln := &b.lines[i]
		if ln.x1 <= X0 || ln.x0 >= X1 || ln.y1 <= Y0 || ln.y0 >= Y1 {
			continue // nowhere near
		}
		center := inGrid((ln.x0+ln.x1)/2, (ln.y0+ln.y1)/2)
		in, out := 0, 0
		for k := ln.lo; k < ln.hi; k++ {
			p := &b.pieces[b.lidx[k]]
			if inGrid((p.x0+p.x1)/2, (p.y0+p.y1)/2) {
				in++
			} else {
				out++
			}
		}
		reach := ln.x0 < X0-ruleSnap || ln.x1 > X1+ruleSnap ||
			ln.y0 < Y0-0.5*ln.size || ln.y1 > Y1+0.5*ln.size
		switch {
		case in == 0:
			continue
		case out > 0 || !center || reach:
			across++
			continue
		}
		mine = append(mine, int32(i))
		cur, start := -1, ln.lo
		for k := ln.lo; k <= ln.hi; k++ {
			cell := -1
			if k < ln.hi {
				p := &b.pieces[b.lidx[k]]
				c := max(0, min(cols-1, sort.SearchFloat64s(xs, (p.x0+p.x1)/2)-1))
				r := max(0, min(rows-1, sort.SearchFloat64s(ys, (p.y0+p.y1)/2)-1))
				cell = of[r*cols+c]
			}
			if cell != cur {
				if cur >= 0 {
					b.ruledSeg(&cells[cur], int32(i), start, k)
				}
				cur, start = cell, k
			}
		}
	}
	if len(mine) == 0 || 4*across > len(mine)+across {
		return blk{}, nil, false
	}

	// Rows of text between two rules: a table ruled between its columns
	// and not between its rows.
	cells, rows = b.splitRows(cells, rows, xs)

	// The text of each cell, its lines from the top.
	filled := 0
	var words []int
	for k := range cells {
		x := &cells[k]
		if len(x.segs) == 0 {
			x.bold = false
			continue
		}
		slices.SortStableFunc(x.segs, func(a, c seg) int {
			if a.line != c.line {
				la, lc := &b.lines[a.line], &b.lines[c.line]
				if d := cmp.Compare(la.y0+la.y1, lc.y0+lc.y1); d != 0 {
					return d
				}
			}
			return cmp.Compare(a.u0, c.u0)
		})
		var out []byte
		prevLine := int32(-1)
		for _, s := range x.segs {
			t := strings.ReplaceAll(b.segText(s), "\t", " ")
			if t == "" {
				continue
			}
			switch {
			case len(out) == 0:
			case s.line != prevLine && endsWithSoftHyphen(out) && startsWithLetter([]byte(t)):
				out = out[:len(out)-2] // the word goes on
			case s.line != prevLine && endsWithHyphen(out) && startsWithLetter([]byte(t)):
			default:
				out = append(out, ' ')
			}
			out = append(out, t...)
			prevLine = s.line
		}
		x.text = string(out)
		x.words = len(strings.Fields(x.text))
		if x.text != "" {
			filled++
			words = append(words, x.words)
		} else {
			x.bold = false
		}
	}

	// Rows and columns with text in them: a cell's text is in its first.
	keepRow, keepCol := make([]bool, rows), make([]bool, cols)
	for k := range cells {
		if x := &cells[k]; x.text != "" {
			keepRow[x.r0], keepCol[x.c0] = true, true
		}
	}
	newRow, newCol := renumber(keepRow), renumber(keepCol)
	nr, nc := newRow[rows], newCol[cols]
	multi := 0
	for r := 0; r < rows; r++ {
		n := 0
		for k := range cells {
			if x := &cells[k]; x.text != "" && x.r0 == r {
				n++
			}
		}
		if n >= 2 {
			multi++
		}
	}
	slices.Sort(words)
	pageArea := b.page.Width * b.page.Height
	switch {
	case nr < 2 || nc < 2 || multi < 2:
		return blk{}, nil, false // no table of rows and columns
	case 4*filled < len(cells):
		return blk{}, nil, false // mostly empty: a chart's grid, a form's boxes
	case words[len(words)/2] > ruledWordsMax:
		return blk{}, nil, false // boxes around prose
	case len(cells) <= 4 && (X1-X0)*(Y1-Y0) > 0.5*pageArea:
		return blk{}, nil, false // the frame of a page
	case b.overPicture(box{X0, Y0, X1, Y1}):
		return blk{}, nil, false // lines on a picture
	}

	// The grid of the table.
	t := &gofastcore.Table{Rows: make([][]string, nr)}
	for r := range t.Rows {
		t.Rows[r] = make([]string, nc)
	}
	type span struct{ r0, r1, c0, c1 int } // in the kept rows and columns
	spans := make([]span, len(cells))
	for k := range cells {
		x := &cells[k]
		sp := span{newRow[x.r0], newRow[x.r1+1] - 1, newCol[x.c0], newCol[x.c1+1] - 1}
		spans[k] = sp
		if sp.r1 < sp.r0 || sp.c1 < sp.c0 {
			continue // all of it left out
		}
		if x.text != "" {
			t.Rows[sp.r0][sp.c0] = x.text
		}
		if sp.r1 > sp.r0 || sp.c1 > sp.c0 {
			t.Merges = append(t.Merges, gofastcore.Merge{Row: sp.r0, Col: sp.c0, RowSpan: sp.r1 - sp.r0 + 1, ColSpan: sp.c1 - sp.c0 + 1})
		}
	}
	slices.SortFunc(t.Merges, func(a, c gofastcore.Merge) int {
		if d := cmp.Compare(a.Row, c.Row); d != 0 {
			return d
		}
		return cmp.Compare(a.Col, c.Col)
	})

	// The header rows: the first, each with two cells or more of text
	// and all in bold, over rows that are not.
	head := 0
	for head < min(3, nr-1) {
		all, n := true, 0
		for k := range cells {
			if x := &cells[k]; x.text != "" && spans[k].r0 == head {
				all = all && x.bold
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
		for k := range cells {
			if x := &cells[k]; x.text != "" && spans[k].r0 >= head {
				body++
				if x.bold {
					bold++
				}
			}
		}
		if body == 0 || 5*bold > body {
			head = 0
		}
	}
	t.HeaderRows = head

	firstLine := &b.lines[mine[0]]
	bl := blk{grid: t, tlines: mine, line: -1, key: firstLine.key, size: firstLine.size,
		x0: X0, y0: Y0, x1: X1, y1: Y1}
	for _, li := range mine {
		ln := &b.lines[li]
		bl.key, bl.size = min(bl.key, ln.key), max(bl.size, ln.size)
	}
	return bl, mine, true
}

// splitRows splits the rows of a grid that hold rows of text. A band is
// rows that no cell reaches out of, one row or several that cells over
// several rows tie together. Where the cells with text in a band mostly
// have a line on nearly every baseline in it, each baseline is a row: a
// table ruled between its columns and not between its rows. A cell of
// several lines beside cells of one is no such band. It returns the
// cells, renumbered, and the rows there are now.
func (b *builder) splitRows(cells []ruledCell, rows int, xs []float64) ([]ruledCell, int) {
	mid := func(s seg) float64 { ln := &b.lines[s.line]; return (ln.y0 + ln.y1) / 2 }
	// The bands: end[r] is the last row of the band that starts at r.
	reach := make([]int, rows)
	for r := range reach {
		reach[r] = r
	}
	for k := range cells {
		x := &cells[k]
		reach[x.r0] = max(reach[x.r0], x.r1)
	}
	type band struct {
		r0, r1 int
		bl     []float64 // the baselines it is split at; nil if it is not
	}
	var bands []band
	for r := 0; r < rows; {
		end := reach[r]
		for q := r; q <= end; q++ {
			end = max(end, reach[q])
		}
		bands = append(bands, band{r0: r, r1: end})
		r = end + 1
	}
	split := false
	var ys []float64
	for n := range bands {
		bd := &bands[n]
		ys = ys[:0]
		size := 0.0
		for k := range cells {
			if x := &cells[k]; x.r0 >= bd.r0 && x.r0 <= bd.r1 {
				for _, s := range x.segs {
					ys = append(ys, mid(s))
					size = max(size, b.lines[s.line].size)
				}
			}
		}
		if len(ys) < 4 {
			continue
		}
		slices.Sort(ys)
		var bl []float64
		for _, y := range ys {
			if m := len(bl); m == 0 || y-bl[m-1] > 0.5*size {
				bl = append(bl, y)
			}
		}
		if len(bl) < 2 {
			continue
		}
		filled, full := 0, 0
		for k := range cells {
			x := &cells[k]
			if x.r0 < bd.r0 || x.r0 > bd.r1 || len(x.segs) == 0 {
				continue
			}
			filled++
			hit := 0
			for _, y := range bl {
				for _, s := range x.segs {
					if d := mid(s) - y; d >= 0 && d <= 0.5*size {
						hit++
						break
					}
				}
			}
			if 5*hit >= 4*len(bl) {
				full++
			}
		}
		if full >= 2 && 5*full >= 3*filled && !b.runOn(cells, bd.r0, bd.r1) {
			bd.bl, split = bl, true
		} else if bd.r0 == bd.r1 {
			keys := b.keyRows(cells, bd.r0, size, xs)
			if keys == nil {
				continue
			}
			bd.bl, split = keys, true
		}
	}
	if !split {
		return cells, rows
	}
	// The new rows: a band that is split is as many rows as baselines,
	// and its cells are cut along them; the rest keep their rows.
	start := make([]int, rows+1)
	of := make([]int, rows) // the band of each row
	at := 0
	for n, bd := range bands {
		for r := bd.r0; r <= bd.r1; r++ {
			of[r] = n
			start[r] = at
			if bd.bl == nil {
				at++
			}
		}
		if bd.bl != nil {
			at += len(bd.bl)
		}
	}
	start[rows] = at
	out := make([]ruledCell, 0, len(cells))
	for _, x := range cells {
		bd := &bands[of[x.r0]]
		if bd.bl == nil {
			x.r0, x.r1 = start[x.r0], start[x.r1+1]-1
			out = append(out, x)
			continue
		}
		parts := make([]ruledCell, len(bd.bl))
		for i := range parts {
			parts[i] = ruledCell{r0: start[bd.r0] + i, r1: start[bd.r0] + i, c0: x.c0, c1: x.c1, bold: true}
		}
		for _, s := range x.segs {
			y, i := mid(s), 0
			for i+1 < len(bd.bl) && y-bd.bl[i+1] >= -0.25*b.lines[s.line].size {
				i++
			}
			parts[i].segs = append(parts[i].segs, s)
			parts[i].bold = parts[i].bold && s.bold
		}
		for i := range parts {
			parts[i].bold = parts[i].bold && len(parts[i].segs) > 0
		}
		out = append(out, parts...)
	}
	return out, at
}

// keyRows returns the baselines of the lines in the first column of
// row r, a row that no cell reaches out of, when each of them begins a
// row of text: an entry of several lines beside a code of one, in a
// table ruled between its columns and not between its rows. It returns
// nil unless there are two lines or more in that column, each of four
// words or fewer and none going on from the one over it as the lines of
// a sentence do; the next column has a line on each of their baselines;
// and no line of another cell on such a baseline goes on from the one
// over it: one left open, ending with a comma or a little word, or one
// that fills most of the cell when the line starts with a small letter.
// A value such as a unit of measure can start with one.
//
// xs are the places of the columns.
func (b *builder) keyRows(cells []ruledCell, r int, size float64, xs []float64) []float64 {
	key, next := -1, -1
	for k := range cells {
		if x := &cells[k]; x.r0 == r && len(x.segs) > 0 && (key < 0 || x.c0 < cells[key].c0) {
			key = k
		}
	}
	if key < 0 || len(cells[key].segs) < 2 {
		return nil
	}
	for k := range cells {
		if x := &cells[k]; x.r0 == r && len(x.segs) > 0 && x.c0 > cells[key].c1 && (next < 0 || x.c0 < cells[next].c0) {
			next = k
		}
	}
	if next < 0 {
		return nil
	}
	lines := b.cellLines(cells[key].segs, size)
	if len(lines) < 2 {
		return nil
	}
	keys := make([]float64, len(lines))
	for i, ln := range lines {
		if len(strings.Fields(ln.text)) > 4 || i > 0 && runsOn(lines[i-1].text, ln.text) {
			return nil
		}
		keys[i] = ln.y
	}
	beside := make([]bool, len(keys))
	for k := range cells {
		x := &cells[k]
		if k == key || x.r0 != r || len(x.segs) == 0 {
			continue
		}
		lines := b.cellLines(x.segs, size)
		for i, ln := range lines {
			at := slices.IndexFunc(keys, func(y float64) bool { return abs(ln.y-y) <= 0.5*size })
			if at < 0 {
				continue
			}
			if k == next {
				beside[at] = true
			}
			if at == 0 || i == 0 {
				continue
			}
			up := &lines[i-1]
			first, _ := utf8.DecodeRuneInString(ln.text)
			if endsOpen(up.text) || unicode.IsLower(first) && up.u1-up.u0 >= 0.6*(xs[x.c1+1]-xs[x.c0]) {
				return nil // a sentence that goes on across the rows
			}
		}
	}
	if slices.Contains(beside, false) {
		return nil
	}
	return keys
}

// textLine is a line of the text of a cell: where its middle is, across
// the line, where it begins and ends along it, and its text.
type textLine struct {
	y, u0, u1 float64 // u0 and u1: where it begins and ends along the line
	text      string
}

// cellLines returns the lines of the segments of a cell, from the top:
// those whose middles are within half of size of each other are one.
func (b *builder) cellLines(segs []seg, size float64) []textLine {
	mid := func(s seg) float64 { ln := &b.lines[s.line]; return (ln.y0 + ln.y1) / 2 }
	segs = slices.Clone(segs)
	slices.SortStableFunc(segs, func(a, c seg) int {
		if d := cmp.Compare(mid(a), mid(c)); d != 0 {
			return d
		}
		return cmp.Compare(a.u0, c.u0)
	})
	var out []textLine
	for _, s := range segs {
		y, t := mid(s), b.segText(s)
		if n := len(out); n > 0 && y-out[n-1].y <= 0.5*size {
			l := &out[n-1]
			l.text += " " + t
			l.u0, l.u1 = min(l.u0, s.u0), max(l.u1, s.u1)
			continue
		}
		out = append(out, textLine{y, s.u0, s.u1, t})
	}
	return out
}

// runOn reports whether the lines in the cells of rows r0 to r1 run on,
// from each to the next, as the lines of sentences do (runsOn): boxes
// around paragraphs, their lines side by side, are no rows.
func (b *builder) runOn(cells []ruledCell, r0, r1 int) bool {
	pairs, running := 0, 0
	for k := range cells {
		x := &cells[k]
		if x.r0 < r0 || x.r0 > r1 {
			continue
		}
		prev, prevLine := "", int32(-1)
		for _, s := range x.segs {
			t := b.segText(s)
			if t == "" {
				continue
			}
			if prev != "" && s.line != prevLine {
				pairs++
				if runsOn(prev, t) {
					running++
				}
			}
			prev, prevLine = t, s.line
		}
	}
	return pairs > 0 && 3*running >= pairs
}

// ruledSeg adds the pieces b.lidx[lo:hi] of line li to a cell.
func (b *builder) ruledSeg(x *ruledCell, li, lo, hi int32) {
	s := b.segment(li, lo, hi)
	x.segs = append(x.segs, s)
	x.bold = x.bold && s.bold
}

// renumber gives each index its place among those kept, and at the end
// how many are kept: n+1 entries for n.
func renumber(keep []bool) []int {
	out := make([]int, len(keep)+1)
	n := 0
	for i, k := range keep {
		out[i] = n
		if k {
			n++
		}
	}
	out[len(keep)] = n
	return out
}
