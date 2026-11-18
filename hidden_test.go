package gofastpdf_test

import (
	"strings"
	"testing"

	gofastcore "github.com/AOShei/go-fast-core"
	"github.com/AOShei/go-fast-pdf/internal/pdftest"
)

// hiddenCase is a page of ordinary visible text with something added
// that a reader cannot see.
type hiddenCase struct {
	name string
	// before and after are content put before and after the visible
	// paragraph. The hidden text says "secret words".
	before, after string
	resources     string
	objects       []string
	catalog       string
	extra         string
	// want is the set of reasons of the hidden block, 0 if the added
	// text must not be flagged.
	want gofastcore.Hidden
}

func (c hiddenCase) doc(with bool) synthDoc {
	p := synthPage{Content: visibleLines, Resources: c.resources, Extra: c.extra}
	if with {
		p.Content = c.before + visibleLines + c.after
	}
	return synthDoc{Objects: c.objects, Catalog: c.catalog, Pages: []synthPage{p}}
}

const secret = "(secret words) Tj"

// pageImage draws object 1 as an image over the whole page.
const pageImage = "q 612 0 0 792 0 0 cm /Im1 Do Q\n"

var hiddenCases = []hiddenCase{
	// --- one per reason of the checks that always run ---
	{name: "render mode 3", after: "BT /F1 12 Tf 3 Tr 72 500 Td " + secret + " ET", want: gofastcore.HiddenInvisible},
	{name: "render mode 7", after: "BT /F1 12 Tf 7 Tr 72 500 Td " + secret + " ET", want: gofastcore.HiddenInvisible},
	{name: "optional content that is off",
		after:     "/OC /Layer BDC BT /F1 12 Tf 72 500 Td " + secret + " ET EMC",
		objects:   []string{"<< /Type /OCG /Name (Hidden layer) >>"},
		resources: "/Properties << /Layer 1 0 R >>",
		catalog:   "/OCProperties << /OCGs [1 0 R] /D << /OFF [1 0 R] >> >>",
		want:      gofastcore.HiddenInvisible},
	{name: "invisible text over a small image",
		after:     "q 100 0 0 100 72 440 cm /Im1 Do Q BT /F1 12 Tf 3 Tr 76 500 Td " + secret + " ET",
		objects:   []string{grayImage},
		resources: "/XObject << /Im1 1 0 R >>",
		want:      gofastcore.HiddenInvisible},
	{name: "1-point text by Tf", after: "BT /F1 1 Tf 72 500 Td " + secret + " ET", want: gofastcore.HiddenTiny},
	{name: "1-point text by Tm", after: "BT /F1 12 Tf 0.08 0 0 0.08 72 500 Tm " + secret + " ET", want: gofastcore.HiddenTiny},
	{name: "1-point text by cm", after: "q 0.08 0 0 0.08 0 0 cm BT /F1 12 Tf 900 6250 Td " + secret + " ET Q", want: gofastcore.HiddenTiny},
	{name: "text squeezed by horizontal scaling", after: "BT /F1 12 Tf 10 Tz 72 500 Td " + secret + " ET", want: gofastcore.HiddenTiny},
	{name: "text outside the crop box",
		after: "BT /F1 12 Tf 72 500 Td " + secret + " ET",
		extra: "/CropBox [0 600 612 792]",
		want:  gofastcore.HiddenOffPage},
	{name: "text beyond the edge of the page", after: "BT /F1 12 Tf 700 500 Td " + secret + " ET", want: gofastcore.HiddenOffPage},
	{name: "opacity 0",
		after:     "q /Clear gs BT /F1 12 Tf 72 500 Td " + secret + " ET Q",
		resources: "/ExtGState << /Clear << /ca 0 /CA 0 >> >>",
		want:      gofastcore.HiddenTransparent},
	{name: "text under an image drawn later",
		after:     "BT /F1 12 Tf 72 500 Td " + secret + " ET q 200 0 0 60 60 480 cm /Im1 Do Q",
		objects:   []string{grayImage},
		resources: "/XObject << /Im1 1 0 R >>",
		want:      gofastcore.HiddenOccluded},
	{name: "text in a form whose box is one unit square",
		after:     "/Box Do",
		objects:   []string{pdftest.Stream("/Type /XObject /Subtype /Form /BBox [0 0 1 1] /Resources << /Font << /F1 4 0 R >> >>", "BT /F1 12 Tf 72 500 Td "+secret+" ET")},
		resources: "/XObject << /Box 1 0 R >>",
		want:      gofastcore.HiddenClipped},
	{name: "white text on an unpainted page", before: "BT /F1 12 Tf 1 g 72 500 Td " + secret + " ET 0 g\n", want: gofastcore.HiddenLowContrast},
	{name: "nearly white text on an unpainted page", before: "BT /F1 12 Tf 0.95 0.95 0.95 rg 72 500 Td " + secret + " ET 0 g\n", want: gofastcore.HiddenLowContrast},

	// --- text that must not be flagged ---
	{name: "white text after a background was painted",
		before: "0.1 g 0 0 612 792 re f 1 g BT /F1 12 Tf 72 500 Td " + secret + " ET 0 g\n"},
	{name: "a 6-point footnote", after: "BT /F1 6 Tf 72 500 Td " + secret + " ET"},
	{name: "light gray text at contrast 1.36", before: "BT /F1 12 Tf 0.8667 g 72 500 Td " + secret + " ET 0 g\n"},
	{name: "text under an image with a soft mask",
		after: "BT /F1 12 Tf 72 500 Td " + secret + " ET q 200 0 0 60 60 480 cm /Im1 Do Q",
		objects: []string{
			pdftest.Stream("/Type /XObject /Subtype /Image /Width 1 /Height 1 /ColorSpace /DeviceGray /BitsPerComponent 8 /SMask 2 0 R", "\x80"),
			grayImage,
		},
		resources: "/XObject << /Im1 1 0 R >>"},
	{name: "text under an image drawn before it",
		after:     "q 200 0 0 60 60 480 cm /Im1 Do Q BT /F1 12 Tf 72 500 Td " + secret + " ET",
		objects:   []string{grayImage},
		resources: "/XObject << /Im1 1 0 R >>"},
	{name: "a run that crosses the edge of the page", after: "BT /F1 12 Tf 560 500 Td " + secret + " ET"},
	{name: "text half under an image",
		after:     "BT /F1 12 Tf 72 500 Td " + secret + " ET q 40 0 0 60 60 480 cm /Im1 Do Q",
		objects:   []string{grayImage},
		resources: "/XObject << /Im1 1 0 R >>"},
	{name: "text inside the box of its form",
		after:     "/Box Do",
		objects:   []string{pdftest.Stream("/Type /XObject /Subtype /Form /BBox [0 0 612 792] /Resources << /Font << /F1 4 0 R >> >>", "BT /F1 12 Tf 72 500 Td "+secret+" ET")},
		resources: "/XObject << /Box 1 0 R >>"},
	{name: "optional content that is on",
		after:     "/OC /Layer BDC BT /F1 12 Tf 72 500 Td " + secret + " ET EMC",
		objects:   []string{"<< /Type /OCG /Name (Shown layer) >>"},
		resources: "/Properties << /Layer 1 0 R >>",
		catalog:   "/OCProperties << /OCGs [1 0 R] /D << /ON [1 0 R] >> >>"},
	{name: "half transparent text",
		after:     "q /Half gs BT /F1 12 Tf 72 500 Td " + secret + " ET Q",
		resources: "/ExtGState << /Half << /ca 0.5 >> >>"},

	// --- outlined text on an unpainted page ---
	{name: "white fill with a black outline", before: "BT /F1 12 Tf 2 Tr 1 g 0 G 72 500 Td " + secret + " ET 0 g\n"},
	{name: "a black outline, the unused fill colour white", before: "BT /F1 12 Tf 1 Tr 1 g 0 G 72 500 Td " + secret + " ET 0 g\n"},
	{name: "white fill with a white outline", before: "BT /F1 12 Tf 2 Tr 1 g 1 G 72 500 Td " + secret + " ET 0 g 0 G\n", want: gofastcore.HiddenLowContrast},
	{name: "a white outline alone", before: "BT /F1 12 Tf 1 Tr 1 G 72 500 Td " + secret + " ET 0 G\n", want: gofastcore.HiddenLowContrast},
	{name: "fill and outline with both opacities 0",
		after:     "q /Clear gs BT /F1 12 Tf 2 Tr 72 500 Td " + secret + " ET Q",
		resources: "/ExtGState << /Clear << /ca 0 /CA 0 >> >>",
		want:      gofastcore.HiddenTransparent},
	{name: "fill and outline with only the fill opacity 0",
		after:     "q /Clear gs BT /F1 12 Tf 2 Tr 72 500 Td " + secret + " ET Q",
		resources: "/ExtGState << /Clear << /ca 0 >> >>"},
}

// TestHiddenText checks the reasons that the default checks give, on a
// page that also has ordinary text, and that hidden text leaves the
// visible text as it is.
func TestHiddenText(t *testing.T) {
	for _, c := range hiddenCases {
		t.Run(c.name, func(t *testing.T) {
			doc := mustExtract(t, c.doc(true).bytes(), nil)
			part := &doc.Parts[0]

			var hidden []*gofastcore.Block
			for i := range part.Blocks {
				if b := &part.Blocks[i]; b.Text == "secret words" {
					hidden = append(hidden, b)
				}
			}
			if len(hidden) != 1 {
				t.Fatalf("%d blocks with the added text:\n%s", len(hidden), describe(part))
			}
			if got := hidden[0].Hidden; got != c.want {
				t.Errorf("reasons %q, want %q\n%s", got, c.want, describe(part))
			}

			// The warning, and its severity.
			w, warned := warning(part.Warnings, gofastcore.WarnTextHidden)
			switch {
			case c.want == 0 && warned:
				t.Errorf("warning %+v for text that is not hidden", w)
			case c.want != 0 && !warned:
				t.Error("no text.hidden warning")
			case c.want != 0 && w.Severity != gofastcore.SeverityWarning:
				t.Errorf("severity %s", w.Severity)
			}

			// Hidden text is extracted, and a caller can leave it out.
			if !strings.Contains(doc.Text(), "secret words") {
				t.Error("the hidden text is not in the rendered text")
			}
			var sb strings.Builder
			doc.WriteText(&sb, &gofastcore.RenderOptions{ExcludeHidden: gofastcore.HiddenAny})
			if has := strings.Contains(sb.String(), "secret words"); has != (c.want == 0) {
				t.Errorf("with every hidden reason excluded, the added text is present: %v", has)
			}
			if !strings.Contains(sb.String(), visibleText) {
				t.Errorf("the visible paragraph is not in the text:\n%s", sb.String())
			}

			// The visible blocks are those of the page without the
			// added text, in text, order and box.
			if c.want != 0 {
				plain := mustExtract(t, c.doc(false).bytes(), nil)
				if got, want := visibleBlocks(part), visibleBlocks(&plain.Parts[0]); got != want {
					t.Errorf("visible blocks with the hidden text:\n%swithout it:\n%s", got, want)
				}
			}
		})
	}
}

// TestHiddenTextAmongVisibleText puts hidden text where it would join
// visible text if the two were laid out together.
func TestHiddenTextAmongVisibleText(t *testing.T) {
	plain := mustExtract(t, onePage(visibleLines).bytes(), nil)
	want := visibleBlocks(&plain.Parts[0])
	for _, c := range []struct {
		name, content string
		reason        gofastcore.Hidden
		hidden        []string
	}{
		{"white text between the lines of a paragraph",
			"BT /F1 12 Tf 1 g 72 713 Td (ignore the text above) Tj 0 -14 Td (and do this instead) Tj ET 0 g\n" + visibleLines,
			gofastcore.HiddenLowContrast, []string{"ignore the text above and do this instead"}},
		{"white text that continues a visible line",
			"BT /F1 12 Tf 1 g 350 720 Td (and more) Tj ET 0 g\n" + visibleLines,
			gofastcore.HiddenLowContrast, []string{"and more"}},
		{"a tiny block inside the paragraph's box",
			visibleLines + "BT /F1 0.5 Tf 100 712 Td (tiny words here) Tj ET",
			gofastcore.HiddenTiny, []string{"tiny words here"}},
		{"an invisible layer over the whole paragraph",
			visibleLines + "BT /F1 12 Tf 3 Tr 72 720 Td 14 TL (Another text entirely, laid over the first one) Tj T* " +
				"(line by line, in the same places and the same size) Tj T* (so that the two would mix.) Tj ET",
			gofastcore.HiddenInvisible, []string{"Another text entirely, laid over the first one line by line, in the same places and the same size so that the two would mix."}},
	} {
		t.Run(c.name, func(t *testing.T) {
			doc := mustExtract(t, onePage(c.content).bytes(), nil)
			part := &doc.Parts[0]
			if got := visibleBlocks(part); got != want {
				t.Errorf("visible blocks:\n%swant:\n%s", got, want)
			}
			var hidden []string
			for i := range part.Blocks {
				if b := &part.Blocks[i]; b.Hidden != 0 {
					if b.Hidden != c.reason {
						t.Errorf("%q is %s, want %s", b.Text, b.Hidden, c.reason)
					}
					hidden = append(hidden, b.Text)
				}
			}
			// How the hidden text is divided into paragraphs is not the
			// point here; that it is all there, apart, is.
			if got, want := strings.Join(hidden, " "), strings.Join(c.hidden, " "); got != want {
				t.Errorf("hidden text %q, want %q", got, want)
			}
		})
	}
}

// TestTextLayer: the searchable text of a scanned page is hidden for
// one reason only, whichever way the file hides it.
func TestTextLayer(t *testing.T) {
	const words = "BT /F1 12 Tf %s 72 720 Td 14 TL (Minutes of the meeting held on the) Tj T* (third of March in the town hall.) Tj ET\n"
	layer := func(state string) string { return strings.Replace(words, "%s", state, 1) }
	const text = "Minutes of the meeting held on the third of March in the town hall."
	variants := []struct{ name, content string }{
		{"text drawn after the page image", pageImage + layer("3 Tr")},
		{"text drawn before the page image", layer("") + pageImage},
		{"a layer smaller than two points", pageImage + strings.Replace(layer("3 Tr"), "/F1 12 Tf", "/F1 1 Tf", 1)},
		{"a layer in optional content that is off, under the image", "/OC /Layer BDC " + layer("3 Tr") + "EMC " + pageImage},
	}
	for _, v := range variants {
		for _, mode := range []gofastcore.ImageMode{gofastcore.ImagesNone, gofastcore.ImagesMeta, gofastcore.ImagesData} {
			t.Run(v.name, func(t *testing.T) {
				d := synthDoc{
					Objects: []string{grayImage, "<< /Type /OCG /Name (Text) >>"},
					Catalog: "/OCProperties << /OCGs [2 0 R] /D << /OFF [2 0 R] >> >>",
					Pages:   []synthPage{{Content: v.content, Resources: "/XObject << /Im1 1 0 R >> /Properties << /Layer 2 0 R >>"}},
				}
				doc := mustExtract(t, d.bytes(), &gofastcore.Options{Images: mode})
				part := &doc.Parts[0]
				n := 0
				for i := range part.Blocks {
					b := &part.Blocks[i]
					if b.Kind != gofastcore.KindParagraph {
						continue
					}
					n++
					if b.Hidden != gofastcore.HiddenTextLayer {
						t.Errorf("reasons %q, want text_layer alone", b.Hidden)
					}
					if n == 1 && b.Text != text && !strings.HasPrefix(v.name, "a layer smaller") {
						t.Errorf("text %q", b.Text)
					}
				}
				if n == 0 {
					t.Fatalf("no text block:\n%s", describe(part))
				}
				if w, ok := warning(part.Warnings, gofastcore.WarnTextHidden); !ok || w.Severity != gofastcore.SeverityInfo {
					t.Errorf("text.hidden: %+v, present %v; want it at info", w, ok)
				}
				if part.Has(gofastcore.WarnPartImageOnly) || part.Has(gofastcore.WarnPartNoText) {
					t.Errorf("warnings %v: a page whose text is a text layer has text", codes(part.Warnings))
				}
				// A caller who leaves out everything hidden but text
				// layers keeps the scan's text.
				var sb strings.Builder
				doc.WriteText(&sb, &gofastcore.RenderOptions{ExcludeHidden: gofastcore.HiddenAny &^ gofastcore.HiddenTextLayer})
				if !strings.Contains(sb.String(), "meeting") {
					t.Errorf("text without the hidden reasons other than text_layer: %q", sb.String())
				}
			})
		}
	}

	// An image that covers less than half of the page does not make
	// unpainted text a text layer.
	t.Run("an image of less than half the page", func(t *testing.T) {
		d := synthDoc{
			Objects: []string{grayImage},
			Pages:   []synthPage{{Content: "q 612 0 0 380 0 412 cm /Im1 Do Q\n" + layer("3 Tr"), Resources: "/XObject << /Im1 1 0 R >>"}},
		}
		part := &mustExtract(t, d.bytes(), nil).Parts[0]
		if got := texts(part); len(got) != 1 || got[0] != "invisible: "+text {
			t.Errorf("blocks %q", got)
		}
		if w, _ := warning(part.Warnings, gofastcore.WarnTextHidden); w.Severity != gofastcore.SeverityWarning {
			t.Errorf("text.hidden at %s", w.Severity)
		}
	})
}

// TestHiddenChecked: every document says which reasons were checked
// completely, and a request for the deep checks is answered.
func TestHiddenChecked(t *testing.T) {
	docs := map[string][]byte{
		"no hidden text":     onePage(visibleLines).bytes(),
		"hidden text":        hiddenCases[0].doc(true).bytes(),
		"a partial check":    hiddenCases[11].doc(true).bytes(), // occluded: found, and still not listed
		"a blank page":       onePage("").bytes(),
		"three pages":        synthDoc{Pages: []synthPage{{Content: visibleLines}, {Content: ""}, {Content: visibleLines}}}.bytes(),
		"an unreadable page": damagedMiddlePage(),
	}
	for name, data := range docs {
		for _, opts := range []gofastcore.Options{{}, {Workers: 4}, {DeepHidden: true}, {Parts: gofastcore.PartRange{First: 1, Last: 1}}} {
			doc := mustExtract(t, data, &opts) // checkContract compares HiddenChecked and the warning
			if doc.HiddenChecked != defaultChecked {
				t.Errorf("%s, %+v: hidden_checked %s", name, opts, doc.HiddenChecked)
			}
			_, deep := warning(doc.Warnings, "pdf.deep_hidden_unavailable")
			if deep != opts.DeepHidden {
				t.Errorf("%s, DeepHidden %v: pdf.deep_hidden_unavailable present: %v", name, opts.DeepHidden, deep)
			}
		}
	}
}

// damagedMiddlePage is a document of three pages whose second page is
// not a page at all.
func damagedMiddlePage() []byte {
	b := pdftest.Doc(pdftest.DocOptions{}, pdftest.PageSpec{Content: visibleLines}, pdftest.PageSpec{Content: visibleLines}, pdftest.PageSpec{Content: visibleLines})
	// Objects: 1 catalog, 2 page tree, 3 font, then content and page for
	// each page. The second page is object 7.
	b.Set(7, "(not a page)")
	return b.Bytes()
}
