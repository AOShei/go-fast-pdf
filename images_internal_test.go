package gofastpdf

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image/png"
	"math/rand/v2"
	"strings"
	"testing"

	gofastcore "github.com/AOShei/go-fast-core"
	"github.com/AOShei/go-fast-pdf/internal/file"
	"github.com/AOShei/go-fast-pdf/internal/function"
	"github.com/AOShei/go-fast-pdf/internal/pdftest"
	"github.com/AOShei/go-fast-pdf/internal/syntax"
)

// testKind and testData are kindOfImage and imageData for one image read
// by itself.
func testKind(fl *file.File, src imageSrc) imageKind {
	return kindOfImage(fl, function.NewReader(fl), src)
}

func testData(fl *file.File, src imageSrc, kind imageKind, limit int64) ([]byte, int64, error) {
	work := maxFunctionWork
	return imageData(fl, function.NewReader(fl), src, kind, limit, &work)
}

// The data of an image, whatever its dictionary and samples say: no
// panic, nothing past the limit, and a PNG of the image's size.
func FuzzImageData(f *testing.F) {
	const img = "/Type /XObject /Subtype /Image "
	for _, s := range [][4]string{
		{"/Width 4 /Height 1 /ColorSpace /DeviceGray /BitsPerComponent 2", "\x1B", "", ""},
		{"/Width 2 /Height 1 /ColorSpace /DeviceRGB /BitsPerComponent 16 /Decode [1 0 0 1 0.5 0.5]", "\xFF\xFF\x80\x00\x00\x00\x01\x02\x03\x04\x05\x06", "", ""},
		{"/Width 3 /Height 1 /ColorSpace /DeviceCMYK /BitsPerComponent 8", "\x00\x00\x00\x00\xFF\x00\x00\x00\x00\x00\x00\xFF", "", ""},
		{"/Width 3 /Height 1 /ColorSpace [/Indexed /DeviceRGB 2 <FF000000FF000000FF>] /BitsPerComponent 2", "\x18", "", ""},
		{"/Width 2 /Height 1 /ColorSpace [/Indexed [/ICCBased 2 0 R] 1 2 0 R] /BitsPerComponent 8", "\x00\x01", "/N 4", "\x00\x00\x00\x00\xFF\x00\xFF\x00"},
		{"/Width 2 /Height 1 /ColorSpace [/Lab << /Range [-128 127 -128 127] >>] /BitsPerComponent 8", "\xFF\x80\x80\x00\x80\x80", "", ""},
		{"/Width 2 /Height 1 /ColorSpace /DeviceRGB /BitsPerComponent 8 /SMask 2 0 R", "\xFF\x00\x00\xFF\x00\x00", img + "/Width 1 /Height 3 /ColorSpace /DeviceGray /BitsPerComponent 4", "\xF0\x0F"},
		{"/Width 2 /Height 2 /ColorSpace /DeviceGray /BitsPerComponent 8 /Mask 2 0 R", "\x00\x00\x00\x00", img + "/Width 3 /Height 1 /ImageMask true /Decode [1 0]", "\x40"},
		{"/Width 3 /Height 1 /ColorSpace /DeviceGray /BitsPerComponent 8 /Mask [40 60]", "\x0A\x32\xC8", "", ""},
		{"/Width 8 /Height 1 /ImageMask true", "\x0F", "", ""},
		{"/Width 2 /Height 2 /ColorSpace [/DeviceN [/A /B] /DeviceRGB 2 0 R] /BitsPerComponent 4", "\x0F\xF0\x12\x34", "/FunctionType 4 /Domain [0 1 0 1] /Range [0 1 0 1 0 1]", "{ 2 copy add 2 div }"},
		{"/Width 2 /Height 1 /ColorSpace [/Separation /S /DeviceGray 2 0 R] /BitsPerComponent 8", "\x00\xFF", "/FunctionType 0 /Domain [0 1] /Range [0 1] /Size [3] /BitsPerSample 8", "\x00\x80\xFF"},
		{"/Width 16 /Height 4 /ColorSpace /DeviceGray /BitsPerComponent 1 /Filter /CCITTFaxDecode /DecodeParms << /K -1 /Columns 16 >>", "\x26\xA0\x00\x10\x01", "", ""},
		{"/Width 16 /Height 4 /ImageMask true /Filter /CCITTFaxDecode /DecodeParms << /K 2 /EncodedByteAlign true /BlackIs1 true >>", "\x00\x1B\x59\x80", "", ""},
		{"/Width 4 /Height 4 /ColorSpace /DeviceGray /BitsPerComponent 8 /SMask 2 0 R", "0123456789abcdef", img + "/Width 4 /Height 4 /ColorSpace /DeviceGray /BitsPerComponent 8 /Filter /DCTDecode", "\xFF\xD8\xFF\xC0\x00\x0B\x08\xFF\xFF\xFF\xFF\x01\x01\x11\x00"},
	} {
		f.Add(s[0], []byte(s[1]), s[2], []byte(s[3]))
	}
	f.Fuzz(func(t *testing.T, dict string, data []byte, dict2 string, data2 []byte) {
		for _, s := range []string{dict, dict2} {
			if bytes.Contains([]byte(s), []byte("stream")) || bytes.Contains([]byte(s), []byte("endobj")) {
				t.Skip()
			}
		}
		b := pdftest.New()
		b.AddStream(img+dict, data)
		b.AddStream(dict2, data2)
		b.Add("<< /Type /Catalog /Pages 4 0 R >>")
		b.Add("<< /Type /Pages /Kids [] /Count 0 >>")
		b.Root = 3
		fl, err := file.Open(b.Bytes(), file.Limits{})
		if err != nil {
			return
		}
		xobj := fl.Object(1)
		kind := testKind(fl, imageSrc{v: xobj})
		if !kind.samples {
			return
		}
		const limit = 1 << 22
		out, decoded, err := testData(fl, imageSrc{v: xobj}, kind, limit)
		if decoded > 2*limit {
			t.Fatalf("%d bytes decoded, limit %d", decoded, limit)
		}
		if err != nil {
			return
		}
		got, err := png.Decode(bytes.NewReader(out))
		if err != nil {
			t.Fatalf("not a PNG: %v", err)
		}
		if old := byPixel(t, fl, xobj, kind, limit); !bytes.Equal(out, old) {
			t.Fatalf("%d bytes by rows, %d a sample at a time", len(out), len(old))
		}
		w, _ := fl.Resolve(xobj.Get("Width")).Integer()
		h, _ := fl.Resolve(xobj.Get("Height")).Integer()
		if r := got.Bounds(); int64(r.Dx()) != w || int64(r.Dy()) != h {
			t.Fatalf("%v, want %d by %d", r, w, h)
		}
	})
}

// byPixel is the data of the image as raster made it before it took the
// samples by their rows.
func byPixel(t *testing.T, fl *file.File, xobj syntax.Value, kind imageKind, limit int64) []byte {
	t.Helper()
	pixelByPixel = true
	defer func() { pixelByPixel = false }()
	out, _, err := testData(fl, imageSrc{v: xobj}, kind, limit)
	if err != nil {
		t.Fatalf("a sample at a time: %v", err)
	}
	return out
}

// The rows of samples give the bytes that the samples one at a time give:
// gray, RGB and CMYK of 8 bits, with a soft mask and without, palettes of
// each depth, stencils, widths that end inside a byte.
func TestRasterRowsArePixels(t *testing.T) {
	const img = "/Type /XObject /Subtype /Image "
	r := rand.New(rand.NewPCG(3, 4))
	noise := func(n int) []byte {
		b := make([]byte, n)
		for i := range b {
			b[i] = byte(r.Uint32())
		}
		return b
	}
	for _, c := range []struct {
		space string
		n     int
		bpc   int
	}{
		{"/DeviceGray", 1, 8}, {"/DeviceRGB", 3, 8}, {"/DeviceCMYK", 4, 8},
		{"/DeviceGray", 1, 1}, {"/DeviceGray", 1, 2}, {"/DeviceGray", 1, 4},
		{"[/Indexed /DeviceRGB 3 <FF000000FF000000FF808080>]", 1, 2},
		{"[/Indexed /DeviceRGB 1 <FF000000FF00>]", 1, 8},
		{"", 1, 1}, // a stencil
	} {
		for _, wh := range [][2]int{{1, 1}, {7, 3}, {33, 17}, {300, 40}} {
			for _, mask := range []bool{false, true} {
				w, h := wh[0], wh[1]
				dict := fmt.Sprintf("/Width %d /Height %d /BitsPerComponent %d /ColorSpace %s", w, h, c.bpc, c.space)
				if c.space == "" {
					dict = fmt.Sprintf("/Width %d /Height %d /ImageMask true", w, h)
				}
				if mask {
					dict += " /SMask 2 0 R"
				}
				b := pdftest.New()
				b.AddStream(img+dict, noise(h*((w*c.n*c.bpc+7)/8)))
				b.AddStream(fmt.Sprintf(img+"/Width %d /Height %d /ColorSpace /DeviceGray /BitsPerComponent 8", w, h), noise(w*h))
				b.Add("<< /Type /Catalog /Pages 4 0 R >>")
				b.Add("<< /Type /Pages /Kids [] /Count 0 >>")
				b.Root = 3
				fl, err := file.Open(b.Bytes(), file.Limits{})
				if err != nil {
					t.Fatal(err)
				}
				xobj := fl.Object(1)
				kind := testKind(fl, imageSrc{v: xobj})
				if !kind.samples {
					t.Fatalf("%s: not samples", dict)
				}
				out, _, err := testData(fl, imageSrc{v: xobj}, kind, 1<<24)
				if err != nil {
					t.Fatalf("%s: %v", dict, err)
				}
				if old := byPixel(t, fl, xobj, kind, 1<<24); !bytes.Equal(out, old) {
					t.Errorf("%s: %d bytes by rows, %d a sample at a time", dict, len(out), len(old))
				}
			}
		}
	}
}

// The work of a colour space's function over an image is bounded: of one
// ink, each sample's colour is made once; of more, colours are kept by
// their samples, and an image of more colours than that is bounded by
// maxFunctionWork.
func TestFunctionWork(t *testing.T) {
	defer func(w int) { maxFunctionWork = w }(maxFunctionWork)
	const img = "/Type /XObject /Subtype /Image "
	fn := pdftest.Stream("/FunctionType 4 /Domain [0 1 0 1] /Range [0 1 0 1 0 1]", "{ 2 copy add 2 div }")
	data := make([]byte, 2*64*64) // every pair of samples there is, of 6 bits each
	for i := range 64 * 64 {
		data[2*i], data[2*i+1] = byte(i%64*4), byte(i/64*4)
	}
	for _, c := range []struct {
		dict, data string
		work       int
		ok         bool
	}{
		{"/Width 64 /Height 64 /ColorSpace [/DeviceN [/A /B] /DeviceRGB 2 0 R] /BitsPerComponent 8", string(data), 64 * 64 * 20, true},
		{"/Width 64 /Height 64 /ColorSpace [/DeviceN [/A /B] /DeviceRGB 2 0 R] /BitsPerComponent 8", string(data), 64 * 64, false},
		// The same colours, row after row: kept, made once.
		{"/Width 64 /Height 64 /ColorSpace [/DeviceN [/A /B] /DeviceRGB 2 0 R] /BitsPerComponent 8", strings.Repeat(string(data[:128]), 64), 64 * 20, true},
	} {
		maxFunctionWork = c.work
		b := pdftest.New()
		b.AddStream(img+c.dict, []byte(c.data))
		b.Add(fn)
		fl, err := file.Open(pdftest.Doc(pdftest.DocOptions{Base: b}, pdftest.PageSpec{}).Bytes(), file.Limits{})
		if err != nil {
			t.Fatal(err)
		}
		src := imageSrc{v: fl.Object(1)}
		_, _, err = testData(fl, src, testKind(fl, src), 1<<24)
		if (err == nil) != c.ok || err != nil && !strings.Contains(err.Error(), "too long") {
			t.Errorf("work %d: %v", c.work, err)
		}
	}
}

// The samples of a colour space's function count against the image's
// limit: what they leave is what the image's samples and pixels may take.
func TestFunctionSamplesLimit(t *testing.T) {
	const img = "/Type /XObject /Subtype /Image "
	b := pdftest.New()
	b.AddStream(img+"/Width 4 /Height 1 /ColorSpace [/Separation /S /DeviceGray 2 0 R] /BitsPerComponent 8", []byte("\x00\x40\x80\xFF"))
	b.AddStream("/FunctionType 0 /Domain [0 1] /Range [0 1] /Size [100] /BitsPerSample 8", bytes.Repeat([]byte{0x80}, 100))
	fl, err := file.Open(pdftest.Doc(pdftest.DocOptions{Base: b}, pdftest.PageSpec{}).Bytes(), file.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	src := imageSrc{v: fl.Object(1)}
	kind := testKind(fl, src)
	for _, c := range []struct {
		limit int64
		ok    bool
	}{{50, false}, {100, false}, {104, false}, {108, true}, {1 << 20, true}} { // 100 for the function, 4 of samples, 4 of pixels
		_, decoded, err := testData(fl, src, kind, c.limit)
		if (err == nil) != c.ok || err != nil && !errors.Is(err, file.ErrTruncated) || decoded > c.limit+4 {
			t.Errorf("limit %d: %d decoded, %v", c.limit, decoded, err)
		}
	}
}

// Of one ink, the colour of a sample is made when a pixel first needs it,
// and each colour made takes its work: an image of 16 bits makes the
// colours of its samples, not of the 65,536 there could be.
func TestFunctionWorkOneInk(t *testing.T) {
	defer func(w int) { maxFunctionWork = w }(maxFunctionWork)
	const img = "/Type /XObject /Subtype /Image "
	// Of cost 5: one input, one output, three operators.
	const cost = 5
	fn := pdftest.Stream("/FunctionType 4 /Domain [0 1] /Range [0 1]", "{ 1 exch sub }")
	for _, c := range []struct {
		dict, data string
		work       int
		ok         bool
	}{
		{"/Width 4 /Height 1 /ColorSpace [/Separation /S /DeviceGray 2 0 R] /BitsPerComponent 8", "\x00\xFF\x00\xFF", 2 * cost, true},
		{"/Width 4 /Height 1 /ColorSpace [/Separation /S /DeviceGray 2 0 R] /BitsPerComponent 8", "\x00\xFF\x00\xFF", 2*cost - 1, false},
		{"/Width 2 /Height 1 /ColorSpace [/Separation /S /DeviceGray 2 0 R] /BitsPerComponent 16", "\x00\x00\xFF\xFF", 2 * cost, true},
		{"/Width 2 /Height 1 /ColorSpace [/Separation /S /DeviceGray 2 0 R] /BitsPerComponent 16", "\x00\x00\xFF\xFF", 2*cost - 1, false},
		// With a mask, the pixels are not of a palette.
		{"/Width 4 /Height 1 /ColorSpace [/Separation /S /DeviceGray 2 0 R] /BitsPerComponent 8 /Mask [0 0]", "\x00\xFF\x00\xFF", 2*cost - 1, false},
	} {
		maxFunctionWork = c.work
		b := pdftest.New()
		b.AddStream(img+c.dict, []byte(c.data))
		b.Add(fn)
		fl, err := file.Open(pdftest.Doc(pdftest.DocOptions{Base: b}, pdftest.PageSpec{}).Bytes(), file.Limits{})
		if err != nil {
			t.Fatal(err)
		}
		src := imageSrc{v: fl.Object(1)}
		out, _, err := testData(fl, src, testKind(fl, src), 1<<20)
		if (err == nil) != c.ok || err != nil && !strings.Contains(err.Error(), "too long") {
			t.Errorf("%s, work %d: %v", c.dict, c.work, err)
			continue
		}
		if err != nil {
			continue
		}
		got, err := png.Decode(bytes.NewReader(out))
		if err != nil {
			t.Fatal(err)
		}
		// 0 is no ink, white; the function turns it to 1, white in gray.
		if r, _, _, _ := got.At(0, 0).RGBA(); r>>8 != 255 {
			t.Errorf("%s: pixel 0 is %v", c.dict, got.At(0, 0))
		}
		if r, _, _, _ := got.At(1, 0).RGBA(); r>>8 != 0 {
			t.Errorf("%s: pixel 1 is %v", c.dict, got.At(1, 0))
		}
	}
}

// The work of the functions of colour spaces is bounded over a reading of
// a document too: an image past it is not delivered, and says why.
func TestFunctionWorkAll(t *testing.T) {
	defer func(w int) { maxFunctionWorkAll = w }(maxFunctionWorkAll)
	const img = "/Type /XObject /Subtype /Image /Width 1 /Height 1 /BitsPerComponent 8 /ColorSpace [/Separation /S /DeviceGray 3 0 R]"
	b := pdftest.New()
	b.AddStream(img, []byte{0x00})
	b.AddStream(img, []byte{0xFF})
	b.Add(pdftest.Stream("/FunctionType 4 /Domain [0 1] /Range [0 1]", "{ 1 exch sub }"))
	data := pdftest.Doc(pdftest.DocOptions{Base: b}, pdftest.PageSpec{
		Content:   "q 10 0 0 10 0 0 cm /A Do Q q 10 0 0 10 20 0 cm /B Do Q q 10 0 0 10 40 0 cm /A Do Q",
		Resources: "/XObject << /A 1 0 R /B 2 0 R >>",
	}).Bytes()
	for _, c := range []struct {
		work      int
		delivered int
	}{{10, 2}, {9, 1}, {5, 1}, {4, 0}} { // of 5 a colour
		maxFunctionWorkAll = c.work
		doc, err := Extract(context.Background(), bytes.NewReader(data), int64(len(data)), &gofastcore.Options{Images: gofastcore.ImagesData})
		if err != nil {
			t.Fatal(err)
		}
		delivered := 0
		for _, bl := range doc.Parts[0].Blocks {
			if bl.Image != nil && len(bl.Image.Data) > 0 {
				delivered++
			}
		}
		warned := doc.Parts[0].Has(gofastcore.WarnImageUnsupportedEncoding)
		if delivered != c.delivered || warned != (c.delivered < 2) {
			t.Errorf("work %d: %d delivered, warned %v; %v", c.work, delivered, warned, doc.Parts[0].Warnings)
		}
	}
}
