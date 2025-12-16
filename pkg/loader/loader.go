package loader

import (
	"fmt"
	"os"
	"time"

	"github.com/AOShei/pdf-loader/pkg/model"
	"github.com/AOShei/pdf-loader/pkg/pdf"
)

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
	meta := model.Metadata{}
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

	doc := &model.Document{
		Metadata: meta,
		Pages:    make([]model.Page, 0, reader.NumPages()),
	}

	// 4. Iterate Pages and Extract Text
	numPages := reader.NumPages()
	fmt.Printf("Processing %d pages...\n", numPages)

	for i := 0; i < numPages; i++ {
		start := time.Now()

		// Get Page Dictionary
		pdfPage, err := reader.GetPage(i)
		if err != nil {
			fmt.Printf("Error getting page %d: %v\n", i+1, err)
			continue
		}

		// Initialize Extractor for this page
		extractor, err := pdf.NewExtractor(reader, pdfPage)
		if err != nil {
			fmt.Printf("Error creating extractor for page %d: %v\n", i+1, err)
			continue
		}

		// Extract!
		text, err := extractor.ExtractText()
		if err != nil {
			fmt.Printf("Error extracting text from page %d: %v\n", i+1, err)
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

		fmt.Printf("Page %d processed in %v (%d chars)\n", i+1, time.Since(start), len(text))
	}

	return doc, nil
}
