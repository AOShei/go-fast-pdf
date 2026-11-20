package gofastpdf

import "testing"

// The marker of an item of the structure tree: its label, a marker, or a
// mark by itself.
func TestTreeMarker(t *testing.T) {
	for _, c := range []struct{ text, label, want string }{
		{"• First", "•", "•|First|false"},
		{"1.\tFirst", "1.", "1.|First|true"},
		{"Step 1: Begin here", "Step 1:", "Step 1:|Begin here|false"},
		{"(a) First", "", "(a)|First|true"},
		{"C. Third", "", "C.|Third|true"},
		{"* Starred", "", "*|Starred|false"},
		{"o Word's bullet", "", "o|Word's bullet|false"},
		{"� Unread bullet", "", "�|Unread bullet|false"},
		{"�Unread bullet", "", "�|Unread bullet|false"},
		{"•Up against", "", "•|Up against|false"},
		{"➢ Arrow", "", "➢|Arrow|false"},
		{"Private", "", "|Private|false"},
		// Not markers: the start of the text.
		{"A first word", "", ""},
		{"once upon", "", ""},
		{"“Quoted” text", "", ""},
		{"(see below)", "", ""},
		{"$100 grant", "", ""},
		{"•", "", ""},
		{"Label only", "Label only", ""},
		{"Other text", "•", ""},
	} {
		marker, rest, ordered, ok := treeMarker(c.text, c.label)
		got := ""
		if ok {
			got = marker + "|" + rest + "|" + map[bool]string{true: "true", false: "false"}[ordered]
		}
		if got != c.want {
			t.Errorf("%q with label %q: %q, want %q", c.text, c.label, got, c.want)
		}
	}
}
