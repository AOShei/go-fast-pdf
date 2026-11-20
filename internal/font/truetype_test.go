package font

import (
	"encoding/binary"
	"testing"

	"github.com/AOShei/go-fast-pdf/internal/pdftest"
)

// sfnt makes a TrueType program of the tables given.
func sfnt(tables map[string][]byte) []byte {
	tags := []string{}
	for tag := range tables {
		tags = append(tags, tag)
	}
	out := binary.BigEndian.AppendUint32(nil, 0x00010000)
	out = binary.BigEndian.AppendUint16(out, uint16(len(tags)))
	out = append(out, make([]byte, 6)...)
	off := 12 + 16*len(tags)
	var body []byte
	for _, tag := range tags {
		out = append(out, tag...)
		out = binary.BigEndian.AppendUint32(out, 0)
		out = binary.BigEndian.AppendUint32(out, uint32(off+len(body)))
		out = binary.BigEndian.AppendUint32(out, uint32(len(tables[tag])))
		body = append(body, tables[tag]...)
	}
	return append(out, body...)
}

// format4 makes a cmap of one subtable, platform 3 encoding 1: A to C
// show glyphs 1 to 3, U+65E5 glyph 4 (through the glyph array), and the
// private-use F041 to F043 glyphs 1 to 3 again.
func format4() []byte {
	u16 := func(b []byte, v int) []byte { return binary.BigEndian.AppendUint16(b, uint16(v)) }
	type seg struct{ start, end, delta, ro int }
	segs := []seg{{0x41, 0x43, 1 - 0x41, 0}, {0x65E5, 0x65E5, 0, 6}, {0xF041, 0xF043, 1 - 0xF041, 0}, {0xFFFF, 0xFFFF, 1, 0}}
	var sub []byte
	sub = u16(sub, 4)
	sub = u16(sub, 0) // length, not read
	sub = u16(sub, 0)
	sub = u16(sub, 2*len(segs))
	sub = append(sub, make([]byte, 6)...)
	for _, s := range segs {
		sub = u16(sub, s.end)
	}
	sub = u16(sub, 0)
	for _, s := range segs {
		sub = u16(sub, s.start)
	}
	for _, s := range segs {
		sub = u16(sub, s.delta&0xFFFF)
	}
	for _, s := range segs {
		sub = u16(sub, s.ro)
	}
	sub = u16(sub, 4) // the glyph array: U+65E5
	var table []byte
	table = u16(table, 0)
	table = u16(table, 1)
	table = u16(table, 3)
	table = u16(table, 1)
	table = binary.BigEndian.AppendUint32(table, 12)
	return append(table, sub...)
}

func TestCMapChars(t *testing.T) {
	chars := cmapChars(sfnt(map[string][]byte{"cmap": format4(), "head": make([]byte, 54)}))
	want := map[int]rune{1: 'A', 2: 'B', 3: 'C', 4: 0x65E5}
	for gid, r := range want {
		if gid >= len(chars) || chars[gid] != r {
			t.Errorf("glyph %d: %q", gid, chars)
		}
	}
	for _, data := range [][]byte{nil, {0, 1, 0, 0}, sfnt(map[string][]byte{"head": nil}), sfnt(map[string][]byte{"cmap": {0, 0, 0, 9}})} {
		if chars := cmapChars(data); chars != nil {
			t.Errorf("% x: %q", data, chars)
		}
	}
}

// A CIDFont of type 2 without ToUnicode, whose collection is Identity:
// its text is that of its glyphs, by the program's cmap.
func TestTrueTypeText(t *testing.T) {
	program := pdftest.Stream("", string(sfnt(map[string][]byte{"cmap": format4()})))
	for _, c := range []struct {
		gidMap, s, want string
	}{
		{"/Identity", "\x00\x01\x00\x02\x00\x04\x00\x09", "AB日�"},
		{"3 0 R", "\x00\x0A\x00\x00", "日�"},
	} {
		gids := make([]byte, 22)
		gids[21] = 4 // CID 10 is glyph 4
		extra := []string{
			"<< /Type /FontDescriptor /FontName /Test /FontFile2 2 0 R >>",
			program,
			pdftest.Stream("", string(gids)),
			"<< /Type /Font /Subtype /CIDFontType2 /BaseFont /Test /CIDSystemInfo << /Registry (Adobe) /Ordering (Identity) /Supplement 0 >> /FontDescriptor 1 0 R /CIDToGIDMap " + c.gidMap + " /DW 1000 >>",
		}
		f := load(t, "<< /Type /Font /Subtype /Type0 /BaseFont /Test /Encoding /Identity-H /DescendantFonts [4 0 R] >>", extra...)
		if got, _ := show(f, c.s); got != c.want {
			t.Errorf("%s: %q, want %q", c.gidMap, got, c.want)
		}
	}
}

func FuzzCMapChars(f *testing.F) {
	f.Add(sfnt(map[string][]byte{"cmap": format4()}))
	f.Fuzz(func(t *testing.T, data []byte) {
		if chars := cmapChars(data); len(chars) > 0x10000 {
			t.Fatalf("%d glyphs", len(chars))
		}
	})
}
