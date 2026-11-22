package extract

import (
	"runtime"
	"testing"
	"time"

	"github.com/AOShei/go-fast-pdf/internal/file"
	"github.com/AOShei/go-fast-pdf/internal/pdftest"
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
