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

> **Status.** v0.4.0 (2026-11-25), on go-fast-core v0.1.9. The API is not that of v0.1:
> [MIGRATION.md](MIGRATION.md) says what became what; [CHANGELOG.md](CHANGELOG.md) says what changed.

## Speed

<!-- comparison:begin (written by go run ./bench/compare -readme README.md) -->
| | Pages per second | Time against go-fast-pdf | Most memory | Words as pdftotext's (F1) |
|---|---:|---:|---:|---:|
| go-fast-pdf | 10,184 | 1 | 32 MB | 0.984 |
| go-fast-pdf, all processors | 19,200 | 0.53 times | 69 MB | 0.984 |
| gofastpdf (the command) | 8,236 | 1.24 times | 24 MB | 0.984 |
| pdftotext (poppler) | 2,009 | 5.07 times | 20 MB | the reference |
| PyMuPDF | 2,238 | 4.55 times | 62 MB | 0.986 |
| pypdfium2 (PDFium) | 2,230 | 4.57 times | 30 MB | 0.974 |
| pypdf | 350 | 29.1 times | 72 MB | 0.970 |
| pdfplumber | 57 | 179 times | 53 MB | 0.964 |
| pdf_oxide (Rust) | 309 | 33.0 times | 30 MB | 0.974 |
| pdf-extract (Rust) | 2,053 | 4.96 times | 24 MB | 0.970 |
| lopdf (Rust) | 6,637 | 1.53 times | 25 MB | 0.900 |

8 documents, 530 pages, 9.7 MB: the long documents of the public corpus's tuning set and one small one.
Each program reads each document 5 times and its best time counts; the table is the sum over the documents, as a batch of them would take, and on one document the order can differ. Measured on AMD Ryzen 9 9900X 12-Core Processor (24 threads, linux/amd64) with go1.27.3-X:nodwarf5, 2026-11-22.
The last column is how far each program's words agree with pdftotext's, not how correct they are: 1 is the same words.
The method, the versions, the time for each document, and how far each pair of programs agree, each as the reference in turn, are in [bench/comparison.md](bench/comparison.md).
<!-- comparison:end -->

Reading the 1,650 files of the public test corpus, damaged ones included, takes 1.2 seconds on one
thread. `go run ./bench/compare` makes the table again on your machine.

A good part of the time is Go's garbage collector. The library leaves its settings alone, since they
are the whole program's; a program with memory to spare can set GOGC above 100 (the environment
variable, or `debug.SetGCPercent`) and spend less time collecting, with `GOMEMLIMIT` as a ceiling.
What each setting costs and gives, on the documents of the table and on a batch of the corpus:

<!-- gc:begin (written by go run ./bench/compare -gc -readme README.md) -->
| Setting | One worker: time | memory | 24 workers: time | memory | 24 documents at once: time | memory |
|---|---:|---:|---:|---:|---:|---:|
| `GOGC=100` (default) | 51.5 ms | 29 MB | 16.9 ms | 92 MB | 145 ms | 597 MB |
| `GOGC=200` | 48.6 ms (-6%) | 43 MB (+47%) | 13.1 ms (-22%) | 140 MB (+53%) | 121 ms (-17%) | 1,217 MB (+104%) |
| `GOGC=400` | 45.4 ms (-12%) | 55 MB (+89%) | 12.8 ms (-24%) | 234 MB (+155%) | 115 ms (-21%) | 2,109 MB (+253%) |

Time is the best run; memory is the most the process had resident, the median of 5 processes. Measured on AMD Ryzen 9 9900X 12-Core Processor (24 threads, linux/amd64) with go1.27.3-X:nodwarf5, 2026-11-23;
what each workload reads, the processor time and the number of collections are in [bench/gc.md](bench/gc.md).
<!-- gc:end -->

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

# Text that is wholly outside the visible box of its page left out, of JSON too
gofastpdf --exclude-off-page document.pdf

# Text with the running headers and footers, the scripts and the stored part,
# and a line before each page saying which it is
gofastpdf --furniture --stored --part-markers --format text document.pdf

# Text with the scripts only, each on its page, and nothing else that is stored
gofastpdf --stored=macro --format text document.pdf
```

The flags for Markdown and text are those of every go-fast command: `--furniture`, `--stored[=kinds]`,
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
`Keys` (private keys, for a file encrypted for a certificate), `Parts` (a range of pages), `Limits`, `Spans` and `Child` (an
extractor for attached files).
Settings that only PDF has are fields of `gofastpdf.Extractor`. The
[package documentation](https://pkg.go.dev/github.com/AOShei/go-fast-pdf) says what each does for PDF.

## What it reads

- **Text** - fonts with and without ToUnicode maps, composite fonts, the standard encodings, the Adobe
  Glyph List; ligatures and Arabic presentation forms are written as their letters
- **Paragraphs and reading order** - lines are joined into paragraphs, and an entry of a table of contents
  that has a leader is one of its own; a word broken at the end of a line is whole again where the document
  has it whole elsewhere, and keeps its hyphen where nothing says; columns, tables drawn by rows, and pages drawn out of order are put
  in reading order; in a tagged PDF the order of the structure tree decides where the white space of the
  page leaves it open; text written from right to left (Hebrew, Arabic) reads in the order it is written,
  with numbers and Latin words in it from left to right, and its columns from right to left
- **Running headers and footers** - titles, dates and page numbers that the pages repeat in their margins
  are tagged, and the renderers leave them out; a paragraph split by a page break is joined again
- **Headings** - from the structure tree of a tagged PDF or the outline where they name them, otherwise by
  size and weight, with levels
- **Tables of tagged PDFs** - table blocks with the grid the structure tree gives: header rows, merged
  cells, and tables over several pages continued
- **Tables in files without tags** - found by the rules they are drawn with, a grid or only rules
  across the table, or by how their text lines up in columns: cells of several lines, cells over several columns and header rows set in bold; columns
  of prose, boxes around paragraphs, lists, tables of contents and code are not taken for tables.
  Looking for ruled tables costs about 5 percent; `Extractor.NoRuledTables` turns it off
- **Descriptions of figures** - an image of a tagged PDF has the alternative text its figure gives
- **List items** - bulleted and numbered, with their markers and how deep they are nested; in a tagged
  PDF, the items of the lists of its structure tree, with their labels as markers, also where the bullet
  is drawn and not written
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
  request JPEG and JPEG 2000 data as it is in the file, and as PNG: samples of any depth in gray, RGB,
  CMYK, Lab or indexed colours, CCITT fax images and stencil masks, with soft masks and masks as alpha
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
  (`RenderOptions.Stored`, or `StoredKinds` for some kinds only; the command's `-stored[=kinds]`)
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

Text outside the page is in the document like any other, with the box that says where it is: the notes of
a printer beside the trim marks, text moved off a page and not deleted. It has no reason, since no switch
of the file says it is not shown. `Extractor.NoOffPageText` (`--exclude-off-page`) leaves out the text
that is wholly outside the visible box of its page; a run of text that is partly on the page stays whole.
That is a rule of position, which you choose, and not a judgement of this library: it is off by default.

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
- **A soak run** over 3,186 files and 50,995 pages, each file in a process of its own under a time limit
  and a memory ceiling, with the default options and with `Spans`: no crash, no file over its
  limits, every result within the contract of the shared model.
- **Fuzzing** of every target listed in `.github/fuzz-targets.txt`, 30 seconds each on every change and an hour each every night. The
  decompressor is go-fast-core's, fuzzed there against `compress/zlib` on every input; here it is
  compared with `compress/zlib` on the 11,394 streams of the tuning set.
- **Budgets**: the allocations per page of the benchmark documents, and the instructions a benchmark
  executes against the last release.
- **Every change** runs on linux/amd64, linux/arm64, darwin/arm64 and windows/amd64 under the race
  detector, and as a 32-bit build. The tests run over 90 percent of the statements.

## Limitations

- **Structure** - a table without tags is found only where its columns stand apart with white space or
  rules; one whose columns are set close together with neither is paragraphs, a row a line with its
  cells apart by tabs. Headings are found by the outline and by type; a heading set like the text around
  it is not found. In a file without tags, list items are found by their markers; a list set without
  markers is paragraphs
- **Scripts written from right to left** - a line's direction is that of the letters at its ends or of
  most of its words, and in a paragraph that of most of the paragraph's text, so a paragraph of Hebrew or
  Arabic that is mostly Latin words can be read from left to right. Columns are read from right to left
  where most of their text reads so. Glyphs that a font leaves without a character are read by their
  outlines only for the fonts built in (Noto Sans, Naskh and Kufi Arabic, Regular and Bold, of one
  version each, which Firefox embeds), and the page has the warning `pdf.glyph_text_by_outline`; in
  other fonts they are not read
- **Fonts without any mapping** - the encodings and glyph names of embedded Type 1, CFF and TrueType
  programs are read, and the legacy Chinese, Japanese and Korean encodings are built in. What no table
  says cannot be read: Type 3 fonts, TrueType subsets without cmap and post tables, CID-keyed CFF fonts
  of no collection. Their glyphs are read by their codes where those are ASCII, or reported as unmapped
- **Images** - JBIG2 images are listed without their data, as are images that cannot be made PNG:
  samples of other than 1, 2, 4, 8 or 16 bits, a Pattern colour space, inks whose function cannot be
  read or would take too long, damaged data; the warning `image.unsupported_encoding` says why. CMYK is made RGB without a colour profile,
  and CalRGB is read as RGB, so their colours are near but not as a profile would make them. JPEG and JPEG 2000 are handed on without their masks
- **Tagged PDF** - of the structure tree, headings, tables, lists and the descriptions of figures are
  read, and its order where the white space of a page leaves the order open; where white space separates
  blocks it decides, so a text box that a word processor puts in the tree where it is anchored is still
  read where it stands. The tree's nesting of lists is not used: their levels are found by indent
- **No OCR** - a scan without a text layer gives its images and the warning `part.image_only`

## How it works

```
PDF file → objects and cross-references → content interpreter → layout → document
           (internal/file, syntax)         (internal/extract)   (internal/layout)
```

1. **Nothing is decoded that is not needed:** image and font streams are not touched during text extraction.
2. **Streams are decompressed by a decoder made for a slice in memory** (`go-fast-core/inflate`, written here and moved to go-fast-core for every library), about twice as fast as the standard library's, which has to serve a reader.
3. **Fonts are parsed once** per document and shared by all pages.
4. **Pages are independent:** each worker fills one part, and the parts are put together in page order.
5. **Drawing operators are skipped without converting their operands**, which keeps pages full of vector graphics cheap.
6. **The layout keeps the order in which a page draws its text wherever the geometry does not speak against it**, and uses it to choose between readings the geometry leaves open (a table drawn by rows against two columns).

## Roadmap

* [x] The shared document model (`go-fast-core`)
* [x] Paragraphs and reading order
* [x] What the file declares about how text is shown (`hidden`, `no_ink`)
* [x] AES-256 encryption
* [x] Image data (JPEG, JPEG 2000, samples of any depth and colour space, CCITT fax, masks)
* [x] Page iterator, lines and words, the command-line tool
* [x] Headings, lists, running headers and footers; bold, italic and links
* [x] Tagged PDF: headings, tables, lists, the descriptions of figures and the order of the tree
* [x] Tables in files without tags, by how their text lines up
* [x] Tables in files without tags, by their drawn lines
* [x] Text from right to left in reading order

## License

MIT License - see [LICENSE](LICENSE). Notices for the tables generated from published data are in [NOTICE](NOTICE).
Changes are listed in [CHANGELOG.md](CHANGELOG.md).

Copyright (c) 2025 Andrew O'Shei
