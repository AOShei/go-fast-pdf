package extract

import (
	"math"

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

	// fillAlpha and strokeAlpha are the opacities that /ca and /CA set,
	// times alphaScale, the opacity of the transparency groups being
	// drawn: inside a group they start at 1 again, and the group as a
	// whole is painted with the opacity in force outside it.
	fillAlpha, strokeAlpha float32
	alphaScale             float32

	// clip is the number of the text object whose text, shown in a
	// clipping render mode, is part of the clipping path; 0 if none.
	clip int32

	// lineWidth is the width of stroked lines, for rules (rules.go).
	lineWidth float64
}

func initialState() gstate {
	return gstate{ctm: identity, th: 1, fillAlpha: 1, strokeAlpha: 1, alphaScale: 1, lineWidth: 1}
}

// extGState handles gs: the entries of a graphics state parameter
// dictionary that text, and what is seen of it, depend on.
func (in *interp) extGState(raw []byte) {
	dict := in.f.Resolve(in.named(resGState, in.gstateDict(), in.decodeName(raw)))
	if !dict.IsDict() {
		return
	}
	g := &in.gs
	// One pass over the entries, from the last to the first, so that of
	// a key that is there twice the first counts. A page can use this
	// operator before every piece of text.
	for i := len(dict.List)&^1 - 2; i >= 0; i -= 2 {
		k := &dict.List[i]
		if k.Kind != syntax.Name || len(k.Data) < 2 || len(k.Data) > 5 {
			continue
		}
		switch string(k.Data) {
		case "ca":
			if v, ok := in.f.Resolve(dict.List[i+1]).Float(); ok {
				g.fillAlpha = float32(math.Min(math.Max(v, 0), 1)) * g.alphaScale
			}
		case "CA":
			if v, ok := in.f.Resolve(dict.List[i+1]).Float(); ok {
				g.strokeAlpha = float32(math.Min(math.Max(v, 0), 1)) * g.alphaScale
			}
		case "LW":
			if v, ok := in.f.Resolve(dict.List[i+1]).Float(); ok {
				g.lineWidth = max(finite(v), 0)
			}
		case "Font":
			if f := in.f.Resolve(dict.List[i+1]).Elems(); len(f) == 2 {
				if loaded := font.Load(in.f, f[0]); loaded != nil {
					g.font, g.fontSet = loaded, true
				}
				g.size = finite(in.f.Resolve(f[1]).Number())
			}
		}
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
func (in *interp) contentOff(ref syntax.Value) bool { return ContentOff(in.f, ref) }

// ContentOff reports whether optional content is switched off in the
// document's default configuration: ref is the /OC entry of what may be
// optional, a group or a membership dictionary.
func ContentOff(f *file.File, ref syntax.Value) bool {
	if ref.IsNull() {
		return false
	}
	cfg := optionalContent(f)
	if !cfg.baseOff && len(cfg.off) == 0 {
		return false
	}
	dict := f.Resolve(ref)
	if !dict.Get("Type").IsName("OCMD") {
		return cfg.groupOff(file.RefNumber(ref))
	}
	// A membership dictionary. Its visibility expression (/VE) is not
	// evaluated; /OCGs with the policy /P covers what files use.
	groups := dict.Get("OCGs")
	list := []syntax.Value{groups}
	if resolved := f.Resolve(groups); resolved.Kind == syntax.Array {
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
	switch policy := f.Resolve(dict.Get("P")); {
	case policy.IsName("AllOn"):
		return anyOff
	case policy.IsName("AnyOff"):
		return !anyOff
	case policy.IsName("AllOff"):
		return anyOn
	}
	return !anyOn // AnyOn, the default
}
