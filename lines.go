package gofastpdf

import (
	gofastcore "github.com/AOShei/go-fast-core"
)

// Line is one line of text on a page, with more detail than a block
// has: where the line is, the font it is set in, and where each of its
// words is.
type Line struct {
	// Text is the text of the line by itself. The text of a block joins
	// its lines by the conventions of the shared model, which this does
	// not: a hyphen at the end of the line is there, and nothing of the
	// next line.
	Text string
	// BBox is the box of the line on its page, in the space of the
	// part: points, origin at the top left.
	BBox gofastcore.Rect
	// Block is the index in Part.Blocks of the paragraph the line is in,
	// for the part that Page.Part returns for the same page.
	Block int
	// Display is what the file declares about how the line is shown:
	// that of its block.
	Display gofastcore.Display
	// Font is the name of the font that most of the line is set in,
	// without the tag of a subset, and Size the height it is displayed
	// at, in points.
	Font string
	Size float64
	// Words are the stretches of the line between its spaces and tabs,
	// in the order of the line.
	Words []Word
}

// Word is one word of a line: what stands between two spaces.
type Word struct {
	// Text is the word as it stands in the text of its line.
	Text string
	// BBox is the box of the word on its page. It runs from where the
	// word begins on the line to where it ends, as wide as its glyphs
	// are said to be by their font, and as high as the line.
	BBox gofastcore.Rect
}

// Lines reads the page and returns its lines of text, block by block in
// the order of the blocks of its part, the lines of a block from its
// first to its last. Images have no lines.
//
// A page that cannot be read has no lines; its part says why. The error
// is ErrClosed after Close.
func (p *Page) Lines() ([]Line, error) {
	out, err := p.read(want{lines: true})
	if err != nil {
		return nil, err
	}
	lines := make([]Line, len(out.lines))
	for i := range out.lines {
		l := &out.lines[i]
		lines[i] = Line{Text: l.Text, BBox: l.BBox, Block: l.Block, Font: l.Font, Size: l.Size}
		if l.Block >= 0 && l.Block < len(out.part.Blocks) {
			lines[i].Display = out.part.Blocks[l.Block].Display
		}
		lines[i].Words = make([]Word, len(l.Words))
		for k, w := range l.Words {
			lines[i].Words[k] = Word{Text: w.Text, BBox: w.BBox}
		}
	}
	return lines, nil
}
