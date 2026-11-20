package layout

import "testing"

// bidiOrder on a line given as it is seen, a unit a character: what it
// reads.
func TestBidiOrder(t *testing.T) {
	for _, c := range []struct {
		seen, read string
		rtl        bool
	}{
		{"םולש", "שלום", true},
		{"1952 תנשב", "בשנת 1952", true},
		{"3.5 לשמל", "למשל 3.5", true},
		{"12,000 םע", "עם 12,000", true},
		{"PDF ץבוקב", "בקובץ PDF", true},
		{"Austen וא Tolstoy ומכ", "כמו Tolstoy או Austen", true},
		{"hello םולש world", "hello שלום world", false},
		{"in Hebrew says תינוריעה היירפסה, and", "in Hebrew says הספרייה העירונית, and", false},
		{"plain text", "plain text", false},
		{"ةنيدملا ةبتكم", "مكتبة المدينة", true},
		{"%75 ةبسنب", "بنسبة 75%", true},
	} {
		var us []unit
		var text []rune
		for _, r := range c.seen {
			text = append(text, r)
			us = append(us, unit{lo: int32(len(text) - 1), class: classOf(r)})
		}
		rtl := bidiOrder(us)
		var got []rune
		for _, u := range us {
			got = append(got, text[u.lo])
		}
		if string(got) != c.read || rtl != c.rtl {
			t.Errorf("%q: %q (right to left %v), want %q (%v)", c.seen, string(got), rtl, c.read, c.rtl)
		}
	}
}

func TestClassOf(t *testing.T) {
	for r, want := range map[rune]bidiClass{
		'a': bidiL, 'Z': bidiL, 'é': bidiL, 'ж': bidiL, '中': bidiL,
		'א': bidiR, 'ש': bidiR, 0x05BE: bidiR,
		'ب': bidiAL, 0xFEFB: bidiAL, 0x061F: bidiAL,
		'7': bidiEN, 0x06F5: bidiEN, 0x0665: bidiAN,
		'-': bidiES, '+': bidiES, '%': bidiET, '$': bidiET,
		',': bidiCS, '.': bidiCS, ':': bidiCS, 0x060C: bidiCS,
		' ': bidiWS, 0x05B4: bidiNSM, 0x064E: bidiNSM,
		'(': bidiON, '!': bidiON, '"': bidiON,
	} {
		if got := classOf(r); got != want {
			t.Errorf("%U %q: %d, want %d", r, r, got, want)
		}
	}
}
