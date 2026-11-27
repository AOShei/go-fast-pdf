package xps

import (
	"math"
	"testing"

	"github.com/AOShei/go-fast-pdf/internal/font"
	"github.com/AOShei/go-fast-pdf/internal/xpstest"
)

func TestParseEntry(t *testing.T) {
	nan := math.NaN()
	cases := []struct {
		in   string
		want []entry
	}{
		{"135", []entry{{1, 1, 135, nan}}},
		{"991,60.241;924,60.241;3", []entry{{1, 1, 991, 60.241}, {1, 1, 924, 60.241}, {1, 1, 3, nan}}},
		{",48.913;,52.174;,50;", []entry{{1, 1, -1, 48.913}, {1, 1, -1, 52.174}, {1, 1, -1, 50}}},
		{"(2:1)191,57.6;;45,,3", []entry{{2, 1, 191, 57.6}, {1, 1, -1, nan}, {1, 1, 45, nan}}},
		{"(3)7,1e1,2,3", []entry{{3, 1, 7, 10}}},
		{"(1:2)5; 6 , 7 ", []entry{{1, 2, 5, nan}, {1, 1, 6, 7}}},
		{";;", []entry{{1, 1, -1, nan}, {1, 1, -1, nan}}},
		{"x;(;:)", []entry{{1, 1, -1, nan}, {1, 1, -1, nan}, {1, 1, -1, nan}}},
	}
	same := func(a, b float64) bool { return a == b || a != a && b != b }
	for _, c := range cases {
		ind := []byte(c.in)
		var got []entry
		for ip := 0; ip < len(ind); {
			before := ip
			got = append(got, parseEntry(ind, &ip))
			if ip <= before {
				t.Fatalf("%q: no progress at %d", c.in, before)
			}
		}
		if len(got) != len(c.want) {
			t.Errorf("%q: %+v", c.in, got)
			continue
		}
		for i := range got {
			if got[i].units != c.want[i].units || got[i].glyphs != c.want[i].glyphs || got[i].glyph != c.want[i].glyph || !same(got[i].advance, c.want[i].advance) {
				t.Errorf("%q: entry %d is %+v, want %+v", c.in, i, got[i], c.want[i])
			}
		}
	}
}

func TestParseNumber(t *testing.T) {
	for in, want := range map[string]float64{"0": 0, "12": 12, "-1.5": -1.5, "+.25": 0.25, "1e2": 100, "2.5E-1": 0.25, " 7 ": 7, "1e400": maxCoord, "-1e400": -maxCoord} {
		if got, ok := parseNumber([]byte(in)); !ok || math.Abs(got-want) > 1e-9*math.Max(1, math.Abs(want)) {
			t.Errorf("%q: %v %v", in, got, ok)
		}
	}
	for _, in := range []string{"", ".", "-", "1e", "1x", "NaN", "Infinity", "1,2"} {
		if _, ok := parseNumber([]byte(in)); ok {
			t.Errorf("%q is read as a number", in)
		}
	}
	if m, ok := parseMatrix([]byte("1,0, 0,1 ,10  20")); !ok || m != (matrix{1, 0, 0, 1, 10, 20}) {
		t.Errorf("matrix: %v %v", m, ok)
	}
	for _, in := range []string{"1,0,0,1,10", "1,0,0,1,10,20,30", "a,b,c,d,e,f", ""} {
		if _, ok := parseMatrix([]byte(in)); ok {
			t.Errorf("%q is read as a matrix", in)
		}
	}
}

func TestDeobfuscate(t *testing.T) {
	plain := xpstest.Font(nil)
	hidden := xpstest.Obfuscate(plain, xpstest.FontGUID)
	if sfnt(hidden) {
		t.Fatal("the obfuscated program begins as a plain one does")
	}
	if got := deobfuscate(hidden, "resources/2b6d30fe-2857-7be6-8635-2a72642c3567.odttf"); string(got) != string(plain) {
		t.Error("the program is not restored")
	}
	for _, name := range []string{"resources/font.odttf", "2b6d30fe-2857-7be6-8635-2a72642c35.odttf", "2b6d30fe-2857-7be6-8635-2a72642c3567aa.odttf"} {
		if got := deobfuscate(hidden, name); string(got) != string(hidden) {
			t.Errorf("%q changed the program", name)
		}
	}
}

// FuzzIndices reads the text and the indices of a Glyphs element into
// clusters: every character of the text is in one, in order, and
// nothing is read for ever.
func FuzzIndices(f *testing.F) {
	f.Add("office x", ";(2:1)71,60;;;;(1:2)70;70;", false)
	f.Add("", "35;36;37,,1,2;900", true)
	f.Add("a\U0001F600b", "(2:1)5;(3:2)6;7", false)
	f.Add("abc", "(0:3)1;2;3;(9999999:9999999)4", false)
	face := font.NewFace(xpstest.Font(nil), 0)
	f.Fuzz(func(t *testing.T, uni, indices string, sideways bool) {
		for _, fc := range []*font.Face{face, nil} {
			var w Worker
			w.g = glyphs{uni: []byte(uni), indices: []byte(indices), size: 12, sideways: sideways}
			w.clusters(fc)
			if len(w.items) > len(uni)+len(indices)+1 {
				t.Fatalf("%d clusters of %d bytes", len(w.items), len(uni)+len(indices))
			}
			text := ""
			for _, it := range w.items {
				if it.lo > it.hi || int(it.hi) > len(w.text) || it.glyphs < 1 || it.adv != it.adv || it.nat != it.nat {
					t.Fatalf("cluster %+v", it)
				}
				text += string(w.text[it.lo:it.hi])
			}
			if uni != "" && text != uni {
				t.Fatalf("the clusters hold %q of %q", text, uni)
			}
		}
	})
}
