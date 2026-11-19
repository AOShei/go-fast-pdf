package layout

import (
	gofastcore "github.com/AOShei/go-fast-core"
	"github.com/AOShei/go-fast-pdf/internal/extract"
)

// The checks that need the shapes of the page: what colour lies under a
// run, whether a shape is painted over it, and whether a clipping path
// cuts it off. They run when Options.Deep is set.
//
// A shape is known exactly in one case: an upright rectangle filled with
// a colour, under a clip that is a rectangle too. Everything else that
// is painted (any other path, a stroke, a shading, a pattern, an image,
// anything painted through a soft mask) is a region: something is
// painted somewhere in it, and which part of it is left out is not
// known. The rules follow from that:
//
//   - The background of a run is the last thing painted before it that
//     touches it. If that is an exact, opaque rectangle of a known colour
//     which holds the whole run, its colour is the background; if nothing
//     touches the run, the white page is. In every other case the
//     background is not known and the contrast is not judged.
//   - Nor is it judged when a region painted after the run touches it.
//     What is painted there may go around the letters and make them
//     stand out: programs draw white text first and the dark ground it
//     stands on afterwards, through a mask that spares the letters. An
//     exact rectangle painted later does not have that effect: it lays
//     one colour over the text and what is around it alike.
//   - A run is covered when an exact, opaque rectangle painted after it
//     holds the whole run.
//   - A run is cut off when it lies wholly outside the box that the
//     clipping paths leave. For a clipping path that is not a rectangle
//     that box is larger than what the path leaves, so text inside the
//     box and outside the path is missed, and no visible text is flagged.
//
// "The run" in the first two rules is the band its small letters fill
// (see cover): what is under that band is what the text stands on, and a
// reader cannot read text whose band is covered. An underline, the rule
// of a table and the edge of the cell next to it do not reach into it.

// patch is something painted on the page, in displayed space.
type patch struct {
	box box
	seq int32
	// exact is set for an exact rectangle, solid when it is opaque as
	// well, and known when it is solid and its colour is known.
	exact, solid, known bool
	col                 extract.Color
}

// gridSide is the number of cells along each side of the grid that the
// shapes of a page are kept in.
const gridSide = 32

// maxDeepSteps bounds the work of the deep checks on one page, unless
// Options.MaxDeepSteps says otherwise: cells visited and shapes
// compared. A page that needs more is not checked to the end.
const maxDeepSteps = 1 << 25

type gridNode struct{ shape, next int32 }

// grid keeps shapes by the cells of the page they touch, each cell's
// shapes with the one painted last in front.
type grid struct {
	head   [gridSide * gridSide]int32
	stamp  [gridSide * gridSide]uint32
	epoch  uint32
	nodes  []gridNode
	free   int32
	cw, ch float64
	w, h   float64
	// all is the shape painted last that covers the whole page, -1 if
	// there is none; what was painted before it is forgotten.
	all int32
}

func (g *grid) reset(w, h float64) {
	g.clear(-1)
	g.w, g.h, g.cw, g.ch = w, h, w/gridSide, h/gridSide
}

// clear empties every cell. all is the shape that covers the page from
// now on, -1 for none.
func (g *grid) clear(all int32) {
	g.epoch++
	if g.epoch == 0 {
		g.stamp = [gridSide * gridSide]uint32{}
		g.epoch = 1
	}
	g.nodes, g.free, g.all = g.nodes[:0], -1, all
}

// cells returns the range of cells that a box touches, and false if it
// lies off the page.
func (g *grid) cells(bx box) (x0, y0, x1, y1 int, ok bool) {
	if !(bx.x1 > 0 && bx.y1 > 0 && bx.x0 < g.w && bx.y0 < g.h && bx.x1 > bx.x0 && bx.y1 > bx.y0) {
		return 0, 0, 0, 0, false
	}
	x0, y0 = int(max(bx.x0, 0)/g.cw), int(max(bx.y0, 0)/g.ch)
	x1, y1 = int(min(bx.x1, g.w)/g.cw), int(min(bx.y1, g.h)/g.ch)
	return min(x0, gridSide-1), min(y0, gridSide-1), min(x1, gridSide-1), min(y1, gridSide-1), true
}

func (g *grid) list(cell int) int32 {
	if g.stamp[cell] != g.epoch {
		return -1
	}
	return g.head[cell]
}

func (g *grid) push(cell int, shape, next int32) {
	n := g.free
	if n >= 0 {
		g.free = g.nodes[n].next
		g.nodes[n] = gridNode{shape, next}
	} else {
		n = int32(len(g.nodes))
		g.nodes = append(g.nodes, gridNode{shape, next})
	}
	g.head[cell], g.stamp[cell] = n, g.epoch
}

// add puts a shape into every cell it touches and returns the work that
// took. With replace set, the shape takes the place of everything in a
// cell that it covers entirely: whatever touches that cell touches the
// shape, and the shape was painted later.
func (g *grid) add(shape int32, bx box, replace bool) int {
	x0, y0, x1, y1, ok := g.cells(bx)
	if !ok {
		return 1
	}
	if replace && bx.x0 <= 0 && bx.y0 <= 0 && bx.x1 >= g.w && bx.y1 >= g.h {
		// It covers the page: start afresh.
		g.clear(shape)
		return 1
	}
	steps := 0
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			steps++
			cell := y*gridSide + x
			next := g.list(cell)
			if replace && bx.x0 <= float64(x)*g.cw && bx.x1 >= float64(x+1)*g.cw && bx.y0 <= float64(y)*g.ch && bx.y1 >= float64(y+1)*g.ch {
				for next >= 0 {
					steps++
					n := next
					next = g.nodes[n].next
					g.nodes[n].next, g.free = g.free, n
				}
			}
			g.push(cell, shape, next)
		}
	}
	return steps
}

func (a box) touches(o box) bool { return a.x0 < o.x1 && o.x0 < a.x1 && a.y0 < o.y1 && o.y0 < a.y1 }

func (a box) holds(o box) bool { return a.x0 <= o.x0 && a.y0 <= o.y0 && a.x1 >= o.x1 && a.y1 >= o.y1 }

// The band of a line that its small letters fill, as shares of the font
// size above the baseline, and how far it stops short of the ends of
// the run, as a share of the size along the line.
const (
	coverBelow = 0.05
	coverAbove = 0.55
	coverInset = 0.05
)

// cover returns the part of a run that decides whether it is seen: the
// band of its small letters. What covers that band hides the text, and
// what lies under it is its background.
func (b *builder) cover(p *piece) box {
	r := &b.c.Runs[p.run]
	lo, hi := -coverBelow*p.size, coverAbove*p.size
	if r.Vertical() {
		// The glyphs of a vertical line are centred on it.
		lo, hi = -0.3*p.size, 0.3*p.size
	}
	rise := r.Rise * b.scale
	lo, hi = lo+rise, hi+rise
	sx, sy, ex, ey := p.sx, p.sy, p.ex, p.ey
	length := hypot(ex-sx, ey-sy)
	if in := min(coverInset*p.em, length/4); length > 0 {
		ux, uy := (ex-sx)/length, (ey-sy)/length
		sx, sy, ex, ey = sx+ux*in, sy+uy*in, ex-ux*in, ey-uy*in
	}
	ax, ay, bx, by := p.nx*hi, p.ny*hi, p.nx*lo, p.ny*lo
	return box{
		min(sx+ax, sx+bx, ex+ax, ex+bx), min(sy+ay, sy+by, ey+ay, ey+by),
		max(sx+ax, sx+bx, ex+ax, ex+bx), max(sy+ay, sy+by, ey+ay, ey+by),
	}
}

// displayBox takes a box of the page's coordinates to displayed space.
func (b *builder) displayBox(x0, y0, x1, y1 float64) box {
	ax, ay := b.toDisplay(x0, y0)
	bx, by := b.toDisplay(x1, y1)
	return box{min(ax, bx), min(ay, by), max(ax, bx), max(ay, by)}
}

// region takes a clipping region to displayed space. ok is false for a
// region that nothing has narrowed.
func (b *builder) region(r *extract.Region) (bx box, ok bool) {
	if !r.Bounded {
		return box{}, false
	}
	return b.displayBox(r.X0, r.Y0, r.X1, r.Y1), true
}

// deep makes the checks that need the shapes of the page. It reports
// whether they were made to the end.
func (b *builder) deep() (complete bool) {
	c := b.c
	complete = c.ShapesUpTo < 0

	// Cut off by a clipping path: wholly outside the box it leaves.
	for i := range b.pieces {
		p := &b.pieces[i]
		if p.hidden&gofastcore.HiddenTextLayer != 0 {
			continue
		}
		if clip, ok := b.region(&c.Runs[p.run].Region); ok {
			if me := (box{p.x0, p.y0, p.x1, p.y1}); clip.area() <= 0 || !clip.touches(me) {
				p.hidden |= gofastcore.HiddenClipped
			}
		}
	}
	if !(b.page.Width > 0 && b.page.Height > 0) {
		return complete
	}

	// Everything painted that is not text, in the order painted.
	us := b.patches[:0]
	si, ii := 0, 0
	for si < len(c.Shapes) || ii < len(c.Images) {
		if ii >= len(c.Images) || si < len(c.Shapes) && c.Shapes[si].Seq <= c.Images[ii].Seq {
			s := &c.Shapes[si]
			si++
			u := patch{box: b.displayBox(s.Box[0], s.Box[1], s.Box[2], s.Box[3]), seq: s.Seq, exact: s.Exact}
			if s.Exact && s.Opaque {
				u.solid, u.known, u.col = true, s.Color.Known, s.Color
			}
			us = append(us, u)
			continue
		}
		img := &c.Images[ii]
		ii++
		_, bb := b.quadBox(img.Quad)
		if clip, ok := b.region(&img.Region); ok {
			bb = box{max(bb.x0, clip.x0), max(bb.y0, clip.y0), min(bb.x1, clip.x1), min(bb.y1, clip.y1)}
		}
		if bb.area() > 0 {
			us = append(us, patch{box: bb, seq: img.Seq})
		}
	}
	b.patches = us

	steps, maxSteps := 0, b.opts.MaxDeepSteps
	if maxSteps <= 0 {
		maxSteps = maxDeepSteps
	}

	// What is painted after each run: the runs from the last to the
	// first. An exact, opaque rectangle that holds a run covers it; one
	// that is smaller than every run covers none, and is left out. A
	// region that touches a run may show it for what it is.
	minW, minH := b.page.Width, b.page.Height
	for i := range b.pieces {
		q := b.cover(&b.pieces[i])
		minW, minH = min(minW, q.x1-q.x0), min(minH, q.y1-q.y0)
	}
	covers, regions := &b.grid, &b.regions
	covers.reset(b.page.Width, b.page.Height)
	regions.reset(b.page.Width, b.page.Height)
	k := len(us) - 1
	for i := len(b.pieces) - 1; i >= 0; i-- {
		p := &b.pieces[i]
		r := &c.Runs[p.run]
		for ; k >= 0 && us[k].seq >= r.Seq && steps <= maxSteps; k-- {
			switch u := &us[k]; {
			case !u.exact:
				steps += regions.add(int32(k), u.box, true)
			case u.solid && u.box.x1-u.box.x0 >= minW && u.box.y1-u.box.y0 >= minH:
				steps += covers.add(int32(k), u.box, false)
			}
		}
		if steps > maxSteps {
			// The runs before this one are judged by what is painted
			// before them alone, as they are without these checks.
			complete = false
			break
		}
		if p.hidden&gofastcore.HiddenTextLayer != 0 {
			continue
		}
		q := b.cover(p)
		touched, n := regions.touched(us, q)
		steps += n
		p.later = touched
		// Text that clips is not covered by what is painted through it.
		if r.Mode == 7 && r.ClipPainted {
			continue
		}
		covered, n := covers.covered(us, q)
		steps += n
		if covered {
			p.hidden |= gofastcore.HiddenOccluded
		}
	}

	// Backgrounds: the runs in the order drawn, each against what was
	// painted before it.
	g := &b.grid
	g.reset(b.page.Width, b.page.Height)
	k = 0
	for i := range b.pieces {
		p := &b.pieces[i]
		r := &c.Runs[p.run]
		for k < len(us) && us[k].seq < r.Seq && steps <= maxSteps {
			steps += g.add(int32(k), us[k].box, true)
			k++
		}
		fill, stroke := painted(r.Mode)
		if p.hidden&gofastcore.HiddenTextLayer != 0 || !(fill || stroke) || r.Blend {
			continue // not to be judged
		}
		if p.later {
			// What a reader sees of the run is not known. That takes back
			// what the check without shapes said of it.
			if p.hidden&gofastcore.HiddenLowContrast != 0 || r.Seq == 0 {
				p.hidden &^= gofastcore.HiddenLowContrast
				b.stats.Judged--
			}
			b.stats.Unjudged++
			continue
		}
		if r.Seq == 0 {
			continue // judged already: it stands on the white page
		}
		if steps > maxSteps || c.ShapesUpTo >= 0 && r.Seq > c.ShapesUpTo {
			// Something that is not known may lie under the run.
			complete = false
			b.stats.Unjudged++
			continue
		}
		q := b.cover(p)
		top, n := g.top(us, q)
		steps += n
		back := white
		if top >= 0 {
			u := &us[top]
			if !u.known || !u.box.holds(q) {
				b.stats.Unjudged++
				continue
			}
			back = u.col
		}
		b.stats.Judged++
		if faintOn(r, fill, stroke, back) {
			p.hidden |= gofastcore.HiddenLowContrast
		}
	}
	return complete
}

// touched reports whether a shape in the grid touches the box, and the
// work that took.
func (g *grid) touched(us []patch, q box) (touched bool, steps int) {
	if g.all >= 0 {
		return true, 1
	}
	x0, y0, x1, y1, ok := g.cells(q)
	if !ok {
		return false, 1
	}
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			steps++
			for n := g.list(y*gridSide + x); n >= 0; n = g.nodes[n].next {
				steps++
				if us[g.nodes[n].shape].box.touches(q) {
					return true, steps
				}
			}
		}
	}
	return false, steps
}

// top returns the shape painted last that touches the box, -1 if none
// does, and the work that took.
func (g *grid) top(us []patch, q box) (top int32, steps int) {
	top = g.all
	x0, y0, x1, y1, ok := g.cells(q)
	if !ok {
		return top, 1
	}
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			steps++
			for n := g.list(y*gridSide + x); n >= 0; n = g.nodes[n].next {
				steps++
				s := g.nodes[n].shape
				if s <= top {
					break // painted before what was found already, as are the rest
				}
				if us[s].box.touches(q) {
					top = s
					break
				}
			}
		}
	}
	return top, steps
}

// covered reports whether a shape in the grid holds the whole box, and
// the work that took. A shape that holds the box holds its middle, so
// the cell of the middle has it.
func (g *grid) covered(us []patch, q box) (covered bool, steps int) {
	mid := box{(q.x0 + q.x1) / 2, (q.y0 + q.y1) / 2, (q.x0+q.x1)/2 + 1e-9, (q.y0+q.y1)/2 + 1e-9}
	x, y, _, _, ok := g.cells(mid)
	if !ok {
		return false, 1
	}
	for n := g.list(y*gridSide + x); n >= 0; n = g.nodes[n].next {
		steps++
		if us[g.nodes[n].shape].box.holds(q) {
			return true, steps
		}
	}
	return false, steps + 1
}

var white = extract.Color{R: 255, G: 255, B: 255, Known: true}

// painted says which parts of a glyph a render mode paints.
func painted(mode uint8) (fill, stroke bool) {
	return mode == 0 || mode == 2 || mode == 4 || mode == 6, mode == 1 || mode == 2 || mode == 5 || mode == 6
}

// faintOn reports whether no painted part of a run can be told from the
// background it stands on. A part painted with opacity 0 is not there to
// be told from anything; a run that has no other part is transparent,
// which is another reason.
func faintOn(r *extract.Run, fill, stroke bool, back extract.Color) bool {
	seen := false
	if fill && r.FillAlpha > 0 {
		if !faint(r.Fill, r.FillAlpha, back) {
			return false
		}
		seen = true
	}
	if stroke && r.StrokeAlpha > 0 {
		if !faint(r.Stroke, r.StrokeAlpha, back) {
			return false
		}
		seen = true
	}
	return seen
}
