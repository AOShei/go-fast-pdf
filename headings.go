package gofastpdf

import (
	"math"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	gofastcore "github.com/AOShei/go-fast-core"
	"github.com/AOShei/go-fast-pdf/internal/layout"
)

// Headings.
//
// A PDF without tags does not say which text is a heading. Two things
// can: the outline, whose entries point at the place a heading stands,
// and the type, since a heading is set larger than the text it heads,
// or bold.
//
//   - A block that an entry of the outline names, on the page it points
//     at, is a heading at the level of the entry, with OriginNative: the
//     file says so. Its text and the title are compared by their letters
//     and digits.
//   - Otherwise a short block is a heading when it is set at least 15
//     percent larger than the text of the document, or bold, on one
//     line, in a document whose text is not bold. Its level is from its
//     size: 1.8 times the text or more is 1 (a title is set about twice
//     as large), 1.4 is 2, larger is 3, bold at the size of the text is
//     4.
//
// The size of the text of the document is the size most of its letters
// are set in, counted over every page that has been put in order so far
// and the pages that wait after it. It is decided in the window of
// furniture.go, so that it does not depend on how the pages were read.

// Heading levels from size, as the ratio to the size of the text.
const (
	headingRatio1 = 1.8
	headingRatio2 = 1.4
	headingRatio3 = 1.15
	// headingBoldFloor is the least ratio at which a bold line can be a
	// heading of level 4: text a little smaller than the body, as some
	// bold faces are measured, still counts.
	headingBoldFloor = 0.95
	// headingMaxBytes and headingMaxLines bound a heading.
	headingMaxBytes = 200
	headingMaxLines = 3
)

// sizes counts the text of the document by the size it is set in,
// in steps of half a point, and how many of them are bold. A document
// has few sizes: they are kept in a list.
type sizes []sizeCount

type sizeCount struct {
	twice         int // twice the size, rounded
	letters, bold int
}

func (s *sizes) add(part *gofastcore.Part, styles []layout.Style) {
	for i := range part.Blocks {
		if i >= len(styles) || styles[i].Size <= 0 || !bodyText(&part.Blocks[i]) {
			continue
		}
		k := int(math.Round(styles[i].Size * 2))
		n := len(part.Blocks[i].Text) // bytes are as good a weight as letters
		at := -1
		for j := range *s {
			if (*s)[j].twice == k {
				at = j
				break
			}
		}
		if at < 0 {
			if len(*s) >= 256 {
				continue // sizes beyond these are too rare to be the text's
			}
			*s = append(*s, sizeCount{twice: k})
			at = len(*s) - 1
		}
		(*s)[at].letters += n
		if styles[i].Bold {
			(*s)[at].bold += n
		}
	}
}

// body returns the size most letters are set in, and whether most of
// them are bold. ok is false when nothing has been counted.
func (s *sizes) body() (size float64, bold, ok bool) {
	best := -1
	for j, c := range *s {
		// The larger number wins; of two the same, the smaller size.
		if best < 0 || c.letters > (*s)[best].letters || c.letters == (*s)[best].letters && c.twice < (*s)[best].twice {
			best = j
		}
	}
	if best < 0 || (*s)[best].letters == 0 {
		return 0, false, false
	}
	c := (*s)[best]
	return float64(c.twice) / 2, c.bold*2 > c.letters, true
}

// fold is text as it is compared with a title of the outline: its
// letters and digits, in small letters, without the number a heading
// may start with, since the outline and the page do not always both
// give it ("2.3 Methods" and "Methods").
func fold(text string) string {
	var sb strings.Builder
	for _, r := range strings.ToLower(text) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			sb.WriteRune(r)
		}
	}
	s := sb.String()
	if rest := strings.TrimLeft(s, "0123456789"); len(rest) >= 3 {
		return rest
	}
	return s
}

// noOutlineHeadings leaves out what the outline says, so that a test can
// measure the headings found by type alone against it.
var noOutlineHeadings = false

// title is an entry of the outline as it is compared with blocks.
type title struct {
	folded string
	level  int
}

// titlesByPage indexes the entries of the outline by the page they point
// at, folded once.
func titlesByPage(outline []gofastcore.OutlineItem) map[int][]title {
	if noOutlineHeadings || len(outline) == 0 {
		return nil
	}
	out := map[int][]title{}
	for _, o := range outline {
		if f := fold(o.Title); f != "" && o.Part > 0 {
			out[o.Part] = append(out[o.Part], title{f, o.Level})
		}
	}
	return out
}

// headings marks the headings of a part. titles are the entries of the
// outline that point at its page; the function may reorder them.
func headings(part *gofastcore.Part, styles []layout.Style, s *sizes, titles []title) {
	body, bodyBold, ok := s.body()
	// The levels found by type, decided after the page has been seen
	// whole: a page with more blocks that look like headings than it can
	// have (a map, a diagram, a poster) gets none of them.
	type found struct{ block, level int }
	var byType []found
	text := 0
	for i := range part.Blocks {
		b := &part.Blocks[i]
		// A numbered item can be a numbered heading; a bullet cannot.
		item := b.Kind == gofastcore.KindListItem && b.Ordered && b.Origin == gofastcore.OriginInferred && b.Hidden == 0
		if !bodyText(b) && !item || b.Continued || len(b.Text) > headingMaxBytes {
			continue
		}
		var st layout.Style
		if i < len(styles) {
			st = styles[i]
		}
		if st.Lines > headingMaxLines {
			continue
		}
		// Named by the outline.
		named := false
		if len(titles) > 0 {
			folded := fold(b.Text)
			for k, o := range titles {
				if folded == o.folded {
					setHeading(b, o.level)
					b.Origin = gofastcore.OriginNative
					titles = append(titles[:k], titles[k+1:]...) // one block for an entry
					named = true
					break
				}
			}
		}
		if named {
			continue
		}
		text++
		if !ok || st.Size <= 0 || !headingText(b.Text) {
			continue
		}
		// By type.
		ratio := st.Size / body
		level := 0
		switch {
		case ratio >= headingRatio1:
			level = 1
		case ratio >= headingRatio2:
			level = 2
		case ratio >= headingRatio3:
			level = 3
		case !item && st.Bold && !bodyBold && ratio >= headingBoldFloor && st.Lines == 1 && len(b.Text) <= headingMaxBytes/2 && startsHeading(b.Text):
			level = 4
		}
		if level > 0 {
			byType = append(byType, found{i, level})
		}
	}
	if len(byType) > max(headingMaxPerPage, text/2) {
		return
	}
	for _, h := range byType {
		setHeading(&part.Blocks[h.block], h.level)
	}
}

// headingMaxPerPage is the number of headings by type a page can have
// whatever its text: more than that, and more than half its blocks, and
// the page is taken for a map or a diagram, whose labels are set large.
const headingMaxPerPage = 8

// setHeading makes a block a heading at a level of the outline, which
// can be deeper than the six levels of the shared model.
func setHeading(b *gofastcore.Block, level int) {
	b.Kind, b.Ordered = gofastcore.KindHeading, false // a marker it has stays
	if level > 6 {
		if b.Attrs == nil {
			b.Attrs = map[string]string{}
		}
		b.Attrs[gofastcore.AttrLevel] = strconv.Itoa(level)
		level = 6
	}
	b.Level = uint8(max(level, 1))
}

// headingText reports whether text can be a heading: it has four letters
// or more, at most one tab (between a number and a title; a row of a
// table has more), and does not end as a sentence or a label does.
func headingText(text string) bool {
	letters := 0
	for _, r := range text {
		if unicode.IsLetter(r) {
			letters++
		}
	}
	if letters < 4 || strings.Count(text, "\t") > 1 {
		return false
	}
	r, _ := utf8.DecodeLastRuneInString(strings.TrimSpace(text))
	return !strings.ContainsRune(".,;:", r)
}

// startsHeading reports whether text starts as a heading in body type
// would: with a capital letter or a number.
func startsHeading(text string) bool {
	r, _ := utf8.DecodeRuneInString(text)
	return unicode.IsUpper(r) || unicode.IsDigit(r)
}
