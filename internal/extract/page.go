// Package extract interprets a page's content streams and produces its
// text and image list.
//
// In Phase 1 the text logic is that of v0.1.1, carried over to the new
// engine without change in behaviour: positions are judged the same way
// and text is decoded the same way, defects included (B2, B3, B5, B9 and
// the F items of IMPLEMENTATION_PLAN.md, all closed in Phase 4). What is
// new is underneath: nothing here can panic on malformed operands, one
// bad byte does not cost the page, and a page allocates a handful of
// times instead of thousands.
package extract

import (
	"fmt"
	"math"
	"strings"
	"sync"
	"unicode/utf8"

	gofastcore "github.com/AOShei/go-fast-core"
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
	// Rect is [x, y, width, height] in the page's coordinate space.
	Rect [4]float64
	// Width and Height are the image's size in pixels.
	Width, Height float64
	// ColorSpace is the name of the color space, with its slash, when it
	// is a name.
	ColorSpace string
}

// Options says what to extract besides text.
type Options struct {
	// Images lists the images on the page.
	Images bool
	// MaxFormDepth bounds the nesting of Form XObjects; 0 means 16.
	MaxFormDepth int
	// MaxDecodedBytes bounds the content decoded for one page; 0 means
	// 512 MiB.
	MaxDecodedBytes int64
}

// Result is what one page yields.
type Result struct {
	Text string
	// Images is nil unless Options.Images is set.
	Images []Image
	// Warnings are the problems met on this page.
	Warnings []gofastcore.Warning
	// Decoded is the number of bytes of content decoded for the page.
	Decoded int64
	// Err is set when the page could not be read at all.
	Err error
}

type matrix [6]float64

var identity = matrix{1, 0, 0, 1, 0, 0}

func (a matrix) mul(b matrix) matrix {
	return matrix{
		a[0]*b[0] + a[1]*b[2],
		a[0]*b[1] + a[1]*b[3],
		a[2]*b[0] + a[3]*b[2],
		a[2]*b[1] + a[3]*b[3],
		a[4]*b[0] + a[5]*b[2] + b[4],
		a[4]*b[1] + a[5]*b[3] + b[5],
	}
}

// interp holds the state of one page's interpretation. Its buffers are
// reused from page to page through a pool.
type interp struct {
	f    *file.File
	opts Options
	res  *Result

	scan   content.Scanner
	bufs   [][]byte // decoded content streams of the current page
	out    []byte   // the page's text
	str    []byte   // scratch for one string operand
	name   []byte   // scratch for a decoded name
	fonts  map[string]*font.Font
	fontRs syntax.Value // the page's /Font resources
	xobjs  syntax.Value // the page's /XObject resources

	ctm   matrix
	stack []matrix

	// Text state.
	font        *font.Font
	fontSize    float64
	charSpacing float64
	wordSpacing float64
	scale       float64
	leading     float64
	tm, tlm     matrix

	lastX, lastY float64

	// Form XObjects being read, for the image list.
	formScans  []content.Scanner
	formBufs   [][]byte
	formActive []uint32
	formCalls  int
}

var interps = sync.Pool{New: func() any { return &interp{fonts: map[string]*font.Font{}} }}

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
			res.Text = toValidUTF8(in.out)
			interps.Put(&interp{fonts: map[string]*font.Font{}}) // do not reuse a state that failed
			return
		}
		in.f, in.res = nil, nil
		in.trim()
		interps.Put(in)
	}()
	in.reset(f, p, opts, &res)
	in.run(p)
	res.Text = in.text()
	return res
}

// text returns the page's text as valid UTF-8. The decoding carried over
// from v0.1.1 can emit a raw byte for a code it has no mapping for; such
// bytes become U+FFFD here, so that no caller ever receives a string
// that is not UTF-8.
func (in *interp) text() string {
	if utf8.Valid(in.out) {
		return string(in.out)
	}
	in.warn(gofastcore.SeverityWarning, gofastcore.WarnTextUnmappedGlyphs,
		"some character codes have no mapping to text and appear as U+FFFD")
	return toValidUTF8(in.out)
}

// toValidUTF8 replaces every byte that is not part of valid UTF-8 with
// U+FFFD, one replacement per byte, so that the number of unreadable
// characters stays visible.
func toValidUTF8(b []byte) string {
	var sb strings.Builder
	sb.Grow(len(b) + 16)
	for len(b) > 0 {
		r, size := utf8.DecodeRune(b)
		if r == utf8.RuneError && size == 1 {
			sb.WriteString("\uFFFD")
		} else {
			sb.Write(b[:size])
		}
		b = b[size:]
	}
	return sb.String()
}

// trim drops buffers that one unusually large page grew, so that the
// pool does not hold on to them.
func (in *interp) trim() {
	const keep = 4 << 20
	for _, bufs := range []*[][]byte{&in.bufs, &in.formBufs} {
		for i := range *bufs {
			if cap((*bufs)[i]) > keep {
				(*bufs)[i] = nil
			}
		}
	}
	if cap(in.out) > keep {
		in.out = nil
	}
	if cap(in.str) > keep {
		in.str = nil
	}
}

func (in *interp) reset(f *file.File, p *file.Page, opts Options, res *Result) {
	in.f, in.opts, in.res = f, opts, res
	in.out, in.stack = in.out[:0], in.stack[:0]
	clear(in.fonts)
	in.fontRs = f.Resolve(p.Resources.Get("Font"))
	in.xobjs = syntax.Value{}
	if opts.Images {
		in.xobjs = f.Resolve(p.Resources.Get("XObject"))
		res.Images = []Image{}
	}
	in.ctm = identity
	in.font, in.fontSize, in.charSpacing, in.wordSpacing, in.leading = nil, 0, 0, 0, 0
	in.scale = 100
	in.tm, in.tlm = identity, identity
	in.lastX, in.lastY = 0, 0
	in.formActive, in.formCalls = in.formActive[:0], 0
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
	first := true
	for i, ref := range streams {
		data := in.decodeContent(&in.bufs, i, in.f.Resolve(ref))
		if data == nil {
			continue
		}
		if first {
			in.scan.Reset(data)
			first = false
		} else {
			in.scan.Feed(data)
		}
		for {
			op, args, ok := in.scan.Next()
			if !ok {
				break
			}
			in.operator(op, args)
		}
	}
	if in.scan.Skipped > 0 {
		in.warn(gofastcore.SeverityInfo, "pdf.content_bytes_skipped",
			fmt.Sprintf("%d bytes of the content stream are not valid syntax and were skipped", in.scan.Skipped))
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
	return matrix{a[0].Float(), a[1].Float(), a[2].Float(), a[3].Float(), a[4].Float(), a[5].Float()}
}

func (in *interp) operator(op []byte, args []content.Operand) {
	switch string(op) {
	case "q":
		if len(in.stack) < 4096 {
			in.stack = append(in.stack, in.ctm)
		}
	case "Q":
		if n := len(in.stack); n > 0 {
			in.ctm = in.stack[n-1]
			in.stack = in.stack[:n-1]
		}
	case "cm":
		if a := last(args, 6); a != nil {
			in.ctm = toMatrix(a).mul(in.ctm)
		}
	case "BT":
		in.tm, in.tlm = identity, identity
	case "Tc":
		if a := last(args, 1); a != nil {
			in.charSpacing = a[0].Float()
		}
	case "Tw":
		if a := last(args, 1); a != nil {
			in.wordSpacing = a[0].Float()
		}
	case "Tz":
		if a := last(args, 1); a != nil {
			in.scale = a[0].Float()
		}
	case "TL":
		if a := last(args, 1); a != nil {
			in.leading = a[0].Float()
		}
	case "Tf":
		if a := last(args, 2); a != nil {
			if a[0].Kind == content.Name {
				if f := in.lookupFont(a[0].Raw); f != nil {
					in.font = f
				}
			}
			in.fontSize = a[1].Float()
		}
	case "Td":
		if a := last(args, 2); a != nil {
			in.moveLine(a[0].Float(), a[1].Float())
		}
	case "TD":
		if a := last(args, 2); a != nil {
			in.leading = -a[1].Float()
			in.moveLine(a[0].Float(), a[1].Float())
		}
	case "Tm":
		if a := last(args, 6); a != nil {
			in.tm = toMatrix(a)
			in.tlm = in.tm
		}
	case "T*":
		in.moveLine(0, -in.leading)
	case "Tj":
		if a := last(args, 1); a != nil {
			in.showText(a[0])
		}
	case "TJ":
		if a := last(args, 1); a != nil && a[0].Kind == content.Array {
			for _, e := range in.scan.Elems(a[0]) {
				if e.IsNumber() {
					shift := -e.Float() / 1000 * in.fontSize * (in.scale / 100)
					in.tm[4] += shift * in.tm[0]
					in.tm[5] += shift * in.tm[1]
				} else {
					in.showText(e)
				}
			}
		}
	case "'":
		in.moveLine(0, -in.leading)
		if a := last(args, 1); a != nil {
			in.showText(a[0])
		}
	case "\"":
		if a := last(args, 3); a != nil {
			in.wordSpacing, in.charSpacing = a[0].Float(), a[1].Float()
			in.moveLine(0, -in.leading)
			in.showText(a[2])
		}
	case "BI":
		if in.opts.Images {
			in.inlineImage(args)
		}
	case "Do":
		if a := last(args, 1); in.opts.Images && a != nil && a[0].Kind == content.Name {
			in.drawXObject(in.xobjs, a[0].Raw, 0)
		}
	}
}

func (in *interp) moveLine(tx, ty float64) {
	in.tlm = matrix{1, 0, 0, 1, tx, ty}.mul(in.tlm)
	in.tm = in.tlm
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

func (in *interp) lookupFont(raw []byte) *font.Font {
	name := in.decodeName(raw)
	if f, ok := in.fonts[string(name)]; ok {
		return f
	}
	var f *font.Font
	if ref := in.fontRs.Get(string(name)); !ref.IsNull() {
		f = font.Load(in.f, ref)
	}
	in.fonts[string(name)] = f
	return f
}

func endsWith(b []byte, c byte) bool { return len(b) > 0 && b[len(b)-1] == c }

// showText is the text logic of v0.1.1: decide from the position whether
// a line break or a space precedes the string, decode it, and advance by
// its width.
func (in *interp) showText(o content.Operand) {
	if !o.IsString() {
		return
	}
	in.str = o.AppendString(in.str[:0])
	raw := in.str

	fm := in.tm.mul(in.ctm)
	x, y := fm[4], fm[5]

	// A gap wider than half a space, or than 0.2 of the font size when
	// the font gives no space width, is a space.
	spaceWidth := 0.0
	if in.font != nil {
		spaceWidth = in.font.SpaceWidth / 1000 * in.fontSize * (in.scale / 100)
	}
	threshold := in.fontSize * 0.2
	if spaceWidth > 0 {
		threshold = spaceWidth * 0.5
	}
	if math.Abs(y-in.lastY) > in.fontSize*0.5 {
		if len(in.out) > 0 {
			in.out = append(in.out, '\n')
		}
	} else if x-in.lastX > threshold {
		if len(in.out) > 0 && !endsWith(in.out, '\n') && !endsWith(in.out, ' ') {
			in.out = append(in.out, ' ')
		}
	}

	// Decode.
	start := len(in.out)
	switch f := in.font; {
	case f != nil && f.HasCMap:
		for i := 0; i < len(raw); {
			if i+1 < len(raw) {
				if s, ok := f.Two(raw[i], raw[i+1]); ok {
					in.out = append(in.out, s...)
					i += 2
					continue
				}
			}
			if s, ok := f.One(raw[i]); ok {
				in.out = append(in.out, s...)
			} else {
				in.out = append(in.out, raw[i])
			}
			i++
		}
	case f != nil && f.HasEncoding:
		for _, b := range raw {
			if s, ok := f.Encoded(b); ok {
				in.out = append(in.out, s...)
			} else {
				in.out = append(in.out, b)
			}
		}
	default:
		// No mapping: keep printable ASCII and white space.
		for _, b := range raw {
			if b >= 0x20 && b <= 0x7E || b == '\t' || b == '\n' || b == '\r' {
				in.out = append(in.out, b)
			}
		}
	}
	decoded := in.out[start:]

	// Width.
	var width float64
	if in.font != nil && in.font.HasWidths {
		for _, b := range raw {
			if w, ok := in.font.Width(b); ok {
				width += w
			}
		}
		width = width / 1000 * in.fontSize
		width += float64(len(raw)) * in.charSpacing
		spaces := 0
		for _, b := range decoded {
			if b == ' ' {
				spaces++
			}
		}
		width += float64(spaces) * in.wordSpacing
		width *= in.scale / 100
	} else {
		width = float64(len(decoded)) * in.fontSize * 0.5 * (in.scale / 100)
	}

	in.lastX, in.lastY = x+width, y
	in.tm[4] += width * in.tm[0]
	in.tm[5] += width * in.tm[1]
}

// --- images ---

func (in *interp) imageRect() [4]float64 {
	c := in.ctm
	return [4]float64{c[4], c[5], math.Hypot(c[0], c[1]), math.Hypot(c[2], c[3])}
}

func (in *interp) addImage(img Image) {
	// A page that draws one image a million times is not asking for a
	// million entries.
	if len(in.res.Images) >= 100000 {
		in.warn(gofastcore.SeverityError, gofastcore.WarnLimitExceeded, "more than 100000 images on the page; the rest are not listed")
		return
	}
	in.res.Images = append(in.res.Images, img)
}

func (in *interp) inlineImage(args []content.Operand) {
	img := Image{Inline: true, Rect: in.imageRect()}
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
		}
	}
	in.addImage(img)
}

// drawXObject handles Do for the image list: an image is recorded, and a
// form is searched for the images it draws. Nothing is decoded for an
// image: its size is in its dictionary.
func (in *interp) drawXObject(xobjs syntax.Value, rawName []byte, depth int) {
	name := string(in.decodeName(rawName))
	xobj := in.f.Resolve(xobjs.Get(name))
	if !xobj.IsDict() {
		return
	}
	switch subtype := in.f.Resolve(xobj.Get("Subtype")); {
	case subtype.IsName("Image"):
		img := Image{Name: "/" + name, Rect: in.imageRect()}
		img.Width = in.f.Resolve(xobj.Get("Width")).Number()
		img.Height = in.f.Resolve(xobj.Get("Height")).Number()
		if cs := in.f.Resolve(xobj.Get("ColorSpace")); cs.Kind == syntax.Name {
			img.ColorSpace = "/" + cs.NameString()
		}
		in.addImage(img)
	case subtype.IsName("Form"):
		in.form(xobj, depth)
	}
}

// form reads a Form XObject's content for the images it draws. Depth,
// the forms already open and the total number of forms are all bounded,
// so a form that draws itself, or a tree of forms that doubles at every
// level, ends.
func (in *interp) form(xobj syntax.Value, depth int) {
	if xobj.Kind != syntax.Stream {
		return
	}
	if depth >= in.opts.MaxFormDepth || in.formCalls >= 10000 {
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
	defer func() { in.formActive = in.formActive[:len(in.formActive)-1] }()

	data := in.decodeContent(&in.formBufs, depth, xobj)
	if data == nil {
		return
	}
	xobjs := in.f.Resolve(in.f.Resolve(xobj.Get("Resources")).Get("XObject"))
	for len(in.formScans) <= depth {
		in.formScans = append(in.formScans, content.Scanner{})
	}
	in.formScans[depth].Reset(data)
	for {
		// Index again on every round: a nested form may grow the slice.
		op, args, ok := in.formScans[depth].Next()
		if !ok {
			break
		}
		if string(op) == "Do" {
			if a := last(args, 1); a != nil && a[0].Kind == content.Name {
				in.drawXObject(xobjs, a[0].Raw, depth+1)
			}
		}
	}
}
