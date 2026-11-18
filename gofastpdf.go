package gofastpdf

import (
	"context"
	"fmt"
	"io"
	"math"
	"runtime"
	"sync"

	gofastcore "github.com/AOShei/go-fast-core"
	"github.com/AOShei/go-fast-pdf/internal/extract"
	"github.com/AOShei/go-fast-pdf/internal/file"
	"github.com/AOShei/go-fast-pdf/internal/layout"
)

// Format is the name of the format in Source.Format.
const Format = "pdf"

// The defaults for the limits of gofastcore.Limits that are left zero.
const (
	// DefaultMaxInputBytes is the largest input read.
	DefaultMaxInputBytes = 1 << 30
	// DefaultMaxDecodedBytes is the most that the streams of one
	// document may decode to, taken together.
	DefaultMaxDecodedBytes = 1 << 30
)

// maxStreamBytes is the most one stream may decode to, unless the limit
// on all of them together is lower.
const maxStreamBytes = 64 << 20

// Extractor extracts PDF files into the shared document model. The zero
// value is ready to use and safe for concurrent use.
//
// Settings that only PDF has become fields of this type.
type Extractor struct{}

var _ gofastcore.Extractor = (*Extractor)(nil)

// Formats returns the format this extractor handles: "pdf".
func (*Extractor) Formats() []string { return []string{Format} }

// Extract reads a PDF file of size bytes from r and returns it as a
// document: one part per page, the text of each page as paragraphs in
// reading order.
//
// opts may be nil. See the package documentation for what each option
// does here.
func Extract(ctx context.Context, r io.ReaderAt, size int64, opts *gofastcore.Options) (*gofastcore.Document, error) {
	return new(Extractor).Extract(ctx, r, size, opts)
}

// Extract implements gofastcore.Extractor. See the function Extract.
func (e *Extractor) Extract(ctx context.Context, r io.ReaderAt, size int64, opts *gofastcore.Options) (*gofastcore.Document, error) {
	if opts == nil {
		opts = &gofastcore.Options{}
	}
	if size < 0 {
		return nil, fmt.Errorf("%w: negative size", gofastcore.ErrMalformed)
	}
	if limit := gofastcore.ResolveLimit(opts.Limits.MaxInputBytes, DefaultMaxInputBytes); size > limit {
		return nil, fmt.Errorf("%w: the input is %d bytes and the limit is %d", gofastcore.ErrLimitExceeded, size, limit)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// The whole file is held in memory: a PDF is read from its end and
	// by offsets all over it.
	data := make([]byte, size)
	if _, err := io.ReadFull(io.NewSectionReader(r, 0, size), data); err != nil {
		return nil, fmt.Errorf("reading the input: %w", err)
	}

	budget := gofastcore.ResolveLimit(opts.Limits.MaxDecodedBytes, DefaultMaxDecodedBytes)
	limits := file.Limits{MaxStreamBytes: min(budget, maxStreamBytes)}
	if opts.Limits.MaxDecodedBytes < 0 {
		limits.MaxStreamBytes = 1 << 40
	}
	f, err := file.OpenPassword(data, limits, opts.Password)
	if err != nil {
		return nil, err
	}
	pages := f.Pages()
	if len(pages) == 0 {
		return nil, fmt.Errorf("%w: %w", gofastcore.ErrMalformed, file.ErrNoPages)
	}

	// The pages asked for.
	rng := opts.Parts
	if rng.First < 1 {
		rng.First = 1
	}
	if rng.Last < 0 || rng.Last != 0 && rng.First > rng.Last || rng.First > len(pages) {
		return nil, fmt.Errorf("%w: pages %d to %d of a document of %d", gofastcore.ErrNoPartsSelected, opts.Parts.First, opts.Parts.Last, len(pages))
	}
	if rng.Last == 0 || rng.Last > len(pages) {
		rng.Last = len(pages)
	}
	selected := pages[rng.First-1 : rng.Last]

	doc := gofastcore.New(Format)
	doc.Source.Name, doc.Source.MIME, doc.Source.Size = opts.Name, "application/pdf", size
	tooMany := false
	if most := gofastcore.ResolveLimit(int64(opts.Limits.MaxParts), math.MaxInt64); int64(len(selected)) > most {
		selected, tooMany = selected[:most], true
	}

	// The pages, each by itself.
	outs := make([]pageOut, len(selected))
	workers := opts.Workers
	if workers <= 0 {
		workers = runtime.GOMAXPROCS(0)
	}
	if workers = min(workers, len(selected)); workers <= 1 {
		for i := range selected {
			if ctx.Err() != nil {
				break
			}
			outs[i] = e.page(f, &selected[i], opts)
		}
	} else {
		var wg sync.WaitGroup
		next := make(chan int)
		for range workers {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := range next {
					if ctx.Err() == nil {
						outs[i] = e.page(f, &selected[i], opts)
					}
				}
			}()
		}
		for i := range selected {
			next <- i
		}
		close(next)
		wg.Wait()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// The parts are put together in page order, and the budget for
	// decoded bytes is charged in that order, so where a document is cut
	// does not depend on how the workers were scheduled.
	m := merger{f: f, opts: opts, doc: doc, budget: budget, images: map[string]bool{}}
	doc.Parts = make([]gofastcore.Part, 0, len(selected))
	for i := range outs {
		if !m.add(&outs[i]) {
			break
		}
	}
	e.describe(ctx, &m, pages)

	// What concerns the file as a whole, in an order that does not
	// depend on which worker found it.
	found := f.Warnings()
	for i := range found {
		found[i].Message = cleanString(found[i].Message)
	}
	doc.Warnings = append(found, doc.Warnings...)
	if rebuilt, redefined := f.Rebuilt(); rebuilt && redefined > 0 {
		doc.Warn(gofastcore.SeverityWarning, gofastcore.WarnInputAmbiguous,
			fmt.Sprintf("the cross-reference data is unusable and %d objects are defined more than once; the last definition of each was read", redefined))
	}
	if m.active {
		doc.Warn(gofastcore.SeverityInfo, gofastcore.WarnInputActiveContent, "the document has scripts or actions that start a program; none was read or run")
	}
	if m.external {
		doc.Warn(gofastcore.SeverityWarning, gofastcore.WarnInputExternalReference, "the document refers to files or addresses outside itself other than by links; nothing was fetched")
	}
	if tooMany {
		doc.Warn(gofastcore.SeverityError, gofastcore.WarnLimitExceeded,
			fmt.Sprintf("%d pages were asked for and the limit is %d; the pages after it are not read", rng.Last-rng.First+1, len(selected)))
	}
	if m.spent {
		doc.Warn(gofastcore.SeverityError, gofastcore.WarnLimitExceeded,
			fmt.Sprintf("the document decodes to more than the limit of %d bytes; what follows page %d is not read", budget, m.lastPage))
	}

	if m.attachmentsCut {
		doc.Warn(gofastcore.SeverityError, gofastcore.WarnLimitExceeded,
			fmt.Sprintf("the document decodes to more than the limit of %d bytes; some attached files were not extracted", budget))
	}
	if m.imagesCut {
		doc.Warn(gofastcore.SeverityError, gofastcore.WarnLimitExceeded,
			fmt.Sprintf("the document decodes to more than the limit of %d bytes; the data of some images is not included", budget))
	}

	// The reasons that were checked completely. Low contrast, occlusion
	// and clipping are checked in part and are not listed.
	doc.HiddenChecked = hiddenChecked
	if opts.DeepHidden {
		doc.Warn(gofastcore.SeverityWarning, WarnDeepHiddenUnavailable,
			"the checks of Options.DeepHidden are not implemented in this version; only the default checks were made")
	}
	if invisibleInMetadata(doc) {
		doc.Warn(gofastcore.SeverityWarning, gofastcore.WarnTextHidden, "the document information or the outline has invisible characters")
	}
	return doc, nil
}

// hiddenChecked is the set of reasons a run with default options checks
// completely.
const hiddenChecked = gofastcore.HiddenInvisible | gofastcore.HiddenTextLayer | gofastcore.HiddenTiny |
	gofastcore.HiddenOffPage | gofastcore.HiddenTransparent | gofastcore.HiddenChars

// Warning codes that only this library uses. Each states a fact about
// the file or about what was done with it.
const (
	// WarnRTLVisualOrder: the page has text in a script written from
	// right to left. Its text is given in the order the page draws it,
	// which may be the order of display and not of reading.
	WarnRTLVisualOrder = "pdf.rtl_visual_order"
	// WarnDeepHiddenUnavailable: Options.DeepHidden was set, and this
	// version makes only the default checks.
	WarnDeepHiddenUnavailable = "pdf.deep_hidden_unavailable"
	// WarnGlyphTextAssumed: characters on the page are set in a font
	// that does not say which characters its glyphs are; they were read
	// as the characters their codes stand for.
	WarnGlyphTextAssumed = "pdf.glyph_text_assumed"
	// WarnFontMissing: text on the page is set in a font that the page's
	// resources do not define.
	WarnFontMissing = "pdf.font_missing"
)

// pageOut is what one page worker hands to the merge.
type pageOut struct {
	part    gofastcore.Part
	done    bool
	decoded int64
	// images says, for each image block of the part, which image it is.
	images []imageRef
	// What the page contributes to the document's warnings.
	active, external bool
	// attachments are the files attached to the page by annotations.
	attachments []attachment
	// What the layout and the interpreter counted. It is for
	// measurement and goes into no document.
	stats                                  layout.Stats
	actualDiffers, actualLonger, longGlyph int
}

type imageRef struct {
	block  int32
	obj    int64 // the object number of an image XObject, 0 for none
	inline bool
}

// page extracts one page into a part. It never fails: a page that
// cannot be read is a part with a part.failed warning.
func (e *Extractor) page(f *file.File, p *file.Page, opts *gofastcore.Options) (out pageOut) {
	out.done = true
	part := &out.part
	part.Kind, part.Number = gofastcore.PartPage, p.Number
	defer part.MarkHidden()
	if p.Err != nil {
		part.Warn(gofastcore.SeverityError, gofastcore.WarnPartFailed, cleanString(p.Err.Error()))
		return out
	}
	m, w, h := p.Display()
	part.Width, part.Height = w, h

	var lay layout.Result
	images := 0
	res := extract.Page(f, p, extract.Options{Use: func(c *extract.Content) {
		lay = layout.Build(layout.Page{M: m, Width: w, Height: h}, c, layout.Options{Images: opts.Images != gofastcore.ImagesNone})
		images = len(c.Images)
		for k, i := range lay.Images {
			if i >= 0 {
				out.images = append(out.images, imageRef{block: int32(k), obj: c.Images[i].Obj, inline: c.Images[i].Inline})
			}
		}
	}})
	out.decoded, out.external, out.stats = res.Decoded, res.External, lay.Stats
	out.actualDiffers, out.actualLonger, out.longGlyph = res.ActualDiffers, res.ActualLonger, res.LongGlyphText
	part.Blocks = lay.Blocks
	part.Warnings = append(part.Warnings, res.Warnings...)

	switch {
	case res.Err != nil:
		part.Warn(gofastcore.SeverityError, gofastcore.WarnPartFailed, res.Err.Error())
	case lay.HasText:
	case images > 0:
		part.Warn(gofastcore.SeverityWarning, gofastcore.WarnPartImageOnly, fmt.Sprintf("the page has %d images and no text", images))
	default:
		part.Warn(gofastcore.SeverityInfo, gofastcore.WarnPartNoText, "the page has no text and no images")
	}
	if lay.Controls > 0 && !part.Has(gofastcore.WarnTextUnmappedGlyphs) {
		part.Warn(gofastcore.SeverityWarning, gofastcore.WarnTextUnmappedGlyphs,
			fmt.Sprintf("%d characters on the page are control characters and appear as U+FFFD", lay.Controls))
	}
	if lay.RightToLeft {
		part.Warn(gofastcore.SeverityWarning, WarnRTLVisualOrder,
			"the page has text in a script written from right to left; it is given in the order the page draws it")
	}
	// A message can quote the file: a name, part of an object.
	for i := range part.Warnings {
		part.Warnings[i].Message = cleanString(part.Warnings[i].Message)
	}
	e.annotations(f, p, &out)
	return out
}

// merger puts the parts together in page order.
type merger struct {
	f      *file.File
	opts   *gofastcore.Options
	doc    *gofastcore.Document
	budget int64 // decoded bytes left
	// spent is set when the budget ran out, after lastPage.
	spent    bool
	lastPage int

	active, external bool
	attachments      []attachment
	images           map[string]bool // the IDs that have had their data
	inline           int
	// imagesCut is set when the data of an image was left out because
	// the budget for decoded bytes ran out.
	imagesCut bool
	// attachmentsCut is the same for attached files.
	attachmentsCut bool
}

// add appends the part of one page. It reports false when the budget
// for decoded bytes is used up: the document ends before this page.
func (m *merger) add(out *pageOut) bool {
	if !out.done {
		return false
	}
	if m.budget -= out.decoded; m.budget < 0 {
		m.spent = true
		return false
	}
	m.lastPage = out.part.Number
	m.active = m.active || out.active
	m.external = m.external || out.external
	m.attachments = append(m.attachments, out.attachments...)
	m.doc.Parts = append(m.doc.Parts, out.part)
	part := &m.doc.Parts[len(m.doc.Parts)-1]
	for _, ref := range out.images {
		m.image(part, ref)
	}
	return true
}
