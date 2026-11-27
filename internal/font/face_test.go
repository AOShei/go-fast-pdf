package font

import (
	"testing"

	"github.com/AOShei/go-fast-pdf/internal/xpstest"
)

func TestFace(t *testing.T) {
	f := NewFace(xpstest.Font(&xpstest.FontOptions{Name: "Some Serif Bold", Bold: true, Advances: map[rune]int{'W': 900, 0x4E00: 1000}}), 0)
	if f == nil {
		t.Fatal("no face")
	}
	if f.Name != "Some Serif Bold" || !f.Bold || f.Italic {
		t.Errorf("%q bold %v italic %v", f.Name, f.Bold, f.Italic)
	}
	for r, want := range map[rune]float64{' ': 0.25, 'a': 0.5, 'W': 0.9, 0x05D0: 0.5, 0x4E00: 1} {
		g := f.Glyph(r)
		if g == 0 || f.Advance(g) != want || f.Rune(g) != r {
			t.Errorf("%q: glyph %d advances %v and shows %q", r, g, f.Advance(g), f.Rune(g))
		}
	}
	if g := f.Glyph(0x00E9); g != 0 {
		t.Errorf("a character the font does not have: glyph %d", g)
	}
	if f.Rune(0) != 0 || f.Rune(1<<20) != 0 || f.Advance(-1) != 0.5 || f.Advance(1<<20) <= 0 {
		t.Error("glyphs outside the font")
	}
	// Sideways, a font without vertical metrics advances by its line.
	if h := f.Height(f.Glyph('a')); h != 1 {
		t.Errorf("height %v", h)
	}
	// The same face and style give the same font.
	if f.Font(false, true) != f.Font(false, true) || f.Font(false, false) == f.Font(false, true) {
		t.Error("fonts of a face")
	}
	if ft := f.Font(false, true); !ft.Bold || !ft.Italic || ft.BaseFont != "Some Serif Bold" || ft.SpaceWidth != 0.25 {
		t.Errorf("font: %+v", ft.BaseFont)
	}

	// A symbol font: its characters are at U+F000 and on, and are
	// written with those codes or with their low byte.
	sym := NewFace(xpstest.Font(&xpstest.FontOptions{Symbol: true}), 0)
	if sym == nil || sym.Glyph('a') == 0 || sym.Glyph(0xF061) != sym.Glyph('a') || sym.Rune(sym.Glyph('a')) != 'a' {
		t.Error("symbol font")
	}

	// A collection of two programs.
	ttc := xpstest.Collection(xpstest.Font(&xpstest.FontOptions{Name: "First"}), xpstest.Font(&xpstest.FontOptions{Name: "Second"}))
	for i, want := range []string{"First", "Second"} {
		if f := NewFace(ttc, i); f == nil || f.Name != want {
			t.Errorf("face %d of a collection: %v", i, f)
		}
	}
	if NewFace(ttc, 2) != nil || NewFace(ttc, -1) != nil || NewFace([]byte("not a font"), 0) != nil || NewFace(nil, 0) != nil {
		t.Error("a face that is not there")
	}
}

// FuzzFace reads font programs as the fonts of an XPS document.
func FuzzFace(f *testing.F) {
	f.Add(xpstest.Font(nil), 0)
	f.Add(xpstest.Font(&xpstest.FontOptions{Symbol: true, Bold: true}), 1)
	f.Fuzz(func(t *testing.T, data []byte, index int) {
		face := NewFace(data, index)
		if face == nil {
			return
		}
		for _, r := range []rune{0, ' ', 'a', 0xE9, 0x5D0, 0xF061, 0x1F600, -1} {
			g := face.Glyph(r)
			if g < 0 || g > 0xFFFF {
				t.Fatalf("glyph %d", g)
			}
			if a, h := face.Advance(g), face.Height(g); a != a || h != h || a < 0 || a > 65535 {
				t.Fatalf("advance %v, height %v", a, h)
			}
			face.Rune(g)
		}
		face.Rune(1 << 30)
		face.Advance(1 << 30)
		face.Height(-5)
		if ft := face.Font(true, false); ft == nil || !ft.Bold || !(ft.SpaceWidth > 0) {
			t.Fatal("font")
		}
	})
}
