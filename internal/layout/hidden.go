package layout

import (
	"bytes"
	"math"
	"sort"

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

// occluder is an opaque image, as a convex quadrilateral, and the box
// that the clipping paths leave of it.
type occluder struct {
	q   [8]float64
	box box
	seq int32
	// page is set when the image covers enough of the page for text
	// under it to be the text layer of a scan.
	page bool
}

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

// maxOccluders bounds the images a run is compared with. Of more, the
// largest are kept: an image that hides text is not a small one.
const maxOccluders = 256

// prepare places every run in displayed space and gives it its hidden
// reasons.
func (b *builder) prepare() {
	c := b.c
	pageBox := box{0, 0, b.page.Width, b.page.Height}
	pageArea := pageBox.area()

	// The images that can hide text, and the ones large enough for
	// unpainted text over them to be the text layer of a scan.
	for i := range c.Images {
		if b.opts.NoHiddenChecks {
			break
		}
		img := &c.Images[i]
		q, bb := b.quadBox(img.Quad)
		// An image is painted only where the clipping paths let it be.
		if clip, ok := b.region(&img.Region); ok {
			bb = box{max(bb.x0, clip.x0), max(bb.y0, clip.y0), min(bb.x1, clip.x1), min(bb.y1, clip.y1)}
		}
		large := false
		if pageArea > 0 {
			seen := box{max(bb.x0, 0), max(bb.y0, 0), min(bb.x1, pageBox.x1), min(bb.y1, pageBox.y1)}
			if large = seen.area() >= gofastcore.HiddenTextLayerCoverage*pageArea; large {
				b.big = append(b.big, bb)
			}
		}
		// It covers what is under it when it is opaque and the clip is a
		// rectangle. Through any other clipping path, how much of it is
		// painted is not known: pages whose transparency was flattened
		// draw pieces of images that way, around text that stays in view.
		if img.Opaque && bb.area() > 0 && !img.Region.Inexact {
			b.occ = append(b.occ, occluder{q: q, box: bb, seq: img.Seq, page: large})
		}
	}
	if len(b.occ) > maxOccluders {
		sort.SliceStable(b.occ, func(i, j int) bool { return b.occ[i].box.area() > b.occ[j].box.area() })
		b.occ = b.occ[:maxOccluders]
		sort.SliceStable(b.occ, func(i, j int) bool { return b.occ[i].seq < b.occ[j].seq })
	}

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
		if !b.opts.NoHiddenChecks {
			p.hidden = b.reasons(r, &p, pageBox)
		}
		// What the contrast check could say of the run: by default only
		// of one drawn before anything else was painted.
		if fill, stroke := painted(r.Mode); (fill || stroke) && !b.opts.NoHiddenChecks && p.hidden&gofastcore.HiddenTextLayer == 0 {
			switch {
			case r.Seq == 0 && !r.Blend:
				b.stats.Judged++
			case !b.opts.Deep || r.Blend:
				b.stats.Unjudged++
			}
		}
		b.pieces = append(b.pieces, p)
	}
	if b.opts.Deep && !b.opts.NoHiddenChecks {
		b.incomplete = !b.deep()
	}
	b.markDuplicates()
}

// reasons returns why a reader cannot see the run, as far as the checks
// that always run can tell.
func (b *builder) reasons(r *extract.Run, p *piece, page box) gofastcore.Hidden {
	// Text that only clips is drawn all the same when something is
	// painted through it.
	unpainted := r.Mode == 3 || r.Mode == 7 && !r.ClipPainted
	cx, cy := (p.x0+p.x1)/2, (p.y0+p.y1)/2
	me := box{p.x0, p.y0, p.x1, p.y1}

	// An image drawn after the run that covers it.
	covered, coveredByPage := false, false
	var band box
	if n := len(b.occ); n > 0 && b.occ[n-1].seq >= r.Seq {
		band = b.cover(p)
	}
	for i := len(b.occ) - 1; i >= 0; i-- {
		o := &b.occ[i]
		if o.seq < r.Seq {
			break // drawn before the run, as are all before it
		}
		if !o.box.holds(band) {
			continue
		}
		if inside(&o.q, band.x0, band.y0) && inside(&o.q, band.x1, band.y0) && inside(&o.q, band.x1, band.y1) && inside(&o.q, band.x0, band.y1) {
			covered = true
			coveredByPage = coveredByPage || o.page
		}
	}

	// Text that clips is not covered by what is painted through it.
	if r.Mode == 7 && r.ClipPainted {
		covered, coveredByPage = false, false
	}

	// What lies wholly outside the page is off the page, whatever else
	// is true of it.
	offPage := page.x1 > 0 && page.y1 > 0 && (me.x1 <= 0 || me.y1 <= 0 || me.x0 >= page.x1 || me.y0 >= page.y1)

	// The text layer of a scan: text on the page that is not painted,
	// or is painted and then covered, within an image of at least half
	// the page. It is the only reason such a run gets.
	if !offPage {
		if unpainted {
			for i := range b.big {
				if b.big[i].contains(cx, cy) {
					return gofastcore.HiddenTextLayer
				}
			}
		}
		if coveredByPage {
			return gofastcore.HiddenTextLayer
		}
	}

	var h gofastcore.Hidden
	if unpainted || r.OptionalOff {
		h |= gofastcore.HiddenInvisible
	}
	if p.size < gofastcore.HiddenTinySize || !r.Vertical() && p.em < gofastcore.HiddenTinySize {
		h |= gofastcore.HiddenTiny
	}
	if offPage {
		h |= gofastcore.HiddenOffPage
	}
	if covered {
		h |= gofastcore.HiddenOccluded
	}
	if r.Form >= 0 && int(r.Form) < len(b.c.Forms) {
		// Text outside the box of the form it is drawn in is cut off.
		_, fb := b.quadBox(b.c.Forms[r.Form])
		if me.x1 <= fb.x0 || me.y1 <= fb.y0 || me.x0 >= fb.x1 || me.y0 >= fb.y1 {
			h |= gofastcore.HiddenClipped
		}
	}

	// What is painted depends on the render mode: the fill, the stroke
	// or both. A run is transparent, or without contrast, only when
	// that holds for every part of it that is painted.
	fill, stroke := painted(r.Mode)
	if (fill || stroke) && (!fill || r.FillAlpha <= 0) && (!stroke || r.StrokeAlpha <= 0) {
		h |= gofastcore.HiddenTransparent
	}
	// The background is taken to be known without looking at shapes in
	// one case: nothing has been painted before the run, so it is the
	// white page. (What is painted afterwards can still change what a
	// reader sees; the checks with shapes look at that.) Text painted
	// with a blend mode or through a soft mask does not show as its
	// colour, and is not judged.
	if r.Seq == 0 && !r.Blend && faintOn(r, fill, stroke, white) {
		h |= gofastcore.HiddenLowContrast
	}
	return h
}

// faint reports whether a colour, painted with an opacity on a
// background, cannot be told from it. A colour that could not be
// converted is not judged.
func faint(c extract.Color, alpha float32, back extract.Color) bool {
	if !c.Known || !back.Known {
		return false
	}
	if alpha < 1 {
		// What shows is the colour mixed with the background.
		mix := func(a, b uint8) uint8 {
			return uint8(float32(a)*alpha + float32(b)*(1-alpha) + 0.5)
		}
		c.R, c.G, c.B = mix(c.R, back.R), mix(c.G, back.G), mix(c.B, back.B)
	}
	// The shared measure of contrast compares how bright two colours
	// are, and two colours can be equally bright and plain to tell apart:
	// green on red, yellow on white. Text is faint when its colour is
	// also close to the background in each of its parts. Two grays that
	// the shared measure cannot tell apart are never further apart than
	// colourSlack, so this takes no flag away from gray on gray. It is
	// also the quick way to say that dark text on white is not faint.
	if farApart(c.R, back.R) || farApart(c.G, back.G) || farApart(c.B, back.B) {
		return false
	}
	return gofastcore.LowContrast(c.R, c.G, c.B, back.R, back.G, back.B)
}

// colourSlack is how far the red, green or blue of faint text may be
// from that of its background, of 255.
const colourSlack = 32

func farApart(a, b uint8) bool {
	d := int(a) - int(b)
	return d > colourSlack || d < -colourSlack
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
	b.twins(false)
	if !b.split {
		return
	}
	// A copy that a clipping path cuts in two is two pieces, and the
	// second of them ends where the line it repeats ends.
	sortStable(b.sorted, func(i, j int32) bool { return b.pieces[i].ex < b.pieces[j].ex })
	b.twins(true)
}

// twins goes through the pieces in the order of b.sorted, which is by
// the point where they start or, with atEnd, where they end, and marks
// those that repeat another.
func (b *builder) twins(atEnd bool) {
	n := len(b.sorted)
	at := func(p *piece) (float64, float64) {
		if atEnd {
			return p.ex, p.ey
		}
		return p.sx, p.sy
	}
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
			if drop := b.twin(ai, b.sorted[k], atEnd); drop != nil {
				drop.dup = true
				b.stats.Duplicates++
			}
		}
	}
}

// twin says which of two pieces that start at the same point, or end at
// the same point, is to be left out as a repetition of the other: nil
// if neither.
func (b *builder) twin(ai, oi int32, atEnd bool) *piece {
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
		// Of two copies, the later one goes. Of a copy a reader can see
		// and one he cannot, the hidden one goes: it hides nothing that
		// is not in plain sight in the same place. Two copies hidden for
		// different reasons both stay.
		switch {
		case o.hidden == a.hidden:
			if a.run > o.run {
				return a
			}
			return o
		case a.hidden == 0:
			return o
		case o.hidden == 0:
			return a
		}
		return nil
	}
	if !b.split {
		return nil
	}
	// One is a part of the other: the start of it, or its end. A page
	// that draws a line and then draws it again through a clipping path
	// gives such parts, one cut off by the path and one not. The part
	// goes when the whole line is seen as well as it is, or better, and
	// only when it is such a part: cut off, or drawn next to text of its
	// line that is.
	part, whole, pi, tp, tw := a, o, ai, ta, to
	if len(ta) > len(to) {
		part, whole, pi, tp, tw = o, a, oi, to, ta
	}
	if atEnd && !bytes.HasSuffix(tw, tp) || !atEnd && !bytes.HasPrefix(tw, tp) {
		return nil
	}
	if (part.hidden == whole.hidden || whole.hidden == 0) && b.cutInTwo(pi) {
		return part
	}
	return nil
}

// cutInTwo reports whether a piece is one of the parts that a clipping
// path makes of a line: it is cut off itself, or the piece drawn just
// before it or just after it is, on the same line.
func (b *builder) cutInTwo(i int32) bool {
	p := &b.pieces[i]
	if b.c.Runs[p.run].CutOff {
		return true
	}
	slack := overprintSlack * p.em
	for _, k := range [2]int32{i - 1, i + 1} {
		if k < 0 || int(k) >= len(b.pieces) {
			continue
		}
		o := &b.pieces[k]
		// How far the other piece starts from the line of this one.
		across := (o.sx-p.sx)*-p.dy + (o.sy-p.sy)*p.dx
		if b.c.Runs[o.run].CutOff && math.Abs(across) <= slack {
			return true
		}
	}
	return false
}
