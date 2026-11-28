# Security

go-fast-pdf is made to be given files from anywhere: an upload, a crawl, an attachment of an email.
This document says what it promises for such a file, what it does not, what the limits are, and how to
report a problem.

## What a file can and cannot do

The input is treated as written by someone who wants the program that reads it to crash, to hang, to
use all the memory of its machine, or to return something other than what the file says.

The library

- **runs nothing that a file contains.** JavaScript, actions and launch commands are not run, evaluated or
  followed. Their presence is reported as the warning `input.active_content`, and the text of a script is
  extracted as a code block whose `Attrs["stored"]` is `macro`, so that a consumer can see what it is.
- **reads the metadata packet and the data of an XFA form as text and nothing more.** It is XML: tags, attributes, text and the five
  entities of XML are read, by the scanner of go-fast-core that the go-fast libraries share. No schema, no entity of the file's
  making, nothing fetched.
- **fetches nothing and opens no other file.** A file that refers to data outside itself gets
  `input.external_reference`, and that data is not read.
- **starts no program and writes no file.**
- **does not panic.** An error in one page is a part with the warning `part.failed`, in its place; the
  other pages are returned. Behind that, recovers turn a defect that was not found into a warning whose
  message says "internal error", and not the end of the process:
  - each page, from its content to its layout, tables and annotations, is read under one: the page is
    a part with `part.failed` and no blocks;
  - so is each image: its block stays without data, and the page has `part.failed`;
  - so is the catalog (document information, outline, page labels, structure tree, the list of
    attached files): none of it is read, and the document has `pdf.object_unreadable`;
  - so is each attached file: it is a child with `child.failed`.

  Outside them run the opening of the file (its header, cross-reference and repair, which the fuzz
  targets `FuzzOpen` and `FuzzRebuild` exercise most), and what reads only blocks already made: the
  comparison of pages for running headers and footers, headings and lists. The fuzz targets fail on
  "internal error", so the recovers hide nothing from them.
- **stops.** Every loop over something a file decides is bounded, and the context is honoured: between
  pages, and every 1,024 operators inside a page.
- **bounds its memory** by the limits below. Each is a number that a caller can lower.
- **gives the same result for any number of workers**, also where a limit cuts the document.

It does **not**

- judge what the text says. Extracted text is data from an untrusted file, and so are the document
  information, the outline, page labels and the names of attached files, which no page displays. A
  consumer that gives them to a language model should treat all of them as it treats the text.
- judge whether text can be seen, or whether it is harmful. It reports what the file declares (text in a
  layer that is off, an annotation that is not shown, text drawn with no ink) and nothing else: colour,
  size, position and what covers the text are not looked at. Detecting threats is outside the library.
- decode JPEG or JPEG 2000. With `ImagesData` their data is handed on as it is in the file, and whatever
  decodes it next is given untrusted bytes. Samples and CCITT fax data are decoded here, into PNG,
  within `MaxDecodedBytes`; the one JPEG decoded is a soft mask's, after its header is checked to be of
  the mask's size.
- extract attached files itself. They go to the `Child` extractor that the caller supplies, at most
  `Limits.MaxDepth` levels deep.

## Limits

The file is read into memory whole, so the memory of a call is the size of the file plus a working set
that the limits bound. A service should set `MaxInputBytes` to what it is willing to hold, pass a
context with a deadline, and bound the number of files it reads at once.

Limits of `gofastcore.Limits`, set through `Options.Limits`:

| Limit | Default | What happens at the limit |
|---|---|---|
| `MaxInputBytes` | 1 GiB | `ErrLimitExceeded`, before the input is read |
| `MaxDecodedBytes` | 1 GiB | The document ends before the page that would pass it, with `limit.exceeded` |
| `MaxTextBytes` | 256 MiB | The titles of the outline, the names of the parts, and for every block its text and 128 bytes are counted. The page that reaches the limit is cut before the block that does not fit, with `limit.exceeded`; nothing after it is read and attached files are listed only |
| `MaxParts` | none | The first pages of the range are returned, with `limit.exceeded` |
| `MaxDepth` | 4 | Attached files further down are listed and not extracted |

Limits of `gofastpdf.Extractor`:

| Field | Default | What happens at the limit |
|---|---|---|
| `MaxStreamBytes` | 64 MiB | A stream that decodes to more is cut there; its page gets `limit.exceeded` |
| `ObjectStreamCacheBytes` | 64 MiB | Decoded object streams beyond it are decoded again when needed |
| `ObjectCacheBytes` | 4 MiB | Parsed objects beyond it are let go and parsed again when needed |

Bounds that are fixed:

| What | Bound |
|---|---|
| Objects in a file | 8,388,607, the most the format allows |
| Pages | 1,048,576 |
| Nesting of arrays and dictionaries | 100 |
| Nesting of forms drawn inside forms | 16, and 10,000 forms on one page |
| Saved graphics states, open marked-content sequences | 4,096 each; of the states, nothing is reported (they place text and are not in it) |
| Text of one page | 16 MiB, in 524,288 pieces |
| Images listed on one page | 100,000 |
| Rules of tables recorded on one page | 65,536; 16,384 for one path. Past 131,072 path operators, or 32,768 segments along an axis, a page's paths are not followed further |
| Work of finding the grids of rules on one page | 4,194,304 comparisons; 16,384 cells for one grid |
| Pixels of an image whose data is handed on | 268,435,456; its pixels, at one byte each for gray or a table of colours and four with a mask or in colour, count against `MaxDecodedBytes` |
| Width of a row of CCITT fax data | 1,048,576 pixels |
| Functions of the colour spaces of images (Separation, DeviceN) | Each function object is read once for a reading of a document, and no deeper than 8 functions within functions; 1,024 functions stitched by one, 32 in an array; 4,194,304 values read from their dictionaries over the document. A sampled function has at most 4,194,304 samples and 12 inputs; a calculator program 1 MiB and 65,536 operators, a stack of 100. Samples and programs count against `MaxDecodedBytes`, and 64 MiB of them are kept for the functions referred to again |
| Work of those functions | 268,435,456 operations over one image, 1,073,741,824 over a reading of a document; the colour of each sample, or of each set of samples among the last 4,096, is made once. An image past it is not delivered, with `limit.exceeded` |
| A TrueType program read for its cmap | 32 MiB, and 1,048,576 characters of the subtable read; its /CIDToGIDMap 128 KiB |
| Fonts kept for one file | 1,024 |
| Outline items | 131,072, 64 levels deep |
| Attached files | 4,096; the name and the stated type of each are held to 1,024 bytes |
| Annotations looked at on one page, form fields in a file | 65,536 each; a form field's name from 32 levels of fields, and 4,096 options of a choice field |
| Text written in the annotations, held as values by the fields, and in the scripts of one page | 16 MiB |
| Text of what the file stores and shows on no page (the stored part) | 16 MiB; 4,096 scripts, 1,024 entries of the document information and properties of the metadata packet; 16,384 values of one property and 65,536 of the packet; of XFA form data, 64 levels and 4,096 pieces of text of one field; 256 attributes of an XML element |
| Name trees and number trees | 65,536 nodes, 1,048,576 entries, 32 levels |
| Names (attached files, form fields, authors, keys of the document information) | 1,024 bytes |
| Of the structure tree, on one page | 65,536 marked-content identifiers; 65,536 bytes of a figure's description |
| Elements of the arrays of one operator, text of one code in a ToUnicode map, an inline image's dictionary, a link's address | 1,048,576; 4,096 bytes (the text is cut there, not dropped); 65,536 bytes; 4,096 bytes |
| Warnings of one file | 8 of each code, in the order of their messages, and one more that counts the others |

A limit that cuts something of the output is reported: as an error where there is no document, and where
there is one as `limit.exceeded`, an error, once for each limit on the part or the document it cut, with
a count of what was cut or not read. That includes every fixed bound above that leaves out text, a name,
a value or an address. A bound on what is used only inside the library (the saved graphics states, the
widths of a font, the grids of rules, the fonts kept) changes at most how a page is laid out, and is not
reported. No other code is used for a cut, so a document without `limit.exceeded` lost nothing to a
limit; one that was cut is never returned as if it were whole.

**Counted together with attached files.** Decoded bytes and text are counted for a document and the
files attached to it together, through the budgets of go-fast-core that the context carries. An attached
file is read by the `Child` extractor within what its document has left, and this library reads within
what it is given when it is itself the child. So a file that attaches four thousand files, each just
inside a limit, costs one limit and not four thousand.

**Where the counting happens.** Pages are read side by side. Decoded bytes and text are counted when a
page is handed on in page order, so that the place where a document is cut does not depend on the
number of workers. What is held and not yet counted is the pages being read and those waiting to be
handed on: at most three for each worker, each of at most 16 MiB of text.

**Blocks count as text.** A block takes memory beyond its text: its place, its kind, and for an image
its size and identifier. A file can draw one small image a hundred thousand times on every page with a
few bytes of content for each. So every block takes 128 bytes from `MaxTextBytes` beyond its text, about
what a block takes written down (the documents of 3,150 real files write 157 bytes a block). At the
default of 256 MiB a document has at most two million blocks, and holds at most about five times the
limit in memory (an image block was measured at 662 bytes). The largest real document met so far, 21
pages that draw 271,808 images, takes an eighth of the default.

## Encrypted files

A password is used to derive the file's key. It is not logged and is not put into any error, warning or
result; an open `File` holds its options, the password among them, until it is closed.
A file encrypted with a method this library does not have is `ErrUnsupported`.

A file encrypted for a certificate is opened with a private key from `Options.Keys`, a `crypto.Decrypter`,
so a key can stay in hardware or in another process: the library asks it to decrypt one small key per
recipient and never sees the private key itself. The file's envelopes are read as DER with
`encoding/asn1`; at most 1,024 envelopes of at most 1 MiB each are tried. A key is taken as the right one
only when both layers of padding check and the content has the length of the file's seed; a key that
does not fit gives `ErrKeyRequired`, never text decrypted with a wrong key.

## XPS and OpenXPS

An XPS or OpenXPS document is a zip archive of XML parts, fonts and images. What is said above of a PDF
file holds for one, with these particulars.

- **The archive and its XML** are read by go-fast-core (`opc`, `xmlscan`). No document type declaration
  that declares anything is accepted, no entity of the file's making is expanded, and nothing a part
  names outside the package is fetched: a resource dictionary or an image at an address outside it gets
  `input.external_reference`.
- **The limit on decoded bytes** (`Limits.MaxDecodedBytes`) bounds what the package decompresses to. It
  is counted page by page in the order of the pages: the markup of a page, its story fragments and the
  resource dictionaries it draws on, a font with the first page that uses it, and the data of an image
  when it is asked for. One page may not pass the limit by itself. So a document that the limit cuts
  ends at the same page for any number of workers. The parts that belong to no page (the table of
  content types, relationships, the lists of documents and pages, the outline) are bounded by the same
  number, counted by the package.
- **Further bounds**: 1,048,576 members of the archive and as many pages; 262,144 addressable names;
  a font program of 32 MiB, 4,096 fonts and 256 MiB of them together; 16,384 named resources and 64
  resource dictionaries of other parts for a page, nested 4 deep; Canvas elements and the content of
  visual brushes nested 256 deep; 131,072 entries of the outline; 1,024 signature definitions with 1 MiB
  of text; 64 MiB read of all images together to find their sizes. The text and the shapes of one page
  are bounded as those of a PDF page are.
- **A page** is read under a recover, as a PDF page is: a defect ends that page with `part.failed`, not
  the process. A page whose markup breaks off keeps what stands before the break, with
  `input.stream_damaged`.
- **Fonts** are read for the widths of their glyphs and for which character a glyph shows: the tables
  `head`, `hhea`, `maxp`, `hmtx`, `vhea`, `vmtx`, `cmap`, `name` and `OS/2`. No glyph is drawn and no
  instruction of a font is run.
- **Images** are not decoded. The size of a PNG or JPEG image is read from its header by Go's `image`
  package (`DecodeConfig`), that of a TIFF image from its first directory. With `ImagesData` the bytes
  of the part are handed on as they are, and whatever decodes them next is given untrusted bytes.
- **An encrypted package** (the envelope of a rights-managed or password-protected document, a compound
  file) is refused with `ErrUnsupported`. Its streams are listed by go-fast-core's `cfb`; nothing of it
  is decrypted and no server is contacted.

## How this is tested

- Every pull request fuzzes the targets of `.github/fuzz-targets.txt` for 30 seconds each; every night for an hour each. A release is
  preceded by a longer campaign. Every input that found something is a regression test.
- A soak test reads 3,186 files (the public corpus, and 1,536 further documents that are never used
  for anything else), each in a process of its own under a time limit of a minute and a memory ceiling
  of 1 GiB.
- Tests with files made for the purpose check each bound: pages of a hundred thousand pieces of text,
  images, shapes or names, forms that draw themselves, objects that contain themselves, decompression
  bombs, an outline of a hundred thousand items.
- The suite runs under the race detector on four platforms, and as a 32-bit build.
- For XPS: the fuzz targets `FuzzXPSPage` and `FuzzXPSStructure` give the markup of pages, story
  fragments and outlines to the whole library, and `FuzzIndices`, `FuzzPathData` and `FuzzFace` the
  parsers under it; each bound above has a test with a document made for it; and the public XPS files of
  the shared go-fast corpus are read cut short and with bytes changed, 1,600 copies.

## Reporting a problem

Please report a crash, a hang, unbounded memory, or a way to make the library return text that a file
does not contain, through **GitHub's private vulnerability reporting**: on the repository page, Security,
then "Report a vulnerability". A file that shows the problem is the most useful thing to attach. If the
file is not yours to share, a description of how it was made helps as much.

Problems that are not security problems (wrong text, wrong reading order, a missing feature) are
ordinary issues.

Supported: the latest release.
