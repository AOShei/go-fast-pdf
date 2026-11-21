package gofastpdf_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"strings"
	"testing"
	"time"

	gofastcore "github.com/AOShei/go-fast-core"
	gofastpdf "github.com/AOShei/go-fast-pdf"
	"github.com/AOShei/go-fast-pdf/internal/pdftest"
)

// imageCase is an image drawn on a page by itself, and the pixels its
// data should have, row by row.
type imageCase struct {
	name string
	// dict and data make the image, the first object; more are objects
	// after it, from 2 0 R.
	dict, data string
	more       []string
	w, h       int
	want       []color.NRGBA
	// tol is how far a component can be from what is wanted.
	tol uint8
}

func gray(v ...uint8) []color.NRGBA {
	out := make([]color.NRGBA, len(v))
	for i, g := range v {
		out[i] = color.NRGBA{g, g, g, 255}
	}
	return out
}

func rgba(v ...uint8) []color.NRGBA {
	out := make([]color.NRGBA, len(v)/4)
	for i := range out {
		out[i] = color.NRGBA{v[4*i], v[4*i+1], v[4*i+2], v[4*i+3]}
	}
	return out
}

// imageOf extracts the page of one image and returns its block's image.
func imageOf(t *testing.T, objects []string) (*gofastcore.Image, *gofastcore.Part) {
	t.Helper()
	d := synthDoc{Objects: objects, Pages: []synthPage{{Content: "q 100 0 0 100 72 600 cm /Im1 Do Q", Resources: "/XObject << /Im1 1 0 R >>"}}}
	doc := mustExtract(t, d.bytes(), &gofastcore.Options{Images: gofastcore.ImagesData})
	part := &doc.Parts[0]
	for i := range part.Blocks {
		if part.Blocks[i].Kind == gofastcore.KindImage {
			return part.Blocks[i].Image, part
		}
	}
	t.Fatal("no image block")
	return nil, nil
}

// TestImageEncodings: images whose data is their samples, of any depth,
// colour space and mask, and CCITT fax, come out as PNG with the pixels
// the samples say.
func TestImageEncodings(t *testing.T) {
	const img = "/Type /XObject /Subtype /Image "
	cases := []imageCase{
		{name: "gray of 2 bits", dict: "/Width 4 /Height 1 /ColorSpace /DeviceGray /BitsPerComponent 2", data: "\x1B",
			w: 4, h: 1, want: gray(0, 85, 170, 255)},
		{name: "gray of 4 bits, inverted by Decode", dict: "/Width 2 /Height 1 /ColorSpace /DeviceGray /BitsPerComponent 4 /Decode [1 0]", data: "\x0F",
			w: 2, h: 1, want: gray(255, 0)},
		{name: "gray of 8 bits, inverted by Decode", dict: "/Width 2 /Height 1 /ColorSpace /DeviceGray /BitsPerComponent 8 /Decode [1 0]", data: "\x00\x40",
			w: 2, h: 1, want: gray(255, 191)},
		{name: "gray of 16 bits", dict: "/Width 2 /Height 1 /ColorSpace /DeviceGray /BitsPerComponent 16", data: "\x00\x00\xFF\xFF",
			w: 2, h: 1, want: gray(0, 255)},
		{name: "rows of 1 bit padded to a byte", dict: "/Width 3 /Height 2 /ColorSpace /DeviceGray /BitsPerComponent 1", data: "\xA0\x40",
			w: 3, h: 2, want: gray(255, 0, 255, 0, 255, 0)},
		{name: "RGB of 16 bits", dict: "/Width 1 /Height 1 /ColorSpace /DeviceRGB /BitsPerComponent 16", data: "\xFF\xFF\x80\x00\x00\x00",
			w: 1, h: 1, want: rgba(255, 128, 0, 255)},
		{name: "RGB of 4 bits", dict: "/Width 1 /Height 1 /ColorSpace /DeviceRGB /BitsPerComponent 4", data: "\xF0\xF0",
			w: 1, h: 1, want: rgba(255, 0, 255, 255)},
		{name: "CMYK", dict: "/Width 3 /Height 1 /ColorSpace /DeviceCMYK /BitsPerComponent 8", data: "\x00\x00\x00\x00\xFF\x00\x00\x00\x00\x00\x00\xFF",
			w: 3, h: 1, want: rgba(255, 255, 255, 255, 0, 255, 255, 255, 0, 0, 0, 255)},
		{name: "an ICC profile of four components", dict: "/Width 1 /Height 1 /ColorSpace [/ICCBased 2 0 R] /BitsPerComponent 8", data: "\x00\xFF\x00\x00",
			more: []string{pdftest.Stream("/N 4", "profile")}, w: 1, h: 1, want: rgba(255, 0, 255, 255)},
		{name: "indexed RGB of 2 bits", dict: "/Width 3 /Height 1 /ColorSpace [/Indexed /DeviceRGB 2 <FF000000FF000000FF>] /BitsPerComponent 2", data: "\x18",
			w: 3, h: 1, want: rgba(255, 0, 0, 255, 0, 255, 0, 255, 0, 0, 255, 255)},
		{name: "indexed gray, its table a stream, an index past it", dict: "/Width 2 /Height 1 /ColorSpace [/Indexed /DeviceGray 1 2 0 R] /BitsPerComponent 8", data: "\x00\x09",
			more: []string{pdftest.Stream("", "\x20\xE0")}, w: 2, h: 1, want: gray(0x20, 0xE0)},
		{name: "indexed CMYK", dict: "/Width 2 /Height 1 /ColorSpace [/Indexed /DeviceCMYK 1 <00000000FF00FF00>] /BitsPerComponent 8", data: "\x00\x01",
			w: 2, h: 1, want: rgba(255, 255, 255, 255, 0, 255, 0, 255)},
		{name: "Lab", dict: "/Width 2 /Height 1 /ColorSpace [/Lab << /WhitePoint [0.9505 1 1.089] /Range [-128 127 -128 127] >>] /BitsPerComponent 8", data: "\xFF\x80\x80\x00\x80\x80",
			w: 2, h: 1, want: rgba(255, 255, 255, 255, 0, 0, 0, 255), tol: 2},
		{name: "a soft mask", dict: "/Width 2 /Height 1 /ColorSpace /DeviceRGB /BitsPerComponent 8 /SMask 2 0 R", data: "\xFF\x00\x00\xFF\x00\x00",
			more: []string{pdftest.Stream(img+"/Width 2 /Height 1 /ColorSpace /DeviceGray /BitsPerComponent 8", "\xFF\x80")},
			w:    2, h: 1, want: rgba(255, 0, 0, 255, 255, 0, 0, 128)},
		{name: "a soft mask of another size", dict: "/Width 4 /Height 1 /ColorSpace /DeviceGray /BitsPerComponent 8 /SMask 2 0 R", data: "\x10\x20\x30\x40",
			more: []string{pdftest.Stream(img+"/Width 2 /Height 1 /ColorSpace /DeviceGray /BitsPerComponent 8 /Filter /FlateDecode", string(pdftest.Deflate([]byte{0, 255})))},
			w:    4, h: 1, want: rgba(0x10, 0x10, 0x10, 0, 0x20, 0x20, 0x20, 0, 0x30, 0x30, 0x30, 255, 0x40, 0x40, 0x40, 255)},
		{name: "a mask image", dict: "/Width 2 /Height 1 /ColorSpace /DeviceGray /BitsPerComponent 8 /Mask 2 0 R", data: "\x00\x00",
			more: []string{pdftest.Stream(img+"/Width 2 /Height 1 /ImageMask true", "\x40")},
			w:    2, h: 1, want: rgba(0, 0, 0, 255, 0, 0, 0, 0)},
		{name: "colours masked by range", dict: "/Width 3 /Height 1 /ColorSpace /DeviceGray /BitsPerComponent 8 /Mask [40 60]", data: "\x0A\x32\xC8",
			w: 3, h: 1, want: rgba(10, 10, 10, 255, 50, 50, 50, 0, 200, 200, 200, 255)},
		{name: "a Separation: a spot red, made CMYK", dict: "/Width 3 /Height 1 /ColorSpace [/Separation /Red /DeviceCMYK 2 0 R] /BitsPerComponent 8", data: "\x00\x80\xFF",
			more: []string{"<< /FunctionType 2 /Domain [0 1] /C0 [0 0 0 0] /C1 [0 1 1 0] /N 1 >>"}, w: 3, h: 1, want: rgba(255, 255, 255, 255, 255, 127, 127, 255, 255, 0, 0, 255), tol: 1},
		{name: "a Separation of 1 bit, inverted by Decode, its function PostScript", dict: "/Width 2 /Height 1 /ColorSpace [/Separation /All /DeviceGray 2 0 R] /BitsPerComponent 1 /Decode [1 0]", data: "\x40",
			more: []string{pdftest.Stream("/FunctionType 4 /Domain [0 1] /Range [0 1]", "{ 1 exch sub }")}, w: 2, h: 1, want: gray(0, 255)},
		{name: "a Separation with a soft mask", dict: "/Width 2 /Height 1 /ColorSpace [/Separation /Red /DeviceCMYK 2 0 R] /BitsPerComponent 8 /SMask 3 0 R", data: "\xFF\x00",
			more: []string{"<< /FunctionType 2 /Domain [0 1] /C0 [0 0 0 0] /C1 [0 1 1 0] /N 1 >>", pdftest.Stream(img+"/Width 2 /Height 1 /ColorSpace /DeviceGray /BitsPerComponent 8", "\xFF\x80")},
			w:    2, h: 1, want: rgba(255, 0, 0, 255, 255, 255, 255, 128)},
		{name: "a Separation made RGB by a sampled function", dict: "/Width 3 /Height 1 /ColorSpace [/Separation /Teal /DeviceRGB 2 0 R] /BitsPerComponent 8", data: "\x00\x80\xFF",
			more: []string{pdftest.Stream("/FunctionType 0 /Domain [0 1] /Range [0 1 0 1 0 1] /Size [2] /BitsPerSample 8 /Filter /ASCIIHexDecode", "FFFFFF008080>")},
			w:    3, h: 1, want: rgba(255, 255, 255, 255, 127, 191, 191, 255, 0, 128, 128, 255), tol: 1},
		{name: "DeviceN of two inks made RGB", dict: "/Width 3 /Height 1 /ColorSpace [/DeviceN [/A /B] /DeviceRGB 2 0 R] /BitsPerComponent 8", data: "\xFF\x00\x00\xFF\xFF\xFF",
			more: []string{pdftest.Stream("/FunctionType 4 /Domain [0 1 0 1] /Range [0 1 0 1 0 1]", "{ 0 }")},
			w:    3, h: 1, want: rgba(255, 0, 0, 255, 0, 255, 0, 255, 255, 255, 0, 255)},
		{name: "NChannel by an array of functions, with a mask", dict: "/Width 2 /Height 1 /ColorSpace [/NChannel [/A] /DeviceRGB [2 0 R 2 0 R 3 0 R] << /Subtype /NChannel >>] /BitsPerComponent 8 /Mask [255 255]", data: "\x00\xFF",
			more: []string{"<< /FunctionType 2 /Domain [0 1] /C0 [1] /C1 [0] /N 1 >>", "<< /FunctionType 2 /Domain [0 1] /N 1 >>"},
			w:    2, h: 1, want: rgba(255, 255, 0, 255, 0, 0, 0, 0)},
		{name: "indexed on a Separation", dict: "/Width 2 /Height 1 /ColorSpace [/Indexed [/Separation /Red /DeviceCMYK 2 0 R] 1 <00FF>] /BitsPerComponent 8", data: "\x01\x00",
			more: []string{"<< /FunctionType 2 /Domain [0 1] /C0 [0 0 0 0] /C1 [0 1 1 0] /N 1 >>"}, w: 2, h: 1, want: rgba(255, 0, 0, 255, 255, 255, 255, 255)},
		{name: "DeviceN on Lab", dict: "/Width 1 /Height 1 /ColorSpace [/DeviceN [/A /B /C] [/Lab << /WhitePoint [0.9505 1 1.089] >>] 2 0 R] /BitsPerComponent 8", data: "\xFF\x00\x00",
			more: []string{pdftest.Stream("/FunctionType 4 /Domain [0 1 0 1 0 1] /Range [0 100 -100 100 -100 100]", "{ pop pop 100 mul 0 0 }")},
			w:    1, h: 1, want: rgba(255, 255, 255, 255), tol: 2},
		{name: "a stencil mask: black where it paints", dict: "/Width 8 /Height 1 /ImageMask true", data: "\x0F",
			w: 8, h: 1, want: gray(0, 0, 0, 0, 255, 255, 255, 255)},
		{name: "a stencil mask inverted by Decode", dict: "/Width 8 /Height 1 /ImageMask true /Decode [1 0]", data: "\x0F",
			w: 8, h: 1, want: gray(255, 255, 255, 255, 0, 0, 0, 0)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			objects := append([]string{pdftest.Stream(img+c.dict, c.data)}, c.more...)
			im, part := imageOf(t, objects)
			if im.MIME != "image/png" || im.Data == nil || part.Has(gofastcore.WarnImageUnsupportedEncoding) {
				t.Fatalf("MIME %q, %d bytes, warnings %v", im.MIME, len(im.Data), part.Warnings)
			}
			got, err := png.Decode(bytes.NewReader(im.Data))
			if err != nil {
				t.Fatal(err)
			}
			if b := got.Bounds(); b.Dx() != c.w || b.Dy() != c.h {
				t.Fatalf("%v, want %d by %d", b, c.w, c.h)
			}
			for i, want := range c.want {
				g := color.NRGBAModel.Convert(got.At(i%c.w, i/c.w)).(color.NRGBA)
				if want.A == 0 {
					g.R, g.G, g.B = want.R, want.G, want.B // a pixel left out has no colour
				}
				if !near(g, want, c.tol) {
					t.Errorf("pixel %d is %v, want %v", i, g, want)
				}
			}
		})
	}
}

func near(a, b color.NRGBA, tol uint8) bool {
	d := func(x, y uint8) bool { return x-y <= tol || y-x <= tol }
	return d(a.R, b.R) && d(a.G, b.G) && d(a.B, b.B) && d(a.A, b.A)
}

// CCITT fax data, coded by libtiff (internal/ccitt/testdata), as an
// image and as a stencil mask: the pixels of the image it was coded from.
func TestFaxImages(t *testing.T) {
	data, err := os.ReadFile("internal/ccitt/testdata/g4.bin")
	if err != nil {
		t.Fatal(err)
	}
	pbm, err := os.ReadFile("internal/ccitt/testdata/image.pbm")
	if err != nil {
		t.Fatal(err)
	}
	const w, h = 203, 61
	bits := pbm[len(pbm)-h*((w+7)/8):] // a 1 bit black
	black := func(x, y int) bool { return bits[y*((w+7)/8)+x/8]&(0x80>>(x%8)) != 0 }
	// libtiff coded the image's white as black: BlackIs1 reads it so.
	parms := "/DecodeParms << /K -1 /Columns 203 /BlackIs1 true >>"
	for _, dict := range []string{
		fmt.Sprintf("/Width %d /Height %d /ColorSpace /DeviceGray /BitsPerComponent 1 /Filter /CCITTFaxDecode %s", w, h, parms),
		fmt.Sprintf("/Width %d /Height %d /ImageMask true /Filter [/CCITTFaxDecode] /DecodeParms [%s]", w, h, strings.TrimPrefix(parms, "/DecodeParms ")),
	} {
		im, part := imageOf(t, []string{pdftest.Stream("/Type /XObject /Subtype /Image "+dict, string(data))})
		if im.MIME != "image/png" || part.Has(gofastcore.WarnImageUnsupportedEncoding) {
			t.Fatalf("%s: MIME %q, warnings %v", dict, im.MIME, part.Warnings)
		}
		got, err := png.Decode(bytes.NewReader(im.Data))
		if err != nil {
			t.Fatal(err)
		}
		bad := 0
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				r, _, _, _ := got.At(x, y).RGBA()
				if (r == 0) != black(x, y) {
					bad++
				}
			}
		}
		if bad > 0 {
			t.Errorf("%s: %d pixels differ from the image coded", dict, bad)
		}
	}
}

// What is not delivered: the block keeps its size, and the page says
// why. A JPEG is handed over as it is, its soft mask left out.
func TestImagesNotDelivered(t *testing.T) {
	for _, c := range []struct{ dict, why string }{
		{"/Width 1 /Height 1 /ColorSpace [/Separation /Spot /DeviceCMYK << /FunctionType 7 /Domain [0 1] >>] /BitsPerComponent 8", "Separation: a function of type 7"},
		{"/Width 1 /Height 1 /ColorSpace [/DeviceN [/A /B] /DeviceCMYK 2 0 R] /BitsPerComponent 8", "DeviceN of 2 inks and 4 components, its function of 1 inputs"},
		{"/Width 1 /Height 1 /ColorSpace [/DeviceN [] /DeviceCMYK 2 0 R] /BitsPerComponent 8", "0 inks"},
		{"/Width 1 /Height 1 /ColorSpace [/Separation /Spot [/Indexed /DeviceRGB 0 <000000>] 2 0 R] /BitsPerComponent 8", "alternate"},
		{"/Width 1 /Height 1 /ColorSpace [/Separation /Spot /Pattern 2 0 R] /BitsPerComponent 8", "Pattern"},
		{"/Width 1 /Height 1 /ColorSpace /DeviceGray /BitsPerComponent 1 /Filter /JBIG2Decode", "JBIG2"},
		{"/Width 1 /Height 1 /ColorSpace /DeviceGray /BitsPerComponent 3", "3 bits"},
		{"/Width 1 /Height 1 /ColorSpace /Pattern /BitsPerComponent 8", "Pattern"},
		{"/Width 1 /Height 1 /ColorSpace [/Indexed /DeviceRGB 300 <00>] /BitsPerComponent 8", "256 colours"},
		{"/Width 1 /Height 1 /ColorSpace [/Indexed [/Indexed /DeviceRGB 0 <000000>] 0 <00>] /BitsPerComponent 8", "indexed"},
		{"/Width 1 /Height 1 /BitsPerComponent 8", "without a colour space"},
	} {
		objects := []string{
			pdftest.Stream("/Type /XObject /Subtype /Image "+c.dict, "\x00\x00\x00\x00"),
			"<< /FunctionType 2 /Domain [0 1] /C0 [0 0 0 0] /C1 [0 0 0 1] /N 1 >>",
		}
		im, part := imageOf(t, objects)
		if im.Data != nil || im.MIME != "" || im.Width != 1 {
			t.Errorf("%s: %+v", c.dict, im)
		}
		var msg string
		for _, w := range part.Warnings {
			if w.Code == gofastcore.WarnImageUnsupportedEncoding {
				msg = w.Message
			}
		}
		if !strings.Contains(msg, c.why) {
			t.Errorf("%s: warning %q, want it to say %q", c.dict, msg, c.why)
		}
	}

	pic := image.NewGray(image.Rect(0, 0, 4, 4))
	var jpg bytes.Buffer
	if err := jpeg.Encode(&jpg, pic, nil); err != nil {
		t.Fatal(err)
	}
	im, _ := imageOf(t, []string{
		pdftest.Stream("/Type /XObject /Subtype /Image /Width 4 /Height 4 /ColorSpace /DeviceGray /BitsPerComponent 8 /Filter /DCTDecode /SMask 2 0 R", jpg.String()),
		pdftest.Stream("/Type /XObject /Subtype /Image /Width 4 /Height 4 /ColorSpace /DeviceGray /BitsPerComponent 8", strings.Repeat("\x80", 16)),
	})
	if im.MIME != "image/jpeg" || !bytes.Equal(im.Data, jpg.Bytes()) {
		t.Errorf("a JPEG with a soft mask: %q, %d bytes", im.MIME, len(im.Data))
	}
}

// TestColourFunctionBounds: the function of a colour space is read once
// in a reading of a document, however often the image, or the function,
// is referred to, and an image shown again is not read again. Each case
// runs in a child process, its allocations bounded: without those
// bounds, reading them takes gigabytes and seconds.
func TestColourFunctionBounds(t *testing.T) {
	sep := pdftest.Stream("/Type /XObject /Subtype /Image /Width 1 /Height 1 /BitsPerComponent 8 /ColorSpace [/Separation /S /DeviceGray 2 0 R]", "\x80")
	// Five levels of stitching functions, each referring 16 times to the
	// one below: a million functions, were each reference read.
	const k, depth = 16, 5
	fan := []string{sep}
	bounds := make([]string, k-1)
	for i := range bounds {
		bounds[i] = fmt.Sprintf("%.6f", float64(i+1)/k)
	}
	for level := range depth {
		fan = append(fan, fmt.Sprintf("<< /FunctionType 3 /Domain [0 1] /Functions [%s] /Bounds [%s] /Encode [%s] >>",
			strings.Repeat(fmt.Sprintf("%d 0 R ", level+3), k), strings.Join(bounds, " "), strings.Repeat("0 1 ", k)))
	}
	fan = append(fan, "<< /FunctionType 2 /Domain [0 1] /C0 [0] /C1 [1] /N 1 >>")
	// A program of a mebibyte, compressed to a kilobyte, and the image
	// shown 2,000 times.
	program := pdftest.Deflate([]byte("{" + strings.Repeat(" ", 1<<20-2) + "}"))
	repeat := []string{sep, pdftest.Stream("/FunctionType 4 /Domain [0 1] /Range [0 1] /Filter /FlateDecode", string(program))}

	for _, c := range []struct {
		name     string
		objects  []string
		content  string
		maxAlloc uint64
	}{
		{"fan", fan, "q 10 0 0 10 0 0 cm /Im1 Do Q", 16 << 20},
		{"repeat", repeat, strings.Repeat("q 1 0 0 1 0 0 cm /Im1 Do Q\n", 2000), 64 << 20},
	} {
		data := synthDoc{Objects: c.objects, Pages: []synthPage{{Content: c.content, Resources: "/XObject << /Im1 1 0 R >>"}}}.bytes()
		for _, mode := range []gofastcore.ImageMode{gofastcore.ImagesMeta, gofastcore.ImagesData} {
			id := fmt.Sprintf("%s-%d", c.name, mode)
			err := pdftest.Isolated(t, id, time.Minute, func() error {
				var doc *gofastcore.Document
				var err error
				n := pdftest.AllocBytes(func() {
					doc, err = gofastpdf.Extract(context.Background(), bytes.NewReader(data), int64(len(data)), &gofastcore.Options{Images: mode})
				})
				if err != nil {
					return err
				}
				if n > c.maxAlloc {
					return fmt.Errorf("allocated %d MB", n>>20)
				}
				images := 0
				for _, b := range doc.Parts[0].Blocks {
					if b.Image == nil {
						continue
					}
					if images++; b.Image.MIME != "image/png" || mode == gofastcore.ImagesData && images == 1 && len(b.Image.Data) == 0 {
						return fmt.Errorf("image %d: %q, %d bytes; warnings %v", images, b.Image.MIME, len(b.Image.Data), doc.Parts[0].Warnings)
					}
				}
				if images == 0 {
					return errors.New("no image")
				}
				return nil
			})
			if err != nil {
				t.Errorf("%s: %v", id, err)
			}
		}
	}
}
