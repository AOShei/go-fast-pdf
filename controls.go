package gofastpdf

import (
	"fmt"
	"sync/atomic"

	gofastcore "github.com/AOShei/go-fast-core"
)

// removed counts the control characters that cleanText removed from the
// strings of one part, or of the document: its information, outline,
// page labels, names and attached files. The part or the document says
// how many with text.control_characters_removed. Strings of the
// document can be read by any worker, so the count is atomic. A nil
// *removed counts nothing.
//
// Not counted: a glyph of a page whose text is a control character,
// which is a mapping that is wrong and becomes U+FFFD with
// text.unmapped_glyphs, and a byte that PDFDocEncoding leaves undefined,
// which has no character and becomes U+FFFD.
type removed struct{ n atomic.Int64 }

func (r *removed) add(n int) {
	if r != nil && n > 0 {
		r.n.Add(int64(n))
	}
}

func (r *removed) count() int64 {
	if r == nil {
		return 0
	}
	return r.n.Load()
}

// warning returns text.control_characters_removed for what r counted,
// of strings that where says where they were; ok is false when there
// were none.
func (r *removed) warning(where string) (w gofastcore.Warning, ok bool) {
	n := r.count()
	if n == 0 {
		return w, false
	}
	return gofastcore.Warning{Code: gofastcore.WarnTextControlCharactersRemoved, Severity: gofastcore.SeverityInfo,
		Message: fmt.Sprintf("%d control %s removed from %s", n, plural(n, "character was", "characters were"), where)}, true
}

func plural(n int64, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// cleanCounted is cleanText for a string that may not be kept: it
// returns the number of control characters removed, for the caller to
// count if it keeps the string.
func cleanCounted(s string) (string, int64) {
	var rm removed
	s = cleanText(s, &rm)
	return s, rm.count()
}
