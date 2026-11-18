package pdf

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/AOShei/go-fast-pdf/internal/pdftest"
)

// Benchmarks of the engine's layers. bench/baseline.txt holds the v0.1.1
// numbers that Phase 1 is measured against.

// benchContent is a content stream of about 1 MB: text lines mixed with
// the path operators that make up most of a real page.
func benchContent() []byte {
	var sb strings.Builder
	for i := 0; sb.Len() < 1<<20; i++ {
		fmt.Fprintf(&sb, "BT /F1 10 Tf 72 %d Td [(Line %d of the) -250 (benchmark) 30 (page)] TJ ET\n", 720-i%60*12, i)
		sb.WriteString("0.5 w 72 100 m 540 100 l S 72.5 101.25 468 12 re f q 1 0 0 1 10 10 cm Q\n")
	}
	return []byte(sb.String())
}

func BenchmarkLexer(b *testing.B) {
	data := benchContent()
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	for b.Loop() {
		l := NewLexer(bytes.NewReader(data))
		for {
			if _, err := l.ReadObject(); err != nil {
				break
			}
		}
	}
}

func BenchmarkContentParser(b *testing.B) {
	data := benchContent()
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	for b.Loop() {
		p := NewContentStreamParser(data)
		for {
			if _, err := p.Next(); err == io.EOF {
				break
			}
		}
	}
}

// BenchmarkResolve opens a 1,000-page document and resolves every object
// once, for a classic file and for one that keeps its objects in an
// object stream.
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
				r, err := NewReader(bytes.NewReader(data))
				if err != nil {
					b.Fatal(err)
				}
				for n := 1; n <= 2003; n++ {
					r.GetObject(IndirectObject{ObjectNumber: n})
				}
			}
		})
	}
}
