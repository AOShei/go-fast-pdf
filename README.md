# PDF Loader

[![Go Version](https://img.shields.io/badge/Go-1.25-00ADD8?logo=go)](https://golang.org/)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

A high-performance, dependency-free Go library for extracting text and image metadata from PDF files. Built from scratch without external PDF libraries, optimized for RAG (Retrieval-Augmented Generation) and LLM document processing pipelines.

**Now featuring concurrent processing and smart caching for 10x-500x performance gains over standard Python libraries.**

## Features

### ✅ Implemented

- **Pure Go implementation** - Zero external dependencies, uses only Go standard library
- **Concurrent Processing** - Multi-threaded page extraction for high-throughput pipelines
- **Smart Caching** - Object and Font caching to minimize I/O and CPU usage on large documents
- **Image Metadata Extraction** - Extracts position, dimensions, and type of images (XObjects and Inline)
- **Vector Graphics Optimization** - Zero-overhead skipping of complex vector drawings (graphs/CAD)
- **Text Extraction** - Full text state machine with proper font metrics and spacing
- **Advanced Character Mapping** - ToUnicode CMap & /Encoding dictionary parsing
- **Ligature & Math Support** - Ligatures (fi, fl) and Greek/Math symbols (α, ∑, ∫, ⊙)
- **Encryption Support** - Automatic decryption of owner-password-only PDFs (RC4 & AES-128)
- **Robust Parsing** - Handles compressed object streams and cross-reference streams
- **JSON Output** - Structured output with page-level metrics

### ⚠️ Limitations

- **Image Content** - Extracts image metadata/locations, but does not yet export raw image bytes
- **AES-256** - AES-256 encryption (PDF 1.7 Extension Level 3) not yet implemented
- **CID Fonts** - Limited support for some complex Asian language fonts (Type0)
- **Layout Analysis** - Does not detect multi-column layouts or tables (returns text in stream order)

## Installation

```bash
go get [github.com/AOShei/pdf-loader](https://github.com/AOShei/pdf-loader)

```

## Usage

### Command Line

The CLI now supports flags for concurrency and image extraction.

```bash
# Basic usage
./pdf-loader document.pdf

# High-performance mode (Concurrent)
./pdf-loader --concurrent --workers 8 document.pdf

# Enable image detection
./pdf-loader --images document.pdf

```

### Library API

```go
package main

import (
    "encoding/json"
    "fmt"
    "log"
    
    "[github.com/AOShei/pdf-loader/pkg/loader](https://github.com/AOShei/pdf-loader/pkg/loader)"
)

func main() {
    // 1. Sequential Load (Simple)
    // Args: path, extractImages (bool)
    doc, err := loader.LoadPDF("document.pdf", false)
    if err != nil {
        log.Fatal(err)
    }
    
    // 2. Concurrent Load (High Performance)
    // Args: path, workers (int, 0=auto), extractImages (bool)
    docFast, err := loader.LoadPDFConcurrent("large_manual.pdf", 0, true)
    if err != nil {
        log.Fatal(err)
    }
    
    // Access Image Metadata
    for _, page := range docFast.Pages {
        if page.Images != nil {
            for _, img := range *page.Images {
                fmt.Printf("Found %s at [%.2f, %.2f]\n", img.Type, img.Rect[0], img.Rect[1])
            }
        }
    }
}

```

## Output Format

```json
{
  "metadata": {
    "title": "Technical Manual",
    "encrypted": false
  },
  "pages": [
    {
      "page_number": 1,
      "content": "Figure 1 shows the component breakdown...",
      "char_count": 120,
      "width": 612.0,
      "height": 792.0,
      "images": [
        {
          "type": "image",
          "id": "Im1",
          "rect": [100.5, 200.0, 300.0, 150.0],
          "width": 1024,
          "height": 768,
          "color_space": "/DeviceRGB"
        }
      ]
    }
  ]
}

```

## Architecture & Performance

The library implements a multi-stage parsing pipeline optimized for speed:

```
PDF File → Reader (xref) → Smart Cache → Extractor (State Machine) → Text/Image Meta

```

**Key Optimizations:**

1. **Lazy Stream Loading:** Large streams (images/videos) are never loaded into RAM unless explicitly requested, preventing memory spikes.
2. **Font Caching:** Font dictionaries and CMaps are parsed once and cached globally, solving the "re-parse" bottleneck on large documents.
3. **Concurrent Workers:** The `LoadPDFConcurrent` function spins up independent workers that process page ranges in parallel, scaling linearly with CPU cores.
4. **Vector Skipping:** The tokenizer aggressively skips vector drawing operators (`l`, `m`, `c`), making the library up to **600x faster** than Python libraries on CAD drawings or scientific papers.

## Benchmarks

Compared against `pypdf` on an 8-core workstation:

| Document Type | Pages | Content | Go (Seq) | Go (Conc) | Python | Speedup |
| --- | --- | --- | --- | --- | --- | --- |
| Standard Doc | 41 | Mixed | 0.03s | 0.03s | 0.32s | **~10x** |
| Scientific | 17 | Graphs | 0.02s | 0.01s | 12.7s | **~600x** |
| Large Manual | 157 | Images/Enc | 0.40s | 0.27s | 3.22s | **~12x** |

## Roadmap

* [x] Concurrent page processing
* [x] Object & Font caching
* [x] Image metadata extraction
* [x] Inline image (`BI`...`EI`) support
* [ ] Raw image byte extraction helper
* [ ] AES-256 encryption (PDF 1.7 Level 3)
* [ ] Layout analysis (table detection)

## License

MIT License - see [LICENSE](https://www.google.com/search?q=LICENSE) file for details.

Copyright (c) 2025 Andrew O'Shei
