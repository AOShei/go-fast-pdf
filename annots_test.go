package gofastpdf_test

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	gofastcore "github.com/AOShei/go-fast-core"
	gofastpdf "github.com/AOShei/go-fast-pdf"
	"github.com/AOShei/go-fast-pdf/internal/pdftest"
)

// What a page says outside its content: the appearances of its
// annotations, what is written in them, and the values of its form
// fields.

// appearance is a form that shows one line of text, as the appearance
// of an annotation. font is the object number of the document's font.
func appearance(font int, text string) string {
	return pdftest.Stream(fmt.Sprintf("/Type /XObject /Subtype /Form /BBox [0 0 200 20] /Resources << /Font << /F1 %d 0 R >> >>", font),
		"BT /F1 12 Tf 2 5 Td ("+text+") Tj ET")
}

// formDoc is a page with labels in its content and, as annotations, a
// form and some notes. The objects before the document are numbered
// from 1 and the font is object 9.
func formDoc() synthDoc {
	const font = 9
	labels := "BT /F1 12 Tf 72 705 Td (Name:) Tj ET\nBT /F1 12 Tf 72 665 Td (City:) Tj ET\n" +
		"BT /F1 12 Tf 72 625 Td (Colours:) Tj ET\nBT /F1 12 Tf 72 300 Td (The text at the foot of the page.) Tj ET\n"
	annots := []string{
		// Notes.
		"<< /Type /Annot /Subtype /Text /Rect [500 700 520 720] /Contents (Check this figure) /T (A. Reviewer) /M (D:20260102030405+01'00') >>",
		"<< /Type /Annot /Subtype /Text /Rect [500 660 520 680] /F 2 /Contents (A remark that is hidden) >>",
		"<< /Type /Annot /Subtype /Highlight /Rect [-900 -900 -880 -880] /Contents (A remark off the page) >>",
		"<< /Type /Annot /Subtype /Popup /Rect [500 600 600 650] /Contents (The window of a note) >>",
		"<< /Type /Annot /Subtype /FreeText /Rect [300 280 500 300] /Contents (Written on the page) /AP << /N 2 0 R >> >>",
		"<< /Type /Annot /Subtype /FreeText /Rect [300 240 500 260] /Contents (Written without an appearance) >>",
		// A text field that shows its value, and one that has no appearance.
		"<< /Type /Annot /Subtype /Widget /FT /Tx /T (name) /V (Jane Doe) /Rect [150 700 350 720] /AP << /N 1 0 R >> >>",
		"<< /Type /Annot /Subtype /Widget /FT /Tx /T (city) /Parent 3 0 R /V (Paris) /Rect [150 660 350 680] >>",
		// A list with two options chosen, one of which is stored under
		// another text than is displayed.
		"<< /Type /Annot /Subtype /Widget /FT /Ch /T (colours) /V [(r) (blue)] /Opt [[(r) (red)] (blue) [(g) (green)]] /Rect [150 620 350 640] >>",
		// Check boxes: on, and off.
		"<< /Type /Annot /Subtype /Widget /FT /Btn /T (agree) /V /Yes /AS /Yes /Rect [150 580 160 590] >>",
		"<< /Type /Annot /Subtype /Widget /FT /Btn /T (newsletter) /V /Off /AS /Off /Rect [150 560 160 570] >>",
		// Two radio buttons of one field: the second is the one chosen.
		"<< /Type /Annot /Subtype /Widget /Parent 4 0 R /AS /Off /Rect [150 540 160 550] >>",
		"<< /Type /Annot /Subtype /Widget /Parent 4 0 R /AS /large /Rect [170 540 180 550] >>",
		// No value to read: a push button, and a password.
		"<< /Type /Annot /Subtype /Widget /FT /Btn /Ff 65536 /T (send) /V /Yes /Rect [150 500 200 520] >>",
		"<< /Type /Annot /Subtype /Widget /FT /Tx /Ff 8192 /T (password) /V (secret) /Rect [150 470 350 490] >>",
		// Hidden: a field with a value and no appearance, and one that
		// would show its value.
		"<< /Type /Annot /Subtype /Widget /FT /Tx /T (internal) /V (Not for the reader) /F 2 /Rect [150 440 350 460] >>",
		"<< /Type /Annot /Subtype /Widget /FT /Tx /T (shown) /V (Unseen value) /F 32 /Rect [150 410 350 430] /AP << /N 5 0 R >> >>",
	}
	return synthDoc{
		Font: fontCourier,
		Objects: []string{
			appearance(font, "Jane Doe"),
			appearance(font, "Written on the page"),
			"<< /T (address) >>",
			"<< /FT /Btn /Ff 32768 /T (size) /V /large >>",
			appearance(font, "Unseen value"),
			grayImage,
		},
		Pages: []synthPage{{Content: "q 40 0 0 40 72 100 cm /Im1 Do Q\n" + labels, Resources: "/XObject << /Im1 6 0 R >>", Extra: "/Annots [" + strings.Join(annots, "\n") + "]"}},
	}
}

func TestAnnotationsAndFormFields(t *testing.T) {
	data := formDoc().bytes()
	doc := mustExtract(t, data, nil)
	part := &doc.Parts[0]
	var got []string
	for _, b := range part.Blocks {
		line := fmt.Sprintf("%s %s %q", b.Kind, b.Origin, b.Text)
		if b.Hidden != 0 {
			line += " hidden:" + b.Hidden.String()
		}
		for _, key := range []string{gofastcore.AttrField, gofastcore.AttrNoteType, gofastcore.AttrAuthor, gofastcore.AttrDate} {
			if v, ok := b.Attrs[key]; ok {
				line += fmt.Sprintf(" %s=%s", key, v)
			}
		}
		got = append(got, line)
	}
	want := []string{
		// The page as it is shown, in reading order: the labels, what the
		// appearances of the annotations display, and after its label
		// the value of each field that displays nothing.
		`paragraph inferred "Name:"`,
		`paragraph inferred "Jane Doe"`,
		`paragraph inferred "Unseen value" hidden:invisible`,
		`paragraph inferred "City:"`,
		`paragraph native "Paris" field=address.city`,
		`paragraph inferred "Colours:"`,
		`paragraph native "red\nblue" field=colours`,
		`paragraph native "Yes" field=agree`,
		`paragraph native "large" field=size`,
		`paragraph native "Not for the reader" hidden:invisible field=internal`,
		`paragraph inferred "The text at the foot of the page."`,
		`image native ""`,
		`paragraph inferred "Written on the page"`,
		// What is written in the notes, in the order of the file.
		`note native "Check this figure" type=comment author=A. Reviewer date=2026-01-02T03:04:05+01:00`,
		`note native "A remark that is hidden" hidden:invisible type=comment`,
		`note native "A remark off the page" hidden:off_page type=comment`,
		`note native "Written without an appearance" type=comment`,
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("blocks:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	text := doc.Text()
	for _, never := range []string{"secret", "The window of a note", "send"} {
		if strings.Contains(text, never) {
			t.Errorf("%q is in the text", never)
		}
	}
	if !part.Has(gofastcore.WarnTextHidden) {
		t.Errorf("no warning for the hidden text: %v", part.Warnings)
	}

	t.Run("hidden text can be left out", func(t *testing.T) {
		var sb strings.Builder
		doc.WriteText(&sb, &gofastcore.RenderOptions{ExcludeHidden: gofastcore.HiddenAny})
		for _, hidden := range []string{"Not for the reader", "Unseen value", "A remark that is hidden", "A remark off the page"} {
			if strings.Contains(sb.String(), hidden) {
				t.Errorf("%q is in the text without hidden text", hidden)
			}
		}
		if !strings.Contains(sb.String(), "Paris") || !strings.Contains(sb.String(), "Check this figure") {
			t.Errorf("the text without hidden text: %q", sb.String())
		}
	})

	t.Run("the image and the lines still belong to their blocks", func(t *testing.T) {
		with := mustExtract(t, data, &gofastcore.Options{Images: gofastcore.ImagesData})
		for _, b := range with.Parts[0].Blocks {
			if b.Kind == gofastcore.KindImage && (b.Image == nil || b.Image.ID != "obj-6" || len(b.Image.Data) == 0) {
				t.Errorf("the image block: %+v", b.Image)
			}
		}
		f, err := gofastpdf.OpenBytes(data, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		page, _ := f.Page(1)
		lines, err := page.Lines()
		if err != nil {
			t.Fatal(err)
		}
		if diff := linesAgainstPart(lines, part); diff != "" {
			t.Error(diff)
		}
	})

	t.Run("the same for any number of workers and with the deeper checks", func(t *testing.T) {
		pages := formDoc()
		pages.Pages = append(pages.Pages, pages.Pages[0], pages.Pages[0])
		many := pages.bytes()
		one := mustExtract(t, many, &gofastcore.Options{Workers: 1})
		if diff := sameDocument(one, mustExtract(t, many, &gofastcore.Options{Workers: 8})); diff != "" {
			t.Error(diff)
		}
		deep := mustExtract(t, many, &gofastcore.Options{DeepHidden: true})
		if diff := deepAgainstDefault(one, deep); diff != "" {
			t.Error(diff)
		}
	})

	t.Run("against qpdf", func(t *testing.T) {
		// A form as a program writes it: the fields are objects, listed
		// in the catalog and on the page.
		fields := synthDoc{
			Font: fontCourier,
			Objects: []string{
				"<< /T (address) /Kids [2 0 R] >>",
				"<< /Type /Annot /Subtype /Widget /FT /Tx /T (city) /Parent 1 0 R /V (Paris) /Rect [150 660 350 680] >>",
				"<< /Type /Annot /Subtype /Widget /FT /Ch /T (colour) /V (blue) /Opt [(red) (blue)] /Rect [150 620 350 640] >>",
				"<< /Type /Annot /Subtype /Widget /FT /Btn /T (agree) /V /Yes /AS /Yes /Rect [150 580 160 590] >>",
			},
			Catalog: "/AcroForm << /Fields [1 0 R 3 0 R 4 0 R] >>",
			Pages:   []synthPage{{Content: visibleLines, Extra: "/Annots [2 0 R 3 0 R 4 0 R]"}},
		}
		data := fields.bytes()
		out, _, err := pdftest.Run(30*time.Second, "qpdf", "--json", "--json-key=acroform", pdftest.TempFile(t, data))
		if err != nil {
			t.Skipf("qpdf: %v", err)
		}
		var parsed struct {
			Acroform struct {
				Fields []struct {
					Name  string `json:"fullname"`
					Value any    `json:"value"`
				} `json:"fields"`
			} `json:"acroform"`
		}
		if err := json.Unmarshal([]byte(out), &parsed); err != nil {
			t.Fatalf("qpdf's JSON: %v", err)
		}
		theirs := map[string]string{}
		for _, f := range parsed.Acroform.Fields {
			// qpdf writes a string as u:text and a name as /name.
			theirs[f.Name] = strings.TrimPrefix(strings.TrimPrefix(fmt.Sprint(f.Value), "u:"), "/")
		}
		ours := map[string]string{}
		for _, b := range mustExtract(t, data, nil).Parts[0].Blocks {
			if name, ok := b.Attrs[gofastcore.AttrField]; ok {
				ours[name] = b.Text
			}
		}
		if len(ours) != 3 || !reflect.DeepEqual(ours, theirs) {
			t.Errorf("fields here %q, qpdf's %q", ours, theirs)
		}
	})
}

// The appearance of an annotation is fitted into its rectangle: its box,
// as its matrix places it, fills the rectangle. What it shows is where
// the rectangle is, at the size the fitting gives it.
func TestAppearanceIsFittedIntoItsRectangle(t *testing.T) {
	const font = 4
	form := func(box, matrix string) string {
		return pdftest.Stream("/Type /XObject /Subtype /Form /BBox "+box+matrix+fmt.Sprintf(" /Resources << /Font << /F1 %d 0 R >> >>", font),
			"BT /F1 10 Tf 0 0 Td (stamp) Tj ET")
	}
	for name, c := range map[string]struct {
		form string
		size float64 // of the text as displayed
	}{
		"same size":        {form("[0 0 100 20]", ""), 10},
		"twice as large":   {form("[0 0 50 10]", ""), 20},
		"moved box":        {form("[-10 -5 90 15]", ""), 10},
		"through a matrix": {form("[0 0 200 40]", " /Matrix [0.5 0 0 0.5 30 30]"), 10},
	} {
		doc := mustExtract(t, synthDoc{Font: fontCourier, Objects: []string{c.form},
			Pages: []synthPage{{Content: visibleLines, Extra: "/Annots [<< /Type /Annot /Subtype /Stamp /Rect [300 400 400 420] /AP << /N 1 0 R >> >>]"}}}.bytes(), nil)
		var box gofastcore.Rect
		found := false
		for _, b := range doc.Parts[0].Blocks {
			if b.Text == "stamp" {
				box, found = b.Loc.BBox, true
			}
		}
		// The page is 792 high: the rectangle is from 372 to 392 from the top.
		// The text stands on the lower edge of the rectangle, and what of
		// its letters goes under the line goes under the edge.
		if !found || box.X0 < 299 || box.X0 > 311 || box.Y0 < 370 || box.Y1 > 392+0.3*c.size || box.Y1-box.Y0 < 0.5*c.size || box.Y1-box.Y0 > 1.3*c.size {
			t.Errorf("%s: found %v, box %+v", name, found, box)
		}
	}

	// An appearance for each state: the one the annotation is in is shown.
	states := synthDoc{Font: fontCourier, Objects: []string{appearance(font, "on"), appearance(font, "off")},
		Pages: []synthPage{{Content: visibleLines, Extra: "/Annots [<< /Type /Annot /Subtype /Widget /FT /Btn /T (box) /V /Yes /AS /Yes /Rect [300 400 500 420] /AP << /N << /Yes 1 0 R /Off 2 0 R >> >> >>]"}}}
	if text := mustExtract(t, states.bytes(), nil).Text(); !strings.Contains(text, "on") || strings.Contains(text, "off") || strings.Contains(text, "Yes") {
		t.Errorf("an appearance with states: %q", text)
	}
}

// Every annotation of a page can name one long string as what is
// written in it. What a page holds of that is bounded, and the page
// says when there was more.
func TestAnnotationTextIsBounded(t *testing.T) {
	if testing.Short() {
		t.Skip("large")
	}
	const n = 4000
	var annots strings.Builder
	for i := 0; i < n; i++ {
		fmt.Fprintf(&annots, "<< /Type /Annot /Subtype /Text /Rect [10 10 20 20] /Contents 1 0 R >>\n")
	}
	data := synthDoc{Font: fontCourier, Objects: []string{"(" + strings.Repeat("x", 10000) + ")"},
		Pages: []synthPage{{Content: visibleLines, Extra: "/Annots [" + annots.String() + "]"}}}.bytes()
	doc := mustExtract(t, data, nil)
	notes, bytes := 0, 0
	for _, b := range doc.Parts[0].Blocks {
		if b.Kind == gofastcore.KindNote {
			notes++
			bytes += len(b.Text)
		}
	}
	// 4,000 notes of 10,000 bytes are 40 MB: more than a page keeps.
	if notes == 0 || notes == n || bytes > 16<<20 || !limitWarning(doc.Parts[0].Warnings) {
		t.Errorf("%d notes of %d, %d bytes, %v", notes, n, bytes, doc.Parts[0].Warnings)
	}
	// And they count against the limit on text like any block.
	small := mustExtract(t, data, &gofastcore.Options{Limits: gofastcore.Limits{MaxTextBytes: 100000}})
	if textBytes(small) > 100000 || !limitWarning(small.Warnings) {
		t.Errorf("with a limit of 100,000: %d bytes, %v", textBytes(small), small.Warnings)
	}
}
