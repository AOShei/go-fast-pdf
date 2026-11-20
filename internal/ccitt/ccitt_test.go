package ccitt

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// readPBM reads a raw PBM: its width, height and rows, a 1 bit black.
func readPBM(t *testing.T, path string) (w, h int, bits []byte) {
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	fields := strings.Fields(string(data[:20]))
	if fields[0] != "P4" {
		t.Fatal("not a raw PBM")
	}
	var n int
	for i, nl := 0, 0; i < len(data); i++ {
		if data[i] == '\n' {
			if nl++; nl == 2 {
				n = i + 1
				break
			}
		}
	}
	if err := json.Unmarshal([]byte("["+fields[1]+","+fields[2]+"]"), &[]*int{&w, &h}); err != nil {
		t.Fatal(err)
	}
	return w, h, data[n:]
}

// The data coded by libtiff in each way a PDF can give it decodes to
// the image it was coded from.
func TestDecode(t *testing.T) {
	w, h, want := readPBM(t, "testdata/image.pbm")
	names, _ := filepath.Glob("testdata/*.bin")
	if len(names) == 0 {
		t.Fatal("no test data")
	}
	for _, name := range names {
		base := strings.TrimSuffix(name, ".bin")
		t.Run(filepath.Base(base), func(t *testing.T) {
			data, _ := os.ReadFile(name)
			raw, _ := os.ReadFile(base + ".json")
			var p Params
			if err := json.Unmarshal(raw, &p); err != nil {
				t.Fatal(err)
			}
			if p.Columns != w {
				t.Fatalf("columns %d, image %d", p.Columns, w)
			}
			got, n, err := Decode(data, p, h)
			if err != nil || n != h {
				t.Fatalf("%d rows of %d: %v", n, h, err)
			}
			// libtiff codes the runs of 0 bits as white, and the image
			// was written with 0 bits for its black: read with BlackIs1,
			// a 1 bit is the image's white, which a PBM writes as 0.
			for i := range got {
				got[i] ^= 0xFF
			}
			stride := (w + 7) / 8
			for y := 0; y < h; y++ {
				g, e := got[y*stride:(y+1)*stride], want[y*stride:(y+1)*stride]
				// The bits past the width are not part of the image.
				last := byte(0xFF) << (stride*8 - w)
				if !bytes.Equal(g[:stride-1], e[:stride-1]) || g[stride-1]&last != e[stride-1]&last {
					t.Fatalf("row %d:\n got %08b\nwant %08b", y, g, e)
				}
			}
		})
	}
}

// No code is the start of another that can be read in its place.
func TestCodes(t *testing.T) {
	for name, set := range map[string][]runCode{
		"white": append(append([]runCode{}, whiteCodes...), longCodes...),
		"black": append(append([]runCode{}, blackCodes...), longCodes...),
		"modes": modeCodes,
	} {
		for i, a := range set {
			for j, b := range set {
				if i != j && strings.HasPrefix(b.bits, a.bits) {
					t.Errorf("%s: %s (%d) starts %s (%d)", name, a.bits, a.run, b.bits, b.run)
				}
			}
		}
	}
}

// Data that ends early gives the rows it holds, the rest white; damaged
// data stops at the damage.
func TestShortAndDamaged(t *testing.T) {
	w, h, _ := readPBM(t, "testdata/image.pbm")
	data, _ := os.ReadFile("testdata/g4.bin")
	p := Params{K: -1, Columns: w, BlackIs1: true}
	full, _, _ := Decode(data, p, h)
	stride := (w + 7) / 8

	got, n, err := Decode(data[:len(data)/2], p, h)
	if n == 0 || n >= h || len(got) != h*stride {
		t.Errorf("half the data: %d rows, %d bytes, %v", n, len(got), err)
	}
	// The rows before the last one decoded are as in the whole image;
	// the rows after it are white (0 with BlackIs1).
	if !bytes.Equal(got[:(n-1)*stride], full[:(n-1)*stride]) {
		t.Error("the rows decoded differ from the whole image's")
	}
	if bytes.Count(got[(n+1)*stride:], []byte{0}) != (h-n-1)*stride {
		t.Error("the rows not decoded are not white")
	}

	bad := append([]byte{}, data...)
	for i := len(bad) / 3; i < len(bad)/3+8; i++ {
		bad[i] = 0 // a run of zeros: no code
	}
	if _, n, err := Decode(bad, p, h); err != ErrDamaged || n >= h {
		t.Errorf("damaged data: %d rows, %v", n, err)
	}
	if _, _, err := Decode(nil, p, h); err != ErrDamaged {
		t.Errorf("no data: %v", err)
	}
	if _, _, err := Decode(data, Params{K: -1, Columns: MaxColumns + 1}, 1); err != ErrDamaged {
		t.Errorf("too wide: %v", err)
	}
}

func FuzzDecode(f *testing.F) {
	names, _ := filepath.Glob("testdata/*.bin")
	for _, name := range names {
		data, _ := os.ReadFile(name)
		f.Add(data, -1, 203, false)
		f.Add(data, 0, 203, true)
		f.Add(data, 4, 203, false)
	}
	f.Fuzz(func(t *testing.T, data []byte, k, cols int, align bool) {
		if cols <= 0 || cols > 4096 {
			return
		}
		const rows = 64
		out, n, _ := Decode(data, Params{K: k, Columns: cols, EncodedByteAlign: align, EndOfBlock: true}, rows)
		if len(out) != rows*((cols+7)/8) || n < 0 || n > rows {
			t.Fatalf("%d bytes, %d rows", len(out), n)
		}
	})
}
