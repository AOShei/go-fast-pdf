package font

import (
	"testing"
	"unicode/utf8"
)

// HasRightToLeft, which looks at bytes, says what decoding them says, for
// every character.
func TestHasRightToLeft(t *testing.T) {
	for r := rune(0); r <= utf8.MaxRune; r++ {
		if !utf8.ValidRune(r) {
			continue
		}
		b := utf8.AppendRune([]byte("ab"), r)
		if got, want := HasRightToLeft(b), rangeRightToLeft(r, r); got != want {
			t.Fatalf("%U: %v, want %v", r, got, want)
		}
		if HasRightToLeft(string(b)) != HasRightToLeft(b) {
			t.Fatalf("%U: a string and its bytes differ", r)
		}
	}
	// A sequence cut short is no letter.
	for _, b := range [][]byte{{0xEF}, {0xEF, 0xAC}, {0xF0, 0x90}, {0xD6}} {
		if HasRightToLeft(b) {
			t.Errorf("% x", b)
		}
	}
}
