package gofastpdf

import (
	"bytes"
	"image/png"
	"strings"
	"testing"

	"github.com/AOShei/go-fast-pdf/internal/file"
	"github.com/AOShei/go-fast-pdf/internal/pdftest"
)

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
		kind := kindOfImage(fl, imageSrc{v: xobj})
		if !kind.samples {
			return
		}
		const limit = 1 << 22
		out, decoded, err := imageData(fl, imageSrc{v: xobj}, kind, limit)
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
		w, _ := fl.Resolve(xobj.Get("Width")).Integer()
		h, _ := fl.Resolve(xobj.Get("Height")).Integer()
		if r := got.Bounds(); int64(r.Dx()) != w || int64(r.Dy()) != h {
			t.Fatalf("%v, want %d by %d", r, w, h)
		}
	})
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
		_, _, err = imageData(fl, src, kindOfImage(fl, src), 1<<24)
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
	kind := kindOfImage(fl, src)
	for _, c := range []struct {
		limit int64
		ok    bool
	}{{50, false}, {100, false}, {104, false}, {108, true}, {1 << 20, true}} { // 100 for the function, 4 of samples, 4 of pixels
		_, decoded, err := imageData(fl, src, kind, c.limit)
		if (err == nil) != c.ok || decoded > c.limit+4 {
			t.Errorf("limit %d: %d decoded, %v", c.limit, decoded, err)
		}
	}
}
