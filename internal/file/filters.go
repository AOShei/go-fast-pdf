package file

import (
	"fmt"

	gofastcore "github.com/AOShei/go-fast-core"
	"github.com/AOShei/go-fast-pdf/internal/syntax"
)

// The stream filters other than Flate. Each appends its output to dst,
// stops with ErrTruncated once it has produced limit bytes, and returns
// what it decoded before any damage together with the error.

func truncated(dst []byte, base int, limit int64) bool {
	return int64(len(dst)-base) >= limit
}

// asciiHexDecode decodes pairs of hexadecimal digits up to '>'.
func asciiHexDecode(dst, src []byte, limit int64) ([]byte, error) {
	base := len(dst)
	for i, c := range src {
		if c == '>' {
			src = src[:i]
			break
		}
	}
	if int64(len(src)/2) > limit {
		dst = syntax.Unhex(dst, src[:2*limit])
		return dst[:base+int(limit)], ErrTruncated
	}
	return syntax.Unhex(dst, src), nil
}

// ascii85Decode decodes groups of five characters to four bytes, up to
// the "~>" marker. White space is ignored; 'z' stands for four zero
// bytes; a final group of two to four characters gives one to three
// bytes.
func ascii85Decode(dst, src []byte, limit int64) ([]byte, error) {
	base := len(dst)
	if len(src) >= 2 && src[0] == '<' && src[1] == '~' {
		src = src[2:]
	}
	var group [5]byte
	n := 0
	flush := func(count int) {
		// Missing characters count as 'u', the highest digit.
		for i := count; i < 5; i++ {
			group[i] = 'u'
		}
		var v uint32
		for _, c := range group {
			v = v*85 + uint32(c-'!')
		}
		out := [4]byte{byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)}
		dst = append(dst, out[:count-1]...)
	}
	for _, c := range src {
		switch {
		case c == '~':
			if n >= 2 {
				flush(n)
			}
			if truncated(dst, base, limit+1) {
				return dst[:base+int(limit)], ErrTruncated
			}
			return dst, nil
		case syntax.IsSpace(c):
		case c == 'z' && n == 0:
			dst = append(dst, 0, 0, 0, 0)
		case c >= '!' && c <= 'u':
			group[n] = c
			if n++; n == 5 {
				flush(5)
				n = 0
			}
		default:
			return dst, fmt.Errorf("%w: byte %#x in ASCII85 data", gofastcore.ErrMalformed, c)
		}
		if truncated(dst, base, limit+1) {
			return dst[:base+int(limit)], ErrTruncated
		}
	}
	if n >= 2 {
		flush(n)
	}
	if truncated(dst, base, limit+1) {
		return dst[:base+int(limit)], ErrTruncated
	}
	return dst, nil
}

// runLengthDecode expands runs: a length byte below 128 is followed by
// that many bytes plus one, copied; above 128 by one byte, repeated
// 257 minus the length times; 128 ends the data.
func runLengthDecode(dst, src []byte, limit int64) ([]byte, error) {
	base := len(dst)
	for i := 0; i < len(src); {
		n := int(src[i])
		i++
		switch {
		case n == 128:
			return dst, nil
		case n < 128:
			end := min(i+n+1, len(src))
			dst = append(dst, src[i:end]...)
			i = end
		default:
			if i >= len(src) {
				return dst, nil
			}
			for range 257 - n {
				dst = append(dst, src[i])
			}
			i++
		}
		if truncated(dst, base, limit+1) {
			return dst[:base+int(limit)], ErrTruncated
		}
	}
	return dst, nil
}

// lzwDecode decodes the LZW variant of PDF and TIFF: codes of 9 to 12
// bits, most significant bit first, 256 to clear the table and 257 to
// end. With earlyChange, the default in PDF, the code width grows one
// code earlier than the table strictly requires.
func lzwDecode(dst, src []byte, limit int64, earlyChange bool) ([]byte, error) {
	const (
		clear   = 256
		eod     = 257
		first   = 258
		maxCode = 4096
	)
	base := len(dst)
	var prefix [maxCode]uint16
	var suffix [maxCode]byte
	var scratch [maxCode]byte
	early := 0
	if earlyChange {
		early = 1
	}
	width, next, prev := uint(9), first, -1
	var bits uint32
	var nbits uint
	for _, c := range src {
		bits = bits<<8 | uint32(c)
		nbits += 8
		for nbits >= width {
			code := int(bits >> (nbits - width) & (1<<width - 1))
			nbits -= width
			switch {
			case code == clear:
				width, next, prev = 9, first, -1
				continue
			case code == eod:
				return dst, nil
			}
			if prev < 0 {
				if code >= clear {
					return dst, fmt.Errorf("%w: LZW data starts with code %d", gofastcore.ErrMalformed, code)
				}
				dst = append(dst, byte(code))
				prev = code
				continue
			}
			// Expand the code into scratch, back to front.
			cur := code
			n := len(scratch)
			if code == next {
				// The code being defined: the previous string plus its
				// own first byte, which is filled in below.
				n--
				cur = prev
			} else if code > next {
				return dst, fmt.Errorf("%w: LZW code %d before it is defined", gofastcore.ErrMalformed, code)
			}
			for cur >= first {
				n--
				scratch[n] = suffix[cur]
				cur = int(prefix[cur])
			}
			n--
			scratch[n] = byte(cur)
			if code == next {
				scratch[len(scratch)-1] = scratch[n]
			}
			dst = append(dst, scratch[n:]...)
			if next < maxCode {
				prefix[next], suffix[next] = uint16(prev), scratch[n]
				next++
				if next+early >= 1<<width && width < 12 {
					width++
				}
			}
			prev = code
			if truncated(dst, base, limit+1) {
				return dst[:base+int(limit)], ErrTruncated
			}
		}
	}
	return dst, nil
}

// untiff undoes TIFF predictor 2 on data[start:], in place: every
// component is stored as the difference from the same component of the
// pixel to its left.
func untiff(data []byte, start int, columns, colors, bpc int64) ([]byte, error) {
	if bpc != 8 && bpc != 16 {
		return data, fmt.Errorf("%w: TIFF predictor with %d bits per component", gofastcore.ErrUnsupported, bpc)
	}
	bytesPer := int(bpc / 8)
	stride := int(colors) * bytesPer
	rowLen := int(columns) * stride
	if rowLen <= 0 {
		return data, nil
	}
	body := data[start:]
	for len(body) > 0 {
		row := body[:min(rowLen, len(body))]
		body = body[len(row):]
		if bytesPer == 1 {
			for i := stride; i < len(row); i++ {
				row[i] += row[i-stride]
			}
			continue
		}
		for i := stride; i+1 < len(row); i += 2 {
			v := uint16(row[i])<<8 | uint16(row[i+1])
			left := uint16(row[i-stride])<<8 | uint16(row[i-stride+1])
			v += left
			row[i], row[i+1] = byte(v>>8), byte(v)
		}
	}
	return data, nil
}
