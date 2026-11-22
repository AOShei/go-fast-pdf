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
	"github.com/AOShei/go-fast-pdf/internal/syntax"
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
	// NoRuledTables leaves out the tables found by the rules they are
	// drawn with, and the following of a page's paths that finding them
	// takes: about 5 percent of the instructions over a mixed set of
	// documents, less on pages of text. Tables of tagged files, and those
	// found by how their text lines up, are found as before.
	NoRuledTables bool
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

// internalError is the message of a panic that a recover stopped: a
// defect of the library, which ends one page, one image or the reading
// of the catalog and not the process. Fuzzing fails on the words
// "internal error" (checkContract), so the recovers hide no defect.
func internalError(r any) string {
	return cleanString(fmt.Sprintf("internal error: %v", r))
}

// panicHook, when set by a test, is called where a panic is recovered
// from: "page", "image", "describe", "child".
var panicHook func(where string)

// Warning codes that only this library uses. Each states a fact about
// the file or about what was done with it.
const (
	// WarnGlyphTextAssumed: characters on the page are set in a font
	// that does not say which characters its glyphs are; they were read
	// as the characters their codes stand for.
	WarnGlyphTextAssumed = "pdf.glyph_text_assumed"
	// WarnGlyphTextByOutline: characters on the page are set in glyphs
	// that the file gives no text; they were read by their outlines,
	// which are those of glyphs of the Noto Arabic fonts the library
	// knows (Sans, Naskh and Kufi, Regular and Bold, one version each).
	WarnGlyphTextByOutline = "pdf.glyph_text_by_outline"
	// WarnFontMissing: text on the page is set in a font that the page's
	// resources do not define.
	WarnFontMissing = "pdf.font_missing"
	// WarnContentStreamDamaged: a content stream of the page is damaged;
	// what could be decoded before the damage was read.
	WarnContentStreamDamaged = "pdf.content_stream_damaged"
	// WarnContentStreamUnreadable: a content stream of the page could
	// not be decoded at all.
	WarnContentStreamUnreadable = "pdf.content_stream_unreadable"
	// WarnContentBytesSkipped: bytes of a content stream of the page are
	// not valid syntax and were skipped.
	WarnContentBytesSkipped = "pdf.content_bytes_skipped"
	// WarnObjectUnreadable: an object of the file could not be read and
	// was taken as null. It is a warning of the document.
	WarnObjectUnreadable = "pdf.object_unreadable"
	// WarnPageTreeDamaged: a node of the page tree is not what it should
	// be; the pages under it may be missing.
	WarnPageTreeDamaged = "pdf.page_tree_damaged"
	// WarnXRefSectionUnreadable: a section of the cross-reference data
	// could not be read.
	WarnXRefSectionUnreadable = "pdf.xref_section_unreadable"
	// WarnXRefEntryUnreadable: an entry of the cross-reference data is
	// unusable.
	WarnXRefEntryUnreadable = "pdf.xref_entry_unreadable"
	// WarnStringUndecryptable: a string of an encrypted file could not be
	// decrypted.
	WarnStringUndecryptable = "pdf.string_undecryptable"
)

// pageOut is what one page worker hands to the merge.
type pageOut struct {
	part    gofastcore.Part
	done    bool
	decoded int64
	// images says, for each image block of the part, which image it is.
	images []imageRef
	// styles says, for each block of the part, how its text is set; the
	// zero Style for a block that is not text of the page.
	styles []layout.Style
	// lines are the lines of the page, when they were asked for.
	lines []layout.Line
	// What the page contributes to the document's warnings.
	active, external bool
	// attachments are the files attached to the page by annotations.
	attachments []attachment
	// scripts are the scripts of the actions that were looked at.
	scripts []script
	// items is what the structure tree says of the list items the
	// blocks are in, nil for none.
	items map[int64]treeItem
	// What the layout and the interpreter counted. It is for
	// measurement and goes into no document.
	stats                                  layout.Stats
	actualDiffers, actualLonger, longGlyph int
}

type imageRef struct {
	block  int32
	obj    int64 // the object number of an image XObject, 0 for none
	inline bool
	// The dictionary of an inline image, and its data when data is
	// asked for (extract.Image).
	dict syntax.Value
	data []byte
}

// want says what a page is read for besides its part.
type want struct {
	// lines asks for the lines of the page with their words.
	lines bool
	// dests finds where the links of the page go, with Options.Spans.
	dests *destinations
	// structure is the structure tree of a tagged PDF, nil for none.
	structure *structure
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
	defer part.Finish()
	defer func() {
		if r := recover(); r != nil {
			// A panic anywhere in the page: its layout, tables, notes. The
			// page is a failed part with nothing else, as one that could
			// not be read (internalError).
			width, height := part.Width, part.Height
			out = pageOut{done: true}
			part.Kind, part.Number, part.Width, part.Height = gofastcore.PartPage, p.Number, width, height
			part.Warn(gofastcore.SeverityError, gofastcore.WarnPartFailed, internalError(r))
		}
	}()
	if p.Err != nil {
		part.Warn(gofastcore.SeverityError, gofastcore.WarnPartFailed, cleanString(p.Err.Error()))
		return out
	}
	m, width, height := p.Display()
	part.Width, part.Height = width, height
	if panicHook != nil {
		panicHook("page")
	}

	var lay layout.Result
	images := 0
	// Spans are made from the words of the lines, placed by their share
	// of their run: near enough to tell which words a link covers.
	// What fixed bounds leave out of the page, reported at its end.
	var limits file.Cuts
	// What the page has of its annotations besides their appearances is
	// taken as the appearances are drawn, each annotation read once
	// (pageAnnots): the links, for spans; the notes and the values of
	// form fields, the text of which notes takes; the files attached and
	// the actions, after those of the page itself.
	notes := reader{f: f, room: maxPageNoteBytes}
	pa := newPageAnnots(f, p, m, notes, opts.Spans, w.dests)
	var annot func(int, syntax.Value, bool)
	if pa != nil {
		annot = pa.annot
	}
	scanActions(f, p.Dict().Get("AA"), &out)
	// What the structure tree of a tagged PDF says of the page.
	tags, ordered := w.structure.pageTags(p, &limits)
	var headingOf func(int32) uint8
	var itemOf func(int32) (int64, bool)
	var rankOf func(int32) int32
	if tags != nil {
		headingOf = func(mcid int32) uint8 {
			if mcid >= 0 && int(mcid) < len(tags) {
				return tags[mcid].heading
			}
			return 0
		}
		itemOf = func(mcid int32) (int64, bool) {
			if mcid >= 0 && int(mcid) < len(tags) {
				return tags[mcid].item, tags[mcid].label
			}
			return 0, false
		}
		rankOf = ordered.rank
	}
	// The tables of a tagged PDF that the page's content is in.
	tables := w.structure.pageTables(tags)
	res := extract.Page(f, p, extract.Options{Words: w.lines, Rules: !e.NoRuledTables, Stop: w.stop, Annot: annot,
		InlineDicts: opts.Images != gofastcore.ImagesNone, InlineData: opts.Images == gofastcore.ImagesData,
		Use: func(c *extract.Content) {
			// Made here, where nothing outside holds it: it stays off the heap.
			lopts := layout.Options{
				Images: opts.Images != gofastcore.ImagesNone, Lines: w.lines,
				Spans: opts.Spans, Links: pa.linksOf(), Heading: headingOf, Item: itemOf, Rank: rankOf,
			}
			tables.options(tags, &lopts)
			lay = layout.Build(layout.Page{M: m, Width: width, Height: height}, c, lopts)
			if tables.drop(&lay) {
				// Tables that the page does not bear out are its text again.
				lay = layout.Build(layout.Page{M: m, Width: width, Height: height}, c, lopts)
			}
			images = len(c.Images)
			for k, i := range lay.Images {
				if i >= 0 {
					// The description of the figure the image is part of.
					if mc := c.Images[i].MCID; mc >= 0 && int(mc) < len(tags) && tags[mc].alt != "" {
						lay.Blocks[k].Text = tags[mc].alt
					}
					if tags != nil {
						lay.Styles[k].Marked = c.Images[i].MCID + 1
					}
					out.images = append(out.images, imageRef{block: int32(k), obj: c.Images[i].Obj, inline: c.Images[i].Inline,
						dict: c.Images[i].Dict, data: c.Images[i].Data})
				}
			}
		}})
	out.lines, out.styles = lay.Lines, lay.Styles
	out.decoded, out.external, out.stats = res.Decoded, res.External, lay.Stats
	out.actualDiffers, out.actualLonger, out.longGlyph = res.ActualDiffers, res.ActualLonger, res.LongGlyphText
	tables.fill(lay.Blocks, &lay)
	part.Blocks = lay.Blocks
	part.Warnings = append(part.Warnings, res.Warnings...)
	// The headings that the structure tree names.
	for k := range part.Blocks {
		if b := &part.Blocks[k]; k < len(lay.Styles) && lay.Styles[k].Heading > 0 && b.Kind == gofastcore.KindParagraph && b.Display == 0 {
			setHeading(part, k, int(lay.Styles[k].Heading))
			b.Origin = gofastcore.OriginNative
		}
	}
	// The items of the lists of the tree, made into list items with
	// those found by their markers (lists.go).
	out.items = treeItems(lay.Styles, tags)
	// The identifiers of the elements of the tree, each on the first block
	// that its content begins (anchors.go).
	if tags != nil {
		var used map[string]bool
		for k := range min(len(part.Blocks), len(lay.Styles)) {
			mc := lay.Styles[k].Marked - 1
			if mc < 0 || int(mc) >= len(tags) || tags[mc].id == "" || used[tags[mc].id] {
				continue
			}
			if used == nil {
				used = map[string]bool{}
			}
			used[tags[mc].id] = true
			part.Blocks[k].Loc.Anchor = tags[mc].id
		}
	}

	// What the page says outside its content: the text of its
	// annotations and the values of its form fields.
	extras, notesCut := pa.extrasOf()
	pa.merge(&out, &limits, &notes)
	out.addNotes(extras)
	if notesCut {
		noteWarning(part)
	}

	switch {
	case res.Err != nil:
		part.Warn(gofastcore.SeverityError, gofastcore.WarnPartFailed, res.Err.Error())
	case lay.HasText || len(extras) > 0:
	case images > 0:
		part.Warn(gofastcore.SeverityWarning, gofastcore.WarnPartImageOnly, fmt.Sprintf("the page has %d images and no text", images))
	default:
		part.Warn(gofastcore.SeverityInfo, gofastcore.WarnPartNoText, "the page has no text and no images")
	}
	if lay.Controls > 0 && !part.Has(gofastcore.WarnTextUnmappedGlyphs) {
		part.Warn(gofastcore.SeverityWarning, gofastcore.WarnTextUnmappedGlyphs,
			fmt.Sprintf("%d characters on the page are control characters and appear as U+FFFD", lay.Controls))
	}
	warnCuts(part, &limits)
	// A message can quote the file: a name, part of an object.
	for i := range part.Warnings {
		part.Warnings[i].Message = cleanString(part.Warnings[i].Message)
	}
	// The scripts of the page's actions and of its annotations: stored
	// and never shown, so read as text, marked with what they are, after
	// everything else of the page.
	if code, cut := scriptBlocks(&notes, out.scripts, map[string]bool{}); len(code) > 0 || cut {
		part.Blocks = append(part.Blocks, code...)
		if cut && !notesCut {
			noteWarning(part)
		}
	}
	out.scripts = nil
	return out
}
