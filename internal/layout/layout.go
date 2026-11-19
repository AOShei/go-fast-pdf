// Package layout turns the text runs of a page into blocks in reading
// order.
//
// The interpreter (internal/extract) says what a page draws and where.
// This package decides what a reader makes of it:
//
//   - which runs a reader cannot see, and why (hidden.go);
//   - which runs form a line, and where its spaces are (lines.go);
//   - which lines form a paragraph (para.go);
//   - in which order the paragraphs are read (order.go).
//
// Everything is worked out in displayed space: the visible box of the
// page after its rotation, in points, origin at the top left, y growing
// downwards. That is the space of Block.Loc.BBox.
//
// Runs with different sets of hidden reasons never meet. The visible
// runs are laid out by themselves, so the visible blocks are what they
// would be if the hidden text were not in the file; each other set is
// laid out by itself and its blocks are put among the visible ones by
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
	// Images puts an image block where each image is.
	Images bool
	// NoHiddenChecks treats all text as visible. It is for measuring
	// what the checks cost.
	NoHiddenChecks bool
	// Deep makes the checks that need the shapes of the page, which the
	// content must have been read with (extract.Options.Shapes).
	Deep bool
	// MaxDeepSteps bounds the work of those checks on the page; 0 means
	// the default.
	MaxDeepSteps int
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
	// HasText reports whether there is a text block, hidden or not.
	HasText bool
	// Controls is the number of control characters that were replaced.
	Controls int
	// RightToLeft reports that some text is in a script written from
	// right to left.
	RightToLeft bool
	// DeepIncomplete is set when Options.Deep was set and the page has
	// more shapes than are followed: low contrast and covering shapes
	// were then not checked for all of its text.
	DeepIncomplete bool
	Stats          Stats
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
	// Unjudged is the number of runs whose contrast is not judged because
	// what they stand on is not known: by default the visible runs drawn
	// after something else was painted on the page, and with
	// Options.Deep those under which the last thing painted is not one
	// rectangle of one colour that holds them. Judged is the number of
	// runs whose contrast is judged.
	Unjudged, Judged int
}

// builder holds the working state of one page. It is reused.
type builder struct {
	page  Page
	c     *extract.Content
	opts  Options
	scale float64 // points per unit of the page's coordinates

	pieces        []piece
	order         []int32 // piece indices, grouped
	lines         []line
	lidx          []int32 // backing store of the lines' piece lists
	text          []byte  // the lines' texts
	cands         []cand
	frags         []frag
	blocks        []blk
	bidx          []int32 // backing store of the blocks' line lists
	sorted        []int32
	sortB         []int32
	pred          []int32
	claims        []int32
	hist          []int32
	alt           []int32
	occ           []occluder
	big           []box
	patches       []patch
	grid, regions grid
	out           []byte
	stats         Stats

	controls   int
	rtl        bool
	incomplete bool
}

var builders = sync.Pool{New: func() any { return new(builder) }}

// Build lays out one page.
func Build(page Page, c *extract.Content, opts Options) Result {
	b := builders.Get().(*builder)
	defer func() {
		b.c = nil
		b.trim()
		builders.Put(b)
	}()
	b.reset(page, c, opts)
	b.prepare()
	b.group()

	// Each group is laid out by itself.
	hiddenFrom := -1
	for lo := 0; lo < len(b.order); {
		hi := lo + 1
		k := b.pieces[b.order[lo]].hidden
		for hi < len(b.order) && b.pieces[b.order[hi]].hidden == k {
			hi++
		}
		first := len(b.blocks)
		b.layoutGroup(b.order[lo:hi])
		if b.pieces[b.order[lo]].hidden != 0 && hiddenFrom < 0 {
			hiddenFrom = first
		}
		lo = hi
	}
	if hiddenFrom < 0 {
		hiddenFrom = len(b.blocks)
	}

	// The visible blocks in reading order.
	visible := make([]int32, hiddenFrom)
	for i := range visible {
		visible[i] = int32(i)
	}
	b.readingOrder(visible)

	// The other blocks go among them by position: each set in its own
	// reading order, and what is off the page at the end.
	final := visible
	var offPage []int32
	for lo := hiddenFrom; lo < len(b.blocks); {
		hi := lo + 1
		for hi < len(b.blocks) && b.blocks[hi].hidden == b.blocks[lo].hidden {
			hi++
		}
		set := make([]int32, hi-lo)
		for i := range set {
			set[i] = int32(lo + i)
		}
		b.readingOrder(set)
		if b.blocks[lo].hidden&gofastcore.HiddenOffPage != 0 {
			offPage = append(offPage, set...)
		} else {
			final = b.insertByPosition(final, set)
		}
		lo = hi
	}
	final = append(final, offPage...)

	res := Result{Controls: b.controls, RightToLeft: b.rtl, DeepIncomplete: b.incomplete, Stats: b.stats}
	res.Blocks = make([]gofastcore.Block, 0, len(final)+len(c.Images)*btoi(opts.Images))
	for _, i := range final {
		bl := &b.blocks[i]
		res.Blocks = append(res.Blocks, gofastcore.Block{
			Kind:   gofastcore.KindParagraph,
			Text:   bl.text,
			Origin: gofastcore.OriginInferred,
			Hidden: bl.hidden,
			Loc:    gofastcore.Location{BBox: rect(bl.x0, bl.y0, bl.x1, bl.y1)},
		})
	}
	res.HasText = len(res.Blocks) > 0
	res.Stats = b.stats
	if opts.Images && len(c.Images) > 0 {
		b.placeImages(&res)
	}
	return res
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
	m := page.M
	b.scale = hypot(m[0], m[1])
	if b.scale == 0 {
		b.scale = 1
	}
	b.pieces, b.order, b.lines, b.lidx = b.pieces[:0], b.order[:0], b.lines[:0], b.lidx[:0]
	b.text, b.cands, b.frags, b.blocks, b.bidx = b.text[:0], b.cands[:0], b.frags[:0], b.blocks[:0], b.bidx[:0]
	b.occ, b.big = b.occ[:0], b.big[:0]
	b.stats = Stats{Runs: len(c.Runs)}
	b.controls, b.rtl, b.incomplete = 0, false, false
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
	if cap(b.patches) > keep {
		b.patches = nil
	}
	if cap(b.grid.nodes) > keep {
		b.grid.nodes = nil
	}
	if cap(b.regions.nodes) > keep {
		b.regions.nodes = nil
	}
	// The blocks hold strings: they are not kept alive by the pool.
	clear(b.blocks[:cap(b.blocks)])
	if cap(b.blocks) > keep {
		b.blocks, b.bidx = nil, nil
	}
}

// piece is one run, placed in displayed space.
type piece struct {
	run    int32
	hidden gofastcore.Hidden
	class  uint8 // direction of writing, in steps of five degrees
	dup    bool  // the same text is printed here by an earlier run
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

// group puts the pieces in the order of their groups: by set of hidden
// reasons, the visible ones first. Inside a group they stay in the
// order drawn.
func (b *builder) group() {
	mixed := false
	for i := range b.pieces {
		if b.pieces[i].dup {
			continue
		}
		b.order = append(b.order, int32(i))
		mixed = mixed || b.pieces[i].hidden != 0
	}
	if !mixed {
		return
	}
	sortStable(b.order, func(i, j int32) bool {
		return b.pieces[i].hidden < b.pieces[j].hidden
	})
}
