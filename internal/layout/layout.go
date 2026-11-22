// Package layout turns the text runs of a page into blocks in reading
// order.
//
// The interpreter (internal/extract) says what a page draws and where.
// This package decides what a reader makes of it:
//
//   - what the file declares about how each run is shown (display.go);
//   - which runs form a line, and where its spaces are (lines.go);
//   - which lines form a paragraph (para.go);
//   - in which order the paragraphs are read (order.go).
//
// Everything is worked out in displayed space: the visible box of the
// page after its rotation, in points, origin at the top left, y growing
// downwards. That is the space of Block.Loc.BBox.
//
// Runs with different display reasons never meet. The runs without
// one are laid out by themselves, so their blocks are what they would
// be if the text the file declares not shown were not in it; each other
// set is laid out by itself and its blocks are put among the others by
// position.
//
// The order in which a file draws its text is evidence of the order in
// which it is read, and usually the best there is: most programs write
// a page as it is read. It is kept wherever the geometry of the page
// does not speak against it, and it decides between readings that the
// geometry leaves open, such as a table read by rows against two
// columns read one after the other.
package layout

import (
	"sync"

	gofastcore "github.com/AOShei/go-fast-core"
	"github.com/AOShei/go-fast-pdf/internal/extract"
)

// Page is the geometry of the page being laid out.
type Page struct {
	// M takes the page's coordinates to displayed space.
	M [6]float64
	// Width and Height are the displayed size in points.
	Width, Height float64
}

// Options says what to produce besides text blocks.
type Options struct {
	// Worker, if set, keeps the builder for the next page of the same
	// goroutine (Worker).
	Worker *Worker
	// Images puts an image block where each image is.
	Images bool
	// Lines returns the lines of the text blocks with their words, in
	// Result.Lines. The words are placed exactly when the content was
	// read with extract.Options.Words.
	Lines bool
	// Spans gives the text blocks spans of bold and italic, and of the
	// links of the page, Links.
	Spans bool
	Links []Link
	// Heading, when set, gives the level of the heading of a tagged PDF
	// that the content under a marked-content identifier is part of, 0
	// for none (see Style.Heading).
	Heading func(mcid int32) uint8
	// Cell, when set, gives the cell of a table of a tagged PDF that the
	// content under a marked-content identifier is in: an index into
	// CellTable, -1 for none. CellTable gives the table of each cell, an
	// index from 0 to Tables-1. The text of each cell, without a display
	// reason, is laid out
	// by itself, and each table that has some is one block of kind table,
	// with its cells' text in Result.CellText (tables.go).
	Cell      func(mcid int32) int32
	CellTable []int32
	Tables    int
	// Item, when set, gives the list item of a tagged PDF that the
	// content under a marked-content identifier is in, 0 for none, and
	// whether it is the item's label. A paragraph never joins lines of two
	// items, or of an item and content outside one (see Style.Item).
	Item func(mcid int32) (item int64, label bool)
	// Rank, when set, gives the place of the content under a
	// marked-content identifier in the order of the structure tree of a
	// tagged PDF, from 1; 0 for none. The blocks are then ordered with
	// the tree as the evidence that the order of drawing is otherwise
	// (order.go).
	Rank func(mcid int32) int32
	// NoTables leaves out the tables found by how their text lines up
	// (aligned.go).
	NoTables bool
}

// Result is the page as blocks.
type Result struct {
	// Blocks are in reading order. Text blocks are paragraphs with
	// OriginInferred; image blocks have Image set to a description
	// without an ID or data, and Images gives the image each stands for.
	Blocks []gofastcore.Block
	// Images has one entry per block: the index in Content.Images of the
	// image an image block shows, -1 for a text block. It is nil when
	// there is no image block.
	Images []int32
	// HasText reports whether there is a text block, with a display
	// reason or not.
	HasText bool
	// Controls is the number of control characters that were replaced.
	Controls int
	// Lines are the lines of the text blocks, block by block, when
	// Options.Lines is set.
	Lines []Line
	// Styles has one entry per block: how its text is set. An image
	// block has the zero Style.
	Styles []Style
	Stats  Stats
	// CellText is the text of each cell of Options.Cell, "" for a cell
	// with none, and Tables the table of each block of kind table, in
	// the order of the blocks.
	CellText []string
	Tables   []int32
	// CellBox is the box of the text of each cell, and CellSize the size
	// of its largest text; zero for a cell without text.
	CellBox  []gofastcore.Rect
	CellSize []float64
}

// Style is how the text of a block is set: by its line with the most
// text.
type Style struct {
	// Size is the size the text is displayed at.
	Size float64
	// Bold is set when the font of that line is bold.
	Bold bool
	// Lines is the number of lines of the block.
	Lines int
	// Heading is the level that Options.Heading gives every run of the
	// block, 0 when it gives none or not the same to all.
	Heading uint8
	// Marked is one more than the marked-content identifier (MCID) of
	// the first run of the block, on a page that Options.Heading is set
	// for; 0 for none.
	Marked int32
	// Item is the list item that Options.Item gives every run of a
	// paragraph, 0 when it gives none or not the same to all; Label is
	// then the text of the runs at its start that are the item's label.
	Item  int64
	Label string
}

// Line is one line of a text block.
type Line struct {
	// Block is the index in Result.Blocks of the block the line is in.
	Block int
	// Text is the text of the line by itself.
	Text string
	BBox gofastcore.Rect
	// Font is the name of the font that most of the line is set in,
	// without the tag of a subset, and Size the size it is displayed at.
	Font  string
	Size  float64
	Words []Word
}

// Word is a stretch of a line between two spaces.
type Word struct {
	Text string
	BBox gofastcore.Rect
	// Style is bold and italic as the font of the word's first letter
	// is.
	Style gofastcore.Style
}

// Stats counts what the layout did. It is for measurement.
type Stats struct {
	Runs, Lines int
	// Duplicates is the number of runs left out because the same text
	// is printed in the same place by another run.
	Duplicates int
	// Rejoined is the number of lines put together from pieces that the
	// page draws at different times.
	Rejoined int
	// Tabs is the number of wide gaps written as a tab, and LetterSpaced
	// the number of lines whose letters stood apart.
	Tabs, LetterSpaced int
	// Reordered reports that the blocks are not in the order drawn.
	Reordered bool
}

// builder holds the working state of one page. It is reused.
type builder struct {
	page  Page
	c     *extract.Content
	opts  Options
	scale float64 // points per unit of the page's coordinates

	pieces []piece
	order  []int32 // piece indices, grouped
	lines  []line
	lidx   []int32 // backing store of the lines' piece lists
	text   []byte  // the lines' texts
	cands  []cand
	frags  []frag
	blocks []blk
	bidx   []int32 // backing store of the blocks' line lists
	sorted []int32
	sortB  []int32
	pred   []int32
	claims []int32
	hist   []int32
	alt    []int32
	out    []byte
	stats  Stats
	// The words of a block and their atoms, for its spans.
	atomBuf   []atom
	placedBuf []placed
	// The grids of the page's rules, found once (ruled.go).
	grids     []grid
	gridsDone bool
	// What the search for tables by alignment works in (aligned.go).
	tOrder []int32
	tV     []float64
	tRows  []trow
	tSegs  []seg
	tGrid  []tgrow
	tCells []tcell
	tWords []int
	// The order of the structure tree (order.go): the place of each
	// block, and the keys of the order drawn that are put back.
	tPlaces, tDrawn [][2]int32
	// The units of a line written from right to left (bidi.go), and
	// whether its brackets are mirrored.
	units      []unit
	segs       []segment
	mirrorLine bool

	controls int
}

var builders = sync.Pool{New: func() any { return new(builder) }}

// A Worker keeps, for one goroutine that lays out the pages of a
// document, the builder that one page leaves for the next to reuse, as
// extract.Worker keeps the state of the reading. The zero Worker is
// ready; Release gives the builder to the pool when the document is laid
// out. A Worker is not for two goroutines at once.
type Worker struct{ b *builder }

// Release gives the builder the Worker holds to the pool.
func (w *Worker) Release() {
	if w.b != nil {
		builders.Put(w.b)
		w.b = nil
	}
}

// Build lays out one page.
func Build(page Page, c *extract.Content, opts Options) Result {
	worker := opts.Worker
	var b *builder
	if worker != nil && worker.b != nil {
		b, worker.b = worker.b, nil
	} else {
		b = builders.Get().(*builder)
	}
	defer func() {
		// Nothing of the document is kept by a builder that waits: the
		// options hold functions over the document's structure tree, and
		// through it all of the document.
		b.c, b.opts, b.page = nil, Options{}, Page{}
		b.trim()
		if worker != nil {
			worker.b = b
		} else {
			builders.Put(b)
		}
	}()
	b.reset(page, c, opts)
	b.cutRuns()
	b.prepare()
	b.group()

	// Each group is laid out by itself: the text without a reason, that of
	// each cell of a table, and the text with each set of display reasons.
	hiddenFrom, cellsFrom := -1, -1
	for lo := 0; lo < len(b.order); {
		hi := lo + 1
		at := &b.pieces[b.order[lo]]
		for hi < len(b.order) && b.pieces[b.order[hi]].display == at.display && b.pieces[b.order[hi]].cell == at.cell {
			hi++
		}
		first := len(b.blocks)
		b.layoutGroup(b.order[lo:hi])
		switch {
		case at.display != 0:
			if hiddenFrom < 0 {
				hiddenFrom = first
			}
		case at.cell != 0:
			if cellsFrom < 0 {
				cellsFrom = first
			}
			for i := first; i < len(b.blocks); i++ {
				b.blocks[i].cell = at.cell
			}
		}
		lo = hi
	}
	if hiddenFrom < 0 {
		hiddenFrom = len(b.blocks)
	}
	if cellsFrom < 0 {
		cellsFrom = hiddenFrom
	}
	hiddenTo := len(b.blocks)

	// The visible blocks in reading order, and the tables among them.
	visible := make([]int32, cellsFrom)
	for i := range visible {
		visible[i] = int32(i)
	}
	b.readingOrder(visible)
	var cellText []string
	var cellBox []gofastcore.Rect
	var cellSize []float64
	if cellsFrom < hiddenFrom {
		var set []int32
		cellText, cellBox, cellSize, set = b.tables(cellsFrom, hiddenFrom)
		visible = b.insertByPosition(visible, set)
	}

	// The other blocks go among them by position: each set in its own
	// reading order, and what is off the page at the end.
	final := visible
	for lo := hiddenFrom; lo < hiddenTo; {
		hi := lo + 1
		for hi < hiddenTo && b.blocks[hi].display == b.blocks[lo].display {
			hi++
		}
		set := make([]int32, hi-lo)
		for i := range set {
			set[i] = int32(lo + i)
		}
		b.readingOrder(set)
		final = b.insertByPosition(final, set)
		lo = hi
	}

	res := Result{Controls: b.controls, Stats: b.stats, CellText: cellText, CellBox: cellBox, CellSize: cellSize}
	res.Blocks = make([]gofastcore.Block, 0, len(final)+len(c.Images)*btoi(opts.Images))
	for _, i := range final {
		bl := &b.blocks[i]
		kind := gofastcore.KindParagraph
		switch {
		case bl.table > 0:
			kind = gofastcore.KindTable
			res.Tables = append(res.Tables, bl.table-1)
		case bl.grid != nil:
			kind = gofastcore.KindTable
		}
		res.Blocks = append(res.Blocks, gofastcore.Block{
			Kind:    kind,
			Text:    bl.text,
			Table:   bl.grid,
			Origin:  gofastcore.OriginInferred,
			Display: bl.display,
			Loc:     gofastcore.Location{BBox: rect(bl.x0, bl.y0, bl.x1, bl.y1)},
		})
	}
	res.HasText = len(res.Blocks) > 0
	res.Stats = b.stats
	res.Styles = make([]Style, len(final), cap(res.Blocks))
	for k, i := range final {
		res.Styles[k] = b.styleOf(i)
	}
	if opts.Lines {
		b.linesOf(&res, final)
	}
	if opts.Spans {
		b.spansOf(&res, final)
	}
	if opts.Images && len(c.Images) > 0 {
		b.placeImages(&res)
		// The image blocks have moved the text blocks along.
		at := make([]int, 0, len(final))
		for k, img := range res.Images {
			if img < 0 {
				at = append(at, k)
			}
		}
		for i := range res.Lines {
			res.Lines[i].Block = at[res.Lines[i].Block]
		}
		styles := res.Styles[:len(res.Blocks)]
		for k := len(at) - 1; k >= 0; k-- {
			styles[at[k]] = res.Styles[k]
		}
		for k, img := range res.Images {
			if img >= 0 {
				styles[k] = Style{}
			}
		}
		res.Styles = styles
	}
	return res
}

// styleOf is the style of a block: that of its line with the most text.
func (b *builder) styleOf(i int32) Style {
	var s Style
	most := int32(-1)
	for li := b.blocks[i].line; li >= 0; li = b.lines[li].next {
		ln := &b.lines[li]
		s.Lines++
		if n := ln.thi - ln.tlo; n > most {
			most = n
			s.Size = ln.size
			s.Bold = ln.main != nil && ln.main.Bold
		}
	}
	if h := b.opts.Heading; h != nil {
		if li := b.blocks[i].line; li >= 0 && b.lines[li].hi > b.lines[li].lo {
			s.Marked = b.c.Runs[b.pieces[b.lidx[b.lines[li].lo]].run].MCID + 1
		}
		level, all := uint8(0), true
		for li := b.blocks[i].line; li >= 0 && all; li = b.lines[li].next {
			ln := &b.lines[li]
			for k := ln.lo; k < ln.hi; k++ {
				l := h(b.c.Runs[b.pieces[b.lidx[k]].run].MCID)
				if l == 0 || level != 0 && l != level {
					all = false
					break
				}
				level = l
			}
		}
		if all {
			s.Heading = level
		}
	}
	if b.opts.Item != nil {
		s.Item, s.Label = b.itemOf(i)
	}
	return s
}

// itemOf returns the list item that every run of paragraph i is in, 0
// for none, and the text of the runs at its start that are the item's
// label.
func (b *builder) itemOf(i int32) (item int64, label string) {
	bl := &b.blocks[i]
	if bl.line < 0 || bl.grid != nil || bl.table > 0 {
		return 0, ""
	}
	var lbl []byte
	leading := true
	for li := bl.line; li >= 0; li = b.lines[li].next {
		ln := &b.lines[li]
		for k := ln.lo; k < ln.hi; k++ {
			run := b.pieces[b.lidx[k]].run
			it, isLabel := b.opts.Item(b.c.Runs[run].MCID)
			if it == 0 || item != 0 && it != item {
				return 0, ""
			}
			item = it
			if leading = leading && isLabel; leading && len(lbl) < 256 {
				lbl = append(lbl, b.runText(run)...)
			}
		}
	}
	return item, string(lbl)
}

// farAway bounds the coordinates of a box. A page can place text a
// trillion points from its corner, or nowhere at all; a box that far
// away says nothing more than that it is off the page, and numbers
// without bound do not survive every way of writing a document down.
const farAway = 1e9

// rect makes the box of a block from its corners.
func rect(x0, y0, x1, y1 float64) gofastcore.Rect {
	clamp := func(v float64) float64 {
		switch {
		case v != v: // not a number
			return 0
		case v > farAway:
			return farAway
		case v < -farAway:
			return -farAway
		}
		return v
	}
	r := gofastcore.Rect{X0: clamp(x0), Y0: clamp(y0), X1: clamp(x1), Y1: clamp(y1)}
	r.X1, r.Y1 = max(r.X0, r.X1), max(r.Y0, r.Y1)
	return r
}

func btoi(v bool) int {
	if v {
		return 1
	}
	return 0
}

func (b *builder) reset(page Page, c *extract.Content, opts Options) {
	b.page, b.c, b.opts = page, c, opts
	b.grids, b.gridsDone = nil, false
	m := page.M
	b.scale = hypot(m[0], m[1])
	if b.scale == 0 {
		b.scale = 1
	}
	b.pieces, b.order, b.lines, b.lidx = b.pieces[:0], b.order[:0], b.lines[:0], b.lidx[:0]
	b.text, b.cands, b.frags, b.blocks, b.bidx = b.text[:0], b.cands[:0], b.frags[:0], b.blocks[:0], b.bidx[:0]
	b.stats = Stats{Runs: len(c.Runs)}
	b.controls = 0
}

// trim drops what one unusually large page grew.
func (b *builder) trim() {
	const keep = 1 << 16
	if cap(b.pieces) > keep {
		b.pieces = nil
	}
	if cap(b.order) > keep {
		b.order, b.sorted, b.sortB, b.pred, b.claims = nil, nil, nil, nil, nil
	}
	if cap(b.lines) > keep {
		b.lines, b.lidx = nil, nil
	}
	if cap(b.text) > 4<<20 {
		b.text, b.out = nil, nil
	}
	if cap(b.cands) > keep {
		b.cands, b.frags = nil, nil
	}
	// The words of spans hold the targets of links, and the blocks their
	// text: they are not kept alive by the pool.
	clear(b.placedBuf[:cap(b.placedBuf)])
	if cap(b.atomBuf) > keep || cap(b.placedBuf) > keep {
		b.atomBuf, b.placedBuf = nil, nil
	}
	// The cells hold their text.
	clear(b.tCells[:cap(b.tCells)])
	if cap(b.tSegs) > keep || cap(b.tCells) > keep {
		b.tOrder, b.tV, b.tRows, b.tSegs, b.tGrid, b.tCells, b.tWords = nil, nil, nil, nil, nil, nil, nil
	}
	clear(b.blocks[:cap(b.blocks)])
	if cap(b.blocks) > keep {
		b.blocks, b.bidx = nil, nil
	}
}

// piece is one run, placed in displayed space.
type piece struct {
	run     int32
	display gofastcore.Display
	cell    int32 // one more than the cell of a table it is in, 0 for none
	class   uint8 // direction of writing, in steps of five degrees
	dup     bool  // the same text is printed here by an earlier run
	// apart is set when the piece and the one before it on its line are
	// not drawn one after the other: the line was put together.
	apart bool
	// later is set when something painted after the piece touches it in
	// a way that is not followed, so that what a reader sees of the
	// piece is not known.
	later bool

	// What separates the piece from the one before it on its line, and
	// how far apart they are along the line.
	sep byte
	gap float32

	x0, y0, x1, y1 float64 // the box
	sx, sy, ex, ey float64 // where the baseline starts and ends
	dx, dy         float64 // the direction of writing
	nx, ny         float64 // across the line, towards the top of the glyphs
	// How far the glyphs reach from the baseline along (nx, ny): up to
	// their top and down to their bottom, the rise included.
	up, down float64
	// In the frame of the piece's group: along the line from u0 to u1,
	// the baseline at v, the glyphs from top to bot across it.
	u0, u1, v, top, bot float64
	size, em            float64
}

func (b *builder) runText(run int32) []byte {
	r := &b.c.Runs[run]
	return b.c.Text[r.Off : r.Off+r.Len]
}

// group puts the pieces in the order of their groups: by display
// reasons, those without one first. Inside a group they stay in the
// order drawn.
func (b *builder) group() {
	mixed := false
	for i := range b.pieces {
		if b.pieces[i].dup {
			continue
		}
		b.order = append(b.order, int32(i))
		mixed = mixed || b.pieces[i].display != 0 || b.pieces[i].cell != 0
	}
	if !mixed {
		return
	}
	// The text without a display reason, then that of each cell, then
	// the text with each set of reasons.
	sortStable(b.order, func(i, j int32) bool {
		p, q := &b.pieces[i], &b.pieces[j]
		if p.display != q.display {
			return p.display < q.display
		}
		return p.cell < q.cell
	})
}
