package font

import (
	"encoding/binary"
	"testing"

	"github.com/AOShei/go-fast-pdf/internal/pdftest"
)

// glyfProgram makes a TrueType program of glyph data: its glyf, loca
// (long offsets if long), head and maxp tables.
func glyfProgram(glyphs [][]byte, long bool) []byte {
	var glyf, loca []byte
	put := func(off int) {
		if long {
			loca = binary.BigEndian.AppendUint32(loca, uint32(off))
		} else {
			loca = binary.BigEndian.AppendUint16(loca, uint16(off/2))
		}
	}
	for _, g := range glyphs {
		put(len(glyf))
		glyf = append(glyf, g...)
		if len(glyf)%2 == 1 {
			glyf = append(glyf, 0)
		}
	}
	put(len(glyf))
	head := make([]byte, 54)
	if long {
		head[51] = 1
	}
	maxp := binary.BigEndian.AppendUint32(nil, 0x00005000)
	maxp = binary.BigEndian.AppendUint16(maxp, uint16(len(glyphs)))
	return sfnt(map[string][]byte{"glyf": glyf, "loca": loca, "head": head, "maxp": maxp})
}

func u16s(vs ...int) []byte {
	var b []byte
	for _, v := range vs {
		b = binary.BigEndian.AppendUint16(b, uint16(v))
	}
	return b
}

// A triangle: (0,0) on the curve, (100,0) off it, (50,80) on it.
var (
	// Coordinates of two bytes each, and instructions.
	triangleLong = append(append(u16s(1, 0, 0, 100, 80, 2, 2), 0xB0, 0x01), append([]byte{1, 0, 1},
		u16s(0, 100, 0xFFCE /* -50 */, 0, 0, 80)...)...)
	// The same outline: x as short vectors, y as one byte where it can
	// be, no instructions.
	triangleShort = []byte{
		0, 1, 0, 0, 0, 0, 0, 100, 0, 80, // one contour, bounds
		0, 2, // the last point
		0, 0, // no instructions
		1 | 2 | 16 | 32, 0 | 2 | 16 | 32, 1 | 2 | 4 | 32, // on, x+ y=; off, x+ y=; on, x- y+
		0, 100, 50, 80,
	}
	// A different point.
	triangleOther = append(append(u16s(1, 0, 0, 100, 80, 2, 0), []byte{1, 0, 1}...), u16s(0, 100, 0xFFCE, 0, 0, 81)...)
)

// composite is a glyph made of glyph part, moved by dx, dy.
func composite(part, dx, dy int) []byte {
	return append(u16s(0xFFFF, 0, 0, 0, 0, 0x0001|0x0002, part), u16s(dx, dy)...)
}

func TestOutlineHashes(t *testing.T) {
	for _, long := range []bool{false, true} {
		glyphs := [][]byte{nil, triangleLong, triangleShort, triangleOther, composite(1, 10, 20), composite(2, 10, 20), composite(1, 10, 21), triangleLong[:13]}
		h := OutlineHashes(glyfProgram(glyphs, long))
		if len(h) != len(glyphs) {
			t.Fatalf("long %v: %d hashes", long, len(h))
		}
		switch {
		case h[0] != 0 || h[7] != 0:
			t.Errorf("long %v: an empty glyph %x, a cut one %x", long, h[0], h[7])
		case h[1] == 0 || h[1] != h[2]:
			t.Errorf("long %v: one outline written two ways: %x, %x", long, h[1], h[2])
		case h[3] == h[1]:
			t.Errorf("long %v: another point, the same hash", long)
		case h[4] == 0 || h[4] != h[5]:
			t.Errorf("long %v: the same parts by other numbers: %x, %x", long, h[4], h[5])
		case h[6] == h[4] || h[4] == h[1]:
			t.Errorf("long %v: a part moved otherwise, or the part alone: %x, %x, %x", long, h[4], h[6], h[1])
		}
	}
	// A glyph made of itself ends, a program with no outlines has none.
	if h := OutlineHashes(glyfProgram([][]byte{composite(0, 0, 0)}, false)); h[0] != 0 {
		t.Errorf("a glyph made of itself: %x", h[0])
	}
	if h := OutlineHashes(sfnt(map[string][]byte{"head": make([]byte, 54)})); h != nil {
		t.Errorf("no glyf table: %v", h)
	}
	if h := OutlineHashes([]byte("not a font")); h != nil {
		t.Errorf("no font: %v", h)
	}
}

func FuzzOutlineHashes(f *testing.F) {
	f.Add(glyfProgram([][]byte{triangleLong, composite(0, 1, 2)}, false))
	f.Fuzz(func(t *testing.T, data []byte) {
		OutlineHashes(data)
	})
}

func TestKnownGlyphs(t *testing.T) {
	k := known()
	if k.fonts < 2 || len(k.hashes) < 8*1000 || len(k.comps) < 100 {
		t.Fatalf("the table: %d fonts, %d outlines, %d compositions", k.fonts, len(k.hashes)/8, len(k.comps))
	}
	// Each entry is read back, in its font and in no other font.
	for i := 0; i < len(k.hashes)/8; i += 97 {
		h := binary.BigEndian.Uint64(k.hashes[8*i:])
		f := int(k.textOf[3*i])
		if knownText(h, f) == "" {
			t.Errorf("entry %d: no text", i)
		}
		if got := knownFont([]uint64{h, h, 0}); got < 0 {
			t.Errorf("entry %d: no font", i)
		}
	}
	if knownFont(nil) != -1 || knownFont([]uint64{1}) != -1 || knownText(1, 0) != "" {
		t.Error("an unknown outline is known")
	}
	for in, want := range map[string]string{"\u0627\u0654": "\u0623", "\u0648\u0654": "\u0624"} {
		if got, ok := Compose([]byte(in)); !ok || got != want {
			t.Errorf("Compose(%+q) = %+q, %v", in, got, ok)
		}
	}
	if _, ok := Compose([]byte("ab")); ok {
		t.Error("Compose of letters")
	}
	if _, ok := Compose(make([]byte, 300)); ok {
		t.Error("Compose of a long text")
	}
	for r, want := range map[rune][2]bool{MarkFirst - 1: {}, MarkFirst: {true, true}, BodyFirst - 1: {true, true}, BodyFirst: {false, true}, PieceLast: {false, true}, PieceLast + 1: {}} {
		if IsMark(r) != want[0] || IsPiece(r) != want[1] {
			t.Errorf("%U: mark %v, piece %v", r, IsMark(r), IsPiece(r))
		}
	}
}

// A ToUnicode CMap that gives a code U+0000 says nothing of it: in a
// CIDFont of type 2, the program's glyph gives its text. Elsewhere it
// is read as it was.
func TestToUnicodeNUL(t *testing.T) {
	toUni := "1 begincodespacerange <0000> <FFFF> endcodespacerange 2 beginbfchar <0001> <0000> <0002> <0043> endbfchar"
	program := pdftest.Stream("", string(sfnt(map[string][]byte{"cmap": format4()})))
	f := load(t, "<< /Type /Font /Subtype /Type0 /BaseFont /Test /Encoding /Identity-H /DescendantFonts [3 0 R] /ToUnicode 4 0 R >>",
		"<< /Type /FontDescriptor /FontName /Test /FontFile2 2 0 R >>",
		program,
		"<< /Type /Font /Subtype /CIDFontType2 /BaseFont /Test /CIDSystemInfo << /Registry (Adobe) /Ordering (Identity) /Supplement 0 >> /FontDescriptor 1 0 R /CIDToGIDMap /Identity >>",
		pdftest.Stream("", toUni))
	if got, _ := show(f, "\x00\x01\x00\x02"); got != "AC" {
		t.Errorf("with a program: %q", got)
	}
	dict, extra := type0("/Identity-H", "", toUni)
	if got, _ := show(load(t, dict, extra...), "\x00\x01"); got != "\x00" {
		t.Errorf("without a program: %q", got)
	}
}

// A code whose text is that of a known glyph with its outline is
// counted (Shown.ByOutline); one whose text the file gives is not. The
// table's outlines are those of the Noto fonts, which no test program
// has: the glyph is marked as the lookup marks it.
func TestShownByOutline(t *testing.T) {
	toUni := "1 begincodespacerange <0000> <FFFF> endcodespacerange 2 beginbfchar <0001> <0000> <0002> <0043> endbfchar"
	program := pdftest.Stream("", string(sfnt(map[string][]byte{"cmap": format4()})))
	f := load(t, "<< /Type /Font /Subtype /Type0 /BaseFont /Test /Encoding /Identity-H /DescendantFonts [3 0 R] /ToUnicode 4 0 R >>",
		"<< /Type /FontDescriptor /FontName /Test /FontFile2 2 0 R >>",
		program,
		"<< /Type /Font /Subtype /CIDFontType2 /BaseFont /Test /CIDSystemInfo << /Registry (Adobe) /Ordering (Identity) /Supplement 0 >> /FontDescriptor 1 0 R /CIDToGIDMap /Identity >>",
		pdftest.Stream("", toUni))
	if _, sh := show(f, "\x00\x01\x00\x02"); sh.ByOutline != 0 {
		t.Errorf("from the cmap: %d by outline", sh.ByOutline)
	}
	f.tt.byOutline = []uint64{1 << 1}
	if got, sh := show(f, "\x00\x01\x00\x02\x00\x01"); got != "ACA" || sh.ByOutline != 2 {
		t.Errorf("glyph 1 by its outline: %q, %d", got, sh.ByOutline)
	}
}

// A part scaled one way, two ways, or by a matrix, or placed by points:
// each its own hash.
func TestOutlineHashScaled(t *testing.T) {
	scaled := func(flag int, scales ...int) []byte {
		return append(append(u16s(0xFFFF, 0, 0, 0, 0, flag, 1), 5, 6), u16s(scales...)...)
	}
	glyphs := [][]byte{nil, triangleLong, scaled(0x0008|0x0002, 0x2000), scaled(0x0040|0x0002, 0x2000, 0x4000),
		scaled(0x0080|0x0002, 0x4000, 0, 0, 0x4000), scaled(0, 0), scaled(0x0008 | 0x0002)}
	h := OutlineHashes(glyfProgram(glyphs, false))
	seen := map[uint64]bool{}
	for i, v := range h[2:6] {
		if v == 0 || seen[v] {
			t.Errorf("glyph %d: %x", i+2, v)
		}
		seen[v] = true
	}
	if h[6] != 0 {
		t.Errorf("a scale cut off: %x", h[6])
	}
}

func TestProgramGlyphs(t *testing.T) {
	chars, names, gsub := ProgramGlyphs(sfnt(map[string][]byte{"cmap": format4(), "post": postTable("uni05D0"), "GSUB": {1, 2}}))
	if len(chars) < 3 || chars[1] != 'A' || len(names) < 2 || string(gsub) != "\x01\x02" {
		t.Errorf("%q %q %q", chars, names, gsub)
	}
}
