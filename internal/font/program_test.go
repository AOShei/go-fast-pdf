package font

import (
	"encoding/binary"
	"testing"

	"github.com/AOShei/go-fast-pdf/internal/pdftest"
)

// index makes a CFF INDEX of the items, with offsets of four bytes.
func index(items ...[]byte) []byte {
	out := binary.BigEndian.AppendUint16(nil, uint16(len(items)))
	if len(items) == 0 {
		return out
	}
	out = append(out, 4)
	off := uint32(1)
	out = binary.BigEndian.AppendUint32(out, off)
	var data []byte
	for _, it := range items {
		off += uint32(len(it))
		out = binary.BigEndian.AppendUint32(out, off)
		data = append(data, it...)
	}
	return append(out, data...)
}

// cffProgram makes a CFF program of five glyphs: A, B and an own glyph
// named "mine", by a charset of format 0, encoded by format 1 (A and B at
// codes 0x30 and 0x31) with a supplement (code 0x40 for B) and "mine"
// at 0x32 by format 0 when ranges is false.
func cffProgram(names []int, encoding []byte) []byte {
	op := func(v int, code byte) []byte {
		return append(binary.BigEndian.AppendUint32([]byte{29}, uint32(v)), code)
	}
	glyphs := index([]byte{14}, []byte{14}, []byte{14}, []byte{14})
	charset := []byte{0}
	for _, sid := range names {
		charset = binary.BigEndian.AppendUint16(charset, uint16(sid))
	}
	header := []byte{1, 0, 4, 4}
	nameIndex := index([]byte("Test"))
	strs := index([]byte("mine"))
	gsubrs := index()
	// The Top DICT's size does not depend on its offsets: five bytes each.
	top := func(cs, enc, gl int) []byte {
		return append(append(op(cs, 15), op(enc, 16)...), op(gl, 17)...)
	}
	start := len(header) + len(nameIndex) + len(index(top(0, 0, 0))) + len(strs) + len(gsubrs)
	glOff := start
	csOff := glOff + len(glyphs)
	encOff := csOff + len(charset)
	out := append([]byte{}, header...)
	out = append(out, nameIndex...)
	out = append(out, index(top(csOff, encOff, glOff))...)
	out = append(out, strs...)
	out = append(out, gsubrs...)
	out = append(out, glyphs...)
	out = append(out, charset...)
	return append(out, encoding...)
}

func TestCFFEncoding(t *testing.T) {
	// SIDs: A is 34, B 35, "mine" the first string of the font's own, 391.
	sids := []int{34, 35, 391}
	t.Run("format 0", func(t *testing.T) {
		names, standard, ok := cffEncoding(cffProgram(sids, []byte{0, 3, 0x30, 0x31, 0x32}))
		if !ok || standard || names[0x30] != "A" || names[0x31] != "B" || names[0x32] != "mine" {
			t.Errorf("%v %v %q", ok, standard, names[0x30:0x33])
		}
	})
	t.Run("format 1 with a supplement", func(t *testing.T) {
		names, _, ok := cffEncoding(cffProgram(sids, []byte{0x81, 1, 0x30, 2, 1, 0x40, 0, 35}))
		if !ok || names[0x30] != "A" || names[0x31] != "B" || names[0x32] != "mine" || names[0x40] != "B" {
			t.Errorf("%v %q %q", ok, names[0x30:0x33], names[0x40])
		}
	})
	t.Run("damaged", func(t *testing.T) {
		data := cffProgram(sids, []byte{0, 3, 0x30, 0x31, 0x32})
		for n := 0; n < len(data); n++ {
			cffEncoding(data[:n]) // no panic
		}
		if _, _, ok := cffEncoding([]byte{1, 0, 4, 4, 0xFF, 0xFF}); ok {
			t.Error("a damaged program read")
		}
	})
}

func TestScrambled(t *testing.T) {
	var names [256]string
	for c := 'A'; c <= 'Z'; c++ {
		names[c] = string(c)
	}
	if scrambled(&names) {
		t.Error("an honest encoding is scrambled")
	}
	for c := 'A'; c <= 'Z'; c++ {
		names[c] = string('A' + (c-'A'+7)%26) // a shift
	}
	if !scrambled(&names) {
		t.Error("a shifted encoding is not scrambled")
	}
	var symbol [256]string
	for c := 'a'; c <= 'z'; c++ {
		symbol[c] = "alpha" // not ASCII: not counted
	}
	if scrambled(&symbol) {
		t.Error("a symbol font is scrambled")
	}
}

// A simple font whose program says what its codes are.
func TestSimpleFontProgram(t *testing.T) {
	t.Run("CFF, no encoding named", func(t *testing.T) {
		prog := pdftest.Stream("/Subtype /Type1C", string(cffProgram([]int{34, 35, 391}, []byte{0, 3, 0x30, 0x31, 0x32})))
		f := load(t, "<< /Type /Font /Subtype /Type1 /BaseFont /Test /FirstChar 0 /LastChar 255 /FontDescriptor 2 0 R >>",
			prog, "<< /Type /FontDescriptor /FontName /Test /Flags 4 /FontFile3 1 0 R >>")
		if got, sh := show(f, "\x30\x31"); got != "AB" || sh.Unmapped != 0 || sh.Assumed != 0 {
			t.Errorf("%q, %+v", got, sh)
		}
	})
	u16 := func(b []byte, v int) []byte { return binary.BigEndian.AppendUint16(b, uint16(v)) }
	// A cmap of the subtables given, each of format 6 (first code, glyphs).
	cmapOf := func(subs map[[2]int][]int, first map[[2]int]int) []byte {
		var table []byte
		table = u16(table, 0)
		table = u16(table, len(subs))
		var bodies []byte
		keys := [][2]int{{1, 0}, {3, 0}, {3, 1}}
		off := 4 + 8*len(subs)
		for _, k := range keys {
			gids, ok := subs[k]
			if !ok {
				continue
			}
			table = u16(table, k[0])
			table = u16(table, k[1])
			table = binary.BigEndian.AppendUint32(table, uint32(off+len(bodies)))
			var sub []byte
			sub = u16(sub, 6)
			sub = u16(sub, 10+2*len(gids))
			sub = u16(sub, 0)
			sub = u16(sub, first[k])
			sub = u16(sub, len(gids))
			for _, g := range gids {
				sub = u16(sub, g)
			}
			bodies = append(bodies, sub...)
		}
		return append(table, bodies...)
	}
	load2 := func(t *testing.T, cmap []byte, post []byte, flags, encoding string) *Font {
		tables := map[string][]byte{"cmap": cmap}
		if post != nil {
			tables["post"] = post
		}
		prog := pdftest.Stream("", string(sfnt(tables)))
		return load(t, "<< /Type /Font /Subtype /TrueType /BaseFont /Test "+encoding+" /FirstChar 0 /LastChar 255 /FontDescriptor 2 0 R >>",
			prog, "<< /Type /FontDescriptor /FontName /Test /Flags "+flags+" /FontFile2 1 0 R >>")
	}
	t.Run("symbolic TrueType", func(t *testing.T) {
		// Codes 0x41 to 0x43 at F041 to F043 reach glyphs 1 to 3; the
		// Unicode subtable gives them α, β, γ.
		cmap := cmapOf(map[[2]int][]int{{3, 0}: {1, 2, 3}, {3, 1}: {1, 2, 3}}, map[[2]int]int{{3, 0}: 0xF041, {3, 1}: 0x3B1})
		if got, sh := show(load2(t, cmap, nil, "4", ""), "ABC"); got != "αβγ" || sh.Assumed != 0 {
			t.Errorf("%q, %+v", got, sh)
		}
	})
	t.Run("symbolic by its flags, reaching its glyphs by Unicode only", func(t *testing.T) {
		// Its codes are WinAnsi's: 0x96 is an en dash.
		cmap := cmapOf(map[[2]int][]int{{3, 1}: {1, 2}}, map[[2]int]int{{3, 1}: 0x41})
		if got, _ := show(load2(t, cmap, nil, "4", ""), "A\x96"); got != "A–" {
			t.Errorf("%q", got)
		}
	})
	t.Run("a difference named by post", func(t *testing.T) {
		// post of format 2: glyph 1 is "uni65E5"-less "mine"; the Unicode
		// subtable gives glyph 1 日.
		post := binary.BigEndian.AppendUint32(nil, 0x00020000)
		post = append(post, make([]byte, 28)...)
		post = u16(post, 2)
		post = u16(post, 0)
		post = u16(post, 258)
		post = append(post, 4)
		post = append(post, "mine"...)
		cmap := cmapOf(map[[2]int][]int{{3, 1}: {1}}, map[[2]int]int{{3, 1}: 0x65E5})
		f := load2(t, cmap, post, "32", "/Encoding << /Differences [65 /mine] >>")
		if got, sh := show(f, "A"); got != "日" || sh.Assumed != 0 {
			t.Errorf("%q, %+v", got, sh)
		}
	})
}

func FuzzCFFEncoding(f *testing.F) {
	f.Add(cffProgram([]int{34, 35, 391}, []byte{0, 3, 0x30, 0x31, 0x32}))
	f.Add(cffProgram([]int{34, 35, 391}, []byte{0x81, 1, 0x30, 2, 1, 0x40, 0, 35}))
	f.Fuzz(func(t *testing.T, data []byte) {
		names, _, _ := cffEncoding(data)
		for _, n := range names {
			if len(n) > len(data) && len(n) > 64 {
				t.Fatalf("a name of %d bytes from %d", len(n), len(data))
			}
		}
	})
}

func FuzzTrueTypeProgram(f *testing.F) {
	post := binary.BigEndian.AppendUint32(nil, 0x00020000)
	post = append(post, make([]byte, 28)...)
	post = append(post, 0, 2, 0, 0, 1, 2, 4)
	post = append(post, "mine"...)
	f.Add(sfnt(map[string][]byte{"cmap": format4(), "post": post}))
	f.Fuzz(func(t *testing.T, data []byte) {
		p := &simpleProgram{}
		p.readTrueType(sfntTables(data))
		p.nameChar("mine")
		for c := range 256 {
			for _, sub := range [][]byte{data, cmapSubtable(data, 3, 1)} {
				lookupGlyph(sub, c)
			}
		}
	})
}

// lookupGlyph finds the glyph of a code as reading the whole subtable does.
func TestLookupGlyph(t *testing.T) {
	sub := cmapSubtable(format4(), 3, 1)
	want := map[int]int{}
	eachMapping(sub, func(c, g int) { want[c] = g })
	if len(want) == 0 {
		t.Fatal("no mappings")
	}
	for c := 0; c < 0x10000; c++ {
		if got := lookupGlyph(sub, c); got != want[c] {
			t.Errorf("U+%04X: glyph %d, want %d", c, got, want[c])
		}
	}
}
