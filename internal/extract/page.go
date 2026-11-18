// Package extract interprets a page's content streams and produces its
// text and image list.
//
// The text comes out as runs: stretches set in one font, size and colour
// on one line. Each run carries where it is on the page and what its
// visibility depends on (render mode, size, colour, opacity, optional
// content). Result.Text joins the runs in the order the page draws them,
// with a space or a line break where their positions call for one.
// Arranging runs into reading order and blocks is the job of the layout
// step that follows this package.
package extract

import (
	"bytes"
	"fmt"
	"sync"
	"unicode/utf8"

	gofastcore "github.com/AOShei/go-fast-core"
	"github.com/AOShei/go-fast-pdf/internal/charset"
	"github.com/AOShei/go-fast-pdf/internal/content"
	"github.com/AOShei/go-fast-pdf/internal/file"
	"github.com/AOShei/go-fast-pdf/internal/font"
	"github.com/AOShei/go-fast-pdf/internal/syntax"
)

// Image is one image drawn on a page.
type Image struct {
	// Inline is true for an image whose data is in the content stream.
	Inline bool
	// Name is the resource name of an image XObject, with its slash.
	Name string
	// Obj is the object number of an image XObject, 0 for an inline
	// image or a direct object.
	Obj int64
	// Rect is [x, y, width, height] in the page's coordinate space.
	Rect [4]float64
	// Quad is the image's four corners on the page.
	Quad Quad
	// Width and Height are the image's size in pixels.
	Width, Height float64
	// ColorSpace is the name of the color space, with its slash, when it
	// is a name.
	ColorSpace string
	// Seq is the number of things other than text painted on the page
	// before the image, and After the number of runs drawn before it.
	Seq, After int32
	// Opaque is false for an image that lets what is under it show: one
	// with a mask or a soft mask, a stencil, or one painted with an
	// opacity below 1.
	Opaque bool
}

// Content is what a page's content streams draw, as the interpreter
// holds it. It is valid only inside Options.Use.
type Content struct {
	// Runs are the page's text runs in the order they are drawn, and
	// Text holds their text: run r's is Text[r.Off : r.Off+r.Len].
	Runs []Run
	Text []byte
	// Gaps are the spaces in Text that are not in the file, by position.
	Gaps []Gap
	// Forms are the boxes of the Form XObjects that text is drawn in;
	// Run.Form is an index into it.
	Forms []Quad
	// Images are the images drawn, in order.
	Images []Image
}

// Options says what to extract besides text.
type Options struct {
	// Images returns the images on the page in Result.Images.
	Images bool
	// Runs returns the page's runs in Result.Runs.
	Runs bool
	// Use, when set, is called with the page's content once it is read,
	// before the buffers that hold it are reused. Result.Text is then
	// not made.
	Use func(*Content)
	// MaxFormDepth bounds the nesting of Form XObjects; 0 means 16.
	MaxFormDepth int
	// MaxDecodedBytes bounds the content decoded for one page; 0 means
	// 512 MiB.
	MaxDecodedBytes int64
}

// Result is what one page yields.
type Result struct {
	Text string
	// Runs is nil unless Options.Runs is set. RunText holds their text:
	// run r's is RunText[r.Off : r.Off+r.Len].
	Runs    []Run
	RunText []byte
	// Forms are the boxes of the Form XObjects that text was drawn in;
	// Run.Form is an index into it.
	Forms []Quad
	// Images is nil unless Options.Images is set.
	Images []Image
	// Warnings are the problems met on this page.
	Warnings []gofastcore.Warning
	// Decoded is the number of bytes of content decoded for the page.
	Decoded int64
	// External is set when the page draws from outside the file: a
	// content stream or an XObject whose data is in another file, or a
	// reference XObject.
	External bool
	// Two facts that are counted for measurement and reported nowhere.
	// ActualDiffers is the number of /ActualText sequences whose text is
	// not the text of the glyphs they replace, letters and digits
	// compared; ActualLonger is how many of them say at least 16 bytes
	// more than their glyphs. LongGlyphText is the number of strings
	// shown in a font in which one character code stands for more than
	// 24 bytes of text.
	ActualDiffers, ActualLonger, LongGlyphText int
	// Err is set when the page could not be read at all.
	Err error
}

// Limits on what one page may produce. A content stream can show the
// same string millions of times.
const (
	maxStateDepth   = 4096
	maxMarkedDepth  = 4096
	maxFormsPerPage = 10000
)

// Variables so that a test can reach them with a small page.
var (
	maxRunsPerPage = 1 << 19
	maxTextPerPage = 16 << 20
)

// resources is one resource dictionary with the parts of it that have
// been looked up.
type resources struct {
	dict                                     syntax.Value
	fonts, xobjs, gstates, spaces, props     syntax.Value
	hasFonts, hasXObjs, hasGS, hasCS, hasPrs bool
	fontCache                                map[string]*font.Font
}

func (r *resources) reset(dict syntax.Value) {
	*r = resources{dict: dict, fontCache: r.fontCache}
	if r.fontCache == nil {
		r.fontCache = map[string]*font.Font{}
	}
	clear(r.fontCache)
}

// marked is one open marked-content sequence.
type marked struct {
	// off is set when the sequence belongs to optional content that is
	// switched off.
	off bool
	// actual is set when the sequence has /ActualText: what it draws is
	// replaced by that text, at the first glyph drawn.
	actual  bool
	emitted bool
	// The text is in interp.actual[lo:hi].
	lo, hi int32
}

// interp holds the state of one page's interpretation. Its buffers are
// reused from page to page through a pool.
type interp struct {
	f    *file.File
	opts Options
	res  *Result

	scans    []*content.Scanner // one per nesting level of forms
	pageBufs [][]byte           // the page's content streams, decoded
	formBufs [][]byte           // decoded form content, one per level
	rs       []resources        // one per level
	level    int
	// What the content of each level may not undo: the depth of the
	// state stack and of the marked content when it began.
	stackFloor, mcFloor []int

	gs    gstate
	stack []gstate
	// geom is derived from the state; geomValid is cleared by whatever
	// changes what it is derived from.
	geom      geometry
	geomValid bool
	// Text object state: not part of the graphics state.
	tm, tlm matrix

	mc       []marked
	offDepth int    // marked sequences and forms that are switched off
	actual   []byte // the /ActualText of the open sequences
	inActual int    // open sequences with /ActualText

	text   []byte // the text of all runs
	runs   []Run
	gaps   []Gap
	images []Image
	forms  []Quad
	form   int32 // the form being drawn, -1 on the page itself
	seq    int32 // paint operations so far, text excluded
	prev   lineEnd
	full   bool // a limit was reached: no more text is taken
	str    []byte
	name   []byte
	silent []byte

	unmapped    int
	assumed     int
	fontMissing bool

	formActive []uint32
	formCalls  int
}

var interps = sync.Pool{New: func() any { return new(interp) }}

// Page extracts one page. It never panics: a failure inside the
// interpreter is returned in Result.Err, with whatever text was produced
// before it.
func Page(f *file.File, p *file.Page, opts Options) (res Result) {
	if p.Err != nil {
		return Result{Err: p.Err}
	}
	if opts.MaxFormDepth <= 0 {
		opts.MaxFormDepth = 16
	}
	if opts.MaxDecodedBytes <= 0 {
		opts.MaxDecodedBytes = 512 << 20
	}
	in := interps.Get().(*interp)
	defer func() {
		if r := recover(); r != nil {
			// A last line of defence. Fuzzing treats reaching it as a
			// failure, so it does not hide defects.
			res.Err = fmt.Errorf("%w: internal error on page %d: %v", gofastcore.ErrMalformed, p.Number, r)
			res.Text = string(bytes.ToValidUTF8(assemble(nil, in.runs, in.text), []byte("�")))
			interps.Put(new(interp)) // do not reuse a state that failed
			return
		}
		in.f, in.res = nil, nil
		in.trim()
		interps.Put(in)
	}()
	in.reset(f, p, opts, &res)
	in.run(p)
	in.finish()
	return res
}

// finish turns what the interpreter collected into the result.
func (in *interp) finish() {
	res := in.res
	if in.opts.Use != nil {
		if !utf8.Valid(in.text) {
			// Every text the fonts hand out is valid by construction;
			// this is the final check that no caller ever receives text
			// that is not UTF-8. Nothing may move: runs and gaps point
			// into the text by position.
			for i, c := range in.text {
				if c >= 0x80 {
					in.text[i] = '?'
				}
			}
			in.unmapped++
		}
		in.opts.Use(&Content{Runs: in.runs, Text: in.text, Gaps: in.gaps, Forms: in.forms, Images: in.images})
	} else {
		out := assemble(in.str[:0], in.runs, in.text)
		in.str = out
		if !utf8.Valid(out) {
			out = bytes.ToValidUTF8(out, []byte("�"))
			in.unmapped++
		}
		res.Text = string(out)
	}
	if in.opts.Images {
		res.Images = append([]Image{}, in.images...)
	}
	if in.unmapped > 0 {
		in.warn(gofastcore.SeverityWarning, gofastcore.WarnTextUnmappedGlyphs,
			fmt.Sprintf("%d character codes on the page have no mapping to text and appear as U+FFFD", in.unmapped))
	}
	if in.assumed > 0 {
		in.warn(gofastcore.SeverityInfo, "pdf.glyph_text_assumed",
			fmt.Sprintf("%d characters on the page are set in fonts that do not say which characters their glyphs are; they were read as the characters their codes stand for", in.assumed))
	}
	if in.fontMissing {
		in.warn(gofastcore.SeverityWarning, "pdf.font_missing",
			"text on the page is set in a font that its resources do not define; it was read with the standard encoding")
	}
	if in.opts.Runs {
		res.Runs = append([]Run{}, in.runs...)
		res.RunText = append([]byte{}, in.text...)
		res.Forms = append([]Quad{}, in.forms...)
	}
}

// trim drops buffers that one unusually large page grew, so that the
// pool does not hold on to them.
func (in *interp) trim() {
	const keep = 4 << 20
	for _, bufs := range [][][]byte{in.pageBufs, in.formBufs} {
		for i := range bufs {
			if cap(bufs[i]) > keep {
				bufs[i] = nil
			}
		}
	}
	if cap(in.text) > keep {
		in.text = nil
	}
	if cap(in.str) > keep {
		in.str = nil
	}
	if cap(in.silent) > keep {
		in.silent = nil
	}
	if cap(in.actual) > keep {
		in.actual = nil
	}
	if cap(in.runs) > keep/64 {
		in.runs = nil
	}
	if cap(in.gaps) > keep/8 {
		in.gaps = nil
	}
	if cap(in.images) > 4096 {
		in.images = nil
	}
	if cap(in.stack) > 256 {
		in.stack = nil
	}
	// Fonts are kept alive by the file's cache, not by this one.
	for i := range in.rs {
		clear(in.rs[i].fontCache)
		in.rs[i].dict = syntax.Value{}
	}
}

func (in *interp) reset(f *file.File, p *file.Page, opts Options, res *Result) {
	in.f, in.opts, in.res = f, opts, res
	in.level = 0
	in.resourcesAt(0).reset(p.Resources)
	in.stackFloor, in.mcFloor = append(in.stackFloor[:0], 0), append(in.mcFloor[:0], 0)
	in.gs = initialState()
	in.geomValid = false
	in.stack = in.stack[:0]
	in.tm, in.tlm = identity, identity
	in.mc, in.offDepth, in.actual, in.inActual = in.mc[:0], 0, in.actual[:0], 0
	in.text, in.runs, in.forms = in.text[:0], in.runs[:0], in.forms[:0]
	in.gaps, in.images = in.gaps[:0], in.images[:0]
	in.form, in.seq, in.prev, in.full = -1, 0, lineEnd{}, false
	in.unmapped, in.assumed, in.fontMissing = 0, 0, false
	in.formActive, in.formCalls = in.formActive[:0], 0
}

func (in *interp) resourcesAt(level int) *resources {
	for len(in.rs) <= level {
		in.rs = append(in.rs, resources{})
	}
	return &in.rs[level]
}

func (in *interp) scannerAt(level int) *content.Scanner {
	for len(in.scans) <= level {
		in.scans = append(in.scans, new(content.Scanner))
	}
	return in.scans[level]
}

func (in *interp) warn(severity gofastcore.Severity, code, message string) {
	// One warning per code is enough for a page.
	for _, w := range in.res.Warnings {
		if w.Code == code {
			return
		}
	}
	in.res.Warnings = append(in.res.Warnings, gofastcore.Warning{Code: code, Severity: severity, Message: message})
}

// decodeContent decodes one content stream into the buffer slot i and
// returns the data, or nil if nothing usable came out.
func (in *interp) decodeContent(bufs *[][]byte, i int, stm syntax.Value) []byte {
	if stm.Kind != syntax.Stream {
		return nil
	}
	if stm.Has("F") {
		// The data is in another file. It is not fetched.
		in.res.External = true
	}
	for len(*bufs) <= i {
		*bufs = append(*bufs, nil)
	}
	if (*bufs)[i] == nil {
		// Never nil: Decode returns the file's own bytes for a nil
		// buffer, and those must not be reused as scratch space.
		(*bufs)[i] = make([]byte, 0, 4096)
	}
	room := in.opts.MaxDecodedBytes - in.res.Decoded
	if room <= 0 {
		in.warn(gofastcore.SeverityError, gofastcore.WarnLimitExceeded, "the page's content is larger than the limit; the rest is not read")
		return nil
	}
	data, err := in.f.Decode(stm, (*bufs)[i][:0], min(room, in.f.Limits().MaxStreamBytes))
	(*bufs)[i] = data
	in.res.Decoded += int64(len(data))
	if err != nil {
		switch {
		case err == file.ErrTruncated:
			in.warn(gofastcore.SeverityError, gofastcore.WarnLimitExceeded, "a content stream is larger than the limit and was cut there")
		case len(data) > 0:
			in.warn(gofastcore.SeverityWarning, "pdf.content_stream_damaged", "a content stream is damaged; what could be decoded before the damage was read")
		default:
			in.warn(gofastcore.SeverityWarning, "pdf.content_stream_unreadable", err.Error())
			return nil
		}
	}
	return data
}

func (in *interp) run(p *file.Page) {
	// /Contents is one stream or an array of them.
	contents := in.f.Resolve(p.Dict.Get("Contents"))
	streams := []syntax.Value{contents}
	if contents.Kind == syntax.Array {
		streams = contents.List
	}
	scan := in.scannerAt(0)
	first := true
	for i, ref := range streams {
		// Each stream gets a buffer of its own: operands read from one
		// stream may still be in use when the next is decoded.
		data := in.decodeContent(&in.pageBufs, i, in.f.Resolve(ref))
		if data == nil {
			continue
		}
		if first {
			scan.Reset(data)
			first = false
		} else {
			scan.Feed(data)
		}
		in.exec(scan)
	}
	if scan.Skipped > 0 {
		in.warn(gofastcore.SeverityInfo, "pdf.content_bytes_skipped",
			fmt.Sprintf("%d bytes of the content stream are not valid syntax and were skipped", scan.Skipped))
	}
}

// exec runs the operators of a scanner to its end.
func (in *interp) exec(scan *content.Scanner) {
	for {
		op, args, ok := scan.Next()
		if !ok {
			return
		}
		in.operator(scan, op, args)
	}
}

// last returns the final n operands, or nil if there are fewer. Operands
// before them are leftovers of damage and are ignored.
func last(args []content.Operand, n int) []content.Operand {
	if len(args) < n {
		return nil
	}
	return args[len(args)-n:]
}

func toMatrix(a []content.Operand) matrix {
	return matrix{finite(a[0].Float()), finite(a[1].Float()), finite(a[2].Float()), finite(a[3].Float()), finite(a[4].Float()), finite(a[5].Float())}
}

func (in *interp) operator(scan *content.Scanner, op []byte, args []content.Operand) {
	g := &in.gs
	switch string(op) {
	// --- graphics state ---
	case "q":
		if len(in.stack) < maxStateDepth {
			in.stack = append(in.stack, in.gs)
		}
	case "Q":
		if n := len(in.stack); n > in.stackFloor[in.level] {
			in.gs = in.stack[n-1]
			in.stack = in.stack[:n-1]
			in.geomValid = false
		}
	case "cm":
		if a := last(args, 6); a != nil {
			g.ctm = toMatrix(a).mul(g.ctm)
			in.geomValid = false
		}
	case "gs":
		if a := last(args, 1); a != nil && a[0].Kind == content.Name {
			in.extGState(a[0].Raw)
			in.geomValid = false
		}

	// --- text state ---
	case "BT":
		in.tm, in.tlm = identity, identity
		in.geomValid = false
	case "Tc":
		if a := last(args, 1); a != nil {
			g.tc = finite(a[0].Float())
		}
	case "Tw":
		if a := last(args, 1); a != nil {
			g.tw = finite(a[0].Float())
		}
	case "Tz":
		if a := last(args, 1); a != nil {
			g.th = finite(a[0].Float()) / 100
			in.geomValid = false
		}
	case "TL":
		if a := last(args, 1); a != nil {
			g.tl = finite(a[0].Float())
		}
	case "Ts":
		if a := last(args, 1); a != nil {
			g.rise = finite(a[0].Float())
		}
	case "Tr":
		if a := last(args, 1); a != nil {
			if mode := a[0].Float(); mode >= 0 && mode <= 7 {
				g.mode = uint8(mode)
			}
		}
	case "Tf":
		if a := last(args, 2); a != nil {
			if a[0].Kind == content.Name {
				g.font = in.lookupFont(a[0].Raw)
				g.fontSet = true
			}
			g.size = finite(a[1].Float())
			in.geomValid = false
		}

	// --- text position ---
	case "Td":
		if a := last(args, 2); a != nil {
			in.moveLine(finite(a[0].Float()), finite(a[1].Float()))
		}
	case "TD":
		if a := last(args, 2); a != nil {
			g.tl = -finite(a[1].Float())
			in.moveLine(finite(a[0].Float()), finite(a[1].Float()))
		}
	case "Tm":
		if a := last(args, 6); a != nil {
			in.tm = toMatrix(a)
			in.tlm = in.tm
			in.geomValid = false
		}
	case "T*":
		in.moveLine(0, -g.tl)

	// --- text showing ---
	case "Tj":
		if a := last(args, 1); a != nil {
			in.show(a[0])
		}
	case "TJ":
		if a := last(args, 1); a != nil && a[0].Kind == content.Array {
			for _, e := range scan.Elems(a[0]) {
				if e.IsNumber() {
					// Thousandths of the font size, against the
					// direction of writing.
					shift := -finite(e.Float()) / 1000 * g.size
					if g.font != nil && g.font.Vertical {
						in.advance(0, shift)
					} else {
						in.advance(shift*g.th, 0)
					}
				} else {
					in.show(e)
				}
			}
		}
	case "'":
		in.moveLine(0, -g.tl)
		if a := last(args, 1); a != nil {
			in.show(a[0])
		}
	case "\"":
		if a := last(args, 3); a != nil {
			g.tw, g.tc = finite(a[0].Float()), finite(a[1].Float())
			in.moveLine(0, -g.tl)
			in.show(a[2])
		}

	// --- colour ---
	case "g":
		in.setColor(&g.fill, csGray, last(args, 1))
	case "G":
		in.setColor(&g.stroke, csGray, last(args, 1))
	case "rg":
		in.setColor(&g.fill, csRGB, last(args, 3))
	case "RG":
		in.setColor(&g.stroke, csRGB, last(args, 3))
	case "k":
		in.setColor(&g.fill, csCMYK, last(args, 4))
	case "K":
		in.setColor(&g.stroke, csCMYK, last(args, 4))
	case "cs":
		if a := last(args, 1); a != nil && a[0].Kind == content.Name {
			in.setSpace(&g.fill, a[0].Raw)
		}
	case "CS":
		if a := last(args, 1); a != nil && a[0].Kind == content.Name {
			in.setSpace(&g.stroke, a[0].Raw)
		}
	case "sc", "scn":
		in.setComponents(&g.fill, args)
	case "SC", "SCN":
		in.setComponents(&g.stroke, args)

	// --- marked content ---
	case "BMC":
		in.beginMarked(nil, content.Operand{})
	case "BDC":
		if a := last(args, 2); a != nil {
			in.beginMarked(a[0].Raw, a[1])
		} else {
			in.beginMarked(nil, content.Operand{})
		}
	case "EMC":
		in.endMarked()

	// --- painting ---
	case "S", "s", "f", "F", "f*", "B", "B*", "b", "b*", "sh":
		in.seq++
	case "BI":
		in.inlineImage(args)
		in.seq++
	case "Do":
		if a := last(args, 1); a != nil && a[0].Kind == content.Name {
			in.drawXObject(a[0].Raw)
		}
	}
}

func (in *interp) moveLine(tx, ty float64) {
	in.tlm = matrix{1, 0, 0, 1, tx, ty}.mul(in.tlm)
	in.tm = in.tlm
}

// advance moves the text position by a displacement in text space.
func (in *interp) advance(tx, ty float64) {
	in.tm[4] += tx*in.tm[0] + ty*in.tm[2]
	in.tm[5] += tx*in.tm[1] + ty*in.tm[3]
}

// decodeName returns a name operand with its #xx escapes resolved.
func (in *interp) decodeName(raw []byte) []byte {
	for _, c := range raw {
		if c == '#' {
			p := syntax.NewParser(append(append(in.name[:0], '/'), raw...), 0, 0)
			v, _ := p.Object()
			in.name = append(in.name[:0], v.Data...)
			return in.name
		}
	}
	return raw
}

// Lookups in the current resource dictionary. Each part of it is
// resolved once per page and level.

func (in *interp) fontDict() syntax.Value {
	r := &in.rs[in.level]
	if !r.hasFonts {
		r.fonts, r.hasFonts = in.f.Resolve(r.dict.Get("Font")), true
	}
	return r.fonts
}

func (in *interp) xobjectDict() syntax.Value {
	r := &in.rs[in.level]
	if !r.hasXObjs {
		r.xobjs, r.hasXObjs = in.f.Resolve(r.dict.Get("XObject")), true
	}
	return r.xobjs
}

func (in *interp) gstateDict() syntax.Value {
	r := &in.rs[in.level]
	if !r.hasGS {
		r.gstates, r.hasGS = in.f.Resolve(r.dict.Get("ExtGState")), true
	}
	return r.gstates
}

func (in *interp) spaceDict() syntax.Value {
	r := &in.rs[in.level]
	if !r.hasCS {
		r.spaces, r.hasCS = in.f.Resolve(r.dict.Get("ColorSpace")), true
	}
	return r.spaces
}

func (in *interp) propertyDict() syntax.Value {
	r := &in.rs[in.level]
	if !r.hasPrs {
		r.props, r.hasPrs = in.f.Resolve(r.dict.Get("Properties")), true
	}
	return r.props
}

func (in *interp) lookupFont(raw []byte) *font.Font {
	name := in.decodeName(raw)
	cache := in.rs[in.level].fontCache
	if f, ok := cache[string(name)]; ok {
		return f
	}
	var f *font.Font
	if ref := in.fontDict().Get(string(name)); !ref.IsNull() {
		f = font.Load(in.f, ref)
	}
	cache[string(name)] = f
	return f
}

// --- marked content and optional content ---

func (in *interp) beginMarked(tag []byte, props content.Operand) {
	if len(in.mc) >= maxMarkedDepth {
		return
	}
	m := marked{}
	// The property list is a dictionary in the stream or a name in the
	// /Properties resources. Only two things in it matter here, and most
	// lists have neither: look before parsing.
	var dict syntax.Value
	switch props.Kind {
	case content.Dict:
		if bytes.Contains(props.Raw, []byte("ActualText")) {
			in.name = append(append(append(in.name[:0], "<<"...), props.Raw...), ">>"...)
			parser := syntax.NewParser(in.name, 0, 64)
			dict, _ = parser.Object()
		}
	case content.Name:
		ref := in.propertyDict().Get(string(in.decodeName(props.Raw)))
		if string(tag) == "OC" {
			m.off = in.contentOff(ref)
		} else {
			dict = in.f.Resolve(ref)
		}
	}
	if text := in.f.Resolve(dict.Get("ActualText")); text.Kind == syntax.String {
		m.actual = true
		m.lo = int32(len(in.actual))
		in.actual = append(in.actual, charset.TextString(text.Data)...)
		m.hi = int32(len(in.actual))
		in.inActual++
	}
	if m.off {
		in.offDepth++
	}
	in.mc = append(in.mc, m)
}

func (in *interp) endMarked() {
	// A form cannot close what its caller opened.
	if n := len(in.mc); n > in.mcFloor[in.level] {
		in.popMarked()
	}
}

func (in *interp) popMarked() {
	n := len(in.mc)
	m := in.mc[n-1]
	in.mc = in.mc[:n-1]
	if m.off {
		in.offDepth--
	}
	if m.actual {
		in.inActual--
		if in.inActual == 0 && m.emitted {
			in.compareActual(in.actual[m.lo:m.hi])
		}
		in.actual = in.actual[:m.lo]
	}
}

// compareActual counts a replacement text that says something other
// than the glyphs it replaces, which are in in.silent.
func (in *interp) compareActual(actual []byte) {
	a, g := letters(actual), letters(in.silent)
	if !bytes.Equal(a, g) {
		in.res.ActualDiffers++
		if len(actual) >= len(in.silent)+16 {
			in.res.ActualLonger++
		}
	}
}

// letters returns b without what an ordinary replacement changes: white
// space, hyphens and soft hyphens. It works in place on a copy when it
// has to change anything.
func letters(b []byte) []byte {
	out := b[:0:0]
	for i := 0; i < len(b); i++ {
		switch c := b[i]; {
		case c == ' ' || c == '-' || c == '\n' || c == '\t':
		case c == 0xC2 && i+1 < len(b) && (b[i+1] == 0xAD || b[i+1] == 0xA0):
			i++
		default:
			out = append(out, c)
		}
	}
	return out
}

// --- images and forms ---

func (in *interp) imageRect() [4]float64 {
	c := in.gs.ctm
	return [4]float64{c[4], c[5], hypot(c[0], c[1]), hypot(c[2], c[3])}
}

// maxImagesPerPage bounds the images listed for one page. A page that
// draws one image a million times is not asking for a million entries.
const maxImagesPerPage = 100000

// addImage records an image drawn now: where it is, and when in the
// order of painting. It is called before the image is counted as
// painted.
func (in *interp) addImage(img Image) {
	if len(in.images) >= maxImagesPerPage {
		in.warn(gofastcore.SeverityError, gofastcore.WarnLimitExceeded, "more than 100000 images on the page; the rest are not listed")
		return
	}
	img.Rect = in.imageRect()
	// An image fills the unit square of the coordinate system it is
	// drawn in.
	img.Quad = in.gs.ctm.quad([4]float64{0, 0, 1, 1})
	img.Seq, img.After = in.seq, int32(len(in.runs))
	img.Opaque = img.Opaque && in.gs.fillAlpha >= 1 && in.offDepth == 0
	in.images = append(in.images, img)
}

func (in *interp) inlineImage(args []content.Operand) {
	img := Image{Inline: true, Opaque: true}
	for i := 0; i+1 < len(args); i += 2 {
		key, val := args[i], args[i+1]
		if key.Kind != content.Name {
			continue
		}
		switch string(key.Raw) {
		case "W", "Width":
			img.Width = val.Float()
		case "H", "Height":
			img.Height = val.Float()
		case "CS", "ColorSpace":
			if val.Kind == content.Name {
				img.ColorSpace = "/" + string(val.Raw)
			}
		case "IM", "ImageMask":
			if val.Kind == content.Other && string(val.Raw) == "true" {
				img.Opaque = false
			}
		}
	}
	in.addImage(img)
}

// drawXObject handles Do: an image is counted as painted and, when
// images are asked for, recorded; a form is interpreted. Nothing is
// decoded for an image: its size is in its dictionary.
func (in *interp) drawXObject(rawName []byte) {
	name := in.decodeName(rawName)
	xobj := in.f.Resolve(in.xobjectDict().Get(string(name)))
	if !xobj.IsDict() {
		return
	}
	switch subtype := in.f.Resolve(xobj.Get("Subtype")); {
	case subtype.IsName("Image"):
		img := Image{Name: "/" + string(name), Obj: int64(xobj.Obj)}
		img.Width = in.f.Resolve(xobj.Get("Width")).Number()
		img.Height = in.f.Resolve(xobj.Get("Height")).Number()
		if cs := in.f.Resolve(xobj.Get("ColorSpace")); cs.Kind == syntax.Name {
			img.ColorSpace = "/" + cs.NameString()
		}
		stencil := in.f.Resolve(xobj.Get("ImageMask"))
		img.Opaque = !(stencil.Kind == syntax.Bool && stencil.N != 0) && !xobj.Has("SMask") && !xobj.Has("Mask")
		if xobj.Has("F") {
			in.res.External = true
		}
		in.addImage(img)
		in.seq++
	case subtype.IsName("Form"):
		if xobj.Has("Ref") || xobj.Has("F") {
			in.res.External = true
		}
		in.drawForm(xobj)
	}
}

// drawForm interprets a Form XObject: its content, under its /Matrix,
// with its own resources. Depth, the forms already open and the total
// number of forms are all bounded, so a form that draws itself, or a
// tree of forms that doubles at every level, ends.
func (in *interp) drawForm(xobj syntax.Value) {
	if xobj.Kind != syntax.Stream {
		return
	}
	if in.level >= in.opts.MaxFormDepth || in.formCalls >= maxFormsPerPage {
		in.warn(gofastcore.SeverityError, gofastcore.WarnLimitExceeded, "Form XObjects are nested or repeated beyond the limit; the rest are not read")
		return
	}
	for _, open := range in.formActive {
		if open == xobj.Obj {
			return // the form draws itself
		}
	}
	in.formCalls++
	in.formActive = append(in.formActive, xobj.Obj)

	// What the form may change is put back when it ends: the graphics
	// state, the text position, and the marked content it left open.
	saved, savedTm, savedTlm, savedForm := in.gs, in.tm, in.tlm, in.form
	in.geomValid = false
	stackDepth, mcDepth := len(in.stack), len(in.mc)
	off := in.contentOff(xobj.Get("OC"))
	if off {
		in.offDepth++
	}
	if m := in.f.Resolve(xobj.Get("Matrix")).Elems(); len(m) == 6 {
		var fm matrix
		for i := range fm {
			fm[i] = finite(in.f.Resolve(m[i]).Number())
		}
		in.gs.ctm = fm.mul(in.gs.ctm)
	}
	if box, ok := in.f.Rect(xobj.Get("BBox")); ok && len(in.forms) < maxFormsPerPage {
		in.forms = append(in.forms, in.gs.ctm.quad(box))
		in.form = int32(len(in.forms) - 1)
	}

	level := in.level + 1
	// A form without resources of its own uses those of what draws it.
	dict := in.f.Resolve(xobj.Get("Resources"))
	if !dict.IsDict() {
		dict = in.rs[in.level].dict
	}
	in.resourcesAt(level).reset(dict)
	for len(in.stackFloor) <= level {
		in.stackFloor = append(in.stackFloor, 0)
		in.mcFloor = append(in.mcFloor, 0)
	}
	in.stackFloor[level], in.mcFloor[level] = stackDepth, mcDepth

	if data := in.decodeContent(&in.formBufs, level, xobj); data != nil {
		scan := in.scannerAt(level)
		scan.Reset(data)
		in.level = level
		in.exec(scan)
		in.level = level - 1
	}

	for len(in.mc) > mcDepth {
		in.popMarked()
	}
	if off {
		in.offDepth--
	}
	in.stack = in.stack[:min(len(in.stack), stackDepth)]
	in.gs, in.tm, in.tlm, in.form = saved, savedTm, savedTlm, savedForm
	in.geomValid = false
	in.formActive = in.formActive[:len(in.formActive)-1]
}
