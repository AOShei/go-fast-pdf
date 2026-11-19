package gofastpdf_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"

	gofastcore "github.com/AOShei/go-fast-core"
	gofastpdf "github.com/AOShei/go-fast-pdf"
	"github.com/AOShei/go-fast-pdf/internal/pdftest"
)

// outlineDoc is a document of three pages with bookmarks. With n
// objects before the document, its pages are the objects n+5, n+7 and
// n+9.
func outlineDoc(loop bool) synthDoc {
	next := ""
	if loop {
		next = "/Next 2 0 R" // the list leads back into itself
	}
	return synthDoc{
		Objects: []string{
			"<< /Type /Outlines /First 2 0 R /Last 6 0 R /Count 5 >>",
			"<< /Title (Chapter 1) /Parent 1 0 R /Next 5 0 R /First 3 0 R /Last 4 0 R /Count 2 /Dest [11 0 R /XYZ 0 700 0] >>",
			"<< /Title (Section 1.1) /Parent 2 0 R /Next 4 0 R /Dest [11 0 R /FitH 400] >>",
			"<< /Title (Section 1.2) /Parent 2 0 R /Prev 3 0 R /A << /S /GoTo /D [13 0 R /Fit] >> >>",
			"<< /Title <FEFF004300680061007000740065007200A00032> /Parent 1 0 R /Prev 2 0 R /Next 6 0 R /Dest (second) >>",
			"<< /Title (Appendix) /Parent 1 0 R /Prev 5 0 R /Dest /Old " + next + " >>",
		},
		Catalog: "/Outlines 1 0 R /Names << /Dests << /Names [(second) [15 0 R /XYZ null 500 null]] >> >> /Dests << /Old << /D [15 0 R /FitR 0 0 100 600] >> >>",
		Pages:   []synthPage{{Content: visibleLines}, {Content: visibleLines}, {Content: visibleLines}},
	}
}

func TestOutline(t *testing.T) {
	want := []gofastcore.OutlineItem{
		{Title: "Chapter 1", Level: 1, Part: 1, Y: 92},
		{Title: "Section 1.1", Level: 2, Part: 1, Y: 392},
		{Title: "Section 1.2", Level: 2, Part: 2},
		{Title: "Chapter\u00a02", Level: 1, Part: 3, Y: 292},
		{Title: "Appendix", Level: 1, Part: 3, Y: 192},
	}
	for _, loop := range []bool{false, true} {
		doc := mustExtract(t, outlineDoc(loop).bytes(), nil)
		if !reflect.DeepEqual(doc.Outline, want) {
			t.Errorf("loop %v: outline %+v\nwant %+v", loop, doc.Outline, want)
		}
	}

	t.Run("the outline of a range of pages", func(t *testing.T) {
		// The entries point at their pages whether or not those are
		// among the parts.
		doc := mustExtract(t, outlineDoc(false).bytes(), &gofastcore.Options{Parts: gofastcore.PartRange{First: 2, Last: 2}})
		if !reflect.DeepEqual(doc.Outline, want) {
			t.Errorf("outline %+v", doc.Outline)
		}
	})

	t.Run("on a rotated page", func(t *testing.T) {
		d := outlineDoc(false)
		d.Pages[0].Extra = "/Rotate 90"
		// (0, 700) of a page turned by 90 degrees is at y = 0 from the
		// top of what is displayed; the position along the other axis
		// is what moves.
		got := mustExtract(t, d.bytes(), nil).Outline[0]
		if got.Part != 1 || got.Y != 0 {
			t.Errorf("%+v", got)
		}
	})

	t.Run("damaged outlines", func(t *testing.T) {
		for _, objects := range [][]string{
			{"<< /Type /Outlines /First 1 0 R >>"},                            // its own first entry
			{"<< /Type /Outlines /First 2 0 R >>", "(not an entry)"},          // an entry that is no dictionary
			{"<< /Type /Outlines /First 2 0 R >>", "<< /Title 12 /Dest 7 >>"}, // wrong types
			{"<< /Type /Outlines /First 2 0 R >>", "<< /Title (Deep) /First 2 0 R >>"},
			{"<< /Type /Outlines /First 2 0 R >>", "<< /Title (Gone) /Dest [99 0 R /Fit] /Next 99 0 R >>"},
		} {
			d := synthDoc{Objects: objects, Catalog: "/Outlines 1 0 R", Pages: []synthPage{{Content: visibleLines}}}
			doc := mustExtract(t, d.bytes(), nil) // must not hang, and must keep the contract
			if len(doc.Outline) > 2 {
				t.Errorf("%q: %d entries", objects, len(doc.Outline))
			}
		}
	})

	t.Run("against qpdf", func(t *testing.T) {
		data := outlineDoc(false).bytes()
		path := pdftest.TempFile(t, data)
		out, _, err := pdftest.Run(30*time.Second, "qpdf", "--json", "--json-key=outlines", path)
		if err != nil {
			t.Skipf("qpdf: %v", err)
		}
		var parsed struct {
			Outlines []qpdfOutline `json:"outlines"`
		}
		if err := json.Unmarshal([]byte(out), &parsed); err != nil {
			t.Fatalf("qpdf's JSON: %v", err)
		}
		var theirs []string
		var walk func(list []qpdfOutline, level int)
		walk = func(list []qpdfOutline, level int) {
			for _, o := range list {
				theirs = append(theirs, fmt.Sprintf("%d %s -> %d", level, o.Title, o.Page))
				walk(o.Kids, level+1)
			}
		}
		walk(parsed.Outlines, 1)
		var ours []string
		for _, o := range mustExtract(t, data, nil).Outline {
			ours = append(ours, fmt.Sprintf("%d %s -> %d", o.Level, o.Title, o.Part))
		}
		if !reflect.DeepEqual(ours, theirs) {
			t.Errorf("ours   %q\nqpdf's %q", ours, theirs)
		}
	})
}

type qpdfOutline struct {
	Title string        `json:"title"`
	Page  int           `json:"destpageposfrom1"`
	Kids  []qpdfOutline `json:"kids"`
}

// attachmentDoc has two files in the catalog's name tree and one
// attached to its second page; the file of the name tree is also
// attached to the first page, which must not list it twice.
func attachmentDoc() synthDoc {
	inner := string(onePage("BT /F1 12 Tf 72 720 Td (Text of the attached document.) Tj ET").bytes())
	return synthDoc{
		Objects: []string{
			pdftest.Stream("/Type /EmbeddedFile /Subtype /text#2Fplain /Params << /Size 11 >>", "hello world"),
			"<< /Type /Filespec /F (notes.txt) /UF (notes.txt) /EF << /F 1 0 R >> >>",
			pdftest.Stream(fmt.Sprintf("/Type /EmbeddedFile /Subtype /application#2Fpdf /Params << /Size %d >>", len(inner)), inner),
			"<< /Type /Filespec /F (inner.pdf) /EF << /F 3 0 R >> >>",
			pdftest.Stream("/Type /EmbeddedFile", "a,b\n1,2\n"),
			"<< /Type /Filespec /F (table.csv) /EF << /F 5 0 R >> >>",
			"<< /Type /Filespec /F (http://example.com/elsewhere.pdf) /FS /URL >>", // not in the document
		},
		Catalog: "/Names << /EmbeddedFiles << /Names [(inner.pdf) 4 0 R (notes.txt) 2 0 R (elsewhere) 7 0 R] >> >>",
		Pages: []synthPage{
			{Content: visibleLines, Extra: "/Annots [<< /Type /Annot /Subtype /FileAttachment /Rect [10 10 30 30] /FS 2 0 R >>]"},
			{Content: visibleLines, Extra: "/Annots [<< /Type /Annot /Subtype /FileAttachment /Rect [10 10 30 30] /FS 6 0 R >>]"},
		},
	}
}

func TestAttachments(t *testing.T) {
	data := attachmentDoc().bytes()
	names := func(doc *gofastcore.Document) []string {
		var out []string
		for _, c := range doc.Children {
			out = append(out, fmt.Sprintf("%s %s %d", c.Source.Name, c.Source.MIME, c.Source.Size))
		}
		return out
	}

	t.Run("listed without a child extractor", func(t *testing.T) {
		doc := mustExtract(t, data, nil)
		want := []string{"inner.pdf application/pdf " + fmt.Sprint(doc.Children[0].Source.Size), "notes.txt text/plain 11", "table.csv  0"}
		if got := names(doc); !reflect.DeepEqual(got, want) {
			t.Errorf("children %q, want %q", got, want)
		}
		for _, c := range doc.Children {
			w, ok := warning(c.Warnings, gofastcore.WarnChildNotExtracted)
			if !ok || w.Severity != gofastcore.SeverityInfo || len(c.Parts) != 0 || c.Source.Format != "" {
				t.Errorf("%s: warnings %+v, %d parts", c.Source.Name, c.Warnings, len(c.Parts))
			}
		}
	})

	// A child extractor that knows PDF and plain text, as a dispatcher
	// would be.
	var seen []string
	child := func(ctx context.Context, name, mime string, r io.ReaderAt, size int64, opts *gofastcore.Options) (*gofastcore.Document, error) {
		seen = append(seen, fmt.Sprintf("%s depth %d password %q", name, opts.Depth, opts.Password))
		switch {
		case strings.HasSuffix(name, ".pdf"):
			return gofastpdf.Extract(ctx, r, size, opts)
		case strings.HasSuffix(name, ".txt"):
			buf := make([]byte, size)
			if _, err := r.ReadAt(buf, 0); err != nil && err != io.EOF {
				return nil, err
			}
			d := gofastcore.New("txt")
			d.HiddenChecked = gofastcore.HiddenChars
			part := gofastcore.Part{Kind: gofastcore.PartBody, Number: 1, Blocks: []gofastcore.Block{{Kind: gofastcore.KindParagraph, Text: string(buf)}}}
			part.MarkHidden()
			d.Parts = append(d.Parts, part)
			return d, nil
		}
		return nil, fmt.Errorf("%w: %s", gofastcore.ErrUnsupported, name)
	}

	t.Run("extracted through a child extractor", func(t *testing.T) {
		seen = nil
		doc, err := gofastpdf.Extract(context.Background(), strings.NewReader(string(data)), int64(len(data)), &gofastcore.Options{Child: child, Password: "pw"})
		if err != nil {
			t.Fatal(err)
		}
		if err := doc.Validate(); err != nil {
			t.Error(err)
		}
		if want := []string{`inner.pdf depth 1 password "pw"`, `notes.txt depth 1 password "pw"`, `table.csv depth 1 password "pw"`}; !reflect.DeepEqual(seen, want) {
			t.Errorf("the child extractor saw %q", seen)
		}
		if got := doc.Children[0]; got.Source.Format != "pdf" || got.Source.Name != "inner.pdf" || got.Parts[0].Text() != "Text of the attached document.\n" {
			t.Errorf("inner.pdf: %+v, %q", got.Source, got.Text())
		}
		if got := doc.Children[1]; got.Source.Format != "txt" || got.Text() != "hello world\n" || got.Source.MIME != "text/plain" {
			t.Errorf("notes.txt: %+v, %q", got.Source, got.Text())
		}
		if w, ok := warning(doc.Children[2].Warnings, gofastcore.WarnChildUnsupported); !ok || w.Severity != gofastcore.SeverityWarning {
			t.Errorf("table.csv: %+v", doc.Children[2].Warnings)
		}
	})

	t.Run("what the child extractor returns", func(t *testing.T) {
		for _, c := range []struct {
			err      error
			code     string
			severity gofastcore.Severity
		}{
			{gofastcore.ErrUnsupported, gofastcore.WarnChildUnsupported, gofastcore.SeverityWarning},
			{gofastcore.ErrPasswordRequired, gofastcore.WarnChildEncrypted, gofastcore.SeverityError},
			{gofastcore.ErrWrongPassword, gofastcore.WarnChildEncrypted, gofastcore.SeverityError},
			{errors.New("broken"), gofastcore.WarnChildFailed, gofastcore.SeverityError},
		} {
			failing := func(context.Context, string, string, io.ReaderAt, int64, *gofastcore.Options) (*gofastcore.Document, error) {
				return nil, c.err
			}
			doc := mustExtract(t, data, &gofastcore.Options{Child: failing})
			for _, ch := range doc.Children {
				if w, ok := warning(ch.Warnings, c.code); !ok || w.Severity != c.severity || len(ch.Parts) != 0 {
					t.Errorf("%v: %s has %+v", c.err, ch.Source.Name, ch.Warnings)
				}
			}
		}
	})

	t.Run("the depth limit", func(t *testing.T) {
		called := false
		never := func(context.Context, string, string, io.ReaderAt, int64, *gofastcore.Options) (*gofastcore.Document, error) {
			called = true
			return nil, errors.New("called")
		}
		doc := mustExtract(t, data, &gofastcore.Options{Child: never, Depth: 2, Limits: gofastcore.Limits{MaxDepth: 2}})
		if called {
			t.Error("the child extractor was called at the depth limit")
		}
		for _, ch := range doc.Children {
			if w, ok := warning(ch.Warnings, gofastcore.WarnChildNotExtracted); !ok || w.Severity != gofastcore.SeverityWarning {
				t.Errorf("%s: %+v", ch.Source.Name, ch.Warnings)
			}
		}
	})

	t.Run("a document inside a document inside a document", func(t *testing.T) {
		// Each level attaches the level below it.
		level := onePage("BT /F1 12 Tf 72 720 Td (Level 3) Tj ET").bytes()
		for n := 2; n >= 1; n-- {
			level = synthDoc{
				Objects: []string{
					pdftest.Stream("/Type /EmbeddedFile", string(level)),
					fmt.Sprintf("<< /Type /Filespec /F (level%d.pdf) /EF << /F 1 0 R >> >>", n+1),
				},
				Catalog: fmt.Sprintf("/Names << /EmbeddedFiles << /Names [(level%d.pdf) 2 0 R] >> >>", n+1),
				Pages:   []synthPage{{Content: fmt.Sprintf("BT /F1 12 Tf 72 720 Td (Level %d) Tj ET", n)}},
			}.bytes()
		}
		doc := mustExtract(t, level, &gofastcore.Options{Child: child})
		var got []string
		doc.Walk(func(d *gofastcore.Document, depth int) bool {
			got = append(got, fmt.Sprintf("%d %s %q", depth, d.Source.Name, strings.TrimSpace(d.Text())))
			return true
		})
		if want := []string{`0  "Level 1"`, `1 level2.pdf "Level 2"`, `2 level3.pdf "Level 3"`}; !reflect.DeepEqual(got, want) {
			t.Errorf("%q", got)
		}
	})

	t.Run("an attached file larger than the limit", func(t *testing.T) {
		doc, err := gofastpdf.Extract(context.Background(), strings.NewReader(string(data)), int64(len(data)),
			&gofastcore.Options{Child: child, Limits: gofastcore.Limits{MaxDecodedBytes: 400}})
		if err != nil {
			t.Fatal(err)
		}
		if w, ok := warning(doc.Warnings, gofastcore.WarnLimitExceeded); !ok || w.Severity != gofastcore.SeverityError {
			t.Errorf("document warnings %+v", doc.Warnings)
		}
		if w, ok := warning(doc.Children[0].Warnings, gofastcore.WarnChildFailed); !ok || w.Severity != gofastcore.SeverityError {
			t.Errorf("inner.pdf: %+v", doc.Children[0].Warnings)
		}
	})
}

// TestActiveContentAndExternalReferences: a document says when it holds
// something meant to run, or points outside itself other than by a
// link. Nothing is run or fetched, and no script is read.
func TestActiveContentAndExternalReferences(t *testing.T) {
	annot := func(action string) synthDoc {
		return synthDoc{Pages: []synthPage{{Content: visibleLines,
			Extra: "/Annots [<< /Type /Annot /Subtype /Link /Rect [72 700 200 730] /A " + action + " >>]"}}}
	}
	for _, c := range []struct {
		name             string
		doc              synthDoc
		active, external bool
	}{
		{"an ordinary document", onePage(visibleLines), false, false},
		{"a link to an address", annot("<< /S /URI /URI (http://example.com/) >>"), false, false},
		{"a link inside the document", annot("<< /S /GoTo /D [5 0 R /Fit] >>"), false, false},
		{"JavaScript in the name tree", synthDoc{Catalog: "/Names << /JavaScript << /Names [(init) << /S /JavaScript /JS (app.alert\\(1\\)) >>] >> >>", Pages: []synthPage{{Content: visibleLines}}}, true, false},
		{"an empty JavaScript name tree", synthDoc{Catalog: "/Names << /JavaScript << /Names [] >> >>", Pages: []synthPage{{Content: visibleLines}}}, false, false},
		{"JavaScript when the document opens", synthDoc{Catalog: "/OpenAction << /S /JavaScript /JS (x) >>", Pages: []synthPage{{Content: visibleLines}}}, true, false},
		{"a destination when the document opens", synthDoc{Catalog: "/OpenAction [5 0 R /Fit]", Pages: []synthPage{{Content: visibleLines}}}, false, false},
		{"JavaScript when the document closes", synthDoc{Catalog: "/AA << /WC << /S /JavaScript /JS (x) >> >>", Pages: []synthPage{{Content: visibleLines}}}, true, false},
		{"JavaScript when a page opens", synthDoc{Pages: []synthPage{{Content: visibleLines, Extra: "/AA << /O << /S /JavaScript /JS (x) >> >>"}}}, true, false},
		{"JavaScript on a link", annot("<< /S /JavaScript /JS (x) >>"), true, false},
		{"JavaScript after a link's action", annot("<< /S /URI /URI (http://example.com/) /Next << /S /JavaScript /JS (x) >> >>"), true, false},
		{"JavaScript in a list of next actions", annot("<< /S /URI /URI (http://example.com/) /Next [<< /S /Named /N /NextPage >> << /S /JavaScript /JS (x) >>] >>"), true, false},
		{"JavaScript on a form field", synthDoc{
			Objects: []string{"<< /FT /Tx /T (total) /AA << /C << /S /JavaScript /JS (x) >> >> >>"},
			Catalog: "/AcroForm << /Fields [1 0 R] >>", Pages: []synthPage{{Content: visibleLines}}}, true, false},
		{"JavaScript on a field inside a field", synthDoc{
			Objects: []string{"<< /T (group) /Kids [2 0 R] >>", "<< /FT /Tx /T (inner) /Parent 1 0 R /A << /S /JavaScript /JS (x) >> >>"},
			Catalog: "/AcroForm << /Fields [1 0 R] >>", Pages: []synthPage{{Content: visibleLines}}}, true, false},
		{"a program started by a link", annot("<< /S /Launch /F (calc.exe) >>"), true, true},
		{"a program started by a link, Windows form", annot("<< /S /Launch /Win << /F (cmd.exe) /P (/c calc) >> >>"), true, true},
		{"a launch that names nothing", annot("<< /S /Launch >>"), true, false},
		{"a link into another file", annot("<< /S /GoToR /F (other.pdf) /D [0 /Fit] >>"), false, true},
		{"a form sent to an address", annot("<< /S /SubmitForm /F (http://example.com/collect) >>"), false, true},
		{"data loaded from a file", annot("<< /S /ImportData /F (data.fdf) >>"), false, true},
		{"an image whose data is in another file", synthDoc{
			Objects: []string{pdftest.Stream("/Type /XObject /Subtype /Image /Width 1 /Height 1 /ColorSpace /DeviceGray /BitsPerComponent 8 /F (picture.jpg)", "")},
			Pages:   []synthPage{{Content: visibleLines + "q 10 0 0 10 0 0 cm /Im1 Do Q", Resources: "/XObject << /Im1 1 0 R >>"}}}, false, true},
		{"a page of another document shown in this one", synthDoc{
			Objects: []string{pdftest.Stream("/Type /XObject /Subtype /Form /BBox [0 0 100 100] /Ref << /F (other.pdf) /Page 0 >>", "")},
			Pages:   []synthPage{{Content: visibleLines + "/Fm1 Do", Resources: "/XObject << /Fm1 1 0 R >>"}}}, false, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			for _, workers := range []int{1, 4} {
				doc := mustExtract(t, c.doc.bytes(), &gofastcore.Options{Workers: workers})
				a, active := warning(doc.Warnings, gofastcore.WarnInputActiveContent)
				e, external := warning(doc.Warnings, gofastcore.WarnInputExternalReference)
				if active != c.active || external != c.external {
					t.Errorf("input.active_content: %v, input.external_reference: %v; want %v and %v", active, external, c.active, c.external)
				}
				if active && a.Severity != gofastcore.SeverityInfo || external && e.Severity != gofastcore.SeverityWarning {
					t.Errorf("severities %s and %s", a.Severity, e.Severity)
				}
				if got := doc.Parts[0].Text(); got != visibleText+"\n" {
					t.Errorf("text %q", got)
				}
			}
		})
	}

	t.Run("on a page that is not extracted", func(t *testing.T) {
		// What is on a page that was not asked for is not looked at.
		d := annot("<< /S /JavaScript /JS (x) >>")
		d.Pages = append(d.Pages, synthPage{Content: visibleLines})
		doc := mustExtract(t, d.bytes(), &gofastcore.Options{Parts: gofastcore.PartRange{First: 2}})
		if _, ok := warning(doc.Warnings, gofastcore.WarnInputActiveContent); ok {
			t.Error("a script on page 1 was reported for an extraction of page 2")
		}
	})
}

// TestRepairedInput: a damaged file that was read all the same says so.
func TestRepairedInput(t *testing.T) {
	data := onePage(visibleLines).bytes()
	cut := data[:strings.LastIndex(string(data), "xref")] // no cross-reference table, no trailer
	doc := mustExtract(t, cut, nil)
	if w, ok := warning(doc.Warnings, gofastcore.WarnInputRepaired); !ok || w.Severity != gofastcore.SeverityInfo {
		t.Errorf("warnings %+v", doc.Warnings)
	}
	if doc.Parts[0].Text() != visibleText+"\n" {
		t.Errorf("text %q", doc.Parts[0].Text())
	}
	if _, ok := warning(mustExtract(t, data, nil).Warnings, gofastcore.WarnInputRepaired); ok {
		t.Error("input.repaired on a sound file")
	}
}

// TestAmbiguousInput: a file without usable cross-reference data that
// defines an object twice can be read in two ways. The last definition
// is read, and the document says that there was a choice.
func TestAmbiguousInput(t *testing.T) {
	data := onePage("BT /F1 12 Tf 72 720 Td (The first version of the page.) Tj ET").bytes()
	body := string(data[:strings.Index(string(data), "\nxref\n")+1]) // the objects, without table and trailer
	// The content stream of the page is object 4. A second definition
	// follows the first, as a later revision of the file would add it.
	again := "4 0 obj\n" + pdftest.Stream("", "BT /F1 12 Tf 72 720 Td (The second version of the page.) Tj ET") + "\nendobj\n"
	doc := mustExtract(t, []byte(body+again), nil)
	if w, ok := warning(doc.Warnings, gofastcore.WarnInputAmbiguous); !ok || w.Severity != gofastcore.SeverityWarning {
		t.Errorf("warnings %+v", doc.Warnings)
	}
	if got := doc.Parts[0].Text(); got != "The second version of the page.\n" {
		t.Errorf("text %q", got)
	}
	// Without a second definition there is no choice, and a sound file
	// that redefines an object says in its cross-reference data which
	// definition counts.
	if _, ok := warning(mustExtract(t, []byte(body), nil).Warnings, gofastcore.WarnInputAmbiguous); ok {
		t.Error("input.ambiguous on a file that defines every object once")
	}
	b := pdftest.Doc(pdftest.DocOptions{}, pdftest.PageSpec{Content: "BT /F1 12 Tf 72 720 Td (First) Tj ET"})
	b.Update()
	b.Set(4, pdftest.Stream("", "BT /F1 12 Tf 72 720 Td (Second) Tj ET"))
	doc = mustExtract(t, b.Bytes(), nil)
	if _, ok := warning(doc.Warnings, gofastcore.WarnInputAmbiguous); ok || doc.Parts[0].Text() != "Second\n" {
		t.Errorf("a file saved in two steps: warnings %+v, text %q", doc.Warnings, doc.Parts[0].Text())
	}
}
