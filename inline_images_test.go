package gofastpdf_test

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"strconv"
	"testing"

	gofastcore "github.com/AOShei/go-fast-core"
	"github.com/AOShei/go-fast-pdf/internal/pdftest"
)

// inlineImageOf extracts a page that draws one inline image and returns
// its block's image.
func inlineImageOf(t *testing.T, bi, resources string, mode gofastcore.ImageMode) (*gofastcore.Image, *gofastcore.Part) {
	t.Helper()
	d := synthDoc{Pages: []synthPage{{Content: "q 100 0 0 100 72 600 cm " + bi + " Q", Resources: resources}}}
	doc := mustExtract(t, d.bytes(), &gofastcore.Options{Images: mode})
	part := &doc.Parts[0]
	for i := range part.Blocks {
		if part.Blocks[i].Kind == gofastcore.KindImage {
			return part.Blocks[i].Image, part
		}
	}
	t.Fatal("no image block")
	return nil, nil
}

// An image in the content stream is delivered as an image object is:
// its dictionary may abbreviate its keys, filters and colour spaces, and
// name a colour space of the page's resources.
func TestInlineImages(t *testing.T) {
	for _, c := range []struct {
		name, bi, resources string
		w, h                int
		want                []color.NRGBA
	}{
		{"gray in hexadecimal", "BI /W 2 /H 1 /CS /G /BPC 8 /F /AHx ID 80FF> EI", "", 2, 1, gray(0x80, 0xFF)},
		{"RGB compressed", "BI /W 1 /H 1 /CS /RGB /BPC 8 /F [/Fl] ID " + string(pdftest.Deflate([]byte{255, 128, 0})) + "\nEI", "", 1, 1, rgba(255, 128, 0, 255)},
		{"indexed, its table in the dictionary", "BI /W 2 /H 1 /CS [/I /RGB 1 <FF000000FF00>] /BPC 8 ID \x00\x01\nEI", "", 2, 1, rgba(255, 0, 0, 255, 0, 255, 0, 255)},
		{"a colour space of the resources", "BI /W 1 /H 1 /CS /CS0 /BPC 8 ID \x00\xFF\x00\nEI", "/ColorSpace << /CS0 /DeviceRGB >>", 1, 1, rgba(0, 255, 0, 255)},
		{"indexed on a colour space of the resources", "BI /W 2 /H 1 /CS [/I /Gr 1 <20E0>] /BPC 8 ID \x01\x00\nEI", "/ColorSpace << /Gr /DeviceGray >>", 2, 1, gray(0xE0, 0x20)},
		{"a stencil mask", "BI /W 8 /H 1 /IM true ID \x0F\nEI", "", 8, 1, gray(0, 0, 0, 0, 255, 255, 255, 255)},
		{"inverted by Decode", "BI /W 2 /H 1 /CS /DeviceGray /BPC 8 /D [1 0] ID \x00\x40\nEI", "", 2, 1, gray(255, 191)},
	} {
		t.Run(c.name, func(t *testing.T) {
			im, part := inlineImageOf(t, c.bi, c.resources, gofastcore.ImagesData)
			if im.MIME != "image/png" || im.Data == nil || part.Has(gofastcore.WarnImageUnsupportedEncoding) || im.ID != "inline-1-1" {
				t.Fatalf("%s: MIME %q, %d bytes, warnings %v", im.ID, im.MIME, len(im.Data), part.Warnings)
			}
			got, err := png.Decode(bytes.NewReader(im.Data))
			if err != nil {
				t.Fatal(err)
			}
			if b := got.Bounds(); b.Dx() != c.w || b.Dy() != c.h {
				t.Fatalf("%v, want %d by %d", b, c.w, c.h)
			}
			for i, want := range c.want {
				if g := color.NRGBAModel.Convert(got.At(i%c.w, i/c.w)).(color.NRGBA); !near(g, want, 0) {
					t.Errorf("pixel %d is %v, want %v", i, g, want)
				}
			}
		})
	}

	t.Run("JPEG as it is", func(t *testing.T) {
		pic := image.NewGray(image.Rect(0, 0, 4, 4))
		var jpg bytes.Buffer
		if err := jpeg.Encode(&jpg, pic, nil); err != nil {
			t.Fatal(err)
		}
		for _, bi := range []string{
			"BI /W 4 /H 4 /CS /G /BPC 8 /F /DCT ID " + jpg.String() + "\nEI",
			// Its length given: what follows is not its data.
			"BI /W 4 /H 4 /CS /G /BPC 8 /F /DCT /L " + strconv.Itoa(jpg.Len()) + " ID " + jpg.String() + "xx \nEI",
		} {
			im, _ := inlineImageOf(t, bi, "", gofastcore.ImagesData)
			if im.MIME != "image/jpeg" || !bytes.Equal(im.Data, jpg.Bytes()) {
				t.Errorf("%q, %d bytes, want %d", im.MIME, len(im.Data), jpg.Len())
			}
		}
	})

	t.Run("its type without its data", func(t *testing.T) {
		im, _ := inlineImageOf(t, "BI /W 2 /H 1 /CS /G /BPC 8 /F /AHx ID 80FF> EI", "", gofastcore.ImagesMeta)
		if im.MIME != "image/png" || im.Data != nil || im.Width != 2 {
			t.Errorf("%+v", im)
		}
	})

	t.Run("what cannot be delivered", func(t *testing.T) {
		for _, bi := range []string{
			"BI /W 1 /H 1 /CS /G /BPC 8 /F /JBIG2Decode ID x\nEI",
			"BI /W 1 /H 1 /CS /Unknown /BPC 8 ID x\nEI",
			"BI /W 4 /H 4 /CS /G /BPC 8 ID x\nEI", // too short
		} {
			im, part := inlineImageOf(t, bi, "", gofastcore.ImagesData)
			if im.Data != nil || !part.Has(gofastcore.WarnImageUnsupportedEncoding) {
				t.Errorf("%s: %d bytes, warnings %v", bi, len(im.Data), part.Warnings)
			}
		}
	})
}
