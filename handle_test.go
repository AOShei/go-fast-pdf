package gofastpdf_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	gofastcore "github.com/AOShei/go-fast-core"
	gofastpdf "github.com/AOShei/go-fast-pdf"
	"github.com/AOShei/go-fast-pdf/internal/pdftest"
)

// collect reads the parts of an open file through its iterator.
func collect(t testing.TB, f *gofastpdf.File) []gofastcore.Part {
	t.Helper()
	var parts []gofastcore.Part
	for part, err := range f.Parts(context.Background()) {
		if err != nil {
			t.Fatalf("Parts: %v", err)
		}
		parts = append(parts, part)
	}
	return parts
}

// equivalenceDocs are documents that the three ways of reading a file
// are compared on: the fixture corpus, and synthetic ones with what
// the corpus lacks.
func equivalenceDocs(t *testing.T) map[string][]byte {
	docs := map[string][]byte{}
	paths, _ := filepath.Glob("testdata/fixtures/*.pdf")
	for _, path := range paths {
		if strings.Contains(path, "userpw") || strings.Contains(path, "unicodepw") {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		docs[filepath.Base(path)] = data
	}
	if len(docs) < 30 {
		t.Fatalf("%d fixtures", len(docs))
	}
	docs["a page that cannot be read"] = damagedMiddlePage()
	docs["a blank page"] = synthDoc{Pages: []synthPage{{Content: visibleLines}, {Content: ""}, {Content: visibleLines}}}.bytes()
	var hidden []synthPage
	for _, c := range deepCases {
		if len(c.objects) == 0 && c.catalog == "" && c.extra == "" {
			hidden = append(hidden, synthPage{Content: c.before + visibleLines + c.after, Resources: c.resources, Fonts: c.fonts})
		}
	}
	docs["hidden text"] = synthDoc{Pages: hidden}.bytes()
	docs["images"] = synthDoc{
		Objects: []string{grayImage},
		Pages: []synthPage{
			{Content: "q 100 0 0 100 72 500 cm /Im1 Do Q BI /W 1 /H 1 /CS /G /BPC 8 ID x\nEI " + visibleLines, Resources: "/XObject << /Im1 1 0 R >>"},
			{Content: visibleLines + "BI /W 1 /H 1 /CS /G /BPC 8 ID x\nEI BI /W 1 /H 1 /CS /G /BPC 8 ID y\nEI"},
			{Content: "q 100 0 0 100 72 300 cm /Im1 Do Q", Resources: "/XObject << /Im1 1 0 R >>"},
		},
	}.bytes()
	docs["page labels"] = synthDoc{
		Catalog: "/PageLabels << /Nums [0 << /S /r >> 2 << /S /D /St 1 >>] >>",
		Pages:   []synthPage{{Content: visibleLines}, {Content: visibleLines}, {Content: visibleLines}},
	}.bytes()
	return docs
}

// TestThreeWaysToTheSameParts: Extract, the Parts iterator and
// Page(n).Part() give the same parts, and a document put together from
// what the latter two hand out keeps the contract.
func TestThreeWaysToTheSameParts(t *testing.T) {
	ctx := context.Background()
	for name, data := range equivalenceDocs(t) {
		for _, opts := range []gofastcore.Options{
			{}, {Workers: 1}, {Images: gofastcore.ImagesMeta, Workers: 3}, {DeepHidden: true},
			{Parts: gofastcore.PartRange{First: 2}, Workers: 2},
		} {
			f, err := gofastpdf.OpenBytes(data, &opts)
			if errors.Is(err, gofastcore.ErrNoPartsSelected) {
				continue // a file of one page has no second page
			}
			if err != nil {
				t.Fatalf("%s: OpenBytes: %v", name, err)
			}
			doc, err := gofastpdf.Extract(ctx, bytes.NewReader(data), int64(len(data)), &opts)
			if err != nil {
				t.Fatalf("%s: Extract: %v", name, err)
			}
			where := fmt.Sprintf("%s, %+v", name, opts)

			iterated := collect(t, f)
			if !reflect.DeepEqual(iterated, doc.Parts) {
				t.Errorf("%s: the iterator's parts are not those of Extract", where)
			}
			var single []gofastcore.Part
			for i := range doc.Parts {
				page, err := f.Page(doc.Parts[i].Number)
				if err != nil {
					t.Fatalf("%s: Page(%d): %v", where, doc.Parts[i].Number, err)
				}
				if page.Number() != doc.Parts[i].Number {
					t.Errorf("%s: page %d says it is page %d", where, doc.Parts[i].Number, page.Number())
				}
				part, err := page.Part()
				if err != nil {
					t.Fatalf("%s: Part: %v", where, err)
				}
				single = append(single, part)
			}
			if !reflect.DeepEqual(single, doc.Parts) {
				for i := range single {
					if !reflect.DeepEqual(single[i], doc.Parts[i]) {
						t.Errorf("%s: page %d read by itself is not the part of Extract:\n%s\nand\n%s", where, single[i].Number, describe(&single[i]), describe(&doc.Parts[i]))
						break
					}
				}
			}

			// What the handle says of the file is what the document says.
			if got := f.HiddenChecked(); got != doc.HiddenChecked {
				t.Errorf("%s: HiddenChecked %s, the document's %s", where, got, doc.HiddenChecked)
			}
			if !reflect.DeepEqual(f.Metadata(), doc.Metadata) || fmt.Sprint(f.Outline()) != fmt.Sprint(doc.Outline) {
				t.Errorf("%s: metadata or outline differ from the document's", where)
			}
			if f.NumPages() < len(doc.Parts) {
				t.Errorf("%s: %d pages and %d parts", where, f.NumPages(), len(doc.Parts))
			}

			// A document assembled by a caller from the parts handed out.
			for _, parts := range [][]gofastcore.Part{iterated, single} {
				own := gofastcore.New(gofastpdf.Format)
				own.Parts, own.HiddenChecked = parts, f.HiddenChecked()
				own.Metadata, own.Outline, own.Warnings = f.Metadata(), f.Outline(), f.Warnings()
				if err := own.Validate(); err != nil {
					t.Errorf("%s: a document made of the parts handed out: %v", where, err)
				}
			}

			// Document on the handle is Extract.
			again, err := f.Document(ctx)
			if err != nil {
				t.Fatalf("%s: Document: %v", where, err)
			}
			if diff := sameDocument(doc, again); diff != "" {
				t.Errorf("%s: Document and Extract: %s", where, diff)
			}
			if err := f.Close(); err != nil {
				t.Errorf("%s: Close: %v", where, err)
			}
		}
	}
}

// TestWarningsOfTheHandle: what the document says of the file as a
// whole, the handle says once the pages have been read.
func TestWarningsOfTheHandle(t *testing.T) {
	data := synthDoc{
		Catalog: "/OpenAction << /S /JavaScript /JS (app.alert(1)) >>",
		Pages: []synthPage{{Content: visibleLines}, {Content: visibleLines,
			Extra: "/Annots [<< /Type /Annot /Subtype /Link /Rect [0 0 10 10] /A << /S /GoToR /F (other.pdf) /D [0 /Fit] >> >>]"}},
	}.bytes()
	doc := mustExtract(t, data, nil)
	if got := codes(doc.Warnings); len(got) != 2 || got[0] != gofastcore.WarnInputActiveContent || got[1] != gofastcore.WarnInputExternalReference {
		t.Fatalf("the document's warnings: %v", got)
	}
	f, err := gofastpdf.OpenBytes(data, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Before a page is read: what the catalog says.
	if got := codes(f.Warnings()); len(got) != 1 || got[0] != gofastcore.WarnInputActiveContent {
		t.Errorf("before reading: %v", got)
	}
	collect(t, f)
	if !reflect.DeepEqual(f.Warnings(), doc.Warnings) {
		t.Errorf("after reading: %v, the document's %v", f.Warnings(), doc.Warnings)
	}
}

// TestImageDataOfOnePage: Document and Parts give the data of an image
// once for the document. A page read by itself gives it on that page.
func TestImageDataOfOnePage(t *testing.T) {
	jpeg := pdftest.Stream("/Type /XObject /Subtype /Image /Width 1 /Height 1 /ColorSpace /DeviceGray /BitsPerComponent 8 /Filter /DCTDecode", "\xff\xd8\xff\xd9")
	show := synthPage{Content: "q 100 0 0 100 72 500 cm /Im1 Do Q q 50 0 0 50 300 500 cm /Im1 Do Q", Resources: "/XObject << /Im1 1 0 R >>"}
	data := synthDoc{Objects: []string{jpeg}, Pages: []synthPage{show, show}}.bytes()
	opts := &gofastcore.Options{Images: gofastcore.ImagesData}
	withData := func(p *gofastcore.Part) (n int) {
		for _, b := range p.Blocks {
			if b.Kind == gofastcore.KindImage && len(b.Image.Data) > 0 {
				n++
			}
		}
		return n
	}
	doc := mustExtract(t, data, opts)
	if a, b := withData(&doc.Parts[0]), withData(&doc.Parts[1]); a != 1 || b != 0 {
		t.Errorf("Extract: %d blocks with data on page 1 and %d on page 2", a, b)
	}
	f, err := gofastpdf.OpenBytes(data, opts)
	if err != nil {
		t.Fatal(err)
	}
	if parts := collect(t, f); !reflect.DeepEqual(parts, doc.Parts) {
		t.Error("the iterator's parts are not those of Extract")
	}
	page, _ := f.Page(2)
	part, err := page.Part()
	if err != nil || withData(&part) != 1 {
		t.Errorf("page 2 by itself: %d blocks with data (%v)", withData(&part), err)
	}
	// It is the same part otherwise.
	for i := range part.Blocks {
		if part.Blocks[i].Image != nil {
			part.Blocks[i].Image.Data = nil
		}
	}
	if !reflect.DeepEqual(part, doc.Parts[1]) {
		t.Errorf("page 2 by itself:\n%s\nfrom Extract:\n%s", describe(&part), describe(&doc.Parts[1]))
	}
}

// TestHiddenCheckedWhileIterating: what was checked is final only when
// every part has been produced. Here the last page paints more shapes
// than are followed.
func TestHiddenCheckedWhileIterating(t *testing.T) {
	last := string(pageContent(t, manyShapes(100, "", "")))
	data := synthDoc{Pages: []synthPage{{Content: visibleLines}, {Content: visibleLines}, {Content: last}}}.bytes()
	deep := &gofastcore.Options{DeepHidden: true, Workers: 1}
	all := defaultChecked | deepChecked
	cut := defaultChecked | gofastcore.HiddenClipped

	f, err := gofastpdf.BoundedExtractor(50, 0).OpenBytes(data, deep)
	if err != nil {
		t.Fatal(err)
	}
	if got := f.HiddenChecked(); got != all {
		t.Errorf("before reading: %s", got)
	}
	n := 0
	for part, err := range f.Parts(context.Background()) {
		if err != nil {
			t.Fatal(err)
		}
		n++
		incomplete := part.Has(gofastpdf.WarnDeepHiddenIncomplete)
		want := all
		if incomplete {
			want = cut
		}
		if incomplete != (n == 3) || f.HiddenChecked() != want {
			t.Errorf("at page %d: incomplete %v, checked %s", n, incomplete, f.HiddenChecked())
		}
	}
	if got := f.HiddenChecked(); n != 3 || got != cut {
		t.Errorf("after %d parts: %s", n, got)
	}

	// The same through one page at a time, and without DeepHidden.
	f, _ = gofastpdf.BoundedExtractor(50, 0).OpenBytes(data, deep)
	for n := 1; n <= 3; n++ {
		page, _ := f.Page(n)
		if _, err := page.Part(); err != nil {
			t.Fatal(err)
		}
		if want := map[bool]gofastcore.Hidden{false: all, true: cut}[n == 3]; f.HiddenChecked() != want {
			t.Errorf("after page %d by itself: %s", n, f.HiddenChecked())
		}
	}
	f, _ = gofastpdf.BoundedExtractor(50, 0).OpenBytes(data, nil)
	collect(t, f)
	if got := f.HiddenChecked(); got != defaultChecked {
		t.Errorf("without DeepHidden: %s", got)
	}
}

// TestPartsForAnyNumberOfWorkers: the iterator hands out the same
// parts in the same order however many pages are read at a time.
func TestPartsForAnyNumberOfWorkers(t *testing.T) {
	var pages []synthPage
	for i := range 60 {
		content := fmt.Sprintf("BT /F1 12 Tf 72 720 Td (This is page %d of the document.) Tj ET\n", i+1)
		if i%7 == 3 {
			content += "BT /F1 12 Tf 3 Tr 72 500 Td (hidden words) Tj ET\n"
		}
		if i%11 == 5 {
			content = "" // a blank page
		}
		pages = append(pages, synthPage{Content: content + strings.Repeat("0 0 5 5 re f\n", i*20)})
	}
	data := synthDoc{Pages: pages}.bytes()
	var first []gofastcore.Part
	for _, workers := range []int{1, 2, 8, 64} {
		for _, deep := range []bool{false, true} {
			f, err := gofastpdf.OpenBytes(data, &gofastcore.Options{Workers: workers, DeepHidden: deep})
			if err != nil {
				t.Fatal(err)
			}
			parts := collect(t, f)
			if len(parts) != 60 {
				t.Fatalf("%d workers: %d parts", workers, len(parts))
			}
			for i := range parts {
				if parts[i].Number != i+1 {
					t.Fatalf("%d workers: part %d is page %d", workers, i, parts[i].Number)
				}
			}
			if first == nil {
				first = parts
			}
			// The deep checks find nothing more on these pages.
			if !reflect.DeepEqual(parts, first) {
				t.Errorf("%d workers, DeepHidden %v: the parts differ from those of one worker", workers, deep)
			}
		}
	}
}

// TestStoppingEarly: a loop over the parts that is left early stops the
// reading and leaves nothing running.
func TestStoppingEarly(t *testing.T) {
	data := pdftest.Doc(pdftest.DocOptions{}, manyPages(400)...).Bytes()
	before := runtime.NumGoroutine()
	for _, workers := range []int{1, 4, 16} {
		f, err := gofastpdf.OpenBytes(data, &gofastcore.Options{Workers: workers})
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for _, err := range f.Parts(context.Background()) {
			if err != nil {
				t.Fatal(err)
			}
			if n++; n == 5 {
				break
			}
		}
		// And again from the start: the handle is not used up.
		if parts := collect(t, f); len(parts) != 400 {
			t.Errorf("%d workers: %d parts on the second pass", workers, len(parts))
		}
	}
	settled(t, before)
}

// manyPages returns n pages of a few lines each.
func manyPages(n int) []pdftest.PageSpec {
	pages := make([]pdftest.PageSpec, n)
	for i := range pages {
		pages[i].Content = pdftest.TextPage(fmt.Sprintf("Page %d begins here.", i+1), "It has a second line,", "and a third.")
	}
	return pages
}

// settled waits for the goroutines of a call to end and fails if more
// are left than there were before it.
func settled(t *testing.T, before int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for runtime.NumGoroutine() > before && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if now := runtime.NumGoroutine(); now > before {
		buf := make([]byte, 1<<16)
		t.Errorf("%d goroutines before and %d after:\n%s", before, now, buf[:runtime.Stack(buf, true)])
	}
}

// slowPage is a document of one page that takes long to read: a form of
// a hundred thousand operators, drawn ten thousand times.
func slowPage() []byte {
	form := pdftest.Stream("/Type /XObject /Subtype /Form /BBox [0 0 612 792]", strings.Repeat("0 0 m 1 1 l S\n", 100000))
	return synthDoc{
		Objects: []string{form},
		Pages:   []synthPage{{Content: visibleLines + strings.Repeat("/Slow Do\n", 10000), Resources: "/XObject << /Slow 1 0 R >>"}},
	}.bytes()
}

// TestCancellation: a context that ends stops the reading promptly,
// between pages and inside a page that is long, and nothing is left
// running.
func TestCancellation(t *testing.T) {
	before := runtime.NumGoroutine()

	// Before anything is read.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	data := pdftest.Doc(pdftest.DocOptions{}, manyPages(300)...).Bytes()
	if _, err := gofastpdf.Extract(ctx, bytes.NewReader(data), int64(len(data)), nil); !errors.Is(err, context.Canceled) {
		t.Errorf("Extract with a context that has ended: %v", err)
	}
	if _, err := gofastpdf.ExtractFile(ctx, "testdata/fixtures/english.pdf", nil); !errors.Is(err, context.Canceled) {
		t.Errorf("ExtractFile with a context that has ended: %v", err)
	}

	// Between pages: the loop gets the error and ends.
	for _, workers := range []int{1, 8} {
		ctx, cancel := context.WithCancel(context.Background())
		f, err := gofastpdf.OpenBytes(data, &gofastcore.Options{Workers: workers})
		if err != nil {
			t.Fatal(err)
		}
		n, failed := 0, error(nil)
		for part, err := range f.Parts(ctx) {
			if err != nil {
				failed = err
				if part.Number != 0 || len(part.Blocks) != 0 {
					t.Errorf("the part that comes with an error is not empty: %+v", part)
				}
				continue
			}
			if failed != nil {
				t.Error("a part after the error")
			}
			if n++; n == 10 {
				cancel()
			}
		}
		if !errors.Is(failed, context.Canceled) || n < 10 || n > 10+3*workers {
			t.Errorf("%d workers: %d parts, then %v", workers, n, failed)
		}
		cancel()
	}

	// Inside one long page.
	slow := slowPage()
	for name, read := range map[string]func(ctx context.Context) error{
		"Extract": func(ctx context.Context) error {
			_, err := gofastpdf.Extract(ctx, bytes.NewReader(slow), int64(len(slow)), nil)
			return err
		},
		"Parts": func(ctx context.Context) error {
			f, err := gofastpdf.OpenBytes(slow, &gofastcore.Options{Workers: 4})
			if err != nil {
				return err
			}
			for _, err := range f.Parts(ctx) {
				if err != nil {
					return err
				}
			}
			return nil
		},
		"Document with DeepHidden": func(ctx context.Context) error {
			f, err := gofastpdf.OpenBytes(slow, &gofastcore.Options{DeepHidden: true})
			if err != nil {
				return err
			}
			_, err = f.Document(ctx)
			return err
		},
	} {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
		start := time.Now()
		err := read(ctx)
		took := time.Since(start)
		cancel()
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("%s: %v after %v; the page was to take far longer than its deadline", name, err, took)
		}
		if took > 3*time.Second {
			t.Errorf("%s: returned %v after its deadline of 30ms", name, took)
		}
	}
	settled(t, before)
}

// TestClosed: a closed file answers with ErrClosed, and what was handed
// out before stays as it is.
func TestClosed(t *testing.T) {
	f, err := gofastpdf.Open("testdata/fixtures/english.pdf", nil)
	if err != nil {
		t.Fatal(err)
	}
	page, _ := f.Page(1)
	part, _ := page.Part()
	text := part.Text()
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Errorf("closing twice: %v", err)
	}
	if _, err := f.Page(1); !errors.Is(err, gofastpdf.ErrClosed) {
		t.Errorf("Page: %v", err)
	}
	if _, err := page.Part(); !errors.Is(err, gofastpdf.ErrClosed) {
		t.Errorf("Part: %v", err)
	}
	if _, err := page.Lines(); !errors.Is(err, gofastpdf.ErrClosed) {
		t.Errorf("Lines: %v", err)
	}
	if _, err := f.Document(context.Background()); !errors.Is(err, gofastpdf.ErrClosed) {
		t.Errorf("Document: %v", err)
	}
	n := 0
	for _, err := range f.Parts(context.Background()) {
		if n++; !errors.Is(err, gofastpdf.ErrClosed) {
			t.Errorf("Parts: %v", err)
		}
	}
	if n != 1 || part.Text() != text {
		t.Errorf("%d turns of the loop; the part changed: %v", n, part.Text() != text)
	}
}

// TestOpening: the errors of opening, and the pages that can be asked
// for.
func TestOpening(t *testing.T) {
	if _, err := gofastpdf.Open("testdata/fixtures/no-such-file.pdf", nil); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a file that is not there: %v", err)
	}
	if _, err := gofastpdf.OpenBytes([]byte("not a PDF"), nil); !errors.Is(err, gofastcore.ErrMalformed) {
		t.Errorf("not a PDF: %v", err)
	}
	if _, err := gofastpdf.Open("testdata/fixtures/enc-aes-256-userpw.pdf", nil); !errors.Is(err, gofastcore.ErrPasswordRequired) {
		t.Errorf("a file that needs a password: %v", err)
	}
	if _, err := gofastpdf.Open("testdata/fixtures/enc-aes-256-userpw.pdf", &gofastcore.Options{Password: "wrong"}); !errors.Is(err, gofastcore.ErrWrongPassword) {
		t.Errorf("a wrong password: %v", err)
	}
	data, _ := os.ReadFile("testdata/fixtures/english.pdf")
	if _, err := gofastpdf.OpenBytes(data, &gofastcore.Options{Limits: gofastcore.Limits{MaxInputBytes: 100}}); !errors.Is(err, gofastcore.ErrLimitExceeded) {
		t.Errorf("a file over the input limit: %v", err)
	}
	if _, err := gofastpdf.OpenReaderAt(bytes.NewReader(data), -1, nil); !errors.Is(err, gofastcore.ErrMalformed) {
		t.Errorf("a negative size: %v", err)
	}
	if _, err := gofastpdf.OpenReaderAt(bytes.NewReader(data), int64(len(data))+10, nil); err == nil {
		t.Error("a size beyond the end of the input: no error")
	}
	if _, err := gofastpdf.OpenBytes(data, &gofastcore.Options{Parts: gofastcore.PartRange{First: 9}}); !errors.Is(err, gofastcore.ErrNoPartsSelected) {
		t.Errorf("a range the file does not have: %v", err)
	}

	// The range of the options is what Parts and Document read; any page
	// can be asked for by its number.
	f, err := gofastpdf.Open("testdata/fixtures/english.pdf", &gofastcore.Options{Parts: gofastcore.PartRange{First: 2, Last: 2}})
	if err != nil {
		t.Fatal(err)
	}
	if parts := collect(t, f); len(parts) != 1 || parts[0].Number != 2 || f.NumPages() != 3 {
		t.Errorf("%d parts of %d pages", len(parts), f.NumPages())
	}
	for n, want := range map[int]bool{0: false, 1: true, 3: true, 4: false, -1: false} {
		page, err := f.Page(n)
		if (err == nil) != want || want && page.Number() != n || !want && !errors.Is(err, gofastcore.ErrNoPartsSelected) {
			t.Errorf("Page(%d): %v", n, err)
		}
	}

	// The name of the source: the file's, unless the options give one.
	doc, err := gofastpdf.ExtractFile(context.Background(), "testdata/fixtures/english.pdf", nil)
	if err != nil || doc.Source.Name != "english.pdf" || doc.Source.Size != int64(len(data)) {
		t.Errorf("ExtractFile: source %+v, %v", doc.Source, err)
	}
	doc, _ = gofastpdf.ExtractFile(context.Background(), "testdata/fixtures/english.pdf", &gofastcore.Options{Name: "report"})
	if doc.Source.Name != "report" {
		t.Errorf("ExtractFile with a name: %q", doc.Source.Name)
	}
}

// TestLimitsOfTheIterator: where Document cuts the document and adds
// limit.exceeded, the iterator ends with ErrLimitExceeded after the
// parts that were within the limit.
func TestLimitsOfTheIterator(t *testing.T) {
	data := pdftest.Doc(pdftest.DocOptions{}, manyPages(10)...).Bytes()
	for name, limits := range map[string]gofastcore.Limits{
		"MaxParts":        {MaxParts: 4},
		"MaxDecodedBytes": {MaxDecodedBytes: 300},
	} {
		opts := &gofastcore.Options{Limits: limits, Workers: 3}
		doc := mustExtract(t, data, opts)
		if _, ok := warning(doc.Warnings, gofastcore.WarnLimitExceeded); !ok || len(doc.Parts) == 0 || len(doc.Parts) == 10 {
			t.Fatalf("%s: Extract gives %d parts and warnings %v", name, len(doc.Parts), codes(doc.Warnings))
		}
		f, err := gofastpdf.OpenBytes(data, opts)
		if err != nil {
			t.Fatal(err)
		}
		var parts []gofastcore.Part
		var failed error
		for part, err := range f.Parts(context.Background()) {
			if err != nil {
				failed = err
				continue
			}
			parts = append(parts, part)
		}
		if !errors.Is(failed, gofastcore.ErrLimitExceeded) || !reflect.DeepEqual(parts, doc.Parts) {
			t.Errorf("%s: %d parts and %v; Extract gives %d parts", name, len(parts), failed, len(doc.Parts))
		}
	}
}

// TestExtractorSettings: the settings that only PDF has.
func TestExtractorSettings(t *testing.T) {
	ctx := context.Background()
	big := synthDoc{Pages: []synthPage{{Content: visibleLines + strings.Repeat("0 0 m 1 1 l S\n", 20000)}}}.bytes()
	read := func(e *gofastpdf.Extractor, data []byte, opts *gofastcore.Options) *gofastcore.Document {
		t.Helper()
		if opts == nil {
			opts = &gofastcore.Options{}
		}
		doc, err := e.Extract(ctx, bytes.NewReader(data), int64(len(data)), opts)
		if err != nil {
			t.Fatal(err)
		}
		if err := checkContract(doc, opts); err != nil {
			t.Fatal(err)
		}
		return doc
	}
	// A stream larger than MaxStreamBytes is cut there, and says so.
	if doc := read(&gofastpdf.Extractor{}, big, nil); doc.Parts[0].Has(gofastcore.WarnLimitExceeded) {
		t.Errorf("by default: %v", codes(doc.Parts[0].Warnings))
	}
	doc := read(&gofastpdf.Extractor{MaxStreamBytes: 1000}, big, nil)
	if !doc.Parts[0].Has(gofastcore.WarnLimitExceeded) || !strings.Contains(doc.Text(), "committee") {
		t.Errorf("with a stream limit of 1000 bytes: %v, %.40q", codes(doc.Parts[0].Warnings), doc.Text())
	}
	// A cache of object streams that holds nothing still reads the file.
	objstm, err := os.ReadFile("testdata/fixtures/english-objstm.pdf")
	if err != nil {
		t.Fatal(err)
	}
	if a, b := read(&gofastpdf.Extractor{}, objstm, nil), read(&gofastpdf.Extractor{ObjectStreamCacheBytes: 1}, objstm, nil); sameDocument(a, b) != "" {
		t.Error("the document differs with a cache of one byte")
	}
	// Nothing kept of parsed objects at all: every object is parsed
	// again each time it is wanted, by eight workers at once, and the
	// document is the same.
	for _, name := range []string{"english", "multilingual-objstm-linearized", "attached", "layout-cols2-firefox", "enc-aes-256", "para-el-libreoffice"} {
		data, err := os.ReadFile(filepath.Join("testdata", "fixtures", name+".pdf"))
		if err != nil {
			t.Fatal(err)
		}
		opts := &gofastcore.Options{Workers: 8, Images: gofastcore.ImagesMeta, DeepHidden: true}
		if a, b := read(&gofastpdf.Extractor{}, data, opts), read(&gofastpdf.Extractor{ObjectCacheBytes: 1}, data, opts); sameDocument(a, b) != "" {
			t.Errorf("%s: the document differs when no parsed object is kept: %s", name, sameDocument(a, b))
		}
	}
	// MaxShapes is the bound of the deep checks.
	shapes := manyShapes(100, "", "")
	deep := &gofastcore.Options{DeepHidden: true}
	if doc := read(&gofastpdf.Extractor{MaxShapes: 50}, shapes, deep); !doc.Parts[0].Has(gofastpdf.WarnDeepHiddenIncomplete) {
		t.Error("MaxShapes 50: the page of 100 shapes is complete")
	}
	if doc := read(&gofastpdf.Extractor{MaxShapes: 500}, shapes, deep); doc.Parts[0].Has(gofastpdf.WarnDeepHiddenIncomplete) {
		t.Error("MaxShapes 500: the page of 100 shapes is incomplete")
	}
}
