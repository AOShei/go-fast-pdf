package gofastpdf

import (
	"testing"

	gofastcore "github.com/AOShei/go-fast-core"
	"github.com/AOShei/go-fast-pdf/internal/file"
	"github.com/AOShei/go-fast-pdf/internal/pdftest"
	"github.com/AOShei/go-fast-pdf/internal/syntax"
)

// A rectangle within the range of coordinates that the page's matrix
// takes out of it has no box, as one out of range in the file; found by
// FuzzOpen, a note's box past 1e9 on a page whose box is moved.
func TestRectOnPageRange(t *testing.T) {
	b := pdftest.New()
	in := b.Add("[0 999999000 20 999999990]")
	out := b.Add("[0 2e9 20 2e9]")
	b.Add("<< /Type /Catalog /Pages 4 0 R >>")
	b.Add("<< /Type /Pages /Kids [] /Count 0 >>")
	b.Root = 3
	f, err := file.Open(b.Bytes(), file.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	ref := func(n int) syntax.Value { return syntax.Value{Kind: syntax.Ref, N: int64(n)} }
	id := [6]float64{1, 0, 0, 1, 0, 0}
	moved := [6]float64{1, 0, 0, 1, -900, 9000}
	if r := rectOnPage(f, ref(in), id); r != (gofastcore.Rect{X0: 0, Y0: 999999000, X1: 20, Y1: 999999990}) {
		t.Errorf("in range: %+v", r)
	}
	if r := rectOnPage(f, ref(in), moved); r != (gofastcore.Rect{}) {
		t.Errorf("moved out of range: %+v", r)
	}
	if r := rectOnPage(f, ref(out), id); r != (gofastcore.Rect{}) {
		t.Errorf("out of range in the file: %+v", r)
	}
}
