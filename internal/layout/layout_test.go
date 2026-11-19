package layout

import (
	"testing"

	gofastcore "github.com/AOShei/go-fast-core"
	"github.com/AOShei/go-fast-pdf/internal/extract"
)

// TestFaint: a colour is faint on a background when the shared test
// cannot tell the two apart and they are close in colour as well. Every
// colour is tried on white, on black and on a colour in between.
func TestFaint(t *testing.T) {
	near := func(a, b uint8) bool { return int(a)-int(b) <= colourSlack && int(b)-int(a) <= colourSlack }
	for _, back := range []extract.Color{white, {Known: true}, {R: 230, G: 40, B: 90, Known: true}} {
		for r := 0; r < 256; r++ {
			for g := 0; g < 256; g++ {
				for b := 0; b < 256; b += 5 {
					c := extract.Color{R: uint8(r), G: uint8(g), B: uint8(b), Known: true}
					want := gofastcore.LowContrast(c.R, c.G, c.B, back.R, back.G, back.B) && near(c.R, back.R) && near(c.G, back.G) && near(c.B, back.B)
					if got := faint(c, 1, back); got != want {
						t.Fatalf("colour %d %d %d on %v: faint %v, want %v", r, g, b, back, got, want)
					}
				}
			}
		}
	}
	// Two grays that the shared test cannot tell apart are close in
	// colour: the second condition takes nothing away from them.
	for a := 0; a < 256; a++ {
		for b := 0; b < 256; b++ {
			if gofastcore.LowContrast(uint8(a), uint8(a), uint8(a), uint8(b), uint8(b), uint8(b)) && !near(uint8(a), uint8(b)) {
				t.Fatalf("gray %d on gray %d: without contrast and not close", a, b)
			}
		}
	}
	for _, c := range []struct {
		name       string
		text, back extract.Color
		alpha      float32
		want       bool
	}{
		{"white on white", white, white, 1, true},
		{"black on white", extract.Color{Known: true}, white, 1, false},
		{"green on red, equally bright", extract.Color{G: 138, Known: true}, extract.Color{R: 230, Known: true}, 1, false},
		{"yellow on white", extract.Color{R: 255, G: 255, Known: true}, white, 1, false},
		{"pale yellow on white", extract.Color{R: 255, G: 255, B: 230, Known: true}, white, 1, true},
		{"dark gray on black", extract.Color{R: 18, G: 18, B: 18, Known: true}, extract.Color{Known: true}, 1, true},
		{"black at 3 percent on white", extract.Color{Known: true}, white, 0.03, true},
		{"black at half on white", extract.Color{Known: true}, white, 0.5, false},
		{"white at 3 percent on black", white, extract.Color{Known: true}, 0.03, true},
		{"a colour that was not converted", extract.Color{R: 255, G: 255, B: 255}, white, 1, false},
		{"a background that was not converted", white, extract.Color{R: 255, G: 255, B: 255}, 1, false},
	} {
		if got := faint(c.text, c.alpha, c.back); got != c.want {
			t.Errorf("%s: faint %v", c.name, got)
		}
	}
}

func TestInside(t *testing.T) {
	square := [8]float64{0, 0, 10, 0, 10, 10, 0, 10}
	turned := [8]float64{5, 0, 10, 5, 5, 10, 0, 5} // a square on its corner
	backwards := [8]float64{0, 10, 10, 10, 10, 0, 0, 0}
	for _, c := range []struct {
		q    *[8]float64
		x, y float64
		want bool
	}{
		{&square, 5, 5, true}, {&square, 0, 0, true}, {&square, 10, 10, true}, {&square, 11, 5, false}, {&square, 5, -1, false},
		{&turned, 5, 5, true}, {&turned, 1, 1, false}, {&turned, 9, 9, false}, {&turned, 5, 0.5, true},
		{&backwards, 5, 5, true}, {&backwards, 5, 11, false},
	} {
		if got := inside(c.q, c.x, c.y); got != c.want {
			t.Errorf("inside(%v, %v, %v) = %v", *c.q, c.x, c.y, got)
		}
	}
}

func TestFragKind(t *testing.T) {
	for in, want := range map[string]frag{
		"":     fragLetters,
		"A":    fragLetters,
		"L R":  fragLetters,
		"é":   fragLetters, // a letter and the mark on it
		"é":    fragLetters,
		".":    fragMarks,
		"t .":  fragMarks,
		"7":    fragDigits,
		"A 1":  fragDigits,
		"+":    fragDigits,
		"ab":   fragWord,
		"a bc": fragWord,
		"日":    fragLetters,
		"日本":   fragWord,
		"a\tb": fragLetters,
		"x ×":  fragDigits,
	} {
		if got := fragKind([]byte(in)); got != want {
			t.Errorf("fragKind(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestPlace(t *testing.T) {
	// Two columns of two boxes each, in reading order.
	boxes := []box{{0, 0, 100, 40}, {0, 50, 100, 90}, {120, 0, 220, 40}, {120, 50, 220, 90}}
	for _, c := range []struct {
		x    box
		want int
	}{
		{box{0, 42, 100, 48}, 1},     // between the boxes of the left column
		{box{120, 42, 220, 48}, 3},   // between those of the right column
		{box{0, 100, 100, 110}, 2},   // under the left column: after its last box
		{box{120, -20, 220, -10}, 2}, // over the right column: before its first box
		{box{300, 45, 400, 47}, 3},   // beside everything: after what lies above it
		{box{300, -50, 400, -40}, 0}, // above and beside everything
	} {
		if got := place(boxes, c.x); got != c.want {
			t.Errorf("place(%v) = %d, want %d", c.x, got, c.want)
		}
	}
	if got := place(nil, box{0, 0, 1, 1}); got != 0 {
		t.Errorf("on an empty page: %d", got)
	}
}

func TestRect(t *testing.T) {
	nan := 0.0
	nan = nan / nan
	if got := rect(nan, -2e9, 5, 3e30); got != (gofastcore.Rect{X0: 0, Y0: -1e9, X1: 5, Y1: 1e9}) {
		t.Errorf("%+v", got)
	}
	if got := rect(10, 10, 5, 5); got.X1 < got.X0 || got.Y1 < got.Y0 {
		t.Errorf("a box of negative size: %+v", got)
	}
}

func TestCleanName(t *testing.T) {
	for in, want := range map[string]string{
		"DeviceRGB": "DeviceRGB",
		"a\x00b":    "a�b",
		"\xff\xfe":  "�",
		"café":      "café",
		"x\u0085y":  "x�y",
	} {
		if got := cleanName(in); got != want {
			t.Errorf("cleanName(%q) = %q, want %q", in, got, want)
		}
	}
}
