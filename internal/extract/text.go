package extract

import (
	"bytes"
	"github.com/AOShei/go-fast-pdf/internal/content"
	"github.com/AOShei/go-fast-pdf/internal/font"
	"math"
	"slices"
	"sort"
	"unicode/utf8"
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
	// RX, RY is where the first string ends of a run of text drawn from
	// right to left (Back).
	RX, RY float64
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
	// Trail is how far the spaces at the end of the run's last string
	// reach along the line, when that is more than the font size: a
	// space set that wide, with word spacing, is how some programs move
	// to the next cell of a row, and no part of the text before it. X1,
	// Y1 is after it.
	Trail float64

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
	// FillAlpha and StrokeAlpha are the opacities (1 is opaque) that the
	// graphics state declares (/ca and /CA), times those of the
	// transparency groups the run is drawn in.
	FillAlpha, StrokeAlpha float32
	// OptionalOff is set when the run belongs to optional content that
	// is switched off in the document's default configuration.
	OptionalOff bool

	// Sep is what separates the run from the one before it in the order
	// of drawing: 0 for nothing, ' ' or '\n'.
	Sep byte
	// ActualText is set when the text is the /ActualText of a marked
	// content sequence and not the glyphs drawn.
	ActualText bool
	// MCID is the marked-content identifier the run is drawn under, of
	// the page's own content, by which the structure tree of a tagged
	// PDF refers to it; -1 for none.
	MCID int32
	// Glyphs is the number of character codes shown. Unmapped is how
	// many of them have no text, and Assumed how many have the text of
	// their code because the font does not say what its glyph is.
	Glyphs, Unmapped, Assumed int32
	// Back is set for a run of text written from right to left whose
	// strings were drawn one after the other leftwards: it reads from
	// RX, RY to LX, LY. RTL is set when the run has a letter of a script
	// written from right to left.
	Back, RTL bool
}

// Vertical reports whether the run is written from top to bottom.
func (r *Run) Vertical() bool { return r.Font != nil && r.Font.Vertical }

// Em is the font size along the line.
func (r *Run) Em() float64 {
	if r.Vertical() {
		return r.Size
	}
	return r.SizeX
}

// Break is where a space of the page's text is on the page: it begins
// at (X0, Y0) on the baseline, where the word before it ends, and ends
// at (X1, Y1), where the word after it begins.
type Break struct {
	// Off is the position of the space in the page's text.
	Off            int32
	X0, Y0, X1, Y1 float64
}

// mark is a space in the string being shown: where it is in the text of
// the string, and how far the string has advanced before it and after
// it, as Font.Append counts an advance.
type mark struct {
	off             int32
	glyphs, spaces  int
	before, after   float64
	glyphs2, space2 int // the counts after the space
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

// Join is a place where a run goes on with a string that stands apart
// from the one before it: by a space put in (Gap), or by more room than
// a space after a string that ends with one. Where the strings are is
// known exactly, as it is not for the spaces inside a string.
type Join struct {
	// Off is where the text of the string begins in the page's text,
	// after a space put in before it.
	Off int32
	// X0, Y0 is where the string before it ends, and X1, Y1 where it
	// begins.
	X0, Y0, X1, Y1 float64
}

// Cluster is a stretch of Content.Text that one glyph stands for.
type Cluster struct {
	Off, Len int32
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

// cover makes a run of text given for glyphs (ActualText), in letters
// written from right to left, reach over a further string of them, from
// x0, y0 to x1, y1. The strings come in any order: LibreOffice draws the
// dots of an Arabic letter before its body, so that neither the first
// nor the last is where the letter begins or ends. The baseline is that
// of the widest string, the body.
func (r *Run) cover(x0, y0, x1, y1 float64) {
	at := func(x, y float64) float64 { return x*r.Dx + y*r.Dy }
	a, e := min(at(r.X0, r.Y0), at(r.X1, r.Y1)), max(at(r.X0, r.Y0), at(r.X1, r.Y1))
	lo, hi := min(at(x0, y0), at(x1, y1)), max(at(x0, y0), at(x1, y1))
	bx, by := r.X0, r.Y0
	if hi-lo > e-a {
		bx, by = x0, y0
	}
	b := at(bx, by)
	a, e = min(a, lo), max(e, hi)
	r.X0, r.Y0 = bx+(a-b)*r.Dx, by+(a-b)*r.Dy
	r.X1, r.Y1 = bx+(e-b)*r.Dx, by+(e-b)*r.Dy
	r.LX, r.LY = r.X0, r.Y0
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
	// leftward is set by separator when it took the string for the next
	// one of text written from right to left.
	leftward bool
	// The string itself: its font and its text in interp.text.
	font           *font.Font
	textLo, textHi int32
	// trail is Run.Trail of the string.
	trail float64
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
	p.leftward = false
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
	// of the two, and starts before it by its own width or more than
	// the slack, which a narrow letter (yod, a full stop) is not.
	before := (p.sx-x1)*p.dx + (p.sy-y1)*p.dy
	width := (x1-x0)*p.dx + (y1-y0)*p.dy
	if fromStart < -min(slack, 0.5*width) && before <= gap && before >= -rightToLeftSlack*min(p.em(), size) {
		p.leftward = true
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
	// Word spacing that makes a space wider than the font size, in an
	// array that takes the spacing back after some of its spaces
	// (tabSpaces): the spaces it leaves wide are the way to the next cell
	// of a row, as a tab is, and what follows each a string of its own
	// (Run.Trail). The spaces of a justified line are all as wide, and
	// are left alone. Word spacing applies to the code 32 of a font of one
	// byte a code.
	if in.tabSpaces && g.tw > 0 && g.th > 0 && !f.Vertical && len(raw) > 1 && f.CodeLen(raw) == 1 {
		m := in.tm.mul(g.ctm)
		scale := g.th * hypot(m[0], m[1])
		if em := g.size * scale; em > 0 && g.tw*scale+f.SpaceWidth*em > em {
			for len(raw) > 0 && !in.full {
				n := bytes.IndexByte(raw, ' ') + 1
				for n > 0 && n < len(raw) && raw[n] == ' ' {
					n++
				}
				if n == 0 {
					n = len(raw)
				}
				in.showCodes(f, raw[:n])
				raw = raw[n:]
			}
			return
		}
	}
	in.showCodes(f, raw)
}

// kernBack takes a move against the direction of writing, of shift in
// text space, off the wide spaces that the string shown last ended with:
// some programs set the word spacing for one space of a line and take it
// back after each of the others.
func (in *interp) kernBack(shift float64) {
	p := &in.prev
	n := len(in.runs)
	if p.trail == 0 || n == 0 || in.runs[n-1].Trail != p.trail {
		return
	}
	m := in.tm.mul(in.gs.ctm)
	p.trail -= shift * hypot(m[0], m[1])
	if p.trail <= p.size {
		p.trail = 0
	}
	in.runs[n-1].Trail = p.trail
}

// showCodes shows the character codes of raw in the font f.
func (in *interp) showCodes(f *font.Font, raw []byte) {
	g := &in.gs

	// The text. Inside a sequence with /ActualText the glyphs are read
	// for their advance only: the first string shown stands for the
	// replacement text and the others for nothing.
	start := len(in.text)
	clustersFrom := len(in.clusters)
	var sh font.Shown
	// rtl is set for a string with letters written from right to left,
	// which take more bytes than glyphs: text of one byte a glyph is not
	// looked at.
	rtl := false
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
		in.byOutline += sh.ByOutline
		in.fontCuts(f)
		if in.opts.Words {
			in.markSpaces(f, raw)
		}
		// Glyphs of text written from right to left that stand for more
		// than one character: rare, and looked for only where the text
		// has more characters than codes and is in such a script.
		if len(in.text)-start > sh.Glyphs && f.MayRightToLeft() && font.HasRightToLeft(in.text[start:]) {
			rtl = true
			in.markClusters(f, raw, start)
		}
		// The dots of letters drawn in pieces, after the letter without
		// them in the same string (marks.go).
		if f.HasPieces() {
			in.composeMarks(start, start, clustersFrom)
		}
	}
	if first {
		rtl = font.HasRightToLeft(in.text[start:])
	}
	// The spaces of this string, when words are asked for. Nothing of
	// this is done otherwise: strings are shown by the thousand.
	var marks []mark
	if in.opts.Words {
		marks, in.marks = in.marks, in.marks[:0]
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
	p := &in.prev
	// The spaces the string ends with, when they are set wide.
	trail := 0.0
	if !f.Vertical && !replaced && !rtl && g.tw > 0 {
		n := 0
		for k := len(in.text); k > start && in.text[k-1] == ' '; k-- {
			n++
		}
		if n > 0 {
			t := float64(n)*ge.space + (float64(n)*g.tw+float64(n-1)*g.tc)*g.th*math.Hypot(ge.a, ge.b)
			if along := (x1-x0)*ge.dx + (y1-y0)*ge.dy; t > ge.size && t <= along {
				trail = t
			}
		}
	}
	prevTrail := p.trail
	// Marks drawn by themselves on the glyph before them: part of it,
	// however far up or down they are put (marks.go).
	if !replaced && start < len(in.text) && in.text[start] >= 0xCC && in.joinMarks(start, clustersFrom, clip, x0, y0, sh) {
		return
	}
	// The same string printed again where it was printed last: the way
	// bold type and shadows are faked. It is the same text once.
	// Only a copy that is drawn the same way is left out here: one that a
	// reader would see differently is for the layout to judge.
	if n := len(in.runs); !replaced && n > 0 && p.valid && p.serial == ge.serial && p.font == f &&
		in.runs[n-1].Clip == clip && in.sameDisplay(&in.runs[n-1]) && in.printedAgain(x0, y0, start) {
		in.text = in.text[:start]
		in.clusters = in.clusters[:clustersFrom]
		in.unmapped -= sh.Unmapped
		in.assumed -= sh.Assumed
		in.byOutline -= sh.ByOutline
		p.sx, p.sy = x0, y0
		if n := len(in.runs); n > 0 {
			in.runs[n-1].LX, in.runs[n-1].LY = x0, y0
		}
		return
	}
	sep := p.separator(x0, y0, x1, y1, ge.dx, ge.dy, ge.size)
	leftward := p.leftward
	along := (x0-p.ex)*p.dx + (y0-p.ey)*p.dy
	var endX, endY float64
	if in.opts.Words {
		endX, endY = p.ex, p.ey // where the string before this one ended
	}
	jointX, jointY := p.ex, p.ey
	p.font, p.textLo, p.textHi = f, int32(start), int32(len(in.text))
	p.sx, p.sy, p.ex, p.ey = x0, y0, x1, y1
	p.trail = trail
	if p.serial != ge.serial || !p.valid {
		p.valid, p.serial = true, ge.serial
		p.dx, p.dy, p.nx, p.ny = ge.dx, ge.dy, ge.nx, ge.ny
		p.size, p.sizeX, p.space, p.vertical = ge.size, ge.sizeX, ge.space, f.Vertical
	}

	if replaced && !first {
		// Part of a replaced sequence: it moved the position, no more.
		if n := len(in.runs); n > 0 && in.runs[n-1].ActualText {
			r := &in.runs[n-1]
			r.Glyphs += int32(sh.Glyphs)
			if r.RTL {
				r.cover(x0, y0, x1, y1)
			} else {
				r.X1, r.Y1 = x1, y1
				r.LX, r.LY = x0, y0
			}
		}
		return
	}

	// A string that continues the run before it, in the same state,
	// becomes part of that run.
	//
	// A gap wider than the font size ends the run: what follows may be
	// another entry of a row, or text that leaves room for a word drawn
	// later, and a reader of the runs has to see the gap.
	//
	// So does a step back along the line to the start of the string shown
	// last or before it: it is a piece of the line drawn out of its
	// order, or the line printed again, and the layout can tell which
	// only if it is a run of its own. A string that ends where the last
	// one began is the next one of text written from right to left, if
	// its script is; in any other script it is such a step back, and a
	// line of two strings that is printed again starts that way.
	back := sep == ' ' && along < 0 || leftward && !font.HasRightToLeft(in.text[start:])
	// A line with letters written from right to left is cut where its
	// strings were joined across gaps when it ends (endLine).
	if sep == '\n' || !p.valid {
		in.endLine()
	}
	in.rtlLine = in.rtlLine || rtl
	if n := len(in.runs); n > 0 && sep != '\n' && !replaced && !back && !(sep == ' ' && along > ge.size) && prevTrail == 0 && in.runs[n-1].Clip == clip && in.continues(&in.runs[n-1], f) {
		last := &in.runs[n-1]
		if sep == ' ' && start > 0 && in.text[start-1] != ' ' && len(in.text) > start && in.text[start] != ' ' {
			in.text = append(in.text, 0)
			copy(in.text[start+1:], in.text[start:])
			in.text[start] = ' '
			for i := clustersFrom; i < len(in.clusters); i++ {
				in.clusters[i].Off++
			}
			in.gaps = append(in.gaps, Gap{Off: int32(start), Width: float32(along)})
			if in.opts.Words {
				in.breaks = append(in.breaks, Break{Off: int32(start), X0: endX, Y0: endY, X1: x0, Y1: y0})
			}
			start++
		} else if in.opts.Words {
			if n := len(in.breaks); n > 0 && in.breaks[n-1].Off == int32(start)-1 {
				// The run ended with a space: the word after it begins
				// where this string begins, which a kern may have moved.
				in.breaks[n-1].X1, in.breaks[n-1].Y1 = x0, y0
			}
		}
		if sep == ' ' && in.opts.Rules && len(in.joins) < maxRunsPerPage {
			in.joins = append(in.joins, Join{Off: int32(start), X0: jointX, Y0: jointY, X1: x0, Y1: y0})
		}
		if len(marks) > 0 {
			if marks[0].glyphs == 0 {
				// The string begins with a space: the word before it ended
				// where the string before this one ended.
				in.breaks = append(in.breaks, Break{Off: int32(start) + marks[0].off, X0: endX, Y0: endY})
				b := &in.breaks[len(in.breaks)-1]
				b.X1, b.Y1 = in.afterMark(marks[0], x0, y0, f.Vertical)
				marks = marks[1:]
			}
			in.placeMarks(marks, start, x0, y0, f.Vertical)
		}
		// Noted where it can matter: on a line that has shown letters
		// written from right to left, and for spaces, punctuation and
		// numbers, which such a line can draw before its letters.
		if sep == ' ' && len(in.joints) < maxRunsPerPage && (in.rtlLine || !hasLetter(in.text[start:])) {
			in.joints = append(in.joints, joint{run: int32(n - 1), off: int32(start), ex: jointX, ey: jointY, sx: x0, sy: y0})
		}
		last.Len = int32(len(in.text)) - last.Off
		if leftward && !last.Back {
			last.Back, last.RX, last.RY = true, last.X1, last.Y1
		}
		last.RTL = last.RTL || rtl
		last.X1, last.Y1 = x1, y1
		last.LX, last.LY = x0, y0
		last.Trail = trail
		last.Glyphs += int32(sh.Glyphs)
		last.Unmapped += int32(sh.Unmapped)
		last.Assumed += int32(sh.Assumed)
		in.checkTextLimit()
		return
	}
	if len(in.runs) >= maxRunsPerPage {
		in.text = in.text[:start]
		in.clusters = in.clusters[:clustersFrom]
		in.tooMuchText()
		return
	}
	if len(marks) > 0 {
		in.placeMarks(marks, start, x0, y0, f.Vertical)
	}
	in.runs = append(in.runs, Run{
		Off: int32(start), Len: int32(len(in.text) - start),
		X0: x0, Y0: y0, X1: x1, Y1: y1, LX: x0, LY: y0, Dx: ge.dx, Dy: ge.dy, Nx: ge.nx, Ny: ge.ny,
		Size: ge.size, SizeX: ge.sizeX, Rise: g.rise * ge.lenY, SpaceWidth: ge.space, Font: f,
		Mode: g.mode, Clip: clip, FillAlpha: g.fillAlpha, StrokeAlpha: g.strokeAlpha,
		OptionalOff: in.offDepth > 0,
		Sep:         sep, ActualText: replaced, Glyphs: int32(sh.Glyphs), Unmapped: int32(sh.Unmapped), Assumed: int32(sh.Assumed),
		MCID: in.mcid, RTL: rtl, Trail: trail,
	})
	in.checkTextLimit()
}

// hasLetter reports whether text has a letter, of ASCII or not: what is
// not ASCII is taken for one, unread.
func hasLetter(text []byte) bool {
	for _, c := range text {
		if c >= 0x80 || c|0x20 >= 'a' && c|0x20 <= 'z' {
			return true
		}
	}
	return false
}

// joint is where a string was joined to the run before it across a gap,
// on the line being drawn: the run, where the string's text begins, where
// the string before it ended and where it begins.
type joint struct {
	run            int32
	off            int32
	ex, ey, sx, sy float64
}

// endLine ends the line being drawn. If it has letters written from
// right to left, its runs are cut where strings were joined across gaps:
// such a line can draw its spaces and punctuation before its letters,
// and it is read by where its pieces stand (the layout's bidi.go).
func (in *interp) endLine() {
	if in.rtlLine {
		for i := len(in.joints) - 1; i >= 0; i-- {
			j := &in.joints[i]
			if int(j.run) >= len(in.runs) || len(in.runs) >= maxRunsPerPage {
				continue
			}
			r := in.runs[j.run]
			if j.off <= r.Off || j.off >= r.Off+r.Len || r.Back || r.ActualText {
				continue
			}
			second := r
			second.Off, second.Len = j.off, r.Off+r.Len-j.off
			second.X0, second.Y0 = j.sx, j.sy
			second.Sep, second.Glyphs, second.Unmapped, second.Assumed = ' ', 0, 0, 0
			first := &in.runs[j.run]
			first.Len = j.off - r.Off
			// A space put in at the gap is the layout's to judge again,
			// from where the pieces stand.
			if g := sort.Search(len(in.gaps), func(i int) bool { return in.gaps[i].Off >= j.off-1 }); g < len(in.gaps) && in.gaps[g].Off == j.off-1 && first.Len > 1 {
				first.Len--
			}
			first.X1, first.Y1, first.LX, first.LY = j.ex, j.ey, j.ex, j.ey
			in.runs = slices.Insert(in.runs, int(j.run)+1, second)
		}
	}
	in.joints, in.rtlLine = in.joints[:0], false
}

// markClusters notes the codes of the string raw, whose text in the
// font f begins at start, that stand for more than one character.
func (in *interp) markClusters(f *font.Font, raw []byte, start int) {
	off := start
	for len(raw) > 0 && len(in.clusters) < maxRunsPerPage {
		n := min(max(f.CodeLen(raw), 1), len(raw))
		in.silent, _ = f.Append(in.silent[:0], raw[:n])
		if utf8.RuneCount(in.silent) > 1 {
			in.clusters = append(in.clusters, Cluster{Off: int32(off), Len: int32(len(in.silent))})
		}
		off += len(in.silent)
		raw = raw[n:]
	}
}

// markSpaces notes the spaces in the text of the string raw, which has
// just been shown in the font f: where each is in that text, and the
// advance of the string before and after it. The codes are gone through
// one by one, which Append does not do; it is done only when words are
// asked for.
func (in *interp) markSpaces(f *font.Font, raw []byte) {
	var sh font.Shown
	off := 0
	for len(raw) > 0 {
		n := min(max(f.CodeLen(raw), 1), len(raw))
		var one font.Shown
		in.silent, one = f.Append(in.silent[:0], raw[:n])
		if len(in.silent) == 1 && in.silent[0] == ' ' {
			in.marks = append(in.marks, mark{
				off: int32(off), glyphs: sh.Glyphs, spaces: sh.Spaces, before: sh.Advance,
				after: sh.Advance + one.Advance, glyphs2: sh.Glyphs + one.Glyphs, space2: sh.Spaces + one.Spaces,
			})
		}
		off += len(in.silent)
		sh.Advance += one.Advance
		sh.Glyphs += one.Glyphs
		sh.Spaces += one.Spaces
		raw = raw[n:]
	}
}

// along returns where a string that starts at (x0, y0) has got to after
// an advance, as showCodes works it out for the whole string.
func (in *interp) along(x0, y0, advance float64, glyphs, spaces int, vertical bool) (float64, float64) {
	g, ge := &in.gs, &in.geom
	spacing := float64(glyphs)*g.tc + float64(spaces)*g.tw
	if vertical {
		ty := advance*g.size - spacing
		return x0 + ty*ge.c, y0 + ty*ge.d
	}
	tx := (advance*g.size + spacing) * g.th
	return x0 + tx*ge.a, y0 + tx*ge.b
}

// afterMark returns where the space of a mark ends.
func (in *interp) afterMark(m mark, x0, y0 float64, vertical bool) (float64, float64) {
	return in.along(x0, y0, m.after, m.glyphs2, m.space2, vertical)
}

// placeMarks turns the marks of a string into breaks: the string's text
// is at start in the page's text, and the string starts at (x0, y0).
func (in *interp) placeMarks(marks []mark, start int, x0, y0 float64, vertical bool) {
	if len(marks) == 0 {
		return
	}
	g, ge := &in.gs, &in.geom
	at := func(advance float64, glyphs, spaces int) (float64, float64) {
		return in.along(x0, y0, advance, glyphs, spaces, vertical)
	}
	// A word ends with its last glyph: the character spacing after it is
	// not part of it, as it is not part of a string.
	tc := g.tc
	for _, m := range marks {
		b := Break{Off: int32(start) + m.off}
		b.X0, b.Y0 = at(m.before, m.glyphs, m.spaces)
		if m.glyphs > 0 && tc != 0 {
			if vertical {
				b.X0, b.Y0 = b.X0+tc*ge.c, b.Y0+tc*ge.d
			} else {
				b.X0, b.Y0 = b.X0-tc*g.th*ge.a, b.Y0-tc*g.th*ge.b
			}
		}
		b.X1, b.Y1 = at(m.after, m.glyphs2, m.space2)
		in.breaks = append(in.breaks, b)
	}
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
func (in *interp) continues(last *Run, f *font.Font) bool {
	g, ge := &in.gs, &in.geom
	return last.Font == f && in.sameDisplay(last) && !last.ActualText && last.MCID == in.mcid &&
		closeTo(last.Size, ge.size) && closeTo(last.SizeX, ge.sizeX) && last.Rise == g.rise*ge.lenY &&
		last.Dx*ge.dx+last.Dy*ge.dy > 0.9999
}

// sameDisplay reports whether a string shown now is drawn as the run
// last is, in everything the file declares about how it is shown: its
// render mode, its opacities and its optional content.
func (in *interp) sameDisplay(last *Run) bool {
	g := &in.gs
	return last.Mode == g.mode && last.FillAlpha == g.fillAlpha && last.StrokeAlpha == g.strokeAlpha &&
		last.OptionalOff == (in.offDepth > 0)
}

func (in *interp) checkTextLimit() {
	if len(in.text) > maxTextPerPage {
		in.tooMuchText()
	}
}

func (in *interp) tooMuchText() {
	in.full = true
	in.cut(boundText, 1, 0)
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
