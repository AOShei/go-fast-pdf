package xps

import (
	"bytes"
	"errors"
	"math"
	"strings"

	"github.com/AOShei/go-fast-core/opc"
	"github.com/AOShei/go-fast-core/xmlscan"
)

// Paths, brushes and resources.
//
// A Path is followed for two things. Its figures whose segments all run
// along the axes, when they are painted, are the rules and the filled
// boxes that tables are drawn with: the page records them as a PDF
// page's are (extract.Fixed.Box), and nothing else of a path is kept,
// not curves, not colours, not its clip. And a Path filled with an image
// is where that image is on the page.
//
// What paints is what the file declares to paint: a Path without a Fill
// is not filled, and one whose brush is a fully transparent colour, or
// whose opacity is 0, paints nothing. That is how the areas of links are
// written, and they are no ground of a cell.
//
// A resource dictionary gives names to brushes, geometries and
// transformations, for the elements after it in the FixedPage or the
// Canvas it belongs to. It is written in the page or is a part of its
// own, which is read when the page names it; a dictionary outside the
// package is never fetched.

// Bounds on the paths and resources of one page.
const (
	// maxResources bounds the named resources of a page.
	maxResources = 1 << 14
	// maxRemote bounds the resource dictionaries of other parts read for
	// one page, and maxRemoteDepth how deep one may name another.
	maxRemote      = 64
	maxRemoteDepth = 4
	// maxKey bounds the name of a resource.
	maxKey = 256
	// maxPathBoxes bounds the segments and figures kept of one path, and
	// maxPathOps the segments looked at on one page: past it the page is
	// a drawing of many lines, and its paths are not followed for rules.
	maxPathBoxes = 1 << 14
	maxPathOps   = 1 << 17
	// maxAlt bounds the description of an image.
	maxAlt = 4 << 10
)

type resKind uint8

const (
	resMatrix resKind = iota + 1
	resBrush
	resGeometry
)

// resource is a named entry of a resource dictionary.
type resource struct {
	key   string
	kind  resKind
	m     matrix
	brush brush
	geom  *geometry
	prev  int // the resource of the same name that this one hides, -1 for none
}

// resources are the resources in scope, the innermost last.
type resources struct {
	list  []resource
	index map[string]int
}

func (rs *resources) reset() {
	clear(rs.list)
	rs.list = rs.list[:0]
	clear(rs.index)
}

func (rs *resources) mark() int { return len(rs.list) }

// release takes the resources added since mark out of scope.
func (rs *resources) release(mark int) {
	for i := len(rs.list) - 1; i >= mark; i-- {
		e := &rs.list[i]
		if e.prev < 0 {
			delete(rs.index, e.key)
		} else {
			rs.index[e.key] = e.prev
		}
		*e = resource{}
	}
	rs.list = rs.list[:min(mark, len(rs.list))]
}

func (rs *resources) add(res resource) bool {
	if len(rs.list) >= maxResources {
		return false
	}
	if rs.index == nil {
		rs.index = map[string]int{}
	}
	res.prev = -1
	if i, ok := rs.index[res.key]; ok {
		res.prev = i
	}
	rs.index[res.key] = len(rs.list)
	rs.list = append(rs.list, res)
	return true
}

func (rs *resources) find(key []byte) *resource {
	if i, ok := rs.index[string(key)]; ok {
		return &rs.list[i]
	}
	return nil
}

// staticResource returns the name in "{StaticResource name}".
func staticResource(v []byte) ([]byte, bool) {
	v = bytes.TrimSpace(v)
	const lead = "{StaticResource"
	if len(v) < len(lead)+2 || string(v[:len(lead)]) != lead || v[len(v)-1] != '}' {
		return nil, false
	}
	key := bytes.TrimSpace(v[len(lead) : len(v)-1])
	return key, len(key) > 0 && len(key) <= maxKey
}

type brushKind uint8

const (
	brushSolid brushKind = iota + 1
	brushImage
	brushVisual
	brushOther // a gradient
)

// brush is what an element is painted with.
type brush struct {
	kind brushKind
	// transparent is set for a colour that is fully transparent, and
	// opacity is the Opacity of the brush.
	transparent bool
	opacity     float64
	// image is the part of an image brush, "" when the package has none.
	image string
	// The transformation of the brush, and the box of its content with
	// the box it is drawn in.
	m                 matrix
	transformed       bool
	viewbox, viewport [4]float64
	boxed             bool
}

// clear reports whether the brush is declared to paint nothing.
func (b *brush) clear() bool {
	return b.kind == brushSolid && b.transparent || b.opacity == 0
}

// paints reports whether there is a brush and it is not declared clear.
func (b *brush) paints() bool { return b.kind != 0 && !b.clear() }

// brushValue reads a brush written as an attribute: a colour, or the
// name of a resource.
func (r *reader) brushValue(v []byte) brush {
	if key, ok := staticResource(v); ok {
		if res := r.w.res.find(key); res != nil && res.kind == resBrush {
			return res.brush
		}
		return brush{}
	}
	if len(bytes.TrimSpace(v)) == 0 {
		return brush{}
	}
	return brush{kind: brushSolid, transparent: transparent(v), opacity: 1}
}

// brushIn reads an element that holds a brush, such as Path.Fill, whose
// start tag is current, to its end. p is the Path the brush paints, nil
// for a brush that paints text.
func (r *reader) brushIn(p *pathState) (b brush, ok bool) {
	s := r.s
	for d := s.Depth(); ; {
		k := s.Next()
		if k == xmlscan.EOF || k == xmlscan.End && s.Depth() < d {
			return b, ok
		}
		if k != xmlscan.Start {
			continue
		}
		if s.Space() != spFixed || ok {
			s.Skip()
			continue
		}
		var base matrix
		visual := false
		if p != nil && len(r.w.stack) > 0 {
			top := &r.w.stack[len(r.w.stack)-1]
			base, visual = top.m, true
			if p.transformed {
				base = p.m.mul(top.m)
			}
		}
		b, ok = r.brushElement(base, visual)
	}
}

// brushElement reads a brush element, whose start tag is current, to
// its end; ok is false for an element that is no brush. The visual of a
// visual brush is read as content of the page when visual is set, in the
// space that base takes to the page.
func (r *reader) brushElement(base matrix, visual bool) (b brush, ok bool) {
	s := r.s
	b.opacity = attrOpacity(s)
	switch string(s.Local()) {
	case "SolidColorBrush":
		b.kind = brushSolid
		v, _ := s.Attr(xmlscan.NoSpace, "Color")
		b.transparent = transparent(v)
		s.Skip()
		return b, true
	case "LinearGradientBrush", "RadialGradientBrush":
		b.kind = brushOther
		s.Skip()
		return b, true
	case "ImageBrush":
		b.kind = brushImage
		if v, has := s.Attr(xmlscan.NoSpace, "ImageSource"); has {
			b.image = r.imagePart(v)
		}
	case "VisualBrush":
		b.kind = brushVisual
	default:
		s.Skip()
		return b, false
	}
	if v, has := s.Attr(xmlscan.NoSpace, "Transform"); has {
		b.m, b.transformed = r.matrixValue(v)
	}
	vb, hasBox := s.Attr(xmlscan.NoSpace, "Viewbox")
	box, okBox := parseBox(vb)
	vp, hasPort := s.Attr(xmlscan.NoSpace, "Viewport")
	port, okPort := parseBox(vp)
	if hasBox && hasPort && okBox && okPort {
		b.viewbox, b.viewport, b.boxed = box, port, true
	}
	for d := s.Depth(); ; {
		k := s.Next()
		if k == xmlscan.EOF || k == xmlscan.End && s.Depth() < d {
			return b, true
		}
		if k != xmlscan.Start {
			continue
		}
		switch {
		case s.Is(spFixed, "ImageBrush.Transform"), s.Is(spFixed, "VisualBrush.Transform"):
			if m, has := r.transform(); has {
				b.m, b.transformed = m, true
			}
		case s.Is(spFixed, "VisualBrush.Visual") && visual && b.kind == brushVisual:
			r.visual(&b, base)
		default:
			s.Skip()
		}
	}
}

// visual reads the content of a visual brush, the start tag of whose
// VisualBrush.Visual is current: what the brush is drawn with is on the
// page where the brush paints, and its text is text of the page. It is
// read once, however often the brush repeats it.
func (r *reader) visual(b *brush, base matrix) {
	s, w := r.s, r.w
	if len(w.stack) == 0 || len(w.stack) >= maxCanvasDepth {
		r.tooDeep = true
		s.Skip()
		return
	}
	top := w.stack[len(w.stack)-1]
	m := base
	if b.transformed {
		m = b.m.mul(m)
	}
	if b.boxed && b.viewbox[2] != 0 && b.viewbox[3] != 0 {
		// The box of the content, drawn in the box of the brush.
		sx, sy := b.viewport[2]/b.viewbox[2], b.viewport[3]/b.viewbox[3]
		m = matrix{sx, 0, 0, sy, b.viewport[0] - b.viewbox[0]*sx, b.viewport[1] - b.viewbox[1]*sy}.mul(m)
	}
	for _, v := range m {
		if v != v || v > maxCoord || v < -maxCoord {
			s.Skip()
			return
		}
	}
	w.stack = append(w.stack, canvas{m: m, opacity: top.opacity * b.opacity, mark: w.res.mark(), href: top.href, id: top.id})
	r.content()
	if n := len(w.stack); n > 1 {
		w.res.release(w.stack[n-1].mark)
		w.stack = w.stack[:n-1]
	}
}

// imagePart returns the part that an ImageSource names: a part of the
// package, written by itself or in the form that also names a colour
// profile. It is "" for an image the package does not have; one outside
// the package is noted and not fetched.
func (r *reader) imagePart(v []byte) string {
	src := strings.TrimSpace(string(v))
	if strings.HasPrefix(src, "{") {
		// {ColorConvertedBitmap image profile}
		f := strings.Fields(strings.Trim(src, "{}"))
		if len(f) < 2 {
			return ""
		}
		src = f[1]
	}
	if src == "" {
		return ""
	}
	if outside(src) {
		r.external = true
		return ""
	}
	if part := opc.Resolve(r.name, src); r.d.pkg.Has(part) {
		return part
	}
	return ""
}

// outside reports whether an address points outside the package: it has
// a scheme, or names a host.
func outside(src string) bool {
	if strings.HasPrefix(src, "//") || strings.HasPrefix(src, `\\`) {
		return true
	}
	for i := 0; i < len(src); i++ {
		switch c := src[i]; {
		case c == ':':
			return i > 1 // one letter before a colon is a drive
		case c == '/' || c == '\\' || c == '?' || c == '#':
			return false
		}
	}
	return false
}

// parseBox reads the four numbers of a Viewbox or a Viewport.
func parseBox(v []byte) (box [4]float64, ok bool) {
	n := 0
	for len(v) > 0 {
		j := 0
		for j < len(v) && v[j] != ',' && v[j] != ' ' && v[j] != '\t' && v[j] != '\n' && v[j] != '\r' {
			j++
		}
		if j > 0 {
			f, good := parseNumber(v[:j])
			if n >= 4 || !good {
				return box, false
			}
			box[n] = f
			n++
		}
		if j < len(v) {
			j++
		}
		v = v[j:]
	}
	return box, n == 4
}

// geometry is what is kept of the figures of a path.
type geometry struct {
	// segs are its segments that run along an axis, as [x0 y0 x1 y1],
	// and polys the boxes of its figures all of whose segments do.
	segs, polys [][4]float64
	// box is the box of all its points, when it has any.
	box [4]float64
	any bool
}

func (g *geometry) reset() {
	g.segs, g.polys, g.any = g.segs[:0], g.polys[:0], false
}

// figures builds a geometry from the figures of a path, whichever way
// they are written.
type figures struct {
	g *geometry
	// m is the transformation of the geometry itself.
	m           matrix
	transformed bool
	// ops counts the segments, and follow is cleared when the page has
	// too many for its paths to be followed further.
	ops    *int
	follow bool

	cur, start [2]float64
	have       bool
	aligned    bool
	n          int
	box        [4]float64
}

func (f *figures) point(x, y float64) [2]float64 {
	if f.transformed {
		x, y = f.m[0]*x+f.m[2]*y+f.m[4], f.m[1]*x+f.m[3]*y+f.m[5]
	}
	g := f.g
	if !g.any {
		g.box, g.any = [4]float64{x, y, x, y}, true
	} else {
		g.box[0], g.box[1], g.box[2], g.box[3] = min(g.box[0], x), min(g.box[1], y), max(g.box[2], x), max(g.box[3], y)
	}
	return [2]float64{x, y}
}

func (f *figures) moveTo(x, y float64) {
	f.end()
	f.cur = f.point(x, y)
	f.start, f.have, f.aligned, f.n = f.cur, true, true, 0
	f.box = [4]float64{f.cur[0], f.cur[1], f.cur[0], f.cur[1]}
}

// along reports whether the segment from a to b runs along an axis.
func along(a, b [2]float64) bool {
	const slack = 1e-9
	dx, dy := math.Abs(a[0]-b[0]), math.Abs(a[1]-b[1])
	return dx <= slack*math.Max(1, math.Abs(a[0])) || dy <= slack*math.Max(1, math.Abs(a[1]))
}

func (f *figures) lineTo(x, y float64, stroked bool) {
	if !f.have {
		f.moveTo(x, y)
		return
	}
	p := f.point(x, y)
	f.segment(p, stroked)
}

func (f *figures) segment(p [2]float64, stroked bool) {
	*f.ops++
	if *f.ops > maxPathOps {
		f.follow = false
	}
	if p != f.cur {
		f.n++
		if !along(f.cur, p) {
			f.aligned = false
		} else if stroked && f.follow && len(f.g.segs) < maxPathBoxes {
			f.g.segs = append(f.g.segs, [4]float64{f.cur[0], f.cur[1], p[0], p[1]})
		}
		f.box[0], f.box[1], f.box[2], f.box[3] = min(f.box[0], p[0]), min(f.box[1], p[1]), max(f.box[2], p[0]), max(f.box[3], p[1])
	}
	f.cur = p
}

// curveTo passes over a curve that ends at (x, y): its figure does not
// run along the axes.
func (f *figures) curveTo(x, y float64) {
	if !f.have {
		f.moveTo(x, y)
		return
	}
	*f.ops++
	f.cur = f.point(x, y)
	f.aligned = false
	f.n++
}

func (f *figures) close() {
	if f.have && f.cur != f.start {
		f.segment(f.start, true)
	}
	f.end()
	f.cur, f.have = f.start, true
	f.aligned, f.n = true, 0
	f.box = [4]float64{f.cur[0], f.cur[1], f.cur[0], f.cur[1]}
}

// end ends the figure: when all its segments run along the axes it is a
// box that a fill paints. A fill closes a figure that is left open.
func (f *figures) end() {
	if f.have && f.aligned && f.follow && (f.n >= 3 || f.n == 2 && f.cur != f.start) && along(f.cur, f.start) && len(f.g.polys) < maxPathBoxes {
		f.g.polys = append(f.g.polys, f.box)
	}
	f.have = false
}

// abbreviated reads figures in the abbreviated syntax of a Data or a
// Figures attribute.
func (f *figures) abbreviated(data []byte) {
	i := 0
	// number reads the next number, passing over what separates them; ok
	// is false at a command or at the end.
	number := func() (v float64, ok bool) {
		for i < len(data) && (data[i] == ' ' || data[i] == ',' || data[i] == '\t' || data[i] == '\n' || data[i] == '\r') {
			i++
		}
		j := i
		if j < len(data) && (data[j] == '+' || data[j] == '-') {
			j++
		}
		digits := false
		for j < len(data) && data[j] >= '0' && data[j] <= '9' {
			j, digits = j+1, true
		}
		if j < len(data) && data[j] == '.' {
			for j++; j < len(data) && data[j] >= '0' && data[j] <= '9'; j++ {
				digits = true
			}
		}
		if !digits {
			return 0, false
		}
		if j < len(data) && (data[j] == 'e' || data[j] == 'E') {
			k := j + 1
			if k < len(data) && (data[k] == '+' || data[k] == '-') {
				k++
			}
			if k < len(data) && data[k] >= '0' && data[k] <= '9' {
				for k < len(data) && data[k] >= '0' && data[k] <= '9' {
					k++
				}
				j = k
			}
		}
		v, ok = parseNumber(data[i:j])
		i = j
		return v, ok
	}
	for i < len(data) {
		c := data[i]
		if c == ' ' || c == ',' || c == '\t' || c == '\n' || c == '\r' {
			i++
			continue
		}
		i++
		rel := c >= 'a' && c <= 'z'
		// at returns a point of the command: as written, or from the
		// current point for a command in small letters.
		at := func(x, y float64) (float64, float64) {
			if rel && f.have && !f.transformed {
				return f.cur[0] + x, f.cur[1] + y
			}
			return x, y
		}
		if rel && f.transformed {
			// The current point is kept transformed: a relative command
			// of a transformed geometry is not followed. Its figure is
			// no box.
			f.aligned = false
		}
		switch c | 0x20 {
		case 'f':
			number()
		case 'm':
			first := true
			for {
				x, ok1 := number()
				y, ok2 := number()
				if !ok1 || !ok2 {
					break
				}
				x, y = at(x, y)
				if first {
					f.moveTo(x, y)
					first = false
				} else {
					f.lineTo(x, y, true)
				}
			}
		case 'l':
			for {
				x, ok1 := number()
				y, ok2 := number()
				if !ok1 || !ok2 {
					break
				}
				x, y = at(x, y)
				f.lineTo(x, y, true)
			}
		case 'h', 'v':
			for {
				v, ok := number()
				if !ok {
					break
				}
				if !f.have || f.transformed {
					f.aligned = false
					continue
				}
				x, y := f.cur[0], f.cur[1]
				switch {
				case c == 'H':
					x = v
				case c == 'h':
					x += v
				case c == 'V':
					y = v
				default:
					y += v
				}
				f.lineTo(x, y, true)
			}
		case 'c', 's', 'q', 'a':
			per := map[byte]int{'c': 6, 's': 4, 'q': 4, 'a': 7}[c|0x20]
			for {
				var args [7]float64
				n := 0
				for n < per {
					v, ok := number()
					if !ok {
						break
					}
					args[n] = v
					n++
				}
				if n < per {
					break
				}
				x, y := at(args[per-2], args[per-1])
				f.curveTo(x, y)
			}
		case 'z':
			f.close()
		default:
			// Not a command: the rest is not read.
			f.end()
			return
		}
	}
	f.end()
}

// pathState is a Path element as it is being read.
type pathState struct {
	data        []byte
	geom        geometry
	m           matrix
	transformed bool
	opacity     float64
	fill        brush
	stroke      brush
	thickness   float64
	alt, name   []byte
	// The name of the element and the address it links to, when links
	// are asked for.
	id, href []byte
}

// path reads a Path element, whose start tag is current: its painted
// boxes go to the page, and the image it is filled with.
func (r *reader) path() {
	s, w := r.s, r.w
	if len(w.stack) == 0 {
		s.Skip()
		return
	}
	level := r.pathLevel
	for len(w.paths) <= level {
		w.paths = append(w.paths, &pathState{})
	}
	p := w.paths[level]
	*p = pathState{data: p.data[:0], geom: p.geom, alt: p.alt[:0], name: p.name[:0], id: p.id[:0], href: p.href[:0], opacity: 1, thickness: 1}
	p.geom.reset()
	fromResource := false
	for i := 0; i < s.NumAttr(); i++ {
		space, local, v := s.AttrAt(i)
		if space != xmlscan.NoSpace {
			continue
		}
		switch string(local) {
		case "Data":
			if key, ok := staticResource(v); ok {
				if res := w.res.find(key); res != nil && res.kind == resGeometry && res.geom != nil {
					p.geom.segs = append(p.geom.segs, res.geom.segs...)
					p.geom.polys = append(p.geom.polys, res.geom.polys...)
					p.geom.box, p.geom.any = res.geom.box, res.geom.any
					fromResource = true
				}
			} else {
				p.data = append(p.data, v...)
			}
		case "Fill":
			p.fill = r.brushValue(v)
		case "Stroke":
			p.stroke = r.brushValue(v)
		case "StrokeThickness":
			if t, ok := parseNumber(v); ok && t >= 0 {
				p.thickness = t
			}
		case "RenderTransform":
			p.m, p.transformed = r.matrixValue(v)
		case "Opacity":
			p.opacity = opacity(v)
		case "AutomationProperties.HelpText":
			p.alt = append(p.alt[:0], v[:min(len(v), maxAlt)]...)
		case "AutomationProperties.Name":
			p.name = append(p.name[:0], v[:min(len(v), maxAlt)]...)
		case "Name":
			if w.links && len(v) <= MaxName {
				p.id = append(p.id, v...)
			}
		case "FixedPage.NavigateUri":
			if w.links && len(v) <= maxHref {
				p.href = append(p.href, v...)
			}
		}
	}
	follow := r.fx.Paths() && r.ops <= maxPathOps
	if len(p.data) > 0 && !fromResource {
		f := figures{g: &p.geom, ops: &r.ops, follow: follow}
		f.abbreviated(p.data)
	}
	r.pathLevel++
	for d := s.Depth(); ; {
		k := s.Next()
		if k == xmlscan.EOF || k == xmlscan.End && s.Depth() < d {
			break
		}
		if k != xmlscan.Start {
			continue
		}
		switch {
		case s.Is(spFixed, "Path.RenderTransform"):
			if m, ok := r.transform(); ok {
				p.m, p.transformed = m, true
			}
		case s.Is(spFixed, "Path.Fill"):
			if b, ok := r.brushIn(p); ok {
				p.fill = b
			}
		case s.Is(spFixed, "Path.Stroke"):
			if b, ok := r.brushIn(p); ok {
				p.stroke = b
			}
		case s.Is(spFixed, "Path.Data"):
			r.geometryIn(&p.geom, follow)
		default:
			s.Skip()
		}
	}
	r.pathLevel--

	top := &w.stack[len(w.stack)-1]
	m := top.m
	if p.transformed {
		m = p.m.mul(m)
	}
	for _, v := range m {
		if v != v || v > maxCoord || v < -maxCoord {
			return
		}
	}
	if !p.geom.any {
		return
	}
	if w.links && (len(p.href) > 0 || len(p.id) > 0 || top.href != "") {
		r.element(p.id, p.href, top.href, m, p.geom.box)
	}
	if top.opacity*p.opacity > 0 && r.fx.Paths() {
		// A figure filled with an image is no ground of a cell.
		if p.fill.paints() && p.fill.kind != brushImage {
			for _, box := range p.geom.polys {
				r.fx.Box(m, box, 0)
			}
		}
		if p.stroke.paints() && p.thickness > 0 {
			for _, seg := range p.geom.segs {
				r.fx.Box(m, seg, p.thickness)
			}
		}
	}
	if p.fill.kind == brushImage && p.fill.image != "" {
		alt := p.alt
		if len(alt) == 0 {
			alt = p.name
		}
		r.fx.Image(m, p.geom.box, p.fill.image, string(alt))
	}
}

// geometryIn reads an element that holds a geometry, Path.Data, whose
// start tag is current, to its end.
func (r *reader) geometryIn(g *geometry, follow bool) {
	s := r.s
	for d := s.Depth(); ; {
		k := s.Next()
		if k == xmlscan.EOF || k == xmlscan.End && s.Depth() < d {
			return
		}
		if k != xmlscan.Start {
			continue
		}
		if s.Is(spFixed, "PathGeometry") {
			r.geometry(g, follow)
		} else {
			s.Skip()
		}
	}
}

// geometry reads a PathGeometry element, whose start tag is current, to
// its end: its figures written as an attribute or as elements, through
// its transformation.
func (r *reader) geometry(g *geometry, follow bool) {
	s := r.s
	f := figures{g: g, ops: &r.ops, follow: follow}
	if v, ok := s.Attr(xmlscan.NoSpace, "Transform"); ok {
		f.m, f.transformed = r.matrixValue(v)
	}
	// The figures of the attribute are read when the element ends, once
	// its transformation, which may be an element, is known.
	var abbreviated []byte
	if v, ok := s.Attr(xmlscan.NoSpace, "Figures"); ok {
		abbreviated = append(abbreviated, v...)
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
		case s.Is(spFixed, "PathGeometry.Transform"):
			if m, ok := r.transform(); ok {
				f.m, f.transformed = m, true
			}
		case s.Is(spFixed, "PathFigure"):
			r.figure(&f)
		default:
			s.Skip()
		}
	}
	if len(abbreviated) > 0 {
		f.abbreviated(abbreviated)
	}
}

// figure reads a PathFigure element, whose start tag is current.
func (r *reader) figure(f *figures) {
	s := r.s
	closed := false
	if v, ok := s.Attr(xmlscan.NoSpace, "IsClosed"); ok {
		closed = string(v) == "true"
	}
	if v, ok := s.Attr(xmlscan.NoSpace, "StartPoint"); ok {
		if pts, good := parsePoints(v, nil); good && len(pts) >= 2 {
			f.moveTo(pts[0], pts[1])
		}
	}
	var pts []float64
	for d := s.Depth(); ; {
		k := s.Next()
		if k == xmlscan.EOF || k == xmlscan.End && s.Depth() < d {
			break
		}
		if k != xmlscan.Start {
			continue
		}
		stroked := true
		if v, ok := s.Attr(xmlscan.NoSpace, "IsStroked"); ok {
			stroked = string(v) != "false"
		}
		switch {
		case s.Is(spFixed, "PolyLineSegment"):
			v, _ := s.Attr(xmlscan.NoSpace, "Points")
			var good bool
			if pts, good = parsePoints(v, pts[:0]); good {
				for i := 0; i+1 < len(pts); i += 2 {
					f.lineTo(pts[i], pts[i+1], stroked)
				}
			}
		case s.Is(spFixed, "PolyBezierSegment"), s.Is(spFixed, "PolyQuadraticBezierSegment"):
			v, _ := s.Attr(xmlscan.NoSpace, "Points")
			var good bool
			if pts, good = parsePoints(v, pts[:0]); good {
				for i := 0; i+1 < len(pts); i += 2 {
					f.curveTo(pts[i], pts[i+1])
				}
			}
		case s.Is(spFixed, "ArcSegment"):
			v, _ := s.Attr(xmlscan.NoSpace, "Point")
			var good bool
			if pts, good = parsePoints(v, pts[:0]); good && len(pts) >= 2 {
				f.curveTo(pts[0], pts[1])
			}
		}
		s.Skip()
	}
	if closed {
		f.close()
	}
	f.end()
}

// maxPoints bounds the numbers read from one list of points.
const maxPoints = 1 << 16

// parsePoints reads a list of numbers, the coordinates of points, into
// dst.
func parsePoints(v []byte, dst []float64) ([]float64, bool) {
	for len(v) > 0 {
		j := 0
		for j < len(v) && v[j] != ',' && v[j] != ' ' && v[j] != '\t' && v[j] != '\n' && v[j] != '\r' {
			j++
		}
		if j > 0 {
			f, ok := parseNumber(v[:j])
			if !ok {
				return dst, false
			}
			if len(dst) < maxPoints {
				dst = append(dst, f)
			}
		}
		if j < len(v) {
			j++
		}
		v = v[j:]
	}
	return dst, true
}

// resources reads an element that holds a resource dictionary, such as
// FixedPage.Resources, whose start tag is current, to its end.
func (r *reader) resources() {
	s := r.s
	for d := s.Depth(); ; {
		k := s.Next()
		if k == xmlscan.EOF || k == xmlscan.End && s.Depth() < d {
			return
		}
		if k != xmlscan.Start {
			continue
		}
		if s.Is(spFixed, "ResourceDictionary") {
			r.dictionary()
		} else {
			s.Skip()
		}
	}
}

// dictionary reads a ResourceDictionary element, whose start tag is
// current, to its end: the resources written in it, or those of the
// part it names.
func (r *reader) dictionary() {
	s, w := r.s, r.w
	if src, ok := s.Attr(xmlscan.NoSpace, "Source"); ok && len(bytes.TrimSpace(src)) > 0 {
		target := strings.TrimSpace(string(src))
		s.Skip()
		r.remoteDictionary(target)
		return
	}
	var key []byte
	for d := s.Depth(); ; {
		k := s.Next()
		if k == xmlscan.EOF || k == xmlscan.End && s.Depth() < d {
			return
		}
		if k != xmlscan.Start {
			continue
		}
		v, named := s.AttrAny("Key")
		if s.Space() != spFixed || !named || len(v) == 0 || len(v) > maxKey {
			s.Skip()
			continue
		}
		key = append(key[:0], v...)
		res := resource{}
		switch string(s.Local()) {
		case "MatrixTransform":
			mv, _ := s.Attr(xmlscan.NoSpace, "Matrix")
			m, ok := parseMatrix(mv)
			s.Skip()
			if !ok {
				continue
			}
			res.kind, res.m = resMatrix, m
		case "PathGeometry":
			g := &geometry{}
			r.geometry(g, r.fx.Paths() && r.ops <= maxPathOps)
			res.kind, res.geom = resGeometry, g
		default:
			// The visual of a brush that a dictionary holds is read
			// where the dictionary stands: the elements that are painted
			// with it are not known yet.
			var base matrix
			if len(w.stack) > 0 {
				base = w.stack[len(w.stack)-1].m
			}
			b, ok := r.brushElement(base, len(w.stack) > 0)
			if !ok {
				continue
			}
			res.kind, res.brush = resBrush, b
		}
		res.key = string(key)
		if !w.res.add(res) {
			r.resourcesCut = true
		}
	}
}

// remoteDictionary reads the resource dictionary that is the part
// target names, relative to the part being read.
func (r *reader) remoteDictionary(target string) {
	if outside(target) {
		r.external = true
		return
	}
	part := opc.Resolve(r.name, target)
	pkg := r.d.pkg
	if !pkg.Has(part) {
		return
	}
	if r.remote >= maxRemote || r.open >= maxRemoteDepth {
		r.resourcesCut = true
		return
	}
	r.remote++
	w := r.w
	for len(w.scans) <= r.open {
		w.scans = append(w.scans, new(xmlscan.Scanner))
	}
	sc := w.scans[r.open]
	closer, err := r.d.scan(part, sc, &scanOptions, r.info)
	if err != nil {
		return
	}
	s, name := r.s, r.name
	r.s, r.name = sc, part
	r.open++
	for {
		k := sc.Next()
		if k == xmlscan.EOF {
			break
		}
		if k == xmlscan.Start {
			if sc.Is(spFixed, "ResourceDictionary") {
				r.dictionary()
			}
			break
		}
	}
	r.open--
	r.s, r.name = s, name
	if err := sc.Err(); err != nil {
		if errors.Is(err, opc.ErrBudget) || errors.Is(err, errSpent) {
			r.info.Spent = true
		} else {
			r.damaged = true
		}
	}
	closer.Close()
}
