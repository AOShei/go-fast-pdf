# GoFast PDF 🛸

[![Go Version](https://img.shields.io/badge/Go-1.25-00ADD8?logo=go)](https://golang.org/)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

A Go library that extracts the text and structure of PDF files, fast, for retrieval, knowledge graphs
and language models. It is written from scratch on the standard library: its one dependency is
[go-fast-core](https://github.com/AOShei/go-fast-core), the document model that the go-fast libraries
share.

- It returns **paragraphs in reading order**, each with its place on the page, as JSON, Markdown or text.
- It **flags text that a reader of the PDF cannot see** and says why, and leaves nothing out.
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

# With the hidden-text checks that follow the shapes of each page,
# and the hidden text left out of what is written
gofastpdf --deep-hidden --exclude-hidden --format text document.pdf

# Only text hidden for the reasons named is left out
gofastpdf --exclude-hidden=invisible,low_contrast --format text document.pdf
```

JSON always has everything, hidden text included and flagged. Warnings of a file that
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
`Parts` (a range of pages), `Limits`, `DeepHidden` and `Child` (an extractor for attached files).
Settings that only PDF has are fields of `gofastpdf.Extractor`. The
[package documentation](https://pkg.go.dev/github.com/AOShei/go-fast-pdf) says what each does for PDF.

## What it reads

- **Text** - fonts with and without ToUnicode maps, composite fonts, the standard encodings, the Adobe
  Glyph List; ligatures are written as their letters
- **Paragraphs and reading order** - lines are joined into paragraphs; columns, tables drawn by rows, and
  pages drawn out of order are put in reading order
- **Running headers and footers** - titles, dates and page numbers that the pages repeat in their margins
  are tagged, and the renderers leave them out; a paragraph split by a page break is joined again
- **Headings** - from the outline where it names them, otherwise by size and weight, with levels
- **List items** - bulleted and numbered, with their markers and how deep they are nested
- **Bold, italic and links** - with `Options.Spans`: spans by the fonts of the words, and links to web
  addresses or to pages of the document (`#page=N`)
- **Hidden text** - flagged with its reason, never dropped: see below
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
- **Outline, page labels, document information, attached files** - attached files are handed to a child
  extractor that you supply
- **What a file stores and shows nowhere** - scripts, the metadata packet, entries of the file's own making
  in the document information, form fields on no page, the filled-in data of an XFA form: read as text, never run, flagged `invisible`, with
  `Attrs["stored"]` saying what each is. A page's scripts are on the page; the rest is in a last part of
  kind `stored`
- **Lines and words** - `Page.Lines` gives every line with its font and size and the box of each word

## Output

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

## Hidden text

A PDF can hold text that nobody who opens it sees: white on white, behind a picture, outside the page,
in a rendering mode that paints nothing. A person does not read it and a model that is given the
extracted text does. This library extracts such text like any other and flags it, with the reason, in
`Block.Hidden`; the page gets the warning `text.hidden`. Nothing is left out. You decide what to do with
it, for instance with `RenderOptions.ExcludeHidden`. Hidden text is laid out apart from the rest, so the
visible paragraphs are what they would be without it.

| Reason | What it means | Checked by default | With `DeepHidden` |
|---|---|---|---|
| `invisible` | Rendering mode that paints nothing; content of a layer that is switched off; an annotation or a form field that its flags hide; what the file stores and shows nowhere (scripts, its metadata packet, entries of its own) | in full | in full |
| `text_layer` | The text of a scan: not painted, or covered, within an image over half the page | in full | in full |
| `tiny` | Displayed smaller than 2 points | in full | in full |
| `off_page` | Wholly outside the visible page | in full | in full |
| `transparent` | Every painted part has opacity 0 | in full | in full |
| `invisible_chars` | Characters without width or display, found by go-fast-core in any text | in full | in full |
| `low_contrast` | Cannot be told from what is behind it | against a white page, where nothing was painted before the text | against the rectangle it stands on |
| `occluded` | Something opaque is painted over it | under an image | under an image or a rectangle |
| `clipped` | Outside what a clipping path leaves | outside the box of the form it is drawn in | outside the clipping paths in force |

**How to read the result.** `Document.HiddenChecked` lists the reasons that were checked in full. For a
reason on the list, a block without the flag was checked and found in view, as far as the check goes.
For a reason not on the list (the last three, without `DeepHidden`) a flag stands where the check found
one, and no flag says nothing. With `DeepHidden` all nine are listed, unless a page paints more shapes
than are followed (65,536, `Extractor.MaxShapes`): that page gets `pdf.deep_hidden_incomplete` and
`low_contrast` and `occluded` are left off the list.

**What it costs.** Measured in instructions executed, on the benchmark documents and over the 3,150
files of the corpus and the soak set that can be read:

| | Benchmark documents | All 3,150 files |
|---|---|---|
| The default checks, against none | 0.1 to 0.8 percent | 1.1 percent |
| `DeepHidden`, against the default | 3 to 9 percent | 20 percent |

Without `DeepHidden` no shape is recorded and no path is followed further than its first operands.

**What is not detected.** A flag is evidence that text is hidden. No flag is not evidence that a
document is safe. Not detected at all: a font whose glyphs draw nothing, or something other than what
its mapping says; text inside images; text hidden by a soft mask, a blend mode or a pattern; text that
is partly visible; text covered by other text or by a shape that is not an upright rectangle. Text on a
photograph, a shading or a shape with round corners is not judged
for contrast. The document information, the outline and the names of attached files are never shown on
a page and are extracted by design: treat them as you treat any text from the file.

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
  and a memory ceiling, with the default options and with `DeepHidden`: no crash, no file over its
  limits, every result within the contract of the shared model.
- **Fuzzing** of 14 targets, 30 seconds each on every change and an hour each every night. The
  decompressor is compared with `compress/zlib` on every input.
- **Budgets**: the allocations per page of the benchmark documents, and the instructions a benchmark
  executes against the last release.
- **Every change** runs on linux/amd64, linux/arm64, darwin/arm64 and windows/amd64 under the race
  detector, and as a 32-bit build. The tests run over 90 percent of the statements.

## Limitations

- **Structure** - tables are not recognised yet; their text is in paragraphs. Headings are found by the
  outline and by type; a heading set like the text around it is not found. List items are found by their
  markers; a list set without markers is paragraphs
- **Scripts written from right to left** - the text is given in the order the page draws it, with a warning
- **Fonts without any mapping** - glyphs of embedded CFF and TrueType programs that the PDF does not
  describe are read by their codes or reported as unmapped; the legacy CJK encodings are not built in
- **Images** - CMYK, indexed, 1-bit, CCITT and JBIG2 images are listed without their data. An image block
  has no text: the alternative text that a tagged PDF may give a figure (`/Alt`) is not read yet
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
* [x] Hidden-text detection, and the deeper checks that follow drawn shapes
* [x] AES-256 encryption
* [x] Image data (JPEG, JPEG 2000, 8-bit gray and RGB)
* [x] Page iterator, lines and words, the command-line tool
* [ ] Headings, lists, tables, running headers and footers
* [ ] Tagged PDF: structure, alternative text
* [ ] Text from right to left in reading order

## License

MIT License - see [LICENSE](LICENSE). Notices for the tables generated from published data are in [NOTICE](NOTICE).
Changes are listed in [CHANGELOG.md](CHANGELOG.md).

Copyright (c) 2025 Andrew O'Shei
