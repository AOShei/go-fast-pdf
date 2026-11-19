package file

import (
	"bytes"
	"compress/lzw"
	"encoding/ascii85"
	"encoding/hex"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"strings"
	"testing"

	gofastcore "github.com/AOShei/go-fast-core"
	"github.com/AOShei/go-fast-pdf/internal/pdftest"
)

// Encoders, written here so that the decoders are checked against an
// independent implementation.

func encodeASCII85(data []byte) []byte {
	var buf bytes.Buffer
	w := ascii85.NewEncoder(&buf)
	w.Write(data)
	w.Close()
	buf.WriteString("~>")
	return buf.Bytes()
}

func encodeRunLength(data []byte) []byte {
	var out []byte
	for i := 0; i < len(data); {
		// A run of equal bytes, or a stretch of differing ones.
		run := 1
		for i+run < len(data) && run < 128 && data[i+run] == data[i] {
			run++
		}
		if run >= 2 {
			out = append(out, byte(257-run), data[i])
			i += run
			continue
		}
		n := 1
		for i+n < len(data) && n < 128 && (i+n+1 >= len(data) || data[i+n] != data[i+n+1]) {
			n++
		}
		out = append(out, byte(n-1))
		out = append(out, data[i:i+n]...)
		i += n
	}
	return append(out, 128)
}

// encodeLZW uses compress/lzw, which implements the variant without the
// early code-size change (/EarlyChange 0).
func encodeLZW(data []byte) []byte {
	var buf bytes.Buffer
	w := lzw.NewWriter(&buf, lzw.MSB, 8)
	w.Write(data)
	w.Close()
	return buf.Bytes()
}

// samples returns inputs of several kinds and sizes.
func samples() map[string][]byte {
	rng := rand.New(rand.NewPCG(1, 2))
	random := make([]byte, 70000)
	for i := range random {
		random[i] = byte(rng.Uint32())
	}
	var text strings.Builder
	for i := range 3000 {
		fmt.Fprintf(&text, "line %d of some repetitive text. ", i)
	}
	return map[string][]byte{
		"empty":      nil,
		"one byte":   {0x41},
		"two bytes":  {0, 0xFF},
		"three":      []byte("abc"),
		"four zeros": {0, 0, 0, 0},
		"five":       []byte("hello"),
		"runs":       bytes.Repeat([]byte{7}, 1000),
		"mixed runs": append(bytes.Repeat([]byte("ab"), 300), bytes.Repeat([]byte{0}, 700)...),
		"all bytes": func() []byte {
			b := make([]byte, 256)
			for i := range b {
				b[i] = byte(i)
			}
			return b
		}(),
		"text":        []byte(text.String()),
		"random":      random,
		"random tail": random[:4097],
	}
}

func TestFilterRoundTrips(t *testing.T) {
	type filter struct {
		encode func([]byte) []byte
		decode func(dst, src []byte, limit int64) ([]byte, error)
	}
	filters := map[string]filter{
		"ASCIIHex":  {func(d []byte) []byte { return []byte(hex.EncodeToString(d) + ">") }, asciiHexDecode},
		"ASCII85":   {encodeASCII85, ascii85Decode},
		"RunLength": {encodeRunLength, runLengthDecode},
		"LZW":       {encodeLZW, func(dst, src []byte, limit int64) ([]byte, error) { return lzwDecode(dst, src, limit, false) }},
	}
	for fname, f := range filters {
		for sname, data := range samples() {
			enc := f.encode(data)
			got, err := f.decode(nil, enc, 1<<30)
			if err != nil || !bytes.Equal(got, data) {
				t.Errorf("%s, %s: %d bytes back from %d, error %v", fname, sname, len(got), len(data), err)
				continue
			}
			// Appending keeps the prefix.
			if got, _ := f.decode([]byte("head"), enc, 1<<30); !bytes.Equal(got, append([]byte("head"), data...)) {
				t.Errorf("%s, %s: appending changed the result", fname, sname)
			}
			// A limit below the size gives exactly the first limit bytes.
			if len(data) > 10 {
				limit := int64(len(data) / 2)
				got, err := f.decode(nil, enc, limit)
				if !errors.Is(err, gofastcore.ErrLimitExceeded) || !bytes.Equal(got, data[:limit]) {
					t.Errorf("%s, %s, limit %d: %d bytes, error %v", fname, sname, limit, len(got), err)
				}
				// A limit equal to the size is not exceeded.
				if got, err := f.decode(nil, enc, int64(len(data))); err != nil || !bytes.Equal(got, data) {
					t.Errorf("%s, %s, limit = size: %d bytes, error %v", fname, sname, len(got), err)
				}
			}
		}
	}
}

func TestLZWReferenceVectors(t *testing.T) {
	// The example of ISO 32000-1, 7.4.4.2.
	got, err := lzwDecode(nil, []byte{0x80, 0x0B, 0x60, 0x50, 0x22, 0x0C, 0x0C, 0x85, 0x01}, 1<<20, true)
	if err != nil || string(got) != "-----A---B" {
		t.Errorf("specification example: %q, %v", got, err)
	}
	// 26,400 bytes encoded by Ghostscript's LZWEncode, which changes the
	// code size early, as PDF does by default. The data is long enough
	// to fill the table and clear it several times.
	enc, err := os.ReadFile("testdata/lzw-ghostscript.bin")
	if err != nil {
		t.Fatal(err)
	}
	var want strings.Builder
	for i := range 400 {
		fmt.Fprintf(&want, "line %04d of some repetitive text for the LZW encoder to work on. ", i)
	}
	got, err = lzwDecode(nil, enc, 1<<20, true)
	if err != nil || string(got) != want.String() {
		t.Errorf("Ghostscript stream: %d bytes, error %v; want %d bytes", len(got), err, want.Len())
	}
	// Read with the wrong setting it must not come out right.
	if got, _ := lzwDecode(nil, enc, 1<<20, false); string(got) == want.String() {
		t.Error("the early-change setting makes no difference")
	}
}

func TestFilterDamage(t *testing.T) {
	cases := []struct {
		name string
		run  func() ([]byte, error)
		want string
		fail bool
	}{
		{"hex: odd digit count", func() ([]byte, error) { return asciiHexDecode(nil, []byte("48 65 6c 6c 6f 7>"), 99) }, "Hellop", false},
		{"hex: no end marker", func() ([]byte, error) { return asciiHexDecode(nil, []byte("4865"), 99) }, "He", false},
		{"hex: data after the marker", func() ([]byte, error) { return asciiHexDecode(nil, []byte("48>65"), 99) }, "H", false},
		{"85: z and white space", func() ([]byte, error) { return ascii85Decode(nil, []byte("z \n87cUR\tDZ~>"), 99) }, "\x00\x00\x00\x00Hello", false},
		{"85: start marker", func() ([]byte, error) { return ascii85Decode(nil, []byte("<~87cURDZ~>"), 99) }, "Hello", false},
		{"85: no end marker", func() ([]byte, error) { return ascii85Decode(nil, []byte("87cURDZ"), 99) }, "Hello", false},
		{"85: invalid byte", func() ([]byte, error) { return ascii85Decode(nil, []byte("87cUR\x7fDZ~>"), 99) }, "Hell", true},
		{"85: single trailing character", func() ([]byte, error) { return ascii85Decode(nil, []byte("87cUR8~>"), 99) }, "Hell", false},
		{"run length: no end marker", func() ([]byte, error) { return runLengthDecode(nil, []byte{2, 'a', 'b', 'c', 254, 'x'}, 99) }, "abcxxx", false},
		{"run length: literal cut short", func() ([]byte, error) { return runLengthDecode(nil, []byte{9, 'a', 'b'}, 99) }, "ab", false},
		{"run length: repeat cut short", func() ([]byte, error) { return runLengthDecode(nil, []byte{0, 'a', 200}, 99) }, "a", false},
		{"lzw: undefined code", func() ([]byte, error) { return lzwDecode(nil, []byte{0x80, 0x0B, 0x7F, 0xFF}, 99, true) }, "-", true},
		{"lzw: starts with a table code", func() ([]byte, error) { return lzwDecode(nil, []byte{0x81, 0x80}, 99, true) }, "", true},
		{"lzw: no end code", func() ([]byte, error) { return lzwDecode(nil, []byte{0x80, 0x0B, 0x60, 0x50}, 99, true) }, "---", false},
	}
	for _, c := range cases {
		got, err := c.run()
		if string(got) != c.want || (err != nil) != c.fail {
			t.Errorf("%s: %q, error %v; want %q, error %v", c.name, got, err, c.want, c.fail)
		}
	}
}

// Every filter stops at its limit however much the input claims: a few
// kilobytes that expand to hundreds of megabytes if followed.
func TestFilterBombs(t *testing.T) {
	const limit = 1 << 20
	rl := bytes.Repeat([]byte{129, 0}, 1<<20)                  // 128 MB of zeros
	a85 := append(bytes.Repeat([]byte{'z'}, 64<<20), '~', '>') // 256 MB of zeros
	lz := encodeLZW(make([]byte, 256<<20))                     // 256 MB of zeros
	for name, run := range map[string]func() ([]byte, error){
		"RunLength": func() ([]byte, error) { return runLengthDecode(nil, rl, limit) },
		"ASCII85":   func() ([]byte, error) { return ascii85Decode(nil, a85, limit) },
		"LZW":       func() ([]byte, error) { return lzwDecode(nil, lz, limit, false) },
	} {
		var out []byte
		var err error
		n := pdftest.AllocBytes(func() { out, err = run() })
		if !errors.Is(err, ErrTruncated) || len(out) != limit {
			t.Errorf("%s: %d bytes, error %v", name, len(out), err)
		}
		if n > 8*limit {
			t.Errorf("%s: allocated %d MB for a 1 MB limit", name, n>>20)
		}
	}
}

// Filters on real streams: each alone, by full and abbreviated name, and
// chained, with content read through the File.
func TestFiltersOnStreams(t *testing.T) {
	text := []byte(strings.Repeat("content stream text ", 500))
	flate := pdftest.Deflate(text)
	cases := []struct {
		name, dict string
		data       []byte
	}{
		{"ASCIIHexDecode", "/Filter /ASCIIHexDecode", []byte(hex.EncodeToString(text) + ">")},
		{"AHx", "/Filter /AHx", []byte(hex.EncodeToString(text) + ">")},
		{"ASCII85Decode", "/Filter /ASCII85Decode", encodeASCII85(text)},
		{"A85", "/Filter [/A85]", encodeASCII85(text)},
		{"RunLengthDecode", "/Filter /RunLengthDecode", encodeRunLength(text)},
		{"RL", "/Filter /RL", encodeRunLength(text)},
		{"LZW without early change", "/Filter /LZWDecode /DecodeParms << /EarlyChange 0 >>", encodeLZW(text)},
		{"LZW abbreviated", "/Filter /LZW /DecodeParms << /EarlyChange 0 >>", encodeLZW(text)},
		{"Fl", "/Filter /Fl", flate},
		{"hex then flate", "/Filter [/ASCIIHexDecode /FlateDecode]", []byte(hex.EncodeToString(flate) + ">")},
		{"85 then flate", "/Filter [/ASCII85Decode /FlateDecode]", encodeASCII85(flate)},
		{"85 then LZW with parameters", "/Filter [/A85 /LZW] /DecodeParms [null << /EarlyChange 0 >>]", encodeASCII85(encodeLZW(text))},
		{"three filters", "/Filter [/AHx /A85 /Fl]", []byte(hex.EncodeToString(encodeASCII85(flate)) + ">")},
		{"identity crypt filter", "/Filter [/Crypt /FlateDecode] /DecodeParms [<< /Name /Identity >> null]", flate},
		{"crypt filter without a name", "/Filter /Crypt", text},
	}
	for _, c := range cases {
		b := doc(nil, "")
		b.Set(4, pdftest.Stream(c.dict, string(c.data)))
		f := open(t, b.Bytes())
		if got := content(t, f, 0); got != string(text) {
			t.Errorf("%s: %d bytes, want %d", c.name, len(got), len(text))
		}
	}
	for name, dict := range map[string]string{
		"image filter":       "/Filter /DCTDecode",
		"unknown filter":     "/Filter /NoSuchDecode",
		"named crypt filter": "/Filter /Crypt /DecodeParms << /Name /StdCF >>",
	} {
		b := doc(nil, "")
		b.Set(4, pdftest.Stream(dict, "data"))
		f := open(t, b.Bytes())
		if _, err := f.Decode(f.Resolve(f.Pages()[0].Dict().Get("Contents")), nil, 0); !errors.Is(err, gofastcore.ErrUnsupported) {
			t.Errorf("%s: error %v, want ErrUnsupported", name, err)
		}
	}
}

// Predictors, each checked against rows encoded here.
func TestTIFFPredictor(t *testing.T) {
	for _, c := range []struct {
		name                 string
		columns, colors, bpc int
	}{{"gray 8", 5, 1, 8}, {"rgb 8", 4, 3, 8}, {"gray 16", 3, 1, 16}, {"rgb 16", 2, 3, 16}} {
		rng := rand.New(rand.NewPCG(3, 4))
		rowLen := c.columns * c.colors * c.bpc / 8
		plain := make([]byte, rowLen*4)
		for i := range plain {
			plain[i] = byte(rng.Uint32())
		}
		enc := append([]byte{}, plain...)
		stride := c.colors * c.bpc / 8
		for r := 0; r < 4; r++ {
			row := enc[r*rowLen : (r+1)*rowLen]
			if c.bpc == 8 {
				for i := len(row) - 1; i >= stride; i-- {
					row[i] -= row[i-stride]
				}
			} else {
				for i := len(row) - 2; i >= stride; i -= 2 {
					v := uint16(row[i])<<8 | uint16(row[i+1])
					v -= uint16(row[i-stride])<<8 | uint16(row[i-stride+1])
					row[i], row[i+1] = byte(v>>8), byte(v)
				}
			}
		}
		for _, lz := range []bool{false, true} {
			dict := fmt.Sprintf("/Filter /FlateDecode /DecodeParms << /Predictor 2 /Columns %d /Colors %d /BitsPerComponent %d >>", c.columns, c.colors, c.bpc)
			data := pdftest.Deflate(enc)
			if lz {
				dict = fmt.Sprintf("/Filter /LZWDecode /DecodeParms << /EarlyChange 0 /Predictor 2 /Columns %d /Colors %d /BitsPerComponent %d >>", c.columns, c.colors, c.bpc)
				data = encodeLZW(enc)
			}
			b := doc(nil, "")
			b.Set(4, pdftest.Stream(dict, string(data)))
			f := open(t, b.Bytes())
			if got := content(t, f, 0); got != string(plain) {
				t.Errorf("%s, lzw=%v: wrong data", c.name, lz)
			}
		}
	}
	b := doc(nil, "")
	b.Set(4, pdftest.Stream("/Filter /FlateDecode /DecodeParms << /Predictor 2 /BitsPerComponent 4 >>", string(pdftest.Deflate([]byte("xx")))))
	f := open(t, b.Bytes())
	if _, err := f.Decode(f.Resolve(f.Pages()[0].Dict().Get("Contents")), nil, 0); !errors.Is(err, gofastcore.ErrUnsupported) {
		t.Errorf("4-bit TIFF predictor: error %v, want ErrUnsupported", err)
	}
}

func FuzzFilters(f *testing.F) {
	f.Add([]byte("48656c6c6f>"), uint8(0))
	f.Add(encodeASCII85([]byte("Hello, world")), uint8(1))
	f.Add(encodeRunLength([]byte("aaaaabcdeeee")), uint8(2))
	f.Add(encodeLZW([]byte("-----A---B-----A---B")), uint8(3))
	f.Add([]byte{0x80, 0x0B, 0x60, 0x50, 0x22, 0x0C, 0x0C, 0x85, 0x01}, uint8(4))
	f.Add(pdftest.Deflate([]byte("flate data flate data")), uint8(5))
	f.Fuzz(func(t *testing.T, data []byte, which uint8) {
		const limit = 1 << 16
		var out []byte
		var err error
		switch which % 6 {
		case 0:
			out, err = asciiHexDecode(nil, data, limit)
		case 1:
			out, err = ascii85Decode(nil, data, limit)
		case 2:
			out, err = runLengthDecode(nil, data, limit)
		case 3:
			out, err = lzwDecode(nil, data, limit, false)
		case 4:
			out, err = lzwDecode(nil, data, limit, true)
		case 5:
			out, err = inflate(nil, data, limit)
		}
		if len(out) > limit {
			t.Fatalf("filter %d produced %d bytes with a limit of %d", which%6, len(out), limit)
		}
		if errors.Is(err, ErrTruncated) && len(out) != limit {
			t.Fatalf("filter %d reported truncation at %d bytes", which%6, len(out))
		}
	})
}
