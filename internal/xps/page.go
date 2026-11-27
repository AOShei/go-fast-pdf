package xps

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"unicode/utf8"

	gofastcore "github.com/AOShei/go-fast-core"
	"github.com/AOShei/go-fast-core/opc"
	"github.com/AOShei/go-fast-core/xmlscan"
	"github.com/AOShei/go-fast-pdf/internal/extract"
	"github.com/AOShei/go-fast-pdf/internal/font"
)

// WarnFontUnreadable: text of the page is set in a font whose program
// is missing from the package or cannot be read. The text is what the
// page says it is; where it stands is known only as far as the page
// states the advance of each glyph.
const WarnFontUnreadable = "xps.font_unreadable"

// Bounds on what one page may hold.
const (
	// maxCanvasDepth bounds the nesting of Canvas elements that is
	// followed; what is nested deeper is not read.
	maxCanvasDepth = 256
	// unit takes the units of a page, 1/96 inch, to points.
	unit = 72.0 / 96.0
	// maxCoord bounds the numbers of a page: no page is a billion units
	// across, and sizes made from larger numbers would not be numbers.
	maxCoord = 1e9
)

type matrix [6]float64

// mul returns the transformation that applies a and then b.
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

// PageInfo is what a page says of itself.
type PageInfo struct {
	// Width and Height are the size of the page in points.
	Width, Height float64
	// Decoded is the size of the page's markup, decompressed.
	Decoded int64
	// Spent is set when the limit on decompressed bytes was reached
	// while the page was read.
	Spent bool
	// External is set when the page names a resource outside the
	// package: a resource dictionary or an image. Nothing is fetched.
	External bool
}

// canvas is what an open Canvas gives the elements in it.
type canvas struct {
	m       matrix  // to the page, in points with y upwards
	opacity float64 // the opacities declared around the element, multiplied
	mark    int     // the resources that were there when it was opened
}

// glyphs is a Glyphs element as it is being read.
type glyphs struct {
	uni, indices, fontURI []byte
	x, y, size            float64
	m                     matrix
	transformed           bool
	opacity               float64
	bidi                  int
	sideways              bool
	bold, italic          bool
	fill                  bool // a Fill is stated, as an attribute or an element
	clear                 bool // the Fill is a colour that is fully transparent
	invalid               bool // the text had bytes that are no character
	// The names of the resources that its transformation and its fill
	// are, when they are written as such, in key.
	key, transformKey, fillKey []byte
}

// item is one cluster of a Glyphs element: the text of some of its
// characters, and the glyphs that show them.
type item struct {
	lo, hi   int32 // its text in Worker.text
	adv, nat float64
	glyphs   int32
	unmapped int32
	space    bool
}

// Worker keeps, for one goroutine that reads the pages of a document,
// the scanner and the buffers one page leaves for the next. The zero
// Worker is ready.
type Worker struct {
	s      xmlscan.Scanner
	stack  []canvas
	g      glyphs
	text   []byte
	str    []byte // the string being shown
	items  []item
	spaces []extract.Space
	clus   []extract.Cluster
	res    resources
	paths  []*pathState       // one for each Path that is open
	scans  []*xmlscan.Scanner // for the resource dictionaries of other parts
}

// reader reads one page: the part being scanned, which is the page or a
// resource dictionary it draws on, and what the page has shown so far.
type reader struct {
	d    *Document
	w    *Worker
	fx   *extract.Fixed
	s    *xmlscan.Scanner
	name string // the part being scanned: what its addresses are relative to
	info *PageInfo
	// remote counts the resource dictionaries of other parts read for
	// the page, and open is how many of them are being read now.
	remote, open int
	// ops counts the segments of the paths looked at for rules.
	ops int

	// pathLevel is how many Path elements are open: one inside the
	// visual of the brush of another.
	pathLevel int

	fontMissing, invalid, tooDeep, external, resourcesCut, damaged bool
}

// ReadPage reads page i of the document into a page of positioned
// strings, begun here with opts and ended before it returns. A page
// whose markup is damaged yields what was read before the damage, with
// a warning in res. The error is not nil when nothing of the page could
// be read. info is filled as the page is read: its size is there when
// opts.Use is called.
func (d *Document) ReadPage(i int, opts extract.Options, res *extract.Result, w *Worker, info *PageInfo) error {
	name := d.Pages[i].Part
	info.Decoded = max(d.pkg.Size(name), 0)
	closer, err := d.pkg.Scan(name, &w.s, &scanOptions)
	if err != nil {
		if errors.Is(err, opc.ErrNotFound) {
			return fmt.Errorf("%w: the document names a page %q that the package does not have", gofastcore.ErrMalformed, d.pkg.Label(name))
		}
		info.Spent = errors.Is(err, opc.ErrBudget)
		return fmt.Errorf("%w: %v", gofastcore.ErrMalformed, err)
	}
	defer closer.Close()
	fx := extract.NewFixed(opts, res)
	defer fx.Finish()
	r := &reader{d: d, w: w, fx: fx, s: &w.s, name: name, info: info}
	s := r.s
	w.stack = w.stack[:0]
	w.res.reset()
	// The root says how large the page is.
	for {
		k := s.Next()
		if k == xmlscan.EOF {
			break
		}
		if k != xmlscan.Start {
			continue
		}
		if !s.Is(spFixed, "FixedPage") {
			return fmt.Errorf("%w: the page %q is not a FixedPage", gofastcore.ErrMalformed, d.pkg.Label(name))
		}
		width, height := attrNumber(s, "Width"), attrNumber(s, "Height")
		if !(width > 0) || !(height > 0) {
			// The size is required. A page without one is read as a
			// page of the size most pages have, so that its text has
			// places.
			width, height = 816, 1056
		}
		info.Width, info.Height = width*unit, height*unit
		w.stack = append(w.stack, canvas{m: matrix{unit, 0, 0, -unit, 0, height * unit}, opacity: 1})
		r.content()
		break
	}
	if err := s.Err(); err != nil {
		if errors.Is(err, opc.ErrBudget) {
			info.Spent = true
		}
		if len(w.stack) == 0 {
			return fmt.Errorf("%w: the page %q: %v", gofastcore.ErrMalformed, d.pkg.Label(name), err)
		}
		fx.Warn(gofastcore.SeverityError, gofastcore.WarnInputStreamDamaged,
			fmt.Sprintf("the markup of the page is damaged at byte %d; what follows it is not read", s.Offset()))
	}
	info.External = r.external
	if r.tooDeep {
		fx.Warn(gofastcore.SeverityError, gofastcore.WarnLimitExceeded,
			fmt.Sprintf("the page nests Canvas elements more than %d deep; what is nested deeper is not read", maxCanvasDepth-1))
	}
	if r.resourcesCut {
		fx.Warn(gofastcore.SeverityError, gofastcore.WarnLimitExceeded,
			fmt.Sprintf("the page has more than %d resources, or draws on more than %d resource dictionaries of other parts; the rest are not read", maxResources, maxRemote))
	}
	if r.damaged {
		fx.Warn(gofastcore.SeverityWarning, gofastcore.WarnInputStreamDamaged,
			"the markup of a resource dictionary that the page draws on is damaged; the resources after the damage are not read")
	}
	if r.invalid {
		fx.Warn(gofastcore.SeverityWarning, gofastcore.WarnTextUnmappedGlyphs,
			"text on the page has bytes that are no characters; they appear as U+FFFD")
	}
	if r.fontMissing {
		fx.Warn(gofastcore.SeverityWarning, WarnFontUnreadable,
			"text on the page is set in a font that the package does not have or that cannot be read; the text is what the page says it is, placed by the advances the page states")
	}
	return nil
}

// content reads what is in the element whose start tag was read last: a
// FixedPage, a Canvas or the visual of a brush. It returns at the end of
// that element.
func (r *reader) content() {
	s := r.s
	for !r.fx.Full() {
		switch s.Next() {
		case xmlscan.EOF, xmlscan.End:
			return
		case xmlscan.Start:
		default:
			continue
		}
		if s.Space() != spFixed {
			s.Skip()
			continue
		}
		switch string(s.Local()) {
		case "Canvas":
			r.canvas()
		case "Glyphs":
			r.glyphs()
		case "Path":
			r.path()
		case "Canvas.RenderTransform":
			// Of the Canvas that was opened last, over what is around it.
			if m, ok := r.transform(); ok {
				if n := len(r.w.stack); n >= 2 {
					r.w.stack[n-1].m = m.mul(r.w.stack[n-2].m)
				}
			}
		case "FixedPage.Resources", "Canvas.Resources":
			r.resources()
		default:
			// What else an element holds: its clip, its opacity mask.
			s.Skip()
		}
	}
	// The page takes no more: what is left of the element is passed over.
	for d := s.Depth(); s.Depth() >= d; {
		if s.Next() == xmlscan.EOF {
			return
		}
	}
}

// canvas reads a Canvas element, whose start tag is current.
func (r *reader) canvas() {
	s, w := r.s, r.w
	if len(w.stack) >= maxCanvasDepth {
		r.tooDeep = true
		s.Skip()
		return
	}
	top := w.stack[len(w.stack)-1]
	if v, ok := s.Attr(xmlscan.NoSpace, "RenderTransform"); ok {
		if m, ok := r.matrixValue(v); ok {
			top.m = m.mul(top.m)
		}
	}
	top.opacity *= attrOpacity(s)
	top.mark = w.res.mark()
	w.stack = append(w.stack, top)
	r.content()
	if n := len(w.stack); n > 1 {
		w.res.release(w.stack[n-1].mark)
		w.stack = w.stack[:n-1]
	}
}

// transform reads an element that holds a transformation, such as
// Canvas.RenderTransform, whose start tag is current, to its end: the
// matrix of the MatrixTransform in it.
func (r *reader) transform() (m matrix, ok bool) {
	s := r.s
	for d := s.Depth(); ; {
		k := s.Next()
		if k == xmlscan.EOF || k == xmlscan.End && s.Depth() < d {
			return m, ok
		}
		if k == xmlscan.Start && s.Is(spFixed, "MatrixTransform") {
			if v, has := s.Attr(xmlscan.NoSpace, "Matrix"); has {
				if mm, good := parseMatrix(v); good {
					m, ok = mm, true
				}
			}
		}
	}
}

// matrixValue reads a transformation written as an attribute: its six
// numbers, or the name of a resource that is one.
func (r *reader) matrixValue(v []byte) (matrix, bool) {
	if key, ok := staticResource(v); ok {
		if res := r.w.res.find(key); res != nil && res.kind == resMatrix {
			return res.m, true
		}
		return matrix{}, false
	}
	return parseMatrix(v)
}

// glyphs reads a Glyphs element, whose start tag is current, and shows
// its text.
func (r *reader) glyphs() {
	s, w := r.s, r.w
	g := w.readGlyphs(s)
	if g.transformKey != nil {
		if res := w.res.find(g.transformKey); res != nil && res.kind == resMatrix {
			g.m, g.transformed = res.m, true
		}
	}
	if g.fillKey != nil {
		if res := w.res.find(g.fillKey); res != nil && res.kind == resBrush {
			g.fill, g.clear = true, res.brush.clear()
		}
	}
	for d := s.Depth(); ; {
		k := s.Next()
		if k == xmlscan.EOF || k == xmlscan.End && s.Depth() < d {
			break
		}
		if k != xmlscan.Start {
			continue
		}
		switch {
		case s.Is(spFixed, "Glyphs.RenderTransform"):
			if m, ok := r.transform(); ok {
				g.m, g.transformed = m, true
			}
		case s.Is(spFixed, "Glyphs.Fill"):
			// What the text is painted with. The visual of a brush is
			// what it is drawn with, not text of the page.
			if b, ok := r.brushIn(nil); ok {
				g.fill, g.clear = true, b.clear()
			}
		default:
			s.Skip()
		}
	}
	r.invalid = r.invalid || g.invalid
	if !r.d.show(r.fx, r.name, w) {
		r.fontMissing = true
	}
}

// readGlyphs takes the attributes of a Glyphs element. They are copied:
// what the scanner hands out lasts until its next token, and the element
// is shown when it ends, after the elements in it.
func (w *Worker) readGlyphs(s *xmlscan.Scanner) *glyphs {
	g := &w.g
	*g = glyphs{uni: g.uni[:0], indices: g.indices[:0], fontURI: g.fontURI[:0], key: g.key[:0], opacity: 1}
	for i := 0; i < s.NumAttr(); i++ {
		space, local, v := s.AttrAt(i)
		if space != xmlscan.NoSpace {
			continue
		}
		switch string(local) {
		case "UnicodeString":
			// A string that begins with a brace is written with an
			// empty pair of them before it.
			if len(v) >= 2 && v[0] == '{' && v[1] == '}' {
				v = v[2:]
			}
			if utf8.Valid(v) {
				g.uni = append(g.uni, v...)
				continue
			}
			// Bytes that are no character: each is U+FFFD, here and not
			// later, since the text is taken apart by its characters.
			g.invalid = true
			for len(v) > 0 {
				r, n := utf8.DecodeRune(v)
				g.uni = utf8.AppendRune(g.uni, r)
				v = v[n:]
			}
		case "Indices":
			g.indices = append(g.indices, v...)
		case "FontUri":
			g.fontURI = append(g.fontURI, v...)
		case "OriginX":
			g.x = number(v)
		case "OriginY":
			g.y = number(v)
		case "FontRenderingEmSize":
			g.size = number(v)
		case "BidiLevel":
			g.bidi = int(number(v))
		case "IsSideways":
			g.sideways = string(v) == "true"
		case "StyleSimulations":
			switch string(v) {
			case "BoldSimulation":
				g.bold = true
			case "ItalicSimulation":
				g.italic = true
			case "BoldItalicSimulation":
				g.bold, g.italic = true, true
			}
		case "RenderTransform":
			if key, ok := staticResource(v); ok {
				n := len(g.key)
				g.key = append(g.key, key...)
				g.transformKey = g.key[n:len(g.key):len(g.key)]
			} else if m, ok := parseMatrix(v); ok {
				g.m, g.transformed = m, true
			}
		case "Opacity":
			g.opacity = opacity(v)
		case "Fill":
			if key, ok := staticResource(v); ok {
				n := len(g.key)
				g.key = append(g.key, key...)
				g.fillKey = g.key[n:len(g.key):len(g.key)]
			} else {
				g.fill = true
				g.clear = transparent(v)
			}
		}
	}
	return g
}

// show hands the Glyphs element read last to the page. It reports false
// when the font of the element could not be read.
func (d *Document) show(fx *extract.Fixed, source string, w *Worker) bool {
	g := &w.g
	if !(g.size > 0) || len(g.uni) == 0 && len(g.indices) == 0 || len(w.stack) == 0 {
		return true
	}
	top := &w.stack[len(w.stack)-1]
	m := top.m
	if g.transformed {
		m = g.m.mul(m)
	}
	for _, v := range m {
		if v != v || v > maxCoord || v < -maxCoord {
			return true
		}
	}
	face := d.face(source, g.fontURI)
	var f *font.Font
	space := 0.25
	if face != nil {
		f = face.Font(g.bold, g.italic)
		space = f.SpaceWidth
	} else {
		f = d.unknownFont(string(g.fontURI))
	}
	// What the file declares of how the text is drawn: an element with
	// no fill has no visible effect, and neither has one whose fill or
	// opacity is fully transparent. Nothing else is looked at.
	mode, alpha := uint8(0), float32(top.opacity*g.opacity)
	if !g.fill {
		mode = 3
	}
	if g.clear {
		alpha = 0
	}
	fx.Font(f, g.size, m, mode, alpha)
	w.clusters(face)

	size := g.size
	gap := min(max(0.5*space*size, 0.08*size), 0.30*size)
	back := 0.30 * size
	if g.bidi&1 == 1 {
		w.showLeftward(fx)
		return face != nil
	}
	str := w.str[:0]
	pen, from, ink, spaceRun := 0.0, 0.0, 0.0, 0.0
	// Where the spaces that end the string so far begin: in its text,
	// on the line, and among its spaces.
	spaceAt, spacePen, spaceMarks := 0, 0.0, 0
	glyphCount, unmapped := 0, 0
	w.spaces = w.spaces[:0]
	flush := func() {
		fx.Show(str, glyphCount, unmapped, g.x+from, g.y, ink-from, w.spaces)
		str, glyphCount, unmapped, from = str[:0], 0, 0, pen
		w.spaces = w.spaces[:0]
	}
	for i := range w.items {
		it := &w.items[i]
		if it.space {
			if spaceRun == 0 {
				spaceAt, spacePen, spaceMarks = len(str), pen, len(w.spaces)
			}
			w.spaces = append(w.spaces, extract.Space{Off: int32(len(str)), Before: pen - from, After: pen + it.adv - from})
		}
		str = append(str, w.text[it.lo:it.hi]...)
		glyphCount += int(it.glyphs)
		unmapped += int(it.unmapped)
		pen += it.adv
		ink = pen
		if it.space {
			// Spaces set wider than the font size are the way to the
			// next entry of a row, as a move between two strings of a PDF
			// is: the string ends before them, and what follows is a
			// string of its own, that far away.
			spaceRun += it.adv
			if spaceRun > size && (i+1 == len(w.items) || !w.items[i+1].space) {
				str, w.spaces, ink = str[:spaceAt], w.spaces[:spaceMarks], spacePen
				flush()
			}
			continue
		}
		spaceRun = 0
		// A glyph that advances further than it is wide leaves a gap, as
		// a move between two strings of a PDF does; one that advances far
		// less steps back. What follows either is a string of its own.
		if d := it.adv - it.nat; d > gap || d < -back {
			ink = pen - it.adv + it.nat
			flush()
		}
	}
	if len(str) > 0 {
		flush()
	}
	w.str = str
	return face != nil
}

// showLeftward shows the element read last when it is written from
// right to left: its glyphs advance leftwards from its origin, and its
// text is in the order of reading.
func (w *Worker) showLeftward(fx *extract.Fixed) {
	g := &w.g
	str := w.str[:0]
	glyphCount, unmapped := 0, 0
	w.spaces, w.clus = w.spaces[:0], w.clus[:0]
	pen := 0.0
	for i := range w.items {
		it := &w.items[i]
		off := int32(len(str))
		if it.space {
			w.spaces = append(w.spaces, extract.Space{Off: off, Before: pen, After: pen + it.adv})
		}
		t := w.text[it.lo:it.hi]
		if utf8.RuneCount(t) > 1 {
			w.clus = append(w.clus, extract.Cluster{Off: off, Len: int32(len(t))})
		}
		str = append(str, t...)
		glyphCount += int(it.glyphs)
		unmapped += int(it.unmapped)
		pen += it.adv
	}
	fx.ShowBack(str, glyphCount, unmapped, g.x, g.y, pen, w.spaces, w.clus)
	w.str = str
}

// clusters reads the text and the indices of the element read last into
// its clusters, in the order they are written.
func (w *Worker) clusters(face *font.Face) {
	g := &w.g
	uni, ind, size := g.uni, g.indices, g.size
	w.items, w.text = w.items[:0], w.text[:0]
	pos, ip := 0, 0
	for pos < len(uni) || ip < len(ind) {
		e := parseEntry(ind, &ip)
		it := item{lo: int32(len(w.text))}
		// The characters of the cluster, counted in units of UTF-16 as
		// the indices count them.
		first, lo := rune(-1), pos
		for units := 0; units < e.units && pos < len(uni); {
			r, n := utf8.DecodeRune(uni[pos:])
			if first < 0 {
				first = r
			}
			pos += n
			units++
			if r >= 0x10000 {
				units++
			}
		}
		w.text = append(w.text, uni[lo:pos]...)
		// Its glyphs: this entry, and those after it that it counts in.
		for k := 0; ; k++ {
			gid := e.glyph
			if gid < 0 && face != nil && first >= 0 && k == 0 {
				gid = face.Glyph(first)
			}
			nat := math.NaN()
			if face != nil && gid >= 0 {
				if g.sideways {
					nat = face.Height(gid) * size
				} else {
					nat = face.Advance(gid) * size
				}
			}
			adv := nat
			if e.advance == e.advance {
				adv = e.advance * size / 100
			}
			if adv != adv {
				adv = 0.5 * size // nothing says how wide the glyph is
			}
			if nat != nat {
				nat = adv
			}
			adv = min(max(adv, -maxCoord), maxCoord)
			it.adv += adv
			it.nat += nat
			it.glyphs++
			if lo == pos && len(uni) == 0 {
				// The element has no text of its own: the glyph is read
				// by the font's table of characters, turned round.
				var r rune
				if face != nil && gid > 0 {
					r = face.Rune(gid)
				}
				if r == 0 {
					r = utf8.RuneError
					it.unmapped++
				}
				w.text = utf8.AppendRune(w.text, r)
			}
			if k+1 >= e.glyphs || ip >= len(ind) {
				break
			}
			e2 := parseEntry(ind, &ip)
			e.glyph, e.advance = e2.glyph, e2.advance
		}
		it.hi = int32(len(w.text))
		t := w.text[it.lo:it.hi]
		it.space = len(t) > 0 && len(bytes.TrimLeft(t, " ")) == 0
		w.items = append(w.items, it)
	}
}

// entry is one entry of an Indices attribute.
type entry struct {
	units, glyphs int     // the cluster it begins: characters and glyphs
	glyph         int     // the glyph, -1 when it is not stated
	advance       float64 // in hundredths of the font size, NaN when not stated
}

// maxCluster bounds the characters and the glyphs of one cluster.
const maxCluster = 1 << 12

// parseEntry reads the entry of ind at *ip and moves *ip past it and the
// semicolon after it. An entry that is not there is one character with
// one glyph, neither stated.
func parseEntry(ind []byte, ip *int) entry {
	e := entry{units: 1, glyphs: 1, glyph: -1, advance: math.NaN()}
	i := *ip
	if i >= len(ind) {
		return e
	}
	digits := func() (int, bool) {
		v, any := 0, false
		for i < len(ind) && ind[i] >= '0' && ind[i] <= '9' {
			if v < 1<<24 {
				v = v*10 + int(ind[i]-'0')
			}
			i++
			any = true
		}
		return v, any
	}
	skipSpace := func() {
		for i < len(ind) && (ind[i] == ' ' || ind[i] == '\t' || ind[i] == '\n' || ind[i] == '\r') {
			i++
		}
	}
	skipSpace()
	if i < len(ind) && ind[i] == '(' {
		i++
		if v, ok := digits(); ok {
			e.units = min(v, maxCluster)
		}
		if i < len(ind) && ind[i] == ':' {
			i++
			if v, ok := digits(); ok {
				e.glyphs = min(max(v, 1), maxCluster)
			}
		}
		if i < len(ind) && ind[i] == ')' {
			i++
		}
	}
	if v, ok := digits(); ok {
		e.glyph = min(v, 0xFFFF)
	}
	skipSpace()
	if i < len(ind) && ind[i] == ',' {
		i++
		j := i
		for j < len(ind) && ind[j] != ',' && ind[j] != ';' {
			j++
		}
		if v, ok := parseNumber(ind[i:j]); ok {
			e.advance = v
		}
		i = j
	}
	// The offsets of the glyph, and anything that is not an entry.
	for i < len(ind) && ind[i] != ';' {
		i++
	}
	if i < len(ind) {
		i++
	}
	*ip = i
	return e
}

// attrNumber returns the number an attribute of the current element
// states, NaN for none.
func attrNumber(s *xmlscan.Scanner, name string) float64 {
	v, ok := s.Attr(xmlscan.NoSpace, name)
	if !ok {
		return math.NaN()
	}
	n, ok := parseNumber(v)
	if !ok {
		return math.NaN()
	}
	return n
}

// attrOpacity returns the Opacity of the current element, 1 for none.
func attrOpacity(s *xmlscan.Scanner) float64 {
	v, ok := s.Attr(xmlscan.NoSpace, "Opacity")
	if !ok {
		return 1
	}
	return opacity(v)
}

func opacity(v []byte) float64 {
	n, ok := parseNumber(v)
	if !ok {
		return 1
	}
	return min(max(n, 0), 1)
}

// number returns the number b states, 0 for none.
func number(b []byte) float64 {
	n, _ := parseNumber(b)
	return n
}

// parseNumber reads a number as XPS writes them: a sign, digits, a
// fraction, an exponent. White space around it is passed over. The
// number is bounded (maxCoord).
func parseNumber(b []byte) (float64, bool) {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || len(b) > 64 {
		return 0, false
	}
	i, neg := 0, false
	if b[0] == '+' || b[0] == '-' {
		neg = b[0] == '-'
		i++
	}
	mant, exp, any := 0.0, 0, false
	for ; i < len(b) && b[i] >= '0' && b[i] <= '9'; i++ {
		mant = mant*10 + float64(b[i]-'0')
		any = true
	}
	if i < len(b) && b[i] == '.' {
		for i++; i < len(b) && b[i] >= '0' && b[i] <= '9'; i++ {
			mant = mant*10 + float64(b[i]-'0')
			exp--
			any = true
		}
	}
	if !any {
		return 0, false
	}
	if i < len(b) && (b[i] == 'e' || b[i] == 'E') {
		i++
		eneg := false
		if i < len(b) && (b[i] == '+' || b[i] == '-') {
			eneg = b[i] == '-'
			i++
		}
		e, anyE := 0, false
		for ; i < len(b) && b[i] >= '0' && b[i] <= '9'; i++ {
			if e < 10000 {
				e = e*10 + int(b[i]-'0')
			}
			anyE = true
		}
		if !anyE {
			return 0, false
		}
		if eneg {
			e = -e
		}
		exp += e
	}
	if i != len(b) {
		return 0, false
	}
	// Divided, not multiplied by a negative power: 48.913 is then the
	// number nearest to it, as it is for any reader of decimals.
	v := mant
	if exp < 0 {
		v = mant / math.Pow(10, float64(-exp))
	} else if exp > 0 {
		v = mant * math.Pow(10, float64(exp))
	}
	if v != v {
		return 0, false
	}
	v = min(v, maxCoord)
	if neg {
		v = -v
	}
	return v, true
}

// parseMatrix reads the six numbers of a transformation, separated by
// commas or white space.
func parseMatrix(v []byte) (matrix, bool) {
	var m matrix
	n := 0
	for len(v) > 0 {
		j := 0
		for j < len(v) && v[j] != ',' && v[j] != ' ' && v[j] != '\t' && v[j] != '\n' && v[j] != '\r' {
			j++
		}
		if j > 0 {
			if n >= 6 {
				return m, false
			}
			f, ok := parseNumber(v[:j])
			if !ok {
				return m, false
			}
			m[n] = f
			n++
		}
		if j < len(v) {
			j++
		}
		v = v[j:]
	}
	return m, n == 6
}

// transparent reports whether a Fill written as an attribute is a colour
// that is fully transparent: "#00rrggbb", or "sc#a,r,g,b" with an alpha
// of 0. Any other fill is one that draws, as far as the file declares.
func transparent(v []byte) bool {
	v = bytes.TrimSpace(v)
	if len(v) == 9 && v[0] == '#' {
		return v[1] == '0' && v[2] == '0'
	}
	if len(v) > 3 && string(v[:3]) == "sc#" {
		parts := bytes.Split(v[3:], []byte{','})
		if len(parts) == 4 {
			a, ok := parseNumber(parts[0])
			return ok && a == 0
		}
	}
	return false
}
