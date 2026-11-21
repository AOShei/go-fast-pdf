package gofastpdf

import (
	"bytes"
	"image/color"
	"image/png"
	"math/rand/v2"
	"testing"
)

// samplePNGs encodes m both ways: from its rows, and by png.Encoder.
func samplePNGs(t testing.TB, m *sampleImage) (rows, encoder []byte) {
	t.Helper()
	var a, b bytes.Buffer
	if err := writeSamplePNG(&a, m); err != nil {
		t.Fatal(err)
	}
	if err := pngEncoder.Encode(&b, m); err != nil {
		t.Fatal(err)
	}
	return a.Bytes(), b.Bytes()
}

// A palette image written from its rows is byte for byte what
// png.Encoder writes pixel by pixel: of each depth, with widths that
// end inside a byte and bits after the last pixel that are not 0, with
// palettes opaque and not, and images large enough for several IDAT
// chunks.
func TestSamplePNGIsEncoders(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	sizes := [][2]int{{1, 1}, {7, 3}, {8, 2}, {9, 5}, {13, 13}, {31, 4}, {640, 480}, {2550, 33}}
	for _, bpc := range []int{1, 2, 4, 8} {
		for _, wh := range sizes {
			for _, alpha := range []bool{false, true} {
				s := &samples{width: wh[0], height: wh[1], bpc: bpc, n: 1}
				s.data = make([]byte, s.stride()*s.height)
				for i := range s.data {
					s.data[i] = byte(r.Uint32())
				}
				if wh[0] == 640 {
					// Runs, as a scan has, and not noise only.
					for i := range s.data {
						if i%97 < 80 {
							s.data[i] = 0
						}
					}
				}
				pal := make(color.Palette, 1<<bpc)
				for i := range pal {
					a := uint8(255)
					if alpha && i%3 == 1 {
						a = uint8(r.Uint32())
					}
					pal[i] = color.NRGBA{uint8(r.Uint32()), uint8(r.Uint32()), uint8(r.Uint32()), a}
				}
				m := &sampleImage{s: s, pal: pal}
				if !m.rows() {
					t.Fatalf("bpc %d %v: not rows", bpc, wh)
				}
				a, b := samplePNGs(t, m)
				if !bytes.Equal(a, b) {
					t.Errorf("bpc %d, %dx%d, alpha %v: %d bytes from the rows, %d from png.Encoder", bpc, wh[0], wh[1], alpha, len(a), len(b))
				}
				if _, err := png.Decode(bytes.NewReader(a)); err != nil {
					t.Errorf("bpc %d %v: %v", bpc, wh, err)
				}
			}
		}
	}
}

// Samples that do not fill the image, or a palette of another depth
// than the samples, are not written from the rows.
func TestSampleImageRows(t *testing.T) {
	s := &samples{width: 9, height: 2, bpc: 1, n: 1, data: make([]byte, 3)}
	if (&sampleImage{s: s, pal: make(color.Palette, 2)}).rows() {
		t.Error("short samples: rows")
	}
	s.data = []byte{0x40, 0, 0, 0}
	pal := color.Palette{color.NRGBA{1, 2, 3, 255}, color.NRGBA{4, 5, 6, 255}}
	m := &sampleImage{s: s, pal: pal}
	if !m.rows() {
		t.Error("full samples: not rows")
	}
	if m.At(1, 0) != pal[1] || m.At(0, 0) != pal[0] || m.ColorIndexAt(1, 0) != 1 {
		t.Errorf("At: %v %v", m.At(0, 0), m.At(1, 0))
	}
	if (&sampleImage{s: s, pal: make(color.Palette, 4)}).rows() {
		t.Error("a palette of 4 for samples of 1 bit: rows")
	}
}

func FuzzSamplePNG(f *testing.F) {
	f.Add(uint8(0), uint16(9), uint16(3), []byte{0xff, 0x80, 0x01, 0x7f, 0xaa, 0x55}, []byte{0, 0, 0, 255, 255, 255, 255, 128})
	f.Add(uint8(3), uint16(2), uint16(2), []byte{1, 2, 3, 4}, []byte{1, 2, 3, 4})
	f.Fuzz(func(t *testing.T, depth uint8, w, h uint16, data, colours []byte) {
		bpc := 1 << (depth % 4)
		s := &samples{width: int(w%300) + 1, height: int(h%40) + 1, bpc: bpc, n: 1}
		need := s.stride() * s.height
		if len(data) < need {
			data = append(data, make([]byte, need-len(data))...)
		}
		s.data = data
		pal := make(color.Palette, 1<<bpc)
		for i := range pal {
			c := color.NRGBA{A: 255}
			if 4*i+3 < len(colours) {
				c = color.NRGBA{colours[4*i], colours[4*i+1], colours[4*i+2], colours[4*i+3]}
			}
			pal[i] = c
		}
		a, b := samplePNGs(t, &sampleImage{s: s, pal: pal})
		if !bytes.Equal(a, b) {
			t.Fatalf("%d bytes from the rows, %d from png.Encoder", len(a), len(b))
		}
	})
}
