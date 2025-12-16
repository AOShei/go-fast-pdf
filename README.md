# PDF Loader

[![Go Version](https://img.shields.io/badge/Go-1.25-00ADD8?logo=go)](https://golang.org/)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

A lightweight, dependency-free Go library for extracting text content from PDF files. Built from scratch without external PDF libraries, implementing core PDF specification parsing for text extraction.

Originally developed for RAG (Retrieval-Augmented Generation) and LLM document processing pipelines where simple, reliable text extraction is needed without the complexity of full-featured PDF libraries.

## Features

### ✅ Implemented

- **Pure Go implementation** - Zero external dependencies, uses only Go standard library
- **Cross-reference table parsing** - Supports both standard xref tables and compressed xref streams
- **Object stream decompression** - Handles modern PDFs with compressed object storage
- **Text extraction** - Full text state machine with proper font metrics and spacing
- **Character mapping** - ToUnicode CMap parsing for accurate character decoding
- **Page tree traversal** - Recursive navigation of nested page structures
- **Metadata extraction** - Extracts title, author, creator, producer from document info
- **JSON output** - Structured output with page numbers, dimensions, and character counts
- **FlateDecode support** - Automatic decompression of zlib-compressed streams

### ⚠️ Limitations

- **CID fonts** - Limited support for complex Type0 fonts (common in Asian language PDFs)
- **Encryption** - No support for password-protected or encrypted PDFs
- **Images** - Text-only extraction, images are ignored
- **Advanced filters** - Only FlateDecode implemented (no LZW, JPEG, ASCII85, etc.)
- **Complex layouts** - May struggle with multi-column text, tables, or right-to-left text
- **Custom encodings** - Font encoding fallbacks may produce incorrect characters for some PDFs

## Installation

```bash
go get github.com/AOShei/pdf-loader
```

## Usage

### Command Line

```bash
# Run directly
go run cmd/main.go path/to/document.pdf

# Or build and run
go build -o pdf-loader cmd/main.go
./pdf-loader path/to/document.pdf
```

Output is JSON to stdout:

```json
{
  "metadata": {
    "title": "Lorem Ipsum Document",
    "author": "John Doe",
    "creator": "LaTeX with hyperref",
    "producer": "pdfTeX-1.40.21",
    "encrypted": false
  },
  "pages": [
    {
      "page_number": 1,
      "content": "Lorem ipsum dolor sit amet, consectetur adipiscing elit...",
      "char_count": 1847,
      "width": 612.0,
      "height": 792.0
    },
    {
      "page_number": 2,
      "content": "Sed do eiusmod tempor incididunt ut labore et dolore magna aliqua...",
      "char_count": 2156,
      "width": 612.0,
      "height": 792.0
    }
  ]
}
```

### Library API

```go
package main

import (
    "encoding/json"
    "fmt"
    "log"
    
    "github.com/AOShei/pdf-loader/pkg/loader"
)

func main() {
    // Load PDF and extract text
    doc, err := loader.LoadPDF("document.pdf")
    if err != nil {
        log.Fatal(err)
    }
    
    // Access metadata
    fmt.Printf("Title: %s\n", doc.Metadata.Title)
    fmt.Printf("Author: %s\n", doc.Metadata.Author)
    
    // Iterate through pages
    for _, page := range doc.Pages {
        fmt.Printf("\n--- Page %d (%d characters) ---\n", 
            page.PageNumber, page.CharCount)
        fmt.Println(page.Content)
    }
    
    // Convert to JSON
    jsonData, _ := json.MarshalIndent(doc, "", "  ")
    fmt.Println(string(jsonData))
}
```

## Architecture

The library implements a multi-stage parsing pipeline:

```
PDF File → Reader (xref resolution) → Page Dictionary → Extractor (state machines) → Text → Document (JSON)
```

**Key Components:**

- **`pkg/pdf/lexer.go`** - Tokenizes PDF objects (dictionaries, arrays, streams, etc.)
- **`pkg/pdf/reader.go`** - Coordinates parsing, resolves indirect object references
- **`pkg/pdf/xref.go`** - Handles cross-reference tables and object lookup
- **`pkg/pdf/extractor.go`** - Text extraction with graphics/text state machines
- **`pkg/pdf/cmap.go`** - Character mapping for font encoding
- **`pkg/pdf/content.go`** - Content stream operator parsing
- **`pkg/loader/loader.go`** - High-level API orchestrating the pipeline
- **`pkg/model/types.go`** - Output data structures (`Document`, `Page`, `Metadata`)

The extractor maintains two state machines:
1. **Graphics State** - Current Transformation Matrix (CTM), saved/restored with `q`/`Q` operators
2. **Text State** - Font metrics, Text Matrix (TM), positioning, spacing, and scale

## Dependencies

**Zero external dependencies** - built entirely with Go standard library:

- `bufio` - Buffered I/O for efficient parsing
- `compress/zlib` - FlateDecode stream decompression
- `encoding/json` - JSON output formatting
- `unicode/utf16` - UTF-16BE decoding for CMaps

## Known Issues

1. **Type assertion fragility** - Some type conversions lack error checks and may panic on malformed PDFs
2. **Spacing heuristics** - Text spacing detection uses heuristics that may fail on complex layouts
3. **No caching** - Objects may be re-parsed multiple times (performance trade-off for simplicity)
4. **Partial spec compliance** - Implements subset of PDF 1.7 specification sufficient for text extraction

## Roadmap

Future development focuses on expanding extraction capabilities for document processing pipelines:

- Additional stream filter support (LZW, ASCII85)
- Improved CID font handling for better Unicode coverage
- Table detection and structured content extraction
- Enhanced spacing/layout preservation for multi-column documents
- Performance optimizations (object caching, parallel page processing)

## Contributing

This is a learning project and contributions are welcome! Areas particularly suited for contribution:

- Test coverage (currently no automated tests)
- Additional PDF operator support
- Font encoding improvements
- Documentation and examples

## License

MIT License - see [LICENSE](LICENSE) file for details.

Copyright (c) 2025 Andrew O'Shei
