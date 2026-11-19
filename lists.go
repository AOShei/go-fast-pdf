package gofastpdf

import (
	"strings"

	gofastcore "github.com/AOShei/go-fast-core"
	"github.com/AOShei/go-fast-pdf/internal/layout"
)

// List items.
//
// A paragraph that starts with the marker of a list item (layout.
// ListMarker: a bullet, or a number or letter that counts) is a list
// item. The marker goes to Block.Marker, out of the text, and the
// renderers put it back in front; Ordered is set when it counts. The
// layout starts a paragraph at every line with a marker, so that items
// set one under the other are items of their own.
//
// Its level is how deep it is nested, by where its marker stands: an
// item further right than the item before it is one level deeper, one
// further left goes back to the level of the item it lines up with. A
// list ends at a block that is not an item.
//
// Items are found from the page alone, before headings: an item whose
// marker counts and that is set as a heading (named by the outline, or
// larger than the text) becomes a heading and keeps its marker, as the
// number of a numbered heading.

// lists marks the list items of a part.
func lists(part *gofastcore.Part, styles []layout.Style) {
	var at []float64 // where the marker of each level stands, for the list being read
	for i := range part.Blocks {
		b := &part.Blocks[i]
		marker, rest, ordered, ok := layout.ListMarker(b.Text)
		if !bodyText(b) || !ok {
			if b.Kind != gofastcore.KindImage {
				at = at[:0] // the list, if there was one, ends here
			}
			continue
		}
		size := 10.0
		if i < len(styles) && styles[i].Size > 0 {
			size = styles[i].Size
		}
		tol := 0.5 * size
		x := b.Loc.BBox.X0
		switch {
		case len(at) == 0:
			at = append(at, x)
		case x > at[len(at)-1]+tol:
			if len(at) < 6 {
				at = append(at, x)
			}
		default:
			for len(at) > 1 && x < at[len(at)-1]-tol {
				at = at[:len(at)-1]
			}
		}
		b.Kind, b.Level, b.Ordered = gofastcore.KindListItem, uint8(len(at)), ordered
		b.Marker = strings.TrimSpace(marker)
		b.Text = rest
	}
}
