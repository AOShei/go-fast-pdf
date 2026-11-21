package extract

import (
	"fmt"
	"math"
	"strings"
	"testing"

	gofastcore "github.com/AOShei/go-fast-core"
	"github.com/AOShei/go-fast-pdf/internal/file"
	"github.com/AOShei/go-fast-pdf/internal/pdftest"
)

// unmapped is U+FFFD as it appears in text.
const unmapped = "\xef\xbf\xbd"

// doc describes a one-page test document.
type doc struct {
	content string
	// resources are entries of the page's resource dictionary besides
	// the font /F1 (Helvetica, WinAnsi, standard metrics).
	resources string
	// objects are added before the document; their numbers start at 1.
	objects []string
	// catalog holds further entries of the document catalog.
	catalog string
	// font replaces the dictionary of /F1; fonts are further entries of
	// the page's font resources.
	font, fonts string
}

// run extracts the page with its runs.
func (d doc) run(t testing.TB) Result {
	t.Helper()
	b := pdftest.New()
	for _, o := range d.objects {
		b.Add(o)
	}
	n := len(d.objects)
	// Doc adds the catalog, the page tree and the font next.
	catalog, font := n+1, n+3
	pdftest.Doc(pdftest.DocOptions{Base: b, Font: d.font}, pdftest.PageSpec{
		Content:   d.content,
		Resources: fmt.Sprintf("/Font << /F1 %d 0 R %s >> %s", font, d.fonts, d.resources),
	})
	if d.catalog != "" {
		b.Set(catalog, fmt.Sprintf("<< /Type /Catalog /Pages %d 0 R %s >>", n+2, d.catalog))
	}
	f, err := file.Open(b.Bytes(), file.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	res := Page(f, &f.Pages()[0], Options{Runs: true})
	if res.Err != nil {
		t.Fatal(res.Err)
	}
	return res
}

// runs extracts a page that needs nothing but its content.
func runs(t testing.TB, content string) Result {
	t.Helper()
	return doc{content: content}.run(t)
}

func (res Result) text(i int) string {
	r := res.Runs[i]
	return string(res.RunText[r.Off : r.Off+r.Len])
}

// texts returns the text of every run, joined by "|".
func (res Result) texts() string {
	parts := make([]string, len(res.Runs))
	for i := range res.Runs {
		parts[i] = res.text(i)
	}
	return strings.Join(parts, "|")
}

func approx(a, b float64) bool { return math.Abs(a-b) < 0.01 }

// Widths of Helvetica, in thousandths of the font size.
const (
	wHello = 722 + 556 + 222 + 222 + 556 // 2278
	wSpace = 278
)

// --- geometry ---

func TestFontSizeAndPosition(t *testing.T) {
	// The same 12-point "Hello" at (72, 720), made in different ways.
	// Its end is 2.278 * 12 = 27.34 further along.
	for name, content := range map[string]string{
		"Tf":       "BT /F1 12 Tf 72 720 Td (Hello) Tj ET",
		"Tm":       "BT /F1 1 Tf 12 0 0 12 72 720 Tm (Hello) Tj ET",
		"cm":       "0.1 0 0 0.1 0 0 cm BT /F1 120 Tf 720 7200 Td (Hello) Tj ET",
		"combined": "2 0 0 2 0 0 cm BT /F1 2 Tf 3 0 0 3 36 360 Tm (Hello) Tj ET",
		"negative": "BT /F1 -12 Tf -1 0 0 -1 72 720 Tm (Hello) Tj ET",
		"TJ":       "BT /F1 12 Tf 72 720 Td [(He) (llo)] TJ ET",
		"flipped":  "1 0 0 -1 0 792 cm BT /F1 12 Tf 1 0 0 -1 72 72 Tm (Hello) Tj ET",
	} {
		res := runs(t, content)
		if len(res.Runs) != 1 {
			t.Errorf("%s: %d runs (%s)", name, len(res.Runs), res.texts())
			continue
		}
		r := res.Runs[0]
		if res.text(0) != "Hello" || !approx(r.Size, 12) || !approx(r.SizeX, 12) ||
			!approx(r.X0, 72) || !approx(r.Y0, 720) || !approx(r.X1, 72+wHello*0.012) || !approx(r.Y1, 720) ||
			!approx(r.Dx, 1) || !approx(r.Dy, 0) || !approx(r.SpaceWidth, wSpace*0.012) || r.Glyphs != 5 {
			t.Errorf("%s: %+v", name, r)
		}
	}
	t.Run("horizontal scaling", func(t *testing.T) {
		// Half as wide: the height stays, the width and the advance
		// halve.
		r := runs(t, "BT /F1 12 Tf 50 Tz 72 720 Td (Hello) Tj ET").Runs[0]
		if !approx(r.Size, 12) || !approx(r.SizeX, 6) || !approx(r.X1, 72+wHello*0.006) {
			t.Errorf("%+v", r)
		}
	})
	t.Run("an uneven matrix", func(t *testing.T) {
		r := runs(t, "BT /F1 10 Tf 2 0 0 3 72 720 Tm (Hello) Tj ET").Runs[0]
		if !approx(r.Size, 30) || !approx(r.SizeX, 20) || !approx(r.X1, 72+wHello*0.020) {
			t.Errorf("%+v", r)
		}
	})
	t.Run("text rise", func(t *testing.T) {
		// The rise lifts the glyphs, not the line the run is on.
		r := runs(t, "BT /F1 12 Tf 72 720 Td 4 Ts (Hello) Tj ET").Runs[0]
		if !approx(r.Rise, 4) || !approx(r.Y0, 720) {
			t.Errorf("%+v", r)
		}
	})
	t.Run("spacing moves the position", func(t *testing.T) {
		// Character spacing after each of 5 glyphs, word spacing after
		// the one space; the run ends with its last glyph.
		res := runs(t, "BT /F1 12 Tf 72 720 Td 1 Tc 5 Tw (He llo) Tj (X) Tj ET")
		want := 72 + (wHello+wSpace)*0.012 + 5*1 + 5
		if res.texts() != "He llo|X" && res.texts() != "He lloX" {
			t.Fatalf("%s", res.texts())
		}
		if r := res.Runs[0]; !approx(r.X0, 72) {
			t.Errorf("%+v", r)
		}
		last := res.Runs[len(res.Runs)-1]
		if got := last.X1 - 667*0.012; !approx(got, want+1) {
			t.Errorf("X starts at %v, want %v", got, want+1)
		}
	})
	t.Run("numbers out of range", func(t *testing.T) {
		// Nothing a content stream states may produce a position that is
		// not a number.
		res := runs(t, "1e300 0 0 1e300 0 0 cm BT /F1 1e300 Tf 1e300 1e300 Td 1e300 Tc 1e300 Tw 1e300 Tz (a b) Tj [(c) 1e300 (d)] TJ ET")
		for _, r := range res.Runs {
			for _, v := range []float64{r.X0, r.Y0, r.X1, r.Y1, r.Size, r.SizeX, r.Dx, r.Dy, r.SpaceWidth} {
				if math.IsNaN(v) {
					t.Fatalf("%+v", r)
				}
			}
		}
	})
}

func TestRotatedText(t *testing.T) {
	// "Hello World" with the space made by a gap, then a second line,
	// at each quarter turn. The second line is one leading further in
	// the direction that is "down" for the text.
	for _, tc := range []struct {
		name   string
		tm     string
		dx, dy float64
	}{
		{"0", "1 0 0 1 300 400", 1, 0},
		{"90", "0 1 -1 0 300 400", 0, 1},
		{"180", "-1 0 0 -1 300 400", -1, 0},
		{"270", "0 -1 1 0 300 400", 0, -1},
		{"30", "0.866 0.5 -0.5 0.866 300 400", 0.866, 0.5},
	} {
		res := runs(t, "BT /F1 12 Tf 14 TL "+tc.tm+" Tm [(Hello) -400 (World)] TJ T* (next) Tj ET")
		if res.Text != "Hello World\nnext" {
			t.Errorf("%s degrees: %q", tc.name, res.Text)
			continue
		}
		r := res.Runs[0]
		if !approx(r.Dx, tc.dx) || !approx(r.Dy, tc.dy) || !approx(r.Size, 12) {
			t.Errorf("%s degrees: %+v", tc.name, r)
		}
	}
	// Text that turns in the middle of a line starts a new one.
	res := runs(t, "BT /F1 12 Tf 72 720 Td (across) Tj 0 1 -1 0 110 720 Tm (up) Tj ET")
	if res.Text != "across\nup" {
		t.Errorf("%q", res.Text)
	}
}

func TestVerticalWriting(t *testing.T) {
	// A font that writes downwards: each glyph one font size below the
	// last. The next column is a new line.
	d := doc{
		objects: []string{
			"<< /Type /Font /Subtype /CIDFontType0 /BaseFont /V /DW 1000 >>",
			pdftest.Stream("", "begincodespacerange <0000> <FFFF> endcodespacerange beginbfrange <0000> <FFFF> <0000> endbfrange"),
			"<< /Type /Font /Subtype /Type0 /BaseFont /V /Encoding /Identity-V /DescendantFonts [1 0 R] /ToUnicode 2 0 R >>",
		},
		fonts:   "/V 3 0 R",
		content: "BT /V 10 Tf 300 700 Td <65E5672C> Tj <8A9E> Tj -20 0 Td <7E26> Tj ET",
	}
	res := d.run(t)
	if res.Text != "日本語\n縦" {
		t.Fatalf("%q", res.Text)
	}
	r := res.Runs[0]
	if !approx(r.Dx, 0) || !approx(r.Dy, -1) || !approx(r.X1, 300) || !approx(r.Y1, 670) || !approx(r.Size, 10) {
		t.Errorf("%+v", r)
	}
}

// --- spaces ---

// gapCase shows "ab", moves by a gap and shows "cd". "ab" is 1.112 font
// sizes wide; the font's space is 0.278.
func gapCase(gap float64) string {
	return fmt.Sprintf("BT /F1 10 Tf 100 700 Td (ab) Tj %.4f 0 Td (cd) Tj ET", 11.12+gap)
}

func TestSpaces(t *testing.T) {
	for _, tc := range []struct{ name, content, want string }{
		// The threshold: half the width of the font's space (1.39 here).
		{"no gap", gapCase(0), "abcd"},
		{"under half a space", gapCase(spaceGapOfSpace*2.78 - 0.05), "abcd"},
		{"over half a space", gapCase(spaceGapOfSpace*2.78 + 0.05), "ab cd"},
		{"a full space", gapCase(2.78), "ab cd"},
		{"a wide gap", gapCase(100), "ab cd"},
		// Stepping back a little is kerning; the strings stay together.
		{"kerned", gapCase(-0.5), "abcd"},
		{"at the edge of the slack", gapCase(-backwardSlack*10 + 0.05), "abcd"},

		{"TJ kerning", "BT /F1 10 Tf 100 700 Td [(Hel) -20 (lo W) 15 (orld)] TJ ET", "Hello World"},
		{"TJ gap", "BT /F1 10 Tf 100 700 Td [(Hello) -300 (World)] TJ ET", "Hello World"},
		{"TJ gap under the threshold", "BT /F1 10 Tf 100 700 Td [(Hel) -130 (lo)] TJ ET", "Hello"},
		{"TJ gap over the threshold", "BT /F1 10 Tf 100 700 Td [(Hello) -150 (World)] TJ ET", "Hello World"},
		{"TJ step back", "BT /F1 10 Tf 100 700 Td [(Hel) 200 (lo)] TJ ET", "Hello"},
		{"no double space", "BT /F1 10 Tf 100 700 Td [(Hello ) -300 (World)] TJ [( and) -300 ( more)] TJ ET", "Hello World and more"},
		{"a space is a space at any size", "BT /F1 1 Tf 30 0 0 30 100 700 Tm [(Hello) -300 (World)] TJ ET", "Hello World"},
		{"scaled by the CTM", "0.01 0 0 0.01 0 0 cm BT /F1 1000 Tf 10000 70000 Td [(Hel) -20 (lo) -300 (World)] TJ ET", "Hello World"},
		{"narrowed by Tz", "BT /F1 10 Tf 50 Tz 100 700 Td [(Hel) -20 (lo) -300 (World)] TJ ET", "Hello World"},

		// Word spacing widens the spaces in a string, nothing else.
		{"word spacing", "BT /F1 10 Tf 100 700 Td 20 Tw (a b) Tj (c) Tj ET", "a bc"},
		// Character spacing: a little is tracking inside a word, as much
		// as a space sets the glyphs apart.
		{"narrow character spacing", "BT /F1 10 Tf 100 700 Td 0.5 Tc (Hello) Tj (World) Tj ET", "HelloWorld"},
		// As groff writes it: the next string is placed where the "d"
		// ends.
		{"character spacing for a space", "BT /F1 10 Tf 100 700 Td 2.5 Tc (Ad) Tj 0 Tc 14.73 0 Td (efault) Tj ET", "A default"},
		{"character spacing taken back", "BT /F1 10 Tf 100 700 Td 3 Tc [(As) 300 (e) 300 (c) 300 (t)] TJ ET", "A sect"},

		// The same place printed twice is two strings.
		// The same string printed again in the same place is bold type or
		// a shadow, made by printing twice: it is the text once.
		{"printed twice", "BT /F1 10 Tf 100 700 Td (Hello) Tj 0 0 Td (Hello) Tj ET", "Hello"},
		{"printed twice, shifted", "BT /F1 10 Tf 100 700 Td (Hello) Tj 0.3 0 Td (Hello) Tj ET", "Hello"},
		{"a shadow", "BT /F1 10 Tf 100 700 Td (Hello) Tj 0.5 -0.5 Td (Hello) Tj ET", "Hello"},
		{"each glyph four times", "BT /F1 10 Tf 1 0 0 1 100 700 Tm (T) Tj 1 0 0 1 100.2 700 Tm (T) Tj 1 0 0 1 100.4 700 Tm (T) Tj 1 0 0 1 100.6 700 Tm (T) Tj " +
			"1 0 0 1 106.11 700 Tm (e) Tj 1 0 0 1 106.31 700 Tm (e) Tj 1 0 0 1 106.51 700 Tm (e) Tj 1 0 0 1 111.67 700 Tm (st) Tj 1 0 0 1 111.87 700 Tm (st) Tj ET", "Test"},
		// Other text in the same place is other text.
		{"another word in the same place", "BT /F1 10 Tf 100 700 Td (Hello) Tj 0 0 Td (World) Tj ET", "Hello World"},
		{"the same word further along", "BT /F1 10 Tf 100 700 Td (Hello) Tj 2 0 Td (Hello) Tj ET", "Hello Hello"},
		{"the same word in another size", "BT /F1 10 Tf 100 700 Td (Hello) Tj /F1 11 Tf 0 0 Td (Hello) Tj ET", "Hello Hello"},
		{"the same letter twice in a word", "BT /F1 10 Tf 100 700 Td (l) Tj (l) Tj ET", "ll"},
		// A string that starts inside the last one, further along:
		// glyphs narrower than the font says, or one set over others.
		{"inside the last string", "BT /F1 10 Tf 100 700 Td (WW) Tj 9 0 Td (x) Tj ET", "WWx"},
		// Right to left, glyph by glyph: each ends where the last began.
		{"right to left", "BT /F1 10 Tf 1 0 0 1 200 700 Tm (c) Tj 1 0 0 1 195 700 Tm (b) Tj 1 0 0 1 189.44 700 Tm (a) Tj ET", "cba"},
		{"right to left with a gap", "BT /F1 10 Tf 1 0 0 1 200 700 Tm (c) Tj 1 0 0 1 180 700 Tm (b) Tj ET", "c b"},
		// Back to the left on the same line: another piece of text.
		{"back to the left", "BT /F1 10 Tf 1 0 0 1 300 700 Tm (right) Tj 1 0 0 1 100 700 Tm (left) Tj ET", "right left"},
	} {
		if got := runs(t, tc.content).Text; got != tc.want {
			t.Errorf("%s: %q, want %q", tc.name, got, tc.want)
		}
	}
	t.Run("the width of the space comes from the font", func(t *testing.T) {
		// A fixed-pitch font has a wide space, and half of it is the
		// threshold, up to the limit of spaceGapMax.
		d := doc{font: "<< /Type /Font /Subtype /Type1 /BaseFont /Courier /Encoding /WinAnsiEncoding >>"}
		d.content = "BT /F1 10 Tf 100 700 Td [(ab) -200 (cd)] TJ ET" // 0.2 of the size: under 0.3
		if got := d.run(t).Text; got != "abcd" {
			t.Errorf("%q", got)
		}
		d.content = "BT /F1 10 Tf 100 700 Td [(ab) -350 (cd)] TJ ET"
		if got := d.run(t).Text; got != "ab cd" {
			t.Errorf("%q", got)
		}
	})
}

// --- lines ---

func TestLines(t *testing.T) {
	for _, tc := range []struct{ name, content, want string }{
		{"Td", "BT /F1 10 Tf 100 700 Td (one) Tj 0 -12 Td (two) Tj ET", "one\ntwo"},
		{"TD sets the leading", "BT /F1 10 Tf 100 700 Td (one) Tj 0 -12 TD (two) Tj T* (three) Tj ET", "one\ntwo\nthree"},
		{"T*", "BT /F1 10 Tf 12 TL 100 700 Td (one) Tj T* (two) Tj ET", "one\ntwo"},
		{"quote", "BT /F1 10 Tf 12 TL 100 700 Td (one) Tj (two) ' ET", "one\ntwo"},
		{"double quote", "BT /F1 10 Tf 12 TL 100 700 Td (one) Tj 0 0 (two) \" ET", "one\ntwo"},
		{"Tm", "BT /F1 10 Tf 1 0 0 1 100 700 Tm (one) Tj 1 0 0 1 100 688 Tm (two) Tj ET", "one\ntwo"},
		{"two text objects", "BT /F1 10 Tf 100 700 Td (one) Tj ET BT /F1 10 Tf 100 688 Td (two) Tj ET", "one\ntwo"},
		{"a text object continues the line", "BT /F1 10 Tf 100 700 Td (one) Tj ET BT /F1 10 Tf 119 700 Td (two) Tj ET", "one two"},
		{"lines set solid", "BT /F1 10 Tf 100 700 Td (one) Tj 0 -10 Td (two) Tj ET", "one\ntwo"},
		{"upwards", "BT /F1 10 Tf 100 700 Td (one) Tj 0 12 Td (two) Tj ET", "one\ntwo"},

		// Raised and lowered text stays on its line.
		{"superscript by rise", "BT /F1 10 Tf 100 700 Td (E=mc) Tj 4 Ts /F1 7 Tf (2) Tj 0 Ts /F1 10 Tf ( is) Tj ET", "E=mc2 is"},
		{"superscript by position", "BT /F1 10 Tf 100 700 Td (note) Tj /F1 6 Tf 18.9 4 Td (1) Tj /F1 10 Tf 3.4 -4 Td (, and) Tj ET", "note1, and"},
		{"subscript by position", "BT /F1 10 Tf 100 700 Td (H) Tj /F1 6 Tf 7.22 -2.5 Td (2) Tj /F1 10 Tf 3.34 2.5 Td (O) Tj ET", "H2O"},

		// A small line under a large one is a line of its own, though
		// it lies within the depth of the large letters.
		{"small under large", "BT /F1 48 Tf 100 700 Td (TITLE) Tj /F1 9 Tf 0 -11 Td (subtitle) Tj ET", "TITLE\nsubtitle"},
		{"small under large, further left", "BT /F1 1 Tf 53 0 0 53 598 138 Tm (WHD) Tj 9.6 0 0 9.6 578 126 Tm [(U.S. W) 111 (age)] TJ ET", "WHD\nU.S. Wage"},
		{"large after small", "BT /F1 9 Tf 100 700 Td (small) Tj /F1 48 Tf 0 -40 Td (LARGE) Tj ET", "small\nLARGE"},
	} {
		if got := runs(t, tc.content).Text; got != tc.want {
			t.Errorf("%s: %q, want %q", tc.name, got, tc.want)
		}
	}
}

// --- graphics state ---

func TestSaveAndRestore(t *testing.T) {
	// Defect B9: everything text depends on is part of the graphics
	// state. Q puts back the font, the size, the spacing, the scaling,
	// the render mode, the colours and the transformation.
	d := doc{
		objects: []string{"<< /Type /Font /Subtype /Type1 /BaseFont /Courier /Encoding /WinAnsiEncoding >>"},
		fonts:   "/F2 1 0 R",
		content: "/F1 12 Tf 1 0 0 rg q /F2 30 Tf 3 Tr 50 Tz 0 1 0 rg 0 0 1 RG 2 0 0 2 0 0 cm 5 Ts " +
			"BT 50 300 Td (inside) Tj ET Q BT 72 720 Td (outside) Tj ET",
	}
	res := d.run(t)
	if res.texts() != "inside|outside" {
		t.Fatalf("%s", res.texts())
	}
	in, out := res.Runs[0], res.Runs[1]
	if in.Font.BaseFont != "Courier" || !approx(in.Size, 60) || !approx(in.SizeX, 30) || in.Mode != 3 ||
		!approx(in.X0, 100) || !approx(in.Rise, 10) {
		t.Errorf("inside: %+v", in)
	}
	if out.Font.BaseFont != "Helvetica" || !approx(out.Size, 12) || !approx(out.SizeX, 12) || out.Mode != 0 ||
		!approx(out.X0, 72) || out.Rise != 0 {
		t.Errorf("outside: %+v", out)
	}

	for _, tc := range []struct{ name, content, want string }{
		// The text state outlives a text object; the position does not.
		{"state across text objects", "BT /F1 12 Tf 14 TL ET BT 72 720 Td (a) Tj T* (b) Tj ET", "a\nb"},
		{"nested", "q q q /F1 9 Tf Q Q Q BT 72 720 Td (x) Tj ET", "x"},
		{"more restores than saves", "/F1 12 Tf Q Q BT 72 720 Td (x) Tj ET", "x"},
	} {
		if got := runs(t, tc.content).Text; got != tc.want {
			t.Errorf("%s: %q, want %q", tc.name, got, tc.want)
		}
	}
	// A stack deeper than the limit is cut off, and nothing breaks.
	deep := strings.Repeat("q ", maxStateDepth+50) + "BT /F1 12 Tf 72 720 Td (deep) Tj ET " + strings.Repeat("Q ", maxStateDepth+50)
	if got := runs(t, deep).Text; got != "deep" {
		t.Errorf("deep stack: %q", got)
	}
}

// --- Form XObjects ---

func form(dict, content string) string {
	return pdftest.Stream("/Type /XObject /Subtype /Form "+dict, content)
}

func TestFormXObjects(t *testing.T) {
	t.Run("text and position", func(t *testing.T) {
		// Defect B5. The inner form is drawn by the outer one; each has
		// a matrix, and the page scales the whole.
		d := doc{
			objects: []string{
				form("/BBox [0 0 100 50] /Matrix [1 0 0 1 10 20] /Resources << /Font << /Fi 6 0 R >> >>", "BT /Fi 10 Tf 5 5 Td (inner) Tj ET"),
				form("/BBox [0 0 200 100] /Matrix [2 0 0 2 100 200] /Resources << /Font << /Fo 6 0 R >> /XObject << /In 1 0 R >> >>",
					"BT /Fo 10 Tf 0 50 Td (outer) Tj ET /In Do"),
			},
			resources: "/XObject << /Out 2 0 R >>",
			content:   "BT /F1 12 Tf 72 720 Td (page) Tj ET q 0.5 0 0 0.5 0 0 cm /Out Do Q BT /F1 12 Tf 72 700 Td (after) Tj ET",
		}
		res := d.run(t)
		if res.Text != "page\nouter\ninner\nafter" {
			t.Fatalf("%q", res.Text)
		}
		// outer: (0, 50) through [2 0 0 2 100 200] and then halved.
		if r := res.Runs[1]; !approx(r.X0, 50) || !approx(r.Y0, 150) || !approx(r.Size, 10) {
			t.Errorf("outer: %+v", r)
		}
		// inner: (5, 5) moved by (10, 20), then as the outer form.
		if r := res.Runs[2]; !approx(r.X0, 65) || !approx(r.Y0, 125) || !approx(r.Size, 10) {
			t.Errorf("inner: %+v", r)
		}
	})
	t.Run("a form without resources uses the page's", func(t *testing.T) {
		d := doc{
			objects:   []string{form("/BBox [0 0 100 100]", "BT /F1 10 Tf 5 5 Td (borrowed) Tj ET")},
			resources: "/XObject << /Fm 1 0 R >>",
			content:   "/Fm Do",
		}
		if got := d.run(t).Text; got != "borrowed" {
			t.Errorf("%q", got)
		}
	})
	t.Run("a form's names are its own", func(t *testing.T) {
		// /F1 is Helvetica on the page and a font that maps A to Z in
		// the form.
		d := doc{
			objects: []string{
				"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding << /BaseEncoding /WinAnsiEncoding /Differences [65 /Z] >> >>",
				form("/BBox [0 0 100 100] /Resources << /Font << /F1 1 0 R >> >>", "BT /F1 10 Tf 5 5 Td (A) Tj ET"),
			},
			resources: "/XObject << /Fm 2 0 R >>",
			content:   "BT /F1 10 Tf 72 720 Td (A) Tj ET /Fm Do BT /F1 10 Tf 72 700 Td (A) Tj ET",
		}
		if got := d.run(t).Text; got != "A\nZ\nA" {
			t.Errorf("%q", got)
		}
	})
	t.Run("a form changes nothing outside it", func(t *testing.T) {
		// The form sets a font, a colour and a matrix, saves without
		// restoring, restores more than it saved and leaves marked
		// content open.
		d := doc{
			objects: []string{
				form("/BBox [0 0 100 100] /Resources << /Font << /Ff 5 0 R >> >>",
					"Q Q Q q q 9 0 0 9 0 0 cm 0 1 0 rg 3 Tr /Ff 40 Tf /Span << /ActualText (never) >> BDC /OC /Missing BDC BT 1 1 Td (in) Tj ET"),
			},
			resources: "/XObject << /Fm 1 0 R >>",
			content:   "q 1 0 0 rg /F1 12 Tf /Fm Do BT 72 720 Td (out) Tj ET Q BT 72 700 Td (end) Tj ET",
		}
		res := d.run(t)
		if res.texts() != "never|out|end" {
			t.Fatalf("%s", res.texts())
		}
		if r := res.Runs[1]; !approx(r.Size, 12) || r.Mode != 0 || r.ActualText || !approx(r.X0, 72) {
			t.Errorf("after the form: %+v", r)
		}
	})
	t.Run("drawn twice", func(t *testing.T) {
		d := doc{
			objects:   []string{form("/BBox [0 0 100 100] /Resources << /Font << /Ff 4 0 R >> >>", "BT /Ff 10 Tf 0 0 Td (stamp) Tj ET")},
			resources: "/XObject << /Fm 1 0 R >>",
			content:   "q 1 0 0 1 72 700 cm /Fm Do Q q 1 0 0 1 72 600 cm /Fm Do Q",
		}
		if got := d.run(t).Text; got != "stamp\nstamp" {
			t.Errorf("%q", got)
		}
	})
	t.Run("a form that is not one", func(t *testing.T) {
		d := doc{
			objects:   []string{"<< /Type /XObject /Subtype /Form /BBox [0 0 1 1] >>", "42", pdftest.Stream("/Type /XObject /Subtype /PS", "(x) show")},
			resources: "/XObject << /A 1 0 R /B 2 0 R /C 3 0 R /D 99 0 R >>",
			content:   "/A Do /B Do /C Do /D Do /E Do BT /F1 12 Tf 72 720 Td (ok) Tj ET",
		}
		if got := d.run(t).Text; got != "ok" {
			t.Errorf("%q", got)
		}
	})
}

// --- /ActualText ---

func TestActualText(t *testing.T) {
	for _, tc := range []struct{ name, content, want string }{
		{"in the stream", "BT /F1 12 Tf 72 720 Td /Span << /ActualText (replacement) >> BDC (glyphs) Tj EMC ET", "replacement"},
		{"in the resources", "BT /F1 12 Tf 72 720 Td /Span /P1 BDC (glyphs) Tj EMC ET", "from resources"},
		{"a ligature inside a word", "BT /F1 12 Tf 72 720 Td (o) Tj /Span << /ActualText (ffi) >> BDC (X) Tj EMC (ce) Tj ET", "office"},
		{"several strings", "BT /F1 12 Tf 72 720 Td /Span << /ActualText (one) >> BDC (a) Tj (b) Tj [(c) -500 (d)] TJ EMC ( two) Tj ET", "one two"},
		{"over two lines", "BT /F1 12 Tf 72 720 Td (ex) Tj /Span << /ActualText (ample) >> BDC (am-) Tj 0 -14 Td (ple) Tj EMC ( text) Tj ET", "example text"},
		{"empty", "BT /F1 12 Tf 72 720 Td (soft) Tj /Span << /ActualText () >> BDC (-) Tj EMC (hyphen) Tj ET", "softhyphen"},
		{"UTF-16", "BT /F1 12 Tf 72 720 Td /Span << /ActualText <FEFF00E9D83DDE00> >> BDC (x) Tj EMC ET", "é😀"},
		{"PDFDocEncoding", `BT /F1 12 Tf 72 720 Td /Span << /ActualText (\351\200) >> BDC (x) Tj EMC ET`, "é•"},
		{"a ligature character", "BT /F1 12 Tf 72 720 Td (o) Tj /Span << /ActualText <FEFFFB03> >> BDC (X) Tj EMC (ce) Tj ET", "office"},
		{"the outer one wins", "BT /F1 12 Tf 72 720 Td /Span << /ActualText (outer) >> BDC (a) Tj /Span << /ActualText (inner) >> BDC (b) Tj EMC (c) Tj EMC ET", "outer"},
		{"nothing drawn, nothing said", "/Span << /ActualText (unseen) >> BDC 0 0 10 10 re f EMC BT /F1 12 Tf 72 720 Td (seen) Tj ET", "seen"},
		{"other properties", "BT /F1 12 Tf 72 720 Td /P << /MCID 0 >> BDC (plain) Tj EMC /Artifact BMC ( text) Tj EMC ET", "plain text"},
		{"never closed", "BT /F1 12 Tf 72 720 Td /Span << /ActualText (open) >> BDC (a) Tj (b) Tj ET", "open"},
		{"closed too often", "EMC EMC BT /F1 12 Tf 72 720 Td (a) Tj EMC (b) Tj ET", "ab"},
		{"not a string", "BT /F1 12 Tf 72 720 Td /Span << /ActualText 12 >> BDC (kept) Tj EMC ET", "kept"},
		{"damaged dictionary", "BT /F1 12 Tf 72 720 Td /Span << /ActualText (a) /X >> BDC (kept) Tj EMC ET", "a"},
	} {
		d := doc{content: tc.content, resources: "/Properties << /P1 << /ActualText (from resources) >> >>"}
		res := d.run(t)
		if res.Text != tc.want {
			t.Errorf("%s: %q, want %q", tc.name, res.Text, tc.want)
		}
	}
	// The run says that its text is not its glyphs, and it covers all
	// the glyphs it replaces.
	res := runs(t, "BT /F1 10 Tf 100 700 Td /Span << /ActualText (fi) >> BDC (ab) Tj (cd) Tj EMC ET")
	if r := res.Runs[0]; !r.ActualText || r.Glyphs != 4 || !approx(r.X0, 100) || !approx(r.X1, 100+21.68) {
		t.Errorf("%+v", r)
	}
	// Glyphs that are replaced are not counted as unmapped.
	d := doc{font: "<< /Type /Font /Subtype /Type1 /Encoding << /Differences [200 /g1] >> >>",
		content: `BT /F1 10 Tf 100 700 Td /Span << /ActualText (known) >> BDC (\310) Tj EMC ET`}
	if res := d.run(t); res.Text != "known" || len(res.Warnings) != 0 {
		t.Errorf("%q, %v", res.Text, res.Warnings)
	}
}

// --- what visibility depends on ---

func TestRenderMode(t *testing.T) {
	for mode := 0; mode <= 7; mode++ {
		res := runs(t, fmt.Sprintf("BT /F1 12 Tf %d Tr 72 720 Td (text) Tj ET", mode))
		if r := res.Runs[0]; int(r.Mode) != mode || res.Text != "text" {
			t.Errorf("mode %d: %+v", mode, r)
		}
	}
	// A mode outside 0 to 7 is not a mode; the one in force stays.
	if r := runs(t, "BT /F1 12 Tf 3 Tr 9 Tr -1 Tr 72 720 Td (text) Tj ET").Runs[0]; r.Mode != 3 {
		t.Errorf("%+v", r)
	}
	// Runs in different modes are different runs, though they touch.
	res := runs(t, "BT /F1 12 Tf 72 720 Td (seen) Tj 3 Tr (unseen) Tj 0 Tr (seen) Tj ET")
	if res.texts() != "seen|unseen|seen" || res.Text != "seenunseenseen" {
		t.Errorf("%s", res.texts())
	}
}

func TestSizeOfSmallText(t *testing.T) {
	// One-point text, made four ways, and text squeezed to a hundredth
	// of its width.
	for name, content := range map[string]string{
		"Tf": "BT /F1 1 Tf 72 720 Td (tiny) Tj ET",
		"Tm": "BT /F1 10 Tf 0.1 0 0 0.1 72 720 Tm (tiny) Tj ET",
		"cm": "0.01 0 0 0.01 0 0 cm BT /F1 100 Tf 7200 72000 Td (tiny) Tj ET",
		"Tz": "BT /F1 1 Tf 100 Tz 72 720 Td (tiny) Tj ET",
	} {
		if r := runs(t, content).Runs[0]; !approx(r.Size, 1) || !approx(r.SizeX, 1) {
			t.Errorf("%s: size %v by %v", name, r.Size, r.SizeX)
		}
	}
	if r := runs(t, "BT /F1 12 Tf 1 Tz 72 720 Td (squeezed) Tj ET").Runs[0]; !approx(r.Size, 12) || !approx(r.SizeX, 0.12) {
		t.Errorf("squeezed: size %v by %v", r.Size, r.SizeX)
	}
	if r := runs(t, "BT /F1 0 Tf 72 720 Td (nothing) Tj ET").Runs[0]; r.Size != 0 || r.SizeX != 0 {
		t.Errorf("size 0: %v by %v", r.Size, r.SizeX)
	}
}

func TestOpacity(t *testing.T) {
	d := doc{resources: "/ExtGState << /Clear << /ca 0 /CA 0 >> /Half << /ca 0.5 >> /Solid << /ca 1 /CA 1 >> " +
		"/Stroke << /CA 0.25 >> /Wild << /ca 7 /CA -2 >> /Other << /LW 2 >> /Bad 12 /Font2 << /Font [3 0 R 30] >> >> "}
	for _, tc := range []struct {
		name, ops    string
		fill, stroke float32
	}{
		{"the initial state", "", 1, 1},
		{"0", "/Clear gs", 0, 0},
		{"0.5", "/Half gs", 0.5, 1},
		{"1", "/Clear gs /Solid gs", 1, 1},
		{"stroke only", "/Stroke gs", 1, 0.25},
		{"out of range", "/Wild gs", 1, 0},
		{"entries that are not opacity", "/Half gs /Other gs", 0.5, 1},
		{"not a dictionary", "/Half gs /Bad gs /Nope gs", 0.5, 1},
		{"restored", "q /Clear gs Q", 1, 1},
	} {
		d.content = tc.ops + " BT /F1 12 Tf 72 720 Td (text) Tj ET"
		if r := d.run(t).Runs[0]; r.FillAlpha != tc.fill || r.StrokeAlpha != tc.stroke {
			t.Errorf("%s: %v and %v, want %v and %v", tc.name, r.FillAlpha, r.StrokeAlpha, tc.fill, tc.stroke)
		}
	}
	// A graphics state dictionary can set the font.
	d.content = "/Font2 gs BT 72 720 Td (text) Tj ET"
	if r := d.run(t).Runs[0]; !approx(r.Size, 30) || r.Font.BaseFont != "Helvetica" {
		t.Errorf("font from gs: %+v", r)
	}
}

func TestOptionalContent(t *testing.T) {
	// Group 1 is off, group 2 is on. Object 3 is a membership dictionary
	// for each policy; object 7 is a form that belongs to the off group.
	base := doc{
		objects: []string{
			"<< /Type /OCG /Name (off) >>",
			"<< /Type /OCG /Name (on) >>",
			"<< /Type /OCMD /OCGs [1 0 R 2 0 R] >>",
			"<< /Type /OCMD /OCGs [1 0 R 2 0 R] /P /AllOn >>",
			"<< /Type /OCMD /OCGs [1 0 R 2 0 R] /P /AnyOff >>",
			"<< /Type /OCMD /OCGs [1 0 R 2 0 R] /P /AllOff >>",
			form("/BBox [0 0 100 100] /OC 1 0 R /Resources << /Font << /Ff 10 0 R >> >>", "BT /Ff 10 Tf 0 0 Td (form) Tj ET"),
		},
		catalog: "/OCProperties << /OCGs [1 0 R 2 0 R] /D << /OFF [1 0 R] >> >>",
		resources: "/Properties << /Off 1 0 R /On 2 0 R /AnyOn 3 0 R /AllOn 4 0 R /AnyOff 5 0 R /AllOff 6 0 R /Single << /Type /OCMD /OCGs 1 0 R >> /None << /Type /OCMD >> >> " +
			"/XObject << /Fm 7 0 R >>",
	}
	show := func(tag string) string {
		return "/OC /" + tag + " BDC (" + tag + ") Tj EMC "
	}
	for _, tc := range []struct {
		name, content string
		off           string // the runs that are switched off
	}{
		{"off and on", show("Off") + show("On"), "Off"},
		{"not marked", "(plain) Tj", ""},
		{"an unknown name", show("Nope"), ""},
		// On inside off is off; off inside on is off.
		{"nested", "/OC /Off BDC /OC /On BDC (in) Tj EMC (mid) Tj EMC (out) Tj", "inmid"},
		{"nested the other way", "/OC /On BDC /OC /Off BDC (in) Tj EMC (mid) Tj EMC", "in"},
		// Another kind of marked content inside does not end it.
		// (a and b are runs of their own: they have different MCIDs.)
		{"other marked content inside", "/OC /Off BDC /Span << /MCID 1 >> BDC (a) Tj EMC (b) Tj EMC (c) Tj", "a|b"},
		// Membership: one group off, one on.
		{"any on", show("AnyOn"), ""},
		{"all on", show("AllOn"), "AllOn"},
		{"any off", show("AnyOff"), ""},
		{"all off", show("AllOff"), "AllOff"},
		{"a single group", show("Single"), "Single"},
		{"no groups", show("None"), ""},
		// A form that belongs to a group.
		{"a form", "(before) Tj ET /Fm Do BT /F1 12 Tf 72 600 Td (after) Tj", "form"},
		// A tag other than OC with the same property list is not
		// optional content.
		{"another tag", "/Span /Off BDC (span) Tj EMC", ""},
	} {
		d := base
		d.content = "BT /F1 12 Tf 72 720 Td " + tc.content + " ET"
		res := d.run(t)
		var off []string
		for i, r := range res.Runs {
			if r.OptionalOff {
				off = append(off, res.text(i))
			}
		}
		if got := strings.Join(off, "|"); got != tc.off {
			t.Errorf("%s: off %q, want %q (runs %s)", tc.name, got, tc.off, res.texts())
		}
		// Content that is switched off is still content.
		if tc.off != "" && !strings.Contains(res.Text, strings.ReplaceAll(tc.off, "|", "")) {
			t.Errorf("%s: %q is missing from the text %q", tc.name, tc.off, res.Text)
		}
	}
	t.Run("everything off but what is listed", func(t *testing.T) {
		d := base
		d.catalog = "/OCProperties << /OCGs [1 0 R 2 0 R] /D << /BaseState /OFF /ON [2 0 R] >> >>"
		d.content = "BT /F1 12 Tf 72 720 Td " + show("Off") + show("On") + " ET"
		res := d.run(t)
		if !res.Runs[0].OptionalOff || res.Runs[1].OptionalOff {
			t.Errorf("%v %v", res.Runs[0].OptionalOff, res.Runs[1].OptionalOff)
		}
	})
	t.Run("no configuration", func(t *testing.T) {
		d := base
		d.catalog = ""
		d.content = "BT /F1 12 Tf 72 720 Td " + show("Off") + " ET"
		if d.run(t).Runs[0].OptionalOff {
			t.Error("off without a configuration that says so")
		}
		d.catalog = "/OCProperties 12"
		if d.run(t).Runs[0].OptionalOff {
			t.Error("off with a configuration that is not one")
		}
	})
}

func TestRunsJoinOnlyInTheSameState(t *testing.T) {
	// Strings that follow each other in one state are one run, however
	// the content splits them.
	res := runs(t, "BT /F1 12 Tf 72 720 Td [(T) 80 (o) -278 (be) -300 (or)] TJ ( not) Tj ET")
	if res.texts() != "To be or not" || res.Runs[0].Glyphs != 10 {
		t.Errorf("%s (%d glyphs)", res.texts(), res.Runs[0].Glyphs)
	}
	// A change of size or of what the file declares about how the text
	// is shown ends a run.
	for name, change := range map[string]string{
		"size":    "/F1 9 Tf",
		"mode":    "3 Tr",
		"opacity": "/Half gs",
		"rise":    "3 Ts",
		"scaling": "80 Tz",
	} {
		d := doc{resources: "/ExtGState << /Half << /ca 0.5 >> >>",
			content: "BT /F1 12 Tf 72 720 Td (one) Tj " + change + " (two) Tj ET"}
		if res := d.run(t); len(res.Runs) != 2 {
			t.Errorf("%s: %s", name, res.texts())
		}
	}
}

// --- warnings and limits ---

func TestTextWarnings(t *testing.T) {
	d := doc{font: "<< /Type /Font /Subtype /Type1 /BaseFont /ABCDEF+Sub /FontDescriptor << /FontFile 1 0 R >> /Encoding << /Differences [65 /g1 /g2 200 /g3] >> >>",
		objects: []string{pdftest.Stream("", "font program")}}
	d.content = `BT /F1 12 Tf 72 720 Td (AB) Tj (\310\310\310) Tj (plain) Tj ET`
	res := d.run(t)
	if res.Text != "AB"+unmapped+unmapped+unmapped+"plain" {
		t.Errorf("%q", res.Text)
	}
	want := map[string]string{
		gofastcore.WarnTextUnmappedGlyphs: "3 character codes",
		"pdf.glyph_text_assumed":          "2 characters",
	}
	if len(res.Warnings) != len(want) {
		t.Fatalf("%v", res.Warnings)
	}
	for _, w := range res.Warnings {
		if !strings.Contains(w.Message, want[w.Code]) || want[w.Code] == "" {
			t.Errorf("%s: %q", w.Code, w.Message)
		}
	}
	if r := res.Runs[0]; r.Unmapped != 3 || r.Assumed != 2 || r.Glyphs != 10 {
		t.Errorf("%+v", r)
	}

	// A font that the resources do not define.
	for _, content := range []string{"BT 72 720 Td (plain) Tj ET", "BT /Nope 12 Tf 72 720 Td (plain) Tj ET"} {
		res := runs(t, content)
		if res.Text != "plain" || codes(res.Warnings) != "pdf.font_missing" {
			t.Errorf("%q: %q, %s", content, res.Text, codes(res.Warnings))
		}
	}
	// Ordinary text has no warnings at all.
	if res := runs(t, "BT /F1 12 Tf 72 720 Td (plain) Tj ET"); len(res.Warnings) != 0 {
		t.Errorf("%v", res.Warnings)
	}
}

func TestTextLimits(t *testing.T) {
	t.Run("runs", func(t *testing.T) {
		defer func(n int) { maxRunsPerPage = n }(maxRunsPerPage)
		maxRunsPerPage = 10
		var sb strings.Builder
		sb.WriteString("BT /F1 12 Tf ")
		for i := 0; i < 50; i++ {
			fmt.Fprintf(&sb, "1 0 0 1 72 %d Tm (line%d) Tj ", 780-15*i, i)
		}
		res := runs(t, sb.String()+"ET")
		if len(res.Runs) != 10 || codes(res.Warnings) != gofastcore.WarnLimitExceeded || !strings.HasSuffix(res.Text, "line9") {
			t.Errorf("%d runs, %s, %q", len(res.Runs), codes(res.Warnings), res.Text)
		}
	})
	t.Run("text", func(t *testing.T) {
		defer func(n int) { maxTextPerPage = n }(maxTextPerPage)
		maxTextPerPage = 1000
		res := runs(t, "BT /F1 12 Tf 72 720 Td "+strings.Repeat("(0123456789) Tj ", 500)+"ET")
		if len(res.Text) < 1000 || len(res.Text) > 1100 || codes(res.Warnings) != gofastcore.WarnLimitExceeded {
			t.Errorf("%d bytes, %s", len(res.Text), codes(res.Warnings))
		}
	})
	t.Run("text that one code expands to", func(t *testing.T) {
		// Every code maps to 120 characters; a megabyte of codes would
		// be 120 megabytes of text.
		defer func(n int) { maxTextPerPage = n }(maxTextPerPage)
		maxTextPerPage = 1 << 20
		long := strings.Repeat("0041", 120)
		d := doc{
			objects: []string{pdftest.Stream("", "beginbfchar <41> <"+long+"> endbfchar")},
			font:    "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /ToUnicode 1 0 R >>",
			content: "BT /F1 12 Tf 72 720 Td " + strings.Repeat("("+strings.Repeat("A", 1000)+") Tj ", 1000) + "ET",
		}
		res := d.run(t)
		if len(res.Text) > 2<<20 || codes(res.Warnings) != gofastcore.WarnLimitExceeded {
			t.Errorf("%d bytes, %s", len(res.Text), codes(res.Warnings))
		}
	})
}

func TestRunsAreOptional(t *testing.T) {
	f := open(t, pdftest.OnePage("BT /F1 12 Tf 72 720 Td (text) Tj ET"), file.Limits{})
	if res := Page(f, &f.Pages()[0], Options{}); res.Runs != nil || res.RunText != nil || res.Text != "text" {
		t.Errorf("%+v", res)
	}
}

// Joins: where a run goes on with a string that stands apart from the
// one before it, recorded when rules are asked for.
func TestJoins(t *testing.T) {
	joins := func(content string, rules bool) string {
		t.Helper()
		f := open(t, pdftest.OnePage(content), file.Limits{})
		var out []string
		Page(f, &f.Pages()[0], Options{Rules: rules, Use: func(c *Content) {
			for _, j := range c.Joins {
				out = append(out, fmt.Sprintf("%d %.2f %.2f %.2f %.2f", j.Off, j.X0, j.Y0, j.X1, j.Y1))
			}
			out = append(out, fmt.Sprintf("%q in %d runs", c.Text, len(c.Runs)))
		}})
		return strings.Join(out, "; ")
	}
	for _, c := range []struct{ name, content, want string }{
		// Helvetica: a and b are 0.556 of the size, the space 0.278.
		{"a space put in", "BT /F1 10 Tf 100 700 Td [(ab) -800 (cd)] TJ ET",
			`3 111.12 700.00 119.12 700.00; "ab cd" in 1 runs`},
		{"after a string that ends with a space", "BT /F1 10 Tf 100 700 Td [(ab ) -500 (cd)] TJ ET",
			`3 113.90 700.00 118.90 700.00; "ab cd" in 1 runs`},
		{"none where a string goes on with no room", "BT /F1 10 Tf 100 700 Td [(ab) -10 (cd)] TJ ET",
			`"abcd" in 1 runs`},
		{"none where a new run begins", "BT /F1 10 Tf 100 700 Td [(ab) -3000 (cd)] TJ ET",
			`"abcd" in 2 runs`},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := joins(c.content, true); got != c.want {
				t.Errorf("got %s, want %s", got, c.want)
			}
		})
	}
	if got := joins("BT /F1 10 Tf 100 700 Td [(ab) -800 (cd)] TJ ET", false); got != `"ab cd" in 1 runs` {
		t.Errorf("without rules: %s", got)
	}
}
