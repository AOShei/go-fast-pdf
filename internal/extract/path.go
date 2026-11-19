package extract

import (
	"math"

	"github.com/AOShei/go-fast-pdf/internal/content"
	"github.com/AOShei/go-fast-pdf/internal/syntax"
)

// Paths are followed for two things. A path that becomes a clipping path
// narrows the region that is painted on, which decides what an image
// covers. And, when shapes are asked for, every path that is painted is
// recorded with its box, as something that may lie under text or over
// it.
//
// Most paths are neither a clip nor wanted as a shape, and a page can
// have a million of them. So nothing is converted while a path is
// built: the operands of its first operators are kept as they stand in
// the stream, which is enough to tell a rectangle, and are read only
// when the path turns out to matter.

// Shape is something painted that is neither text nor an image: a path
// that is filled or stroked, or a shading.
type Shape struct {
	// Box is x0, y0, x1, y1 in the page's coordinates. The shape paints
	// inside it, cut to the clipping region already.
	Box [4]float64
	// Seq is the number of things other than text painted before it.
	Seq int32
	// Color is the colour of an exact shape.
	Color Color
	// Exact is set when the shape lays one colour over the whole of its
	// box: an upright rectangle, under a clip that is one too, filled
	// with a colour and not a pattern, and not through a soft mask. Any
	// other shape paints somewhere in its box, in a way that is not
	// followed: it may leave out any part of it.
	Exact bool
	// Opaque is set for an exact shape that lets nothing under it show:
	// its opacity is 1 and it is painted with the normal blend mode.
	Opaque bool
}

// maxShapesPerPage bounds the shapes recorded for one page, unless
// Options.MaxShapes says otherwise.
const maxShapesPerPage = 1 << 16

// keptOperands is the number of operands of a path that are kept: a
// rectangle has four, and four corners and the way back to the first
// have ten.
const keptOperands = 10

// pathState is the path being built.
type pathState struct {
	// ops is the number of operators that built the path so far.
	ops int32
	// n is the number of operands kept in raw. They are those of the
	// first operators: the four of a rectangle when rect is set, and
	// otherwise the points of a move and the lines after it.
	n    int8
	rect bool
	// plain is set while the path is nothing but what raw holds.
	plain bool
	// clip is set by W and W*: the path becomes part of the clipping
	// path when it ends.
	clip bool
	raw  [keptOperands][]byte
	// The box of the points that are not in raw, when shapes are asked
	// for, in the coordinates the path is built in.
	more bool
	box  [4]float64
}

func (p *pathState) reset() {
	p.ops, p.n, p.rect, p.plain, p.clip, p.more = 0, 0, false, false, false, false
}

// keep stores operands, if there is room, and reports whether it did.
func (p *pathState) keep(a []content.Operand) bool {
	if int(p.n)+len(a) > keptOperands {
		return false
	}
	for i := range a {
		p.raw[int(p.n)+i] = a[i].Raw
	}
	p.n += int8(len(a))
	return true
}

func (p *pathState) point(x, y float64) {
	if !p.more {
		p.box, p.more = [4]float64{x, y, x, y}, true
		return
	}
	p.box[0], p.box[1] = min(p.box[0], x), min(p.box[1], y)
	p.box[2], p.box[3] = max(p.box[2], x), max(p.box[3], y)
}

// pathOp handles an operator that builds the path. kind is 'm', 'l', 'r'
// for a rectangle or 'c' for a curve, and n the number of its operands.
func (in *interp) pathOp(kind byte, args []content.Operand, n int) {
	// An operator without its numbers builds nothing.
	a := last(args, n)
	if a == nil {
		return
	}
	for i := range a {
		if a[i].Kind != content.Number {
			return
		}
	}
	p := &in.path
	p.ops++
	switch {
	case p.ops == 1 && (kind == 'm' || kind == 'r'):
		if p.keep(a) {
			p.rect, p.plain = kind == 'r', true
			return
		}
	case p.plain && !p.rect && kind == 'l':
		if p.keep(a) {
			return
		}
	}
	// The path is more than a rectangle or four corners. Where it lies
	// matters only when shapes are recorded.
	p.plain = false
	if !in.opts.Shapes {
		return
	}
	if kind == 'r' {
		x, y := finite(a[0].Float()), finite(a[1].Float())
		p.point(x, y)
		p.point(x+finite(a[2].Float()), y+finite(a[3].Float()))
		return
	}
	// The control points of a curve bound it.
	for i := 0; i+1 < n; i += 2 {
		p.point(finite(a[i].Float()), finite(a[i+1].Float()))
	}
}

func rawNumber(raw []byte) float64 {
	v, _ := syntax.ParseNumber(raw, 0)
	return finite(v.F)
}

// uprightSlack is how far, in points, the corners of a rectangle may be
// from where an upright one has them.
const uprightSlack = 1e-3

// outline says where the path lies on the page: its box, and whether
// the path is that box, an upright rectangle. known is false when
// nothing was kept of the path's extent. hollow is set for a path of
// fewer than three points: it encloses nothing, so that it fills nothing
// and, as a clipping path, leaves nothing.
func (in *interp) outline() (box [4]float64, rect, known, hollow bool) {
	p := &in.path
	c := &in.gs.ctm
	box = [4]float64{math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)}
	add := func(x, y float64) (float64, float64) {
		x, y = c.apply(x, y)
		box[0], box[1], box[2], box[3] = min(box[0], x), min(box[1], y), max(box[2], x), max(box[3], y)
		return x, y
	}
	var pts [5][2]float64
	n := 0
	if p.rect && p.n >= 4 {
		x, y := rawNumber(p.raw[0]), rawNumber(p.raw[1])
		w, h := rawNumber(p.raw[2]), rawNumber(p.raw[3])
		for i, q := range [4][2]float64{{x, y}, {x + w, y}, {x + w, y + h}, {x, y + h}} {
			pts[i][0], pts[i][1] = add(q[0], q[1])
		}
		n = 4
	} else {
		for i := 0; i+1 < int(p.n); i += 2 {
			pts[n][0], pts[n][1] = add(rawNumber(p.raw[i]), rawNumber(p.raw[i+1]))
			n++
		}
	}
	if !p.plain {
		if !in.opts.Shapes {
			return box, false, false, false
		}
		if p.more {
			for _, q := range [4][2]float64{{p.box[0], p.box[1]}, {p.box[2], p.box[1]}, {p.box[2], p.box[3]}, {p.box[0], p.box[3]}} {
				add(q[0], q[1])
			}
		}
		return box, false, n > 0 || p.more, false
	}
	if n < 3 {
		return box, false, n > 0, true
	}
	// Four corners, with or without the line back to the first: an
	// upright rectangle when each side runs along an axis.
	if n == 5 && near(pts[4][0], pts[0][0]) && near(pts[4][1], pts[0][1]) {
		n = 4
	}
	if n == 4 {
		a, b, d, e := pts[0], pts[1], pts[2], pts[3]
		rect = near(a[0], b[0]) && near(b[1], d[1]) && near(d[0], e[0]) && near(e[1], a[1]) ||
			near(a[1], b[1]) && near(b[0], d[0]) && near(d[1], e[1]) && near(e[0], a[0])
	}
	return box, rect, true, false
}

func near(a, b float64) bool { return a-b <= uprightSlack && b-a <= uprightSlack }

// endPath handles the operator that ends a path: it is filled, stroked,
// both or neither. The path is recorded as a shape if shapes are asked
// for, and narrows the clipping region if W came before. A path is
// painted with the clip that was in force before it.
func (in *interp) endPath(fill, stroke bool) {
	p := &in.path
	shape := in.opts.Shapes && (fill || stroke) && in.offDepth == 0
	if p.ops == 0 || !p.clip && !shape {
		p.reset()
		return
	}
	box, rect, known, hollow := in.outline()
	if shape && known {
		in.addShape(box, rect, fill && !hollow, stroke)
	}
	if p.clip {
		g := &in.gs
		switch {
		case hollow:
			g.region.cut(box[0], box[1], box[0], box[1], true)
		case known:
			g.region.cut(box[0], box[1], box[2], box[3], rect)
		default:
			g.region.Inexact = true
		}
	}
	p.reset()
}

// addShape records a path that is painted.
func (in *interp) addShape(box [4]float64, rect, fill, stroke bool) {
	g := &in.gs
	// How wide the stroke is on the page.
	var width float64
	if stroke && g.strokeAlpha*g.alphaScale > 0 {
		width = g.lineWidth * math.Sqrt(abs(g.ctm[0]*g.ctm[3]-g.ctm[1]*g.ctm[2]))
	}
	alpha := g.fillAlpha * g.alphaScale
	if !fill || alpha <= 0 {
		if width <= 0 {
			return // nothing is painted
		}
		fill = false
	}
	exact := fill && rect && !g.region.Inexact && g.fill.space != csPattern && !g.softMask && !g.outerBlend
	if width > 0 {
		// A stroke paints along the outline, to either side of it, in a
		// colour of its own.
		in.pushShape(Shape{Box: [4]float64{box[0] - width, box[1] - width, box[2] + width, box[3] + width}})
		if !exact {
			return
		}
		// What the stroke leaves of the rectangle is the fill.
		box = [4]float64{box[0] + width/2, box[1] + width/2, box[2] - width/2, box[3] - width/2}
	}
	s := Shape{Box: box}
	if exact {
		s.Exact, s.Color = true, g.fill.color
		s.Opaque = alpha >= 1 && !g.blendMode
	}
	in.pushShape(s)
}

// shade records a shading painted with sh: it fills the clipping region.
func (in *interp) shade() {
	if !in.opts.Shapes || in.offDepth > 0 {
		return
	}
	in.pushShape(Shape{Box: [4]float64{-farAway, -farAway, farAway, farAway}})
}

// farAway bounds the box of a shape that has no bounds of its own.
const farAway = 1e12

// pushShape cuts a shape to the clipping region and records what is
// left of it.
func (in *interp) pushShape(s Shape) {
	if r := &in.gs.region; r.Bounded {
		s.Box = [4]float64{max(s.Box[0], r.X0), max(s.Box[1], r.Y0), min(s.Box[2], r.X1), min(s.Box[3], r.Y1)}
	}
	if !(s.Box[2] > s.Box[0] && s.Box[3] > s.Box[1]) {
		return // nothing of it is on the page; also a box that is not a number
	}
	if len(in.shapes) >= in.opts.MaxShapes {
		if in.shapesUpTo < 0 {
			in.shapesUpTo = in.seq
		}
		return
	}
	s.Seq = in.seq
	in.shapes = append(in.shapes, s)
}
