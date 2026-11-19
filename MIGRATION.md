# Moving from v0.1 to v0.2

v0.2 is a clean break. The package `pkg/loader` and its JSON are gone, with no shim: the library now
returns the document model that all go-fast libraries share
([go-fast-core](https://github.com/AOShei/go-fast-core)), and its public API is in the root of the
module.

## Imports and calls

| v0.1 | v0.2 |
|---|---|
| `import "github.com/AOShei/go-fast-pdf/pkg/loader"` | `import gofastpdf "github.com/AOShei/go-fast-pdf"` and `gofastcore "github.com/AOShei/go-fast-core"` |
| `loader.LoadPDF(path, true)` | `gofastpdf.ExtractFile(ctx, path, nil)`: a block for every image is the default |
| `loader.LoadPDF(path, false)` | `gofastpdf.ExtractFile(ctx, path, &gofastcore.Options{Images: gofastcore.ImagesNone})` |
| `loader.LoadPDFConcurrent(path, 8, false)` | `gofastpdf.ExtractFile(ctx, path, &gofastcore.Options{Workers: 8})`; without `Workers`, one per processor |
| Reading from memory or a reader | `gofastpdf.Extract(ctx, readerAt, size, opts)`, `gofastpdf.OpenBytes(data, opts)` |
| A page at a time (not in v0.1) | `gofastpdf.Open(path, opts)`, then `File.Parts(ctx)` or `File.Page(n)` |
| Progress lines on standard error | Nothing is printed. What goes wrong is an error or a warning in the result |
| The program in `cmd` | `cmd/gofastpdf`: `go install github.com/AOShei/go-fast-pdf/cmd/gofastpdf@latest` |

Every call takes a `context.Context` and stops when it ends.

## The document

| v0.1 (`pkg/model`) | v0.2 (`gofastcore`) |
|---|---|
| `Document.Pages[]` | `Document.Parts[]`, each of kind `page` |
| `Page.PageNumber` | `Part.Number`: the page's place in the file, also when a range is extracted. A page label such as `iv` is `Part.Name` |
| `Page.Width`, `Height` | `Part.Width`, `Height`, as the page is displayed: the crop box after rotation |
| `Page.Content` (one string) | `Part.Blocks[]`: paragraphs in reading order. `part.Text()` or `part.Markdown()` gives a string |
| `Page.CharCount` | Gone. Count the text of the blocks |
| `Page.Images[]` | Blocks of kind `image`, where the image stands among the text |
| `Image.ID` (`Im1`, the name on its page) | `Image.ID` from the object number (`obj-12`), the same on every page that shows the image; `inline-<page>-<n>` for an image inside a content stream |
| `Image.Width`, `Height` | The same, as `int` |
| `Image.Rect` `[x, y, w, h]`, origin at the bottom left | `Block.Loc.BBox` `{x0, y0, x1, y1}`, origin at the top left of the displayed page |
| `Image.Type`, `ColorSpace` | `Block.Attrs["inline"]`, `Block.Attrs["color_space"]` |
| `Metadata.Title`, `Encrypted` | The same |
| `Metadata.Author` | `Metadata.Authors`, one entry, not split |
| Keywords | `Metadata.Keywords`, one entry, not split |
| `Metadata.Creator` | `Metadata.Application` |
| `Metadata.Producer` | `Metadata.Extra["producer"]` |
| Bookmarks (not extracted) | `Document.Outline` |
| Attached files (not extracted) | `Document.Children`, through `Options.Child` |
| Errors as text | Errors that wrap those of `gofastcore`: `ErrMalformed`, `ErrPasswordRequired`, `ErrWrongPassword`, `ErrUnsupported`, `ErrLimitExceeded`, `ErrNoPartsSelected`. Test with `errors.Is` |
| A page that fails is named on standard error and left out | The page is a part with the warning `part.failed`, in its place |

## The text is not the same

v0.1 gave a page as one string, in the order the page drew its text. v0.2 gives paragraphs, in the
order they are read. Anything that compares output with stored output of v0.1 will differ. What
changed, beyond that:

- Lines of a paragraph are joined with a space; a hyphen at the end of a line stays and the next line
  follows without a space.
- Ligatures are written as their letters (`ﬁ` is `fi`).
- A character the file gives no meaning for is U+FFFD and is reported (`text.unmapped_glyphs`). The text
  is always valid UTF-8.
- Text that cannot be seen is flagged in `Block.Hidden`. It is still in the output: pass
  `RenderOptions.ExcludeHidden` to leave it out of rendered text.
- A tab stands between two pieces of a line that are far apart (a label and its value, the cells of a
  row).

## JSON

The JSON form is that of the shared model, `schema_version` `"0.1"`; the README shows an example. A
decoder for it is `encoding/json` into `gofastcore.Document`.

## Minimum Go version

Go 1.25, as go-fast-core.
