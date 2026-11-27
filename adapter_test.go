package gofastpdf_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"unicode/utf8"

	gofastcore "github.com/AOShei/go-fast-core"
	gofastpdf "github.com/AOShei/go-fast-pdf"
	"github.com/AOShei/go-fast-pdf/internal/pdftest"
)

// This file is where the conformance tests touch the library's API: the
// defect tests, the corpus and the fuzz targets all extract through it,
// and every document that passes through is checked against the shared
// model's contract.

// extraction is what the tests look at.
type extraction struct {
	Pages         []pageOut
	Title, Author string
	Encrypted     bool
	// Doc is the document as the library returned it.
	Doc *gofastcore.Document
}

type pageOut struct {
	Number int
	// Text is the page as plain text, as part.Text() gives it.
	Text          string
	Width, Height float64
	// Failed marks a page that is present in the document but could not
	// be read.
	Failed bool
	// Chars is the number of characters of Text.
	Chars  int
	Images int
}

type extractOpts struct {
	Password string
	Images   bool
	Workers  int // 0 or 1: sequential
	// Spans asks for spans.
	Spans bool
	// Unchecked skips the contract checks, for benchmarks.
	Unchecked bool
}

func (o extractOpts) options() *gofastcore.Options {
	opts := &gofastcore.Options{Password: o.Password, Workers: max(o.Workers, 1), Spans: o.Spans}
	// Said in so many words: an option left out means a block for every
	// image.
	opts.Images = gofastcore.ImagesNone
	if o.Images {
		opts.Images = gofastcore.ImagesMeta
	}
	return opts
}

func extractFile(path string, o extractOpts) (*extraction, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return extractBytes(data, o)
}

func extractBytes(data []byte, o extractOpts) (*extraction, error) {
	doc, err := gofastpdf.Extract(context.Background(), bytes.NewReader(data), int64(len(data)), o.options())
	if err != nil {
		return nil, err
	}
	out := &extraction{Title: doc.Metadata.Title, Encrypted: doc.Metadata.Encrypted, Doc: doc}
	if len(doc.Metadata.Authors) > 0 {
		out.Author = doc.Metadata.Authors[0]
	}
	for i := range doc.Parts {
		p := &doc.Parts[i]
		if p.Kind == gofastcore.PartStored {
			continue // not a page
		}
		po := pageOut{Number: p.Number, Width: p.Width, Height: p.Height, Failed: p.Has(gofastcore.WarnPartFailed)}
		for j := range p.Blocks {
			if p.Blocks[j].Kind == gofastcore.KindImage {
				po.Images++
			}
		}
		po.Text = pageText(p)
		po.Chars = utf8.RuneCountInString(po.Text)
		if !utf8.ValidString(po.Text) {
			return out, fmt.Errorf("%w: page %d: text is not valid UTF-8", errContract, p.Number)
		}
		out.Pages = append(out.Pages, po)
	}
	if !o.Unchecked {
		if err := checkContract(doc, o.options()); err != nil {
			return out, fmt.Errorf("%w: %w", errContract, err)
		}
	}
	return out, nil
}

// pageText is the text that a page shows: what its content draws and
// what its annotations display, which the reference reader gives too.
// Two things are in the part and not here, because the reference reader
// does not read them and a score against it would count them as wrong:
// what is written in a note, which is shown beside the page if at all,
// the value of a form field whose appearance shows nothing, and the
// scripts of the page.
func pageText(p *gofastcore.Part) string {
	extra := func(b *gofastcore.Block) bool {
		_, field := b.Attrs[gofastcore.AttrField]
		_, stored := b.Attrs[gofastcore.AttrStored]
		return field || stored || b.Kind == gofastcore.KindNote
	}
	q := *p
	q.Blocks = nil
	for i := range p.Blocks {
		if !extra(&p.Blocks[i]) {
			q.Blocks = append(q.Blocks, p.Blocks[i])
		}
	}
	// With the running headers and footers, which the reference reader
	// gives as text of the page, and which the renderers leave out
	// unless asked.
	var sb strings.Builder
	(&gofastcore.Document{Parts: []gofastcore.Part{q}}).WriteText(&sb, &gofastcore.RenderOptions{Furniture: true})
	return sb.String()
}

// errContract marks a result that the library must never return: a
// document that breaks the contract of the shared model. An input that
// cannot be read is an error of another kind and is no defect.
var errContract = errors.New("the document breaks the contract of the shared model")

// checkContract checks what must hold for every document this library
// returns, whatever the input (section 4.2 of the plan).
func checkContract(doc *gofastcore.Document, opts *gofastcore.Options) error {
	if err := doc.Validate(); err != nil {
		return err
	}
	// A recover stopped a panic: a defect, whatever the input
	// (internalError).
	var internal func(d *gofastcore.Document) error
	internal = func(d *gofastcore.Document) error {
		ws := d.Warnings
		for i := range d.Parts {
			ws = append(ws[:len(ws):len(ws)], d.Parts[i].Warnings...)
		}
		for _, w := range ws {
			if strings.Contains(w.Message, "internal error") {
				return fmt.Errorf("%s: %s", w.Code, w.Message)
			}
		}
		for _, c := range d.Children {
			if err := internal(c); err != nil {
				return err
			}
		}
		return nil
	}
	if err := internal(doc); err != nil {
		return err
	}
	// The JSON form decodes back to an equal document and has no null
	// where a list is expected.
	enc, err := json.Marshal(doc)
	if err != nil {
		return fmt.Errorf("JSON: %w", err)
	}
	if bytes.Contains(enc, []byte(`"parts":null`)) || bytes.Contains(enc, []byte(`"blocks":null`)) {
		return fmt.Errorf("JSON has a null list")
	}
	var back gofastcore.Document
	if err := json.Unmarshal(enc, &back); err != nil {
		return fmt.Errorf("JSON does not decode: %w", err)
	}
	if again, err := json.Marshal(&back); err != nil || !bytes.Equal(enc, again) {
		return fmt.Errorf("the document changes when it is written as JSON and read back (%v)", err)
	}
	// The format and its media type go together.
	switch [2]string{doc.Source.Format, doc.Source.MIME} {
	case [2]string{gofastpdf.Format, "application/pdf"},
		[2]string{gofastpdf.FormatXPS, "application/vnd.ms-xpsdocument"},
		[2]string{gofastpdf.FormatOpenXPS, "application/oxps"}:
	default:
		return fmt.Errorf("source format %q with media type %q", doc.Source.Format, doc.Source.MIME)
	}
	// The rendered text holds the words of the blocks, all of them and
	// in the order the renderers write them, and the Markdown is there
	// when the text is. The
	// renderers leave running headers and footers out, and stored data,
	// in the stored part or on a page (a page's scripts), unless asked
	// for it.
	// The renderers hold the footnotes that follow a block until the
	// next block they write: one that continues the block before the
	// footnotes is written first, and the footnotes after it.
	rendered := func(stored bool) (out []string) {
		var held []string // the words of the footnotes that wait
		before, cols := gofastcore.KindInvalid, 0
		for _, b := range doc.AllBlocks() {
			if b.Kind.Furniture() || !stored && b.Attrs[gofastcore.AttrStored] != "" {
				continue
			}
			var w []string
			// The renderers put a marker back in front of its block, and a
			// dash in front of a list item without one.
			w = append(w, strings.Fields(b.Marker)...)
			if b.Kind == gofastcore.KindListItem && b.Marker == "" && !b.Continued {
				w = append(w, "-")
			}
			w = append(w, strings.Fields(b.Text)...)
			if b.Table != nil { // a table row by row, cell by cell
				for _, row := range b.Table.Rows {
					for _, cell := range row {
						w = append(w, strings.Fields(cell)...)
					}
				}
			}
			if len(w) == 0 {
				continue // not written
			}
			if b.Kind == gofastcore.KindFootnote && before != gofastcore.KindInvalid {
				held = append(held, w...)
				continue
			}
			joins := b.Continued && b.Kind == before && (b.Table == nil || len(b.Table.Rows) > 0 && len(b.Table.Rows[0]) == cols)
			if !joins {
				out, held = append(out, held...), held[:0]
			}
			out = append(out, w...)
			if before = b.Kind; b.Table != nil && len(b.Table.Rows) > 0 {
				cols = len(b.Table.Rows[0])
			}
		}
		return append(out, held...)
	}
	words, withStored := rendered(false), rendered(true)
	text := doc.Text()
	if got := strings.Fields(text); !reflect.DeepEqual(got, words) && len(got)+len(words) > 0 {
		return fmt.Errorf("the rendered text has %d words and the blocks have %d, or they differ", len(got), len(words))
	}
	var all strings.Builder
	if err := doc.WriteText(&all, &gofastcore.RenderOptions{Stored: true}); err != nil {
		return err
	}
	if got := strings.Fields(all.String()); !reflect.DeepEqual(got, withStored) && len(got)+len(withStored) > 0 {
		return fmt.Errorf("the text rendered with stored data has %d words and the blocks have %d, or they differ", len(got), len(withStored))
	}
	// Markdown shows an image where the plain text has nothing.
	if opts.Images == gofastcore.ImagesNone && (strings.TrimSpace(doc.Markdown()) == "") != (strings.TrimSpace(text) == "") {
		return fmt.Errorf("the Markdown is empty and the text is not, or the other way round")
	}
	// No warning says that the document or a part has text it does not
	// show: the reasons are on the blocks, and that is all.
	for _, w := range doc.AllWarnings() {
		if w.Code == "text.hidden" || w.Code == "text.unshown_outlier" || strings.HasPrefix(w.Code, "pdf.deep_hidden") {
			return fmt.Errorf("warning %s", w.Code)
		}
	}
	for i := range doc.Parts {
		p := &doc.Parts[i]
		if p.Kind == gofastcore.PartStored {
			// What the file holds and shows nowhere: the last part, with
			// no display reason, every block saying what it is.
			if i != len(doc.Parts)-1 || p.Display != 0 || len(p.Blocks) == 0 && !p.Has(gofastcore.WarnLimitExceeded) {
				return fmt.Errorf("the stored part is part %d of %d, display %q, with %d blocks", i+1, len(doc.Parts), p.Display, len(p.Blocks))
			}
			for j := range p.Blocks {
				if b := &p.Blocks[j]; b.Attrs[gofastcore.AttrStored] == "" || b.Display != 0 || b.Origin != gofastcore.OriginNative {
					return fmt.Errorf("the stored part, block %d: %s with attributes %v, display %q, origin %s", j, b.Kind, b.Attrs, b.Display, b.Origin)
				}
			}
			continue
		}
		if p.Kind != gofastcore.PartPage {
			return fmt.Errorf("part %d is a %s", p.Number, p.Kind)
		}
		text := false
		for j := range p.Blocks {
			b := &p.Blocks[j]
			where := fmt.Sprintf("page %d, block %d", p.Number, j)
			// The one stored data a page holds is its scripts: the
			// renderers leave out every block marked stored, so anything
			// else marked so would vanish from the page's text.
			if s := b.Attrs[gofastcore.AttrStored]; s != "" && (s != gofastcore.StoredMacro || b.Kind != gofastcore.KindCode) {
				return fmt.Errorf("%s: a %s block marked stored %q", where, b.Kind, s)
			}
			switch b.Kind {
			case gofastcore.KindImage:
				if opts.Images == gofastcore.ImagesNone {
					return fmt.Errorf("%s: an image block without the option", where)
				}
				if b.Origin != gofastcore.OriginNative || b.Image.ID == "" {
					return fmt.Errorf("%s: image block with origin %s and ID %q", where, b.Origin, b.Image.ID)
				}
				if opts.Images != gofastcore.ImagesData && b.Image.Data != nil {
					return fmt.Errorf("%s: image data without the option", where)
				}
				continue
			case gofastcore.KindParagraph:
				text = true
			case gofastcore.KindHeading:
				// Found by its type, or named by the outline.
				text = true
				if b.Level < 1 || b.Level > 6 {
					return fmt.Errorf("%s: a heading of level %d", where, b.Level)
				}
				continue
			case gofastcore.KindListItem:
				// A paragraph that starts with the marker of an item, or
				// an item of the structure tree of a tagged PDF, whose
				// marker can be drawn rather than written.
				text = true
				if tree := b.Origin == gofastcore.OriginNative; b.Level < 1 || !tree && (b.Level > 6 || b.Marker == "") || b.Text == "" {
					return fmt.Errorf("%s: a list item of level %d, marker %q and text %q", where, b.Level, b.Marker, b.Text)
				}
			case gofastcore.KindTable:
				// A table of a tagged PDF, which the structure tree says
				// is one, or found by how its text lines up. Its cells
				// are the text.
				text = true
				if b.Table == nil || b.Text != "" || b.Display != 0 {
					return fmt.Errorf("%s: a table with origin %s, text %q and display %q", where, b.Origin, b.Text, b.Display)
				}
				continue
			case gofastcore.KindHeader, gofastcore.KindFooter:
				// Running headers and footers: text of the page that the
				// layout found in its margins and on the pages around it.
				text = true
			case gofastcore.KindFootnote:
				// A small paragraph under the text that starts with the
				// mark of a note: the mark is its marker.
				text = true
				if b.Marker == "" || b.Text == "" || b.Display != 0 {
					return fmt.Errorf("%s: a footnote with marker %q, text %q and display %q", where, b.Marker, b.Text, b.Display)
				}
			case gofastcore.KindCode:
				// A script of the page's actions: stored, with no display
				// reason; the attribute says what it is.
				if b.Attrs[gofastcore.AttrStored] != gofastcore.StoredMacro || b.Display != 0 || b.Origin != gofastcore.OriginNative {
					return fmt.Errorf("%s: code with attributes %v, display %q, origin %s", where, b.Attrs, b.Display, b.Origin)
				}
				continue
			case gofastcore.KindNote:
				// What an annotation says, or the options of a choice
				// field: the file says what they are.
				text = true
				switch b.Attrs[gofastcore.AttrNoteType] {
				case gofastcore.NoteComment:
				case gofastcore.NoteChoices:
					if _, named := b.Attrs[gofastcore.AttrField]; !named {
						return fmt.Errorf("%s: options of a field with attributes %v", where, b.Attrs)
					}
				default:
					return fmt.Errorf("%s: a note of type %q", where, b.Attrs[gofastcore.AttrNoteType])
				}
				if b.Origin != gofastcore.OriginNative || b.Text == "" {
					return fmt.Errorf("%s: a note with origin %s and attributes %v", where, b.Origin, b.Attrs)
				}
			default:
				return fmt.Errorf("%s: a %s block", where, b.Kind)
			}
			// The text of the page is put into paragraphs by this library.
			// The value of a form field and a note are what the file says
			// they are.
			// And so is the header or the footer that an XPS document
			// declares a fragment of a page to be.
			_, field := b.Attrs[gofastcore.AttrField]
			declared := doc.Source.Format != gofastpdf.Format && (b.Kind == gofastcore.KindHeader || b.Kind == gofastcore.KindFooter) && b.Origin == gofastcore.OriginNative
			if native := field || declared || b.Kind == gofastcore.KindNote || b.Kind == gofastcore.KindListItem && b.Origin == gofastcore.OriginNative; native != (b.Origin == gofastcore.OriginNative) || !native && b.Origin != gofastcore.OriginInferred {
				return fmt.Errorf("%s: a %s block with origin %s (the value of a form field: %v)", where, b.Kind, b.Origin, field)
			}
			if len(b.Spans) > 0 && !opts.Spans {
				return fmt.Errorf("%s: spans without Options.Spans", where)
			}
			r := b.Loc.BBox
			for _, v := range [...]float64{r.X0, r.Y0, r.X1, r.Y1} {
				if v != v || v > 1e9 || v < -1e9 {
					return fmt.Errorf("%s: box %+v", where, r)
				}
			}
		}
		// A page without text says which kind it is, and only then.
		none, image := p.Has(gofastcore.WarnPartNoText), p.Has(gofastcore.WarnPartImageOnly)
		failed := p.Has(gofastcore.WarnPartFailed)
		// A page that the limit on text cut may have lost its text to
		// the cut: it has none here and is not a page without text. Or it
		// had none to begin with and says so. Both are right.
		if p.Has(gofastcore.WarnLimitExceeded) && !text {
			continue
		}
		switch {
		case text && (none || image):
			return fmt.Errorf("page %d has text and a warning that it has none", p.Number)
		case !text && !failed && none == image:
			return fmt.Errorf("page %d has no text; part.no_text: %v, part.image_only: %v", p.Number, none, image)
		case failed && (none || image):
			return fmt.Errorf("page %d failed and is also described as empty", p.Number)
		}
	}
	return nil
}

// sameDocument reports how two documents differ, or "" if they do not.
func sameDocument(a, b *gofastcore.Document) string {
	if reflect.DeepEqual(a, b) {
		return ""
	}
	ja, _ := json.Marshal(a)
	jb, _ := json.Marshal(b)
	if bytes.Equal(ja, jb) {
		return ""
	}
	n := 0
	for n < len(ja) && n < len(jb) && ja[n] == jb[n] {
		n++
	}
	from := max(n-60, 0)
	return fmt.Sprintf("they differ at byte %d of their JSON: ...%s | ...%s", n, ja[from:min(n+60, len(ja))], jb[from:min(n+60, len(jb))])
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
