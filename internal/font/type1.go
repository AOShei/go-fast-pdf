package font

import (
	"bytes"

	"github.com/AOShei/go-fast-pdf/internal/file"
	"github.com/AOShei/go-fast-pdf/internal/syntax"
)

// type1Limit bounds how much of a font program is decoded to find its
// encoding, which is in the readable part at its start.
const type1Limit = 256 << 10

// type1Encoding reads the encoding built into a Type 1 font program: the
// glyph name of each code. A font that names no /Encoding in its
// dictionary uses this one; documents made with TeX rely on it for their
// ligatures, quotes and dashes.
//
// The program starts with PostScript in clear text, which either says
//
//	/Encoding StandardEncoding def
//
// or fills an array:
//
//	/Encoding 256 array
//	0 1 255 {1 index exch /.notdef put} for
//	dup 11 /ff put
//	dup 12 /fi put
//	readonly def
//
// standard is true for the first form. ok is false when neither is
// found.
func type1Encoding(f *file.File, descriptor syntax.Value) (names *[256]string, standard, ok bool) {
	stm := f.Resolve(descriptor.Get("FontFile"))
	if stm.Kind != syntax.Stream {
		return nil, false, false
	}
	data, _ := f.Decode(stm, nil, type1Limit)
	// The encrypted part follows "eexec"; nothing after it is text.
	if i := bytes.Index(data, []byte("eexec")); i >= 0 {
		data = data[:i]
	}
	i := bytes.Index(data, []byte("/Encoding"))
	if i < 0 {
		return nil, false, false
	}
	data = data[i+len("/Encoding"):]
	fields := bytes.Fields(data[:min(len(data), 64)])
	if len(fields) > 0 && bytes.Equal(fields[0], []byte("StandardEncoding")) {
		return nil, true, true
	}
	names = new([256]string)
	found := false
	// Each entry is "dup <code> /<name> put". The tokens are read one
	// by one, so that line breaks and comments between them do no harm.
	tokens := bytes.Fields(data)
	for k := 0; k+3 < len(tokens); k++ {
		if bytes.Equal(tokens[k], []byte("def")) && found {
			break // the end of the array's definition
		}
		if !bytes.Equal(tokens[k], []byte("dup")) || !bytes.Equal(tokens[k+3], []byte("put")) || len(tokens[k+2]) < 2 || tokens[k+2][0] != '/' {
			continue
		}
		code, valid := 0, len(tokens[k+1]) > 0 && len(tokens[k+1]) <= 3
		for _, c := range tokens[k+1] {
			if c < '0' || c > '9' {
				valid = false
				break
			}
			code = code*10 + int(c-'0')
		}
		if valid && code < 256 {
			names[code] = string(tokens[k+2][1:])
			found = true
		}
		k += 3
	}
	return names, false, found
}
