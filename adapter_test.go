package gofastpdf_test

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/AOShei/go-fast-pdf/internal/pdftest"
	"github.com/AOShei/go-fast-pdf/pkg/loader"
	"github.com/AOShei/go-fast-pdf/pkg/model"
)

// This file is the only place where the conformance tests touch the
// library's API. When the API changes (Phase 5: pkg/loader is replaced by
// gofastpdf.Extract and the shared document model), this file changes and
// the tests do not.

// extraction is what the tests look at.
type extraction struct {
	Pages         []pageOut
	Title, Author string
	Encrypted     bool
}

type pageOut struct {
	Number        int
	Text          string
	Width, Height float64
	// Failed marks a page that is present in the document but could not
	// be read. The pkg/loader API cannot express this; it drops the page.
	Failed bool
	// Chars is the library's own character count for the page.
	Chars  int
	Images int
}

type extractOpts struct {
	Password string
	Images   bool
	Workers  int // 0 or 1: sequential
}

// errNoPassword is returned when a test asks for a capability the current
// API does not have.
var errNoPassword = errors.New("the API has no password parameter")

func extractFile(path string, o extractOpts) (*extraction, error) {
	if o.Password != "" {
		return nil, errNoPassword
	}
	var doc *model.Document
	var err error
	if o.Workers > 1 {
		doc, err = loader.LoadPDFConcurrent(path, o.Workers, o.Images)
	} else {
		doc, err = loader.LoadPDF(path, o.Images)
	}
	if err != nil {
		return nil, err
	}
	out := &extraction{Title: doc.Metadata.Title, Author: doc.Metadata.Author, Encrypted: doc.Metadata.Encrypted}
	for _, p := range doc.Pages {
		po := pageOut{Number: p.PageNumber, Text: p.Content, Width: p.Width, Height: p.Height, Chars: p.CharCount}
		if p.Images != nil {
			po.Images = len(*p.Images)
		}
		if !utf8.ValidString(p.Content) {
			return out, fmt.Errorf("page %d: text is not valid UTF-8", p.PageNumber)
		}
		out.Pages = append(out.Pages, po)
	}
	return out, nil
}

// tempPattern names the temporary files of these tests. TestMain sweeps
// stale ones by this pattern, so it must not match anything else.
const tempPattern = "gofastpdf-*.pdf"

func extractBytes(data []byte, o extractOpts) (*extraction, error) {
	f, err := os.CreateTemp("", tempPattern)
	if err != nil {
		return nil, err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(data); err != nil {
		f.Close()
		return nil, err
	}
	if err := f.Close(); err != nil {
		return nil, err
	}
	return extractFile(f.Name(), o)
}

// pageTexts returns the text of each page.
func (e *extraction) pageTexts() []string {
	out := make([]string, len(e.Pages))
	for i, p := range e.Pages {
		out[i] = p.Text
	}
	return out
}

// words returns the document's words joined by single spaces, which is
// the form the defect tests compare.
func (e *extraction) words() string {
	return strings.Join(pdftest.Words(strings.Join(e.pageTexts(), "\n")), " ")
}

// textOf extracts data and returns its words, or an error.
func textOf(data []byte) (string, error) {
	e, err := extractBytes(data, extractOpts{})
	if err != nil {
		return "", err
	}
	return e.words(), nil
}

// wantText is the body of most defect tests: the document must extract
// without error to exactly these words.
func wantText(data []byte, want string) func() error {
	return func() error {
		got, err := textOf(data)
		if err != nil {
			return fmt.Errorf("extraction failed: %w", err)
		}
		if got != want {
			return fmt.Errorf("text is %q, want %q", got, want)
		}
		return nil
	}
}
