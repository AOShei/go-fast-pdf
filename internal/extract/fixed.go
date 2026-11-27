package extract

import (
	gofastcore "github.com/AOShei/go-fast-core"
	"github.com/AOShei/go-fast-pdf/internal/font"
)

// Fixed takes a page that is given as strings at stated places and not
// as a content stream: a page of an XPS document. The strings go where
// those of a content stream go, so what the page yields is what
// a PDF page with the same strings at the same places yields.
//
// The page is described in the space of a PDF page: its origin at the
// bottom left, y upwards, in points. The matrix given with a font takes
// the space the strings are placed in to that space.
type Fixed struct {
	in     *interp
	worker *Worker
	raw    []byte // the string being shown, as a placed font takes it
}

// Space is a space in a string given to Fixed.Show: where it is in the
// text of the string, and how far the string has advanced before it and
// after it, in the units the string is placed in.
type Space struct {
	Off           int32
	Before, After float64
}

// NewFixed begins a page. Options.Words, Rules, Stop, Use and Worker are
// looked at. Finish ends it; a page that is not finished leaves nothing
// behind.
func NewFixed(opts Options, res *Result) *Fixed {
	worker := opts.Worker
	var in *interp
	if worker != nil && worker.in != nil {
		in, worker.in = worker.in, nil
	} else {
		in = interps.Get().(*interp)
	}
	in.reset(nil, nil, opts, res)
	// Text space with y upwards, as place expects it, over a space with
	// y downwards: the matrix of Font has the rest.
	in.tm = matrix{1, 0, 0, -1, 0, 0}
	return &Fixed{in: in, worker: worker}
}

// Font says how the strings shown next are set: in the font f at size,
// in a space that m takes to the page, and drawn or not (mode is a text
// render mode of PDF, 0 for filled and 3 for not drawn at all; alpha is
// the opacity the file declares, 1 for opaque).
func (x *Fixed) Font(f *font.Font, size float64, m [6]float64, mode uint8, alpha float32) {
	in := x.in
	g := &in.gs
	if g.font != f || g.size != size || g.ctm != matrix(m) {
		g.font, g.size, g.ctm = f, size, matrix(m)
		in.geomValid = false
	}
	g.mode, g.fillAlpha = mode, alpha
}

// Full reports whether the page takes no more text: a limit was reached,
// or Options.Stop was set.
func (x *Fixed) Full() bool {
	in := x.in
	if in.opts.Stop != nil && in.opts.Stop.Load() {
		in.stopped = true
	}
	return in.full || in.stopped
}

// Show places a string: text, shown by glyphs glyphs, unmapped of them
// without text of their own. It begins at (px, py) in the space of the
// font's matrix and advances by advance along its x axis. spaces are its
// spaces, which are looked at when Options.Words is set.
//
// The font is a placed font (font.NewPlaced): the string goes the way a
// string of a content stream goes, as a record of its text and its
// advance in place of character codes (showCodes).
func (x *Fixed) Show(text []byte, glyphs, unmapped int, px, py, advance float64, spaces []Space) {
	in := x.in
	g := &in.gs
	f := g.font
	if in.full || len(text) == 0 || f == nil || !f.Placed() || g.size == 0 {
		return
	}
	// The text position, which the interpreter takes the place of the
	// string from, and the spaces, which it would mark from the codes.
	in.tm[4], in.tm[5] = px, py
	if in.opts.Words {
		in.marks = in.marks[:0]
		for _, s := range spaces {
			in.marks = append(in.marks, mark{off: s.Off, glyphs: int(s.Off), before: s.Before / g.size, after: s.After / g.size, glyphs2: int(s.Off) + 1})
		}
	}
	x.raw = font.PlacedString(x.raw[:0], text, advance/g.size, glyphs, unmapped)
	in.showCodes(f, x.raw)
	in.sealed = false
}

// ShowBack places a string that is written from right to left and
// given in the order it is read: it begins at (px, py), its right end,
// and advances by advance against the x axis of the font's matrix. The
// arguments are those of Show, with the spaces measured from the right
// end; clusters are the stretches of the text that one glyph stands
// for, by their place in it. It is a run of its own, read from its right end to its left
// (Run.Back), as the strings of a PDF are that are drawn one after the
// other leftwards.
func (x *Fixed) ShowBack(str []byte, glyphs, unmapped int, px, py, advance float64, spaces []Space, clusters []Cluster) {
	in := x.in
	if in.full || len(str) == 0 || len(in.runs) >= maxRunsPerPage {
		if len(in.runs) >= maxRunsPerPage && !in.full {
			in.tooMuchText()
		}
		return
	}
	start := len(in.text)
	in.text = append(in.text, str...)
	text := in.text
	g := &in.gs
	f := g.font
	if !in.geomValid || in.geom.font != f {
		in.measure(f)
	}
	ge := &in.geom
	c := &g.ctm
	rx, ry := px*c[0]+py*c[2]+c[4], px*c[1]+py*c[3]+c[5]
	lx, ly := rx-advance*ge.a, ry-advance*ge.b
	for _, cl := range clusters {
		if len(in.clusters) >= maxRunsPerPage {
			break
		}
		in.clusters = append(in.clusters, Cluster{Off: int32(start) + cl.Off, Len: cl.Len})
	}
	if in.opts.Words {
		for _, s := range spaces {
			in.breaks = append(in.breaks, Break{Off: int32(start) + s.Off,
				X0: rx - s.Before*ge.a, Y0: ry - s.Before*ge.b, X1: rx - s.After*ge.a, Y1: ry - s.After*ge.b})
		}
	}
	in.unmapped += unmapped
	p := &in.prev
	sep := p.separator(lx, ly, rx, ry, ge.dx, ge.dy, ge.size)
	if sep == '\n' || !p.valid {
		in.endLine()
	}
	p.font, p.textLo, p.textHi = f, int32(start), int32(len(text))
	p.sx, p.sy, p.ex, p.ey = lx, ly, rx, ry
	p.trail = 0
	p.valid, p.serial = true, ge.serial
	p.dx, p.dy, p.nx, p.ny = ge.dx, ge.dy, ge.nx, ge.ny
	p.size, p.sizeX, p.space, p.vertical = ge.size, ge.sizeX, ge.space, false
	in.runs = append(in.runs, Run{
		Off: int32(start), Len: int32(len(text) - start),
		X0: lx, Y0: ly, X1: rx, Y1: ry, LX: lx, LY: ly, RX: rx, RY: ry, Dx: ge.dx, Dy: ge.dy, Nx: ge.nx, Ny: ge.ny,
		Size: ge.size, SizeX: ge.sizeX, SpaceWidth: ge.space, Font: f,
		Mode: g.mode, FillAlpha: g.fillAlpha, StrokeAlpha: g.strokeAlpha,
		Sep: sep, Glyphs: int32(glyphs), Unmapped: int32(unmapped), MCID: in.mcid,
		Back: true, RTL: font.HasRightToLeft(text[start:]),
	})
	in.sealed = true
	in.checkTextLimit()
}

// Warn adds a warning to the page, once for each code.
func (x *Fixed) Warn(severity gofastcore.Severity, code, message string) {
	x.in.warn(severity, code, message)
}

// Finish ends the page: Options.Use is called with its content, and the
// warnings of the page are in the result given to NewFixed.
func (x *Fixed) Finish() {
	in := x.in
	in.finish()
	in.cutWarnings()
	in.f, in.res, in.opts = nil, nil, Options{}
	in.trim()
	if x.worker != nil {
		x.worker.in = in
	} else {
		interps.Put(in)
	}
	x.in = nil
}
