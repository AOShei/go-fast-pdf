package xps

import (
	"bytes"
	"encoding/binary"
	"image"
	_ "image/jpeg" // the sizes of JPEG images
	_ "image/png"  // the sizes of PNG images
	"io"
)

// ImageInfo is what is known of an image part without decoding it.
type ImageInfo struct {
	// ID identifies the image in the document: the name of its part, as
	// the archive writes it.
	ID string
	// MIME is the media type of the image: what its first bytes say it
	// is, or else what the package says.
	MIME string
	// Width and Height are its size in pixels, 0 when it is not read.
	Width, Height int
}

// The media types of the images an XPS document may hold.
const (
	mimePNG  = "image/png"
	mimeJPEG = "image/jpeg"
	mimeTIFF = "image/tiff"
	mimeJXR  = "image/vnd.ms-photo"
)

// maxImageHeader bounds what is read of an image to find its size.
const maxImageHeader = 1 << 20

// maxImages bounds the images of a document whose sizes are kept.
const maxImages = 1 << 16

// Image returns what is known of the image that is the part name. Its
// size is read once for the document.
func (d *Document) Image(name string) ImageInfo {
	d.mu.Lock()
	info, ok := d.images[name]
	d.mu.Unlock()
	if ok {
		return info
	}
	info = ImageInfo{ID: d.pkg.Label(d.pkg.Written(name)), MIME: d.pkg.Label(d.pkg.ContentType(name))}
	if rc, err := d.pkg.Open(name); err == nil {
		head := make([]byte, 16)
		n, _ := io.ReadFull(rc, head)
		head = head[:n]
		rest := io.MultiReader(bytes.NewReader(head), io.LimitReader(rc, maxImageHeader))
		switch {
		case bytes.HasPrefix(head, []byte("\x89PNG\r\n\x1a\n")):
			info.MIME = mimePNG
			if cfg, _, err := image.DecodeConfig(rest); err == nil {
				info.Width, info.Height = cfg.Width, cfg.Height
			}
		case bytes.HasPrefix(head, []byte{0xFF, 0xD8}):
			info.MIME = mimeJPEG
			if cfg, _, err := image.DecodeConfig(rest); err == nil {
				info.Width, info.Height = cfg.Width, cfg.Height
			}
		case bytes.HasPrefix(head, []byte("II\xBC")):
			info.MIME = mimeJXR
		case bytes.HasPrefix(head, []byte("II*\x00")), bytes.HasPrefix(head, []byte("MM\x00*")):
			info.MIME = mimeTIFF
			data, _ := io.ReadAll(rest)
			info.Width, info.Height = tiffSize(data)
		}
		rc.Close()
	}
	d.mu.Lock()
	if d.images == nil {
		d.images = map[string]ImageInfo{}
	}
	if len(d.images) < maxImages {
		d.images[name] = info
	}
	d.mu.Unlock()
	return info
}

// tiffSize returns the size of the first image of a TIFF file, as the
// beginning of the file states it; 0 when it is not there.
func tiffSize(data []byte) (width, height int) {
	if len(data) < 8 {
		return 0, 0
	}
	var order binary.ByteOrder = binary.LittleEndian
	if data[0] == 'M' {
		order = binary.BigEndian
	}
	off := int(order.Uint32(data[4:]))
	if off < 8 || off+2 > len(data) {
		return 0, 0
	}
	n := int(order.Uint16(data[off:]))
	for i := 0; i < n; i++ {
		e := off + 2 + 12*i
		if e+12 > len(data) {
			break
		}
		tag, typ := order.Uint16(data[e:]), order.Uint16(data[e+2:])
		var v int
		switch typ {
		case 3:
			v = int(order.Uint16(data[e+8:]))
		case 4:
			v = int(order.Uint32(data[e+8:]) & 0x7FFFFFFF)
		default:
			continue
		}
		switch tag {
		case 256:
			width = v
		case 257:
			height = v
		}
	}
	if width <= 0 || height <= 0 || width > 1<<30 || height > 1<<30 {
		return 0, 0
	}
	return width, height
}

// ImageData returns the bytes of an image part, as the package holds
// them, when they are no more than limit; over is set when they are
// more.
func (d *Document) ImageData(name string, limit int64) (data []byte, over bool, err error) {
	rc, err := d.pkg.Open(name)
	if err != nil {
		return nil, false, err
	}
	defer rc.Close()
	data, err = io.ReadAll(io.LimitReader(rc, limit+1))
	if int64(len(data)) > limit {
		return nil, true, nil
	}
	if err != nil {
		return nil, false, err
	}
	return data, false, nil
}
