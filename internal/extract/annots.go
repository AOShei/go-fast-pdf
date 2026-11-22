package extract

import (
	"github.com/AOShei/go-fast-pdf/internal/file"
	"github.com/AOShei/go-fast-pdf/internal/syntax"
)

// MaxAnnotations is the number of annotations of a page that are looked
// at. A page lists them in an array of any length. A file of the corpus
// has 32,768 links on one page; reading them takes about 7 microseconds
// each.
const MaxAnnotations = 1 << 16

// Annotation flags (ISO 32000-1, table 165).
const (
	annotHidden = 1 << 1
	annotNoView = 1 << 5
)

// appearances draws what the annotations of the page show: for each,
// its normal appearance, fitted into its rectangle. A viewer paints
// them over the content of the page, so they are drawn after it: the
// text of a filled form field, of a note written on the page, of a
// stamp.
//
// The appearance of an annotation that its flags hide, or that belongs
// to a layer that is off, is read like content of such a layer: its
// text is extracted and marked as not shown.
//
// Result.Shown says, for each annotation, whether its appearance showed
// text.
func (in *interp) appearances(p *file.Page) {
	annots := in.f.Resolve(p.Dict().Get("Annots")).Elems()
	if len(annots) == 0 {
		return
	}
	if len(annots) > MaxAnnotations {
		// This also bounds what the root package reads of the page's
		// annotations: their notes, form values, links and attached
		// files. This count stands for all of it.
		in.cut(boundAnnots, int64(len(annots)-MaxAnnotations), 0)
		annots = annots[:MaxAnnotations]
	}
	in.res.Shown = make([]bool, len(annots))
	for i, ref := range annots {
		if in.stopped {
			return
		}
		a := in.f.Resolve(ref)
		if !a.IsDict() || in.f.Resolve(a.Get("Subtype")).IsName("Popup") {
			continue
		}
		// The normal appearance: a form, or one form for each state, of
		// which the annotation names the one it is in.
		form := in.f.Resolve(in.f.Resolve(a.Get("AP")).Get("N"))
		if form.Kind == syntax.Dict {
			state := in.f.Resolve(a.Get("AS"))
			if state.Kind != syntax.Name {
				continue
			}
			form = in.f.Resolve(form.Get(state.NameString()))
		}
		if form.Kind != syntax.Stream {
			continue
		}
		rect, ok := in.f.Rect(a.Get("Rect"))
		if !ok {
			continue
		}
		box, ok := in.f.Rect(form.Get("BBox"))
		if !ok {
			continue
		}
		// The form's box, as its matrix places it, is made to fill the
		// rectangle of the annotation (ISO 32000-1, 12.5.5).
		fm := identity
		if m := in.f.Resolve(form.Get("Matrix")).Elems(); len(m) == 6 {
			for k := range fm {
				fm[k] = finite(in.f.Resolve(m[k]).Number())
			}
		}
		q := fm.quad(box)
		x0, y0 := min(q[0], q[2], q[4], q[6]), min(q[1], q[3], q[5], q[7])
		x1, y1 := max(q[0], q[2], q[4], q[6]), max(q[1], q[3], q[5], q[7])
		if !(x1 > x0 && y1 > y0) || !(rect[2] > rect[0] && rect[3] > rect[1]) {
			continue // nothing of it is shown
		}
		sx, sy := (rect[2]-rect[0])/(x1-x0), (rect[3]-rect[1])/(y1-y0)
		fit := matrix{finite(sx), 0, 0, finite(sy), finite(rect[0] - x0*sx), finite(rect[1] - y0*sy)}

		flags, _ := in.f.Resolve(a.Get("F")).Integer()
		off := flags&(annotHidden|annotNoView) != 0 || in.contentOff(a.Get("OC"))

		// An appearance is drawn in a graphics state of its own.
		saved, savedTm, savedTlm := in.gs, in.tm, in.tlm
		in.gs = initialState()
		in.gs.ctm = fit
		in.tm, in.tlm = identity, identity
		in.geomValid = false
		if off {
			in.offDepth++
		}
		before := len(in.text)
		in.drawForm(form)
		in.res.Shown[i] = len(in.text) > before
		if off {
			in.offDepth--
		}
		in.gs, in.tm, in.tlm = saved, savedTm, savedTlm
		in.geomValid = false
	}
}
