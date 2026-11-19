package gofastpdf

import (
	"context"
	"fmt"
	"io"
	"sync/atomic"

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
// value is ready to use and safe for concurrent use; the functions of
// this package that are not its methods use the zero value.
//
// Settings that only PDF has are fields of this type. Zero means the
// default for each.
type Extractor struct {
	// MaxStreamBytes is the most that one stream of the file may decode
	// to: a page's content, an image, an attached file. The default is
	// 64 MiB, or Limits.MaxDecodedBytes where that is lower. A stream
	// that is larger is cut there and its part gets limit.exceeded.
	MaxStreamBytes int64
	// ObjectStreamCacheBytes is how much decoded object-stream data is
	// kept, so that an object stream is not decoded again for every
	// object in it. The default is 64 MiB.
	ObjectStreamCacheBytes int64
	// ObjectCacheBytes is how much of the parsed objects of a file is
	// kept, so that an object used by many pages is parsed once. The
	// default is 4 MiB. When more is kept, all of it is let go: this is
	// what keeps a file of many pages from taking more memory with every
	// page that is read.
	ObjectCacheBytes int64
	// MaxShapes is the number of shapes followed on one page by the
	// checks of Options.DeepHidden. The default is 65,536. A page that
	// paints more gets pdf.deep_hidden_incomplete.
	MaxShapes int

	// noHiddenChecks leaves out the checks for hidden text. It exists
	// to measure what they cost and is not reachable from outside.
	noHiddenChecks bool
	// maxDeepSteps bounds the work of the checks of DeepHidden on one
	// page; 0 means the default. Tests set it to reach the bound with a
	// small page.
	maxDeepSteps int
}

var _ gofastcore.Extractor = (*Extractor)(nil)

// Formats returns the format this extractor handles: "pdf".
func (*Extractor) Formats() []string { return []string{Format} }

// Extract reads a PDF file of size bytes from r and returns it as a
// document: one part per page, the text of each page as paragraphs in
// reading order.
//
// opts may be nil. See the package documentation for what each option
// does here. Extract is Open, Document and Close in one.
func Extract(ctx context.Context, r io.ReaderAt, size int64, opts *gofastcore.Options) (*gofastcore.Document, error) {
	return new(Extractor).Extract(ctx, r, size, opts)
}

// ExtractFile is Extract for the file at path. The name of the file is
// the document's Source.Name unless opts gives another.
func ExtractFile(ctx context.Context, path string, opts *gofastcore.Options) (*gofastcore.Document, error) {
	return new(Extractor).ExtractFile(ctx, path, opts)
}

// Extract implements gofastcore.Extractor. See the function Extract.
func (e *Extractor) Extract(ctx context.Context, r io.ReaderAt, size int64, opts *gofastcore.Options) (*gofastcore.Document, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f, err := e.OpenReaderAt(r, size, opts)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return f.Document(ctx)
}

// ExtractFile is Extract for the file at path. See the function
// ExtractFile.
func (e *Extractor) ExtractFile(ctx context.Context, path string, opts *gofastcore.Options) (*gofastcore.Document, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f, err := e.Open(path, opts)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return f.Document(ctx)
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
	// WarnDeepHiddenIncomplete: Options.DeepHidden was set and the page
	// paints more shapes than are followed. Its text is complete; low
	// contrast and covering shapes were not checked for all of it.
	WarnDeepHiddenIncomplete = "pdf.deep_hidden_incomplete"
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
	// lines are the lines of the page, when they were asked for.
	lines []layout.Line
	// What the page contributes to the document's warnings.
	active, external bool
	// deepIncomplete is set when the checks of DeepHidden were not made
	// for all of the page.
	deepIncomplete bool
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

// want says what a page is read for besides its part.
type want struct {
	// lines asks for the lines of the page with their words.
	lines bool
	// stop, when set, is looked at as the page is read: once it holds
	// true the page is abandoned, and what is returned for it is to be
	// thrown away.
	stop *atomic.Bool
}

// page extracts one page into a part. It never fails: a page that
// cannot be read is a part with a part.failed warning.
func (e *Extractor) page(f *file.File, p *file.Page, opts *gofastcore.Options, w want) (out pageOut) {
	out.done = true
	part := &out.part
	part.Kind, part.Number = gofastcore.PartPage, p.Number
	defer part.MarkHidden()
	if p.Err != nil {
		part.Warn(gofastcore.SeverityError, gofastcore.WarnPartFailed, cleanString(p.Err.Error()))
		return out
	}
	m, width, height := p.Display()
	part.Width, part.Height = width, height

	var lay layout.Result
	images := 0
	res := extract.Page(f, p, extract.Options{Shapes: opts.DeepHidden, MaxShapes: e.MaxShapes, Words: w.lines, Stop: w.stop, Use: func(c *extract.Content) {
		lay = layout.Build(layout.Page{M: m, Width: width, Height: height}, c, layout.Options{
			Images: opts.Images != gofastcore.ImagesNone, NoHiddenChecks: e.noHiddenChecks,
			Deep: opts.DeepHidden, MaxDeepSteps: e.maxDeepSteps, Lines: w.lines,
		})
		images = len(c.Images)
		for k, i := range lay.Images {
			if i >= 0 {
				out.images = append(out.images, imageRef{block: int32(k), obj: c.Images[i].Obj, inline: c.Images[i].Inline})
			}
		}
	}})
	out.lines = lay.Lines
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
	if lay.DeepIncomplete {
		out.deepIncomplete = true
		part.Warn(gofastcore.SeverityWarning, WarnDeepHiddenIncomplete,
			"the page paints more shapes than are followed; low contrast and covering shapes were not checked for all of its text")
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
