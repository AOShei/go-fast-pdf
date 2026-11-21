package gofastpdf

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"testing"

	gofastcore "github.com/AOShei/go-fast-core"
	"github.com/AOShei/go-fast-pdf/internal/pdftest"
)

// A panic in a page, an image, the catalog or an attached file ends that
// and not the process: the page is a failed part, the image has no data,
// the catalog is not read, the attached file is a failed child; the rest
// of the document is there, and keeps the contract of the model.
func TestRecovers(t *testing.T) {
	t.Cleanup(func() { panicHook = nil })
	b := pdftest.New()
	b.Add(pdftest.Stream("/Type /XObject /Subtype /Image /Width 1 /Height 1 /ColorSpace /DeviceGray /BitsPerComponent 8", "\x80"))
	b.Add("<< /Title (A title) >>")
	b.Trailer = "/Info 2 0 R"
	page := pdftest.PageSpec{
		Content:   "q 100 0 0 100 72 500 cm /Im Do Q BT /F1 12 Tf 72 720 Td (a picture) Tj ET",
		Resources: "/Font << /F1 5 0 R >> /XObject << /Im 1 0 R >>",
	}
	two := page
	two.Content = "BT /F1 12 Tf 72 720 Td (page two) Tj ET"
	b = pdftest.Doc(pdftest.DocOptions{Base: b}, page, two)
	embedded := b.Add(pdftest.Stream("/Type /EmbeddedFile", "attached"))
	spec := b.Add(fmt.Sprintf("<< /Type /Filespec /F (notes.txt) /EF << /F %d 0 R >> >>", embedded))
	b.Set(b.Root, fmt.Sprintf("<< /Type /Catalog /Pages %d 0 R /Names << /EmbeddedFiles << /Names [(notes.txt) %d 0 R] >> >> >>", b.Root+1, spec))
	data := b.Bytes()
	child := func(ctx context.Context, name, mime string, r io.ReaderAt, size int64, opts *gofastcore.Options) (*gofastcore.Document, error) {
		d := gofastcore.New("text")
		d.Source.Name = name
		d.Warn(gofastcore.SeverityInfo, gofastcore.WarnPartNoText, "a test child has no parts")
		return d, nil
	}

	extract := func(where string) *gofastcore.Document {
		t.Helper()
		calls := 0
		panicHook = func(at string) {
			if at == where {
				if calls++; calls == 1 {
					panic("a test of " + where)
				}
			}
		}
		defer func() { panicHook = nil }()
		opts := &gofastcore.Options{Workers: 1, Images: gofastcore.ImagesData, Child: child}
		doc, err := Extract(context.Background(), bytes.NewReader(data), int64(len(data)), opts)
		if err != nil {
			t.Fatalf("%s: %v", where, err)
		}
		if err := doc.Validate(); err != nil {
			t.Fatalf("%s: %v", where, err)
		}
		return doc
	}
	failed := func(ws []gofastcore.Warning, code string) bool {
		for _, w := range ws {
			if w.Code == code && strings.Contains(w.Message, "internal error: a test of") {
				return true
			}
		}
		return false
	}

	// The first page fails; the second is read.
	doc := extract("page")
	if len(doc.Parts) != 2 || len(doc.Parts[0].Blocks) != 0 || !failed(doc.Parts[0].Warnings, gofastcore.WarnPartFailed) ||
		doc.Parts[0].Width != 612 || !strings.Contains(doc.Text(), "page two") {
		t.Errorf("page: %+v", doc.Parts)
	}

	// The image has no data; the text of its page is there.
	doc = extract("image")
	p := &doc.Parts[0]
	var img *gofastcore.Image
	for i := range p.Blocks {
		if p.Blocks[i].Image != nil {
			img = p.Blocks[i].Image
		}
	}
	if img == nil || img.Data != nil || img.MIME != "" || !failed(p.Warnings, gofastcore.WarnPartFailed) || !strings.Contains(doc.Text(), "a picture") {
		t.Errorf("image: %+v %+v", img, p.Warnings)
	}

	// The catalog is not read; the pages are.
	doc = extract("describe")
	if doc.Metadata.Title != "" || !failed(doc.Warnings, WarnObjectUnreadable) || !strings.Contains(doc.Text(), "page two") {
		t.Errorf("describe: %+v %+v", doc.Metadata, doc.Warnings)
	}

	// The attached file failed; the document is there.
	doc = extract("child")
	if len(doc.Children) != 1 || doc.Children[0].Source.Name != "notes.txt" || !failed(doc.Children[0].Warnings, gofastcore.WarnChildFailed) ||
		!strings.Contains(doc.Text(), "page two") {
		t.Errorf("child: %+v", doc.Children)
	}

	// Without a panic: all of it.
	doc = extract("nowhere")
	if doc.Metadata.Title != "A title" || doc.Parts[0].Has(gofastcore.WarnPartFailed) || len(doc.Children) != 1 || doc.Children[0].Source.Format != "text" {
		t.Errorf("no panic: %+v %+v %+v", doc.Metadata, doc.Parts[0].Warnings, doc.Children)
	}
}
