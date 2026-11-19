package syntax

import (
	"errors"
	"io"
	"math"
	"sync"
)

// Errors returned by the parser. io.EOF means there was no object left.
var (
	// ErrSyntax: the bytes at the position do not start an object.
	ErrSyntax = errors.New("pdf syntax error")
	// ErrTooDeep: arrays and dictionaries are nested beyond the limit.
	ErrTooDeep = errors.New("pdf objects nested too deeply")
)

// DefaultMaxDepth is the nesting limit when a Parser is given none. Real
// files stay below ten.
const DefaultMaxDepth = 100

// Parser reads objects from a byte slice, starting at a position.
//
// It is tolerant inside containers: a stray byte in an array or a
// dictionary is skipped and counted in Skipped, so that one bad byte does
// not cost the object.
type Parser struct {
	data     []byte
	pos      int
	maxDepth int
	// Skipped counts the bytes that were skipped as unreadable.
	Skipped int
	// stack holds the elements of the arrays and dictionaries that are
	// open, while they are read. A container gets a list of its own, of
	// the size it turned out to have, when it closes.
	stack []Value
	box   *[]Value
	// opened is the number of containers that are open.
	opened int
}

// stacks keeps the room for elements between objects. Without it a
// list would be grown step by step for every array and dictionary, and
// that was a third of all the memory allocated in reading a file.
var stacks = sync.Pool{New: func() any {
	s := make([]Value, 0, 64)
	return &s
}}

// maxKeptStack is the largest room that is kept for the next object.
const maxKeptStack = 4096

// open starts a container and returns where its elements begin.
func (p *Parser) open() int {
	if p.box == nil {
		p.box = stacks.Get().(*[]Value)
		p.stack = (*p.box)[:0]
	}
	p.opened++
	return len(p.stack)
}

// close ends the container whose elements begin at base and returns
// them, in a list of their own.
func (p *Parser) close(base int) []Value {
	var list []Value
	if n := len(p.stack) - base; n > 0 {
		list = make([]Value, n)
		copy(list, p.stack[base:])
		clear(p.stack[base:]) // nothing of the file is held by the room that is kept
	}
	p.stack = p.stack[:base]
	if p.opened--; p.opened == 0 {
		if cap(p.stack) <= maxKeptStack {
			*p.box = p.stack
			stacks.Put(p.box)
		}
		p.stack, p.box = nil, nil
	}
	return list
}

// NewParser returns a parser positioned at pos. maxDepth bounds the
// nesting of arrays and dictionaries; 0 means DefaultMaxDepth.
func NewParser(data []byte, pos, maxDepth int) Parser {
	if maxDepth <= 0 {
		maxDepth = DefaultMaxDepth
	}
	return Parser{data: data, pos: max(pos, 0), maxDepth: maxDepth}
}

// Pos returns the position after the last object read.
func (p *Parser) Pos() int { return p.pos }

// SetPos moves the parser.
func (p *Parser) SetPos(pos int) { p.pos = pos }

// Object reads the next object. Two integers followed by R are returned
// as one Ref. At the end of the data it returns io.EOF; at a byte that
// cannot start an object it returns ErrSyntax and stays at that byte.
func (p *Parser) Object() (Value, error) { return p.object(0) }

func (p *Parser) object(depth int) (Value, error) {
	p.pos = SkipSpace(p.data, p.pos)
	if p.pos >= len(p.data) {
		return Value{}, io.EOF
	}
	switch c := p.data[p.pos]; {
	case c == '/':
		return p.name(), nil
	case c == '(':
		return p.literalString(), nil
	case c == '<':
		if p.pos+1 < len(p.data) && p.data[p.pos+1] == '<' {
			return p.dict(depth)
		}
		return p.hexString(), nil
	case c == '[':
		return p.array(depth)
	case c >= '0' && c <= '9', c == '+', c == '-', c == '.':
		return p.numberOrRef(), nil
	case class[c] == classDelim:
		return Value{}, ErrSyntax
	}
	return p.keyword(), nil
}

func (p *Parser) keyword() Value {
	start := p.pos
	for p.pos < len(p.data) && class[p.data[p.pos]] == classRegular {
		p.pos++
	}
	tok := p.data[start:p.pos]
	switch string(tok) {
	case "true":
		return Value{Kind: Bool, N: 1}
	case "false":
		return Value{Kind: Bool}
	case "null":
		return Value{}
	}
	return Value{Kind: Keyword, Data: tok}
}

func (p *Parser) name() Value {
	p.pos++ // the slash
	start := p.pos
	escaped := false
	for p.pos < len(p.data) && class[p.data[p.pos]] == classRegular {
		if p.data[p.pos] == '#' {
			escaped = true
		}
		p.pos++
	}
	raw := p.data[start:p.pos]
	if !escaped {
		return Value{Kind: Name, Data: raw}
	}
	out := make([]byte, 0, len(raw))
	for i := 0; i < len(raw); i++ {
		if raw[i] == '#' && i+2 < len(raw) && isHex(raw[i+1]) && isHex(raw[i+2]) {
			out = append(out, unhex(raw[i+1])<<4|unhex(raw[i+2]))
			i += 2
			continue
		}
		out = append(out, raw[i])
	}
	return Value{Kind: Name, Data: out}
}

func isHex(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}

func unhex(c byte) byte {
	switch {
	case c >= '0' && c <= '9':
		return c - '0'
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10
	}
	return c - 'A' + 10
}

// literalString reads a string in parentheses. An unterminated string
// ends at the end of the data.
func (p *Parser) literalString() Value {
	p.pos++ // the opening parenthesis
	start := p.pos
	depth, plain := 1, true
scan:
	for p.pos < len(p.data) {
		switch p.data[p.pos] {
		case '\\':
			plain = false
			p.pos++ // the escaped byte is skipped below
		case '\r':
			plain = false
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				break scan
			}
		}
		p.pos++
	}
	end := min(p.pos, len(p.data))
	raw := p.data[start:end]
	p.pos = min(end+1, len(p.data)) // the closing parenthesis
	if plain {
		return Value{Kind: String, Data: raw}
	}
	return Value{Kind: String, Data: UnescapeString(make([]byte, 0, len(raw)), raw)}
}

// UnescapeString appends to dst the bytes a literal string body stands
// for: backslash escapes are resolved and an end of line in any form
// becomes a line feed.
func UnescapeString(dst, raw []byte) []byte {
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		switch c {
		case '\r':
			if i+1 < len(raw) && raw[i+1] == '\n' {
				i++
			}
			dst = append(dst, '\n')
		case '\\':
			i++
			if i >= len(raw) {
				return dst
			}
			switch e := raw[i]; e {
			case 'n':
				dst = append(dst, '\n')
			case 'r':
				dst = append(dst, '\r')
			case 't':
				dst = append(dst, '\t')
			case 'b':
				dst = append(dst, '\b')
			case 'f':
				dst = append(dst, '\f')
			case '\r': // line continuation
				if i+1 < len(raw) && raw[i+1] == '\n' {
					i++
				}
			case '\n':
			default:
				if e >= '0' && e <= '7' {
					v := int(e - '0')
					for k := 0; k < 2 && i+1 < len(raw) && raw[i+1] >= '0' && raw[i+1] <= '7'; k++ {
						i++
						v = v<<3 | int(raw[i]-'0')
					}
					dst = append(dst, byte(v))
				} else {
					dst = append(dst, e) // \( \) \\ and anything else
				}
			}
		default:
			dst = append(dst, c)
		}
	}
	return dst
}

func (p *Parser) hexString() Value {
	p.pos++ // the opening bracket
	start := p.pos
	for p.pos < len(p.data) && p.data[p.pos] != '>' {
		p.pos++
	}
	raw := p.data[start:p.pos]
	if p.pos < len(p.data) {
		p.pos++
	}
	return Value{Kind: String, Hex: true, Data: Unhex(make([]byte, 0, len(raw)/2+1), raw)}
}

// Unhex appends to dst the bytes that hexadecimal digits stand for.
// Anything that is not a digit is ignored, and an odd final digit is
// taken as the high half of a byte.
func Unhex(dst, raw []byte) []byte {
	var hi byte
	half := false
	for _, c := range raw {
		if !isHex(c) {
			continue
		}
		if half {
			dst = append(dst, hi<<4|unhex(c))
		} else {
			hi = unhex(c)
		}
		half = !half
	}
	if half {
		dst = append(dst, hi<<4)
	}
	return dst
}

func (p *Parser) array(depth int) (Value, error) {
	if depth >= p.maxDepth {
		return Value{}, ErrTooDeep
	}
	p.pos++ // the opening bracket
	base := p.open()
	for {
		p.pos = SkipSpace(p.data, p.pos)
		if p.pos >= len(p.data) {
			break // unterminated: keep what was read
		}
		if p.data[p.pos] == ']' {
			p.pos++
			break
		}
		v, err := p.object(depth + 1)
		if err == ErrTooDeep {
			p.close(base)
			return Value{}, err
		}
		if err != nil {
			p.pos++ // a byte that starts nothing
			p.Skipped++
			continue
		}
		p.stack = append(p.stack, v)
	}
	return Value{Kind: Array, List: p.close(base)}, nil
}

func (p *Parser) dict(depth int) (Value, error) {
	if depth >= p.maxDepth {
		return Value{}, ErrTooDeep
	}
	p.pos += 2 // the opening brackets
	base := p.open()
	for {
		p.pos = SkipSpace(p.data, p.pos)
		if p.pos >= len(p.data) {
			break // unterminated: keep what was read
		}
		if p.data[p.pos] == '>' {
			p.pos++
			if p.pos < len(p.data) && p.data[p.pos] == '>' {
				p.pos++
			}
			break
		}
		if p.data[p.pos] != '/' {
			// Not a key. Read past whatever it is.
			if _, err := p.object(depth + 1); err == ErrTooDeep {
				p.close(base)
				return Value{}, err
			} else if err != nil {
				p.pos++
			}
			p.Skipped++
			continue
		}
		key := p.name()
		p.pos = SkipSpace(p.data, p.pos)
		// A key directly followed by the end of the dictionary or by
		// another key has no value: it reads as null.
		var val Value
		if p.pos < len(p.data) && p.data[p.pos] != '>' {
			v, err := p.object(depth + 1)
			if err == ErrTooDeep {
				p.close(base)
				return Value{}, err
			}
			if err != nil {
				p.pos++
				p.Skipped++
				continue
			}
			val = v
		}
		p.stack = append(p.stack, key, val)
	}
	return Value{Kind: Dict, List: p.close(base)}, nil
}

// numberOrRef reads a number and, when it is a non-negative integer
// followed by another and by R, the whole indirect reference.
func (p *Parser) numberOrRef() Value {
	v := p.number()
	if v.Kind != Int || v.N < 0 {
		return v
	}
	// Look ahead for "generation R".
	q := SkipSpace(p.data, p.pos)
	gen, digits := 0, 0
	for q < len(p.data) && p.data[q] >= '0' && p.data[q] <= '9' && digits < 6 {
		gen = gen*10 + int(p.data[q]-'0')
		q++
		digits++
	}
	if digits == 0 || q >= len(p.data) || class[p.data[q]] != classSpace {
		return v
	}
	q = SkipSpace(p.data, q)
	if q < len(p.data) && p.data[q] == 'R' && (q+1 == len(p.data) || class[p.data[q+1]] != classRegular) {
		p.pos = q + 1
		return Value{Kind: Ref, N: v.N, Gen: uint16(min(gen, math.MaxUint16))}
	}
	return v
}

// number reads a numeric token. PDF numbers have no exponent. The token
// ends where the number does, so "1-2" reads as two numbers, as other
// readers treat it.
func (p *Parser) number() Value {
	v, end := ParseNumber(p.data, p.pos)
	if end == p.pos {
		end++ // cannot happen for a byte that starts a number; stay safe
	}
	p.pos = end
	return v
}

// ParseNumber reads the number that starts at pos and returns it with the
// position after it. A sign or a point with no digits reads as 0.
func ParseNumber(data []byte, pos int) (Value, int) {
	i := pos
	neg := false
	if i < len(data) && (data[i] == '+' || data[i] == '-') {
		neg = data[i] == '-'
		i++
		// Some producers write a doubled sign.
		for i < len(data) && (data[i] == '+' || data[i] == '-') {
			i++
		}
	}
	var whole uint64
	overflow := false
	var f float64
	for i < len(data) && data[i] >= '0' && data[i] <= '9' {
		d := uint64(data[i] - '0')
		if whole > (math.MaxInt64-d)/10 {
			overflow = true
		}
		whole = whole*10 + d
		f = f*10 + float64(d)
		i++
	}
	if i >= len(data) || data[i] != '.' {
		if overflow {
			if neg {
				f = -f
			}
			return Value{Kind: Real, F: f}, i
		}
		n := int64(whole)
		if neg {
			n = -n
		}
		return Value{Kind: Int, N: n, F: float64(n)}, i
	}
	i++ // the point
	scale := 1.0
	for i < len(data) && data[i] >= '0' && data[i] <= '9' {
		if scale < 1e17 {
			f = f*10 + float64(data[i]-'0')
			scale *= 10
		}
		i++
	}
	f /= scale
	if neg {
		f = -f
	}
	return Value{Kind: Real, F: f}, i
}

// StreamData reports whether the keyword stream follows at pos (after
// white space), and if so the offset of the first byte of stream data.
// The keyword is followed by a line feed, or a carriage return and a line
// feed; a lone carriage return is tolerated.
func StreamData(data []byte, pos int) (offset int, ok bool) {
	pos = SkipSpace(data, pos)
	if !HasPrefixAt(data, pos, "stream") {
		return 0, false
	}
	pos += len("stream")
	if pos < len(data) && data[pos] == '\r' {
		pos++
	}
	if pos < len(data) && data[pos] == '\n' {
		pos++
	}
	return pos, true
}
