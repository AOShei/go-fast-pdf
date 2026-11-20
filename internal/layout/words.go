package layout

import (
	"sort"
	"strings"

	gofastcore "github.com/AOShei/go-fast-core"
)

// The lines of a page with their words, for a caller that wants more
// than blocks: where each line is, in what font, and where each of its
// words is.
//
// A word is what stands between two spaces of the line's text. Where it
// begins and ends is known exactly when the interpreter recorded the
// places of the spaces (extract.Options.Words): then the box of a word
// runs from the end of the space before it to the start of the space
// after it. Without that, and for text that replaces the glyphs drawn,
// a word is placed by its share of the characters of its run.

// atom is a stretch of text without a space in one run: its length in
// bytes, and where it begins and ends on the baseline, displayed.
type atom struct {
	n              int
	x0, y0, x1, y1 float64
	style          gofastcore.Style // bold and italic, from the font of its run
}

// linesOf fills in res.Lines for the text blocks final, which are in
// the order of res.Blocks.
func (b *builder) linesOf(res *Result, final []int32) {
	var atoms []atom
	for k, bi := range final {
		// The lines of a table are those of its cells.
		own := [1]int32{bi}
		parts := own[:]
		if b.blocks[bi].table > 0 {
			parts = b.blocks[bi].members
		}
		for _, pi := range parts {
			for li := b.blocks[pi].line; li >= 0; li = b.lines[li].next {
				ln := &b.lines[li]
				raw := b.text[ln.tlo:ln.thi]
				out := Line{Block: k, Text: b.clean(raw), BBox: rect(ln.x0, ln.y0, ln.x1, ln.y1), Size: ln.size}
				if ln.main != nil {
					out.Font = fontName(ln.main.BaseFont)
				}
				if ln.hi <= ln.lo {
					continue
				}
				atoms = b.atoms(atoms[:0], ln)
				out.Words = b.words(raw, atoms, ln)
				res.Lines = append(res.Lines, out)
			}
		}
	}
}

// fontName returns the name of a font without the tag of a subset, six
// capital letters and a plus sign.
func fontName(name string) string {
	if len(name) > 7 && name[6] == '+' {
		tag := true
		for i := 0; i < 6; i++ {
			tag = tag && name[i] >= 'A' && name[i] <= 'Z'
		}
		if tag {
			name = name[7:]
		}
	}
	return cleanName(name)
}

// isLineSpace reports whether a byte of a run's text is a space in the
// text of its line: a space, or one of the control characters that a
// line's text has a space for.
func isLineSpace(c byte) bool {
	return c == ' ' || c == '\n' || c == '\t' || c == '\r' || c == '\f' || c == '\v'
}

// atoms appends the stretches of text without a space that the pieces
// of a line have, in the order of the line.
func (b *builder) atoms(dst []atom, ln *line) []atom {
	breaks := b.c.Breaks
	for k := ln.lo; k < ln.hi; k++ {
		p := &b.pieces[b.lidx[k]]
		r := &b.c.Runs[p.run]
		t := b.c.Text[r.Off : r.Off+r.Len]
		if len(t) == 0 {
			continue
		}
		// Where byte i of the run begins, by its share of the run.
		share := func(i int) (float64, float64) {
			f := float64(i) / float64(len(t))
			return p.sx + (p.ex-p.sx)*f, p.sy + (p.ey-p.sy)*f
		}
		g := sort.Search(len(breaks), func(i int) bool { return breaks[i].Off >= r.Off })
		var style gofastcore.Style
		if r.Font != nil {
			if r.Font.Bold {
				style |= gofastcore.StyleBold
			}
			if r.Font.Italic {
				style |= gofastcore.StyleItalic
			}
		}
		cur := atom{x0: p.sx, y0: p.sy, style: style}
		for i := 0; i < len(t); i++ {
			if !isLineSpace(t[i]) {
				cur.n++
				continue
			}
			// The space: where it begins the word before it ends, and
			// where it ends the next one begins.
			var sx, sy, ex, ey float64
			for g < len(breaks) && breaks[g].Off < r.Off+int32(i) {
				g++
			}
			if g < len(breaks) && breaks[g].Off == r.Off+int32(i) {
				sx, sy = b.toDisplay(breaks[g].X0, breaks[g].Y0)
				ex, ey = b.toDisplay(breaks[g].X1, breaks[g].Y1)
			} else {
				sx, sy = share(i)
				ex, ey = share(i + 1)
			}
			if cur.n > 0 {
				cur.x1, cur.y1 = sx, sy
				dst = append(dst, cur)
			}
			cur = atom{x0: ex, y0: ey, style: style}
		}
		if cur.n > 0 {
			cur.x1, cur.y1 = p.ex, p.ey
			dst = append(dst, cur)
		}
	}
	return dst
}

// eachWord splits the text of a line at its spaces and tabs and gives
// each word, text[i:j], the place of the atoms it is made of, and the
// style of the first. The text of a line is the text of its pieces with
// spaces put in and taken out, so the words use up the atoms in their
// order.
func (b *builder) eachWord(text []byte, atoms []atom, ln *line, fn func(i, j int, box gofastcore.Rect, style gofastcore.Style)) {
	first := &b.pieces[b.lidx[ln.lo]]
	a, used := 0, 0 // the atom being used, and how many of its bytes are
	for i := 0; i < len(text); {
		if text[i] == ' ' || text[i] == '\t' {
			i++
			continue
		}
		j := i
		for j < len(text) && text[j] != ' ' && text[j] != '\t' {
			j++
		}
		need := j - i
		if a >= len(atoms) {
			// More text than the pieces have: no place to give.
			fn(i, j, rect(ln.x0, ln.y0, ln.x1, ln.y1), 0)
			i = j
			continue
		}
		// From where in the first atom the word begins to where in the
		// last one it ends.
		at := func(k, bytes int) (float64, float64) {
			f := float64(bytes) / float64(atoms[k].n)
			return atoms[k].x0 + (atoms[k].x1-atoms[k].x0)*f, atoms[k].y0 + (atoms[k].y1-atoms[k].y0)*f
		}
		sx, sy := at(a, used)
		ex, ey := sx, sy
		style := atoms[a].style
		for need > 0 && a < len(atoms) {
			take := min(need, atoms[a].n-used)
			need -= take
			used += take
			ex, ey = at(a, used)
			if used == atoms[a].n {
				a, used = a+1, 0
			}
		}
		// The box: the baseline of the word with the height of the line's
		// glyphs above it and their depth below.
		ax, ay := first.nx*first.up, first.ny*first.up
		bx, by := first.nx*first.down, first.ny*first.down
		fn(i, j, rect(
			min(sx+ax, sx+bx, ex+ax, ex+bx), min(sy+ay, sy+by, ey+ay, ey+by),
			max(sx+ax, sx+bx, ex+ax, ex+bx), max(sy+ay, sy+by, ey+ay, ey+by)), style)
		i = j
	}
}

// words returns the words of a line.
func (b *builder) words(text []byte, atoms []atom, ln *line) []Word {
	var out []Word
	b.eachWord(text, atoms, ln, func(i, j int, box gofastcore.Rect, style gofastcore.Style) {
		if w := b.clean(text[i:j]); strings.TrimSpace(w) != "" {
			out = append(out, Word{Text: w, BBox: box, Style: style})
		}
	})
	return out
}

// printableASCII reports whether t is all printable ASCII, which
// cleaning leaves as it is.
func printableASCII(t []byte) bool {
	for _, c := range t {
		if c < 0x20 || c >= 0x7F {
			return false
		}
	}
	return true
}
