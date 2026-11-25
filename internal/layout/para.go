package layout

import (
	"strings"
	"unicode"
	"unicode/utf8"

	gofastcore "github.com/AOShei/go-fast-core"
	"github.com/AOShei/go-fast-pdf/internal/font"
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
	// grid is the table that a block of kind table found by alignment
	// is, and tlines its lines, in b.lines.
	grid   *gofastcore.Table
	tlines []int32
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
		ln.item, ln.gap, ln.li = false, false, 0
		if b.opts.Item != nil && ln.hi > ln.lo {
			ln.li, _ = b.opts.Item(b.c.Runs[b.pieces[b.lidx[ln.lo]].run].MCID)
		}
		if !mayStartItem(raw) {
			continue
		}
		_, _, _, _, ok := splitMarker(raw, false)
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
		// Lines read from right to left start at the right and end at
		// the left: they are looked at as in a mirror. So is a line that
		// reads from left to right by itself but ends at the left.
		endsLeft := up.rtl && !ln.rtl && b.endsAtLeft(ln)
		mirrored := up.rtl && (ln.rtl || endsLeft)
		upEnd, lnStart, lnEnd := up.u1, ln.u0, ln.u1
		if mirrored {
			upEnd, lnStart, lnEnd = -up.u0, -ln.u1, -ln.u0
		}
		// How far to the right the lines of this column reach: the two
		// lines, the lines of the paragraph over them, and the lines
		// that follow under them from the same left edge.
		right := max(upEnd, lnEnd)
		for k, at := 0, up.pred; k < 2 && at >= 0; k, at = k+1, lines[at].pred {
			e := lines[at].u1
			if mirrored {
				e = -lines[at].u0
			}
			right = max(right, e)
		}
		last := ln
		for k, at := 0, under[li]; k < 2 && at >= 0; k, at = k+1, under[at] {
			next := &lines[at]
			size := max(last.size, next.size)
			nextStart, nextEnd := next.u0, next.u1
			if mirrored {
				nextStart, nextEnd = -next.u1, -next.u0
			}
			if abs(nextStart-lnStart) > edgeSlack*size || min(last.size, next.size) < (1-sameSize)*size || (next.v-last.v)/size > leading+leadingSlack {
				break
			}
			right = max(right, nextEnd)
			last = next
		}
		upC, lnC := rowTab(up), rowTab(ln)
		// The Latin words of text written from right to left are often in
		// another font, for want of those letters in the first. They do
		// not set a line apart whose other letters are in the font above,
		// nor one that ends at the left.
		sameFont := endsLeft || up.rtl && ln.bidi && b.rtlInFont(ln, up.main)
		var upM, lnM line
		if mirrored || sameFont {
			upM, lnM = *upC, *lnC
			upC, lnC = &upM, &lnM
		}
		if sameFont {
			lnM.first, lnM.main = up.last, up.main
		}
		if mirrored {
			upM.u0, upM.u1 = -up.u1, -up.u0
			lnM.u0, lnM.u1 = lnStart, lnEnd
		}
		// Lines of two items of a tagged list, or of an item and what is
		// not in one, are two paragraphs. So are an entry of a table of
		// contents and what follows it; rows stay together as rows.
		if !ln.item && ln.li == up.li && continues(upC, lnC, before, leading, right) &&
			(upC.tab || !EndsEntry(b.text[up.tlo:up.thi])) {
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
		b.paragraphDirection(lines, li)
		var prev *line
		for at := li; at >= 0; at = b.claims[at] {
			ln := &lines[at]
			bl.x0, bl.y0, bl.x1, bl.y1 = min(bl.x0, ln.x0), min(bl.y0, ln.y0), max(bl.x1, ln.x1), max(bl.y1, ln.y1)
			bl.key = min(bl.key, ln.key)
			if ln.numberFirst {
				b.numberAfterLine(lines, ln)
			}
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

// endsAtLeft reports whether a line that reads from left to right by
// itself has a mark that ends a sentence, or a part of one, at its left
// end, next to a letter written from left to right: the last Latin words
// of a sentence written from right to left, which end there.
func (b *builder) endsAtLeft(ln *line) bool {
	// The text furthest left: the pieces may not be in that order.
	var left []byte
	u0 := 0.0
	for k := ln.lo; k < ln.hi; k++ {
		p := &b.pieces[b.lidx[k]]
		if left != nil && p.u0 >= u0 {
			continue
		}
		r := &b.c.Runs[p.run]
		if t := trimSpace(b.c.Text[r.Off : r.Off+r.Len]); len(t) > 0 {
			left, u0 = t, p.u0
		}
	}
	r, n := utf8.DecodeRune(left)
	if !sentenceMark(r) {
		return false
	}
	if n < len(left) {
		r, _ := utf8.DecodeRune(left[n:])
		return classOf(r) == bidiL
	}
	// The mark is drawn by itself.
	return ln.bidi || lastLetter(b.text[ln.tlo:ln.thi]) == bidiL
}

// rtlInFont reports whether the letters of a line that are written from
// right to left are all in font f.
func (b *builder) rtlInFont(ln *line, f *font.Font) bool {
	some := false
	for k := ln.lo; k < ln.hi; k++ {
		r := &b.c.Runs[b.pieces[b.lidx[k]].run]
		if r.RTL && !sameFace(r.Font, f) {
			return false
		}
		some = some || r.RTL
	}
	return some
}

// paragraphDirection writes again the lines of the paragraph that starts
// with line li that read in the other direction than most of its text,
// in the direction of the paragraph. The Unicode bidirectional algorithm
// gives a paragraph one direction; a line by itself, of Latin words for
// the most part or of nothing else, can seem to have the other. claims
// has the line that follows each.
func (b *builder) paragraphDirection(lines []line, li int32) {
	nr, nl := int32(0), int32(0)
	for at := li; at >= 0; at = b.claims[at] {
		if ln := &lines[at]; ln.rtl {
			nr += ln.thi - ln.tlo
		} else {
			nl += ln.thi - ln.tlo
		}
	}
	if nr == 0 || nl == 0 || nr == nl {
		return
	}
	dir := bidiL
	if nr > nl {
		dir = bidiR
	}
	// A line is turned that starts where a line next to it does, from the
	// side that the paragraph is read from.
	flush := func(ln *line, at int32) bool {
		if at < 0 {
			return false
		}
		d := ln.u0 - lines[at].u0
		if dir == bidiR {
			d = ln.u1 - lines[at].u1
		}
		return abs(d) <= edgeSlack*max(ln.size, lines[at].size)
	}
	for at := li; at >= 0; at = b.claims[at] {
		ln := &lines[at]
		if ln.rtl != (dir == bidiR) && (flush(ln, ln.pred) || flush(ln, b.claims[at])) && b.mayTurn(ln) {
			ln.dir = dir
			b.bidiText(ln, 0, true)
		}
	}
}

// mayTurn reports whether a line may be read in the other direction than
// its own, as one of a paragraph of that direction. Not one that ends
// with the mark that ends a sentence, or a part of one: the other way
// round it would begin with it, so it was not set in that direction. And
// not one without letters written from right to left that reads the same
// either way, beginning and ending with a letter, or that is a row of
// cells.
func (b *builder) mayTurn(ln *line) bool {
	t := trimSpace(b.text[ln.tlo:ln.thi])
	if len(t) == 0 {
		return false
	}
	first, _ := utf8.DecodeRune(t)
	last, _ := utf8.DecodeLastRune(t)
	if sentenceMark(last) && (ln.rtl || !b.endsAtLeft(ln)) {
		return false
	}
	return ln.bidi || !ln.tab && (classOf(first) != bidiL || classOf(last) != bidiL)
}

// sentenceMark reports whether r ends a sentence, or a part of one.
func sentenceMark(r rune) bool {
	switch r {
	case '.', ',', ':', ';', '!', '?', 0x060C, 0x061B, 0x061F: // and the Arabic comma, semicolon and question mark
		return true
	}
	return false
}

// numberAfterLine writes again the text of line ln of a paragraph, a
// line with letters written from right to left and a number before its
// first letter, when the letter before it in the paragraph changes how
// the number reads: an Arabic letter makes it an Arabic number, a Latin
// letter a Latin one, where the line by itself took the type of its
// direction. The Unicode bidirectional algorithm resolves a paragraph,
// and the line is read as a part of one. lines are those of the group,
// to which pred refers.
func (b *builder) numberAfterLine(lines []line, ln *line) {
	var before bidiClass
	for at := ln.pred; at >= 0 && before == 0; at = lines[at].pred {
		before = lastLetter(b.text[lines[at].tlo:lines[at].thi])
	}
	if numberReadsOtherwise(ln, before) {
		b.bidiText(ln, before, true)
	}
}

// lastLetter returns the type of the last letter of t: bidiL, bidiR or
// bidiAL, 0 for none.
func lastLetter(t []byte) bidiClass {
	for len(t) > 0 {
		r, n := utf8.DecodeLastRune(t)
		t = t[:len(t)-n]
		if c := classOf(r); c == bidiL || c == bidiR || c == bidiAL {
			return c
		}
	}
	return 0
}

// numberReadsOtherwise reports whether the number at the start of line
// ln reads otherwise after a letter of type before than as the line
// read it by itself, after the type of its own direction.
func numberReadsOtherwise(ln *line, before bidiClass) bool {
	took := bidiL
	if ln.rtl {
		took = bidiR
	}
	return ln.numberFirst && before != 0 && before != took
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
	// A change of typeface from one line to the next, where they meet and
	// for most of both, is a heading and what follows it.
	if !sameFace(up.last, ln.first) && !sameFace(up.main, ln.main) {
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

// sameFace reports whether two fonts are of one typeface: the same font,
// or two with one name but for the tag of a subset. A file can embed a
// typeface several times, a subset for each part it was put together
// from, and the text passes from one to another inside a sentence.
func sameFace(f, g *font.Font) bool {
	if f == g {
		return true
	}
	if f == nil || g == nil || f.Bold != g.Bold || f.Italic != g.Italic {
		return false
	}
	name := untagged(f.BaseFont)
	return name != "" && name == untagged(g.BaseFont)
}

// EndsEntry reports whether text ends as an entry of a table of contents
// does: in a leader, with more points than an ellipsis has, and a page
// label. A label is a short word that ends in a digit or in a digit and a
// letter, or a Roman numeral.
func EndsEntry[T string | []byte](t T) bool {
	const longest = 8 // of a label, in characters: "Annex-12"
	i := len(t)
	for i > 0 && t[i-1] == ' ' {
		i--
	}
	if i < len("....1") {
		return false
	}
	// Two characters tell nearly every line that does not end in a label.
	roman := false
	switch c, d := t[i-1], t[i-2]; {
	case isDigit(c):
	case isRoman(c):
		roman = !isDigit(d)
		if roman && !isRoman(d) && d != ' ' && d != '.' && d < utf8.RuneSelf {
			return false
		}
	case c < utf8.RuneSelf:
		if c |= 0x20; c < 'a' || c > 'z' || !isDigit(d) {
			return false
		}
	default: // a digit of another script
		if r, _ := lastRune(t, i); !unicode.IsDigit(r) {
			return false
		}
	}
	n, end := 0, i
	for i > 0 {
		r, at := lastRune(t, i)
		if leaderPoints(r) > 0 || unicode.IsSpace(r) {
			break
		}
		if n++; n > longest || roman && (r >= utf8.RuneSelf || !isRoman(byte(r))) {
			return false
		}
		i = at
	}
	if roman && !isNumeral(string(t[i:end])) {
		return false // a word of those letters: "il", "civil"
	}
	points := 0
	for i > 0 && points <= 3 {
		r, at := lastRune(t, i)
		p := leaderPoints(r)
		if p == 0 && !unicode.IsSpace(r) {
			return false
		}
		points += p
		i = at
	}
	return points > 3
}

// lastRune returns the last character of the first i bytes of t, and
// where it starts.
func lastRune[T string | []byte](t T, i int) (r rune, at int) {
	if c := t[i-1]; c < utf8.RuneSelf {
		return rune(c), i - 1
	}
	r, n := utf8.DecodeLastRuneInString(string(t[max(0, i-utf8.UTFMax):i]))
	return r, i - n
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

// isRoman reports whether c is a letter of a Roman numeral, of those that
// number the first pages of a book.
func isRoman(c byte) bool {
	switch c | 0x20 {
	case 'i', 'v', 'x', 'l', 'c':
		return true
	}
	return false
}

// isNumeral reports whether s, of the letters of isRoman, is a Roman
// numeral.
func isNumeral(s string) bool {
	for _, part := range [...]string{"c", "c", "c", "xc", "xl", "l", "x", "x", "x", "ix", "iv", "v", "i", "i", "i"} {
		if len(s) >= len(part) && strings.EqualFold(s[:len(part)], part) {
			s = s[len(part):]
		}
	}
	return s == ""
}

// leaderPoints returns how many points of a leader r is.
func leaderPoints(r rune) int {
	switch r {
	case '.', 0xB7, 0x2024: // and the middle dot, the one dot leader
		return 1
	case 0x2025: // the two dot leader
		return 2
	case 0x2026: // the horizontal ellipsis
		return 3
	}
	return 0
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
