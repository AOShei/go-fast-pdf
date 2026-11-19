package gofastpdf

import (
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
// own making in the document information, and fields of a form that no
// page shows. Nobody who opens the file sees any of it, which makes it a
// place to put text for a program that reads the extracted document.
// The shared model's rule is that such data is read as data, never run
// or followed, bounded, and flagged as invisible with Attrs["stored"]
// saying what it is. What belongs to a page stands on its page; what
// belongs to the file is in one part of kind stored, after the pages.

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
	if len(data) >= 2 && (data[0] == 0xFE && data[1] == 0xFF || data[0] == 0xFF && data[1] == 0xFE) {
		s = charset.TextString(data) // UTF-16, which TextString reads by its mark
	} else {
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
		b := gofastcore.Block{Kind: gofastcore.KindCode, Text: src, Origin: gofastcore.OriginNative, Hidden: gofastcore.HiddenInvisible}
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
// scripts are those of the actions of the catalog and of the form.
func stored(f *file.File, catalog syntax.Value, md *gofastcore.Metadata, scripts []script) (blocks []gofastcore.Block, cut bool) {
	r := &reader{f: f, room: maxStoredBytes}
	// What the document information says, and what has been said here
	// already: a value is given once. Programs write the same title in
	// the information and in three properties of the packet, and the
	// entries of their own making in both.
	said := []string{md.Title, md.Subject, md.Application, md.Extra["producer"]}
	said = append(append(said, md.Authors...), md.Keywords...)
	variable := func(kind, name, value string) {
		text := charset.Letters(strings.TrimSpace(name + ": " + value))
		blocks = append(blocks, gofastcore.Block{Kind: gofastcore.KindParagraph, Text: text, Origin: gofastcore.OriginNative,
			Hidden: gofastcore.HiddenInvisible, Attrs: map[string]string{gofastcore.AttrStored: kind}})
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
	if packet := f.Resolve(catalog.Get("Metadata")); packet.Kind == syntax.Stream {
		src, ok := r.packet(packet)
		if !ok {
			return blocks, true
		}
		for _, p := range packetProperties(src) {
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
		if f.Resolve(field.Get("Subtype")).IsName("Widget") || field.Has("Rect") {
			continue // it has a place on a page, and is read there
		}
		x, ok := fieldValue(f, field, r.text)
		if !ok {
			return blocks, true
		}
		if x.block.Text != "" {
			b := x.block
			b.Origin, b.Hidden = gofastcore.OriginNative, gofastcore.HiddenInvisible
			b.Attrs[gofastcore.AttrStored] = gofastcore.StoredFormField
			if !field.Has("Parent") {
				b.Attrs[gofastcore.AttrField] = clipName(name)
			}
			blocks = append(blocks, b)
		}
	}
	return blocks, false
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
// information does not: it is found in one of its strings.
func within(v string, known []string) bool {
	for _, k := range known {
		if k != "" && strings.Contains(k, v) {
			return true
		}
	}
	return false
}

// property is one property of the metadata packet with its values.
type property struct {
	name   string
	values []string
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
	for len(out) < maxVariables {
		t := x.next()
		switch t.kind {
		case tokEnd:
			return out
		case tokOpen:
			if local(t.name) == "Description" {
				inDescription = !t.closed
				for _, a := range t.attrs {
					if prefix(a.name) == "rdf" || prefix(a.name) == "xmlns" || a.name == "xmlns" || prefix(a.name) == "xml" || packetSkipped[local(a.name)] {
						continue
					}
					if v := strings.TrimSpace(a.value); v != "" {
						out = append(out, property{name: a.name, values: []string{v}})
					}
				}
				continue
			}
			if !inDescription || t.closed {
				continue
			}
			// A property: the text in it, however it is nested.
			p := property{name: t.name}
			skip := packetSkipped[local(t.name)]
			for level := 1; level > 0; {
				in := x.next()
				switch in.kind {
				case tokEnd:
					return out
				case tokOpen:
					if !in.closed {
						level++
					}
				case tokClose:
					level--
				case tokText:
					if v := strings.TrimSpace(in.text); v != "" && !skip && len(p.values) < 256 {
						p.values = append(p.values, v)
					}
				}
			}
			if len(p.values) > 0 {
				out = append(out, p)
			}
		case tokClose:
			if local(t.name) == "Description" {
				inDescription = false
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
