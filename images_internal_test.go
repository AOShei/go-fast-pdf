package gofastpdf

import (
	"bytes"
	"image/png"
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
		kind := kindOfImage(fl, xobj)
		if !kind.samples {
			return
		}
		const limit = 1 << 22
		out, decoded, err := imageData(fl, xobj, kind, limit)
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
