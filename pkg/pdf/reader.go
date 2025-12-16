package pdf

import (
	"bytes"
	"compress/zlib"
	"errors"
	"fmt"
	"io"
)

// Reader is the high-level entry point for reading a PDF.
type Reader struct {
	rs    io.ReadSeeker
	lexer *Lexer
	xref  *XRefTable
}

func NewReader(rs io.ReadSeeker) (*Reader, error) {
	// 1. Parse XRef
	xref, err := ParseXRef(rs)
	if err != nil {
		return nil, err
	}

	return &Reader{
		rs:    rs,
		xref:  xref,
		lexer: NewLexer(rs),
	}, nil
}

// GetObject resolves an indirect reference to the actual object.
func (r *Reader) GetObject(ref IndirectObject) (Object, error) {
	entry, ok := r.xref.Entries[ref.ObjectNumber]
	if !ok {
		return nil, fmt.Errorf("object %d not found in xref", ref.ObjectNumber)
	}

	if entry.Free {
		return NullObject{}, nil
	}

	// Check if object is in a compressed stream
	if entry.Compressed {
		return r.getCompressedObject(entry.StreamObj, entry.StreamIdx)
	}

	// Jump to offset
	r.rs.Seek(entry.Offset, io.SeekStart)

	lexer := NewLexer(r.rs)

	// Consume "ObjNum Gen obj" header
	lexer.ReadObject() // ID
	lexer.ReadObject() // Gen
	lexer.ReadObject() // "obj" keyword

	// Read the actual object
	obj, err := lexer.ReadObject()
	if err != nil {
		return nil, err
	}

	// If it's a Dictionary, check if it's followed by a Stream
	if dict, ok := obj.(DictionaryObject); ok {
		lexer.skipWhitespace()
		peek, _ := lexer.reader.Peek(6)
		if string(peek) == "stream" {
			return r.readStream(dict, lexer)
		}
	}

	return obj, nil
}

// readStream handles reading and DECOMPRESSING the stream data
func (r *Reader) readStream(dict DictionaryObject, lexer *Lexer) (StreamObject, error) {
	// 1. Get Length
	lengthObj := r.Resolve(dict["/Length"])
	length := int64(0)
	if n, ok := lengthObj.(NumberObject); ok {
		length = int64(n)
	} else {
		return StreamObject{}, errors.New("stream length missing or invalid")
	}

	// 2. Consume "stream" keyword
	lexer.reader.Discard(6)

	// 3. Consume STRICT EOL (CRLF or LF)
	// PDF binary streams start immediately after the newline.
	// We cannot use skipWhitespace() because it might eat binary data (e.g. 0x0A inside the stream).
	b, err := lexer.reader.ReadByte()
	if err != nil {
		return StreamObject{}, err
	}
	if b == '\r' {
		next, _ := lexer.reader.Peek(1)
		if len(next) > 0 && next[0] == '\n' {
			lexer.reader.ReadByte() // Consume \n
		}
	} else if b == '\n' {
		// OK
	} else {
		// Not a standard newline, back up to be safe
		lexer.reader.UnreadByte()
	}

	// 4. Read Raw Compressed Data
	data := make([]byte, length)

	// FIX: Use lexer.reader, NOT r.rs.
	// r.rs is the underlying file, which might be ahead of the buffer.
	if _, err := io.ReadFull(lexer.reader, data); err != nil {
		return StreamObject{}, err
	}

	// 5. Decompress
	finalData := data
	filterObj := r.Resolve(dict["/Filter"])
	filters := []string{}

	if name, ok := filterObj.(NameObject); ok {
		filters = append(filters, string(name))
	} else if arr, ok := filterObj.(ArrayObject); ok {
		for _, f := range arr {
			if name, ok := f.(NameObject); ok {
				filters = append(filters, string(name))
			}
		}
	}

	for _, f := range filters {
		if f == "/FlateDecode" {
			zr, err := zlib.NewReader(bytes.NewReader(finalData))
			if err != nil {
				// Don't fail completely on zlib error, return raw data so we can debug
				// or maybe it wasn't compressed.
				return StreamObject{Dictionary: dict, Data: finalData}, nil
			}
			decompressed, err := io.ReadAll(zr)
			zr.Close()
			if err == nil {
				finalData = decompressed
			}
		}
	}

	return StreamObject{
		Dictionary: dict,
		Data:       finalData,
	}, nil
}

// NumPages returns the total page count.
func (r *Reader) NumPages() int {
	catalog := r.Resolve(r.xref.Trailer["/Root"])
	if catDict, ok := catalog.(DictionaryObject); ok {
		pages := r.Resolve(catDict["/Pages"])
		if pagesDict, ok := pages.(DictionaryObject); ok {
			if count, ok := pagesDict["/Count"].(NumberObject); ok {
				return int(count)
			}
		}
	}
	return 0
}

// GetPage returns the dictionary for the Nth page (0-indexed).
func (r *Reader) GetPage(pageIndex int) (DictionaryObject, error) {
	catalog := r.Resolve(r.xref.Trailer["/Root"])
	catDict, ok := catalog.(DictionaryObject)
	if !ok {
		return nil, fmt.Errorf("catalog is not a dictionary")
	}

	rootPages := r.Resolve(catDict["/Pages"])
	rootPagesDict, ok := rootPages.(DictionaryObject)
	if !ok {
		return nil, fmt.Errorf("root pages is not a dictionary")
	}

	return r.findPage(rootPagesDict, &pageIndex)
}

func (r *Reader) findPage(node DictionaryObject, targetIndex *int) (DictionaryObject, error) {
	nodeType := node["/Type"].String()

	if nodeType == "/Page" {
		if *targetIndex == 0 {
			return node, nil
		}
		*targetIndex--
		return nil, nil
	}

	kids := r.Resolve(node["/Kids"]).(ArrayObject)
	for _, kidRef := range kids {
		kid := r.Resolve(kidRef).(DictionaryObject)

		if countObj, ok := kid["/Count"].(NumberObject); ok {
			count := int(countObj)
			if *targetIndex < count {
				found, err := r.findPage(kid, targetIndex)
				if err != nil {
					return nil, err
				}
				if found != nil {
					return found, nil
				}
			} else {
				*targetIndex -= count
			}
		} else {
			found, err := r.findPage(kid, targetIndex)
			if err != nil {
				return nil, err
			}
			if found != nil {
				return found, nil
			}
		}
	}

	return nil, nil
}

func (r *Reader) getCompressedObject(streamObjNum int, index int) (Object, error) {
	// Get the object stream itself
	// This calls GetObject -> readStream, so fixing readStream fixes this too.
	objStream, err := r.GetObject(IndirectObject{ObjectNumber: streamObjNum, Generation: 0})
	if err != nil {
		return nil, err
	}

	stm, ok := objStream.(StreamObject)
	if !ok {
		return nil, errors.New("referenced object stream is not a stream")
	}

	n := int(stm.Dictionary["/N"].(NumberObject))
	first := int(stm.Dictionary["/First"].(NumberObject))

	// Create a lexer for the UNCOMPRESSED content
	stmReader := bytes.NewReader(stm.Data)
	stmLexer := NewLexer(stmReader)

	offsets := make([]int, n)
	for i := 0; i < n; i++ {
		stmLexer.ReadObject() // ObjNum
		off, _ := stmLexer.ReadObject()
		offsets[i] = int(off.(NumberObject))
	}

	if index >= n {
		return nil, errors.New("object index out of bounds")
	}

	startOffset := int64(first + offsets[index])
	stmReader.Seek(startOffset, io.SeekStart)

	objLexer := NewLexer(stmReader)
	return objLexer.ReadObject()
}

func (r *Reader) Resolve(obj Object) Object {
	if ref, ok := obj.(IndirectObject); ok {
		res, err := r.GetObject(ref)
		if err != nil {
			fmt.Printf("Warning: failed to resolve object %v: %v\n", ref, err)
			return NullObject{}
		}
		return res
	}
	return obj
}

func (r *Reader) GetInfo() (DictionaryObject, error) {
	if infoRef, ok := r.xref.Trailer["/Info"]; ok {
		resolved := r.Resolve(infoRef)
		if dict, ok := resolved.(DictionaryObject); ok {
			return dict, nil
		}
	}
	return nil, nil
}
