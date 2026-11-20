// Package gofastpdf extracts the text and structure of PDF files into
// the document model shared by the go-fast libraries
// (github.com/AOShei/go-fast-core). It uses the standard library and
// the go-fast modules only, starts no program, fetches nothing, runs
// nothing that a file contains, and does no OCR.
//
//	doc, err := gofastpdf.ExtractFile(ctx, "report.pdf", nil)
//	fmt.Print(doc.Markdown())
//
// # Two ways to read a file
//
// Extract and ExtractFile return the whole document at once: its parts,
// and with them the document information, the outline, the attached
// files and the warnings. That is the entry point every go-fast library
// has, and Extractor is its form as a gofastcore.Extractor.
//
// Open, OpenBytes and OpenReaderAt return a File, which reads nothing
// of the pages until it is asked:
//
//	f, err := gofastpdf.Open("report.pdf", nil)
//	defer f.Close()
//	for part, err := range f.Parts(ctx) {
//		...
//	}
//
// File.Parts hands out the parts in page order, reading several pages
// at a time, and keeps none of them: a file of any number of pages is
// gone through in the memory that the file, a few pages and a bounded
// amount of what was parsed take, plus about two hundred bytes for each
// page of the file. File.Page gives one page by its number, from any
// goroutine. Page.Lines gives more than the part has: the lines of the
// page with the font and size of each and the box of every word.
// File.Document is Extract on an open file.
//
// The parts are the same whichever way they are read, with one
// difference for image data, which Page.Part describes. What Extract
// puts in the document as a whole, a File gives by its methods
// Metadata, Outline and Warnings; the last is final only when every part
// has been produced, since a page can add to it.
//
// Reading stops when the context ends, between pages and inside a page
// whose content is long. Nothing is printed: what goes wrong is an
// error or a warning in the result.
//
// Settings that only PDF has are the fields of Extractor, whose methods
// are the functions of this package: the size one stream may decode
// to, and how much of what was parsed is kept.
//
// The command gofastpdf, in cmd/gofastpdf, writes files as JSON,
// Markdown or text.
//
// # The document
//
// A document has one part per page, of kind page, numbered as the
// pages are in the file also when only a range is extracted. A part's
// Width and Height are the page as it is displayed, in points: the crop
// box, turned by the page's rotation and scaled by its user unit. Every
// box of a block is in that space, with its origin at the top left. A
// page label that differs from the page's number is the part's Name.
//
// Text blocks are paragraphs, in reading order. A PDF without tags
// does not say where a paragraph ends or in which order a page is read,
// so both are worked out from where the text is, and every text block
// has OriginInferred.
//
// A table of a tagged PDF, which the structure tree says is one (Table,
// with rows TR and cells TD and TH), is a block of kind table with
// OriginNative. Its grid is the tree's: a cell takes the first column
// its row leaves free, RowSpan and ColSpan make merges, and the leading
// rows in THead, or all of TH, are HeaderRows. The visible text of each
// cell is laid out by itself; text in a cell that the file declares not
// shown or drawn with no ink stays out of the table, a block of its own
// with its reason, as such text is anywhere. A table over
// several pages is a block on each, with the rows that are on that page,
// and the block on a later page is Continued. A table whose text the
// page does not bear out, a row reaching into the next or cells that do
// not follow each other across the page, is taken for a tree made badly:
// its text is laid out as if the tree had no table there. Other tables
// are not recognised yet: their text is in paragraph blocks, a row a
// line with its cells apart by tabs.
//
// A paragraph that starts with the marker of a list item is a block of
// kind list_item: a bullet, a dash, or a number or letter that counts
// ("3.", "(a)", "iv)", "4.2.1"). The marker is in Block.Marker, out of
// the text, and Ordered says whether it counts; the renderers put it
// back in front. Every line that starts with a marker starts an item.
// Level is how deep the item is nested, by how far right its marker
// stands against the items before it on the page. Not markers: a
// decimal number, a year, an initial ("J. Smith"), an em dash (prose
// starts lines with it), an asterisk (more often a note), and a line
// with a tab after the marker, which is a row of a table. A numbered
// line set as a heading is a heading and keeps its number as Marker.
//
// Headings are blocks of kind heading. In a tagged PDF, a block whose
// text is all part of one heading of the structure tree (H1 to H6, or H
// at the depth of the sections around it, through the role map) is that
// heading with OriginNative, and a page with such a heading has no
// other heading by type. A block that an entry of the
// outline names, on the page the entry points at, is a heading at the
// entry's level with OriginNative: the file says it is one. A level
// deeper than six, which an outline or nested H elements can give, is
// 6, with the real level in Attrs["level"] and the warning
// structure.heading_level_clamped on the part. Otherwise a
// short block is a heading when it is set at least 15 percent larger
// than the text of the document, or bold on one line where the text is
// not bold; its level is from its size, 1 for a title set about twice
// as large as the text, down to 4 for bold at the size of the text. A
// page with more such blocks than it can have headings, a map or a
// diagram, gets none by type. The size of the text of the document is
// the size most of it is set in, counted over the pages put in order so
// far and those that wait after them; a page read by itself uses its
// own.
//
// Running headers and footers are blocks of kind header and footer: text
// that the pages repeat at their top or bottom, a title, a date, a page
// number, found by comparing each page with up to four pages on each
// side of it. The renderers of the shared model leave them out unless
// RenderOptions.Furniture asks for them. A paragraph that a page break
// splits is marked Continued on the page where it goes on, and the
// renderers run its two pieces into one. Both are decided as the pages
// are put in order, by Extract, File.Parts and File.Document; a page read
// by itself with File.Page is compared with nothing and has neither.
//
// What the annotations of a page display is part of the page: the
// value shown in a form field, a note written on the page, a stamp.
// Their appearances are read after the content of the page, as a
// viewer paints them, and their text is in the paragraphs like any
// other. Two more things an annotation holds are text, and are blocks
// of their own:
//
//   - What is written in an annotation, which a viewer shows in a
//     window beside the page or in a list: a block of kind note, of type
//     comment, with its author and date where the file has them, after
//     the other blocks of its page.
//   - The value of a form field whose appearance shows nothing, which a
//     viewer would draw by itself: a paragraph at the place of the
//     field, after the text that starts above it, with the full name of
//     the field under the key "field" (gofastcore.AttrField). A choice gives the
//     text of the option as it is displayed, a check box or radio
//     button that is on the name of its state. A push button has no
//     value, a signature is not text, and the value of a password field
//     is not read.
//   - The options of a choice field, a list box or a combo box: a note of
//     type choices (gofastcore.NoteChoices), one option per line as it
//     is displayed, in the order of the file, with the field's name,
//     after the block of the field's value where there is one. It is
//     there once for a field however many widgets show it, and has no
//     display reason for being a list: a reader who opens it sees the
//     options.
//
// Both have OriginNative: the file says what they are. An annotation
// whose flags say it is not shown (Hidden, NoView), or that is in
// optional content that is off, has its text, displayed or written,
// with the reason hidden.
//
// # Anchors
//
// Loc.Anchor is a name the file gives a block. A named destination,
// which links of the document and links to it from outside use
// (#nameddest=), names a place on a page; the block that begins there,
// a little below the place and not to its left, has the name, with
// Options.Spans. A destination that names a page, a place inside a
// block, a place no block begins near, or a running header or footer
// names no block. Finding every named destination means reading an
// object for each, as long as some pages take in a file with many,
// which is why it comes with the links. In a tagged PDF, an element of
// the structure tree with an identifier (/ID) gives it to the first
// block of its content, whatever the options; where both name a block,
// the destination wins. A name longer than 512 bytes is left out.
//
// # What a file stores and shows nowhere
//
// A PDF can carry text that nobody who opens it sees: scripts, a packet
// of metadata in XML, entries of the file's own making in the document
// information, fields of a form that no page shows. A program that reads
// the extracted document reads it all the same, so it is extracted and
// marked with what it is in Attrs["stored"] (gofastcore.AttrStored),
// with no display reason: where it is says what it is. Nothing of it is
// run or followed.
//
//   - A script that an action of a page or of an annotation on it would
//     run is a code block on that page, after its other blocks: stored
//     "macro", with Attrs["language"] "javascript".
//   - What belongs to the file and to no page is in one part of kind
//     stored, after the pages and numbered after them: the scripts that
//     the document names or that its own actions and its form would run
//     ("macro", with the name the document gives a script); entries of
//     the document information other than the standard ones
//     ("document_variable", as "name: value"); what the metadata packet
//     says that Metadata does not ("custom_xml", as "property:
//     values"); and the values of fields of the form that
//     are on no page ("form_field", with the field's name).
//   - The data of an XFA form, which only Adobe's viewer shows: each
//     value filled in, as "form_field" with the path of its element as
//     the field's name (form1.address.city). A value that the form
//     also has in an ordinary field, as a static XFA form does, is read
//     there and not said again. The template of the form, its captions
//     and its static text, is not read.
//   - What follows the last %%EOF of the file, padding aside, when it is
//     text: UTF-8, or Latin-1 without control characters ("after_end").
//     An update added to a file ends in a %%EOF of its own, so only what
//     follows the last one counts. The warning input.trailing_data says
//     how many bytes follow the end, also when they are not text.
//
// The stored part, and a page's scripts, have no display reason: they
// are where they are, which says what they are. The renderers of the
// shared model leave the stored part out unless RenderOptions.Stored
// asks for it; a page's scripts are rendered with the page.
//
// A value is given once, and values that say nothing in words (a yes or
// no, a number, a date, an identifier) are left out; anything with
// words in it is kept, whatever property it is under. The stored part is
// there only when the whole document is read: Options.Parts and
// Limits.MaxParts leave it out, and File.Parts gives it last. Its text
// is bounded (16 MiB) and counted against the limit on text.
//
// The document information becomes Metadata: the title; the author as
// one entry, not split; subject and keywords; the creating program as
// Application and the producing one as Extra["producer"]; the dates;
// the language of the catalog; and whether the file was encrypted. The
// metadata packet (XMP) adds to it. Its lists of authors (dc:creator)
// and of keywords (dc:subject) are the authors and the keywords, an
// entry for each item as the file declares it, unless the information
// was changed after the packet (ModDate later than xmp:MetadataDate);
// its pdf:Keywords is one entry where it has no list. What the
// information leaves out is taken from the packet: the title, the
// subject (dc:description), the programs, the dates and the language.
// Properties are known by their namespace, whatever their prefix. The
// bookmarks become Outline, each with the page it points at and how far
// down that page. Files attached to the document or to a page become
// Children.
//
// # Options
//
// The options are those of gofastcore.Options. Here they mean:
//
//   - Workers: pages read at a time, by Extract and by File.Parts. The
//     result is the same for any number.
//
//   - Images: with ImagesMeta, which is what options mean that name no
//     mode, an image block stands where each image
//     is, with its size in pixels; its ID is "obj-" and the number of
//     the image's object, or, for an image inside a content stream,
//     "inline-", the number of its page, a hyphen and its count on that
//     page. With ImagesData the first block of each image also has its
//     data: JPEG and JPEG 2000 as they are in the file, samples of 8
//     bits in a gray or RGB colour space as PNG. An image in another
//     encoding has no data, and its page gets
//     image.unsupported_encoding. With ImagesNone there are no image
//     blocks. In a tagged PDF an image drawn as part of a figure that
//     the structure tree describes (/Alt) has that description as its
//     text. A block for every image costs under one percent of the time
//     on most documents, and up to as much again on a page that is
//     nearly all images.
//
//   - Password: tried as the user password and as the owner password.
//     The empty user password is always tried.
//
//   - Keys: private keys, for a file encrypted for a certificate (the
//     public-key security handler, /Adobe.PubSec). Every key is tried on
//     every recipient the file names. Keys are RSA: the file's envelope
//     holds its key encrypted with RSA, with PKCS #1 v1.5 or OAEP, and
//     the envelope's own content in AES or triple DES. An envelope in
//     RC2, which old versions of Acrobat wrote, is ErrUnsupported. A
//     file that no key opens is gofastcore.ErrKeyRequired; a password
//     does not open it.
//
//   - Parts: a range of pages, for Extract, File.Parts and
//     File.Document. A range that holds no page of the file is
//     gofastcore.ErrNoPartsSelected. File.Page gives any page.
//
//   - Limits: MaxInputBytes (default 1 GiB) is checked before the input
//     is read. MaxDecodedBytes (default 1 GiB) bounds what the streams
//     of a document decode to, taken together and counted in page
//     order: the document ends before the page that would exceed it.
//     MaxTextBytes (default 256 MiB) bounds the text of a document: the
//     titles of its outline, the names that page labels give its parts,
//     and the text of its blocks. Every block counts as its text and
//     128 bytes more, about what a block takes written down, so that a
//     document of very many blocks without text, such as a page that
//     draws one image a hundred thousand times, is bounded as well. The
//     page that reaches the limit is cut before the block that does not
//     fit, and nothing after it is read.
//     MaxParts keeps the first pages of the range. MaxDepth bounds how
//     deep attached files are followed. A limit that cuts the document
//     adds limit.exceeded; File.Parts ends with ErrLimitExceeded there.
//
//     Decoded bytes and text are counted for a document and the files
//     attached to it together, through the budgets of gofastcore that a
//     context carries: an attached file is read within what its
//     document has left, and this package reads within what it is given
//     when it is itself the Child of another extractor.
//
//     Both are counted where the pages are put in order, a page at a
//     time, and not while a page is being read: pages are read side by
//     side, and counting them as they finish would make the place of
//     the cut depend on which finished first. What is held and not yet
//     counted is the pages being read and those waiting to be handed
//     on, at most three for each worker, each of at most 16 MiB of text.
//
//   - Child: the extractor for attached files. Without one they are
//     listed with their names and sizes, and nothing of them is decoded.
//
//   - Spans: gives the text blocks spans of bold and italic, by the
//     fonts of their words, and of links. A link is a link annotation of
//     the page: the words it covers, each word as a whole, go to the
//     address of its URI action, or, for a destination in the document,
//     to "#page=N" (the open parameter of a PDF for page N). Links to
//     other files, links that are hidden and addresses over 4 KiB have
//     no span. Link targets count against Limits.MaxTextBytes. A word
//     is placed by its share of the characters of its run, and a word
//     that the joining of lines changed may have no span. Spans take a
//     tenth more time on pages of text, up to a third on short
//     documents. With Spans, the names of the destinations of the
//     document are also anchors: see "Anchors".
//
// # Text
//
// The text of a block follows the conventions of the shared model. The
// lines of a paragraph are joined with one space. A hyphen at the end
// of a line stays and the next line follows without a space, so that
// "well-" and "known" are "well-known" and "inter-" and "national" are
// "inter-national". A soft hyphen at the end of a line, between the
// letters of a word, is dropped and the word joined again; anywhere else
// it stays.
// Ligatures are written as their letters. A character the file gives no
// meaning for is U+FFFD, and so is a control character.
//
// Two characters in a block's text are put there by this package. A tab
// stands where two pieces of a line are more than twice the font size
// apart: a label and its value, the cells of a row. A line break stands
// between two such rows, which are kept in one block. Text set with its
// letters apart is read as its words when the rest of its line shows
// that it is one; a line that consists of nothing but single letters
// evenly apart is read as a word, which is wrong for a row of separate
// letters.
//
// The order of reading is found by cutting the page along the white
// space between its blocks. Where a page can be cut in more than one
// way, the cut is taken that agrees best with the order in which the
// page draws its text: a page drawn column by column is read by
// columns, a table drawn row by row is read by rows. Text in a script
// written from right to left is given as the page draws it, and its
// page has pdf.rtl_visual_order.
//
// # What the file declares
//
// Text is extracted whether or not a reader sees it. Where the file
// itself declares, by one switch or one value, that text is not shown
// or is drawn with no ink, the text's block (Block.Display) has the
// reason; following the file's own inheritance, through forms, groups
// and membership dictionaries, is reading the declaration. Nothing else
// gives a reason. The caller decides what to do with the text, for
// instance with RenderOptions.ExcludeDisplay.
//
//	hidden   content in optional content (a layer) that is off in the
//	         document's default configuration (/OFF, /BaseState /OFF,
//	         or a membership dictionary whose policy its layers make
//	         off); the text of an annotation whose flags are Hidden or
//	         NoView, and the value of such a form field
//	no_ink   text in render mode 3, neither filled nor stroked, or 7,
//	         clip only, when nothing is painted through it; text whose
//	         opacity is 0 for what its mode paints (/ca for the fill,
//	         /CA for the stroke), by its graphics state or that of the
//	         transparency group it is drawn in
//
// The searchable text of a scanned page is text in render mode 3 over
// the picture of the page: it is no_ink, and leaving out no_ink leaves
// out a scan's text.
//
// Not judged: colour against what is under the text, its size, where it
// is on the page or outside it, what is painted over it, clipping
// paths, soft masks and blend modes, how much of the page an image
// covers. Text with a reason is laid out apart from the rest, so that
// the other blocks are what they would be without it. The absence of a
// reason says nothing about whether text can be seen. Characters are
// kept as the file gives them, zero-width characters, tag characters
// and bidirectional controls included. Judging text by what it may do
// to a reader or a program is outside this package.
//
// # Warnings
//
// The codes of the shared model are used where they apply. Those that
// need a word about PDF:
//
//	input.repaired            the cross-reference data was unusable, or
//	                          wrong about some object, and the objects
//	                          were found by scanning the file
//	input.ambiguous           the file had to be read by scanning and
//	                          defines an object more than once. The last
//	                          definition is read, as it is the newest in
//	                          a file that was saved in steps
//	input.active_content      JavaScript (in the name tree, on opening
//	                          or closing, on a page, a link or a form
//	                          field) or an action that starts a program
//	input.external_reference  a stream or an image whose data is in
//	                          another file, a page of another document
//	                          shown in this one, an action that goes to
//	                          another file, sends a form or loads data,
//	                          a launch that names a file. A link to an
//	                          address is an ordinary link and is not one
//	input.trailing_data       bytes other than padding follow the last
//	                          %%EOF of the file; the message says how
//	                          many. Their text, where they are text, is
//	                          in the stored part (Attrs["stored"] is
//	                          after_end), which then has the warning;
//	                          otherwise the document has it
//
// Scripts and actions are looked for on the pages that are extracted
// and in the catalog and the form fields; no script is read.
//
// Codes that only this package uses, each for a fact about the file:
//
//	pdf.rtl_visual_order            a page has text written from right
//	                                to left, given as the page draws it
//	pdf.glyph_text_assumed          characters of a font that does not
//	                                say what its glyphs are were read as
//	                                the characters their codes stand for
//	pdf.font_missing                text is set in a font that the
//	                                page's resources do not define
//	pdf.content_stream_damaged      a content stream is damaged; what
//	                                came before the damage was read
//	pdf.content_stream_unreadable   a content stream could not be decoded
//	pdf.content_bytes_skipped       bytes of a content stream are not
//	                                valid syntax and were skipped
//	pdf.object_unreadable           an object could not be read
//	pdf.page_tree_damaged           a node of the page tree is not what
//	                                it should be
//	pdf.xref_section_unreadable     a section of the cross-reference
//	pdf.xref_entry_unreadable       data, or an entry of it, is unusable
//	pdf.string_undecryptable        a string of an encrypted file could
//	                                not be decrypted
//
// # Errors
//
// An error means that there is no document. It wraps one of the errors
// of the shared model: ErrMalformed for input that is not a readable
// PDF, including one without pages; ErrPasswordRequired and
// ErrWrongPassword; ErrKeyRequired for a file encrypted for a
// certificate that no key of Options.Keys opens; ErrUnsupported for an
// encryption this package does not have; ErrLimitExceeded for input larger than MaxInputBytes;
// ErrNoPartsSelected. A file that can be read in part is not an error:
// a page that cannot be read is a part with part.failed, in its place.
package gofastpdf
