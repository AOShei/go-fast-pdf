package layout

import (
	"slices"
	"sort"
	"unicode"
	"unicode/utf8"

	gofastcore "github.com/AOShei/go-fast-core"
)

// Text written from right to left, in the order it is read.
//
// A PDF places glyphs; it does not say in which order they are read.
// Programs draw a line of Hebrew or Arabic in more than one way: from
// left to right as it is seen (each word's letters backwards), word by
// word in the order of reading (each word drawn from left to right), or
// glyph by glyph from right to left. So a line with letters of a script
// written from right to left is not read in the order it is drawn: it is
// put together from where its glyphs stand, from left to right, and the
// Unicode bidirectional algorithm (UAX #9) then gives the order of
// reading. Each glyph is a unit: a letter with the marks set on it, or a
// glyph that stands for more than one letter (a ligature of lam and
// alef), whose letters are already in their order. Text that a file
// gives for its glyphs (/ActualText) is in the order of reading, and is
// laid out from right to left.
//
// The algorithm is applied to one line at a time, without the explicit
// embeddings and isolates that a file cannot give: the direction of the
// line is that of the letters at its ends, or of most of its words,
// until its paragraph is known, and then that of most of the paragraph
// (paragraphDirection); numbers, and Latin words, read from left to right
// within it; spaces and punctuation take the direction of what surrounds
// them. A bracket in a stretch read from right to left is the other
// bracket, as a reader sees it (L4).
//
// Pieces are placed within their run by their share of its text; the
// spaces between runs are found from the gaps between them, as for any
// line, a gap of a tab and more being a tab.

// bidiClass is the bidirectional type of a character, of those the
// algorithm needs here.
type bidiClass uint8

const (
	bidiON  bidiClass = iota // other neutral
	bidiL                    // left to right
	bidiR                    // right to left: Hebrew and the others
	bidiAL                   // right to left, Arabic
	bidiEN                   // European number
	bidiAN                   // Arabic number
	bidiES                   // number separator: plus and minus
	bidiET                   // number terminator: percent, currency, degree
	bidiCS                   // common separator: comma, full stop, colon, slash
	bidiNSM                  // nonspacing mark
	bidiWS                   // white space
)

// classOf returns the bidirectional type of r.
func classOf(r rune) bidiClass {
	switch {
	case r == ' ' || r == '\t' || r == 0xA0:
		if r == 0xA0 {
			return bidiCS
		}
		return bidiWS
	case r >= '0' && r <= '9', r >= 0x06F0 && r <= 0x06F9:
		return bidiEN
	case r >= 0x0660 && r <= 0x0669, r == 0x066B || r == 0x066C:
		return bidiAN
	case r == '+' || r == '-' || r == 0x2212:
		return bidiES
	case r == '#' || r == '$' || r == '%' || r == 0xB0 || r == 0xB1 || r >= 0xA2 && r <= 0xA5 || r == 0x2030 || r == 0x20AC || r == 0x066A:
		return bidiET
	case r == ',' || r == '.' || r == ':' || r == '/' || r == 0x060C:
		return bidiCS
	case unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r):
		return bidiNSM
	case rightToLeft(r):
		arabic := r >= 0x0600 && r <= 0x08FF || r >= 0xFB50 && r <= 0xFDFF || r >= 0xFE70 && r <= 0xFEFF
		switch {
		case arabic && (unicode.IsLetter(r) || r == 0x061F || r == 0x061B):
			return bidiAL
		case unicode.IsLetter(r) || r == 0x05BE:
			return bidiR
		}
		return bidiON
	case unicode.IsLetter(r) || unicode.IsDigit(r):
		return bidiL
	}
	return bidiON
}

// mirror returns the bracket that r is read as when it is set from
// right to left.
func mirror(r rune) rune {
	switch r {
	case '(':
		return ')'
	case ')':
		return '('
	case '[':
		return ']'
	case ']':
		return '['
	case '{':
		return '}'
	case '}':
		return '{'
	case '<':
		return '>'
	case '>':
		return '<'
	case '«':
		return '»'
	case '»':
		return '«'
	case '‹':
		return '›'
	case '›':
		return '‹'
	}
	return r
}

// unit is a glyph of a line put together from where its glyphs stand,
// or a space or tab put between two of its pieces.
type unit struct {
	lo, hi int32 // its text, b.c.Text[lo:hi]; empty for one put in
	put    byte  // the space or tab put in, 0 for text
	run    int32 // the run it is of; -1 for one put in
	// Where it begins and ends on the baseline, displayed, from left to
	// right.
	x0, y0, x1, y1 float64
	style          gofastcore.Style
	class          bidiClass
	level          uint8
}

// rightToLeftLine reports whether a line has letters of a script
// written from right to left, and so is read by bidiUnits.
func (b *builder) rightToLeftLine(ln *line) bool {
	for k := ln.lo; k < ln.hi; k++ {
		if b.c.Runs[b.pieces[b.lidx[k]].run].RTL {
			return true
		}
	}
	return false
}

// segment is a piece of a line with letters written from right to left,
// as it stands along the line.
type segment struct {
	k              int32 // the piece, in b.lidx
	lo, hi         int   // its text, in the run's
	sx, sy, ex, ey float64
	u0, u1         float64 // along the line
}

// bidiUnits returns the units of a line with letters of a script written
// from right to left, in the order they are read, in b.units. It reports
// whether the line reads from right to left.
//
// The interpreter cuts the runs of such a line where it joined strings
// across gaps (extract's endLine): each piece is placed where it is
// drawn, and a program that fills the gaps between words with spaces and
// punctuation drawn later is read as it is seen.
//
// before is the type of the last letter before the line in its
// paragraph (bidiL, bidiR or bidiAL), 0 for none or not known: the rules
// that resolve numbers look back past the start of a line to it.
// numberFirst is set when a number comes before the first letter of the
// line as it is read, which is where before can change the reading.
func (b *builder) bidiUnits(ln *line, before bidiClass) (rtl, numberFirst bool) {
	segs := b.segs[:0]
	for k := ln.lo; k < ln.hi; k++ {
		p := &b.pieces[b.lidx[k]]
		r := &b.c.Runs[p.run]
		if r.Len == 0 {
			continue
		}
		a, e := p.sx*p.dx+p.sy*p.dy, p.ex*p.dx+p.ey*p.dy
		segs = append(segs, segment{k: k, lo: 0, hi: int(r.Len), sx: p.sx, sy: p.sy, ex: p.ex, ey: p.ey, u0: min(a, e), u1: max(a, e)})
	}
	b.segs = segs
	// The stretches from left to right along the line.
	slices.SortStableFunc(segs, func(x, y segment) int {
		switch {
		case x.u0 < y.u0:
			return -1
		case x.u0 > y.u0:
			return 1
		}
		return 0
	})
	us := b.units[:0]
	clusters := b.c.Clusters
	var prev *segment
	for si := range segs {
		sg := &segs[si]
		p := &b.pieces[b.lidx[sg.k]]
		r := &b.c.Runs[p.run]
		t := b.c.Text[r.Off+int32(sg.lo) : r.Off+int32(sg.hi)]
		if len(t) == 0 {
			continue
		}
		// A space or a tab between this stretch and the one left of it.
		if prev != nil && len(us) > 0 && us[len(us)-1].class != bidiWS {
			pp := &b.pieces[b.lidx[prev.k]]
			gap := sg.u0 - prev.u1
			em := max(p.em, pp.em)
			switch {
			case em > 0 && gap >= tabGap*em:
				us = append(us, unit{lo: -1, hi: -1, put: '\t', run: -1, class: bidiWS})
			case gap > b.c.Runs[pp.run].SpaceGap()*b.scale && t[0] != ' ':
				us = append(us, unit{lo: -1, hi: -1, put: ' ', run: -1, class: bidiWS})
			}
		}
		prev = sg
		var style gofastcore.Style
		if r.Font != nil {
			if r.Font.Bold {
				style |= gofastcore.StyleBold
			}
			if r.Font.Italic {
				style |= gofastcore.StyleItalic
			}
		}
		// The stretch's units in the order of its text, each placed by
		// its share of the stretch; then from left to right.
		from := len(us)
		share := func(i int) (float64, float64) {
			f := float64(i) / float64(len(t))
			return sg.sx + (sg.ex-sg.sx)*f, sg.sy + (sg.ey-sg.sy)*f
		}
		base := r.Off + int32(sg.lo)
		c := sort.Search(len(clusters), func(i int) bool { return clusters[i].Off >= base })
		for i := 0; i < len(t); {
			j := i
			if c < len(clusters) && clusters[c].Off == base+int32(i) && clusters[c].Len > 0 {
				j = i + int(clusters[c].Len)
				c++
			} else {
				_, n := utf8.DecodeRune(t[i:])
				j = i + n
				// The marks set on a letter go with it.
				for j < len(t) {
					m, n := utf8.DecodeRune(t[j:])
					if !unicode.Is(unicode.Mn, m) {
						break
					}
					j += n
				}
			}
			j = min(j, len(t))
			first, _ := utf8.DecodeRune(t[i:])
			x0, y0 := share(i)
			x1, y1 := share(j)
			us = append(us, unit{lo: base + int32(i), hi: base + int32(j), run: p.run, x0: x0, y0: y0, x1: x1, y1: y1, style: style, class: classOf(first)})
			i = j
		}
		// The text runs against the stretch: drawn from right to left, or
		// given for glyphs drawn from left to right.
		if (sg.ex-sg.sx)*p.dx+(sg.ey-sg.sy)*p.dy < 0 || r.ActualText && r.RTL {
			slices.Reverse(us[from:])
		}
		for i := from; i < len(us); i++ {
			u := &us[i]
			if (u.x1-u.x0)*p.dx+(u.y1-u.y0)*p.dy < 0 {
				u.x0, u.y0, u.x1, u.y1 = u.x1, u.y1, u.x0, u.y0
			}
		}
	}
	rtl, numberFirst = bidiOrder(us, ln.dir, before)
	// One space where two met, from either side of a stretch turned
	// round.
	space := func(u *unit) bool {
		return u.put != 0 || u.hi-u.lo == 1 && b.c.Text[u.lo] == ' '
	}
	w := 0
	for i := range us {
		if w > 0 && space(&us[i]) && space(&us[w-1]) && us[i].put != '\t' {
			continue
		}
		us[w] = us[i]
		w++
	}
	us = us[:w]
	b.units = us
	b.mirrorLine = b.mirrors(us, ln)
	return rtl, numberFirst
}

// mirrors reports whether the brackets of line ln, in units us in the
// order read, are to be mirrored where they are read from right to left.
// That depends on the file: some give a bracket set from right to left
// as the character it stands for, others as the glyph it is drawn with,
// which is the other bracket. The brackets are mirrored when that leaves
// fewer of them without a partner, opening before closing; where it
// makes no difference, as for a bracket whose partner is on another
// line, when the line was drawn from right to left glyph by glyph, as
// browsers draw it, which give the glyph.
func (b *builder) mirrors(us []unit, ln *line) bool {
	alone := func(mirrored bool) int {
		open, n := 0, 0
		for i := range us {
			u := &us[i]
			if u.put != 0 || u.level%2 == 0 || u.hi-u.lo > 3 {
				continue
			}
			r, _ := utf8.DecodeRune(b.c.Text[u.lo:u.hi])
			if mirrored {
				r = mirror(r)
			}
			switch r {
			case '(', '[', '{', '<', '«', '‹':
				open++
			case ')', ']', '}', '>', '»', '›':
				if open > 0 {
					open--
				} else {
					n++
				}
			}
		}
		return n + open
	}
	if plain, mirrored := alone(false), alone(true); plain != mirrored {
		return mirrored < plain
	}
	for k := ln.lo; k < ln.hi; k++ {
		if b.c.Runs[b.pieces[b.lidx[k]].run].Back {
			return true
		}
	}
	return false
}

// bidiOrder puts units that stand from left to right in the order they
// are read, and reports whether the line reads from right to left, and
// whether a number comes before its first letter as it is read.
//
// The rules that resolve the types of numbers and neutrals look at what
// comes before a unit in the order of reading. For a line read from
// right to left they are applied to its units from right to left, which
// is that order but within the stretches read the other way, where
// they change nothing that matters.
//
// The algorithm resolves a paragraph, not a line: a number at the start
// of a line takes the type of the last letter before it, which may end
// the line before. before is that letter's type (bidiL, bidiR or
// bidiAL), 0 for none: then the line's direction stands in for it, as
// at the start of a paragraph.
//
// dir is the direction the line is read in, bidiL or bidiR, 0 for the
// line's own.
func bidiOrder(us []unit, dir, before bidiClass) (rtl, numberFirst bool) {
	// The direction of the line: that of its letters at both ends, where
	// they agree, else that of most of its words, right to left when as
	// many are written each way. (The algorithm takes the first letter of
	// a paragraph in the order of reading, which is the thing not known.)
	// Or that of the paragraph, where its other lines have told it.
	var left, right bidiClass
	nl, nr := 0, 0
	var prev bidiClass
	for i := range us {
		c := us[i].class
		if c == bidiAL {
			c = bidiR
		}
		if c != bidiL && c != bidiR {
			if c != bidiNSM {
				prev = bidiON
			}
			continue
		}
		if left == 0 {
			left = c
		}
		right = c
		if c != prev {
			if c == bidiL {
				nl++
			} else {
				nr++
			}
		}
		prev = c
	}
	rtl = left == bidiR && right == bidiR || left != right && nr > 0 && nr >= nl
	if dir != 0 {
		rtl = dir == bidiR
	}
	base := bidiL
	var level uint8
	if rtl {
		base, level = bidiR, 1
		slices.Reverse(us)
	}
	for i := range us {
		if c := us[i].class; c == bidiL || c == bidiR || c == bidiAL {
			break
		} else if c == bidiEN {
			numberFirst = true
			break
		}
	}
	cls := func(i int) bidiClass { return us[i].class }
	// W1: a mark takes the type of what it is set on.
	for i := range us {
		if us[i].class == bidiNSM {
			us[i].class = base
			if i > 0 {
				us[i].class = cls(i - 1)
			}
		}
	}
	// W2: a European number after an Arabic letter is an Arabic number;
	// W3: an Arabic letter is a letter written from right to left.
	last := base
	if before != 0 {
		last = before
	}
	for i := range us {
		switch c := cls(i); c {
		case bidiL, bidiR, bidiAL:
			last = c
		case bidiEN:
			if last == bidiAL {
				us[i].class = bidiAN
			}
		}
	}
	for i := range us {
		if us[i].class == bidiAL {
			us[i].class = bidiR
		}
	}
	// W4: one separator between two numbers of a kind is part of them.
	for i := 1; i+1 < len(us); i++ {
		a, s, c := cls(i-1), cls(i), cls(i+1)
		if a == bidiEN && c == bidiEN && (s == bidiES || s == bidiCS) || a == bidiAN && c == bidiAN && s == bidiCS {
			us[i].class = a
		}
	}
	// W5: terminators next to a European number are part of it.
	for i := 0; i < len(us); {
		if cls(i) != bidiET {
			i++
			continue
		}
		j := i
		for j < len(us) && cls(j) == bidiET {
			j++
		}
		if i > 0 && cls(i-1) == bidiEN || j < len(us) && cls(j) == bidiEN {
			for k := i; k < j; k++ {
				us[k].class = bidiEN
			}
		}
		i = j
	}
	// W6: other separators and terminators are neutral; W7: a European
	// number after a letter written from left to right is such a letter.
	strong := base
	switch before {
	case bidiL:
		strong = bidiL
	case bidiR, bidiAL:
		strong = bidiR
	}
	for i := range us {
		switch c := cls(i); c {
		case bidiES, bidiET, bidiCS:
			us[i].class = bidiON
		case bidiL, bidiR:
			strong = c
		case bidiEN:
			if strong == bidiL {
				us[i].class = bidiL
			}
		}
	}
	// N1, N2: neutrals between two of one direction (numbers counting as
	// right to left) take it; others take the line's.
	side := func(c bidiClass) bidiClass {
		switch c {
		case bidiEN, bidiAN, bidiR:
			return bidiR
		case bidiL:
			return bidiL
		}
		return bidiON
	}
	for i := 0; i < len(us); {
		if c := cls(i); c != bidiON && c != bidiWS {
			i++
			continue
		}
		j := i
		for j < len(us) && (cls(j) == bidiON || cls(j) == bidiWS) {
			j++
		}
		before, after := base, base
		if i > 0 {
			before = side(cls(i - 1))
		}
		if j < len(us) {
			after = side(cls(j))
		}
		d := base
		if before == after {
			d = before
		}
		for k := i; k < j; k++ {
			us[k].class = d
		}
		i = j
	}
	// I1, I2: the level of each.
	top := level
	for i := range us {
		l := level
		switch c := cls(i); {
		case level%2 == 0 && c == bidiR:
			l = level + 1
		case level%2 == 0 && (c == bidiEN || c == bidiAN):
			l = level + 2
		case level%2 == 1 && (c == bidiL || c == bidiEN || c == bidiAN):
			l = level + 1
		}
		us[i].level = l
		top = max(top, l)
	}
	if rtl {
		slices.Reverse(us) // from left to right again
	}
	// L2: from the highest level to the lowest odd one, each stretch at
	// that level or higher is turned round.
	lowestOdd := level | 1
	for l := top; l >= lowestOdd && l > 0; l-- {
		for i := 0; i < len(us); {
			if us[i].level < l {
				i++
				continue
			}
			j := i
			for j < len(us) && us[j].level >= l {
				j++
			}
			slices.Reverse(us[i:j])
			i = j
		}
	}
	return rtl, numberFirst
}

// unitText appends the text of a unit, a bracket read from right to
// left being the other one.
func (b *builder) unitText(dst []byte, u *unit) []byte {
	if u.put != 0 {
		return append(dst, u.put)
	}
	t := b.c.Text[u.lo:u.hi]
	if u.level%2 == 1 && b.mirrorLine {
		if r, n := utf8.DecodeRune(t); n == len(t) {
			if m := mirror(r); m != r {
				return utf8.AppendRune(dst, m)
			}
		}
	}
	return append(dst, t...)
}
