package gofastpdf

import (
	"context"
	"fmt"
	"runtime"
	"testing"
	"time"

	gofastcore "github.com/AOShei/go-fast-core"
	"github.com/AOShei/go-fast-pdf/internal/pdftest"
)

// The workers of a document keep their state from page to page, and give
// it back when the document is read: nothing of the document is kept
// after, by them or by the pools (#65). Read with one worker and with
// several, by the iterator and by Extract.
func TestWorkersKeepNothingAfter(t *testing.T) {
	var pages []pdftest.PageSpec
	for i := 0; i < 40; i++ {
		pages = append(pages, pdftest.PageSpec{Content: "BT /F1 12 Tf 72 700 Td [(page) -200 (text)] TJ ET"})
	}
	plain := pdftest.Doc(pdftest.DocOptions{}, pages...).Bytes()
	// Tagged: the layout is given functions over the structure tree.
	b := pdftest.New()
	root := b.Reserve()
	var elems, nums string
	tagged := make([]pdftest.PageSpec, 8)
	for i := range tagged {
		el := b.Add(fmt.Sprintf("<< /Type /StructElem /S /H1 /P %d 0 R /K 0 >>", root))
		elems += fmt.Sprintf("%d 0 R ", el)
		nums += fmt.Sprintf("%d [%d 0 R] ", i, el)
		tagged[i] = pdftest.PageSpec{Content: "/H1 <</MCID 0>> BDC BT /F1 12 Tf 72 700 Td (A heading) Tj ET EMC", Extra: fmt.Sprintf("/StructParents %d", i)}
	}
	b.Set(root, fmt.Sprintf("<< /Type /StructTreeRoot /K [%s] /ParentTree << /Nums [%s] >> >>", elems, nums))
	d := pdftest.Doc(pdftest.DocOptions{Base: b}, tagged...)
	d.Set(d.Root, fmt.Sprintf("<< /Type /Catalog /Pages %d 0 R /StructTreeRoot %d 0 R /MarkInfo << /Marked true >> >>", d.Root+1, root))
	for _, c := range []struct {
		src     []byte
		pages   int
		workers int
	}{{plain, 40, 1}, {plain, 40, 4}, {d.Bytes(), 8, 1}, {d.Bytes(), 8, 4}} {
		src, workers := c.src, c.workers
		freed := make(chan bool, 1)
		func() {
			data := make([]byte, len(src))
			copy(data, src)
			runtime.SetFinalizer(&data[0], func(*byte) { freed <- true })
			f, err := OpenBytes(data, &gofastcore.Options{Workers: workers})
			if err != nil {
				t.Fatal(err)
			}
			n := 0
			for _, err := range f.Parts(context.Background()) {
				if err != nil {
					t.Fatal(err)
				}
				n++
			}
			f.Close()
			if n != c.pages {
				t.Fatalf("%d parts", n)
			}
		}()
		runtime.GC()
		select {
		case <-freed:
		case <-time.After(2 * time.Second):
			t.Fatalf("%d workers: the document's bytes are kept after one collection", workers)
		}
	}
}
