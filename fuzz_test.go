package gofastpdf_test

import (
	"testing"

	"github.com/AOShei/go-fast-pdf/internal/pdftest"
)

// FuzzOpen feeds whole files to the library through its public entry
// point. The engine's own targets (FuzzParser, FuzzScanner, FuzzCMap,
// FuzzOpen and FuzzExtract under internal/) reach more inputs per second;
// this one checks that nothing is lost between them and the caller.
func FuzzOpen(f *testing.F) {
	f.Add(pdftest.OnePage(pdftest.TextPage("Hello, world")))
	f.Add(pdftest.Doc(pdftest.DocOptions{Compress: true, Configure: func(b *pdftest.Builder) { b.ObjStm = true }},
		pdftest.PageSpec{Content: pdftest.TextPage("object streams")}).Bytes())
	f.Fuzz(func(t *testing.T, data []byte) {
		// An error is fine. Text that is not valid UTF-8 is not.
		if _, err := extractBytes(data, extractOpts{Images: true}); err != nil && err.Error() == "page 1: text is not valid UTF-8" {
			t.Error(err)
		}
	})
}
