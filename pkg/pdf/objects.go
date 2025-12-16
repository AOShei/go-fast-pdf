package pdf

import (
	"fmt"
	"strings"
)

// Object is the generic interface for all PDF objects.
type Object interface {
	String() string
}

// NullObject represents the PDF 'null' value.
type NullObject struct{}

func (n NullObject) String() string { return "null" }

// BooleanObject represents PDF 'true' or 'false'.
type BooleanObject bool

func (b BooleanObject) String() string {
	if b {
		return "true"
	}
	return "false"
}

// NumberObject represents integer or float values.
type NumberObject float64

func (n NumberObject) String() string {
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%f", n), "0"), ".")
}

// NameObject represents PDF names (e.g., /Type).
type NameObject string

func (n NameObject) String() string { return string(n) }

// StringObject represents literal strings (e.g., (Hello World)).
type StringObject string

func (s StringObject) String() string { return fmt.Sprintf("(%s)", string(s)) }

// HexStringObject represents hex strings (e.g., <AABB>).
type HexStringObject []byte

func (h HexStringObject) String() string { return fmt.Sprintf("<%X>", []byte(h)) }

// ArrayObject represents PDF arrays (e.g., [1 2 R]).
type ArrayObject []Object

func (a ArrayObject) String() string {
	var sb strings.Builder
	sb.WriteString("[")
	for i, obj := range a {
		if i > 0 {
			sb.WriteString(" ")
		}
		sb.WriteString(obj.String())
	}
	sb.WriteString("]")
	return sb.String()
}

// DictionaryObject represents PDF dictionaries (e.g., << /Type /Page >>).
type DictionaryObject map[string]Object

func (d DictionaryObject) String() string {
	var sb strings.Builder
	sb.WriteString("<<")
	for k, v := range d {
		sb.WriteString(fmt.Sprintf(" %s %s", k, v.String()))
	}
	sb.WriteString(" >>")
	return sb.String()
}

// IndirectObject represents a reference (e.g., 12 0 R).
type IndirectObject struct {
	ObjectNumber int
	Generation   int
}

func (i IndirectObject) String() string {
	return fmt.Sprintf("%d %d R", i.ObjectNumber, i.Generation)
}

// StreamObject represents a dictionary followed by binary stream data.
type StreamObject struct {
	Dictionary DictionaryObject
	Data       []byte
}

func (s StreamObject) String() string {
	return fmt.Sprintf("Stream(len=%d)", len(s.Data))
}

// KeywordObject represents raw keywords (e.g., obj, stream, Tj).
type KeywordObject string

func (k KeywordObject) String() string { return string(k) }
