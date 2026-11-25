package gofastpdf

import (
	"unicode"
	"unicode/utf8"

	gofastcore "github.com/AOShei/go-fast-core"
	"github.com/AOShei/go-fast-pdf/internal/layout"
)

// Hyphens at the ends of lines.
//
// A hyphen that ends a line inside a paragraph belongs to the word
// ("well-" and "known") or to the line break ("decla-" and "ration"),
// and the page does not say which. The document often does: where it
// writes the word whole in another place, and nowhere with the hyphen
// inside a line, the hyphen is the line break's and is left out. Without
// that it stays, and the two parts are joined with it.
//
// The words are those of the page, of the pages that wait after it to be
// compared with it (furnitureReach of them), and of the pages before it,
// back to those that waited when the first page with such a hyphen came:
// a document without one has none of this work. What is decided does not
// depend on how the pages were read. A page read by itself (Page.Part)
// has its own words only.

// wordSet is the set of the words of the pages seen, as their hashes,
// and of the pairs of words that a hyphen inside a line joins.
type wordSet struct {
	slots []uint64 // open addressing; 0 is an empty slot
	n     int
	// on is set from the first page that has a hyphen at the end of a
	// line: a document without one asks nothing, and its words are not
	// read.
	on bool
}

// hyphened reports whether a block of the part has a hyphen at the end
// of a line.
func hyphened(styles []layout.Style) bool {
	for i := range styles {
		if len(styles[i].Hyphens) > 0 {
			return true
		}
	}
	return false
}

// wordSetMax bounds the set: a document of more words than this adds no
// more, and its later words are not evidence.
const wordSetMax = 1 << 20

const (
	wordSeed  = 14695981039346656037
	wordPrime = 1099511628211
)

func (s *wordSet) insert(h uint64) {
	if h == 0 {
		h = 1
	}
	if 2*s.n >= len(s.slots) {
		if len(s.slots) >= wordSetMax {
			return
		}
		old := s.slots
		s.slots = make([]uint64, max(256, 2*len(old)))
		s.n = 0
		for _, o := range old {
			if o != 0 {
				s.put(o)
			}
		}
	}
	s.put(h)
}

func (s *wordSet) put(h uint64) {
	mask := uint64(len(s.slots) - 1)
	for i := (h * 0x9E3779B97F4A7C15) >> 20 & mask; ; i = (i + 1) & mask {
		switch s.slots[i] {
		case 0:
			s.slots[i] = h
			s.n++
			return
		case h:
			return
		}
	}
}

func (s *wordSet) has(h uint64) bool {
	if len(s.slots) == 0 {
		return false
	}
	if h == 0 {
		h = 1
	}
	mask := uint64(len(s.slots) - 1)
	for i := (h * 0x9E3779B97F4A7C15) >> 20 & mask; ; i = (i + 1) & mask {
		switch s.slots[i] {
		case 0:
			return false
		case h:
			return true
		}
	}
}

// pairHash is the hash of two words joined by a hyphen, from theirs.
func pairHash(a, b uint64) uint64 {
	return (a*0x9E3779B97F4A7C15 ^ b) * wordPrime
}

// add takes the words of a part. The two parts of a word around a hyphen
// that ends a line are not words: they are what is asked about.
func (s *wordSet) add(part *gofastcore.Part, styles []layout.Style) {
	for i := range part.Blocks {
		b := &part.Blocks[i]
		if b.Text == "" || b.Display != 0 {
			continue
		}
		var marks []int32
		if i < len(styles) {
			marks = styles[i].Hyphens
		}
		s.addText(b.Text, marks)
	}
}

// wordMin is the least number of letters of a word that is kept: the two
// parts of a word broken at a line end have that many together, and the
// short words of a text, which are many, are not looked for.
const wordMin = 4

// lowerASCII maps the letters of ASCII to their small letters, and
// every other byte to 0.
var lowerASCII = func() (t [256]byte) {
	for c := 'a'; c <= 'z'; c++ {
		t[c], t[c-'a'+'A'] = byte(c), byte(c)
	}
	return
}()

func (s *wordSet) addText(t string, marks []int32) {
	// What the word that starts at next is, if one does: after a hyphen
	// at a line end it is no word; after one inside a line it is the
	// second word of a pair, whose first is nextPrev.
	next, nextSkip, nextOn, nextPrev := -1, false, false, uint64(0)
	for i := 0; i < len(t); {
		if c := t[i]; c < utf8.RuneSelf {
			if lowerASCII[c] == 0 {
				i++
				continue
			}
		} else if r, size := utf8.DecodeRuneInString(t[i:]); !unicode.IsLetter(r) {
			i += size
			continue
		}
		start, n := i, 0
		h := uint64(wordSeed)
		for i < len(t) {
			c := t[i]
			if l := lowerASCII[c]; l != 0 {
				h = (h ^ uint64(l)) * wordPrime
				i++
				n++
				continue
			}
			if c < utf8.RuneSelf {
				break
			}
			r, size := utf8.DecodeRuneInString(t[i:])
			if !unicode.IsLetter(r) {
				break
			}
			h = (h ^ uint64(unicode.ToLower(r))) * wordPrime
			i += size
			n++
		}
		skip, paired := start == next && nextSkip, start == next && nextOn
		// A hyphen after the word, and whether it ends a line.
		hyphen, marked := 0, false
		if i < len(t) && (t[i] == '-' || t[i] == 0xE2) {
			if hyphen = layout.HyphenLen(t[i:]); hyphen > 0 {
				for len(marks) > 0 && int(marks[0]) < i {
					marks = marks[1:]
				}
				marked = len(marks) > 0 && int(marks[0]) == i
			}
		}
		if !skip && !marked {
			if n >= wordMin {
				s.insert(h)
			}
			if paired {
				s.insert(pairHash(nextPrev, h))
			}
		}
		if hyphen > 0 {
			next, nextSkip, nextOn, nextPrev = i+hyphen, marked, !marked && !skip, h
		}
	}
}

// hashLetters continues the hash h of a word with the letters of t.
func hashLetters(h uint64, t string) uint64 {
	for _, r := range t {
		h = (h ^ uint64(unicode.ToLower(r))) * wordPrime
	}
	return h
}

// breaks reports whether the hyphen at off in t, n bytes long, which
// ends a line, is there for the line break alone: the set has the two
// parts as one word, and not as two with a hyphen.
func (s *wordSet) breaks(t string, off, n int) bool {
	lo := off
	for lo > 0 {
		r, size := utf8.DecodeLastRuneInString(t[:lo])
		if !unicode.IsLetter(r) {
			break
		}
		lo -= size
	}
	hi := off + n
	for hi < len(t) {
		r, size := utf8.DecodeRuneInString(t[hi:])
		if !unicode.IsLetter(r) {
			break
		}
		hi += size
	}
	if lo == off || hi == off+n || utf8.RuneCountInString(t[lo:off])+utf8.RuneCountInString(t[off+n:hi]) < wordMin {
		return false
	}
	a := hashLetters(wordSeed, t[lo:off])
	return s.has(hashLetters(a, t[off+n:hi])) && !s.has(pairHash(a, hashLetters(wordSeed, t[off+n:hi])))
}

// join leaves out of the text of a part the hyphens at the ends of its
// lines that the set shows to be there for the line break alone.
func (s *wordSet) join(part *gofastcore.Part, styles []layout.Style) {
	if !s.on {
		return
	}
	for i := range min(len(part.Blocks), len(styles)) {
		marks := styles[i].Hyphens
		if len(marks) == 0 {
			continue
		}
		b := &part.Blocks[i]
		t := b.Text
		var out []byte
		var cut []int // where a hyphen was left out, and how long it was
		from := 0
		for _, m := range marks {
			off := int(m)
			if off < from || off >= len(t) {
				continue
			}
			n := layout.HyphenLen(t[off:])
			if n == 0 || !s.breaks(t, off, n) {
				continue
			}
			if out == nil {
				out = make([]byte, 0, len(t))
			}
			out = append(out, t[from:off]...)
			from = off + n
			cut = append(cut, off, n)
		}
		styles[i].Hyphens = nil // the places are those of the text as it was
		if out == nil {
			continue
		}
		b.Text = string(append(out, t[from:]...))
		for k := range b.Spans {
			sp := &b.Spans[k]
			start, end := sp.Start, sp.End
			for c := 0; c < len(cut); c += 2 {
				if cut[c] < start {
					sp.Start -= cut[c+1]
				}
				if cut[c] < end {
					sp.End -= cut[c+1]
				}
			}
		}
	}
}
