package gofastpdf

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"time"

	gofastcore "github.com/AOShei/go-fast-core"
	"github.com/AOShei/go-fast-pdf/internal/charset"
	"github.com/AOShei/go-fast-pdf/internal/extract"
	"github.com/AOShei/go-fast-pdf/internal/file"
	"github.com/AOShei/go-fast-pdf/internal/layout"
	"github.com/AOShei/go-fast-pdf/internal/syntax"
)

// Text that a page holds outside its content: what its annotations say,
// and the values of its form fields.
//
// Neither is in the content stream. A comment is shown in a pop-up or a
// list beside the page, if the viewer shows it at all; the value of a
// field is shown by the field's own appearance, which is not read here.
// Both are text of the file that a consumer of the page gets or does not
// get, so both are extracted, and flagged where the file keeps them
// from being shown.

const (
	// maxPageNoteBytes bounds the text of the annotations and fields of
	// one page, as the text of its content is bounded. A file can give
	// every one of sixteen thousand annotations one long string.
	maxPageNoteBytes = 16 << 20
	// maxFieldDepth bounds the fields above a field that are looked at.
	maxFieldDepth = 32
)

// Annotation flags (ISO 32000-1, table 165), and field flags (tables
// 226 and 228).
const (
	annotHidden = 1 << 1
	annotNoView = 1 << 5

	fieldPassword   = 1 << 13
	fieldRadio      = 1 << 15
	fieldPushbutton = 1 << 16
)

// extra is a block that does not come from the content of the page,
// with what is needed to put it among those that do.
type extra struct {
	block gofastcore.Block
	// field is set for the value of a form field, which is put where
	// its widget is. A note goes after everything else.
	field bool
}

// pageNotes reads the text of the annotations of a page and the values
// of the form fields that have a widget on it. cut reports that there
// was more of it than is kept.
//
// shown says of each annotation whether its appearance, which is read
// with the content of the page, showed text. What is shown is there
// already and is not said twice: the value of a field becomes a block
// only when the field's appearance shows nothing, and so does what is
// written in a note that is itself written on the page.
func pageNotes(r *reader, p *file.Page, shown []bool, m [6]float64, width, height float64) (extras []extra, cut bool) {
	f := r.f
	annots := f.Resolve(p.Dict().Get("Annots")).Elems()
	if len(annots) > maxAnnotations {
		annots = annots[:maxAnnotations]
	}
	text := r.text
	choicesOf := map[string]bool{}
	for i, ref := range annots {
		a := f.Resolve(ref)
		if !a.IsDict() {
			continue
		}
		visible := i < len(shown) && shown[i]
		var found [2]extra
		ok := true
		switch sub := f.Resolve(a.Get("Subtype")); {
		case sub.IsName("Popup"):
			continue // the window of another annotation, whose text is that one's
		case sub.IsName("Widget"):
			if !visible {
				found[0], ok = fieldValue(f, a, text)
			}
			// The options of a choice field, once for the field,
			// however many widgets show it.
			if ok {
				if name := fieldOf(f, a).name; !choicesOf[name] {
					found[1], ok = fieldChoices(f, a, text)
					if found[1].block.Text != "" && name != "" {
						choicesOf[name] = true
					}
				}
			}
		case sub.IsName("FreeText") && visible:
			continue // its text is what it shows
		default:
			found[0], ok = note(f, a, text)
		}
		if !ok {
			return extras, true
		}
		for _, x := range found {
			if x.block.Text == "" {
				continue
			}
			b := &x.block
			b.Origin = gofastcore.OriginNative
			b.Loc.BBox = rectOnPage(f, a.Get("Rect"), m)
			// The annotation's flags say it is not shown (Hidden,
			// NoView), or it is in optional content that is off.
			if flags, _ := f.Resolve(a.Get("F")).Integer(); flags&(annotHidden|annotNoView) != 0 || extract.ContentOff(f, a.Get("OC")) {
				b.Display |= gofastcore.DisplayHidden
			}
			extras = append(extras, x)
		}
	}
	return extras, false
}

// note is the text of an annotation: what its author wrote in it.
func note(f *file.File, a syntax.Value, text func(syntax.Value) (string, bool)) (extra, bool) {
	contents, ok := text(a.Get("Contents"))
	if !ok || contents == "" {
		return extra{}, ok
	}
	b := gofastcore.Block{Kind: gofastcore.KindNote, Text: contents}
	b.Attrs = map[string]string{gofastcore.AttrNoteType: gofastcore.NoteComment}
	if author, _ := text(a.Get("T")); author != "" {
		b.Attrs[gofastcore.AttrAuthor] = clipName(f, author)
	}
	if when := parseDate(f.Resolve(a.Get("M"))); !when.IsZero() {
		b.Attrs[gofastcore.AttrDate] = when.Format(time.RFC3339)
	}
	return extra{block: b}, true
}

// fieldValue is the value of the form field that a widget shows.
//
// A field's type, flags and value can stand in the widget's own
// dictionary or in a field above it, and its name is the names from the
// top down. A text field's value is its text; a choice field's the
// option chosen, or the options, as they are displayed; a check box or
// radio button that is on
// gives the name of its state, once, at the widget that shows it. A
// push button has no value, the value of a password field is not read,
// and a signature is not text.
// formField is what a field of the form says of itself, in its own
// dictionary or in a field above it.
type formField struct {
	name                 string // the full name, the names from the top joined by full stops
	kind, value, options syntax.Value
	flags                int64
}

// fieldOf walks from a widget, or a field, up to the top of the form.
func fieldOf(f *file.File, widget syntax.Value) formField {
	var ff formField
	var names []string
	haveFlags := false
	node := widget
	seen := map[int64]bool{}
	depth := 0
	for ; depth < maxFieldDepth && node.IsDict(); depth++ {
		if t := f.Resolve(node.Get("T")); t.Kind == syntax.String {
			names = append(names, clipName(f, strings.TrimSpace(cleanString(charset.TextString(t.Data)))))
		}
		if ff.kind.IsNull() {
			ff.kind = f.Resolve(node.Get("FT"))
		}
		if ff.value.IsNull() {
			ff.value = f.Resolve(node.Get("V"))
		}
		if ff.options.IsNull() {
			ff.options = f.Resolve(node.Get("Opt"))
		}
		if !haveFlags {
			if v := f.Resolve(node.Get("Ff")); !v.IsNull() {
				ff.flags, _ = v.Integer()
				haveFlags = true
			}
		}
		parent := node.Get("Parent")
		if n := file.RefNumber(parent); n != 0 {
			if seen[n] {
				break // the fields lead back into themselves
			}
			seen[n] = true
		}
		node = f.Resolve(parent)
	}
	if depth == maxFieldDepth && node.IsDict() {
		cutOnce(f, boundFieldDepth, nameID(ff.name, ff.name), 1, 0)
	}
	slices.Reverse(names)
	ff.name = clipName(f, strings.Trim(strings.Join(names, "."), "."))
	return ff
}

// maxChoices is the number of options of a choice field that are read.
const maxChoices = 4096

// fieldChoices is the note of the options a choice field offers (a list
// box or a combo box), one per line in the order of the file, as they are
// displayed. A reader who opens the list sees them, so the note is not
// flagged for that.
func fieldChoices(f *file.File, widget syntax.Value, text func(syntax.Value) (string, bool)) (extra, bool) {
	ff := fieldOf(f, widget)
	if !ff.kind.IsName("Ch") {
		return extra{}, true
	}
	var lines []string
	for i, o := range ff.options.Elems() {
		if i >= maxChoices {
			cutOnce(f, boundChoices, nameID(ff.name, ff.name), int64(len(ff.options.Elems())-maxChoices), 0)
			break
		}
		// An option is its text, or a pair of the value that is stored
		// and the text that is displayed.
		o = f.Resolve(o)
		if pair := o.Elems(); len(pair) == 2 {
			o = pair[1]
		}
		s, ok := text(o)
		if !ok {
			return extra{}, false
		}
		if s = strings.ReplaceAll(s, "\n", " "); s != "" {
			lines = append(lines, s)
		}
	}
	if len(lines) == 0 {
		return extra{}, true
	}
	b := gofastcore.Block{Kind: gofastcore.KindNote, Text: charset.Letters(strings.Join(lines, "\n"))}
	b.Attrs = map[string]string{gofastcore.AttrNoteType: gofastcore.NoteChoices, gofastcore.AttrField: ff.name}
	return extra{block: b, field: true}, true
}

func fieldValue(f *file.File, widget syntax.Value, text func(syntax.Value) (string, bool)) (extra, bool) {
	ff := fieldOf(f, widget)
	kind, value, options, flags := ff.kind, ff.value, ff.options, ff.flags

	var out string
	ok := true
	switch {
	case kind.IsName("Tx"):
		if flags&fieldPassword != 0 {
			return extra{}, true
		}
		out, ok = text(value)
	case kind.IsName("Ch"):
		// An option can have two texts, the value that is stored and
		// the one that is displayed. The second is what a reader sees.
		displayed := func(v syntax.Value) syntax.Value {
			v = f.Resolve(v)
			for _, o := range options.Elems() {
				if pair := f.Resolve(o).Elems(); len(pair) == 2 {
					if stored := f.Resolve(pair[0]); stored.Kind == syntax.String && v.Kind == syntax.String && string(stored.Data) == string(v.Data) {
						return pair[1]
					}
				}
			}
			return v
		}
		if value.Kind == syntax.Array {
			var chosen []string
			for _, v := range value.Elems() {
				s, fits := text(displayed(v))
				if !fits {
					return extra{}, false
				}
				if s != "" {
					chosen = append(chosen, s)
				}
			}
			out = strings.Join(chosen, "\n")
		} else {
			out, ok = text(displayed(value))
		}
	case kind.IsName("Btn"):
		// On, and this widget is the one that shows it.
		if flags&fieldPushbutton != 0 || value.Kind != syntax.Name || value.IsName("Off") {
			return extra{}, true
		}
		if state := f.Resolve(widget.Get("AS")); state.Kind == syntax.Name && state.NameString() != value.NameString() {
			return extra{}, true
		}
		out = strings.TrimSpace(cleanString(strings.ToValidUTF8(value.NameString(), "\uFFFD")))
		out = clipName(f, charset.Letters(out))
	}
	if !ok || out == "" {
		return extra{}, ok
	}
	b := gofastcore.Block{Kind: gofastcore.KindParagraph, Text: out}
	b.Attrs = map[string]string{gofastcore.AttrField: ff.name}
	return extra{block: b, field: true}, true
}

// rectOnPage is a rectangle of the page's own coordinates as the box it
// has on the page as it is displayed.
func rectOnPage(f *file.File, v syntax.Value, m [6]float64) (r gofastcore.Rect) {
	elems := f.Resolve(v).Elems()
	if len(elems) != 4 {
		return r
	}
	var c [4]float64
	for i := range c {
		c[i] = f.Resolve(elems[i]).Number()
		if c[i] != c[i] || c[i] > 1e9 || c[i] < -1e9 {
			return gofastcore.Rect{}
		}
	}
	x0, y0 := m[0]*c[0]+m[2]*c[1]+m[4], m[1]*c[0]+m[3]*c[1]+m[5]
	x1, y1 := m[0]*c[2]+m[2]*c[3]+m[4], m[1]*c[2]+m[3]*c[3]+m[5]
	// The page's matrix can take a rectangle that is within the range
	// out of it (a page turned, or its box moved): such a box is none,
	// as one that is out of range in the file.
	for _, v := range [...]float64{x0, y0, x1, y1} {
		if v != v || v > 1e9 || v < -1e9 {
			return gofastcore.Rect{}
		}
	}
	return gofastcore.Rect{X0: min(x0, x1), Y0: min(y0, y1), X1: max(x0, x1), Y1: max(y0, y1)}
}

// addNotes puts the blocks that do not come from the page's content
// among those that do: the value of a field after the last block that
// starts above the middle of its widget, which for a form is the label
// before it; the notes after everything, in the order of the file. It
// keeps what refers to blocks by their place pointing at the same
// blocks.
func (out *pageOut) addNotes(extras []extra) {
	if len(extras) == 0 {
		return
	}
	blocks := out.part.Blocks
	n := len(blocks)
	type placed struct {
		at    int // before the block blocks[at], or after all of them
		y, x  float64
		block *gofastcore.Block
	}
	var fields []placed
	notes := 0
	for i := range extras {
		x := &extras[i]
		if !x.field {
			notes++
			continue
		}
		r := x.block.Loc.BBox
		middle := (r.Y0 + r.Y1) / 2
		at := 0
		for k := range blocks {
			if blocks[k].Loc.BBox.Y0 <= middle {
				at = k + 1
			}
		}
		fields = append(fields, placed{at: at, y: middle, x: r.X0, block: &x.block})
	}
	slices.SortStableFunc(fields, func(a, b placed) int {
		return cmp.Or(cmp.Compare(a.at, b.at), cmp.Compare(a.y, b.y), cmp.Compare(a.x, b.x))
	})

	merged := make([]gofastcore.Block, 0, n+len(extras))
	var styles []layout.Style
	if len(out.styles) == n {
		styles = make([]layout.Style, n+len(extras))
	}
	moved := make([]int32, n) // where each block of the content is now
	next := 0
	for k := 0; k <= n; k++ {
		for next < len(fields) && fields[next].at == k {
			merged = append(merged, *fields[next].block)
			next++
		}
		if k < n {
			moved[k] = int32(len(merged))
			merged = append(merged, blocks[k])
		}
	}
	for i := range extras {
		if !extras[i].field {
			merged = append(merged, extras[i].block)
		}
	}
	out.part.Blocks = merged
	if styles != nil {
		for k := range n {
			styles[moved[k]] = out.styles[k]
		}
		out.styles = styles
	}
	for i := range out.images {
		out.images[i].block = moved[out.images[i].block]
	}
	for i := range out.lines {
		if b := out.lines[i].Block; b >= 0 && b < n {
			out.lines[i].Block = int(moved[b])
		}
	}
}

// noteWarning is the warning of a page that has more text in its
// annotations and fields than is kept.
func noteWarning(part *gofastcore.Part) {
	part.Warn(gofastcore.SeverityError, gofastcore.WarnLimitExceeded,
		fmt.Sprintf("the annotations, form fields and scripts of the page have more than %d bytes of text; the rest is not read", maxPageNoteBytes))
}
