package font

import (
	"encoding/binary"
	"sync"
	"unicode/utf8"

	"github.com/AOShei/go-fast-pdf/internal/file"
	"github.com/AOShei/go-fast-pdf/internal/syntax"
)

// The characters of the glyphs of an embedded TrueType program.
//
// A TrueType program says which glyph shows each character, in its cmap
// table. Turned round, that says which character a glyph shows: what a
// CIDFont of type 2 needs when nothing else says what its codes are. The
// CID gives the glyph through /CIDToGIDMap (the same number when it is
// /Identity), and the glyph the character. A glyph that several
// characters reach shows the first of them, other than one for private
// use. Subtables for Unicode are read, in formats 0, 4, 6 and 12; one of
// a symbol font (platform 3, encoding 0) gives codes of its own and no
// characters. The program is read the first time a code of the font has
// no text otherwise, not when the font is loaded.

const (
	// maxProgramBytes bounds the font program that is read for its cmap.
	maxProgramBytes = 32 << 20
	// maxCMapChars bounds the characters a cmap subtable is read for.
	maxCMapChars = 1 << 20
	// maxGIDMapBytes bounds a /CIDToGIDMap stream: two bytes for each of
	// 65,536 CIDs.
	maxGIDMapBytes = 1 << 17
)

// trueTypeText gives the character of each CID of a CIDFont of type 2,
// from its program, read once when first asked.
type trueTypeText struct {
	f       *file.File
	program syntax.Value // FontFile2
	gidMap  syntax.Value // CIDToGIDMap: a stream, or anything else for Identity

	once  sync.Once
	chars []rune // by glyph
	gids  []byte // the CIDToGIDMap stream, two bytes a CID; nil for Identity
}

// char returns the character of a CID, 0 for none.
func (t *trueTypeText) char(cid uint32) rune {
	t.once.Do(t.read)
	gid := cid
	if t.gids != nil {
		if int(cid)*2+1 >= len(t.gids) {
			return 0
		}
		gid = uint32(binary.BigEndian.Uint16(t.gids[2*cid:]))
	}
	if int(gid) >= len(t.chars) {
		return 0
	}
	return t.chars[gid]
}

func (t *trueTypeText) read() {
	if t.gidMap.Kind == syntax.Stream {
		t.gids, _ = t.f.Decode(t.gidMap, nil, maxGIDMapBytes)
		if t.gids == nil {
			t.gids = []byte{}
		}
	}
	data, _ := t.f.Decode(t.program, nil, maxProgramBytes)
	t.chars = cmapChars(data)
	t.f, t.program, t.gidMap = nil, syntax.Value{}, syntax.Value{}
}

// cmapChars reads the cmap table of a TrueType program and returns the
// character each glyph shows; nil when it says nothing.
func cmapChars(data []byte) []rune {
	u16 := func(b []byte, i int) (int, bool) {
		if i < 0 || i+2 > len(b) {
			return 0, false
		}
		return int(binary.BigEndian.Uint16(b[i:])), true
	}
	u32 := func(b []byte, i int) (int, bool) {
		if i < 0 || i+4 > len(b) {
			return 0, false
		}
		return int(binary.BigEndian.Uint32(b[i:]) & 0x7FFFFFFF), true
	}
	// The table directory.
	n, ok := u16(data, 4)
	if !ok {
		return nil
	}
	var table []byte
	for i := 0; i < n && i < 256; i++ {
		rec := 12 + 16*i
		if rec+16 > len(data) {
			return nil
		}
		if string(data[rec:rec+4]) == "cmap" {
			off, _ := u32(data, rec+8)
			length, _ := u32(data, rec+12)
			if off >= len(data) {
				return nil
			}
			table = data[off:min(len(data), off+length)]
			break
		}
	}
	if table == nil {
		return nil
	}
	// The subtable that says the most about Unicode.
	count, _ := u16(table, 2)
	best, rank := -1, 0
	for i := 0; i < count && i < 256; i++ {
		platform, ok1 := u16(table, 4+8*i)
		encoding, ok2 := u16(table, 6+8*i)
		off, ok3 := u32(table, 8+8*i)
		if !ok1 || !ok2 || !ok3 {
			break
		}
		r := 0
		switch {
		case platform == 3 && encoding == 10, platform == 0 && (encoding == 4 || encoding == 6):
			r = 3
		case platform == 3 && encoding == 1, platform == 0:
			r = 2
		}
		if r > rank {
			best, rank = off, r
		}
	}
	if best < 0 || best >= len(table) {
		return nil
	}
	sub := table[best:]
	var chars []rune
	set := func(c, gid int) {
		if gid <= 0 || gid > 0xFFFF || c <= 0 || c > utf8.MaxRune || c >= 0xD800 && c <= 0xDFFF {
			return
		}
		for len(chars) <= gid {
			chars = append(chars, 0)
		}
		private := func(r rune) bool { return r >= 0xE000 && r <= 0xF8FF }
		if old := chars[gid]; old == 0 || private(old) && !private(rune(c)) {
			chars[gid] = rune(c)
		}
	}
	format, _ := u16(sub, 0)
	switch format {
	case 0:
		for c := 0; c < 256; c++ {
			if 6+c < len(sub) {
				set(c, int(sub[6+c]))
			}
		}
	case 4:
		segX2, ok := u16(sub, 6)
		if !ok {
			return nil
		}
		ends, starts, deltas, ranges := 14, 16+segX2, 16+2*segX2, 16+3*segX2
		done := 0
		for s := 0; s < segX2/2; s++ {
			end, ok1 := u16(sub, ends+2*s)
			start, ok2 := u16(sub, starts+2*s)
			delta, ok3 := u16(sub, deltas+2*s)
			ro, ok4 := u16(sub, ranges+2*s)
			if !ok1 || !ok2 || !ok3 || !ok4 || start > end {
				continue
			}
			for c := start; c <= end && c != 0xFFFF && done < maxCMapChars; c++ {
				done++
				g := c
				if ro != 0 {
					var ok bool
					if g, ok = u16(sub, ranges+2*s+ro+2*(c-start)); !ok || g == 0 {
						continue
					}
				}
				set(c, (g+delta)&0xFFFF)
			}
		}
	case 6:
		first, ok1 := u16(sub, 6)
		n, ok2 := u16(sub, 8)
		if !ok1 || !ok2 {
			return nil
		}
		for i := 0; i < n; i++ {
			if g, ok := u16(sub, 10+2*i); ok {
				set(first+i, g)
			}
		}
	case 12:
		groups, ok := u32(sub, 12)
		if !ok {
			return nil
		}
		done := 0
		for i := 0; i < groups && done < maxCMapChars; i++ {
			start, ok1 := u32(sub, 16+12*i)
			end, ok2 := u32(sub, 20+12*i)
			gid, ok3 := u32(sub, 24+12*i)
			if !ok1 || !ok2 || !ok3 {
				break
			}
			for c := start; c <= end && done < maxCMapChars; c++ {
				done++
				set(c, gid+(c-start))
			}
		}
	}
	return chars
}
