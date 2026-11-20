package gofastpdf

import (
	"strings"
	"sync"

	"github.com/AOShei/go-fast-pdf/internal/file"
	"github.com/AOShei/go-fast-pdf/internal/syntax"
)

// The structure tree of a tagged PDF.
//
// A tagged PDF says what its content is: a structure element of type H1
// is a heading of level 1, one of type Figure an illustration with, in
// /Alt, a description of it in words. The content of a page refers to
// its elements by marked-content identifiers (MCID): the page's
// /StructParents is a key of the tree's /ParentTree, a number tree whose
// value for the page is an array with, at the index of each MCID, the
// element it belongs to. Each element has its parent in /P, and a type
// in /S, which /RoleMap may map to a standard type.
//
// Of all this, six things are read, for the content of each page as
// it is read: the headings, H1 to H6 and H (whose level is the number of
// sections, Sect, around it), the alternative description of a figure,
// the cells of tables (tables.go), the items of lists (lists.go), the
// identifiers of elements, /ID, and the order of the content in the
// tree (treeorder.go). A block of text whose every run
// is under one heading is that heading, with OriginNative; an image
// drawn under a figure has its description as its text. A page that has
// a heading of the tree has none by type. An element with an identifier
// gives it to the block that its content begins, as Loc.Anchor: the
// block whose first run is the first item of the element, or of the
// first of its kids, and so on down (anchors.go). Identifiers are looked
// for only in a tree that lists them, in /IDTree, as the format requires
// of a tree that has them.

// structure is what is read of a document's structure tree once.
type structure struct {
	f          *file.File
	parentTree syntax.Value
	roles      map[string]string
	// ids is set when the tree lists identifiers of elements.
	ids bool
	// The grids of the tables, read once each (tables.go).
	mu    sync.Mutex
	grids map[int64]*tableGrid
	// root is the object number of the tree's root. kidAt gives, for an
	// element, where each of its kids that is an element stands in its
	// /K, and contentAt where each of its marked contents stands, by
	// page: read once for each element with many kids (treeorder.go).
	root      int64
	om        sync.Mutex
	kidAt     map[int64]map[int64]int32
	contentAt map[int64]map[int64][]placedContent
}

// maxStructHops bounds the parents followed up from an element, and
// maxPageMCIDs the identifiers of a page that are looked at.
const (
	maxStructHops = 64
	maxPageMCIDs  = 1 << 16
	maxAltBytes   = 64 << 10
)

// newStructure reads the structure tree of a catalog: nil when there is
// none, or no parent tree to find its elements by.
func newStructure(f *file.File, catalog syntax.Value) *structure {
	root := f.Resolve(catalog.Get("StructTreeRoot"))
	if !root.IsDict() {
		return nil
	}
	s := &structure{f: f, parentTree: root.Get("ParentTree"), root: file.RefNumber(catalog.Get("StructTreeRoot"))}
	if !f.Resolve(s.parentTree).IsDict() {
		return nil
	}
	s.ids = !f.Resolve(root.Get("IDTree")).IsNull()
	if rm := f.Resolve(root.Get("RoleMap")); rm.IsDict() {
		s.roles = map[string]string{}
		for i := 0; i < rm.Entries() && i < 4096; i++ {
			k, v := rm.Entry(i)
			if v := f.Resolve(v); v.Kind == syntax.Name {
				s.roles[string(k)] = string(v.Data)
			}
		}
	}
	return s
}

// role returns the standard type of a structure type, through the role
// map.
func (s *structure) role(t string) string {
	for hops := 0; hops < 8; hops++ {
		if standardType(t) {
			return t
		}
		next, ok := s.roles[t]
		if !ok || next == t {
			return t
		}
		t = next
	}
	return t
}

func standardType(t string) bool {
	switch t {
	case "H", "H1", "H2", "H3", "H4", "H5", "H6", "Figure", "Sect", "P", "Span", "Document", "Part", "Div", "L", "LI", "Lbl", "LBody", "Table", "TR", "TH", "TD", "Caption", "Link", "Note", "Formula", "Form", "Art", "BlockQuote", "TOC", "TOCI", "Index", "Quote", "Reference", "BibEntry", "Code", "Annot", "Ruby", "Warichu", "THead", "TBody", "TFoot", "NonStruct", "Private":
		return true
	}
	return false
}

// tag is what the structure tree says of the content under one MCID.
type tag struct {
	// heading is the level of the heading the content is part of, 0 for
	// none.
	heading uint8
	// alt is the description of the figure the content is part of.
	alt string
	// id is the identifier of the innermost element that the content
	// begins.
	id string
	// cell and table are the object numbers of the cell of a table the
	// content is in, TD or TH, and of the table; 0 for none.
	cell, table int64
	// item is the object number of the list item, LI, the content is
	// in, 0 for none; label is set when the content is the item's label,
	// Lbl, and numbering is what the list's attributes say of its
	// numbering (lists.go).
	item      int64
	label     bool
	numbering numbering
	// rank is one more than the place of the content in the order of the
	// tree among the content of the page, 0 when the tree does not place
	// it (treeorder.go).
	rank int32
}

// pageTags returns, by MCID, what the structure tree says of the content
// of a page, and the page as the tree orders it, whose ranks are found
// when first asked for (treeorder.go); nil when the tree says nothing.
func (s *structure) pageTags(p *file.Page) ([]tag, *treePage) {
	if s == nil {
		return nil, nil
	}
	f := s.f
	key, ok := f.Resolve(p.Dict().Get("StructParents")).Integer()
	if !ok {
		return nil, nil
	}
	elems := f.Resolve(findNumber(f, s.parentTree, key)).Elems()
	if len(elems) == 0 {
		return nil, nil
	}
	elems = elems[:min(len(elems), maxPageMCIDs)]
	w := &walker{s: s, memo: map[int64]reach{}}
	var out []tag
	for mcid, ref := range elems {
		r := w.reach(ref, 0)
		t := tag{alt: r.alt}
		if r.cell != 0 && r.table != 0 {
			t.cell, t.table = r.cell, r.table
		}
		if s.ids {
			t.id = w.firstID(ref, mcid, p.Obj)
		}
		switch {
		case r.heading > 0:
			t.heading = r.heading
		case r.h:
			// No deeper than the parents followed: maxStructHops.
			t.heading = uint8(max(r.hSections, 1))
		}
		if r.item != 0 {
			t.item, t.label, t.numbering = r.item, r.label, r.numbering
		}
		if t != (tag{}) {
			if out == nil {
				out = make([]tag, len(elems))
			}
			out[mcid] = t
		}
	}
	if out == nil {
		out = make([]tag, len(elems))
	}
	return out, &treePage{w: w, elems: elems, page: p.Obj, tags: out}
}

// reach is what an element and the elements above it say.
type reach struct {
	// heading is the level of the nearest numbered heading, H1 to H6; h
	// is set when the nearest heading is H, and hSections is then the
	// number of sections around it.
	heading   uint8
	h         bool
	hSections int
	// sections is the number of sections, the element included.
	sections int
	// alt is the description of the nearest figure that has one.
	alt string
	// cell is the nearest cell of a table, TD or TH, inside the nearest
	// table, and table that table, by their object numbers.
	cell, table int64
	// item is the nearest list item, LI, not inside a table nearer than
	// it; label is set inside the label, Lbl, of the nearest item;
	// numbering is what the nearest list says of its numbering.
	item      int64
	label     bool
	numbering numbering
	// pathed is set once the element's path is looked for (walker.path),
	// and placed when its way up reaches the root through elements that
	// are objects and kids of their parents; the path is then
	// walker.paths[at:at+n]: where each element from the root's kid down
	// to it stands among the kids of its parent (treeorder.go).
	pathed, placed bool
	at, n          int32
}

// walker follows elements up to the root, each once on a page.
type walker struct {
	s     *structure
	memo  map[int64]reach
	paths []int32
}

func (w *walker) reach(ref syntax.Value, depth int) reach {
	n := file.RefNumber(ref)
	if r, ok := w.memo[n]; ok && n != 0 {
		return r
	}
	if n != 0 {
		w.memo[n] = reach{} // a parent that leads back here ends the walk
	}
	f := w.s.f
	el := f.Resolve(ref)
	if !el.IsDict() || depth >= maxStructHops {
		return reach{}
	}
	if f.Resolve(el.Get("Type")).IsName("StructTreeRoot") {
		return reach{}
	}
	r := w.reach(el.Get("P"), depth+1)
	r.pathed, r.placed, r.at, r.n = false, false, 0, 0 // found by path, when asked for
	switch t := w.s.role(f.Resolve(el.Get("S")).NameString()); {
	case len(t) == 2 && t[0] == 'H' && t[1] >= '1' && t[1] <= '6':
		r.heading, r.h = t[1]-'0', false
	case t == "H":
		r.heading, r.h, r.hSections = 0, true, r.sections
	case t == "Sect":
		r.sections++
	case t == "TD" || t == "TH":
		r.cell = n
	case t == "Table":
		r.table, r.cell = n, 0 // a table in a cell has cells of its own
		r.item, r.label = 0, false
	case t == "L":
		r.numbering = w.s.listNumbering(el)
	case t == "LI":
		r.item, r.label = n, false
	case t == "Lbl":
		r.label = r.item != 0
	case t == "Figure":
		if alt := strings.TrimSpace(text(f, el.Get("Alt"))); alt != "" {
			r.alt = clipText(alt, maxAltBytes)
		}
	}
	if n != 0 {
		w.memo[n] = r
	}
	return r
}

// firstID returns the identifier of the innermost element that the
// content with an MCID on a page begins, from ref, the element the
// content is in: that element if the content is its first item, the
// element above it if that one is its first kid as well, and so on up.
// "" for none.
func (w *walker) firstID(ref syntax.Value, mcid int, page int64) string {
	f := w.s.f
	var child int64 // the element below, 0 at the content
	for hops := 0; hops < maxStructHops; hops++ {
		el := f.Resolve(ref)
		if !el.IsDict() || f.Resolve(el.Get("Type")).IsName("StructTreeRoot") || !w.begins(el, child, mcid, page) {
			return ""
		}
		if id := text(f, el.Get("ID")); id != "" && len(id) <= maxAnchorBytes {
			return id
		}
		if child = file.RefNumber(ref); child == 0 {
			return "" // an element that is not an object is no parent's kid by number
		}
		ref = el.Get("P")
	}
	return ""
}

// begins reports whether the first item of an element is the element
// numbered child, or, for child 0, the content with an MCID on a page.
func (w *walker) begins(el syntax.Value, child int64, mcid int, page int64) bool {
	f := w.s.f
	k := el.Get("K")
	if list := f.Resolve(k); list.Kind == syntax.Array {
		if len(list.List) == 0 {
			return false
		}
		k = list.List[0]
	}
	if child != 0 {
		return file.RefNumber(k) == child
	}
	item, pg := f.Resolve(k), el.Get("Pg")
	if item.IsDict() {
		// A marked-content reference, which can name a page of its own.
		if !f.Resolve(item.Get("Type")).IsName("MCR") {
			return false
		}
		if p := item.Get("Pg"); !p.IsNull() {
			pg = p
		}
		item = f.Resolve(item.Get("MCID"))
	}
	n, ok := item.Integer()
	return ok && n == int64(mcid) && page != 0 && file.RefNumber(pg) == page
}

// findNumber looks one key up in a number tree: each node says which
// keys its branches hold, and the keys of a leaf are sorted.
func findNumber(f *file.File, root syntax.Value, key int64) syntax.Value {
	node := f.Resolve(root)
	for depth := 0; depth < 32 && node.IsDict(); depth++ {
		if list := f.Resolve(node.Get("Nums")).Elems(); len(list) > 1 {
			lo, hi := 0, len(list)/2
			for lo < hi {
				mid := (lo + hi) / 2
				k, ok := f.Resolve(list[2*mid]).Integer()
				switch {
				case !ok:
					return syntax.Value{}
				case k == key:
					return list[2*mid+1]
				case k < key:
					lo = mid + 1
				default:
					hi = mid
				}
			}
			return syntax.Value{}
		}
		var next syntax.Value
		for _, kid := range f.Resolve(node.Get("Kids")).Elems() {
			k := f.Resolve(kid)
			limits := f.Resolve(k.Get("Limits")).Elems()
			if len(limits) != 2 {
				continue
			}
			first, ok1 := f.Resolve(limits[0]).Integer()
			last, ok2 := f.Resolve(limits[1]).Integer()
			if ok1 && ok2 && key >= first && key <= last {
				next = kid
				break
			}
		}
		node = f.Resolve(next)
	}
	return syntax.Value{}
}

// clipText cuts text at a character boundary to at most n bytes.
func clipText(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && s[n]&0xC0 == 0x80 {
		n--
	}
	return s[:n]
}
