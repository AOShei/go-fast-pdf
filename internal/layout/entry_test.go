package layout

import "testing"

func TestEndsEntry(t *testing.T) {
	for _, c := range []struct {
		text string
		want bool
	}{
		{"Introduction ........ 3", true},
		{"Introduction....3", true},
		{"Introduction . . . . . . 12", true},
		{"Introduction ........ 3 ", true},
		{"Preface ........ iv", true},
		{"PREFACE ........ IV", true},
		{"Appendix A ........ 1a", true},
		{"The rules ........ A-12", true},
		{"The rules ........ Annex-12", true},
		{"Préface …… iv", true},
		{"Préface ‥‥‥ 4", true},
		{"L’été ···· 7", true},
		{"مقدمة ........ 1", true},
		{"مقدمة ........ ١٢", true},
		{"מבוא ........ 5", true},
		{"前言…………3", true},

		{"", false},
		{"3", false},
		{"iv", false},
		{"....", false},
		{"........ ", false},
		{"Introduction 3", false},
		{"Introduction ... 3", false}, // an ellipsis
		{"Introduction … 3", false},
		{"and so on ..... and then", false},
		{"and so on ..... civil", false}, // a word of the letters of a numeral
		{"et puis ..... il", false},
		{"Notes ........ xlviii", true},
		{"Notes ........ XCIX", true},
		{"Notes ........ iiii", false},
		{"and so on ..... 12 March", false},
		{"and so on ..... 3rd", false},
		{"in 1952. In 1953", false},
		{"see sections 2.3.1", false},
		{"Total ........ 12,400,000", false}, // longer than a label
		{"he said ........", false},
		{"See ........ 3.", false},
		{"وهكذا ........ ثم", false},
	} {
		if got := EndsEntry(c.text); got != c.want {
			t.Errorf("EndsEntry(%q) is %v", c.text, got)
		}
		if got := EndsEntry([]byte(c.text)); got != c.want {
			t.Errorf("EndsEntry of the bytes of %q is %v", c.text, got)
		}
	}
	// Any bytes at all.
	for _, s := range []string{"\xff", "....\xff", "....\xc2\xb7\x80", "\x80\x80\x80....1", "....\xe2\x80\xa61", "\xd9....\xd9\xa1"} {
		EndsEntry(s)
	}
	if n := testing.AllocsPerRun(100, func() {
		EndsEntry("مقدمة ........ ١٢")
		EndsEntry("The committee met on Tuesday to review the budget")
	}); n != 0 {
		t.Errorf("%v allocations", n)
	}
}
