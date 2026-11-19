// Package gofastpdf extracts the text and structure of PDF files into
// the document model shared by the go-fast libraries
// (github.com/AOShei/go-fast-core). It uses the standard library and
// the go-fast modules only, starts no program, fetches nothing, runs
// nothing that a file contains, and does no OCR.
//
//	doc, err := gofastpdf.Extract(ctx, file, size, nil)
//	fmt.Print(doc.Markdown())
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
// has OriginInferred. Headings, lists, tables and running headers are
// not recognised yet: their text is in paragraph blocks.
//
// The document information becomes Metadata: the title; the author as
// one entry, not split; subject and keywords; the creating program as
// Application and the producing one as Extra["producer"]; the dates;
// the language of the catalog; and whether the file was encrypted. The
// bookmarks become Outline, each with the page it points at and how far
// down that page. Files attached to the document or to a page become
// Children.
//
// # Options
//
// The options are those of gofastcore.Options. Here they mean:
//
//   - Workers: pages extracted at a time. The document is the same for
//     any number.
//   - Images: with ImagesMeta an image block stands where each image
//     is, with its size in pixels; its ID is "obj-" and the number of
//     the image's object, or "inline-" and a count for an image inside
//     a content stream. With ImagesData the first block of each image
//     also has its data: JPEG and JPEG 2000 as they are in the file,
//     samples of 8 bits in a gray or RGB colour space as PNG. An image
//     in another encoding has no data, and its page gets
//     image.unsupported_encoding.
//   - Password: tried as the user password and as the owner password.
//     The empty user password is always tried.
//   - Parts: a range of pages. A range that holds no page of the file
//     is gofastcore.ErrNoPartsSelected.
//   - Limits: MaxInputBytes (default 1 GiB) is checked before the input
//     is read. MaxDecodedBytes (default 1 GiB) bounds what the streams
//     of a document decode to, taken together and counted in page
//     order: the document ends before the page that would exceed it.
//     MaxParts keeps the first pages of the range. MaxDepth bounds how
//     deep attached files are followed. A limit that cuts the document
//     adds limit.exceeded.
//   - Child: the extractor for attached files. Without one they are
//     listed with their names and sizes, and nothing of them is decoded.
//   - DeepHidden: also follows the shapes that a page paints and its
//     clipping paths, and checks text against them: see "Hidden text".
//     It costs time on pages with many shapes and changes no text.
//   - Spans: not implemented yet and ignored.
//
// # Text
//
// The text of a block follows the conventions of the shared model. The
// lines of a paragraph are joined with one space. A hyphen at the end
// of a line stays and the next line follows without a space, so that
// "well-" and "known" are "well-known" and "inter-" and "national" are
// "inter-national"; a soft hyphen at the end of a line is dropped.
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
// # Hidden text
//
// Text that a reader of the file cannot see is extracted like any other
// and flagged with the reason in Block.Hidden; a part that has such a
// block has the text.hidden warning. Nothing is left out: the caller
// decides, for instance with RenderOptions.ExcludeHidden. Hidden text
// is laid out apart from the visible text, so the visible blocks are
// what they would be if it were not in the file.
//
// These checks always run:
//
//	invisible     text in render mode 3, or in mode 7 with nothing
//	              painted through it; content of a layer that is off
//	text_layer    text that is not painted, or is painted and then
//	              covered, within an image that covers at least half of
//	              the page: the text of a scan. It is the only reason
//	              such text gets, and its warning is at info
//	tiny          text displayed smaller than 2 points
//	off_page      text that lies wholly outside the visible page
//	transparent   text whose every painted part has opacity 0, by itself
//	              or as part of a transparency group
//	occluded      text under an opaque image that is drawn after it
//	clipped       text outside the box of the form it is drawn in
//	low_contrast  text that cannot be told from white, when nothing was
//	              painted on the page before it
//
// With Options.DeepHidden the shapes that the page paints are followed
// as well, and three of the reasons are checked against them:
//
//	low_contrast  text that cannot be told from the rectangle it stands
//	              on, or from the white page where nothing is under it
//	occluded      text under an opaque rectangle that is painted after
//	              it, such as a box drawn over text to blank it out
//	clipped       text outside what the clipping paths in force leave
//	              of the page
//
// What these checks know of a shape is its box, and of one kind of
// shape all: an upright rectangle filled with a colour, under a clip
// that is a rectangle too. Any other path, a stroke, a shading, a
// pattern, an image and anything painted through a soft mask is a
// region in which something is painted that is not followed. So:
//
//   - The background of text is the last thing painted before it that
//     touches it. Its contrast is judged when that is one opaque
//     rectangle of a known colour that holds it, or nothing at all. On
//     any other background, a photograph or a shading or a banner with
//     round corners, it is not judged.
//   - Nor is it judged when a region painted after the text touches it:
//     a page can draw white text first and the dark ground afterwards,
//     around the letters. This takes back a low_contrast that the check
//     without shapes gives such text.
//   - Text is covered by a rectangle that lets nothing show through:
//     not by one painted with an opacity below 1, with a blend mode or
//     with overprinting.
//   - A clipping path that is no rectangle is taken as its box, which
//     is larger: text inside the box and outside the path is not found.
//     An image painted through such a path is not taken to cover what
//     is under it, with DeepHidden or without.
//
// Text is covered, and stands on a background, with the band of its
// small letters: from the baseline to somewhat over half the font size
// above it. Text cannot be told from its background when the shared
// measure of contrast, which compares brightness, is below its threshold
// and the two colours are also within an eighth of the range of each
// other in red, green and blue: green text on a red box of the same
// brightness is in plain view. Text painted with an opacity is judged as
// the colour that shows, and text painted with a blend mode or through
// a soft mask is not judged.
//
// Document.HiddenChecked lists the reasons that were checked as fully
// as this package can. Without DeepHidden those are invisible,
// text_layer, tiny, off_page, transparent and invisible_chars; the last
// three reasons of the first table are checked in part and are not
// listed: they are flagged where the check finds them, and their
// absence says nothing. With DeepHidden all nine are listed. A page
// that paints more shapes than are followed (65,536, or fewer where
// they take much work to compare) gets pdf.deep_hidden_incomplete: its
// text is complete, what was found stands, and low_contrast and
// occluded are then left off the list for the whole document.
//
// A flag is evidence that text is hidden. No flag is not evidence that
// a document is safe. Not detected at all: a font whose glyphs draw
// nothing or something other than what its mapping says; text inside
// images; text hidden by a soft mask, a blend mode or a pattern; text
// that is partly visible; text covered by other text, by a shape that
// is no upright rectangle, or by the appearance of an annotation. The
// document information, the outline and the names of attached files
// are never displayed on a page and are extracted by design; a consumer
// that feeds them to a model should treat them as it treats any text
// from the file.
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
//	pdf.deep_hidden_incomplete      Options.DeepHidden was set and the
//	                                page paints more shapes than are
//	                                followed
//
// # Errors
//
// An error means that there is no document. It wraps one of the errors
// of the shared model: ErrMalformed for input that is not a readable
// PDF, including one without pages; ErrPasswordRequired and
// ErrWrongPassword; ErrUnsupported for an encryption this package does
// not have; ErrLimitExceeded for input larger than MaxInputBytes;
// ErrNoPartsSelected. A file that can be read in part is not an error:
// a page that cannot be read is a part with part.failed, in its place.
package gofastpdf
