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
	p, ok := f.x.Properties()
	if !ok {
		return
	}
	md := &d.meta
	val := func(s string) string { return cleanText(s, &f.controls) }
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
	err := f.x.ReadPage(i, extract.Options{Words: w.lines, Rules: !e.NoRuledTables, Stop: w.stop, Worker: &ws.ex,
		Use: func(c *extract.Content) {
			// The page as the interpreter of PDF hands one over: in
			// points, from the bottom left.
			page := layout.Page{M: [6]float64{1, 0, 0, -1, 0, info.Height}, Width: info.Width, Height: info.Height}
			lay = layout.Build(page, c, layout.Options{
				Worker: &ws.lay, Images: opts.Images != gofastcore.ImagesNone, Lines: w.lines,
				Spans: opts.Spans, OnPage: e.NoOffPageText,
			})
		}}, &res, &ws.xps, &info)
	part.Width, part.Height = info.Width, info.Height
	out.decoded = info.Decoded
	if info.Spent {
		// The limit on decompressed bytes was reached in this page: the
		// document ends here, as it does when the pages before it have
		// used the limit up.
		out.decoded = math.MaxInt64 / 4
	}
	out.lines, out.styles, out.stats = lay.Lines, lay.Styles, lay.Stats
	part.Blocks = lay.Blocks
	part.Warnings = append(part.Warnings, res.Warnings...)
	switch {
	case err != nil:
		part.Blocks, out.lines, out.styles = nil, nil, nil
		part.Warn(gofastcore.SeverityError, gofastcore.WarnPartFailed, err.Error())
	case res.Err != nil:
		part.Warn(gofastcore.SeverityError, gofastcore.WarnPartFailed, res.Err.Error())
	case lay.HasText:
	default:
		part.Warn(gofastcore.SeverityInfo, gofastcore.WarnPartNoText, "the page has no text and no images")
	}
	if lay.Controls > 0 && !part.Has(gofastcore.WarnTextUnmappedGlyphs) {
		part.Warn(gofastcore.SeverityWarning, gofastcore.WarnTextUnmappedGlyphs,
			fmt.Sprintf("%d characters on the page are control characters and appear as U+FFFD", lay.Controls))
	}
	var controls removed
	controls.add(lay.Removed)
	for k := range part.Warnings {
		part.Warnings[k].Message = cleanString(part.Warnings[k].Message)
	}
	if w, ok := controls.warning("the strings of the page"); ok {
		part.Warnings = append(part.Warnings, w)
	}
	return out
}
