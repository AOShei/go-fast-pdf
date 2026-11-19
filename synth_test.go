package gofastpdf_test

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	gofastcore "github.com/AOShei/go-fast-core"
	gofastpdf "github.com/AOShei/go-fast-pdf"
	"github.com/AOShei/go-fast-pdf/internal/pdftest"
)

// synthPage is one page of a synthetic document.
type synthPage struct {
	Content string
	// Resources are entries of the resource dictionary besides the font
	// /F1, and Fonts further entries of its font dictionary.
	Resources, Fonts string
	// Extra are further entries of the page dictionary.
	Extra    string
	MediaBox string
}

// synthDoc describes a synthetic document for the tests of the layout
// and of the document-level features.
type synthDoc struct {
	// Objects are added before the document itself; their numbers start
	// at 1. A stream is written with pdftest.Stream.
	Objects []string
	// Catalog are further entries of the document catalog.
	Catalog string
	// Font is the dictionary of /F1, Helvetica if empty.
	Font  string
	Pages []synthPage
	// Info are the entries of the document information dictionary; none
	// if empty.
	Info string
}

func (d synthDoc) bytes() []byte {
	b := pdftest.New()
	for _, o := range d.Objects {
		b.Add(o)
	}
	n := len(d.Objects)
	// Doc adds the catalog, the page tree and the font next.
	catalog, tree, font := n+1, n+2, n+3
	specs := make([]pdftest.PageSpec, len(d.Pages))
	for i, p := range d.Pages {
		specs[i] = pdftest.PageSpec{
			Content:   p.Content,
			Resources: fmt.Sprintf("/Font << /F1 %d 0 R %s >> %s", font, p.Fonts, p.Resources),
			Extra:     p.Extra,
			MediaBox:  p.MediaBox,
		}
	}
	pdftest.Doc(pdftest.DocOptions{Base: b, Font: d.Font}, specs...)
	if d.Catalog != "" {
		b.Set(catalog, fmt.Sprintf("<< /Type /Catalog /Pages %d 0 R %s >>", tree, d.Catalog))
	}
	if d.Info != "" {
		b.Trailer += fmt.Sprintf(" /Info %d 0 R", b.Add("<< "+d.Info+" >>"))
	}
	return b.Bytes()
}

// onePage is a document of one page with the given content.
func onePage(content string) synthDoc { return synthDoc{Pages: []synthPage{{Content: content}}} }

// mustExtract extracts data and checks the result against the contract
// of the shared model.
func mustExtract(t testing.TB, data []byte, opts *gofastcore.Options) *gofastcore.Document {
	t.Helper()
	if opts == nil {
		opts = &gofastcore.Options{}
	}
	doc, err := gofastpdf.Extract(context.Background(), bytes.NewReader(data), int64(len(data)), opts)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if err := checkContract(doc, opts); err != nil {
		t.Fatalf("contract: %v", err)
	}
	return doc
}

// describe lists the blocks of a part, one per line, for failure
// messages and for comparing two extractions.
func describe(p *gofastcore.Part) string {
	var sb strings.Builder
	for i := range p.Blocks {
		b := &p.Blocks[i]
		r := b.Loc.BBox
		fmt.Fprintf(&sb, "%s [%s] (%.1f %.1f %.1f %.1f) %q\n", b.Kind, b.Hidden, r.X0, r.Y0, r.X1, r.Y1, b.Text)
	}
	return sb.String()
}

// visibleBlocks describes the text blocks of a part that carry no
// hidden reason.
func visibleBlocks(p *gofastcore.Part) string {
	q := *p
	q.Blocks = nil
	for i := range p.Blocks {
		if p.Blocks[i].Hidden == 0 && p.Blocks[i].Kind != gofastcore.KindImage {
			q.Blocks = append(q.Blocks, p.Blocks[i])
		}
	}
	return describe(&q)
}

// texts returns the text of each block of a part, with the hidden
// reasons in front of the hidden ones: "tiny: text".
func texts(p *gofastcore.Part) []string {
	var out []string
	for i := range p.Blocks {
		b := &p.Blocks[i]
		switch {
		case b.Kind == gofastcore.KindImage:
			out = append(out, "<image>")
		case b.Hidden != 0:
			out = append(out, b.Hidden.String()+": "+b.Text)
		case b.Marker != "":
			// As the renderers write it.
			out = append(out, b.Marker+" "+b.Text)
		default:
			out = append(out, b.Text)
		}
	}
	return out
}

// textBlocks is texts without the image blocks: for a test of text on
// a page that has images, whatever the image mode is.
func textBlocks(p *gofastcore.Part) []string {
	var out []string
	for _, s := range texts(p) {
		if s != "<image>" {
			out = append(out, s)
		}
	}
	return out
}

func warning(ws []gofastcore.Warning, code string) (gofastcore.Warning, bool) {
	for _, w := range ws {
		if w.Code == code {
			return w, true
		}
	}
	return gofastcore.Warning{}, false
}

func codes(ws []gofastcore.Warning) []string {
	out := []string{}
	for _, w := range ws {
		out = append(out, w.Code)
	}
	return out
}

// A paragraph of ordinary visible text in three lines, set with
// Helvetica 12 from the top of the page, and its text as one block.
const (
	visibleLines = "BT /F1 12 Tf 72 720 Td 14 TL (The committee met on Tuesday to review the budget) Tj T* " +
		"(for the coming year and agreed on most of the items) Tj T* (that had been put before it.) Tj ET\n"
	visibleText = "The committee met on Tuesday to review the budget for the coming year and agreed on most of the items that had been put before it."
)

// grayImage is a one-pixel gray image XObject.
var grayImage = pdftest.Stream("/Type /XObject /Subtype /Image /Width 1 /Height 1 /ColorSpace /DeviceGray /BitsPerComponent 8", "\x80")

// fontCourier is a font in which every character is 0.6 of the font
// size wide, so that a test can say where text ends.
const fontCourier = "<< /Type /Font /Subtype /Type1 /BaseFont /Courier >>"

// textLine is one line of text with the place its baseline starts.
type textLine struct {
	x, y float64
	s    string
}

// stack returns lines set one under the other from (x, y).
func stack(x, y, leading float64, lines ...string) []textLine {
	out := make([]textLine, len(lines))
	for i, s := range lines {
		out[i] = textLine{x, y - float64(i)*leading, s}
	}
	return out
}

// draw returns the content that sets the lines in /F1 at the given
// size, in the order given.
func draw(size float64, lines ...textLine) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "BT /F1 %g Tf\n", size)
	for _, l := range lines {
		fmt.Fprintf(&sb, "1 0 0 1 %g %g Tm (%s) Tj\n", l.x, l.y, pdftest.EscapeString(l.s))
	}
	sb.WriteString("ET\n")
	return sb.String()
}

// blockTexts extracts a one-page document set in Courier and returns
// the texts of its blocks.
func blockTexts(t testing.TB, content string) []string {
	t.Helper()
	d := synthDoc{Font: fontCourier, Pages: []synthPage{{Content: content}}}
	return texts(&mustExtract(t, d.bytes(), nil).Parts[0])
}
