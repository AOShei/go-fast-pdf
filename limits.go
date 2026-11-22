package gofastpdf

import (
	"fmt"

	gofastcore "github.com/AOShei/go-fast-core"
	"github.com/AOShei/go-fast-pdf/internal/file"
)

// What a fixed bound of this library leaves out of what reaches the
// output (text, names, values, addresses) is reported as limit.exceeded,
// an error, once for each bound on the part or the document it was cut
// from, with a count: go-fast-core's rule. What is cut is counted while
// it is read (file.Cuts) and the warning made at the end, so that it does
// not depend on the order of reading. A bound on what is used only inside
// the library is not reported, and an ordinary file does not reach a
// bound: those that files of the corpus reached were raised.

// Bounds of the reading of XML: XFA forms and the metadata packet.
const (
	maxXFAPieces = 4096 // pieces of text of one field
	maxXFADepth  = 64
	// A program's list in the packet, such as Illustrator's swatches,
	// has hundreds of values: 512 is the most in the corpus.
	maxXMPValues = 1 << 14 // values of one property
	maxXMPTotal  = 1 << 16 // values of the whole packet
	maxXMLAttrs  = 256     // attributes of one element
)

// The bounds, by what they leave out.
var (
	// Of the document.
	boundName        = &file.Bound{Key: "names", Message: file.CutMessage("names", "names", maxNameBytes)}
	boundAttachments = &file.Bound{Key: "attached files", Message: file.PastMessage("attached files", "files", maxAttachments)}
	boundScripts     = &file.Bound{Key: "scripts", Message: file.PastMessage("scripts", "scripts", maxScripts)}
	boundFields      = &file.Bound{Key: "form fields", Message: file.MoreMessage("the form", "fields", maxFields)}
	boundFieldDepth  = &file.Bound{Key: "form field levels", Message: func(n, _ int64) string {
		return fmt.Sprintf("the names of form fields past %d levels of fields above them were not read (%d fields)", maxFieldDepth, n)
	}}
	boundChoices      = &file.Bound{Key: "choice options", Message: file.PastMessage("options of choice fields", "options", maxChoices)}
	boundOutline      = &file.Bound{Key: "outline entries", Message: file.MoreMessage("the outline", "entries", maxOutlineItems)}
	boundOutlineDepth = &file.Bound{Key: "outline levels", Message: file.MoreMessage("the outline", "levels", maxOutlineDepth)}
	boundTree         = &file.Bound{Key: "name trees", Message: func(n, _ int64) string {
		return fmt.Sprintf("name trees and number trees past %d nodes, %d entries or %d levels were not read further: attached files, scripts, destinations or page labels (%d trees)", maxTreeNodes, maxTreeEntries, maxTreeDepth, n)
	}}
	boundInfo      = &file.Bound{Key: "information entries", Message: file.PastMessage("entries of the document information", "entries", maxVariables)}
	boundXMPProps  = &file.Bound{Key: "metadata properties", Message: file.PastMessage("properties of the metadata packet", "properties", maxVariables)}
	boundXMPValues = &file.Bound{Key: "metadata values", Message: file.PastMessage("values of one property of the metadata packet", "values", maxXMPValues)}
	boundXMPTotal  = &file.Bound{Key: "metadata values, all", Message: file.PastMessage("values of the metadata packet", "values", maxXMPTotal)}
	boundXMLAttrs  = &file.Bound{Key: "xml attributes", Message: file.PastMessage("attributes of one XML element", "attributes", maxXMLAttrs)}
	boundXFAPieces = &file.Bound{Key: "xfa pieces", Message: file.PastMessage("pieces of text of one field of the XFA form's data", "pieces", maxXFAPieces)}
	boundXFADepth  = &file.Bound{Key: "xfa levels", Message: file.MoreMessage("the XFA form's data", "levels", maxXFADepth)}

	// Of a page.
	boundHref = &file.Bound{Key: "link addresses", Message: func(n, _ int64) string {
		return fmt.Sprintf("links whose address is longer than %d bytes were not kept (%d links)", maxHrefBytes, n)
	}}
	boundAlt   = &file.Bound{Key: "figure descriptions", Message: file.CutMessage("descriptions of figures", "descriptions", maxAltBytes)}
	boundMCIDs = &file.Bound{Key: "marked-content identifiers", Message: func(n, _ int64) string {
		return fmt.Sprintf("marked-content identifiers of a page past %d were not looked up in the structure tree, for their roles, headings, table cells and figure descriptions; their text is read (%d identifiers)", maxPageMCIDs, n)
	}}
	boundImagePixels = &file.Bound{Key: "image pixels", Message: func(n, _ int64) string {
		return fmt.Sprintf("images of more than %d pixels, or %d on a side, were not given their data (%d images)", maxImagePixels, 1<<20, n)
	}}
	boundImageWork = &file.Bound{Key: "image work", Message: func(n, _ int64) string {
		return fmt.Sprintf("images whose colour space's function would take more than %d operations, or more than the %d of a document, were not given their data (%d images)", maxFunctionWork, maxFunctionWorkAll, n)
	}}
)

// cutDoc counts what a bound left out of the document; f may be nil, where
// nothing is counted.
func cutDoc(f *file.File, b *file.Bound, n, bytes int64) {
	if f != nil && n > 0 {
		f.Cut(b, n, bytes)
	}
}

// cutOnce counts what a bound left out of what id names, once.
func cutOnce(f *file.File, b *file.Bound, id string, n, bytes int64) {
	if f != nil && n > 0 {
		f.CutOnce(b, id, n, bytes)
	}
}

// cutFunc counts what a bound left out; id, when it is not "", names
// what it was left out of, to count it once. The readers of XML count
// through one.
type cutFunc func(b *file.Bound, id string, n, bytes int64)

// cutter is the cutFunc of the document of f.
func cutter(f *file.File) cutFunc {
	return func(b *file.Bound, id string, n, bytes int64) {
		if id == "" {
			cutDoc(f, b, n, bytes)
		} else {
			cutOnce(f, b, id, n, bytes)
		}
	}
}

// warnCuts gives the part the warnings of what bounds left out of it.
func warnCuts(part *gofastcore.Part, c *file.Cuts) {
	for _, w := range c.Warnings() {
		part.Warn(w.Severity, w.Code, w.Message)
	}
	c.Reset()
}
