package font

import "encoding/binary"

// The outlines of the glyphs of a TrueType program, as numbers that say
// which glyph of a known font one is (glyphs.go).
//
// A program embedded in a PDF file is mostly a subset: its glyphs are
// numbered anew and lose their names, and their instructions may be
// dropped. Their outlines are those of the font they were taken from.
// The hash of a glyph is taken over what its outline is: the ends of its
// contours and each point, on the curve or not, where it is; for a glyph
// made of others, the hash of each part with where it is put and how it
// is scaled. Not over the bytes, which a subset can write otherwise.

const (
	// maxCompositeDepth bounds the nesting of glyphs made of others.
	maxCompositeDepth = 8
	// maxGlyphPoints bounds the points of one glyph.
	maxGlyphPoints = 1 << 16
)

// glyfTables is what is needed to read the outlines of a program.
type glyfTables struct {
	glyf, loca []byte
	long       bool // loca has 4-byte offsets
	n          int  // glyphs
}

func newGlyfTables(tables map[string][]byte) (glyfTables, bool) {
	head, maxp := tables["head"], tables["maxp"]
	t := glyfTables{glyf: tables["glyf"], loca: tables["loca"]}
	if len(head) < 54 || len(maxp) < 6 || t.glyf == nil || t.loca == nil {
		return t, false
	}
	t.long = binary.BigEndian.Uint16(head[50:]) == 1
	t.n = int(binary.BigEndian.Uint16(maxp[4:]))
	return t, true
}

// data returns the bytes of glyph g; nil for an empty glyph or one out
// of the tables.
func (t *glyfTables) data(g int) []byte {
	if g < 0 || g >= t.n {
		return nil
	}
	var lo, hi int
	if t.long {
		if 4*g+8 > len(t.loca) {
			return nil
		}
		lo, hi = int(binary.BigEndian.Uint32(t.loca[4*g:])), int(binary.BigEndian.Uint32(t.loca[4*g+4:]))
	} else {
		if 2*g+4 > len(t.loca) {
			return nil
		}
		lo, hi = 2*int(binary.BigEndian.Uint16(t.loca[2*g:])), 2*int(binary.BigEndian.Uint16(t.loca[2*g+2:]))
	}
	if lo < 0 || hi <= lo || hi > len(t.glyf) {
		return nil
	}
	return t.glyf[lo:hi]
}

// fnv is a 64-bit FNV-1a hash.
type fnv uint64

func newFNV() fnv { return 14695981039346656037 }

func (h *fnv) add(v int) {
	for i := 0; i < 4; i++ {
		*h ^= fnv(byte(v >> (8 * i)))
		*h *= 1099511628211
	}
}

// hash returns the hash of the outline of glyph g; 0 for an empty glyph
// or one that cannot be read.
func (t *glyfTables) hash(g, depth int) uint64 {
	d := t.data(g)
	if len(d) < 10 || depth > maxCompositeDepth {
		return 0
	}
	contours := int(int16(binary.BigEndian.Uint16(d)))
	h := newFNV()
	if contours >= 0 {
		h.add('S')
		h.add(contours)
		p := 10
		if contours == 0 || p+2*contours+2 > len(d) {
			return 0
		}
		points := 0
		for i := 0; i < contours; i++ {
			end := int(binary.BigEndian.Uint16(d[p:]))
			p += 2
			h.add(end)
			points = end + 1
		}
		if points > maxGlyphPoints {
			return 0
		}
		p += 2 + int(binary.BigEndian.Uint16(d[p:])) // the instructions
		// The flags, then the x coordinates, then the y ones.
		flags := make([]byte, 0, points)
		for len(flags) < points {
			if p >= len(d) {
				return 0
			}
			f := d[p]
			p++
			n := 1
			if f&8 != 0 {
				if p >= len(d) {
					return 0
				}
				n += int(d[p])
				p++
			}
			for ; n > 0 && len(flags) < points; n-- {
				flags = append(flags, f)
			}
		}
		xs := make([]int, points)
		for axis, short, same := 0, byte(2), byte(16); axis < 2; axis, short, same = axis+1, 4, 32 {
			v := 0
			for i, f := range flags {
				switch {
				case f&short != 0:
					if p >= len(d) {
						return 0
					}
					if f&same != 0 {
						v += int(d[p])
					} else {
						v -= int(d[p])
					}
					p++
				case f&same == 0:
					if p+2 > len(d) {
						return 0
					}
					v += int(int16(binary.BigEndian.Uint16(d[p:])))
					p += 2
				}
				if axis == 0 {
					xs[i] = v
				} else {
					h.add(int(f & 1))
					h.add(xs[i])
					h.add(v)
				}
			}
		}
	} else {
		h.add('C')
		p := 10
		for more := true; more; {
			if p+4 > len(d) {
				return 0
			}
			flags := binary.BigEndian.Uint16(d[p:])
			part := t.hash(int(binary.BigEndian.Uint16(d[p+2:])), depth+1)
			if part == 0 {
				return 0
			}
			p += 4
			h.add(int(part))
			h.add(int(part >> 32))
			h.add(int(flags & (0x0002 | 0x0800 | 0x1000))) // offsets or points; how offsets scale
			var a1, a2 int
			if flags&1 != 0 {
				if p+4 > len(d) {
					return 0
				}
				a1, a2 = int(binary.BigEndian.Uint16(d[p:])), int(binary.BigEndian.Uint16(d[p+2:]))
				if flags&2 != 0 {
					a1, a2 = int(int16(a1)), int(int16(a2))
				}
				p += 4
			} else {
				if p+2 > len(d) {
					return 0
				}
				a1, a2 = int(d[p]), int(d[p+1])
				if flags&2 != 0 {
					a1, a2 = int(int8(a1)), int(int8(a2))
				}
				p += 2
			}
			h.add(a1)
			h.add(a2)
			scales := 0
			switch {
			case flags&0x0008 != 0:
				scales = 1
			case flags&0x0040 != 0:
				scales = 2
			case flags&0x0080 != 0:
				scales = 4
			}
			h.add(scales)
			for ; scales > 0; scales-- {
				if p+2 > len(d) {
					return 0
				}
				h.add(int(int16(binary.BigEndian.Uint16(d[p:]))))
				p += 2
			}
			more = flags&0x0020 != 0
		}
	}
	if h == 0 {
		h = 1
	}
	return uint64(h)
}

// OutlineHashes returns the hash of the outline of each glyph of a
// TrueType program, 0 for an empty glyph or one that cannot be read; nil
// when it is not a TrueType program. The generator of the table of known
// glyphs uses it (gen/glyphs.go).
func OutlineHashes(program []byte) []uint64 {
	t, ok := newGlyfTables(sfntTables(program))
	if !ok {
		return nil
	}
	out := make([]uint64, t.n)
	for g := range out {
		out[g] = t.hash(g, 0)
	}
	return out
}

// ProgramGlyphs returns what the generator of the table of known glyphs
// reads of a TrueType program: the character of each glyph by its cmap
// (glyphChars), the name of each by its post table, and its GSUB table.
func ProgramGlyphs(program []byte) (chars []rune, names []string, gsub []byte) {
	tables := sfntTables(program)
	chars = glyphChars(tables["cmap"])
	p := readPost(tables["post"])
	names = make([]string, p.n)
	for g := range names {
		names[g] = p.name(g)
	}
	return chars, names, tables["GSUB"]
}
