package extract

import (
	"bytes"
	"math"

	"github.com/AOShei/go-fast-pdf/internal/content"
	"github.com/AOShei/go-fast-pdf/internal/syntax"
)

// Rules: the thin straight lines and bars that a page paints along its
// axes, which is what the borders of a table are drawn with. Nothing
// else of a path is followed: not curves, not shapes, not colours, not
// clipping. A path is read from the raw text of its operands, and a
// number is converted only where the text shows that a line runs along
// an axis (one coordinate written the same at both ends) or for a
// rectangle: the thousands of short strokes of a map or a drawing are
// passed over at the cost of comparing a few bytes.

const (
	// MaxRuleWidth is the thickest a rule is across, in points.
	MaxRuleWidth = 3
	// maxRules bounds the rules recorded for one page, and maxPathRules
	// the candidates of one path.
	maxRules     = 1 << 16
	maxPathRules = 1 << 14
	// maxRuleSegs bounds the segments along an axis that are converted
	// on one page, and maxRuleOps the path operators looked at: past
	// either the page is a drawing of many lines, a map or a hatching,
	// and its paths are not followed further.
	maxRuleSegs = 1 << 15
	maxRuleOps  = 1 << 17
)

// Rule is the box of a rule on the page, in the page's coordinates.
type Rule [4]float64

// rulePath is the path being built, as far as rules go.
type rulePath struct {
	// The current point and the start of the subpath, as written, and
	// the current point's value when known (known). They are the bytes
	// of the operands in the content stream: the buffer of each stream
	// of a page is kept while the page is read.
	curX, curY, startX, startY []byte
	have                       bool // there is a current point
	known                      bool
	x, y                       float64
	// The subpath so far: whether all its segments run along an axis,
	// how many it has, and the box of its points, in user space, when
	// they all do.
	aligned bool
	n       int
	box     [4]float64
	// Candidates of the path: segments along an axis and rectangles, in
	// user space, as [x0 y0 x1 y1]; polys are subpaths of segments along
	// the axes, as their boxes, which a fill may make a bar of.
	segs  [][4]float64
	rects [][4]float64
	polys [][4]float64
	full  bool // maxPathRules reached
	// skip is set when the first segment of the path does not run along
	// an axis: a curve, a map, a drawing, whose segments are not looked
	// at further. started is set once the path has a segment.
	skip, started bool
	// scale is how long a unit of user space is on the page, as the
	// matrix in force when the path began, which painting uses.
	scale float64
}

func (p *rulePath) reset() {
	p.curX, p.curY, p.startX, p.startY = nil, nil, nil, nil
	p.have, p.aligned, p.n = false, true, 0
	p.segs, p.rects, p.polys, p.full = p.segs[:0], p.rects[:0], p.polys[:0], false
	p.skip, p.started, p.scale = false, false, 0
}

// pathOperator follows the operators that build a path, and those that
// paint or end it. It reports whether op was one of them.
func (in *interp) pathOperator(op string, args []content.Operand) bool {
	p := &in.rp
	if in.ruleOps++; in.ruleSegs > maxRuleSegs || in.ruleOps > maxRuleOps {
		in.followPaths = false
		p.reset()
		return true
	}
	if p.skip {
		switch op {
		case "m", "l", "h", "c", "v", "y":
			return true // the rest of a path that is not looked at
		}
	}
	switch op {
	case "m":
		if a := last(args, 2); a != nil {
			in.endSubpath()
			p.curX, p.curY, p.startX, p.startY = a[0].Raw, a[1].Raw, a[0].Raw, a[1].Raw
			p.have, p.aligned, p.n, p.known = true, true, 0, false
		}
	case "l":
		if a := last(args, 2); a != nil {
			in.lineTo(a[0].Raw, a[1].Raw)
		}
	case "h":
		if p.have && p.n > 0 {
			in.lineTo(p.startX, p.startY)
		}
	case "c", "v", "y":
		n := 6
		if op != "c" {
			n = 4
		}
		if a := last(args, n); a != nil {
			p.aligned, p.known = false, false
			p.n++
			p.curX, p.curY = a[n-2].Raw, a[n-1].Raw
		}
	case "re":
		if a := last(args, 4); a != nil {
			in.endSubpath()
			if !p.full && len(p.rects) < maxPathRules {
				x, y := finite(rawNumber(a[0].Raw)), finite(rawNumber(a[1].Raw))
				w, h := finite(rawNumber(a[2].Raw)), finite(rawNumber(a[3].Raw))
				p.rects = append(p.rects, [4]float64{min(x, x+w), min(y, y+h), max(x, x+w), max(y, y+h)})
			} else {
				p.full = true
			}
			p.curX, p.curY, p.startX, p.startY = a[0].Raw, a[1].Raw, a[0].Raw, a[1].Raw
			p.have, p.aligned, p.n, p.known = true, true, 0, false
		}
	case "f", "F", "f*":
		in.paintPath(true, false, false)
	case "S":
		in.paintPath(false, true, false)
	case "s":
		in.paintPath(false, true, true)
	case "B", "B*":
		in.paintPath(true, true, false)
	case "b", "b*":
		in.paintPath(true, true, true)
	case "n":
		p.reset()
	case "w":
		if a := last(args, 1); a != nil {
			in.gs.lineWidth = max(finite(rawNumber(a[0].Raw)), 0)
		}
	default:
		return false
	}
	return true
}

// lineTo adds a straight segment from the current point. Only one that
// runs along an axis, by its text, is converted.
func (in *interp) lineTo(x, y []byte) {
	p := &in.rp
	if !p.have {
		return
	}
	p.n++
	sameX, sameY := bytes.Equal(x, p.curX), bytes.Equal(y, p.curY)
	if !p.started {
		p.started = true
		if !sameX && !sameY {
			p.skip = true // not along an axis: no rules in this path
			return
		}
	}
	if sameX || sameY {
		in.ruleSegs++
		if p.scale == 0 {
			// The matrix cannot change while a path is built: it is the
			// one the path is painted with.
			c := &in.gs.ctm
			p.scale = math.Sqrt(math.Abs(c[0]*c[3] - c[1]*c[2]))
		}
		// One coordinate is written the same: of the new point only the
		// other is converted, and the current point is when not known.
		if !p.known {
			p.x, p.y, p.known = finite(rawNumber(p.curX)), finite(rawNumber(p.curY)), true
		}
		x0, y0, x1, y1 := p.x, p.y, p.x, p.y
		if !sameX {
			x1 = finite(rawNumber(x))
		}
		if !sameY {
			y1 = finite(rawNumber(y))
		}
		p.x, p.y = x1, y1
		s := [4]float64{min(x0, x1), min(y0, y1), max(x0, x1), max(y0, y1)}
		// A segment too short to be a rule is not kept; its box counts
		// for the polygon it may be a side of.
		if long := max(s[2]-s[0], s[3]-s[1])*p.scale >= 2*MaxRuleWidth; long && !p.full && len(p.segs) < maxPathRules {
			p.segs = append(p.segs, s)
		} else if long {
			p.full = true
		}
		if p.n == 1 {
			p.box = s
		} else {
			p.box = [4]float64{min(p.box[0], s[0]), min(p.box[1], s[1]), max(p.box[2], s[2]), max(p.box[3], s[3])}
		}
	} else {
		p.aligned, p.known = false, false
	}
	p.curX, p.curY = x, y
}

// rawNumber is the value of a number as written in a content stream; 0
// for anything else. A plain decimal, which is nearly every number of a
// path, is read here; anything else by the parser.
func rawNumber(raw []byte) float64 {
	i, neg := 0, false
	if len(raw) > 0 && (raw[0] == '-' || raw[0] == '+') {
		neg, i = raw[0] == '-', 1
	}
	var v, scale float64
	digits, dot := 0, false
	for ; i < len(raw); i++ {
		switch c := raw[i]; {
		case c >= '0' && c <= '9':
			if digits >= 15 {
				v, _ := syntax.ParseNumber(raw, 0)
				return v.Number()
			}
			v = v*10 + float64(c-'0')
			digits++
			if dot {
				scale *= 10
			}
		case c == '.' && !dot:
			dot, scale = true, 1
		default:
			v, _ := syntax.ParseNumber(raw, 0)
			return v.Number()
		}
	}
	if dot {
		v /= scale
	}
	if neg {
		v = -v
	}
	return v
}

// endSubpath notes the subpath that ends: when it is made of segments
// along the axes, a fill may make a bar of its box.
func (in *interp) endSubpath() {
	p := &in.rp
	if p.have && p.aligned && p.n >= 3 && !p.full && len(p.polys) < maxPathRules {
		p.polys = append(p.polys, p.box)
	}
	p.have = false
}

// paintPath records the rules that painting the path draws: a filled
// rectangle or polygon along the axes that is thin one way, and each
// stroked segment along an axis, as wide as the line.
func (in *interp) paintPath(fill, stroke, close bool) {
	p := &in.rp
	defer p.reset()
	if close && p.have && p.n > 0 {
		in.lineTo(p.startX, p.startY)
	}
	in.endSubpath()
	if in.offDepth > 0 || len(in.rules) >= maxRules {
		return
	}
	if fill {
		for _, r := range p.rects {
			in.addRule(r, 0)
		}
		for _, r := range p.polys {
			in.addRule(r, 0)
		}
	}
	if stroke {
		w := in.gs.lineWidth
		for _, s := range p.segs {
			in.addRule(s, w)
		}
		for _, r := range p.rects {
			in.addRule([4]float64{r[0], r[1], r[2], r[1]}, w)
			in.addRule([4]float64{r[0], r[3], r[2], r[3]}, w)
			in.addRule([4]float64{r[0], r[1], r[0], r[3]}, w)
			in.addRule([4]float64{r[2], r[1], r[2], r[3]}, w)
		}
	}
}

// addRule records a box of user space, grown across by half the width
// of the line that strokes it (the ends of a line are not drawn past
// its points), when it stays along the axes on the page and is thin one
// way and longer the other.
func (in *interp) addRule(r [4]float64, width float64) {
	if len(in.rules) >= maxRules {
		return
	}
	c := in.gs.ctm
	if !(c[1] == 0 && c[2] == 0 || c[0] == 0 && c[3] == 0) {
		return // turned by other than a right angle, or skewed
	}
	x0, y0 := c.apply(r[0], r[1])
	x1, y1 := c.apply(r[2], r[3])
	w := width * math.Sqrt(math.Abs(c[0]*c[3]-c[1]*c[2])) / 2
	box := Rule{min(x0, x1), min(y0, y1), max(x0, x1), max(y0, y1)}
	if box[2]-box[0] >= box[3]-box[1] {
		box[1], box[3] = box[1]-w, box[3]+w
	} else {
		box[0], box[2] = box[0]-w, box[2]+w
	}
	dx, dy := box[2]-box[0], box[3]-box[1]
	if !(dx >= 0 && dy >= 0) || min(dx, dy) > MaxRuleWidth || max(dx, dy) < 2*MaxRuleWidth {
		return
	}
	in.rules = append(in.rules, box)
}
