package gofastpdf_test

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	gofastcore "github.com/AOShei/go-fast-core"
	gofastpdf "github.com/AOShei/go-fast-pdf"
)

// The whole document at once, from anything that can be read at an
// offset.
func ExampleExtract() {
	data, err := os.ReadFile("testdata/fixtures/english.pdf")
	if err != nil {
		log.Fatal(err)
	}
	doc, err := gofastpdf.Extract(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(doc.Metadata.Title)
	fmt.Println(len(doc.Parts), "pages")
	fmt.Println(doc.Parts[0].Blocks[0].Text)
	// Output:
	// Sample document
	// 3 pages
	// Extracting text from documents
}

// The whole document of a file on disk, with options.
func ExampleExtractFile() {
	opts := &gofastcore.Options{Parts: gofastcore.PartRange{First: 2, Last: 2}}
	doc, err := gofastpdf.ExtractFile(context.Background(), "testdata/fixtures/english.pdf", opts)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(doc.Source.Name)
	for _, part := range doc.Parts {
		fmt.Printf("page %d: %d blocks, %d words\n", part.Number, len(part.Blocks), len(strings.Fields(part.Text())))
	}
	// Output:
	// english.pdf
	// page 2: 6 blocks, 356 words
}

// Page by page, without holding the document: each part is handed out
// as soon as its turn comes, and several pages are read at a time.
func ExampleOpen() {
	f, err := gofastpdf.Open("testdata/fixtures/english.pdf", nil)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	fmt.Println(f.NumPages(), "pages by", f.Metadata().Authors[0])
	for part, err := range f.Parts(context.Background()) {
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("page %d: %d blocks\n", part.Number, len(part.Blocks))
	}
	// Output:
	// 3 pages by go-fast-pdf tests
	// page 1: 6 blocks
	// page 2: 6 blocks
	// page 3: 2 blocks
}

// A file that is already in memory.
func ExampleOpenBytes() {
	data, err := os.ReadFile("testdata/fixtures/english.pdf")
	if err != nil {
		log.Fatal(err)
	}
	f, err := gofastpdf.OpenBytes(data, nil)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	fmt.Println(f.NumPages())
	// Output:
	// 3
}

// A file read from anything that can be read at an offset.
func ExampleOpenReaderAt() {
	in, err := os.Open("testdata/fixtures/english.pdf")
	if err != nil {
		log.Fatal(err)
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		log.Fatal(err)
	}
	f, err := gofastpdf.OpenReaderAt(in, info.Size(), nil)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	fmt.Println(f.Metadata().Title)
	// Output:
	// Sample document
}

// One page by its number, from any goroutine.
func ExampleFile_Page() {
	f, err := gofastpdf.Open("testdata/fixtures/english.pdf", nil)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	page, err := f.Page(3)
	if err != nil {
		log.Fatal(err)
	}
	part, err := page.Part()
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("page %d is %.0f by %.0f points and has %d blocks\n", part.Number, part.Width, part.Height, len(part.Blocks))
	// Output:
	// page 3 is 595 by 842 points and has 2 blocks
}

// The lines of a page, with their fonts and the boxes of their words.
func ExamplePage_Lines() {
	f, err := gofastpdf.Open("testdata/fixtures/english.pdf", nil)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	page, err := f.Page(1)
	if err != nil {
		log.Fatal(err)
	}
	lines, err := page.Lines()
	if err != nil {
		log.Fatal(err)
	}
	title := lines[0]
	fmt.Printf("%q in %s at %.0f points\n", title.Text, title.Font, title.Size)
	for _, word := range title.Words {
		fmt.Printf("%-10s from %3.0f to %3.0f\n", word.Text, word.BBox.X0, word.BBox.X1)
	}
	// Output:
	// "Extracting text from documents" in Times-Roman at 15 points
	// Extracting from  72 to 134
	// text       from 138 to 161
	// from       from 164 to 193
	// documents  from 197 to 262
}

// The whole document from an open file, as Extract gives it.
func ExampleFile_Document() {
	f, err := gofastpdf.Open("testdata/fixtures/english.pdf", nil)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	doc, err := f.Document(context.Background())
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(len(doc.Parts), "parts")
	// Output:
	// 3 parts
}

// What the file declares about how text is shown is on its blocks:
// hidden for content the file switches off, no_ink for text drawn with
// no paint. Such text is extracted like any other.
func ExampleFile_Parts_display() {
	f, err := gofastpdf.Open("testdata/fixtures/english.pdf", nil)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	declared := 0
	for part, err := range f.Parts(context.Background()) {
		if err != nil {
			log.Fatal(err)
		}
		for _, block := range part.Blocks {
			if block.Display != 0 {
				declared++
			}
		}
	}
	fmt.Println(declared, "blocks with a display reason")
	// Output:
	// 0 blocks with a display reason
}

// Settings that only PDF has are fields of an Extractor.
func ExampleExtractor() {
	e := &gofastpdf.Extractor{MaxStreamBytes: 8 << 20}
	doc, err := e.ExtractFile(context.Background(), "testdata/fixtures/english.pdf", nil)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(e.Formats(), len(doc.Parts))
	// Output:
	// [pdf xps oxps] 3
}
