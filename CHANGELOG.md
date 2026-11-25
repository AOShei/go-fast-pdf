# Changelog

## v0.4.0 (2026-11-25)

Paragraphs and reading order. The API is that of v0.3; what changes is the text's division into
paragraphs, headings and list items, and the order of text with Hebrew or Arabic in it.

The largest change is that a sentence is no longer cut where it passes between two copies of one typeface.
On 50 French and English documents from the web (3,661 pages: forms, reports, contracts, manuals, journals),
collected for this after the changes were made and used to change nothing, 13.8 percent of the sentences
that plainly run on from one line to the next were cut in v0.3.1, and 2.2 percent are now; by the tags of
the 23 that have them, lines of one paragraph taken apart fall from 8,684 to 1,997 and lines of two put
together from 1,192 to 1,019, 18 files better and 1 worse. In those documents 10,797 such sentences are
joined that were cut, and 18 cut that were joined. 55 headings, nearly all in two files, are no longer
found: the headings that a page can have are bounded by its blocks of text, which are fewer.

- **An entry of a table of contents is a paragraph of its own.** Entries with dot leaders ran on into each
  other, in any language: "Preface .... iv Introduction .... 1". A line that ends in more than three points
  and a page label (a number, "A-3", "1a", a Roman numeral) now ends its paragraph; an entry of several
  lines stays one, and entries with a tab stay rows of one paragraph, as they were. Such a line is not taken
  for a heading either, nor counted as text of its page where headings are. In the 2,631 files of the
  corpora that are not held out, 5,266 of the 7,740 entries without a tab had been joined to what follows;
  60 of these, drawn at random in two lots, are right on their pages, and 3 of them all are before a line in
  small letters. Against the tags 58 files are better and 8 worse; in the half that was read, the tags that
  disagree are of one P over several entries. Entries taken for headings fall from 159 to 7. An entry with a
  section number is now a list item: "2.3.1" is its marker, no longer in its text. 213 of the 3,192 files
  change; one score of the public corpus moves, by +0.0001. Not covered: entries without leaders, or with
  several page numbers. Three fixtures (`testdata/gen-contents.sh`: English, French, Arabic) check it.
- **A typeface that a file embeds twice is one typeface.** A line in another font than the line above it
  is taken for a heading, and what follows it. The fonts were compared as objects of the file, and many
  files embed a typeface several times, a subset for each part they were put together from: a sentence
  that passed from one to another was cut there. Two fonts are now one typeface when their names are the
  same but for the tag of a subset and both or neither are bold, and slanted. Over the 3,192 files of the
  corpora 97 change. Held against the tags of 415 tagged files 30 are better and 3 worse: lines of one
  paragraph taken apart fall from 11,999 to 11,232, lines of two put together rise from 14,010 to 14,042.
  One join was drawn at random from each of 35 of the files and looked at on its page: 26 are right, 5
  wrong and 4 cannot be judged. The wrong ones are short lines one under another, of an address or of the
  labels of a drawing, that had been apart only because their typeface is there more than once; so are,
  in Arabic files, some lines of a table of contents. Seven scores of the public corpus move, three up
  and four down; the largest, -0.125, is of a small page of such labels (`pdfjs/issue15092.pdf`). On 31
  tagged Hebrew and Arabic files from the web, not part of the corpora, 11 are better and 4 worse.
- **A line reads in the direction of its paragraph, and a bracket whose partner is on another line as it
  is set.** Three changes to text with Hebrew or Arabic in it:
  - A closing bracket whose partner was on the line before always read as an opening one, "(" for ")".
    Such a bracket now reads as its line was drawn.
  - A line's direction was decided by that line alone, so the last line of a Hebrew paragraph, of Latin
    words for the most part, read ".Austen או Tolstoy" for "Tolstoy או Austen.", and a line of an English
    one, of Hebrew and a comma, the other way round. The line is now read in the direction of most of its
    paragraph, if it starts where a line next to it does and would not begin with its own full stop. A
    line of Latin words with the mark that ends a sentence at its left end is taken for one that ends
    there, and continues the paragraph above it.
  - A line mostly of Latin words, in another font for want of those letters in the first, was taken for
    one that starts a paragraph. It no longer is where its Hebrew or Arabic letters are all in the font of
    the line above. A heading of Latin words in another font with a Hebrew or Arabic word in the font of
    the text above it looks the same, and can be joined to that text.

  On 40 pages of Hebrew and Arabic set narrow by LibreOffice and Firefox, 29 read with the source's
  paragraphs, word for word, where 5 did, and on 20 of English with Hebrew and Arabic in it 11 where 8
  did; 10 of the other 11 of the 40 have a letter that Firefox gives as another one. Of 180 pages made to
  mislead these rules, with a heading or a line of Latin words set close between two paragraphs, 5 have
  it joined to one of them no longer, and 4 have it joined that had not. Eight fixtures
  (`testdata/gen-rtl-narrow.sh`) check it. Over the 3,192 files of the corpora nothing changes.
- **The first word of a line is measured in characters on every line.** A paragraph ends where the line
  above has room for the next line's first word, and the width of that word was its share of the line's
  bytes: too wide where it has characters of two or three bytes (a quotation mark, a bullet, a letter
  before Latin text), too narrow where the rest of the line has them. It is now its share of the line's
  characters, as it already was on lines with letters written from right to left. Over the 3,192 files
  of the corpora 159 change, 10 of them in the tuning set: held against the page images, 28 of the 29
  paragraphs that change there with text a reader can judge are now as the page shows them, and one, on
  a page of random letters, is not. Two scores of the public corpus move, by 0.0001, one up and one down.
- **Arabic given as ActualText reads in whole words, whatever order its glyphs are drawn in.** A run of
  `/ActualText` began where the first glyph drawn began. LibreOffice, with fonts such as Noto Sans
  Arabic, draws the dots of a letter before its body, so the letter began at its dots, the gap to the
  letter before looked like a space, and words were cut ("ف ي" for "في"). Such a run, in letters
  written from right to left, now reaches over all its glyphs, on the baseline of the widest. A fixture
  checks it (`rtl-ar-noto-libreoffice.pdf`, from `testdata/gen-rtl.sh`). Over the 3,192 files of the
  corpora one changes, PDFium's `arabic_actual_text_ordering.pdf`: the word it sets four times in
  different orders reads the same each time, where three were cut. No score changes.
- **Columns of text written from right to left are read from right to left.** Where the reading order
  cuts a page, or a part of it, into blocks side by side, they were read from left to right whatever
  the script; they are now read from right to left where most of their text is in lines that read so,
  as the columns of a page in Hebrew or Arabic are. Five fixtures set by LibreOffice and Firefox
  (`testdata/gen-rtl-columns.sh`) check it, one of them English with Hebrew and Arabic in it, whose
  columns stay from left to right. Over the 3,192 files of the corpora, one file of the public corpus
  is read otherwise, with the same score.
- **A number at the start of a line reads after the letter before it in its paragraph.** The Unicode
  bidirectional algorithm resolves a paragraph, and the library resolved a line: a number that starts
  a line of Arabic, after an Arabic letter at the end of the line before, is an Arabic number, and a
  percent sign after it stands at its left; it was read as a European number, "%75" for "75%". And a
  paragraph with letters written from right to left is no longer split before a short number: the
  width of a line's first word was its share of the line's bytes, and a digit is one byte where an
  Arabic or Hebrew letter is two. Two Firefox fixtures check it; over the 3,192 files of the corpora
  nothing changes.
- go-fast-core v0.1.9, whose changes since v0.1.5 are not used here: the output is byte-identical.

### Checked before the release

On the code that is tagged: the 3,186 files of the corpora read in processes of their own, three times, with
no crash, hang or memory failure, at most 2.4 s and 658 MiB for a file; instructions per operation of the
benchmarks within 0.8 percent of v0.3.1's, a median change of 0.0 (`bench/v0.4.0.txt`); fuzzing of its 25
targets for an hour each on machines of four processors, 100 processor hours and 3.16 billion inputs, with
nothing found.

## v0.3.1 (2026-11-24)

Two releases of go-fast-core, v0.1.4 and v0.1.5, with the same output except for the words of one
warning: an error of a damaged stream now begins `inflate:` where it said `zlib:`.

- go-fast-core v0.1.4: `Document.Text`, `Document.Markdown`, `Part.Text` and `Part.Markdown` allocate
  their result once, and `WriteText` and `WriteMarkdown` write straight into a `strings.Builder` or
  `bytes.Buffer`. The output is the same: over the 1,650 files of the public corpus, the JSON, Markdown
  and text of the command and the text and Markdown of the library are byte-identical to v0.3.0's.
  `Table.RowLocs` (core v0.1.3) is not filled.
- go-fast-core v0.1.5: the decompressor of zlib streams is go-fast-core's `inflate`, which is this
  library's moved to core for every library (`internal/inflate` is gone). Its tests and fuzz targets
  are in core; `TestCorpusLikeCompressZlib` and `BenchmarkZlibCorpus` stay here, on the streams of PDF
  files. On the 11,394 streams of the tuning set it takes 3.0 percent fewer instructions than before,
  and gives the same bytes as compress/zlib. The CJK CMaps are decompressed with it too, in place of
  compress/flate. An error of a damaged stream in a warning now begins `inflate:` where it said `zlib:`.

Over the 3,192 files of the corpora, the Markdown and text are byte-identical to v0.3.0's, and the JSON
too except in 5 damaged files of the pdf.js and PDFium suites, whose 12 warnings about a stream say
`inflate:` for `zlib:`.

### Checked before the release

On the tree that is tagged: the 3,186 files of the corpora read in processes of their own, three
times, with no crash, hang or memory failure, at most 2.3 s and 666 MiB for a file; instructions per
operation of the benchmarks within 1.1 percent of v0.3.0's, a median 0.3 percent fewer
(`bench/v0.3.1.txt`); fuzzing of its 25 targets for an hour each on machines of four processors,
100 processor hours and 3.5 billion inputs, with nothing found. The two targets of the decompressor
went to go-fast-core with it.

## v0.3.0 (2026-11-23)

Two shared warning codes of go-fast-core v0.1.2, for events that were silent until now. No code is
renamed; the output gains warnings, and loses control characters that were U+FFFD.

### Added

- **`input.decrypted`** (info) on every document that was decrypted, with `Metadata.Encrypted`. The
  message names the security handler (standard, with its revision, or public-key, with its
  sub-filter), the cipher (RC4 with the length of its key, AES-128, AES-256) and what opened the
  file: the user or the owner password given, the empty user password, or which of the private keys
  given. A file that opens with the empty user password is encrypted all the same. An encryption
  dictionary of null is none: such a file is not `Encrypted`.
- **`text.control_characters_removed`** (info), with the count, once for the document (its
  information and metadata packet, outline, page labels, names and attached files), once for a page
  (its notes, form fields, link addresses, and the descriptions and identifiers of its structure),
  and once for the stored part.
- **What the settings of the garbage collector cost and give**, in the README and in
  `bench/gc.md`: the time and the memory with GOGC at 100 (the default), 200 and 400, one document at
  a time with one worker and with 24, and 24 documents at once. The library still leaves the
  settings alone; `go run ./bench/compare -gc` measures them on another machine.

### Changed

- **Control characters in the strings of a file are removed, not U+FFFD.** In the strings outside the
  pages' content, a control character other than a tab or a line end was U+FFFD; it is now removed
  and counted. NUL is among them, and so are the zero bytes a C program leaves at the end of a
  string, which were removed silently. A vertical tab, a form feed and U+0085 are white space, as on a
  page, and become a space: a title that PowerPoint broke over two lines reads as one. A byte that
  PDFDocEncoding leaves undefined among 0x00 to 0x17, and 0x7F, is the control character of that
  number. A glyph of a page whose text is a control character is still U+FFFD with
  `text.unmapped_glyphs`: that is a mapping that is wrong, not a control character in text.
- **A language escape is no text.** UTF-16 and UTF-8 text strings can say in which language the text
  that follows is written: ESC, a language code, perhaps a country code, ESC (ISO 32000-2,
  7.9.2.2.1). It is left out; it was a CJK character between two U+FFFD.
- go-fast-core v0.1.2.

Over the 3,190 files of the corpora, 3,150 of which are read (the other 40 fail with both versions),
the documents are the same JSON as with v0.2.2 except for the new warnings, in all but 36 files.
In 34 of those, U+FFFD is gone or is a space, in outline titles, descriptions of figures and
the document information. In one, the language escapes of its document information. In one more, the
description of a figure was one control character and is now empty. `input.decrypted` is on 97
documents and `text.control_characters_removed` on 45.

### Checked before the release

On the tree that is tagged: the 3,186 files of the corpora read in processes of their own, three
times, with no crash, hang or memory failure, at most 2.4 s and 657 MiB for a file; the speed of the
benchmarks the same as v0.2.2's (`bench/v0.3.0.txt`); fuzzing of its 27 targets for an hour each on
machines of four processors, 108 processor hours and 4.0 billion inputs, with nothing found.

## v0.2.2 (2026-11-23)

Faster, with the same output: for each of the 3,253 PDF files of the corpora and fixtures, the
document is the same JSON as with v0.2.1. The public corpus is read in 1.22 s on one thread, against
1.31 s.

### Faster

- **The content tokeniser reads numbers where it stacks them** (#71). Reading the 1,089 files of the
  public corpus's tuning set on one worker takes 4.5 percent fewer instructions, and the tokeniser's
  share of the time falls from 16 to 10 percent. The output is unchanged: the same JSON for all 3,253
  files of the corpora and fixtures, and a fuzz target that compares it with the reader as it was.
- **Decompression is 9 percent faster** (#74) on the zlib streams of the tuning set, which are a fifth of
  the time to read it: the Adler-32 checksum is computed 32 bytes at a step, 1.7 times as fast as
  `hash/adler32`, and the first level of the decoding tables is of 10 bits for literals and 7 for
  distances, where 9 and 6 took more second looks. Reading the tuning set takes 1.3 percent fewer
  instructions; the output is unchanged.

### The comparison with other libraries

`go run ./bench/compare` also measures three Rust libraries (pdf_oxide, pdf-extract and lopdf), each
on one thread (#70); scores each program's words against pdftotext's, as a word F1 (#72); and writes
into `bench/comparison.md` how far each pair of programs agree, each as the reference in turn (#73).
The F1 measures agreement with pdftotext, not correctness. The time of go-fast-pdf in the table now
includes making the text of the document, as the other programs' times do.

### Checked before the release

On the tree that is tagged: the 3,186 files of the corpora read in processes of their own, three
times, with no crash, hang or memory failure, at most 2.3 s and 656 MiB for a file; the documents the
same as those of v0.2.1; fuzzing of its 27 targets for an hour each on machines of four processors,
108 processor hours and 3.9 billion inputs, with nothing found.

## v0.2.1 (2026-11-22)

What a fixed bound of the library leaves out of the output is reported, never left out silently:
`limit.exceeded`, an error, once for each bound on the part or the document it cut, with a count, in
the words the go-fast libraries share: "X longer than N bytes were cut there (K X, B bytes left
out)", "X past N were not read (K X)", or where reading stops and nothing can be counted, "… has more
than N X; the rest were not read". A bound on what is used only inside the library (the saved
graphics states, the widths of a font) says nothing, and no other code is used for a cut: a document
without `limit.exceeded` lost nothing to a bound. This is go-fast-core's rule from its v0.1.1 (core
PRs #3 and #5). An audit of v0.2.0 found bounds that cut without a word; each of them now reports, and
SECURITY.md lists them all. Over the 3,186 files of the test corpora, documents are the same as in
v0.2.0, except for the nine named below, which gain what a bound cut; no file of them meets a bound
that is newly reported.

### Fixed

- **A ToUnicode text longer than one code may have (4,096 bytes)** was dropped, and the code read by
  the font's encoding: wrong text. It is cut at 4,096 bytes instead, and counted. A CMap of a font
  past its entries or text, and a font program longer than 33,554,432 bytes, are counted on the pages
  that show text in the font.
- **Annotations past the bound of a page** were not read, with no warning: their appearances, notes,
  form values, links and attached files. The page says how many are not read; the bound is now 65,536
  (it was 16,384; a file of the corpus has 32,768 links on one page, which are now read).
- **The values of a property of the metadata packet** were cut at 256. Illustrator's list of swatches
  has up to 512 in the corpus (8 files); the bound is now 16,384 for a property and 65,536 for the
  packet.
- **Reported now, where they were silent:** the elements of an operator's arrays past 1,048,576; the
  options of choice fields past 4,096; names longer than 1,024 bytes; form field names past 32 levels;
  the outline past 131,072 entries or 64 levels; name and number trees past their bounds; attached
  files, scripts and form fields past theirs; entries of the document information and properties of
  the metadata packet past 1,024; XML attributes past 256; XFA data past 64 levels, or 4,096 pieces of
  text of a field; descriptions of figures longer than 65,536 bytes; marked-content identifiers of a
  page past 65,536; links with an address longer than 4,096 bytes; marked content nested past 4,096;
  dictionaries of inline images longer than 65,536 bytes.
- **The bounds that reported already** (images of a page past 100,000, forms nested or repeated past
  their bounds, the text of a page) say it in the same words, with a count.
- **An image past the pixels or the work whose data is made** gives `limit.exceeded`, not
  `image.unsupported_encoding`.
- **Attached files and images over the bound on one stream** (`MaxStreamBytes`) were reported as the
  document's decoding budget running out; they are now reported as what they are, with a count.
- **Several bounds on one page** gave one `limit.exceeded`, the first; each bound now gives its own.
- **The warnings of the document as a whole** were kept up to 100, and the rest dropped without a
  count. Eight of each code are kept, in the order of their messages, and one more warning counts the
  others.
- **A ruled table could lose the text of a cell** when a line of text crossed one of its rules, as on
  a page whose rules are broken or turned: two cells had their text in one place, and the table kept
  one. Such a band of rows is not cut along its lines, and two cells never share a place; where they
  would, the text stays paragraphs.
- **The text of a list item could start with a space outside ASCII** (U+00A0 and the like) after its
  marker, against the shared model's rule that a block's text has none at its ends.
- **The box of a note or of a form field's value** could fall past the range of coordinates when the
  page's matrix moved it there (a page turned, or its box moved). Such a box is none.
- **q and marked content opened past their bound (4,096)** were not kept, but the Q or EMC that closed
  each closed a level that was: the matrix of a q around them, or a sequence with its replacement text,
  ended early. Their closers close nothing kept now.
- Found by fuzzing while this was checked: a warning that quotes a name of the file (the colour
  space of an inline image) could hold a C1 control character, and an author, title or subject taken
  from the metadata packet could hold a ligature (U+FB00 to U+FB06), against the rules of the shared
  model. Both are cleaned now, as the document information is.

### Faster

- **Fewer collections with many workers.** Each goroutine that reads the pages of a document keeps
  its working state (the interpreter of the content and the builder of the layout, with their lists)
  from page to page for as long as the document is read, rather than in a `sync.Pool` that loses it
  at every collection; when the document is read it goes to the pool. On the documents of the
  comparison, with 24 workers, collections went from about 450 to about 110, and the time from 28 ms
  to 16 to 19 ms (v0.2.0: 19 to 21 ms). A font's name is no longer given to a new `strings.Replacer`
  for every font.
- **Nothing of a document is kept after it is read.** The pooled state of the reading and of the
  layout kept the options of its last page, whose functions hold the document's structure tree and so
  all of the document, and the interpreter references into the document's data; they let go of all of
  it now. The soak's largest resident memory, which moves from run to run with the collections, was
  516 to 674 MiB over six runs, against 659 to 775 MiB before.
- **The annotations of a page are read once.** The reading of their appearances hands each one to what
  else is taken from it (notes, form values, links, attached files, actions), which read the page's list
  again each before. A page of 32,768 links (a file of the corpus) is read in half the instructions,
  and the tuning set of the public corpus in 6 percent fewer than with v0.2.1's higher bound on
  annotations alone. The objects a file holds in its cache are cleared by their numbers when the cache
  is emptied, rather than every slot of the file.

### Changed

- go-fast-core v0.1.1: `Part.Warn` and `Document.Warn` keep at most 1,000 warnings, then count the rest
  in one `warnings.omitted`; `limit.exceeded` covers a library's fixed bounds.

### Checked before the release

On the tree that is tagged: the 3,186 files of the corpora read in processes of their own, three
times, with no crash, hang or memory failure, at most 2.4 s and 675 MiB for a file; the documents
the same as those of v0.2.0 except for the nine that gain what a bound cut; fuzzing of its 25 targets
for an hour each on machines of four processors, 100 processor hours and 3.4 billion inputs, with
nothing found (on the way to it, fuzzing found the five defects fixed above); the public corpus read
in 1.3 s on one thread.

## v0.2.0 (2026-11-21)

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
  tints, of any of the four types: sampled, exponential, stitching and PostScript calculator. A function
  is read once for a reading of the document however often it is referred to, and its samples and
  program count against `MaxDecodedBytes`. Images of a palette, of gray to 4 bits, and stencil masks
  (scans among them) are written to PNG from their rows of samples as they are, and 8-bit gray, RGB
  and CMYK are made a row at a time: a scanned file of 124 pages gives its images in 0.7 s instead of
  5.3 s, and the PNG bytes are those of `image/png`.
- **Forms and annotations.** The appearances of annotations are read as part of the page: the values
  that filled form fields show, stamps, notes written on the page. What is written in a comment is a
  `note` block; the value of a field that shows nothing is a block with the field's name; the options of a list or
  combo box are a `choices` note. Those whose flags say they are not shown have the reason `hidden`.
- **What a file stores and shows nowhere** is extracted: scripts, the metadata packet, entries
  of the file's own making in the document information, form fields on no page, the filled-in data of an
  XFA form (which only Adobe's viewer shows). A page's scripts are on
  the page; the rest is in a last part of kind `stored`. Each block of it says what it is in
  `Attrs["stored"]`, and the renderers leave it out unless `RenderOptions.Stored` asks for it. Nothing
  is run.
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
  source, where pdftotext reads few of its letters. Such a page has the warning
  `pdf.glyph_text_by_outline`, with the number of characters read that way, since the file itself gives
  those glyphs no text.
- **Limits** on input size, decoded size, the text of a document, pages, nesting and more; see
  [SECURITY.md](SECURITY.md). Decoded bytes and text are counted for a document and the files attached to
  it together. Cancellation through the context, also inside a long page.
- **The command `gofastpdf`**: JSON, Markdown or text, for files and directories, with the render flags
  every go-fast command shares: `-furniture`, `-stored` (all of it, or kinds: `-stored=macro,form_field`),
  `-part-markers`, `-children` (on by default) and `-exclude-display` (`=all`, or reasons).
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
| 1,000 synthetic pages | 61.9 ms | 20.0 ms |
| A 157-page manual with graphics | 29.1 ms | 9.2 ms |
| A 3-page text document | 0.278 ms | 0.107 ms |
| Resolving every object of a file that keeps them in object streams | 411 ms | 1.5 ms |
| Allocations per page of the synthetic document | 975 | 21 |

Against other libraries, see the table in the README, which `go run ./bench/compare` writes.

### Checked before the release

What "ready for production" was taken to mean, and what each point was measured by:

| Property | Measured |
|---|---|
| Never crashes | 3,186 files and 50,995 pages read, each file in a process of its own, with the default options and with a second set of options: no panic, no stack overflow, no hang. Fuzzing: 24 targets for an hour each on machines of four processors, 96 processor hours and 3.3 billion inputs, with nothing found |
| Bounded resources | In six runs of it no file took more than 3.8 s of the 60 s allowed or more than 775 MiB of the 1 GiB allowed, for a process that reads the file twice and checks the results. The limits and fixed bounds are listed in SECURITY.md; cancellation is tested between pages and inside a long page. The text and the number of blocks of a document, with the files attached to it, are bounded by `MaxTextBytes` |
| Correct text | Word-level F1 against `pdftotext`: at least 0.988 on each of the 27 born-digital Latin files of the fixture corpus (the floor is 0.98), 1.0 on each of the 8 files in other scripts (floor 0.95). Public corpus: mean F1 0.923 and order 0.860 over 1,650 files; 0.983 and 0.908 on the 200 ordinary documents of GovDocs1 |
| Conforms to the shared model | Every document of the runs above passes `Validate` and the contract checks, and is equal after a round trip through JSON |
| Honest failure | 21 files that need a password and 15 that are not readable PDFs return the matching error; 27 pages that cannot be read are parts with `part.failed`. No file gives text that is not valid UTF-8 |
| Reports what the file declares | Every declaration has a fixture that asserts its reason and that the other text is unchanged; every case that is not a declaration (white on white, a 1-point font, text off the page, under an image or a shape, outside a clip) has one that asserts no reason; characters that display as nothing survive |
| Safe for concurrent use | The suite passes under the race detector on linux/amd64, linux/arm64, darwin/arm64 and windows/amd64; one open file is read from many goroutines; the result is the same for 1 and 8 workers on every file of the corpus |
| Fast | Budgets for allocations per page are tests; the nightly job compares every benchmark with the last release on one runner; the comparison with other libraries is one command |
| Stable API | The public surface is a snapshot under test; every exported name is documented; ten examples run as tests |
| Standard library and go-fast-core only | Checked by CI on every change |

Display reasons in the soak run, as blocks (and files with such a block), of 729,492 blocks of text in
3,150 files: `hidden` 89 (10), `no_ink` 34,580 (92).

## v0.1.1 and v0.1.0 (2025-12-25)

The first releases: text and image metadata through `pkg/loader`.
