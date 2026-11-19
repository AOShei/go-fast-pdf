// Package font turns a font dictionary into what text extraction needs:
// how a string splits into character codes, the text of each code, and
// how far each glyph advances.
//
// Where the text comes from, in order of preference:
//
//   - the font's ToUnicode CMap;
//   - for a simple font, its encoding: one of the encodings the
//     specification names, changed by /Differences, whose glyph names
//     are read by the rules of the Adobe Glyph List;
//   - for a composite font whose character codes are Unicode already
//     (the predefined CMaps with UCS2, UTF16 or UTF32 in their name),
//     the codes themselves.
//
// A simple font can leave a glyph without any of these: its name is one
// some program made up ("g23", "a192"), or it has none. Such a glyph is
// read as the character its code stands for in ASCII, when the code is
// one: fonts made by subsetting keep the codes of the characters far
// more often than not, and every reader of PDF relies on it. A name
// that is a number equal to the code extends this to Latin-1. These
// characters are counted as assumed (Shown.Assumed), so that a caller
// can say how much of a page rests on the assumption.
//
// A code with no text by any of these is reported as unmapped and
// appears as U+FFFD. Font programs are not read.
//
// A ligature (U+FB00 to U+FB06) is written as its letters, whether the
// text comes from a glyph name or a CMap states the ligature character.
package font

import (
	"math"
	"sort"
	"strings"
	"sync/atomic"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/AOShei/go-fast-pdf/internal/charset"
	"github.com/AOShei/go-fast-pdf/internal/file"
	"github.com/AOShei/go-fast-pdf/internal/syntax"
)

// Font is immutable once loaded and is shared by every page that uses it.
type Font struct {
	// BaseFont is the font's name, with a subset tag if it has one.
	BaseFont string
	// Composite is true for a Type0 font, whose codes can have several
	// bytes.
	Composite bool
	// Vertical is true for a composite font that writes downwards.
	Vertical bool
	// Type3 is true for a font whose glyphs are content streams.
	Type3 bool
	// SpaceWidth is the advance of the space, as a fraction of the font
	// size. It is 0.25 when the font does not say.
	SpaceWidth float64
	// Truncated is set when a table of the font was larger than is kept.
	Truncated bool
	// LongestText is the length in bytes of the longest text that one
	// character code of the font stands for.
	LongestText int
	// Bold is set for a font whose strokes are heavier than a regular
	// font's: its descriptor says so (a weight of 600 or more, or the
	// flag that forces bold), or its name does.
	Bold bool

	// A simple font: the text and the advance of each of its 256 codes.
	// An empty text is a code with no mapping, unless the code is blank:
	// then the font has no glyph for it and draws nothing.
	text    [256]string
	one     [256]byte // the text of a code when it is one ASCII byte, else 0
	blank   [256]bool
	widths  [256]float32
	assumed *[256]bool // codes read by the ASCII assumption; nil if none

	// A composite font.
	space   codespace
	toUni   *cmap        // ToUnicode
	cids    *cmap        // code to CID; nil when the CID is the code
	noCIDs  bool         // the code to CID mapping is not known
	unicode unicodeCodes // the codes are Unicode
	dw      float32      // default advance
	w       []cidWidth   // sorted by first
	wArr    []float32
}

// cidWidth gives the advance of the CIDs first to last: all w, or, when
// arr is not negative, wArr[arr], wArr[arr+1] and so on.
type cidWidth struct {
	first, last uint32
	w           float32
	arr         int32
}

// Shown describes a string after Append.
type Shown struct {
	// Advance is the sum of the glyph advances, as a fraction of the
	// font size. For a vertical font it is negative: down the page.
	Advance float64
	// Glyphs is the number of character codes.
	Glyphs int
	// Spaces is the number of one-byte codes with the value 32, the only
	// ones word spacing applies to.
	Spaces int
	// Unmapped is the number of codes with no text. Each is U+FFFD in
	// the output.
	Unmapped int
	// Assumed is the number of codes whose text is taken from the code
	// itself, because the font does not say what its glyph is.
	Assumed int
}

const replacement = "�"

// Append appends the text of the string s, as shown in this font, to
// dst. The result is valid UTF-8 whatever s holds.
func (f *Font) Append(dst, s []byte) ([]byte, Shown) {
	var sh Shown
	if !f.Composite {
		var adv float32
		for _, c := range s {
			if b := f.one[c]; b != 0 {
				// Most text is this: one code, one ASCII character.
				dst = append(dst, b)
			} else if t := f.text[c]; t != "" {
				dst = append(dst, t...)
			} else if !f.blank[c] {
				dst = append(dst, replacement...)
				sh.Unmapped++
			}
			adv += f.widths[c]
			if c == ' ' {
				sh.Spaces++
			}
		}
		sh.Advance, sh.Glyphs = float64(adv), len(s)
		if f.assumed != nil {
			for _, c := range s {
				if f.assumed[c] {
					sh.Assumed++
				}
			}
		}
		return dst, sh
	}
	start := len(dst)
	for len(s) > 0 {
		code, n := f.space.next(s)
		s = s[n:]
		sh.Glyphs++
		if n == 1 && code == ' ' {
			sh.Spaces++
		}
		ok := false
		if f.toUni != nil {
			dst, ok = f.toUni.appendText(dst, code, n)
		}
		if !ok && f.unicode != 0 {
			dst, ok = appendUnicode(dst, code, n, f.unicode)
		}
		if !ok {
			dst = append(dst, replacement...)
			sh.Unmapped++
		}
		cid := code
		if f.cids != nil {
			cid = f.cids.cid(code, n)
		}
		if f.noCIDs {
			sh.Advance += float64(f.dw)
		} else {
			sh.Advance += float64(f.cidWidth(cid))
		}
	}
	return charset.LettersIn(dst, start), sh
}

// CodeLen returns the number of bytes of the first character code of s,
// which must not be empty.
func (f *Font) CodeLen(s []byte) int {
	if !f.Composite {
		return 1
	}
	_, n := f.space.next(s)
	return n
}

// appendUnicode appends a character code that is Unicode already.
func appendUnicode(dst []byte, code uint32, n int, form unicodeCodes) ([]byte, bool) {
	if form == asciiOneByte {
		if n == 1 && code >= 0x20 && code <= 0x7E {
			return append(dst, byte(code)), true
		}
		return dst, false
	}
	r := rune(code)
	if form == unicodeUTF16 && n == 4 {
		r = utf16.DecodeRune(rune(code>>16), rune(code&0xFFFF))
	}
	if r == 0 || r > utf8.MaxRune || r >= 0xD800 && r <= 0xDFFF || r == utf8.RuneError {
		return dst, false
	}
	return utf8.AppendRune(dst, r), true
}

func (f *Font) cidWidth(cid uint32) float32 {
	ws := f.w
	i := sort.Search(len(ws), func(i int) bool { return ws[i].first > cid }) - 1
	// Ranges are few and seldom overlap; look a little way back for one
	// that reaches the CID.
	for k := 0; i >= 0 && k < 4; i, k = i-1, k+1 {
		if e := &ws[i]; cid <= e.last {
			if e.arr >= 0 {
				return f.wArr[e.arr+int32(cid-e.first)]
			}
			return e.w
		}
	}
	return f.dw
}

// Default is the font used when a page names one that does not exist:
// StandardEncoding and glyphs half as wide as they are high.
var Default = func() *Font {
	f := &Font{SpaceWidth: 0.25}
	f.text = *charset.Standard()
	for i := range f.widths {
		f.widths[i] = 0.5
	}
	f.index()
	return f
}()

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
		actual, loaded := f.Cache.LoadOrStore(cacheKey{num}, font)
		if !loaded {
			// A file can have a font of its own on every page. What is
			// kept is bounded: beyond it everything is let go, and a font
			// that is still wanted is parsed again. A page keeps the
			// fonts it is using by itself.
			counter, _ := f.Cache.LoadOrStore(countKey{}, new(atomic.Int64))
			if n := counter.(*atomic.Int64); n.Add(1) > maxCachedFonts {
				f.Cache.Range(func(key, _ any) bool {
					if _, ok := key.(cacheKey); ok {
						f.Cache.Delete(key)
					}
					return true
				})
				n.Store(0)
			}
		}
		return actual.(*Font)
	}
	return font
}

// bold reports whether a font is bold: by the weight or the flags of
// its descriptor (that of its descendant, for a composite font), or by
// the name of the font.
func bold(f *file.File, dict syntax.Value, name string) bool {
	// The name first: it settles most fonts without reading more. Arial-
	// BoldMT, TimesNewRomanPS-BoldItalicMT, Helvetica-Black,
	// MyriadPro-Semibold, NimbusSans-Heavy, CMBX10 (TeX's bold extended).
	for _, w := range []string{"bold", "black", "heavy", "demi"} { // "semibold" has "bold"
		if containsFold(name, w) {
			return true
		}
	}
	if i := strings.IndexByte(name, '+'); i >= 0 {
		name = name[i+1:]
	}
	if strings.HasPrefix(name, "CMB") {
		return true
	}
	descriptor := f.Resolve(dict.Get("FontDescriptor"))
	if !descriptor.IsDict() {
		if kids := f.Resolve(dict.Get("DescendantFonts")).Elems(); len(kids) > 0 {
			descriptor = f.Resolve(f.Resolve(kids[0]).Get("FontDescriptor"))
		}
	}
	if weight, ok := f.Resolve(descriptor.Get("FontWeight")).Float(); ok && weight >= 600 {
		return true
	}
	const forceBold = 1 << 18
	flags, _ := f.Resolve(descriptor.Get("Flags")).Integer()
	return flags&forceBold != 0
}

// containsFold reports whether s contains the lower-case ASCII word w,
// in any case, without making a copy of s.
func containsFold(s, w string) bool {
	for i := 0; i+len(w) <= len(s); i++ {
		j := 0
		for j < len(w) && s[i+j]|0x20 == w[j] {
			j++
		}
		if j == len(w) {
			return true
		}
	}
	return false
}

// maxCachedFonts is the number of fonts of a file that are kept at a
// time.
const maxCachedFonts = 1024

type countKey struct{}

// maxAdvance bounds a glyph advance, as a fraction of the font size. A
// file can state any number; no glyph is a hundred times wider than it
// is high.
const maxAdvance = 100

func advance(v float64) float32 {
	if !(v > -maxAdvance) { // also catches NaN
		return -maxAdvance
	}
	if v > maxAdvance {
		return maxAdvance
	}
	return float32(v)
}

func parse(f *file.File, dict syntax.Value) *Font {
	font := &Font{SpaceWidth: 0.25}
	font.BaseFont = f.Resolve(dict.Get("BaseFont")).NameString()

	var toUni *cmap
	tu := f.Resolve(dict.Get("ToUnicode"))
	// /ToUnicode /Identity-H says that the codes are Unicode already.
	font.unicode = 0
	if tu.IsName("Identity-H") || tu.IsName("Identity-V") {
		font.unicode = unicodeUCS2
	}
	if tu.Kind == syntax.Stream {
		if data, err := f.Decode(tu, nil, 0); err == nil || len(data) > 0 {
			if c := parseCMap(data); len(c.entries) > 0 {
				toUni = c
				font.Truncated = c.truncated
			}
		}
	}

	font.Bold = bold(f, dict, font.BaseFont)
	if f.Resolve(dict.Get("Subtype")).IsName("Type0") {
		font.parseComposite(f, dict, toUni)
		if font.toUni != nil {
			for i := range font.toUni.entries {
				if e := &font.toUni.entries[i]; e.kind != kindCID {
					font.LongestText = max(font.LongestText, int(e.tlen))
				}
			}
		}
	} else {
		font.parseSimple(f, dict, toUni)
		font.index()
	}
	return font
}

// index fills the table of one-byte texts from the texts of the codes.
func (f *Font) index() {
	for code, t := range f.text {
		// A ToUnicode map can state a ligature character.
		t = charset.Letters(t)
		f.text[code] = t
		f.LongestText = max(f.LongestText, len(t))
		f.one[code] = 0
		if len(t) == 1 && t[0] < 0x80 && t[0] != 0 {
			f.one[code] = t[0]
		}
	}
}

// --- simple fonts ---

func (font *Font) parseSimple(f *file.File, dict syntax.Value, toUni *cmap) {
	subtype := f.Resolve(dict.Get("Subtype"))
	font.Type3 = subtype.IsName("Type3")
	descriptor := f.Resolve(dict.Get("FontDescriptor"))
	flags, _ := f.Resolve(descriptor.Get("Flags")).Integer()
	const fixedPitch, symbolic = 1 << 0, 1 << 2
	core := charset.Core(font.BaseFont)

	// The encoding the differences apply to. A font that names none has
	// the encoding built into its program, which is not read here: a
	// standard font has the one the specification gives it, and for any
	// other the usual one for its kind is assumed.
	var base *charset.Encoding
	enc := f.Resolve(dict.Get("Encoding"))
	switch {
	case enc.Kind == syntax.Name:
		base = charset.Named(enc.NameString())
	case enc.Kind == syntax.Dict:
		base = charset.Named(f.Resolve(enc.Get("BaseEncoding")).NameString())
	}
	// certain: the encoding is the one the file names, or the one of a
	// font that is not embedded and so can only be a standard one. A
	// code it has no character for has no glyph and draws nothing. In an
	// embedded font without a named encoding such a code may well have a
	// glyph, of which nothing is known.
	embedded := descriptor.Has("FontFile") || descriptor.Has("FontFile2") || descriptor.Has("FontFile3")
	certain := base != nil || !embedded
	glyphText := charset.GlyphText
	if core != nil && core.Dingbats() {
		glyphText = charset.DingbatText
	}
	var unknown [256]string // glyph names that say nothing
	if base == nil {
		// A Type 1 font program states its encoding in clear text.
		if names, standard, ok := type1Encoding(f, descriptor); ok {
			certain = true
			if standard {
				base = charset.Standard()
			} else {
				base = &charset.Encoding{}
				for code, name := range names {
					if name == "" || name == ".notdef" {
						continue
					}
					t, ok := glyphText(name)
					base[code] = t
					if !ok {
						unknown[code] = name
					}
				}
			}
		}
	}
	if base == nil {
		switch {
		case font.Type3:
			base, certain = &charset.Encoding{}, true
		case core != nil:
			base = core.Builtin
		case subtype.IsName("TrueType") && flags&symbolic == 0:
			base = charset.WinAnsi()
		default:
			base = charset.Standard()
		}
	}
	font.text = *base
	for code, t := range font.text {
		font.blank[code] = certain && t == "" && unknown[code] == ""
	}

	// /Differences: a number sets the code, each name that follows is
	// the glyph of the next code.
	if enc.Kind == syntax.Dict {
		code := -1
		for _, item := range f.Resolve(enc.Get("Differences")).Elems() {
			item = f.Resolve(item)
			switch item.Kind {
			case syntax.Int, syntax.Real:
				code = -1
				if item.F >= 0 && item.F < 256 {
					code = int(item.F)
				}
			case syntax.Name:
				if code >= 0 && code < 256 {
					// .notdef is what producers write for a code they
					// do not use; it changes nothing.
					if name := string(item.Data); name != ".notdef" {
						t, ok := glyphText(name)
						font.text[code], font.blank[code], unknown[code] = t, false, ""
						if !ok {
							unknown[code] = name
						}
					}
					code++
				}
			}
		}
	}

	// Glyphs the font says nothing about: read by their codes.
	for code := range font.text {
		if font.text[code] != "" || !font.blank[code] && unknown[code] == "" {
			continue
		}
		t := ""
		switch {
		case code >= 0x20 && code <= 0x7E:
			t = string(rune(code))
		case code >= 0xA1 && unknown[code] != "" && nameNumber(unknown[code]) == code:
			t = string(rune(code))
		}
		if t != "" {
			if font.assumed == nil {
				font.assumed = new([256]bool)
			}
			font.text[code], font.blank[code], font.assumed[code] = t, false, true
		}
	}

	// ToUnicode has the last word, code by code.
	if toUni != nil {
		var buf []byte
		for code := range font.text {
			// An entry with no text at all leaves the encoding's text.
			if t, ok := toUni.appendText(buf[:0], uint32(code), 1); ok && len(t) > 0 {
				buf = t
				font.text[code] = string(t)
				if font.assumed != nil {
					font.assumed[code] = false
				}
			}
		}
	}

	// Widths. /Widths is in units of a thousandth of the font size, or,
	// for a Type3 font, in the units of its /FontMatrix.
	unit := 0.001
	if font.Type3 {
		if m := f.Resolve(dict.Get("FontMatrix")).Elems(); len(m) == 6 {
			if a, ok := f.Resolve(m[0]).Float(); ok && a != 0 && !math.IsNaN(a) {
				unit = math.Abs(a)
			}
		}
	}
	missing, _ := f.Resolve(descriptor.Get("MissingWidth")).Float()
	widths := f.Resolve(dict.Get("Widths")).Elems()
	first, hasFirst := f.Resolve(dict.Get("FirstChar")).Float()
	var stated [256]bool // codes whose width the font states
	switch {
	case len(widths) > 0 && hasFirst:
		for code := range font.widths {
			font.widths[code] = advance(missing * unit)
		}
		for i, w := range widths {
			width, ok := f.Resolve(w).Float()
			if code := first + float64(i); ok && code >= 0 && code < 256 {
				font.widths[int(code)] = advance(width * unit)
				stated[int(code)] = true
			}
		}
	default:
		// No widths: a standard font has known ones, and any other is
		// taken to be of average width.
		fallback := 500.0
		if missing > 0 {
			fallback = missing
		} else if flags&fixedPitch != 0 {
			fallback = 600
		}
		for code := range font.widths {
			width := fallback
			if core != nil {
				if w, ok := core.Width(byte(code), font.text[code]); ok {
					width, stated[code] = w, true
				} else if w, ok := core.Width(byte(code), base[code]); ok {
					width, stated[code] = w, true
				}
			}
			font.widths[code] = advance(width * 0.001)
		}
	}

	// The space: code 32 if it is one, else whichever code is. Only a
	// width the font states for it counts: many fonts have no space at
	// all, and what stands in for a missing width says nothing.
	space := -1
	if font.text[' '] == " " {
		space = ' '
	} else {
		for code, t := range font.text {
			if t == " " {
				space = code
				break
			}
		}
	}
	if space >= 0 && stated[space] && font.widths[space] > 0 {
		font.SpaceWidth = float64(font.widths[space])
	}
}

// nameNumber returns the number a made-up glyph name ends in, when the
// name is up to three letters followed by digits ("a192", "g65", "c7"),
// or -1.
func nameNumber(name string) int {
	i := 0
	for i < len(name) && i < 3 && (name[i] >= 'a' && name[i] <= 'z' || name[i] >= 'A' && name[i] <= 'Z') {
		i++
	}
	if i == len(name) || len(name)-i > 5 {
		return -1
	}
	n := 0
	for ; i < len(name); i++ {
		if name[i] < '0' || name[i] > '9' {
			return -1
		}
		n = n*10 + int(name[i]-'0')
	}
	return n
}

// --- composite fonts ---

func (font *Font) parseComposite(f *file.File, dict syntax.Value, toUni *cmap) {
	font.Composite = true
	font.toUni = toUni

	// /Encoding names a predefined CMap or is a CMap stream.
	known := false
	switch enc := f.Resolve(dict.Get("Encoding")); enc.Kind {
	case syntax.Name:
		name := enc.NameString()
		var codes unicodeCodes
		font.space, codes, font.Vertical, known = predefined(name)
		if codes != 0 {
			font.unicode = codes
		}
		// Only for the identity CMaps is the CID the code.
		font.noCIDs = name != "Identity-H" && name != "Identity-V"
	case syntax.Stream:
		if data, err := f.Decode(enc, nil, 0); err == nil || len(data) > 0 {
			c := parseCMap(data)
			font.Truncated = font.Truncated || c.truncated
			font.Vertical = c.vertical
			if wmode, ok := f.Resolve(enc.Get("WMode")).Integer(); ok {
				font.Vertical = wmode == 1
			}
			font.space = c.space
			known = len(c.space.ranges) > 0
			parent := c.parent
			if parent == "" {
				parent = f.Resolve(enc.Get("UseCMap")).NameString()
			}
			if !known && parent != "" {
				var codes unicodeCodes
				font.space, codes, _, known = predefined(parent)
				if codes != 0 {
					font.unicode = codes
				}
			}
			switch {
			case len(c.entries) > 0:
				font.cids = c
			case parent != "Identity-H" && parent != "Identity-V":
				font.noCIDs = true
			}
		}
	}
	if !known {
		// The lengths of the codes are not known from the encoding. The
		// ToUnicode CMap states them too; failing that, two bytes is by
		// far the most common.
		if toUni != nil && len(toUni.space.ranges) > 0 {
			font.space = toUni.space
		} else {
			font.space = fixed(2)
		}
		font.noCIDs = font.cids == nil
	}

	// Advances are in the descendant CIDFont.
	font.dw = 1
	descendant := syntax.Value{}
	if list := f.Resolve(dict.Get("DescendantFonts")).Elems(); len(list) > 0 {
		descendant = f.Resolve(list[0])
	}
	if dw, ok := f.Resolve(descendant.Get("DW")).Float(); ok {
		font.dw = advance(dw * 0.001)
	}
	if font.Vertical {
		// Down the page by /DW2's second number, -1000 if absent. The
		// individual vertical advances of /W2 are not read.
		font.dw = -1
		if dw2 := f.Resolve(descendant.Get("DW2")).Elems(); len(dw2) == 2 {
			if v, ok := f.Resolve(dw2[1]).Float(); ok {
				font.dw = advance(v * 0.001)
			}
		}
		font.noCIDs = true
	} else {
		font.parseW(f, f.Resolve(descendant.Get("W")).Elems())
	}

	// The space: the code whose text is a space.
	if toUni != nil {
		if code, n, ok := toUni.codeFor(" "); ok {
			cid := code
			if font.cids != nil {
				cid = font.cids.cid(code, n)
			}
			if w := font.cidWidth(cid); !font.noCIDs && w > 0 {
				font.SpaceWidth = float64(w)
			}
		}
	}
}

// parseW reads /W: "c [w1 w2 ...]" gives the advances of the CIDs from
// c on, "first last w" gives one advance to a range.
func (font *Font) parseW(f *file.File, items []syntax.Value) {
	const maxWidths = 1 << 20
	for i := 0; i < len(items); {
		first, ok := f.Resolve(items[i]).Float()
		if !ok || first < 0 || first > 0xFFFFFF || i+1 >= len(items) {
			i++
			continue
		}
		second := f.Resolve(items[i+1])
		if second.Kind == syntax.Array {
			start := len(font.wArr)
			for _, w := range second.List {
				width, _ := f.Resolve(w).Float()
				font.wArr = append(font.wArr, advance(width*0.001))
			}
			if n := len(font.wArr) - start; n > 0 && len(font.wArr) <= maxWidths {
				font.w = append(font.w, cidWidth{first: uint32(first), last: uint32(first) + uint32(n) - 1, arr: int32(start)})
			} else {
				font.wArr = font.wArr[:start]
				font.Truncated = font.Truncated || n > 0
			}
			i += 2
			continue
		}
		last, ok1 := second.Float()
		if i+2 >= len(items) {
			break
		}
		width, ok2 := f.Resolve(items[i+2]).Float()
		if ok1 && ok2 && last >= first && last <= 0xFFFFFF && len(font.w) < maxWidths {
			font.w = append(font.w, cidWidth{first: uint32(first), last: uint32(last), w: advance(width * 0.001), arr: -1})
		}
		i += 3
	}
	sort.SliceStable(font.w, func(i, j int) bool { return font.w[i].first < font.w[j].first })
}
