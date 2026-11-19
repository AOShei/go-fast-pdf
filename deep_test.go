package gofastpdf_test

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	gofastcore "github.com/AOShei/go-fast-core"
	gofastpdf "github.com/AOShei/go-fast-pdf"
	"github.com/AOShei/go-fast-pdf/internal/pdftest"
)

// The added text of the cases below stands at (72, 500) in 12 points:
// it reaches from 72 to about 139 across and from 497 to 510 up the
// page. band is a rectangle that holds it with room to spare, and
// circle a circle of radius 100 around (300, 300), which does not.
const (
	hide   = "BT /F1 12 Tf 72 500 Td " + secret + " ET\n"
	band   = "60 490 300 30 re"
	circle = "400 300 m 400 355 355 400 300 400 c 245 400 200 355 200 300 c 200 245 245 200 300 200 c 355 200 400 245 400 300 c"
	// white shows the added text in white, and leaves black behind.
	white = "1 g " + hide + "0 g\n"
)

// deepCase is a hiddenCase for the checks of DeepHidden. want is what
// they find, and plain what the default checks do.
type deepCase struct {
	hiddenCase
	plain gofastcore.Hidden
}

var deepStates = "/ExtGState << /Half << /ca 0.5 >> /Faint << /ca 0.03 >> /Clear << /ca 0 /CA 0 >> /Opaque << /ca 1 /CA 1 >> " +
	"/Mul << /BM /Multiply >> /Diff << /BM /Difference >> >>"

// formFont is the font dictionary of a form in a document with one
// object before those of the document itself: /F1 is then object 4.
const formFont = "/Resources << /Font << /F1 4 0 R >> >>"

var deepCases = []deepCase{
	// --- low contrast against what is painted under the text ---
	{hiddenCase: hiddenCase{name: "white text on a white rectangle over a dark page",
		before: "0.1 g 0 300 612 300 re f 1 g " + band + " f " + white, want: gofastcore.HiddenLowContrast}},
	{hiddenCase: hiddenCase{name: "dark gray text on a black rectangle",
		before: "0 g " + band + " f 0.07 g " + hide + "0 g\n", want: gofastcore.HiddenLowContrast}},
	{hiddenCase: hiddenCase{name: "white text on a rectangle that is nearly white",
		before: "0.97 g " + band + " f " + white, want: gofastcore.HiddenLowContrast}},
	{hiddenCase: hiddenCase{name: "yellow text on a yellow rectangle",
		before: "1 1 0 rg " + band + " f " + hide + "0 g\n", want: gofastcore.HiddenLowContrast}},
	{hiddenCase: hiddenCase{name: "white text on the page beside a dark rectangle",
		before: "0.1 g 300 300 200 100 re f " + white, want: gofastcore.HiddenLowContrast}},
	{hiddenCase: hiddenCase{name: "white text on a white rectangle drawn as four lines",
		before: "0.1 g 0 300 612 300 re f 1 g 60 490 m 360 490 l 360 520 l 60 520 l h f " + white, want: gofastcore.HiddenLowContrast}},
	{hiddenCase: hiddenCase{name: "white text on a white rectangle in a form that is moved",
		before:    "0.1 g 0 300 612 300 re f /Moved Do\n",
		objects:   []string{pdftest.Stream("/Type /XObject /Subtype /Form /BBox [0 0 612 792] /Matrix [1 0 0 1 0 200] "+formFont, "1 g 60 290 300 30 re f BT /F1 12 Tf 72 300 Td "+secret+" ET")},
		resources: "/XObject << /Moved 1 0 R >>",
		want:      gofastcore.HiddenLowContrast}},
	{hiddenCase: hiddenCase{name: "nearly transparent text",
		after: "q /Faint gs " + hide + "Q", resources: deepStates, want: gofastcore.HiddenLowContrast},
		plain: gofastcore.HiddenLowContrast},
	{hiddenCase: hiddenCase{name: "nearly transparent text on a white rectangle",
		before: "1 g " + band + " f 0 g q /Faint gs " + hide + "Q\n", resources: deepStates, want: gofastcore.HiddenLowContrast}},

	// --- covered by what is painted after the text ---
	{hiddenCase: hiddenCase{name: "text under a black rectangle drawn later",
		after: hide + "0 g " + band + " f", want: gofastcore.HiddenOccluded}},
	{hiddenCase: hiddenCase{name: "text under a white rectangle drawn later",
		after: hide + "1 g " + band + " f", want: gofastcore.HiddenOccluded}},
	{hiddenCase: hiddenCase{name: "text under a rectangle drawn as four lines",
		after: hide + "60 490 m 360 490 l 360 520 l 60 520 l f", want: gofastcore.HiddenOccluded}},
	{hiddenCase: hiddenCase{name: "text under the fill of a rectangle with a border",
		after: hide + "1 g 0 G 2 w " + band + " B", want: gofastcore.HiddenOccluded}},
	{hiddenCase: hiddenCase{name: "text under a rectangle in a colour that is not converted",
		after:     hide + "/Spot cs 1 scn " + band + " f",
		resources: "/ColorSpace << /Spot [/Separation /Gold /DeviceRGB << /FunctionType 2 /Domain [0 1] /C0 [1 1 1] /C1 [0.8 0.6 0] /N 1 >>] >>",
		want:      gofastcore.HiddenOccluded}},
	{hiddenCase: hiddenCase{name: "text under a rectangle inside a clip that leaves it whole",
		after: hide + "q 0 0 612 792 re W n 0 g " + band + " f Q", want: gofastcore.HiddenOccluded}},
	{hiddenCase: hiddenCase{name: "text under a rectangle on a page that is turned",
		after: hide + "0 g " + band + " f", extra: "/Rotate 90", want: gofastcore.HiddenOccluded}},
	{hiddenCase: hiddenCase{name: "text under a rectangle drawn in a scaled system",
		after: hide + "q 2 0 0 2 0 0 cm 0 g 30 245 150 15 re f Q", want: gofastcore.HiddenOccluded}},

	// --- cut off by a clipping path ---
	{hiddenCase: hiddenCase{name: "text outside a rectangular clip",
		after: "q 300 300 100 100 re W n " + hide + "Q", want: gofastcore.HiddenClipped}},
	{hiddenCase: hiddenCase{name: "text outside the box of a round clip",
		after: "q " + circle + " W n " + hide + "Q", want: gofastcore.HiddenClipped}},
	{hiddenCase: hiddenCase{name: "text in a clip of no area",
		after: "q 72 500 0 0 re W n " + hide + "Q", want: gofastcore.HiddenClipped}},
	{hiddenCase: hiddenCase{name: "text outside the second of two clips",
		after: "q 0 0 612 792 re W n q 300 300 100 100 re W* n " + hide + "Q Q", want: gofastcore.HiddenClipped}},
	{hiddenCase: hiddenCase{name: "text outside a clip set in a form",
		after:     "/Clipped Do",
		objects:   []string{pdftest.Stream("/Type /XObject /Subtype /Form /BBox [0 0 612 792] "+formFont, "300 300 100 100 re W n "+hide)},
		resources: "/XObject << /Clipped 1 0 R >>",
		want:      gofastcore.HiddenClipped}},

	// --- a transparency group ---
	{hiddenCase: hiddenCase{name: "text in a group painted with opacity 0",
		after:     "q /Clear gs /Group Do Q",
		objects:   []string{pdftest.Stream("/Type /XObject /Subtype /Form /BBox [0 0 612 792] /Group << /S /Transparency >> /Resources << /Font << /F1 4 0 R >> "+deepStates+" >>", "/Opaque gs "+hide)},
		resources: "/XObject << /Group 1 0 R >> " + deepStates,
		want:      gofastcore.HiddenTransparent},
		plain: gofastcore.HiddenTransparent},

	// --- white text and what is painted after it ---
	{hiddenCase: hiddenCase{name: "white text under a rectangle that lets it show through",
		before: white + "q /Half gs 0.8 0.9 1 rg " + band + " f Q 0 g\n", resources: deepStates, want: gofastcore.HiddenLowContrast},
		plain: gofastcore.HiddenLowContrast},
	{hiddenCase: hiddenCase{name: "white text under a black rectangle",
		before: white + band + " f\n", want: gofastcore.HiddenLowContrast | gofastcore.HiddenOccluded},
		plain: gofastcore.HiddenLowContrast},
	// A page can draw white text first and its dark ground afterwards,
	// through a mask or a path that spares the letters. Without the
	// shapes, the text looks like white on a white page.
	{hiddenCase: hiddenCase{name: "white text, then a ground with round corners over it",
		before: white + "0.1 g 70 490 m 350 490 l 360 490 360 500 360 500 c 360 510 l 360 520 350 520 350 520 c 70 520 l 60 520 60 510 60 510 c 60 500 l 60 490 70 490 70 490 c f 0 g\n"},
		plain: gofastcore.HiddenLowContrast},
	{hiddenCase: hiddenCase{name: "white text, then a ground painted through a soft mask",
		before:    white + "q /Masked gs 0.1 g " + band + " f Q 0 g\n",
		resources: "/ExtGState << /Masked << /SMask << /S /Luminosity >> >> >>"},
		plain: gofastcore.HiddenLowContrast},
	{hiddenCase: hiddenCase{name: "white text on a white rectangle, then a photograph over it",
		before:    "0.1 g 0 300 612 300 re f 1 g " + band + " f " + white + "q /Half gs 300 0 0 30 60 490 cm /Im1 Do Q\n",
		objects:   []string{grayImage},
		resources: "/XObject << /Im1 1 0 R >> " + deepStates}},

	// --- text that must not be flagged ---
	{hiddenCase: hiddenCase{name: "green text on a red rectangle that is as bright",
		before: "0.9 0 0 rg " + band + " f 0 0.54 0 rg " + hide + "0 g\n"}},
	{hiddenCase: hiddenCase{name: "yellow text on the white page", before: "1 1 0 rg " + hide + "0 g\n"}},
	{hiddenCase: hiddenCase{name: "white text on a dark banner", before: "0.1 g " + band + " f " + white}},
	{hiddenCase: hiddenCase{name: "black text on a coloured cell", before: "0.9 0.9 0.6 rg " + band + " f 0 g " + hide}},
	{hiddenCase: hiddenCase{name: "white text on a dark rectangle over a white one",
		before: "1 g 50 480 320 50 re f 0.1 g " + band + " f " + white}},
	{hiddenCase: hiddenCase{name: "white text on a dark rectangle that does not hold all of it",
		before: "0.1 g 60 490 40 30 re f " + white}},
	{hiddenCase: hiddenCase{name: "text in a clip's box and outside its circle",
		after: "q " + circle + " W n BT /F1 4 Tf 375 392 Td " + secret + " ET Q"}},
	{hiddenCase: hiddenCase{name: "white text over a photograph",
		before:    "q 300 0 0 30 60 490 cm /Im1 Do Q " + white,
		objects:   []string{grayImage},
		resources: "/XObject << /Im1 1 0 R >>"}},
	{hiddenCase: hiddenCase{name: "white text over a photograph on a white rectangle",
		before:    "1 g " + band + " f q 300 0 0 30 60 490 cm /Im1 Do Q " + white,
		objects:   []string{grayImage},
		resources: "/XObject << /Im1 1 0 R >>"}},
	{hiddenCase: hiddenCase{name: "text half under a rectangle", after: hide + "0 g 60 490 40 30 re f"}},
	{hiddenCase: hiddenCase{name: "text under a rectangle that covers its upper half", after: hide + "0 g 60 504 300 30 re f"}},
	{hiddenCase: hiddenCase{name: "text under a rectangle with opacity 0.5",
		after: hide + "q /Half gs 0 g " + band + " f Q", resources: deepStates}},
	{hiddenCase: hiddenCase{name: "white text with a dark outline on a white rectangle",
		before: "1 g " + band + " f q 1 g 0 G BT /F1 12 Tf 2 Tr 72 500 Td " + secret + " ET Q 0 g\n"}},
	{hiddenCase: hiddenCase{name: "white text on a banner with round corners",
		before: "0.1 g 70 490 m 350 490 l 360 490 360 500 360 500 c 360 510 l 360 520 350 520 350 520 c 70 520 l 60 520 60 510 60 510 c 60 500 l 60 490 70 490 70 490 c f " + white}},
	{hiddenCase: hiddenCase{name: "white text on a banner drawn as a thick line",
		before: "0.1 G 30 w 60 505 m 360 505 l S 1 w 0 G " + white}},
	{hiddenCase: hiddenCase{name: "white text on a shading",
		before:    "q " + band + " W n /Dark sh Q " + white,
		resources: "/Shading << /Dark << /ShadingType 2 /ColorSpace /DeviceGray /Coords [0 0 612 0] /Function << /FunctionType 2 /Domain [0 1] /C0 [0] /C1 [0.3] /N 1 >> >> >>"}},
	{hiddenCase: hiddenCase{name: "white text on a dark fill seen through a round clip",
		before: "q 400 500 m 400 555 355 600 300 600 c 60 600 l 60 400 l 300 400 l 355 400 400 445 400 500 c W n 0.1 g 0 0 612 600 re f Q " + white}},
	{hiddenCase: hiddenCase{name: "white text on a dark rectangle in a colour that is not converted",
		before:    "/Spot cs 1 scn " + band + " f " + white,
		resources: "/ColorSpace << /Spot [/Separation /Gold /DeviceRGB << /FunctionType 2 /Domain [0 1] /C0 [1 1 1] /C1 [0 0 0] /N 1 >>] >>"}},
	{hiddenCase: hiddenCase{name: "white text painted with the blend mode Difference",
		before: "q /Diff gs " + white + "Q\n", resources: deepStates}},
	{hiddenCase: hiddenCase{name: "white text on a white rectangle painted with a blend mode",
		before: "0.1 g 0 300 612 300 re f q /Mul gs 1 g " + band + " f Q " + white, resources: deepStates}},
	{hiddenCase: hiddenCase{name: "text under a rectangle painted with a blend mode",
		after: hide + "q /Mul gs 1 1 0 rg " + band + " f Q", resources: deepStates}},
	{hiddenCase: hiddenCase{name: "text under a rectangle filled with a pattern",
		after:     hide + "/Pattern cs /Dots scn " + band + " f",
		objects:   []string{pdftest.Stream("/Type /Pattern /PatternType 1 /PaintType 1 /TilingType 1 /BBox [0 0 10 10] /XStep 10 /YStep 10 /Resources << >>", "0 0 2 2 re f")},
		resources: "/Pattern << /Dots 1 0 R >>"}},
	{hiddenCase: hiddenCase{name: "text under a rectangle that a clip keeps off it",
		after: hide + "q 300 0 100 792 re W n 0 g " + band + " f Q"}},
	{hiddenCase: hiddenCase{name: "text under a rectangle seen through a round clip",
		after: hide + "q " + circle + " W n 0 g " + band + " f Q"}},
	{hiddenCase: hiddenCase{name: "text under a rectangle in optional content that is off",
		after:     hide + "/OC /Layer BDC 0 g " + band + " f EMC",
		objects:   []string{"<< /Type /OCG /Name (Hidden layer) >>"},
		resources: "/Properties << /Layer 1 0 R >>",
		catalog:   "/OCProperties << /OCGs [1 0 R] /D << /OFF [1 0 R] >> >>"}},
	{hiddenCase: hiddenCase{name: "text under a rectangle with opacity 0",
		after: hide + "q /Clear gs 0 g " + band + " f Q", resources: deepStates}},
	{hiddenCase: hiddenCase{name: "text inside a frame that is stroked", after: hide + "0 G " + band + " S"}},
	{hiddenCase: hiddenCase{name: "text under a triangle", after: hide + "0 g 60 490 m 660 490 l 60 790 l f"}},
	{hiddenCase: hiddenCase{name: "text under a rectangle that is turned", after: hide + "q 0.966 0.259 -0.259 0.966 0 0 cm 0 g 150 440 300 60 re f Q"}},
	{hiddenCase: hiddenCase{name: "text under a path of two rectangles", after: hide + "0 g " + band + " 62 492 296 26 re f*"}},
	{hiddenCase: hiddenCase{name: "text inside a clip", after: "q " + band + " W n " + hide + "Q"}},
	{hiddenCase: hiddenCase{name: "text after a clip that was undone", after: "q 0 0 10 10 re W n Q " + hide}},
	{hiddenCase: hiddenCase{name: "text after a form that sets a clip",
		after:     "/Clipped Do " + hide,
		objects:   []string{pdftest.Stream("/Type /XObject /Subtype /Form /BBox [0 0 612 792]", "0 0 10 10 re W n")},
		resources: "/XObject << /Clipped 1 0 R >>"}},
	{hiddenCase: hiddenCase{name: "text after a path that W was not followed by", after: "0 0 10 10 re n " + hide}},
	{hiddenCase: hiddenCase{name: "text after W without a path", after: "q W n " + hide + "Q"}},
	{hiddenCase: hiddenCase{name: "text in a group painted with opacity 0.5",
		after:     "q /Half gs /Group Do Q",
		objects:   []string{pdftest.Stream("/Type /XObject /Subtype /Form /BBox [0 0 612 792] /Group << /S /Transparency >> "+formFont, hide)},
		resources: "/XObject << /Group 1 0 R >> " + deepStates}},
}

// TestDeepHiddenText checks the reasons that the checks of DeepHidden
// give, case by case, and what the default checks make of the same
// page.
func TestDeepHiddenText(t *testing.T) {
	for _, c := range deepCases {
		t.Run(c.name, func(t *testing.T) {
			checkHidden(t, c.hiddenCase, &gofastcore.Options{DeepHidden: true}, c.want)
			checkHidden(t, c.hiddenCase, &gofastcore.Options{}, c.plain)
		})
	}
}

// TestDeepHiddenLeavesTheTextAlone: the checks of DeepHidden flag text
// and change none. The words of every case are the same with them and
// without.
func TestDeepHiddenLeavesTheTextAlone(t *testing.T) {
	words := func(doc *gofastcore.Document) string {
		w := strings.Fields(doc.Text())
		// The order may differ: hidden text is laid out apart.
		counts := map[string]int{}
		for _, s := range w {
			counts[s]++
		}
		return fmt.Sprint(len(w), counts)
	}
	for _, c := range deepCases {
		data := c.doc(true).bytes()
		if a, b := words(mustExtract(t, data, nil)), words(mustExtract(t, data, &gofastcore.Options{DeepHidden: true})); a != b {
			t.Errorf("%s: words by default %s, with DeepHidden %s", c.name, a, b)
		}
	}
}

// TestDeepHiddenTextLayer: the text of a scan is a text layer and
// nothing else with DeepHidden too, whatever is drawn around it.
func TestDeepHiddenTextLayer(t *testing.T) {
	const words = "BT /F1 12 Tf %s 72 720 Td 14 TL (Minutes of the meeting held on the) Tj T* (third of March in the town hall.) Tj ET\n"
	layer := func(state string) string { return strings.Replace(words, "%s", state, 1) }
	over := "0 g 60 680 400 60 re f\n" // a rectangle that covers the text
	variants := []struct{ name, content string }{
		{"text drawn after the page image", pageImage + layer("3 Tr")},
		{"text drawn before the page image", layer("") + pageImage},
		{"a layer smaller than two points", pageImage + strings.Replace(layer("3 Tr"), "/F1 12 Tf", "/F1 1 Tf", 1)},
		{"a white page under the image", "1 g 0 0 612 792 re f " + pageImage + layer("3 Tr")},
		{"white text before the page image", "1 g " + layer("") + "0 g " + pageImage},
		{"the image inside a clip that is the page", "q 0 0 612 792 re W n " + pageImage + "Q " + layer("3 Tr")},
		{"image and layer inside a clip", "q 0 0 612 792 re W n " + pageImage + layer("3 Tr") + "Q"},
		{"a rectangle over the layer", pageImage + layer("3 Tr") + over},
		{"a rectangle over text and image", layer("") + pageImage + over},
		{"a layer outside a clip", pageImage + "q 0 0 100 100 re W n " + layer("3 Tr") + "Q"},
	}
	for _, v := range variants {
		t.Run(v.name, func(t *testing.T) {
			d := synthDoc{Objects: []string{grayImage}, Pages: []synthPage{{Content: v.content, Resources: "/XObject << /Im1 1 0 R >>"}}}
			doc := mustExtract(t, d.bytes(), &gofastcore.Options{DeepHidden: true})
			part := &doc.Parts[0]
			if len(part.Blocks) == 0 {
				t.Fatal("no text block")
			}
			for i := range part.Blocks {
				if b := &part.Blocks[i]; b.Hidden != gofastcore.HiddenTextLayer {
					t.Errorf("reasons %q, want text_layer alone\n%s", b.Hidden, describe(part))
				}
			}
			if w, ok := warning(part.Warnings, gofastcore.WarnTextHidden); !ok || w.Severity != gofastcore.SeverityInfo {
				t.Errorf("text.hidden: %+v, present %v; want it at info", w, ok)
			}
		})
	}
}

// TestClippedImages: an image hides the text under it as far as the
// clipping path it is drawn through lets it be painted. This is checked
// without DeepHidden as well.
func TestClippedImages(t *testing.T) {
	image := "q 300 0 0 30 60 490 cm /Im1 Do Q\n" // over the added text
	cases := []struct {
		name, after string
		want        gofastcore.Hidden
	}{
		{"no clip", image, gofastcore.HiddenOccluded},
		{"a clip that leaves the image whole", "q 0 0 612 792 re W n " + image + "Q", gofastcore.HiddenOccluded},
		{"a clip that holds the text", "q 65 492 200 26 re W n " + image + "Q", gofastcore.HiddenOccluded},
		{"a clip that keeps the image off the text", "q 200 0 400 792 re W n " + image + "Q", 0},
		{"a clip that leaves half of the text in view", "q 100 0 500 792 re W n " + image + "Q", 0},
		{"a clip of no area", "q 0 0 0 0 re W n " + image + "Q", 0},
		{"a round clip", "q 400 500 m 400 555 355 600 300 600 c 60 600 l 60 400 l 300 400 l 355 400 400 445 400 500 c W n " + image + "Q", 0},
		{"a clip of five corners", "q 0 0 m 612 0 l 612 792 l 300 700 l 0 792 l W n " + image + "Q", 0},
		{"a clip drawn as four lines", "q 0 0 m 612 0 l 612 792 l 0 792 l h W n " + image + "Q", gofastcore.HiddenOccluded},
		{"two clips, the second keeps the image off", "q 0 0 612 792 re W n 200 0 400 792 re W n " + image + "Q", 0},
		{"a clip that was undone", "q 200 0 400 792 re W n Q " + image, gofastcore.HiddenOccluded},
		{"text that clips", "q BT /F1 40 Tf 7 Tr 300 300 Td (X) Tj ET " + image + "Q", 0},
		{"an image painted with a blend mode", "q /Mul gs " + image + "Q", 0},
	}
	for _, c := range cases {
		for _, deep := range []bool{false, true} {
			hc := hiddenCase{name: c.name, after: hide + c.after, objects: []string{grayImage},
				resources: "/XObject << /Im1 1 0 R >> " + deepStates, want: c.want}
			t.Run(fmt.Sprintf("%s/deep=%v", c.name, deep), func(t *testing.T) {
				checkHidden(t, hc, &gofastcore.Options{DeepHidden: deep}, c.want)
			})
		}
	}

	// A form is painted inside its box: an image in it covers no more.
	form := func(box string) synthDoc {
		return synthDoc{
			Objects: []string{grayImage, pdftest.Stream("/Type /XObject /Subtype /Form /BBox "+box+" /Resources << /XObject << /Im1 1 0 R >> >>", image)},
			Pages:   []synthPage{{Content: visibleLines + hide + "/Form Do", Resources: "/XObject << /Form 2 0 R >>"}},
		}
	}
	for box, want := range map[string]string{"[0 0 612 792]": "occluded: secret words", "[200 0 612 792]": "secret words"} {
		part := &mustExtract(t, form(box).bytes(), nil).Parts[0]
		if got := texts(part); len(got) != 2 || got[1] != want {
			t.Errorf("an image in a form with the box %s: blocks %q, want the second to be %q", box, got, want)
		}
	}
}

// manyShapes is a page that paints n small rectangles in a corner, then
// the visible lines, and around them what cases is.
func manyShapes(n int, before, after string) []byte {
	var sb strings.Builder
	sb.WriteString(before)
	sb.WriteString("0.5 g\n")
	for i := range n {
		fmt.Fprintf(&sb, "%d 10 1 1 re f\n", 10+i%500)
	}
	sb.WriteString("0 g\n" + visibleLines + after)
	return onePage(sb.String()).bytes()
}

// TestDeepHiddenShapeBound: a page with more shapes than are followed
// keeps its text and the flags found up to there, says that its checks
// are not complete, and takes two reasons off the list of what was
// checked.
func TestDeepHiddenShapeBound(t *testing.T) {
	ctx := context.Background()
	extract := func(e *gofastpdf.Extractor, data []byte, opts *gofastcore.Options) *gofastcore.Document {
		t.Helper()
		doc, err := e.Extract(ctx, bytes.NewReader(data), int64(len(data)), opts)
		if err != nil {
			t.Fatal(err)
		}
		if err := checkContract(doc, opts); err != nil {
			t.Fatal(err)
		}
		return doc
	}
	deep := &gofastcore.Options{DeepHidden: true}

	// Before the bound: white text on a white rectangle, and text under a
	// rectangle. After it: the same again, lower on the page.
	early := "0.1 g 0 300 612 300 re f 1 g " + band + " f " + white +
		"BT /F1 12 Tf 72 420 Td (under a box) Tj ET 0 g 60 410 300 30 re f\n"
	late := "0.1 g 0 100 612 200 re f 1 g 60 190 300 30 re f 1 g BT /F1 12 Tf 72 200 Td (late words) Tj ET 0 g\n" +
		"BT /F1 12 Tf 72 120 Td (late under a box) Tj ET 0 g 60 110 300 30 re f\n" +
		"q 0 0 10 10 re W n BT /F1 12 Tf 72 250 Td (cut off) Tj ET Q\n"
	data := manyShapes(100, early, late)
	const shapes = 50 // reached among the small rectangles

	whole := extract(gofastpdf.BoundedExtractor(0, 0), data, deep)
	if whole.HiddenChecked != defaultChecked|deepChecked || whole.Parts[0].Has(gofastpdf.WarnDeepHiddenIncomplete) {
		t.Errorf("without a bound: checked %s, warnings %v", whole.HiddenChecked, codes(whole.Parts[0].Warnings))
	}
	wantWhole := map[string]gofastcore.Hidden{
		"secret words": gofastcore.HiddenLowContrast, "under a box": gofastcore.HiddenOccluded,
		"late words": gofastcore.HiddenLowContrast, "late under a box": gofastcore.HiddenOccluded,
		"cut off": gofastcore.HiddenClipped, visibleText: 0,
	}
	// With the bound, what was found before it stands, what comes after
	// is not found, and clipping is still followed.
	wantBound := map[string]gofastcore.Hidden{
		"secret words": gofastcore.HiddenLowContrast, "under a box": gofastcore.HiddenOccluded,
		"late words": 0, "late under a box": 0,
		"cut off": gofastcore.HiddenClipped, visibleText: 0,
	}
	check := func(name string, doc *gofastcore.Document, want map[string]gofastcore.Hidden) {
		t.Helper()
		got := map[string]gofastcore.Hidden{}
		for _, b := range doc.Parts[0].Blocks {
			got[b.Text] = b.Hidden
		}
		if fmt.Sprint(got) != fmt.Sprint(want) {
			t.Errorf("%s:\n got %v\nwant %v", name, got, want)
		}
	}
	check("without a bound", whole, wantWhole)

	for name, e := range map[string]*gofastpdf.Extractor{
		"a bound on the shapes": gofastpdf.BoundedExtractor(shapes, 0),
	} {
		doc := extract(e, data, deep)
		part := &doc.Parts[0]
		check(name, doc, wantBound)
		if w, ok := warning(part.Warnings, gofastpdf.WarnDeepHiddenIncomplete); !ok || w.Severity != gofastcore.SeverityWarning {
			t.Errorf("%s: %s: %+v, present %v", name, gofastpdf.WarnDeepHiddenIncomplete, w, ok)
		}
		if part.Has(gofastcore.WarnLimitExceeded) || len(doc.Warnings) != 0 {
			t.Errorf("%s: warnings %v on the part and %v on the document", name, codes(part.Warnings), codes(doc.Warnings))
		}
		if want := defaultChecked | gofastcore.HiddenClipped; doc.HiddenChecked != want {
			t.Errorf("%s: checked %s, want %s", name, doc.HiddenChecked, want)
		}
		// The text is that of the run without DeepHidden.
		plain := extract(e, data, &gofastcore.Options{})
		if a, b := strings.Fields(plain.Text()), strings.Fields(doc.Text()); len(a) != len(b) {
			t.Errorf("%s: %d words, and %d without DeepHidden", name, len(b), len(a))
		}
		if plain.Parts[0].Has(gofastpdf.WarnDeepHiddenIncomplete) {
			t.Errorf("%s: the warning without DeepHidden", name)
		}
	}

	// A bound on the work stops the checks as well. Whatever was found
	// by then stands, and nothing visible is flagged.
	doc := extract(gofastpdf.BoundedExtractor(0, 40), data, deep)
	if !doc.Parts[0].Has(gofastpdf.WarnDeepHiddenIncomplete) || doc.HiddenChecked != defaultChecked|gofastcore.HiddenClipped {
		t.Errorf("a bound on the work: checked %s, warnings %v", doc.HiddenChecked, codes(doc.Parts[0].Warnings))
	}
	for _, b := range doc.Parts[0].Blocks {
		if b.Text == visibleText && b.Hidden != 0 {
			t.Errorf("a bound on the work: the visible text is %s", b.Hidden)
		}
		if want := wantWhole[b.Text]; b.Hidden&^want != 0 {
			t.Errorf("a bound on the work: %q is %s, and %s without the bound", b.Text, b.Hidden, want)
		}
	}

	// One page with too many shapes takes the two reasons off the list
	// for the document, and only that page has the warning.
	d := synthDoc{Pages: []synthPage{{Content: visibleLines}, {Content: string(pageContent(t, data))}, {Content: visibleLines}}}
	three := extract(gofastpdf.BoundedExtractor(shapes, 0), d.bytes(), deep)
	for i, p := range three.Parts {
		if p.Has(gofastpdf.WarnDeepHiddenIncomplete) != (i == 1) {
			t.Errorf("page %d of three: warnings %v", i+1, codes(p.Warnings))
		}
	}
	if want := defaultChecked | gofastcore.HiddenClipped; three.HiddenChecked != want {
		t.Errorf("three pages: checked %s, want %s", three.HiddenChecked, want)
	}
}

// pageContent returns the content of the first page of a synthetic
// document.
func pageContent(t *testing.T, data []byte) []byte {
	t.Helper()
	i := bytes.Index(data, []byte("stream\n"))
	j := bytes.Index(data, []byte("\nendstream"))
	if i < 0 || j < i {
		t.Fatal("no content stream")
	}
	return data[i+len("stream\n") : j]
}

// TestDeepHiddenWorkers: the flags do not depend on how many pages are
// read at a time.
func TestDeepHiddenWorkers(t *testing.T) {
	var pages []synthPage
	objects := []string{grayImage}
	for i, c := range deepCases {
		if len(c.objects) > 0 || c.catalog != "" || c.extra != "" {
			continue
		}
		pages = append(pages, synthPage{Content: c.before + visibleLines + c.after, Resources: c.resources})
		if i%7 == 0 {
			pages = append(pages, synthPage{Content: pageImage + "BT /F1 12 Tf 3 Tr 72 500 Td " + secret + " ET", Resources: "/XObject << /Im1 1 0 R >>"})
		}
	}
	data := synthDoc{Objects: objects, Pages: pages}.bytes()
	first := mustExtract(t, data, &gofastcore.Options{DeepHidden: true, Workers: 1})
	flagged := 0
	for _, p := range first.Parts {
		if p.Has(gofastcore.WarnTextHidden) {
			flagged++
		}
	}
	if flagged < 15 {
		t.Fatalf("%d of %d pages have hidden text: the test would say little", flagged, len(first.Parts))
	}
	for _, workers := range []int{2, 8, 64} {
		doc := mustExtract(t, data, &gofastcore.Options{DeepHidden: true, Workers: workers})
		if diff := sameDocument(first, doc); diff != "" {
			t.Errorf("%d workers: %s", workers, diff)
		}
	}
}
