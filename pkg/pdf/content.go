package pdf

import (
	"bytes"
	"fmt"
	"io"
)

// Operation represents a single PDF command (Operator + Arguments).
type Operation struct {
	Operator string
	Operands []Object
}

// ContentStreamParser parses the stream of instructions for a page.
type ContentStreamParser struct {
	lexer *Lexer
}

func NewContentStreamParser(data []byte) *ContentStreamParser {
	return &ContentStreamParser{
		lexer: NewLexer(bytes.NewReader(data)),
	}
}

// Parse extracts all operations from the stream.
func (p *ContentStreamParser) Parse() ([]Operation, error) {
	var operations []Operation
	var operands []Object

	for {
		// Peek EOF
		p.lexer.skipWhitespace()
		_, err := p.lexer.reader.Peek(1)
		if err == io.EOF {
			break
		}

		// Read next object (Keywords are now objects too)
		obj, err := p.lexer.ReadObject()
		if err != nil {
			return nil, fmt.Errorf("failed to read object: %v", err)
		}

		// Check if it is an Operator (Keyword)
		if kw, ok := obj.(KeywordObject); ok {
			op := Operation{
				Operator: string(kw),
				Operands: append([]Object(nil), operands...), // Copy operands
			}
			operations = append(operations, op)
			operands = nil // Reset operands for next operator
			continue
		}

		// If it's data, append to operands buffer
		operands = append(operands, obj)
	}

	return operations, nil
}
