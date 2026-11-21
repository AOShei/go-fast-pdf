// Package syntax reads PDF objects from a byte slice.
//
// Nothing here does I/O or follows references: the package turns bytes
// into values, and internal/file decides which bytes. Values point into
// the input wherever they can, so the input must outlive them and must
// not be modified.
package syntax

import "math"

// Kind is the type of a Value.
type Kind uint8

const (
	// Null is the null object, and the zero Value.
	Null Kind = iota
	Bool
	Int
	Real
	// Name holds the name without its leading slash, with #xx decoded.
	Name
	// String holds the decoded bytes of a literal or hexadecimal string.
	String
	// Array holds its elements in List.
	Array
	// Dict holds its entries in List as alternating key and value.
	Dict
	// Ref is an indirect reference: object number in N, generation in Gen.
	Ref
	// Stream is a dictionary followed by stream data. List holds the
	// dictionary entries; N is the offset of the first byte of data in the
	// input; Obj and Gen identify the object, which decryption needs.
	Stream
	// Keyword is any other token of regular characters: obj, endobj,
	// stream, xref, and the operators of a content stream.
	Keyword
)

func (k Kind) String() string {
	names := [...]string{"null", "boolean", "integer", "real", "name", "string", "array", "dictionary", "reference", "stream", "keyword"}
	if int(k) < len(names) {
		return names[k]
	}
	return "invalid"
}

// Value is one PDF object. It is a plain struct, not an interface, so
// that numbers and names need no allocation of their own.
type Value struct {
	Kind Kind
	// Hex reports that a String was written in hexadecimal form.
	Hex bool
	Gen uint16
	// Obj is the object number of a Stream.
	Obj uint32
	// N is the value of an Int or Bool (0 or 1), the bits of the float64
	// of a Real (Float and Number read it), the object number of a Ref,
	// or the data offset of a Stream.
	N int64
	// Data is the bytes of a Name, String or Keyword.
	Data []byte
	// List is the elements of an Array or the entries of a Dict or Stream.
	List []Value
}

// IsNull reports whether v is the null object.
func (v Value) IsNull() bool { return v.Kind == Null }

// IsDict reports whether v has dictionary entries: a Dict or a Stream.
func (v Value) IsDict() bool { return v.Kind == Dict || v.Kind == Stream }

// RealValue returns a Real of value f.
func RealValue(f float64) Value { return Value{Kind: Real, N: int64(math.Float64bits(f))} }

// Float returns the value of a number.
func (v Value) Float() (float64, bool) {
	switch v.Kind {
	case Int:
		return float64(v.N), true
	case Real:
		return math.Float64frombits(uint64(v.N)), true
	}
	return 0, false
}

// Number returns the value of a number, or 0 for anything else.
func (v Value) Number() float64 {
	f, _ := v.Float()
	return f
}

// Integer returns the value of an Int, or of a Real with no fraction that
// fits, which is how some producers write counts and lengths.
func (v Value) Integer() (int64, bool) {
	switch v.Kind {
	case Int:
		return v.N, true
	case Real:
		if f := math.Float64frombits(uint64(v.N)); f == float64(int64(f)) && f > -1e15 && f < 1e15 {
			return int64(f), true
		}
	}
	return 0, false
}

// IsName reports whether v is the name s (given without the slash).
func (v Value) IsName(s string) bool {
	return v.Kind == Name && string(v.Data) == s
}

// NameString returns the name without its slash, or "" if v is not a name.
func (v Value) NameString() string {
	if v.Kind == Name {
		return string(v.Data)
	}
	return ""
}

// Get returns the value of key (given without the slash) in a Dict or
// Stream, or the null Value. When a key occurs twice the first wins.
func (v Value) Get(key string) Value {
	if v.Kind != Dict && v.Kind != Stream {
		return Value{}
	}
	for i := 0; i+1 < len(v.List); i += 2 {
		if k := &v.List[i]; k.Kind == Name && string(k.Data) == key {
			return v.List[i+1]
		}
	}
	return Value{}
}

// Has reports whether a Dict or Stream has the key.
func (v Value) Has(key string) bool {
	if v.Kind != Dict && v.Kind != Stream {
		return false
	}
	for i := 0; i+1 < len(v.List); i += 2 {
		if k := &v.List[i]; k.Kind == Name && string(k.Data) == key {
			return true
		}
	}
	return false
}

// Entries returns the number of entries of a Dict or Stream.
func (v Value) Entries() int {
	if v.Kind != Dict && v.Kind != Stream {
		return 0
	}
	return len(v.List) / 2
}

// Entry returns the i-th key and value of a Dict or Stream.
func (v Value) Entry(i int) (key []byte, val Value) {
	return v.List[2*i].Data, v.List[2*i+1]
}

// Elems returns the elements of an Array, or nil.
func (v Value) Elems() []Value {
	if v.Kind == Array {
		return v.List
	}
	return nil
}

// AsDict returns the dictionary of a Stream as a Dict value, and any
// other value unchanged.
func (v Value) AsDict() Value {
	if v.Kind == Stream {
		return Value{Kind: Dict, List: v.List}
	}
	return v
}

// Equal reports whether two keyword or name values hold the same bytes.
func (v Value) Equal(s string) bool { return string(v.Data) == s }

// Character classes (ISO 32000-1, 7.2.2).
const (
	classRegular = iota
	classSpace
	classDelim
)

var class [256]uint8

func init() {
	for _, c := range []byte{0, '\t', '\n', '\f', '\r', ' '} {
		class[c] = classSpace
	}
	for _, c := range []byte("()<>[]{}/%") {
		class[c] = classDelim
	}
}

// IsSpace reports whether c is a PDF white-space character.
func IsSpace(c byte) bool { return class[c] == classSpace }

// IsDelim reports whether c is a PDF delimiter character.
func IsDelim(c byte) bool { return class[c] == classDelim }

// IsRegular reports whether c is neither white space nor a delimiter.
func IsRegular(c byte) bool { return class[c] == classRegular }

// SkipSpace returns the position of the first byte at or after pos that
// is not white space and not inside a comment.
func SkipSpace(data []byte, pos int) int {
	for pos < len(data) {
		c := data[pos]
		if class[c] == classSpace {
			pos++
			continue
		}
		if c != '%' {
			break
		}
		// A comment runs to the end of the line.
		for pos < len(data) && data[pos] != '\n' && data[pos] != '\r' {
			pos++
		}
	}
	return pos
}

// HasPrefixAt reports whether data has the token s at pos, followed by a
// delimiter, white space or the end of the data.
func HasPrefixAt(data []byte, pos int, s string) bool {
	if pos < 0 || pos+len(s) > len(data) || string(data[pos:pos+len(s)]) != s {
		return false
	}
	end := pos + len(s)
	return end == len(data) || class[data[end]] != classRegular
}
