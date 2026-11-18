package file

import (
	"errors"
	"fmt"

	gofastcore "github.com/AOShei/go-fast-core"
	"github.com/AOShei/go-fast-pdf/internal/syntax"
)

// Page is one leaf of the page tree, with the attributes it inherits
// from its ancestors already applied.
type Page struct {
	// Number is the 1-based position of the page in the document.
	Number int
	// Obj is the object number of the page dictionary, 0 if it is direct.
	Obj  int64
	Dict syntax.Value
	// Resources is the resolved resource dictionary, or null.
	Resources syntax.Value
	// MediaBox and CropBox are [llx lly urx ury], normalised. HasMediaBox
	// is false when no box is given anywhere up the tree.
	MediaBox, CropBox [4]float64
	HasMediaBox       bool
	// Rotate is the page rotation in degrees.
	Rotate int
	// Err is set when the entry in the page tree is not a readable page.
	// The page still counts, so that page numbers stay true.
	Err error
}

// Pages returns the pages of the document in order. The slice is shared
// and must not be modified.
func (f *File) Pages() []Page { return f.pages }

// inherited holds the attributes a page tree node passes down.
type inherited struct {
	resources syntax.Value
	mediaBox  syntax.Value
	cropBox   syntax.Value
	rotate    syntax.Value
}

// flattenPages walks the page tree once. /Count is not trusted.
func (f *File) flattenPages(root syntax.Value) []Page {
	type item struct {
		ref syntax.Value
		inh inherited
	}
	var pages []Page
	seen := map[int64]bool{}
	stack := []item{{ref: root}}
	for len(stack) > 0 {
		it := stack[len(stack)-1]
		stack = stack[:len(stack)-1]

		num := RefNumber(it.ref)
		if len(pages) >= f.limits.MaxPages {
			f.Warn(gofastcore.SeverityError, gofastcore.WarnLimitExceeded,
				fmt.Sprintf("the page tree has more than %d pages", f.limits.MaxPages))
			break
		}
		node := f.Resolve(it.ref)
		if node.Kind != syntax.Dict {
			pages = append(pages, Page{Number: len(pages) + 1, Obj: num,
				Err: fmt.Errorf("%w: page tree entry is a %s", gofastcore.ErrMalformed, node.Kind)})
			continue
		}
		inh := it.inh
		for _, a := range []struct {
			key string
			dst *syntax.Value
		}{{"Resources", &inh.resources}, {"MediaBox", &inh.mediaBox}, {"CropBox", &inh.cropBox}, {"Rotate", &inh.rotate}} {
			if v := node.Get(a.key); !v.IsNull() {
				*a.dst = v
			}
		}

		// A node with /Kids is an intermediate node, whatever its /Type
		// says or does not say.
		kids := f.Resolve(node.Get("Kids"))
		if kids.Kind == syntax.Array && !node.Get("Type").IsName("Page") {
			// An intermediate node reached a second time is skipped, which
			// ends any cycle. A page may be listed more than once and is
			// then a page each time, as other readers treat it.
			if num != 0 {
				if seen[num] {
					continue
				}
				seen[num] = true
			}
			// Push in reverse so that the first kid is handled first.
			for i := len(kids.List) - 1; i >= 0; i-- {
				stack = append(stack, item{ref: kids.List[i], inh: inh})
			}
			continue
		}
		if node.Has("Kids") && !node.Get("Type").IsName("Page") {
			// /Kids is present but is not an array: nothing to walk.
			f.Warn(gofastcore.SeverityWarning, "pdf.page_tree_damaged", "a page tree node has a /Kids that is not an array")
			continue
		}

		p := Page{Number: len(pages) + 1, Obj: num, Dict: node}
		if res := f.Resolve(inh.resources); res.IsDict() {
			p.Resources = res
		}
		if box, ok := f.rect(inh.mediaBox); ok {
			p.MediaBox, p.HasMediaBox = box, true
			p.CropBox = box
		}
		if box, ok := f.rect(inh.cropBox); ok {
			p.CropBox = box
		}
		if r, ok := f.Resolve(inh.rotate).Integer(); ok {
			p.Rotate = int(((r % 360) + 360) % 360)
		}
		pages = append(pages, p)
	}
	return pages
}

// Rect reads a rectangle of four numbers, such as a /BBox, and puts its
// corners in order: [llx lly urx ury].
func (f *File) Rect(v syntax.Value) ([4]float64, bool) { return f.rect(v) }

// rect reads a rectangle of four numbers and puts its corners in order.
func (f *File) rect(v syntax.Value) ([4]float64, bool) {
	elems := f.Resolve(v).Elems()
	if len(elems) < 4 {
		return [4]float64{}, false
	}
	var r [4]float64
	for i := range r {
		n, ok := f.Resolve(elems[i]).Float()
		if !ok {
			return [4]float64{}, false
		}
		r[i] = n
	}
	if r[0] > r[2] {
		r[0], r[2] = r[2], r[0]
	}
	if r[1] > r[3] {
		r[1], r[3] = r[3], r[1]
	}
	return r, true
}

// ErrNoPages is returned by callers that need at least one page.
var ErrNoPages = errors.New("the document has no pages")
