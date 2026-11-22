package layout

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// Markers of list items.
//
// A list item starts with its marker: a bullet, or a number or letter
// that counts the items. ListMarker reads one at the start of a text.
// The rules lean towards not seeing a marker where text happens to start
// with a number: a year (four digits) is not one, nor is a decimal
// number ("3.5 million"), nor a capital letter and a full stop, which is
// how a name can start ("J. Smith"). An em dash is not one either: prose
// sets it between words, and a line can start with it ("— which"); nor
// an asterisk, which more often marks a note than an item. A line whose
// text after the marker has a tab is a row of a table, not an item.

// bullets are the characters that mark the items of a list without
// counting them.
const bullets = "•◦▪▫‣⁃∙·●○■□◆◇►▸▹▶➢➤✓✔❖"

// ListMarker returns the marker a text starts with, the text after it,
// and whether the marker counts (a number or a letter). ok is false when
// the text does not start with one, or nothing follows it.
func ListMarker(text string) (marker, rest string, ordered, ok bool) {
	start, end, at, ordered, ok := splitMarker(text, false)
	if !ok {
		return "", "", false, false
	}
	return text[start:end], text[at:], ordered, true
}

// ItemMarker reads the marker of a text that is known to be a list
// item, as the structure tree of a tagged PDF says one is: as
// ListMarker, and also a capital letter or a capital Roman number
// before a full stop ("C.", "IV."), which ListMarker leaves to names,
// whatever tabs the text has after it.
func ItemMarker(text string) (marker, rest string, ordered, ok bool) {
	start, end, at, ordered, ok := splitMarker(text, true)
	if !ok {
		return "", "", false, false
	}
	return text[start:end], text[at:], ordered, true
}

// splitMarker finds the marker that text starts with: it is
// text[start:end], and what follows it starts at text[at:]. It reads a
// line of the layout as it is, without making a string of it. item is
// set for the text of a known item (ItemMarker).
func splitMarker[T ~string | ~[]byte](text T, item bool) (start, end, at int, ordered, ok bool) {
	for start < len(text) && (text[start] == ' ' || text[start] == '\t') {
		start++
	}
	// A marker is short: its first bytes are enough to read it, and a
	// string of them is made without allocating.
	head := text[start:min(len(text), start+24)]
	n, ordered := markerEnd(string(head), item)
	end = start + n
	if n == 0 || end >= len(text) || text[end] != ' ' && text[end] != '\t' {
		return 0, 0, 0, false, false
	}
	// The text after the marker starts after every space, a space
	// outside ASCII too (U+00A0, U+2003, ...): the text of a block has
	// none at its ends (the shared model).
	at = end
	for at < len(text) {
		r, size := utf8.DecodeRuneInString(string(text[at:min(len(text), at+utf8.UTFMax)]))
		if !unicode.IsSpace(r) {
			break
		}
		at += size
	}
	if at == len(text) {
		return 0, 0, 0, false, false
	}
	for i := at; i < len(text) && !item; i++ {
		if text[i] == '\t' {
			return 0, 0, 0, false, false // a row of a table
		}
	}
	if r, size := utf8.DecodeRuneInString(string(head[:n])); size == n && (r == '-' || r == '–') {
		// A dash marks an item when a word follows it, not a number
		// ("- 5 degrees").
		if r2, _ := utf8.DecodeRuneInString(string(text[at:min(len(text), at+4)])); !unicode.IsLetter(r2) {
			return 0, 0, 0, false, false
		}
	}
	return start, end, at, ordered, true
}

// mayStartItem reports, without making a string of it, whether a line
// can start with a marker: a character outside ASCII (bullets), a
// bracket, a dash or an asterisk, or a first word that ends with a full
// stop or a bracket. Most lines start with a word that does not.
func mayStartItem(t []byte) bool {
	for len(t) > 0 && (t[0] == ' ' || t[0] == '\t') {
		t = t[1:]
	}
	if len(t) == 0 {
		return false
	}
	switch c := t[0]; {
	case c >= 0x80, c == '(', c == '-', c == '*':
		return true
	case c >= '0' && c <= '9', c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z':
		for i := 1; i < len(t) && i <= 13; i++ {
			if t[i] == ' ' || t[i] == '\t' {
				return t[i-1] == '.' || t[i-1] == ')'
			}
		}
	}
	return false
}

// markerEnd returns where a marker at the start of s ends, 0 if there is
// none. item is set for the text of a known item, where a capital before
// a full stop counts.
func markerEnd(s string, item bool) (int, bool) {
	if s == "" {
		return 0, false
	}
	r, size := utf8.DecodeRuneInString(s)
	switch {
	case strings.ContainsRune(bullets, r) || r == '-' || r == '–':
		return size, false
	case r >= 0xF000 && r <= 0xF0FF:
		// A bullet of a symbol font, which has no Unicode of its own: such
		// fonts put their glyphs at F020 to F0FF. (Higher up are the pieces
		// of large brackets of Adobe's Symbol font.)
		return size, false
	case r == '(':
		// (3), (a), (iv)
		if i := strings.IndexByte(s, ')'); i > 1 && i <= 5 && counter(s[1:i], true) {
			return i + 1, true
		}
		return 0, false
	}
	// 1. 12) a. b) iv. A) 4.2.1 4.2.
	i := 0
	for i < len(s) && i < 12 && (s[i] >= '0' && s[i] <= '9' || s[i] == '.' || s[i] >= 'a' && s[i] <= 'z' || s[i] >= 'A' && s[i] <= 'Z') {
		i++
	}
	if i == 0 || i >= len(s) {
		return 0, false
	}
	word, closing := s[:i], s[i]
	switch {
	case closing == ')' && counter(word, true):
		return i + 1, true
	case strings.HasSuffix(word, ".") && (counter(word[:len(word)-1], item) || item && len(word) >= 2 && len(word) <= 6 && strings.Trim(word[:len(word)-1], "IVX") == ""):
		return i, true
	case clause(word):
		return i, true
	}
	return 0, false
}

// counter reports whether s counts items: a number of up to three
// digits, a letter, or a small Roman number. A capital letter counts
// only in brackets or before a bracket (capital), not before a full
// stop, which is how a name starts.
func counter(s string, capital bool) bool {
	switch {
	case s == "":
		return false
	case len(s) <= 3 && strings.Trim(s, "0123456789") == "":
		return true
	case len(s) == 1 && s[0] >= 'a' && s[0] <= 'z':
		return true
	case len(s) == 1 && s[0] >= 'A' && s[0] <= 'Z':
		return capital
	case len(s) <= 5 && strings.Trim(s, "ivx") == "":
		return true
	}
	return false
}

// clause reports whether s is a number of several levels, as a clause
// of a contract is numbered: 4.2.1, or 4.2. with a full stop at its end.
// Two numbers and no full stop after them are a decimal number.
func clause(s string) bool {
	dot := strings.HasSuffix(s, ".")
	s = strings.TrimSuffix(s, ".")
	parts, digits := 1, 0
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c == '.':
			if digits == 0 {
				return false
			}
			parts, digits = parts+1, 0
		case c >= '0' && c <= '9':
			if digits++; digits > 3 {
				return false
			}
		default:
			return false
		}
	}
	return digits > 0 && (parts >= 3 || parts == 2 && dot)
}
