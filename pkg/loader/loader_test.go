package loader

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	gofastcore "github.com/AOShei/go-fast-core"
	"github.com/AOShei/go-fast-pdf/internal/pdftest"
)

// A file larger than the input limit is refused before any of it is
// read. The file is sparse: 2 GiB in size, no data on disk.
func TestInputSizeLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "huge.pdf")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(2 << 30); err != nil {
		f.Close()
		t.Skipf("cannot create a sparse file here: %v", err)
	}
	f.Close()

	start := time.Now()
	var loadErr error
	allocated := pdftest.AllocBytes(func() { _, loadErr = LoadPDF(path, false) })
	if !errors.Is(loadErr, gofastcore.ErrLimitExceeded) {
		t.Errorf("error %v, want ErrLimitExceeded", loadErr)
	}
	if allocated > 1<<20 || time.Since(start) > 5*time.Second {
		t.Errorf("refusing the file allocated %d KB and took %v", allocated>>10, time.Since(start))
	}
}

func TestLoadErrors(t *testing.T) {
	dir := t.TempDir()
	if _, err := LoadPDF(filepath.Join(dir, "missing.pdf"), false); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("missing file: %v", err)
	}
	notPDF := filepath.Join(dir, "text.pdf")
	os.WriteFile(notPDF, []byte("this is not a PDF"), 0o644)
	for _, load := range []func() error{
		func() error { _, err := LoadPDF(notPDF, false); return err },
		func() error { _, err := LoadPDFConcurrent(notPDF, 4, true); return err },
	} {
		if err := load(); !errors.Is(err, gofastcore.ErrMalformed) {
			t.Errorf("not a PDF: %v, want ErrMalformed", err)
		}
	}
}

func TestSequentialAndConcurrentAgree(t *testing.T) {
	pages := make([]pdftest.PageSpec, 60)
	for i := range pages {
		pages[i].Content = pdftest.TextPage("page", string(rune('A'+i%26)))
	}
	b := pdftest.Doc(pdftest.DocOptions{Compress: true}, pages...)
	b.Set(9, "42") // the third page is not a page
	path := pdftest.TempFile(t, b.Bytes())
	one, err := LoadPDF(path, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(one.Pages) != 60 || one.Pages[2].Content != "" || one.Pages[3].PageNumber != 4 || one.Pages[3].Content == "" {
		t.Fatalf("%d pages; the unreadable third page must stay in place, empty", len(one.Pages))
	}
	for _, workers := range []int{0, 2, 8, 64, 1000} {
		many, err := LoadPDFConcurrent(path, workers, true)
		if err != nil {
			t.Fatal(err)
		}
		if len(many.Pages) != len(one.Pages) {
			t.Fatalf("%d workers: %d pages", workers, len(many.Pages))
		}
		for i := range one.Pages {
			a, b := one.Pages[i], many.Pages[i]
			if a.PageNumber != b.PageNumber || a.Content != b.Content || a.Width != b.Width || len(*a.Images) != len(*b.Images) {
				t.Fatalf("%d workers: page %d differs", workers, i+1)
			}
		}
	}
}
