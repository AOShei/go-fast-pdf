// Package loader is the entry point of go-fast-pdf v0.1: it reads a PDF
// file and returns its text page by page.
//
// The package is kept, with unchanged signatures and output, while the
// engine underneath is replaced. It is removed when the public API moves
// to the module root and the shared document model (Phase 5 of
// IMPLEMENTATION_PLAN.md).
package loader

import (
	"fmt"
	"os"
	"runtime"
	"sync"

	gofastcore "github.com/AOShei/go-fast-core"
	"github.com/AOShei/go-fast-pdf/internal/extract"
	"github.com/AOShei/go-fast-pdf/internal/file"
	"github.com/AOShei/go-fast-pdf/internal/syntax"
	"github.com/AOShei/go-fast-pdf/pkg/model"
)

// Defaults for the limits that have no parameter in this API.
const (
	maxInputBytes   = 1 << 30 // 1 GiB
	maxDecodedBytes = 1 << 30 // per document, counted in page order
)

// LoadPDF takes a file path and returns the structured Document.
func LoadPDF(path string, extractImages bool) (*model.Document, error) {
	return load(path, 1, extractImages)
}

// LoadPDFConcurrent loads a PDF and extracts its pages with several
// workers. workers <= 0 means one per CPU. The result is the same as
// LoadPDF's for any number of workers.
func LoadPDFConcurrent(path string, workers int, extractImages bool) (*model.Document, error) {
	if workers <= 0 {
		workers = runtime.NumCPU()
	}
	return load(path, workers, extractImages)
}

func load(path string, workers int, images bool) (*model.Document, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.Size() > maxInputBytes {
		return nil, fmt.Errorf("%w: the file is %d bytes and the limit is %d", gofastcore.ErrLimitExceeded, info.Size(), int64(maxInputBytes))
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	f, err := file.Open(data, file.Limits{})
	if err != nil {
		return nil, fmt.Errorf("failed to create pdf reader: %w", err)
	}

	doc := &model.Document{Metadata: model.Metadata{Encrypted: f.IsEncrypted()}}
	if info := f.Info(); info.IsDict() {
		text := func(key string) string {
			if v := f.Resolve(info.Get(key)); v.Kind == syntax.String {
				return string(v.Data)
			}
			return ""
		}
		doc.Metadata.Title, doc.Metadata.Author = text("Title"), text("Author")
		doc.Metadata.Creator, doc.Metadata.Producer = text("Creator"), text("Producer")
	}

	pages := f.Pages()
	results := make([]extract.Result, len(pages))
	opts := extract.Options{Images: images}
	if workers = min(workers, len(pages)); workers <= 1 {
		for i := range pages {
			results[i] = extract.Page(f, &pages[i], opts)
		}
	} else {
		// One shared File; each worker takes the next page.
		var wg sync.WaitGroup
		next := make(chan int)
		for range workers {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := range next {
					results[i] = extract.Page(f, &pages[i], opts)
				}
			}()
		}
		for i := range pages {
			next <- i
		}
		close(next)
		wg.Wait()
	}

	// Results are merged in page order, and the decoded-bytes budget is
	// charged in that order, so where a document is cut does not depend
	// on how the workers were scheduled.
	doc.Pages = make([]model.Page, 0, len(pages))
	var decoded int64
	for i := range pages {
		res := &results[i]
		decoded += res.Decoded
		if decoded > maxDecodedBytes {
			break
		}
		// A page that could not be read stays in the list, empty, so
		// that the numbers of the pages after it remain true.
		page := model.Page{
			PageNumber: pages[i].Number,
			Content:    res.Text,
			CharCount:  len(res.Text),
		}
		if pages[i].HasMediaBox {
			page.Width = pages[i].MediaBox[2] - pages[i].MediaBox[0]
			page.Height = pages[i].MediaBox[3] - pages[i].MediaBox[1]
		}
		if images {
			list := make([]model.Image, len(res.Images))
			for k, img := range res.Images {
				list[k] = model.Image{
					Type:       "image",
					ID:         img.Name,
					Rect:       img.Rect[:],
					Width:      img.Width,
					Height:     img.Height,
					ColorSpace: img.ColorSpace,
				}
				if img.Inline {
					list[k].Type = "inline_image"
				}
			}
			page.Images = &list
		}
		doc.Pages = append(doc.Pages, page)
	}
	return doc, nil
}
