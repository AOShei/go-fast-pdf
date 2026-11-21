package extract

import (
	"unicode"
	"unicode/utf8"

	"github.com/AOShei/go-fast-pdf/internal/font"
)

// Marks drawn by themselves, and letters drawn in pieces.
//
// A browser draws text glyph by glyph, each where the shaping of the
// line put it, and a mark (a vowel sign, a hamza) as a glyph of its own
// over or under the letter it belongs to: raised or lowered so far that
// it would stand on a line of its own. A string of marks only that is
// drawn on the glyph before it, starting within that glyph's advance and
// less than a font size above or below it, is part of that glyph's run.
// It may start before the advance, where a glyph reaches past its
// origin, but not after it: a mark there is set on what follows, as TeX
// sets the slash of a crossed-out sign after a space.
//
// Some fonts draw a letter as pieces: the letter without its dots and the
// dots. The text of the pieces of a known font (font.Compose) is the
// character of the body and private-use characters for the dots; a body
// and the dots after it are the letter they make, in the same string or
// drawn after it. A dot that no letter takes is U+FFFD at the end of the
// page, a character that is not read.

const (
	// markBefore is how far before the advance of the glyph it is drawn
	// on a mark may start, as a part of the font size: the dot of jeem is
	// in its bowl, which reaches past the start of its advance.
	markBefore = 1.0
	// markAfter is how far past the end of the advance a mark may start:
	// rounding only.
	markAfter = 0.05
)

// isMark reports whether r is a mark: a combining mark of Unicode, or a
// dot of a known font.
func isMark(r rune) bool {
	if font.IsMark(r) {
		return true
	}
	if b := r >> 7; b < 0 || int(b) >= 8*len(markBlocks) || markBlocks[b>>3]&(1<<(b&7)) == 0 {
		return false // the letters of most scripts: no search of the tables
	}
	return unicode.In(r, unicode.Mn, unicode.Me)
}

// markBlocks has a bit for each 128 characters, set where a combining
// mark is among them.
var markBlocks = func() (bits [utf8.MaxRune>>7/8 + 1]byte) {
	for _, t := range []*unicode.RangeTable{unicode.Mn, unicode.Me} {
		set := func(lo, hi, stride uint32) {
			for r := lo; r <= hi; r += stride {
				bits[r>>10] |= 1 << (r >> 7 & 7)
			}
		}
		for _, rg := range t.R16 {
			set(uint32(rg.Lo), uint32(rg.Hi), uint32(rg.Stride))
		}
		for _, rg := range t.R32 {
			set(rg.Lo, rg.Hi, rg.Stride)
		}
	}
	return bits
}()

// onGlyph reports whether the string just shown, in.text[start:], is
// marks only, drawn on the glyph shown before it, which is the end of
// the last run.
func (in *interp) onGlyph(start int, clip int32, x0, y0 float64) bool {
	n := len(in.runs)
	if n == 0 || start >= len(in.text) || in.text[start] < 0xCC {
		return false // no mark starts with a byte under 0xCC: U+0300 is CC 80
	}
	if r, _ := utf8.DecodeRune(in.text[start:]); !isMark(r) {
		return false
	}
	last, p := &in.runs[n-1], &in.prev
	if !p.valid || last.Clip != clip || last.ActualText || int(last.Off+last.Len) != start || !in.sameDisplay(last) {
		return false
	}
	for t := in.text[start:]; len(t) > 0; {
		r, k := utf8.DecodeRune(t)
		if !isMark(r) {
			return false
		}
		t = t[k:]
	}
	along := (x0-p.sx)*p.dx + (y0-p.sy)*p.dy
	end := (p.ex-p.sx)*p.dx + (p.ey-p.sy)*p.dy
	lo, hi := min(0, end), max(0, end)
	if end < 0 {
		lo, hi = lo-markAfter*p.size, hi+markBefore*p.size
	} else {
		lo, hi = lo-markBefore*p.size, hi+markAfter*p.size
	}
	return along >= lo && along <= hi &&
		abs((x0-p.sx)*p.nx+(y0-p.sy)*p.ny) <= p.size
}

// joinMarks adds the string just shown, in.text[start:], to the last run
// when it is marks drawn on its last glyph (onGlyph), and puts the
// letters drawn in pieces together. It is apart from showCodes, which
// shows every string, to keep that lean.
func (in *interp) joinMarks(start, clustersFrom int, clip int32, x0, y0 float64, sh font.Shown) bool {
	if !in.onGlyph(start, clip, x0, y0) {
		return false
	}
	last := &in.runs[len(in.runs)-1]
	last.Glyphs += int32(sh.Glyphs)
	last.Unmapped += int32(sh.Unmapped)
	last.Assumed += int32(sh.Assumed)
	in.composeMarks(int(last.Off), start, clustersFrom)
	last.Len = int32(len(in.text)) - last.Off
	in.checkTextLimit()
	return true
}

// composeMarks puts together the letters drawn in pieces in
// in.text[from:]: each character that is no mark, and as many of the
// marks after it, from at on, as make a letter with it. The text of the
// string shown last begins at start. A dot of a known font that is left
// is noted, to be read as U+FFFD if no letter takes it.
func (in *interp) composeMarks(from, start, clustersFrom int) {
	for i := max(from, start); i < len(in.text); {
		r, k := utf8.DecodeRune(in.text[i:])
		if !isMark(r) {
			if font.IsPiece(r) && !in.isPending(i) {
				in.pending = append(in.pending, int32(i)) // a body: taken by its marks, or not
			}
			i += k
			continue
		}
		// The marks from i, and the character before them.
		e := i
		var ends []int // where each mark ends
		for e < len(in.text) {
			r, k := utf8.DecodeRune(in.text[e:])
			if !isMark(r) {
				break
			}
			e += k
			ends = append(ends, e)
		}
		// The body: the character before the marks, and before those
		// drawn earlier on it that no letter has taken.
		b := i
		for b > from {
			r, k := utf8.DecodeLastRune(in.text[from:b])
			b -= k
			if !isMark(r) {
				break
			}
		}
		if r, _ := utf8.DecodeRune(in.text[b:]); isMark(r) || in.inCluster(b, clustersFrom) {
			b = i
		}
		done := false
		for m := len(ends) - 1; b < i && m >= 0; m-- {
			if letter, ok := font.Compose(in.text[b:ends[m]]); ok {
				in.replace(b, ends[m], letter, start, clustersFrom)
				i, done = b+len(letter), true
				break
			}
		}
		if done {
			continue
		}
		for j := b; j < e; {
			r, k := utf8.DecodeRune(in.text[j:])
			if font.IsPiece(r) && !in.isPending(j) {
				in.pending = append(in.pending, int32(j))
			}
			j += k
		}
		i = e
	}
}

// replace writes letter over in.text[b:e], which ends at or after start,
// the beginning of the text of the string shown last, and moves what
// points after it: the clusters of that string and the places of its
// spaces, and the dots not yet taken.
func (in *interp) replace(b, e int, letter string, start, clustersFrom int) {
	d := len(letter) - (e - b)
	in.text = append(in.text[:b], append([]byte(letter), in.text[e:]...)...)
	for i := clustersFrom; i < len(in.clusters); i++ {
		if c := &in.clusters[i]; int(c.Off) >= e {
			c.Off += int32(d)
		}
	}
	for i := range in.marks {
		if m := &in.marks[i]; start+int(m.off) >= e {
			m.off += int32(d)
		}
	}
	w := 0
	for _, p := range in.pending {
		switch {
		case int(p) >= e:
			in.pending[w] = p + int32(d)
			w++
		case int(p) < b:
			in.pending[w] = p
			w++
		}
	}
	in.pending = in.pending[:w]
}

// inCluster reports whether position b is inside a cluster of the
// string shown last, a glyph of several characters.
func (in *interp) inCluster(b, clustersFrom int) bool {
	for i := clustersFrom; i < len(in.clusters); i++ {
		if c := &in.clusters[i]; b >= int(c.Off) && b < int(c.Off+c.Len) {
			return true
		}
	}
	return false
}

func (in *interp) isPending(j int) bool {
	for _, p := range in.pending {
		if int(p) == j {
			return true
		}
	}
	return false
}

// settleMarks reads the dots that no letter took as U+FFFD, which has
// as many bytes: nothing that points into the text moves.
func (in *interp) settleMarks() {
	for _, p := range in.pending {
		if int(p)+3 <= len(in.text) {
			if r, _ := utf8.DecodeRune(in.text[p:]); font.IsPiece(r) {
				copy(in.text[p:], "\uFFFD")
				in.unmapped++
			}
		}
	}
	in.pending = in.pending[:0]
}
