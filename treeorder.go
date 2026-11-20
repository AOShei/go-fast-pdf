package gofastpdf

import (
	"bytes"
	"encoding/binary"
	"slices"

	"github.com/AOShei/go-fast-pdf/internal/file"
	"github.com/AOShei/go-fast-pdf/internal/syntax"
)

// The order of a tagged PDF.
//
// The structure tree gives its content in the order it is read: the
// kids of each element, /K, in order, the content of a kid before that
// of the kid after it. The place of a page's content in that order is
// found from the element each MCID belongs to, up to the root (as the
// walker of structure.go goes up): at each step, where the element
// stands among the kids of its parent. Content whose way up does not
// reach the root, through elements that are objects and kids of their
// parents, is not placed.

const (
	// maxKids bounds the kids of one element that are looked at to find
	// where a kid stands. An element with more than fewKids kids that
	// are elements, or more than manyContents that are content, has
	// where they stand read once and kept; one with fewer is looked
	// through: the kids that are elements for each, and the content once
	// on each page, for all of the page's content in it.
	maxKids      = 1 << 20
	fewKids      = 8
	manyContents = 256
)

// treePage is a page of a tagged PDF as the tree orders it: the ranks
// of its content are found when first asked for, which the layout does
// only for a page that is not drawn from top to bottom in one column.
type treePage struct {
	w     *walker
	elems []syntax.Value
	page  int64
	tags  []tag
	done  bool
}

// rank returns one more than the place of the content under an MCID in
// the order of the tree, 0 when the tree does not place it.
func (t *treePage) rank(mcid int32) int32 {
	if !t.done {
		t.done = true
		t.w.ranks(t.elems, t.page, t.tags)
	}
	if mcid >= 0 && int(mcid) < len(t.tags) {
		return t.tags[mcid].rank
	}
	return 0
}

// ranks gives each MCID of a page its place in the order of the tree,
// in tags[mcid].rank, from elems, the element of each MCID, whose paths
// the walker has found. Most pages give their content in the order of
// the tree, and are only checked to be in it.
func (w *walker) ranks(elems []syntax.Value, page int64, tags []tag) {
	// The place of each MCID: the path of its element, and where it
	// stands among the element's kids.
	type placed struct {
		off, n, at, mcid int32
	}
	all := make([]placed, 0, len(elems))
	// where[m] is one more than where MCID m stands among the kids of its
	// element, -1 when it is not among them, 0 before it is looked for.
	where := make([]int32, len(elems))
	w.paths = make([]int32, 0, 8*len(elems))
	for mcid, ref := range elems {
		r := w.path(ref, 0)
		if !r.placed {
			continue
		}
		if where[mcid] == 0 {
			w.s.contentPlaces(ref, page, elems, where, mcid)
		}
		if at := where[mcid] - 1; at >= 0 {
			all = append(all, placed{r.at, r.n, at, int32(mcid)})
		}
	}
	// Each path written as bytes, four to an item, which compare as the
	// paths do: item by item, a path before the paths it begins.
	p := w.paths
	keys := make([]byte, 0, 4*(len(all)+len(p)))
	type keyed struct {
		k0, k1, mcid int32
	}
	sorted := true
	order := make([]keyed, len(all))
	for i, x := range all {
		k0 := int32(len(keys))
		for _, v := range p[x.off : x.off+x.n] {
			keys = binary.BigEndian.AppendUint32(keys, uint32(v))
		}
		keys = binary.BigEndian.AppendUint32(keys, uint32(x.at))
		order[i] = keyed{k0, int32(len(keys)), x.mcid}
		if i > 0 && sorted {
			prev := order[i-1]
			sorted = bytes.Compare(keys[prev.k0:prev.k1], keys[k0:]) <= 0
		}
	}
	if !sorted {
		slices.SortStableFunc(order, func(x, y keyed) int {
			return bytes.Compare(keys[x.k0:x.k1], keys[y.k0:y.k1])
		})
	}
	for k, x := range order {
		tags[x.mcid].rank = int32(k + 1)
	}
}

// path returns what the walker knows of an element, with its path
// (reach.placed, at and n) found.
func (w *walker) path(ref syntax.Value, depth int) reach {
	n := file.RefNumber(ref)
	r := w.reach(ref, 0)
	if n == 0 || r.pathed || depth >= maxStructHops {
		return r
	}
	r.pathed = true
	w.memo[n] = r // a parent that leads back here ends the walk
	f := w.s.f
	up := f.Resolve(ref).Get("P")
	parent := file.RefNumber(up)
	var above reach
	switch {
	case parent == 0:
		return r
	case parent == w.s.root || f.Resolve(f.Resolve(up).Get("Type")).IsName("StructTreeRoot"):
		above.placed = true
	default:
		if above = w.path(up, depth+1); !above.placed {
			return r
		}
	}
	if at := w.s.kidIndex(parent, n); at >= 0 {
		r.placed, r.at, r.n = true, int32(len(w.paths)), above.n+1
		w.paths = append(w.paths, w.paths[above.at:above.at+above.n]...)
		w.paths = append(w.paths, at)
		w.memo[n] = r
	}
	return r
}

// kidIndex returns where the element numbered kid stands among the
// kids of the element (or root) numbered parent; -1 when it is not one.
func (s *structure) kidIndex(parent, kid int64) int32 {
	var one [1]syntax.Value
	list := s.kList(s.f.Object(parent), &one)
	if len(list) <= fewKids {
		for i, k := range list {
			if file.RefNumber(k) == kid {
				return int32(i)
			}
		}
		return -1
	}
	s.om.Lock()
	defer s.om.Unlock()
	at, ok := s.kidAt[parent]
	if !ok {
		at = make(map[int64]int32, len(list))
		for i, k := range list {
			if n := file.RefNumber(k); n != 0 {
				if _, dup := at[n]; !dup {
					at[n] = int32(i)
				}
			}
		}
		if s.kidAt == nil {
			s.kidAt = map[int64]map[int64]int32{}
		}
		s.kidAt[parent] = at
	}
	if i, ok := at[kid]; ok {
		return i
	}
	return -1
}

// contentPlaces finds where the content of the page in the element ref
// stands among its kids, for the MCID mcid and every other MCID of the
// page that the parent tree, elems, gives the element: in where, as for
// ranks. An MCID that no page is given for, by the element or by a
// reference, is taken to be on the page: the parent tree says the
// element has it.
func (s *structure) contentPlaces(ref syntax.Value, page int64, elems []syntax.Value, where []int32, mcid int) {
	where[mcid] = -1
	n := file.RefNumber(ref)
	if n == 0 || page == 0 {
		return
	}
	el := s.f.Object(n)
	var one [1]syntax.Value
	list := s.kList(el, &one)
	set := func(m int64, i int32) {
		if m >= 0 && m < int64(len(where)) && where[m] <= 0 && (m == int64(mcid) || where[m] == 0 && file.RefNumber(elems[m]) == n) {
			where[m] = i + 1
		}
	}
	if len(list) > manyContents {
		at := s.contentMap(n, el, list)
		for _, on := range [2]int64{page, 0} {
			for _, c := range at[on] {
				set(c.mcid, c.at)
			}
		}
		return
	}
	pg := file.RefNumber(el.Get("Pg"))
	for i, k := range list {
		if on, m, ok := s.content(k, pg); ok && (on == page || on == 0) {
			set(m, int32(i))
		}
	}
}

// placedContent is a content among the kids of an element: its MCID,
// and where it stands.
type placedContent struct {
	mcid int64
	at   int32
}

// contentMap returns the content among the kids of an element with many
// of them, by page (0 for content that no page is given for), read once.
func (s *structure) contentMap(n int64, el syntax.Value, list []syntax.Value) map[int64][]placedContent {
	s.om.Lock()
	defer s.om.Unlock()
	if at, ok := s.contentAt[n]; ok {
		return at
	}
	at := map[int64][]placedContent{}
	pg := file.RefNumber(el.Get("Pg"))
	for i, k := range list {
		if on, m, ok := s.content(k, pg); ok {
			at[on] = append(at[on], placedContent{m, int32(i)})
		}
	}
	if s.contentAt == nil {
		s.contentAt = map[int64]map[int64][]placedContent{}
	}
	s.contentAt[n] = at
	return at
}

// content reads a kid of an element that is marked content: an MCID,
// on the element's page pg, or a marked-content reference, which can
// name a page of its own. ok is false for any other kid.
func (s *structure) content(k syntax.Value, pg int64) (page, mcid int64, ok bool) {
	f := s.f
	item := f.Resolve(k)
	if item.IsDict() {
		if !f.Resolve(item.Get("Type")).IsName("MCR") {
			return 0, 0, false
		}
		if p := file.RefNumber(item.Get("Pg")); p != 0 {
			pg = p
		}
		item = f.Resolve(item.Get("MCID"))
	}
	m, ok := item.Integer()
	return pg, m, ok
}

// kList returns the items of an element's /K: one item or an array of
// them, at most maxKids. One item is returned in one, which the caller
// keeps.
func (s *structure) kList(el syntax.Value, one *[1]syntax.Value) []syntax.Value {
	k := el.Get("K")
	if k.IsNull() {
		return nil
	}
	if v := s.f.Resolve(k); v.Kind == syntax.Array {
		return v.List[:min(len(v.List), maxKids)]
	}
	one[0] = k
	return one[:]
}
