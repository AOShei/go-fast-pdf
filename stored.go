package gofastpdf

import (
	"bytes"
	"strings"
	"sync"
	"unicode/utf8"

	gofastcore "github.com/AOShei/go-fast-core"
	"github.com/AOShei/go-fast-core/xmlscan"
	"github.com/AOShei/go-fast-pdf/internal/charset"
	"github.com/AOShei/go-fast-pdf/internal/file"
	"github.com/AOShei/go-fast-pdf/internal/syntax"
)

// What a file holds and shows nowhere.
//
// A PDF can carry scripts, a packet of metadata in XML, entries of its
// own making in the document information, fields of a form that no page
// shows, and data after its end. Nobody who opens the file sees any of
// it, and a program that reads the extracted document reads it all the
// same. The shared model's rule is that such data is read as data, never
// run or followed, bounded, and marked with what it is in
// Attrs["stored"], with no display reason: where it is says that. What
// belongs to a page stands on its page; what belongs to the file is in
// one part of kind stored, after the pages.

const (
	// maxStoredBytes bounds the text of the stored part, as the text of
	// a page is bounded.
	maxStoredBytes = 16 << 20
	// maxScripts is the number of scripts kept for one page, and for
	// the document.
	maxScripts = 4096
	// maxVariables is the number of entries of the document information
	// and of properties of the metadata packet that are looked at.
	maxVariables = 1024
)

// script is a script that an action of the file would run. controls is
// the number of control characters removed from its name.
type script struct {
	name     string
	controls int64
	js       syntax.Value
}

// reader reads strings of the file for blocks that do not come from the
// content of a page, as long as there is room for them. rm counts the
// control characters removed from what it reads for the output.
type reader struct {
	f    *file.File
	room int
	rm   *removed
}

// text reads a text string, trimmed, and counts the control characters
// removed from it. ok is false when there is no room for it: a string
// is not longer decoded than three times its bytes.
func (r *reader) text(v syntax.Value) (s string, ok bool) {
	s, n, ok := r.textCounted(v)
	r.rm.add(int(n))
	return s, ok
}

// textCounted is text for a string that may not be kept: it returns the
// number of control characters removed, and does not count them.
func (r *reader) textCounted(v syntax.Value) (s string, controls int64, ok bool) {
	v = r.f.Resolve(v)
	if v.Kind != syntax.String || len(v.Data) == 0 {
		return "", 0, true
	}
	if 3*len(v.Data) > r.room {
		return "", 0, false
	}
	s, controls = cleanCounted(charset.Text(v.Data))
	s = strings.TrimSpace(s)
	r.room -= len(s)
	return s, controls, true
}

// source reads the text of a script, which the file has as a string or
// as a stream, and is a text string of the file in either. It is not
// trimmed. controls is the number of control characters removed, not
// counted: a script is kept once.
func (r *reader) source(v syntax.Value) (s string, controls int64, ok bool) {
	data, ok := r.bytes(v)
	if !ok || len(data) == 0 {
		return "", 0, ok
	}
	s, controls = cleanCounted(charset.Text(data))
	r.room -= len(s)
	return s, controls, true
}

// packet reads a metadata packet. It is XML, in UTF-8 unless it starts
// with the mark of UTF-16, and not a text string of the file.
func (r *reader) packet(v syntax.Value) (s string, ok bool) {
	data, ok := r.bytes(v)
	if !ok || len(data) == 0 {
		return "", ok
	}
	switch {
	case len(data) >= 2 && (data[0] == 0xFE && data[1] == 0xFF || data[0] == 0xFF && data[1] == 0xFE):
		s = charset.TextString(data) // UTF-16, which TextString reads by its mark
	case utf8.Valid(data):
		// The usual case, checked eight bytes at a time; ToValidUTF8
		// would look at every character.
		s = strings.TrimPrefix(string(data), "\uFEFF")
	default:
		s = strings.ToValidUTF8(strings.TrimPrefix(string(data), "\uFEFF"), "\uFFFD")
	}
	r.room -= len(s)
	return s, true
}

// bytes reads the bytes of a string or a stream, if there is room for
// three times as many.
func (r *reader) bytes(v syntax.Value) (data []byte, ok bool) {
	v = r.f.Resolve(v)
	switch v.Kind {
	case syntax.String:
		data = v.Data
	case syntax.Stream:
		if r.room <= 0 {
			return nil, false
		}
		decoded, err := r.f.Decode(v, []byte{}, int64(min(r.room, int(min(r.f.Limits().MaxStreamBytes, 1<<30)))))
		if err == file.ErrTruncated {
			return nil, false
		}
		data = decoded // what could be decoded, if the stream is damaged
	default:
		return nil, true
	}
	if 3*len(data) > r.room {
		return nil, false
	}
	return data, true
}

// scriptBlocks makes a block of every script: its text as it stands in
// the file, as code. A script that many actions share, or that is
// written out again for each of a thousand fields, is given once.
func scriptBlocks(r *reader, scripts []script, seen map[string]bool) (blocks []gofastcore.Block, cut bool) {
	for _, s := range scripts {
		src, controls, ok := r.source(s.js)
		if !ok {
			return blocks, true
		}
		if strings.TrimSpace(src) == "" || seen[src] {
			continue
		}
		seen[src] = true
		r.rm.add(int(controls))
		b := gofastcore.Block{Kind: gofastcore.KindCode, Text: src, Origin: gofastcore.OriginNative}
		b.Attrs = map[string]string{gofastcore.AttrStored: gofastcore.StoredMacro, gofastcore.AttrLanguage: "javascript"}
		if s.name != "" {
			b.Attrs[gofastcore.AttrName] = clipName(r.f, s.name)
			r.rm.add(int(s.controls))
		}
		blocks = append(blocks, b)
	}
	return blocks, false
}

// The entries of the document information that the format defines.
// Any other is of the file's own making.
var standardInfo = map[string]bool{
	"Title": true, "Author": true, "Subject": true, "Keywords": true, "Creator": true,
	"Producer": true, "CreationDate": true, "ModDate": true, "Trapped": true,
}

// stored reads what the file holds for itself and shows on no page.
// scripts are those of the actions of the catalog and of the form, and
// pk the metadata packet, read already for the document information.
//
// The control characters removed from what it reads are counted in rm.
func stored(f *file.File, catalog syntax.Value, md *gofastcore.Metadata, scripts []script, pk packet, rm *removed) (blocks []gofastcore.Block, cut bool) {
	r := &reader{f: f, room: maxStoredBytes, rm: rm}
	// What the document information says, and what has been said here
	// already: a value is given once. Programs write the same title in
	// the information and in three properties of the packet, and the
	// entries of their own making in both.
	said := []string{md.Title, md.Subject, md.Application, md.Extra["producer"]}
	said = append(append(said, md.Authors...), md.Keywords...)
	// controls is the number of control characters removed from value.
	variable := func(kind, name, value string, controls int64) {
		// The name is the file's too: a property of the packet can be
		// called anything, a control character included.
		name = clipName(f, strings.TrimSpace(cleanText(name, rm)))
		rm.add(int(controls))
		text := charset.Letters(strings.TrimSpace(name + ": " + value))
		blocks = append(blocks, gofastcore.Block{Kind: gofastcore.KindParagraph, Text: text, Origin: gofastcore.OriginNative,
			Attrs: map[string]string{gofastcore.AttrStored: kind}})
		said = append(said, value)
	}

	// --- scripts: those the document names, then those of its actions ---
	var named []script
	walkTree(f, f.Resolve(catalog.Get("Names")).Get("JavaScript"), "Names", func(key, val syntax.Value) bool {
		if len(named) >= maxScripts {
			cutDoc(f, boundScripts, 1, 0) // and on, to count them
			return true
		}
		if act := f.Resolve(val); act.IsDict() {
			var rm removed
			name := text(f, key, &rm)
			named = append(named, script{name: name, controls: rm.count(), js: act.Get("JS")})
		}
		return true
	})
	code, cut := scriptBlocks(r, append(named, scripts...), map[string]bool{})
	blocks = append(blocks, code...)
	if cut {
		return blocks, true
	}

	// --- entries of the document information that are the file's own ---
	if info := f.Info(); info.IsDict() {
		cutDoc(f, boundInfo, int64(info.Entries()-maxVariables), 0)
		for i := 0; i < info.Entries() && i < maxVariables; i++ {
			key, val := info.Entry(i)
			if standardInfo[string(key)] {
				continue
			}
			value, controls, ok := r.textCounted(val)
			if !ok {
				return blocks, true
			}
			name, n := cleanCounted(strings.ToValidUTF8(string(key), "\uFFFD"))
			if name = strings.TrimSpace(name); value != "" && name != "" && !mechanical(value) && !within(value, said) {
				variable(gofastcore.StoredDocumentVariable, name, value, controls+n)
			}
		}
	}

	// --- the metadata packet: what it says that the document
	// information does not ---
	if pk.read {
		// The packet takes its room here, after the scripts and the
		// information, as if it were read here.
		if !pk.whole || 3*pk.size > r.room {
			return blocks, true
		}
		r.room -= pk.size
		for _, p := range pk.props {
			var values []string
			var controls int64
			has := map[string]bool{}
			for _, v := range p.values {
				v, n := cleanCounted(v)
				if v = strings.TrimSpace(v); v != "" && !has[v] && !mechanical(v) && !within(v, said) {
					values = append(values, v)
					controls += n
					has[v] = true
				}
			}
			if len(values) > 0 {
				variable(gofastcore.StoredCustomXML, p.name, strings.Join(values, "; "), controls)
			}
		}
	}

	// --- fields of the form that no page shows, in the order of the
	// form; a field's name is the names from the top, as the walk down
	// finds them, whether or not the field points back at its parent ---
	type node struct {
		ref  syntax.Value
		name string
	}
	seen := map[int64]bool{}
	// The names of all the fields, as the data of an XFA form names
	// them, to tell which of its values the form has already.
	fieldNames := map[string]bool{}
	var stack []node
	push := func(list []syntax.Value, name string) {
		for i := len(list) - 1; i >= 0; i-- {
			stack = append(stack, node{list[i], name})
		}
	}
	push(f.Resolve(f.Resolve(catalog.Get("AcroForm")).Get("Fields")).Elems(), "")
	for n := 0; len(stack) > 0 && n < maxFields; n++ {
		at := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if num := file.RefNumber(at.ref); num != 0 {
			if seen[num] {
				continue
			}
			seen[num] = true
		}
		field := f.Resolve(at.ref)
		if !field.IsDict() {
			continue
		}
		name := at.name
		if t := f.Resolve(field.Get("T")); t.Kind == syntax.String {
			if own := strings.TrimSpace(cleanString(charset.Text(t.Data))); own != "" {
				name = strings.TrimPrefix(name+"."+own, ".")
			}
		}
		if kids := f.Resolve(field.Get("Kids")).Elems(); len(kids) > 0 {
			push(kids, name)
			continue
		}
		fieldNames[xfaName(name)] = true
		if f.Resolve(field.Get("Subtype")).IsName("Widget") || field.Has("Rect") {
			continue // it has a place on a page, and is read there
		}
		value, ok := fieldValue(f, field, r.text)
		if !ok {
			return blocks, true
		}
		choices, ok := fieldChoices(f, field, r.text)
		if !ok {
			return blocks, true
		}
		for _, x := range []extra{value, choices} {
			if x.block.Text == "" {
				continue
			}
			b := x.block
			b.Origin = gofastcore.OriginNative
			b.Attrs[gofastcore.AttrStored] = gofastcore.StoredFormField
			if !field.Has("Parent") {
				b.Attrs[gofastcore.AttrField] = clipName(f, name)
			}
			blocks = append(blocks, b)
			rm.add(int(x.controls))
		}
	}
	if len(stack) > 0 {
		cutOnce(f, boundFields, "the form", 1, 0)
	}

	// --- the data of an XFA form ---
	data, cut := xfaData(r, f.Resolve(catalog.Get("AcroForm")).Get("XFA"), fieldNames)
	blocks = append(blocks, data...)
	if cut {
		return blocks, true
	}

	// --- what follows the end of the file ---
	if text, _, ok := afterEnd(f.AfterEnd()); text != "" {
		if 3*len(text) > r.room || !ok {
			return blocks, true
		}
		r.room -= len(text)
		blocks = append(blocks, gofastcore.Block{Kind: gofastcore.KindParagraph, Text: text, Origin: gofastcore.OriginNative,
			Attrs: map[string]string{gofastcore.AttrStored: gofastcore.StoredAfterEnd}})
	}
	return blocks, false
}

// afterEnd reads the bytes that follow the last %%EOF of a file. Spaces,
// line ends and zero bytes are what a file is padded with, and are no
// data. Of anything else, n is how many bytes follow the end, and text
// is what they say when they are text: UTF-8, or else Latin-1 with no
// control character but tabs and line ends. Bytes of another kind (an
// appended file, a signature) are not text: text is empty, and only
// their number is told. ok is false when the text is longer than the
// stored part may hold.
func afterEnd(tail []byte) (text string, n int, ok bool) {
	data := bytes.Trim(tail, " \t\r\n\f\x00")
	if len(data) == 0 {
		return "", 0, true
	}
	n = len(tail)
	if len(data) > maxStoredBytes {
		return "", n, false
	}
	control := func(r rune) bool { return r < 0x20 && r != '\t' && r != '\n' && r != '\r' || r >= 0x7F && r < 0xA0 }
	switch {
	case utf8.Valid(data) && !bytes.ContainsFunc(data, control):
		text = string(data)
	case latin1Text(data):
		runes := make([]rune, len(data))
		for i, c := range data {
			runes[i] = rune(c)
		}
		text = string(runes)
	default:
		return "", n, true
	}
	return strings.TrimSpace(cleanString(text)), n, true
}

// latin1Text reports whether bytes read as Latin-1 are text: no control
// character but tabs and line ends.
func latin1Text(data []byte) bool {
	for _, c := range data {
		if c < 0x20 && c != '\t' && c != '\n' && c != '\r' || c >= 0x7F && c < 0xA0 {
			return false
		}
	}
	return true
}

// xfaName is the name of a field of the form as the data of an XFA form
// would name it: without the index that XFA puts after each part of the
// name ("form1[0]"), and without the parts that stand for subforms
// without a name ("#subform[0]"), which have no place in the data.
func xfaName(name string) string {
	parts := strings.Split(name, ".")
	out := parts[:0]
	for _, p := range parts {
		if i := strings.IndexByte(p, '['); i > 0 && strings.HasSuffix(p, "]") {
			p = p[:i]
		}
		if p != "" && !strings.HasPrefix(p, "#") {
			out = append(out, p)
		}
	}
	return strings.Join(out, ".")
}

// xfaData reads the values of an XFA form: the data packet of the form
// (xfa:datasets), which holds what was filled in. A viewer other than
// Adobe's shows none of it. A value whose field the form also has as an
// ordinary field, as a static XFA form does, is left out: it is read
// there. Each value is a block named by the path of its element.
//
// The data is read as XML and nothing more. The template, which says
// how the form looks and what its fields are called on the screen, is
// not read.
func xfaData(r *reader, v syntax.Value, named map[string]bool) (blocks []gofastcore.Block, cut bool) {
	v = r.f.Resolve(v)
	var packet syntax.Value
	switch v.Kind {
	case syntax.Stream:
		packet = v // one XDP document with every packet
	case syntax.Array:
		el := v.Elems()
		for i := 0; i+1 < len(el); i += 2 {
			if name := r.f.Resolve(el[i]); name.Kind == syntax.String && string(name.Data) == "datasets" {
				packet = r.f.Resolve(el[i+1])
				break
			}
		}
	}
	if packet.Kind != syntax.Stream {
		return nil, false
	}
	src, ok := r.packet(packet)
	if !ok {
		return nil, true
	}
	return xfaValues(src, named, cutter(r.f), r.rm)
}

// xfaValues reads the values of the data packet of an XFA form, which is
// in src, alone or in a whole XDP document.
//
// what a bound of the reading leaves out is counted by cutf, and the
// control characters removed from what is kept by rm.
func xfaValues(src string, named map[string]bool, cutf cutFunc, rm *removed) (blocks []gofastcore.Block, cut bool) {
	x := &xmlScan{s: src, cut: cutf}
	defer x.close()
	// Find xfa:data inside xfa:datasets.
	for inDatasets := false; ; {
		t := x.next()
		if t.kind == tokEnd {
			return nil, false
		}
		if t.kind == tokOpen && !t.closed {
			if local(t.name) == "datasets" {
				inDatasets = true
			} else if inDatasets && local(t.name) == "data" {
				break
			}
		}
	}

	// The elements under xfa:data. An element with elements in it is a
	// group; one without is a field, whose text is its value. A field
	// whose value is a list has an element "value" for each item, and
	// one whose value is rich text has XHTML in it.
	type frame struct {
		name     string
		text     []string
		controls int64 // removed from text
		group    bool  // it has elements other than "value"
		values   bool  // it has "value" elements
	}
	var stack []frame
	rich := 0 // depth inside XHTML
	path := func() string {
		names := make([]string, len(stack))
		for i, fr := range stack {
			names[i] = fr.name
		}
		return strings.Join(names, ".")
	}
	for len(blocks) < maxFields {
		t := x.next()
		switch t.kind {
		case tokEnd:
			return blocks, false
		case tokText:
			if len(stack) > 0 {
				s, n := cleanCounted(t.text)
				if s = strings.TrimSpace(s); s != "" {
					if top := &stack[len(stack)-1]; len(top.text) < maxXFAPieces {
						top.text = append(top.text, s)
						top.controls += n
					} else {
						cutf(boundXFAPieces, "", 1, 0)
					}
				}
			}
		case tokOpen:
			name := local(t.name)
			switch {
			case rich > 0:
				if !t.closed {
					rich++
				}
			case name == "body" && xhtml(t.attrs):
				if !t.closed {
					rich = 1
				}
			case t.closed:
				if len(stack) > 0 && name != "value" {
					stack[len(stack)-1].group = true
				}
			default:
				if len(stack) > 0 {
					if name == "value" {
						stack[len(stack)-1].values = true
					} else {
						stack[len(stack)-1].group = true
					}
				}
				if len(stack) >= maxXFADepth {
					// Nested beyond what a form has.
					cutf(boundXFADepth, "", 1, 0)
					return blocks, false
				}
				stack = append(stack, frame{name: name})
			}
		case tokClose:
			if rich > 0 {
				rich--
				continue
			}
			if len(stack) == 0 {
				return blocks, false // the end of xfa:data
			}
			top := stack[len(stack)-1]
			if top.name == "value" && len(stack) > 1 {
				// An item of a list: the value of the field above it.
				stack = stack[:len(stack)-1]
				parent := &stack[len(stack)-1]
				parent.text = append(parent.text, strings.Join(top.text, " "))
				parent.controls += top.controls
				continue
			}
			field := path()
			stack = stack[:len(stack)-1]
			if top.group || len(top.text) == 0 || named[field] {
				continue
			}
			sep := " "
			if top.values {
				sep = "\n"
			}
			text := charset.Letters(strings.Join(top.text, sep))
			if text == "" {
				continue
			}
			blocks = append(blocks, gofastcore.Block{Kind: gofastcore.KindParagraph, Text: text, Origin: gofastcore.OriginNative,
				Attrs: map[string]string{
					gofastcore.AttrStored: gofastcore.StoredFormField, gofastcore.AttrField: x.name(cleanText(field, rm))}})
			rm.add(int(top.controls))
		}
	}
	return blocks, true
}

// xhtml reports whether attributes name the namespace of XHTML.
func xhtml(attrs []xmlAttr) bool {
	for _, a := range attrs {
		if (a.name == "xmlns" || prefix(a.name) == "xmlns") && strings.Contains(a.value, "xhtml") {
			return true
		}
	}
	return false
}

// mechanical reports whether a value is one that programs write for
// themselves and that says nothing in words: a yes or no, a number, a
// version, a point in time, an identifier. Anything with words in it is
// kept, whatever property it is under: that is where text would be put
// for a model to read.
func mechanical(v string) bool {
	switch strings.ToLower(v) {
	case "true", "false", "yes", "no":
		return true
	}
	if strings.HasPrefix(v, "D:") && len(v) >= 6 && strings.Trim(v[2:6], "0123456789") == "" {
		return true // a date as PDF writes it
	}
	digits := false
	for _, c := range v {
		switch {
		case c >= '0' && c <= '9':
			digits = true
		case strings.ContainsRune(".,:+-/ ", c):
		default:
			return identifier(v)
		}
	}
	return digits
}

// identifier reports whether a value is a number that a program gave
// the file, or a point in time.
func identifier(v string) bool {
	for _, prefix := range []string{"uuid:", "urn:uuid:", "xmp.iid:", "xmp.did:", "xmp.id:", "adobe:docid:"} {
		if strings.HasPrefix(v, prefix) {
			return true
		}
	}
	// A date: 2026-11-19, with or without a time after it.
	if len(v) >= 10 && len(v) <= 35 && v[4] == '-' && v[7] == '-' && !strings.ContainsAny(v, " \n") {
		digits := 0
		for _, c := range v[:10] {
			if c >= '0' && c <= '9' {
				digits++
			}
		}
		return digits == 8
	}
	return false
}

// within reports whether a value says nothing that the document
// information does not: it is found in one of its strings, or it is a
// list, by commas or semicolons, whose every item is (the keywords of a
// packet as one string, beside the list of them).
func within(v string, known []string) bool {
	in := func(v string) bool {
		for _, k := range known {
			if k != "" && strings.Contains(k, v) {
				return true
			}
		}
		return false
	}
	if in(v) {
		return true
	}
	if !strings.ContainsAny(v, ",;") {
		return false
	}
	for item := range strings.FieldsFuncSeq(v, func(r rune) bool { return r == ',' || r == ';' }) {
		if item = strings.TrimSpace(item); item != "" && !in(item) {
			return false
		}
	}
	return true
}

// property is one property of the metadata packet with its values.
type property struct {
	name   string
	values []string
	// ns is the namespace of the name, by the declaration of its prefix
	// in the packet: "" when there is none.
	ns string
}

// Properties of the packet that hold what programs keep for themselves:
// identifiers, a history of saves, a picture of the first page.
var packetSkipped = map[string]bool{
	"DocumentID": true, "InstanceID": true, "OriginalDocumentID": true, "VersionID": true, "RenditionClass": true,
	"History": true, "DerivedFrom": true, "ManagedFrom": true, "Manifest": true, "Ingredients": true, "Pantry": true,
	"Thumbnails": true, "DocumentAncestors": true, "ICCProfile": true,
	"format": true, "PDFVersion": true, "part": true, "conformance": true, "amd": true, "corr": true, "Trapped": true,
}

// packetProperties reads the properties of a metadata packet (XMP): the
// attributes and the elements of each rdf:Description, with the text
// that is in them, under the names the packet writes. A packet is XML of
// a simple and regular kind, and it is read here as such and no more:
// tags, attributes, text, comments, CDATA and the five entities of XML,
// by the scanner of go-fast-core (xmlScan). No schema, no entity of the
// packet's own making, nothing fetched. A packet that is not well formed
// is read on through the fault, as far as its properties can be told.
//
// what a bound of the reading leaves out is counted by cutf.
func packetProperties(src string, cutf cutFunc) (out []property) {
	x := &xmlScan{s: src, cut: cutf}
	defer x.close()
	total := 0 // values of all the properties
	inDescription := false
	// The namespaces of the prefixes, as the elements declare them. A
	// packet declares each prefix once, or the same way each time: one
	// map for the packet does.
	spaces := make([][2]string, 0, 8) // prefix and namespace; a packet has a few
	declare := func(attrs []xmlAttr) {
		for _, a := range attrs {
			if prefix(a.name) == "xmlns" && len(spaces) < maxVariables {
				spaces = append(spaces, [2]string{local(a.name), a.value})
			}
		}
	}
	space := func(name string) string {
		p := prefix(name)
		for i := len(spaces) - 1; i >= 0 && p != ""; i-- {
			if spaces[i][0] == p {
				return spaces[i][1] // the last declaration of the prefix
			}
		}
		return ""
	}
	for {
		t := x.next()
		switch t.kind {
		case tokEnd:
			return out
		case tokOpen:
			declare(t.attrs)
			if local(t.name) == "Description" {
				inDescription = !t.closed
				for _, a := range t.attrs {
					if prefix(a.name) == "rdf" || prefix(a.name) == "xmlns" || a.name == "xmlns" || prefix(a.name) == "xml" || packetSkipped[local(a.name)] {
						continue
					}
					if v := strings.TrimSpace(a.value); v != "" {
						out = append(out, property{name: a.name, values: []string{v}, ns: space(a.name)})
					}
				}
				continue
			}
			if !inDescription || t.closed {
				continue
			}
			// A property: the text in it, however it is nested, a value
			// for each element that has text (an item of a list), text
			// and CDATA side by side being one.
			p := property{name: t.name, ns: space(t.name)}
			skip := packetSkipped[local(t.name)]
			// The text of the element, which is joined only when there
			// is more than one piece of it.
			var value string
			var more strings.Builder
			pieces := 0
			flush := func() {
				if pieces > 1 {
					value = more.String()
					more.Reset()
				}
				if v := strings.TrimSpace(value); v != "" && !skip {
					switch {
					case total >= maxXMPTotal:
						cutf(boundXMPTotal, "", 1, 0)
					case len(p.values) >= maxXMPValues:
						cutf(boundXMPValues, "", 1, 0)
					default:
						p.values = append(p.values, v)
						total++
					}
				}
				value, pieces = "", 0
			}
			for level := 1; level > 0; {
				in := x.next()
				switch in.kind {
				case tokEnd:
					return out
				case tokOpen:
					declare(in.attrs)
					flush()
					if !in.closed {
						level++
					}
				case tokClose:
					flush()
					level--
				case tokText:
					switch {
					case skip:
					case pieces == 0:
						value = in.text
					case more.Len() < maxStoredBytes:
						if pieces == 1 {
							more.WriteString(value)
						}
						more.WriteString(in.text)
					}
					pieces++
				}
			}
			if len(p.values) > 0 {
				// Past the bound they are counted, to the end of the
				// packet, whose room bounds them.
				if len(out) >= maxVariables {
					cutf(boundXMPProps, "", 1, 0)
				} else {
					out = append(out, p)
				}
			}
		case tokClose:
			switch local(t.name) {
			case "Description":
				inDescription = false
			case "RDF":
				// The metadata ends here. What follows is the blank space
				// a packet keeps to be edited in place, kilobytes of it,
				// which would take longer to look at than the rest.
				return out
			}
		}
	}
}

func local(name string) string {
	if i := strings.IndexByte(name, ':'); i >= 0 {
		return name[i+1:]
	}
	return name
}

func prefix(name string) string {
	if i := strings.IndexByte(name, ':'); i >= 0 {
		return name[:i]
	}
	return ""
}

// The kinds of what xmlScan finds.
const (
	tokEnd   = iota // the end of the text, or a fault
	tokOpen         // a start tag, closed if it ends in />
	tokClose        // an end tag
	tokText         // text, with its entities read
)

type xmlAttr struct{ name, value string }

type xmlToken struct {
	kind   int
	name   string
	closed bool
	attrs  []xmlAttr
	text   string
}

// xmlScan reads the tags and the text of XML, one at a time: the
// metadata packet and the data of an XFA form. The reading is that of
// go-fast-core's scanner (xmlscan), which every go-fast library reads
// XML with; what is here turns its tokens into those the two readers
// are written for: names as the file writes them, with their prefixes,
// a start tag that says whether it is also the end of its element, and
// text in one piece.
//
// The scanner reads on through markup that is not well formed, as a
// lenient reader does (xmlscan.Options.Recover): a packet is XML of a
// simple and regular kind that programs write by hand, and its
// properties are read as far as they can be told.
type xmlScan struct {
	s string
	// cut counts what a bound of the reading leaves out.
	cut cutFunc

	sc      *xmlscan.Scanner
	data    []byte
	started bool
	// skipEnd is set after a start tag that is also the end of its
	// element: the scanner gives an end for it, which is not handed on.
	skipEnd  bool
	attrsCut int
	text     []byte
	// held is the token the scanner is at when it was read ahead of a
	// text, 0 for none.
	held xmlscan.Kind
}

// xmlScanOptions are the options the packets are read with. The text is
// UTF-8 already, whatever a declaration in it says: it was decoded when
// the stream was read.
var xmlScanOptions = xmlscan.Options{Decoded: true, Recover: true, Spans: true, MaxAttrs: maxXMLAttrs}

// name holds a name to maxNameBytes, and counts a cut.
func (x *xmlScan) name(s string) string {
	c, cutBytes := clipped(s)
	if cutBytes > 0 {
		x.cut(boundName, nameID(c, s), 1, cutBytes)
	}
	return c
}

func (x *xmlScan) next() xmlToken {
	if !x.started {
		x.started = true
		x.data = []byte(x.s)
		x.sc = xmlScanners.Get().(*xmlscan.Scanner)
		x.sc.ResetBytes(x.data, &xmlScanOptions)
	}
	sc := x.sc
	if sc == nil {
		return xmlToken{kind: tokEnd} // closed
	}
	for {
		// The token read ahead of a text, or the next one.
		k := x.held
		if x.held = 0; k == 0 {
			k = sc.Next()
		}
		switch k {
		case xmlscan.EOF:
			x.close()
			return xmlToken{kind: tokEnd}
		case xmlscan.Start:
			// The names and the values are taken from the text as it
			// was given, where they stand in it as the scanner hands
			// them out: nothing is copied for them then.
			start, end := sc.Span()
			at, to := x.within(start, end)
			var t xmlToken
			t.kind = tokOpen
			t.name, at = x.str(sc.Name(), at, to)
			// A start tag that ends in "/>" is the whole element.
			if to >= 2 && x.data[to-2] == '/' {
				t.closed, x.skipEnd = true, true
			}
			if n := sc.NumAttr(); n > 0 {
				t.attrs = make([]xmlAttr, n)
				for i := range t.attrs {
					_, _, v := sc.AttrAt(i)
					a := &t.attrs[i]
					a.name, at = x.str(sc.AttrNameAt(i), at, to)
					a.value, at = x.str(v, at, to)
				}
			}
			// The attributes past the bound, which the scanner counts.
			if cut := sc.Stats.AttrsCut; cut > x.attrsCut {
				x.cut(boundXMLAttrs, "", int64(cut-x.attrsCut), 0)
				x.attrsCut = cut
			}
			return t
		case xmlscan.End:
			if x.skipEnd {
				x.skipEnd = false
				continue
			}
			start, end := sc.Span()
			at, to := x.within(start, end)
			name, _ := x.str(sc.Name(), at, to)
			return xmlToken{kind: tokClose, name: name}
		case xmlscan.Text:
			// The scanner hands long text over in pieces: they are one
			// text here. A CDATA section is a text of its own, as it is
			// written apart.
			start, end := sc.Span()
			at, to := x.within(start, end)
			first, _ := x.str(sc.Text(), at, to)
			pieces := 1
			for section := x.section(); !section; {
				if x.held = sc.Next(); x.held != xmlscan.Text || x.section() {
					break
				}
				x.held = 0
				if pieces == 1 {
					x.text = append(x.text[:0], first...)
				}
				x.text = append(x.text, sc.Text()...)
				pieces++
			}
			if pieces > 1 {
				first = string(x.text)
			}
			if strings.TrimSpace(first) == "" {
				continue
			}
			return xmlToken{kind: tokText, text: first}
		}
	}
}

// xmlScanners keeps scanners between packets: one holds the tables it
// grew, which a new one would make again for every packet.
var xmlScanners = sync.Pool{New: func() any { return new(xmlscan.Scanner) }}

// close ends the reading and gives the scanner back. What was handed
// out stays valid: it is of the text as it was given, or a copy.
func (x *xmlScan) close() {
	if x.sc != nil {
		x.sc.Release()
		xmlScanners.Put(x.sc)
		x.sc = nil
	}
}

// within returns the part of the text that a token the scanner says is
// at start to end can be in: none of it when the scanner says nothing
// usable.
func (x *xmlScan) within(start, end int64) (from, to int) {
	if start < 0 || end < start || end > int64(len(x.data)) {
		return 0, 0
	}
	return int(start), int(end)
}

// str returns b, which the scanner handed out, as a string: the stretch
// of the text as it was given where b stands in it between from and to,
// and where that stretch ends; or a copy of b, and from, when b is not
// there as it is (a value with a reference in it, a name that was
// mended).
func (x *xmlScan) str(b []byte, from, to int) (string, int) {
	if len(b) == 0 {
		return "", from
	}
	if from < to {
		if i := bytes.Index(x.data[from:to], b); i >= 0 {
			return x.s[from+i : from+i+len(b)], from + i + len(b)
		}
	}
	return string(b), from
}

// section reports whether the text the scanner is at is a CDATA section.
func (x *xmlScan) section() bool {
	start, _ := x.sc.Span()
	return start >= 0 && int(start)+9 <= len(x.data) && string(x.data[start:start+9]) == "<![CDATA["
}
