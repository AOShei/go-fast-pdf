package gofastpdf_test

import (
	"bytes"
	"context"
	"runtime"
	"testing"

	gofastcore "github.com/AOShei/go-fast-core"
	gofastpdf "github.com/AOShei/go-fast-pdf"
	"github.com/AOShei/go-fast-pdf/internal/pdftest"
)

// heapWhile reads the parts of a document one by one and returns the
// most that was on the heap at any of the times it looked, which is
// after every 100 parts and a collection.
func heapWhile(t *testing.T, data []byte, workers int) (peak uint64, parts int) {
	t.Helper()
	f, err := gofastpdf.OpenBytes(data, &gofastcore.Options{Workers: workers})
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var ms runtime.MemStats
	for part, err := range f.Parts(context.Background()) {
		if err != nil {
			t.Fatal(err)
		}
		if len(part.Blocks) == 0 {
			t.Fatalf("page %d has no text", part.Number)
		}
		if parts++; parts%100 == 0 {
			runtime.GC()
			runtime.ReadMemStats(&ms)
			peak = max(peak, ms.HeapAlloc)
		}
	}
	return peak, parts
}

// TestMemoryOfTheIterator: going through a file part by part holds the
// file, a few pages, and what is kept of parsed objects, which has a
// budget. Beyond that budget the memory does not grow with the pages
// read: what a page adds for good is its entry in the list of pages and
// in the table of objects, a few hundred bytes. A document of 15,000
// pages is read in little more than one of 5,000.
func TestMemoryOfTheIterator(t *testing.T) {
	if testing.Short() || pdftest.RaceEnabled {
		t.Skip("documents of thousands of pages, and sizes that the race detector changes")
	}
	small := pdftest.Doc(pdftest.DocOptions{Compress: true}, manyPages(5000)...).Bytes()
	large := pdftest.Doc(pdftest.DocOptions{Compress: true}, manyPages(15000)...).Bytes()

	var ms runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&ms)
	base := ms.HeapAlloc // the two files and whatever else the tests hold

	for _, workers := range []int{1, 8} {
		peakSmall, n := heapWhile(t, small, workers)
		if n != 5000 {
			t.Fatalf("%d parts", n)
		}
		peakLarge, n := heapWhile(t, large, workers)
		if n != 15000 {
			t.Fatalf("%d parts", n)
		}
		usedSmall, usedLarge := int64(peakSmall)-int64(base), int64(peakLarge)-int64(base)
		perPage := float64(usedLarge-usedSmall) / 10000
		t.Logf("%d workers: %d KB over the files for 5,000 pages, %d KB for 15,000: %.0f bytes for every further page (the files are %d KB and %d KB)",
			workers, usedSmall>>10, usedLarge>>10, perPage, len(small)>>10, len(large)>>10)
		if perPage > 600 {
			t.Errorf("%d workers: %.0f bytes more on the heap for every page of the file", workers, perPage)
		}
		if usedSmall > 12<<20 {
			t.Errorf("%d workers: %d KB on the heap while reading 5,000 pages", workers, usedSmall>>10)
		}
	}

	// Holding the document is another matter: its parts are kept.
	doc, err := gofastpdf.Extract(context.Background(), bytes.NewReader(large), int64(len(large)), nil)
	if err != nil {
		t.Fatal(err)
	}
	runtime.GC()
	runtime.ReadMemStats(&ms)
	t.Logf("the document of 15,000 parts, held: %d KB over the files", (int64(ms.HeapAlloc)-int64(base))>>10)
	runtime.KeepAlive(doc)
	doc = nil

	// With a smaller budget for parsed objects, less is held; the parts
	// are the same.
	e := &gofastpdf.Extractor{ObjectCacheBytes: 256 << 10}
	f, err := e.OpenBytes(small, &gofastcore.Options{Workers: 4})
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	var peak uint64
	for part, err := range f.Parts(context.Background()) {
		if err != nil || part.Number != n+1 || len(part.Blocks) != 1 {
			t.Fatalf("page %d: part %d, %d blocks, %v", n+1, part.Number, len(part.Blocks), err)
		}
		if n++; n%500 == 0 {
			runtime.GC()
			runtime.ReadMemStats(&ms)
			peak = max(peak, ms.HeapAlloc)
		}
	}
	runtime.KeepAlive(large) // it is part of what base measured
	if used := int64(peak) - int64(base); used > 3<<20 {
		t.Errorf("with a budget of 256 KB: %d KB on the heap while reading 5,000 pages", used>>10)
	} else {
		t.Logf("with a budget of 256 KB for parsed objects: %d KB over the files for 5,000 pages", used>>10)
	}
}
