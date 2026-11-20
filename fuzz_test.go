package gofastpdf_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"unicode"

	gofastcore "github.com/AOShei/go-fast-core"
	gofastpdf "github.com/AOShei/go-fast-pdf"
	"github.com/AOShei/go-fast-pdf/internal/pdftest"
)

// FuzzOpen feeds whole files to the library through its public entry
// point. The engine's own targets (FuzzParser, FuzzScanner, FuzzCMap,
// FuzzOpen and FuzzExtract under internal/) reach more inputs per second;
// this one checks that nothing is lost between them and the caller.
func FuzzOpen(f *testing.F) {
	f.Add(pdftest.OnePage(pdftest.TextPage("Hello, world")))
	f.Add(pdftest.Doc(pdftest.DocOptions{Compress: true, Configure: func(b *pdftest.Builder) { b.ObjStm = true }},
		pdftest.PageSpec{Content: pdftest.TextPage("object streams")}).Bytes())
	// Annotations with appearances, notes and form fields.
	f.Add(formDoc().bytes())
	// Scripts, a metadata packet, entries of the file's own making, fields
	// on no page.
	f.Add(storedDoc().bytes())
	f.Fuzz(func(t *testing.T, data []byte) {
		// An error is fine. A document that breaks the contract of the
		// shared model is not, and neither is one that depends on the
		// number of workers. Every other input is read with the checks
		// of DeepHidden.
		deep := len(data)%2 == 1
		one, err := extractBytes(data, extractOpts{Images: true, Deep: deep})
		if errors.Is(err, errContract) {
			t.Fatal(err)
		}
		if err != nil {
			return
		}
		many, err := extractBytes(data, extractOpts{Images: true, Workers: 4, Deep: deep})
		if err != nil {
			t.Fatalf("with 4 workers: %v", err)
		}
		if diff := sameDocument(one.Doc, many.Doc); diff != "" {
			t.Fatalf("1 worker and 4 workers: %s", diff)
		}
	})
}

// FuzzPage feeds content streams to the whole library: the interpreter,
// the layout and the assembly of the document. Whatever a page draws,
// the document keeps the contract of the shared model and is the same
// for any number of workers.
func FuzzPage(f *testing.F) {
	for _, seed := range []string{
		visibleLines,
		visibleLines + "BT /F1 12 Tf 3 Tr 72 500 Td (hidden) Tj ET",
		"q 612 0 0 792 0 0 cm /Im1 Do Q BT /F1 12 Tf 3 Tr 72 720 Td (layer) Tj ET",
		"BT /F1 10 Tf 4 Tc 1 0 0 1 72 700 Tm (CONTENTS) Tj ET",
		"BT /F1 10 Tf 1 0 0 1 72 700 Tm (Name:) Tj 150 0 Td (Jane) Tj 1 0 0 1 72 688 Tm (Age:) Tj 150 0 Td (41) Tj ET",
		"BT /F1 1 Tf 72 500 Td (tiny) Tj /F1 12 Tf 1 g 0 -20 Td (white) Tj ET /Fm1 Do q /G0 gs BT /F1 9 Tf 0 1 -1 0 300 300 Tm (turned) Tj ET Q",
		"BT /F1 12 Tf 72 720 Td (inter\255) Tj 0 -14 Td (national well-) Tj 0 -14 Td (known) Tj ET BI /W 1 /H 1 /CS /G /BPC 8 ID x\nEI",
		"0.1 g 0 300 612 300 re f 1 g 60 490 300 30 re f BT /F1 12 Tf 72 500 Td (white on white) Tj ET 0 g 60 400 m 360 400 l 360 430 l 60 430 l h f",
		"BT /F1 12 Tf 72 500 Td (under a box) Tj ET 60 490 300 30 re f q 0 0 10 10 re W n BT /F1 12 Tf 72 300 Td (cut off) Tj ET Q /G1 gs 2 w 5 5 90 90 re B",
		"q 400 300 m 400 355 355 400 300 400 c 245 400 200 355 200 300 c W* n /Im1 Do BT /F1 12 Tf 250 300 Td (in a circle) Tj ET Q /Sh sh /P cs /P1 scn 0 0 9 9 re f",
		draw(10, cat(stack(72, 700, 12, paraA...), stack(320, 700, 12, paraB...))...),
	} {
		f.Add(seed, uint8(0))
	}
	form := pdftest.Stream("/Type /XObject /Subtype /Form /BBox [0 0 100 100] /Matrix [1 0 0 1 50 50]", "BT /F1 12 Tf 10 10 Td (in a form) Tj 0 200 Td (outside its box) Tj ET")
	f.Fuzz(func(t *testing.T, content string, turn uint8) {
		if strings.Contains(content, "endstream") || strings.Contains(content, "endobj") {
			t.Skip()
		}
		d := synthDoc{
			Objects: []string{grayImage, form},
			Pages: []synthPage{{
				Content:   content,
				Resources: "/XObject << /Im1 1 0 R /Fm1 2 0 R >> /ExtGState << /G0 << /ca 0 /CA 0.5 >> /G1 << /BM /Multiply /ca 0.9 >> >> /ColorSpace << /P /Pattern >>",
				Extra:     fmt.Sprintf("/Rotate %d /CropBox [10 10 %d 700]", int(turn%4)*90, 300+int(turn)),
			}},
		}
		data := d.bytes()
		var docs []*gofastcore.Document
		for _, opts := range []gofastcore.Options{{Workers: 1}, {Workers: 3, Images: gofastcore.ImagesData}, {DeepHidden: true, Spans: true}} {
			doc, err := gofastpdf.Extract(context.Background(), bytes.NewReader(data), int64(len(data)), &opts)
			if err != nil {
				t.Fatalf("Extract: %v", err)
			}
			if err := checkContract(doc, &opts); err != nil {
				t.Fatalf("%v\n%s", err, describe(&doc.Parts[0]))
			}
			docs = append(docs, doc)
		}
		// With images the text blocks are the same, with image blocks
		// among them.
		if a, b := visibleText0(docs[0]), visibleText0(docs[1]); a != b {
			t.Fatalf("the text differs with image blocks:\n%q\n%q", a, b)
		}
		// The page read through a file handle: the same part from the
		// iterator and from the page by itself, and lines that are those
		// of its blocks.
		f, err := gofastpdf.OpenBytes(data, &gofastcore.Options{Workers: 1})
		if err != nil {
			t.Fatalf("OpenBytes: %v", err)
		}
		for part, err := range f.Parts(context.Background()) {
			if err != nil || !reflect.DeepEqual(part, docs[0].Parts[0]) {
				t.Fatalf("the iterator's part is not that of Extract (%v)", err)
			}
		}
		page, _ := f.Page(1)
		if part, err := page.Part(); err != nil || !reflect.DeepEqual(part, docs[0].Parts[0]) {
			t.Fatalf("the part of the page by itself is not that of Extract (%v)", err)
		}
		lines, err := page.Lines()
		if err != nil {
			t.Fatalf("Lines: %v", err)
		}
		if diff := linesAgainstPart(lines, &docs[0].Parts[0]); diff != "" {
			t.Fatalf("the lines: %s\n%s", diff, describe(&docs[0].Parts[0]))
		}

		// The checks of DeepHidden hide text, and show none but text
		// that was taken for white on white: what is in view with them is
		// in view without them, or flagged for its contrast alone.
		// Letters are compared, since where words and lines end depends
		// on what is in view.
		seen := map[rune]int{}
		for _, r := range visibleLetters0(docs[0], gofastcore.HiddenLowContrast) {
			seen[r]++
		}
		for _, r := range visibleLetters0(docs[2], 0) {
			if seen[r]--; seen[r] < 0 {
				t.Fatalf("%q is in view with DeepHidden and not without:\n%s\n%s", r, describe(&docs[0].Parts[0]), describe(&docs[2].Parts[0]))
			}
		}
	})
}

// visibleLetters0 returns the letters and digits of the first page that
// are in blocks without a hidden reason other than those of allow.
func visibleLetters0(doc *gofastcore.Document, allow gofastcore.Hidden) []rune {
	var out []rune
	for i := range doc.Parts[0].Blocks {
		if b := &doc.Parts[0].Blocks[i]; b.Hidden&^allow == 0 {
			for _, r := range b.Text {
				if unicode.IsLetter(r) || unicode.IsDigit(r) {
					out = append(out, r)
				}
			}
		}
	}
	return out
}

// visibleText0 returns the text blocks of the first page.
func visibleText0(doc *gofastcore.Document) string {
	var sb strings.Builder
	for i := range doc.Parts[0].Blocks {
		if b := &doc.Parts[0].Blocks[i]; b.Kind == gofastcore.KindParagraph {
			fmt.Fprintf(&sb, "[%s] %s\n", b.Hidden, b.Text)
		}
	}
	return sb.String()
}
