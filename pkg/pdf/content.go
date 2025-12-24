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
	lexer    *Lexer
	operands []Object
	eof      bool
}

func NewContentStreamParser(data []byte) *ContentStreamParser {
	return &ContentStreamParser{
		lexer:    NewLexer(bytes.NewReader(data)),
		operands: []Object{},
	}
}

// Next returns the next operation, or (nil, io.EOF) when done
func (p *ContentStreamParser) Next() (*Operation, error) {
	if p.eof {
		return nil, io.EOF
	}

	for {
		p.lexer.skipWhitespace()
		_, err := p.lexer.reader.Peek(1)
		if err == io.EOF {
			p.eof = true
			return nil, io.EOF
		}
		if err != nil {
			return nil, err
		}

		obj, err := p.lexer.ReadObject()
		if err != nil {
			return nil, fmt.Errorf("failed to read object: %v", err)
		}

		// Check if it's an operator (keyword)
		if kw, ok := obj.(KeywordObject); ok {
			// Special handling for inline images
			if string(kw) == "BI" {
				if err := p.skipInlineImage(); err != nil {
					return nil, err
				}
				// Return synthetic operation for inline image
				op := &Operation{
					Operator: "INLINE_IMAGE",
					Operands: p.operands, // Contains image dict
				}
				p.operands = nil
				return op, nil
			}

			op := &Operation{
				Operator: string(kw),
				Operands: append([]Object(nil), p.operands...),
			}
			p.operands = nil
			return op, nil
		}

		// Accumulate operands
		p.operands = append(p.operands, obj)
	}
}

// skipInlineImage reads the inline image dictionary and skips binary data until EI marker
func (p *ContentStreamParser) skipInlineImage() error {
	// 1. Read image dictionary key-value pairs until ID keyword
	imageDict := make(map[string]Object)

	for {
		p.lexer.skipWhitespace()
		obj, err := p.lexer.ReadObject()
		if err != nil {
			return err
		}

		// Check for ID keyword (marks end of dict, start of data)
		if kw, ok := obj.(KeywordObject); ok && string(kw) == "ID" {
			break
		}

		// Read dictionary key (should be a name)
		key, ok := obj.(NameObject)
		if !ok {
			return fmt.Errorf("expected name in inline image dict, got %T", obj)
		}

		// Read dictionary value
		p.lexer.skipWhitespace()
		val, err := p.lexer.ReadObject()
		if err != nil {
			return err
		}

		imageDict[string(key)] = val
	}

	// Store image dict in operands for the INLINE_IMAGE operation
	p.operands = []Object{DictionaryObject(imageDict)}

	// 2. After ID, consume strict EOL (CR, LF, or CRLF)
	b, err := p.lexer.reader.ReadByte()
	if err != nil {
		return err
	}
	if b == '\r' {
		next, _ := p.lexer.reader.Peek(1)
		if len(next) > 0 && next[0] == '\n' {
			p.lexer.reader.ReadByte()
		}
	} else if b != '\n' {
		// Not a standard newline, back up
		p.lexer.reader.UnreadByte()
	}

	// 3. Scan for EI marker
	// Pattern: (whitespace) + "EI" + (whitespace/delimiter)
	for {
		b, err := p.lexer.reader.ReadByte()
		if err != nil {
			return err
		}

		if b == 'E' {
			next, _ := p.lexer.reader.Peek(1)
			if len(next) > 0 && next[0] == 'I' {
				p.lexer.reader.ReadByte() // Consume 'I'

				// Check what follows EI (must be whitespace or delimiter)
				after, _ := p.lexer.reader.Peek(1)
				if len(after) == 0 || isWhitespace(after[0]) || isDelimiter(after[0]) {
					// Found valid EI marker
					return nil
				}
				// False alarm, 'EI' embedded in data, continue scanning
			}
		}
	}
}
