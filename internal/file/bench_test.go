package file

import (
	"testing"

	"github.com/AOShei/go-fast-pdf/internal/pdftest"
)

// BenchmarkResolve opens a 1,000-page document and resolves every object
// once, for a classic file and for one that keeps its objects in an
// object stream (defect P3). bench/baseline.txt has the v0.1.1 numbers
// under the same name.
func BenchmarkResolve(b *testing.B) {
	pages := make([]pdftest.PageSpec, 1000)
	for i := range pages {
		pages[i].Content = pdftest.TextPage("page")
	}
	for _, objstm := range []bool{false, true} {
		data := pdftest.Doc(pdftest.DocOptions{Configure: func(bd *pdftest.Builder) { bd.ObjStm = objstm }}, pages...).Bytes()
		name := "classic"
		if objstm {
			name = "objstm"
		}
		b.Run(name, func(b *testing.B) {
			b.SetBytes(int64(len(data)))
			b.ReportAllocs()
			for b.Loop() {
				f, err := Open(data, Limits{})
				if err != nil {
					b.Fatal(err)
				}
				for n := int64(1); n <= 2003; n++ {
					f.Object(n)
				}
			}
		})
	}
}
