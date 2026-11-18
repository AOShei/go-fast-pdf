package gofastpdf_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	gofastcore "github.com/AOShei/go-fast-core"
	gofastpdf "github.com/AOShei/go-fast-pdf"
	"github.com/AOShei/go-fast-pdf/internal/pdftest"
)

// TestGeometry: the size of a part and the boxes of its blocks are in
// displayed space, whatever the page's rotation, crop box and unit.
func TestGeometry(t *testing.T) {
	// "Hello" in Helvetica 12 is 27.336 wide. With its baseline starting
	// at (100, 700) its glyphs lie in x 100 to 127.34 and, with the
	// ascent and descent this library gives a line, y 697.6 to 709.6.
	const content = "BT /F1 12 Tf 100 700 Td (Hello) Tj ET"
	for _, c := range []struct {
		name          string
		extra         string
		width, height float64
		box           gofastcore.Rect
	}{
		{"no rotation", "", 612, 792, gofastcore.Rect{X0: 100, Y0: 82.4, X1: 127.34, Y1: 94.4}},
		{"rotated 90", "/Rotate 90", 792, 612, gofastcore.Rect{X0: 697.6, Y0: 100, X1: 709.6, Y1: 127.34}},
		{"rotated 180", "/Rotate 180", 612, 792, gofastcore.Rect{X0: 484.66, Y0: 697.6, X1: 512, Y1: 709.6}},
		{"rotated 270", "/Rotate 270", 792, 612, gofastcore.Rect{X0: 82.4, Y0: 484.66, X1: 94.4, Y1: 512}},
		{"rotated -90", "/Rotate -90", 792, 612, gofastcore.Rect{X0: 82.4, Y0: 484.66, X1: 94.4, Y1: 512}},
		{"rotated 450", "/Rotate 450", 792, 612, gofastcore.Rect{X0: 697.6, Y0: 100, X1: 709.6, Y1: 127.34}},
		{"a crop box", "/CropBox [50 100 562 742]", 512, 642, gofastcore.Rect{X0: 50, Y0: 32.4, X1: 77.34, Y1: 44.4}},
		{"a crop box and a rotation", "/CropBox [50 100 562 742] /Rotate 90", 642, 512, gofastcore.Rect{X0: 597.6, Y0: 50, X1: 609.6, Y1: 77.34}},
		{"a crop box larger than the page", "/CropBox [-100 -100 2000 2000]", 612, 792, gofastcore.Rect{X0: 100, Y0: 82.4, X1: 127.34, Y1: 94.4}},
		{"a user unit of 2", "/UserUnit 2", 1224, 1584, gofastcore.Rect{X0: 200, Y0: 164.8, X1: 254.67, Y1: 188.8}},
	} {
		t.Run(c.name, func(t *testing.T) {
			d := synthDoc{Pages: []synthPage{{Content: content, Extra: c.extra}}}
			part := &mustExtract(t, d.bytes(), nil).Parts[0]
			if part.Width != c.width || part.Height != c.height {
				t.Errorf("size %v by %v, want %v by %v", part.Width, part.Height, c.width, c.height)
			}
			if len(part.Blocks) != 1 || part.Blocks[0].Text != "Hello" || part.Blocks[0].Hidden != 0 {
				t.Fatalf("blocks:\n%s", describe(part))
			}
			got := part.Blocks[0].Loc.BBox
			for i, pair := range [][2]float64{{got.X0, c.box.X0}, {got.Y0, c.box.Y0}, {got.X1, c.box.X1}, {got.Y1, c.box.Y1}} {
				if math.Abs(pair[0]-pair[1]) > 0.5 {
					t.Errorf("box %+v, want %+v (coordinate %d)", got, c.box, i)
					break
				}
			}
		})
	}

	t.Run("numbers without bound", func(t *testing.T) {
		// Whatever the file states, the document has sizes and boxes
		// that are numbers, and can be written as JSON (which the
		// contract check does).
		for _, d := range []synthDoc{
			{Pages: []synthPage{{Content: content, MediaBox: "[0 0 1e300 1e300]"}}},
			{Pages: []synthPage{{Content: content, MediaBox: "[-1e308 -1e308 1e308 1e308]", Extra: "/UserUnit 9999"}}},
			{Pages: []synthPage{{Content: "1e30 0 0 1e30 1e30 1e30 cm " + content}}},
			{Pages: []synthPage{{Content: "BT /F1 1e30 Tf 1e30 1e30 Td (Hello) Tj ET"}}},
			{Pages: []synthPage{{Content: "BT /F1 12 Tf 1e-30 0 0 1e-30 0 0 Tm (Hello) Tj ET q 1e30 0 0 1e30 0 0 cm /Im1 Do Q", Resources: "/XObject << /Im1 1 0 R >>"}}, Objects: []string{grayImage}},
		} {
			doc := mustExtract(t, d.bytes(), &gofastcore.Options{Images: gofastcore.ImagesMeta})
			if got := doc.Text(); got != "Hello\n" {
				t.Errorf("text %q", got)
			}
		}
	})

	t.Run("text rotated on the page", func(t *testing.T) {
		// Written upwards along x = 100: its box is tall and narrow.
		part := &mustExtract(t, onePage("BT /F1 12 Tf 0 1 -1 0 100 300 Tm (Hello) Tj ET").bytes(), nil).Parts[0]
		got := part.Blocks[0].Loc.BBox
		want := gofastcore.Rect{X0: 90.4, Y0: 792 - 327.34, X1: 102.4, Y1: 792 - 300}
		if math.Abs(got.X0-want.X0) > 0.5 || math.Abs(got.Y0-want.Y0) > 0.5 || math.Abs(got.X1-want.X1) > 0.5 || math.Abs(got.Y1-want.Y1) > 0.5 {
			t.Errorf("box %+v, want %+v", got, want)
		}
	})
	t.Run("no media box", func(t *testing.T) {
		b := pdftest.New()
		font := b.Add(pdftest.FontHelvetica)
		content := b.AddStream("", []byte(content))
		page := b.Add(fmt.Sprintf("<< /Type /Page /Parent 4 0 R /Resources << /Font << /F1 %d 0 R >> >> /Contents %d 0 R >>", font, content))
		b.Add(fmt.Sprintf("<< /Type /Pages /Kids [%d 0 R] /Count 1 >>", page))
		b.Root = b.Add("<< /Type /Catalog /Pages 4 0 R >>")
		part := &mustExtract(t, b.Bytes(), nil).Parts[0]
		if part.Width != 612 || part.Height != 792 {
			t.Errorf("size %v by %v; a page without a media box is taken as US Letter", part.Width, part.Height)
		}
	})
}

// TestTextlessPages: a page without text says whether it has images, in
// every image mode, and a page with text says neither.
func TestTextlessPages(t *testing.T) {
	const image = "q 300 0 0 200 100 400 cm /Im1 Do Q\n"
	const inline = "q 100 0 0 100 50 50 cm BI /W 1 /H 1 /CS /G /BPC 8 ID \x80\nEI Q\n"
	form := pdftest.Stream("/Type /XObject /Subtype /Form /BBox [0 0 612 792] /Resources << /XObject << /Im1 1 0 R >> >>", image)
	for _, c := range []struct {
		name, content string
		want          []string
		images        int
	}{
		{"a blank page", "", []string{"part.no_text"}, 0},
		{"a page of drawing only", "0 0 1 rg 100 100 200 200 re f 1 w 72 72 m 300 300 l S", []string{"part.no_text"}, 0},
		{"an image", image, []string{"part.image_only"}, 1},
		{"an inline image", inline, []string{"part.image_only"}, 1},
		{"an image inside a form", "/Fm1 Do", []string{"part.image_only"}, 1},
		{"an image and text", image + visibleLines, []string{}, 1},
		{"text that is only spaces", "BT /F1 12 Tf 72 720 Td (   ) Tj ET", []string{"part.no_text"}, 0},
		{"text that is only spaces, and an image", image + "BT /F1 12 Tf 72 720 Td (   ) Tj ET", []string{"part.image_only"}, 1},
	} {
		for _, mode := range []gofastcore.ImageMode{gofastcore.ImagesNone, gofastcore.ImagesMeta, gofastcore.ImagesData} {
			t.Run(fmt.Sprintf("%s/mode %d", c.name, mode), func(t *testing.T) {
				d := synthDoc{
					Objects: []string{grayImage, form},
					Pages:   []synthPage{{Content: c.content, Resources: "/XObject << /Im1 1 0 R /Fm1 2 0 R >>"}},
				}
				part := &mustExtract(t, d.bytes(), &gofastcore.Options{Images: mode}).Parts[0]
				got := codes(part.Warnings)
				if mode == gofastcore.ImagesData && strings.Contains(c.name, "inline") {
					// The data of an inline image is not delivered.
					got = remove(got, gofastcore.WarnImageUnsupportedEncoding)
				}
				if !reflect.DeepEqual(got, c.want) {
					t.Errorf("warnings %v, want %v", got, c.want)
				}
				n := 0
				for i := range part.Blocks {
					if part.Blocks[i].Kind == gofastcore.KindImage {
						n++
					}
				}
				if want := c.images * btoi(mode != gofastcore.ImagesNone); n != want {
					t.Errorf("%d image blocks, want %d", n, want)
				}
			})
		}
	}
}

func btoi(v bool) int {
	if v {
		return 1
	}
	return 0
}

func remove(list []string, s string) []string {
	out := []string{}
	for _, v := range list {
		if v != s {
			out = append(out, v)
		}
	}
	return out
}

// TestImages: image blocks, their identity, their place among the text
// and their data.
func TestImages(t *testing.T) {
	// A JPEG and the samples of two small images.
	picture := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			picture.Set(x, y, color.RGBA{uint8(x * 30), uint8(y * 30), 128, 255})
		}
	}
	var jpg bytes.Buffer
	if err := jpeg.Encode(&jpg, picture, nil); err != nil {
		t.Fatal(err)
	}
	rgb := []byte{255, 0, 0, 0, 255, 0, 0, 0, 255, 10, 20, 30} // 2 by 2
	gray := []byte{0, 85, 170, 255, 1, 2}                      // 3 by 2
	objects := []string{
		pdftest.Stream("/Type /XObject /Subtype /Image /Width 8 /Height 8 /ColorSpace /DeviceRGB /BitsPerComponent 8 /Filter /DCTDecode", jpg.String()),
		pdftest.Stream("/Type /XObject /Subtype /Image /Width 2 /Height 2 /ColorSpace /DeviceRGB /BitsPerComponent 8 /Filter /FlateDecode", string(pdftest.Deflate(rgb))),
		pdftest.Stream("/Type /XObject /Subtype /Image /Width 3 /Height 2 /ColorSpace /DeviceGray /BitsPerComponent 8", string(gray)),
		pdftest.Stream("/Type /XObject /Subtype /Image /Width 8 /Height 1 /ColorSpace /DeviceGray /BitsPerComponent 1", "\xAA"),
		pdftest.Stream("/Type /XObject /Subtype /Image /Width 1 /Height 1 /ColorSpace /DeviceCMYK /BitsPerComponent 8", "\x00\x00\x00\xFF"),
		pdftest.Stream("/Type /XObject /Subtype /Image /Width 4 /Height 4 /ColorSpace /DeviceRGB /BitsPerComponent 8", "too short"),
		pdftest.Stream("/Type /XObject /Subtype /Image /Width 64 /Height 64 /ImageMask true /BitsPerComponent 1 /Filter /CCITTFaxDecode", "fax"),
	}
	const resources = "/XObject << /Jpeg 1 0 R /Rgb 2 0 R /Gray 3 0 R /Bits 4 0 R /Cmyk 5 0 R /Short 6 0 R /Fax 7 0 R >>"
	draw := func(name string, y int) string { return fmt.Sprintf("q 100 0 0 80 72 %d cm /%s Do Q\n", y, name) }
	text := func(s string, y int) string {
		return fmt.Sprintf("BT /F1 12 Tf 72 %d Td (%s) Tj ET\n", y, s)
	}
	d := synthDoc{Objects: objects, Pages: []synthPage{
		{Content: text("Above the picture.", 720) + draw("Jpeg", 600) + text("Below the picture.", 560) + draw("Rgb", 400) + draw("Gray", 300), Resources: resources},
		{Content: draw("Jpeg", 600) + draw("Bits", 500) + draw("Cmyk", 400) + draw("Short", 300) + draw("Fax", 200) + text("Second page.", 720), Resources: resources},
		{Content: text("Third page.", 720) + draw("Jpeg", 600) + "q 50 0 0 50 300 300 cm BI /W 1 /H 1 /CS /G /BPC 8 ID \x80\nEI Q\n", Resources: resources},
	}}

	t.Run("no image blocks by default", func(t *testing.T) {
		doc := mustExtract(t, d.bytes(), nil)
		if got := texts(&doc.Parts[0]); !reflect.DeepEqual(got, []string{"Above the picture.", "Below the picture."}) {
			t.Errorf("blocks %q", got)
		}
	})

	t.Run("description", func(t *testing.T) {
		doc := mustExtract(t, d.bytes(), &gofastcore.Options{Images: gofastcore.ImagesMeta})
		// In reading order: the picture stands between the two lines.
		if got := texts(&doc.Parts[0]); !reflect.DeepEqual(got, []string{"Above the picture.", "<image>", "Below the picture.", "<image>", "<image>"}) {
			t.Errorf("blocks %q", got)
		}
		b := &doc.Parts[0].Blocks[1]
		if b.Origin != gofastcore.OriginNative || b.Image.ID != "obj-1" || b.Image.MIME != "image/jpeg" || b.Image.Width != 8 || b.Image.Height != 8 || b.Image.Data != nil {
			t.Errorf("image block %+v, image %+v", b, b.Image)
		}
		if want := (gofastcore.Rect{X0: 72, Y0: 792 - 680, X1: 172, Y1: 792 - 600}); b.Loc.BBox != want {
			t.Errorf("box %+v, want %+v", b.Loc.BBox, want)
		}
		if b.Attrs["color_space"] != "DeviceRGB" {
			t.Errorf("attrs %v", b.Attrs)
		}
		// An image in the content stream has no object number; it is
		// named by its place.
		last := &doc.Parts[2].Blocks[len(doc.Parts[2].Blocks)-1]
		if last.Kind != gofastcore.KindImage || last.Image.ID != "inline-1" || last.Attrs["inline"] != "true" {
			t.Errorf("inline image: %+v, %+v, %v", last, last.Image, last.Attrs)
		}
		for i := range doc.Parts {
			if doc.Parts[i].Has(gofastcore.WarnImageUnsupportedEncoding) {
				t.Errorf("page %d: image.unsupported_encoding though no data was asked for", i+1)
			}
		}
	})

	t.Run("data", func(t *testing.T) {
		doc := mustExtract(t, d.bytes(), &gofastcore.Options{Images: gofastcore.ImagesData})
		byID := map[string][]*gofastcore.Image{}
		for _, b := range doc.AllBlocks() {
			if b.Kind == gofastcore.KindImage {
				byID[b.Image.ID] = append(byID[b.Image.ID], b.Image)
			}
		}
		// The JPEG is handed over as it is in the file, once.
		if imgs := byID["obj-1"]; len(imgs) != 3 || !bytes.Equal(imgs[0].Data, jpg.Bytes()) || imgs[1].Data != nil || imgs[2].Data != nil {
			t.Errorf("the JPEG: %d blocks; data on the first: %v", len(imgs), len(imgs) > 0 && bytes.Equal(imgs[0].Data, jpg.Bytes()))
		} else if _, err := jpeg.Decode(bytes.NewReader(imgs[0].Data)); err != nil {
			t.Errorf("the JPEG does not decode: %v", err)
		}
		// Samples come out as PNG with the same pixels.
		if img := byID["obj-2"][0]; img.MIME != "image/png" {
			t.Errorf("RGB samples: %+v", img)
		} else if got, err := png.Decode(bytes.NewReader(img.Data)); err != nil {
			t.Errorf("RGB samples: %v", err)
		} else {
			for i := 0; i < 4; i++ {
				r, g, b, a := got.At(i%2, i/2).RGBA()
				if want := rgb[i*3:]; uint8(r>>8) != want[0] || uint8(g>>8) != want[1] || uint8(b>>8) != want[2] || a != 0xFFFF {
					t.Errorf("pixel %d is %d %d %d, want %v", i, r>>8, g>>8, b>>8, want[:3])
				}
			}
		}
		if got, err := png.Decode(bytes.NewReader(byID["obj-3"][0].Data)); err != nil {
			t.Errorf("gray samples: %v", err)
		} else {
			for i := 0; i < 6; i++ {
				if g, _, _, _ := got.At(i%3, i/3).RGBA(); uint8(g>>8) != gray[i] {
					t.Errorf("gray pixel %d is %d, want %d", i, g>>8, gray[i])
				}
			}
		}
		// What cannot be delivered is a block with its size and no
		// data, and the page says so.
		for _, id := range []string{"obj-4", "obj-5", "obj-6", "obj-7", "inline-1"} {
			imgs := byID[id]
			if len(imgs) != 1 || imgs[0].Data != nil || imgs[0].Width == 0 || imgs[0].Height == 0 {
				t.Errorf("%s: %+v", id, imgs)
			}
		}
		if doc.Parts[0].Has(gofastcore.WarnImageUnsupportedEncoding) {
			t.Error("page 1: every image was delivered, yet the page has image.unsupported_encoding")
		}
		for _, n := range []int{1, 2} {
			if !doc.Parts[n].Has(gofastcore.WarnImageUnsupportedEncoding) {
				t.Errorf("page %d: no image.unsupported_encoding", n+1)
			}
		}
	})

	t.Run("data on the first block of the pages asked for", func(t *testing.T) {
		doc := mustExtract(t, d.bytes(), &gofastcore.Options{Images: gofastcore.ImagesData, Parts: gofastcore.PartRange{First: 2}})
		first := true
		for _, b := range doc.AllBlocks() {
			if b.Kind == gofastcore.KindImage && b.Image.ID == "obj-1" {
				if (b.Image.Data != nil) != first {
					t.Errorf("data present: %v on a block that is the first: %v", b.Image.Data != nil, first)
				}
				first = false
			}
		}
	})
}

// TestPageRanges: the pages asked for, with their real numbers.
func TestPageRanges(t *testing.T) {
	var pages []synthPage
	for i := 1; i <= 60; i++ {
		pages = append(pages, synthPage{Content: fmt.Sprintf("BT /F1 12 Tf 72 720 Td (Page %d of the document) Tj ET", i)})
	}
	data := synthDoc{Pages: pages}.bytes()
	extract := func(opts gofastcore.Options) (*gofastcore.Document, error) {
		return gofastpdf.Extract(context.Background(), bytes.NewReader(data), int64(len(data)), &opts)
	}
	numbers := func(doc *gofastcore.Document) (first, last, n int) {
		for i := range doc.Parts {
			if want := fmt.Sprintf("Page %d of the document", doc.Parts[i].Number); doc.Parts[i].Text() != want+"\n" {
				t.Errorf("part %d has the text %q", doc.Parts[i].Number, doc.Parts[i].Text())
			}
		}
		return doc.Parts[0].Number, doc.Parts[len(doc.Parts)-1].Number, len(doc.Parts)
	}
	for _, c := range []struct {
		rng         gofastcore.PartRange
		first, last int
	}{
		{gofastcore.PartRange{}, 1, 60},
		{gofastcore.PartRange{First: 40, Last: 55}, 40, 55},
		{gofastcore.PartRange{First: 40}, 40, 60},
		{gofastcore.PartRange{Last: 3}, 1, 3},
		{gofastcore.PartRange{First: 60, Last: 60}, 60, 60},
		{gofastcore.PartRange{First: 50, Last: 500}, 50, 60},
		{gofastcore.PartRange{First: -5, Last: 2}, 1, 2},
	} {
		doc, err := extract(gofastcore.Options{Parts: c.rng})
		if err != nil {
			t.Errorf("%+v: %v", c.rng, err)
			continue
		}
		if err := checkContract(doc, &gofastcore.Options{}); err != nil {
			t.Errorf("%+v: %v", c.rng, err)
		}
		if first, last, n := numbers(doc); first != c.first || last != c.last || n != c.last-c.first+1 {
			t.Errorf("%+v: parts %d to %d (%d), want %d to %d", c.rng, first, last, n, c.first, c.last)
		}
	}
	for _, rng := range []gofastcore.PartRange{{First: 61}, {First: 10, Last: 9}, {First: 1000, Last: 2000}, {Last: -1}} {
		if doc, err := extract(gofastcore.Options{Parts: rng}); !errors.Is(err, gofastcore.ErrNoPartsSelected) || doc != nil {
			t.Errorf("%+v: error %v, document %v; want ErrNoPartsSelected", rng, err, doc != nil)
		}
	}

	t.Run("MaxParts", func(t *testing.T) {
		doc, err := extract(gofastcore.Options{Parts: gofastcore.PartRange{First: 10}, Limits: gofastcore.Limits{MaxParts: 5}})
		if err != nil {
			t.Fatal(err)
		}
		if first, last, n := numbers(doc); first != 10 || last != 14 || n != 5 {
			t.Errorf("parts %d to %d (%d), want 10 to 14", first, last, n)
		}
		if w, ok := warning(doc.Warnings, gofastcore.WarnLimitExceeded); !ok || w.Severity != gofastcore.SeverityError {
			t.Errorf("warnings %+v: want limit.exceeded at error", doc.Warnings)
		}
		// A limit that is not reached says nothing, and a negative one
		// is no limit.
		for _, most := range []int{51, 1000, -1} {
			doc, err := extract(gofastcore.Options{Parts: gofastcore.PartRange{First: 10}, Limits: gofastcore.Limits{MaxParts: most}})
			if err != nil || len(doc.Parts) != 51 || len(doc.Warnings) != 0 {
				t.Errorf("MaxParts %d: %d parts, warnings %v, error %v", most, len(doc.Parts), doc.Warnings, err)
			}
		}
	})
}

// TestLimits: the limits of the shared options.
func TestLimits(t *testing.T) {
	data := onePage(visibleLines).bytes()
	ctx := context.Background()

	t.Run("MaxInputBytes", func(t *testing.T) {
		// The limit is checked before anything is read.
		r := failingReader{}
		_, err := gofastpdf.Extract(ctx, r, 2<<30, nil)
		if !errors.Is(err, gofastcore.ErrLimitExceeded) {
			t.Errorf("a 2 GiB input with the default limit: %v", err)
		}
		_, err = gofastpdf.Extract(ctx, bytes.NewReader(data), int64(len(data)), &gofastcore.Options{Limits: gofastcore.Limits{MaxInputBytes: 100}})
		if !errors.Is(err, gofastcore.ErrLimitExceeded) {
			t.Errorf("an input over a limit of 100 bytes: %v", err)
		}
		if _, err = gofastpdf.Extract(ctx, bytes.NewReader(data), int64(len(data)), &gofastcore.Options{Limits: gofastcore.Limits{MaxInputBytes: int64(len(data))}}); err != nil {
			t.Errorf("an input of exactly the limit: %v", err)
		}
	})

	t.Run("MaxDecodedBytes", func(t *testing.T) {
		// Five pages of 40 kB of content each, and a limit of 100 kB.
		filler := strings.Repeat("% a comment that decodes to nothing but bytes\n", 900)
		var pages []synthPage
		for i := 1; i <= 5; i++ {
			pages = append(pages, synthPage{Content: filler + fmt.Sprintf("BT /F1 12 Tf 72 720 Td (Page %d) Tj ET", i)})
		}
		big := synthDoc{Pages: pages}.bytes()
		var docs []*gofastcore.Document
		for _, workers := range []int{1, 2, 8} {
			opts := &gofastcore.Options{Workers: workers, Limits: gofastcore.Limits{MaxDecodedBytes: 100 << 10}}
			doc, err := gofastpdf.Extract(ctx, bytes.NewReader(big), int64(len(big)), opts)
			if err != nil {
				t.Fatal(err)
			}
			if err := checkContract(doc, opts); err != nil {
				t.Error(err)
			}
			if len(doc.Parts) != 2 {
				t.Errorf("%d workers: %d parts, want the 2 that fit the limit", workers, len(doc.Parts))
			}
			if w, ok := warning(doc.Warnings, gofastcore.WarnLimitExceeded); !ok || w.Severity != gofastcore.SeverityError {
				t.Errorf("%d workers: warnings %+v", workers, doc.Warnings)
			}
			docs = append(docs, doc)
		}
		for _, doc := range docs[1:] {
			if diff := sameDocument(docs[0], doc); diff != "" {
				t.Errorf("the cut depends on the number of workers: %s", diff)
			}
		}
	})

	t.Run("an input that cannot be read", func(t *testing.T) {
		if _, err := gofastpdf.Extract(ctx, failingReader{}, 100, nil); err == nil {
			t.Error("no error")
		}
		if _, err := gofastpdf.Extract(ctx, bytes.NewReader(data), int64(len(data))+10, nil); err == nil {
			t.Error("no error for a size beyond the end of the input")
		}
		if _, err := gofastpdf.Extract(ctx, bytes.NewReader(data), -1, nil); !errors.Is(err, gofastcore.ErrMalformed) {
			t.Errorf("negative size: %v", err)
		}
	})

	t.Run("a cancelled context", func(t *testing.T) {
		cancelled, cancel := context.WithCancel(ctx)
		cancel()
		if doc, err := gofastpdf.Extract(cancelled, bytes.NewReader(data), int64(len(data)), nil); !errors.Is(err, context.Canceled) || doc != nil {
			t.Errorf("error %v, document %v", err, doc != nil)
		}
	})

	t.Run("not a PDF", func(t *testing.T) {
		for _, bad := range []string{"", "hello", strings.Repeat("x", 5000)} {
			if doc, err := gofastpdf.Extract(ctx, strings.NewReader(bad), int64(len(bad)), nil); !errors.Is(err, gofastcore.ErrMalformed) || doc != nil {
				t.Errorf("%.10q: error %v", bad, err)
			}
		}
	})

	t.Run("no pages", func(t *testing.T) {
		empty := pdftest.Doc(pdftest.DocOptions{}).Bytes()
		if doc, err := gofastpdf.Extract(ctx, bytes.NewReader(empty), int64(len(empty)), nil); !errors.Is(err, gofastcore.ErrMalformed) || doc != nil {
			t.Errorf("error %v; a document has at least one part", err)
		}
	})
}

type failingReader struct{}

func (failingReader) ReadAt([]byte, int64) (int, error) { return 0, io.ErrUnexpectedEOF }

// TestFailedPage: a page that cannot be read stays in its place.
func TestFailedPage(t *testing.T) {
	for _, workers := range []int{1, 4} {
		doc := mustExtract(t, damagedMiddlePage(), &gofastcore.Options{Workers: workers})
		if len(doc.Parts) != 3 {
			t.Fatalf("%d parts", len(doc.Parts))
		}
		for i := range doc.Parts {
			p := &doc.Parts[i]
			if p.Number != i+1 {
				t.Errorf("part %d has number %d", i, p.Number)
			}
			w, failed := warning(p.Warnings, gofastcore.WarnPartFailed)
			if failed != (i == 1) {
				t.Errorf("page %d: part.failed present: %v", i+1, failed)
			}
			if i == 1 {
				if w.Severity != gofastcore.SeverityError || len(p.Blocks) != 0 || p.Blocks == nil {
					t.Errorf("failed page: severity %s, blocks %v", w.Severity, p.Blocks)
				}
			} else if p.Text() != visibleText+"\n" {
				t.Errorf("page %d: %q", i+1, p.Text())
			}
		}
	}
}

// TestWorkers: the result does not depend on the number of workers.
func TestWorkers(t *testing.T) {
	var pages []synthPage
	for i := 1; i <= 40; i++ {
		p := synthPage{Content: visibleLines + fmt.Sprintf("BT /F1 12 Tf 72 600 Td (This is page %d.) Tj ET\n", i)}
		switch i % 5 {
		case 1:
			p.Content += "BT /F1 12 Tf 3 Tr 72 500 Td (hidden on this page) Tj ET"
		case 2:
			p.Content = ""
		case 3:
			p.Content += "q 100 0 0 100 300 300 cm /Im1 Do Q"
			p.Resources = "/XObject << /Im1 1 0 R >>"
		}
		pages = append(pages, p)
	}
	data := synthDoc{Objects: []string{grayImage}, Pages: pages}.bytes()
	for _, mode := range []gofastcore.ImageMode{gofastcore.ImagesNone, gofastcore.ImagesData} {
		one := mustExtract(t, data, &gofastcore.Options{Workers: 1, Images: mode})
		for _, workers := range []int{0, 2, 8, 64} {
			many := mustExtract(t, data, &gofastcore.Options{Workers: workers, Images: mode})
			if diff := sameDocument(one, many); diff != "" {
				t.Errorf("image mode %d: 1 worker and %d workers: %s", mode, workers, diff)
			}
		}
	}
}

// TestSource: what the document says about its input.
func TestSource(t *testing.T) {
	data := onePage(visibleLines).bytes()
	doc := mustExtract(t, data, &gofastcore.Options{Name: "minutes.pdf"})
	want := gofastcore.Source{Format: "pdf", Name: "minutes.pdf", MIME: "application/pdf", Size: int64(len(data))}
	if doc.Source != want || doc.SchemaVersion != gofastcore.SchemaVersion {
		t.Errorf("source %+v, schema %q", doc.Source, doc.SchemaVersion)
	}
	if got := (&gofastpdf.Extractor{}).Formats(); !reflect.DeepEqual(got, []string{"pdf"}) {
		t.Errorf("formats %v", got)
	}
	// The method and the function are the same thing, and nil options
	// are the zero options.
	var e gofastcore.Extractor = &gofastpdf.Extractor{}
	other, err := e.Extract(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatal(err)
	}
	doc.Source.Name = ""
	if diff := sameDocument(doc, other); diff != "" {
		t.Error(diff)
	}
}

// TestMetadata: the document information.
func TestMetadata(t *testing.T) {
	d := synthDoc{
		Objects: []string{`<< /Title (Annual report) /Author (Jane Doe; John Roe) /Subject (Finance) /Keywords (budget, 2024, report)
			/Creator (Writer) /Producer (PDF Library 3) /CreationDate (D:20240315143000+02'00') /ModDate (D:20240316) /Trapped /False >>`},
		Catalog: "/Lang (en-GB)",
		Pages:   []synthPage{{Content: visibleLines}},
	}
	b := pdftest.New()
	for _, o := range d.Objects {
		b.Add(o)
	}
	pdftest.Doc(pdftest.DocOptions{Base: b}, pdftest.PageSpec{Content: visibleLines})
	b.Set(2, "<< /Type /Catalog /Pages 3 0 R /Lang (en-GB) >>")
	b.Trailer = "/Info 1 0 R"
	md := mustExtract(t, b.Bytes(), nil).Metadata
	want := gofastcore.Metadata{
		Title:       "Annual report",
		Authors:     []string{"Jane Doe; John Roe"}, // one field, one entry
		Subject:     "Finance",
		Keywords:    []string{"budget, 2024, report"},
		Language:    "en-GB",
		Created:     time.Date(2024, 3, 15, 14, 30, 0, 0, time.FixedZone("", 2*3600)),
		Modified:    time.Date(2024, 3, 16, 0, 0, 0, 0, time.UTC),
		Application: "Writer",
		Extra:       map[string]string{"producer": "PDF Library 3"},
	}
	if !md.Created.Equal(want.Created) || !md.Modified.Equal(want.Modified) {
		t.Errorf("created %v, modified %v", md.Created, md.Modified)
	}
	md.Created, md.Modified, want.Created, want.Modified = time.Time{}, time.Time{}, time.Time{}, time.Time{}
	if !reflect.DeepEqual(md, want) {
		t.Errorf("metadata %+v\nwant     %+v", md, want)
	}

	t.Run("dates", func(t *testing.T) {
		for in, want := range map[string]string{
			"D:20240315143000+02'00'": "2024-03-15T14:30:00+02:00",
			"D:20240315143000-0530":   "2024-03-15T14:30:00-05:30",
			"D:20240315143000Z":       "2024-03-15T14:30:00Z",
			"D:20240315143000Z00'00'": "2024-03-15T14:30:00Z",
			"D:202403151430":          "2024-03-15T14:30:00Z",
			"D:2024":                  "2024-01-01T00:00:00Z",
			"20240315":                "2024-03-15T00:00:00Z",
			"D:20240231":              "", // no such day
			"D:20241315":              "",
			"March 15, 2024":          "",
			"":                        "",
			"D:":                      "",
		} {
			b := pdftest.New()
			b.Add(fmt.Sprintf("<< /CreationDate (%s) >>", in))
			pdftest.Doc(pdftest.DocOptions{Base: b}, pdftest.PageSpec{Content: visibleLines})
			b.Trailer = "/Info 1 0 R"
			got := mustExtract(t, b.Bytes(), nil).Metadata.Created
			if text := got.Format(time.RFC3339); (want == "") != got.IsZero() || want != "" && text != want {
				t.Errorf("%q: %s, want %q", in, text, want)
			}
		}
	})

	t.Run("strings that are not clean", func(t *testing.T) {
		b := pdftest.New()
		b.Add("<< /Title (Report\\000\\000) /Author (A\\rB\\r\\nC\\007) /Subject <FEFFFB01006C0065> >>")
		pdftest.Doc(pdftest.DocOptions{Base: b}, pdftest.PageSpec{Content: visibleLines})
		b.Trailer = "/Info 1 0 R"
		md := mustExtract(t, b.Bytes(), nil).Metadata
		if md.Title != "Report" || len(md.Authors) != 1 || md.Authors[0] != "A\nB\nC�" || md.Subject != "file" {
			t.Errorf("%q, %q, %q", md.Title, md.Authors, md.Subject)
		}
	})

	t.Run("invisible characters", func(t *testing.T) {
		// Tag characters spell a message that no viewer shows.
		b := pdftest.New()
		b.Add("<< /Title <FEFF0041DB40DC69DB40DC67> >>")
		pdftest.Doc(pdftest.DocOptions{Base: b}, pdftest.PageSpec{Content: visibleLines})
		b.Trailer = "/Info 1 0 R"
		doc := mustExtract(t, b.Bytes(), nil)
		if w, ok := warning(doc.Warnings, gofastcore.WarnTextHidden); !ok || w.Severity != gofastcore.SeverityWarning {
			t.Errorf("warnings %+v", doc.Warnings)
		}
		if _, ok := warning(mustExtract(t, onePage(visibleLines).bytes(), nil).Warnings, gofastcore.WarnTextHidden); ok {
			t.Error("text.hidden on a document without invisible characters")
		}
	})
}

// TestPageLabels: the labels of the pages are their names, where they
// differ from their numbers.
func TestPageLabels(t *testing.T) {
	pages := make([]synthPage, 12)
	for i := range pages {
		pages[i].Content = visibleLines
	}
	d := synthDoc{
		Catalog: "/PageLabels << /Nums [0 << /S /r >> 3 << /S /D >> 7 << /S /A /P (App-) >> 9 << /P (Index) >> 10 << /S /D /St 4 >> ] >>",
		Pages:   pages,
	}
	doc := mustExtract(t, d.bytes(), nil)
	// Pages 4 to 7 are labelled 1 to 4, which is not their number.
	want := []string{"i", "ii", "iii", "1", "2", "3", "4", "App-A", "App-B", "Index", "4", "5"}
	for i := range doc.Parts {
		if doc.Parts[i].Name != want[i] {
			t.Errorf("page %d is named %q, want %q", i+1, doc.Parts[i].Name, want[i])
		}
	}
	// A label that is the page's number is not a name.
	d.Catalog = "/PageLabels << /Nums [0 << /S /D >>] >>"
	for _, p := range mustExtract(t, d.bytes(), nil).Parts {
		if p.Name != "" {
			t.Errorf("page %d is named %q", p.Number, p.Name)
		}
	}
	// In a tree of several levels, and for a range of pages.
	d.Catalog = "/PageLabels << /Kids [<< /Nums [0 << /S /R >>] >> << /Kids [<< /Nums [6 << /S /a >>] >>] >>] >>"
	doc = mustExtract(t, d.bytes(), &gofastcore.Options{Parts: gofastcore.PartRange{First: 6, Last: 8}})
	if got := []string{doc.Parts[0].Name, doc.Parts[1].Name, doc.Parts[2].Name}; !reflect.DeepEqual(got, []string{"VI", "a", "b"}) {
		t.Errorf("names %q", got)
	}
}

// TestRendering: the Markdown and the plain text of a document hold the
// words of its blocks.
func TestRendering(t *testing.T) {
	d := synthDoc{Pages: []synthPage{
		{Content: visibleLines + "BT /F1 12 Tf 72 600 Td (# Not a heading, and 1. not a list) Tj ET"},
		{Content: "BT /F1 12 Tf 72 720 Td (Second page) Tj ET"},
	}}
	doc := mustExtract(t, d.bytes(), nil)
	var words []string
	for _, b := range doc.AllBlocks() {
		words = append(words, strings.Fields(b.Text)...)
	}
	text := strings.Fields(doc.Text())
	if !reflect.DeepEqual(text, words) {
		t.Errorf("text has the words %q\nblocks have %q", text, words)
	}
	md := strings.Fields(strings.NewReplacer(`\#`, "#", `1\.`, "1.").Replace(doc.Markdown()))
	if !reflect.DeepEqual(md, words) {
		t.Errorf("Markdown has the words %q\nblocks have %q", md, words)
	}
	// The document survives JSON.
	enc, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	var back gofastcore.Document
	if err := json.Unmarshal(enc, &back); err != nil {
		t.Fatal(err)
	}
	if back.Text() != doc.Text() || back.Validate() != nil {
		t.Errorf("after JSON: %v", back.Validate())
	}
}
