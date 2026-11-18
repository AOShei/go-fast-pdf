// Package font turns a font dictionary into what text extraction needs:
// a mapping from character codes to text, and glyph widths.
//
// In Phase 1 this is the font logic of v0.1.1 on the new engine, with the
// same behaviour: one- and two-byte ToUnicode entries, /Differences with
// a small glyph-name table, and /Widths for simple fonts. Its known
// defects (F1 to F5, B3 in IMPLEMENTATION_PLAN.md) are closed in Phase 4,
// which replaces most of this package.
package font

import (
	"errors"
	"io"
	"unicode/utf16"

	"github.com/AOShei/go-fast-pdf/internal/file"
	"github.com/AOShei/go-fast-pdf/internal/syntax"
)

// Font is immutable once loaded and is shared by every page that uses it.
type Font struct {
	BaseFont string

	// ToUnicode entries with one-byte and two-byte codes. HasCMap is true
	// when the font has any ToUnicode entry at all, of any code length.
	one     [256]string
	hasOne  [256]bool
	two     map[uint16]string
	HasCMap bool

	// Text for the codes that /Differences names. HasEncoding is true
	// when /Differences names any code.
	enc         [256]string
	hasEnc      [256]bool
	HasEncoding bool

	// Widths from /FirstChar and /Widths, in thousandths of the font
	// size. HasWidths is true when the font gives any.
	widths    [256]float64
	hasWidth  [256]bool
	HasWidths bool

	// SpaceWidth is the width of code 32, or 250 when unknown.
	SpaceWidth float64
}

// One returns the ToUnicode text of a one-byte code.
func (f *Font) One(code byte) (string, bool) { return f.one[code], f.hasOne[code] }

// Two returns the ToUnicode text of a two-byte code.
func (f *Font) Two(hi, lo byte) (string, bool) {
	s, ok := f.two[uint16(hi)<<8|uint16(lo)]
	return s, ok
}

// Encoded returns the text /Differences gives a code.
func (f *Font) Encoded(code byte) (string, bool) { return f.enc[code], f.hasEnc[code] }

// Width returns the width of a code and whether the font states it.
func (f *Font) Width(code byte) (float64, bool) { return f.widths[code], f.hasWidth[code] }

type cacheKey struct{ obj int64 }

// Load returns the font that ref refers to, or nil if it is not a font
// dictionary. Fonts that are indirect objects are parsed once per file.
func Load(f *file.File, ref syntax.Value) *Font {
	num := file.RefNumber(ref)
	if num != 0 {
		if cached, ok := f.Cache.Load(cacheKey{num}); ok {
			return cached.(*Font)
		}
	}
	dict := f.Resolve(ref)
	if dict.Kind != syntax.Dict {
		return nil
	}
	font := parse(f, dict)
	if num != 0 {
		actual, _ := f.Cache.LoadOrStore(cacheKey{num}, font)
		return actual.(*Font)
	}
	return font
}

func parse(f *file.File, dict syntax.Value) *Font {
	font := &Font{SpaceWidth: 250}
	font.BaseFont = f.Resolve(dict.Get("BaseFont")).NameString()

	// Widths of a simple font: /Widths[i] is the width of /FirstChar + i.
	if first, ok := f.Resolve(dict.Get("FirstChar")).Float(); ok {
		for i, w := range f.Resolve(dict.Get("Widths")).Elems() {
			width, ok := w.Float()
			if !ok {
				continue
			}
			font.HasWidths = true
			if code := int(first) + i; code >= 0 && code < 256 {
				font.widths[code], font.hasWidth[code] = width, true
			}
		}
	}
	if font.hasWidth[32] {
		font.SpaceWidth = font.widths[32]
	}

	// A ToUnicode CMap, when present and readable, is the only mapping
	// used; otherwise /Differences is.
	if tu := f.Resolve(dict.Get("ToUnicode")); tu.Kind == syntax.Stream {
		if data, err := f.Decode(tu, nil, 0); err == nil || len(data) > 0 {
			font.parseCMap(data)
			return font
		}
	}
	if enc := f.Resolve(dict.Get("Encoding")); enc.Kind == syntax.Dict {
		code := 0
		for _, item := range f.Resolve(enc.Get("Differences")).Elems() {
			switch item.Kind {
			case syntax.Int, syntax.Real:
				code = int(item.F)
			case syntax.Name:
				font.HasEncoding = true
				if code >= 0 && code < 256 {
					font.enc[code], font.hasEnc[code] = glyphText(item.Data, byte(code)), true
				}
				code++
			}
		}
	}
	return font
}

// glyphText returns the text for a glyph name: from the table, else the
// name itself if it is a single character, else the code as a byte.
func glyphText(name []byte, code byte) string {
	if text, ok := glyphNames[string(name)]; ok {
		return text
	}
	if len(name) == 1 {
		return string(name)
	}
	return string([]byte{code})
}

// parseCMap reads the bfchar and bfrange sections of a ToUnicode CMap.
// Everything else in it is PostScript that is stepped over.
func (font *Font) parseCMap(data []byte) {
	p := syntax.NewParser(data, 0, 0)
	// next returns the next object, stepping over bytes that start none,
	// such as the braces of a PostScript procedure.
	next := func() (syntax.Value, bool) {
		for {
			v, err := p.Object()
			if err == nil {
				return v, true
			}
			if err == io.EOF || !errors.Is(err, syntax.ErrSyntax) {
				return syntax.Value{}, false
			}
			p.SetPos(p.Pos() + 1)
		}
	}
	isHex := func(v syntax.Value) bool { return v.Kind == syntax.String && v.Hex }

	for {
		v, ok := next()
		if !ok {
			return
		}
		if v.Kind != syntax.Keyword {
			continue
		}
		switch string(v.Data) {
		case "beginbfchar":
			for {
				src, ok := next()
				if !ok {
					return
				}
				if src.Kind == syntax.Keyword {
					if src.Equal("endbfchar") {
						break
					}
					continue
				}
				dst, ok := next()
				if !ok {
					return
				}
				if isHex(src) && isHex(dst) {
					font.add(src.Data, decodeUTF16BE(dst.Data))
				}
			}
		case "beginbfrange":
			for {
				lo, ok := next()
				if !ok {
					return
				}
				if lo.Kind == syntax.Keyword {
					if lo.Equal("endbfrange") {
						break
					}
					continue
				}
				hi, ok1 := next()
				dst, ok2 := next()
				if !ok1 || !ok2 {
					return
				}
				if isHex(lo) && isHex(hi) {
					font.addRange(lo.Data, hi.Data, dst)
				}
			}
		}
	}
}

func (font *Font) add(code []byte, text string) {
	font.HasCMap = true
	switch len(code) {
	case 1:
		font.one[code[0]], font.hasOne[code[0]] = text, true
	case 2:
		if font.two == nil {
			font.two = map[uint16]string{}
		}
		font.two[uint16(code[0])<<8|uint16(code[1])] = text
	}
	// Codes of other lengths are never looked up by this version.
}

func bytesToInt(b []byte) (int64, bool) {
	if len(b) > 7 {
		return 0, false
	}
	var v int64
	for _, c := range b {
		v = v<<8 | int64(c)
	}
	return v, true
}

func intToBytes(v int64, n int) []byte {
	out := make([]byte, n)
	for i := n - 1; i >= 0; i-- {
		out[i] = byte(v)
		v >>= 8
	}
	return out
}

// addRange adds a bfrange entry: codes lo to hi map either to the
// elements of an array, or to consecutive values starting at a string.
func (font *Font) addRange(lo, hi []byte, dst syntax.Value) {
	start, ok1 := bytesToInt(lo)
	end, ok2 := bytesToInt(hi)
	if !ok1 || !ok2 || end < start {
		return
	}
	n := len(lo)
	if n != 1 && n != 2 {
		// Never looked up; only the fact that the font has a CMap counts.
		if dst.Kind == syntax.Array && len(dst.List) > 0 || dst.Kind == syntax.String && dst.Hex {
			font.HasCMap = true
		}
		return
	}
	// A code of n bytes has at most 256^n values, whatever the range says.
	last := min(end, int64(1)<<(8*n)-1)
	switch {
	case dst.Kind == syntax.Array:
		for i, elem := range dst.List {
			if code := start + int64(i); code <= last && elem.Kind == syntax.String && elem.Hex {
				font.add(intToBytes(code, n), decodeUTF16BE(elem.Data))
			}
		}
	case dst.Kind == syntax.String && dst.Hex:
		base, ok := bytesToInt(dst.Data)
		if !ok {
			return
		}
		for code := start; code <= last; code++ {
			font.add(intToBytes(code, n), decodeUTF16BE(intToBytes(base+code-start, len(dst.Data))))
		}
	}
}

func decodeUTF16BE(b []byte) string {
	if len(b)%2 != 0 {
		return string(b)
	}
	units := make([]uint16, len(b)/2)
	for i := range units {
		units[i] = uint16(b[2*i])<<8 | uint16(b[2*i+1])
	}
	return string(utf16.Decode(units))
}
