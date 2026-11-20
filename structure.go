package gofastpdf

import (
	"strings"

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
// Of all this, two things are read, for the content of each page as it
// is read: the headings, H1 to H6 and H (whose level is the number of
// sections, Sect, around it), and the alternative description of a
// figure. A block of text whose every run is under one heading is that
// heading, with OriginNative; an image drawn under a figure has its
// description as its text. A page that has a heading of the tree has
// none by type.

// structure is what is read of a document's structure tree once.
type structure struct {
	f          *file.File
	parentTree syntax.Value
	roles      map[string]string
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
	s := &structure{f: f, parentTree: root.Get("ParentTree")}
	if !f.Resolve(s.parentTree).IsDict() {
		return nil
	}
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
}

// pageTags returns, by MCID, what the structure tree says of the content
// of a page; nil when it says nothing.
func (s *structure) pageTags(p *file.Page) []tag {
	if s == nil {
		return nil
	}
	f := s.f
	key, ok := f.Resolve(p.Dict().Get("StructParents")).Integer()
	if !ok {
		return nil
	}
	elems := f.Resolve(findNumber(f, s.parentTree, key)).Elems()
	if len(elems) == 0 {
		return nil
	}
	elems = elems[:min(len(elems), maxPageMCIDs)]
	w := walker{s: s, memo: map[int64]reach{}}
	var out []tag
	for mcid, ref := range elems {
		r := w.reach(ref, 0)
		t := tag{alt: r.alt}
		switch {
		case r.heading > 0:
			t.heading = r.heading
		case r.h:
			t.heading = uint8(min(max(r.hSections, 1), 6))
		}
		if t != (tag{}) {
			if out == nil {
				out = make([]tag, len(elems))
			}
			out[mcid] = t
		}
	}
	return out
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
}

// walker follows elements up to the root, each once on a page.
type walker struct {
	s    *structure
	memo map[int64]reach
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
	if !el.IsDict() || depth >= maxStructHops || f.Resolve(el.Get("Type")).IsName("StructTreeRoot") {
		return reach{}
	}
	r := w.reach(el.Get("P"), depth+1)
	switch t := w.s.role(f.Resolve(el.Get("S")).NameString()); {
	case len(t) == 2 && t[0] == 'H' && t[1] >= '1' && t[1] <= '6':
		r.heading, r.h = t[1]-'0', false
	case t == "H":
		r.heading, r.h, r.hSections = 0, true, r.sections
	case t == "Sect":
		r.sections++
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
