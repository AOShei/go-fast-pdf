package extract

import (
	"math"

	"github.com/AOShei/go-fast-pdf/internal/content"
	"github.com/AOShei/go-fast-pdf/internal/file"
	"github.com/AOShei/go-fast-pdf/internal/font"
	"github.com/AOShei/go-fast-pdf/internal/syntax"
)

// matrix is a transformation [a b c d e f]: x' = a*x + c*y + e and
// y' = b*x + d*y + f.
type matrix [6]float64

var identity = matrix{1, 0, 0, 1, 0, 0}

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

func (m matrix) apply(x, y float64) (float64, float64) {
	return m[0]*x + m[2]*y + m[4], m[1]*x + m[3]*y + m[5]
}

// Quad is the four corners of a box after a transformation, as x and y
// pairs: lower left, lower right, upper right, upper left of the box as
// it was before.
type Quad [8]float64

func (m matrix) quad(box [4]float64) Quad {
	var q Quad
	q[0], q[1] = m.apply(box[0], box[1])
	q[2], q[3] = m.apply(box[2], box[1])
	q[4], q[5] = m.apply(box[2], box[3])
	q[6], q[7] = m.apply(box[0], box[3])
	return q
}

// finite bounds a number from a content stream. A stream can state any
// number; nothing on a page is a trillion units away, and keeping the
// numbers in range keeps the arithmetic that follows meaningful.
func finite(v float64) float64 {
	const limit = 1e12
	switch {
	case v > limit:
		return limit
	case v < -limit:
		return -limit
	case v != v: // NaN
		return 0
	}
	return v
}

func hypot(x, y float64) float64 { return math.Sqrt(x*x + y*y) }

// Color is a colour as 8-bit sRGB. Known is false when the colour space
// is one whose colours this library does not convert: a pattern, a
// separation, an indexed space.
type Color struct {
	R, G, B uint8
	Known   bool
}

var black = Color{Known: true}

type colorSpace uint8

const (
	csGray colorSpace = iota
	csRGB
	csCMYK
	csUnknown
)

// paint is a colour with the space the next components are read in.
type paint struct {
	color Color
	space colorSpace
}

// gstate is the graphics state: what q saves and Q restores.
type gstate struct {
	ctm matrix

	// Text state. It is part of the graphics state: a font chosen
	// between q and Q is gone after Q.
	font    *font.Font
	fontSet bool // Tf was used, whether or not the font exists
	size    float64
	tc, tw  float64 // character and word spacing
	th      float64 // horizontal scaling, 1 for 100%
	tl      float64 // leading
	rise    float64
	mode    uint8 // text render mode, 0 to 7

	fill, stroke           paint
	fillAlpha, strokeAlpha float32

	// clip is the number of the text object whose text, shown in a
	// clipping render mode, is part of the clipping path; 0 if none.
	clip int32
}

func initialState() gstate {
	return gstate{ctm: identity, th: 1, fill: paint{color: black}, stroke: paint{color: black}, fillAlpha: 1, strokeAlpha: 1}
}

func unit8(v float64) uint8 {
	switch {
	case !(v > 0): // also NaN
		return 0
	case v >= 1:
		return 255
	}
	return uint8(v*255 + 0.5)
}

func toColor(space colorSpace, c [4]float64) Color {
	switch space {
	case csGray:
		v := unit8(c[0])
		return Color{v, v, v, true}
	case csRGB:
		return Color{unit8(c[0]), unit8(c[1]), unit8(c[2]), true}
	case csCMYK:
		// The plain conversion, without a colour profile: enough to tell
		// white from black.
		k := 1 - math.Min(math.Max(c[3], 0), 1)
		return Color{unit8((1 - c[0]) * k), unit8((1 - c[1]) * k), unit8((1 - c[2]) * k), true}
	}
	return Color{}
}

var components = [...]int{csGray: 1, csRGB: 3, csCMYK: 4}

// setColor handles g, rg, k and their stroking forms: they choose the
// colour space and the colour at once.
func (in *interp) setColor(p *paint, space colorSpace, args []content.Operand) {
	if args == nil {
		return
	}
	var c [4]float64
	for i, a := range args {
		c[i] = a.Float()
	}
	p.space, p.color = space, toColor(space, c)
}

// setComponents handles sc, scn and their stroking forms: components in
// the current colour space. Operands that do not fit the space, such as
// the name of a pattern, leave the colour unknown.
func (in *interp) setComponents(p *paint, args []content.Operand) {
	if p.space == csUnknown {
		p.color = Color{}
		return
	}
	a := last(args, components[p.space])
	if a == nil {
		return
	}
	var c [4]float64
	for i, o := range a {
		if !o.IsNumber() {
			p.color = Color{}
			return
		}
		c[i] = o.Float()
	}
	p.color = toColor(p.space, c)
}

// setSpace handles cs and CS. The colour becomes the space's initial
// one, which is black in the spaces this library converts.
func (in *interp) setSpace(p *paint, raw []byte) {
	p.space = in.space(string(in.decodeName(raw)))
	p.color = black
	if p.space == csUnknown {
		p.color = Color{}
	}
}

// space returns the kind of a colour space given by name: a device
// space, or an entry of the /ColorSpace resources.
func (in *interp) space(name string) colorSpace {
	switch name {
	case "DeviceGray", "G", "CalGray":
		return csGray
	case "DeviceRGB", "RGB", "CalRGB":
		return csRGB
	case "DeviceCMYK", "CMYK":
		return csCMYK
	case "Pattern":
		return csUnknown
	}
	def := in.f.Resolve(in.spaceDict().Get(name))
	switch def.Kind {
	case syntax.Name:
		switch def.NameString() {
		case "DeviceGray":
			return csGray
		case "DeviceRGB":
			return csRGB
		case "DeviceCMYK":
			return csCMYK
		}
	case syntax.Array:
		if len(def.List) == 0 {
			break
		}
		switch family := in.f.Resolve(def.List[0]); {
		case family.IsName("CalGray"):
			return csGray
		case family.IsName("CalRGB"):
			return csRGB
		case family.IsName("ICCBased") && len(def.List) > 1:
			// A profile is not applied; its number of components says
			// which device space it stands in for.
			switch n, _ := in.f.Resolve(in.f.Resolve(def.List[1]).Get("N")).Integer(); n {
			case 1:
				return csGray
			case 3:
				return csRGB
			case 4:
				return csCMYK
			}
		}
	}
	return csUnknown
}

// extGState handles gs: the entries of a graphics state parameter
// dictionary that text depends on.
func (in *interp) extGState(raw []byte) {
	dict := in.f.Resolve(in.gstateDict().Get(string(in.decodeName(raw))))
	if !dict.IsDict() {
		return
	}
	g := &in.gs
	if v, ok := in.f.Resolve(dict.Get("ca")).Float(); ok {
		g.fillAlpha = float32(math.Min(math.Max(v, 0), 1))
	}
	if v, ok := in.f.Resolve(dict.Get("CA")).Float(); ok {
		g.strokeAlpha = float32(math.Min(math.Max(v, 0), 1))
	}
	if f := in.f.Resolve(dict.Get("Font")).Elems(); len(f) == 2 {
		if loaded := font.Load(in.f, f[0]); loaded != nil {
			g.font, g.fontSet = loaded, true
		}
		g.size = finite(in.f.Resolve(f[1]).Number())
	}
}

// --- optional content ---

// ocConfig is the default configuration of the document's optional
// content: which groups are switched off when the file is opened.
type ocConfig struct {
	baseOff bool // /BaseState /OFF: every group is off unless listed in /ON
	on, off map[int64]bool
}

type ocKey struct{}

func optionalContent(f *file.File) *ocConfig {
	if cached, ok := f.Cache.Load(ocKey{}); ok {
		return cached.(*ocConfig)
	}
	cfg := &ocConfig{on: map[int64]bool{}, off: map[int64]bool{}}
	d := f.Resolve(f.Resolve(f.Resolve(f.Trailer().Get("Root")).Get("OCProperties")).Get("D"))
	cfg.baseOff = f.Resolve(d.Get("BaseState")).IsName("OFF")
	for _, ref := range f.Resolve(d.Get("ON")).Elems() {
		cfg.on[file.RefNumber(ref)] = true
	}
	for _, ref := range f.Resolve(d.Get("OFF")).Elems() {
		cfg.off[file.RefNumber(ref)] = true
	}
	actual, _ := f.Cache.LoadOrStore(ocKey{}, cfg)
	return actual.(*ocConfig)
}

func (c *ocConfig) groupOff(num int64) bool {
	if num == 0 {
		return false
	}
	return c.off[num] || c.baseOff && !c.on[num]
}

// contentOff reports whether optional content is switched off in the
// document's default configuration. ref is what an /OC entry or a
// property list refers to: a group, or a membership dictionary that
// names groups and says how their states combine.
func (in *interp) contentOff(ref syntax.Value) bool {
	if ref.IsNull() {
		return false
	}
	cfg := optionalContent(in.f)
	if !cfg.baseOff && len(cfg.off) == 0 {
		return false
	}
	dict := in.f.Resolve(ref)
	if !dict.Get("Type").IsName("OCMD") {
		return cfg.groupOff(file.RefNumber(ref))
	}
	// A membership dictionary. Its visibility expression (/VE) is not
	// evaluated; /OCGs with the policy /P covers what files use.
	groups := dict.Get("OCGs")
	list := []syntax.Value{groups}
	if resolved := in.f.Resolve(groups); resolved.Kind == syntax.Array {
		list = resolved.List
	}
	anyOn, anyOff, n := false, false, 0
	for _, g := range list {
		if num := file.RefNumber(g); num != 0 {
			n++
			if cfg.groupOff(num) {
				anyOff = true
			} else {
				anyOn = true
			}
		}
	}
	if n == 0 {
		return false
	}
	switch policy := in.f.Resolve(dict.Get("P")); {
	case policy.IsName("AllOn"):
		return anyOff
	case policy.IsName("AnyOff"):
		return !anyOff
	case policy.IsName("AllOff"):
		return anyOn
	}
	return !anyOn // AnyOn, the default
}
