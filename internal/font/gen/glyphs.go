//go:build ignore

// Command glyphs writes glyphs.bin, the table of known glyphs of package
// font (glyphs.go):
//
//	go run internal/font/gen/glyphs.go -out internal/font/glyphs.bin FONT...
//
// For each glyph of each font, the hash of its outline (font.OutlineHashes)
// and the text it stands for; and the letters that a glyph of a letter
// without its dots and the glyphs of the dots stand for together. Some
// fonts draw a letter as such pieces, and a PDF file that embeds a subset
// of one can give the pieces no text: Firefox does, with the Noto fonts
// for Arabic. The subset's glyphs have the outlines of the font's, so
// the hash of an outline says which glyph of the font it is.
//
// The text of a glyph is read from the font itself. Its cmap gives the
// characters of some glyphs. Its GSUB table gives the others: a glyph
// that one substitutes for another (a form of a letter at the start of a
// word, say) stands for what that one does, a ligature for the text of
// its parts. A substitution of several glyphs for one is a letter drawn
// in pieces: the first is the letter without its dots, which has a
// character of its own in the font's cmap (U+066E for the body of beh),
// and the others are dots and marks. A glyph with no character in the
// cmap has the one its name gives, as uni0627.short. A piece with none
// is given a private-use character by its name, U+EC00 and up for a mark
// and U+EC80 and up for a body (kafDotlessar), which never leave the
// library: the interpreter puts a letter together from a body and its
// marks (font.Compose), and a piece left over is a character not read.
// The Adobe Glyph List uses none of them.
//
// The fonts are the Noto fonts for Arabic, under the SIL Open Font
// License 1.1; the table holds no part of them but hashes and text. They
// were those of the Arch Linux package noto-fonts 1:2026.11.01-1:
//
//	NotoSansArabic-Regular.ttf   2.013
//	NotoSansArabic-Bold.ttf      2.013
//	NotoNaskhArabic-Regular.ttf  2.021
//	NotoNaskhArabic-Bold.ttf     2.021
//	NotoKufiArabic-Regular.ttf   2.110
//	NotoKufiArabic-Bold.ttf      2.110
//
// The outlines of small glyphs, dots, can be the same in two fonts and
// stand for different marks in each: each outline has a text for each
// font that has it, and the library reads a subset by the font that has
// most of its outlines.
//
// The file is: "GFPGLYF2"; the number of fonts (a byte); the number of
// texts (2 bytes), then each text (a byte of length, then its UTF-8);
// the number of entries (4 bytes), their hashes in increasing order and
// by font (8 bytes each), then the font (a byte) and the number of the
// text (2 bytes) of each; the number of compositions (2 bytes), then for
// each the body and its marks, and the letter (each a byte of length,
// then its UTF-8). All numbers are big-endian.
package main

import (
	"bytes"
	"encoding/binary"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/AOShei/go-fast-pdf/internal/charset"
	"github.com/AOShei/go-fast-pdf/internal/font"
)

// markFirst and bodyFirst are the first of the characters given to
// marks and to bodies of letters that have none.
const markFirst, bodyFirst = 0xEC00, 0xEC80

// sub is a substitution: in for out, or out for the glyphs of in (a
// ligature), or the glyphs of out for in (several for one).
type sub struct {
	kind int // 1 one for one, 2 several for one, 4 a ligature
	in   []int
	out  []int
}

type fontData struct {
	name   string
	text   []string
	names  []string
	hashes []uint64
	subs   []sub
}

func main() {
	out := flag.String("out", "", "the file to write")
	flag.Parse()
	log.SetFlags(0)
	if *out == "" || flag.NArg() == 0 {
		log.Fatal("usage: glyphs -out FILE FONT...")
	}
	var fonts []*fontData
	for _, path := range flag.Args() {
		fonts = append(fonts, readFont(path))
	}

	// The pieces with no character, by name, in all the fonts: the
	// bodies and the marks.
	pieceNames := [2]map[string]bool{{}, {}}
	for _, f := range fonts {
		for _, s := range f.subs {
			if s.kind == 2 && len(s.out) > 1 {
				for i, g := range s.out {
					if f.text[g] == "" && f.names[g] != "" {
						pieceNames[min(i, 1)][f.names[g]] = true
					}
				}
			}
		}
	}
	pieceOf := map[string]string{}
	var marks []string
	for kind, first := range [2]int{bodyFirst, markFirst} {
		names := slices.Sorted(func(yield func(string) bool) {
			for n := range pieceNames[kind] {
				if !yield(n) {
					return
				}
			}
		})
		if len(names) > 128 {
			log.Fatalf("%d pieces", len(names))
		}
		for i, n := range names {
			pieceOf[n] = string(rune(first + i))
		}
		if kind == 1 {
			marks = names
		}
		log.Printf("%d bodies or marks with no character: %v", len(names), names)
	}

	// The text of each outline in each font.
	type key struct {
		h    uint64
		font int
	}
	byHash := map[key]string{}
	comps := map[string]string{}
	conflicts := map[string]bool{}
	for fi, f := range fonts {
		for _, s := range f.subs {
			if s.kind == 2 && len(s.out) > 1 {
				for _, g := range s.out {
					if f.text[g] == "" {
						f.text[g] = pieceOf[f.names[g]]
					}
				}
			}
		}
		propagate(f)
		// The text of each outline: of two glyphs with one outline, the
		// first text in the order of characters.
		for g, h := range f.hashes {
			if h == 0 || f.text[g] == "" {
				continue
			}
			if old, ok := byHash[key{h, fi}]; !ok || f.text[g] < old {
				byHash[key{h, fi}] = f.text[g]
			}
		}
		// The letters drawn in pieces: the text of the body, whichever of
		// the glyphs with its outline gives it, and of the marks.
		forms := formsOf(f)
		same := map[uint64][]int{}
		for g, h := range f.hashes {
			if h != 0 {
				same[h] = append(same[h], g)
			}
		}
		for _, s := range f.subs {
			if s.kind != 2 || len(s.out) < 2 {
				continue
			}
			letter := f.text[s.in[0]]
			if utf8.RuneCountInString(letter) != 1 {
				continue
			}
			// Each piece in each of its forms (at the start of a word and
			// so on, which the font substitutes for it after it is drawn
			// in pieces; a dot moves from the middle of jeem to under it),
			// as the text of any glyph with the outline of the form.
			variants := func(g int) []string {
				seen := map[string]bool{}
				var out []string
				for _, form := range forms[g] {
					glyphs := []int{form}
					if h := f.hashes[form]; h != 0 {
						glyphs = same[h]
					}
					for _, v := range glyphs {
						if t := f.text[v]; t != "" && !seen[t] {
							seen[t] = true
							out = append(out, t)
						}
					}
				}
				return out
			}
			keys := variants(s.out[0])
			for _, g := range s.out[1:] {
				var next []string
				for _, k := range keys {
					for _, v := range variants(g) {
						next = append(next, k+v)
					}
				}
				keys = next
			}
			for _, key := range keys {
				if strings.HasPrefix(key, letter) {
					continue
				}
				// Pieces that two letters share in some form (the tooth of
				// beh, teh, noon and yeh at the start of a word) are the
				// letter first in the order of characters: the basic
				// letters come before those added for other languages.
				if old, ok := comps[key]; ok && old != letter {
					conflicts[key] = true
					if old < letter {
						continue
					}
				}
				comps[key] = letter
			}
		}
		log.Printf("%s: %d glyphs, %d with text", f.name, len(f.text), count(f.text))
	}
	log.Printf("%d marks, %d outlines, %d compositions, %d of them shared by two letters or more", len(marks), len(byHash), len(comps), len(conflicts))
	for _, l := range []string{"\u0628", "\u062A", "\u062B", "\u062C", "\u062E", "\u0646", "\u064A", "\u0623", "\u0643"} {
		var keys []string
		for k, v := range comps {
			if v == l {
				keys = append(keys, fmt.Sprintf("%+q", k))
			}
		}
		sort.Strings(keys)
		log.Printf("%+q from %v", l, keys)
	}
	entries := make([]entry, 0, len(byHash))
	for k, t := range byHash {
		entries = append(entries, entry{k.h, k.font, t})
	}
	write(*out, entries, len(fonts), comps)
}

func count(ts []string) int {
	n := 0
	for _, t := range ts {
		if t != "" {
			n++
		}
	}
	return n
}

// formsOf returns, for each glyph, itself and the glyphs substituted for
// it one for one, and for those, and so on.
func formsOf(f *fontData) [][]int {
	next := make([][]int, len(f.text))
	for _, s := range f.subs {
		if s.kind == 1 || s.kind == 2 && len(s.out) == 1 {
			next[s.in[0]] = append(next[s.in[0]], s.out[0])
		}
	}
	out := make([][]int, len(f.text))
	for g := range out {
		seen := map[int]bool{g: true}
		list := []int{g}
		for i := 0; i < len(list); i++ {
			for _, n := range next[list[i]] {
				if !seen[n] {
					seen[n] = true
					list = append(list, n)
				}
			}
		}
		out[g] = list
	}
	return out
}

// propagate gives the glyphs the text of those they are substituted
// for, until none changes.
func propagate(f *fontData) {
	for changed := true; changed; {
		changed = false
		set := func(g int, t string) {
			if f.text[g] == "" && t != "" {
				f.text[g], changed = t, true
			}
		}
		for _, s := range f.subs {
			switch {
			case s.kind == 1 || s.kind == 2 && len(s.out) == 1:
				set(s.out[0], f.text[s.in[0]])
			case s.kind == 4:
				var sb strings.Builder
				for _, g := range s.in {
					if f.text[g] == "" {
						sb.Reset()
						break
					}
					sb.WriteString(f.text[g])
				}
				set(s.out[0], sb.String())
			}
		}
	}
}

func readFont(path string) *fontData {
	data, err := os.ReadFile(path)
	if err != nil {
		log.Fatal(err)
	}
	chars, names, gsub := font.ProgramGlyphs(data)
	hashes := font.OutlineHashes(data)
	if hashes == nil || gsub == nil {
		log.Fatalf("%s: no glyf or GSUB table", path)
	}
	f := &fontData{name: filepath.Base(path), text: make([]string, len(hashes)), names: make([]string, len(hashes)), hashes: hashes}
	for g := range f.text {
		if g < len(names) {
			f.names[g] = names[g]
		}
		if g < len(chars) && chars[g] != 0 {
			f.text[g] = charset.Letters(string(chars[g]))
		} else if t, ok := charset.GlyphText(f.names[g]); ok && f.names[g] != ".notdef" {
			// No character by the cmap: that of its name, as uni0627.short
			// (a short alef, under a hamza) has.
			f.text[g] = charset.Letters(t)
		}
	}
	f.subs = readGSUB(gsub, len(hashes))
	return f
}

// readGSUB returns the substitutions of all lookups of a GSUB table of
// types 1 to 4, and 7 for them; contextual lookups apply those.
func readGSUB(t []byte, glyphs int) []sub {
	u16 := func(i int) int {
		if i < 0 || i+2 > len(t) {
			log.Fatalf("GSUB: offset %d out of the table", i)
		}
		return int(binary.BigEndian.Uint16(t[i:]))
	}
	coverage := func(at int) []int {
		var gs []int
		switch u16(at) {
		case 1:
			for i := 0; i < u16(at+2); i++ {
				gs = append(gs, u16(at+4+2*i))
			}
		case 2:
			for i := 0; i < u16(at+2); i++ {
				r := at + 4 + 6*i
				for g := u16(r); g <= u16(r+2); g++ {
					gs = append(gs, g)
				}
			}
		default:
			log.Fatalf("GSUB: coverage format %d", u16(at))
		}
		return gs
	}
	var out []sub
	add := func(s sub) {
		for _, g := range append(slices.Clone(s.in), s.out...) {
			if g >= glyphs {
				log.Fatalf("GSUB: glyph %d of %d", g, glyphs)
			}
		}
		out = append(out, s)
	}
	var subtable func(kind, at int)
	subtable = func(kind, at int) {
		switch kind {
		case 1:
			cov := coverage(at + u16(at+2))
			switch u16(at) {
			case 1:
				delta := int(int16(u16(at + 4)))
				for _, g := range cov {
					add(sub{kind: 1, in: []int{g}, out: []int{(g + delta) & 0xFFFF}})
				}
			case 2:
				for i, g := range cov {
					add(sub{kind: 1, in: []int{g}, out: []int{u16(at + 6 + 2*i)}})
				}
			}
		case 2, 3:
			cov := coverage(at + u16(at+2))
			for i, g := range cov {
				seq := at + u16(at+6+2*i)
				var gs []int
				for k := 0; k < u16(seq); k++ {
					gs = append(gs, u16(seq+2+2*k))
				}
				if kind == 2 {
					add(sub{kind: 2, in: []int{g}, out: gs})
				} else {
					for _, a := range gs {
						add(sub{kind: 1, in: []int{g}, out: []int{a}})
					}
				}
			}
		case 4:
			cov := coverage(at + u16(at+2))
			for i, g := range cov {
				set := at + u16(at+6+2*i)
				for k := 0; k < u16(set); k++ {
					lig := set + u16(set+2+2*k)
					in := []int{g}
					for c := 1; c < u16(lig+2); c++ {
						in = append(in, u16(lig+4+2*(c-1)))
					}
					add(sub{kind: 4, in: in, out: []int{u16(lig)}})
				}
			}
		case 7:
			ext := at + int(binary.BigEndian.Uint32(t[at+4:]))
			subtable(u16(at+2), ext)
		}
	}
	list := u16(8)
	for i := 0; i < u16(list); i++ {
		lk := list + u16(list+2+2*i)
		kind := u16(lk)
		for k := 0; k < u16(lk+4); k++ {
			subtable(kind, lk+u16(lk+6+2*k))
		}
	}
	return out
}

// entry is the text of an outline in one of the fonts.
type entry struct {
	h    uint64
	font int
	text string
}

func write(path string, entries []entry, fonts int, comps map[string]string) {
	slices.SortFunc(entries, func(a, b entry) int {
		if a.h != b.h {
			if a.h < b.h {
				return -1
			}
			return 1
		}
		return a.font - b.font
	})
	textIdx := map[string]int{}
	var texts []string
	for _, e := range entries {
		if _, ok := textIdx[e.text]; !ok {
			textIdx[e.text] = len(texts)
			texts = append(texts, e.text)
		}
	}
	var b bytes.Buffer
	b.WriteString("GFPGLYF2")
	short := func(s string) {
		if len(s) > 255 {
			log.Fatalf("text %q too long", s)
		}
		b.WriteByte(byte(len(s)))
		b.WriteString(s)
	}
	b.WriteByte(byte(fonts))
	binary.Write(&b, binary.BigEndian, uint16(len(texts)))
	for _, t := range texts {
		short(t)
	}
	binary.Write(&b, binary.BigEndian, uint32(len(entries)))
	for _, e := range entries {
		binary.Write(&b, binary.BigEndian, e.h)
	}
	for _, e := range entries {
		b.WriteByte(byte(e.font))
		binary.Write(&b, binary.BigEndian, uint16(textIdx[e.text]))
	}
	keys := slices.Sorted(func(yield func(string) bool) {
		for k := range comps {
			if !yield(k) {
				return
			}
		}
	})
	binary.Write(&b, binary.BigEndian, uint16(len(keys)))
	for _, k := range keys {
		short(k)
		short(comps[k])
	}
	if err := os.WriteFile(path, b.Bytes(), 0o644); err != nil {
		log.Fatal(err)
	}
	log.Printf("%s: %d bytes", path, b.Len())
}
