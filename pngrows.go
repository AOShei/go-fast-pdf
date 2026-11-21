package gofastpdf

import (
	"bufio"
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"sync"
)

// sampleImage is an image of one component of up to 8 bits and no mask
// whose samples are the indices of its palette, as they are: the PNG
// rows of a palette image are packed as the samples of a PDF image are.
// writeSamplePNG writes it from the rows; png.Encoder, through
// ColorIndexAt, writes the same bytes pixel by pixel.
type sampleImage struct {
	s   *samples
	pal color.Palette
}

func (m *sampleImage) ColorModel() color.Model { return m.pal }

func (m *sampleImage) Bounds() image.Rectangle { return image.Rect(0, 0, m.s.width, m.s.height) }

func (m *sampleImage) At(x, y int) color.Color { return m.pal[m.s.at(x, y, 0)] }

func (m *sampleImage) ColorIndexAt(x, y int) uint8 { return uint8(m.s.at(x, y, 0)) }

// rows reports whether the samples are rows that writeSamplePNG can
// write as they are: of the depth at which png.Encoder writes the
// palette, and all there.
func (m *sampleImage) rows() bool {
	s := m.s
	depth := 8
	switch n := len(m.pal); {
	case n <= 2:
		depth = 1
	case n <= 4:
		depth = 2
	case n <= 16:
		depth = 4
	}
	return s.n == 1 && s.bpc == depth && len(m.pal) <= 256 && s.width > 0 && s.height > 0 &&
		int64(len(s.data)) >= int64(s.stride())*int64(s.height)
}

// pngRows are the buffers of writeSamplePNG, kept for the next image as
// png.Encoder keeps its own.
type pngRows struct {
	zw  *zlib.Writer
	bw  *bufio.Writer
	row []byte
}

var pngRowsPool sync.Pool

// writeSamplePNG writes m to out as png.Encoder writes it: the header,
// the palette (and its transparency), the rows of the samples behind a
// filter byte of none, one write a row, compressed at the default level
// into IDAT chunks of up to 32 KiB, and the end. m.rows() must be true.
func writeSamplePNG(out *bytes.Buffer, m *sampleImage) error {
	s := m.s
	out.WriteString("\x89PNG\r\n\x1a\n")
	var ihdr [13]byte
	binary.BigEndian.PutUint32(ihdr[0:4], uint32(s.width))
	binary.BigEndian.PutUint32(ihdr[4:8], uint32(s.height))
	ihdr[8] = byte(s.bpc)
	ihdr[9] = 3 // a palette
	writePNGChunk(out, "IHDR", ihdr[:])
	var plte [3 * 256]byte
	var trns [256]byte
	last := -1
	for i, c := range m.pal {
		c1 := color.NRGBAModel.Convert(c).(color.NRGBA)
		plte[3*i], plte[3*i+1], plte[3*i+2] = c1.R, c1.G, c1.B
		if c1.A != 0xff {
			last = i
		}
		trns[i] = c1.A
	}
	writePNGChunk(out, "PLTE", plte[:3*len(m.pal)])
	if last != -1 {
		writePNGChunk(out, "tRNS", trns[:1+last])
	}

	b, _ := pngRowsPool.Get().(*pngRows)
	if b == nil {
		b = &pngRows{bw: bufio.NewWriterSize(idatWriter{out}, 1<<15)}
		b.zw, _ = zlib.NewWriterLevel(b.bw, zlib.DefaultCompression)
	} else {
		b.bw.Reset(idatWriter{out})
		b.zw.Reset(b.bw)
	}
	defer pngRowsPool.Put(b)
	stride := s.stride()
	if cap(b.row) < 1+stride {
		b.row = make([]byte, 1+stride)
	}
	row := b.row[:1+stride]
	row[0] = 0 // the filter: none
	// The bits after the last pixel of a row are 0, whatever the PDF
	// has there.
	pad := byte(0xff)
	if r := s.width * s.bpc % 8; r != 0 {
		pad = 0xff << (8 - r)
	}
	for y := 0; y < s.height; y++ {
		copy(row[1:], s.data[y*stride:(y+1)*stride])
		row[stride] &= pad
		if _, err := b.zw.Write(row); err != nil {
			return err
		}
	}
	if err := b.zw.Close(); err != nil {
		return err
	}
	if err := b.bw.Flush(); err != nil {
		return err
	}
	writePNGChunk(out, "IEND", nil)
	return nil
}

// idatWriter writes each write as an IDAT chunk.
type idatWriter struct{ out *bytes.Buffer }

func (w idatWriter) Write(p []byte) (int, error) {
	writePNGChunk(w.out, "IDAT", p)
	return len(p), nil
}

func writePNGChunk(out *bytes.Buffer, name string, data []byte) {
	var head [8]byte
	binary.BigEndian.PutUint32(head[:4], uint32(len(data)))
	copy(head[4:], name)
	out.Write(head[:])
	out.Write(data)
	crc := crc32.Update(crc32.ChecksumIEEE(head[4:8]), crc32.IEEETable, data)
	var foot [4]byte
	binary.BigEndian.PutUint32(foot[:], crc)
	out.Write(foot[:])
}
