package pdf

import (
	"bytes"
	"compress/zlib"
	"errors"
	"fmt"
	"io"
	"sync"
)

// Reader is the high-level entry point for reading a PDF.
type Reader struct {
	rs             io.ReadSeeker
	lexer          *Lexer
	xref           *XRefTable
	encryptHandler *EncryptionHandler

	// Object cache for performance
	mu          sync.RWMutex
	objectCache map[int]Object
}

func NewReader(rs io.ReadSeeker) (*Reader, error) {
	// 1. Parse XRef
	xref, err := ParseXRef(rs)
	if err != nil {
		return nil, err
	}

	reader := &Reader{
		rs:          rs,
		xref:        xref,
		lexer:       NewLexer(rs),
		objectCache: make(map[int]Object),
	}

	// 2. Check for encryption and initialize handler
	if encRef, exists := xref.Trailer["/Encrypt"]; exists {
		encObj := reader.Resolve(encRef)

		// Get file ID from trailer
		var fileID []byte
		if idArray, ok := xref.Trailer["/ID"].(ArrayObject); ok && len(idArray) > 0 {
			if idStr, ok := idArray[0].(StringObject); ok {
				fileID = []byte(idStr)
			} else if idHex, ok := idArray[0].(HexStringObject); ok {
				fileID = []byte(idHex)
			}
		}

		encDict, err := ParseEncryptDict(encObj, reader)
		if err != nil {
			return nil, fmt.Errorf("failed to parse encryption: %w", err)
		}

		handler, err := NewEncryptionHandler(encDict, fileID)
		if err != nil {
			return nil, fmt.Errorf("failed to initialize encryption: %w", err)
		}

		reader.encryptHandler = handler
	}

	return reader, nil
}

// GetObject resolves an indirect reference to the actual object.
func (r *Reader) GetObject(ref IndirectObject) (Object, error) {
	// Check cache first
	r.mu.RLock()
	if cached, ok := r.objectCache[ref.ObjectNumber]; ok {
		r.mu.RUnlock()
		return cached, nil
	}
	r.mu.RUnlock()

	entry, ok := r.xref.Entries[ref.ObjectNumber]
	if !ok {
		return nil, fmt.Errorf("object %d not found in xref", ref.ObjectNumber)
	}

	if entry.Free {
		return NullObject{}, nil
	}

	// Check if object is in a compressed stream
	if entry.Compressed {
		obj, err := r.getCompressedObject(entry.StreamObj, entry.StreamIdx)
		if err == nil {
			// Cache the object
			r.mu.Lock()
			r.objectCache[ref.ObjectNumber] = obj
			r.mu.Unlock()
		}
		return obj, err
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
			return r.readStream(dict, lexer, ref.ObjectNumber, ref.Generation)
		}
	}

	// Decrypt non-stream objects
	if r.encryptHandler != nil {
		obj = r.decryptObject(obj, ref.ObjectNumber, ref.Generation)
	}

	// Cache the object
	r.mu.Lock()
	r.objectCache[ref.ObjectNumber] = obj
	r.mu.Unlock()

	return obj, nil
}

// readStream handles reading and DECOMPRESSING the stream data
func (r *Reader) readStream(dict DictionaryObject, lexer *Lexer, objNum, genNum int) (StreamObject, error) {
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
	switch b {
	case '\r':
		next, _ := lexer.reader.Peek(1)
		if len(next) > 0 && next[0] == '\n' {
			lexer.reader.ReadByte() // Consume \n
		}
	case '\n':
		// OK - standard LF
	default:
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

	// 4.5. Decrypt data BEFORE decompression (if encrypted)
	if r.encryptHandler != nil {
		decrypted, err := r.encryptHandler.Decrypt(data, objNum, genNum)
		if err == nil {
			data = decrypted
		}
		// If decryption fails, continue with original data
		// Decompression will likely fail, but we'll handle that gracefully
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

	nObj, ok := stm.Dictionary["/N"].(NumberObject)
	if !ok {
		return nil, errors.New("object stream missing or invalid /N parameter")
	}
	n := int(nObj)

	firstObj, ok := stm.Dictionary["/First"].(NumberObject)
	if !ok {
		return nil, errors.New("object stream missing or invalid /First parameter")
	}
	first := int(firstObj)

	// Create a lexer for the UNCOMPRESSED content
	stmReader := bytes.NewReader(stm.Data)
	stmLexer := NewLexer(stmReader)

	offsets := make([]int, n)
	for i := 0; i < n; i++ {
		// Read object number
		objNum, err := stmLexer.ReadObject()
		if err != nil {
			return nil, fmt.Errorf("failed to read object number at index %d: %w", i, err)
		}
		if objNum == nil {
			return nil, fmt.Errorf("unexpected nil object number at index %d", i)
		}

		// Read offset
		offsetObj, err := stmLexer.ReadObject()
		if err != nil {
			return nil, fmt.Errorf("failed to read offset at index %d: %w", i, err)
		}
		if offsetObj == nil {
			return nil, fmt.Errorf("unexpected nil offset at index %d", i)
		}

		// Type assert to NumberObject
		offset, ok := offsetObj.(NumberObject)
		if !ok {
			return nil, fmt.Errorf("expected NumberObject for offset at index %d, got %T", i, offsetObj)
		}
		offsets[i] = int(offset)
	}

	if index < 0 || index >= n {
		return nil, fmt.Errorf("object index %d out of bounds [0, %d)", index, n)
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

// IsEncrypted checks if the PDF has an encryption dictionary in its trailer
func (r *Reader) IsEncrypted() bool {
	_, exists := r.xref.Trailer["/Encrypt"]
	return exists
}

// metadataKeys are dictionary keys that should not be encrypted per PDF spec
var metadataKeys = map[string]bool{
	"/Type":             true,
	"/Subtype":          true,
	"/Length":           true,
	"/Filter":           true,
	"/DecodeParms":      true,
	"/Width":            true,
	"/Height":           true,
	"/BitsPerComponent": true,
	"/ColorSpace":       true,
	"/Encrypt":          true,
	"/ID":               true,
	"/Size":             true,
	"/Root":             true,
	"/Info":             true,
	"/Prev":             true,
	"/Index":            true,
	"/W":                true,
	"/First":            true,
	"/N":                true,
}

// isMetadataKey checks if a dictionary key should not be encrypted
func isMetadataKey(key string) bool {
	return metadataKeys[key]
}

// decryptObject recursively decrypts an object
func (r *Reader) decryptObject(obj Object, objNum, genNum int) Object {
	if r.encryptHandler == nil {
		return obj // Not encrypted
	}

	switch v := obj.(type) {
	case StringObject:
		decrypted, err := r.encryptHandler.Decrypt([]byte(v), objNum, genNum)
		if err != nil {
			return v // Return original on error
		}
		return StringObject(decrypted)

	case HexStringObject:
		decrypted, err := r.encryptHandler.Decrypt([]byte(v), objNum, genNum)
		if err != nil {
			return v
		}
		return HexStringObject(decrypted)

	case ArrayObject:
		// Recursively decrypt array elements
		for i, elem := range v {
			v[i] = r.decryptObject(elem, objNum, genNum)
		}
		return v

	case DictionaryObject:
		// Recursively decrypt dictionary values (except metadata keys)
		for key, val := range v {
			if !isMetadataKey(key) {
				v[key] = r.decryptObject(val, objNum, genNum)
			}
		}
		return v

	default:
		// Numbers, names, booleans, null, etc. are not encrypted
		return obj
	}
}
