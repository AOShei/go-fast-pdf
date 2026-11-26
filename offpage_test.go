package gofastpdf_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	gofastpdf "github.com/AOShei/go-fast-pdf"
)

// Extractor.NoOffPageText leaves out the text that is wholly outside
// the page's visible box: outside the crop box though inside the media
// box, and outside both. Text that is partly on the page stays, and
// without the setting all of it is in the document.
func TestNoOffPageText(t *testing.T) {
	content := set("F1", 12, 100, 700, "Printed on the page") +
		set("F1", 12, 100, 30, "In the margin that is cut off") +
		set("F1", 12, 100, -300, "Below every box") +
		set("F1", 12, 2000, 700, "Far to the right") +
		set("F1", 12, 480, 600, "Runs over the edge of the page")
	data := synthDoc{Pages: []synthPage{{Content: content, MediaBox: "[0 0 612 792] /CropBox [50 50 562 742]"}}}.bytes()
	inside := []string{"Printed on the page", "Runs over the edge of the page"}
	outside := []string{"In the margin that is cut off", "Below every box", "Far to the right"}
	for _, off := range []bool{false, true} {
		e := &gofastpdf.Extractor{NoOffPageText: off}
		doc, err := e.Extract(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
		if err != nil {
			t.Fatal(err)
		}
		var sb strings.Builder
		for _, b := range doc.Parts[0].Blocks {
			sb.WriteString(b.Text + "\n")
		}
		text := sb.String()
		for _, s := range inside {
			if !strings.Contains(text, s) {
				t.Errorf("NoOffPageText %v: %q is not in the page\n%s", off, s, text)
			}
		}
		for _, s := range outside {
			if strings.Contains(text, s) == off {
				t.Errorf("NoOffPageText %v: %q in the page: %v\n%s", off, s, !off, text)
			}
		}
		// A page read by itself is the same.
		f, err := e.OpenBytes(data, nil)
		if err != nil {
			t.Fatal(err)
		}
		p, err := f.Page(1)
		if err != nil {
			t.Fatal(err)
		}
		part, err := p.Part()
		if err != nil {
			t.Fatal(err)
		}
		sb.Reset()
		for _, b := range part.Blocks {
			sb.WriteString(b.Text + "\n")
		}
		if sb.String() != text {
			t.Errorf("NoOffPageText %v: the page by itself:\n%s\nin the document:\n%s", off, sb.String(), text)
		}
		f.Close()
	}
}
