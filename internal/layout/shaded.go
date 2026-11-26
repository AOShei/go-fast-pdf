package layout

import (
	"cmp"
	"slices"
)

// Tables drawn with shades.
//
// Many tables have no rules between their rows, or none at all: each
// cell is drawn as a box filled with a shade, the cells of a row side by
// side, and the eye takes the edges of the boxes for the lines of the
// table. The boxes a page fills (extract.Content.Fills) are looked at
// for that alone: a box that stands edge to edge with another of the
// same height, beside it, is the ground of a cell, and its four edges
// are rules. A box by itself is a panel, a bar of a chart, the ground of
// a page; boxes one over the other of one width are the lines of a
// shaded paragraph as often as the rows of a table, and say nothing.
// What a box is filled with is not looked at.
//
// The edges make grids with the rules of the page (ruleGrids), and a
// grid is a table as one drawn with rules is (ruledTable). Such grids
// are tried after those of the rules alone, for the text that those
// left: where rules draw a table, they say more than shades do. A box
// inside a grid of the rules is not looked at at all.

// tileSnap is how close, in points, the edges of two boxes are that
// stand edge to edge. The boxes of one row are drawn from the same
// numbers; a rule is drawn over an edge, not beside it.
const tileSnap = 0.75

// shadedGrids returns the grids that the edges of the page's cell
// grounds make with its rules, found once for the page.
func (b *builder) shadedGrids() []grid {
	if b.shadedDone {
		return b.shaded
	}
	b.shadedDone = true
	fh, fv := b.tileEdges()
	if len(fh) == 0 {
		return nil
	}
	hs, vs := b.displayRules()
	hs, vs = joinRules(append(hs, fh...)), joinRules(append(vs, fv...))
	vs2 := bridgeRules(vs, hs)
	hs, vs = bridgeRules(hs, vs), vs2
	if len(hs) < 2 || len(vs) < 2 {
		return nil
	}
	b.shaded = ruleGrids(hs, vs)
	return b.shaded
}

// tileEdges returns the edges of the filled boxes that are the grounds
// of cells, as rules in displayed space: the level ones and the upright
// ones.
func (b *builder) tileEdges() (hs, vs []arule) {
	if len(b.c.Fills) < 2 {
		return nil, nil
	}
	// The boxes inside a grid of the page's rules are left out: the grid
	// was a table, and has its text, or it was found to be none
	// (ruledTable), and the shades in it change neither.
	var within []box
	for _, g := range b.grids {
		e := box{g.hs[0].lo, g.vs[0].lo, g.hs[0].hi, g.vs[0].hi}
		for _, h := range g.hs {
			e.x0, e.x1 = min(e.x0, h.lo), max(e.x1, h.hi)
		}
		for _, v := range g.vs {
			e.y0, e.y1 = min(e.y0, v.lo), max(e.y1, v.hi)
		}
		within = append(within, e)
	}
	bs := make([]box, 0, len(b.c.Fills))
fills:
	for _, r := range b.c.Fills {
		x0, y0 := b.toDisplay(r[0], r[1])
		x1, y1 := b.toDisplay(r[2], r[3])
		a := box{min(x0, x1), min(y0, y1), max(x0, x1), max(y0, y1)}
		mx, my := (a.x0+a.x1)/2, (a.y0+a.y1)/2
		for _, e := range within {
			if mx > e.x0 && mx < e.x1 && my > e.y0 && my < e.y1 {
				continue fills
			}
		}
		bs = append(bs, a)
	}
	// Those of one top together, from the left: a box beside another
	// follows it there.
	slices.SortFunc(bs, func(a, c box) int {
		if d := cmp.Compare(a.y0, c.y0); d != 0 {
			return d
		}
		return cmp.Compare(a.x0, c.x0)
	})
	near := func(a, c float64) bool { return a-c <= tileSnap && c-a <= tileSnap }
	tile := make([]bool, len(bs))
	for i := range bs {
		a := &bs[i]
		for k := i + 1; k < len(bs) && bs[k].y0-a.y0 <= tileSnap; k++ {
			if c := &bs[k]; near(a.y1, c.y1) && (near(a.x1, c.x0) || near(a.x0, c.x1)) {
				tile[i], tile[k] = true, true
			}
		}
	}
	// The rows of cells: the tiles of one top and bottom, each beside
	// the last, in the order of the sort.
	type tileRow struct {
		y0, y1 float64
		xs     []float64 // the edges of its cells, from the left
	}
	var rows []tileRow
	for i, a := range bs {
		if !tile[i] {
			continue
		}
		hs = append(hs, arule{a.y0, a.x0, a.x1}, arule{a.y1, a.x0, a.x1})
		vs = append(vs, arule{a.x0, a.y0, a.y1}, arule{a.x1, a.y0, a.y1})
		if n := len(rows); n > 0 {
			if r := &rows[n-1]; near(r.y0, a.y0) && near(r.y1, a.y1) && near(r.xs[len(r.xs)-1], a.x0) {
				r.xs = append(r.xs, a.x1)
				continue
			}
		}
		rows = append(rows, tileRow{a.y0, a.y1, []float64{a.x0, a.x1}})
	}
	// A row of cells under another with the same edges is a row of the
	// same table, and what stands between them is rows that are not
	// shaded, when its text keeps to the columns: the edges go on from
	// the one to the other. Text that runs across an edge there is a
	// note or a title between two tables.
	for i := 0; i+1 < len(rows); i++ {
		a, c := &rows[i], &rows[i+1]
		if c.y0 < a.y1+tileSnap || len(a.xs) != len(c.xs) {
			continue
		}
		same := true
		for k := range a.xs {
			same = same && near(a.xs[k], c.xs[k])
		}
		if !same || b.crossed(a.xs[1:len(a.xs)-1], a.y1, c.y0) {
			continue
		}
		for _, x := range a.xs {
			vs = append(vs, arule{x, a.y1, c.y0})
		}
	}
	return hs, vs
}

// crossed reports whether a piece of text between y0 and y1 down the
// page reaches over one of the places xs by more than the snap of a
// rule on each side.
func (b *builder) crossed(xs []float64, y0, y1 float64) bool {
	for i := range b.pieces {
		p := &b.pieces[i]
		if m := (p.y0 + p.y1) / 2; m <= y0 || m >= y1 {
			continue
		}
		for _, x := range xs {
			if p.x0 < x-ruleSnap && p.x1 > x+ruleSnap {
				return true
			}
		}
	}
	return false
}
