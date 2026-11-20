package gofastpdf

import (
	"strings"
	"unicode"
	"unicode/utf8"

	gofastcore "github.com/AOShei/go-fast-core"
	"github.com/AOShei/go-fast-pdf/internal/layout"
	"github.com/AOShei/go-fast-pdf/internal/syntax"
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
// list ends at a block that is not an item, other than an image or a
// later paragraph of an item of the structure tree.
//
// Items are found from the page alone, before headings: an item whose
// marker counts and that is set as a heading (named by the outline, or
// larger than the text) becomes a heading and keeps its marker, as the
// number of a numbered heading.

// lists marks the list items of a part: those of the structure tree,
// then those found by their markers.
func lists(part *gofastcore.Part, styles []layout.Style, items map[int64]treeItem) {
	tree := treeLists(part.Blocks, styles, items)
	var at []float64 // where the marker of each level stands, for the list being read
	for i := range part.Blocks {
		b := &part.Blocks[i]
		var marker, rest string
		var ordered, ok bool
		native := tree != nil && tree[i]
		if !native {
			marker, rest, ordered, ok = layout.ListMarker(b.Text)
		}
		if !native && (!bodyText(b) || !ok) {
			// The list, if there was one, ends here; not at an image, nor
			// at a paragraph of an item of the tree after its first.
			if b.Kind != gofastcore.KindImage && (i >= len(styles) || styles[i].Item == 0) {
				at = at[:0]
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
		case native && b.Marker == "" && len(at) > 0:
			// An item of the tree whose bullet is drawn, not written: its
			// box starts at its text, right of where markers stand. It is
			// at the level of the item before it.
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
		b.Level = uint8(len(at))
		if native {
			continue
		}
		b.Kind, b.Ordered = gofastcore.KindListItem, ordered
		b.Marker = strings.TrimSpace(marker)
		cut := len(b.Text) - len(rest)
		b.Text = rest
		if len(b.Spans) > 0 {
			cutSpans(b, cut)
		}
	}
}

// Lists of a tagged PDF.
//
// The structure tree can say that content is a list: an element L has
// items, LI, and an item a label, Lbl, the bullet or number in front of
// it, and a body, LBody. A block whose every run is under one item is
// that item, with OriginNative, and its marker the text of its label at
// its start. Its level is found as for items found by their markers,
// by where it stands: programs nest the lists of the tree in more than
// one way (a list in an item, or a list in a list, in as many lists as
// the program used), and how many lists are around an item says less
// than its indent. The layout
// starts a paragraph at the first line of each item, and never joins
// lines of two items, or of an item and what is not in one: an item
// whose bullet is drawn, not written, is an item of its own all the
// same. A second block of the same item, its next paragraph, stays a
// paragraph. Ordered is what the list's ListNumbering attribute says,
// and where it says nothing (or None), whether the marker counts. Items
// of the tree are made where items are found by their markers, after
// the headers and footers and the sizes of the text: until then they
// are paragraphs like the others. The paragraphs that are not items of
// the tree can still be items found by their markers.

// numbering is what a list's attributes say of how its items are
// numbered.
type numbering int8

const (
	numberingUnknown numbering = iota
	numberingOrdered
	numberingUnordered
)

// listNumbering reads the ListNumbering of a list's attributes (/A with
// /O /List).
func (s *structure) listNumbering(el syntax.Value) numbering {
	f := s.f
	a := f.Resolve(el.Get("A"))
	list := []syntax.Value{a}
	if a.Kind == syntax.Array {
		list = a.List
	}
	for _, item := range list {
		d := f.Resolve(item)
		if !d.IsDict() || !f.Resolve(d.Get("O")).IsName("List") {
			continue
		}
		switch f.Resolve(d.Get("ListNumbering")).NameString() {
		case "Decimal", "UpperRoman", "LowerRoman", "UpperAlpha", "LowerAlpha", "Ordered":
			return numberingOrdered
		case "Disc", "Circle", "Square", "Unordered":
			return numberingUnordered
		}
	}
	return numberingUnknown
}

// maxLabelBytes bounds the label of an item of the tree that becomes its
// marker; a longer one stays in the text.
const maxLabelBytes = 32

// treeItem is what the structure tree says of a list item: how its
// list is numbered.
type treeItem struct {
	numbering numbering
}

// treeItems returns what the tree says of each item that a block of a
// page is in; nil for none.
func treeItems(styles []layout.Style, tags []tag) map[int64]treeItem {
	var items map[int64]treeItem
	for k := range styles {
		it := styles[k].Item
		if it == 0 {
			continue
		}
		if items == nil {
			items = map[int64]treeItem{}
			for i := range tags {
				if t := &tags[i]; t.item != 0 {
					if _, ok := items[t.item]; !ok {
						items[t.item] = treeItem{t.numbering}
					}
				}
			}
		}
	}
	return items
}

// treeLists makes the paragraphs that are items of the structure tree
// list items: the first block of each item; a block after it of the
// same item is its next paragraph. It returns which blocks it made
// items, nil for none; their levels are set by the caller.
func treeLists(blocks []gofastcore.Block, styles []layout.Style, items map[int64]treeItem) []bool {
	if items == nil {
		return nil
	}
	var made []bool
	seen := map[int64]bool{}
	for k := range min(len(blocks), len(styles)) {
		b, st := &blocks[k], &styles[k]
		if st.Item == 0 || !bodyText(b) || seen[st.Item] {
			continue
		}
		seen[st.Item] = true
		t, ok := items[st.Item]
		if !ok {
			continue
		}
		marker, rest, ordered, found := treeMarker(b.Text, st.Label)
		switch t.numbering {
		case numberingOrdered:
			ordered = true
		case numberingUnordered:
			ordered = false
		}
		b.Kind, b.Level, b.Ordered, b.Origin = gofastcore.KindListItem, 1, ordered, gofastcore.OriginNative
		if found {
			b.Marker = marker
			cut := len(b.Text) - len(rest)
			b.Text = rest
			if len(b.Spans) > 0 {
				cutSpans(b, cut)
			}
		}
		if made == nil {
			made = make([]bool, len(blocks))
		}
		made[k] = true
	}
	return made
}

// treeMarker finds the marker that the text of an item of the tree
// starts with: its label, when the tree gives one and the text starts
// with it; else a marker as ItemMarker reads one, or a character by
// itself that is a mark, not a word: a symbol or a punctuation mark
// with a space after it ("*", "o" too, which word processors set as a
// bullet), and a bullet, a symbol or a character that could not be read
// (U+FFFD) up against the text as well ("•Text"). An opening bracket or
// quotation mark starts the text, not a marker. The text after the
// marker is rest, never empty.
func treeMarker(text, label string) (marker, rest string, ordered, ok bool) {
	lbl := strings.TrimSpace(label)
	switch {
	case lbl != "" && len(lbl) <= maxLabelBytes && strings.HasPrefix(text, lbl):
		marker, rest = lbl, text[len(lbl):]
		_, _, ordered, _ = layout.ListMarker(lbl + " x")
	default:
		if m, r, o, found := layout.ItemMarker(text); found {
			marker, rest, ordered = strings.TrimSpace(m), r, o
			break
		}
		r, n := utf8.DecodeRuneInString(text)
		after := text[n:]
		spaced := after != "" && (after[0] == ' ' || after[0] == '\t')
		_, _, o, bullet := layout.ListMarker(string(r) + " x")
		glued := bullet && !o || r == utf8.RuneError && n > 0 || unicode.Is(unicode.So, r) || unicode.Is(unicode.Co, r)
		mark := (unicode.IsPunct(r) || unicode.IsSymbol(r)) && !unicode.In(r, unicode.Ps, unicode.Pi) && r != '"' && r != '\''
		if glued || spaced && (mark || r == 'o') {
			marker, rest = string(r), after
		}
	}
	rest = strings.TrimLeft(rest, " \t")
	if marker == "" || rest == "" {
		return "", text, false, false
	}
	return marker, rest, ordered, true
}
