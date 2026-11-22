package inflate

import (
	"bytes"
	"compress/flate"
	"compress/zlib"
	"errors"
	"fmt"
	stdAdler32 "hash/adler32"
	"io"
	"math/rand"
	"testing"

	"github.com/AOShei/go-fast-pdf/internal/pdftest"
)

// reference decompresses with compress/zlib, in the way this module did
// before it had a decompressor of its own. The error is nil, ErrLimit, or
// errDamaged for every other failure.
var errDamaged = errors.New("damaged")

func reference(dst, src []byte, limit int64) ([]byte, error) {
	zr, err := zlib.NewReader(bytes.NewReader(src))
	if err != nil {
		return dst, errDamaged
	}
	base := len(dst)
	for {
		used := int64(len(dst) - base)
		if used >= limit {
			var one [1]byte
			if n, _ := zr.Read(one[:]); n > 0 {
				return dst, ErrLimit
			}
			return dst, nil
		}
		if len(dst) == cap(dst) {
			grown := make([]byte, len(dst), 2*cap(dst)+512)
			copy(grown, dst)
			dst = grown
		}
		end := cap(dst)
		if int64(end-base) > limit {
			end = base + int(limit)
		}
		n, err := zr.Read(dst[len(dst):end])
		dst = dst[:len(dst)+n]
		if err == io.EOF {
			return dst, nil
		}
		if err != nil {
			return dst, errDamaged
		}
	}
}

// same compares one decompression with the reference: the same bytes,
// and the same kind of outcome.
func same(t testing.TB, src []byte, limit int64, prefix []byte) {
	t.Helper()
	want, wantErr := reference(append([]byte(nil), prefix...), src, limit)
	got, gotErr := Zlib(append([]byte(nil), prefix...), src, limit)
	if !bytes.Equal(got, want) {
		at := 0
		for at < len(got) && at < len(want) && got[at] == want[at] {
			at++
		}
		t.Fatalf("limit %d: %d bytes (%v), compress/zlib gives %d (%v); they differ at %d\ninput %x", limit, len(got), gotErr, len(want), wantErr, at, clip(src))
	}
	kind := func(err error) string {
		switch {
		case err == nil:
			return "no error"
		case errors.Is(err, ErrLimit):
			return "limit"
		}
		return "damaged"
	}
	if kind(gotErr) != kind(wantErr) {
		// One difference is meant. With the output exactly at the limit
		// and no more of it to come, the reference read on in a way that
		// let an error in the rest of the stream pass or not, depending
		// on how compress/flate had cut its output into pieces. Here
		// such an error is always reported.
		if int64(len(got)-len(prefix)) == limit && wantErr == nil && kind(gotErr) == "damaged" {
			return
		}
		t.Fatalf("limit %d: %v, compress/zlib: %v\ninput %x", limit, gotErr, wantErr, clip(src))
	}
}

func clip(b []byte) []byte {
	if len(b) > 256 {
		return b[:256]
	}
	return b
}

func deflated(t testing.TB, data []byte, level int) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw, err := zlib.NewWriterLevel(&buf, level)
	if err != nil {
		t.Fatal(err)
	}
	zw.Write(data)
	zw.Close()
	return buf.Bytes()
}

// samples are inputs that take the decompressor through its cases:
// literals only, matches at every small distance, runs, matches from far
// back, incompressible data, long and short.
func samples() map[string][]byte {
	rng := rand.New(rand.NewSource(1))
	text := []byte("BT /F1 10 Tf 72 720 Td (The quick brown fox jumps over the lazy dog) Tj ET\n")
	random := make([]byte, 70000)
	rng.Read(random)
	var periods []byte
	for p := 1; p <= 9; p++ {
		unit := random[p*10 : p*10+p]
		for i := 0; i < 400; i += p {
			periods = append(periods, unit...)
		}
	}
	// Words drawn unevenly, so that the code has lengths from short to
	// long, and far matches.
	words := make([][]byte, 3000)
	for i := range words {
		words[i] = random[i*5 : i*5+1+rng.Intn(9)]
	}
	var skewed []byte
	for len(skewed) < 300000 {
		skewed = append(skewed, words[int(float64(len(words))*rng.Float64()*rng.Float64()*rng.Float64())]...)
		skewed = append(skewed, ' ')
	}
	var letters []byte
	for i := 0; i < 5000; i++ {
		letters = append(letters, "eeeeeeeetttttaaaooinshrdlu"[rng.Intn(26)])
	}
	return map[string][]byte{
		"empty":    nil,
		"one byte": {'a'},
		"text":     bytes.Repeat(text, 40),
		"zeros":    make([]byte, 100000),
		"random":   random,
		"periods":  periods,
		"skewed":   skewed,
		"letters":  letters,
		"far":      append(append(append([]byte(nil), random[:300]...), make([]byte, 32768-300)...), random[:300]...),
		"too far":  append(append(append([]byte(nil), random[:300]...), random[1000:33768]...), random[:300]...),
	}
}

var levels = []int{zlib.NoCompression, zlib.BestSpeed, zlib.DefaultCompression, zlib.BestCompression, zlib.HuffmanOnly}

func TestZlibGivesWhatTheStandardLibraryGives(t *testing.T) {
	for name, data := range samples() {
		for _, level := range levels {
			src := deflated(t, data, level)
			got, err := Zlib(nil, src, 1<<30)
			if err != nil || !bytes.Equal(got, data) {
				t.Fatalf("%s at level %d: %d bytes of %d, %v", name, level, len(got), len(data), err)
			}
			same(t, src, 1<<30, nil)
			same(t, src, 1<<30, []byte("what was there before"))
		}
	}
}

// A stream that is cut off gives what was decompressed up to the cut.
func TestCutOff(t *testing.T) {
	for name, data := range samples() {
		if len(data) > 6000 {
			data = data[:6000]
		}
		for _, level := range levels {
			src := deflated(t, data, level)
			step := 1
			if len(src) > 3000 {
				step = 7
			}
			for cut := 0; cut < len(src); cut += step {
				same(t, src[:cut], 1<<30, nil)
			}
			_ = name
		}
	}
}

// A stream with a wrong bit gives what was decompressed before the
// damage showed.
func TestDamaged(t *testing.T) {
	for _, data := range samples() {
		if len(data) > 1500 {
			data = data[:1500]
		}
		for _, level := range levels {
			src := deflated(t, data, level)
			if len(src) > 700 {
				src = src[:700]
			}
			for bit := 0; bit < 8*len(src); bit++ {
				damaged := append([]byte(nil), src...)
				damaged[bit/8] ^= 1 << (bit % 8)
				same(t, damaged, 1<<30, nil)
			}
		}
	}
}

// The limit cuts the output at exactly that many bytes.
func TestLimit(t *testing.T) {
	for name, data := range samples() {
		for _, level := range levels {
			src := deflated(t, data, level)
			limits := []int64{0, 1, 2, 3, 100, 511, 512, 513, 1000, int64(len(data)) - 1, int64(len(data)), int64(len(data)) + 1}
			if len(data) < 3000 {
				limits = limits[:0]
				for l := int64(0); l <= int64(len(data))+2; l++ {
					limits = append(limits, l)
				}
			}
			for _, limit := range limits {
				if limit < 0 {
					continue
				}
				same(t, src, limit, nil)
				same(t, src, limit, []byte("before"))
				got, err := Zlib(nil, src, limit)
				if int64(len(got)) > limit {
					t.Fatalf("%s: %d bytes with a limit of %d", name, len(got), limit)
				}
				if (limit < int64(len(data))) != errors.Is(err, ErrLimit) {
					t.Fatalf("%s: limit %d of %d bytes: %v", name, limit, len(data), err)
				}
			}
		}
	}
}

// The output is appended: what dst holds stays, a match cannot reach
// into it, and spare room in dst is used.
func TestAppends(t *testing.T) {
	data := samples()["text"]
	src := deflated(t, data, zlib.DefaultCompression)
	dst := make([]byte, 5, 5+len(data))
	copy(dst, "12345")
	got, err := Zlib(dst, src, 1<<30)
	if err != nil || string(got[:5]) != "12345" || !bytes.Equal(got[5:], data) {
		t.Fatalf("%d bytes, %v", len(got), err)
	}
	if &got[0] != &dst[0] {
		t.Error("the room in dst was not used")
	}
	// A stream whose first match reaches back before its own start: a
	// fixed block with the literal "a" and then length 3 at distance 2.
	var raw bytes.Buffer
	fw, _ := flate.NewWriter(&raw, flate.BestCompression)
	fw.Write([]byte("abcabcabcabc"))
	fw.Close()
	stream := append([]byte{0x78, 0x9c}, raw.Bytes()...)
	if _, err := Zlib([]byte("abcabc"), stream, 1<<30); !errors.Is(err, ErrChecksum) && !errors.Is(err, ErrShort) {
		t.Fatalf("a raw stream without checksum: %v", err)
	}
	for _, bad := range [][]byte{
		{0x78, 0x9c, 0x4b, 0x04, 0x12, 0x00}, // "a", then three bytes from two back
	} {
		got, err := Zlib([]byte("abcabc"), bad, 1<<30)
		if !errors.Is(err, ErrCorrupt) || string(got) != "abcabca" {
			t.Errorf("%x: %q, %v", bad, got, err)
		}
		same(t, bad, 1<<30, []byte("abcabc"))
	}
}

func TestHeader(t *testing.T) {
	for _, c := range []struct {
		src  []byte
		want error
	}{
		{nil, ErrShort},
		{[]byte{0x78}, ErrShort},
		{[]byte{0x78, 0x9d}, ErrHeader},                               // the check bits
		{[]byte{0x79, 0x9c}, ErrHeader},                               // not deflate
		{[]byte{0x88, 0x1c}, ErrHeader},                               // a window larger than 32 KiB
		{[]byte{0x78, 0xbb, 0, 0, 0, 2, 3, 0, 0, 0, 0, 1}, ErrHeader}, // a dictionary that is not here
		{[]byte{0x78, 0xbb, 0, 0}, ErrShort},
		{[]byte{0x78, 0x9c}, ErrShort},
		{[]byte{0x78, 0x9c, 0x07}, ErrCorrupt}, // a block of a kind that does not exist
		{[]byte{0x78, 0x9c, 0x03, 0x00}, ErrShort},
		{[]byte{0x78, 0x9c, 0x03, 0x00, 0, 0, 0, 2}, ErrChecksum},
		{[]byte{0x78, 0x9c, 0x03, 0x00, 0, 0, 0, 1}, nil},
		{[]byte{0x78, 0xbb, 0, 0, 0, 1, 0x03, 0x00, 0, 0, 0, 1}, nil},
	} {
		got, err := Zlib(nil, c.src, 100)
		if !errors.Is(err, c.want) || err != nil && c.want == nil || len(got) != 0 {
			t.Errorf("%x: %v, want %v", c.src, err, c.want)
		}
		same(t, c.src, 100, nil)
	}
}

// Decompression makes one allocation, the output, when the size was
// guessed right, and none into room that is given.
func TestAllocations(t *testing.T) {
	if pdftest.RaceEnabled {
		// Under the race detector a pool forgets some of what is put
		// into it, on purpose, and the tables are made again.
		t.Skip("the pool of tables does not keep them under the race detector")
	}
	data := samples()["text"]
	src := deflated(t, data, zlib.DefaultCompression)
	Zlib(nil, src, 1<<30)
	room := make([]byte, 0, len(data)+fastRoom)
	if n := testing.AllocsPerRun(100, func() { Zlib(room, src, 1<<30) }); n != 0 {
		t.Errorf("%v allocations into room that was given", n)
	}
}

func FuzzZlib(f *testing.F) {
	for _, data := range samples() {
		if len(data) > 2000 {
			data = data[:2000]
		}
		for _, level := range levels {
			f.Add(deflated(f, data, level), int64(1<<20))
			f.Add(deflated(f, data, level), int64(len(data)/2))
		}
	}
	f.Fuzz(func(t *testing.T, src []byte, limit int64) {
		if limit < 0 || limit > 1<<24 {
			limit = 1 << 24
		}
		same(t, src, limit, nil)
		// The same bytes as a raw deflate stream behind a valid header,
		// so that the mutations reach the blocks.
		same(t, append([]byte{0x78, 0x9c}, src...), limit, []byte("before"))
	})
}

func BenchmarkZlib(b *testing.B) {
	for _, name := range []string{"text", "skewed", "letters", "random"} {
		data := samples()[name]
		src := deflated(b, data, zlib.DefaultCompression)
		b.Run(name+"/inflate", func(b *testing.B) {
			b.SetBytes(int64(len(data)))
			dst := make([]byte, 0, len(data)+fastRoom)
			defer pdftest.BenchInstructions(b)()
			for b.Loop() {
				if _, err := Zlib(dst, src, 1<<30); err != nil {
					b.Fatal(err)
				}
			}
		})
		b.Run(name+"/compress-zlib", func(b *testing.B) {
			b.SetBytes(int64(len(data)))
			dst := make([]byte, 0, len(data)+fastRoom)
			zr, _ := zlib.NewReader(bytes.NewReader(src))
			defer pdftest.BenchInstructions(b)()
			for b.Loop() {
				zr.(zlib.Resetter).Reset(bytes.NewReader(src), nil)
				out := dst[:cap(dst)]
				n, err := io.ReadFull(zr, out[:len(data)])
				if n != len(data) || err != nil {
					b.Fatal(fmt.Sprint(n, err))
				}
			}
		})
	}
}

// adler32 gives what hash/adler32 gives, for every length up to past
// two of its reductions and for bytes that make the sums largest.
func TestAdler32(t *testing.T) {
	data := make([]byte, 3*adlerMax+40)
	for i := range data {
		data[i] = byte(i*7 + i>>8)
	}
	full := bytes.Repeat([]byte{0xff}, len(data))
	for _, d := range [][]byte{data, full} {
		for n := 0; n <= len(d); n += 1 + n/64 {
			if got, want := adler32(d[:n]), stdAdler32.Checksum(d[:n]); got != want {
				t.Fatalf("%d bytes: %08x, want %08x", n, got, want)
			}
		}
	}
}

func FuzzAdler32(f *testing.F) {
	f.Add([]byte("Wikipedia"))
	f.Add(bytes.Repeat([]byte{0xff}, 2*adlerMax+17))
	f.Fuzz(func(t *testing.T, p []byte) {
		if got, want := adler32(p), stdAdler32.Checksum(p); got != want {
			t.Fatalf("%08x, want %08x", got, want)
		}
	})
}

func BenchmarkAdler32(b *testing.B) {
	data := samples()["text"]
	b.Run("inflate", func(b *testing.B) {
		b.SetBytes(int64(len(data)))
		for b.Loop() {
			adler32(data)
		}
	})
	b.Run("hash-adler32", func(b *testing.B) {
		b.SetBytes(int64(len(data)))
		for b.Loop() {
			stdAdler32.Checksum(data)
		}
	})
}
