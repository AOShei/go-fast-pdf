package gofastpdf

import (
	"bytes"
	"context"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	gofastcore "github.com/AOShei/go-fast-core"
	"github.com/AOShei/go-fast-pdf/internal/charset"
	"github.com/AOShei/go-fast-pdf/internal/file"
	"github.com/AOShei/go-fast-pdf/internal/syntax"
)

// Bounds on what the document-level structures of a file may make this
// package do. Each is far above what documents have.
const (
	maxTreeNodes    = 1 << 16 // nodes of one name tree or number tree
	maxTreeEntries  = 1 << 20
	maxOutlineItems = 1 << 17
	maxOutlineDepth = 64
	maxAnnotations  = 1 << 14 // looked at on one page
	maxFields       = 1 << 16 // form fields looked at
	maxAttachments  = 1 << 12
	maxActionChain  = 32
)

// described is what a file says about itself, read once and without
// reading a page.
type described struct {
	meta    gofastcore.Metadata
	outline []gofastcore.OutlineItem
	// label gives the label of the page with an index from 0, nil when
	// the file has no page labels.
	label func(index int) string
	// active and external say that the catalog or the form fields hold
	// things that are meant to run or that point outside the file.
	active, external bool
	// attachments are the files attached to the document.
	attachments []attachment
	// invisible is set when the document information or the outline has
	// invisible characters.
	invisible bool
}

// describe reads what the document says about itself: its information,
// the labels of its pages, its outline, the files attached to it, and
// whether it holds things that are meant to run or that point outside
// it.
func (f *File) describe() *described {
	f.once.Do(func() {
		d, in := &f.desc, f.f
		catalog := in.Catalog()

		// --- document information ---
		md := &d.meta
		md.Encrypted = in.IsEncrypted()
		if info := in.Info(); info.IsDict() {
			md.Title = text(in, info.Get("Title"))
			if a := text(in, info.Get("Author")); a != "" {
				md.Authors = []string{a} // one field, one entry: it is not split
			}
			md.Subject = text(in, info.Get("Subject"))
			if k := text(in, info.Get("Keywords")); k != "" {
				md.Keywords = []string{k}
			}
			md.Application = text(in, info.Get("Creator"))
			if p := text(in, info.Get("Producer")); p != "" {
				md.Extra = map[string]string{"producer": p}
			}
			md.Created = parseDate(in.Resolve(info.Get("CreationDate")))
			md.Modified = parseDate(in.Resolve(info.Get("ModDate")))
		}
		md.Language = text(in, catalog.Get("Lang"))

		// --- page labels and outline ---
		d.label = pageLabels(in, catalog.Get("PageLabels"))
		if outlines := in.Resolve(catalog.Get("Outlines")); outlines.IsDict() {
			d.outline = outline(in, catalog, outlines, f.pages)
		}

		// --- things that run, and things outside the file ---
		var facts pageOut
		if open := in.Resolve(catalog.Get("OpenAction")); open.IsDict() {
			scanAction(in, open, &facts, 0)
		}
		scanActions(in, catalog.Get("AA"), &facts)
		names := in.Resolve(catalog.Get("Names"))
		if js := in.Resolve(names.Get("JavaScript")); js.IsDict() && (len(in.Resolve(js.Get("Names")).Elems()) > 0 || len(in.Resolve(js.Get("Kids")).Elems()) > 0) {
			facts.active = true
		}
		scanFields(in, in.Resolve(catalog.Get("AcroForm")).Get("Fields"), &facts)
		d.active, d.external = facts.active, facts.external

		// --- attached files ---
		walkTree(in, names.Get("EmbeddedFiles"), "Names", func(key, val syntax.Value) bool {
			d.attachments = append(d.attachments, attachment{name: text(in, key), spec: val})
			return len(d.attachments) < maxAttachments
		})

		d.invisible = invisibleInMetadata(&gofastcore.Document{Metadata: d.meta, Outline: d.outline})
	})
	return &f.desc
}

// attach makes a child document of every attached file of list, those
// of the catalog first and then those of the pages, each once.
func (st *state) attach(ctx context.Context, doc *gofastcore.Document, list []attachment) {
	seen := map[int64]bool{}
	for i := range list {
		if ctx.Err() != nil || len(doc.Children) >= maxAttachments {
			break
		}
		a := &list[i]
		if n := file.RefNumber(a.spec); n != 0 {
			if seen[n] {
				continue // attached to a page and listed in the catalog
			}
			seen[n] = true
		}
		if child := st.child(ctx, a); child != nil {
			doc.Children = append(doc.Children, child)
		}
	}
}

// text returns a text string of the file, cleaned for the shared model.
func text(f *file.File, v syntax.Value) string {
	if v = f.Resolve(v); v.Kind != syntax.String {
		return ""
	}
	data := v.Data
	if !(len(data) >= 2 && (data[0] == 0xFE && data[1] == 0xFF || data[0] == 0xFF && data[1] == 0xFE)) {
		// Zero bytes at the end are what a C program leaves there. In
		// UTF-16 they are found after decoding.
		data = bytes.TrimRight(data, "\x00")
	}
	return cleanString(charset.TextString(data))
}

// cleanString makes a string from outside the pages fit the shared
// model's text rules: no control characters but line breaks and tabs.
// Zero bytes at the end are what C programs leave there and are
// dropped; a carriage return is a line break; any other control
// character becomes U+FFFD.
func cleanString(s string) string {
	if !utf8.ValidString(s) {
		s = strings.ToValidUTF8(s, "\uFFFD")
	}
	s = strings.TrimRight(s, "\x00")
	clean := true
	for i := 0; i < len(s); i++ {
		if c := s[i]; c < 0x20 && c != '\n' && c != '\t' || c == 0x7F || c == 0xC2 {
			clean = false
			break
		}
	}
	if clean {
		return s
	}
	var sb strings.Builder
	sb.Grow(len(s))
	for i, r := range s {
		switch {
		case r == '\r':
			if i+1 < len(s) && s[i+1] == '\n' {
				continue
			}
			sb.WriteByte('\n')
		case r == '\n' || r == '\t':
			sb.WriteRune(r)
		case r < 0x20 || r >= 0x7F && r <= 0x9F:
			sb.WriteRune(utf8.RuneError)
		default:
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

// parseDate reads a PDF date: D:YYYYMMDDHHmmSSOHH'mm', of which
// everything after the year may be missing. It returns the zero time
// for anything else.
func parseDate(v syntax.Value) time.Time {
	if v.Kind != syntax.String {
		return time.Time{}
	}
	s := strings.TrimSpace(charset.TextString(v.Data))
	s = strings.TrimPrefix(s, "D:")
	num := func(n int, def int) (int, bool) {
		if len(s) < n {
			return def, false
		}
		v := 0
		for i := 0; i < n; i++ {
			if s[i] < '0' || s[i] > '9' {
				return def, false
			}
			v = v*10 + int(s[i]-'0')
		}
		s = s[n:]
		return v, true
	}
	year, ok := num(4, 0)
	if !ok || year < 1 {
		return time.Time{}
	}
	month, day, hour, minute, second := 1, 1, 0, 0, 0
	if month, ok = num(2, 1); ok {
		if day, ok = num(2, 1); ok {
			if hour, ok = num(2, 0); ok {
				if minute, ok = num(2, 0); ok {
					second, _ = num(2, 0)
				}
			}
		}
	}
	if month < 1 || month > 12 || day < 1 || day > 31 || hour > 23 || minute > 59 || second > 60 {
		return time.Time{}
	}
	loc := time.UTC
	if len(s) > 0 && (s[0] == '+' || s[0] == '-') {
		sign := 1
		if s[0] == '-' {
			sign = -1
		}
		s = s[1:]
		if h, ok := num(2, 0); ok && h <= 23 {
			s = strings.TrimPrefix(s, "'")
			mm, _ := num(2, 0)
			if off := sign * (h*3600 + min(mm, 59)*60); off != 0 {
				loc = time.FixedZone("", off)
			}
		}
	}
	t := time.Date(year, time.Month(month), day, hour, minute, second, 0, loc)
	if t.Day() != day {
		return time.Time{} // the 31st of a month that has 30 days
	}
	return t
}

// walkTree calls fn for every entry of a name tree (leaf is "Names") or
// a number tree ("Nums"), in the order of the tree, until fn returns
// false. A tree that refers back to itself, or is larger than any real
// one, is cut short.
func walkTree(f *file.File, root syntax.Value, leaf string, fn func(key, val syntax.Value) bool) {
	seen := map[int64]bool{}
	nodes, entries := 0, 0
	var walk func(v syntax.Value, depth int) bool
	walk = func(v syntax.Value, depth int) bool {
		if n := file.RefNumber(v); n != 0 {
			if seen[n] {
				return true
			}
			seen[n] = true
		}
		node := f.Resolve(v)
		if nodes++; !node.IsDict() || nodes > maxTreeNodes || depth > 32 {
			return true
		}
		list := f.Resolve(node.Get(leaf)).Elems()
		for i := 0; i+1 < len(list); i += 2 {
			if entries++; entries > maxTreeEntries || !fn(f.Resolve(list[i]), list[i+1]) {
				return false
			}
		}
		for _, kid := range f.Resolve(node.Get("Kids")).Elems() {
			if !walk(kid, depth+1) {
				return false
			}
		}
		return true
	}
	walk(root, 0)
}

// findName looks one key up in a name tree, which is in the order of
// its keys: each node says which keys its branches hold, and the keys
// of a leaf are sorted. It follows one path and searches one leaf, and
// can miss a key in a tree that is not in order.
func findName(f *file.File, root syntax.Value, name string) (syntax.Value, bool) {
	node := f.Resolve(root)
	for depth := 0; depth < 32 && node.IsDict(); depth++ {
		if list := f.Resolve(node.Get("Names")).Elems(); len(list) > 1 {
			// The leaf holds key and value by turns.
			lo, hi := 0, len(list)/2
			for lo < hi {
				mid := (lo + hi) / 2
				key := f.Resolve(list[2*mid])
				switch {
				case key.Kind != syntax.String:
					return syntax.Value{}, false
				case string(key.Data) == name:
					return list[2*mid+1], true
				case string(key.Data) < name:
					lo = mid + 1
				default:
					hi = mid
				}
			}
			return syntax.Value{}, false
		}
		kids := f.Resolve(node.Get("Kids")).Elems()
		lo, hi := 0, len(kids)
		var next syntax.Value
		for lo < hi {
			mid := (lo + hi) / 2
			kid := f.Resolve(kids[mid])
			limits := f.Resolve(kid.Get("Limits")).Elems()
			if len(limits) != 2 {
				return syntax.Value{}, false
			}
			first, last := f.Resolve(limits[0]), f.Resolve(limits[1])
			switch {
			case first.Kind != syntax.String || last.Kind != syntax.String:
				return syntax.Value{}, false
			case name < string(first.Data):
				hi = mid
			case name > string(last.Data):
				lo = mid + 1
			default:
				next, lo = kid, hi
			}
		}
		if next.IsNull() {
			return syntax.Value{}, false
		}
		node = next
	}
	return syntax.Value{}, false
}

// pageLabels returns the function from a page's index, counted from 0,
// to its label, or nil if the document has no page labels.
func pageLabels(f *file.File, tree syntax.Value) func(index int) string {
	type span struct {
		from   int
		style  byte
		prefix string
		start  int
	}
	var spans []span
	walkTree(f, tree, "Nums", func(key, val syntax.Value) bool {
		from, ok := key.Integer()
		d := f.Resolve(val)
		if !ok || from < 0 || !d.IsDict() {
			return true
		}
		s := span{from: int(from), prefix: text(f, d.Get("P")), start: 1}
		if style := f.Resolve(d.Get("S")).NameString(); len(style) == 1 {
			s.style = style[0]
		}
		if st, ok := f.Resolve(d.Get("St")).Integer(); ok && st >= 1 && st < 1<<31 {
			s.start = int(st)
		}
		// The tree is in order of page index; one that is not is taken
		// up to where it goes wrong.
		if n := len(spans); n > 0 && spans[n-1].from >= s.from {
			return false
		}
		spans = append(spans, s)
		return true
	})
	if len(spans) == 0 {
		return nil
	}
	return func(index int) string {
		lo, hi := 0, len(spans)
		for lo < hi {
			if mid := (lo + hi) / 2; spans[mid].from <= index {
				lo = mid + 1
			} else {
				hi = mid
			}
		}
		if lo == 0 {
			return ""
		}
		s := &spans[lo-1]
		n := s.start + index - s.from
		switch s.style {
		case 'D':
			return s.prefix + strconv.Itoa(n)
		case 'R':
			return s.prefix + roman(n)
		case 'r':
			return s.prefix + strings.ToLower(roman(n))
		case 'A':
			return s.prefix + letters(n, 'A')
		case 'a':
			return s.prefix + letters(n, 'a')
		}
		return s.prefix
	}
}

func roman(n int) string {
	if n < 1 || n > 1<<20 {
		return strconv.Itoa(n)
	}
	var sb strings.Builder
	for ; n >= 1000 && sb.Len() < 64; n -= 1000 {
		sb.WriteByte('M')
	}
	n %= 1000
	for _, p := range []struct {
		v int
		s string
	}{{900, "CM"}, {500, "D"}, {400, "CD"}, {100, "C"}, {90, "XC"}, {50, "L"}, {40, "XL"}, {10, "X"}, {9, "IX"}, {5, "V"}, {4, "IV"}, {1, "I"}} {
		for n >= p.v {
			sb.WriteString(p.s)
			n -= p.v
		}
	}
	return sb.String()
}

// letters returns the label of the letter styles: A to Z, then AA to
// ZZ, and so on.
func letters(n int, first byte) string {
	if n < 1 {
		return ""
	}
	return strings.Repeat(string(first+byte((n-1)%26)), min((n-1)/26+1, 64))
}

// outline reads the document's bookmarks.
func outline(f *file.File, catalog, outlines syntax.Value, pages []file.Page) []gofastcore.OutlineItem {
	// Page object numbers, to find the page a destination points at.
	byObj := make(map[int64]int, len(pages))
	for i := range pages {
		if pages[i].Obj != 0 {
			if _, dup := byObj[pages[i].Obj]; !dup {
				byObj[pages[i].Obj] = i
			}
		}
	}
	// Named destinations are in a name tree, or, in the form of PDF
	// 1.1, in a dictionary. A tree is in order, so a name is found
	// along one path; a tree that is not in order is read whole, once,
	// when a name is not found that way.
	dests := f.Resolve(catalog.Get("Names")).Get("Dests")
	var all map[string]syntax.Value
	lookup := func(name string) syntax.Value {
		if all == nil {
			if v, ok := findName(f, dests, name); ok {
				return v
			}
			all = map[string]syntax.Value{}
			walkTree(f, dests, "Names", func(key, val syntax.Value) bool {
				if key.Kind == syntax.String {
					if _, dup := all[string(key.Data)]; !dup {
						all[string(key.Data)] = val
					}
				}
				return true
			})
		}
		if v, ok := all[name]; ok {
			return v
		}
		return f.Resolve(catalog.Get("Dests")).Get(name)
	}
	target := func(item syntax.Value) (part int, y float64) {
		dest := f.Resolve(item.Get("Dest"))
		if dest.IsNull() {
			if a := f.Resolve(item.Get("A")); a.Get("S").IsName("GoTo") {
				dest = f.Resolve(a.Get("D"))
			}
		}
		for hops := 0; hops < 4; hops++ {
			switch dest.Kind {
			case syntax.Name, syntax.String:
				dest = f.Resolve(lookup(string(dest.Data)))
				continue
			case syntax.Dict:
				dest = f.Resolve(dest.Get("D"))
				continue
			}
			break
		}
		if dest.Kind != syntax.Array || len(dest.List) == 0 {
			return 0, 0
		}
		index := -1
		if n := file.RefNumber(dest.List[0]); n != 0 {
			if i, ok := byObj[n]; ok {
				index = i
			}
		} else if n, ok := f.Resolve(dest.List[0]).Integer(); ok && n >= 0 && n < int64(len(pages)) {
			index = int(n) // a page index, as some programs write it
		}
		if index < 0 {
			return 0, 0
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
			return p.Number, 0
		}
		m, _, h := p.Display()
		y = m[1]*left + m[3]*top + m[5]
		return p.Number, min(max(y, 0), h)
	}

	var out []gofastcore.OutlineItem
	seen := map[int64]bool{}
	type entry struct {
		ref   syntax.Value
		level int
	}
	stack := []entry{{outlines.Get("First"), 1}}
	for len(stack) > 0 && len(out) < maxOutlineItems {
		e := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if n := file.RefNumber(e.ref); n != 0 {
			if seen[n] {
				continue // the list leads back into itself
			}
			seen[n] = true
		}
		item := f.Resolve(e.ref)
		if !item.IsDict() {
			continue
		}
		part, y := target(item)
		out = append(out, gofastcore.OutlineItem{Title: text(f, item.Get("Title")), Level: e.level, Part: part, Y: y})
		// The entries under this one come before the one after it.
		if next := item.Get("Next"); !next.IsNull() {
			stack = append(stack, entry{next, e.level})
		}
		if first := item.Get("First"); !first.IsNull() && e.level < maxOutlineDepth {
			stack = append(stack, entry{first, e.level + 1})
		}
	}
	return out
}

// attachment is a file attached to the document or to a page.
type attachment struct {
	name string
	spec syntax.Value // the file specification
}

// attach makes the child document of one attached file. The file's data
// is decoded only when there is a child extractor to give it to.
func (st *state) child(ctx context.Context, a *attachment) *gofastcore.Document {
	f := st.f.f
	spec := f.Resolve(a.spec)
	if !spec.IsDict() {
		return nil
	}
	name := a.name
	for _, key := range []string{"UF", "F", "Unix", "Mac", "DOS"} {
		if s := text(f, spec.Get(key)); s != "" {
			name = s
			break
		}
	}
	ef := f.Resolve(spec.Get("EF"))
	var stream syntax.Value
	for _, key := range []string{"UF", "F", "Unix", "Mac", "DOS"} {
		if stream = f.Resolve(ef.Get(key)); stream.Kind == syntax.Stream {
			break
		}
	}
	if stream.Kind != syntax.Stream {
		return nil // a reference to a file that is not in the document
	}
	mime := ""
	if sub := f.Resolve(stream.Get("Subtype")); sub.Kind == syntax.Name {
		mime = cleanString(strings.ToValidUTF8(sub.NameString(), "�"))
	}
	size, _ := f.Resolve(f.Resolve(stream.Get("Params")).Get("Size")).Integer()
	if size < 0 {
		size = 0
	}

	opts := &st.f.opts
	if opts.Child == nil || int64(opts.Depth) >= gofastcore.ResolveLimit(int64(opts.Limits.MaxDepth), gofastcore.DefaultMaxDepth) {
		// Listed, not extracted: nothing is decoded.
		return opts.ExtractChild(ctx, name, mime, bytes.NewReader(nil), size)
	}
	failed := func(why string) *gofastcore.Document {
		d := gofastcore.New("")
		d.Source.Name, d.Source.MIME, d.Source.Size = name, mime, size
		d.Warn(gofastcore.SeverityError, gofastcore.WarnChildFailed, why)
		return d
	}
	if st.budget <= 0 {
		st.attachmentsCut = true
		return failed("the limit on decoded bytes was reached before this file")
	}
	data, err := f.Decode(stream, []byte{}, min(st.budget, f.Limits().MaxStreamBytes))
	st.budget -= int64(len(data))
	if err != nil {
		if err == file.ErrTruncated {
			st.attachmentsCut = true
		}
		return failed(cleanString("the attached file could not be decoded: " + err.Error()))
	}
	return opts.ExtractChild(ctx, name, mime, bytes.NewReader(data), int64(len(data)))
}

// annotations looks at the annotations of a page for two things: files
// attached to the page, and actions. The annotations themselves are not
// extracted.
func (e *Extractor) annotations(f *file.File, p *file.Page, out *pageOut) {
	dict := p.Dict()
	scanActions(f, dict.Get("AA"), out)
	annots := f.Resolve(dict.Get("Annots")).Elems()
	if len(annots) > maxAnnotations {
		annots = annots[:maxAnnotations]
	}
	for _, ref := range annots {
		a := f.Resolve(ref)
		if !a.IsDict() {
			continue
		}
		if f.Resolve(a.Get("Subtype")).IsName("FileAttachment") {
			if len(out.attachments) < maxAttachments {
				out.attachments = append(out.attachments, attachment{spec: a.Get("FS")})
			}
			continue
		}
		if act := a.Get("A"); !act.IsNull() {
			scanAction(f, f.Resolve(act), out, 0)
		}
		if aa := a.Get("AA"); !aa.IsNull() {
			scanActions(f, aa, out)
		}
	}
}

// scanActions looks at a dictionary of additional actions (/AA): each
// of its values is an action.
func scanActions(f *file.File, v syntax.Value, out *pageOut) {
	aa := f.Resolve(v)
	if !aa.IsDict() {
		return
	}
	for i := 0; i < aa.Entries() && i < 64; i++ {
		_, act := aa.Entry(i)
		scanAction(f, f.Resolve(act), out, 0)
	}
}

// scanAction notes what an action, and the actions chained to it, would
// do. Nothing is done and no script is read.
//
// JavaScript and Launch make something run. Launch, GoToR, SubmitForm
// and ImportData reach outside the document. A URI action is an
// ordinary link and is neither.
func scanAction(f *file.File, act syntax.Value, out *pageOut, depth int) {
	if !act.IsDict() || depth > maxActionChain {
		return
	}
	switch f.Resolve(act.Get("S")).NameString() {
	case "JavaScript":
		out.active = true
	case "Launch":
		out.active = true
		if act.Has("F") || act.Has("Win") || act.Has("Mac") || act.Has("Unix") {
			out.external = true
		}
	case "GoToR", "SubmitForm", "ImportData":
		out.external = true
	}
	switch next := f.Resolve(act.Get("Next")); next.Kind {
	case syntax.Dict:
		scanAction(f, next, out, depth+1)
	case syntax.Array:
		for i, n := range next.List {
			if i >= maxActionChain {
				break
			}
			scanAction(f, f.Resolve(n), out, depth+1)
		}
	}
}

// scanFields looks at the actions of the document's form fields.
func scanFields(f *file.File, fields syntax.Value, out *pageOut) {
	seen := map[int64]bool{}
	stack := f.Resolve(fields).Elems()
	for n := 0; len(stack) > 0 && n < maxFields; n++ {
		ref := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if num := file.RefNumber(ref); num != 0 {
			if seen[num] {
				continue
			}
			seen[num] = true
		}
		field := f.Resolve(ref)
		if !field.IsDict() {
			continue
		}
		if act := field.Get("A"); !act.IsNull() {
			scanAction(f, f.Resolve(act), out, 0)
		}
		scanActions(f, field.Get("AA"), out)
		stack = append(stack, f.Resolve(field.Get("Kids")).Elems()...)
	}
}

// invisibleInMetadata reports whether a string of the document that is
// not on a page has characters that display as nothing yet can carry a
// message. Such strings cannot be flagged as blocks are.
func invisibleInMetadata(doc *gofastcore.Document) bool {
	md := &doc.Metadata
	check := []string{md.Title, md.Subject, md.Language, md.Application}
	check = append(check, md.Authors...)
	check = append(check, md.Keywords...)
	for _, v := range md.Extra {
		check = append(check, v)
	}
	for i := range doc.Outline {
		check = append(check, doc.Outline[i].Title)
	}
	for i := range doc.Parts {
		check = append(check, doc.Parts[i].Name)
	}
	for _, s := range check {
		if gofastcore.InvisibleChars(s) != nil {
			return true
		}
	}
	return false
}
