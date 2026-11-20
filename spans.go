package gofastpdf

import (
	"strconv"
	"strings"
	"unicode/utf8"

	gofastcore "github.com/AOShei/go-fast-core"
	"github.com/AOShei/go-fast-pdf/internal/file"
	"github.com/AOShei/go-fast-pdf/internal/layout"
	"github.com/AOShei/go-fast-pdf/internal/syntax"
)

// Inline spans: bold and italic, and links (Options.Spans).
//
// The layout makes the spans (layout.spansOf): bold and italic by the
// fonts of the words, and links by the words that a link covers, a word
// as a whole. The links are the link annotations of the page, read
// here: the boxes of each (its quadrilaterals, or its rectangle) and its
// target, the address of a URI action, or, for a destination in the
// document, "#page=N", the open parameter of a PDF for page N. A link
// that goes to another file, launches something or is hidden has no
// span; an address longer than maxHrefBytes neither. Link targets count
// against the limit on text with the block they are in.

// maxHrefBytes is the longest address of a link that is kept.
const maxHrefBytes = 4096

// pageLinks returns the links of a page.
func pageLinks(f *file.File, p *file.Page, m [6]float64, width, height float64, dests *destinations) []layout.Link {
	annots := f.Resolve(p.Dict().Get("Annots")).Elems()
	if len(annots) > maxAnnotations {
		annots = annots[:maxAnnotations]
	}
	var out []layout.Link
	for _, ref := range annots {
		a := f.Resolve(ref)
		if !a.IsDict() || !f.Resolve(a.Get("Subtype")).IsName("Link") {
			continue
		}
		const hidden, noView = 1 << 1, 1 << 5
		if flags, _ := f.Resolve(a.Get("F")).Integer(); flags&(hidden|noView) != 0 {
			continue
		}
		href := ""
		act := f.Resolve(a.Get("A"))
		switch {
		case act.IsDict() && f.Resolve(act.Get("S")).IsName("URI"):
			if uri := f.Resolve(act.Get("URI")); uri.Kind == syntax.String {
				href = strings.TrimSpace(cleanString(strings.ToValidUTF8(string(uri.Data), "")))
			}
		case a.Has("Dest") || act.IsDict() && f.Resolve(act.Get("S")).IsName("GoTo"):
			if dests != nil {
				if part, _ := dests.target(a); part > 0 {
					href = "#page=" + strconv.Itoa(part)
				}
			}
		}
		if href == "" || len(href) > maxHrefBytes {
			continue
		}
		l := layout.Link{Href: href}
		if q := f.Resolve(a.Get("QuadPoints")).Elems(); len(q) >= 8 && len(q)%8 == 0 {
			for i := 0; i+8 <= len(q) && len(l.Boxes) < 64; i += 8 {
				var r gofastcore.Rect
				for k := 0; k < 4; k++ {
					x, y := f.Resolve(q[i+2*k]).Number(), f.Resolve(q[i+2*k+1]).Number()
					dx, dy := m[0]*x+m[2]*y+m[4], m[1]*x+m[3]*y+m[5]
					if k == 0 {
						r = gofastcore.Rect{X0: dx, Y0: dy, X1: dx, Y1: dy}
					}
					r = gofastcore.Rect{X0: min(r.X0, dx), Y0: min(r.Y0, dy), X1: max(r.X1, dx), Y1: max(r.Y1, dy)}
				}
				if r.X0 == r.X0 && r.Y0 == r.Y0 && r.X1 == r.X1 && r.Y1 == r.Y1 { // not NaN
					l.Boxes = append(l.Boxes, r)
				}
			}
		}
		if len(l.Boxes) == 0 {
			if r := rectOnPage(f, a.Get("Rect"), m); r.X1 > r.X0 && r.Y1 > r.Y0 {
				l.Boxes = append(l.Boxes, r)
			}
		}
		if len(l.Boxes) > 0 {
			out = append(out, l)
		}
	}
	return out
}

// cutSpans keeps the spans of a block whose first n bytes of text were
// taken away: the marker of a list item.
func cutSpans(b *gofastcore.Block, n int) {
	out := b.Spans[:0]
	for _, s := range b.Spans {
		s.Start, s.End = max(s.Start-n, 0), min(s.End-n, len(b.Text))
		for s.Start < s.End && !utf8.RuneStart(b.Text[s.Start]) {
			s.Start++
		}
		if s.Start < s.End {
			out = append(out, s)
		}
	}
	if b.Spans = out; len(out) == 0 {
		b.Spans = nil
	}
}

// blockBytes is what a block takes from the limit on text: its cost, its
// text, its marker, the targets of its links, its anchor and the cells
// of its table.
func blockBytes(b *gofastcore.Block) int {
	n := gofastcore.BlockCost + len(b.Text) + len(b.Marker) + len(b.Loc.Anchor)
	if t := b.Table; t != nil {
		for _, row := range t.Rows {
			for _, cell := range row {
				n += gofastcore.CellCost + len(cell)
			}
		}
	}
	for i := range b.Spans {
		n += len(b.Spans[i].Href)
	}
	return n
}
