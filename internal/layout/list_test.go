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
