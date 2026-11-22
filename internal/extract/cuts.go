package extract

import (
	"fmt"

	"github.com/AOShei/go-fast-pdf/internal/content"
	"github.com/AOShei/go-fast-pdf/internal/file"
	"github.com/AOShei/go-fast-pdf/internal/font"
)

// What the fixed bounds of a page leave out of what reaches the output is
// counted (interp.cut) and given as limit.exceeded when the page is done,
// one warning for each bound with what was counted. A bound on what is
// used only inside the library, such as the saved graphics states, is
// not reported.
var (
	boundAnnots = &file.Bound{Key: "annotations", Message: file.PastMessage("annotations of a page", "annotations", MaxAnnotations)}
	boundElems  = &file.Bound{Key: "array elements", Message: file.PastMessage("elements of an operator's arrays", "elements", content.MaxElems)}
	boundMarked = &file.Bound{Key: "marked content", Message: file.PastMessage("marked-content sequences nested", "sequences", maxMarkedDepth)}
	boundImages = &file.Bound{Key: "images", Message: file.PastMessage("images of a page", "images", maxImagesPerPage)}
	boundForms  = &file.Bound{Key: "forms", Message: func(n, _ int64) string {
		return fmt.Sprintf("forms (Form XObjects) nested deeper than the limit, or past %d on a page, were not read (%d forms)", maxFormsPerPage, n)
	}}
	boundText = &file.Bound{Key: "text", Message: func(int64, int64) string {
		return fmt.Sprintf("the page has more than %d bytes of text, or more than %d pieces of it; the rest were not read", maxTextPerPage, maxRunsPerPage)
	}}
	boundInline = &file.Bound{Key: "inline dictionaries", Message: func(n, _ int64) string {
		return fmt.Sprintf("dictionaries of inline images longer than %d bytes were not read, and their images have no size or encoding (%d dictionaries)", maxInlineDict, n)
	}}
	boundCodeText = &file.Bound{Key: "font code texts", Message: file.CutMessage("texts of character codes in ToUnicode maps", "texts", font.MaxCodeText)}
	boundCMap     = &file.Bound{Key: "font maps", Message: func(n, _ int64) string {
		return fmt.Sprintf("CMaps of fonts past %d entries, or their text past %d bytes, were not read, and the rest of their codes may have the wrong text (%d CMaps)", font.MaxCMapEntries, font.MaxCMapBytes, n)
	}}
	boundProgram = &file.Bound{Key: "font programs", Message: func(n, _ int64) string {
		return fmt.Sprintf("font programs longer than %d bytes were cut there, and the text read from them may be wrong (%d programs)", font.MaxProgramBytes, n)
	}}
)

// cut counts what a bound left out of the page.
func (in *interp) cut(b *file.Bound, n, bytes int64) { in.cuts.Add(b, n, bytes) }

// fontCuts counts, once for each font of the page, what bounds left out
// of the tables its text is made from.
func (in *interp) fontCuts(f *font.Font) {
	if !f.Cut() || in.cutFonts[f] {
		return
	}
	if in.cutFonts == nil {
		in.cutFonts = map[*font.Font]bool{}
	}
	in.cutFonts[f] = true
	c := f.Cuts()
	if c.Texts > 0 {
		in.cut(boundCodeText, int64(c.Texts), c.TextBytes)
	}
	if c.Map {
		in.cut(boundCMap, 1, 0)
	}
	if c.Program {
		in.cut(boundProgram, 1, 0)
	}
}

// cutWarnings gives the page the warnings of what its bounds left out.
func (in *interp) cutWarnings() {
	in.res.Warnings = append(in.res.Warnings, in.cuts.Warnings()...)
	in.cuts.Reset()
	clear(in.cutFonts)
}
