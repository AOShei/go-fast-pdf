package font

import (
	"encoding/binary"
)

// The encoding built into a CFF font program (Adobe Technical Note
// #5176, the Compact Font Format).
//
// A CFF program names its glyphs in its charset, by string IDs (SIDs):
// those below 391 are standard strings (cffstrings_gen.go), the others
// its own, in its String INDEX. Its encoding gives each code a glyph. A
// font that names no encoding in the PDF has this one: the name of each
// code's glyph, read as any glyph name is. A CID-keyed program has no
// encoding and is not read here, nor are the Expert charset and
// encoding, which only fonts of small capitals and old-style figures
// use.

// maxCFFGlyphs bounds the glyphs of a CFF program that are read.
const maxCFFGlyphs = 1 << 16

// cffIndex is an INDEX, read where it is: count items, the offsets of
// size bytes each at offsets, the items' data after base.
type cffIndex struct {
	data                       []byte
	count, size, offsets, base int
}

// item returns item i; nil for a damaged one.
func (x *cffIndex) item(i int) []byte {
	if i < 0 || i >= x.count {
		return nil
	}
	lo, hi := x.at(i), x.at(i+1)
	if lo < 0 || lo > hi || hi > len(x.data) {
		return nil
	}
	return x.data[lo:hi]
}

func (x *cffIndex) at(i int) int {
	v := 0
	for k := 0; k < x.size; k++ {
		v = v<<8 | int(x.data[x.offsets+i*x.size+k])
	}
	return x.base + v
}

// readIndex reads the INDEX at off, and returns it with the offset after
// it; ok is false for a damaged one.
func readIndex(data []byte, off int) (cffIndex, int, bool) {
	if off < 0 || off+2 > len(data) {
		return cffIndex{}, off, false
	}
	x := cffIndex{data: data, count: int(binary.BigEndian.Uint16(data[off:]))}
	if x.count == 0 {
		return x, off + 2, true
	}
	if off+3 > len(data) {
		return cffIndex{}, off, false
	}
	x.size = int(data[off+2])
	if x.size < 1 || x.size > 4 {
		return cffIndex{}, off, false
	}
	x.offsets = off + 3
	x.base = x.offsets + (x.count+1)*x.size - 1
	if x.offsets+(x.count+1)*x.size > len(data) {
		return cffIndex{}, off, false
	}
	end := x.at(x.count)
	if end < x.base || end > len(data) {
		return cffIndex{}, off, false
	}
	return x, end, true
}

// topDict reads the operators of a Top DICT that tell where the charset,
// the encoding and the glyphs are, and whether the font is CID-keyed.
func topDict(d []byte) (charset, encoding, charStrings int, cid bool) {
	var operands []int
	for i := 0; i < len(d); {
		b := int(d[i])
		switch {
		case b <= 21: // an operator
			op := b
			i++
			if b == 12 && i < len(d) {
				op = 1200 + int(d[i])
				i++
			}
			last := 0
			if len(operands) > 0 {
				last = operands[len(operands)-1]
			}
			switch op {
			case 15:
				charset = last
			case 16:
				encoding = last
			case 17:
				charStrings = last
			case 1230: // ROS
				cid = true
			}
			operands = operands[:0]
		case b == 28 && i+3 <= len(d):
			operands = append(operands, int(int16(binary.BigEndian.Uint16(d[i+1:]))))
			i += 3
		case b == 29 && i+5 <= len(d):
			operands = append(operands, int(int32(binary.BigEndian.Uint32(d[i+1:]))))
			i += 5
		case b == 30: // a real number: nibbles to the one that ends it
			i++
			for i < len(d) && d[i]&0x0F != 0x0F && d[i]>>4 != 0x0F {
				i++
			}
			i++
			operands = append(operands, 0)
		case b >= 32 && b <= 246:
			operands = append(operands, b-139)
			i++
		case b >= 247 && b <= 250 && i+1 < len(d):
			operands = append(operands, (b-247)*256+int(d[i+1])+108)
			i += 2
		case b >= 251 && b <= 254 && i+1 < len(d):
			operands = append(operands, -(b-251)*256-int(d[i+1])-108)
			i += 2
		default:
			return
		}
		if len(operands) > 48 {
			return // more than any operator takes
		}
	}
	return
}

// cffEncoding returns the glyph name of each code by the encoding built
// into a CFF program. standard is set when that is the Standard
// encoding, whose names the caller has. ok is false when the program
// cannot be read or has no encoding of its own.
func cffEncoding(data []byte) (names [256]string, standard, ok bool) {
	if len(data) < 4 {
		return names, false, false
	}
	hdr := int(data[2])
	_, off, ok1 := readIndex(data, hdr) // names
	tops, off, ok2 := readIndex(data, off)
	strs, _, ok3 := readIndex(data, off)
	if !ok1 || !ok2 || !ok3 || tops.count == 0 {
		return names, false, false
	}
	charsetOff, encodingOff, charStrings, cid := topDict(tops.item(0))
	if cid || charStrings <= 0 || charsetOff < 0 || encodingOff < 0 {
		return names, false, false
	}
	switch encodingOff {
	case 0:
		return names, true, true
	case 1:
		return names, false, false // Expert
	}
	glyphs, _, ok := readIndex(data, charStrings)
	if !ok || glyphs.count == 0 {
		return names, false, false
	}
	n := min(glyphs.count, maxCFFGlyphs)
	// The SID of each glyph.
	sids := make([]int, n)
	switch charsetOff {
	case 0: // ISOAdobe: glyph i is SID i
		for g := range sids {
			if g < 229 {
				sids[g] = g
			}
		}
	case 1, 2:
		return names, false, false // Expert, Expert Subset
	default:
		if !readCharset(data, charsetOff, sids) {
			return names, false, false
		}
	}
	name := func(sid int) string {
		switch {
		case sid < len(cffStandardStrings):
			return cffStandardStrings[sid]
		case sid-len(cffStandardStrings) < strs.count:
			return string(strs.item(sid - len(cffStandardStrings)))
		}
		return ""
	}
	// The code of each glyph.
	p := encodingOff
	if p >= len(data) {
		return names, false, false
	}
	format := data[p]
	p++
	switch format & 0x7F {
	case 0:
		if p >= len(data) {
			return names, false, false
		}
		count := int(data[p])
		p++
		for g := 1; g <= count && p < len(data); g, p = g+1, p+1 {
			if g < n {
				names[data[p]] = name(sids[g])
			}
		}
	case 1:
		if p >= len(data) {
			return names, false, false
		}
		ranges := int(data[p])
		p++
		g := 1
		for r := 0; r < ranges && p+1 < len(data); r, p = r+1, p+2 {
			first, left := int(data[p]), int(data[p+1])
			for c := first; c <= first+left && c < 256; c, g = c+1, g+1 {
				if g < n {
					names[c] = name(sids[g])
				}
			}
		}
	default:
		return names, false, false
	}
	// Supplements: more codes for glyphs that have one, by SID.
	if format&0x80 != 0 && p < len(data) {
		count := int(data[p])
		p++
		for i := 0; i < count && p+2 < len(data); i, p = i+1, p+3 {
			names[data[p]] = name(int(binary.BigEndian.Uint16(data[p+1:])))
		}
	}
	return names, false, true
}

// readCharset fills in the SID of each glyph from a charset of format 0,
// 1 or 2; glyph 0 is .notdef.
func readCharset(data []byte, off int, sids []int) bool {
	if off < 0 || off >= len(data) {
		return false
	}
	p := off + 1
	switch data[off] {
	case 0:
		for g := 1; g < len(sids) && p+1 < len(data); g, p = g+1, p+2 {
			sids[g] = int(binary.BigEndian.Uint16(data[p:]))
		}
	case 1, 2:
		wide := data[off] == 2
		for g := 1; g < len(sids); {
			step := 3
			if wide {
				step = 4
			}
			if p+step > len(data) {
				break
			}
			first := int(binary.BigEndian.Uint16(data[p:]))
			left := int(data[p+2])
			if wide {
				left = int(binary.BigEndian.Uint16(data[p+2:]))
			}
			for k := 0; k <= left && g < len(sids); k, g = k+1, g+1 {
				sids[g] = first + k
			}
			p += step
		}
	default:
		return false
	}
	return true
}
