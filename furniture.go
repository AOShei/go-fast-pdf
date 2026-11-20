package gofastpdf

import (
	"strings"
	"unicode"
	"unicode/utf8"

	gofastcore "github.com/AOShei/go-fast-core"
)

// Running headers and footers, and paragraphs that a page break splits.
//
// A running header or footer is text that the pages of a document repeat
// in their margins: a title, a chapter name, a date, a page number. It
// is told from the text of a page by comparing the page with the pages
// around it, as they are handed on in page order. A block is a header
// (at the top of the page) or a footer (at the bottom) when it is short,
// stands in the tenth of the page at that edge, and its text stands at
// about the same height on at least two of the neighbouring pages, with
// its numbers left out of the comparison so that page numbers match. The
// renderers of the shared model leave headers and footers out unless
// asked for them; they stay in the document. A page whose text is all in
// its margins has no headers or footers: its text is not running over
// anything. A line of more than three pieces, as a row of a table that
// each page repeats, is not one, nor is a line of numbers alone, as the
// scale of a map, unless it is one number. Only the pages that are read
// are compared: in a short range of a document whose headers differ
// between left and right pages, one side may be on too few pages.
//
// A paragraph that a page break splits is marked Continued on the page
// where it goes on: when the last paragraph of a page ends without the
// mark that ends a sentence and the first paragraph of the next page
// starts with a small letter.
//
// Both need the pages around a page, so they are decided where the pages
// are put in order: up to furnitureReach pages wait there to be compared,
// and what is decided does not depend on how the pages were read. A page
// read by itself (Page.Part) is compared with nothing and has neither.

// furnitureReach is the number of pages on each side of a page that it
// is compared with. Four reach the pages of the same side, left or right,
// twice on each side.
const furnitureReach = 4

// furnitureMaxBytes is the longest text a header or footer can have.
const furnitureMaxBytes = 160

// furnitureBand is the share of the page height at the top and at the
// bottom in which headers and footers stand.
const furnitureBand = 0.1

// mark is a block of a page that may be a header or a footer: its text
// as it is compared, and where it stands.
type mark struct {
	block int
	text  string
	top   bool
	// at is how far the block is from its edge of the page, in points.
	at float64
}

// marks are the blocks in the margins of a part that may be headers or
// footers.
func marks(part *gofastcore.Part) []mark {
	h := part.Height
	if h <= 0 {
		return nil
	}
	var out []mark
	body := false // text of the page outside its margins
	for i := range part.Blocks {
		b := &part.Blocks[i]
		if !bodyText(b) {
			continue
		}
		r := b.Loc.BBox
		short := len(b.Text) <= furnitureMaxBytes && strings.Count(b.Text, "\n") <= 1 && runningLine(b.Text)
		switch {
		case short && r.Y1 <= furnitureBand*h:
			out = append(out, mark{block: i, text: comparable(b.Text), top: true, at: r.Y0})
		case short && r.Y0 >= (1-furnitureBand)*h:
			out = append(out, mark{block: i, text: comparable(b.Text), at: h - r.Y1})
		default:
			body = true
		}
	}
	// A page whose text is all in its margins has no text that headers
	// would run over: what it has is its text, a slide's title or a
	// line set at the top of an otherwise empty page.
	if !body {
		return nil
	}
	return out
}

// runningLine reports whether text has the form of a running header or
// footer: at most three pieces, left, centre and right (a row of a table
// that each page repeats has more), and, if it has no letter, one item
// alone, as a page number is (the scale of a map has more).
func runningLine(text string) bool {
	if strings.Count(text, "\t") > 2 {
		return false
	}
	for _, r := range text {
		if unicode.IsLetter(r) {
			return true
		}
	}
	return len(strings.Fields(text)) == 1
}

// bodyText reports whether a block is text that the page sets: a
// paragraph laid out from the page's content, in view.
func bodyText(b *gofastcore.Block) bool {
	return b.Kind == gofastcore.KindParagraph && b.Origin == gofastcore.OriginInferred && b.Display == 0 && b.Text != ""
}

// flowText reports whether a block is text that runs from page to page:
// a paragraph or a list item laid out from the page's content, in view.
func flowText(b *gofastcore.Block) bool {
	return bodyText(b) || b.Kind == gofastcore.KindListItem && b.Origin == gofastcore.OriginInferred && b.Display == 0
}

// comparable is text as it is compared with the text of other pages: in
// small letters, with every number, Arabic or Roman, as one sign, and
// with its spaces made one.
func comparable(text string) string {
	var sb strings.Builder
	for _, word := range strings.Fields(strings.ToLower(text)) {
		if sb.Len() > 0 {
			sb.WriteByte(' ')
		}
		if romanNumber(word) {
			sb.WriteByte('#')
			continue
		}
		digits := false
		for _, r := range word {
			if unicode.IsDigit(r) {
				if !digits {
					sb.WriteByte('#')
				}
				digits = true
				continue
			}
			digits = false
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

// romanNumber reports whether a word is a Roman number of the size that pages
// are numbered with.
func romanNumber(word string) bool {
	word = strings.Trim(word, ".-–—()[]")
	if word == "" || len(word) > 7 {
		return false
	}
	for _, r := range word {
		if !strings.ContainsRune("ivxlc", r) {
			return false
		}
	}
	return true
}

// lastBody is what is known of the last paragraph of a page: whether it
// goes on.
type lastBody struct {
	open bool // it ends without the mark that ends a sentence
}

// furnitureWindow holds the pages that wait to be compared with the
// pages after them.
type furnitureWindow struct {
	pending []pageOut
	pmarks  [][]mark // the marks of each pending page
	behind  [][]mark // the marks of the pages handed on, the last furnitureReach of them
	last    lastBody
	done    bool // a page was refused; nothing more is handed on
	// firstLater is set while the marks of the first page are not read:
	// they are when a second page comes.
	firstLater bool

	// The letters of the pages by size, for headings, and the entries of
	// the outline by the page they point at.
	sizes  sizes
	titles map[int][]title
	// dests gives the named destinations of a page, for its anchors.
	dests *destinations

	sizesRoom   [16]sizeCount
	pendingRoom [2 * (furnitureReach + 1)]pageOut
	pmarksRoom  [2 * (furnitureReach + 1)][]mark
	behindRoom  [furnitureReach][]mark
}

// push takes the next page in order and hands on the page that now has
// all its neighbours. It reports false once emit has.
func (w *furnitureWindow) push(out *pageOut, emit func(*pageOut) bool) bool {
	if w.done {
		return false
	}
	if w.pending == nil {
		// The room is in the window itself: its size is known, or as for
		// the sizes of the text, a document has few.
		w.pending, w.pmarks, w.behind = w.pendingRoom[:0], w.pmarksRoom[:0], w.behindRoom[:0]
		w.sizes = w.sizesRoom[:0]
	}
	if len(w.pending) == cap(w.pending) {
		// Move what waits to the front, so the room is used again.
		n := copy(w.pending[:cap(w.pending)], w.pending)
		w.pending = w.pending[:n]
		copy(w.pmarks[:cap(w.pmarks)], w.pmarks)
		w.pmarks = w.pmarks[:n]
	}
	// The first page is compared only if a second comes: a document of
	// one page needs nothing of this.
	alone := len(w.pending) == 0 && len(w.behind) == 0
	if !alone && w.firstLater {
		w.pmarks[0], w.firstLater = marks(&w.pending[0].part), false
	}
	w.sizes.add(&out.part, out.styles)
	w.pending = append(w.pending, *out)
	if alone {
		w.pmarks, w.firstLater = append(w.pmarks, nil), true
	} else {
		w.pmarks = append(w.pmarks, marks(&out.part))
	}
	if len(w.pending) > furnitureReach {
		return w.next(emit)
	}
	return true
}

// flush hands on the pages that wait, with the neighbours there are.
func (w *furnitureWindow) flush(emit func(*pageOut) bool) {
	for len(w.pending) > 0 && !w.done {
		w.next(emit)
	}
}

// next decides the first page that waits and hands it on.
func (w *furnitureWindow) next(emit func(*pageOut) bool) bool {
	out, own := &w.pending[0], w.pmarks[0]
	ahead := w.pmarks[1:min(len(w.pmarks), furnitureReach+1)]
	part := &out.part
	need := min(2, len(w.behind)+len(ahead))
	for _, m := range own {
		if need == 0 {
			break
		}
		tol := max(6, 0.015*part.Height)
		seen := 0
		for _, pages := range [2][][]mark{w.behind, ahead} {
			for _, page := range pages {
				for _, n := range page {
					if n.top == m.top && n.text == m.text && n.at-m.at <= tol && m.at-n.at <= tol {
						seen++
						break
					}
				}
			}
		}
		if seen >= need {
			if m.top {
				part.Blocks[m.block].Kind = gofastcore.KindHeader
			} else {
				part.Blocks[m.block].Kind = gofastcore.KindFooter
			}
		}
	}

	// List items, from the page alone, after its headers and footers.
	lists(part, out.styles, out.items)

	// A paragraph split by the page break before this page. A list item
	// is text that flows too, but starts an item of its own.
	first, last := -1, -1
	for i := range part.Blocks {
		if flowText(&part.Blocks[i]) {
			if first < 0 {
				first = i
			}
			last = i
		}
	}
	if first >= 0 && w.last.open && part.Blocks[first].Kind == gofastcore.KindParagraph {
		if r, _ := utf8.DecodeRuneInString(part.Blocks[first].Text); unicode.IsLower(r) {
			part.Blocks[first].Continued = true
		}
	}
	w.last = lastBody{}
	if last >= 0 {
		w.last.open = !sentenceEnds(part.Blocks[last].Text)
	}

	headings(part, out.styles, &w.sizes, w.titles[part.Number])
	// The names of places on the page, once its headers and footers are
	// known: a name does not go on those.
	nameAnchors(part, w.dests.named(part.Number))

	if len(w.behind) == furnitureReach {
		copy(w.behind, w.behind[1:])
		w.behind = w.behind[:furnitureReach-1]
	}
	w.behind = append(w.behind, own)
	ok := emit(out)
	w.pending, w.pmarks = w.pending[1:], w.pmarks[1:]
	if !ok {
		w.done = true
	}
	return ok
}

// sentenceEnds reports whether text ends as a sentence or a heading
// does: with a full stop, a question or exclamation mark, a colon, or
// one of these inside a closing quote or bracket.
func sentenceEnds(text string) bool {
	text = strings.TrimRight(text, " \t\n")
	for text != "" {
		r, size := utf8.DecodeLastRuneInString(text)
		switch {
		case strings.ContainsRune(".!?:;…。！？", r):
			return true
		case strings.ContainsRune("\"'”’»)]", r):
			text = text[:len(text)-size]
			continue
		}
		return false
	}
	return true
}
