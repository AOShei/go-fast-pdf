// Package file reads the structure of a PDF file: the cross-reference
// data, the objects, the streams and the page tree.
//
// A File is immutable after Open and safe for use from many goroutines.
// It holds the whole input in memory and parses objects on first use.
package file

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"

	gofastcore "github.com/AOShei/go-fast-core"
	"github.com/AOShei/go-fast-pdf/internal/syntax"
)

// Limits bounds what a file can make the reader do. Every quantity that
// the file's content controls has one.
type Limits struct {
	// MaxStreamBytes is the largest size one stream may decode to. A
	// longer stream is cut there.
	MaxStreamBytes int64
	// MaxObjects is the highest object number accepted.
	MaxObjects int
	// MaxPages is the largest number of pages read from the page tree.
	MaxPages int
	// MaxNesting bounds the nesting of arrays and dictionaries.
	MaxNesting int
	// MaxXRefSections bounds the chain of cross-reference sections.
	MaxXRefSections int
	// ObjStmCacheBytes is the budget for decoded object streams.
	ObjStmCacheBytes int64
}

// DefaultLimits returns the limits used when none are given.
func DefaultLimits() Limits {
	return Limits{
		MaxStreamBytes:   64 << 20,
		MaxObjects:       4 << 20,
		MaxPages:         1 << 20,
		MaxNesting:       syntax.DefaultMaxDepth,
		MaxXRefSections:  1024,
		ObjStmCacheBytes: 64 << 20,
	}
}

func (l Limits) withDefaults() Limits {
	d := DefaultLimits()
	if l.MaxStreamBytes <= 0 {
		l.MaxStreamBytes = d.MaxStreamBytes
	}
	if l.MaxObjects <= 0 {
		l.MaxObjects = d.MaxObjects
	}
	if l.MaxPages <= 0 {
		l.MaxPages = d.MaxPages
	}
	if l.MaxNesting <= 0 {
		l.MaxNesting = d.MaxNesting
	}
	if l.MaxXRefSections <= 0 {
		l.MaxXRefSections = d.MaxXRefSections
	}
	if l.ObjStmCacheBytes <= 0 {
		l.ObjStmCacheBytes = d.ObjStmCacheBytes
	}
	return l
}

// Entry kinds of the cross-reference data.
const (
	// entryMissing: no cross-reference section mentions the object. It
	// is the zero value. The object may still be in the file; see
	// recovered.
	entryMissing = iota
	// entryFree: a section says the object is free, that is, deleted.
	entryFree
	entryOffset
	entryCompressed
)

// entry says where one object is: at a byte offset, or at an index
// inside an object stream.
type entry struct {
	off  int64 // byte offset, or the object stream's number
	idx  int32 // index inside the object stream
	gen  uint16
	kind uint8
}

// File is an opened PDF file.
type File struct {
	data    []byte
	limits  Limits
	xref    []entry
	trailer syntax.Value
	objs    []atomic.Pointer[syntax.Value]
	enc     *decrypter
	encObj  int64 // object number of the encryption dictionary, -1 if none
	stms    objStmCache
	pages   []Page
	rec     recovery

	mu       sync.Mutex
	warnings []gofastcore.Warning

	// Cache holds values derived from the file by other packages, such as
	// parsed fonts, keyed by them.
	Cache sync.Map
}

func malformed(format string, args ...any) error {
	return fmt.Errorf("%w: %s", gofastcore.ErrMalformed, fmt.Sprintf(format, args...))
}

// Open reads the cross-reference data and the page tree of a PDF held in
// memory. data must not be modified afterwards.
//
// A file whose cross-reference data cannot be used is read by finding
// its objects directly; such a file carries an input.repaired warning.
func Open(data []byte, limits Limits) (*File, error) {
	limits = limits.withDefaults()
	// Offsets count from the %PDF- header, which need not be the first
	// byte of the file. Some files with leading bytes count from the
	// start of the file all the same, so both are tried.
	header := headerOffset(data)
	bases := []int{header}
	if header != 0 {
		bases = append(bases, 0)
	}
	var first error
	for _, base := range bases {
		f := &File{data: data[base:], limits: limits, encObj: -1}
		err := f.load()
		if err == nil {
			return f.finish(), nil
		}
		if !errors.Is(err, gofastcore.ErrMalformed) {
			return nil, err // unsupported encryption and the like
		}
		if first == nil {
			first = err
		}
	}
	f := &File{data: data[header:], limits: limits, encObj: -1}
	if err := f.rebuild(); err != nil {
		if !errors.Is(err, gofastcore.ErrMalformed) {
			return nil, err
		}
		return nil, first
	}
	f.Warn(gofastcore.SeverityInfo, gofastcore.WarnInputRepaired,
		"the cross-reference data is unusable; the objects were found by scanning the file")
	return f.finish(), nil
}

// headerOffset returns the position of the %PDF- header if it is within
// the first kilobyte, and 0 otherwise.
func headerOffset(data []byte) int {
	if i := bytes.Index(data[:min(len(data), 1024)], []byte("%PDF-")); i > 0 {
		return i
	}
	return 0
}

// load reads the cross-reference data, sets up decryption and checks
// that the catalog can be reached.
func (f *File) load() error {
	if err := f.readXRef(); err != nil {
		return err
	}
	f.objs = make([]atomic.Pointer[syntax.Value], len(f.xref))
	if err := f.setupEncryption(); err != nil {
		return err
	}
	if catalog := f.Resolve(f.trailer.Get("Root")); !catalog.IsDict() {
		return malformed("the document catalog is missing or is a %s", catalog.Kind)
	}
	return nil
}

func (f *File) finish() *File {
	f.pages = f.flattenPages(f.Resolve(f.trailer.Get("Root")).Get("Pages"))
	return f
}

// Warn records a problem that concerns the file as a whole.
func (f *File) Warn(severity gofastcore.Severity, code, message string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	// A damaged file can repeat one problem thousands of times.
	for _, w := range f.warnings {
		if w.Code == code && w.Message == message {
			return
		}
	}
	if len(f.warnings) < 100 {
		f.warnings = append(f.warnings, gofastcore.Warning{Code: code, Severity: severity, Message: message})
	}
}

// Warnings returns the problems recorded so far.
func (f *File) Warnings() []gofastcore.Warning {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]gofastcore.Warning(nil), f.warnings...)
}

// Limits returns the limits the file was opened with.
func (f *File) Limits() Limits { return f.limits }

// Size returns the size of the input in bytes.
func (f *File) Size() int { return len(f.data) }

// Trailer returns the trailer dictionary of the newest revision.
func (f *File) Trailer() syntax.Value { return f.trailer }

// IsEncrypted reports whether the file has an encryption dictionary.
func (f *File) IsEncrypted() bool { return f.trailer.Has("Encrypt") }

// Info returns the document information dictionary, or null.
func (f *File) Info() syntax.Value {
	if info := f.Resolve(f.trailer.Get("Info")); info.IsDict() {
		return info
	}
	return syntax.Value{}
}

// --- cross-reference data ---

func (f *File) readXRef() error {
	// The offset of the newest section follows the last startxref.
	tail := f.data
	if len(tail) > 2048 {
		tail = tail[len(tail)-2048:]
	}
	i := bytes.LastIndex(tail, []byte("startxref"))
	if i < 0 {
		// Not near the end: bytes were appended after %%EOF.
		tail = f.data
		if i = bytes.LastIndex(tail, []byte("startxref")); i < 0 {
			return malformed("startxref not found")
		}
	}
	pos := len(f.data) - len(tail) + i + len("startxref")
	v, end := syntax.ParseNumber(f.data, syntax.SkipSpace(f.data, pos))
	if v.Kind != syntax.Int || end == syntax.SkipSpace(f.data, pos) {
		return malformed("no offset after startxref")
	}

	seen := map[int64]bool{}
	offset := v.N
	for sections := 0; ; sections++ {
		if offset < 0 || offset >= int64(len(f.data)) {
			if sections == 0 {
				return malformed("cross-reference offset %d is outside the file", offset)
			}
			break
		}
		if seen[offset] || sections >= f.limits.MaxXRefSections {
			break // a loop in the /Prev chain, or an absurd one
		}
		seen[offset] = true

		var trailer syntax.Value
		var err error
		at := syntax.SkipSpace(f.data, int(offset))
		if syntax.HasPrefixAt(f.data, at, "xref") {
			trailer, err = f.readXRefTable(at + len("xref"))
		} else {
			trailer, err = f.readXRefStream(offset)
		}
		if err != nil {
			if sections == 0 {
				return err
			}
			// An older revision is unreadable; the newer ones stand.
			f.Warn(gofastcore.SeverityWarning, "pdf.xref_section_unreadable", err.Error())
			break
		}
		if sections == 0 {
			f.trailer = trailer
		}
		prev, ok := trailer.Get("Prev").Integer()
		if !ok {
			break
		}
		offset = prev
	}
	if f.trailer.Kind != syntax.Dict {
		return malformed("no trailer dictionary")
	}
	return nil
}

// setEntry records where object num is, unless a newer section already
// said so.
func (f *File) setEntry(num int64, e entry) {
	if num < 0 || num >= int64(f.limits.MaxObjects) {
		return
	}
	f.growXRef(num)
	if f.xref[num].kind == entryMissing {
		f.xref[num] = e
	}
}

// growXRef makes the table long enough to hold object num. New entries
// are entryMissing, the zero value. The capacity at least doubles when it
// has to grow, so filling a table costs time in proportion to its size,
// in whatever order the objects come.
func (f *File) growXRef(num int64) {
	if num < int64(len(f.xref)) {
		return
	}
	if num < int64(cap(f.xref)) {
		f.xref = f.xref[:num+1]
		return
	}
	grown := make([]entry, num+1, max(int(num)+1, 2*cap(f.xref)))
	copy(grown, f.xref)
	f.xref = grown
}

// readXRefTable reads a classic cross-reference table, positioned after
// the xref keyword, and the trailer dictionary that follows it. Entries
// are read as tokens, so their exact width does not matter.
func (f *File) readXRefTable(pos int) (syntax.Value, error) {
	type pending struct {
		num int64
		e   entry
	}
	var entries []pending
	p := syntax.NewParser(f.data, pos, f.limits.MaxNesting)
	for {
		first, err := p.Object()
		if err != nil {
			return syntax.Value{}, malformed("cross-reference table ends without a trailer")
		}
		if first.Kind == syntax.Keyword && first.Equal("trailer") {
			break
		}
		count, err := p.Object()
		if first.Kind != syntax.Int || err != nil || count.Kind != syntax.Int || count.N < 0 {
			return syntax.Value{}, malformed("cross-reference subsection header is not two integers")
		}
		for i := int64(0); i < count.N; i++ {
			start := syntax.SkipSpace(f.data, p.Pos())
			if start >= len(f.data) {
				return syntax.Value{}, malformed("cross-reference table ends without a trailer")
			}
			p.SetPos(start)
			off, err1 := p.Object()
			gen, err2 := p.Object()
			kind, err3 := p.Object()
			if err1 != nil || err2 != nil || err3 != nil || off.Kind != syntax.Int || gen.Kind != syntax.Int ||
				kind.Kind != syntax.Keyword || !(kind.Equal("n") || kind.Equal("f")) {
				// One damaged entry costs at most one object, not the
				// file. Each entry is a line: continue with the next. The
				// object stays unmentioned, so it can still be found by
				// scanning.
				end := start
				for end < len(f.data) && f.data[end] != '\n' && f.data[end] != '\r' {
					end++
				}
				p.SetPos(end)
				f.Warn(gofastcore.SeverityWarning, "pdf.xref_entry_unreadable",
					fmt.Sprintf("cross-reference entry for object %d is not readable", first.N+i))
				continue
			}
			e := entry{kind: entryFree, gen: uint16(gen.N)}
			if kind.Equal("n") {
				e.kind, e.off = entryOffset, off.N
			}
			if len(entries) < f.limits.MaxObjects {
				entries = append(entries, pending{first.N + i, e})
			}
		}
	}
	trailer, err := p.Object()
	if err != nil || trailer.Kind != syntax.Dict {
		return syntax.Value{}, malformed("the trailer is not a dictionary")
	}

	// In a hybrid-reference file the trailer names a cross-reference
	// stream that holds the objects kept in object streams. It ranks
	// after this table's objects and before its free entries, which some
	// producers write for those very objects, and before /Prev.
	for _, pe := range entries {
		if pe.e.kind == entryOffset {
			f.setEntry(pe.num, pe.e)
		}
	}
	if at, ok := trailer.Get("XRefStm").Integer(); ok {
		if _, err := f.readXRefStream(at); err != nil {
			f.Warn(gofastcore.SeverityWarning, "pdf.xref_section_unreadable", err.Error())
		}
	}
	for _, pe := range entries {
		if pe.e.kind == entryFree {
			f.setEntry(pe.num, pe.e)
		}
	}
	return trailer, nil
}

// readXRefStream reads a cross-reference stream at offset. Its
// dictionary doubles as the trailer.
func (f *File) readXRefStream(offset int64) (syntax.Value, error) {
	stm, err := f.parseAt(offset, -1)
	if err != nil {
		return syntax.Value{}, malformed("no cross-reference section at offset %d: %v", offset, err)
	}
	if stm.Kind != syntax.Stream {
		return syntax.Value{}, malformed("object at offset %d is not a cross-reference stream", offset)
	}
	// A cross-reference stream is never encrypted, and f.enc is not set
	// up yet in any case.
	data, err := f.decode(stm, nil, f.limits.MaxStreamBytes, false, 0)
	if err != nil && len(data) == 0 {
		return syntax.Value{}, malformed("cross-reference stream: %v", err)
	}

	var w [3]int
	widths := stm.Get("W").Elems()
	if len(widths) < 3 {
		return syntax.Value{}, malformed("cross-reference stream has no usable /W")
	}
	for i := range w {
		n, ok := widths[i].Integer()
		if !ok || n < 0 || n > 8 {
			return syntax.Value{}, malformed("cross-reference stream field width %d", n)
		}
		w[i] = int(n)
	}
	rowLen := w[0] + w[1] + w[2]
	if rowLen == 0 {
		return syntax.Value{}, malformed("cross-reference stream has empty rows")
	}

	// /Index lists pairs of first object number and count; the default
	// is one range from 0 to /Size.
	index := stm.Get("Index").Elems()
	if len(index) < 2 {
		size, _ := stm.Get("Size").Integer()
		index = []syntax.Value{{Kind: syntax.Int}, {Kind: syntax.Int, N: size}}
	}
	field := func(b []byte) int64 {
		var v int64
		for _, c := range b {
			v = v<<8 | int64(c)
		}
		return v
	}
	for i := 0; i+1 < len(index); i += 2 {
		first, ok1 := index[i].Integer()
		count, ok2 := index[i+1].Integer()
		if !ok1 || !ok2 || first < 0 {
			break
		}
		for k := int64(0); k < count && len(data) >= rowLen; k++ {
			row := data[:rowLen]
			data = data[rowLen:]
			typ := int64(1) // the default when the type field is absent
			if w[0] > 0 {
				typ = field(row[:w[0]])
			}
			f2, f3 := field(row[w[0]:w[0]+w[1]]), field(row[w[0]+w[1]:])
			var e entry
			switch typ {
			case 0:
				e = entry{kind: entryFree}
			case 1:
				e = entry{kind: entryOffset, off: f2, gen: uint16(f3)}
			case 2:
				e = entry{kind: entryCompressed, off: f2, idx: int32(f3)}
			default:
				continue // a type this reader does not know: treat as absent
			}
			f.setEntry(first+k, e)
		}
	}
	return stm.AsDict(), nil
}

// --- objects ---

// parseAt reads the indirect object at a byte offset. want is the object
// number expected there, or -1 for any.
func (f *File) parseAt(offset int64, want int64) (syntax.Value, error) {
	if offset < 0 || offset >= int64(len(f.data)) {
		return syntax.Value{}, fmt.Errorf("offset %d is outside the file", offset)
	}
	p := syntax.NewParser(f.data, int(offset), f.limits.MaxNesting)
	num, err1 := p.Object()
	gen, err2 := p.Object()
	kw, err3 := p.Object()
	if err1 != nil || err2 != nil || err3 != nil || num.Kind != syntax.Int || gen.Kind != syntax.Int ||
		kw.Kind != syntax.Keyword || !kw.Equal("obj") {
		return syntax.Value{}, fmt.Errorf("no object at offset %d", offset)
	}
	if want >= 0 && num.N != want {
		return syntax.Value{}, fmt.Errorf("offset %d holds object %d, not %d", offset, num.N, want)
	}
	v, err := p.Object()
	if err == io.EOF || errors.Is(err, syntax.ErrSyntax) {
		return syntax.Value{}, nil // "N G obj endobj" and the like: null
	}
	if err != nil {
		return syntax.Value{}, err
	}
	if v.Kind == syntax.Keyword {
		return syntax.Value{}, nil // endobj directly: null
	}
	if v.Kind == syntax.Dict {
		if start, ok := syntax.StreamData(f.data, p.Pos()); ok {
			v = syntax.Value{Kind: syntax.Stream, List: v.List, N: int64(start), Obj: uint32(num.N), Gen: uint16(gen.N)}
		}
	}
	return v, nil
}

// Object returns object number num. A free or missing object is null, as
// the specification says. An object that cannot be read is null too, and
// the reason is recorded as a warning.
func (f *File) Object(num int64) syntax.Value { return f.object(num, 0) }

func (f *File) object(num int64, depth int) syntax.Value {
	if num <= 0 || depth > 8 {
		return syntax.Value{}
	}
	if num >= int64(len(f.xref)) {
		// Beyond what the cross-reference data covers.
		v, _ := f.recovered(num, depth)
		return v
	}
	if cached := f.objs[num].Load(); cached != nil {
		return *cached
	}
	var v syntax.Value
	var err error
	found := true
	switch e := f.xref[num]; e.kind {
	case entryOffset:
		v, err = f.parseAt(e.off, num)
		if err == nil {
			v = f.decryptObject(v, num, e.gen)
		}
	case entryCompressed:
		v, err = f.fromObjStm(e.off, num, int(e.idx), depth)
	case entryMissing:
		found = false
	}
	if err != nil || !found {
		// The table is wrong about this object, or silent. The object
		// may still be in the file.
		if rv, ok := f.recovered(num, depth); ok {
			v, err = rv, nil
		}
	}
	if err != nil {
		f.Warn(gofastcore.SeverityWarning, "pdf.object_unreadable", fmt.Sprintf("object %d: %v", num, err))
		v = syntax.Value{}
	}
	// Two goroutines may parse the same object; both get the same result
	// and one of the copies is dropped.
	f.objs[num].CompareAndSwap(nil, &v)
	return *f.objs[num].Load()
}

// decryptObject decrypts the strings of an object read from the file
// itself. Objects from object streams are not encrypted individually.
func (f *File) decryptObject(v syntax.Value, num int64, gen uint16) syntax.Value {
	if f.enc == nil || num == f.encObj || v.Get("Type").IsName("XRef") {
		return v
	}
	return f.enc.decryptStrings(v, uint32(num), gen)
}

// Resolve follows indirect references until it reaches a value that is
// not one. A chain that does not end resolves to null.
func (f *File) Resolve(v syntax.Value) syntax.Value { return f.resolve(v, 0) }

func (f *File) resolve(v syntax.Value, depth int) syntax.Value {
	for hops := 0; v.Kind == syntax.Ref; hops++ {
		if hops >= 32 {
			return syntax.Value{}
		}
		v = f.object(v.N, depth)
	}
	return v
}

// RefNumber returns the object number v refers to, or 0 if v is not a
// reference. Object 0 is never a real object.
func RefNumber(v syntax.Value) int64 {
	if v.Kind == syntax.Ref {
		return v.N
	}
	return 0
}
