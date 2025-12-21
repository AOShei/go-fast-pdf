package loader

import (
	"fmt"
	"os"
	"runtime"
	"sync"
	"time"

	"github.com/AOShei/pdf-loader/pkg/model"
	"github.com/AOShei/pdf-loader/pkg/pdf"
)

// pageResult holds the result of processing a single page
type pageResult struct {
	pageNum int
	page    model.Page
	err     error
}

// LoadPDF takes a file path and returns the structured Document.
func LoadPDF(path string) (*model.Document, error) {
	// 1. Open File
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	// 2. Initialize the Low-Level Reader
	reader, err := pdf.NewReader(f)
	if err != nil {
		return nil, fmt.Errorf("failed to create pdf reader: %w", err)
	}

	// 3. Extract Metadata
	meta := model.Metadata{
		Encrypted: reader.IsEncrypted(),
	}

	// Skip metadata extraction if encrypted (strings will be garbage)
	if !meta.Encrypted {
		if info, err := reader.GetInfo(); err == nil && info != nil {
			if t, ok := info["/Title"].(pdf.StringObject); ok {
				meta.Title = string(t)
			}
			if a, ok := info["/Author"].(pdf.StringObject); ok {
				meta.Author = string(a)
			}
			if c, ok := info["/Creator"].(pdf.StringObject); ok {
				meta.Creator = string(c)
			}
			if p, ok := info["/Producer"].(pdf.StringObject); ok {
				meta.Producer = string(p)
			}
		}
	}

	// Log if encrypted (attempting decryption with empty password)
	if meta.Encrypted {
		fmt.Fprintf(os.Stderr, "PDF is encrypted. Attempting to decrypt with empty password (owner-password-only PDFs)...\n")
	}

	doc := &model.Document{
		Metadata: meta,
		Pages:    make([]model.Page, 0, reader.NumPages()),
	}

	// 4. Iterate Pages and Extract Text
	numPages := reader.NumPages()
	fmt.Fprintf(os.Stderr, "Processing %d pages...\n", numPages)

	for i := 0; i < numPages; i++ {
		start := time.Now()

		// Get Page Dictionary
		pdfPage, err := reader.GetPage(i)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error getting page %d: %v\n", i+1, err)
			continue
		}

		// Initialize Extractor for this page
		extractor, err := pdf.NewExtractor(reader, pdfPage)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error creating extractor for page %d: %v\n", i+1, err)
			continue
		}

		// Extract!
		text, err := extractor.ExtractText()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error extracting text from page %d: %v\n", i+1, err)
			continue
		}

		// Basic dimensions (MediaBox)
		var width, height float64
		if mBox, ok := pdfPage["/MediaBox"].(pdf.ArrayObject); ok && len(mBox) == 4 {
			// [x1 y1 x2 y2] -> width = x2-x1, height = y2-y1
			// Simplified: assume x1,y1 are 0
			if w, ok := mBox[2].(pdf.NumberObject); ok {
				width = float64(w)
			}
			if h, ok := mBox[3].(pdf.NumberObject); ok {
				height = float64(h)
			}
		}

		doc.Pages = append(doc.Pages, model.Page{
			PageNumber: i + 1,
			Content:    text,
			CharCount:  len(text),
			Width:      width,
			Height:     height,
		})

		fmt.Fprintf(os.Stderr, "Page %d processed in %v (%d chars)\n", i+1, time.Since(start), len(text))
	}

	return doc, nil
}

// LoadPDFConcurrent loads a PDF and extracts text using concurrent page processing.
// The workers parameter specifies the number of concurrent workers (0 = auto-detect using NumCPU).
func LoadPDFConcurrent(path string, workers int) (*model.Document, error) {
	// 1. Open File to get metadata and page count
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	// 2. Initialize the Low-Level Reader
	reader, err := pdf.NewReader(f)
	if err != nil {
		return nil, fmt.Errorf("failed to create pdf reader: %w", err)
	}

	// 3. Extract Metadata
	meta := model.Metadata{
		Encrypted: reader.IsEncrypted(),
	}

	if !meta.Encrypted {
		if info, err := reader.GetInfo(); err == nil && info != nil {
			if t, ok := info["/Title"].(pdf.StringObject); ok {
				meta.Title = string(t)
			}
			if a, ok := info["/Author"].(pdf.StringObject); ok {
				meta.Author = string(a)
			}
			if c, ok := info["/Creator"].(pdf.StringObject); ok {
				meta.Creator = string(c)
			}
			if p, ok := info["/Producer"].(pdf.StringObject); ok {
				meta.Producer = string(p)
			}
		}
	}

	if meta.Encrypted {
		fmt.Fprintf(os.Stderr, "PDF is encrypted. Attempting to decrypt with empty password (owner-password-only PDFs)...\n")
	}

	numPages := reader.NumPages()
	fmt.Fprintf(os.Stderr, "Processing %d pages concurrently...\n", numPages)

	// 4. Process pages concurrently
	return loadPDFParallel(path, meta, numPages, workers)
}

// loadPDFParallel implements the worker pool pattern for concurrent page extraction
func loadPDFParallel(path string, meta model.Metadata, numPages int, workers int) (*model.Document, error) {
	// 1. Determine worker count
	if workers <= 0 {
		workers = runtime.NumCPU()
	}
	if workers > numPages {
		workers = numPages
	}

	// 2. Create channels
	pageIndices := make(chan int, numPages)
	results := make(chan pageResult, numPages)

	// 3. Launch workers
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			// Each worker opens its own file handle
			f, err := os.Open(path)
			if err != nil {
				// Try to send error for first page
				select {
				case idx := <-pageIndices:
					results <- pageResult{pageNum: idx, err: err}
				default:
				}
				return
			}
			defer f.Close()

			// Create reader for this worker
			reader, err := pdf.NewReader(f)
			if err != nil {
				select {
				case idx := <-pageIndices:
					results <- pageResult{pageNum: idx, err: err}
				default:
				}
				return
			}

			// Process pages from the channel
			for pageIdx := range pageIndices {
				start := time.Now()

				pdfPage, err := reader.GetPage(pageIdx)
				if err != nil {
					results <- pageResult{pageNum: pageIdx, err: err}
					continue
				}

				extractor, err := pdf.NewExtractor(reader, pdfPage)
				if err != nil {
					results <- pageResult{pageNum: pageIdx, err: err}
					continue
				}

				text, err := extractor.ExtractText()
				if err != nil {
					results <- pageResult{pageNum: pageIdx, err: err}
					continue
				}

				var width, height float64
				if mBox, ok := pdfPage["/MediaBox"].(pdf.ArrayObject); ok && len(mBox) == 4 {
					if w, ok := mBox[2].(pdf.NumberObject); ok {
						width = float64(w)
					}
					if h, ok := mBox[3].(pdf.NumberObject); ok {
						height = float64(h)
					}
				}

				page := model.Page{
					PageNumber: pageIdx + 1,
					Content:    text,
					CharCount:  len(text),
					Width:      width,
					Height:     height,
				}

				fmt.Fprintf(os.Stderr, "Page %d processed in %v (%d chars)\n",
					pageIdx+1, time.Since(start), len(text))

				results <- pageResult{pageNum: pageIdx, page: page, err: nil}
			}
		}()
	}

	// 4. Send page indices to workers
	go func() {
		for i := 0; i < numPages; i++ {
			pageIndices <- i
		}
		close(pageIndices)
	}()

	// 5. Wait for workers to finish
	go func() {
		wg.Wait()
		close(results)
	}()

	// 6. Collect results
	pages := make([]model.Page, numPages)
	errorCount := 0

	for result := range results {
		if result.err != nil {
			fmt.Fprintf(os.Stderr, "Error processing page %d: %v\n", result.pageNum+1, result.err)
			errorCount++
			continue
		}
		pages[result.pageNum] = result.page
	}

	// 7. Filter out empty pages (from errors)
	validPages := make([]model.Page, 0, numPages-errorCount)
	for _, page := range pages {
		if page.PageNumber > 0 { // Skip uninitialized pages
			validPages = append(validPages, page)
		}
	}

	return &model.Document{
		Metadata: meta,
		Pages:    validPages,
	}, nil
}
