package gofastpdf

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"math"

	"github.com/AOShei/go-fast-pdf/internal/ccitt"
	"github.com/AOShei/go-fast-pdf/internal/file"
	"github.com/AOShei/go-fast-pdf/internal/function"
	"github.com/AOShei/go-fast-pdf/internal/syntax"
)

// The pixels of an image whose data is its samples.
//
// The samples of an image are its colour components, one to four per
// pixel of 1, 2, 4, 8 or 16 bits each, row after row, each row starting
// on a byte. Its Decode array maps them onto the range of each
// component, and its colour space makes colours of them:
//
//   - DeviceGray, CalGray and ICC profiles of one component are gray;
//   - DeviceRGB, CalRGB and ICC profiles of three are RGB;
//   - DeviceCMYK and ICC profiles of four are CMYK, made RGB by taking
//     each ink and the black from white (red is 1-C times 1-K): the
//     colour of an ink on paper is not known without a profile, and an
//     ICC profile is not read;
//   - Lab is made RGB through CIE XYZ, its white point made the white
//     of sRGB;
//   - Separation and DeviceN (and NChannel) are inks: the file gives a
//     function of their tints (internal/function) that makes a colour of
//     another of these spaces, its alternate, and that colour is
//     delivered. Separation's All and None are made so too;
//   - Indexed picks a colour of its table, of any of these.
//
// Pattern is not delivered.
//
// What the image's samples cannot say is said by its mask, which makes
// pixels transparent: a soft mask (SMask, an image of gray samples whose
// values are how opaque each pixel is), a mask image (Mask, a stencil
// whose 1 samples leave the image out), or ranges of colour (Mask as an
// array: a pixel whose every sample is in its range is left out). A mask
// of another size than the image is scaled to it. A mask that cannot be
// read leaves the image without one.
//
// A stencil mask (ImageMask) has no colour of its own: it paints with
// the colour that is current when it is drawn. It is delivered black
// where it paints, white elsewhere.
//
// CCITT fax data is decoded into samples of 1 bit (internal/ccitt).

const (
	// maxImagePixels bounds the images written as PNG; the bytes decoded
	// for them count against Limits.MaxDecodedBytes too.
	maxImagePixels = 1 << 28
	// maxColourSpaceDepth bounds the colour spaces followed from one: an
	// ICC profile's alternate, the base of an indexed one.
	maxColourSpaceDepth = 8
	// maxColourants is the most inks of a DeviceN colour space.
	maxColourants = 32
)

// maxFunctionWork bounds the work of the function of a colour space over
// one image, in operations (function.Func.Cost): about a second. It is a
// variable for its test.
var maxFunctionWork = 1 << 28

// family is the kind of a colour space, as far as its samples go.
type family int

const (
	famNone family = iota
	famGray
	famRGB
	famCMYK
	famLab
	famIndexed
	famFunc // Separation, DeviceN
)

// colourSpace is what is needed of a colour space to make colours of
// samples.
type colourSpace struct {
	fam family
	n   int // components per sample
	// Lab: the range of a and b.
	ab [4]float64
	// Indexed: the colours of the table, as RGB, and whether they are
	// all gray; hival is the highest index.
	palette []color.NRGBA
	gray    bool
	hival   int
	// Separation and DeviceN: the function of the tints, the colour space
	// it makes colours of, and room for its inputs and outputs.
	fn          *function.Func
	alt         *colourSpace
	fnIn, fnOut []float64
	// decoded is the bytes decoded for the table and the function.
	decoded int64
}

// readColourSpace reads the colour space of an image. why says what is
// not delivered, when fam is famNone. The table of an indexed colour
// space, and the samples of a sampled function, are read only when limit
// is not 0, up to limit bytes.
func readColourSpace(f *file.File, v syntax.Value, depth int, limit int64) (cs colourSpace, why string) {
	if depth > maxColourSpaceDepth {
		return cs, "a colour space that refers to itself"
	}
	v = f.Resolve(v)
	name := v
	if v.Kind == syntax.Array && len(v.List) > 0 {
		name = f.Resolve(v.List[0])
	}
	arg := func(i int) syntax.Value {
		if v.Kind == syntax.Array && len(v.List) > i {
			return f.Resolve(v.List[i])
		}
		return syntax.Value{}
	}
	switch name.NameString() {
	case "DeviceGray", "G", "CalGray":
		return colourSpace{fam: famGray, n: 1}, ""
	case "DeviceRGB", "RGB", "CalRGB":
		return colourSpace{fam: famRGB, n: 3}, ""
	case "DeviceCMYK", "CMYK":
		return colourSpace{fam: famCMYK, n: 4}, ""
	case "ICCBased":
		n, _ := f.Resolve(arg(1).Get("N")).Integer()
		switch n {
		case 1:
			return colourSpace{fam: famGray, n: 1}, ""
		case 3:
			return colourSpace{fam: famRGB, n: 3}, ""
		case 4:
			return colourSpace{fam: famCMYK, n: 4}, ""
		}
		if alt := arg(1).Get("Alternate"); !alt.IsNull() {
			return readColourSpace(f, alt, depth+1, limit)
		}
		return cs, fmt.Sprintf("an ICC profile of %d components", n)
	case "Lab":
		cs = colourSpace{fam: famLab, n: 3, ab: [4]float64{-100, 100, -100, 100}}
		for i, x := range f.Resolve(arg(1).Get("Range")).Elems() {
			if v, ok := f.Resolve(x).Float(); ok && i < 4 && !math.IsNaN(v) && !math.IsInf(v, 0) {
				cs.ab[i] = v
			}
		}
		return cs, ""
	case "Indexed", "I":
		base, why := readColourSpace(f, arg(1), depth+1, limit)
		if base.fam == famNone || base.fam == famIndexed {
			if why == "" {
				why = "an indexed colour space on an indexed one"
			}
			return colourSpace{}, why
		}
		hival, ok := arg(2).Integer()
		if !ok || hival < 0 || hival > 255 {
			return cs, "an indexed colour space without a table of up to 256 colours"
		}
		cs = colourSpace{fam: famIndexed, n: 1, hival: int(hival), gray: true, decoded: base.decoded}
		if limit == 0 {
			return cs, ""
		}
		table := arg(3)
		var lookup []byte
		switch table.Kind {
		case syntax.String:
			lookup = table.Data
		case syntax.Stream:
			lookup, _ = f.Decode(table, nil, 4*256)
		}
		comps := make([]float64, base.n)
		for i := 0; i <= int(hival); i++ {
			for c := range comps {
				b := 0.0
				if k := i*base.n + c; k < len(lookup) {
					b = float64(lookup[k]) / 255
				}
				comps[c] = base.natural(c, b)
			}
			col := base.colour(comps)
			cs.palette = append(cs.palette, col)
			cs.gray = cs.gray && col.R == col.G && col.G == col.B
		}
		return cs, ""
	case "Separation", "DeviceN", "NChannel":
		kind := name.NameString()
		n := 1
		if kind != "Separation" {
			n = len(arg(1).Elems())
			if n == 0 || n > maxColourants {
				return cs, fmt.Sprintf("a colour space of type %s of %d inks", kind, n)
			}
		}
		alt, why := readColourSpace(f, arg(2), depth+1, limit)
		switch alt.fam {
		case famNone:
			return cs, why
		case famIndexed, famFunc:
			return cs, "a colour space of type " + kind + " whose alternate is not a colour space of colours"
		}
		// An alternate of colours decodes nothing: the limit is the
		// function's.
		fn, decoded, err := function.Parse(f, arg(3), limit)
		if err != nil {
			return colourSpace{decoded: decoded}, fmt.Sprintf("a colour space of type %s: %v", kind, err)
		}
		if fn.In() != n || fn.Out() < alt.n {
			return cs, fmt.Sprintf("a colour space of type %s of %d inks and %d components, its function of %d inputs and %d outputs", kind, n, alt.n, fn.In(), fn.Out())
		}
		return colourSpace{fam: famFunc, n: n, fn: fn, alt: &alt, fnIn: make([]float64, n), fnOut: make([]float64, fn.Out()),
			decoded: decoded}, ""
	case "":
		return cs, "an image without a colour space"
	}
	return cs, "a colour space of type " + name.NameString()
}

// natural maps a component from 0 to 1 onto its range in the colour
// space: L from 0 to 100, a and b over their ranges.
func (cs *colourSpace) natural(c int, v float64) float64 {
	if cs.fam == famLab {
		switch c {
		case 0:
			return 100 * v
		case 1:
			return cs.ab[0] + v*(cs.ab[1]-cs.ab[0])
		case 2:
			return cs.ab[2] + v*(cs.ab[3]-cs.ab[2])
		}
	}
	return v
}

// colour makes a colour of components in their ranges.
func (cs *colourSpace) colour(c []float64) color.NRGBA {
	b := func(v float64) uint8 { return uint8(math.Round(255 * min(max(v, 0), 1))) }
	switch cs.fam {
	case famGray:
		g := b(c[0])
		return color.NRGBA{g, g, g, 255}
	case famRGB:
		return color.NRGBA{b(c[0]), b(c[1]), b(c[2]), 255}
	case famCMYK:
		k := 1 - min(max(c[3], 0), 1)
		return color.NRGBA{b((1 - c[0]) * k), b((1 - c[1]) * k), b((1 - c[2]) * k), 255}
	case famLab:
		r, g, bl := labToRGB(c[0], c[1], c[2])
		return color.NRGBA{b(r), b(g), b(bl), 255}
	case famFunc:
		copy(cs.fnIn, c)
		cs.fn.Eval(cs.fnIn, cs.fnOut)
		return cs.alt.colour(cs.fnOut[:cs.alt.n])
	}
	return color.NRGBA{A: 255}
}

// labToRGB makes sRGB of CIE L*a*b*: through XYZ relative to its white
// point, which is made the white of sRGB (D65), then the sRGB curve.
func labToRGB(l, a, b float64) (float64, float64, float64) {
	fy := (l + 16) / 116
	fx, fz := fy+a/500, fy-b/200
	g := func(t float64) float64 {
		if t > 6.0/29 {
			return t * t * t
		}
		return 3 * (6.0 / 29) * (6.0 / 29) * (t - 4.0/29)
	}
	x, y, z := g(fx)*0.9505, g(fy), g(fz)*1.089
	lin := [3]float64{
		3.2406*x - 1.5372*y - 0.4986*z,
		-0.9689*x + 1.8758*y + 0.0415*z,
		0.0557*x - 0.2040*y + 1.0570*z,
	}
	for i, v := range lin {
		if v <= 0.0031308 {
			lin[i] = 12.92 * v
		} else {
			lin[i] = 1.055*math.Pow(v, 1/2.4) - 0.055
		}
	}
	return lin[0], lin[1], lin[2]
}

// samples are the decoded samples of an image.
type samples struct {
	data          []byte
	width, height int
	bpc, n        int // bits per component, components per pixel
}

// stride is the bytes of a row.
func (s *samples) stride() int { return (s.width*s.n*s.bpc + 7) / 8 }

// at returns component c of pixel x of row y, from 0 to 2^bpc-1; for 16
// bits, its high byte.
func (s *samples) at(x, y, c int) int {
	row := s.data[y*s.stride():]
	i := x*s.n + c
	switch s.bpc {
	case 8:
		return int(row[i])
	case 16:
		return int(row[2*i])
	case 1, 2, 4:
		bit := i * s.bpc
		return int(row[bit/8]>>(8-s.bpc-bit%8)) & (1<<s.bpc - 1)
	}
	return 0
}

// maxValue is the largest value at returns.
func (s *samples) maxValue() int {
	if s.bpc == 16 {
		return 255
	}
	return 1<<s.bpc - 1
}

var errNotSamples = errors.New("not samples")

// readSamples decodes the samples of an image stream, of n components:
// its filters, and CCITT fax, whose samples are of 1 bit. JPEG data is
// decoded too when jpg is set, into 8-bit gray: for a soft mask. limit
// bounds the bytes decoded; decoded says how many were.
func readSamples(f *file.File, src imageSrc, n int, jpg bool, limit int64) (s samples, decoded int64, err error) {
	xobj := src.v
	w, _ := f.Resolve(xobj.Get("Width")).Integer()
	h, _ := f.Resolve(xobj.Get("Height")).Integer()
	bpc, _ := f.Resolve(xobj.Get("BitsPerComponent")).Integer()
	if im := f.Resolve(xobj.Get("ImageMask")); im.Kind == syntax.Bool && im.N != 0 {
		bpc, n = 1, 1
	}
	if w <= 0 || h <= 0 || w > 1<<20 || h > 1<<20 || w*h > maxImagePixels {
		return s, 0, fmt.Errorf("an image of %d by %d", w, h)
	}
	s = samples{width: int(w), height: int(h), bpc: int(bpc), n: n}
	raw, codec, err := src.decode(f, limit)
	decoded = int64(len(raw))
	if err != nil {
		return s, decoded, err
	}
	switch codec {
	case "":
		switch bpc {
		case 1, 2, 4, 8, 16:
		default:
			return s, decoded, fmt.Errorf("%d bits per component", bpc)
		}
		if need := int64(s.stride()) * h; int64(len(raw)) < need {
			return s, decoded, fmt.Errorf("the image has %d bytes of samples and its size needs %d", len(raw), need)
		}
		s.data = raw
	case "CCITTFaxDecode", "CCF":
		if n != 1 {
			return s, decoded, fmt.Errorf("CCITT fax data in a colour space of %d components", n)
		}
		s.bpc = 1
		if need := int64(s.stride()) * h; need > limit-decoded {
			return s, decoded, file.ErrTruncated
		}
		p := faxParams(f, xobj, int(w))
		out, rows, err := ccitt.Decode(raw, p, int(h))
		decoded += int64(len(out))
		if err != nil && rows == 0 {
			return s, decoded, errors.New("the CCITT fax data is damaged")
		}
		s.data = out
	case "DCTDecode", "DCT":
		if !jpg {
			return s, decoded, errNotSamples
		}
		// The JPEG's own size, before anything is made of that size.
		cfg, err := jpeg.DecodeConfig(bytes.NewReader(raw))
		if err != nil {
			return s, decoded, err
		}
		if cfg.Width != int(w) || cfg.Height != int(h) || cfg.ColorModel != color.GrayModel {
			return s, decoded, errors.New("a JPEG mask that is not gray, or not of its size")
		}
		if w*h > limit-decoded {
			return s, decoded, file.ErrTruncated
		}
		img, err := jpeg.Decode(bytes.NewReader(raw))
		if err != nil {
			return s, decoded, err
		}
		g, ok := img.(*image.Gray)
		if !ok || g.Rect.Dx() != int(w) || g.Rect.Dy() != int(h) {
			return s, decoded, errors.New("a JPEG mask that is not gray")
		}
		decoded += int64(len(g.Pix))
		s.bpc, s.data = 8, g.Pix
		if g.Stride != int(w) {
			return s, decoded, errors.New("a JPEG mask with padded rows")
		}
	default:
		return s, decoded, errNotSamples
	}
	return s, decoded, nil
}

// faxParams reads the parameters of the CCITTFaxDecode filter, the last
// of the image's filters.
func faxParams(f *file.File, xobj syntax.Value, width int) ccitt.Params {
	parms := f.Resolve(xobj.Get("DecodeParms"))
	if parms.Kind == syntax.Array {
		list := parms.List
		parms = syntax.Value{}
		if n := len(list); n > 0 {
			parms = f.Resolve(list[n-1])
		}
	}
	// Rows are decoded at the image's width. The filter's Columns says
	// the same in any file that can be read, and where it is left out
	// it means 1728, a fax machine's width, which an image of another
	// width was not coded at.
	p := ccitt.Params{Columns: width, EndOfBlock: true}
	if k, ok := f.Resolve(parms.Get("K")).Integer(); ok {
		p.K = int(max(min(k, 1<<20), -1))
	}
	if v := f.Resolve(parms.Get("EncodedByteAlign")); v.Kind == syntax.Bool {
		p.EncodedByteAlign = v.N != 0
	}
	if v := f.Resolve(parms.Get("EndOfBlock")); v.Kind == syntax.Bool {
		p.EndOfBlock = v.N != 0
	}
	if v := f.Resolve(parms.Get("BlackIs1")); v.Kind == syntax.Bool {
		p.BlackIs1 = v.N != 0
	}
	return p
}

// decodeArray reads the Decode array of an image of n components:
// the value of each component's 0 and of its largest sample. def gives
// those of a component when the array does not.
func decodeArray(f *file.File, xobj syntax.Value, n int, def func(c int) (float64, float64)) [][2]float64 {
	out := make([][2]float64, n)
	list := f.Resolve(xobj.Get("Decode")).Elems()
	for c := range out {
		lo, hi := def(c)
		if 2*c+1 < len(list) {
			a, ok1 := f.Resolve(list[2*c]).Float()
			b, ok2 := f.Resolve(list[2*c+1]).Float()
			if ok1 && ok2 && !math.IsNaN(a) && !math.IsNaN(b) && !math.IsInf(a, 0) && !math.IsInf(b, 0) {
				lo, hi = a, b
			}
		}
		out[c] = [2]float64{lo, hi}
	}
	return out
}

// raster makes an image of samples: of the colour space cs, or a
// stencil mask, with alpha (nil for none) of one byte a pixel. It
// returns the image and the bytes of its pixels.
func raster(f *file.File, xobj syntax.Value, s *samples, cs *colourSpace, stencil bool, alpha []byte) (image.Image, int64, error) {
	w, h := s.width, s.height
	rect := image.Rect(0, 0, w, h)
	maxv := s.maxValue()
	// The value of each sample of each component, by a table.
	natural := func(c int) (float64, float64) {
		switch {
		case stencil:
			return 0, 1
		case cs.fam == famIndexed:
			return 0, float64(int(1)<<s.bpc - 1)
		case cs.fam == famLab:
			return cs.natural(c, 0), cs.natural(c, 1)
		}
		return 0, 1
	}
	dec := decodeArray(f, xobj, s.n, natural)
	tables := make([][]float64, s.n)
	for c := range tables {
		tables[c] = make([]float64, maxv+1)
		for v := range tables[c] {
			tables[c][v] = dec[c][0] + float64(v)*(dec[c][1]-dec[c][0])/float64(maxv)
		}
	}
	// One sample a pixel, and no mask: a palette, which PNG writes at the
	// depth of the samples.
	if alpha == nil && s.n == 1 && (stencil || cs.fam == famIndexed || cs.fam == famFunc || cs.fam == famGray && s.bpc <= 4) {
		pal := make(color.Palette, maxv+1)
		for v := range pal {
			x := tables[0][v]
			switch {
			case stencil:
				// 0 paints (black), 1 leaves the page as it is (white).
				g := 255 * btou8(x >= 0.5)
				pal[v] = color.NRGBA{g, g, g, 255}
			case cs.fam == famIndexed:
				i := int(math.Round(x))
				pal[v] = cs.palette[min(max(i, 0), cs.hival)]
			default:
				pal[v] = cs.colour([]float64{x})
			}
		}
		img := image.NewPaletted(rect, pal)
		for y := 0; y < h; y++ {
			row := img.Pix[y*img.Stride:]
			for x := 0; x < w; x++ {
				row[x] = uint8(s.at(x, y, 0))
			}
		}
		return img, int64(len(img.Pix)), nil
	}
	if alpha == nil && cs.fam == famGray {
		img := image.NewGray(rect)
		lut := make([]uint8, maxv+1)
		for v := range lut {
			lut[v] = cs.colour([]float64{tables[0][v]}).R
		}
		for y := 0; y < h; y++ {
			row := img.Pix[y*img.Stride:]
			for x := 0; x < w; x++ {
				row[x] = lut[s.at(x, y, 0)]
			}
		}
		return img, int64(len(img.Pix)), nil
	}
	img := image.NewNRGBA(rect)
	comps := make([]float64, s.n)
	var tint *tints
	if cs.fam == famFunc && !stencil {
		tint = newTints(cs, tables)
	}
	// Device colours by a table of each component; Lab and indexed
	// colours pixel by pixel.
	var luts [][]uint8
	if cs.fam == famGray || cs.fam == famRGB || cs.fam == famCMYK || stencil {
		luts = make([][]uint8, s.n)
		for c := range luts {
			luts[c] = make([]uint8, maxv+1)
			for v := range luts[c] {
				luts[c][v] = uint8(math.Round(255 * min(max(tables[c][v], 0), 1)))
			}
		}
	}
	for y := 0; y < h; y++ {
		row := img.Pix[y*img.Stride:]
		for x := 0; x < w; x++ {
			p := row[4*x : 4*x+4]
			switch {
			case stencil:
				g := 255 * (luts[0][s.at(x, y, 0)] >> 7)
				p[0], p[1], p[2] = g, g, g
			case cs.fam == famGray:
				g := luts[0][s.at(x, y, 0)]
				p[0], p[1], p[2] = g, g, g
			case cs.fam == famRGB:
				p[0], p[1], p[2] = luts[0][s.at(x, y, 0)], luts[1][s.at(x, y, 1)], luts[2][s.at(x, y, 2)]
			case cs.fam == famCMYK:
				k := 255 - int(luts[3][s.at(x, y, 3)])
				p[0] = uint8((255 - int(luts[0][s.at(x, y, 0)])) * k / 255)
				p[1] = uint8((255 - int(luts[1][s.at(x, y, 1)])) * k / 255)
				p[2] = uint8((255 - int(luts[2][s.at(x, y, 2)])) * k / 255)
			case cs.fam == famIndexed:
				i := int(math.Round(tables[0][s.at(x, y, 0)]))
				col := cs.palette[min(max(i, 0), cs.hival)]
				p[0], p[1], p[2] = col.R, col.G, col.B
			case tint != nil:
				col, ok := tint.at(s, x, y)
				if !ok {
					return nil, 0, errors.New("the colour space's function would take too long over the image")
				}
				p[0], p[1], p[2] = col.R, col.G, col.B
			default:
				for c := range comps {
					comps[c] = tables[c][s.at(x, y, c)]
				}
				col := cs.colour(comps)
				p[0], p[1], p[2] = col.R, col.G, col.B
			}
			p[3] = 255
			if alpha != nil {
				p[3] = alpha[y*w+x]
			}
		}
	}
	return img, int64(len(img.Pix)), nil
}

// tints makes the colours of the samples of a colour space made by a
// function. Of one ink, the colour of each sample is made once; of more,
// the colours last made are kept by their samples, and the work of the
// function over the image is bounded by maxFunctionWork.
type tints struct {
	cs     *colourSpace
	tables [][]float64 // the value of each sample of each ink
	one    []color.NRGBA
	// A cache of colours by their samples, a byte each, for up to eight
	// inks.
	keys       []uint64
	set        []bool
	cols       []color.NRGBA
	comps      []float64
	work, cost int
}

const tintCacheBits = 12

func newTints(cs *colourSpace, tables [][]float64) *tints {
	t := &tints{cs: cs, tables: tables, comps: make([]float64, cs.n), cost: max(cs.fn.Cost(), 1)}
	switch {
	case cs.n == 1:
		t.one = make([]color.NRGBA, len(tables[0]))
		for v := range t.one {
			t.one[v] = cs.colour([]float64{tables[0][v]})
		}
	case cs.n <= 8:
		t.keys = make([]uint64, 1<<tintCacheBits)
		t.set = make([]bool, 1<<tintCacheBits)
		t.cols = make([]color.NRGBA, 1<<tintCacheBits)
	}
	return t
}

// at returns the colour of pixel x of row y; ok is false when the work
// is spent.
func (t *tints) at(s *samples, x, y int) (col color.NRGBA, ok bool) {
	if t.one != nil {
		return t.one[s.at(x, y, 0)], true
	}
	slot := -1
	if t.keys != nil {
		var key uint64
		for c := range t.comps {
			key = key<<8 | uint64(s.at(x, y, c))
		}
		slot = int(key * 0x9E3779B97F4A7C15 >> (64 - tintCacheBits))
		if t.set[slot] && t.keys[slot] == key {
			return t.cols[slot], true
		}
		t.keys[slot], t.set[slot] = key, true
	}
	if t.work += t.cost; t.work > maxFunctionWork {
		return col, false
	}
	for c := range t.comps {
		t.comps[c] = t.tables[c][s.at(x, y, c)]
	}
	col = t.cs.colour(t.comps)
	if slot >= 0 {
		t.cols[slot] = col
	}
	return col, true
}

// imageAlpha reads the mask of an image of samples s: how opaque each
// of its pixels is, nil for an image without a mask or with one that
// cannot be read. decoded says how many bytes were decoded for it.
func imageAlpha(f *file.File, xobj syntax.Value, s *samples, limit int64) (alpha []byte, decoded int64) {
	w, h := s.width, s.height
	switch m := f.Resolve(xobj.Get("SMask")); {
	case m.Kind == syntax.Stream:
		ms, d, err := readSamples(f, imageSrc{v: m}, 1, true, limit)
		decoded += d
		if err != nil || int64(w*h) > limit-decoded {
			return nil, decoded
		}
		dec := decodeArray(f, m, 1, func(int) (float64, float64) { return 0, 1 })
		maxv := ms.maxValue()
		lut := make([]uint8, maxv+1)
		for v := range lut {
			lut[v] = uint8(math.Round(255 * min(max(dec[0][0]+float64(v)*(dec[0][1]-dec[0][0])/float64(maxv), 0), 1)))
		}
		alpha = make([]byte, w*h)
		decoded += int64(len(alpha))
		for y := 0; y < h; y++ {
			my := y * ms.height / h
			for x := 0; x < w; x++ {
				alpha[y*w+x] = lut[ms.at(x*ms.width/w, my, 0)]
			}
		}
		return alpha, decoded
	}
	switch m := f.Resolve(xobj.Get("Mask")); m.Kind {
	case syntax.Stream:
		// A stencil: where its sample, decoded, is 1 the image is left
		// out.
		ms, d, err := readSamples(f, imageSrc{v: m}, 1, false, limit)
		decoded += d
		if err != nil || ms.bpc != 1 || int64(w*h) > limit-decoded {
			return nil, decoded
		}
		dec := decodeArray(f, m, 1, func(int) (float64, float64) { return 0, 1 })
		out := [2]uint8{255 * btou8(dec[0][0] < 0.5), 255 * btou8(dec[0][1] < 0.5)}
		alpha = make([]byte, w*h)
		decoded += int64(len(alpha))
		for y := 0; y < h; y++ {
			my := y * ms.height / h
			for x := 0; x < w; x++ {
				alpha[y*w+x] = out[ms.at(x*ms.width/w, my, 0)]
			}
		}
		return alpha, decoded
	case syntax.Array:
		// Ranges of colour: a pixel whose every sample, as written, is
		// in its range is left out.
		list := m.List
		if len(list) < 2*s.n || int64(w*h) > limit {
			return nil, 0
		}
		ranges := make([][2]int, s.n)
		for c := range ranges {
			lo, _ := f.Resolve(list[2*c]).Integer()
			hi, _ := f.Resolve(list[2*c+1]).Integer()
			if s.bpc == 16 {
				lo, hi = lo>>8, hi>>8
			}
			ranges[c] = [2]int{int(lo), int(hi)}
		}
		alpha = make([]byte, w*h)
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				out := true
				for c, r := range ranges {
					if v := s.at(x, y, c); v < r[0] || v > r[1] {
						out = false
						break
					}
				}
				alpha[y*w+x] = 255 * btou8(!out)
			}
		}
		return alpha, int64(len(alpha))
	}
	return nil, 0
}

func btou8(v bool) uint8 {
	if v {
		return 1
	}
	return 0
}
