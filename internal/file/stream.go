package file

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"sync"

	gofastcore "github.com/AOShei/go-fast-core"
	zlib "github.com/AOShei/go-fast-pdf/internal/inflate"
	"github.com/AOShei/go-fast-pdf/internal/syntax"
)

// ErrTruncated is returned with the data read so far when a stream
// decodes to more than the limit.
var ErrTruncated = fmt.Errorf("%w: decoded stream is larger than the limit", gofastcore.ErrLimitExceeded)

// rawStream returns the bytes of a stream as stored in the file, before
// decryption and filters. The slice points into the input.
func (f *File) rawStream(stm syntax.Value, depth int) ([]byte, error) {
	start := stm.N
	if stm.Kind != syntax.Stream || start < 0 || start > int64(len(f.data)) {
		return nil, errors.New("not a stream")
	}
	length, ok := f.resolve(stm.Get("Length"), depth+1).Integer()
	declared := ok && length >= 0 && start+length <= int64(len(f.data))
	if declared {
		// The length is right if endstream follows the data.
		if end := syntax.SkipSpace(f.data, int(start+length)); syntax.HasPrefixAt(f.data, end, "endstream") {
			return f.data[start : start+length], nil
		}
	}
	// /Length is missing, not a number, or wrong: the stream ends where
	// endstream says it does.
	end := bytes.Index(f.data[start:], []byte("endstream"))
	if end < 0 {
		if declared {
			return f.data[start : start+length], nil // the file is cut off; nothing better is known
		}
		return nil, errors.New("stream has no usable /Length and no endstream")
	}
	raw := f.data[start : start+int64(end)]
	// The end-of-line before endstream is not part of the data.
	if n := len(raw); n > 0 && raw[n-1] == '\n' {
		raw = raw[:n-1]
	}
	if n := len(raw); n > 0 && raw[n-1] == '\r' {
		raw = raw[:n-1]
	}
	if declared {
		f.Warn(gofastcore.SeverityInfo, gofastcore.WarnInputRepaired,
			"some streams have a wrong /Length; their end was found by their endstream keyword")
	}
	return raw, nil
}

// Decode appends the decoded data of a stream to dst and returns the
// result. limit bounds the decoded size; 0 means the file's
// MaxStreamBytes.
//
// With ErrTruncated, and with an error from a damaged stream, the bytes
// decoded before the problem are returned too. The caller decides
// whether a prefix is useful.
func (f *File) Decode(stm syntax.Value, dst []byte, limit int64) ([]byte, error) {
	if limit <= 0 {
		limit = f.limits.MaxStreamBytes
	}
	return f.decode(stm, dst, limit, true, 0)
}

// DecodeImage is Decode for the data of an image. An image can end its
// chain of filters with a codec of its own: JPEG (DCTDecode), JPEG 2000
// (JPXDecode), CCITT fax or JBIG2. Those are not decoded here: the data
// is returned as that codec has it, with the filter's name in codec.
// codec is empty when the data is the image's samples.
func (f *File) DecodeImage(stm syntax.Value, dst []byte, limit int64) (data []byte, codec string, err error) {
	if limit <= 0 {
		limit = f.limits.MaxStreamBytes
	}
	data, err = f.decodeTo(stm, dst, limit, true, 0, &codec)
	return data, codec, err
}

func (f *File) decode(stm syntax.Value, dst []byte, limit int64, decrypt bool, depth int) ([]byte, error) {
	return f.decodeTo(stm, dst, limit, decrypt, depth, nil)
}

func (f *File) decodeTo(stm syntax.Value, dst []byte, limit int64, decrypt bool, depth int, codec *string) ([]byte, error) {
	raw, err := f.rawStream(stm, depth)
	if err != nil {
		return dst, err
	}
	// A stream damaged in its encryption is decrypted as far as it goes;
	// the error is returned with whatever the filters make of the rest.
	var damage error
	if decrypt && f.enc != nil && !stm.Get("Type").IsName("XRef") {
		kind, err := f.streamCipher(stm, depth)
		if err != nil {
			return dst, err
		}
		raw, damage = f.enc.decrypt(kind, raw, stm.Obj, stm.Gen)
		if damage != nil && len(raw) == 0 {
			return dst, damage
		}
	}
	return f.filter(stm, raw, dst, limit, depth, codec, damage)
}

// DecodeImageBytes is DecodeImage for the data of an inline image: raw,
// with the filters its dictionary names. It is part of a content stream,
// decrypted with it, and is not decrypted again.
func (f *File) DecodeImageBytes(dict syntax.Value, raw, dst []byte, limit int64) (data []byte, codec string, err error) {
	if limit <= 0 {
		limit = f.limits.MaxStreamBytes
	}
	data, err = f.filter(dict, raw, dst, limit, 0, &codec, nil)
	return data, codec, err
}

// filter applies the filters that the dictionary of a stream names to
// its data, raw. damage is an error of decryption, returned when nothing
// worse happens.
func (f *File) filter(stm syntax.Value, raw, dst []byte, limit int64, depth int, codec *string, damage error) ([]byte, error) {
	var err error
	filters := f.resolve(stm.Get("Filter"), depth+1)
	parms := f.resolve(stm.Get("DecodeParms"), depth+1)
	var names, parmList []syntax.Value
	switch filters.Kind {
	case syntax.Null:
	case syntax.Name:
		names, parmList = []syntax.Value{filters}, []syntax.Value{parms}
	case syntax.Array:
		names = filters.List
		parmList = parms.Elems()
	default:
		return dst, fmt.Errorf("%w: /Filter is a %s", gofastcore.ErrMalformed, filters.Kind)
	}
	if len(names) == 0 {
		if int64(len(raw)) > limit {
			return append(dst, raw[:limit]...), ErrTruncated
		}
		if dst == nil {
			return raw, damage // no copy: the caller must not modify it
		}
		return append(dst, raw...), damage
	}
	if len(names) > 8 {
		return dst, fmt.Errorf("%w: %d chained filters", gofastcore.ErrMalformed, len(names))
	}

	// Each filter's output is the next one's input. Only the last writes
	// into dst.
	data := raw
	for i, name := range names {
		name = f.resolve(name, depth+1)
		var parm syntax.Value
		if i < len(parmList) {
			parm = f.resolve(parmList[i], depth+1)
		}
		last := i == len(names)-1
		var out []byte
		start := 0
		if last {
			out, start = dst, len(dst)
		}
		predict := false
		switch filterName(name) {
		case "FlateDecode":
			out, err = inflate(out, data, limit)
			predict = true
		case "LZWDecode":
			early := true
			if e, ok := f.resolve(parm.Get("EarlyChange"), depth+1).Integer(); ok && e == 0 {
				early = false
			}
			out, err = lzwDecode(out, data, limit, early)
			predict = true
		case "ASCIIHexDecode":
			out, err = asciiHexDecode(out, data, limit)
		case "ASCII85Decode":
			out, err = ascii85Decode(out, data, limit)
		case "RunLengthDecode":
			out, err = runLengthDecode(out, data, limit)
		case "Crypt":
			// Decryption is done before the filters, with the cipher
			// this filter names (streamCipher). Here the data passes
			// through. In a file that is not encrypted, only the
			// Identity filter means anything.
			if n := f.resolve(parm.Get("Name"), depth+1); f.enc == nil && !n.IsNull() && !n.IsName("Identity") {
				return dst, fmt.Errorf("%w: crypt filter /%s in a file that is not encrypted", gofastcore.ErrUnsupported, n.NameString())
			}
			out = append(out, data...)
		case "DCTDecode", "DCT", "JPXDecode", "CCITTFaxDecode", "CCF", "JBIG2Decode":
			if codec == nil || !last {
				return dst, fmt.Errorf("%w: stream filter /%s", gofastcore.ErrUnsupported, name.NameString())
			}
			// What the filters before it produced is the codec's input.
			*codec = name.NameString()
			if int64(len(data)) > limit {
				return append(dst, data[:limit]...), ErrTruncated
			}
			return append(dst, data...), damage
		default:
			return dst, fmt.Errorf("%w: stream filter /%s", gofastcore.ErrUnsupported, name.NameString())
		}
		if predict && parm.IsDict() {
			var perr error
			if out, perr = f.unpredict(out, start, parm, depth); err == nil {
				err = perr
			}
		}
		if err != nil {
			if last {
				return out, err
			}
			return dst, err
		}
		data = out
	}
	return data, damage
}

// filterName returns the full name of a stream filter. The abbreviations
// are defined for inline images only, but occur on streams and are
// accepted by other readers.
func filterName(v syntax.Value) string {
	switch name := v.NameString(); name {
	case "Fl":
		return "FlateDecode"
	case "LZW":
		return "LZWDecode"
	case "AHx":
		return "ASCIIHexDecode"
	case "A85":
		return "ASCII85Decode"
	case "RL":
		return "RunLengthDecode"
	default:
		return name
	}
}

// inflate appends the zlib-decompressed form of src to dst. It stops at
// limit bytes of output with ErrTruncated. On a damaged stream it returns
// what was decompressed before the damage, with the error.
func inflate(dst, src []byte, limit int64) ([]byte, error) {
	out, err := zlib.Zlib(dst, src, limit)
	switch {
	case err == nil:
		return out, nil
	case err == zlib.ErrLimit:
		return out, ErrTruncated
	}
	return out, fmt.Errorf("%w: %v", gofastcore.ErrMalformed, err)
}

// unpredict undoes the predictor named in a filter's parameters on
// data[start:], in place, and returns the possibly shortened slice.
func (f *File) unpredict(data []byte, start int, parm syntax.Value, depth int) ([]byte, error) {
	predictor, _ := f.resolve(parm.Get("Predictor"), depth+1).Integer()
	if predictor <= 1 {
		return data, nil
	}
	intParm := func(key string, def int64) int64 {
		if n, ok := f.resolve(parm.Get(key), depth+1).Integer(); ok && n > 0 {
			return n
		}
		return def
	}
	columns, colors, bpc := intParm("Columns", 1), intParm("Colors", 1), intParm("BitsPerComponent", 8)
	if columns > 1<<20 || colors > 32 || bpc > 16 {
		return data, fmt.Errorf("%w: predictor parameters out of range", gofastcore.ErrMalformed)
	}
	if predictor == 2 {
		return untiff(data, start, columns, colors, bpc)
	}
	if predictor < 10 {
		return data, fmt.Errorf("%w: predictor %d", gofastcore.ErrUnsupported, predictor)
	}
	bpp := int((colors*bpc + 7) / 8)            // bytes per pixel, rounded up
	rowLen := int((columns*colors*bpc + 7) / 8) // bytes per row, without the tag
	in := data[start:]
	out := in[:0]
	var prev []byte
	for len(in) >= 1 {
		tag := in[0]
		row := in[1:]
		if len(row) > rowLen {
			row = row[:rowLen]
		}
		in = in[1+len(row):]
		// Writing a row at out never overtakes reading it: the output is
		// one byte per row behind the input.
		cur := out[len(out) : len(out)+len(row)]
		for i, x := range row {
			var a, b, c byte // left, above, above-left
			if i >= bpp {
				a = cur[i-bpp]
			}
			if prev != nil && i < len(prev) {
				b = prev[i]
				if i >= bpp {
					c = prev[i-bpp]
				}
			}
			switch tag {
			case 1:
				x += a
			case 2:
				x += b
			case 3:
				x += byte((int(a) + int(b)) / 2)
			case 4:
				x += paeth(a, b, c)
			}
			cur[i] = x
		}
		out = out[:len(out)+len(row)]
		prev = cur
	}
	return data[:start+len(out)], nil
}

func paeth(a, b, c byte) byte {
	p := int(a) + int(b) - int(c)
	pa, pb, pc := abs(p-int(a)), abs(p-int(b)), abs(p-int(c))
	switch {
	case pa <= pb && pa <= pc:
		return a
	case pb <= pc:
		return b
	}
	return c
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// --- object streams ---

// objStm is a decoded object stream: the objects' text and where each
// one starts.
type objStm struct {
	data  []byte
	first int
	nums  []int64
	offs  []int32
}

// objStmCache keeps decoded object streams, up to a byte budget. Parsed
// objects are cached by the File itself, so a stream is only needed
// again for objects not read yet.
type objStmCache struct {
	mu    sync.Mutex
	stms  map[int64]*objStm
	order []int64
	bytes int64
}

// loadObjStm returns the object stream with the given cache key. get
// fetches the stream object when it is not cached.
func (f *File) loadObjStm(key int64, depth int, get func() syntax.Value) (*objStm, error) {
	c := &f.stms
	c.mu.Lock()
	if s := c.stms[key]; s != nil {
		c.mu.Unlock()
		return s, nil
	}
	c.mu.Unlock()

	stm := get()
	if stm.Kind != syntax.Stream {
		return nil, fmt.Errorf("an object stream is a %s", stm.Kind)
	}
	data, err := f.decode(stm, nil, f.limits.MaxStreamBytes, true, depth+1)
	if err != nil && len(data) == 0 {
		return nil, fmt.Errorf("object stream %d: %v", stm.Obj, err)
	}
	n, _ := f.resolve(stm.Get("N"), depth+1).Integer()
	first, _ := f.resolve(stm.Get("First"), depth+1).Integer()
	if n < 0 || first < 0 || first > int64(len(data)) || n > int64(len(data)) {
		return nil, fmt.Errorf("object stream %d has /N %d and /First %d for %d bytes", stm.Obj, n, first, len(data))
	}
	s := &objStm{data: data, first: int(first), nums: make([]int64, 0, n), offs: make([]int32, 0, n)}
	// The header is N pairs of object number and offset.
	pos := 0
	for i := int64(0); i < n; i++ {
		a, end := syntax.ParseNumber(data, syntax.SkipSpace(data, pos))
		b, end2 := syntax.ParseNumber(data, syntax.SkipSpace(data, end))
		if a.Kind != syntax.Int || b.Kind != syntax.Int || end2 > int(first) || b.N < 0 {
			break
		}
		pos = end2
		s.nums = append(s.nums, a.N)
		s.offs = append(s.offs, int32(b.N))
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.stms == nil {
		c.stms = map[int64]*objStm{}
	}
	if existing := c.stms[key]; existing != nil {
		return existing, nil
	}
	c.stms[key] = s
	c.order = append(c.order, key)
	c.bytes += int64(len(data))
	for c.bytes > f.limits.ObjStmCacheBytes && len(c.order) > 1 {
		oldest := c.order[0]
		c.order = c.order[1:]
		c.bytes -= int64(len(c.stms[oldest].data))
		delete(c.stms, oldest)
	}
	return s, nil
}

// fromObjStm reads object num, expected at index idx of object stream
// stmNum.
func (f *File) fromObjStm(stmNum, num int64, idx, depth int) (syntax.Value, error) {
	s, err := f.loadObjStm(stmNum, depth, func() syntax.Value { return f.object(stmNum, depth+1) })
	if err != nil {
		return syntax.Value{}, err
	}
	return s.object(f, num, idx)
}

// object reads object num from the stream, expected at index idx.
func (s *objStm) object(f *File, num int64, idx int) (syntax.Value, error) {
	if idx < 0 || idx >= len(s.nums) || s.nums[idx] != num {
		// The index is wrong; the header still says where the object is.
		idx = -1
		for i, n := range s.nums {
			if n == num {
				idx = i
				break
			}
		}
		if idx < 0 {
			return syntax.Value{}, fmt.Errorf("the object stream does not hold object %d", num)
		}
	}
	pos := s.first + int(s.offs[idx])
	if pos > len(s.data) {
		return syntax.Value{}, fmt.Errorf("object %d lies outside its object stream", num)
	}
	p := syntax.NewParser(s.data, pos, f.limits.MaxNesting)
	v, err := p.Object()
	if err == io.EOF || errors.Is(err, syntax.ErrSyntax) {
		return syntax.Value{}, nil
	}
	return v, err
}
