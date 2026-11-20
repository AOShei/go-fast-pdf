package layout

import (
	"cmp"
	"slices"
	"strings"

	gofastcore "github.com/AOShei/go-fast-core"
)

// Limits of the ordering step. Beyond them a page keeps the order it is
// drawn in.
const (
	maxOrdered  = 2000
	maxCutDepth = 200
	// maxPlacing bounds the comparisons made to place blocks with a
	// display reason, and images, among the others.
	maxPlacing = 4 << 20
	// columnGap is the least white space, in points, between two blocks
	// that stand side by side.
	columnGap = 1.0
)

// sortStable sorts s by less, keeping the order of what is equal. It
// allocates nothing, which the sorts of package sort do.
func sortStable(s []int32, less func(a, b int32) bool) {
	slices.SortStableFunc(s, func(a, b int32) int {
		switch {
		case less(a, b):
			return -1
		case less(b, a):
			return 1
		}
		return 0
	})
}

// cutBox is the box of a block for the purpose of finding white space
// between blocks: a little less than the full height of its lines, so
// that tightly set blocks do not touch.
func (b *builder) cutBox(i int32) box {
	bl := &b.blocks[i]
	y0, y1 := bl.y0+0.15*bl.size, bl.y1-0.10*bl.size
	if y1 <= y0 {
		y0, y1 = bl.y0, bl.y1
	}
	return box{bl.x0, y0, bl.x1, y1}
}

// readingOrder puts the blocks idx in the order they are read.
//
// The page is cut along the white space between its blocks, across and
// down, again and again (a recursive XY cut): the parts are read from
// top to bottom and from left to right. Where the page can be cut in
// more than one way, the cut is taken that the order of drawing agrees
// with best. A page drawn column by column is read by columns; a table
// drawn row by row is read by rows. Blocks that no white space
// separates stay in the order they are drawn.
func (b *builder) readingOrder(idx []int32) {
	if len(idx) < 2 {
		return
	}
	sortStable(idx, func(i, j int32) bool { return b.blocks[i].key < b.blocks[j].key })
	if len(idx) > maxOrdered {
		return
	}
	// Most pages are drawn from top to bottom in one column: each block
	// under the one before it. Such a page is read so, whatever the
	// structure tree of a tagged PDF says.
	inOrder := true
	for k := 1; k < len(idx); k++ {
		if b.cutBox(idx[k]).y0 < b.cutBox(idx[k-1]).y1 {
			inOrder = false
			break
		}
	}
	if inOrder {
		return
	}
	if b.opts.Rank != nil {
		if drawn := b.treeKeys(idx); drawn != nil {
			defer func() {
				for _, d := range drawn {
					b.blocks[d[0]].key = d[1]
				}
			}()
			sortStable(idx, func(i, j int32) bool { return b.blocks[i].key < b.blocks[j].key })
		}
	}
	b.cut(idx, 0)
	if b.blocks[idx[0]].display == 0 {
		for k := 1; k < len(idx); k++ {
			if b.blocks[idx[k]].key < b.blocks[idx[k-1]].key {
				b.stats.Reordered = true
				break
			}
		}
	}
}

// treeKeys gives the blocks idx, which are in the order drawn, keys in
// the order of the structure tree of a tagged PDF (Options.Rank): each
// block by the first of its content in that order, and a block that
// the tree does not place right after the block drawn before it. It
// returns each block with its key of the order drawn, which the caller
// puts back once the blocks are ordered; nil when the tree places none
// of them, and the keys are unchanged.
//
// The order of the tree is the evidence of a tagged PDF in place of the
// order of drawing: where white space separates blocks, they are read
// by it all the same, so that the tree can say which way a page is cut
// but not that a text box set at the top of a page, which a word
// processor puts in the tree where it is anchored, is read further
// down.
func (b *builder) treeKeys(idx []int32) [][2]int32 {
	// places[k] is the place in the tree that block idx[k] is read at,
	// and k after it: the order drawn among blocks at one place.
	places := b.tPlaces[:0]
	cur, any := int32(0), false
	for k, i := range idx {
		r := b.blockRank(i)
		if r > 0 {
			cur, any = r, true
		}
		places = append(places, [2]int32{cur, int32(k)})
	}
	b.tPlaces = places
	if !any {
		return nil
	}
	slices.SortStableFunc(places, func(x, y [2]int32) int { return cmp.Compare(x[0], y[0]) })
	drawn := b.tDrawn[:0]
	for _, i := range idx {
		drawn = append(drawn, [2]int32{i, b.blocks[i].key})
	}
	b.tDrawn = drawn
	for n, p := range places {
		b.blocks[idx[p[1]]].key = int32(n)
	}
	return drawn
}

// blockRank is the first place in the order of the structure tree of
// the content of block i, 0 for none.
func (b *builder) blockRank(i int32) int32 {
	best := int32(0)
	b.tOrder = b.blockLines(b.tOrder[:0], i)
	for _, li := range b.tOrder {
		ln := &b.lines[li]
		for k := ln.lo; k < ln.hi; k++ {
			if r := b.opts.Rank(b.c.Runs[b.pieces[b.lidx[k]].run].MCID); r > 0 && (best == 0 || r < best) {
				best = r
			}
		}
	}
	return best
}

// cut orders idx, which is in the order drawn.
func (b *builder) cut(idx []int32, depth int) {
	n := len(idx)
	if n < 2 || depth > maxCutDepth {
		return
	}
	boxes := make([]box, n)
	for k, i := range idx {
		boxes[k] = b.cutBox(i)
	}
	// For each axis: the blocks in order along it, the places where
	// white space separates them, and for each such place how often the
	// order of drawing crosses it.
	type axis struct {
		ord  []int32 // positions in idx, sorted along the axis
		at   []int   // a cut lies before ord[at[k]]
		gap  []float64
		alt  []int32
		best int // index into at, -1 if there is no cut
	}
	var ax [2]axis
	rank := make([]int32, n)
	diff := make([]int32, n+2)
	for a := range ax {
		lo := func(k int32) float64 {
			if a == 0 {
				return boxes[k].x0
			}
			return boxes[k].y0
		}
		hi := func(k int32) float64 {
			if a == 0 {
				return boxes[k].x1
			}
			return boxes[k].y1
		}
		least := 0.0
		if a == 0 {
			least = columnGap
		}
		x := &ax[a]
		x.best = -1
		x.ord = make([]int32, n)
		for k := range x.ord {
			x.ord[k] = int32(k)
		}
		sortStable(x.ord, func(i, j int32) bool { return lo(i) < lo(j) })
		reach := hi(x.ord[0])
		for s := 1; s < n; s++ {
			if g := lo(x.ord[s]) - reach; g > least {
				x.at = append(x.at, s)
				x.gap = append(x.gap, g)
			}
			reach = max(reach, hi(x.ord[s]))
		}
		if len(x.at) == 0 {
			continue
		}
		// Two blocks drawn one after the other lie on different sides
		// of a cut at s when s is between their ranks.
		for s, k := range x.ord {
			rank[k] = int32(s)
		}
		clear(diff)
		for k := 1; k < n; k++ {
			r1, r2 := rank[k-1], rank[k]
			if r1 > r2 {
				r1, r2 = r2, r1
			}
			diff[r1+1]++
			diff[r2+1]--
		}
		crossings := make([]int32, n+1)
		var sum int32
		for s := 1; s <= n; s++ {
			sum += diff[s]
			crossings[s] = sum
		}
		x.alt = make([]int32, len(x.at))
		for k, s := range x.at {
			x.alt[k] = crossings[s]
			if x.best < 0 || x.alt[k] < x.alt[x.best] || x.alt[k] == x.alt[x.best] && x.gap[k] > x.gap[x.best] {
				x.best = k
			}
		}
	}

	// The cut the order of drawing crosses least; of two alike, the
	// wider; of two as wide, the one across the page.
	var use *axis
	switch v, h := &ax[0], &ax[1]; {
	case v.best < 0 && h.best < 0:
		return // no white space: the order drawn
	case v.best < 0:
		use = h
	case h.best < 0:
		use = v
	case v.alt[v.best] != h.alt[h.best]:
		use = h
		if v.alt[v.best] < h.alt[h.best] {
			use = v
		}
	case v.gap[v.best] > h.gap[h.best]:
		use = v
	default:
		use = h
	}

	// Cut at every place along that axis that is crossed as little.
	// Each part keeps the order drawn and is ordered in its turn.
	part := make([]int32, n) // of each position in idx
	parts := int32(1)
	next := 0
	for s, k := range use.ord {
		for next < len(use.at) && use.at[next] < s {
			next++
		}
		if next < len(use.at) && use.at[next] == s && use.alt[next] == use.alt[use.best] {
			parts++
		}
		part[k] = parts - 1
	}
	out := make([]int32, 0, n)
	for p := int32(0); p < parts; p++ {
		from := len(out)
		for k, i := range idx {
			if part[k] == p {
				out = append(out, i)
			}
		}
		b.cut(out[from:], depth+1)
	}
	copy(idx, out)
}

// place returns where among the boxes, which are in reading order, a
// box that is not one of them belongs: after what lies above it in its
// column, or failing that before what lies under it.
func place(boxes []box, x box) int {
	cy := (x.y0 + x.y1) / 2
	above, below, anyAbove := -1, -1, -1
	for i := range boxes {
		o := &boxes[i]
		oy := (o.y0 + o.y1) / 2
		if oy < cy {
			anyAbove = i
		}
		if min(o.x1, x.x1)-max(o.x0, x.x0) <= 0 {
			continue
		}
		if oy < cy {
			above = i
		} else if below < 0 {
			below = i
		}
	}
	switch {
	case above >= 0:
		return above + 1
	case below >= 0:
		return below
	}
	return anyAbove + 1
}

// insertByPosition puts the blocks of set, which have one set of display
// reasons, among the blocks of final by where they are on the page.
func (b *builder) insertByPosition(final, set []int32) []int32 {
	if len(final)*len(set) > maxPlacing {
		return append(final, set...)
	}
	boxes := make([]box, len(final), len(final)+len(set))
	for k, i := range final {
		bl := &b.blocks[i]
		boxes[k] = box{bl.x0, bl.y0, bl.x1, bl.y1}
	}
	for _, i := range set {
		bl := &b.blocks[i]
		x := box{bl.x0, bl.y0, bl.x1, bl.y1}
		at := place(boxes, x)
		final = append(final, 0)
		copy(final[at+1:], final[at:])
		final[at] = i
		boxes = append(boxes, box{})
		copy(boxes[at+1:], boxes[at:])
		boxes[at] = x
	}
	return final
}

// placeImages puts a block for each image among the text blocks: before
// the text that is set over it, or else where it lies on the page.
// Images that belong at the same place among the text stand in the
// order of the page, from the top and from the left.
//
// Every image is placed against the text blocks alone and the blocks
// are then put together in one pass, so that a page of a hundred
// thousand images costs what a hundred thousand images cost, and not
// the square of it.
func (b *builder) placeImages(res *Result) {
	c := b.c
	n, m := len(res.Blocks), len(c.Images)
	boxes := make([]box, n)
	for k := range res.Blocks {
		r := res.Blocks[k].Loc.BBox
		boxes[k] = box{r.X0, r.Y0, r.X1, r.Y1}
	}

	// Where each image goes: before the text block at[i], or after all
	// of them.
	type placed struct {
		at     int
		cx, cy float64
		bb     box
		image  int32
	}
	images := make([]placed, m)
	byPosition := n*m <= maxPlacing
	for i := range c.Images {
		_, bb := b.quadBox(c.Images[i].Quad)
		at := n
		if byPosition {
			at = -1
			for k := range boxes {
				if bb.contains((boxes[k].x0+boxes[k].x1)/2, (boxes[k].y0+boxes[k].y1)/2) {
					at = k
					break
				}
			}
			if at < 0 {
				at = place(boxes, bb)
			}
		}
		images[i] = placed{at: at, cx: (bb.x0 + bb.x1) / 2, cy: (bb.y0 + bb.y1) / 2, bb: bb, image: int32(i)}
	}
	slices.SortStableFunc(images, func(x, y placed) int {
		switch {
		case x.at != y.at:
			return cmp.Compare(x.at, y.at)
		case x.cy != y.cy:
			return cmp.Compare(x.cy, y.cy)
		}
		return cmp.Compare(x.cx, y.cx)
	})

	text := res.Blocks
	res.Blocks = make([]gofastcore.Block, 0, n+m)
	res.Images = make([]int32, 0, n+m)
	next := 0
	for k := 0; k <= n; k++ {
		for ; next < m && images[next].at == k; next++ {
			p := &images[next]
			img := &c.Images[p.image]
			block := gofastcore.Block{
				Kind:  gofastcore.KindImage,
				Image: &gofastcore.Image{Width: int(img.Width), Height: int(img.Height)},
				Loc:   gofastcore.Location{BBox: rect(p.bb.x0, p.bb.y0, p.bb.x1, p.bb.y1)},
			}
			if img.ColorSpace != "" || img.Inline {
				block.Attrs = map[string]string{}
				if img.ColorSpace != "" {
					block.Attrs["color_space"] = cleanName(strings.TrimPrefix(img.ColorSpace, "/"))
				}
				if img.Inline {
					block.Attrs["inline"] = "true"
				}
			}
			res.Blocks = append(res.Blocks, block)
			res.Images = append(res.Images, p.image)
		}
		if k < n {
			res.Blocks = append(res.Blocks, text[k])
			res.Images = append(res.Images, -1)
		}
	}
}

// cleanName makes a name from the file fit for a string of the shared
// model: a name can hold any bytes at all.
func cleanName(s string) string {
	clean := true
	for i := 0; i < len(s); i++ {
		if c := s[i]; c < 0x20 || c >= 0x7F {
			clean = false
			break
		}
	}
	if clean {
		return s
	}
	var sb strings.Builder
	for _, r := range strings.ToValidUTF8(s, "\uFFFD") {
		if r < 0x20 || r >= 0x7F && r <= 0x9F {
			r = '\uFFFD'
		}
		sb.WriteRune(r)
	}
	return sb.String()
}
