package gofastpdf

import (
	"strings"
	"unicode"
	"unicode/utf8"

	gofastcore "github.com/AOShei/go-fast-core"

	"github.com/AOShei/go-fast-pdf/internal/layout"
)

// Footnotes.
//
// A page says nothing of which text is a note. A paragraph is taken for
// one when three things hold together: it starts with the mark of a note,
// a number of up to three digits or up to three of the signs * † ‡ §,
// before a letter that is not small or a quote (after a sign, any
// letter); it is set smaller than the text of the document; and in its
// column the text of the document stands above it and none below it.
// One of these alone is common in text that is no note: a small caption,
// a line that starts with a number, the last paragraph of a page.
//
// Such a block is a KindFootnote, inferred, with its mark as its Marker
// and out of its Text. A note without a mark, as the second paragraph of
// a note or a note that goes on from the page before, is not found, nor
// is a note set as large as the text.
//
// Notes are found once the text of the pages around is counted, since
// the size of the text of the document is that of most of its letters.

// noteSize is how large a note can be, as a share of the size of the
// text of the document.
const noteSize = 0.95

// noteBody is the least text, in bytes, of a block that counts as the
// text of the document above or below a note.
const noteBody = 40

// noteSigns are the signs that mark a note without counting it.
const noteSigns = "*†‡§"

// notes marks the footnotes of a part.
func notes(part *gofastcore.Part, styles []layout.Style, s *sizes) {
	body, _, ok := s.body()
	if !ok {
		return
	}
	limit := noteSize * body
	for i := range part.Blocks {
		b := &part.Blocks[i]
		if i >= len(styles) || !bodyText(b) || styles[i].Size <= 0 || styles[i].Size > limit {
			continue
		}
		end, at := noteMark(b.Text)
		if end == 0 || strings.IndexByte(b.Text, '\t') >= 0 {
			continue
		}
		box := b.Loc.BBox
		above, below := false, false
		for j := range part.Blocks {
			q := &part.Blocks[j]
			if j == i || j >= len(styles) || styles[j].Size <= limit || len(q.Text) <= noteBody || !bodyText(q) {
				continue
			}
			qb := q.Loc.BBox
			if min(qb.X1, box.X1) <= max(qb.X0, box.X0) {
				continue // another column
			}
			if qb.Y1 <= box.Y0+1 {
				above = true
			} else if qb.Y0 >= box.Y1-1 {
				below = true
				break
			}
		}
		if !above || below {
			continue
		}
		b.Kind, b.Marker = gofastcore.KindFootnote, b.Text[:end]
		b.Text = b.Text[at:]
		if len(b.Spans) > 0 {
			cutSpans(b, at)
		}
	}
}

// noteMark finds the mark of a note that text starts with: it is
// text[:end], and the note starts at text[at:]. end is 0 when text does
// not start with one.
func noteMark(text string) (end, at int) {
	sign := false
	for end < len(text) && end < 3 && text[end] >= '0' && text[end] <= '9' {
		end++
	}
	if end == 0 {
		for n := 0; n < 3; n++ {
			r, size := utf8.DecodeRuneInString(text[end:])
			if size == 0 || !strings.ContainsRune(noteSigns, r) {
				break
			}
			end += size
		}
		sign = true
	}
	if end == 0 {
		return 0, 0
	}
	at = end
	for at < len(text) && text[at] == ' ' {
		at++
	}
	r, _ := utf8.DecodeRuneInString(text[at:])
	switch {
	case unicode.IsLetter(r) && (sign || !unicode.IsLower(r)):
	case strings.ContainsRune("\"'“‘«„", r):
	default:
		return 0, 0
	}
	return end, at
}
