# GoFast PDF 🛸

[![Go Version](https://img.shields.io/badge/Go-1.25-00ADD8?logo=go)](https://golang.org/)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

A Go library that extracts the text and structure of PDF files, fast, for retrieval, knowledge graphs
and language models. It is written from scratch on the standard library: its one dependency is
[go-fast-core](https://github.com/AOShei/go-fast-core), the document model that the go-fast libraries
share.

- It returns **paragraphs in reading order**, each with its place on the page, as JSON, Markdown or text.
- It extracts **everything** a page holds, and says where the file declares text **not shown** or drawn
  with **no ink**; it leaves nothing out and judges nothing.
- It reads files that are **encrypted or damaged**, and is bounded in time and memory on files made to
  exhaust them.
- It starts no program, fetches nothing, runs nothing that a file contains, and does no OCR.

> **Status.** This is the tree of v0.2.0, which is not tagged yet: the go-fast libraries are released
> together. Until then `go get github.com/AOShei/go-fast-pdf@main`. The API is not that of v0.1:
> [MIGRATION.md](MIGRATION.md) says what became what.

## Speed

<!-- comparison:begin (written by go run ./bench/compare -readme README.md) -->
| | Pages per second | Time against go-fast-pdf | Most memory |
|---|---:|---:|---:|
| go-fast-pdf | 12,571 | 1 | 27 MB |
| go-fast-pdf, all processors | 19,627 | 0.64 times | 47 MB |
| gofastpdf (the command) | 9,588 | 1.31 times | 17 MB |
| pdftotext (poppler) | 2,046 | 6.14 times | 20 MB |
| PyMuPDF | 2,237 | 5.62 times | 61 MB |
| pypdfium2 (PDFium) | 2,226 | 5.65 times | 30 MB |
| pypdf | 348 | 36.1 times | 71 MB |
| pdfplumber | 57 | 222 times | 52 MB |

8 documents, 530 pages, 9.7 MB: the long documents of the public corpus's tuning set and one small one.
Each program reads each document 5 times and its best time counts. Measured on AMD Ryzen 9 9900X 12-Core Processor (24 threads, linux/amd64) with go1.27.3-X:nodwarf5, 2026-11-19.
The method, the versions and the time for each document are in [bench/comparison.md](bench/comparison.md).
<!-- comparison:end -->

Reading the 1,650 files of the public test corpus, damaged ones included, takes 1.2 seconds on one
thread. `go run ./bench/compare` makes the table again on your machine.

## Installation

```bash
go get github.com/AOShei/go-fast-pdf
```

Go 1.25 or later.

## Usage

### Command line

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

# Text the file declares not shown, or drawn with no ink, left out of what is written
gofastpdf --exclude-display --format text document.pdf

# Only text with the reasons named is left out
gofastpdf --exclude-display=hidden --format text document.pdf

# Text with the running headers and footers, the scripts and the stored part,
# and a line before each page saying which it is
gofastpdf --furniture --stored --part-markers --format text document.pdf
```

The flags for Markdown and text are those of every go-fast command: `--furniture`, `--stored`,
`--part-markers`, `--exclude-display[=reasons]`, and `--children` (on by default: a line naming each
attached file; `--children=false` leaves them out). JSON always has everything, with what the file
declares about each block. Warnings of a file that
was read are named on standard error (`--quiet` leaves that out); a file that cannot be
read is reported there and makes the exit status 1.

### Library

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
    // nil options: text and structure of every page, one worker per processor.
    doc, err := gofastpdf.ExtractFile(context.Background(), "document.pdf", nil)
    if err != nil {
        log.Fatal(err)
    }

    fmt.Println(doc.Markdown()) // or doc.Text(), or encoding/json

    // Blocks in reading order, with where they are on their page.
    for part, block := range doc.AllBlocks() {
        fmt.Printf("page %d %v: %.40q\n", part.Number, block.Loc.BBox, block.Text)
    }

    // Text that the file declares not shown, or drawn with no ink, is
    // extracted with the reason.
    for part, block := range doc.AllBlocks() {
        if block.Display != 0 {
            fmt.Printf("page %d, %s: %q\n", part.Number, block.Display, block.Text)
        }
    }
    // Leave out what the file switches off, and keep the text layer of
    // scans, which is drawn with no ink:
    doc.WriteText(os.Stdout, &gofastcore.RenderOptions{ExcludeDisplay: gofastcore.DisplayHidden})

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

`gofastpdf.Extract(ctx, readerAt, size, opts)` does the same for anything that can be read at an offset.

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

Options are `gofastcore.Options`: `Workers`, `Images` (a block for each image, which is the default; its data too; or none), `Password`,
`Parts` (a range of pages), `Limits`, `Spans` and `Child` (an extractor for attached files).
Settings that only PDF has are fields of `gofastpdf.Extractor`. The
[package documentation](https://pkg.go.dev/github.com/AOShei/go-fast-pdf) says what each does for PDF.

## What it reads

- **Text** - fonts with and without ToUnicode maps, composite fonts, the standard encodings, the Adobe
  Glyph List; ligatures are written as their letters
- **Paragraphs and reading order** - lines are joined into paragraphs; columns, tables drawn by rows, and
  pages drawn out of order are put in reading order
- **Running headers and footers** - titles, dates and page numbers that the pages repeat in their margins
  are tagged, and the renderers leave them out; a paragraph split by a page break is joined again
- **Headings** - from the structure tree of a tagged PDF or the outline where they name them, otherwise by
  size and weight, with levels
- **Tables of tagged PDFs** - table blocks with the grid the structure tree gives: header rows, merged
  cells, and tables over several pages continued
- **Tables in files without tags** - found by the rules they are drawn with, or by how their text lines
  up in columns: cells of several lines, cells over several columns and header rows set in bold; columns
  of prose, boxes around paragraphs, lists, tables of contents and code are not taken for tables
- **Descriptions of figures** - an image of a tagged PDF has the alternative text its figure gives
- **List items** - bulleted and numbered, with their markers and how deep they are nested
- **Bold, italic and links** - with `Options.Spans`: spans by the fonts of the words, and links to web
  addresses or to pages of the document (`#page=N`)
- **Anchors** - the identifiers of the elements of a tagged PDF, and with `Options.Spans` the names of
  its destinations, on the blocks they name (`Loc.Anchor`)
- **What the file declares** - text that the file switches off or draws with no ink keeps its reason
  (`hidden`, `no_ink`) and is never dropped: see below
- **Encryption** - RC4, AES-128 and AES-256, with user and owner passwords, and files encrypted for a
  certificate, with the private key (`Options.Keys`)
- **Damaged files** - broken cross-reference data is rebuilt; a page that cannot be read is reported in
  its place and the rest is returned
- **Images** - a block for every image, where it stands among the text, with its position and size; on
  request JPEG and JPEG 2000 data as it is in the file, and 8-bit gray and RGB samples as PNG
- **Forms and annotations** - what a filled form field, a stamp or a note on the page displays is read as
  text of the page; what is written in a comment is a `note` block with its author and date; the value
  of a field that displays nothing is a block with the field's name; the options of a list or combo box
  are a `choices` note
- **Outline, page labels, document information, attached files** - the metadata packet (XMP) gives the
  lists of authors and keywords as the file declares them, and what the document information leaves out;
  attached files are handed to a child
  extractor that you supply
- **What a file stores and shows nowhere** - scripts, the metadata packet, entries of the file's own making
  in the document information, form fields on no page, the filled-in data of an XFA form, text after the
  last `%%EOF`: read as text, never run, with `Attrs["stored"]` saying what each is. A page's scripts are
  on the page; the rest is in a last part of kind `stored`. The renderers leave both out unless asked
  (`RenderOptions.Stored`, the command's `-stored`)
- **Lines and words** - `Page.Lines` gives every line with its font and size and the box of each word

## Output

```json
{
  "schema_version": "0.2",
  "source": {"format": "pdf", "name": "manual.pdf", "mime": "application/pdf", "size": 48213},
  "metadata": {"title": "Technical Manual", "authors": ["A. Writer"], "extra": {"producer": "..."}},
  "outline": [{"title": "Introduction", "level": 1, "part": 1, "y": 72}],
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

## What the file declares

This library extracts everything a file contains and reports what the file itself declares about it. It
does not judge whether a reader can see text, nor whether text is harmful; the absence of a reason says
nothing about either. Judging that is outside this library.

A block carries a reason in `Block.Display` only where the file states, by one switch or one value, that
its text is not shown or is drawn with no ink. Following the file's own inheritance (forms, transparency
groups, membership dictionaries) is reading the declaration.

| Reason | What PDF declares |
|---|---|
| `hidden` | Optional content (a layer) that is off in the default configuration: `/OFF`, `/BaseState /OFF`, or a membership dictionary whose policy its layers make off |
| `hidden` | An annotation with the Hidden or NoView flag: its text, and the value of such a form field |
| `no_ink` | Text in render mode 3 (neither filled nor stroked), or 7 (clip only) when nothing is painted through the clip |
| `no_ink` | Text whose opacity is 0 for what its mode paints: `/ca 0` for the fill, `/CA 0` for the stroke, by its graphics state or its transparency group |

The searchable text of a scanned page is text in render mode 3 over the picture of the page, so it is
`no_ink`: leaving out `no_ink` leaves out a scan's text.

**Not judged:** colour against what is under the text, its size, where it is on the page or whether it is
on the page at all, what is painted over it, clipping paths, soft masks, blend modes, how much of the page
an image covers. **Characters are kept** as the file gives them: zero-width characters, Unicode tag
characters and bidirectional controls stay in the text.

Nothing is left out: you decide, for instance with `RenderOptions.ExcludeDisplay`. Text with a reason is
laid out apart from the rest, so that the other paragraphs are what they would be without it. The stored
part, the document information, the outline and the names of attached files are not shown on a page and
have no reason: where they are says what they are.

## Untrusted files

The library is made to be given files from anywhere. Every size that a file decides is bounded: the
input (1 GiB by default), what its streams decode to (1 GiB together, 64 MiB each), the text and the
blocks of the document (256 MiB), pages, objects, nesting, the text and the shapes of one page. Decoded bytes and text
are counted for a document and the files attached to it together. A limit that cuts a document is reported as a warning or
an error, never passed over. Reading stops when the context ends, also inside a long page. One opened
file can be read from many goroutines, and the result is the same for any number of workers.
[SECURITY.md](SECURITY.md) has the threat model, every default, and how to report a problem.

## How it is tested

- **A public corpus** of 1,650 PDFs (the test suites of pdf.js and PDFium, and documents of GovDocs1),
  scored against `pdftotext` word by word. A third of it is held out: scored, and never looked at when
  a heuristic is changed. A change may not lower the score of any file.
- **A soak run** over 3,186 files and 50,569 pages, each file in a process of its own under a time limit
  and a memory ceiling, with the default options and with `Spans`: no crash, no file over its
  limits, every result within the contract of the shared model.
- **Fuzzing** of 14 targets, 30 seconds each on every change and an hour each every night. The
  decompressor is compared with `compress/zlib` on every input.
- **Budgets**: the allocations per page of the benchmark documents, and the instructions a benchmark
  executes against the last release.
- **Every change** runs on linux/amd64, linux/arm64, darwin/arm64 and windows/amd64 under the race
  detector, and as a 32-bit build. The tests run over 90 percent of the statements.

## Limitations

- **Structure** - a table without tags is found only where its columns stand apart with white space or
  rules; one whose columns are set close together with neither is paragraphs, a row a line with its
  cells apart by tabs. Headings are found by the
  outline and by type; a heading set like the text around it is not found. List items are found by their
  markers; a list set without markers is paragraphs
- **Scripts written from right to left** - the text is given in the order the page draws it, with a warning
- **Fonts without any mapping** - glyphs of embedded CFF and TrueType programs that the PDF does not
  describe are read by their codes or reported as unmapped; the legacy CJK encodings are not built in
- **Images** - CMYK, indexed, 1-bit, CCITT and JBIG2 images are listed without their data
- **Tagged PDF** - of the structure tree, headings, tables and the descriptions of figures are read;
  lists and the reading order it gives are not yet
- **No OCR** - a scan without a text layer gives its images and the warning `part.image_only`

## How it works

```
PDF file → objects and cross-references → content interpreter → layout → document
           (internal/file, syntax)         (internal/extract)   (internal/layout)
```

1. **Nothing is decoded that is not needed:** image and font streams are not touched during text extraction.
2. **Streams are decompressed by a decoder made for a slice in memory** (`internal/inflate`), about twice as fast as the standard library's, which has to serve a reader.
3. **Fonts are parsed once** per document and shared by all pages.
4. **Pages are independent:** each worker fills one part, and the parts are put together in page order.
5. **Drawing operators are skipped without converting their operands**, which keeps pages full of vector graphics cheap.
6. **The layout keeps the order in which a page draws its text wherever the geometry does not speak against it**, and uses it to choose between readings the geometry leaves open (a table drawn by rows against two columns).

## Roadmap

* [x] The shared document model (`go-fast-core`)
* [x] Paragraphs and reading order
* [x] What the file declares about how text is shown (`hidden`, `no_ink`)
* [x] AES-256 encryption
* [x] Image data (JPEG, JPEG 2000, 8-bit gray and RGB)
* [x] Page iterator, lines and words, the command-line tool
* [x] Headings, lists, running headers and footers; bold, italic and links
* [x] Tagged PDF: headings, tables and the descriptions of figures
* [x] Tables in files without tags, by how their text lines up
* [x] Tables in files without tags, by their drawn lines
* [ ] Text from right to left in reading order

## License

MIT License - see [LICENSE](LICENSE). Notices for the tables generated from published data are in [NOTICE](NOTICE).
Changes are listed in [CHANGELOG.md](CHANGELOG.md).

Copyright (c) 2025 Andrew O'Shei
