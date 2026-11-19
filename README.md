# GoFast PDF 🛸 Loader

[![Go Version](https://img.shields.io/badge/Go-1.25-00ADD8?logo=go)](https://golang.org/)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

A high-performance, dependency-free Go library for extracting text and image metadata from PDF files. Built from scratch without external PDF libraries, optimized for RAG (Retrieval-Augmented Generation) and LLM document processing pipelines.

**Now featuring concurrent processing and smart caching for 10x-500x performance gains over standard Python libraries.**

> **Status.** The `main` branch is being rebuilt towards v0.2.0 and no longer has the
> API of v0.1.1 (`pkg/loader`). It returns the document model shared by the go-fast
> libraries ([go-fast-core](https://github.com/AOShei/go-fast-core)). The benchmark
> figures further down were measured on v0.1.1 and will be measured again before the
> release.

## Features

### ✅ Implemented

- **Standard library only** - the single dependency is `go-fast-core`, the shared document model
- **One model for every format** - `gofastpdf.Extract` returns a `*gofastcore.Document`: one part per page, blocks in reading order, JSON, Markdown and plain text
- **Paragraphs and reading order** - lines are joined into paragraphs; columns, tables drawn by rows, and pages drawn out of order are put in reading order
- **Hidden text is flagged, never dropped** - invisible, tiny, off-page, transparent, white-on-white and covered text is extracted with the reason it cannot be seen; the text layer of a scan is told apart from the rest
- **Deeper checks on request** - with `DeepHidden`, text is also checked against the shapes a page paints: text in the colour of the box it stands on, text under a box drawn over it, text cut off by a clipping path
- **Text extraction** - fonts with and without ToUnicode maps, composite fonts, the standard encodings, the Adobe Glyph List, ligatures as letters
- **Encryption** - RC4, AES-128 and AES-256, with user and owner passwords
- **Damaged files** - broken cross-reference data is rebuilt; a page that cannot be read is reported and the rest is returned
- **Images** - a block for every image, where it stands among the text, with its position and size; on request JPEG and JPEG 2000 data as it is in the file, and 8-bit gray and RGB samples as PNG. The command lists images unless told `--images none`
- **Outline, page labels, document information, attached files** - attached files are handed to a child extractor that you supply
- **Limits** - input size, decoded size, page count and nesting are bounded; the result is the same for any number of workers
- **Page by page** - `Open` gives a file handle whose parts are read on demand and not kept; reading stops when the context ends, also inside a long page
- **Lines and words** - `Page.Lines` gives every line with its font and size and the box of each word
- **A command** - `gofastpdf` writes JSON, Markdown or text for files and directories
- **No OCR, no network, no code from the file is run**

### ⚠️ Limitations

- **Structure** - every text block is a paragraph. Headings, lists, tables and running headers are not recognised yet
- **Scripts written from right to left** - the text is given in the order the page draws it, with a warning
- **Fonts without any mapping** - glyphs of embedded CFF and TrueType programs that the PDF does not describe are read by their codes or reported as unmapped; the legacy CJK encodings are not built in
- **Images** - CMYK, indexed, 1-bit, CCITT and JBIG2 images are listed without their data. An image block has no text: the alternative text that a tagged PDF may give a figure (`/Alt`) is not read yet
- **Hidden text** - a flag is evidence that text is hidden; no flag is not evidence that a document is safe. Shapes are followed as upright rectangles: text on a photograph, a shading or a shape with round corners is not judged for contrast, and only a rectangle or an image is taken to cover text. The package documentation lists what is not detected. With `DeepHidden`, 65,536 shapes are followed on a page (`Extractor.MaxShapes`); a page that paints more says so, and the number may be raised later

## Installation

```bash
go get github.com/AOShei/go-fast-pdf
```

## Usage

### Command Line

```bash
go install github.com/AOShei/go-fast-pdf/cmd/gofastpdf@latest

# The document as JSON (the shared document model)
gofastpdf document.pdf

# As Markdown or plain text, into a file
gofastpdf --format markdown -o document.md document.pdf

# Several files, or every PDF under a directory: one JSON document per line
gofastpdf reports/ appendix.pdf > all.jsonl

# Some pages, eight at a time, without blocks for the images
gofastpdf --pages 10-20 --workers 8 --images none document.pdf

# With the data of the images
gofastpdf --images data document.pdf

# An encrypted file
gofastpdf --password secret document.pdf

# With the hidden-text checks that follow the shapes of each page,
# and the hidden text left out of what is written
gofastpdf --deep-hidden --exclude-hidden --format text document.pdf

# Only text hidden for the reasons named is left out
gofastpdf --exclude-hidden=invisible,low_contrast --format text document.pdf
```

JSON always has everything, hidden text included and flagged. Warnings of a file that
was read are named on standard error (`--quiet` leaves that out); a file that cannot be
read is reported there and makes the exit status 1.

### Library API

```go
package main

import (
    "context"
    "fmt"
    "log"
    "os"

    gofastcore "github.com/AOShei/go-fast-core"
    gofastpdf "github.com/AOShei/go-fast-pdf"
)

func main() {
    f, err := os.Open("document.pdf")
    if err != nil {
        log.Fatal(err)
    }
    defer f.Close()
    info, err := f.Stat()
    if err != nil {
        log.Fatal(err)
    }

    // nil options: text and structure of every page, one worker per CPU.
    doc, err := gofastpdf.Extract(context.Background(), f, info.Size(), nil)
    if err != nil {
        log.Fatal(err)
    }

    fmt.Println(doc.Markdown()) // or doc.Text(), or encoding/json

    // Blocks in reading order, with where they are on their page.
    for part, block := range doc.AllBlocks() {
        fmt.Printf("page %d %v: %.40q\n", part.Number, block.Loc.BBox, block.Text)
    }

    // Text that a reader of the PDF cannot see is extracted and flagged.
    for part, block := range doc.AllBlocks() {
        if block.Hidden != 0 {
            fmt.Printf("page %d, hidden (%s): %q\n", part.Number, block.Hidden, block.Text)
        }
    }
    // Leave it out of the rendered text, except the text layer of scans:
    doc.WriteText(os.Stdout, &gofastcore.RenderOptions{
        ExcludeHidden: gofastcore.HiddenAny &^ gofastcore.HiddenTextLayer,
    })
    // doc.HiddenChecked says which reasons were checked in full. Extract
    // with &gofastcore.Options{DeepHidden: true} to have low contrast,
    // covering shapes and clipping paths checked in full as well.

    // What went wrong, per page and for the file.
    for part, w := range doc.AllWarnings() {
        if part != nil {
            fmt.Printf("page %d: %s: %s\n", part.Number, w.Code, w.Message)
        } else {
            fmt.Printf("%s: %s\n", w.Code, w.Message)
        }
    }
}
```

`gofastpdf.ExtractFile(ctx, "document.pdf", opts)` does the same for a path.

#### Page by page

A file can also be opened and read a page at a time, without holding the document. A file
of any number of pages is read in bounded memory this way.

```go
f, err := gofastpdf.Open("document.pdf", &gofastcore.Options{Workers: 8})
if err != nil {
    log.Fatal(err)
}
defer f.Close()

fmt.Println(f.NumPages(), "pages;", f.Metadata().Title)

// The parts in page order, several pages read at a time, none kept.
for part, err := range f.Parts(ctx) {
    if err != nil {
        log.Fatal(err) // the context ended, or a limit was reached
    }
    index(part.Number, part.Text())
}

// One page by its number, from any goroutine.
page, _ := f.Page(12)
part, _ := page.Part()

// More than a part has: lines, their font and size, and the box of every word.
lines, _ := page.Lines()
for _, line := range lines {
    fmt.Println(line.Font, line.Size, line.Text)
    for _, word := range line.Words {
        fmt.Println("  ", word.Text, word.BBox)
    }
}
```

Options are `gofastcore.Options`: `Workers`, `Images` (none, description, data), `Password`,
`Parts` (a range of pages), `Limits`, `DeepHidden` and `Child` (an extractor for attached files).
Settings that only PDF has are fields of `gofastpdf.Extractor`. The
package documentation says what each does for PDF.

## Output Format

```json
{
  "schema_version": "0.1",
  "source": {"format": "pdf", "name": "manual.pdf", "mime": "application/pdf", "size": 48213},
  "metadata": {"title": "Technical Manual", "authors": ["A. Writer"], "extra": {"producer": "..."}},
  "outline": [{"title": "Introduction", "level": 1, "part": 1, "y": 72}],
  "hidden_checked": "invisible+text_layer+tiny+off_page+transparent+invisible_chars",
  "parts": [
    {
      "kind": "page",
      "number": 1,
      "width": 612,
      "height": 792,
      "blocks": [
        {
          "kind": "paragraph",
          "text": "Figure 1 shows the component breakdown...",
          "origin": "inferred",
          "loc": {"bbox": {"x0": 72, "y0": 96.4, "x1": 540, "y1": 136.4}}
        },
        {
          "kind": "image",
          "image": {"id": "obj-12", "mime": "image/jpeg", "width": 1024, "height": 768},
          "loc": {"bbox": {"x0": 100.5, "y0": 200, "x1": 400.5, "y1": 350}},
          "attrs": {"color_space": "DeviceRGB"}
        }
      ]
    }
  ]
}
```

Coordinates are in points on the page as it is displayed (crop box, rotation and user
unit applied), with the origin at the top left.

## Architecture & Performance

```
PDF file → objects and cross-references → content interpreter → layout → document
           (internal/file, syntax)         (internal/extract)   (internal/layout)
```

1. **Nothing is decoded that is not needed:** image and font streams are not touched during text extraction.
2. **Fonts are parsed once** per document and shared by all pages.
3. **Pages are independent:** each worker fills one part, and the parts are put together in page order.
4. **Drawing operators are skipped without converting their operands**, which keeps pages full of vector graphics cheap.
5. **The layout keeps the order in which a page draws its text wherever the geometry does not speak against it**, and uses it to choose between readings the geometry leaves open (a table drawn by rows against two columns).

## Benchmarks

Measured on v0.1.1; to be measured again for v0.2.0.

Compared against `pypdf` on an 8-core workstation:

| Document Type | Pages | Content | Go (Seq) | Go (Conc) | Python | Speedup |
| --- | --- | --- | --- | --- | --- | --- |
| Standard Doc | 41 | Mixed | 0.03s | 0.03s | 0.32s | **~10x** |
| Scientific | 17 | Graphs | 0.02s | 0.01s | 12.7s | **~600x** |
| Large Manual | 157 | Images/Enc | 0.40s | 0.27s | 3.22s | **~12x** |

## Roadmap

* [x] The shared document model (`go-fast-core`)
* [x] Paragraphs and reading order
* [x] Hidden-text detection, default checks
* [x] AES-256 encryption
* [x] Image data (JPEG, JPEG 2000, 8-bit gray and RGB)
* [ ] Hidden-text checks that need drawn shapes
* [ ] Page iterator and the full command-line tool
* [ ] Headings, lists, tables, running headers and footers
* [ ] Text from right to left in reading order

## License

MIT License - see [LICENSE](LICENSE). Notices for the tables generated from published data are in [NOTICE](NOTICE).

Copyright (c) 2025 Andrew O'Shei
