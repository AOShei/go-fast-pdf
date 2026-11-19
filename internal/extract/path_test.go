package extract

import (
	"fmt"
	"runtime/debug"
	"strings"
	"testing"

	"github.com/AOShei/go-fast-pdf/internal/file"
	"github.com/AOShei/go-fast-pdf/internal/pdftest"
)

// shapesOf reads a page with shapes recorded and returns them, with the
// runs and the images, as copies.
func shapesOf(t testing.TB, data []byte, opts Options) (shapes []Shape, upTo int32, runs []Run, images []Image) {
	t.Helper()
	f := open(t, data, file.Limits{})
	opts.Use = func(c *Content) {
		shapes, upTo = append([]Shape{}, c.Shapes...), c.ShapesUpTo
		runs, images = append([]Run{}, c.Runs...), append([]Image{}, c.Images...)
	}
	if res := Page(f, &f.Pages()[0], opts); res.Err != nil {
		t.Fatal(res.Err)
	}
	return shapes, upTo, runs, images
}

func (s Shape) String() string {
	kind := "region"
	if s.Exact {
		kind = fmt.Sprintf("exact rgb(%d,%d,%d)", s.Color.R, s.Color.G, s.Color.B)
		if !s.Color.Known {
			kind = "exact, colour unknown"
		}
		if !s.Opaque {
			kind += ", not opaque"
		}
	}
	return fmt.Sprintf("[%g %g %g %g] seq %d %s", s.Box[0], s.Box[1], s.Box[2], s.Box[3], s.Seq, kind)
}

func (r Region) String() string {
	switch {
	case !r.Bounded && !r.Inexact:
		return "everything"
	case !r.Bounded:
		return "somewhere"
	case r.Inexact:
		return fmt.Sprintf("within [%g %g %g %g]", r.X0, r.Y0, r.X1, r.Y1)
	}
	return fmt.Sprintf("[%g %g %g %g]", r.X0, r.Y0, r.X1, r.Y1)
}

const text = " BT /F1 10 Tf 100 100 Td (x) Tj ET "

// im draws the image of pathDoc.
const im = " /Im Do "

// TestShapes: what is recorded of the paths a page paints.
func TestShapes(t *testing.T) {
	cases := []struct {
		name, content string
		want          []string
	}{
		{"a filled rectangle", "0.5 g 10 20 100 50 re f", []string{"[10 20 110 70] seq 0 exact rgb(128,128,128)"}},
		{"the other fill operators", "1 0 0 rg 10 20 100 50 re f* 0 1 0 rg 0 0 5 5 re F",
			[]string{"[10 20 110 70] seq 0 exact rgb(255,0,0)", "[0 0 5 5] seq 1 exact rgb(0,255,0)"}},
		{"a rectangle of negative size", "110 70 -100 -50 re f", []string{"[10 20 110 70] seq 0 exact rgb(0,0,0)"}},
		{"four lines", "10 20 m 110 20 l 110 70 l 10 70 l f", []string{"[10 20 110 70] seq 0 exact rgb(0,0,0)"}},
		{"four lines, closed", "10 20 m 110 20 l 110 70 l 10 70 l h f", []string{"[10 20 110 70] seq 0 exact rgb(0,0,0)"}},
		{"four lines and back to the start", "10 20 m 10 70 l 110 70 l 110 20 l 10 20 l f", []string{"[10 20 110 70] seq 0 exact rgb(0,0,0)"}},
		{"a triangle", "10 20 m 110 20 l 10 70 l f", []string{"[10 20 110 70] seq 0 region"}},
		{"four corners that are no rectangle", "10 20 m 110 20 l 100 70 l 10 70 l f", []string{"[10 20 110 70] seq 0 region"}},
		{"five corners", "10 20 m 110 20 l 110 70 l 60 90 l 10 70 l f", []string{"[10 20 110 90] seq 0 region"}},
		{"a curve", "10 20 m 10 60 50 100 110 100 c 110 20 l f", []string{"[10 20 110 100] seq 0 region"}},
		{"the short curves", "10 20 m 50 100 110 100 v 200 50 110 20 y f", []string{"[10 20 200 100] seq 0 region"}},
		{"two rectangles in one path", "10 20 100 50 re 200 20 100 50 re f", []string{"[10 20 300 70] seq 0 region"}},
		{"a rectangle and a line", "10 20 100 50 re 300 300 l f", []string{"[10 20 300 300] seq 0 region"}},
		{"two subpaths", "10 20 m 110 20 l 110 70 l 10 70 l 200 200 m 210 210 l f", []string{"[10 20 210 210] seq 0 region"}},
		{"many lines", "0 0 m 1 5 l 2 0 l 3 5 l 4 0 l 5 5 l 6 0 l 7 9 l 8 0 l 9 5 l 30 -2 l f", []string{"[0 -2 30 9] seq 0 region"}},
		{"a path that is not painted", "10 20 100 50 re n 0 0 5 5 re f", []string{"[0 0 5 5] seq 0 exact rgb(0,0,0)"}},
		{"a stroke", "2 w 10 20 m 110 20 l S", []string{"[8 18 112 22] seq 0 region"}},
		{"a stroked rectangle", "10 20 100 50 re S", []string{"[9 19 111 71] seq 0 region"}},
		{"a stroke of no width on a line", "0 w 10 20 m 110 20 l S", nil},
		{"fill and stroke", "4 w 1 g 10 20 100 50 re B",
			[]string{"[6 16 114 74] seq 0 region", "[12 22 108 68] seq 0 exact rgb(255,255,255)"}},
		{"fill and stroke of a triangle", "4 w 10 20 m 110 20 l 10 70 l b", []string{"[6 16 114 74] seq 0 region"}},
		{"a shading", "/Sh sh", []string{"[-1e+12 -1e+12 1e+12 1e+12] seq 0 region"}},
		{"a shading in a clip", "10 20 100 50 re W n /Sh sh", []string{"[10 20 110 70] seq 0 region"}},
		{"scaled and moved", "2 0 0 2 5 5 cm 10 20 100 50 re f", []string{"[25 45 225 145] seq 0 exact rgb(0,0,0)"}},
		{"turned by a quarter", "0 1 -1 0 300 0 cm 10 20 100 50 re f", []string{"[230 10 280 110] seq 0 exact rgb(0,0,0)"}},
		{"turned by less", "0.8 0.6 -0.6 0.8 0 0 cm 0 0 100 50 re f", []string{"[-30 0 80 100] seq 0 region"}},
		{"the width of a stroke is scaled", "3 0 0 3 0 0 cm 1 w 10 20 m 110 20 l S", []string{"[27 57 333 63] seq 0 region"}},
		{"a pattern", "/Pattern cs /P scn 10 20 100 50 re f", []string{"[10 20 110 70] seq 0 region"}},
		{"a colour that is not converted", "/Spot cs 0.5 scn 10 20 100 50 re f", []string{"[10 20 110 70] seq 0 exact, colour unknown"}},
		{"half opaque", "/Half gs 10 20 100 50 re f", []string{"[10 20 110 70] seq 0 exact rgb(0,0,0), not opaque"}},
		{"opacity 0", "/Clear gs 10 20 100 50 re f 0 0 5 5 re f /Opaque gs 0 0 6 6 re f", []string{"[0 0 6 6] seq 2 exact rgb(0,0,0)"}},
		{"opacity 0, fill and stroke", "/Clear gs 10 20 100 50 re B", nil},
		{"a blend mode", "/Mul gs 10 20 100 50 re f /Norm gs 0 0 5 5 re f",
			[]string{"[10 20 110 70] seq 0 exact rgb(0,0,0), not opaque", "[0 0 5 5] seq 1 exact rgb(0,0,0)"}},
		{"a soft mask", "/Mask gs 10 20 100 50 re f /NoMask gs 0 0 5 5 re f",
			[]string{"[10 20 110 70] seq 0 region", "[0 0 5 5] seq 1 exact rgb(0,0,0)"}},
		{"overprinting", "/Over gs 10 20 100 50 re f /NoOver gs 0 0 5 5 re f /OverAll gs 0 0 6 6 re f",
			[]string{"[10 20 110 70] seq 0 exact rgb(0,0,0), not opaque", "[0 0 5 5] seq 1 exact rgb(0,0,0)", "[0 0 6 6] seq 2 exact rgb(0,0,0), not opaque"}},
		{"optional content that is off", "/OC /L BDC 10 20 100 50 re f EMC 0 0 5 5 re f", []string{"[0 0 5 5] seq 1 exact rgb(0,0,0)"}},
		{"cut by a clip", "0 0 50 50 re W n 10 20 100 50 re f", []string{"[10 20 50 50] seq 0 exact rgb(0,0,0)"}},
		{"outside the clip", "0 0 5 5 re W n 10 20 100 50 re f", nil},
		{"under a clip that is no rectangle", "0 0 m 50 0 l 0 50 l W n 10 20 100 50 re f", []string{"[10 20 50 50] seq 0 region"}},
		{"painted with the clip it sets", "0 0 50 50 re W n 10 20 100 50 re W f 0 0 300 300 re f",
			[]string{"[10 20 50 50] seq 0 exact rgb(0,0,0)", "[10 20 50 50] seq 1 exact rgb(0,0,0)"}},
		{"the clip is undone by Q", "q 0 0 5 5 re W n Q 10 20 100 50 re f", []string{"[10 20 110 70] seq 0 exact rgb(0,0,0)"}},
		{"colour and transformation are undone by Q", "q 1 0 0 rg 2 0 0 2 0 0 cm Q 10 20 100 50 re f", []string{"[10 20 110 70] seq 0 exact rgb(0,0,0)"}},
		{"after text and an image", text + "/Im Do 10 20 100 50 re f", []string{"[10 20 110 70] seq 1 exact rgb(0,0,0)"}},
		{"in a form with a matrix", "/Moved Do 0 0 5 5 re f",
			[]string{"[60 70 160 120] seq 0 exact rgb(255,0,0)", "[0 0 5 5] seq 1 exact rgb(0,0,0)"}},
		{"in a form, cut by its box", "/Small Do", []string{"[10 20 30 30] seq 0 exact rgb(0,0,0)"}},
		{"in a form that is turned", "/Turned Do", []string{"[0 0 110 100] seq 0 region"}},
		{"a path left open by a form", "/Open Do 0 0 5 5 re f", []string{"[0 0 5 5] seq 0 exact rgb(0,0,0)"}},
		{"a path left open before a form", "300 300 m /Moved Do 0 0 5 5 re f",
			[]string{"[60 70 160 120] seq 0 exact rgb(255,0,0)", "[0 0 5 5] seq 1 exact rgb(0,0,0)"}},
		{"operands that are not numbers", "/A /B /C /D re f 0 0 5 5 re f", []string{"[0 0 5 5] seq 1 exact rgb(0,0,0)"}},
		{"too few operands", "10 20 re f 5 m 7 l f 0 0 5 5 re f", []string{"[0 0 5 5] seq 2 exact rgb(0,0,0)"}},
		{"numbers without bounds", "-99999999999999999999 -99999999999999999999 99999999999999999999 99999999999999999999 re f",
			[]string{"[-1e+12 -1e+12 0 0] seq 0 exact rgb(0,0,0)"}},
		{"a line that is filled", "10 20 m 110 70 l f 0 0 5 5 re f", []string{"[0 0 5 5] seq 1 exact rgb(0,0,0)"}},
		{"a point that is stroked", "10 20 m S 0 0 5 5 re f", []string{"[9 19 11 21] seq 0 region", "[0 0 5 5] seq 1 exact rgb(0,0,0)"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			shapes, upTo, _, _ := shapesOf(t, pathDoc(c.content), Options{Shapes: true})
			var got []string
			for _, s := range shapes {
				got = append(got, s.String())
			}
			if strings.Join(got, "\n") != strings.Join(c.want, "\n") {
				t.Errorf("shapes:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(c.want, "\n"))
			}
			if upTo != -1 {
				t.Errorf("shapes up to %d", upTo)
			}
			// Without the option nothing is recorded.
			if shapes, _, _, _ := shapesOf(t, pathDoc(c.content), Options{}); len(shapes) != 0 {
				t.Errorf("%d shapes without the option", len(shapes))
			}
		})
	}
}

// pathDoc is a page with what the cases of this file draw from: forms,
// an image, graphics states, colour spaces, optional content.
func pathDoc(content string) []byte {
	b := pdftest.New()
	b.Add(pdftest.Stream("/Type /XObject /Subtype /Form /BBox [0 0 500 500] /Matrix [1 0 0 1 50 50]", "1 0 0 rg 10 20 100 50 re f"))                    // 1
	b.Add(pdftest.Stream("/Type /XObject /Subtype /Form /BBox [0 0 30 30]", "10 20 100 50 re f"+text+im))                                               // 2
	b.Add(pdftest.Stream("/Type /XObject /Subtype /Form /BBox [0 0 100 50] /Matrix [0.8 0.6 -0.6 0.8 30 0]", "0 0 100 50 re f"+text+im))                // 3
	b.Add(pdftest.Stream("/Type /XObject /Subtype /Form /BBox [0 0 500 500]", "300 300 m 310 310 l 0 0 5 5 re W"))                                      // 4
	b.Add(pdftest.Stream("/Type /XObject /Subtype /Image /Width 1 /Height 1 /ColorSpace /DeviceGray /BitsPerComponent 8", "\x80"))                      // 5
	b.Add("<< /Type /OCG /Name (layer) >>")                                                                                                             // 6
	b.Add(pdftest.Stream("/Type /XObject /Subtype /Form /BBox [0 0 500 500] /Group << /S /Transparency >>", "/Opaque gs 10 20 100 50 re f"+text))       // 7
	b.Add(pdftest.Stream("/Type /XObject /Subtype /Form /BBox [0 0 500 500] /Group << /S /Transparency >>", "/Seven Do /Opaque gs 0 0 5 5 re f"))       // 8
	b.Add(pdftest.Stream("/Type /XObject /Subtype /Form /BBox [0 0 500 500] /Resources << /XObject << /Im 5 0 R >> >>", "q 50 0 0 50 0 0 cm /Im Do Q")) // 9
	pdftest.Doc(pdftest.DocOptions{Base: b}, pdftest.PageSpec{
		Content: content,
		Resources: "/Font << /F1 12 0 R >> /XObject << /Moved 1 0 R /Small 2 0 R /Turned 3 0 R /Open 4 0 R /Im 5 0 R /Seven 7 0 R /Eight 8 0 R /Nine 9 0 R >> " +
			"/ExtGState << /Half << /ca 0.5 /CA 0.25 >> /Clear << /ca 0 /CA 0 >> /Opaque << /ca 1 /CA 1 >> /Mul << /BM /Multiply >> /Norm << /BM [/Normal /Multiply] >> " +
			"/Mask << /SMask << /S /Alpha >> >> /NoMask << /SMask /None >> /Over << /op true >> /NoOver << /OP true /op false >> /OverAll << /OP true >> >> " +
			"/ColorSpace << /Spot [/Separation /Gold /DeviceRGB << /FunctionType 2 /Domain [0 1] /N 1 >>] >> /Properties << /L 6 0 R >>",
	})
	// Objects: the nine above, then 10 catalog, 11 page tree, 12 font.
	b.Set(10, "<< /Type /Catalog /Pages 11 0 R /OCProperties << /OCGs [6 0 R] /D << /OFF [6 0 R] >> >> >>")
	return b.Bytes()
}

// TestClipRegions: what the clipping paths leave of the page for the
// text and the images drawn under them, with shapes recorded and
// without.
func TestClipRegions(t *testing.T) {
	cases := []struct {
		name, content string
		// want is the region that the last image is painted in; plain is
		// the region when shapes are not recorded, if it is another.
		want, plain string
	}{
		{"no clip", im, "everything", ""},
		{"a rectangle", "10 20 100 50 re W n" + im, "[10 20 110 70]", ""},
		{"the even-odd rule", "10 20 100 50 re W* n" + im, "[10 20 110 70]", ""},
		{"four lines", "10 20 m 110 20 l 110 70 l 10 70 l h W n" + im, "[10 20 110 70]", ""},
		{"two rectangles, one after the other", "10 20 100 50 re W n 50 0 100 100 re W n" + im, "[50 20 110 70]", ""},
		{"two that do not meet", "10 20 100 50 re W n 300 300 10 10 re W n" + im, "[300 300 300 300]", ""},
		{"no area", "10 20 0 50 re W n" + im, "[10 20 10 20]", ""},
		{"a line", "10 20 m 110 70 l W n" + im, "[10 20 10 20]", ""},
		{"a triangle", "10 20 m 110 20 l 10 70 l W n" + im, "within [10 20 110 70]", ""},
		{"a curve", "10 20 m 10 60 50 100 110 100 c 110 20 l W n" + im, "within [10 20 110 100]", "somewhere"},
		{"a rectangle, then a curve", "0 0 60 60 re W n 10 20 m 10 60 50 100 110 100 c 110 20 l W n" + im, "within [10 20 60 60]", "within [0 0 60 60]"},
		{"a curve, then a rectangle", "10 20 m 10 60 50 100 110 100 c 110 20 l W n 0 0 60 60 re W n" + im, "within [10 20 60 60]", "within [0 0 60 60]"},
		{"two rectangles in one path", "10 20 100 50 re 200 20 100 50 re W n" + im, "within [10 20 300 70]", "somewhere"},
		{"a turned rectangle", "0.8 0.6 -0.6 0.8 0 0 cm 0 0 100 50 re W n" + im, "within [-30 0 80 100]", ""},
		{"a rectangle turned by a quarter", "q 0 1 -1 0 300 0 cm 10 20 100 50 re W n Q" + im, "everything", ""},
		{"turned by a quarter, in force", "0 1 -1 0 300 0 cm 10 20 100 50 re W n" + im, "[230 10 280 110]", ""},
		{"undone by Q", "q 10 20 100 50 re W n Q" + im, "everything", ""},
		{"kept by q", "10 20 100 50 re W n q 1 0 0 1 5 5 cm" + im + "Q", "[10 20 110 70]", ""},
		{"set while it is painted", "10 20 100 50 re W f" + im, "[10 20 110 70]", ""},
		{"W without a path", "W n" + im, "everything", ""},
		{"a path without W", "10 20 100 50 re n" + im, "everything", ""},
		{"W, and the path is never ended", "10 20 100 50 re W" + im, "everything", ""},
		{"set in a form, text after it", "/Open Do n" + im, "everything", ""},
		{"text in a form with a box", "/Small Do", "[0 0 30 30]", ""},
		{"text in a form that is turned", "/Turned Do", "within [0 0 110 100]", ""},
		{"text in a form inside a clip", "10 20 100 50 re W n /Small Do", "[10 20 30 30]", ""},
		{"text that clips", "BT /F1 40 Tf 7 Tr 0 0 Td (X) Tj ET" + im, "somewhere", ""},
		{"text that clips, inside a rectangle", "10 20 100 50 re W n BT /F1 40 Tf 7 Tr 0 0 Td (X) Tj ET" + im, "within [10 20 110 70]", ""},
		{"operands that are not numbers", "/A /B /C /D re W n" + im, "everything", ""},
		{"a point", "10 20 m W n" + im, "[10 20 10 20]", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			for _, record := range []bool{true, false} {
				_, _, _, images := shapesOf(t, pathDoc(c.content), Options{Shapes: record})
				want := c.want
				if !record && c.plain != "" {
					want = c.plain
				}
				if len(images) == 0 {
					t.Fatal("no image")
				}
				if got := images[len(images)-1].Region.String(); got != want {
					t.Errorf("shapes recorded %v: the image is painted on %s, want %s", record, got, want)
				}
			}
		})
	}

}

// TestCutOff: text that lies wholly outside what the clipping paths
// leave is marked, when shapes are recorded, and is a run of its own.
func TestCutOff(t *testing.T) {
	// The text of these cases stands at (100, 100) in 10 points: about 5
	// wide, from 98 to 108 up the page.
	cases := []struct {
		name, content string
		want          string // the runs: "x" in view, "[x]" cut off
	}{
		{"no clip", text, "x"},
		{"a clip that holds it", "90 90 30 30 re W n" + text, "x"},
		{"a clip that it reaches into", "0 0 102 102 re W n" + text, "x"},
		{"a clip beside it", "10 20 50 500 re W n" + text, "[x]"},
		{"a clip under it", "0 0 500 97 re W n" + text, "[x]"},
		{"a clip that its descent reaches into", "0 0 500 99 re W n" + text, "x"},
		{"a clip of no area", "100 100 0 0 re W n" + text, "[x]"},
		{"the box of a round clip elsewhere", "300 300 m 300 400 400 400 400 300 c W n" + text, "[x]"},
		{"the box of a round clip around it", "50 100 m 50 200 200 200 200 100 c 200 0 50 0 50 100 c W n" + text, "x"},
		{"a clip that was undone", "q 10 20 50 50 re W n Q" + text, "x"},
		{"a form with a small box", "/Small Do", "[x]"},
		{"text lifted into the clip", "0 0 500 97 re W n BT /F1 10 Tf -20 Ts 100 100 Td (x) Tj ET", "x"},
		{"text lifted out of the clip", "90 90 30 30 re W n BT /F1 10 Tf 50 Ts 100 100 Td (x) Tj ET", "[x]"},
		{"one line, the second word under a clip that cuts it off",
			"BT /F1 10 Tf 100 100 Td (a) Tj ET q 0 0 10 10 re W n BT /F1 10 Tf 105.56 100 Td (b) Tj ET Q", "a [b]"},
		{"one line, the second word under a clip that holds it",
			"BT /F1 10 Tf 100 100 Td (a) Tj ET q 90 90 30 30 re W n BT /F1 10 Tf 105.56 100 Td (b) Tj ET Q", "ab"},
		{"one line, both words cut off by different clips",
			"q 0 0 10 10 re W n BT /F1 10 Tf 100 100 Td (a) Tj ET Q q 0 0 20 20 re W n BT /F1 10 Tf 105.56 100 Td (b) Tj ET Q", "[ab]"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := open(t, pathDoc(c.content), file.Limits{})
			describe := func(opts Options) string {
				opts.Runs = true
				res := Page(f, &f.Pages()[0], opts)
				var out []string
				for _, r := range res.Runs {
					s := string(res.RunText[r.Off : r.Off+r.Len])
					if r.CutOff {
						s = "[" + s + "]"
					}
					out = append(out, s)
				}
				return strings.Join(out, " ")
			}
			if got := describe(Options{Shapes: true}); got != c.want {
				t.Errorf("runs %s, want %s", got, c.want)
			}
			// Without shapes nothing is marked, and the runs are those of
			// a page without clipping paths.
			plain := strings.NewReplacer("[", "", "]", "").Replace(c.want)
			if strings.Contains(c.name, "one line") {
				plain = "ab"
			}
			if got := describe(Options{}); got != plain {
				t.Errorf("without shapes: runs %s, want %s", got, plain)
			}
		})
	}
}

// TestTransparencyGroups: a group is painted with the opacity in force
// where it is drawn, whatever its content sets.
func TestTransparencyGroups(t *testing.T) {
	cases := []struct {
		name, content string
		fill          float32
		blend         bool
		shape         string
	}{
		{"opaque", "/Seven Do", 1, false, "exact rgb(0,0,0)"},
		{"half", "/Half gs /Seven Do", 0.5, false, "exact rgb(0,0,0), not opaque"},
		{"clear", "/Clear gs /Seven Do", 0, false, ""},
		{"a blend mode", "/Mul gs /Seven Do", 1, true, "region"},
		{"a soft mask", "/Mask gs /Seven Do", 1, true, "region"},
		{"a group in a group", "/Half gs /Eight Do", 0.5, false, "exact rgb(0,0,0), not opaque"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			shapes, _, runs, _ := shapesOf(t, pathDoc(c.content), Options{Shapes: true})
			if len(runs) != 1 || runs[0].FillAlpha != c.fill || runs[0].Blend != c.blend {
				t.Errorf("runs %+v, want one with opacity %v and blend %v", runs, c.fill, c.blend)
			}
			got := ""
			if len(shapes) > 0 {
				got = strings.SplitN(shapes[0].String(), " seq 0 ", 2)[1]
			}
			if got != c.shape {
				t.Errorf("shape %q, want %q", got, c.shape)
			}
		})
	}
	// After the group the state is what it was.
	_, _, runs, _ := shapesOf(t, pathDoc("/Half gs /Seven Do"+text), Options{})
	if len(runs) != 2 || runs[1].FillAlpha != 0.5 || runs[1].StrokeAlpha != 0.25 {
		t.Errorf("runs after a group: %+v", runs)
	}
}

// TestShapeBound: of more shapes than are kept, the first are recorded,
// and the page says from where on they are missing.
func TestShapeBound(t *testing.T) {
	content := strings.Repeat("0 0 5 5 re f ", 10) + text + strings.Repeat("0 0 5 5 re f ", 10)
	for _, c := range []struct{ max, shapes, upTo int }{{0, 20, -1}, {20, 20, -1}, {19, 19, 19}, {5, 5, 5}, {1, 1, 1}} {
		shapes, upTo, runs, _ := shapesOf(t, pathDoc(content), Options{Shapes: true, MaxShapes: c.max})
		if len(shapes) != c.shapes || int(upTo) != c.upTo || len(runs) != 1 || runs[0].Seq != 10 {
			t.Errorf("at most %d: %d shapes, up to %d, run after %d", c.max, len(shapes), upTo, runs[0].Seq)
		}
	}
}

// TestPathsCostNothingUnlessWanted: a page of paths that are neither
// clips nor wanted as shapes is read without converting a number of
// them, which shows as no allocation and no trace in the result.
func TestPathsCostNothingUnlessWanted(t *testing.T) {
	if pdftest.RaceEnabled {
		// The race detector drops pooled buffers at random, and they are
		// then allocated again.
		t.Skip("allocation counts are not meaningful under the race detector")
	}
	// The collector empties the pools of buffers as well; it is kept from
	// running while allocations are counted.
	defer debug.SetGCPercent(debug.SetGCPercent(-1))
	var sb strings.Builder
	for i := range 2000 {
		fmt.Fprintf(&sb, "%d %d m %d %d l %d %d %d %d %d %d c h S %d 0 9 9 re f\n", i, i, i+5, i, i, i, i+1, i+1, i+2, i+2, i)
	}
	var shapes, runs int
	var last Run
	use := func(c *Content) { shapes, runs, last = len(c.Shapes), len(c.Runs), c.Runs[len(c.Runs)-1] }
	allocs := func(content string) float64 {
		f := open(t, pdftest.OnePage(content), file.Limits{})
		page := &f.Pages()[0]
		run := func() {
			if res := Page(f, page, Options{Use: use}); res.Err != nil {
				t.Fatal(res.Err)
			}
		}
		run() // fonts are parsed and buffers grown on the first pass
		return testing.AllocsPerRun(10, run)
	}
	paths := allocs(sb.String() + text)
	if shapes != 0 || runs != 1 || last.CutOff || last.Seq != 4000 {
		t.Errorf("%d shapes, %d runs, the last %+v", shapes, runs, last)
	}
	if plain := allocs("4000 0 0 4000 0 0 cm" + text); paths > plain {
		t.Errorf("%.0f allocations for a page of 4000 paths, and %.0f for one without", paths, plain)
	}
}
