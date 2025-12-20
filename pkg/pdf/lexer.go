package pdf

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// Lexer handles the low-level parsing of PDF objects.
type Lexer struct {
	reader *bufio.Reader
	seeker io.Seeker
}

func NewLexer(r io.ReadSeeker) *Lexer {
	return &Lexer{
		reader: bufio.NewReader(r),
		seeker: r,
	}
}

// ReadObject parses the next object from the stream.
func (l *Lexer) ReadObject() (Object, error) {
	l.skipWhitespace()

	b, err := l.reader.Peek(1)
	if err != nil {
		return nil, err
	}
	token := b[0]

	switch token {
	case '/':
		return l.readName()
	case '(':
		return l.readString()
	case '<':
		peek, _ := l.reader.Peek(2)
		if len(peek) == 2 && peek[1] == '<' {
			return l.readDictionary()
		}
		return l.readHexString()
	case '[':
		return l.readArray()
	case '%':
		l.reader.ReadByte()
		l.reader.ReadLine()
		return l.ReadObject()
	case '\'', '"':
		l.reader.ReadByte()
		return KeywordObject(token), nil
	default:
		if isDigit(token) || token == '-' || token == '+' || token == '.' {
			return l.readNumberOrReference()
		}
		// Handle Keywords / Booleans / Null
		if isAlpha(token) {
			return l.readKeywordOrBoolean()
		}
		return nil, fmt.Errorf("unexpected token: %c", token)
	}
}

// Helpers

func (l *Lexer) skipWhitespace() {
	for {
		b, err := l.reader.Peek(1)
		if err != nil {
			break
		}
		if isWhitespace(b[0]) {
			l.reader.ReadByte()
		} else {
			break
		}
	}
}

func (l *Lexer) readName() (NameObject, error) {
	l.reader.ReadByte() // consume '/'
	var sb strings.Builder
	sb.WriteString("/")
	for {
		b, err := l.reader.Peek(1)
		if err != nil || isDelimiter(b[0]) || isWhitespace(b[0]) {
			break
		}
		l.reader.ReadByte()
		if b[0] == '#' {
			hex := make([]byte, 2)
			l.reader.Read(hex)
			val, _ := strconv.ParseInt(string(hex), 16, 32)
			sb.WriteByte(byte(val))
		} else {
			sb.WriteByte(b[0])
		}
	}
	return NameObject(sb.String()), nil
}

func (l *Lexer) readString() (StringObject, error) {
	l.reader.ReadByte() // consume '('
	var sb strings.Builder
	parens := 1
	for {
		b, err := l.reader.ReadByte()
		if err != nil {
			return "", err
		}
		if b == '(' {
			parens++
		} else if b == ')' {
			parens--
			if parens == 0 {
				break
			}
		} else if b == '\\' {
			next, _ := l.reader.ReadByte()
			switch next {
			case 'n':
				sb.WriteByte('\n')
			case 'r':
				sb.WriteByte('\r')
			case 't':
				sb.WriteByte('\t')
			case 'b':
				sb.WriteByte('\b')
			case 'f':
				sb.WriteByte('\f')
			case '(':
				sb.WriteByte('(')
			case ')':
				sb.WriteByte(')')
			case '\\':
				sb.WriteByte('\\')
			case '0', '1', '2', '3', '4', '5', '6', '7':
				// Octal escape sequence: \ddd where d is 0-7
				// Can be 1-3 digits
				octalStr := string(next)
				for i := 0; i < 2; i++ { // Read up to 2 more digits
					peek, err := l.reader.Peek(1)
					if err != nil || peek[0] < '0' || peek[0] > '7' {
						break
					}
					digit, _ := l.reader.ReadByte()
					octalStr += string(digit)
				}
				// Convert octal string to byte value
				val, _ := strconv.ParseInt(octalStr, 8, 32)
				sb.WriteByte(byte(val))
			default:
				// For any other escape, just include the character literally
				sb.WriteByte(next)
			}
			continue
		}
		sb.WriteByte(b)
	}
	return StringObject(sb.String()), nil
}

func (l *Lexer) readHexString() (HexStringObject, error) {
	l.reader.ReadByte() // consume '<'
	var data []byte
	for {
		b, err := l.reader.ReadByte()
		if err != nil {
			return nil, err
		}
		if b == '>' {
			break
		}
		if isWhitespace(b) {
			continue
		}
		data = append(data, b)
	}
	return HexStringObject(data), nil
}

func (l *Lexer) readNumberOrReference() (Object, error) {
	// 1. Read the first number (Object Number)
	num1Str, err := l.readTokenString()
	if err != nil {
		return nil, err
	}

	// 2. Check for Reference: "12 0 R"
	// We need to look ahead without consuming if it's not a reference.
	// Since bufio.Peek is limited, and we can't easily unread multiple tokens,
	// we will use a heuristic that respects delimiters.

	l.skipWhitespace()

	// Peek enough bytes to analyze the next two tokens (Generation + 'R')
	// " 65535 R" is roughly max length ~10 bytes. 20 is safe.
	peekBuf, _ := l.reader.Peek(24)

	// Scan the peek buffer for: <Digits> <Whitespace> <R> <Delimiter|Whitespace>
	// We implement a mini-scanner on the peek buffer.

	idx := 0
	// 2a. Parse Generation Number
	genStr := ""
	for idx < len(peekBuf) {
		b := peekBuf[idx]
		if isDigit(b) {
			genStr += string(b)
			idx++
		} else {
			break
		}
	}

	// If we didn't find a generation number, it's just a number.
	if genStr == "" {
		return makeNumber(num1Str), nil
	}

	// 2b. Consume whitespace after Gen
	if idx >= len(peekBuf) || !isWhitespace(peekBuf[idx]) {
		// e.g. "0s" -> not a valid generation number, so original was just a number
		return makeNumber(num1Str), nil
	}
	for idx < len(peekBuf) && isWhitespace(peekBuf[idx]) {
		idx++
	}

	// 2c. Check for 'R'
	if idx < len(peekBuf) && peekBuf[idx] == 'R' {
		// Check what comes AFTER 'R'. Must be delimiter or whitespace.
		// "R" followed by "efer" -> "Refer" is not "R"
		nextIdx := idx + 1
		isValidR := false
		if nextIdx >= len(peekBuf) {
			// buffer ended exactly at R? Unlikely with 24 bytes but possible.
			isValidR = true
		} else {
			nextChar := peekBuf[nextIdx]
			if isWhitespace(nextChar) || isDelimiter(nextChar) {
				isValidR = true
			}
		}

		if isValidR {
			// Found "0 R"! Now we actually consume them from the stream.
			l.readTokenString() // Consume Generation
			l.skipWhitespace()  // Skip whitespace before R
			l.readTokenString() // Consume R

			objNum, _ := strconv.Atoi(num1Str)
			genNum, _ := strconv.Atoi(genStr)
			return IndirectObject{ObjectNumber: objNum, Generation: genNum}, nil
		}
	}

	// Not a reference
	return makeNumber(num1Str), nil
}

func makeNumber(s string) NumberObject {
	if strings.Contains(s, ".") {
		f, _ := strconv.ParseFloat(s, 64)
		return NumberObject(f)
	}
	i, _ := strconv.Atoi(s)
	return NumberObject(float64(i))
}

func (l *Lexer) readKeywordOrBoolean() (Object, error) {
	token, err := l.readTokenString()
	if err != nil {
		return nil, err
	}

	switch token {
	case "true":
		return BooleanObject(true), nil
	case "false":
		return BooleanObject(false), nil
	case "null":
		return NullObject{}, nil
	}

	// Otherwise it's a structural keyword or operator (obj, stream, Tj, etc)
	return KeywordObject(token), nil
}

func (l *Lexer) readTokenString() (string, error) {
	var sb strings.Builder
	for {
		b, err := l.reader.Peek(1)
		if err != nil {
			if sb.Len() > 0 {
				break
			}
			return "", err
		}
		if isDelimiter(b[0]) || isWhitespace(b[0]) {
			break
		}
		l.reader.ReadByte()
		sb.WriteByte(b[0])
	}
	return sb.String(), nil
}

func (l *Lexer) readArray() (ArrayObject, error) {
	l.reader.ReadByte() // consume '['
	var arr ArrayObject
	for {
		l.skipWhitespace()
		b, _ := l.reader.Peek(1)
		if b[0] == ']' {
			l.reader.ReadByte()
			break
		}
		obj, err := l.ReadObject()
		if err != nil {
			return nil, err
		}
		arr = append(arr, obj)
	}
	return arr, nil
}

func (l *Lexer) readDictionary() (DictionaryObject, error) {
	l.reader.ReadByte()
	l.reader.ReadByte() // consume <<
	dict := make(DictionaryObject)
	for {
		l.skipWhitespace()
		b, err := l.reader.Peek(2)
		if err != nil && err != io.EOF {
			return nil, err
		}
		if len(b) >= 2 && string(b[:2]) == ">>" {
			l.reader.Read(make([]byte, 2))
			break
		}

		keyObj, err := l.ReadObject()
		if err != nil {
			return nil, err
		}
		key, ok := keyObj.(NameObject)
		if !ok {
			return nil, fmt.Errorf("dictionary key must be a name, got %T", keyObj)
		}

		valObj, err := l.ReadObject()
		if err != nil {
			return nil, err
		}
		dict[string(key)] = valObj
	}
	return dict, nil
}

func isWhitespace(b byte) bool {
	return b == 0x00 || b == 0x09 || b == 0x0A || b == 0x0C || b == 0x0D || b == 0x20
}

func isDelimiter(b byte) bool {
	return bytes.IndexByte([]byte("()<>[]{}/%"), b) != -1
}

func isDigit(b byte) bool {
	return b >= '0' && b <= '9'
}

func isAlpha(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

func isInteger(s string) bool {
	_, err := strconv.Atoi(s)
	return err == nil
}
