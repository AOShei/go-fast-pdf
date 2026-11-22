package file

import (
	"bytes"
	"sort"
	"sync"
	"sync/atomic"

	gofastcore "github.com/AOShei/go-fast-core"
	"github.com/AOShei/go-fast-pdf/internal/syntax"
)

// Recovery: reading a file whose cross-reference data is wrong.
//
// The objects of a PDF announce themselves ("12 0 obj"), so a reader can
// find them without the cross-reference data by scanning the file. That
// is done in two situations:
//
//   - The cross-reference data as a whole is unusable: no startxref, an
//     offset that leads nowhere, no trailer, no catalog. Open then calls
//     rebuild, and the scan replaces the table.
//   - The table is usable but wrong about an object, or does not mention
//     it. The first such object triggers one scan of the file, kept as a
//     second index beside the table (recovered).
//
// Whether an object is taken from the recovery index depends only on that
// object's own entry, never on what was read before, so the result does
// not depend on the order in which goroutines ask.

// found is one object header located by the scan.
type found struct {
	num int64
	gen uint16
	off int64
}

// scanObjects returns every indirect object header in data, in file
// order: a number, a generation and the keyword obj, separated by white
// space and standing on their own.
func scanObjects(data []byte, maxObjects int) []found {
	var out []found
	for pos := 0; ; {
		i := bytes.Index(data[pos:], []byte("obj"))
		if i < 0 {
			return out
		}
		k := pos + i
		pos = k + 3
		// The keyword must end here ("object" does not count) and be
		// preceded by white space ("endobj" does not count).
		if k+3 < len(data) && syntax.IsRegular(data[k+3]) {
			continue
		}
		j := k
		for j > 0 && syntax.IsSpace(data[j-1]) {
			j--
		}
		if j == k {
			continue
		}
		gen, j, ok := digitsBefore(data, j, 5)
		if !ok {
			continue
		}
		sep := j
		for j > 0 && syntax.IsSpace(data[j-1]) {
			j--
		}
		if j == sep {
			continue
		}
		num, j, ok := digitsBefore(data, j, 10)
		if !ok || (j > 0 && syntax.IsRegular(data[j-1])) {
			continue
		}
		if num <= 0 || num >= int64(maxObjects) {
			continue
		}
		out = append(out, found{num: num, gen: uint16(min(gen, 65535)), off: int64(j)})
	}
}

// digitsBefore reads the run of at most max digits that ends at end and
// returns its value and where it starts.
func digitsBefore(data []byte, end, max int) (value int64, start int, ok bool) {
	start = end
	for start > 0 && end-start < max && data[start-1] >= '0' && data[start-1] <= '9' {
		start--
	}
	if start == end {
		return 0, end, false
	}
	for _, c := range data[start:end] {
		value = value*10 + int64(c-'0')
	}
	return value, start, true
}

// mentions reports whether the first part of the object at off, up to
// its stream data or its end, contains the given name. It is a cheap
// test that spares parsing every object of a large file.
func (f *File) mentions(off int64, name string) bool {
	window := f.data[off:min(int64(len(f.data)), off+1024)]
	if i := bytes.Index(window, []byte("stream")); i >= 0 {
		window = window[:i]
	}
	if i := bytes.Index(window, []byte("endobj")); i >= 0 {
		window = window[:i]
	}
	return bytes.Contains(window, []byte(name))
}

// recoverTrailer puts a trailer together from what the file still has:
// its trailer dictionaries and the dictionaries of its cross-reference
// streams, later ones overriding earlier ones, and failing those the
// last object that says it is the catalog.
func (f *File) recoverTrailer(objects []found) syntax.Value {
	type candidate struct {
		pos  int64
		dict syntax.Value
	}
	var candidates []candidate
	for pos := 0; ; {
		i := bytes.Index(f.data[pos:], []byte("trailer"))
		if i < 0 {
			break
		}
		at := pos + i + len("trailer")
		pos = at
		p := syntax.NewParser(f.data, at, f.limits.MaxNesting)
		if v, err := p.Object(); err == nil && v.Kind == syntax.Dict {
			candidates = append(candidates, candidate{int64(at), v})
		}
		if len(candidates) > f.limits.MaxXRefSections {
			break
		}
	}
	for _, o := range objects {
		if !f.mentions(o.off, "/XRef") {
			continue
		}
		if v, err := f.parseAt(o.off, o.num); err == nil && v.Kind == syntax.Stream && v.Get("Type").IsName("XRef") {
			candidates = append(candidates, candidate{o.off, v.AsDict()})
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].pos < candidates[j].pos })

	keys := []string{"Root", "Info", "Encrypt", "ID"}
	values := make([]syntax.Value, len(keys))
	for _, c := range candidates {
		for i, key := range keys {
			if v := c.dict.Get(key); !v.IsNull() {
				values[i] = v
			}
		}
	}
	if values[0].IsNull() {
		for i := len(objects) - 1; i >= 0; i-- {
			o := objects[i]
			if !f.mentions(o.off, "/Catalog") {
				continue
			}
			if v, err := f.parseAt(o.off, o.num); err == nil && v.Kind == syntax.Dict && v.Get("Type").IsName("Catalog") {
				values[0] = syntax.Value{Kind: syntax.Ref, N: o.num, Gen: o.gen}
				break
			}
		}
	}
	if values[2].IsNull() {
		// No trailer says the file is encrypted. If it holds an
		// encryption dictionary all the same, it is: reading it as if
		// it were not would pass ciphertext on as text.
		for i := len(objects) - 1; i >= 0; i-- {
			o := objects[i]
			if !f.mentions(o.off, "/Standard") {
				continue
			}
			v, err := f.parseAt(o.off, o.num)
			if err == nil && v.Kind == syntax.Dict && v.Get("Filter").IsName("Standard") && v.Has("O") && v.Has("U") && v.Has("P") {
				values[2] = syntax.Value{Kind: syntax.Ref, N: o.num, Gen: o.gen}
				f.rec.lostTrailer = true
				break
			}
		}
	}
	var list []syntax.Value
	for i, key := range keys {
		if !values[i].IsNull() {
			list = append(list, syntax.Value{Kind: syntax.Name, Data: []byte(key)}, values[i])
		}
	}
	return syntax.Value{Kind: syntax.Dict, List: list}
}

// located says where the scan found an object and how late in the file
// that place is. Of two places for one object the later one is the newer
// revision.
type located struct {
	e   entry
	pos int64
	// stm is the object number of the object stream that holds the
	// object, when e says it is inside one. e identifies the stream by
	// its offset; the rebuilt table identifies it by this number.
	stm int64
}

// locate builds the index of every object the scan can reach: the
// objects found directly, and those inside the object streams among
// them. An object stream is identified by the offset of its own header,
// which is also its position for deciding which revision is newer.
//
// depth is passed on to everything that might follow a reference. The
// index built on first need passes noResolve: while it is being built no
// reference may be followed, because following one could ask for the
// index itself.
func (f *File) locate(objects []found, depth int) map[int64]located {
	index := make(map[int64]located, len(objects))
	for _, o := range objects {
		index[o.num] = located{e: entry{kind: entryOffset, off: o.off, gen: o.gen}, pos: o.off}
	}
	for _, o := range objects {
		if !f.mentions(o.off, "/ObjStm") {
			continue
		}
		s, err := f.objStmAt(o, depth)
		if err != nil {
			continue
		}
		for i, num := range s.nums {
			if num <= 0 || num >= int64(f.limits.MaxObjects) {
				continue
			}
			if have, ok := index[num]; ok && have.pos > o.off {
				continue // a newer copy of the object comes later in the file
			}
			index[num] = located{e: entry{kind: entryCompressed, off: o.off, idx: int32(i)}, pos: o.off, stm: o.num}
		}
	}
	return index
}

// objStmAt loads the object stream whose header the scan found. It is
// cached under a key that cannot collide with an object number.
func (f *File) objStmAt(o found, depth int) (*objStm, error) {
	return f.loadObjStm(-o.off-1, depth, func() syntax.Value {
		v, err := f.parseAt(o.off, o.num)
		if err != nil || !v.Get("Type").IsName("ObjStm") {
			return syntax.Value{}
		}
		return f.decryptObject(v, int64(v.Obj), v.Gen)
	})
}

// noResolve is a depth at which no indirect reference is followed.
const noResolve = 9

// rebuild replaces the cross-reference data with what a scan of the file
// finds. It is used when the data as a whole is unusable.
func (f *File) rebuild() error {
	objects := scanObjects(f.data, f.limits.MaxObjects)
	if len(objects) == 0 {
		return malformed("the file contains no objects")
	}
	// First the objects found directly, which is enough to read the
	// trailer and the encryption dictionary; then, with decryption in
	// place, the objects inside object streams.
	f.xref = nil
	f.rebuilt = true
	for _, o := range objects {
		if o.num > 0 && o.num < int64(len(f.xref)) && f.xref[o.num].kind != entryMissing {
			f.redefined++ // the later definition is the one read
		}
		f.putEntry(o.num, entry{kind: entryOffset, off: o.off, gen: o.gen})
	}
	f.objs = make([]atomic.Pointer[syntax.Value], len(f.xref))
	f.resetFilled()
	f.trailer = f.recoverTrailer(objects)
	// Nothing else may trigger a second scan: the table now is the scan.
	f.rec.once.Do(func() {})
	if err := f.setupEncryption(); err != nil {
		return err
	}
	index := f.locate(objects, 0)
	for num, loc := range index {
		if loc.e.kind == entryCompressed {
			// The table names an object stream by its number.
			loc.e.off = loc.stm
		}
		f.putEntry(num, loc.e)
	}
	f.objs = make([]atomic.Pointer[syntax.Value], len(f.xref))
	f.resetFilled()
	if f.Resolve(f.trailer.Get("Root")).IsDict() {
		return nil
	}
	// No trailer names a usable catalog. Look for an object that says it
	// is one, the highest-numbered first; that includes objects inside
	// object streams, which the first search could not see.
	nums := make([]int64, 0, len(index))
	for num := range index {
		nums = append(nums, num)
	}
	sort.Slice(nums, func(i, j int) bool { return nums[i] > nums[j] })
	for _, num := range nums {
		if v := f.Object(num); v.Kind == syntax.Dict && v.Get("Type").IsName("Catalog") {
			f.trailer.List = append([]syntax.Value{{Kind: syntax.Name, Data: []byte("Root")}, {Kind: syntax.Ref, N: num}}, f.trailer.List...)
			return nil
		}
	}
	return malformed("the file has no document catalog")
}

// Rebuilt reports whether the file's cross-reference data was unusable
// and its objects were found by scanning, and how many of the objects
// found that way were defined more than once. Of several definitions
// the last in the file is read, as it is the newest when a file was
// saved in steps.
func (f *File) Rebuilt() (rebuilt bool, redefined int) { return f.rebuilt, f.redefined }

// putEntry sets the entry of object num, replacing any earlier one.
func (f *File) putEntry(num int64, e entry) {
	if num <= 0 || num >= int64(f.limits.MaxObjects) {
		return
	}
	f.growXRef(num)
	f.xref[num] = e
}

// recovery is the second index, built on first need.
type recovery struct {
	// lostTrailer is set when the encryption dictionary was found by
	// scanning because no trailer names it.
	lostTrailer bool
	once        sync.Once
	index       map[int64]located
	objs        sync.Map // object number -> *syntax.Value
}

// recovered returns object num as the scan finds it, for an object the
// cross-reference data gets wrong or does not mention. ok is false if
// the file does not contain the object.
func (f *File) recovered(num int64, depth int) (syntax.Value, bool) {
	f.rec.once.Do(func() {
		f.rec.index = f.locate(scanObjects(f.data, f.limits.MaxObjects), noResolve)
	})
	loc, ok := f.rec.index[num]
	if !ok {
		return syntax.Value{}, false
	}
	if cached, ok := f.rec.objs.Load(num); ok {
		return *cached.(*syntax.Value), true
	}
	var v syntax.Value
	var err error
	switch loc.e.kind {
	case entryOffset:
		if v, err = f.parseAt(loc.e.off, num); err == nil {
			v = f.decryptObject(v, num, loc.e.gen)
		}
	case entryCompressed:
		var s *objStm
		// The stream's own number is not needed again: it is identified
		// by where it is.
		if s, err = f.objStmAt(found{num: -1, off: loc.e.off}, depth); err == nil {
			v, err = s.object(f, num, int(loc.e.idx))
		}
	}
	if err != nil {
		return syntax.Value{}, false
	}
	f.Warn(gofastcore.SeverityInfo, gofastcore.WarnInputRepaired,
		"the cross-reference data is wrong about some objects; they were found by scanning the file")
	actual, _ := f.rec.objs.LoadOrStore(num, &v)
	return *actual.(*syntax.Value), true
}
