package gofastpdf

import (
	"sync"

	"github.com/AOShei/go-fast-pdf/internal/file"
	"github.com/AOShei/go-fast-pdf/internal/syntax"
)

// destinations finds the page that a destination points at: of an entry
// of the outline, or of a link. It is safe for concurrent use: the pages
// of a document are read at the same time, and their links share it.
type destinations struct {
	f     *file.File
	pages []file.Page
	// byObj gives the index of a page by the number of its object.
	byObj map[int64]int
	// Named destinations are in a name tree, or, in the form of PDF 1.1,
	// in a dictionary. A tree is in order, so a name is found along one
	// path; a tree that is not in order is read whole, once, when a name
	// is not found that way.
	tree, dict syntax.Value
	mu         sync.Mutex
	all, old   map[string]syntax.Value
	// byPart holds the named destinations by the part they have a place
	// on, read once (anchors.go).
	namedOnce sync.Once
	byPart    map[int][]named
	// rm counts the control characters removed from the names that
	// are kept.
	rm *removed
}

func newDestinations(f *file.File, catalog syntax.Value, pages []file.Page, rm *removed) *destinations {
	d := &destinations{f: f, rm: rm, pages: pages, byObj: make(map[int64]int, len(pages))}
	for i := range pages {
		if pages[i].Obj != 0 {
			if _, dup := d.byObj[pages[i].Obj]; !dup {
				d.byObj[pages[i].Obj] = i
			}
		}
	}
	d.tree = f.Resolve(catalog.Get("Names")).Get("Dests")
	d.dict = catalog.Get("Dests")
	return d
}

// lookup returns the destination of a name.
func (d *destinations) lookup(name string) syntax.Value {
	f := d.f
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.all == nil {
		if v, ok := findName(f, d.tree, name); ok {
			return v
		}
		d.all = map[string]syntax.Value{}
		walkTree(f, d.tree, "Names", func(key, val syntax.Value) bool {
			if key.Kind == syntax.String {
				if _, dup := d.all[string(key.Data)]; !dup {
					d.all[string(key.Data)] = val
				}
			}
			return true
		})
	}
	if v, ok := d.all[name]; ok {
		return v
	}
	// The dictionary is read into a map once: an outline can have a
	// hundred thousand items, and the dictionary as many names.
	if d.old == nil {
		dict := f.Resolve(d.dict)
		d.old = map[string]syntax.Value{}
		for i := len(dict.List)&^1 - 2; i >= 0 && dict.IsDict(); i -= 2 {
			if k := &dict.List[i]; k.Kind == syntax.Name {
				d.old[string(k.Data)] = dict.List[i+1] // the first of a name wins
			}
		}
	}
	return d.old[name]
}

// target returns the part that the destination of an item (an entry of
// the outline, a link annotation) points at, and how far down its page,
// in displayed points; 0 for no part. The destination is the item's
// /Dest, or the /D of a GoTo action in its /A.
func (d *destinations) target(item syntax.Value) (part int, y float64) {
	dest := d.f.Resolve(item.Get("Dest"))
	if dest.IsNull() {
		if a := d.f.Resolve(item.Get("A")); a.Get("S").IsName("GoTo") {
			dest = d.f.Resolve(a.Get("D"))
		}
	}
	at := d.place(dest)
	return at.part, at.y
}

// spot is where a destination points: a part, and on its page, in
// displayed points, the left and the top of the view where the
// destination gives them (top is false where it gives neither).
type spot struct {
	part int
	x, y float64
	top  bool
}

// place returns where a destination points: an explicit one, an array,
// or a name to look up.
func (d *destinations) place(dest syntax.Value) spot {
	f, pages := d.f, d.pages
	for hops := 0; hops < 4; hops++ {
		switch dest.Kind {
		case syntax.Name, syntax.String:
			dest = f.Resolve(d.lookup(string(dest.Data)))
			continue
		case syntax.Dict:
			dest = f.Resolve(dest.Get("D"))
			continue
		}
		break
	}
	if dest.Kind != syntax.Array || len(dest.List) == 0 {
		return spot{}
	}
	index := -1
	if n := file.RefNumber(dest.List[0]); n != 0 {
		if i, ok := d.byObj[n]; ok {
			index = i
		}
	} else if n, ok := f.Resolve(dest.List[0]).Integer(); ok && n >= 0 && n < int64(len(pages)) {
		index = int(n) // a page index, as some programs write it
	}
	if index < 0 {
		return spot{}
	}
	// The top of the view, where the destination gives one.
	p := &pages[index]
	arg := func(i int) (float64, bool) {
		if i < len(dest.List) {
			return f.Resolve(dest.List[i]).Float()
		}
		return 0, false
	}
	box := p.VisibleBox()
	left, top, hasTop := box[0], 0.0, false
	if len(dest.List) > 1 {
		switch f.Resolve(dest.List[1]).NameString() {
		case "XYZ":
			if l, ok := arg(2); ok {
				left = l
			}
			top, hasTop = arg(3)
		case "FitH", "FitBH":
			top, hasTop = arg(2)
		case "FitR":
			if l, ok := arg(2); ok {
				left = l
			}
			top, hasTop = arg(5)
		}
	}
	if !hasTop || p.Err != nil {
		return spot{part: p.Number}
	}
	m, w, h := p.Display()
	x := m[0]*left + m[2]*top + m[4]
	y := m[1]*left + m[3]*top + m[5]
	return spot{part: p.Number, x: min(max(x, 0), w), y: min(max(y, 0), h), top: true}
}
