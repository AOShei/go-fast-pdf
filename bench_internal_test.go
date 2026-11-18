package gofastpdf

import (
	"bytes"
	"context"
	"os"
	"testing"

	gofastcore "github.com/AOShei/go-fast-core"
)

// BenchmarkHiddenChecks measures what the checks for hidden text that
// always run add to the extraction of a whole document: the same files
// with the checks and without them.
func BenchmarkHiddenChecks(b *testing.B) {
	files := []struct{ name, path string }{
		{"english", "testdata/fixtures/english.pdf"},
		{"layout-justified-libreoffice", "testdata/fixtures/layout-justified-libreoffice.pdf"},
		{"gutenprint-users-manual", "/usr/share/gutenprint/doc/gutenprint-users-manual.pdf"},
		{"gutenprint-manual", "/usr/share/gutenprint/doc/gutenprint.pdf"},
	}
	for _, f := range files {
		data, err := os.ReadFile(f.path)
		if err != nil {
			continue
		}
		for _, mode := range []struct {
			name string
			off  bool
		}{{"checks=on", false}, {"checks=off", true}} {
			b.Run(f.name+"/"+mode.name, func(b *testing.B) {
				e := &Extractor{noHiddenChecks: mode.off}
				opts := &gofastcore.Options{Workers: 1}
				b.ReportAllocs()
				cpu := processorTime()
				for b.Loop() {
					if _, err := e.Extract(context.Background(), bytes.NewReader(data), int64(len(data)), opts); err != nil {
						b.Fatal(err)
					}
				}
				if used := processorTime() - cpu; used > 0 {
					b.ReportMetric(float64(used.Nanoseconds())/float64(b.N), "cpu-ns/op")
				}
			})
		}
	}
}
