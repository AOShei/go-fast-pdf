package gofastpdf_test

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"runtime"
	"strings"
	"testing"

	gofastpdf "github.com/AOShei/go-fast-pdf"
	"github.com/AOShei/go-fast-pdf/internal/pdftest"
	"github.com/AOShei/go-fast-pdf/internal/xpstest"
)

// Whole-document benchmarks through the public entry point. The numbers
// for v0.1.1 are in bench/baseline.txt and those of each phase beside
// it; regenerate with bench/run.sh.

func benchExtract(b *testing.B, path string, o extractOpts) {
	b.Helper()
	info, err := os.Stat(path)
	if err != nil {
		b.Skipf("not on this machine: %s", path)
	}
	// v0.1.1 prints a progress line per page to standard error. Send it
	// to the null device: the cost of producing it stays in the
	// measurement, the output does not flood the benchmark report.
	if null, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0); err == nil {
		stderr := os.Stderr
		os.Stderr = null
		defer func() { os.Stderr = stderr; null.Close() }()
	}
	b.SetBytes(info.Size())
	b.ReportAllocs()
	pages := 0
	// A run with one worker stays on this thread, where the instructions
	// it takes can be counted: a number that, unlike any time, is the
	// same on a busy machine.
	read, stop, counting := func() uint64 { return 0 }, func() {}, false
	if o.Workers <= 1 {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		read, stop, counting = pdftest.CountInstructions()
		defer stop()
	}
	cpu, instr := cpuTime(), read()
	for b.Loop() {
		// As a caller does it: read the file, extract it. The checks of
		// the test adapter are not part of the measurement.
		data, err := os.ReadFile(path)
		if err != nil {
			b.Fatal(err)
		}
		doc, err := gofastpdf.Extract(context.Background(), bytes.NewReader(data), int64(len(data)), o.options())
		if err != nil {
			b.Fatal(err)
		}
		pages = len(doc.Parts)
	}
	b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N)/float64(max(pages, 1)), "ns/page")
	// Processor time, which a busy machine does not inflate as it does
	// the time on the clock.
	if used := cpuTime() - cpu; used > 0 {
		b.ReportMetric(float64(used.Nanoseconds())/float64(b.N), "cpu-ns/op")
	}
	if counting {
		b.ReportMetric(float64(read()-instr)/float64(b.N), "instr/op")
	}
}

// BenchmarkExtract reads synthetic documents of three sizes, sequentially
// and with eight workers.
func BenchmarkExtract(b *testing.B) {
	for _, pages := range []int{10, 100, 1000} {
		path := pdftest.TempFile(b, benchPages(pages))
		b.Run(fmt.Sprintf("pages=%d/workers=1", pages), func(b *testing.B) { benchExtract(b, path, extractOpts{}) })
		b.Run(fmt.Sprintf("pages=%d/workers=8", pages), func(b *testing.B) { benchExtract(b, path, extractOpts{Workers: 8}) })
	}
}

// benchXPS returns an XPS document of the number of pages given: on
// each a running title, three paragraphs of a few lines whose glyphs
// state their advances as a printer driver writes them, a ruled table
// with text in its cells, and a page number.
func benchXPS(pages int) []byte {
	g := xpstest.Glyphs
	indices := func(text string) string {
		var sb strings.Builder
		sb.WriteString(`Indices="`)
		for i, r := range text {
			if i > 0 {
				sb.WriteByte(';')
			}
			if r == ' ' {
				fmt.Fprintf(&sb, "%d,25", xpstest.Glyph(r))
			} else {
				fmt.Fprintf(&sb, "%d,50", xpstest.Glyph(r))
			}
		}
		sb.WriteByte('"')
		return sb.String()
	}
	line := func(x, y float64, text string) string { return g(x, y, 14, text, indices(text)) }
	var out []string
	for p := 1; p <= pages; p++ {
		var b strings.Builder
		b.WriteString(g(96, 60, 11, "The Running Title of the Document", ""))
		y := 130.0
		for para := 0; para < 3; para++ {
			for l := 0; l < 5; l++ {
				b.WriteString(line(96, y, fmt.Sprintf("Line %d of paragraph %d on page %d, with enough words to fill the measure of it", l+1, para+1, p)))
				y += 18
			}
			y += 14
		}
		xs, ys := []float64{96, 296, 496, 696}, []float64{y, y + 30, y + 60, y + 90}
		b.WriteString(xpsGrid(xs, ys))
		for r := 0; r < 3; r++ {
			for c := 0; c < 3; c++ {
				b.WriteString(line(xs[c]+8, ys[r]+21, fmt.Sprintf("cell %d %d of page %d", r+1, c+1, p)))
			}
		}
		b.WriteString(g(400, 1000, 11, fmt.Sprint(p), ""))
		out = append(out, xpstest.Page(xpstest.XPS, 816, 1056, b.String()))
	}
	return xpstest.Document(xpstest.XPS, out...).Bytes()
}

// BenchmarkExtractXPS reads synthetic XPS documents of two sizes,
// sequentially and with eight workers.
func BenchmarkExtractXPS(b *testing.B) {
	for _, pages := range []int{10, 100} {
		path := pdftest.TempFile(b, benchXPS(pages))
		b.Run(fmt.Sprintf("pages=%d/workers=1", pages), func(b *testing.B) { benchExtract(b, path, extractOpts{}) })
		b.Run(fmt.Sprintf("pages=%d/workers=8", pages), func(b *testing.B) { benchExtract(b, path, extractOpts{Workers: 8}) })
	}
}

// BenchmarkExtractFiles reads real documents: the generated fixtures and,
// where present, the two manuals from the corpus.
func BenchmarkExtractFiles(b *testing.B) {
	files := map[string]string{
		"english":                 fixture("english"),
		"multilingual":            fixture("multilingual"),
		"gutenprint-users-manual": "/usr/share/gutenprint/doc/gutenprint-users-manual.pdf",
		"gutenprint-manual":       "/usr/share/gutenprint/doc/gutenprint.pdf",
	}
	for _, name := range []string{"english", "multilingual", "gutenprint-users-manual", "gutenprint-manual"} {
		b.Run(name+"/workers=1", func(b *testing.B) { benchExtract(b, files[name], extractOpts{}) })
		b.Run(name+"/workers=8", func(b *testing.B) { benchExtract(b, files[name], extractOpts{Workers: 8}) })
	}
}

// BenchmarkExtractImages reads a document with one large image, asking for
// image metadata (defect P4).
func BenchmarkExtractImages(b *testing.B) {
	const side = 2048
	bd := pdftest.New()
	img := bd.AddFlate(fmt.Sprintf("/Type /XObject /Subtype /Image /Width %d /Height %d /ColorSpace /DeviceGray /BitsPerComponent 8", side, side),
		make([]byte, side*side))
	data := pdftest.Doc(pdftest.DocOptions{Base: bd}, pdftest.PageSpec{
		Content:   "q 200 0 0 200 72 500 cm /Im1 Do Q",
		Resources: fmt.Sprintf("/XObject << /Im1 %d 0 R >>", img),
	}).Bytes()
	benchExtract(b, pdftest.TempFile(b, data), extractOpts{Images: true})
}
