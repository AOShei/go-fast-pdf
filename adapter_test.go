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
	// Deep asks for the checks of DeepHidden.
	Deep bool
	// Unchecked skips the contract checks, for benchmarks.
	Unchecked bool
}

func (o extractOpts) options() *gofastcore.Options {
	opts := &gofastcore.Options{Password: o.Password, Workers: max(o.Workers, 1), DeepHidden: o.Deep}
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
// and the value of a form field whose appearance shows nothing.
func pageText(p *gofastcore.Part) string {
	extra := func(b *gofastcore.Block) bool {
		_, field := b.Attrs[gofastcore.AttrField]
		return field || b.Kind == gofastcore.KindNote
	}
	any := false
	for i := range p.Blocks {
		any = any || extra(&p.Blocks[i])
	}
	if !any {
		return p.Text()
	}
	q := *p
	q.Blocks = nil
	for i := range p.Blocks {
		if !extra(&p.Blocks[i]) {
			q.Blocks = append(q.Blocks, p.Blocks[i])
		}
	}
	return q.Text()
}

// errContract marks a result that the library must never return: a
// document that breaks the contract of the shared model. An input that
// cannot be read is an error of another kind and is no defect.
var errContract = errors.New("the document breaks the contract of the shared model")

// deepChecked are the reasons that Document.HiddenChecked lists only
// for a run with DeepHidden.
const deepChecked = gofastcore.HiddenLowContrast | gofastcore.HiddenOccluded | gofastcore.HiddenClipped

// defaultChecked is what Document.HiddenChecked is for a run without it.
const defaultChecked = gofastcore.HiddenInvisible | gofastcore.HiddenTextLayer | gofastcore.HiddenTiny |
	gofastcore.HiddenOffPage | gofastcore.HiddenTransparent | gofastcore.HiddenChars

// checkContract checks what must hold for every document this library
// returns, whatever the input (section 4.2 of the plan).
func checkContract(doc *gofastcore.Document, opts *gofastcore.Options) error {
	if err := doc.Validate(); err != nil {
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
	if doc.Source.Format != gofastpdf.Format {
		return fmt.Errorf("source format %q", doc.Source.Format)
	}
	// The rendered text holds the words of the blocks, all of them and
	// in their order, and the Markdown is there when the text is.
	var words []string
	for _, b := range doc.AllBlocks() {
		words = append(words, strings.Fields(b.Text)...)
	}
	text := doc.Text()
	if got := strings.Fields(text); !reflect.DeepEqual(got, words) && len(got)+len(words) > 0 {
		return fmt.Errorf("the rendered text has %d words and the blocks have %d, or they differ", len(got), len(words))
	}
	// Markdown shows an image where the plain text has nothing.
	if opts.Images == gofastcore.ImagesNone && (strings.TrimSpace(doc.Markdown()) == "") != (strings.TrimSpace(text) == "") {
		return fmt.Errorf("the Markdown is empty and the text is not, or the other way round")
	}
	// What was checked: the reasons of the default checks, and with
	// DeepHidden the three that need the shapes of a page, two of which
	// are left out when a page had more shapes than are followed.
	incomplete := false
	for i := range doc.Parts {
		incomplete = incomplete || doc.Parts[i].Has(gofastpdf.WarnDeepHiddenIncomplete)
	}
	want := defaultChecked
	switch {
	case !opts.DeepHidden && incomplete:
		return fmt.Errorf("%s without DeepHidden", gofastpdf.WarnDeepHiddenIncomplete)
	case opts.DeepHidden && incomplete:
		want |= gofastcore.HiddenClipped
	case opts.DeepHidden:
		want |= deepChecked
	}
	if doc.HiddenChecked != want {
		return fmt.Errorf("hidden_checked is %s, want %s", doc.HiddenChecked, want)
	}
	for i := range doc.Parts {
		p := &doc.Parts[i]
		if p.Kind != gofastcore.PartPage {
			return fmt.Errorf("part %d is a %s", p.Number, p.Kind)
		}
		text := false
		for j := range p.Blocks {
			b := &p.Blocks[j]
			where := fmt.Sprintf("page %d, block %d", p.Number, j)
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
			case gofastcore.KindNote:
				// What an annotation says: the file says that it is one.
				text = true
				if b.Origin != gofastcore.OriginNative || b.Attrs[gofastcore.AttrNoteType] != gofastcore.NoteComment || b.Text == "" {
					return fmt.Errorf("%s: a note with origin %s and attributes %v", where, b.Origin, b.Attrs)
				}
			default:
				return fmt.Errorf("%s: a %s block", where, b.Kind)
			}
			// The text of the page is put into paragraphs by this library.
			// The value of a form field and a note are what the file says
			// they are.
			_, field := b.Attrs[gofastcore.AttrField]
			if native := field || b.Kind == gofastcore.KindNote; native != (b.Origin == gofastcore.OriginNative) || !native && b.Origin != gofastcore.OriginInferred {
				return fmt.Errorf("%s: a %s block with origin %s (the value of a form field: %v)", where, b.Kind, b.Origin, field)
			}
			if len(b.Spans) > 0 && b.Hidden&gofastcore.HiddenChars == 0 {
				return fmt.Errorf("%s: spans on a block without invisible characters", where)
			}
			// A block is on its page unless it is flagged as off it.
			r := b.Loc.BBox
			const tol = 1.0
			off := r.X1 < -tol || r.Y1 < -tol || r.X0 > p.Width+tol || r.Y0 > p.Height+tol
			if off && p.Width > 0 && p.Height > 0 && b.Hidden&gofastcore.HiddenOffPage == 0 {
				return fmt.Errorf("%s: box %+v lies off a page of %v by %v and is not flagged", where, r, p.Width, p.Height)
			}
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
