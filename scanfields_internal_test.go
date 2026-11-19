package gofastpdf

import (
	"testing"

	"github.com/AOShei/go-fast-pdf/internal/file"
	"github.com/AOShei/go-fast-pdf/internal/pdftest"
	"github.com/AOShei/go-fast-pdf/internal/syntax"
)

// Looking at the actions of a form's fields leaves the form as it is.
// The walk once used the elements of the parsed /Fields array as its
// stack, and pushing a field's kids onto it overwrote the array in the
// file's cache: what read the form after it found the kids of a field
// in the place of the field.
func TestScanFieldsLeavesTheFormAlone(t *testing.T) {
	b := pdftest.New()
	b.Add("<< /T (root) /Kids [3 0 R 4 0 R] >>")
	b.Add("<< /T (other) /FT /Tx /V (z) >>")
	b.Add("<< /T (a) /Parent 1 0 R /FT /Tx /V (x) >>")
	b.Add("<< /T (b) /Parent 1 0 R /FT /Tx /V (y) /AA << /K << /S /JavaScript /JS (1) >> >> >>")
	fieldsObj := b.Add("[1 0 R 2 0 R]")
	data := pdftest.Doc(pdftest.DocOptions{Base: b}, pdftest.PageSpec{Content: "x"}).Bytes()
	f, err := file.Open(data, file.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	ref := syntax.Value{Kind: syntax.Ref, N: int64(fieldsObj)}
	numbers := func() (out []int64) {
		for _, e := range f.Resolve(ref).Elems() {
			out = append(out, file.RefNumber(e))
		}
		return out
	}
	before := numbers()
	var out pageOut
	scanFields(f, ref, &out)
	if after := numbers(); len(after) != 2 || after[0] != before[0] || after[1] != before[1] || !out.active {
		t.Errorf("/Fields was %v and is %v after the walk; script found: %v", before, after, out.active)
	}
}
