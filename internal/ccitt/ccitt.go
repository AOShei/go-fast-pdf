// Package ccitt decodes images coded as fax machines code them: ITU-T
// T.4 (Group 3, one- and two-dimensional) and T.6 (Group 4), as the
// CCITTFaxDecode filter of a PDF gives them.
//
// A row is coded by where its colour changes. One-dimensional coding
// gives the lengths of its runs, white first; two-dimensional coding
// gives each change against the row above (the reference row, white
// above the first). Group 3 can start each row with an end-of-line code,
// and in its two-dimensional form says after it, by one bit, which way
// the row is coded. Group 4 has no end-of-line codes and codes every row
// against the one above.
//
// What the data does not hold, or holds damaged, is white: the decoder
// stops at the first code it cannot read and returns the rows before it.
package ccitt

import (
	"errors"
)

// Params are the parameters of the CCITTFaxDecode filter.
type Params struct {
	// K is below 0 for Group 4, 0 for one-dimensional Group 3, and above
	// 0 for Group 3 that mixes the two.
	K int
	// Columns is the width of a row in pixels.
	Columns int
	// EncodedByteAlign says that each row starts on a byte, after bits
	// of padding.
	EncodedByteAlign bool
	// EndOfBlock says that the data ends with a code for its end, after
	// which nothing is read.
	EndOfBlock bool
	// BlackIs1 makes 1 the bit for black; by default 1 is white.
	BlackIs1 bool
}

// MaxColumns bounds the width of a row.
const MaxColumns = 1 << 20

// ErrDamaged is returned when the data has a code that is not one, or a
// row that does not add up to its width, before the rows asked for.
var ErrDamaged = errors.New("ccitt: damaged data")

// Decode decodes up to rows rows into dst, each (Columns+7)/8 bytes with
// its first pixel in the high bit of its first byte. It returns the
// rows, as many as asked for: those the data does not hold, or that come
// after a damaged one, are white. n is the number of rows decoded, and
// err is ErrDamaged when the data was damaged before them all.
func Decode(data []byte, p Params, rows int) (out []byte, n int, err error) {
	if p.Columns <= 0 || p.Columns > MaxColumns || rows <= 0 {
		return nil, 0, ErrDamaged
	}
	stride := (p.Columns + 7) / 8
	out = make([]byte, stride*rows)
	white, black := byte(0xFF), byte(0)
	if p.BlackIs1 {
		white, black = 0, 0xFF
	}
	for i := range out {
		out[i] = white
	}
	d := decoder{r: reader{data: data}, cols: p.Columns}
	// The reference row: where its colour changes, then the width three
	// times, which ends any search along it.
	ref := []int{p.Columns, p.Columns, p.Columns}
	var cur []int
	for n < rows {
		twoD := p.K < 0
		if p.K >= 0 {
			if p.EncodedByteAlign {
				d.r.align()
			}
			eols := d.endsOfLine()
			if eols >= 2 && p.EndOfBlock || d.r.done() {
				break
			}
			if p.K > 0 {
				twoD = d.r.bit() == 0
			}
		} else {
			if p.EncodedByteAlign && n > 0 {
				d.r.align()
			}
			if d.r.peek(24) == 0x001001 || d.r.done() {
				break // the end of the block
			}
		}
		var ok bool
		if twoD {
			cur, ok = d.row2D(ref, cur[:0])
		} else {
			cur, ok = d.row1D(cur[:0])
		}
		if !ok {
			return out, n, ErrDamaged
		}
		// The row's pixels: black between each change to black and the
		// change after it.
		row := out[n*stride : (n+1)*stride]
		for i := 0; i < len(cur); i += 2 {
			end := p.Columns
			if i+1 < len(cur) {
				end = cur[i+1]
			}
			fill(row, cur[i], end, black)
		}
		n++
		ref = append(append(ref[:0], cur...), p.Columns, p.Columns, p.Columns)
	}
	if n < rows && n == 0 {
		return out, n, ErrDamaged
	}
	return out, n, nil
}

// fill sets the pixels from x0 to x1 of a row to v.
func fill(row []byte, x0, x1 int, v byte) {
	for x := x0; x < x1; {
		if x%8 == 0 && x+8 <= x1 {
			row[x/8] = v
			x += 8
			continue
		}
		mask := byte(0x80) >> (x % 8)
		row[x/8] = row[x/8]&^mask | v&mask
		x++
	}
}

type decoder struct {
	r    reader
	cols int
}

// endsOfLine reads the end-of-line codes before a row of Group 3, with
// the zero bits that can pad them, and returns how many there were.
func (d *decoder) endsOfLine() int {
	n := 0
	for !d.r.done() {
		switch v := d.r.peek(12); {
		case v == 1:
			d.r.skip(12)
			n++
		case v == 0:
			// No code starts with twelve zeros: this is padding before
			// an end of line.
			d.r.skip(1)
		default:
			return n
		}
	}
	return n
}

// row1D reads a row coded by its runs, white first, and appends where
// its colour changes to cur.
func (d *decoder) row1D(cur []int) ([]int, bool) {
	a0, white := 0, true
	for a0 < d.cols {
		run, ok := d.run(white)
		if !ok {
			return cur, false
		}
		a0 = min(a0+run, d.cols)
		cur = append(cur, a0)
		white = !white
	}
	return trim(cur, d.cols), true
}

// row2D reads a row coded against the reference row ref, and appends
// where its colour changes to cur.
func (d *decoder) row2D(ref, cur []int) ([]int, bool) {
	a0, white := -1, true
	bi := 0 // where the search for b1 along ref starts
	for a0 < d.cols {
		// b1 is the first change of the reference row right of a0 to the
		// colour that a0 is not: a change to black has an even index.
		for bi > 0 && ref[bi-1] > a0 {
			bi--
		}
		for ref[bi] <= a0 && bi < len(ref)-3 {
			bi++
		}
		if (bi%2 == 0) != white {
			bi++
		}
		b1, b2 := ref[bi], ref[bi+1]
		e := modeTable[d.r.peek(tableBits)]
		if e.n == 0 {
			return cur, false
		}
		d.r.skip(int(e.n))
		switch e.value {
		case modePass:
			a0 = b2
		case modeHorizontal:
			r1, ok1 := d.run(white)
			r2, ok2 := d.run(!white)
			if !ok1 || !ok2 {
				return cur, false
			}
			a1 := min(max(a0, 0)+r1, d.cols)
			a2 := min(a1+r2, d.cols)
			cur = append(cur, a1, a2)
			a0 = a2
		default:
			a1 := b1 + vertical[e.value]
			if a1 < 0 || a1 > d.cols || a1 < a0 {
				return cur, false
			}
			cur = append(cur, a1)
			a0 = a1
			white = !white
		}
		if d.r.over() {
			return cur, false
		}
	}
	return trim(cur, d.cols), true
}

// vertical is how far from b1 each vertical mode puts a1.
var vertical = [...]int{modeV0: 0, modeVR1: 1, modeVR2: 2, modeVR3: 3, modeVL1: -1, modeVL2: -2, modeVL3: -3}

// trim drops the changes at the end of the row, which change nothing.
func trim(cur []int, cols int) []int {
	for len(cur) > 0 && cur[len(cur)-1] >= cols {
		cur = cur[:len(cur)-1]
	}
	return cur
}

// run reads a run of one colour: make-up codes, then the code that ends
// it.
func (d *decoder) run(white bool) (int, bool) {
	table := &blackTable
	if white {
		table = &whiteTable
	}
	sum := 0
	for {
		e := table[d.r.peek(tableBits)]
		if e.n == 0 || d.r.over() {
			return 0, false
		}
		d.r.skip(int(e.n))
		sum += int(e.value)
		if e.value < 64 {
			return sum, true
		}
		if sum > d.cols {
			return 0, false // longer than the row
		}
	}
}

// reader reads bits from the high bit of each byte; past the end of
// the data, it reads zeros.
type reader struct {
	data []byte
	pos  int // in bits
}

func (r *reader) peek(n int) uint32 {
	var v uint32
	i, shift := r.pos/8, r.pos%8
	// Four bytes from where the bits start hold any n up to 25.
	for k := 0; k < 4; k++ {
		v <<= 8
		if i+k < len(r.data) {
			v |= uint32(r.data[i+k])
		}
	}
	return v << shift >> (32 - n)
}

func (r *reader) skip(n int) { r.pos += n }

func (r *reader) bit() uint32 {
	v := r.peek(1)
	r.pos++
	return v
}

func (r *reader) align() { r.pos = (r.pos + 7) &^ 7 }

// done reports whether every bit has been read.
func (r *reader) done() bool { return r.pos >= 8*len(r.data) }

// over reports whether more bits were read than there are.
func (r *reader) over() bool { return r.pos > 8*len(r.data) }
