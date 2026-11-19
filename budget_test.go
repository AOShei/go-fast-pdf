package gofastpdf_test

import (
	"bytes"
	"context"
	"os"
	"runtime"
	"strconv"
	"testing"

	gofastcore "github.com/AOShei/go-fast-core"
	gofastpdf "github.com/AOShei/go-fast-pdf"
	"github.com/AOShei/go-fast-pdf/internal/pdftest"
)

// The budgets of Phase 8 (IMPLEMENTATION_PLAN.md): what reading a
// document may allocate, counted and in bytes, per page. A change that
// goes over a budget either gives the memory back or raises the number
// here, with the reason, in the same change.
//
// The number of allocations is the same from run to run; its budget is
// what was measured when it was set, plus an eighth. The bytes move by a
// quarter with what the collector does to the tables and buffers that
// are kept between pages; their budget is the most that was seen, plus a
// quarter. They are those of a 64-bit build, and the test is skipped on
// any other.
//
// For comparison: v0.1.1 made 975 allocations for each of the hundred
// pages of 40 lines, where this makes 20.
var budgets = []struct {
	name          string
	data          func(t *testing.T) []byte
	allocsPerPage float64
	bytesPerPage  float64
	deep          bool
}{
	// Measured: 69 allocations and 26.5 KB for each of 3 pages.
	{name: "english", data: fixtureBytes("english"), allocsPerPage: 78, bytesPerPage: 34000},
	// 448 and 92.7 KB: one page in five scripts, with as many fonts.
	{name: "multilingual", data: fixtureBytes("multilingual"), allocsPerPage: 504, bytesPerPage: 118000},
	// 277 and 104 KB.
	{name: "layout-justified-libreoffice", data: fixtureBytes("layout-justified-libreoffice"), allocsPerPage: 312, bytesPerPage: 132000},
	// 196 and 81.1 KB.
	{name: "layout-cols2-firefox", data: fixtureBytes("layout-cols2-firefox"), allocsPerPage: 221, bytesPerPage: 104000},
	// 20 and 10.0 KB.
	{name: "100 pages of 40 lines", data: func(*testing.T) []byte { return benchPages(100) }, allocsPerPage: 23, bytesPerPage: 13000},
	// The checks of DeepHidden allocate nothing per page that the
	// default does not: 69 and 27.2 KB, 196 and 83.0 KB.
	{name: "english with DeepHidden", data: fixtureBytes("english"), deep: true, allocsPerPage: 78, bytesPerPage: 35000},
	{name: "layout-cols2-firefox with DeepHidden", data: fixtureBytes("layout-cols2-firefox"), deep: true, allocsPerPage: 221, bytesPerPage: 106000},
}

func fixtureBytes(name string) func(t *testing.T) []byte {
	return func(t *testing.T) []byte {
		data, err := os.ReadFile(fixture(name))
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
}

func TestBudgets(t *testing.T) {
	if pdftest.RaceEnabled || strconv.IntSize != 64 {
		t.Skip("the budgets are those of a 64-bit build without the race detector")
	}
	for _, b := range budgets {
		t.Run(b.name, func(t *testing.T) {
			data := b.data(t)
			opts := &gofastcore.Options{Workers: 1, DeepHidden: b.deep, Images: gofastcore.ImagesMeta}
			pages := 0
			run := func() {
				doc, err := gofastpdf.Extract(context.Background(), bytes.NewReader(data), int64(len(data)), opts)
				if err != nil {
					t.Fatal(err)
				}
				pages = len(doc.Parts)
			}
			run() // what is made once, such as tables, is not a page's
			allocs := testing.AllocsPerRun(10, run) / float64(pages)
			// Bytes: the growth of the total allocated, over as many runs.
			const runs = 10
			var before, after runtime.MemStats
			runtime.GC()
			runtime.ReadMemStats(&before)
			for range runs {
				run()
			}
			runtime.ReadMemStats(&after)
			perPage := float64(after.TotalAlloc-before.TotalAlloc) / runs / float64(pages)
			t.Logf("%d pages: %.0f allocations and %.0f bytes per page (budget %.0f and %.0f)", pages, allocs, perPage, b.allocsPerPage, b.bytesPerPage)
			if allocs > b.allocsPerPage {
				t.Errorf("%.0f allocations per page, over the budget of %.0f", allocs, b.allocsPerPage)
			}
			// Coverage changes what the compiler inlines and what escapes
			// to the heap, and so the bytes; not the number of allocations
			// enough to matter. Bytes are held to their budget only in a
			// build without it.
			if perPage > b.bytesPerPage && testing.CoverMode() == "" {
				t.Errorf("%.0f bytes per page, over the budget of %.0f", perPage, b.bytesPerPage)
			}
		})
	}
}
