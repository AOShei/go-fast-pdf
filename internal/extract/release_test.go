package extract

import (
	"fmt"
	"runtime"
	"testing"
	"time"

	"github.com/AOShei/go-fast-pdf/internal/file"
	"github.com/AOShei/go-fast-pdf/internal/pdftest"
	"github.com/AOShei/go-fast-pdf/internal/syntax"
)

// An interpreter that waits in the pool keeps nothing of the document it
// read: once the document is let go, one collection frees its bytes.
// Before, its resource dictionaries, fonts, operands and runs pointed
// into the file, and kept all of it until the pool let the interpreter
// go, two collections later; found by TestMemoryOfTheIterator, which
// counted a document's 5 MB too many now and then.
func TestPoolKeepsNothingOfTheDocument(t *testing.T) {
	src := pdftest.Doc(pdftest.DocOptions{}, pdftest.PageSpec{
		Content: "q 1 0 0 1 10 10 cm BT /F1 12 Tf 72 700 Td [(one) -200 (two)] TJ ET BI /W 1 /H 1 /CS /G /BPC 8 ID \x80 EI Q",
	}).Bytes()
	freed := make(chan bool, 1)
	read := func() {
		// The bytes of the file, in an allocation of their own whose end
		// can be seen.
		data := make([]byte, len(src))
		copy(data, src)
		runtime.SetFinalizer(&data[0], func(*byte) { freed <- true })
		f, err := file.Open(data, file.Limits{})
		if err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 4; i++ { // the same interpreter, most likely, each time
			if res := Page(f, &f.Pages()[0], Options{Runs: true, Words: true}); len(res.Runs) == 0 {
				t.Fatal("no text")
			}
		}
	}
	read()
	runtime.GC()
	select {
	case <-freed:
	case <-time.After(2 * time.Second):
		t.Fatal("the document's bytes are kept after one collection")
	}
}

// Options.Annot is given each annotation of the page once, in the order
// of its list, with what Result.Shown says of it: those that draw
// nothing, are not dictionaries or are popups included.
func TestAnnotGivenEachOnce(t *testing.T) {
	b := pdftest.New()
	form := b.Add(pdftest.Stream("/Type /XObject /Subtype /Form /BBox [0 0 100 20] /Resources << /Font << /F1 << /Type /Font /Subtype /Type1 /BaseFont /Helvetica >> >> >>", "BT /F1 10 Tf 2 5 Td (shown) Tj ET"))
	shown := b.Add(fmt.Sprintf("<< /Type /Annot /Subtype /Widget /Rect [10 10 110 30] /AP << /N %d 0 R >> >>", form))
	plain := b.Add("<< /Type /Annot /Subtype /Text /Rect [0 0 10 10] /Contents (a note) >>")
	popup := b.Add("<< /Type /Annot /Subtype /Popup /Rect [0 0 10 10] >>")
	data := pdftest.Doc(pdftest.DocOptions{Base: b}, pdftest.PageSpec{
		Content: "BT /F1 12 Tf 72 700 Td (page) Tj ET",
		Extra:   fmt.Sprintf("/Annots [%d 0 R 7 %d 0 R %d 0 R]", shown, plain, popup),
	}).Bytes()
	f, err := file.Open(data, file.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	res := Page(f, &f.Pages()[0], Options{Annot: func(i int, a syntax.Value, s bool) {
		got = append(got, fmt.Sprintf("%d %v %s %v", i, a.IsDict(), f.Resolve(a.Get("Subtype")).NameString(), s))
	}})
	want := []string{"0 true Widget true", "1 false  false", "2 true Text false", "3 true Popup false"}
	if fmt.Sprint(got) != fmt.Sprint(want) || len(res.Shown) != 4 || !res.Shown[0] {
		t.Errorf("%q, shown %v\nwant %q", got, res.Shown, want)
	}
}
