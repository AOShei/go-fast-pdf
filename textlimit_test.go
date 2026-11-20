package gofastpdf_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"

	gofastcore "github.com/AOShei/go-fast-core"
	gofastpdf "github.com/AOShei/go-fast-pdf"
	"github.com/AOShei/go-fast-pdf/internal/pdftest"
)

// The limit on the text of a document (Limits.MaxTextBytes) and the
// budgets that a document shares with the documents embedded in it.

// cost is what every block takes from the budget beyond its text.
const cost = gofastcore.BlockCost

// A page of threeParagraphs takes pageCost from the budget.
const pageCost = 85 + 3*cost

// Three paragraphs far apart: three blocks of 28, 29 and 28 bytes.
const threeParagraphs = "BT /F1 12 Tf 72 700 Td (First paragraph of the page.) Tj ET\n" +
	"BT /F1 12 Tf 72 500 Td (Second paragraph of the page.) Tj ET\n" +
	"BT /F1 12 Tf 72 300 Td (Third paragraph of the page.) Tj ET\n"

func pagesOf(n int, content string) []synthPage {
	pages := make([]synthPage, n)
	for i := range pages {
		pages[i].Content = content
	}
	return pages
}

// textBytes is what a document counts against the limit: the titles of
// its outline, the names of its parts, and for each block its text and
// what a block costs.
func textBytes(doc *gofastcore.Document) int {
	n := 0
	for _, o := range doc.Outline {
		n += len(o.Title)
	}
	for i := range doc.Parts {
		n += len(doc.Parts[i].Name)
		for j := range doc.Parts[i].Blocks {
			n += cost + len(doc.Parts[i].Blocks[j].Text)
		}
	}
	return n
}

func limitWarning(ws []gofastcore.Warning) bool {
	w, ok := warning(ws, gofastcore.WarnLimitExceeded)
	return ok && w.Severity == gofastcore.SeverityError
}

func TestTextLimitCutsThePartThatReachesIt(t *testing.T) {
	data := synthDoc{Font: fontCourier, Pages: pagesOf(5, threeParagraphs)}.bytes()
	// The first page whole, and the first block of the second.
	const limit = pageCost + 28 + cost + 10
	var first *gofastcore.Document
	for _, workers := range []int{1, 2, 8} {
		opts := &gofastcore.Options{Workers: workers, Limits: gofastcore.Limits{MaxTextBytes: limit}}
		doc := mustExtract(t, data, opts)
		if first == nil {
			first = doc
		} else if diff := sameDocument(first, doc); diff != "" {
			t.Fatalf("%d workers: not the document of one worker: %s", workers, diff)
		}
		if len(doc.Parts) != 2 || len(doc.Parts[0].Blocks) != 3 || len(doc.Parts[1].Blocks) != 1 {
			t.Fatalf("%d workers: %d parts: %s", workers, len(doc.Parts), doc.Text())
		}
		if limitWarning(doc.Parts[0].Warnings) || !limitWarning(doc.Parts[1].Warnings) || !limitWarning(doc.Warnings) {
			t.Errorf("%d workers: warnings of the parts %v and %v, of the document %v", workers, doc.Parts[0].Warnings, doc.Parts[1].Warnings, doc.Warnings)
		}
		if n := textBytes(doc); n > limit {
			t.Errorf("%d workers: %d bytes of text with a limit of %d", workers, n, limit)
		}

		// The handle gives the same parts, and then says why it ends.
		f, err := gofastpdf.OpenBytes(data, opts)
		if err != nil {
			t.Fatal(err)
		}
		var parts []gofastcore.Part
		var last error
		for part, err := range f.Parts(context.Background()) {
			if err != nil {
				last = err
				break
			}
			parts = append(parts, part)
		}
		if !reflect.DeepEqual(parts, doc.Parts) || !errors.Is(last, gofastcore.ErrLimitExceeded) {
			t.Errorf("%d workers: the handle gives %d parts and ends with %v", workers, len(parts), last)
		}
		f.Close()
	}

	t.Run("a limit that nothing reaches", func(t *testing.T) {
		for _, limit := range []int64{5 * pageCost, 0, -1} {
			doc := mustExtract(t, data, &gofastcore.Options{Limits: gofastcore.Limits{MaxTextBytes: limit}})
			if len(doc.Parts) != 5 || limitWarning(doc.Warnings) || textBytes(doc) != 5*pageCost {
				t.Errorf("limit %d: %d parts, %d bytes, %v", limit, len(doc.Parts), textBytes(doc), doc.Warnings)
			}
		}
	})

	t.Run("a page read by itself", func(t *testing.T) {
		f, err := gofastpdf.OpenBytes(data, &gofastcore.Options{Limits: gofastcore.Limits{MaxTextBytes: 30 + cost}})
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		for _, n := range []int{4, 1} {
			page, _ := f.Page(n)
			part, err := page.Part()
			if err != nil || len(part.Blocks) != 1 || !limitWarning(part.Warnings) {
				t.Errorf("page %d: %d blocks, %v, %v", n, len(part.Blocks), part.Warnings, err)
			}
		}
	})
}

// Every entry of an outline can name one string of the file as its
// title. The titles are counted before any page, and where they are more
// than the limit the outline ends there and nothing of the pages is
// read.
func TestTextLimitAndTheOutline(t *testing.T) {
	const n, titleBytes = 1000, 1000
	objects := []string{fmt.Sprintf("<< /Type /Outlines /First 2 0 R /Last %d 0 R >>", n+1)}
	for i := 0; i < n; i++ {
		next := ""
		if i < n-1 {
			next = fmt.Sprintf("/Next %d 0 R", i+3)
		}
		objects = append(objects, fmt.Sprintf("<< /Title %d 0 R /Parent 1 0 R %s >>", n+2, next))
	}
	objects = append(objects, "("+strings.Repeat("t", titleBytes)+")")
	data := synthDoc{Font: fontCourier, Objects: objects, Catalog: "/Outlines 1 0 R", Pages: pagesOf(3, threeParagraphs)}.bytes()

	whole := mustExtract(t, data, nil)
	if len(whole.Outline) != n || len(whole.Parts) != 3 || limitWarning(whole.Warnings) {
		t.Fatalf("without a limit that is reached: %d entries, %d parts, %v", len(whole.Outline), len(whole.Parts), whole.Warnings)
	}

	t.Run("the outline is more than the limit", func(t *testing.T) {
		opts := &gofastcore.Options{Limits: gofastcore.Limits{MaxTextBytes: 100*titleBytes + 500}}
		doc := mustExtract(t, data, opts)
		// A document has a part. The first page is the one that meets
		// the limit: it is there, cut before its first block.
		if len(doc.Outline) != 100 || len(doc.Parts) != 1 || len(doc.Parts[0].Blocks) != 0 || !limitWarning(doc.Parts[0].Warnings) || !limitWarning(doc.Warnings) {
			t.Errorf("%d entries, %d parts, %v", len(doc.Outline), len(doc.Parts), doc.Warnings)
		}
		f, err := gofastpdf.OpenBytes(data, opts)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if len(f.Outline()) != 100 || !limitWarning(f.Warnings()) {
			t.Errorf("the handle: %d entries, %v", len(f.Outline()), f.Warnings())
		}
		parts := 0
		var last error
		for _, err := range f.Parts(context.Background()) {
			if err != nil {
				last = err
				break
			}
			parts++
		}
		if parts != 1 || !errors.Is(last, gofastcore.ErrLimitExceeded) {
			t.Errorf("the handle gives %d parts and ends with %v", parts, last)
		}
	})

	t.Run("the outline leaves room for one page", func(t *testing.T) {
		doc := mustExtract(t, data, &gofastcore.Options{Workers: 4, Limits: gofastcore.Limits{MaxTextBytes: n*titleBytes + pageCost + 30 + cost}})
		if len(doc.Outline) != n || len(doc.Parts) != 2 || len(doc.Parts[1].Blocks) != 1 || !limitWarning(doc.Parts[1].Warnings) {
			t.Errorf("%d entries, %d parts: %s", len(doc.Outline), len(doc.Parts), doc.Text())
		}
	})
}

// A page label can put one long prefix before the number of every page.
// It is the name of the part, and is counted with it.
func TestTextLimitAndPageLabels(t *testing.T) {
	prefix := strings.Repeat("p", 1000)
	data := synthDoc{Font: fontCourier, Catalog: "/PageLabels << /Nums [0 << /P (" + prefix + ") /S /D >>] >>", Pages: pagesOf(20, threeParagraphs)}.bytes()
	const limit = 4*(1001+pageCost) + 500 // four pages with their names; the fifth has no room for its name
	var first *gofastcore.Document
	for _, workers := range []int{1, 8} {
		doc := mustExtract(t, data, &gofastcore.Options{Workers: workers, Limits: gofastcore.Limits{MaxTextBytes: limit}})
		if first == nil {
			first = doc
		} else if diff := sameDocument(first, doc); diff != "" {
			t.Fatalf("%d workers: %s", workers, diff)
		}
		if len(doc.Parts) != 5 || doc.Parts[3].Name != prefix+"4" || len(doc.Parts[3].Blocks) != 3 {
			t.Fatalf("%d parts; the fourth is named %.20q and has %d blocks", len(doc.Parts), doc.Parts[3].Name, len(doc.Parts[3].Blocks))
		}
		if cut := doc.Parts[4]; cut.Name != "" || len(cut.Blocks) != 0 || !limitWarning(cut.Warnings) || cut.Number != 5 {
			t.Errorf("the part that was cut: name %.20q, %d blocks, %v", cut.Name, len(cut.Blocks), cut.Warnings)
		}
		if n := textBytes(doc); n > limit {
			t.Errorf("%d bytes of text with a limit of %d", n, limit)
		}
	}
}

// budgetedChild is a child extractor as a dispatcher of the go-fast
// libraries would be: PDF goes to this library, and a text file is read
// by a small extractor that counts against the budgets it is given, as
// every extractor of the series does.
func budgetedChild(calls *[]string, decode int64) func(context.Context, string, string, io.ReaderAt, int64, *gofastcore.Options) (*gofastcore.Document, error) {
	return func(ctx context.Context, name, mime string, r io.ReaderAt, size int64, opts *gofastcore.Options) (*gofastcore.Document, error) {
		*calls = append(*calls, fmt.Sprintf("%s text %d decode %d", name, gofastcore.TextBudgetFrom(ctx).Left(), gofastcore.DecodeBudgetFrom(ctx).Left()))
		if strings.HasSuffix(name, ".pdf") {
			return gofastpdf.Extract(ctx, r, size, opts)
		}
		_, dec := gofastcore.WithDecodeBudget(ctx, opts.Limits, 1<<30)
		defer dec.Close()
		dec.Take(decode)
		_, text := gofastcore.WithTextBudget(ctx, opts.Limits)
		defer text.Close()
		buf := make([]byte, size)
		if _, err := r.ReadAt(buf, 0); err != nil && err != io.EOF {
			return nil, err
		}
		d := gofastcore.New("txt")
		part := gofastcore.Part{Kind: gofastcore.PartBody, Number: 1}
		if text.Take(cost + len(buf)) {
			part.Blocks = []gofastcore.Block{{Kind: gofastcore.KindParagraph, Text: strings.TrimSpace(string(buf))}}
		} else {
			part.Warn(gofastcore.SeverityError, gofastcore.WarnLimitExceeded, "cut")
		}
		part.Finish()
		d.Parts = append(d.Parts, part)
		return d, nil
	}
}

// The text of a document and of the documents attached to it is counted
// together: what the pages used is not there for the attached files, and
// what one attached file used is not there for the next.
func TestTextLimitIsSharedWithAttachedFiles(t *testing.T) {
	data := attachmentDoc().bytes()
	plain := mustExtract(t, data, nil)
	own := textBytes(plain) // the two pages of the document itself
	total := func(doc *gofastcore.Document) int {
		n := textBytes(doc)
		for _, c := range doc.Children {
			n += textBytes(c)
		}
		return n
	}

	t.Run("room for all", func(t *testing.T) {
		var calls []string
		doc := mustExtract(t, data, &gofastcore.Options{Child: budgetedChild(&calls, 0), Limits: gofastcore.Limits{MaxTextBytes: int64(own) + 1000}})
		// Each is one block: inner.pdf has 30 bytes of text, notes.txt 11
		// and table.csv 8.
		want := []string{fmt.Sprintf("inner.pdf text %d", 1000), fmt.Sprintf("notes.txt text %d", 1000-30-cost), fmt.Sprintf("table.csv text %d", 1000-30-11-2*cost)}
		for i := range want {
			if i >= len(calls) || !strings.HasPrefix(calls[i], want[i]+" ") {
				t.Fatalf("the child extractor was called with %q, want %q", calls, want)
			}
		}
		if total(doc) != own+30+11+7+3*cost || limitWarning(doc.Warnings) {
			t.Errorf("%d bytes of text, %v", total(doc), doc.Warnings)
		}
	})

	t.Run("the first attached file takes what is left", func(t *testing.T) {
		var calls []string
		limit := own + 10
		doc := mustExtract(t, data, &gofastcore.Options{Child: budgetedChild(&calls, 0), Limits: gofastcore.Limits{MaxTextBytes: int64(limit)}})
		if len(doc.Children) != 3 || !limitWarning(doc.Children[0].Warnings) || len(doc.Children[0].Parts) != 1 || len(doc.Children[0].Parts[0].Blocks) != 0 {
			t.Fatalf("inner.pdf: %+v", doc.Children[0])
		}
		if !limitWarning(doc.Children[1].Parts[0].Warnings) {
			t.Errorf("notes.txt was read with no budget left: %q", doc.Children[1].Text())
		}
		if total(doc) > limit {
			t.Errorf("%d bytes of text with a limit of %d", total(doc), limit)
		}
	})

	t.Run("the pages take all of it", func(t *testing.T) {
		var calls []string
		doc := mustExtract(t, data, &gofastcore.Options{Child: budgetedChild(&calls, 0), Limits: gofastcore.Limits{MaxTextBytes: int64(own) - 20}})
		if len(calls) != 0 {
			t.Errorf("the child extractor was called after the limit was reached: %q", calls)
		}
		if len(doc.Children) != 3 || !limitWarning(doc.Warnings) {
			t.Fatalf("%d children, %v", len(doc.Children), doc.Warnings)
		}
		for _, c := range doc.Children {
			if w, ok := warning(c.Warnings, gofastcore.WarnChildNotExtracted); !ok || w.Severity != gofastcore.SeverityWarning || len(c.Parts) != 0 || c.Source.Name == "" {
				t.Errorf("%s: %+v", c.Source.Name, c.Warnings)
			}
		}
		unread := 0
		for _, w := range doc.Warnings {
			if w.Code == gofastcore.WarnLimitExceeded && strings.Contains(w.Message, "attached files were not read") {
				unread++
			}
		}
		if unread != 1 {
			t.Errorf("the document does not say that attached files were not read: %v", doc.Warnings)
		}
	})

	t.Run("as an attached file of another document", func(t *testing.T) {
		// The document that this one is embedded in has room for one
		// page and 100 bytes left.
		pages := synthDoc{Font: fontCourier, Pages: pagesOf(3, threeParagraphs)}.bytes()
		ctx, parent := gofastcore.WithTextBudget(context.Background(), gofastcore.Limits{MaxTextBytes: pageCost + 100})
		doc, err := gofastpdf.Extract(ctx, bytes.NewReader(pages), int64(len(pages)), nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(doc.Parts) != 2 || len(doc.Parts[1].Blocks) != 0 || !limitWarning(doc.Warnings) || textBytes(doc) != pageCost {
			t.Errorf("%d parts, %d bytes, %v", len(doc.Parts), textBytes(doc), doc.Warnings)
		}
		if left := parent.Left(); left > 100 {
			t.Errorf("the document it is embedded in has %d bytes left, after a page was used and 100 were over", left)
		}
	})
}

// The limit on decoded bytes is shared in the same way: what an attached
// file decodes is gone for the document and for the files after it.
func TestDecodeLimitIsSharedWithAttachedFiles(t *testing.T) {
	data := attachmentDoc().bytes()
	left := func(call string) (n int64) {
		fmt.Sscanf(call[strings.LastIndex(call, " ")+1:], "%d", &n)
		return n
	}

	t.Run("each file sees less", func(t *testing.T) {
		var calls []string
		const limit = 1 << 20
		mustExtract(t, data, &gofastcore.Options{Child: budgetedChild(&calls, 1000), Limits: gofastcore.Limits{MaxDecodedBytes: limit}})
		if len(calls) != 3 {
			t.Fatalf("calls %q", calls)
		}
		// Before the first: the pages and the first file's own bytes are
		// gone. inner.pdf decodes a page; notes.txt takes 1,000 bytes and
		// table.csv then has that much less, and its own 8 bytes.
		if a, b, c := left(calls[0]), left(calls[1]), left(calls[2]); !(a < limit && b < a && c == b-1000-8) {
			t.Errorf("left for the three files: %d, %d, %d of %d", a, b, c, limit)
		}
	})

	t.Run("a file that takes all of it", func(t *testing.T) {
		var calls []string
		doc := mustExtract(t, data, &gofastcore.Options{Child: budgetedChild(&calls, 1<<40), Limits: gofastcore.Limits{MaxDecodedBytes: 1 << 20}})
		// inner.pdf is read within the limit; notes.txt takes more than
		// there is; table.csv is listed and not read.
		if len(calls) != 2 || len(doc.Children) != 3 {
			t.Fatalf("calls %q, %d children", calls, len(doc.Children))
		}
		if w, ok := warning(doc.Children[2].Warnings, gofastcore.WarnChildNotExtracted); !ok || w.Severity != gofastcore.SeverityWarning {
			t.Errorf("table.csv: %+v", doc.Children[2].Warnings)
		}
		if !limitWarning(doc.Warnings) {
			t.Errorf("the document does not say that a limit was reached: %v", doc.Warnings)
		}
	})

	t.Run("as an attached file of another document", func(t *testing.T) {
		// The document this one is embedded in has 200 bytes left to
		// decode. A page of 1,000 lines decodes to more.
		var lines []string
		for i := 0; i < 1000; i++ {
			lines = append(lines, fmt.Sprintf("line %d", i))
		}
		pages := pdftest.Doc(pdftest.DocOptions{Compress: true}, pdftest.PageSpec{Content: pdftest.TextPage(lines...)}, pdftest.PageSpec{Content: pdftest.TextPage(lines...)}).Bytes()
		ctx, parent := gofastcore.WithDecodeBudget(context.Background(), gofastcore.Limits{MaxDecodedBytes: 200}, 1<<30)
		doc, err := gofastpdf.Extract(ctx, bytes.NewReader(pages), int64(len(pages)), nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(doc.Parts) != 0 || !limitWarning(doc.Warnings) {
			t.Errorf("%d parts, %v", len(doc.Parts), doc.Warnings)
		}
		if parent.Left() > 0 {
			t.Errorf("the document it is embedded in has %d bytes left of 200", parent.Left())
		}
	})
}

// An attached file can be given a name of any length, and so can each of
// four thousand. A name is held to the length of a name.
func TestAttachedFileNamesAreHeldToALength(t *testing.T) {
	long := strings.Repeat("n", 5000) + ".txt"
	data := synthDoc{
		Objects: []string{
			pdftest.Stream("/Type /EmbeddedFile /Subtype /"+strings.Repeat("s", 3000), "hello world"),
			"<< /Type /Filespec /F (" + long + ") /EF << /F 1 0 R >> >>",
		},
		Catalog: "/Names << /EmbeddedFiles << /Names [(" + long + ") 2 0 R] >> >>",
		Pages:   []synthPage{{Content: visibleLines}},
	}.bytes()
	doc := mustExtract(t, data, nil)
	if len(doc.Children) != 1 || len(doc.Children[0].Source.Name) != 1024 || len(doc.Children[0].Source.MIME) > 1024 {
		t.Errorf("%d children; name of %d bytes, type of %d", len(doc.Children), len(doc.Children[0].Source.Name), len(doc.Children[0].Source.MIME))
	}
}

// A block of no text is not free. A page can draw one image a hundred
// thousand times with a few bytes of content for each; what bounds the
// document that lists them is that each block is counted.
func TestTextLimitCountsBlocksWithoutText(t *testing.T) {
	const perPage = 10000
	pages := make([]synthPage, 6)
	for i := range pages {
		pages[i] = synthPage{Content: strings.Repeat("/I Do ", perPage), Resources: "/XObject << /I 1 0 R >>"}
	}
	data := synthDoc{Font: fontCourier, Objects: []string{grayImage}, Pages: pages}.bytes()
	blocks := func(doc *gofastcore.Document) (n int) {
		for i := range doc.Parts {
			n += len(doc.Parts[i].Blocks)
		}
		return n
	}
	if doc := mustExtract(t, data, nil); blocks(doc) != 6*perPage || limitWarning(doc.Warnings) {
		t.Fatalf("with the default limit: %d blocks, %v", blocks(doc), doc.Warnings)
	}
	// Room for two pages and a half.
	const limit = 25000 * cost
	var first *gofastcore.Document
	for _, workers := range []int{1, 8} {
		doc := mustExtract(t, data, &gofastcore.Options{Workers: workers, Limits: gofastcore.Limits{MaxTextBytes: limit}})
		if first == nil {
			first = doc
		} else if diff := sameDocument(first, doc); diff != "" {
			t.Fatalf("%d workers: %s", workers, diff)
		}
		if len(doc.Parts) != 3 || blocks(doc) != 25000 || !limitWarning(doc.Parts[2].Warnings) || !limitWarning(doc.Warnings) {
			t.Errorf("%d workers: %d parts, %d blocks, %v", workers, len(doc.Parts), blocks(doc), doc.Warnings)
		}
	}
	// With no image blocks asked for there is nothing to count.
	if doc := mustExtract(t, data, &gofastcore.Options{Images: gofastcore.ImagesNone, Limits: gofastcore.Limits{MaxTextBytes: limit}}); len(doc.Parts) != 6 || limitWarning(doc.Warnings) {
		t.Errorf("without image blocks: %d parts, %v", len(doc.Parts), doc.Warnings)
	}
}
