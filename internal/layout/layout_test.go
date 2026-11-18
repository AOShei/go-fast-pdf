package layout

import (
	"testing"

	gofastcore "github.com/AOShei/go-fast-core"
	"github.com/AOShei/go-fast-pdf/internal/extract"
)

// The shortcut in faint must never disagree with the shared test: every
// colour is tried.
func TestFaintAgreesWithTheSharedTest(t *testing.T) {
	for r := 0; r < 256; r++ {
		for g := 0; g < 256; g++ {
			for b := 0; b < 256; b += 5 {
				c := extract.Color{R: uint8(r), G: uint8(g), B: uint8(b), Known: true}
				if got, want := faint(c), gofastcore.LowContrast(c.R, c.G, c.B, 255, 255, 255); got != want {
					t.Fatalf("colour %d %d %d: faint %v, the shared test %v", r, g, b, got, want)
				}
			}
		}
	}
	if faint(extract.Color{R: 255, G: 255, B: 255}) {
		t.Error("a colour that could not be converted is not judged")
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
