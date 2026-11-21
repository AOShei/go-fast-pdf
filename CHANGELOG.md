# Changelog

## v0.2.0 (not yet tagged)

The library was rebuilt. It returns the document model shared by the go-fast libraries
([go-fast-core](https://github.com/AOShei/go-fast-core)), reads files that v0.1 could not, is bounded on
files made to exhaust it, and is several times faster. The tag follows that of go-fast-core v0.1.0.

### Breaking

- The API is new and lives in the root of the module: `gofastpdf.Extract`, `ExtractFile`, `Open`. The
  packages `pkg/loader` and `pkg/model` are gone, and so is their JSON. [MIGRATION.md](MIGRATION.md) has
  the table of what became what.
- The output is a `gofastcore.Document`: one part per page, blocks in reading order. A page is no longer
  one string.
- Go 1.25 is the minimum.
- Nothing is printed. v0.1 wrote a line per page to standard error.

### Added

- **Paragraphs and reading order.** Lines are joined into paragraphs. Columns, tables drawn row by row
  and pages drawn out of order are put in the order they are read. In a tagged PDF the order of the
  structure tree takes the place of the order of drawing where the white space of a page leaves the
  order open.
- **Running headers and footers** are tagged by comparing each page with its neighbours, and a paragraph
  split by a page break is marked as continued.
- **Headings**, with levels: named by the structure tree of a tagged PDF or by the outline, or found by
  their size and weight.
- **Descriptions of figures**: an image of a tagged PDF has the alternative text (`/Alt`) of its figure.
- **Tables of tagged PDFs** as table blocks, with header rows, merged cells, and a table over several
  pages continued on each.
- **Tables in files without tags** (`OriginInferred`), found by the rules they are drawn with, and
  otherwise by how their text lines up in columns: cells of several lines, cells over several columns
  (a rule missing inside a grid), header rows in bold, tables set side by side as a table each, a table
  ruled only between its columns read row by row, with a row at each code where entries take several
  lines, and the entries of two cells drawn as one string split at the rule between them. Columns of prose, boxes around paragraphs, the grids of
  charts and forms left mostly empty, lists, tables of contents, code and labels repeated side by side
  are not taken for tables. Finding ruled tables costs about 5 percent of the instructions over a mixed
  set of documents; `Extractor.NoRuledTables` turns it off.
- **List items**, bulleted and numbered, with their markers and nesting. In a tagged PDF the items of
  the lists of the structure tree are list items with `OriginNative`: each its own block, its label as
  its marker, also an item whose bullet is drawn rather than written. `ListNumbering` says whether a
  list is ordered.
- **Bold, italic and links** as spans, with `Options.Spans`.
- **Anchors**: a block that a named destination (with `Options.Spans`) or an element of a tagged PDF
  names has that name in `Loc.Anchor`.
- **What the file declares about how text is shown**, in `Block.Display`: `hidden` for optional content
  that is off and for annotations whose flags say they are not shown, `no_ink` for text drawn in render
  mode 3 or 7 or with an opacity of 0. Nothing is judged: not colour, size, position or what covers the
  text. Characters are kept as the file gives them.
- **Data after the last `%%EOF`** is reported with `input.trailing_data`, and its text, where it is text,
  is in the stored part with `Attrs["stored"]` = `after_end`.
- **Reading a page at a time**: `Open`, `File.Parts`, `File.Page`. A file of any number of pages is read
  in bounded memory.
- **Lines and words**: `Page.Lines`, with the font and size of each line and the box of each word.
- **Encryption**: AES-256 (revisions 5 and 6) beside RC4 and AES-128; owner passwords; Unicode passwords;
  files encrypted for a certificate, opened with a private key (`Options.Keys`).
- **Damaged files**: cross-reference data that is broken or missing is rebuilt by scanning; a page that
  cannot be read is a part with `part.failed` and the rest is returned.
- **Images** as blocks in reading order, with position and size; with `ImagesData`, JPEG and JPEG 2000 as
  they are in the file, and as PNG: samples of 1 to 16 bits in gray, RGB, CMYK, Lab, ICC or indexed
  colours with their Decode arrays, CCITT fax images (Group 3 and 4, decoded by this library), and
  stencil masks; soft masks, mask images and colour-key masks make the alpha. Images inside content
  streams (inline images) are delivered the same way, their abbreviations and named colour spaces read.
  Images in inks (Separation, DeviceN, NChannel) are made colours by the file's function of their
  tints, of any of the four types: sampled, exponential, stitching and PostScript calculator.
- **Forms and annotations.** The appearances of annotations are read as part of the page: the values
  that filled form fields show, stamps, notes written on the page. What is written in a comment is a
  `note` block; the value of a field that shows nothing is a block with the field's name; the options of a list or
  combo box are a `choices` note. Those whose flags say they are not shown have the reason `hidden`.
- **What a file stores and shows nowhere** is extracted: scripts, the metadata packet, entries
  of the file's own making in the document information, form fields on no page, the filled-in data of an
  XFA form (which only Adobe's viewer shows). A page's scripts are on
  the page; the rest is in a last part of kind `stored`. Nothing is run.
- **Outline, page labels, document information, attached files** (through `Options.Child`). The
  metadata packet (XMP) gives the lists of authors and keywords, an entry for each item, and fills in what
  the document information leaves out.
- **Warnings** for what a file has that a consumer may want to know: active content, references to
  other files, repaired input, fonts without a mapping.
- **Chinese, Japanese and Korean text without ToUnicode**: the CMaps the PDF specification predefines for
  the legacy encodings (Shift-JIS, EUC-JP, GBK, GB2312, Big Five, UHC, EUC-KR and the others) are built
  in from Adobe's resources (about 1.2 MB), giving CIDs, their widths, and through each collection's map
  their characters; a CID TrueType font takes its characters from its program's cmap. Public corpus:
  F1 of the CJK files 0.79 to 0.93.
- **Glyph names and characters from embedded font programs**: a simple font without ToUnicode or a
  named encoding takes the encoding of its CFF program (custom charsets and encodings, with
  supplements) or the cmap of its symbolic TrueType program; names in /Differences are looked up in the
  TrueType 'post' table. Ligatures, braces and accented letters that were guessed wrong or unmapped now
  read right on six files of the public corpus.
- **Text written from right to left** (Hebrew, Arabic) in the order it is read, whatever order the
  page draws it in, by the Unicode bidirectional algorithm on lines put together from where their glyphs
  stand; numbers and Latin words within them read from left to right, ligatures keep their letters in
  order, brackets are mirrored where the file gives the glyph drawn. The warning `pdf.rtl_visual_order` is
  gone with the need for it.
- **Arabic as Firefox prints it.** Arabic presentation forms are written as their letters. A mark drawn
  by itself on the glyph before it is read with that glyph. The letters that Firefox's subsets of the
  Noto fonts draw in pieces without characters (a body and its dots) are read by the outlines of their
  glyphs, for Noto Sans, Naskh and Kufi Arabic, Regular and Bold: a page printed by Firefox reads as its
  source, where pdftotext reads few of its letters.
- **Limits** on input size, decoded size, the text of a document, pages, nesting and more; see
  [SECURITY.md](SECURITY.md). Decoded bytes and text are counted for a document and the files attached to
  it together. Cancellation through the context, also inside a long page.
- **The command `gofastpdf`**: JSON, Markdown or text, for files and directories.
- **A decompressor of its own** for zlib streams (`internal/inflate`), about twice as fast as
  `compress/zlib` on the streams of a PDF, and checked against it on every input by a fuzz target.

### Fixed

Every defect found in the review of v0.1.1 is closed, each with a test:

- **Text that was silently wrong or missing.** Characters above 0x7E were dropped, for want of the
  standard encodings. Text drawn through forms (Form XObjects) was not extracted. Spaces and line breaks
  were found from positions and widths in two different units. Composite fonts were read without their
  widths and with guessed code lengths. The glyph list had 250 names. Resources and page boxes inherited
  from the page tree were ignored. One unexpected byte in a content stream discarded its page. A ligature
  was lost or kept depending on the order of the objects in the file. The document information was not
  decoded. The output could hold bytes that are not UTF-8.
- **Crashes and hangs.** Operators without their operands, truncated arrays and wrong types panicked.
  Forms that draw themselves, circular page trees and deeply nested objects overflowed the stack. A
  character map with an unexpected byte looped forever. Lengths and counts in the file were trusted, and
  decompression had no limit.
- **File structure.** No rebuilding of damaged cross-reference data; no hybrid files, no cross-reference
  streams with all their forms, no bytes before the header, no `endstream` when `/Length` is wrong. Only
  Flate: now also LZW, ASCII85, ASCIIHex, RunLength and the predictors. A damaged Flate stream now gives
  what precedes the damage.
- **Encryption.** A file with a user password gave no pages and no error; an AES-256 file crashed. The
  key is now checked, crypt filters are followed, and a password can be given.
- **A page that failed** was named on standard error and left out of the result.

### Speed

Against v0.1.1, on the same machine (AMD Ryzen 9 9900X), one worker, best of five runs. v0.2.0 does
more in that time: it reads the text that v0.1.1 dropped and lays it out.

| | v0.1.1 | v0.2.0 |
|---|---:|---:|
| 1,000 synthetic pages | 61.9 ms | 21.2 ms |
| A 157-page manual with graphics | 29.1 ms | 9.3 ms |
| A 3-page text document | 0.278 ms | 0.121 ms |
| Resolving every object of a file that keeps them in object streams | 411 ms | 1.8 ms |
| Allocations per page of the synthetic document | 975 | 20 |

Against other libraries, see the table in the README, which `go run ./bench/compare` writes.

### Checked before the release

What "ready for production" was taken to mean, and what each point was measured by:

| Property | Measured |
|---|---|
| Never crashes | 3,186 files and 50,569 pages read, each file in a process of its own, with the default options and with a second set of options: no panic, no stack overflow, no hang. Fuzzing: 14 targets for an hour each on machines of four processors, 56 processor hours and 2.3 billion inputs, with nothing found |
| Bounded resources | In that run no file took more than 5 s of the 60 s allowed or more than 711 MiB of the 1 GiB allowed, for a process that reads the file twice and checks the results. The limits and fixed bounds are listed in SECURITY.md; cancellation is tested between pages and inside a long page. The text and the number of blocks of a document, with the files attached to it, are bounded by `MaxTextBytes` |
| Correct text | Word-level F1 against `pdftotext`: at least 0.988 on each of the 27 born-digital Latin files of the fixture corpus (the floor is 0.98), 1.0 on each of the 8 files in other scripts (floor 0.95). Public corpus: mean F1 0.914 and order 0.849 over 1,650 files; 0.983 and 0.907 on the 200 ordinary documents of GovDocs1 |
| Conforms to the shared model | Every document of the runs above passes `Validate` and the contract checks, and is equal after a round trip through JSON |
| Honest failure | 21 files that need a password and 15 that are not readable PDFs return the matching error; 27 pages that cannot be read are parts with `part.failed`. No file gives text that is not valid UTF-8 |
| Reports what the file declares | Every declaration has a fixture that asserts its reason and that the other text is unchanged; every former inferred case (white on white, a 1-point font, text off the page, under an image or a shape, outside a clip) has one that asserts no reason; characters that display as nothing survive |
| Safe for concurrent use | The suite passes under the race detector on linux/amd64, linux/arm64, darwin/arm64 and windows/amd64; one open file is read from many goroutines; the result is the same for 1 and 8 workers on every file of the corpus |
| Fast | Budgets for allocations per page are tests; the nightly job compares every benchmark with the last release on one runner; the comparison with other libraries is one command |
| Stable API | The public surface is a snapshot under test; every exported name is documented; ten examples run as tests |
| Standard library and go-fast modules only | Checked by CI on every change |

Display reasons in the soak run, as blocks (and files with such a block), of 795,587 blocks of text in
3,150 files: `hidden` 89 (10), `no_ink` 34,583 (92).

### Changed before the release: declared, not judged

The tree of v0.2.0 at first judged whether text could be seen, by nine reasons and an option for deeper
checks. That was replaced before the release (go-fast-core schema 0.2): the library reports only what
the file declares, and judges nothing.

- `Block.Hidden`, `Line.Hidden` and the `gofastcore.Hidden` type are `Display`; the JSON key is
  `display`. The reasons are `hidden` and `no_ink`.
- Gone, with no replacement: the reasons `tiny`, `low_contrast`, `off_page`, `clipped`, `occluded`,
  `invisible_chars` and `text_layer` (the text of a scan is `no_ink`); `Options.DeepHidden`,
  `Extractor.MaxShapes`, `Document.HiddenChecked` and `File.HiddenChecked`; the warnings `text.hidden`
  and `pdf.deep_hidden_incomplete`; the command's `-deep-hidden`. Its `-exclude-hidden` is
  `-exclude-display`.
- The stored part and a page's scripts have no reason; `Attrs["stored"]` says what they are. The
  renderers leave both out unless `RenderOptions.Stored` asks for them (go-fast-core `d8ba98e`); a
  script then stands on its page.
- The command has the render flags of every go-fast command: `-furniture`, `-stored`, `-part-markers`,
  `-children` (on by default) and `-exclude-display`, which now takes `=all`, and with an empty value
  leaves nothing out.
- The command's `-stored` takes kinds: `-stored=macro` shows the scripts alone, each on its page,
  `-stored=macro,form_field` those two kinds; alone, `=true` or `=all` it shows everything, and `=false`
  or empty nothing. An unknown kind is an error that lists them (go-fast-core `d4cd8e2`, which also
  makes a field note stored data and a thematic break always native; this library emits neither).
- Text off the page is placed among the other text by where it is, no longer at the end of its page.

## v0.1.1 and v0.1.0 (2025-12-25)

The first releases: text and image metadata through `pkg/loader`.
