package layout

import (
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

// occluder is an opaque image, as a convex quadrilateral.
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
		img := &c.Images[i]
		q, bb := b.quadBox(img.Quad)
		large := false
		if pageArea > 0 {
			seen := box{max(bb.x0, 0), max(bb.y0, 0), min(bb.x1, pageBox.x1), min(bb.y1, pageBox.y1)}
			if large = seen.area() >= gofastcore.HiddenTextLayerCoverage*pageArea; large {
				b.big = append(b.big, bb)
			}
		}
		if img.Opaque && bb.area() > 0 {
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
		p.hidden = b.reasons(r, &p, pageBox)
		if p.hidden == 0 && r.Seq > 0 {
			b.stats.Unjudged++
		}
		b.pieces = append(b.pieces, p)
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

	// An image drawn after the run that covers all of it.
	covered, coveredByPage := false, false
	for i := len(b.occ) - 1; i >= 0; i-- {
		o := &b.occ[i]
		if o.seq < r.Seq {
			break // drawn before the run, as are all before it
		}
		if me.x0 < o.box.x0 || me.x1 > o.box.x1 || me.y0 < o.box.y0 || me.y1 > o.box.y1 {
			continue
		}
		if inside(&o.q, me.x0, me.y0) && inside(&o.q, me.x1, me.y0) && inside(&o.q, me.x1, me.y1) && inside(&o.q, me.x0, me.y1) {
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
	fill := r.Mode == 0 || r.Mode == 2 || r.Mode == 4 || r.Mode == 6
	stroke := r.Mode == 1 || r.Mode == 2 || r.Mode == 5 || r.Mode == 6
	if (fill || stroke) && (!fill || r.FillAlpha <= 0) && (!stroke || r.StrokeAlpha <= 0) {
		h |= gofastcore.HiddenTransparent
	}
	// The background is known without looking at shapes in one case:
	// nothing has been painted before the run, so it is the white page.
	if (fill || stroke) && r.Seq == 0 && (!fill || faint(r.Fill)) && (!stroke || faint(r.Stroke)) {
		h |= gofastcore.HiddenLowContrast
	}
	return h
}

// faint reports whether a colour cannot be told from white. A colour
// that could not be converted is not judged.
func faint(c extract.Color) bool {
	return c.Known && gofastcore.LowContrast(c.R, c.G, c.B, 255, 255, 255)
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
	sort.Slice(b.sorted, func(i, j int) bool {
		pi, pj := &b.pieces[b.sorted[i]], &b.pieces[b.sorted[j]]
		if pi.sx != pj.sx {
			return pi.sx < pj.sx
		}
		return pi.run < pj.run
	})
	for i, ai := range b.sorted {
		a := &b.pieces[ai]
		slack := overprintSlack * a.em
		ra := &b.c.Runs[a.run]
		// A bound on the neighbours looked at keeps a page that draws
		// everything at one point from costing the square of its size.
		for k := i + 1; k < n && k <= i+32; k++ {
			o := &b.pieces[b.sorted[k]]
			if o.sx-a.sx > slack {
				break
			}
			ro := &b.c.Runs[o.run]
			if o.dup || a.dup || ro.Font != ra.Font || ro.Len != ra.Len ||
				math.Abs(o.sy-a.sy) > slack || math.Abs(o.size-a.size) > 0.001*a.size ||
				o.dx*a.dx+o.dy*a.dy < 0.999 || string(b.runText(o.run)) != string(b.runText(a.run)) {
				continue
			}
			// Of two copies, the later one goes. Of a copy a reader can
			// see and one he cannot, the hidden one goes: it hides
			// nothing that is not in plain sight in the same place. Two
			// copies hidden for different reasons both stay.
			var drop *piece
			switch {
			case o.hidden == a.hidden:
				drop = o
				if a.run > o.run {
					drop = a
				}
			case a.hidden == 0:
				drop = o
			case o.hidden == 0:
				drop = a
			default:
				continue
			}
			drop.dup = true
			b.stats.Duplicates++
		}
	}
}
