package layout

import (
	"strings"

	gofastcore "github.com/AOShei/go-fast-core"
)

// Inline spans over the text of the blocks: bold and italic, and links.
//
// Each word of a block (see eachWord) has the bold and italic of the
// font its first letter is set in, and a place on the page. It is found
// in the text of its block, in order; a word that the joining of lines
// changed (a soft hyphen at the end of a line) may not be, and has no
// span. Words one after the other with the same style make one span,
// the space between them included, and so do words of the same link.

// Link is a link of the page: the boxes it covers, displayed, and its
// target.
type Link struct {
	Boxes []gofastcore.Rect
	Href  string
}

// placed is a word found in the text of its block.
type placed struct {
	start, end int
	style      gofastcore.Style
	href       string
}

// spansOf gives the text blocks of res, which are b.blocks[final], the
// spans of their words.
func (b *builder) spansOf(res *Result, final []int32) {
	atoms, words := b.atomBuf[:0], b.placedBuf[:0]
	defer func() { b.atomBuf, b.placedBuf = atoms[:0], words[:0] }()
	for k, bi := range final {
		bl := &res.Blocks[k]
		words = words[:0]
		at := 0
		for li := b.blocks[bi].line; li >= 0; li = b.lines[li].next {
			ln := &b.lines[li]
			if ln.hi <= ln.lo {
				continue
			}
			raw := b.text[ln.tlo:ln.thi]
			atoms = b.atoms(atoms[:0], ln)
			b.eachWord(raw, atoms, ln, func(i, j int, box gofastcore.Rect, style gofastcore.Style) {
				var start, n int
				if w := raw[i:j]; printableASCII(w) {
					// Cleaning leaves it as it is.
					start, n = locate(bl.Text, at, w), len(w)
				} else {
					c := b.clean(w)
					start, n = locate(bl.Text, at, c), len(c)
				}
				if start < 0 || n == 0 {
					return
				}
				w := placed{start: start, end: start + n, style: style}
			links:
				for _, l := range b.opts.Links {
					for _, r := range l.Boxes {
						if linked(box, r) {
							w.href = l.Href
							break links
						}
					}
				}
				words = append(words, w)
				at = w.end
			})
		}
		bl.Spans = appendRuns(bl.Spans, words, false)
		if len(b.opts.Links) > 0 {
			bl.Spans = appendRuns(bl.Spans, words, true)
		}
	}
}

// linked reports whether a word, at w, is text of a link whose box is r:
// along the line they share half of the word or half of the box, and
// across it a third of the lower of the two. A link over a raised
// footnote number at the end of a word makes the word a link.
func linked(w, r gofastcore.Rect) bool {
	along := min(w.X1, r.X1) - max(w.X0, r.X0)
	across := min(w.Y1, r.Y1) - max(w.Y0, r.Y0)
	return along > 0 && across > 0 && (along >= 0.5*(w.X1-w.X0) || along >= 0.5*(r.X1-r.X0)) &&
		across >= min(w.Y1-w.Y0, r.Y1-r.Y0)/3
}

// appendRuns appends a span for each run of words one after the other
// with the same style (or, with links, the same link), other than none.
func appendRuns(spans []gofastcore.Span, words []placed, links bool) []gofastcore.Span {
	key := func(w *placed) (gofastcore.Style, string) {
		if links {
			return 0, w.href
		}
		return w.style, ""
	}
	for i := 0; i < len(words); {
		s, h := key(&words[i])
		j := i + 1
		for j < len(words) {
			if s2, h2 := key(&words[j]); s2 != s || h2 != h {
				break
			}
			j++
		}
		if s != 0 || h != "" {
			spans = append(spans, gofastcore.Span{Start: words[i].start, End: words[j-1].end, Style: s, Href: h})
		}
		i = j
	}
	return spans
}

// locate returns where word is in text at from or a little after it,
// past the spaces there; -1 if it is not.
func locate[T string | []byte](text string, from int, word T) int {
	if len(word) == 0 || from > len(text) {
		return -1
	}
	for from < len(text) && (text[from] == ' ' || text[from] == '\n' || text[from] == '\t') {
		from++
	}
	if len(text)-from >= len(word) && text[from:from+len(word)] == string(word) {
		return from
	}
	// The word was changed where lines were joined, or a word before it
	// was: it is looked for in a few bytes more.
	end := min(len(text), from+len(word)+16)
	if k := strings.Index(text[from:end], string(word)); k >= 0 {
		return from + k
	}
	return -1
}
