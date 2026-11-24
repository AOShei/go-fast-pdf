package layout

import (
	"math"
	"sort"
	"unicode"
	"unicode/utf8"

	"github.com/AOShei/go-fast-pdf/internal/extract"
	"github.com/AOShei/go-fast-pdf/internal/font"
)

// Thresholds of the line step. Lengths are shares of the font size.
const (
	// baselineSlack is how far apart two baselines may be and still be
	// one line, for pieces the page draws at different times.
	baselineSlack = 0.15
	// rejoinGap is how far apart two such pieces may be along the line:
	// a wide space between words, not the gutter between two columns.
	rejoinGap = 0.6
	// rejoinOverlap is how far into each other they may reach.
	rejoinOverlap = 0.3
	// tabGap is the gap from which two pieces of a line are not words of
	// one sentence but entries set side by side: a label and its value,
	// the cells of a row. It is written as a tab. The widest space of a
	// justified line stays well below it.
	tabGap = 2.0
	// letterGapMax is the widest gap that can be the spacing of letters.
	letterGapMax = 1.0
)

// line is the text of one line: pieces that follow each other along a
// baseline.
type line struct {
	lo, hi int32 // its pieces are b.lidx[lo:hi], in reading order
	class  uint8 // the direction of writing, as of its pieces

	// In the frame of the group: from u0 to u1 along the line, the
	// baseline at v, from top to bot across it.
	u0, u1, v, top, bot float64
	x0, y0, x1, y1      float64 // the box
	// size, em and space are those of the piece with the most text.
	size, em, space float64
	key             int32 // the first run of the line, in the order drawn

	tlo, thi int32 // its text is b.text[tlo:thi]
	tab      bool  // the text has a tab
	// item is set when the line starts with the marker of a list item,
	// and gap when its one tab is the space after that marker: a tab
	// that does not make the line a row.
	item, gap bool
	// li is the list item of a tagged PDF that the line's first run is
	// in (Options.Item), 0 for none.
	li int64
	// bidi is set for a line with letters of a script written from right
	// to left, whose text is put together from its units (bidi.go); rtl
	// when it reads from right to left.
	bidi, rtl bool
	// numberFirst is set for such a line that has a number before its
	// first letter as it is read: the letter before it in its paragraph,
	// on the line before, can change how the number reads.
	numberFirst bool
	// before is the type of the letter before the line in its paragraph
	// that its text was read with (bidiText), 0 for none.
	before bidiClass
	// The fonts of the first and last piece, and of the piece with the
	// most text.
	first, last, main *font.Font
	// firstWord is the estimated width of the first word.
	firstWord float64

	pred  int32 // the line it continues, -1 for the first of a paragraph
	start bool  // set when the line starts a paragraph
	next  int32 // the line that follows in its paragraph, in b.lines; -1 for the last
}

// layoutGroup lays out the pieces of one group, which share their
// display reasons: lines, then paragraphs. The blocks are appended to
// b.blocks.
func (b *builder) layoutGroup(idx []int32) {
	first := len(b.lines)
	b.chains(idx)

	// Lines are worked on together with the lines written in the same
	// direction. Nearly always that is all of them.
	lines := b.lines[first:]
	mixed := false
	for i := range lines {
		mixed = mixed || lines[i].class != lines[0].class
	}
	if mixed {
		sort.SliceStable(lines, func(i, j int) bool { return lines[i].class < lines[j].class })
	}
	display := b.pieces[idx[0]].display
	for lo := first; lo < len(b.lines); {
		hi := lo + 1
		for hi < len(b.lines) && b.lines[hi].class == b.lines[lo].class {
			hi++
		}
		// rejoin can only make the lines fewer; the ones of the next
		// direction are moved up behind them.
		rest := append([]line(nil), b.lines[hi:]...)
		b.lines = b.lines[:hi]
		b.frame(lo)
		b.rejoin(lo)
		for i := lo; i < len(b.lines); i++ {
			b.lineText(&b.lines[i])
		}
		b.stats.Lines += len(b.lines) - lo
		// Tables found by alignment take their lines first, in the text
		// without a display reason that is in no cell of a tagged table (aligned.go).
		var taken []line
		var tables []int32
		if display == 0 && b.pieces[idx[0]].cell == 0 && !b.opts.NoTables {
			// Tables drawn with rules first (ruled.go), then those found
			// among the lines left.
			taken, tables = b.findRuledTables(lo)
			more, also := b.findTables(lo)
			for _, k := range also {
				for n := range b.blocks[k].tlines {
					b.blocks[k].tlines[n] += int32(len(taken))
				}
			}
			taken, tables = append(taken, more...), append(tables, also...)
		}
		b.paragraphs(lo, display)
		if len(taken) > 0 {
			// Their lines go after those of the paragraphs.
			base := int32(len(b.lines))
			b.lines = append(b.lines, taken...)
			for _, k := range tables {
				for n := range b.blocks[k].tlines {
					b.blocks[k].tlines[n] += base
				}
			}
		}
		lo = len(b.lines)
		b.lines = append(b.lines, rest...)
	}
}

// frame gives the pieces of the lines b.lines[first:], which are
// written in one direction, their places in a frame of their own: u
// along the direction of writing, v across it, growing towards the
// lines that follow. The lines are then measured in it.
func (b *builder) frame(first int) {
	lines := b.lines[first:]
	dx, dy := 1.0, 0.0
	if lines[0].class != 0 {
		var sx, sy float64
		for i := range lines {
			for k := lines[i].lo; k < lines[i].hi; k++ {
				p := &b.pieces[b.lidx[k]]
				w := hypot(p.ex-p.sx, p.ey-p.sy) + p.size
				sx, sy = sx+p.dx*w, sy+p.dy*w
			}
		}
		if l := hypot(sx, sy); l > 0 {
			dx, dy = sx/l, sy/l
		} else {
			p := &b.pieces[b.lidx[lines[0].lo]]
			dx, dy = p.dx, p.dy
		}
	}
	mx, my := -dy, dx
	for i := range lines {
		for k := lines[i].lo; k < lines[i].hi; k++ {
			p := &b.pieces[b.lidx[k]]
			a, e := p.sx*dx+p.sy*dy, p.ex*dx+p.ey*dy
			p.u0, p.u1 = min(a, e), max(a, e)
			p.v = p.sx*mx + p.sy*my
			side := p.nx*mx + p.ny*my // -1 for text that stands upright on its line
			t1, t2 := p.v+p.up*side, p.v+p.down*side
			p.top, p.bot = min(t1, t2), max(t1, t2)
		}
		b.measure(&lines[i])
	}
}

// chains makes a line of every stretch of pieces that the page draws
// one after the other on one line. What separates two pieces is decided
// as the interpreter decides it between two strings.
//
// A line has the direction of most of its text. Text set along a curve
// turns a little with every letter and is still one line.
func (b *builder) chains(idx []int32) {
	start := int32(len(b.lidx))
	var prev *piece
	for _, i := range idx {
		p := &b.pieces[i]
		p.sep, p.gap = 0, 0
		if prev != nil {
			sep, gap := extract.Separator(&b.c.Runs[prev.run], &b.c.Runs[p.run])
			if sep == '\n' {
				b.addLine(start, int32(len(b.lidx)))
				start = int32(len(b.lidx))
			} else {
				p.sep, p.gap = sep, float32(gap*b.scale)
			}
		}
		b.lidx = append(b.lidx, i)
		prev = p
	}
	b.addLine(start, int32(len(b.lidx)))
}

func (b *builder) addLine(lo, hi int32) {
	if lo == hi {
		return
	}
	ln := line{lo: lo, hi: hi, pred: -1}
	most := int32(-1)
	for k := lo; k < hi; k++ {
		p := &b.pieces[b.lidx[k]]
		if n := b.c.Runs[p.run].Len; n > most {
			most, ln.class = n, p.class
		}
	}
	b.lines = append(b.lines, ln)
}

// measure works out the extents of a line from its pieces.
func (b *builder) measure(ln *line) {
	first := &b.pieces[b.lidx[ln.lo]]
	ln.u0, ln.u1, ln.top, ln.bot = first.u0, first.u1, first.top, first.bot
	ln.x0, ln.y0, ln.x1, ln.y1 = first.x0, first.y0, first.x1, first.y1
	ln.key = first.run
	main, most := first, int32(-1)
	for k := ln.lo; k < ln.hi; k++ {
		p := &b.pieces[b.lidx[k]]
		ln.u0, ln.u1 = min(ln.u0, p.u0), max(ln.u1, p.u1)
		ln.top, ln.bot = min(ln.top, p.top), max(ln.bot, p.bot)
		ln.x0, ln.y0, ln.x1, ln.y1 = min(ln.x0, p.x0), min(ln.y0, p.y0), max(ln.x1, p.x1), max(ln.y1, p.y1)
		ln.key = min(ln.key, p.run)
		if n := b.c.Runs[p.run].Len; n > most {
			main, most = p, n
		}
	}
	r := &b.c.Runs[main.run]
	ln.v, ln.size, ln.em, ln.space = main.v, main.size, main.em, r.SpaceWidth*b.scale
	ln.main = r.Font
	ln.first = b.c.Runs[b.pieces[b.lidx[ln.lo]].run].Font
	ln.last = b.c.Runs[b.pieces[b.lidx[ln.hi-1]].run].Font
}

// rejoin puts together lines that lie on one baseline next to each
// other but are drawn at different times: a page that draws all its
// bold words after the rest, for one. Pieces joined this way are read
// from left to right along the line.
func (b *builder) rejoin(first int) {
	n := len(b.lines) - first
	if n < 2 {
		return
	}
	b.sorted = b.sorted[:0]
	for i := first; i < len(b.lines); i++ {
		b.sorted = append(b.sorted, int32(i))
	}
	sortStable(b.sorted, func(i, j int32) bool {
		a, c := &b.lines[i], &b.lines[j]
		if a.v != c.v {
			return a.v < c.v
		}
		return a.u0 < c.u0
	})
	// The rows: lines on one baseline, from left to right. Most pages
	// have one line to a row and nothing to do.
	var rows [][2]int
	shared := false
	for i := 0; i < n; {
		head := &b.lines[b.sorted[i]]
		j := i + 1
		for j < n && b.lines[b.sorted[j]].v-head.v <= baselineSlack*head.size {
			j++
		}
		shared = shared || j-i > 1
		i = j
	}
	if !shared {
		return
	}
	for i := 0; i < n; {
		head := &b.lines[b.sorted[i]]
		j := i + 1
		for j < n && b.lines[b.sorted[j]].v-head.v <= baselineSlack*head.size {
			j++
		}
		if j-i > 1 {
			row := b.sorted[i:j]
			sortStable(row, func(x, y int32) bool { return b.lines[x].u0 < b.lines[y].u0 })
		}
		rows = append(rows, [2]int{i, j})
		i = j
	}
	// Where the lines are before any of them is joined.
	type extent struct{ u0, u1, v float64 }
	was := make([]extent, n)
	for s, li := range b.sorted {
		was[s] = extent{b.lines[li].u0, b.lines[li].u1, b.lines[li].v}
	}
	// gutter reports whether the white space from lo to hi in row r is
	// there in the rows over it or in the rows under it as well, with
	// text on both sides: then it is the space between two columns, and
	// what stands on its two sides are two lines. The lines of two
	// columns need not share their baselines, so several rows are
	// looked at together.
	gutter := func(r int, lo, hi, size float64) bool {
		mid := (lo + hi) / 2
		at := was[rows[r][0]].v
		for _, step := range [2]int{-1, 1} {
			left, right, across := false, false, false
			for o := r + step; o >= 0 && o < len(rows) && abs(was[rows[o][0]].v-at) <= 2.5*size; o += step {
				for s := rows[o][0]; s < rows[o][1]; s++ {
					switch e := was[s]; {
					case e.u0 < mid && e.u1 > mid:
						across = true
					case e.u1 <= mid:
						left = true
					default:
						right = true
					}
				}
			}
			if left && right && !across {
				return true
			}
		}
		return false
	}

	joined := false
	for r, span := range rows {
		if span[1]-span[0] < 2 {
			continue
		}
		row := b.sorted[span[0]:span[1]]
		cur := &b.lines[row[0]]
		for _, k := range row[1:] {
			next := &b.lines[k]
			if b.adjoins(cur, next) && !(next.u0 > cur.u1 && gutter(r, cur.u1, next.u0, cur.size)) {
				b.merge(cur, next)
				joined = true
			} else {
				cur = next
			}
		}
	}
	if !joined {
		return
	}
	// Drop the lines that went into others.
	w := first
	for i := first; i < len(b.lines); i++ {
		if b.lines[i].lo != b.lines[i].hi {
			b.lines[w] = b.lines[i]
			w++
		}
	}
	b.lines = b.lines[:w]
}

// adjoins reports whether the line c, which starts at or after the
// start of a on the same baseline, belongs to the same line of text.
func (b *builder) adjoins(a, c *line) bool {
	if a.size <= 0 || c.size > 1.15*a.size || a.size > 1.15*c.size {
		return false
	}
	em := a.em
	if gap := c.u0 - a.u1; gap >= -rejoinOverlap*em {
		return gap <= rejoinGap*em
	}
	// c starts inside a: it belongs there if it fills a hole, that is,
	// if it does not lie over any piece of a.
	for k := a.lo; k < a.hi; k++ {
		p := &b.pieces[b.lidx[k]]
		if min(p.u1, c.u1)-max(p.u0, c.u0) > rejoinOverlap*em {
			return false
		}
	}
	return true
}

// merge moves the pieces of c into a, orders them along the line and
// decides again what separates them.
func (b *builder) merge(a, c *line) {
	lo := int32(len(b.lidx))
	b.lidx = append(b.lidx, b.lidx[a.lo:a.hi]...)
	b.lidx = append(b.lidx, b.lidx[c.lo:c.hi]...)
	hi := int32(len(b.lidx))
	list := b.lidx[lo:hi]
	sortStable(list, func(i, j int32) bool { return b.pieces[i].u0 < b.pieces[j].u0 })
	for k := range list {
		p := &b.pieces[list[k]]
		p.sep, p.gap = 0, 0
		if k > 0 {
			q := &b.pieces[list[k-1]]
			sep, gap := extract.Separator(&b.c.Runs[q.run], &b.c.Runs[p.run])
			// Two pieces drawn at different times are two pieces of
			// text, however close they stand: the cells of a row, a
			// word in bold among others.
			p.apart = q.run+1 != p.run
			if sep == '\n' || p.apart {
				sep = ' '
			}
			p.sep, p.gap = sep, float32(gap*b.scale)
		}
	}
	a.lo, a.hi = lo, hi
	c.lo, c.hi = 0, 0
	b.measure(a)
	b.stats.Rejoined++
}

// cand is a space that is not in the file but was put where two
// strings stand apart.
type cand struct {
	pos   int32   // of the space in b.text
	width float32 // of the gap, in ems
	class uint8
}

const (
	gapOther  uint8 = iota
	gapLetter       // between two single letters
	gapWord         // next to a word of several letters
)

// The kinds of what stands between two such spaces.
type frag uint8

const (
	fragLetters frag = iota // single letters, each by itself
	fragMarks               // single letters and punctuation marks, or marks alone
	fragDigits              // single characters with a digit or a symbol among them
	fragWord                // a word of several characters
)

// lineText writes the text of a line to b.text.
func (b *builder) lineText(ln *line) {
	ln.bidi, ln.rtl, ln.numberFirst, ln.before = false, false, false, 0
	if b.rightToLeftLine(ln) {
		b.bidiText(ln, 0, false)
		return
	}
	b.cands = b.cands[:0]
	start := len(b.text)
	gaps := b.c.Gaps
	for k := ln.lo; k < ln.hi; k++ {
		p := &b.pieces[b.lidx[k]]
		r := &b.c.Runs[p.run]
		t := b.c.Text[r.Off : r.Off+r.Len]
		skipped := 0
		if k > ln.lo && p.sep == ' ' {
			n := len(b.text)
			var width float32
			if p.em > 0 {
				width = p.gap / float32(p.em)
			}
			switch {
			case width >= tabGap:
				for n > start && b.text[n-1] == ' ' {
					n--
				}
				for len(t) > 0 && t[0] == ' ' {
					t = t[1:]
					skipped++
				}
				// The piece may be nothing but a space; the gap is there
				// all the same. A tab at the end of a line is dropped
				// when the lines are joined.
				if n > start && b.text[n-1] != '\t' {
					b.text = append(b.text[:n], '\t')
					ln.tab = true
					b.stats.Tabs++
				}
			case n > start && b.text[n-1] != ' ' && b.text[n-1] != '\t' && len(t) > 0 && t[0] != ' ':
				if p.apart {
					// Letters drawn at different times are not the
					// letters of one word set apart.
					width = 0
				}
				b.cands = append(b.cands, cand{pos: int32(n), width: width})
				b.text = append(b.text, ' ')
			}
		}
		base := len(b.text)
		b.text = append(b.text, t...)
		// A file can say that a glyph stands for a tab or a line break.
		// Within a line that is a space: tabs and line breaks in a
		// block's text are the ones this package writes.
		for i := base; i < len(b.text); i++ {
			if c := b.text[i]; c < 0x20 && (c == '\n' || c == '\t' || c == '\r' || c == '\f' || c == '\v') {
				b.text[i] = ' '
			}
		}
		if len(gaps) == 0 {
			continue
		}
		// The spaces put inside the run.
		g := sort.Search(len(gaps), func(i int) bool { return gaps[i].Off >= r.Off })
		for ; g < len(gaps) && gaps[g].Off < r.Off+r.Len; g++ {
			at := int(gaps[g].Off-r.Off) - skipped
			if at <= 0 || base+at >= len(b.text) {
				continue
			}
			var width float32
			if p.em > 0 {
				width = gaps[g].Width * float32(b.scale) / float32(p.em)
			}
			if b.text[base+at] != ' ' {
				continue
			}
			if width >= tabGap {
				b.text[base+at] = '\t'
				ln.tab = true
				b.stats.Tabs++
				continue
			}
			b.cands = append(b.cands, cand{pos: int32(base + at), width: width})
		}
	}
	if len(b.cands) >= 3 && b.closeLetters(start) {
		b.stats.LetterSpaced++
	}
	b.endLine(ln, start)
}

// bidiText writes the text of a line with letters of a script written
// from right to left to b.text, in the order it is read (bidi.go), with
// before the type of the last letter before it in its paragraph, 0 for
// none or not known. again is set when the line is written a second
// time, once its paragraph is known; it is not counted again.
func (b *builder) bidiText(ln *line, before bidiClass, again bool) {
	start := len(b.text)
	ln.bidi, ln.before = true, before
	ln.rtl, ln.numberFirst = b.bidiUnits(ln, before)
	if ln.rtl && !again {
		b.rtlLines++
	}
	// The fonts where the line meets the lines before and after it: of
	// its first and its last letters as they are read.
	first, last := -1, -1
	for i := range b.units {
		if u := &b.units[i]; u.run >= 0 && u.class != bidiWS {
			if first < 0 {
				first = i
			}
			last = i
		}
	}
	if first >= 0 {
		ln.first, ln.last = b.c.Runs[b.units[first].run].Font, b.c.Runs[b.units[last].run].Font
	}
	for i := range b.units {
		u := &b.units[i]
		n := len(b.text)
		b.text = b.unitText(b.text, u)
		if u.put == '\t' {
			ln.tab = true
			if !again {
				b.stats.Tabs++
			}
			continue
		}
		for k := n; k < len(b.text); k++ {
			if c := b.text[k]; c < 0x20 && (c == '\n' || c == '\t' || c == '\r' || c == '\f' || c == '\v') {
				b.text[k] = ' '
			}
		}
	}
	b.endLine(ln, start)
}

// endLine notes where the text of a line, written from start, is.
func (b *builder) endLine(ln *line, start int) {
	ln.tlo, ln.thi = int32(start), int32(len(b.text))

	// The width of the first word, taken as its share of the line's
	// characters.
	t := b.text[start:]
	i := 0
	for i < len(t) && (t[i] == ' ' || t[i] == '\t') {
		i++
	}
	j := i
	for j < len(t) && t[j] != ' ' && t[j] != '\t' {
		j++
	}
	if len(t) > 0 {
		ln.firstWord = (ln.u1 - ln.u0) * float64(j-i) / float64(len(t))
	}
}

// closeLetters removes the spaces put between the letters of text that
// is set with its letters apart, as headings are: "H E A D I N G" is
// the word HEADING. It reports whether it removed any.
//
// A gap between two strings is a space when it is wide enough, and that
// is judged against the font size. For letters set apart it is the
// wrong judgement, and only the rest of the line can show it: the gaps
// between single letters are all alike, and they are narrower than any
// gap next to a whole word. A line that is nothing but single letters
// evenly apart cannot be told from a row of separate letters; it is
// read as a word.
func (b *builder) closeLetters(start int) bool {
	cs := b.cands
	// What stands between the spaces.
	b.frags = b.frags[:0]
	from := start
	for i := range cs {
		b.frags = append(b.frags, fragKind(b.text[from:cs[i].pos]))
		from = int(cs[i].pos) + 1
	}
	b.frags = append(b.frags, fragKind(b.text[from:]))

	narrowest := float32(math.Inf(1)) // of the gaps next to a word
	letters := 0
	for i := range cs {
		c := &cs[i]
		left, right := b.frags[i], b.frags[i+1]
		switch {
		case c.width <= 0:
			c.class = gapOther
		case left == fragWord || right == fragWord:
			c.class = gapWord
			narrowest = min(narrowest, c.width)
		case left <= fragMarks && right <= fragMarks && (left == fragLetters || right == fragLetters):
			// Between two letters, or a letter and the mark that
			// follows it. Marks in a row are not letters set apart:
			// the dots that lead to a page number stay as they are.
			c.class = gapLetter
			letters++
		default:
			c.class = gapOther
		}
	}
	if letters < 3 {
		return false
	}
	// The gaps between letters that are narrower than every gap next to
	// a word, and their middle value.
	var widths []float32
	for i := range cs {
		if c := &cs[i]; c.class == gapLetter && c.width < 0.8*narrowest && c.width <= letterGapMax {
			widths = append(widths, c.width)
		}
	}
	if len(widths) < 3 {
		return false
	}
	sort.Slice(widths, func(i, j int) bool { return widths[i] < widths[j] })
	limit := 1.3*widths[len(widths)/2] + 0.05
	n := 0
	for i := range cs {
		c := &cs[i]
		if c.class == gapLetter && c.width < 0.8*narrowest && c.width <= limit {
			n++
		} else {
			c.class = gapOther
		}
	}
	if n < 3 {
		return false
	}
	// Take the spaces out.
	w, next := int(cs[0].pos), 0
	for i := int(cs[0].pos); i < len(b.text); i++ {
		for next < len(cs) && int(cs[next].pos) < i {
			next++
		}
		if next < len(cs) && int(cs[next].pos) == i && cs[next].class == gapLetter {
			continue
		}
		b.text[w] = b.text[i]
		w++
	}
	b.text = b.text[:w]
	return true
}

// fragKind says what a piece of text between two spaces is.
func fragKind(t []byte) frag {
	kind := fragLetters
	n := 0 // characters of the current word
	for i := 0; i < len(t); {
		c := t[i]
		if c == ' ' || c == '\t' {
			n = 0
			i++
			continue
		}
		r, size := rune(c), 1
		if c >= utf8.RuneSelf {
			r, size = utf8.DecodeRune(t[i:])
		}
		i += size
		if unicode.Is(unicode.Mn, r) {
			continue // a mark on the letter before it
		}
		if n++; n > 1 {
			return fragWord
		}
		switch {
		case unicode.IsLetter(r):
		case unicode.IsPunct(r):
			kind = max(kind, fragMarks)
		default:
			kind = fragDigits
		}
	}
	return kind
}
