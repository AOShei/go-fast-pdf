package gofastpdf

import (
	"bytes"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	gofastcore "github.com/AOShei/go-fast-core"
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

// script is a script that an action of the file would run.
type script struct {
	name string
	js   syntax.Value
}

// reader reads strings of the file for blocks that do not come from the
// content of a page, as long as there is room for them.
type reader struct {
	f    *file.File
	room int
}

// text reads a text string, trimmed. ok is false when there is no room
// for it: a string is not longer decoded than three times its bytes.
func (r *reader) text(v syntax.Value) (s string, ok bool) {
	v = r.f.Resolve(v)
	if v.Kind != syntax.String || len(v.Data) == 0 {
		return "", true
	}
	if 3*len(v.Data) > r.room {
		return "", false
	}
	s = strings.TrimSpace(cleanString(charset.TextString(v.Data)))
	r.room -= len(s)
	return s, true
}

// source reads the text of a script, which the file has as a string or
// as a stream, and is a text string of the file in either. It is not
// trimmed.
func (r *reader) source(v syntax.Value) (s string, ok bool) {
	data, ok := r.bytes(v)
	if !ok || len(data) == 0 {
		return "", ok
	}
	s = cleanString(charset.TextString(data))
	r.room -= len(s)
	return s, true
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
		src, ok := r.source(s.js)
		if !ok {
			return blocks, true
		}
		if strings.TrimSpace(src) == "" || seen[src] {
			continue
		}
		seen[src] = true
		b := gofastcore.Block{Kind: gofastcore.KindCode, Text: src, Origin: gofastcore.OriginNative}
		b.Attrs = map[string]string{gofastcore.AttrStored: gofastcore.StoredMacro, gofastcore.AttrLanguage: "javascript"}
		if s.name != "" {
			b.Attrs[gofastcore.AttrName] = clipName(s.name)
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
func stored(f *file.File, catalog syntax.Value, md *gofastcore.Metadata, scripts []script, pk packet) (blocks []gofastcore.Block, cut bool) {
	r := &reader{f: f, room: maxStoredBytes}
	// What the document information says, and what has been said here
	// already: a value is given once. Programs write the same title in
	// the information and in three properties of the packet, and the
	// entries of their own making in both.
	said := []string{md.Title, md.Subject, md.Application, md.Extra["producer"]}
	said = append(append(said, md.Authors...), md.Keywords...)
	variable := func(kind, name, value string) {
		// The name is the file's too: a property of the packet can be
		// called anything, a control character included.
		name = clipName(strings.TrimSpace(cleanString(name)))
		text := charset.Letters(strings.TrimSpace(name + ": " + value))
		blocks = append(blocks, gofastcore.Block{Kind: gofastcore.KindParagraph, Text: text, Origin: gofastcore.OriginNative,
			Attrs: map[string]string{gofastcore.AttrStored: kind}})
		said = append(said, value)
	}

	// --- scripts: those the document names, then those of its actions ---
	var named []script
	walkTree(f, f.Resolve(catalog.Get("Names")).Get("JavaScript"), "Names", func(key, val syntax.Value) bool {
		if act := f.Resolve(val); act.IsDict() {
			named = append(named, script{name: text(f, key), js: act.Get("JS")})
		}
		return len(named) < maxScripts
	})
	code, cut := scriptBlocks(r, append(named, scripts...), map[string]bool{})
	blocks = append(blocks, code...)
	if cut {
		return blocks, true
	}

	// --- entries of the document information that are the file's own ---
	if info := f.Info(); info.IsDict() {
		for i := 0; i < info.Entries() && i < maxVariables; i++ {
			key, val := info.Entry(i)
			if standardInfo[string(key)] {
				continue
			}
			value, ok := r.text(val)
			if !ok {
				return blocks, true
			}
			if name := clipName(strings.TrimSpace(cleanString(strings.ToValidUTF8(string(key), "\uFFFD")))); value != "" && name != "" && !mechanical(value) && !within(value, said) {
				variable(gofastcore.StoredDocumentVariable, name, value)
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
			for _, v := range p.values {
				if v = strings.TrimSpace(cleanString(v)); v != "" && !mechanical(v) && !within(v, said) && !slices.Contains(values, v) {
					values = append(values, v)
				}
			}
			if len(values) > 0 {
				variable(gofastcore.StoredCustomXML, p.name, strings.Join(values, "; "))
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
			if own := strings.TrimSpace(cleanString(charset.TextString(t.Data))); own != "" {
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
				b.Attrs[gofastcore.AttrField] = clipName(name)
			}
			blocks = append(blocks, b)
		}
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
	return xfaValues(src, named)
}

// xfaValues reads the values of the data packet of an XFA form, which is
// in src, alone or in a whole XDP document.
func xfaValues(src string, named map[string]bool) (blocks []gofastcore.Block, cut bool) {
	x := &xmlScan{s: src}
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
		name   string
		text   []string
		group  bool // it has elements other than "value"
		values bool // it has "value" elements
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
				if s := strings.TrimSpace(cleanString(t.text)); s != "" && len(stack[len(stack)-1].text) < 4096 {
					stack[len(stack)-1].text = append(stack[len(stack)-1].text, s)
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
				if len(stack) >= 64 {
					return blocks, false // nested beyond what a form has
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
					gofastcore.AttrStored: gofastcore.StoredFormField, gofastcore.AttrField: clipName(cleanString(field))}})
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
// tags, attributes, text, comments, CDATA and the five entities of XML.
// No schema, no entity of the packet's own making, nothing fetched. A
// packet that is not well formed gives what was read before the fault.
//
// encoding/xml reads it too, at about twenty times the cost: on a page
// of text a packet would take as long as the page.
func packetProperties(src string) (out []property) {
	x := &xmlScan{s: src}
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
	for len(out) < maxVariables {
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
				if v := strings.TrimSpace(value); v != "" && !skip && len(p.values) < 256 {
					p.values = append(p.values, v)
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
				out = append(out, p)
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
	return out
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

// xmlScan reads the tags and the text of XML, one at a time.
type xmlScan struct {
	s   string
	pos int
}

func (x *xmlScan) next() xmlToken {
	for x.pos < len(x.s) {
		s, i := x.s, x.pos
		if s[i] != '<' {
			end := strings.IndexByte(s[i:], '<')
			if end < 0 {
				end = len(s) - i
			}
			x.pos = i + end
			if text := s[i : i+end]; strings.TrimSpace(text) != "" {
				return xmlToken{kind: tokText, text: unescapeXML(text)}
			}
			continue
		}
		rest := s[i:]
		switch {
		case strings.HasPrefix(rest, "<!--"):
			end := strings.Index(rest, "-->")
			if end < 0 {
				return xmlToken{kind: tokEnd}
			}
			x.pos = i + end + 3
		case strings.HasPrefix(rest, "<![CDATA["):
			end := strings.Index(rest, "]]>")
			if end < 0 {
				return xmlToken{kind: tokEnd}
			}
			x.pos = i + end + 3
			return xmlToken{kind: tokText, text: rest[9:end]}
		case strings.HasPrefix(rest, "<?"), strings.HasPrefix(rest, "<!"):
			end := strings.IndexByte(rest, '>')
			if end < 0 {
				return xmlToken{kind: tokEnd}
			}
			x.pos = i + end + 1
		case strings.HasPrefix(rest, "</"):
			end := strings.IndexByte(rest, '>')
			if end < 0 {
				return xmlToken{kind: tokEnd}
			}
			x.pos = i + end + 1
			return xmlToken{kind: tokClose, name: strings.TrimSpace(rest[2:end])}
		default:
			return x.startTag()
		}
	}
	return xmlToken{kind: tokEnd}
}

// startTag reads a start tag at x.pos, with its attributes.
func (x *xmlScan) startTag() xmlToken {
	s := x.s
	i := x.pos + 1
	name := func() string {
		start := i
		for i < len(s) && !isXMLSpace(s[i]) && s[i] != '>' && s[i] != '/' && s[i] != '=' {
			i++
		}
		return s[start:i]
	}
	t := xmlToken{kind: tokOpen, name: name()}
	if t.name == "" {
		return xmlToken{kind: tokEnd}
	}
	for {
		for i < len(s) && isXMLSpace(s[i]) {
			i++
		}
		switch {
		case i >= len(s):
			return xmlToken{kind: tokEnd}
		case s[i] == '>':
			x.pos = i + 1
			return t
		case strings.HasPrefix(s[i:], "/>"):
			x.pos, t.closed = i+2, true
			return t
		}
		an := name()
		for i < len(s) && isXMLSpace(s[i]) {
			i++
		}
		if an == "" || i >= len(s) || s[i] != '=' {
			return xmlToken{kind: tokEnd}
		}
		i++
		for i < len(s) && isXMLSpace(s[i]) {
			i++
		}
		if i >= len(s) || s[i] != '"' && s[i] != '\'' {
			return xmlToken{kind: tokEnd}
		}
		end := strings.IndexByte(s[i+1:], s[i])
		if end < 0 {
			return xmlToken{kind: tokEnd}
		}
		if len(t.attrs) < 256 {
			t.attrs = append(t.attrs, xmlAttr{name: an, value: unescapeXML(s[i+1 : i+1+end])})
		}
		i += end + 2
	}
}

func isXMLSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' }

// unescapeXML reads the five entities of XML and character references.
// An entity it does not know stays as it is written.
func unescapeXML(s string) string {
	if strings.IndexByte(s, '&') < 0 {
		return s
	}
	var sb strings.Builder
	for {
		i := strings.IndexByte(s, '&')
		if i < 0 {
			sb.WriteString(s)
			return sb.String()
		}
		sb.WriteString(s[:i])
		s = s[i:]
		end := strings.IndexByte(s, ';')
		if end < 0 || end > 12 {
			sb.WriteByte('&')
			s = s[1:]
			continue
		}
		ref := s[1:end]
		switch ref {
		case "amp":
			sb.WriteByte('&')
		case "lt":
			sb.WriteByte('<')
		case "gt":
			sb.WriteByte('>')
		case "quot":
			sb.WriteByte('"')
		case "apos":
			sb.WriteByte('\'')
		default:
			n, ok := int64(0), false
			switch {
			case strings.HasPrefix(ref, "#x") || strings.HasPrefix(ref, "#X"):
				n, ok = parseRef(ref[2:], 16)
			case strings.HasPrefix(ref, "#"):
				n, ok = parseRef(ref[1:], 10)
			}
			if !ok {
				sb.WriteString(s[:end+1])
			} else if r := rune(n); utf8.ValidRune(r) {
				sb.WriteRune(r)
			} else {
				sb.WriteRune(utf8.RuneError)
			}
		}
		s = s[end+1:]
	}
}

func parseRef(digits string, base int) (int64, bool) {
	n, err := strconv.ParseInt(digits, base, 32)
	return n, err == nil && digits != "" && n >= 0
}
