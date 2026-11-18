package gofastpdf_test

import (
	"errors"
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
		// An error is fine. A document that breaks the contract of the
		// shared model is not, and neither is one that depends on the
		// number of workers.
		one, err := extractBytes(data, extractOpts{Images: true})
		if errors.Is(err, errContract) {
			t.Fatal(err)
		}
		if err != nil {
			return
		}
		many, err := extractBytes(data, extractOpts{Images: true, Workers: 4})
		if err != nil {
			t.Fatalf("with 4 workers: %v", err)
		}
		if diff := sameDocument(one.Doc, many.Doc); diff != "" {
			t.Fatalf("1 worker and 4 workers: %s", diff)
		}
	})
}
