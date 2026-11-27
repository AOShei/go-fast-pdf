package gofastpdf

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	gofastcore "github.com/AOShei/go-fast-core"
	"github.com/AOShei/go-fast-core/cfb"
	"github.com/AOShei/go-fast-pdf/internal/charset"
	"github.com/AOShei/go-fast-pdf/internal/extract"
	"github.com/AOShei/go-fast-pdf/internal/layout"
	"github.com/AOShei/go-fast-pdf/internal/xps"
)

// XPS and OpenXPS.
//
// An XPS document is a package (a zip archive of parts, as a Word file
// is) whose pages are fixed layouts: runs of glyphs at stated places,
// paths and images, on pages of a stated size. That is what a PDF page
// is once its content stream is interpreted, and from there on the two
// are read by the same code: the text is put in reading order and made
// into paragraphs, headings, lists and tables, the pages are compared
// for their running lines, and a paragraph that goes on over a page
// break is found, all as for a PDF without tags.
//
// What differs is in internal/xps: the package, the pages' markup, the
// fonts. The package itself, its XML and the limits on what it may
// decompress to are go-fast-core's (opc, xmlscan).

// WarnFontUnreadable: text of a page of an XPS document is set in a
// font that the package does not have or that cannot be read. The text
// is what the page says it is; it is placed by the advances the page
// states, and by half the font size for a glyph without one.
const WarnFontUnreadable = xps.WarnFontUnreadable

// The messages of an encrypted package that is not read. The first is
// that of go-fast-ooxml for the same envelope.
const (
	rightsManaged = "encrypted package protected by rights management (Information Rights Management): its key is in a licence that a rights-management server issues to the people the document names, and this library contacts no server"
	passwordSet   = "encrypted package protected with a password, in the envelope of an encrypted Office document: this library does not decrypt it"
	unstated      = "encrypted package that is not protected with a password, and whose encryption is not stated"
)

var compoundSignature = []byte{0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1}

// openFixed opens data when it is an XPS or OpenXPS document. It returns
// nil and no error for anything else, which is then read as a PDF file;
// and an error for an XPS document that cannot be read, or for the
// envelope that holds an encrypted package, which an XPS document under
// rights management is in.
func openFixed(data []byte, budget int64) (*xps.Document, error) {
	switch {
	case bytes.HasPrefix(data, compoundSignature):
		cf, err := cfb.OpenDirectory(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			return nil, nil
		}
		rights := cf.Has("\x06DataSpaces", "DataSpaceInfo", "\x09DRMEncryptedDataSpace") ||
			cf.Has("\x06DataSpaces", "TransformInfo", "\x09DRMEncryptedTransform") || cf.Has("\x09DRMContent")
		// The envelope that the .NET packaging classes write around an XPS
		// document under rights management names its transform as it
		// likes, and keeps the licences of its users beside it, each in a
		// stream whose name begins "EUL-".
		for _, t := range cf.List("\x06DataSpaces", "TransformInfo") {
			for _, name := range cf.List("\x06DataSpaces", "TransformInfo", t) {
				rights = rights || strings.HasPrefix(name, "EUL-")
			}
		}
		switch {
		case rights:
			return nil, fmt.Errorf("gofastpdf: %s: %w", rightsManaged, gofastcore.ErrUnsupported)
		case cf.Has("EncryptionInfo") && cf.Has("EncryptedPackage"):
			return nil, fmt.Errorf("gofastpdf: %s: %w", passwordSet, gofastcore.ErrUnsupported)
		case cf.Has("\x06DataSpaces") || cf.Has("EncryptedPackage"):
			return nil, fmt.Errorf("gofastpdf: %s: %w", unstated, gofastcore.ErrUnsupported)
		}
		return nil, nil
	case bytes.HasPrefix(data, []byte("PK")):
		d, err := xps.Open(bytes.NewReader(data), int64(len(data)), xps.Limits{MaxDecodedBytes: budget})
		if errors.Is(err, xps.ErrNotXPS) {
			return nil, nil
		}
		if err != nil {
			return nil, fmt.Errorf("gofastpdf: %w", err)
		}
		return d, nil
	}
	return nil, nil
}

// Format returns the name of the file's format, as Source.Format has
// it: Format for a PDF file, FormatXPS or FormatOpenXPS.
func (f *File) Format() string {
	switch {
	case f.x == nil:
		return Format
	case f.x.OpenXPS:
		return FormatOpenXPS
	}
	return FormatXPS
}

// mime returns the media type of the file's format.
func (f *File) mime() string {
	if f.x != nil {
		return f.x.MIME()
	}
	return "application/pdf"
}

// describeFixed reads what an XPS document says about itself: the core
// properties of its package.
func (f *File) describeFixed() {
	d := &f.desc
	defer func() {
		if r := recover(); r != nil {
			*d = described{}
		}
	}()
	// The outline: the entries of the structure of each fixed document.
	entries, cut, many, damaged := f.x.Outline(f.textLimit)
	for _, e := range entries {
		d.outline = append(d.outline, gofastcore.OutlineItem{Title: fixedText(e.Title, &f.controls), Level: e.Level, Part: e.Page})
	}
	d.outlineCut = cut
	f.fixedOutlineMany, f.fixedStructureDamaged = many, damaged
	p, ok := f.x.Properties()
	if !ok {
		return
	}
	md := &d.meta
	val := func(s string) string { return fixedText(s, &f.controls) }
	md.Title, md.Subject, md.Language = val(p.Title), val(p.Subject), val(p.Language)
	// One field, one entry: it is free text and is not split.
	if a := val(p.Creator); a != "" {
		md.Authors = []string{a}
	}
	if k := val(p.Keywords); k != "" {
		md.Keywords = []string{k}
	}
	md.Created, md.Modified = packageTime(p.Created), packageTime(p.Modified)
	for _, e := range [][2]string{{"description", p.Description}, {"last_modified_by", p.LastModifiedBy}, {"revision", p.Revision},
		{"category", p.Category}, {"content_status", p.ContentStatus}} {
		if v := val(e[1]); v != "" {
			if md.Extra == nil {
				md.Extra = map[string]string{}
			}
			md.Extra[e[0]] = v
		}
	}
}

// fixedText returns a string of an XPS document that is text to be read,
// such as a title, as the strings of a PDF are given: without control
// characters, which rm counts, and with ligatures and presentation forms
// written as their letters.
func fixedText(s string, rm *removed) string {
	return charset.Letters(cleanText(s, rm))
}

// packageTime reads a date of the core properties: the W3C profile of
// ISO 8601, of which everything after the year may be missing. It
// returns the zero time for anything else.
func packageTime(s string) time.Time {
	s = string(bytes.TrimSpace([]byte(s)))
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05", "2006-01-02T15:04Z07:00", "2006-01-02T15:04", "2006-01-02", "2006-01", "2006"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

// fixedWarnings returns the warnings of an XPS document as a whole.
func (f *File) fixedWarnings(external bool) []gofastcore.Warning {
	var ws []gofastcore.Warning
	add := func(severity gofastcore.Severity, code, message string) {
		ws = append(ws, gofastcore.Warning{Code: code, Severity: severity, Message: cleanString(message)})
	}
	pkg := f.x.Package()
	if rec := pkg.Recovered(); rec != nil {
		add(gofastcore.SeverityWarning, gofastcore.WarnInputRepaired, rec.Message(pkg.Label))
	}
	if n := pkg.Ambiguous(); n > 0 {
		add(gofastcore.SeverityWarning, gofastcore.WarnInputAmbiguous,
			fmt.Sprintf("%d members of the archive can be read in more than one way (a name written twice, or sizes that disagree); each was read as the directory of the archive says", n))
	}
	if n := pkg.BrokenPieces(); n > 0 {
		add(gofastcore.SeverityWarning, gofastcore.WarnInputStreamDamaged,
			fmt.Sprintf("%d parts of the package are stored in pieces that do not make a whole; they are not read", n))
	}
	// What reading the structure found, when it has been read.
	f.describe()
	if f.fixedStructureDamaged {
		add(gofastcore.SeverityWarning, gofastcore.WarnInputStreamDamaged,
			"the part that holds the outline of the document is damaged; the entries after the damage are not read")
	}
	if f.fixedOutlineMany {
		add(gofastcore.SeverityError, gofastcore.WarnLimitExceeded,
			fmt.Sprintf("the outline has more than %d entries; the rest are not read", xps.MaxOutline))
	}
	if _, cut, damaged := f.x.Intents(0); cut || damaged {
		if damaged {
			add(gofastcore.SeverityWarning, gofastcore.WarnInputStreamDamaged,
				"a part that holds the signature definitions of the document is damaged; what stands after the damage is not read")
		}
		if cut {
			add(gofastcore.SeverityError, gofastcore.WarnLimitExceeded,
				"the signature definitions of the document say more than is read of what their signatures are to mean")
		}
	}
	if _, total := pkg.Trailing(); total > 0 {
		add(gofastcore.SeverityInfo, gofastcore.WarnInputTrailingData,
			fmt.Sprintf("%d bytes follow the end of the archive", total))
	}
	if n := f.x.TargetsCut; n > 0 {
		add(gofastcore.SeverityError, gofastcore.WarnLimitExceeded,
			fmt.Sprintf("the document lists %d addressable names more than are kept; links to them lead nowhere", n))
	}
	if n := f.x.PagesCut; n > 0 {
		add(gofastcore.SeverityError, gofastcore.WarnLimitExceeded,
			fmt.Sprintf("the document names %d pages more than the %d that are read", n, len(f.x.Pages)))
	}
	if external {
		add(gofastcore.SeverityWarning, gofastcore.WarnInputExternalReference, "the document refers to files or addresses outside itself other than by links; nothing was fetched")
	}
	if w, ok := f.controls.warning("the strings of the document: its properties and outline"); ok {
		ws = append(ws, w)
	}
	return ws
}

// fixedPage reads page i, from 0, of an XPS document into a part. As
// for a PDF page it never fails: a page that cannot be read is a part
// with part.failed.
func (e *Extractor) fixedPage(f *File, i int, w want, ws *workState) (out pageOut) {
	opts := &f.opts
	out.done = true
	part := &out.part
	part.Kind, part.Number = gofastcore.PartPage, i+1
	defer part.Finish()
	defer func() {
		if r := recover(); r != nil {
			width, height := part.Width, part.Height
			out = pageOut{done: true}
			part.Kind, part.Number, part.Width, part.Height = gofastcore.PartPage, i+1, width, height
			part.Warn(gofastcore.SeverityError, gofastcore.WarnPartFailed, internalError(r))
		}
	}()
	if panicHook != nil {
		panicHook("page")
	}
	var local workState
	if ws == nil {
		ws = &local
		defer local.release()
	}
	var lay layout.Result
	var info xps.PageInfo
	var res extract.Result
	var controls removed
	images := 0
	var tags []tag
	var tables *pageTables
	err := f.x.ReadPage(i, extract.Options{Words: w.lines, Rules: !e.NoRuledTables, Stop: w.stop, Worker: &ws.ex,
		Use: func(c *extract.Content) {
			// The page as the interpreter of PDF hands one over: in
			// points, from the bottom left.
			page := layout.Page{M: [6]float64{1, 0, 0, -1, 0, info.Height}, Width: info.Width, Height: info.Height}
			lopts := layout.Options{
				Worker: &ws.lay, Images: opts.Images != gofastcore.ImagesNone, Lines: w.lines,
				Spans: opts.Spans, OnPage: e.NoOffPageText,
			}
			// The links of the page, where they are as it is displayed.
			for i := range info.Links {
				l := &info.Links[i]
				lopts.Links = append(lopts.Links, layout.Link{Href: cleanText(l.Href, &controls),
					Boxes: []gofastcore.Rect{{X0: l.Box[0], Y0: info.Height - l.Box[3], X1: l.Box[2], Y1: info.Height - l.Box[1]}}})
			}
			// What the story fragments of the page declare of its
			// elements, used as the structure tree of a tagged PDF is.
			if m := info.Marks; m != nil {
				tags, tables = fixedTags(m)
				lopts.Item = func(mcid int32) (int64, bool) {
					if mcid >= 0 && int(mcid) < len(tags) {
						return tags[mcid].item, tags[mcid].label
					}
					return 0, false
				}
				lopts.Rank = func(mcid int32) int32 {
					if mcid >= 0 && int(mcid) < len(tags) {
						return tags[mcid].rank
					}
					return 0
				}
				// No heading is declared; asking makes the layout say of
				// each block which element it begins with.
				lopts.Heading = func(int32) uint8 { return 0 }
				lopts.Para = func(mcid int32) int32 {
					if mcid >= 0 && int(mcid) < len(m.Marks) {
						return m.Marks[mcid].Para
					}
					return 0
				}
				tables.options(tags, &lopts)
			}
			lay = layout.Build(page, c, lopts)
			if tables.drop(&lay) {
				// Tables that the page does not bear out are its text again.
				lay = layout.Build(page, c, lopts)
			}
			images = len(c.Images)
			for k, i := range lay.Images {
				if i < 0 {
					continue
				}
				// What the file says the image shows, where it draws it.
				// It is the text of the block: without white space around it.
				if alt := c.Images[i].Alt; alt != "" {
					lay.Blocks[k].Text = strings.TrimSpace(fixedText(alt, &controls))
				}
				out.images = append(out.images, imageRef{block: int32(k), part: c.Images[i].Name})
			}
		}}, &res, &ws.xps, &info, opts.Spans)
	out.external = info.External
	out.fonts = info.Fonts
	// The names the document lists as addressable on the page, each
	// once, where they are as the page is displayed.
	var seen map[string]bool
	for i := range info.Anchors {
		a := &info.Anchors[i]
		name := cleanText(a.Name, &controls)
		if name == "" || len(name) > maxAnchorBytes || seen[name] {
			continue
		}
		if seen == nil {
			seen = map[string]bool{}
		}
		seen[name] = true
		out.anchors = append(out.anchors, named{name: name, x: a.X, y: info.Height - a.Y})
	}
	part.Width, part.Height = info.Width, info.Height
	out.decoded = info.Decoded
	if info.Spent {
		// The limit on decompressed bytes was reached in this page: the
		// document ends here, as it does when the pages before it have
		// used the limit up.
		out.decoded = math.MaxInt64 / 4
	}
	out.lines, out.styles, out.stats = lay.Lines, lay.Styles, lay.Stats
	tables.fill(lay.Blocks, &lay)
	part.Blocks = lay.Blocks
	out.items = treeItems(lay.Styles, tags)
	if m := info.Marks; m != nil {
		// What stands in a fragment that the file declares to be the
		// header or the footer of the page is that.
		for k := range min(len(part.Blocks), len(lay.Styles)) {
			b := &part.Blocks[k]
			mc := lay.Styles[k].Marked - 1
			if mc < 0 || int(mc) >= len(m.Marks) || b.Kind != gofastcore.KindParagraph || b.Display != 0 {
				continue
			}
			switch m.Marks[mc].Kind {
			case xps.FragmentHeader:
				b.Kind, b.Origin = gofastcore.KindHeader, gofastcore.OriginNative
			case xps.FragmentFooter:
				b.Kind, b.Origin = gofastcore.KindFooter, gofastcore.OriginNative
			}
		}
	}
	part.Warnings = append(part.Warnings, res.Warnings...)
	// What the signature definitions of the document say a signature
	// asked for on this page is to mean: a note of the page, after its
	// content, as what an annotation of a PDF says is.
	var extras []extra
	if err == nil {
		intents, _, _ := f.x.Intents(i)
		for _, intent := range intents {
			if text := strings.TrimSpace(fixedText(intent, &controls)); text != "" {
				extras = append(extras, extra{block: gofastcore.Block{Kind: gofastcore.KindNote, Text: text,
					Attrs: map[string]string{gofastcore.AttrNoteType: gofastcore.NoteComment}}})
			}
		}
		out.addNotes(extras)
	}
	switch {
	case err != nil:
		part.Blocks, out.lines, out.styles, out.images = nil, nil, nil, nil
		part.Warn(gofastcore.SeverityError, gofastcore.WarnPartFailed, err.Error())
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
	controls.add(lay.Removed)
	for k := range part.Warnings {
		part.Warnings[k].Message = cleanString(part.Warnings[k].Message)
	}
	if w, ok := controls.warning("the strings of the page"); ok {
		part.Warnings = append(part.Warnings, w)
	}
	return out
}

// fixedImage completes one image block of a page of an XPS document: its
// ID, which is the name of its part, its media type and size, and, when
// data is asked for, its bytes as the package holds them, on the first
// block that shows the image.
func (st *state) fixedImage(part *gofastcore.Part, ref imageRef) {
	x := st.f.x
	img := part.Blocks[ref.block].Image
	if img == nil {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			img.MIME, img.Data = "", nil
			if !part.Has(gofastcore.WarnPartFailed) {
				part.Warn(gofastcore.SeverityError, gofastcore.WarnPartFailed, "an image on the page could not be read: "+internalError(r))
			}
		}
	}()
	if panicHook != nil {
		panicHook("image")
	}
	info := x.Image(ref.part)
	img.ID, img.MIME, img.Width, img.Height = info.ID, info.MIME, info.Width, info.Height
	if st.f.opts.Images != gofastcore.ImagesData || st.images[img.ID] {
		return
	}
	st.images[img.ID] = true
	if st.budget <= 0 {
		st.imagesCut = true
		return
	}
	data, over, err := x.ImageData(ref.part, st.budget)
	switch {
	case over:
		st.imagesCut = true
	case err != nil:
		if !part.Has(gofastcore.WarnImageUnsupportedEncoding) {
			part.Warn(gofastcore.SeverityWarning, gofastcore.WarnImageUnsupportedEncoding,
				"the data of an image on the page is not included: "+printable(err.Error()))
		}
	default:
		st.budget -= int64(len(data))
		img.Data = data
	}
}

// fixedTags turns what the story fragments of a page say of its named
// elements into what the structure tree of a tagged PDF says of marked
// content, by identifier: the list item and the place in the order of
// each, and the tables with their cells, which are then laid out, held
// against the page and filled as those of a tagged PDF are (tables.go).
// The cells and the tables are numbered from 1, in their order.
func fixedTags(m *xps.Marks) ([]tag, *pageTables) {
	tags := make([]tag, len(m.Marks))
	// A table larger than a table may be is not one: its text is laid
	// out as the page sets it.
	usable := make([]bool, len(m.Tables))
	any := false
	for t, g := range m.Tables {
		usable[t] = g.Rows > 0 && g.Cols > 0 && int64(g.Rows)*int64(g.Cols) <= maxTableCells
		any = any || usable[t]
	}
	for i := range m.Marks {
		mk, t := &m.Marks[i], &tags[i]
		t.item, t.label, t.rank = mk.Item, mk.Label, mk.Rank
		if c := mk.Cell; c >= 0 && int(c) < len(m.Cells) && usable[m.Cells[c].Table] {
			t.cell, t.table = int64(c)+1, int64(m.Cells[c].Table)+1
		}
	}
	if !any {
		return tags, nil
	}
	pt := &pageTables{cellOf: map[int64]int32{}}
	index := make([]int32, len(m.Tables)) // of each table, its place among those kept
	for t, g := range m.Tables {
		index[t] = -1
		if !usable[t] {
			continue
		}
		index[t] = int32(len(pt.tables))
		pt.tables = append(pt.tables, int64(t)+1)
		pt.grids = append(pt.grids, &tableGrid{cells: map[int64]gridCell{}, rows: g.Rows, cols: g.Cols, header: make([]bool, g.Rows)})
	}
	for i := range m.Cells {
		c := &m.Cells[i]
		k := index[c.Table]
		if k < 0 {
			continue
		}
		id := int64(i) + 1
		pt.grids[k].cells[id] = gridCell{row: c.Row, col: c.Col, rows: c.Rows, cols: c.Cols}
		pt.cellOf[id] = int32(len(pt.cells))
		pt.cells, pt.cellTable = append(pt.cells, id), append(pt.cellTable, k)
	}
	return tags, pt
}

// newFonts returns the bytes of the font programs that a page of an XPS
// document uses and no page before it used: a font is read once for a
// document, and is counted with the first of the pages that use it, in
// the order of the pages, so that where the limit on decoded bytes cuts
// a document does not depend on which worker read the font.
func (st *state) newFonts(out *pageOut) int64 {
	var n int64
	for _, f := range out.fonts {
		if st.fonts[f.Part] {
			continue
		}
		if st.fonts == nil {
			st.fonts = map[string]bool{}
		}
		st.fonts[f.Part] = true
		n += f.Bytes
	}
	return n
}
