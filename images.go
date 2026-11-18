package gofastpdf

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/png"
	"strconv"

	gofastcore "github.com/AOShei/go-fast-core"
	"github.com/AOShei/go-fast-pdf/internal/file"
	"github.com/AOShei/go-fast-pdf/internal/syntax"
)

// imageKind says how the data of an image can be delivered.
type imageKind struct {
	// mime is the media type of what is delivered, empty when the
	// image's encoding is not one this library delivers.
	mime string
	// components is 1 or 3 for an image whose samples are written as
	// PNG, 0 otherwise.
	components int
	why        string // why not, for the warning
}

// kindOfImage reads from the image's dictionary how its data can be
// delivered. Nothing is decoded.
//
// JPEG and JPEG 2000 data is handed over as it is. Samples of 8 bits in
// a gray or RGB colour space are written as PNG. Everything else is not
// delivered: other depths and colour spaces, stencil masks, CCITT fax
// and JBIG2.
func kindOfImage(f *file.File, xobj syntax.Value) imageKind {
	if xobj.Kind != syntax.Stream {
		return imageKind{why: "the image is not a stream"}
	}
	filters := f.Resolve(xobj.Get("Filter"))
	last := filters
	if filters.Kind == syntax.Array {
		last = syntax.Value{}
		if n := len(filters.List); n > 0 {
			last = f.Resolve(filters.List[n-1])
		}
	}
	switch last.NameString() {
	case "DCTDecode", "DCT":
		return imageKind{mime: "image/jpeg"}
	case "JPXDecode":
		return imageKind{mime: "image/jp2"}
	case "CCITTFaxDecode", "CCF":
		return imageKind{why: "CCITT fax encoding"}
	case "JBIG2Decode":
		return imageKind{why: "JBIG2 encoding"}
	}
	if mask := f.Resolve(xobj.Get("ImageMask")); mask.Kind == syntax.Bool && mask.N != 0 {
		return imageKind{why: "a stencil mask"}
	}
	if bpc, _ := f.Resolve(xobj.Get("BitsPerComponent")).Integer(); bpc != 8 {
		return imageKind{why: fmt.Sprintf("%d bits per component", bpc)}
	}
	if xobj.Has("Decode") {
		// A decode array can invert or rescale the samples.
		return imageKind{why: "a decode array"}
	}
	switch n := components(f, xobj.Get("ColorSpace")); n {
	case 1, 3:
		return imageKind{mime: "image/png", components: n}
	}
	return imageKind{why: "a colour space other than gray or RGB"}
}

// components returns 1 for a gray colour space and 3 for an RGB one,
// and 0 for any other.
func components(f *file.File, v syntax.Value) int {
	cs := f.Resolve(v)
	family := cs
	if cs.Kind == syntax.Array && len(cs.List) > 0 {
		family = f.Resolve(cs.List[0])
	}
	switch family.NameString() {
	case "DeviceGray", "G", "CalGray":
		return 1
	case "DeviceRGB", "RGB", "CalRGB":
		return 3
	case "ICCBased":
		if cs.Kind == syntax.Array && len(cs.List) > 1 {
			if n, _ := f.Resolve(f.Resolve(cs.List[1]).Get("N")).Integer(); n == 1 || n == 3 {
				return int(n)
			}
		}
	}
	return 0
}

// maxImagePixels bounds the images written as PNG.
const maxImagePixels = 1 << 28

// imageData returns the bytes of an image as kind says they are
// delivered, and how many bytes were decoded for it.
func imageData(f *file.File, xobj syntax.Value, kind imageKind, limit int64) (data []byte, decoded int64, err error) {
	raw, codec, err := f.DecodeImage(xobj, []byte{}, limit)
	decoded = int64(len(raw))
	if err != nil {
		return nil, decoded, err
	}
	if kind.components == 0 {
		if codec == "" {
			return nil, decoded, errors.New("the image's filters do not end in the codec its dictionary names")
		}
		return raw, decoded, nil
	}
	w, _ := f.Resolve(xobj.Get("Width")).Integer()
	h, _ := f.Resolve(xobj.Get("Height")).Integer()
	if codec != "" || w <= 0 || h <= 0 || w > 1<<20 || h > 1<<20 || w*h > maxImagePixels {
		return nil, decoded, fmt.Errorf("an image of %d by %d", w, h)
	}
	if need := w * h * int64(kind.components); int64(len(raw)) < need {
		return nil, decoded, fmt.Errorf("the image has %d bytes of samples and its size needs %d", len(raw), need)
	}
	rect := image.Rect(0, 0, int(w), int(h))
	var img image.Image
	if kind.components == 1 {
		img = &image.Gray{Pix: raw[:w*h], Stride: int(w), Rect: rect}
	} else {
		pix := make([]byte, w*h*4)
		for i, j := 0, 0; i < len(pix); i, j = i+4, j+3 {
			pix[i], pix[i+1], pix[i+2], pix[i+3] = raw[j], raw[j+1], raw[j+2], 0xFF
		}
		img = &image.NRGBA{Pix: pix, Stride: int(w) * 4, Rect: rect}
	}
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		return nil, decoded, err
	}
	return out.Bytes(), decoded, nil
}

// image completes one image block: its ID, its media type and, when
// data is asked for, its bytes on the first block that shows the image.
func (m *merger) image(part *gofastcore.Part, ref imageRef) {
	img := part.Blocks[ref.block].Image
	wantData := m.opts.Images == gofastcore.ImagesData
	undelivered := func(why string) {
		if wantData && !part.Has(gofastcore.WarnImageUnsupportedEncoding) {
			part.Warn(gofastcore.SeverityWarning, gofastcore.WarnImageUnsupportedEncoding,
				"the data of an image on the page is not included: "+why)
		}
	}
	if ref.obj == 0 {
		// An image in the content stream, or one that is not an object
		// of its own: it has no number, and gets one by its place.
		m.inline++
		img.ID = "inline-" + strconv.Itoa(m.inline)
		undelivered("the image is in the page's content stream")
		return
	}
	img.ID = "obj-" + strconv.FormatInt(ref.obj, 10)
	xobj := m.f.Object(ref.obj)
	kind := kindOfImage(m.f, xobj)
	img.MIME = kind.mime
	if !wantData || m.images[img.ID] {
		return
	}
	m.images[img.ID] = true
	if kind.mime == "" {
		undelivered(kind.why)
		return
	}
	if m.budget <= 0 {
		m.imagesCut = true
		return
	}
	data, decoded, err := imageData(m.f, xobj, kind, min(m.budget, m.f.Limits().MaxStreamBytes))
	m.budget -= decoded
	switch {
	case err == file.ErrTruncated:
		m.imagesCut = true
	case err != nil:
		undelivered(err.Error())
	default:
		img.Data = data
	}
}
