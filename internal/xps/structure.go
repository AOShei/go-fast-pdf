package xps

import (
	"strings"

	"github.com/AOShei/go-fast-core/opc"
	"github.com/AOShei/go-fast-core/xmlscan"
)

// The structure a document declares: its outline (the DocumentStructure
// part of each fixed document), and the addresses inside it that its
// outline and its links name.

const (
	spStructure = xmlscan.FirstSpace + 16 + iota
)

var structureOptions = xmlscan.Options{Spaces: map[string]xmlscan.Space{
	nsXPS + "/documentstructure":     spStructure,
	nsOpenXPS + "/documentstructure": spStructure,
}}

// MaxOutline is the number of entries of the outline that are read.
const MaxOutline = maxOutline

const (
	// maxOutline bounds the entries of the outline that are read.
	maxOutline = 1 << 17
	// maxOutlineLevel bounds the level of an entry.
	maxOutlineLevel = 64
	// maxTitle bounds the title of one entry.
	maxTitle = 64 << 10
)

// OutlineEntry is an entry of the outline of a document, as written.
type OutlineEntry struct {
	Title string
	Level int
	// Page is the number of the page the entry points at, from 1 over
	// all documents of the sequence; 0 when it names none that is known.
	Page int
}

// Outline reads the outlines of the fixed documents, in their order,
// until the titles take more than limit bytes: cut is set when they do,
// and many when there are more entries than are read. damaged is set
// when a structure part breaks off; what stands before the damage is
// read.
func (d *Document) Outline(limit int64) (entries []OutlineEntry, cut, many, damaged bool) {
	var s xmlscan.Scanner
	for i := range d.Docs {
		name := d.Docs[i].Structure
		if name == "" {
			continue
		}
		closer, err := d.pkg.Scan(name, &s, &structureOptions)
		if err != nil {
			damaged = true
			continue
		}
		for !cut && !many {
			k := s.Next()
			if k == xmlscan.EOF {
				break
			}
			if k != xmlscan.Start || !s.Is(spStructure, "OutlineEntry") {
				continue
			}
			if len(entries) >= maxOutline {
				many = true
				break
			}
			e := OutlineEntry{Level: 1}
			if v, ok := s.Attr(xmlscan.NoSpace, "OutlineLevel"); ok {
				if n, good := parseNumber(v); good && n >= 1 {
					e.Level = int(min(n, maxOutlineLevel))
				}
			}
			if v, ok := s.Attr(xmlscan.NoSpace, "Description"); ok {
				e.Title = string(v[:min(len(v), maxTitle)])
			}
			if limit -= int64(len(e.Title)); limit < 0 {
				cut = true
				break
			}
			if v, ok := s.Attr(xmlscan.NoSpace, "OutlineTarget"); ok {
				e.Page = d.pageOf(string(v), name, i) + 1
			}
			entries = append(entries, e)
		}
		if s.Err() != nil {
			damaged = true
		}
		closer.Close()
		if cut || many {
			break
		}
	}
	return entries, cut, many, damaged
}

// pageOf returns the index of the page that an address inside the
// package names, -1 for none: a page by its number over all documents
// ("…#12"), or a name that a fixed document lists as addressable
// ("…FixedDoc.fdoc#name"). The address is written in the part from, of
// the document doc; without a part of its own it is of that document.
func (d *Document) pageOf(address, from string, doc int) int {
	i := strings.IndexByte(address, '#')
	if i < 0 || i+1 >= len(address) {
		return -1
	}
	path, frag := address[:i], address[i+1:]
	number, digits := 0, true
	for _, c := range frag {
		if c < '0' || c > '9' || number > maxPages {
			digits = false
			break
		}
		number = number*10 + int(c-'0')
	}
	if digits {
		if number >= 1 && number <= len(d.Pages) {
			return number - 1
		}
		return -1
	}
	if path != "" {
		part := opc.Resolve(from, path)
		doc = -1
		for k := range d.Docs {
			if d.Docs[k].Part == part {
				doc = k
				break
			}
		}
		if doc < 0 {
			return -1
		}
	}
	if p, ok := d.targets[target{doc, frag}]; ok {
		return int(p)
	}
	return -1
}

// Target reports whether name is a name that document doc lists as
// addressable on the page with index page.
func (d *Document) Target(doc int, name []byte, page int) bool {
	p, ok := d.targets[target{doc, string(name)}]
	return ok && int(p) == page
}
