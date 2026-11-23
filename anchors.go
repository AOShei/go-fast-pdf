package gofastpdf

import (
	"github.com/AOShei/go-fast-pdf/internal/syntax"

	gofastcore "github.com/AOShei/go-fast-core"
)

// Anchors: the names a file gives to places in it.
//
// Loc.Anchor is a name the file itself gives a block, by which a link or
// a citation can point at it. A PDF has two kinds of name. A named
// destination (in the name tree /Dests of the catalog's /Names, or in
// the catalog's /Dests of PDF 1.1) names a place on a page, the top left
// corner of a view; a link to the file can name it too, as #nameddest=.
// The block it names is the one that begins there: its top below the
// place by no more than anchorReach, or above it by no more than
// anchorSlack, and its right edge not to the left of the place. Of those
// the nearest is taken, counting how far down from the place a block
// begins and how far to the right: in two columns the block below the
// place, not the one beside it. A destination names no block when it names a
// page with no place on it or with its top edge as the place (a view of
// the page from its top shows the page), when the place is inside a block, when no
// block begins near it, or when what begins there is a running header
// or footer. Of the names of one block the nearest wins, and of names
// as near the first in the order of the file.
//
// A structure element of a tagged PDF can have an identifier, /ID; the
// first block made from its content has it (structure.go). Where a
// block has both, the name of the destination wins: it is the name that
// links use.
//
// The names of destinations are read with Options.Spans, which gives
// the links that use them: finding the places of all of them means
// reading an object for each, which for a file with many takes as long
// as some of its pages. Identifiers of structure elements are read
// always: they are found with the headings of the tree, at no cost
// worth counting.
//
// A name longer than maxAnchorBytes is left out rather than cut: a name
// cut short names nothing.

const (
	// anchorReach is how far below the place of a destination, in
	// points, the block it names can begin: a program puts the place a
	// line or so above the text, the line's leading.
	anchorReach = 20
	// anchorSlack is how far above the place the top of the block can
	// be, and how far to the right of the block's right edge the place.
	anchorSlack = 3
	// maxAnchorBytes is the longest name that is an anchor.
	maxAnchorBytes = 512
)

// named is a named destination with a place on a page: the place in
// displayed points.
type named struct {
	name string
	x, y float64
}

// named returns the named destinations with a place on a part, in the
// order of the file. They are read for all parts when they are first
// asked for.
func (d *destinations) named(part int) []named {
	if d == nil {
		return nil
	}
	d.namedOnce.Do(d.readNamed)
	return d.byPart[part]
}

func (d *destinations) readNamed() {
	f := d.f
	seen := map[string]bool{}
	// controls is the number of control characters removed from the
	// name, counted when the name is kept.
	add := func(name string, controls int64, dest syntax.Value) {
		if name == "" || len(name) > maxAnchorBytes || seen[name] {
			return
		}
		seen[name] = true // the first of a name wins, as in lookup
		at := d.place(f.Resolve(dest))
		if at.part == 0 || !at.top || at.y < 1 {
			return
		}
		if d.byPart == nil {
			d.byPart = map[int][]named{}
		}
		d.byPart[at.part] = append(d.byPart[at.part], named{name: name, x: at.x, y: at.y})
		d.rm.add(int(controls))
	}
	walkTree(f, d.tree, "Names", func(key, val syntax.Value) bool {
		var rm removed
		name := text(f, key, &rm)
		add(name, rm.count(), val)
		return true
	})
	if dict := f.Resolve(d.dict); dict.IsDict() {
		for i := 0; i < dict.Entries(); i++ {
			key, val := dict.Entry(i)
			var rm removed
			name := cleanText(string(key), &rm)
			add(name, rm.count(), val)
		}
	}
}

// nameAnchors gives the blocks of a part the names of the destinations
// on it.
func nameAnchors(part *gofastcore.Part, names []named) {
	if len(names) == 0 {
		return
	}
	type pick struct {
		name int
		dist float64
	}
	var won []pick // by block; name is one more than the index of the name
	for n := range names {
		at := &names[n]
		k, dist, inside := -1, 0.0, false
		for i := range part.Blocks {
			b := &part.Blocks[i]
			r := b.Loc.BBox
			if b.Kind == gofastcore.KindHeader || b.Kind == gofastcore.KindFooter || r == (gofastcore.Rect{}) || r.X1 < at.x-anchorSlack {
				continue
			}
			d := r.Y0 - at.y
			if d < -anchorSlack {
				// A block that the place is inside of.
				inside = inside || r.Y1 > at.y+anchorSlack && r.X0 <= at.x+anchorSlack
				continue
			}
			if d > anchorReach {
				continue
			}
			if d += max(0, r.X0-at.x); k < 0 || d < dist {
				k, dist = i, d
			}
		}
		if k < 0 || inside {
			continue
		}
		if won == nil {
			won = make([]pick, len(part.Blocks))
		}
		if w := won[k]; w.name == 0 || dist < w.dist {
			won[k] = pick{name: n + 1, dist: dist}
		}
	}
	for i, w := range won {
		if w.name > 0 {
			part.Blocks[i].Loc.Anchor = names[w.name-1].name
		}
	}
}
