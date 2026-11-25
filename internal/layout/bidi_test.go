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
		rtl, _ := bidiOrder(us, 0, 0)
		var got []rune
		for _, u := range us {
			got = append(got, text[u.lo])
		}
		if string(got) != c.read || rtl != c.rtl {
			t.Errorf("%q: %q (right to left %v), want %q (%v)", c.seen, string(got), rtl, c.read, c.rtl)
		}
	}
}

// bidiOrder on a line that starts with a number, after a letter of the
// paragraph on the line before: an Arabic letter makes the number an
// Arabic one, after which a percent sign stands at its left; with no
// letter before, or a Hebrew one, it is a European number, which the
// sign follows.
func TestBidiOrderBefore(t *testing.T) {
	for _, c := range []struct {
		seen   string
		before bidiClass
		read   string
		first  bool
	}{
		{"نع %75", bidiAL, "75% عن", true},
		{"נע 75%", 0, "75% ענ", true},
		{"נע 75%", bidiR, "75% ענ", true},
		{"نع 75%", 0, "75% عن", true},
		{"%75 ةبسنب", bidiAL, "بنسبة 75%", false},
	} {
		var us []unit
		var text []rune
		for _, r := range c.seen {
			text = append(text, r)
			us = append(us, unit{lo: int32(len(text) - 1), class: classOf(r)})
		}
		_, first := bidiOrder(us, 0, c.before)
		var got []rune
		for _, u := range us {
			got = append(got, text[u.lo])
		}
		if string(got) != c.read || first != c.first {
			t.Errorf("%q after %d: %q (number first %v), want %q (%v)", c.seen, c.before, string(got), first, c.read, c.first)
		}
	}
}

// bidiOrder on a line read in the direction of its paragraph, which is
// not its own: the full stop of a line of Latin words is at its left end
// in a paragraph read from right to left, and the comma of a line of
// Hebrew at its right end in one read from left to right.
func TestBidiOrderIn(t *testing.T) {
	for _, c := range []struct {
		seen string
		dir  bidiClass
		read string
	}{
		{".Austen", 0, ".Austen"},
		{".Austen", bidiR, "Austen."},
		{".Austen וא Tolstoy", bidiR, "Tolstoy או Austen."},
		{",םולש", bidiR, "שלום,"},
		{"םולש,", bidiL, "שלום,"},
	} {
		var us []unit
		var text []rune
		for _, r := range c.seen {
			text = append(text, r)
			us = append(us, unit{lo: int32(len(text) - 1), class: classOf(r)})
		}
		bidiOrder(us, c.dir, 0)
		var got []rune
		for _, u := range us {
			got = append(got, text[u.lo])
		}
		if string(got) != c.read {
			t.Errorf("%q in direction %d: %q, want %q", c.seen, c.dir, string(got), c.read)
		}
	}
}

// mayTurn on lines that do not end at the left: a line that its text
// shows was not set in the other direction, or that reads the same, is
// left as it is.
func TestMayTurn(t *testing.T) {
	for _, c := range []struct {
		text           string
		bidi, rtl, tab bool
		want           bool
	}{
		{"Tolstoy and Austen", false, false, false, false},
		{"(Tolstoy and Austen", false, false, false, true},
		{"Tolstoy 2024", false, false, false, true},
		{"Tolstoy\t2024", false, false, true, false},
		{"Tolstoy או Austen", true, false, false, true},
		{"ספר Tolstoy ספר", true, true, false, true},
		{"שלום עולם.", true, true, false, false},
		{",שלום עולם", true, true, false, true},
		{" ", false, false, false, false},
	} {
		b := &builder{text: []byte(c.text)}
		ln := &line{thi: int32(len(c.text)), bidi: c.bidi, rtl: c.rtl, tab: c.tab}
		if got := b.mayTurn(ln); got != c.want {
			t.Errorf("%q: %v, want %v", c.text, got, c.want)
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
