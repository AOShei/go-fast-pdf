// Package file reads the structure of a PDF file: the cross-reference
// data, the objects, the streams and the page tree.
//
// A File is immutable after Open and safe for use from many goroutines.
// It holds the whole input in memory and parses objects on first use.
package file

import (
	"bytes"
	"crypto"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"unsafe"

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
	// ObjCacheBytes is the budget for parsed objects. When more than
	// that is kept, all of it is let go and the keeping starts afresh.
	ObjCacheBytes int64
}

// DefaultLimits returns the limits used when none are given.
func DefaultLimits() Limits {
	return Limits{
		MaxStreamBytes: 64 << 20,
		// The most indirect objects the format allows a file to have
		// (ISO 32000-1, Annex C).
		MaxObjects:       8388607,
		MaxPages:         1 << 20,
		MaxNesting:       syntax.DefaultMaxDepth,
		MaxXRefSections:  1024,
		ObjStmCacheBytes: 64 << 20,
		ObjCacheBytes:    4 << 20,
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
	if l.ObjCacheBytes <= 0 {
		l.ObjCacheBytes = d.ObjCacheBytes
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
	// kept is the estimated size of what objs holds, and letGo is set
	// while it is being emptied. filled are the numbers of the objects
	// held, so that emptying it clears those and not every slot of a
	// file of many objects.
	kept     atomic.Int64
	letGo    atomic.Bool
	filledMu sync.Mutex
	filled   []int64
	enc      *decrypter
	// password is what the caller gave to open an encrypted file, and
	// keys the private keys for a file encrypted for a certificate.
	password string
	keys     []crypto.Decrypter
	encObj   int64 // object number of the encryption dictionary, -1 if none
	stms     objStmCache
	pages    []Page
	rec      recovery
	// rebuilt and redefined describe a file read by scanning; see
	// Rebuilt.
	rebuilt   bool
	redefined int

	mu sync.Mutex
	// warnings are the problems of the file, by their code (Warn).
	warnings map[string]*codeWarnings
	counted  int
	// cuts counts what fixed bounds left out of the document (Cut).
	cuts Cuts

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
	return OpenPassword(data, limits, "")
}

// OpenPassword is Open for a file that may be encrypted. The password is
// tried as the user password and as the owner password; the empty user
// password, which opens most encrypted files, is always tried too.
//
// An encrypted file that the password does not open is
// ErrPasswordRequired when no password was given and ErrWrongPassword
// otherwise. Nothing is decrypted with a key that was not checked.
func OpenPassword(data []byte, limits Limits, password string) (*File, error) {
	return OpenKeys(data, limits, password, nil)
}

// OpenKeys is OpenPassword with private keys as well, for a file
// encrypted for a certificate (the public-key security handler). Such a
// file that none of the keys opens is ErrKeyRequired.
func OpenKeys(data []byte, limits Limits, password string, keys []crypto.Decrypter) (*File, error) {
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
		f := &File{data: data[base:], limits: limits, encObj: -1, password: password, keys: keys}
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
	f := &File{data: data[header:], limits: limits, encObj: -1, password: password, keys: keys}
	if err := f.rebuild(); err != nil {
		if !errors.Is(err, gofastcore.ErrMalformed) {
			return nil, err
		}
		return nil, fmt.Errorf("%w; reading the objects directly did not help: %v", first, err)
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
	f.filled = make([]int64, 0, min(len(f.xref), 256))
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

// A damaged file can have a problem in every object, so the problems of
// a file are kept by their code: the first maxPerCode of a code in the
// order of their messages, and a count of the others, which Warnings
// gives as one more warning of that code. Messages are counted once
// each, up to maxCounted messages over all codes.
const (
	maxPerCode = 8
	maxCounted = 1 << 20
)

// codeWarnings are the problems of one code.
type codeWarnings struct {
	kept     []gofastcore.Warning // in the order of their messages
	seen     map[uint64]bool      // the messages, by a hash of them
	severity gofastcore.Severity  // the highest of them
	uncount  bool                 // seen past maxCounted
}

// Warn records a problem that concerns the file as a whole. A message
// recorded already is not recorded again.
//
// Problems are found as objects are read, and several goroutines read
// objects in no fixed order. So that the result does not depend on that
// order, the problems kept of a code are not the first ones found but
// the first ones in the order of their messages.
func (f *File) Warn(severity gofastcore.Severity, code, message string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.warnings == nil {
		f.warnings = map[string]*codeWarnings{}
	}
	cw := f.warnings[code]
	if cw == nil {
		cw = &codeWarnings{seen: map[uint64]bool{}}
		f.warnings[code] = cw
	}
	h := fnv.New64a()
	h.Write([]byte(message))
	sum := h.Sum64()
	if cw.seen[sum] {
		return
	}
	if f.counted < maxCounted {
		cw.seen[sum] = true
		f.counted++
	} else {
		cw.uncount = true
	}
	cw.severity = max(cw.severity, severity)
	w := gofastcore.Warning{Code: code, Severity: severity, Message: message}
	i, _ := slices.BinarySearchFunc(cw.kept, w, func(a, b gofastcore.Warning) int { return strings.Compare(a.Message, b.Message) })
	switch {
	case len(cw.kept) < maxPerCode:
		cw.kept = slices.Insert(cw.kept, i, w)
	case i < maxPerCode:
		cw.kept = slices.Insert(cw.kept[:maxPerCode-1], i, w)
	}
}

// Warnings returns the problems recorded so far, in the order of their
// code and message: of each code, those kept and, when there were more,
// a warning that counts the others.
func (f *File) Warnings() []gofastcore.Warning {
	f.mu.Lock()
	defer f.mu.Unlock()
	codes := make([]string, 0, len(f.warnings))
	for code := range f.warnings {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	var out []gofastcore.Warning
	for _, code := range codes {
		cw := f.warnings[code]
		out = append(out, cw.kept...)
		if n := len(cw.seen) - len(cw.kept); n > 0 || cw.uncount {
			msg := fmt.Sprintf("%d more warnings with this code are not listed", n)
			if cw.uncount {
				msg = fmt.Sprintf("more than %d more warnings with this code are not listed", n)
			}
			out = append(out, gofastcore.Warning{Code: code, Severity: cw.severity, Message: msg})
		}
	}
	// What the bounds cut, among the warnings of its code.
	out = append(out, f.cuts.Warnings()...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Code < out[j].Code })
	return out
}

// Limits returns the limits the file was opened with.
func (f *File) Limits() Limits { return f.limits }

// Size returns the size of the input in bytes.
func (f *File) Size() int { return len(f.data) }

// Trailer returns the trailer dictionary of the newest revision.
func (f *File) Trailer() syntax.Value { return f.trailer }

// IsEncrypted reports whether the file has an encryption dictionary.
func (f *File) IsEncrypted() bool { return f.trailer.Has("Encrypt") }

// Catalog returns the document catalog, or null.
func (f *File) Catalog() syntax.Value {
	if c := f.Resolve(f.trailer.Get("Root")); c.IsDict() {
		return c
	}
	return syntax.Value{}
}

// Info returns the document information dictionary, or null.
func (f *File) Info() syntax.Value {
	if info := f.Resolve(f.trailer.Get("Info")); info.IsDict() {
		return info
	}
	return syntax.Value{}
}

// AfterEnd returns the bytes that follow the last %%EOF of the file and
// the end of its line: data after the file's logical end. An update
// added to a file ends in a %%EOF of its own, so only what follows the
// last one is after the end. nil when there is no %%EOF.
func (f *File) AfterEnd() []byte {
	i := bytes.LastIndex(f.data, []byte("%%EOF"))
	if i < 0 {
		return nil
	}
	tail := f.data[i+len("%%EOF"):]
	switch {
	case bytes.HasPrefix(tail, []byte("\r\n")):
		tail = tail[2:]
	case len(tail) > 0 && (tail[0] == '\n' || tail[0] == '\r'):
		tail = tail[1:]
	}
	return tail
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
	if f.objs[num].CompareAndSwap(nil, &v) {
		f.filledMu.Lock()
		f.filled = append(f.filled, num)
		f.filledMu.Unlock()
		// What is kept is bounded: a file of many pages has an object or
		// several for each, and they are not needed once the page is read.
		// When the budget is used up everything is let go; what is still
		// wanted is parsed again.
		if f.kept.Add(weigh(&v, 0)) > f.limits.ObjCacheBytes && f.letGo.CompareAndSwap(false, true) {
			f.filledMu.Lock()
			nums := f.filled
			f.filled = make([]int64, 0, len(nums))
			f.filledMu.Unlock()
			for _, n := range nums {
				f.objs[n].Store(nil)
			}
			f.kept.Store(0)
			f.letGo.Store(false)
		}
	}
	return v
}

// resetFilled forgets the objects held, for slots made afresh.
func (f *File) resetFilled() {
	f.filledMu.Lock()
	f.filled = f.filled[:0]
	f.filledMu.Unlock()
	f.kept.Store(0)
}

// weigh estimates the memory a parsed object takes.
func weigh(v *syntax.Value, depth int) int64 {
	return values(v, depth) * int64(unsafe.Sizeof(*v))
}

// values counts the values an object is made of.
func values(v *syntax.Value, depth int) int64 {
	n := int64(1 + len(v.List))
	if depth > 64 {
		return n
	}
	for i := range v.List {
		if len(v.List[i].List) > 0 {
			n += values(&v.List[i], depth+1) - 1
		}
	}
	return n
}

// decryptObject decrypts the strings of an object read from the file
// itself. Objects from object streams are not encrypted individually.
func (f *File) decryptObject(v syntax.Value, num int64, gen uint16) syntax.Value {
	if f.enc == nil || num == f.encObj || v.Get("Type").IsName("XRef") {
		return v
	}
	v, bad := f.enc.decryptStrings(v, uint32(num), gen)
	if bad > 0 {
		f.Warn(gofastcore.SeverityWarning, "pdf.string_undecryptable",
			"some encrypted strings are damaged and could not be decrypted; they are left empty")
	}
	return v
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
