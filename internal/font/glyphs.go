package font

import (
	_ "embed"
	"encoding/binary"
	"sort"
	"sync"
)

// Known glyphs: the text of a glyph of a TrueType program that nothing
// in the file gives one, when its outline is that of a glyph of a font
// this package knows (gen/glyphs.go, the Noto fonts for Arabic).
//
// Such a font draws some letters in pieces: the letter without its dots,
// and the dots. A subset of it that Firefox embeds gives the pieces no
// text. The body has a character of its own (U+066E for that of beh);
// a dot has one of the characters MarkFirst and up, which stand for no
// character: the interpreter puts the body and the dots after it
// together into the letter they draw (Compose), and a dot left over is
// a character it could not read.

//go:embed glyphs.bin
var glyphsBin []byte

// The characters given to the pieces of letters of known fonts that have
// none (gen/glyphs.go): from MarkFirst the dots and marks, from BodyFirst
// the letters without them. They are private-use characters of three
// bytes in UTF-8 and never leave the library: one left over is replaced
// by U+FFFD, of the same length, where it is.
const (
	MarkFirst = 0xEC00
	BodyFirst = 0xEC80
	PieceLast = 0xECFF
)

// IsMark reports whether r is the character of a dot or mark of a
// known font.
func IsMark(r rune) bool { return r >= MarkFirst && r < BodyFirst }

// IsPiece reports whether r is the character of a piece of a letter of a
// known font: a dot or mark, or a body that has no character of its own.
func IsPiece(r rune) bool { return r >= MarkFirst && r <= PieceLast }

type knownGlyphs struct {
	fonts   int
	texts   []string
	hashes  []byte // 8 bytes an entry, in increasing order and by font
	textOf  []byte // 3 bytes an entry: the font, and the number of its text
	comps   map[string]string
	longest int // the longest key of comps, in bytes
}

var known = sync.OnceValue(func() *knownGlyphs {
	b := glyphsBin
	k := &knownGlyphs{comps: map[string]string{}}
	if len(b) < 11 || string(b[:8]) != "GFPGLYF2" {
		return k
	}
	k.fonts = int(b[8])
	p := 9
	short := func() string {
		if p >= len(b) || p+1+int(b[p]) > len(b) {
			p = len(b)
			return ""
		}
		s := string(b[p+1 : p+1+int(b[p])])
		p += 1 + int(b[p])
		return s
	}
	n := int(binary.BigEndian.Uint16(b[p:]))
	p += 2
	for i := 0; i < n; i++ {
		k.texts = append(k.texts, short())
	}
	if p+4 > len(b) {
		return k
	}
	m := int(binary.BigEndian.Uint32(b[p:]))
	p += 4
	if p+11*m+2 > len(b) {
		return k
	}
	k.hashes, k.textOf = b[p:p+8*m], b[p+8*m:p+11*m]
	p += 11 * m
	c := int(binary.BigEndian.Uint16(b[p:]))
	p += 2
	for i := 0; i < c; i++ {
		key, val := short(), short()
		k.comps[key] = val
		k.longest = max(k.longest, len(key))
	}
	return k
})

// entries returns the entries of hash h: from i to j.
func (k *knownGlyphs) entries(h uint64) (i, j int) {
	n := len(k.hashes) / 8
	at := func(i int) uint64 { return binary.BigEndian.Uint64(k.hashes[8*i:]) }
	i = sort.Search(n, func(i int) bool { return at(i) >= h })
	for j = i; j < n && at(j) == h; j++ {
	}
	return i, j
}

// knownFont returns the known font that has most of the outlines whose
// hashes are given, those of a program's glyphs; -1 for none.
func knownFont(hashes []uint64) int {
	if len(hashes) == 0 {
		return -1
	}
	k := known()
	if k.fonts == 0 {
		return -1
	}
	votes := make([]int, k.fonts)
	for _, h := range hashes {
		if h == 0 {
			continue
		}
		i, j := k.entries(h)
		for ; i < j; i++ {
			if f := int(k.textOf[3*i]); f < len(votes) {
				votes[f]++
			}
		}
	}
	best := -1
	for f, v := range votes {
		if v > 0 && (best < 0 || v > votes[best]) {
			best = f
		}
	}
	return best
}

// knownText returns the text of the glyph of known font f whose outline
// has hash h; empty for none.
func knownText(h uint64, f int) string {
	k := known()
	i, j := k.entries(h)
	for ; i < j; i++ {
		if int(k.textOf[3*i]) == f {
			if t := int(binary.BigEndian.Uint16(k.textOf[3*i+1:])); t < len(k.texts) {
				return k.texts[t]
			}
		}
	}
	return ""
}

// Compose returns the letter that text, a letter without its dots and
// the dots and marks after it, stands for; false for none.
func Compose(text []byte) (string, bool) {
	k := known()
	if len(text) > k.longest {
		return "", false
	}
	l, ok := k.comps[string(text)]
	return l, ok
}
