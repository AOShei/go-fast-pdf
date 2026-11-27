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

// Signature definitions say who is asked to sign a document, where on
// which page, by when, and what the signature is to mean: the Intent, a
// sentence the author of the document wrote, which is text to be read.

const (
	spSignature = xmlscan.FirstSpace + 24 + iota
)

var signatureOptions = xmlscan.Options{Spaces: map[string]xmlscan.Space{
	nsXPS + "/signature-definitions":     spSignature,
	nsOpenXPS + "/signature-definitions": spSignature,
}}

const (
	// maxIntents bounds the signature definitions that are read, and
	// maxIntentBytes the text of all their intents together.
	maxIntents     = 1 << 10
	maxIntentBytes = 1 << 20
)

// Intents returns what the signature definitions say the signatures
// asked for on the page with index page are to mean, in the order of
// the definitions. A definition that names no page of the document is
// on the first page of its fixed document. cut is set when there are
// more definitions, or more text, than are read, and damaged when a
// part of them breaks off.
func (d *Document) Intents(page int) (intents []string, cut, damaged bool) {
	d.intentsOnce.Do(d.readIntents)
	return d.intents[page], d.intentsCut, d.intentsDamaged
}

func (d *Document) readIntents() {
	var s xmlscan.Scanner
	count, room := 0, maxIntentBytes
	for i := range d.Docs {
		name := d.Docs[i].Signatures
		if name == "" {
			continue
		}
		first := -1 // the first page of the document
		for k := range d.Pages {
			if d.Pages[k].Doc == i {
				first = k
				break
			}
		}
		if first < 0 {
			continue
		}
		closer, err := d.pkg.Scan(name, &s, &signatureOptions)
		if err != nil {
			d.intentsDamaged = true
			continue
		}
		page := first
		var buf []byte
		for {
			k := s.Next()
			if k == xmlscan.EOF {
				break
			}
			if k != xmlscan.Start || s.Space() != spSignature {
				continue
			}
			switch string(s.Local()) {
			case "SignatureDefinition":
				page = first
			case "SpotLocation":
				if v, ok := s.Attr(xmlscan.NoSpace, "PageURI"); ok {
					part := opc.Resolve(name, string(v))
					for k := range d.Pages {
						if d.Pages[k].Part == part {
							page = k
							break
						}
					}
				}
			case "Intent":
				buf = s.AppendText(buf[:0])
				text := strings.TrimSpace(string(buf))
				if text == "" {
					continue
				}
				if count >= maxIntents || len(text) > room {
					d.intentsCut = true
					continue
				}
				count++
				room -= len(text)
				if d.intents == nil {
					d.intents = map[int][]string{}
				}
				d.intents[page] = append(d.intents[page], text)
			}
		}
		if s.Err() != nil {
			d.intentsDamaged = true
		}
		closer.Close()
	}
}
