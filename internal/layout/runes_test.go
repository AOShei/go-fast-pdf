package layout

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestRuneCount(t *testing.T) {
	ascii := "The committee met on Tuesday to review the budget"
	for _, s := range []string{"", "a", ascii, "l’été à la bibliothèque municipale", "مكتبة المدينة", "前言前言前言"} {
		for pad := range 20 {
			for _, text := range []string{strings.Repeat("x", pad) + s, s + strings.Repeat("x", pad)} {
				if got, want := runeCount([]byte(text)), utf8.RuneCountInString(text); got != want {
					t.Errorf("%q: %d, want %d", text, got, want)
				}
			}
		}
	}
}
