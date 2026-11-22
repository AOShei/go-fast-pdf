// Package content reads content streams: the operators that draw a page
// and their operands.
package content

import (
	"github.com/AOShei/go-fast-pdf/internal/syntax"
)

// Kind is the type of an operand.
type Kind uint8

const (
	// Number: Raw is the numeric token.
	Number Kind = iota + 1
	// Name: Raw is the name without its slash, with #xx not yet decoded.
	Name
	// String: Raw is the body of a literal string between its
	// parentheses, with escapes not yet resolved.
	String
	// HexString: Raw is the hexadecimal digits between the brackets.
	HexString
	// Array: the elements are returned by Scanner.Elems.
	Array
	// Dict: Raw is the text between << and >>. It is not parsed here;
	// few operators take one and syntax.Parser reads it when needed.
	Dict
	// Other: true, false, null, or a nested array inside an array. Raw is
	// the token text.
	Other
)

// Operand is one operand of an operator. Raw points into the content
// stream: nothing is converted until the interpreter asks, so operands
// that are never used, such as the coordinates of a path, cost nothing.
type Operand struct {
	Kind   Kind
	Raw    []byte
	lo, hi int32 // element range of an Array
}

// Float returns the value of a Number, or 0.
func (o Operand) Float() float64 {
	if o.Kind != Number {
		return 0
	}
	v, _ := syntax.ParseNumber(o.Raw, 0)
	return v.Number()
}

// IsNumber reports whether o is a number.
func (o Operand) IsNumber() bool { return o.Kind == Number }

// IsString reports whether o is a literal or hexadecimal string.
func (o Operand) IsString() bool { return o.Kind == String || o.Kind == HexString }

// AppendString appends the bytes of a string operand to dst.
func (o Operand) AppendString(dst []byte) []byte {
	switch o.Kind {
	case String:
		return syntax.UnescapeString(dst, o.Raw)
	case HexString:
		return syntax.Unhex(dst, o.Raw)
	}
	return dst
}

// maxOperands bounds the operands kept for one operator, and MaxElems the
// elements of its arrays. A stream of numbers with no operator would
// otherwise grow the stack without limit.
const (
	maxOperands = 1024
	// MaxElems is the most elements of arrays kept for one operator;
	// ElemsCut says that there were more.
	MaxElems = 1 << 20
)

// Scanner splits a content stream into operators with their operands.
// It allocates nothing per operator: the operand stack is reused.
//
// Bytes that fit nowhere, such as a stray brace, are skipped and counted.
// A page's content can be split over several streams between any two
// tokens; Feed continues with the next stream and keeps the operands
// read so far.
type Scanner struct {
	data  []byte
	pos   int
	args  []Operand
	elems []Operand
	done  bool // the operands in args belong to an operator already returned

	// Skipped counts the bytes skipped as unreadable.
	Skipped int
	// ElemsCut counts the elements of operators' arrays left out past
	// MaxElems.
	ElemsCut int
	// InlineData is the data of the inline image whose BI operator was
	// returned last, and InlineDict the bytes of its dictionary's
	// entries, from after BI to before ID.
	InlineData, InlineDict []byte
}

// Reset starts a new page with its first content stream.
func (s *Scanner) Reset(data []byte) {
	s.data, s.pos = data, 0
	s.args, s.elems = s.args[:0], s.elems[:0]
	s.done, s.Skipped, s.ElemsCut, s.InlineData, s.InlineDict = false, 0, 0, nil, nil
}

// Release lets go of the stream and of the operands read from it, the
// room past their length included, keeping the room for the next.
func (s *Scanner) Release() {
	clear(s.args[:cap(s.args)]) // at most maxOperands
	if cap(s.elems) > 4096 {
		s.elems = nil // a long array once: not cleared, and not kept
	} else {
		clear(s.elems[:cap(s.elems)])
	}
	s.Reset(nil)
}

// Feed continues the same page with its next content stream.
func (s *Scanner) Feed(data []byte) { s.data, s.pos = data, 0 }

// Elems returns the elements of an Array operand. The slice is valid
// until the next call to Next.
func (s *Scanner) Elems(a Operand) []Operand {
	if a.Kind != Array {
		return nil
	}
	return s.elems[a.lo:a.hi]
}

// Next returns the next operator and its operands. ok is false at the
// end of the stream. The returned slices are valid until the next call.
//
// An inline image is returned as the operator BI with the entries of its
// dictionary as operands, alternating key and value; its data is in
// InlineData.
func (s *Scanner) Next() (op []byte, args []Operand, ok bool) {
	if s.done {
		s.args, s.elems, s.done = s.args[:0], s.elems[:0], false
	}
	data := s.data
	for {
		s.pos = syntax.SkipSpace(data, s.pos)
		if s.pos >= len(data) {
			return nil, nil, false
		}
		c := data[s.pos]
		if syntax.IsRegular(c) && !isNumberStart(c) {
			start := s.pos
			for s.pos < len(data) && syntax.IsRegular(data[s.pos]) {
				s.pos++
			}
			tok := data[start:s.pos]
			switch string(tok) {
			case "true", "false", "null":
				s.push(Operand{Kind: Other, Raw: tok})
				continue
			case "BI":
				s.inlineImage()
			}
			s.done = true
			return tok, s.args, true
		}
		if o, ok := s.operand(true); ok {
			s.push(o)
		}
	}
}

func isNumberStart(c byte) bool {
	return c >= '0' && c <= '9' || c == '-' || c == '+' || c == '.'
}

func (s *Scanner) push(o Operand) {
	if len(s.args) >= maxOperands {
		// No operator takes this many operands: the stream is damaged.
		// Keep the most recent ones.
		s.args = s.args[:0]
		s.Skipped++
	}
	s.args = append(s.args, o)
}

// operand reads the operand at s.pos, which is not white space and not a
// keyword. It returns false after skipping a byte that starts nothing.
// Arrays are expanded only at the top level.
func (s *Scanner) operand(top bool) (Operand, bool) {
	data := s.data
	start := s.pos
	switch c := data[s.pos]; {
	case isNumberStart(c):
		s.pos = numberEnd(data, s.pos)
		return Operand{Kind: Number, Raw: data[start:s.pos]}, true

	case c == '/':
		s.pos++
		for s.pos < len(data) && syntax.IsRegular(data[s.pos]) {
			s.pos++
		}
		return Operand{Kind: Name, Raw: data[start+1 : s.pos]}, true

	case c == '(':
		s.pos = stringEnd(data, s.pos)
		end := s.pos
		if end > start+1 && end <= len(data) && data[end-1] == ')' {
			end--
		}
		return Operand{Kind: String, Raw: data[start+1 : end]}, true

	case c == '<':
		if s.pos+1 < len(data) && data[s.pos+1] == '<' {
			s.pos = dictEnd(data, s.pos)
			end := s.pos
			if end >= start+4 && data[end-1] == '>' && data[end-2] == '>' {
				end -= 2
			}
			return Operand{Kind: Dict, Raw: data[start+2 : max(end, start+2)]}, true
		}
		s.pos++
		for s.pos < len(data) && data[s.pos] != '>' {
			s.pos++
		}
		raw := data[start+1 : s.pos]
		if s.pos < len(data) {
			s.pos++
		}
		return Operand{Kind: HexString, Raw: raw}, true

	case c == '[':
		if !top {
			// A nested array is kept as text.
			s.pos = arrayEnd(data, s.pos)
			return Operand{Kind: Other, Raw: data[start:s.pos]}, true
		}
		s.pos++
		lo := len(s.elems)
		for {
			s.pos = syntax.SkipSpace(data, s.pos)
			if s.pos >= len(data) {
				break // unterminated: keep what was read
			}
			e := data[s.pos]
			if e == ']' {
				s.pos++
				break
			}
			if syntax.IsRegular(e) && !isNumberStart(e) {
				// A keyword inside an array: true, false, null, or an
				// operator where the closing bracket is missing. Either
				// way it is not an element this reader uses.
				tokStart := s.pos
				for s.pos < len(data) && syntax.IsRegular(data[s.pos]) {
					s.pos++
				}
				if len(s.elems) < MaxElems {
					s.elems = append(s.elems, Operand{Kind: Other, Raw: data[tokStart:s.pos]})
				} else {
					s.ElemsCut++
				}
				continue
			}
			if el, ok := s.operand(false); ok {
				if len(s.elems) < MaxElems {
					s.elems = append(s.elems, el)
				} else {
					s.ElemsCut++
				}
			}
		}
		return Operand{Kind: Array, lo: int32(lo), hi: int32(len(s.elems))}, true
	}
	// A delimiter that starts nothing here: ) ] > { }
	s.pos++
	s.Skipped++
	return Operand{}, false
}

// numberEnd returns the position after the numeric token at pos: signs,
// digits, at most one point, digits.
func numberEnd(data []byte, pos int) int {
	i := pos
	for i < len(data) && (data[i] == '+' || data[i] == '-') {
		i++
	}
	for i < len(data) && data[i] >= '0' && data[i] <= '9' {
		i++
	}
	if i < len(data) && data[i] == '.' {
		i++
		for i < len(data) && data[i] >= '0' && data[i] <= '9' {
			i++
		}
	}
	if i == pos {
		i++
	}
	return i
}

// stringEnd returns the position after the literal string that starts at
// pos, which holds its opening parenthesis.
func stringEnd(data []byte, pos int) int {
	depth := 0
	for i := pos; i < len(data); i++ {
		switch data[i] {
		case '\\':
			i++
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i + 1
			}
		}
	}
	return len(data)
}

// dictEnd returns the position after the dictionary that starts at pos,
// stepping over the strings inside it. It does not recurse.
func dictEnd(data []byte, pos int) int {
	depth := 0
	for i := pos; i < len(data); {
		switch data[i] {
		case '(':
			i = stringEnd(data, i)
		case '<':
			if i+1 < len(data) && data[i+1] == '<' {
				depth++
				i += 2
				continue
			}
			for i < len(data) && data[i] != '>' {
				i++
			}
			i++
		case '>':
			if i+1 < len(data) && data[i+1] == '>' {
				depth--
				i += 2
				if depth <= 0 {
					return i
				}
				continue
			}
			i++
		default:
			i++
		}
	}
	return len(data)
}

// arrayEnd returns the position after the array that starts at pos,
// stepping over strings and nested arrays. It does not recurse.
func arrayEnd(data []byte, pos int) int {
	depth := 0
	for i := pos; i < len(data); {
		switch data[i] {
		case '(':
			i = stringEnd(data, i)
		case '[':
			depth++
			i++
		case ']':
			depth--
			i++
			if depth <= 0 {
				return i
			}
		default:
			i++
		}
	}
	return len(data)
}

// inlineImage reads an inline image after its BI keyword: the dictionary
// entries up to ID become the operands, and the data up to EI is stepped
// over.
func (s *Scanner) inlineImage() {
	data := s.data
	s.args, s.elems = s.args[:0], s.elems[:0]
	dictStart := s.pos
	s.InlineDict = nil
	for {
		s.pos = syntax.SkipSpace(data, s.pos)
		if s.pos >= len(data) {
			s.InlineData = nil
			return
		}
		c := data[s.pos]
		if syntax.IsRegular(c) && !isNumberStart(c) {
			start := s.pos
			for s.pos < len(data) && syntax.IsRegular(data[s.pos]) {
				s.pos++
			}
			if string(data[start:s.pos]) == "ID" {
				s.InlineDict = data[dictStart:start]
				break
			}
			s.push(Operand{Kind: Other, Raw: data[start:s.pos]})
			continue
		}
		if o, ok := s.operand(true); ok {
			s.push(o)
		}
	}
	// One white-space character separates ID from the data.
	if s.pos < len(data) && data[s.pos] == '\r' {
		s.pos++
	}
	if s.pos < len(data) && data[s.pos] == '\n' {
		s.pos++
	} else if s.pos < len(data) && data[s.pos] == ' ' {
		s.pos++
	}
	start := s.pos
	// The data ends at an EI that stands alone and is followed by
	// something that looks like content, not like more binary data.
	for i := start; i+1 < len(data); i++ {
		if data[i] != 'E' || data[i+1] != 'I' {
			continue
		}
		if i > start && !syntax.IsSpace(data[i-1]) {
			continue
		}
		if i+2 < len(data) && !syntax.IsSpace(data[i+2]) {
			continue
		}
		if !looksLikeContent(data[min(i+2, len(data)):]) {
			continue
		}
		s.InlineData = data[start:i]
		s.pos = i + 2
		return
	}
	// No EI: the image runs to the end of the stream.
	s.InlineData = data[start:]
	s.pos = len(data)
}

// looksLikeContent reports whether the next few bytes could be operators
// and operands: printable ASCII and white space only.
func looksLikeContent(data []byte) bool {
	for i := 0; i < len(data) && i < 16; i++ {
		c := data[i]
		if c > 0x7E || (c < 0x20 && !syntax.IsSpace(c)) {
			return false
		}
	}
	return true
}
