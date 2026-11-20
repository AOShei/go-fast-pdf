package extract

import (
	"fmt"
	"testing"

	"github.com/AOShei/go-fast-pdf/internal/file"
	"github.com/AOShei/go-fast-pdf/internal/pdftest"
	"github.com/AOShei/go-fast-pdf/internal/syntax"
)

// rulesOf returns the rules of a page of content, as text.
func rulesOf(t *testing.T, content string) []string {
	t.Helper()
	f := open(t, pdftest.OnePage(content), file.Limits{})
	pages := f.Pages()
	var out []string
	Page(f, &pages[0], Options{Rules: true, Use: func(c *Content) {
		for _, r := range c.Rules {
			out = append(out, fmt.Sprintf("%g %g %g %g", r[0], r[1], r[2], r[3]))
		}
	}})
	return out
}

func TestRules(t *testing.T) {
	for _, c := range []struct {
		name, content string
		want          []string
	}{
		{"a stroked level line", "1 w 72 700 m 300 700 l S", []string{"72 699.5 300 700.5"}},
		{"a stroked upright line", "2 w 100 600 m 100 700 l S", []string{"99 600 101 700"}},
		{"a thin filled bar", "72 700 228 0.5 re f", []string{"72 700 300 700.5"}},
		{"a stroked rectangle is its four sides", "0 w 72 600 100 50 re S",
			[]string{"72 600 172 600", "72 650 172 650", "72 600 72 650", "172 600 172 650"}},
		{"lines closed into a thin bar, filled", "72 700 m 300 700 l 300 701 l 72 701 l h f", []string{"72 700 300 701"}},
		{"several lines in one path", "0 w 72 700 m 300 700 l 72 680 m 300 680 l S",
			[]string{"72 700 300 700", "72 680 300 680"}},
		{"closed with s", "0 w 72 700 m 300 700 l 300 600 l s",
			[]string{"72 700 300 700", "300 600 300 700"}},
		{"turned a quarter", "q 0 1 -1 0 400 0 cm 0 w 100 100 m 300 100 l S Q", []string{"300 100 300 300"}},
		{"line width from a graphics state is kept by q and Q", "q 4 w Q 72 700 m 300 700 l S", []string{"72 699.5 300 700.5"}},

		{"a slanted line", "0 w 72 700 m 300 710 l S", nil},
		{"a curve", "0 w 72 700 m 100 720 200 720 300 700 c S", nil},
		{"a filled rectangle that is no bar", "72 600 200 100 re f", nil},
		{"a line that is not painted", "72 700 m 300 700 l n", nil},
		{"a line too short", "0 w 72 700 m 75 700 l S", nil},
		{"a stroke too wide", "5 w 72 700 m 300 700 l S", nil},
		{"turned by a slant", "q 0.7 0.7 -0.7 0.7 0 0 cm 0 w 100 100 m 300 100 l S Q", nil},
		{"a clipping path", "72 700 228 0.5 re W n", nil},
		{"filled lines are no bar", "72 700 m 300 700 l f", nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := rulesOf(t, c.content)
			if fmt.Sprint(got) != fmt.Sprint(c.want) {
				t.Errorf("rules %q, want %q", got, c.want)
			}
		})
	}
}

// Rules are not recorded unless asked for, nor in content that is off.
func TestRulesNotAsked(t *testing.T) {
	f := open(t, pdftest.OnePage("0 w 72 700 m 300 700 l S"), file.Limits{})
	pages := f.Pages()
	Page(f, &pages[0], Options{Use: func(c *Content) {
		if len(c.Rules) != 0 {
			t.Errorf("rules %v", c.Rules)
		}
	}})
}

// rawNumber reads what the parser reads.
func TestRawNumber(t *testing.T) {
	for _, s := range []string{"0", "12", "-12", "+12", "3.25", "-.5", ".5", "5.", "-0", "007", "1.000001",
		"123456789012345678", "0.000000000000000001", ".", "-", "1.2.3", "--1", "1e5", "abc", ""} {
		want, _ := syntax.ParseNumber([]byte(s), 0)
		if got := rawNumber([]byte(s)); got != want.F && !(got != got && want.F != want.F) {
			t.Errorf("%q: %g, the parser %g", s, got, want.F)
		}
	}
}

// A path is measured with the matrix in force where it starts, which is
// the one it is painted with.
func TestRulesScale(t *testing.T) {
	got := rulesOf(t, "0 w 4 0 0 4 0 0 cm 10 100 m 12 100 l S")
	if fmt.Sprint(got) != "[40 400 48 400]" {
		t.Errorf("rules %q", got)
	}
	if got := rulesOf(t, "0 w 72 700 m 100 720 l 300 720 l S"); got != nil {
		t.Errorf("a path that starts slanted: %q", got)
	}
}
