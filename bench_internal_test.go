package gofastpdf

import (
	"bytes"
	"context"
	"os"
	"runtime"
	"testing"

	gofastcore "github.com/AOShei/go-fast-core"
	"github.com/AOShei/go-fast-pdf/internal/pdftest"
)

// BenchmarkHiddenChecks measures what the checks for hidden text add to
// the extraction of a whole document: the same files without any check,
// with the checks that always run, and with those of DeepHidden.
func BenchmarkHiddenChecks(b *testing.B) {
	files := []struct{ name, path string }{
		{"english", "testdata/fixtures/english.pdf"},
		{"layout-justified-libreoffice", "testdata/fixtures/layout-justified-libreoffice.pdf"},
		{"layout-cols2-firefox", "testdata/fixtures/layout-cols2-firefox.pdf"},
		{"gutenprint-users-manual", "/usr/share/gutenprint/doc/gutenprint-users-manual.pdf"},
		{"gutenprint-manual", "/usr/share/gutenprint/doc/gutenprint.pdf"},
	}
	for _, f := range files {
		data, err := os.ReadFile(f.path)
		if err != nil {
			continue
		}
		for _, mode := range []struct {
			name      string
			off, deep bool
		}{{"checks=off", true, false}, {"checks=on", false, false}, {"checks=deep", false, true}} {
			b.Run(f.name+"/"+mode.name, func(b *testing.B) {
				e := &Extractor{noHiddenChecks: mode.off}
				opts := &gofastcore.Options{Workers: 1, DeepHidden: mode.deep}
				b.ReportAllocs()
				// One worker stays on this thread, where its instructions
				// can be counted.
				runtime.LockOSThread()
				defer runtime.UnlockOSThread()
				read, stop, counting := pdftest.CountInstructions()
				defer stop()
				cpu, instr := processorTime(), read()
				for b.Loop() {
					if _, err := e.Extract(context.Background(), bytes.NewReader(data), int64(len(data)), opts); err != nil {
						b.Fatal(err)
					}
				}
				if used := processorTime() - cpu; used > 0 {
					b.ReportMetric(float64(used.Nanoseconds())/float64(b.N), "cpu-ns/op")
				}
				if counting {
					b.ReportMetric(float64(read()-instr)/float64(b.N), "instr/op")
				}
			})
		}
	}
}
