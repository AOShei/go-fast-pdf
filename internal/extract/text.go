package extract

import (
	gofastcore "github.com/AOShei/go-fast-core"
	"github.com/AOShei/go-fast-pdf/internal/content"
	"github.com/AOShei/go-fast-pdf/internal/font"
)

// Run is a stretch of text set in one go: one font, size, colour and
// line, with no gap inside it wider than a space.
//
// Positions and sizes are in the page's default user space, before
// /Rotate and /UserUnit are applied.
type Run struct {
	// Off and Len locate the run's text in Result.RunText.
	Off, Len int32
	// X0, Y0 is where the baseline starts and X1, Y1 where it ends.
	X0, Y0, X1, Y1 float64
	// LX, LY is where the last string of the run starts. What separates
	// the run from the next one depends on it (see Separator).
	LX, LY float64
	// Dx, Dy is the direction of writing, a unit vector. Nx, Ny is the
	// unit vector across the line, towards the top of the glyphs.
	Dx, Dy, Nx, Ny float64
	// Size is the height the font is set at: the font size through the
	// text matrix and the current transformation. SizeX is the same
	// along the line, with horizontal scaling.
	Size, SizeX float64
	// Rise is how far the glyphs are lifted off the baseline (Ts).
	Rise float64
	// SpaceWidth is the width of the font's space at this size.
	SpaceWidth float64
	Font       *font.Font

	// What the visibility of the run depends on, as it was when the
	// run was drawn. No judgement is made here.

	// Mode is the text render mode (Tr): 0 fill, 1 stroke, 2 both, 3
	// neither, 4 to 7 the same with the text added to the clip path.
	Mode uint8
	// Clip numbers the text object of a run shown in a clipping mode (4
	// to 7), 0 for any other run. ClipPainted is set when something was
	// painted while that text clipped: the text then shows as what was
	// painted through it, which is how text filled with an image or a
	// shading is drawn.
	Clip        int32
	ClipPainted bool
	// Fill and Stroke are the colours, and FillAlpha and StrokeAlpha
	// the opacities (1 is opaque): those of the graphics state, times
	// those of the transparency groups the run is drawn in.
	Fill, Stroke           Color
	FillAlpha, StrokeAlpha float32
	// Blend is set when the run is painted with a blend mode other than
	// the normal one or through a soft mask, or is in a transparency
	// group that is: what a reader sees of it is then not its colour
	// laid over what is under it.
	Blend bool
	// CutOff is set when the run lies wholly outside what the clipping
	// paths in force leave of the page, as far as they are followed: no
	// part of it is painted. It is worked out only when shapes are
	// recorded (Options.Shapes).
	CutOff bool
	// OptionalOff is set when the run belongs to optional content that
	// is switched off in the document's default configuration.
	OptionalOff bool
	// Seq is the number of things other than text painted on the page
	// before the run: paths, images, shadings.
	Seq int32
	// Form is the index in Result.Forms of the box of the Form XObject
	// the run is drawn in, or -1 on the page itself.
	Form int32

	// Sep is what separates the run from the one before it in the order
	// of drawing: 0 for nothing, ' ' or '\n'.
	Sep byte
	// ActualText is set when the text is the /ActualText of a marked
	// content sequence and not the glyphs drawn.
	ActualText bool
	// Glyphs is the number of character codes shown. Unmapped is how
	// many of them have no text, and Assumed how many have the text of
	// their code because the font does not say what its glyph is.
	Glyphs, Unmapped, Assumed int32
}

// PaintedBefore reports whether anything other than text was painted on
// the page before the run.
func (r *Run) PaintedBefore() bool { return r.Seq > 0 }

// Vertical reports whether the run is written from top to bottom.
func (r *Run) Vertical() bool { return r.Font != nil && r.Font.Vertical }

// Em is the font size along the line.
func (r *Run) Em() float64 {
	if r.Vertical() {
		return r.Size
	}
	return r.SizeX
}

// Gap is a space that is not in the file: two strings of one run stand
// apart by more than the threshold, and a space was put between their
// texts.
type Gap struct {
	// Off is the position of the space in the page's text.
	Off int32
	// Width is how far apart the strings are, along the line.
	Width float32
}

// Separator says what lies between two runs when next is drawn after
// prev with nothing between them: 0 for nothing, ' ' or '\n'. gap is how
// far along the line next starts after the end of prev; it means
// something when the two are on one line.
//
// It is the decision the interpreter takes between two strings, made
// again from what the runs record, for a reader that leaves runs out or
// puts them in another order.
func Separator(prev, next *Run) (sep byte, gap float64) {
	p := lineEnd{
		valid: true,
		sx:    prev.LX, sy: prev.LY, ex: prev.X1, ey: prev.Y1,
		dx: prev.Dx, dy: prev.Dy, nx: prev.Nx, ny: prev.Ny,
		size: prev.Size, sizeX: prev.SizeX, space: prev.SpaceWidth,
		vertical: prev.Vertical(),
	}
	sep = p.separator(next.X0, next.Y0, next.X1, next.Y1, next.Dx, next.Dy, next.Size)
	return sep, (next.X0-prev.X1)*prev.Dx + (next.Y0-prev.Y1)*prev.Dy
}

// SpaceGap is the distance along the line from which two strings set at
// the size and space width of r are apart by a space.
func (r *Run) SpaceGap() float64 {
	p := lineEnd{size: r.Size, sizeX: r.SizeX, space: r.SpaceWidth, vertical: r.Vertical()}
	return p.gap()
}

// The thresholds that turn positions into spaces and line breaks. All
// are fractions of the font size unless said otherwise.
const (
	// spaceGapOfSpace: a gap of at least this share of the font's space
	// is a space. Justified text shrinks spaces to about two thirds;
	// kerning moves glyphs by far less than half a space.
	spaceGapOfSpace = 0.5
	// spaceGapMin and spaceGapMax bound that threshold, for fonts whose
	// stated space is unusually narrow or wide.
	spaceGapMin = 0.08
	spaceGapMax = 0.30
	// backwardSlack is how far a string may start before the end of the
	// one before it and still continue it: kerning, and accents set
	// over their letter.
	backwardSlack = 0.30
	// overprintSlack is how far from a string the same string may be
	// printed again and be taken for the same text: bold type made by
	// printing twice, a shadow.
	overprintSlack = 0.12
	// rightToLeftSlack is how far a string written from right to left
	// may reach into the one before it.
	rightToLeftSlack = 0.10
	// glyphAscent and glyphDescent are the part of the font size a line
	// of text takes above and below its baseline.
	glyphAscent  = 0.80
	glyphDescent = 0.20
	// lineOverlap: two strings are on one line when their extents
	// across the line overlap by this share of the smaller. A
	// superscript overlaps its line by most of its height; the next
	// line of a paragraph does not overlap at all.
	lineOverlap = 0.5
	// baselineSlack is how far apart two baselines may be and still be
	// one, as a share of the smaller font size.
	baselineSlack = 0.15
	// sameDirection is the cosine above which two directions of writing
	// are the same: about eight degrees.
	sameDirection = 0.99
)

// longGlyphBytes is the length of text from which one character code
// stands for a passage and not for a character or a ligature.
const longGlyphBytes = 24

// lineEnd is where the string shown last began and ended.
type lineEnd struct {
	valid          bool
	sx, sy, ex, ey float64 // start and end of the baseline
	dx, dy         float64 // direction of writing
	nx, ny         float64 // across the line, towards the top of the glyphs
	size, sizeX    float64
	space          float64
	vertical       bool
	serial         uint32 // the geometry the fields above were copied from
	// The string itself: its font and its text in interp.text.
	font           *font.Font
	textLo, textHi int32
}

// extent returns how far a line of the given size reaches across the
// line, below and above its baseline.
func extent(size float64, vertical bool) (below, above float64) {
	if vertical {
		// Glyphs of a vertical line are centred on it.
		return size / 2, size / 2
	}
	return glyphDescent * size, glyphAscent * size
}

// separator decides what lies between the string shown last and one
// that starts at (x0, y0), ends at (x1, y1) and is written in direction
// (dx, dy) at the given size.
func (p *lineEnd) separator(x0, y0, x1, y1, dx, dy, size float64) byte {
	if !p.valid {
		return 0
	}
	if p.dx*dx+p.dy*dy < sameDirection {
		return '\n'
	}
	vx, vy := x0-p.ex, y0-p.ey
	along := vx*p.dx + vy*p.dy
	across := vx*p.nx + vy*p.ny

	// The same line?
	below1, above1 := extent(p.size, p.vertical)
	below2, above2 := extent(size, p.vertical)
	overlap := min(above1, across+above2) - max(-below1, across-below2)
	smaller := min(p.size, size)
	if smaller <= 0 {
		smaller = max(p.size, size)
	}
	if overlap < lineOverlap*smaller || smaller <= 0 && across != 0 {
		return '\n'
	}

	// On the line: a gap, a continuation, or a step back.
	gap, slack := p.gap(), backwardSlack*p.em()
	switch {
	case along > gap:
		return ' '
	case along >= -slack:
		return 0
	}
	// The string starts before the end of the last one.
	fromStart := (x0-p.sx)*p.dx + (y0-p.sy)*p.dy
	if fromStart > slack {
		// Inside the last string, further along than its start: a glyph
		// set over others, or glyphs narrower than the font says they
		// are. Either way it continues the text.
		return 0
	}
	// At or before the start of the last string. Text written from
	// right to left does that with every string: then this one ends
	// where the last one began, give or take the kerning of the smaller
	// of the two.
	before := (p.sx-x1)*p.dx + (p.sy-y1)*p.dy
	if fromStart < -slack && before <= gap && before >= -rightToLeftSlack*min(p.em(), size) {
		return 0
	}
	// Another piece of text. On the same baseline it is the same place
	// printed twice, or a line drawn out of order. On another baseline
	// it is another line, though the two overlap: small print set under
	// large letters, within their depth.
	if abs(across) > baselineSlack*smaller {
		return '\n'
	}
	return ' '
}

// em is the font size along the line.
func (p *lineEnd) em() float64 {
	if p.vertical {
		return p.size
	}
	return p.sizeX
}

// gap is the distance along the line from which two strings are apart
// by a space.
func (p *lineEnd) gap() float64 {
	em := p.em()
	return min(max(spaceGapOfSpace*p.space, spaceGapMin*em), spaceGapMax*em)
}

// show handles one string of Tj, TJ, ' or ".
func (in *interp) show(o content.Operand) {
	if !o.IsString() || in.full {
		return
	}
	in.str = o.AppendString(in.str[:0])
	raw := in.str
	if len(raw) == 0 {
		return
	}
	g := &in.gs
	f := g.font
	if f == nil {
		f = font.Default
		in.fontMissing = true
	}
	// Character spacing as wide as a space sets each glyph apart from
	// the next. Programs use it for the space between two words shown
	// as one string (groff writes "A default" as the string "Ad" with
	// such a spacing), and designers for letter-spaced headings. The two
	// cannot be told apart from the page, and this library reads both
	// the way other readers do: each glyph is a string of its own, and
	// the spacing between them is a gap like any other.
	if g.tc > 0 && len(raw) > 1 {
		m := in.tm.mul(g.ctm)
		em := abs(g.size*g.th) * hypot(m[0], m[1])
		step := g.tc * abs(g.th) * hypot(m[0], m[1])
		if f.Vertical {
			em = abs(g.size) * hypot(m[2], m[3])
			step = g.tc * hypot(m[2], m[3])
		}
		if p := (lineEnd{size: em, sizeX: em, space: f.SpaceWidth * em}); step > p.gap() {
			for len(raw) > 0 && !in.full {
				n := f.CodeLen(raw)
				in.showCodes(f, raw[:n])
				raw = raw[n:]
			}
			return
		}
	}
	in.showCodes(f, raw)
}

// showCodes shows the character codes of raw in the font f.
func (in *interp) showCodes(f *font.Font, raw []byte) {
	g := &in.gs

	// The text. Inside a sequence with /ActualText the glyphs are read
	// for their advance only: the first string shown stands for the
	// replacement text and the others for nothing.
	start := len(in.text)
	var sh font.Shown
	replaced, first := in.inActual > 0, false
	if f.LongestText > longGlyphBytes {
		in.res.LongGlyphText++
	}
	if replaced {
		for i := range in.mc {
			if m := &in.mc[i]; m.actual {
				// The outermost sequence replaces everything inside it.
				if !m.emitted {
					m.emitted, first = true, true
					in.text = append(in.text, in.actual[m.lo:m.hi]...)
					in.silent = in.silent[:0]
				}
				break
			}
		}
		// The glyphs are kept only to be compared with the replacement.
		if len(in.silent) < 1<<16 {
			in.silent, sh = f.Append(in.silent, raw)
		} else {
			keep := len(in.silent)
			in.silent, sh = f.Append(in.silent, raw)
			in.silent = in.silent[:keep]
		}
	} else {
		in.text, sh = f.Append(in.text, raw)
		in.unmapped += sh.Unmapped
		in.assumed += sh.Assumed
	}

	// Text in a clipping mode belongs to the clip of its text object;
	// text that is painted while other text clips paints through it.
	var clip int32
	if g.mode >= 4 {
		if in.textClip == 0 {
			in.clips++
			in.textClip = in.clips
		}
		clip = in.textClip
	}
	if g.mode != 3 && g.mode != 7 {
		in.paintedThroughClip()
	}

	// The advance in text space, and the text position after it.
	var tx, ty float64
	spacing := float64(sh.Glyphs)*g.tc + float64(sh.Spaces)*g.tw
	if f.Vertical {
		ty = sh.Advance*g.size - spacing
	} else {
		tx = (sh.Advance*g.size + spacing) * g.th
	}
	// Where that is on the page. The string ends with its last glyph:
	// the character spacing after it is not part of it.
	if !in.geomValid || in.geom.font != f {
		in.measure(f)
	}
	ge := &in.geom
	c := &g.ctm
	x0 := in.tm[4]*c[0] + in.tm[5]*c[2] + c[4]
	y0 := in.tm[4]*c[1] + in.tm[5]*c[3] + c[5]
	in.advance(tx, ty)
	if sh.Glyphs > 0 {
		if f.Vertical {
			ty += g.tc
		} else {
			tx -= g.tc * g.th
		}
	}
	x1, y1 := x0+tx*ge.a+ty*ge.c, y0+tx*ge.b+ty*ge.d
	// Whether a clipping path cuts the string off altogether. Strings
	// that it treats differently are not one run.
	cut := in.opts.Shapes && g.region.Bounded && in.cutOff(x0, y0, x1, y1, f.Vertical)

	p := &in.prev
	// The same string printed again where it was printed last: the way
	// bold type and shadows are faked. It is the same text once.
	// Only a copy that is drawn the same way is left out here: one that a
	// reader would see differently is for the layout to judge.
	if n := len(in.runs); !replaced && n > 0 && p.valid && p.serial == ge.serial && p.font == f &&
		in.runs[n-1].Clip == clip && in.sameVisibility(&in.runs[n-1], cut) && in.printedAgain(x0, y0, start) {
		in.text = in.text[:start]
		in.unmapped -= sh.Unmapped
		in.assumed -= sh.Assumed
		p.sx, p.sy = x0, y0
		if n := len(in.runs); n > 0 {
			in.runs[n-1].LX, in.runs[n-1].LY = x0, y0
		}
		return
	}
	sep := p.separator(x0, y0, x1, y1, ge.dx, ge.dy, ge.size)
	along := (x0-p.ex)*p.dx + (y0-p.ey)*p.dy
	p.font, p.textLo, p.textHi = f, int32(start), int32(len(in.text))
	p.sx, p.sy, p.ex, p.ey = x0, y0, x1, y1
	if p.serial != ge.serial || !p.valid {
		p.valid, p.serial = true, ge.serial
		p.dx, p.dy, p.nx, p.ny = ge.dx, ge.dy, ge.nx, ge.ny
		p.size, p.sizeX, p.space, p.vertical = ge.size, ge.sizeX, ge.space, f.Vertical
	}

	if replaced && !first {
		// Part of a replaced sequence: it moved the position, no more.
		if n := len(in.runs); n > 0 && in.runs[n-1].ActualText {
			in.runs[n-1].X1, in.runs[n-1].Y1 = x1, y1
			in.runs[n-1].LX, in.runs[n-1].LY = x0, y0
			in.runs[n-1].Glyphs += int32(sh.Glyphs)
		}
		return
	}

	// A string that continues the run before it, in the same state,
	// becomes part of that run.
	//
	// A gap wider than the font size ends the run: what follows may be
	// another entry of a row, or text that leaves room for a word drawn
	// later, and a reader of the runs has to see the gap.
	if n := len(in.runs); n > 0 && sep != '\n' && !replaced && !(sep == ' ' && along > ge.size) && in.runs[n-1].Clip == clip && in.continues(&in.runs[n-1], f, cut) {
		last := &in.runs[n-1]
		if sep == ' ' && start > 0 && in.text[start-1] != ' ' && len(in.text) > start && in.text[start] != ' ' {
			in.text = append(in.text, 0)
			copy(in.text[start+1:], in.text[start:])
			in.text[start] = ' '
			in.gaps = append(in.gaps, Gap{Off: int32(start), Width: float32(along)})
		}
		last.Len = int32(len(in.text)) - last.Off
		last.X1, last.Y1 = x1, y1
		last.LX, last.LY = x0, y0
		last.Glyphs += int32(sh.Glyphs)
		last.Unmapped += int32(sh.Unmapped)
		last.Assumed += int32(sh.Assumed)
		in.checkTextLimit()
		return
	}
	if len(in.runs) >= maxRunsPerPage {
		in.text = in.text[:start]
		in.tooMuchText()
		return
	}
	in.runs = append(in.runs, Run{
		Off: int32(start), Len: int32(len(in.text) - start),
		X0: x0, Y0: y0, X1: x1, Y1: y1, LX: x0, LY: y0, Dx: ge.dx, Dy: ge.dy, Nx: ge.nx, Ny: ge.ny,
		Size: ge.size, SizeX: ge.sizeX, Rise: g.rise * ge.lenY, SpaceWidth: ge.space, Font: f,
		Mode: g.mode, Clip: clip, Fill: g.fill.color, Stroke: g.stroke.color,
		FillAlpha: g.fillAlpha, StrokeAlpha: g.strokeAlpha, Blend: g.blend, CutOff: cut,
		OptionalOff: in.offDepth > 0, Seq: in.seq, Form: in.form,
		Sep: sep, ActualText: replaced, Glyphs: int32(sh.Glyphs), Unmapped: int32(sh.Unmapped), Assumed: int32(sh.Assumed),
	})
	in.checkTextLimit()
}

// printedAgain reports whether the string whose text is in.text[start:]
// and which starts at (x0, y0) repeats the string shown last: the same
// text, in the same font and size, within overprintSlack of its place.
func (in *interp) printedAgain(x0, y0 float64, start int) bool {
	p := &in.prev
	slack := overprintSlack * p.em()
	dx, dy := x0-p.sx, y0-p.sy
	if dx > slack || dx < -slack || dy > slack || dy < -slack {
		return false
	}
	if int(p.textHi) > start || p.textLo < 0 {
		return false
	}
	return string(in.text[p.textLo:p.textHi]) == string(in.text[start:]) && start < len(in.text)
}

// geometry is what the place of a string depends on besides the text
// position: the scale and direction that the text matrix, the current
// transformation, the font size and the horizontal scaling give. It
// changes far less often than strings are shown.
type geometry struct {
	font       *font.Font
	a, b, c, d float64 // text space to the page, without the translation
	lenY       float64 // the page's length of one unit up in text space
	size       float64
	sizeX      float64
	dx, dy     float64 // the direction of writing
	nx, ny     float64 // across the line, towards the top of the glyphs
	space      float64
	serial     uint32 // changes with every new measurement
}

// measure computes the geometry for the current state and the font f.
func (in *interp) measure(f *font.Font) {
	g := &in.gs
	m := in.tm.mul(g.ctm)
	old := in.geom
	ge := &in.geom
	ge.font, ge.a, ge.b, ge.c, ge.d = f, m[0], m[1], m[2], m[3]
	lenX, lenY := hypot(m[0], m[1]), hypot(m[2], m[3])
	ge.lenY = lenY
	ge.size, ge.sizeX = abs(g.size)*lenY, abs(g.size*g.th)*lenX
	// The direction of writing: along the x axis of text space, or down
	// its y axis for a vertical font; backwards when the size or the
	// scaling is negative.
	switch {
	case f.Vertical && lenY > 0:
		ge.dx, ge.dy = -m[2]/lenY, -m[3]/lenY
		if g.size < 0 {
			ge.dx, ge.dy = -ge.dx, -ge.dy
		}
	case !f.Vertical && lenX > 0:
		ge.dx, ge.dy = m[0]/lenX, m[1]/lenX
		if g.size*g.th < 0 {
			ge.dx, ge.dy = -ge.dx, -ge.dy
		}
	default:
		ge.dx, ge.dy = 1, 0
	}
	// Across the line: at a right angle to the writing, on the side the
	// tops of the glyphs are.
	ge.nx, ge.ny = -ge.dy, ge.dx
	if !f.Vertical && (ge.nx*m[2]+ge.ny*m[3])*g.size < 0 {
		ge.nx, ge.ny = -ge.nx, -ge.ny
	}
	ge.space = f.SpaceWidth * ge.sizeX
	if f.Vertical {
		ge.space = f.SpaceWidth * ge.size
	}
	// Setting the text matrix again to what it was is not a change.
	if old.font != ge.font || old.a != ge.a || old.b != ge.b || old.c != ge.c || old.d != ge.d ||
		old.size != ge.size || old.sizeX != ge.sizeX {
		ge.serial++
	}
	in.geomValid = true
}

// continues reports whether a string shown now, in the font f, can join
// the run last: everything about it that a reader of the runs can see
// is the same.
func (in *interp) continues(last *Run, f *font.Font, cut bool) bool {
	g, ge := &in.gs, &in.geom
	return last.Font == f && in.sameVisibility(last, cut) && !last.ActualText &&
		closeTo(last.Size, ge.size) && closeTo(last.SizeX, ge.sizeX) && last.Rise == g.rise*ge.lenY &&
		last.Dx*ge.dx+last.Dy*ge.dy > 0.9999
}

// cutOff reports whether a string whose baseline runs from (x0, y0) to
// (x1, y1) lies wholly outside the clipping region: its box, the
// baseline with the height of the glyphs above it and their depth below,
// does not touch the box of the region.
func (in *interp) cutOff(x0, y0, x1, y1 float64, vertical bool) bool {
	g, ge := &in.gs, &in.geom
	r := &g.region
	if r.Empty() {
		return true
	}
	below, above := extent(ge.size, vertical)
	rise := g.rise * ge.lenY
	ax, ay := ge.nx*(rise+above), ge.ny*(rise+above)
	bx, by := ge.nx*(rise-below), ge.ny*(rise-below)
	return min(x0+ax, x0+bx, x1+ax, x1+bx) >= r.X1 || max(x0+ax, x0+bx, x1+ax, x1+bx) <= r.X0 ||
		min(y0+ay, y0+by, y1+ay, y1+by) >= r.Y1 || max(y0+ay, y0+by, y1+ay, y1+by) <= r.Y0
}

// sameVisibility reports whether a string shown now is drawn as the run
// last is, in everything that decides whether a reader sees it.
func (in *interp) sameVisibility(last *Run, cut bool) bool {
	g := &in.gs
	return last.Mode == g.mode && last.Fill == g.fill.color && last.Stroke == g.stroke.color &&
		last.FillAlpha == g.fillAlpha && last.StrokeAlpha == g.strokeAlpha &&
		last.Blend == g.blend && last.CutOff == cut &&
		last.OptionalOff == (in.offDepth > 0) && last.Seq == in.seq && last.Form == in.form
}

func (in *interp) checkTextLimit() {
	if len(in.text) > maxTextPerPage {
		in.tooMuchText()
	}
}

func (in *interp) tooMuchText() {
	in.full = true
	in.warn(gofastcore.SeverityError, gofastcore.WarnLimitExceeded, "the page has more text than the limit; the rest is not read")
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

func closeTo(a, b float64) bool {
	d := a - b
	return d <= 0.001*a && -d <= 0.001*a
}

// assemble appends the text of the runs to dst in the order they were
// drawn, with the separators between them.
func assemble(dst []byte, runs []Run, text []byte) []byte {
	for i := range runs {
		r := &runs[i]
		t := text[r.Off : r.Off+r.Len]
		if len(t) == 0 {
			continue
		}
		if n := len(dst); n > 0 {
			switch {
			case r.Sep == '\n':
				dst = append(dst, '\n')
			case r.Sep == ' ' && dst[n-1] != ' ' && dst[n-1] != '\n' && t[0] != ' ':
				dst = append(dst, ' ')
			}
		}
		dst = append(dst, t...)
	}
	return dst
}
