package inflate

import "encoding/binary"

// The Adler-32 checksum of RFC 1950, as hash/adler32 computes it, in
// fewer steps.
//
// For each byte b, s1 += b and s2 += s1. Over a run of k bytes that is
// s2 += k*s1 + k*b[0] + (k-1)*b[1] + ... + 1*b[k-1], and s1 += the sum of
// the bytes; both sums of a word of eight bytes are made with two
// multiplications each, on its bytes spread into four lanes of 16 bits.
// The sums are reduced modulo 65521 as often as zlib reduces them, so
// that they never overflow.
const (
	adlerMod = 65521
	// adlerMax is the most bytes after which s2 still fits in 32 bits
	// (zlib's NMAX is 5552), as a multiple of 32.
	adlerMax = 5536
)

const (
	lanes = 0x00ff00ff00ff00ff
	// ones adds the four lanes into the top one; the even and odd
	// weights do the same with the weights 8, 6, 4, 2 and 7, 5, 3, 1 of
	// the bytes 0, 2, 4, 6 and 1, 3, 5, 7 of a word, read little endian.
	ones        = 0x0001000100010001
	evenWeights = 0x0008000600040002
	oddWeights  = 0x0007000500030001
)

// sums8 returns the sum of the eight bytes of v and their weighted sum,
// 8 times the first byte down to 1 times the last. No lane passes 16
// bits: 255 times 20 is less than 65536.
func sums8(v uint64) (sum, weighted uint64) {
	even, odd := v&lanes, v>>8&lanes
	sum = (even + odd) * ones >> 48
	weighted = (even*evenWeights + odd*oddWeights) >> 48
	return sum, weighted
}

func adler32(p []byte) uint32 {
	s1, s2 := uint64(1), uint64(0)
	for len(p) > 0 {
		n := min(len(p), adlerMax)
		q := p[:n]
		p = p[n:]
		for len(q) >= 32 {
			sa, wa := sums8(binary.LittleEndian.Uint64(q))
			sb, wb := sums8(binary.LittleEndian.Uint64(q[8:]))
			sc, wc := sums8(binary.LittleEndian.Uint64(q[16:]))
			sd, wd := sums8(binary.LittleEndian.Uint64(q[24:]))
			s2 += 32*s1 + 24*sa + 16*sb + 8*sc + wa + wb + wc + wd
			s1 += sa + sb + sc + sd
			q = q[32:]
		}
		for _, b := range q {
			s1 += uint64(b)
			s2 += s1
		}
		s1 %= adlerMod
		s2 %= adlerMod
	}
	return uint32(s2<<16 | s1)
}
