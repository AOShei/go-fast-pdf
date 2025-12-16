package pdf

import (
	"bytes"
	"io"
	"unicode/utf16"
)

// CMap represents the mapping from Character Codes (CIDs) to Unicode strings.
type CMap struct {
	SpaceWidth float64 // Fallback width
	Map        map[string]string
}

func NewCMap() *CMap {
	return &CMap{
		Map: make(map[string]string),
	}
}

// ParseCMap parses a ToUnicode stream.
func ParseCMap(data []byte) (*CMap, error) {
	cmap := NewCMap()
	lexer := NewLexer(bytes.NewReader(data))

	// Iterate tokens to find beginbfchar / beginbfrange
	for {
		token, err := readRawToken(lexer)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}

		switch token {
		case "beginbfchar":
			if err := parseBFChar(lexer, cmap); err != nil {
				return nil, err
			}
		case "beginbfrange":
			if err := parseBFRange(lexer, cmap); err != nil {
				return nil, err
			}
		}
	}
	return cmap, nil
}

// parseBFChar handles: <srcCode> <dstString>
func parseBFChar(l *Lexer, cmap *CMap) error {
	// Loop until endbfchar
	for {
		srcObj, err := l.ReadObject()
		if err != nil {
			// Check if it is "endbfchar" (ReadObject fails on keywords usually)
			// But since <HEX> is standard object, we only fail on the keyword "endbfchar"
			// We might need to peek or handle error.
			// Simplified: assume we check raw token if ReadObject fails
			token, _ := readRawToken(l)
			if token == "endbfchar" {
				return nil
			}
			return err
		}

		// Standard BFChar is: <AAAA> <BBBB>
		dstObj, err := l.ReadObject()
		if err != nil {
			return err
		}

		srcHex, ok1 := srcObj.(HexStringObject)
		dstHex, ok2 := dstObj.(HexStringObject)

		if ok1 && ok2 {
			cmap.Map[string(srcHex)] = decodeUTF16BE(dstHex)
		}
	}
}

// parseBFRange handles two formats:
// 1. <start> <end> <dstStart>  (Sequential range)
// 2. <start> <end> [<dst1> <dst2> ...] (Array mapping)
func parseBFRange(l *Lexer, cmap *CMap) error {
	for {
		startObj, err := l.ReadObject()
		if err != nil {
			token, _ := readRawToken(l)
			if token == "endbfrange" {
				return nil
			}
			return err
		}

		endObj, err := l.ReadObject()
		if err != nil {
			return err
		}

		nextObj, err := l.ReadObject()
		if err != nil {
			return err
		}

		startCode := hexToInt(startObj.(HexStringObject))
		endCode := hexToInt(endObj.(HexStringObject))

		// Case 2: Array [<dst1> <dst2> ...]
		if arr, ok := nextObj.(ArrayObject); ok {
			for i, elem := range arr {
				if hexStr, ok := elem.(HexStringObject); ok {
					currentCode := startCode + i
					if currentCode <= endCode {
						key := intToHex(currentCode, len(startObj.(HexStringObject)))
						cmap.Map[key] = decodeUTF16BE(hexStr)
					}
				}
			}
		} else if dstStartHex, ok := nextObj.(HexStringObject); ok {
			// Case 1: Sequential <dstStart>
			// We map startCode..endCode to dstStart..dstStart+(diff)
			// Logic: The destination code increments too.
			// NOTE: Handle UTF16 incrementing carefully.

			dstCode := hexToInt(dstStartHex)

			for i := 0; i <= (endCode - startCode); i++ {
				srcKey := intToHex(startCode+i, len(startObj.(HexStringObject)))
				// This is a simplification. Real unicode incrementing is complex.
				// However, PDF spec says the last byte increments.
				dstVal := intToHex(dstCode+i, len(dstStartHex))
				cmap.Map[srcKey] = decodeUTF16BE(HexStringObject(dstVal))
			}
		}
	}
}

// Helpers

func readRawToken(l *Lexer) (string, error) {
	l.skipWhitespace()
	var buf bytes.Buffer
	for {
		b, err := l.reader.Peek(1)
		if err != nil {
			if buf.Len() > 0 {
				break
			}
			return "", err
		}
		c := b[0]
		if isDelimiter(c) || isWhitespace(c) {
			if buf.Len() == 0 && isDelimiter(c) {
				// If it's a delimiter, maybe we should skip it or return it?
				// For keywords like "beginbfchar", they are not delimiters.
			}
			break
		}
		l.reader.ReadByte()
		buf.WriteByte(c)
	}
	return buf.String(), nil
}

func hexToInt(h HexStringObject) int {
	// Convert bytes to integer
	// <00 41> -> 65
	bs := []byte(h)
	val := 0
	for _, b := range bs {
		val = (val << 8) | int(b)
	}
	return val
}

func intToHex(val int, byteLen int) string {
	// Reconstruct hex string of specific length
	out := make([]byte, byteLen)
	for i := byteLen - 1; i >= 0; i-- {
		out[i] = byte(val & 0xFF)
		val >>= 8
	}
	return string(out)
}

func decodeUTF16BE(b []byte) string {
	// Assuming b is Big Endian UTF-16
	if len(b)%2 != 0 {
		return string(b) // Fallback
	}
	u16s := make([]uint16, len(b)/2)
	for i := 0; i < len(b); i += 2 {
		u16s[i/2] = (uint16(b[i]) << 8) | uint16(b[i+1])
	}
	return string(utf16.Decode(u16s))
}
