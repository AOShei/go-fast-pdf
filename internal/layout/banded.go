package layout

import (
	"slices"
)

// Tables between level rules.
//
// Many tables are drawn with rules across them and none between their
// columns: one over the table, one under its header, one at its end.
// Such rules make no grid (ruled.go), but they say where the table is:
// level rules of one width, one under the other, with rows of text set
// in columns between them. The rows between the first and the last of
// them are weighed as a table by themselves, apart from the text beside
// and around them, and their columns are the white space that runs down
// all of them, not that of the first row alone.
//
// The rules of one width are taken together. Between two of them that
// follow each other is a slab; a slab with a row of text in two places
// or more is part of a table, and slabs that follow each other are one
// table. A caption or a paragraph between two tables of one width is a
// slab that is not, and keeps them apart.
//
// A table with three rules or more is then a grid: its level rules, and
// an upright rule that is not on the page in the middle of each gutter
// (bandGrid). It is made a table as a grid that is drawn (ruledTable): a
// slab is a row, the lines of a cell are its text, and a slab that holds
// rows of text, as the body under a header does, is split into them.
// With two rules, or where that is no table, the rows of text are
// weighed as those of a table found by alignment (makeTable). Either
// way the rules say where to look, and do not make a table of prose.

const (
	// bandRuleMin is the shortest rule, in points, that can bound a table.
	bandRuleMin = 36.0
	// maxBandRules bounds the level rules of a page that are looked at.
	maxBandRules = 512
	// bandOver is how far past the ends of the rules a cell can reach,
	// as a share of their length.
	bandOver = 0.1
	// bandMinRows is the fewest rows of a table between rules.
	bandMinRows = 2
)

// ruleBand is the rows between level rules of one width that are weighed as
// a table, and their gutters.
type ruleBand struct {
	rows []trow
	g    []gutter
	// grid is the rules with an upright rule in each gutter, in every
	// slab that no text crosses it in: the table as its rules and the
	// white space between its columns draw it (bandGrid).
	grid grid
}

// bands returns the tables that the level rules of the page mark among
// rows, which are those of text set upright.
func (b *builder) bands(rows []trow) []ruleBand {
	if len(b.c.Rules) < 2 || len(rows) < bandMinRows {
		return nil
	}
	hs, _ := b.displayRules()
	rules := hs[:0:0]
	// The rules of a grid are not looked at again: the grid was a table,
	// and has its text, or it was found to be none (ruledTable).
	inGrid := func(h arule) bool {
		for _, g := range b.grids {
			for _, x := range g.hs {
				if x == h {
					return true
				}
			}
		}
		return false
	}
	for _, h := range hs {
		if h.hi-h.lo >= bandRuleMin && !inGrid(h) {
			rules = append(rules, h)
		}
	}
	if len(rules) < 2 || len(rules) > maxBandRules {
		return nil
	}
	// Those of one width together, from the top.
	slices.SortFunc(rules, func(a, c arule) int {
		switch {
		case a.lo < c.lo-2*ruleSnap:
			return -1
		case c.lo < a.lo-2*ruleSnap:
			return 1
		case a.at < c.at:
			return -1
		case a.at > c.at:
			return 1
		}
		return 0
	})
	var out []ruleBand
	done := make([]bool, len(rules))
	for i := range rules {
		if done[i] {
			continue
		}
		group := []arule{rules[i]}
		for k := i + 1; k < len(rules); k++ {
			if r := rules[k]; !done[k] && abs(r.lo-rules[i].lo) <= 2*ruleSnap && abs(r.hi-rules[i].hi) <= 2*ruleSnap {
				done[k] = true
				group = append(group, r)
			}
		}
		if len(group) < 2 {
			continue
		}
		slices.SortFunc(group, func(a, c arule) int {
			switch {
			case a.at < c.at:
				return -1
			case a.at > c.at:
				return 1
			}
			return 0
		})
		out = b.bandTables(out, rows, group)
	}
	return out
}

// bandTables adds the tables between the rules of group, which have one
// width and are in order from the top.
func (b *builder) bandTables(out []ruleBand, rows []trow, group []arule) []ruleBand {
	lo, hi := group[0].lo-ruleSnap, group[0].hi+ruleSnap
	over := bandOver * (hi - lo)
	// The rows of each slab, with the segments that are between the
	// ends of the rules. A line that reaches past them is not the
	// table's, and a slab with one is not either.
	var cur []trow
	var ends []int // where each slab of cur ends in it
	first := 0     // the rule over the first slab of cur
	flush := func(next int) {
		if len(cur) >= bandMinRows {
			if g := bandGutters(cur); len(g) > 0 {
				t := ruleBand{rows: cur, g: g}
				if b.slabsAreRows(cur, ends, g) {
					t.grid = bandGrid(cur, ends, g, group[first:first+len(ends)+1])
				}
				out = append(out, t)
			}
		}
		cur, ends, first = nil, nil, next
	}
	at := 0
	for s := 0; s+1 < len(group); s++ {
		top, bottom := group[s].at, group[s+1].at
		for at < len(rows) && rows[at].v <= top {
			at++
		}
		var slab []trow
		several, cut := 0, false
		for ; at < len(rows) && rows[at].v <= bottom; at++ {
			r := rows[at]
			var in []seg
			for _, sg := range r.segs {
				// A segment is the table's when its middle is between
				// the ends of the rules. Text is not measured to the
				// point, and a cell at the edge can reach past the rule a
				// little; text that reaches far past is not a cell, and
				// neither is a segment of a line that has text outside.
				if mid := (sg.u0 + sg.u1) / 2; mid < lo || mid > hi {
					continue
				}
				if sg.u0 < lo-over || sg.u1 > hi+over || !b.lineWithin(sg.line, lo, hi) {
					cut = true
					continue
				}
				in = append(in, sg)
			}
			if len(in) == 0 {
				continue
			}
			if len(in) >= 2 {
				several++
			}
			r.segs = in
			slab = append(slab, r)
		}
		if cut || several == 0 {
			flush(s + 1)
			continue
		}
		cur = append(cur, slab...)
		ends = append(ends, len(cur))
	}
	flush(0)
	return out
}

// slabsAreRows reports whether each slab of a band can be read as a row
// of a table or as rows of text, with nothing left open. A slab is one
// row when it has one line, or when its cells are set from its top down,
// each a paragraph of its own: lines one under the other at one pitch,
// from the first line of the slab. It is rows of text when all its
// columns with text have a line on each of its baselines, and the lines
// do not run on as those of sentences do: the body under a header. A
// slab that is neither, as a list of entries of one or two lines beside
// a column of codes is, cannot be told from the rules, and the band is
// then weighed by its rows of text alone.
func (b *builder) slabsAreRows(rows []trow, ends []int, g []gutter) bool {
	cols := len(g) + 1
	colOf := func(u float64) int {
		k := 0
		for k < len(g) && (g[k].u0+g[k].u1)/2 < u {
			k++
		}
		return k
	}
	from := 0
	for _, end := range ends {
		slab := rows[from:end]
		from = end
		if len(slab) < 2 {
			continue
		}
		pitch := slab[1].v - slab[0].v
		for i := 2; i < len(slab); i++ {
			pitch = min(pitch, slab[i].v-slab[i-1].v)
		}
		// For each column: its lines, and whether they stand one under
		// the other from the top of the slab.
		same, fromTop := true, true
		pairs, running := 0, 0
		for c := 0; c < cols; c++ {
			n, last, lastText := 0, -1, ""
			for i, r := range slab {
				text := ""
				for _, s := range r.segs {
					if colOf((s.u0+s.u1)/2) == c {
						if text != "" {
							text += " "
						}
						text += b.segText(s)
					}
				}
				if text == "" {
					continue
				}
				if n == 0 && i > 0 {
					fromTop = false
				}
				if last >= 0 {
					if slab[i].v-slab[last].v > 1.25*pitch {
						fromTop = false
					}
					pairs++
					if runsOn(lastText, text) {
						running++
					}
				}
				n, last, lastText = n+1, i, text
			}
			if n > 0 && n != len(slab) {
				same = false
			}
		}
		switch {
		case same && pairs > 0 && 3*running < pairs:
			// rows of text
		case fromTop:
			// one row, its cells of several lines
		default:
			return false
		}
	}
	return true
}

// bandGrid makes a grid of the level rules of a band and its gutters.
// The upright rules are not on the page: each stands in the middle of a
// gutter, in the slabs where no text reaches over it. Where text does,
// the cells on its two sides are one, as with a rule left out of a grid
// that is drawn. The level rules reach as far as the text does.
func bandGrid(rows []trow, ends []int, g []gutter, rules []arule) grid {
	lo, hi := rules[0].lo, rules[0].hi
	for _, r := range rows {
		for _, s := range r.segs {
			lo, hi = min(lo, s.u0-1), max(hi, s.u1+1)
		}
	}
	var out grid
	for _, r := range rules {
		out.hs = append(out.hs, arule{r.at, lo, hi})
	}
	for _, x := range g {
		mid := (x.u0 + x.u1) / 2
		from := 0
		for k, end := range ends {
			crossed := false
			for _, r := range rows[from:end] {
				for _, s := range r.segs {
					crossed = crossed || s.u0 < mid && s.u1 > mid
				}
			}
			if !crossed {
				out.vs = append(out.vs, arule{mid, rules[k].at, rules[k+1].at})
			}
			from = end
		}
	}
	return out
}

// lineWithin reports whether the segments of line li all have their
// middles between lo and hi along the lines: whether a table there can
// take the line whole.
func (b *builder) lineWithin(li int32, lo, hi float64) bool {
	ln := &b.lines[li]
	for k := ln.lo; k < ln.hi; k++ {
		p := &b.pieces[b.lidx[k]]
		if mid := (p.u0 + p.u1) / 2; mid < lo || mid > hi {
			return false
		}
	}
	return true
}

// bandGutters returns the gutters of rows: the stretches that the text
// of no row covers. A row whose text reaches from one column into the
// next holds a cell over several columns, as a header often does; such
// rows, a fifth of them at most, are found first, by the stretches that
// the others leave white, and do not count.
func bandGutters(rows []trow) []gutter {
	allow := len(rows) / 5
	if allow == 0 {
		return gutters(rows, nil, 0)
	}
	wide := gutters(rows, nil, allow)
	skip := make([]bool, len(rows))
	n := 0
	for i, r := range rows {
		for _, s := range r.segs {
			for _, x := range wide {
				// Over an end of the gutter, not inside it: text inside
				// is a column that few rows have text in.
				if s.u0 < x.u1 && s.u1 > x.u0 && (s.u0 < x.u0 || s.u1 > x.u1) {
					skip[i] = true
				}
			}
		}
		if skip[i] {
			n++
		}
	}
	if n > allow {
		return gutters(rows, nil, 0)
	}
	return gutters(rows, skip, 0)
}

// gutters returns the stretches between the text of rows that the
// segments of allow rows at most cover, leaving out the rows of skip.
func gutters(rows []trow, skip []bool, allow int) []gutter {
	type edge struct {
		u    float64
		open bool
	}
	var edges []edge
	size := 0.0
	for i, r := range rows {
		if skip != nil && skip[i] {
			continue
		}
		size = max(size, r.size)
		// The segments of a row are in order along it; where two
		// overlap, the row covers the stretch once.
		end := 0.0
		for k, s := range r.segs {
			u0 := s.u0
			if k > 0 && u0 < end {
				u0 = end
			}
			if s.u1 > u0 {
				edges = append(edges, edge{u0, true}, edge{s.u1, false})
			}
			end = max(end, s.u1)
		}
	}
	slices.SortFunc(edges, func(a, c edge) int {
		switch {
		case a.u < c.u:
			return -1
		case a.u > c.u:
			return 1
		case !a.open && c.open:
			return -1
		case a.open && !c.open:
			return 1
		}
		return 0
	})
	var g []gutter
	depth, from, started := 0, 0.0, false
	for _, e := range edges {
		if e.open {
			if depth == allow && started && e.u-from >= gutterMin*size {
				g = append(g, gutter{from, e.u})
			}
			depth++
			started = started || depth > allow
		} else {
			depth--
			if depth == allow {
				from = e.u
			}
		}
	}
	return g
}
