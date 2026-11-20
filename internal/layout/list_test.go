package layout

import "testing"

func TestListMarker(t *testing.T) {
	for text, want := range map[string]string{
		"• First item":          "•|First item|false",
		"•\tTabbed item":        "•|Tabbed item|false",
		"\uf0b7 Symbol bullet":  "\uf0b7|Symbol bullet|false",
		"- A dash item":         "-|A dash item|false",
		"1. Numbered":           "1.|Numbered|true",
		"12) Numbered":          "12)|Numbered|true",
		"(3) In brackets":       "(3)|In brackets|true",
		"a. Lettered":           "a.|Lettered|true",
		"b) Lettered":           "b)|Lettered|true",
		"(A) Capital":           "(A)|Capital|true",
		"A) Capital":            "A)|Capital|true",
		"iv. Roman":             "iv.|Roman|true",
		"(iv) Roman":            "(iv)|Roman|true",
		"4.2.1 The Contractor":  "4.2.1|The Contractor|true",
		"4.2. The Contractor":   "4.2.|The Contractor|true",
		"3.5 million people":    "",
		"2001. The year":        "",
		"J. Smith said":         "",
		"- 5 degrees":           "",
		"— which is":            "",
		"* A note":              "",
		"\uf8eb bracket piece":  "",
		"- production\t656.3":   "",
		"6. Options\t. . . 6-1": "",
		"Text without a marker": "",
		"1.":                    "",
		"•":                     "",
		"1.Numbered":            "",
	} {
		marker, rest, ordered, ok := ListMarker(text)
		got := ""
		if ok {
			got = marker + "|" + rest + "|" + map[bool]string{true: "true", false: "false"}[ordered]
		}
		if got != want {
			t.Errorf("%q: %q, want %q", text, got, want)
		}
	}
}

// The marker of a text known to be an item: also a capital letter or a
// capital Roman number before a full stop, and whatever tabs follow.
func TestItemMarker(t *testing.T) {
	for text, want := range map[string]string{
		"1. Numbered":           "1.|Numbered|true",
		"C. BCRA, which":        "C.|BCRA, which|true",
		"IV. Fourth":            "IV.|Fourth|true",
		"III.\tThe Ability":     "III.|The Ability|true",
		"- production\t656.3":   "-|production\t656.3|false",
		"6. Options\t. . . 6-1": "6.|Options\t. . . 6-1|true",
		"VIX. Letters of Roman": "VIX.|Letters of Roman|true",
		"IVXIVX. Too long":      "",
		". A full stop":         "",
		"Text without a marker": "",
		"3.5 million people":    "",
	} {
		marker, rest, ordered, ok := ItemMarker(text)
		got := ""
		if ok {
			got = marker + "|" + rest + "|" + map[bool]string{true: "true", false: "false"}[ordered]
		}
		if got != want {
			t.Errorf("%q: %q, want %q", text, got, want)
		}
	}
	// A list found by its markers is read as before.
	if _, _, _, ok := ListMarker("C. BCRA, which"); ok {
		t.Error("ListMarker reads a capital before a full stop")
	}
}
