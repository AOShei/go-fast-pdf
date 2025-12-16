package pdf

import (
	"bytes"
	"compress/zlib"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

type XRefEntry struct {
	Offset     int64
	Generation int
	Free       bool
	Compressed bool
	StreamObj  int
	StreamIdx  int
}

type XRefTable struct {
	Entries map[int]XRefEntry
	Trailer DictionaryObject
}

func NewXRefTable() *XRefTable {
	return &XRefTable{
		Entries: make(map[int]XRefEntry),
		Trailer: make(DictionaryObject),
	}
}

func ParseXRef(rs io.ReadSeeker) (*XRefTable, error) {
	table := NewXRefTable()
	nextOffset, err := findStartXRef(rs)
	if err != nil {
		return nil, fmt.Errorf("findStartXRef failed: %w", err)
	}

	visited := make(map[int64]bool)

	for nextOffset != 0 {
		if visited[nextOffset] {
			break
		}
		visited[nextOffset] = true

		_, err := rs.Seek(nextOffset, io.SeekStart)
		if err != nil {
			return nil, fmt.Errorf("failed to seek to xref at %d: %w", nextOffset, err)
		}

		sig := make([]byte, 5)
		n, err := rs.Read(sig)
		if err != nil && err != io.EOF {
			return nil, fmt.Errorf("failed to read xref signature: %w", err)
		}
		if n < 4 {
			return nil, fmt.Errorf("xref signature too short: got %d bytes", n)
		}

		_, err = rs.Seek(nextOffset, io.SeekStart)
		if err != nil {
			return nil, fmt.Errorf("failed to seek back to xref: %w", err)
		}

		var prevOffset int64
		var tr DictionaryObject

		if string(sig[:4]) == "xref" {
			prevOffset, tr, err = table.readStandardXRef(rs)
			if err != nil {
				return nil, fmt.Errorf("readStandardXRef failed: %w", err)
			}
		} else {
			prevOffset, tr, err = table.readXRefStream(rs)
			if err != nil {
				return nil, fmt.Errorf("readXRefStream failed: %w", err)
			}
		}

		for k, v := range tr {
			if _, exists := table.Trailer[k]; !exists {
				table.Trailer[k] = v
			}
		}
		nextOffset = prevOffset
	}

	if _, ok := table.Trailer["/Root"]; !ok {
		return nil, errors.New("invalid PDF: missing /Root in trailer")
	}

	return table, nil
}

func findStartXRef(rs io.ReadSeeker) (int64, error) {
	size, err := rs.Seek(0, io.SeekEnd)
	if err != nil {
		return 0, err
	}
	readSize := int64(1024)
	if size < readSize {
		readSize = size
	}
	rs.Seek(-readSize, io.SeekEnd)

	buf := make([]byte, readSize)
	io.ReadFull(rs, buf)

	idx := bytes.LastIndex(buf, []byte("startxref"))
	if idx == -1 {
		return 0, errors.New("startxref not found")
	}

	content := string(buf[idx+9:])
	content = strings.TrimSpace(content)
	end := 0
	for end < len(content) && content[end] >= '0' && content[end] <= '9' {
		end++
	}
	return strconv.ParseInt(content[:end], 10, 64)
}

func (t *XRefTable) readStandardXRef(rs io.ReadSeeker) (int64, DictionaryObject, error) {
	var buf [4]byte
	rs.Read(buf[:]) // consumes "xref"

	lexer := NewLexer(rs)

	for {
		lexer.skipWhitespace()
		b, err := lexer.reader.Peek(7)
		if err == nil && len(b) >= 7 && string(b[:7]) == "trailer" {
			lexer.reader.Discard(7)
			break
		}
		// If we can't peek 7 bytes, check if we can at least see "trailer" partially
		if err == io.EOF || len(b) < 7 {
			// Try to read what we can and check if it starts with "trailer"
			if len(b) > 0 && strings.HasPrefix(string(b), "trailer") {
				lexer.reader.Discard(len("trailer"))
				break
			}
		}

		// Read Start Number
		obj, err := lexer.ReadObject()
		if err != nil {
			return 0, nil, fmt.Errorf("failed to read xref start number: %w", err)
		}
		startNum, ok1 := obj.(NumberObject)

		// Read Count Number
		obj2, err := lexer.ReadObject()
		if err != nil {
			return 0, nil, err
		}
		countNum, ok2 := obj2.(NumberObject)

		if !ok1 || !ok2 {
			return 0, nil, errors.New("malformed xref table: expected integers")
		}

		start := int(startNum)
		count := int(countNum)
		lexer.skipWhitespace()

		// Read `count` lines of 20 bytes each
		lineBuf := make([]byte, 20)
		for i := 0; i < count; i++ {
			if _, err := io.ReadFull(rs, lineBuf); err != nil {
				return 0, nil, err
			}
			offsetStr := string(lineBuf[:10])
			offset, _ := strconv.ParseInt(offsetStr, 10, 64)
			genStr := string(lineBuf[11:16])
			gen, _ := strconv.ParseInt(genStr, 10, 64)
			flag := lineBuf[17]

			id := start + i
			if _, exists := t.Entries[id]; !exists {
				t.Entries[id] = XRefEntry{
					Offset:     offset,
					Generation: int(gen),
					Free:       flag == 'f',
				}
			}
		}
	}

	// Read Trailer
	obj, err := lexer.ReadObject()
	if err != nil {
		return 0, nil, err
	}
	tr, ok := obj.(DictionaryObject)
	if !ok {
		return 0, nil, errors.New("expected trailer dictionary")
	}

	var prev int64
	if p, ok := tr["/Prev"]; ok {
		prev = int64(p.(NumberObject))
	}
	return prev, tr, nil
}

func (t *XRefTable) readXRefStream(rs io.ReadSeeker) (int64, DictionaryObject, error) {
	lexer := NewLexer(rs)
	var streamDict DictionaryObject

	// 1. Read the indirect object header: "objNum gen obj"
	// XRef streams are indirect objects, so skip: objNum, gen, "obj"
	_, err := lexer.ReadObject() // Skip object number
	if err != nil {
		return 0, nil, fmt.Errorf("failed reading xref stream obj number: %w", err)
	}
	_, err = lexer.ReadObject() // Skip generation number
	if err != nil {
		return 0, nil, fmt.Errorf("failed reading xref stream gen number: %w", err)
	}
	_, err = lexer.ReadObject() // Skip "obj" keyword
	if err != nil {
		return 0, nil, fmt.Errorf("failed reading xref stream 'obj' keyword: %w", err)
	}

	// 2. Now read the actual stream dictionary
	obj, err := lexer.ReadObject()
	if err != nil {
		return 0, nil, fmt.Errorf("failed reading xref stream dictionary: %w", err)
	}
	var ok bool
	streamDict, ok = obj.(DictionaryObject)
	if !ok {
		return 0, nil, fmt.Errorf("expected dictionary for xref stream, got %T", obj)
	}

	typeObj, hasType := streamDict["/Type"]
	if !hasType {
		return 0, nil, errors.New("XRef stream missing /Type field")
	}
	if typeObj.String() != "/XRef" {
		return 0, nil, fmt.Errorf("object is not an XRef stream, /Type is %v", typeObj.String())
	}

	// 2. Prepare parameters
	lengthObj, ok := streamDict["/Length"].(NumberObject)
	if !ok {
		return 0, nil, errors.New("XRef stream missing /Length")
	}

	// /W [ 1 2 1 ] -> Field widths
	wArr, ok := streamDict["/W"].(ArrayObject)
	if !ok || len(wArr) != 3 {
		return 0, nil, errors.New("invalid /W array")
	}
	w := []int{int(wArr[0].(NumberObject)), int(wArr[1].(NumberObject)), int(wArr[2].(NumberObject))}
	stride := w[0] + w[1] + w[2]

	// /Index [ 0 12 ] -> Start, Count (pairs)
	// Default is [0 Size]
	var index []int
	if idxObj, ok := streamDict["/Index"].(ArrayObject); ok {
		for _, v := range idxObj {
			index = append(index, int(v.(NumberObject)))
		}
	} else {
		if sizeObj, ok := streamDict["/Size"].(NumberObject); ok {
			index = []int{0, int(sizeObj)}
		}
	}

	// 3. Read & Decode Stream Data
	lexer.skipWhitespace()
	peek, _ := lexer.reader.Peek(6)
	if string(peek) == "stream" {
		lexer.reader.Discard(6)
	}
	lexer.skipWhitespace()

	// IMPORTANT: Read from lexer.reader (not rs) to avoid buffering issues
	// The lexer has buffered data, so reading from rs would skip buffered content
	compressedData := make([]byte, int64(lengthObj))
	if _, err := io.ReadFull(lexer.reader, compressedData); err != nil {
		return 0, nil, fmt.Errorf("failed to read compressed stream data: %w", err)
	}

	zr, err := zlib.NewReader(bytes.NewReader(compressedData))
	if err != nil {
		return 0, nil, err
	}
	defer zr.Close()
	decoded, err := io.ReadAll(zr)
	if err != nil {
		return 0, nil, err
	}

	// 4. Apply Predictor (PNG Up) if needed
	// PDF uses Predictor 12 (PNG Up) commonly for XRef streams
	predictor := 1
	columns := 1
	if params, ok := streamDict["/DecodeParms"].(DictionaryObject); ok {
		if p, ok := params["/Predictor"].(NumberObject); ok {
			predictor = int(p)
		}
		if c, ok := params["/Columns"].(NumberObject); ok {
			columns = int(c)
		} else {
			columns = 1 // Default usually 1 for XRef? Actually it's 'stride' conceptually
		}
	}

	// If Predictor >= 10, data is PNG encoded.
	// Rows are (columns + 1) bytes wide (1 byte filter tag)
	if predictor >= 10 {
		// Re-calculate stride if columns wasn't set explicitly to match W sum?
		// Actually for XRef, 'Columns' usually isn't set, the row width is sum(W).
		// The PDF spec says for XRef streams: "The columns parameter... defaults to the sum of items in W"
		if columns == 1 && stride > 1 {
			columns = stride
		}

		var err error
		decoded, err = applyPngPredictor(decoded, columns, predictor)
		if err != nil {
			return 0, nil, err
		}
	}

	// 5. Parse entries
	reader := bytes.NewReader(decoded)
	for i := 0; i < len(index); i += 2 {
		start := index[i]
		count := index[i+1]

		for j := 0; j < count; j++ {
			// Read 3 fields of widths w[0], w[1], w[2]
			f1 := readField(reader, w[0])
			f2 := readField(reader, w[1])
			f3 := readField(reader, w[2])

			id := start + j
			if _, exists := t.Entries[id]; !exists {
				// Type 0: Free (f1=0, f2=nextGen, f3=gen?? spec says f2=objNum of next free)
				// Type 1: InUse (f1=1, f2=offset, f3=gen)
				// Type 2: Compressed (f1=2, f2=streamObjNum, f3=index)

				switch f1 {
				case 1: // In Use
					t.Entries[id] = XRefEntry{Offset: f2, Generation: int(f3), Free: false}
				case 2: // Compressed
					t.Entries[id] = XRefEntry{Compressed: true, StreamObj: int(f2), StreamIdx: int(f3), Free: false}
				case 0: // Free
					t.Entries[id] = XRefEntry{Free: true, Generation: int(f3)}
				}
			}
		}
	}

	var prev int64
	if p, ok := streamDict["/Prev"]; ok {
		prev = int64(p.(NumberObject))
	}
	return prev, streamDict, nil
}

// readField reads `width` bytes as a big-endian integer
func readField(r io.Reader, width int) int64 {
	if width == 0 {
		return 0 // Default value for field is 0 if width is 0
	}
	buf := make([]byte, width)
	r.Read(buf)

	var res int64
	for _, b := range buf {
		res = (res << 8) | int64(b)
	}
	return res
}

// applyPngPredictor decodes PNG predicted data (Predictor >= 10)
// Simplified for PNG Up (12) which is most common in PDFs.
func applyPngPredictor(data []byte, columns int, predictor int) ([]byte, error) {
	// Row size = columns + 1 (filter byte)
	rowSize := columns + 1
	if len(data)%rowSize != 0 {
		// It might be loose, but let's warn/ignore
	}

	rowCount := len(data) / rowSize
	out := make([]byte, rowCount*columns)

	// Previous row buffer (initially zero)
	prevRow := make([]byte, columns)

	for i := 0; i < rowCount; i++ {
		rowStart := i * rowSize
		filter := data[rowStart]
		rowBytes := data[rowStart+1 : rowStart+rowSize]

		// Target slice in output
		outStart := i * columns
		outRow := out[outStart : outStart+columns]

		switch filter {
		case 0: // None
			copy(outRow, rowBytes)
		case 1: // Sub (Left)
			var left byte = 0
			for x := 0; x < columns; x++ {
				val := rowBytes[x] + left
				outRow[x] = val
				left = val
			}
		case 2: // Up
			for x := 0; x < columns; x++ {
				outRow[x] = rowBytes[x] + prevRow[x]
			}
		case 3: // Average
			var left byte = 0
			for x := 0; x < columns; x++ {
				avg := (int(left) + int(prevRow[x])) / 2
				val := byte(int(rowBytes[x]) + avg)
				outRow[x] = val
				left = val
			}
		case 4: // Paeth
			var left byte = 0
			var upperLeft byte = 0
			for x := 0; x < columns; x++ {
				upper := prevRow[x]
				val := byte(int(rowBytes[x]) + paethPredictor(int(left), int(upper), int(upperLeft)))
				outRow[x] = val
				left = val
				upperLeft = upper
			}
		default: // Fallback treat as None
			copy(outRow, rowBytes)
		}

		copy(prevRow, outRow)
	}
	return out, nil
}

func paethPredictor(a, b, c int) int {
	p := a + b - c
	pa := abs(p - a)
	pb := abs(p - b)
	pc := abs(p - c)
	if pa <= pb && pa <= pc {
		return a
	} else if pb <= pc {
		return b
	}
	return c
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
