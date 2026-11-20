package font

import (
	"encoding/binary"
	"testing"
)

// cmapTable makes a cmap table of one subtable, for the platform and
// encoding given.
func cmapTable(platform, encoding int, sub []byte) []byte {
	u16 := func(b []byte, v int) []byte { return binary.BigEndian.AppendUint16(b, uint16(v)) }
	var table []byte
	table = u16(table, 0)
	table = u16(table, 1)
	table = u16(table, platform)
	table = u16(table, encoding)
	table = binary.BigEndian.AppendUint32(table, 12)
	return append(table, sub...)
}

// The formats of cmap subtables other than 4.
func TestCMapCharsFormats(t *testing.T) {
	u16 := func(b []byte, v int) []byte { return binary.BigEndian.AppendUint16(b, uint16(v)) }
	u32 := func(b []byte, v int) []byte { return binary.BigEndian.AppendUint32(b, uint32(v)) }
	format0 := func() []byte {
		sub := u16(nil, 0)
		sub = u16(sub, 262)
		sub = u16(sub, 0)
		glyphs := make([]byte, 256)
		glyphs['A'], glyphs['B'] = 1, 2
		return append(sub, glyphs...)
	}
	format6 := func() []byte {
		sub := u16(nil, 6)
		sub = u16(sub, 16)
		sub = u16(sub, 0)
		sub = u16(sub, 0x3B1) // α
		sub = u16(sub, 3)
		return u16(u16(u16(sub, 1), 2), 3)
	}
	format12 := func() []byte {
		sub := u16(nil, 12)
		sub = u16(sub, 0)
		sub = u32(sub, 0)
		sub = u32(sub, 0)
		sub = u32(sub, 3)
		for _, g := range [][3]int{{0x1F600, 0x1F601, 1}, {0xE000, 0xE000, 3}, {0x41, 0x41, 3}} {
			sub = u32(u32(u32(sub, g[0]), g[1]), g[2])
		}
		return sub
	}
	for _, tc := range []struct {
		name               string
		platform, encoding int
		sub                []byte
		want               map[int]rune
	}{
		{"format 0, platform 0", 0, 3, format0(), map[int]rune{1: 'A', 2: 'B'}},
		{"format 6", 3, 1, format6(), map[int]rune{1: 'α', 2: 'β', 3: 'γ'}},
		// A glyph reached from private use and from a letter is the letter.
		{"format 12", 3, 10, format12(), map[int]rune{1: 0x1F600, 2: 0x1F601, 3: 'A'}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := sfnt(map[string][]byte{"cmap": cmapTable(tc.platform, tc.encoding, tc.sub)})
			chars := cmapChars(data)
			for g, r := range tc.want {
				if g >= len(chars) || chars[g] != r {
					t.Errorf("glyph %d: %q, want %q", g, chars, r)
				}
			}
			// Cut anywhere: no panic.
			for n := range data {
				cmapChars(data[:n])
			}
		})
	}
	t.Run("a symbol subtable only", func(t *testing.T) {
		if chars := cmapChars(sfnt(map[string][]byte{"cmap": cmapTable(3, 0, format6())})); len(chars) != 0 {
			t.Errorf("%q", chars)
		}
	})
	t.Run("not a font", func(t *testing.T) {
		for _, data := range [][]byte{nil, []byte("not a font at all"), sfnt(map[string][]byte{"head": make([]byte, 54)})} {
			if chars := cmapChars(data); len(chars) != 0 {
				t.Errorf("%q", chars)
			}
		}
	})
}

func TestMayRightToLeft(t *testing.T) {
	for _, tc := range []struct {
		name, toUnicode string
		want            bool
	}{
		{"Hebrew in an entry's text", "1 beginbfchar <0001> <05D0> endbfchar", true},
		{"Hebrew at the end of a range", "1 beginbfrange <0001> <0040> <05C0> endbfrange", true},
		{"Latin", "1 beginbfrange <0001> <0040> <0041> endbfrange", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dict, extra := type0("/Identity-H", "", "1 begincodespacerange <0000> <FFFF> endcodespacerange "+tc.toUnicode)
			f := load(t, dict, extra...)
			for range 2 { // the second time from what was worked out
				if got := f.MayRightToLeft(); got != tc.want {
					t.Errorf("%v", got)
				}
			}
			if f.CodeLen([]byte{0, 1}) != 2 {
				t.Errorf("code length %d", f.CodeLen([]byte{0, 1}))
			}
		})
	}
	t.Run("a simple font of Hebrew glyph names", func(t *testing.T) {
		f := load(t, "<< /Type /Font /Subtype /Type1 /BaseFont /Test /Encoding << /Differences [65 /afii57664] >> >>")
		if !f.MayRightToLeft() || f.CodeLen([]byte("AB")) != 1 {
			t.Error("not right to left")
		}
	})
	t.Run("Helvetica", func(t *testing.T) {
		if f := load(t, "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>"); f.MayRightToLeft() {
			t.Error("right to left")
		}
	})
}
