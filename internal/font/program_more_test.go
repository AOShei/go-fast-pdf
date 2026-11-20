package font

import (
	"encoding/binary"
	"testing"

	"github.com/AOShei/go-fast-pdf/internal/pdftest"
)

// cffBuild makes a CFF program of four glyphs. Its Top DICT begins with
// prefix, then gives the charset and the encoding: charsetOp and
// encodingOp when they are 0 to 2, else the offsets of the charset and
// encoding given.
func cffBuild(prefix []byte, charsetOp int, charset []byte, encodingOp int, encoding []byte) []byte {
	op := func(v int, code byte) []byte {
		return append(binary.BigEndian.AppendUint32([]byte{29}, uint32(v)), code)
	}
	glyphs := index([]byte{14}, []byte{14}, []byte{14}, []byte{14})
	header := []byte{1, 0, 4, 4}
	nameIndex := index([]byte("Test"))
	strs := index([]byte("mine"))
	gsubrs := index()
	top := func(cs, enc, gl int) []byte {
		return append(append(append(append([]byte{}, prefix...), op(cs, 15)...), op(enc, 16)...), op(gl, 17)...)
	}
	glOff := len(header) + len(nameIndex) + len(index(top(0, 0, 0))) + len(strs) + len(gsubrs)
	csOff := glOff + len(glyphs)
	encOff := csOff + len(charset)
	if charsetOp <= 2 {
		csOff = charsetOp
	}
	if encodingOp <= 2 {
		encOff = encodingOp
	}
	out := append([]byte{}, header...)
	out = append(out, nameIndex...)
	out = append(out, index(top(csOff, encOff, glOff))...)
	out = append(out, strs...)
	out = append(out, gsubrs...)
	out = append(out, glyphs...)
	out = append(out, charset...)
	return append(out, encoding...)
}

func TestCFFCharsets(t *testing.T) {
	enc := []byte{0, 3, 0x30, 0x31, 0x32} // glyphs 1 to 3 at codes 0x30 to 0x32
	for _, tc := range []struct {
		name    string
		charset []byte
		want    [3]string
	}{
		// SIDs 34 to 36 are A to C.
		{"format 1", []byte{1, 0, 34, 2}, [3]string{"A", "B", "C"}},
		{"format 2", []byte{2, 0, 34, 0, 1, 1, 0x87, 0, 0}, [3]string{"A", "B", "mine"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			names, _, ok := cffEncoding(cffBuild(nil, 3, tc.charset, 3, enc))
			if !ok || [3]string(names[0x30:0x33]) != tc.want {
				t.Errorf("%v %q", ok, names[0x30:0x33])
			}
		})
	}
	t.Run("ISOAdobe charset", func(t *testing.T) {
		// Glyph i is SID i: 1 is space, 2 exclam, 3 quotedbl.
		names, _, ok := cffEncoding(cffBuild(nil, 0, nil, 3, enc))
		if !ok || names[0x30] != "space" || names[0x32] != "quotedbl" {
			t.Errorf("%v %q", ok, names[0x30:0x33])
		}
	})
	t.Run("an unknown charset format", func(t *testing.T) {
		if _, _, ok := cffEncoding(cffBuild(nil, 3, []byte{7, 0, 0}, 3, enc)); ok {
			t.Error("read")
		}
	})
	t.Run("an unknown encoding format", func(t *testing.T) {
		if _, _, ok := cffEncoding(cffBuild(nil, 0, nil, 3, []byte{5, 0})); ok {
			t.Error("read")
		}
	})
	t.Run("Standard encoding", func(t *testing.T) {
		if _, standard, ok := cffEncoding(cffBuild(nil, 0, nil, 0, nil)); !ok || !standard {
			t.Errorf("%v %v", ok, standard)
		}
	})
	for _, tc := range []struct {
		name             string
		charsetOp, encOp int
		prefix           []byte
	}{
		{"Expert encoding", 0, 1, nil},
		{"Expert charset", 1, 3, nil},
		{"Expert subset charset", 2, 3, nil},
		// ROS: three operands, then 12 30.
		{"CID-keyed", 0, 3, []byte{139, 139, 139, 12, 30}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, ok := cffEncoding(cffBuild(tc.prefix, tc.charsetOp, nil, tc.encOp, enc)); ok {
				t.Error("read")
			}
		})
	}
	t.Run("operands of every size", func(t *testing.T) {
		// Ignored operators (12 0: Copyright; 0: version) after operands of
		// each encoding: a small integer, two-byte forms, 28, and a real.
		prefix := []byte{32, 247, 0, 251, 0, 28, 1, 0, 30, 0x1A, 0x2F, 12, 0}
		names, _, ok := cffEncoding(cffBuild(prefix, 0, nil, 3, enc))
		if !ok || names[0x30] != "space" {
			t.Errorf("%v %q", ok, names[0x30])
		}
	})
	t.Run("a Top DICT with too many operands", func(t *testing.T) {
		prefix := make([]byte, 60)
		for i := range prefix {
			prefix[i] = 139
		}
		if _, _, ok := cffEncoding(cffBuild(prefix, 0, nil, 3, enc)); ok {
			t.Error("read")
		}
	})
}

// A CFF program inside an OpenType one (FontFile3 of subtype OpenType).
func TestOpenTypeCFF(t *testing.T) {
	cff := cffBuild(nil, 0, nil, 3, []byte{0, 3, 0x41, 0x42, 0x43})
	prog := pdftest.Stream("/Subtype /OpenType", string(sfnt(map[string][]byte{"CFF ": cff})))
	f := load(t, "<< /Type /Font /Subtype /Type1 /BaseFont /Test /FontDescriptor 2 0 R >>",
		prog, "<< /Type /FontDescriptor /FontName /Test /Flags 4 /FontFile3 1 0 R >>")
	// Codes A to C are glyphs 1 to 3 of ISOAdobe: space, exclam, quotedbl.
	if got, _ := show(f, "ABC"); got != " !\"" {
		t.Errorf("%q", got)
	}
}

// postTable makes a 'post' table of format 2 that names glyph 1 by the
// Macintosh order (index 36, "A") and glyph 2 by a name of its own.
func postTable(own string) []byte {
	post := binary.BigEndian.AppendUint32(nil, 0x00020000)
	post = append(post, make([]byte, 28)...)
	post = append(post, 0, 3, 0, 0, 0, 36, 1, 2)
	post = append(post, byte(len(own)))
	return append(post, own...)
}

func TestPost(t *testing.T) {
	t.Run("format 1", func(t *testing.T) {
		post := binary.BigEndian.AppendUint32(nil, 0x00010000)
		p := readPost(append(post, make([]byte, 28)...))
		if p.name(36) != "A" || p.name(258) != "" || p.name(-1) != "" {
			t.Errorf("%q %q", p.name(36), p.name(258))
		}
	})
	t.Run("format 2", func(t *testing.T) {
		p := readPost(postTable("uni05D0"))
		if p.name(0) != ".notdef" || p.name(1) != "A" || p.name(2) != "uni05D0" || p.name(3) != "" {
			t.Errorf("%q %q %q", p.name(0), p.name(1), p.name(2))
		}
	})
	t.Run("damaged", func(t *testing.T) {
		data := postTable("uni05D0")
		for n := range data {
			p := readPost(data[:n])
			for g := range 4 {
				p.name(g)
			}
		}
		bad := append([]byte{}, data...)
		bad[38] = 0xFF // glyph 2: a name number past the table's own names
		if p := readPost(bad); p.name(2) != "" {
			t.Errorf("%q", p.name(2))
		}
		if p := readPost(append([]byte{0, 3, 0, 0}, data[4:]...)); p.n != 0 {
			t.Error("format 3 names glyphs")
		}
	})
}

// lookupGlyph on each format, against reading the whole subtable.
func TestLookupGlyphFormats(t *testing.T) {
	u16 := func(b []byte, v int) []byte { return binary.BigEndian.AppendUint16(b, uint16(v)) }
	u32 := func(b []byte, v int) []byte { return binary.BigEndian.AppendUint32(b, uint32(v)) }
	format0 := append(u16(u16(u16(nil, 0), 262), 0), make([]byte, 256)...)
	format0[6+'A'], format0[6+'Z'] = 1, 2
	format6 := u16(u16(u16(u16(u16(nil, 6), 16), 0), 0x40), 3)
	format6 = u16(u16(u16(format6, 5), 6), 7)
	format12 := u32(u32(u32(u16(u16(nil, 12), 0), 0), 0), 2)
	format12 = u32(u32(u32(format12, 0x41), 0x43), 10)
	format12 = u32(u32(u32(format12, 0x1F600), 0x1F600), 20)
	for name, sub := range map[string][]byte{"0": format0, "6": format6, "12": format12, "unknown": {0, 9, 0, 0}} {
		want := map[int]int{}
		eachMapping(sub, func(c, g int) { want[c] = g })
		for _, c := range []int{0, 0x3F, 0x40, 0x41, 0x42, 0x43, 0x44, 0x5A, 0xFF, 0x100, 0x1F600, 0x1F601} {
			if got := lookupGlyph(sub, c); got != want[c] {
				t.Errorf("format %s, code %#x: glyph %d, want %d", name, c, got, want[c])
			}
		}
		for n := range sub {
			lookupGlyph(sub[:n], 0x41)
		}
	}
}

// A symbolic TrueType font whose codes reach glyphs through the Macintosh
// subtable, the characters named by 'post'.
func TestMacintoshSubtable(t *testing.T) {
	u16 := func(b []byte, v int) []byte { return binary.BigEndian.AppendUint16(b, uint16(v)) }
	sub := u16(u16(u16(u16(u16(nil, 6), 14), 0), 0x61), 2)
	sub = u16(u16(sub, 1), 2) // a: glyph 1, b: glyph 2
	prog := pdftest.Stream("", string(sfnt(map[string][]byte{"cmap": cmapTable(1, 0, sub), "post": postTable("uni05D0")})))
	f := load(t, "<< /Type /Font /Subtype /TrueType /BaseFont /Test /FontDescriptor 2 0 R >>",
		prog, "<< /Type /FontDescriptor /FontName /Test /Flags 4 /FontFile2 1 0 R >>")
	if got, sh := show(f, "ab"); got != "Aא" || sh.Unmapped != 0 {
		t.Errorf("%q %+v", got, sh)
	}
}

// A CIDFont of type 2 whose cmap does not reach a glyph that 'post' names,
// its CIDs mapped to glyphs by a stream.
func TestTrueTypeTextPost(t *testing.T) {
	cmap := cmapTable(3, 1, func() []byte {
		u16 := func(b []byte, v int) []byte { return binary.BigEndian.AppendUint16(b, uint16(v)) }
		return u16(u16(u16(u16(u16(u16(nil, 6), 12), 0), 0x42), 1), 1) // B: glyph 1
	}())
	prog := pdftest.Stream("", string(sfnt(map[string][]byte{"cmap": cmap, "post": postTable("uni05D0")})))
	gidMap := pdftest.Stream("", "\x00\x00\x00\x02\x00\x01") // CID 1: glyph 2, CID 2: glyph 1
	f := load(t, "<< /Type /Font /Subtype /Type0 /BaseFont /Test /Encoding /Identity-H /DescendantFonts [3 0 R] >>",
		prog, gidMap,
		"<< /Type /Font /Subtype /CIDFontType2 /BaseFont /Test /CIDSystemInfo << /Registry (Adobe) /Ordering (Identity) /Supplement 0 >> /CIDToGIDMap 2 0 R /FontDescriptor 4 0 R >>",
		"<< /Type /FontDescriptor /FontName /Test /Flags 4 /FontFile2 1 0 R >>")
	// Glyph 1 is B by the cmap, though post calls it A; glyph 2 is named
	// only by post. CID 3 is past the map.
	if got, sh := show(f, "\x00\x01\x00\x02\x00\x03"); got != "אB\uFFFD" || sh.Unmapped != 1 {
		t.Errorf("%q %+v", got, sh)
	}
}
