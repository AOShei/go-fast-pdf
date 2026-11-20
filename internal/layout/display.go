package layout

import (
	"math"

	gofastcore "github.com/AOShei/go-fast-core"
	"github.com/AOShei/go-fast-pdf/internal/extract"
)

// The part of the font size a line of text takes above and below its
// baseline. The interpreter uses the same shares to tell lines apart.
const (
	glyphAscent  = 0.80
	glyphDescent = 0.20
)

type box struct{ x0, y0, x1, y1 float64 }

func (a box) area() float64 { return max(a.x1-a.x0, 0) * max(a.y1-a.y0, 0) }

func (a box) contains(x, y float64) bool { return x >= a.x0 && x <= a.x1 && y >= a.y0 && y <= a.y1 }

func hypot(x, y float64) float64 { return math.Sqrt(x*x + y*y) }

func (b *builder) toDisplay(x, y float64) (float64, float64) {
	m := &b.page.M
	return m[0]*x + m[2]*y + m[4], m[1]*x + m[3]*y + m[5]
}

// direction takes a unit vector of the page's coordinates to displayed
// space.
func (b *builder) direction(x, y float64) (float64, float64) {
	m := &b.page.M
	dx, dy := m[0]*x+m[2]*y, m[1]*x+m[3]*y
	if l := hypot(dx, dy); l > 0 {
		return dx / l, dy / l
	}
	return 1, 0
}

func (b *builder) quadBox(q extract.Quad) (out [8]float64, bb box) {
	bb = box{math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)}
	for i := 0; i < 8; i += 2 {
		x, y := b.toDisplay(q[i], q[i+1])
		out[i], out[i+1] = x, y
		bb.x0, bb.y0, bb.x1, bb.y1 = min(bb.x0, x), min(bb.y0, y), max(bb.x1, x), max(bb.y1, y)
	}
	return out, bb
}

// inside reports whether the point lies in the convex quadrilateral q.
func inside(q *[8]float64, x, y float64) bool {
	pos, neg := false, false
	for i := 0; i < 8; i += 2 {
		j := (i + 2) % 8
		cross := (q[j]-q[i])*(y-q[i+1]) - (q[j+1]-q[i+1])*(x-q[i])
		if cross > 1e-9 {
			pos = true
		} else if cross < -1e-9 {
			neg = true
		}
	}
	return !(pos && neg)
}

// prepare places every run in displayed space and gives it its display
// reasons.
func (b *builder) prepare() {
	c := b.c
	for i := range c.Runs {
		r := &c.Runs[i]
		if r.Len == 0 {
			continue
		}
		var p piece
		p.run = int32(i)
		p.dx, p.dy = b.direction(r.Dx, r.Dy)
		nx, ny := b.direction(r.Nx, r.Ny)
		p.nx, p.ny = nx, ny
		p.sx, p.sy = b.toDisplay(r.X0, r.Y0)
		ex, ey := b.toDisplay(r.X1, r.Y1)
		if r.Back {
			// Drawn from right to left: from where its first string ends
			// to where its last begins.
			p.sx, p.sy = b.toDisplay(r.RX, r.RY)
			ex, ey = b.toDisplay(r.LX, r.LY)
		}
		p.ex, p.ey = ex, ey
		p.size, p.em = r.Size*b.scale, r.Em()*b.scale

		// The box: the baseline, moved by the rise, with the glyphs'
		// height above it and their depth below.
		up, down := glyphAscent*p.size, glyphDescent*p.size
		if r.Vertical() {
			up, down = p.size/2, p.size/2
		}
		rise := r.Rise * b.scale
		p.up, p.down = rise+up, rise-down
		ax, ay := nx*p.up, ny*p.up
		bx, by := nx*p.down, ny*p.down
		p.x0 = min(p.sx+ax, p.sx+bx, ex+ax, ex+bx)
		p.x1 = max(p.sx+ax, p.sx+bx, ex+ax, ex+bx)
		p.y0 = min(p.sy+ay, p.sy+by, ey+ay, ey+by)
		p.y1 = max(p.sy+ay, p.sy+by, ey+ay, ey+by)

		// The direction of writing, in steps of five degrees. Nearly all
		// text is written along the page's x axis.
		if p.dx < 0.9999 {
			deg := math.Atan2(p.dy, p.dx) * 180 / math.Pi
			p.class = uint8(int(math.Round(deg/5)+72) % 72)
		}
		p.display = display(r)
		// Text in a cell of a table is the table's, unless the file
		// declares it not shown: that stays out of the table, laid out
		// with the rest of its set.
		if b.opts.Cell != nil && p.display == 0 {
			if k := b.opts.Cell(r.MCID); k >= 0 && int(k) < len(b.opts.CellTable) && b.opts.CellTable[k] >= 0 && int(b.opts.CellTable[k]) < b.opts.Tables {
				p.cell = k + 1
			}
		}
		b.pieces = append(b.pieces, p)
	}
	b.markDuplicates()
}

// display returns what the file declares about how a run is shown.
//
//   - hidden: the run is in optional content (a layer) that is off in
//     the document's default configuration, directly or through a
//     membership dictionary (internal/extract).
//   - no_ink: the run is drawn with no paint. Its render mode is 3,
//     neither fill nor stroke, or 7, clip only, when nothing is painted
//     through that clip; or the opacity of what its mode paints is 0,
//     the fill's for the modes that fill and the stroke's for those that
//     stroke, as the graphics state sets it (/ca, /CA).
//
// Nothing else is looked at: not colour, size, position or what is
// painted over the run.
func display(r *extract.Run) gofastcore.Display {
	var d gofastcore.Display
	if r.OptionalOff {
		d |= gofastcore.DisplayHidden
	}
	fill, stroke := painted(r.Mode)
	switch {
	case r.Mode == 3 || r.Mode == 7 && !r.ClipPainted:
		d |= gofastcore.DisplayNoInk
	case (fill || stroke) && (!fill || r.FillAlpha <= 0) && (!stroke || r.StrokeAlpha <= 0):
		d |= gofastcore.DisplayNoInk
	}
	return d
}

// painted reports which parts of a glyph a text render mode paints.
func painted(mode uint8) (fill, stroke bool) {
	switch mode {
	case 0, 4:
		return true, false
	case 1, 5:
		return false, true
	case 2, 6:
		return true, true
	}
	return false, false
}

// overprintSlack is how far from a run the same text may be printed
// again and be the same text: bold type made by printing twice, a
// shadow. It is a share of the font size, as in the interpreter.
const overprintSlack = 0.12

// markDuplicates finds text printed twice in the same place, wherever
// the two are in the order of drawing, and marks the later one. The
// interpreter does this for a string that repeats the one before it;
// here a whole line or page that is printed and printed again is found.
func (b *builder) markDuplicates() {
	n := len(b.pieces)
	if n < 2 {
		return
	}
	b.sorted = b.sorted[:0]
	for i := range b.pieces {
		b.sorted = append(b.sorted, int32(i))
	}
	// Copies start at the same point.
	sortStable(b.sorted, func(i, j int32) bool { return b.pieces[i].sx < b.pieces[j].sx })
	b.twins()
}

// twins goes through the pieces in the order of b.sorted, which is by
// the point where they start, and marks those that repeat another.
func (b *builder) twins() {
	n := len(b.sorted)
	at := func(p *piece) (float64, float64) { return p.sx, p.sy }
	for i, ai := range b.sorted {
		a := &b.pieces[ai]
		slack := overprintSlack * a.em
		ax, ay := at(a)
		// A bound on the neighbours looked at keeps a page that draws
		// everything at one point from costing the square of its size.
		for k := i + 1; k < n && k <= i+32; k++ {
			o := &b.pieces[b.sorted[k]]
			ox, oy := at(o)
			if ox-ax > slack {
				break
			}
			if o.dup || a.dup || math.Abs(oy-ay) > slack {
				continue
			}
			if drop := b.twin(ai, b.sorted[k]); drop != nil {
				drop.dup = true
				b.stats.Duplicates++
			}
		}
	}
}

// twin says which of two pieces that start at the same point, or end at
// the same point, is to be left out as a repetition of the other: nil
// if neither.
func (b *builder) twin(ai, oi int32) *piece {
	a, o := &b.pieces[ai], &b.pieces[oi]
	ra, ro := &b.c.Runs[a.run], &b.c.Runs[o.run]
	if ro.Font != ra.Font || math.Abs(o.size-a.size) > 0.001*a.size || o.dx*a.dx+o.dy*a.dy < 0.999 {
		return nil
	}
	ta, to := b.runText(a.run), b.runText(o.run)
	if len(ta) == len(to) {
		if string(ta) != string(to) {
			return nil
		}
		// Of two copies, the later one goes. Of a copy without a display
		// reason and one with, the one with goes: it holds nothing that is
		// not in the other, in the same place. Two copies with different
		// reasons both stay.
		switch {
		case o.display == a.display:
			if a.run > o.run {
				return a
			}
			return o
		case a.display == 0:
			return o
		case o.display == 0:
			return a
		}
	}
	return nil
}
