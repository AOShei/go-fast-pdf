package gofastpdf

import (
	"fmt"
	"slices"

	gofastcore "github.com/AOShei/go-fast-core"
	"github.com/AOShei/go-fast-pdf/internal/file"
)

// What a fixed bound of this library leaves out is never left out
// silently: it is reported as limit.exceeded, an error, once for each
// bound. Each bound has a message of its own, which names it.

// Bounds whose cut is reported from several places, by one message.
var (
	msgNameCut        = fmt.Sprintf("a name is longer than %d bytes and is cut there: the name of an attached file, a form field or a script, an author, a key of the document information or the like", maxNameBytes)
	msgAttachmentsCut = fmt.Sprintf("the document has more than %d attached files; the rest are not read", maxAttachments)
	msgTreeCut        = fmt.Sprintf("a name tree or number tree has more than %d nodes or %d entries, or is nested more than %d deep; the rest of it (attached files, scripts, destinations or page labels) is not read", maxTreeNodes, maxTreeEntries, maxTreeDepth)
	msgScriptsCut     = fmt.Sprintf("the document has more than %d scripts; the rest are not read", maxScripts)
	msgFieldsCut      = fmt.Sprintf("the form has more than %d fields; the rest are not read", maxFields)
)

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

// cutWarn reports what a bound left out of the document as a whole.
// File.Warn keeps one warning of a message, so the report is made once
// however often the bound is met. f may be nil, where nothing is
// reported.
func cutWarn(f *file.File, message string) {
	if f != nil {
		f.Warn(gofastcore.SeverityError, gofastcore.WarnLimitExceeded, message)
	}
}

// pageLimits collects, for one page, what bounds left out of it: one
// message for each bound.
type pageLimits []string

func (l *pageLimits) add(message string) {
	if l != nil && !slices.Contains(*l, message) {
		*l = append(*l, message)
	}
}

// warn gives the part a limit.exceeded for each message.
func (l pageLimits) warn(part *gofastcore.Part) {
	for _, m := range l {
		partLimit(part, m)
	}
}

// partLimit gives the part a limit.exceeded with the message, unless it
// has one already.
func partLimit(part *gofastcore.Part, message string) {
	if !slices.ContainsFunc(part.Warnings, func(w gofastcore.Warning) bool {
		return w.Code == gofastcore.WarnLimitExceeded && w.Message == message
	}) {
		part.Warn(gofastcore.SeverityError, gofastcore.WarnLimitExceeded, message)
	}
}
