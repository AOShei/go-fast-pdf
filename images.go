package gofastpdf

import (
	"bytes"
	"errors"
	"fmt"
	"image/png"
	"strconv"
	"sync"

	gofastcore "github.com/AOShei/go-fast-core"
	"github.com/AOShei/go-fast-pdf/internal/file"
	"github.com/AOShei/go-fast-pdf/internal/syntax"
)

// imageSrc is an image: an image XObject, or an inline image, whose
// dictionary is v and whose data, from its content stream, is data.
type imageSrc struct {
	v      syntax.Value
	data   []byte
	inline bool
}

// decode returns the data of the image through its filters, up to the
// codec it ends with (file.File.DecodeImage).
func (src imageSrc) decode(f *file.File, limit int64) ([]byte, string, error) {
	if !src.inline {
		return f.DecodeImage(src.v, []byte{}, limit)
	}
	// The data runs to the white space before EI, which may be one byte
	// of it or not: a length the dictionary gives says, and the end of
	// JPEG data does. Samples are read for as many bytes as they need.
	raw := src.data
	if n, ok := src.v.Get("Length").Integer(); ok && n >= 0 && n <= int64(len(raw)) {
		raw = raw[:n]
	}
	data, codec, err := f.DecodeImageBytes(src.v, raw, []byte{}, limit)
	if codec == "DCTDecode" || codec == "DCT" {
		end := bytes.LastIndex(data, []byte{0xFF, 0xD9})
		if end >= 0 && len(bytes.TrimSpace(data[end+2:])) == 0 {
			data = data[:end+2]
		}
	}
	return data, codec, err
}

// pngEncoder writes the PNGs. Its buffers, a compressor's state of
// hundreds of kilobytes, are kept for the next image: a page can have
// thousands of small ones.
var pngEncoder = png.Encoder{BufferPool: &pngBuffers{}}

type pngBuffers struct{ p sync.Pool }

func (b *pngBuffers) Get() *png.EncoderBuffer {
	buf, _ := b.p.Get().(*png.EncoderBuffer)
	return buf
}

func (b *pngBuffers) Put(buf *png.EncoderBuffer) { b.p.Put(buf) }

// imageKind says how the data of an image can be delivered.
type imageKind struct {
	// mime is the media type of what is delivered, empty when the
	// image's encoding is not one this library delivers.
	mime string
	// samples is set for an image whose samples are written as PNG.
	samples bool
	why     string // why not, for the warning
}

// kindOfImage reads from the image's dictionary how its data can be
// delivered. Nothing is decoded.
//
// JPEG and JPEG 2000 data is handed over as it is. Samples, and CCITT
// fax data, are written as PNG (raster.go). JBIG2 is not delivered, nor
// are images in colour spaces made by functions (Separation, DeviceN).
func kindOfImage(f *file.File, src imageSrc) imageKind {
	xobj := src.v
	if xobj.Kind != syntax.Stream && !(src.inline && xobj.Kind == syntax.Dict) {
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
	fax := false
	switch last.NameString() {
	case "DCTDecode", "DCT":
		return imageKind{mime: "image/jpeg"}
	case "JPXDecode":
		return imageKind{mime: "image/jp2"}
	case "CCITTFaxDecode", "CCF":
		fax = true
	case "JBIG2Decode":
		return imageKind{why: "JBIG2 encoding"}
	}
	png := imageKind{mime: "image/png", samples: true}
	if mask := f.Resolve(xobj.Get("ImageMask")); mask.Kind == syntax.Bool && mask.N != 0 {
		return png
	}
	cs, why := readColourSpace(f, xobj.Get("ColorSpace"), 0, false)
	if cs.fam == famNone {
		return imageKind{why: why}
	}
	if fax {
		if cs.n != 1 {
			return imageKind{why: "CCITT fax data in a colour space of more than one component"}
		}
		return png
	}
	switch bpc, _ := f.Resolve(xobj.Get("BitsPerComponent")).Integer(); bpc {
	case 1, 2, 4, 8, 16:
	default:
		return imageKind{why: fmt.Sprintf("%d bits per component", bpc)}
	}
	return png
}

// imageData returns the bytes of an image as kind says they are
// delivered, and how many bytes were decoded for it.
func imageData(f *file.File, src imageSrc, kind imageKind, limit int64) (data []byte, decoded int64, err error) {
	xobj := src.v
	if !kind.samples {
		raw, codec, err := src.decode(f, limit)
		decoded = int64(len(raw))
		if err != nil {
			return nil, decoded, err
		}
		if codec == "" {
			return nil, decoded, errors.New("the image's filters do not end in the codec its dictionary names")
		}
		return raw, decoded, nil
	}
	stencil := false
	if mask := f.Resolve(xobj.Get("ImageMask")); mask.Kind == syntax.Bool && mask.N != 0 {
		stencil = true
	}
	cs := colourSpace{n: 1}
	if !stencil {
		var why string
		if cs, why = readColourSpace(f, xobj.Get("ColorSpace"), 0, true); cs.fam == famNone {
			return nil, 0, errors.New(why)
		}
	}
	s, decoded, err := readSamples(f, src, cs.n, false, limit)
	if err != nil {
		return nil, decoded, err
	}
	var alpha []byte
	if !stencil {
		var d int64
		alpha, d = imageAlpha(f, xobj, &s, limit-decoded)
		decoded += d
	}
	// The pixels: a byte each for one component and no mask, else four.
	per := int64(4)
	if alpha == nil && cs.n == 1 {
		per = 1
	}
	if need := per * int64(s.width) * int64(s.height); need > limit-decoded {
		return nil, decoded, file.ErrTruncated
	}
	img, px := raster(f, xobj, &s, &cs, stencil, alpha)
	decoded += px
	var out bytes.Buffer
	if err := pngEncoder.Encode(&out, img); err != nil {
		return nil, decoded, err
	}
	return out.Bytes(), decoded, nil
}

// image completes one image block: its ID, its media type and, when
// data is asked for, its bytes on the first block that shows the image.
// inline counts the images of the page that have no object of their own.
func (st *state) image(part *gofastcore.Part, ref imageRef, inline *int) {
	f := st.f.f
	img := part.Blocks[ref.block].Image
	wantData := st.f.opts.Images == gofastcore.ImagesData
	undelivered := func(why string) {
		if wantData && !part.Has(gofastcore.WarnImageUnsupportedEncoding) {
			part.Warn(gofastcore.SeverityWarning, gofastcore.WarnImageUnsupportedEncoding,
				"the data of an image on the page is not included: "+why)
		}
	}
	var src imageSrc
	if ref.obj == 0 {
		// An image in the content stream, or one that is not an object
		// of its own: it has no number, and gets one by its page and its
		// place on it, so that a page's part does not depend on the
		// pages before it.
		*inline++
		img.ID = "inline-" + strconv.Itoa(part.Number) + "-" + strconv.Itoa(*inline)
		if !ref.inline {
			undelivered("the image is not an object of its own")
			return
		}
		src = imageSrc{v: ref.dict, data: ref.data, inline: true}
	} else {
		img.ID = "obj-" + strconv.FormatInt(ref.obj, 10)
		src = imageSrc{v: f.Object(ref.obj)}
	}
	kind := kindOfImage(f, src)
	img.MIME = kind.mime
	if !wantData || st.images[img.ID] {
		return
	}
	st.images[img.ID] = true
	if kind.mime == "" {
		undelivered(kind.why)
		return
	}
	if st.budget <= 0 {
		st.imagesCut = true
		return
	}
	data, decoded, err := imageData(f, src, kind, min(st.budget, f.Limits().MaxStreamBytes))
	st.budget -= decoded
	switch {
	case err == file.ErrTruncated:
		st.imagesCut = true
	case err != nil:
		undelivered(err.Error())
	default:
		img.Data = data
	}
}
