package layout

import (
	"unicode"
	"unicode/utf8"

	gofastcore "github.com/AOShei/go-fast-core"
)

// Thresholds of the paragraph step. Lengths are shares of the font size
// unless said otherwise.
const (
	// sameSize is how much two lines of one paragraph may differ in font
	// size.
	sameSize = 0.12
	// leadingDefault is the distance between the baselines of a
	// paragraph, as a multiple of the font size, on a page that has too
	// few lines to show its own.
	leadingDefault = 1.2
	// leadingSlack is how much wider than the page's usual distance two
	// baselines may be apart within a paragraph. The space between two
	// paragraphs is more.
	leadingSlack = 0.25
	// leadingMax bounds the distances looked at: beyond it two lines are
	// never one paragraph.
	leadingMax = 2.8
	// edgeSlack is how far two lines' edges may differ and be aligned.
	edgeSlack = 0.35
	// indentMax is the widest indent of a first line, or of the lines
	// after it.
	indentMax = 6.0
	// lookBack bounds the lines above a line that are looked at for the
	// one it continues.
	lookBack = 64
)

// blk is one block: a paragraph.
type blk struct {
	x0, y0, x1, y1 float64
	display        gofastcore.Display
	key            int32 // its first run in the order drawn
	size           float64
	text           string
	line           int32 // its first line, in b.lines; the others follow by line.next
	// cell is one more than the cell of a table that the block is the
	// text of, and table one more than the table a block of kind table
	// is; members are the blocks of its cells, in order of cell.
	cell, table int32
	members     []int32
}

// paragraphs joins the lines b.lines[first:] into paragraphs and
// appends a block for each.
//
// A line continues the nearest line above it when the two could be
// consecutive lines of one paragraph: the same size, no further apart
// than lines are on this page, the same font where they meet, edges
// that fit one of the ways paragraphs are set, and no room at the end
// of the upper line for the first word of the lower one.
func (b *builder) paragraphs(first int, display gofastcore.Display) {
	lines := b.lines[first:]
	n := len(lines)
	if n == 0 {
		return
	}
	// From top to bottom.
	b.sorted = b.sorted[:0]
	for i := range lines {
		b.sorted = append(b.sorted, int32(i))
	}
	sortStable(b.sorted, func(i, j int32) bool {
		a, c := &lines[i], &lines[j]
		if a.v != c.v {
			return a.v < c.v
		}
		return a.u0 < c.u0
	})
	order := b.sorted

	// For every line, the nearest line above it that it lies under.
	b.pred = append(b.pred[:0], make([]int32, n)...)
	b.claims = append(b.claims[:0], make([]int32, n)...)
	for i := range b.pred {
		b.pred[i] = -1
	}
	for oi, li := range order {
		ln := &lines[li]
		// between is the nearest line above ln that ln does not lie
		// under: the short last line of a paragraph, when ln is the
		// indented first line of the next one.
		between := int32(-1)
		for back := 1; back <= lookBack && oi-back >= 0; back++ {
			at := order[oi-back]
			up := &lines[at]
			d := ln.v - up.v
			if d > leadingMax*max(ln.size, up.size) {
				break
			}
			if d < 0.5*min(ln.size, up.size) {
				continue // on the same line, further along
			}
			if !under(ln, up) {
				if between < 0 {
					between = at
				}
				continue
			}
			// ln lies under up. If the line between them lies under up
			// as well, ln follows that line, not up.
			if between >= 0 && under(&lines[between], up) {
				at = between
			}
			b.pred[li] = at
			b.claims[at]++
			break
		}
	}

	// The usual distance between baselines on this page, as a multiple
	// of the font size.
	leading := b.usualLeading(lines)

	// under[j] is the one line that lies under line j, -1 if there is
	// none or more than one.
	b.sortB = append(b.sortB[:0], make([]int32, n)...)
	under := b.sortB
	for i := range under {
		under[i] = -1
	}
	for li, p := range b.pred {
		if p >= 0 && b.claims[p] == 1 {
			under[p] = int32(li)
		}
	}

	// Which lines start a list item.
	for li := range lines {
		ln := &lines[li]
		raw := b.text[ln.tlo:ln.thi]
		ln.item, ln.gap = false, false
		if !mayStartItem(raw) {
			continue
		}
		_, _, _, _, ok := splitMarker(raw)
		// A tab after the marker would make it a row: what tab the line
		// has is the space after its marker.
		ln.item, ln.gap = ok, ok && ln.tab
	}

	// Decide, from top to bottom, which of these pairs are one
	// paragraph. Two lines under one line are two columns under a
	// heading: neither continues it. A line that starts a list item
	// starts a paragraph.
	for _, li := range order {
		ln := &lines[li]
		ln.pred, ln.start = -1, true
		p := b.pred[li]
		if p < 0 || b.claims[p] > 1 {
			continue
		}
		up := &lines[p]
		var before *line
		if up.pred >= 0 {
			before = &lines[up.pred]
		}
		// How far to the right the lines of this column reach: the two
		// lines, the lines of the paragraph over them, and the lines
		// that follow under them from the same left edge.
		right := max(up.u1, ln.u1)
		for k, at := 0, up.pred; k < 2 && at >= 0; k, at = k+1, lines[at].pred {
			right = max(right, lines[at].u1)
		}
		last := ln
		for k, at := 0, under[li]; k < 2 && at >= 0; k, at = k+1, under[at] {
			next := &lines[at]
			size := max(last.size, next.size)
			if abs(next.u0-ln.u0) > edgeSlack*size || min(last.size, next.size) < (1-sameSize)*size || (next.v-last.v)/size > leading+leadingSlack {
				break
			}
			right = max(right, next.u1)
			last = next
		}
		if !ln.item && continues(rowTab(up), rowTab(ln), before, leading, right) {
			ln.pred, ln.start = p, false
		}
	}

	// A block for each paragraph, its lines from top to bottom.
	b.claims = append(b.claims[:0], make([]int32, n)...) // now: the line that follows
	for i := range b.claims {
		b.claims[i] = -1
	}
	for _, li := range order {
		if p := lines[li].pred; p >= 0 {
			b.claims[p] = li
		}
	}
	for _, li := range order {
		if !lines[li].start {
			continue
		}
		bl := blk{display: display, key: lines[li].key, size: lines[li].size, line: int32(first) + li}
		bl.x0, bl.y0, bl.x1, bl.y1 = lines[li].x0, lines[li].y0, lines[li].x1, lines[li].y1
		b.out = b.out[:0]
		var prev *line
		for at := li; at >= 0; at = b.claims[at] {
			ln := &lines[at]
			bl.x0, bl.y0, bl.x1, bl.y1 = min(bl.x0, ln.x0), min(bl.y0, ln.y0), max(bl.x1, ln.x1), max(bl.y1, ln.y1)
			bl.key = min(bl.key, ln.key)
			b.joinLine(prev, ln)
			prev = ln
			if ln.next = -1; b.claims[at] >= 0 {
				ln.next = int32(first) + b.claims[at]
			}
		}
		if bl.text = b.clean(b.out); bl.text != "" {
			b.blocks = append(b.blocks, bl)
		}
	}
}

// rowTab returns a line as the rule on rows sees it: the gap after the
// marker of a list item is no tab of a row.
func rowTab(ln *line) *line {
	if !ln.gap {
		return ln
	}
	c := *ln
	c.tab = false
	return &c
}

// under reports whether the line ln, which is below up, lies under it:
// the two share a good part of their extent along the line.
func under(ln, up *line) bool {
	over := min(ln.u1, up.u1) - max(ln.u0, up.u0)
	return over > 0.5*min(ln.u1-ln.u0, up.u1-up.u0) || over > ln.size
}

// usualLeading returns the most common distance between the baselines
// of two lines that stand one under the other, as a multiple of their
// font size.
func (b *builder) usualLeading(lines []line) float64 {
	const step = 0.05
	b.hist = append(b.hist[:0], make([]int32, int(leadingMax/step)+2)...)
	pairs := 0
	for li := range lines {
		p := b.pred[li]
		if p < 0 || b.claims[p] > 1 {
			continue
		}
		ln, up := &lines[li], &lines[p]
		size := max(ln.size, up.size)
		if size <= 0 || ln.size < (1-sameSize)*size || up.size < (1-sameSize)*size {
			continue
		}
		if r := (ln.v - up.v) / size; r > 0.5 && r < leadingMax {
			b.hist[int(r/step)]++
			pairs++
		}
	}
	if pairs < 3 {
		return leadingDefault
	}
	// The most common value, counting the two neighbouring steps with
	// each step so that a value on a boundary is not split.
	best, at := int32(0), 0
	for i := 1; i+1 < len(b.hist); i++ {
		if c := b.hist[i-1] + b.hist[i] + b.hist[i+1]; c > best {
			best, at = c, i
		}
	}
	if best < 2 {
		return leadingDefault
	}
	return (float64(at) + 0.5) * step
}

// continues reports whether ln, which lies under up, continues its
// paragraph. before is the line that up continues, or nil when up is
// the first line of its paragraph; right is the right edge of the
// column the two lines are in.
func continues(up, ln, before *line, leading, right float64) bool {
	size := max(up.size, ln.size)
	if size <= 0 || min(up.size, ln.size) < (1-sameSize)*size {
		return false
	}
	// No further apart than the lines of this page are.
	if d := (ln.v - up.v) / size; d > leading+leadingSlack {
		return false
	}
	// A change of font from one line to the next, where they meet and
	// for most of both, is a heading and what follows it.
	if up.last != ln.first && up.main != ln.main {
		return false
	}
	// A line with entries side by side is a row. Rows follow each other
	// but are not sentences that run on.
	if up.tab != ln.tab {
		return false
	}

	slack := edgeSlack * size
	left := ln.u0 - up.u0
	centred := abs((ln.u0+ln.u1)/2-(up.u0+up.u1)/2) <= slack && abs(left) > slack
	switch {
	case abs(left) <= slack:
		// Set flush left.
	case centred:
		// Lines centred on one axis.
		return true
	case abs(ln.u1-up.u1) <= slack && before == nil && left > 0:
		// Flush right, two lines: ambiguous with a hanging indent, and
		// read as one paragraph either way.
	case before == nil && abs(left) <= indentMax*size:
		// up is the first line: it is indented and ln is not, or it
		// hangs out and ln is indented.
	case abs(ln.u1-up.u1) <= slack:
		// Set flush right.
	default:
		return false
	}
	if up.tab {
		return true
	}
	// Room at the end of the upper line for the first word of the
	// lower: the line was ended on purpose, so a paragraph ends there.
	// right is how far the lines of this column reach.
	return right-up.u1 <= ln.firstWord+ln.space+0.5*size
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

// joinLine appends the text of a line to b.out, after the line prev of
// the same paragraph.
func (b *builder) joinLine(prev, ln *line) {
	t := b.text[ln.tlo:ln.thi]
	for len(t) > 0 && (t[0] == ' ' || t[0] == '\t') {
		t = t[1:]
	}
	for len(t) > 0 && (t[len(t)-1] == ' ' || t[len(t)-1] == '\t') {
		t = t[:len(t)-1]
	}
	if len(t) == 0 {
		return
	}
	out := b.out
	switch n := len(out); {
	case n == 0:
	case prev != nil && (prev.tab && !prev.gap || ln.tab && !ln.gap):
		// Rows stay rows.
		out = append(out, '\n')
	case endsWithSoftHyphen(out) && startsWithLetter(t):
		// A soft hyphen between the letters of a word is there only
		// because the line ends: it goes, and the word is whole again.
		// Anywhere else it stays, as any character does.
		out = out[:n-2]
	case endsWithHyphen(out) && startsWithLetter(t):
		// A hyphen at the end of a line may belong to the word or to
		// the line break. It stays, and the word is not broken by a
		// space: "well-" and "known" are "well-known".
	default:
		out = append(out, ' ')
	}
	b.out = append(out, t...)
}

// endsWithHyphen reports whether text ends with a letter or digit
// followed by a hyphen.
func endsWithHyphen(t []byte) bool {
	n := len(t)
	switch {
	case n >= 1 && t[n-1] == '-':
		n--
	case n >= 3 && t[n-3] == 0xE2 && t[n-2] == 0x80 && t[n-1] == 0x90: // U+2010
		n -= 3
	default:
		return false
	}
	r, _ := utf8.DecodeLastRune(t[:n])
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}

// endsWithSoftHyphen reports whether text ends with a letter or digit
// followed by a soft hyphen (U+00AD).
func endsWithSoftHyphen(t []byte) bool {
	n := len(t)
	if n < 3 || t[n-2] != 0xC2 || t[n-1] != 0xAD {
		return false
	}
	r, _ := utf8.DecodeLastRune(t[:n-2])
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}

func startsWithLetter(t []byte) bool {
	r, _ := utf8.DecodeRune(t)
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}

// clean makes the text of a block out of its bytes: control characters
// are replaced, the ends trimmed. It also notes whether the text has
// characters of a script written from right to left.
//
// A file can say that a glyph stands for any character at all. White
// space of any kind is a space here; a line break in a block's text
// means that a line was ended on purpose, and that is said only by the
// layout. Any other control character has no place in text and becomes
// U+FFFD.
func (b *builder) clean(t []byte) string {
	plain := true
	for _, c := range t {
		if c < 0x20 && c != '\n' && c != '\t' || c == 0x7F || c >= 0xC2 {
			plain = false
			break
		}
	}
	if !plain {
		w := 0
		for i := 0; i < len(t); {
			c := t[i]
			if c < utf8.RuneSelf {
				i++
				switch {
				case c == '\r' || c == '\f' || c == '\v':
					t[w] = ' '
					w++
				case c < 0x20 && c != '\n' && c != '\t' || c == 0x7F:
					// The replacement is longer than what it replaces;
					// the cases are rare enough to take the slow way.
					return b.cleanSlow(t)
				default:
					t[w] = c
					w++
				}
				continue
			}
			r, size := utf8.DecodeRune(t[i:])
			if r >= 0x80 && r <= 0x9F {
				return b.cleanSlow(t)
			}
			if !b.rtl && rightToLeft(r) {
				b.rtl = true
			}
			w += copy(t[w:], t[i:i+size])
			i += size
		}
		t = t[:w]
	}
	return string(trimSpace(t))
}

func (b *builder) cleanSlow(t []byte) string {
	out := make([]byte, 0, len(t)+16)
	for _, r := range string(t) {
		switch {
		case r == '\n' || r == '\t':
			out = append(out, byte(r))
		case r == '\r' || r == '\f' || r == '\v' || r == 0x85:
			out = append(out, ' ')
		case r < 0x20 || r >= 0x7F && r <= 0x9F:
			out = append(out, "\xef\xbf\xbd"...)
			b.controls++
		default:
			if !b.rtl && rightToLeft(r) {
				b.rtl = true
			}
			out = utf8.AppendRune(out, r)
		}
	}
	return string(trimSpace(out))
}

// trimSpace removes white space from both ends, as the shared model
// defines it: every character unicode.IsSpace reports.
func trimSpace(t []byte) []byte {
	for len(t) > 0 {
		r, size := utf8.DecodeRune(t)
		if !unicode.IsSpace(r) {
			break
		}
		t = t[size:]
	}
	for len(t) > 0 {
		r, size := utf8.DecodeLastRune(t)
		if !unicode.IsSpace(r) {
			break
		}
		t = t[:len(t)-size]
	}
	return t
}

// rightToLeft reports whether r belongs to a script written from right
// to left: Hebrew, Arabic, Syriac, Thaana, N'Ko and their presentation
// forms.
func rightToLeft(r rune) bool {
	return r >= 0x0590 && r <= 0x08FF || r >= 0xFB1D && r <= 0xFDFF || r >= 0xFE70 && r <= 0xFEFC ||
		r >= 0x10800 && r <= 0x10FFF || r >= 0x1E800 && r <= 0x1EFFF
}
