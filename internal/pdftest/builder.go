// Package pdftest holds the test harness for go-fast-pdf: a builder that
// assembles PDF files in memory, the pdftotext oracle, the accuracy scorer,
// the golden-file helper and the known-failing register.
//
// It is imported by tests only.
package pdftest

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"sort"
	"strings"
)

// Builder assembles a PDF file from object bodies. Object numbers start at
// 1 and are returned by Add; object 1 is the catalog unless Root is set.
//
// The zero configuration writes a classic cross-reference table. The
// builder does no validation: tests use it to produce malformed files as
// readily as valid ones.
type Builder struct {
	// Version is the header version, "1.7" if empty.
	Version string
	// Prefix is written before the %PDF- header (junk that shifts offsets).
	Prefix []byte
	// Root is the catalog's object number, 1 if zero.
	Root int
	// Trailer holds extra trailer entries, e.g. "/Info 6 0 R".
	Trailer string
	// XRefStream writes a cross-reference stream instead of a table.
	XRefStream bool
	// ObjStm packs every non-stream object into one object stream. It
	// implies XRefStream.
	ObjStm bool
	// EntryEOL is the two-byte end of each classic xref entry, " \n" if
	// empty. One-byte and three-byte values produce 19- and 21-byte entries.
	EntryEOL string
	// XRefNoType writes the cross-reference stream with /W [0 4 2]: the
	// type field is absent and every entry defaults to type 1. Not valid
	// together with ObjStm.
	XRefNoType bool
	// XRefRaw stores the cross-reference stream without a filter.
	XRefRaw bool
	// XRefIndex replaces the computed /Index array body, for malformed
	// cases such as an odd number of elements.
	XRefIndex string

	revs []revision
	next int
}

type revision struct {
	objs map[int]string
	free []int
}

// New returns an empty Builder.
func New() *Builder {
	return &Builder{revs: []revision{{objs: map[int]string{}}}, next: 1}
}

func (b *Builder) cur() *revision { return &b.revs[len(b.revs)-1] }

// Add appends an object body and returns its object number.
func (b *Builder) Add(body string) int {
	n := b.Reserve()
	b.Set(n, body)
	return n
}

// Reserve allocates an object number to be filled in later with Set.
func (b *Builder) Reserve() int {
	n := b.next
	b.next++
	return n
}

// Set stores the body of object n in the current revision. In a later
// revision it replaces the earlier definition.
func (b *Builder) Set(n int, body string) {
	if n >= b.next {
		b.next = n + 1
	}
	b.cur().objs[n] = body
}

// Free marks object n as free in the current revision.
func (b *Builder) Free(n int) { b.cur().free = append(b.cur().free, n) }

// Update starts a new revision: everything added afterwards is written as
// an incremental update after the previous %%EOF.
func (b *Builder) Update() { b.revs = append(b.revs, revision{objs: map[int]string{}}) }

// AddStream adds a stream object with the given dictionary entries (no
// enclosing << >>, no /Length) and raw data.
func (b *Builder) AddStream(dict string, data []byte) int {
	return b.Add(Stream(dict, string(data)))
}

// AddFlate adds a stream whose data is compressed with FlateDecode.
func (b *Builder) AddFlate(dict string, data []byte) int {
	return b.Add(Stream(dict+" /Filter /FlateDecode", string(Deflate(data))))
}

// Stream formats a stream object body with a direct /Length.
func Stream(dict, data string) string {
	return fmt.Sprintf("<< %s /Length %d >>\nstream\n%s\nendstream", dict, len(data), data)
}

// Deflate compresses data in zlib format, as FlateDecode expects.
func Deflate(data []byte) []byte {
	var buf bytes.Buffer
	w := zlib.NewWriter(&buf)
	w.Write(data)
	w.Close()
	return buf.Bytes()
}

func isStream(body string) bool { return strings.Contains(body, "\nstream\n") }

// Bytes renders the file.
func (b *Builder) Bytes() []byte {
	var out bytes.Buffer
	out.Write(b.Prefix)
	version := b.Version
	if version == "" {
		version = "1.7"
	}
	fmt.Fprintf(&out, "%%PDF-%s\n%%\xe2\xe3\xcf\xd3\n", version)

	root := b.Root
	if root == 0 {
		root = 1
	}
	prev := -1
	for _, rev := range b.revs {
		if b.XRefStream || b.ObjStm {
			prev = b.writeStreamRevision(&out, rev, root, prev)
		} else {
			prev = b.writeTableRevision(&out, rev, root, prev)
		}
	}
	return out.Bytes()
}

func sortedKeys(m map[int]string) []int {
	keys := make([]int, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	return keys
}

// Offsets are relative to the %PDF- header only when there is no prefix;
// with a prefix they are deliberately left relative to the start of the
// header, which is what a file with leading junk looks like.
func (b *Builder) offset(out *bytes.Buffer) int { return out.Len() - len(b.Prefix) }

func (b *Builder) writeTableRevision(out *bytes.Buffer, rev revision, root, prev int) int {
	offs := map[int]int{}
	for _, n := range sortedKeys(rev.objs) {
		offs[n] = b.offset(out)
		fmt.Fprintf(out, "%d 0 obj\n%s\nendobj\n", n, rev.objs[n])
	}
	eol := b.EntryEOL
	if eol == "" {
		eol = " \n"
	}
	xref := b.offset(out)
	out.WriteString("xref\n")

	type entry struct {
		off  int
		free bool
	}
	entries := map[int]entry{}
	if prev < 0 {
		entries[0] = entry{free: true}
	}
	for n, off := range offs {
		entries[n] = entry{off: off}
	}
	for _, n := range rev.free {
		entries[n] = entry{free: true}
	}
	nums := make([]int, 0, len(entries))
	for n := range entries {
		nums = append(nums, n)
	}
	sort.Ints(nums)
	// Subsections of consecutive object numbers.
	for i := 0; i < len(nums); {
		j := i
		for j+1 < len(nums) && nums[j+1] == nums[j]+1 {
			j++
		}
		fmt.Fprintf(out, "%d %d\n", nums[i], j-i+1)
		for k := i; k <= j; k++ {
			e := entries[nums[k]]
			if e.free {
				gen := 0
				if nums[k] == 0 {
					gen = 65535
				}
				fmt.Fprintf(out, "%010d %05d f%s", 0, gen, eol)
			} else {
				fmt.Fprintf(out, "%010d %05d n%s", e.off, 0, eol)
			}
		}
		i = j + 1
	}
	fmt.Fprintf(out, "trailer\n<< /Size %d /Root %d 0 R", b.next, root)
	if prev >= 0 {
		fmt.Fprintf(out, " /Prev %d", prev)
	}
	if b.Trailer != "" {
		out.WriteString(" " + b.Trailer)
	}
	fmt.Fprintf(out, " >>\nstartxref\n%d\n%%%%EOF\n", xref)
	return xref
}

func (b *Builder) writeStreamRevision(out *bytes.Buffer, rev revision, root, prev int) int {
	type entry struct{ typ, f2, f3 int }
	entries := map[int]entry{}
	if prev < 0 {
		entries[0] = entry{0, 0, 65535}
	}
	for _, n := range rev.free {
		entries[n] = entry{0, 0, 0}
	}

	var packed []int
	for _, n := range sortedKeys(rev.objs) {
		body := rev.objs[n]
		if b.ObjStm && !isStream(body) {
			packed = append(packed, n)
			continue
		}
		entries[n] = entry{1, b.offset(out), 0}
		fmt.Fprintf(out, "%d 0 obj\n%s\nendobj\n", n, body)
	}
	if len(packed) > 0 {
		var index, bodies bytes.Buffer
		for _, n := range packed {
			fmt.Fprintf(&index, "%d %d ", n, bodies.Len())
			bodies.WriteString(rev.objs[n])
			bodies.WriteByte('\n')
		}
		stm := b.Reserve()
		first := index.Len()
		data := Deflate(append(index.Bytes(), bodies.Bytes()...))
		entries[stm] = entry{1, b.offset(out), 0}
		fmt.Fprintf(out, "%d 0 obj\n%s\nendobj\n", stm,
			Stream(fmt.Sprintf("/Type /ObjStm /N %d /First %d /Filter /FlateDecode", len(packed), first), string(data)))
		for i, n := range packed {
			entries[n] = entry{2, stm, i}
		}
	}

	xrefNum := b.Reserve()
	xref := b.offset(out)
	entries[xrefNum] = entry{1, xref, 0}

	nums := make([]int, 0, len(entries))
	for n := range entries {
		nums = append(nums, n)
	}
	sort.Ints(nums)
	var index strings.Builder
	var rows bytes.Buffer
	for i := 0; i < len(nums); {
		j := i
		for j+1 < len(nums) && nums[j+1] == nums[j]+1 {
			j++
		}
		fmt.Fprintf(&index, "%d %d ", nums[i], j-i+1)
		for k := i; k <= j; k++ {
			e := entries[nums[k]]
			if !b.XRefNoType {
				rows.WriteByte(byte(e.typ))
			}
			rows.Write([]byte{byte(e.f2 >> 24), byte(e.f2 >> 16), byte(e.f2 >> 8), byte(e.f2), byte(e.f3 >> 8), byte(e.f3)})
		}
		i = j + 1
	}
	w, idx := "[1 4 2]", strings.TrimSpace(index.String())
	if b.XRefNoType {
		w = "[0 4 2]"
	}
	if b.XRefIndex != "" {
		idx = b.XRefIndex
	}
	dict := fmt.Sprintf("/Type /XRef /Size %d /Root %d 0 R /W %s /Index [%s]", b.next, root, w, idx)
	data := rows.Bytes()
	if !b.XRefRaw {
		dict += " /Filter /FlateDecode"
		data = Deflate(data)
	}
	if prev >= 0 {
		dict += fmt.Sprintf(" /Prev %d", prev)
	}
	if b.Trailer != "" {
		dict += " " + b.Trailer
	}
	fmt.Fprintf(out, "%d 0 obj\n%s\nendobj\nstartxref\n%d\n%%%%EOF\n", xrefNum, Stream(dict, string(data)), xref)
	return xref
}

// Standard font dictionaries used by the document helpers.
const (
	// FontHelvetica is a Core-14 font with WinAnsi encoding and no widths,
	// as most producers write it.
	FontHelvetica = "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>"
)

// PageSpec describes one page for Doc.
type PageSpec struct {
	// Content is the page's content stream.
	Content string
	// Resources replaces the default resource dictionary body (which maps
	// /F1 to the document font). Give the entries without << >>.
	Resources string
	// Extra holds further page dictionary entries, e.g. "/Rotate 90".
	Extra string
	// MediaBox replaces the default "[0 0 612 792]".
	MediaBox string
}

// DocOptions configures Doc.
type DocOptions struct {
	// Font is the dictionary of the font installed as /F1, FontHelvetica
	// if empty.
	Font string
	// Compress stores content streams with FlateDecode.
	Compress bool
	// Configure is called on the builder before the pages are added, so a
	// test can switch on object streams or add trailer entries.
	Configure func(*Builder)
	// Base is the builder to add the document to, a new one if nil. A test
	// that needs extra objects (a composite font, an XObject) adds them to
	// a builder first and refers to their numbers in Font or Resources.
	Base *Builder
}

// Doc builds a well-formed document with one page per spec and returns the
// builder, so that a test can add objects or damage it before rendering.
// Unless Configure adds objects first, object 1 is the catalog, 2 the page
// tree, 3 the font, and the pages follow as content stream then page.
func Doc(opts DocOptions, pages ...PageSpec) *Builder {
	b := opts.Base
	if b == nil {
		b = New()
	}
	if opts.Configure != nil {
		opts.Configure(b)
	}
	catalog, tree, font := b.Reserve(), b.Reserve(), b.Reserve()
	b.Root = catalog
	b.Set(catalog, fmt.Sprintf("<< /Type /Catalog /Pages %d 0 R >>", tree))
	if opts.Font == "" {
		opts.Font = FontHelvetica
	}
	b.Set(font, opts.Font)

	var kids []string
	for _, p := range pages {
		var content int
		if opts.Compress {
			content = b.AddFlate("", []byte(p.Content))
		} else {
			content = b.AddStream("", []byte(p.Content))
		}
		res := p.Resources
		if res == "" {
			res = fmt.Sprintf("/Font << /F1 %d 0 R >>", font)
		}
		box := p.MediaBox
		if box == "" {
			box = "[0 0 612 792]"
		}
		page := b.Add(fmt.Sprintf("<< /Type /Page /Parent %d 0 R /MediaBox %s /Resources << %s >> /Contents %d 0 R %s >>",
			tree, box, res, content, p.Extra))
		kids = append(kids, fmt.Sprintf("%d 0 R", page))
	}
	b.Set(tree, fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), len(pages)))
	return b
}

// OnePage builds a one-page document with the given content stream and
// the default font as /F1.
func OnePage(content string) []byte {
	return Doc(DocOptions{}, PageSpec{Content: content}).Bytes()
}

// TextPage returns a content stream that sets each line with Helvetica 12
// from the top of a US Letter page.
func TextPage(lines ...string) string {
	var sb strings.Builder
	sb.WriteString("BT /F1 12 Tf 72 720 Td 14 TL\n")
	for _, l := range lines {
		fmt.Fprintf(&sb, "(%s) Tj T*\n", EscapeString(l))
	}
	sb.WriteString("ET")
	return sb.String()
}

// EscapeString escapes s for use inside a PDF literal string.
func EscapeString(s string) string {
	return strings.NewReplacer(`\`, `\\`, `(`, `\(`, `)`, `\)`).Replace(s)
}
