// Package xps reads XPS and OpenXPS documents: packages (OPC) whose pages
// are fixed layouts of positioned glyph runs. A page is handed to the
// interpreter's place for such pages (extract.Fixed) as strings at their
// places, so that what is made of a PDF page is made of it.
//
// The container, its XML and its limits on decompression are those of
// go-fast-core (opc, xmlscan).
package xps

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	gofastcore "github.com/AOShei/go-fast-core"
	"github.com/AOShei/go-fast-core/opc"
	"github.com/AOShei/go-fast-core/xmlscan"
	"github.com/AOShei/go-fast-pdf/internal/font"
)

// The namespaces of the two formats. Their markup is the same.
const (
	nsXPS     = "http://schemas.microsoft.com/xps/2005/06"
	nsOpenXPS = "http://schemas.openxps.org/oxps/v1.0"

	relCore = "http://schemas.openxmlformats.org/package/2006/relationships/metadata/core-properties"

	// The content type of a font whose first 32 bytes are obfuscated.
	typeObfuscated = "application/vnd.ms-package.obfuscated-opentype"
)

// The media types of the two formats.
const (
	MIMEXPS     = "application/vnd.ms-xpsdocument"
	MIMEOpenXPS = "application/oxps"
)

const (
	spFixed = xmlscan.FirstSpace + iota
)

var spaces = map[string]xmlscan.Space{nsXPS: spFixed, nsOpenXPS: spFixed}

var scanOptions = xmlscan.Options{Spaces: spaces}

// Bounds on what a package may hold. They are far above what a document
// has and stand between a made-up package and the memory of the reader.
const (
	// maxDocuments bounds the fixed documents of a sequence.
	maxDocuments = 1 << 16
	// maxPages bounds the pages of a package.
	maxPages = 1 << 20
	// maxFonts bounds the fonts of a package that are read.
	maxFonts = 1 << 12
	// maxFontBytes bounds the font programs kept decoded, all together.
	maxFontBytes = 256 << 20
	// maxMembers bounds the members of the archive.
	maxMembers = 1 << 20
	// maxTargets bounds the addressable names of a package that are kept.
	maxTargets = 1 << 18
	// MaxName is the longest name of an element that is kept: a name cut
	// short names nothing.
	MaxName = 512
)

// ErrNotXPS is the error of an input that is no XPS document: not a zip
// archive, or one whose relationships name no fixed representation.
var ErrNotXPS = errors.New("not an XPS document")

// Limits bounds what reading a document may cost.
type Limits struct {
	// MaxDecodedBytes is the number of bytes that may be decompressed
	// from the package, over all its parts.
	MaxDecodedBytes int64
}

// Document is an open XPS or OpenXPS document.
type Document struct {
	pkg *opc.Package
	// OpenXPS is set for an OpenXPS document (ECMA-388); otherwise it is
	// XPS 1.0. The start part's relationship says which, never the name
	// of the file.
	OpenXPS bool
	// Pages are the pages of all fixed documents of the sequence, in
	// order.
	Pages []Page
	// PagesCut is the number of pages past maxPages, which are not read.
	PagesCut int
	// Core is the name of the core properties part, "" for none.
	Core string
	// Docs are the fixed documents of the sequence, in order.
	Docs []Doc
	// targets are the names that the documents list as addressable, each
	// with the page it is on: by the document and the name.
	targets map[target]int32
	// TargetsCut is the number of such names past maxTargets, which are
	// not kept.
	TargetsCut int

	mu        sync.Mutex
	fonts     map[fontKey]*fontEntry
	fontBytes int64
	fallback  map[string]*font.Font
	images    map[string]ImageInfo
}

// Doc is one fixed document of the sequence.
type Doc struct {
	// Part is the name of the FixedDocument part.
	Part string
	// Structure is the name of its DocumentStructure part, "" for none.
	Structure string
}

// target is a name that a fixed document lists as addressable on one of
// its pages (LinkTarget).
type target struct {
	doc  int
	name string
}

// Page is one page of a document.
type Page struct {
	// Part is the name of the fixed page part.
	Part string
	// Doc is the fixed document the page is in, from 0.
	Doc int
}

type fontKey struct {
	part  string
	index int
}

type fontEntry struct {
	once sync.Once
	face *font.Face
}

// Open opens a document. The error is ErrNotXPS when the input is not
// one at all, and otherwise wraps an error of gofastcore.
func Open(r io.ReaderAt, size int64, lim Limits) (*Document, error) {
	pkg, err := opc.Open(r, size, opc.Limits{MaxDecodedBytes: lim.MaxDecodedBytes, MaxMembers: maxMembers})
	if err != nil {
		switch {
		case errors.Is(err, opc.ErrNotZip):
			return nil, ErrNotXPS
		case errors.Is(err, opc.ErrBudget), errors.Is(err, opc.ErrTooManyParts):
			return nil, fmt.Errorf("%w: %v", gofastcore.ErrLimitExceeded, err)
		}
		return nil, fmt.Errorf("%w: %v", gofastcore.ErrMalformed, err)
	}
	d := &Document{pkg: pkg, fonts: map[fontKey]*fontEntry{}}
	rels, err := pkg.Rels("")
	if err != nil {
		return nil, partError("the relationships of the package", err)
	}
	start := ""
	for i := range rels {
		rel := &rels[i]
		if rel.External {
			continue
		}
		switch {
		case rel.Type == nsXPS+"/fixedrepresentation" && start == "":
			start = rel.Target
		case rel.Type == nsOpenXPS+"/fixedrepresentation" && start == "":
			start, d.OpenXPS = rel.Target, true
		case rel.Type == relCore && d.Core == "":
			d.Core = rel.Target
		}
	}
	if start == "" {
		return nil, ErrNotXPS
	}
	if !pkg.Has(start) {
		return nil, fmt.Errorf("%w: the package names %q as its document and has no such part", gofastcore.ErrMalformed, pkg.Label(start))
	}
	var s xmlscan.Scanner
	docs, err := d.sources(&s, start, "FixedDocumentSequence", "DocumentReference", maxDocuments, nil)
	if err != nil {
		return nil, err
	}
	for i, doc := range docs {
		d.Docs = append(d.Docs, Doc{Part: doc})
		if !pkg.Has(doc) {
			continue // a document that is not there has no pages
		}
		if err := d.document(&s, i); err != nil {
			return nil, err
		}
		// The structure the document declares: its outline.
		if rels, err := pkg.RelsWith(doc, &s); err == nil {
			for k := range rels {
				if rel := &rels[k]; !rel.External && (rel.Type == nsXPS+"/documentstructure" || rel.Type == nsOpenXPS+"/documentstructure") && pkg.Has(rel.Target) {
					d.Docs[i].Structure = rel.Target
					break
				}
			}
		}
	}
	if len(d.Pages) == 0 {
		return nil, fmt.Errorf("%w: the document has no pages", gofastcore.ErrMalformed)
	}
	return d, nil
}

// partError is the error of a part of the package itself that cannot be
// read: the document is not read without it.
func partError(what string, err error) error {
	if errors.Is(err, opc.ErrBudget) {
		return fmt.Errorf("%w: %s: %v", gofastcore.ErrLimitExceeded, what, err)
	}
	return fmt.Errorf("%w: %s: %v", gofastcore.ErrMalformed, what, err)
}

// sources reads the part name, whose root element is root, and returns
// the parts that the Source attributes of its elements item name, at
// most room of them; the rest are counted in cut.
func (d *Document) sources(s *xmlscan.Scanner, name, root, item string, room int, cut *int) ([]string, error) {
	closer, err := d.pkg.Scan(name, s, &scanOptions)
	if err != nil {
		return nil, partError(d.pkg.Label(name), err)
	}
	defer closer.Close()
	var out []string
	for {
		k := s.Next()
		if k == xmlscan.EOF {
			break
		}
		if k != xmlscan.Start {
			continue
		}
		if s.Depth() == 1 && !s.Is(spFixed, root) {
			return nil, fmt.Errorf("%w: %s is not a %s", gofastcore.ErrMalformed, d.pkg.Label(name), root)
		}
		if !s.Is(spFixed, item) {
			continue
		}
		src, ok := s.Attr(xmlscan.NoSpace, "Source")
		if !ok || len(src) == 0 {
			continue
		}
		if len(out) >= room {
			if cut != nil {
				*cut++
			}
			continue
		}
		out = append(out, opc.Resolve(name, string(src)))
	}
	if err := s.Err(); err != nil {
		return nil, partError(d.pkg.Label(name), err)
	}
	return out, nil
}

// document reads the FixedDocument part of document i: its pages, and
// the names it lists as addressable on each.
func (d *Document) document(s *xmlscan.Scanner, i int) error {
	name := d.Docs[i].Part
	closer, err := d.pkg.Scan(name, s, &scanOptions)
	if err != nil {
		return partError(d.pkg.Label(name), err)
	}
	defer closer.Close()
	page := int32(-1) // the page whose PageContent is open
	for {
		k := s.Next()
		if k == xmlscan.EOF {
			break
		}
		if k != xmlscan.Start {
			continue
		}
		if s.Depth() == 1 && !s.Is(spFixed, "FixedDocument") {
			return fmt.Errorf("%w: %s is not a FixedDocument", gofastcore.ErrMalformed, d.pkg.Label(name))
		}
		switch {
		case s.Is(spFixed, "PageContent"):
			page = -1
			src, ok := s.Attr(xmlscan.NoSpace, "Source")
			if !ok || len(src) == 0 {
				continue
			}
			if len(d.Pages) >= maxPages {
				d.PagesCut++
				continue
			}
			page = int32(len(d.Pages))
			d.Pages = append(d.Pages, Page{Part: opc.Resolve(name, string(src)), Doc: i})
		case s.Is(spFixed, "LinkTarget") && page >= 0:
			v, ok := s.Attr(xmlscan.NoSpace, "Name")
			if !ok || len(v) == 0 || len(v) > MaxName {
				continue
			}
			if len(d.targets) >= maxTargets {
				d.TargetsCut++
				continue
			}
			if d.targets == nil {
				d.targets = map[target]int32{}
			}
			// The first page that lists a name has it.
			key := target{i, string(v)}
			if _, ok := d.targets[key]; !ok {
				d.targets[key] = page
			}
		}
	}
	if err := s.Err(); err != nil {
		return partError(d.pkg.Label(name), err)
	}
	return nil
}

// Package returns the package of the document.
func (d *Document) Package() *opc.Package { return d.pkg }

// MIME returns the media type of the document's format.
func (d *Document) MIME() string {
	if d.OpenXPS {
		return MIMEOpenXPS
	}
	return MIMEXPS
}

// face returns the font of a FontUri written in the part source: the
// program of the part it names, the face its fragment numbers when the
// program is a collection. It is nil for a font that cannot be read.
func (d *Document) face(source string, uri []byte) *font.Face {
	target, index := string(uri), 0
	if i := strings.IndexByte(target, '#'); i >= 0 {
		for _, c := range target[i+1:] {
			if c < '0' || c > '9' || index > 1<<16 {
				index = 0
				break
			}
			index = index*10 + int(c-'0')
		}
		target = target[:i]
	}
	key := fontKey{opc.Resolve(source, target), index}
	d.mu.Lock()
	e := d.fonts[key]
	if e == nil {
		if len(d.fonts) >= maxFonts {
			d.mu.Unlock()
			return nil
		}
		e = &fontEntry{}
		d.fonts[key] = e
	}
	d.mu.Unlock()
	e.once.Do(func() { e.face = d.readFace(key) })
	return e.face
}

func (d *Document) readFace(key fontKey) *font.Face {
	rc, err := d.pkg.Open(key.part)
	if err != nil {
		return nil
	}
	defer rc.Close()
	data, err := io.ReadAll(io.LimitReader(rc, font.MaxFaceBytes))
	if err != nil && len(data) < 64 {
		return nil
	}
	d.mu.Lock()
	d.fontBytes += int64(len(data))
	over := d.fontBytes > maxFontBytes
	d.mu.Unlock()
	if over {
		return nil
	}
	// The first 32 bytes of an obfuscated font are combined with the
	// identifier that is the name of its part. The content type says
	// which fonts are; a program that is one as it stands is left alone,
	// and one that is not is tried the other way, whatever the type says.
	if !sfnt(data) || d.pkg.ContentType(key.part) == typeObfuscated {
		if clear := deobfuscate(data, key.part); sfnt(clear) {
			data = clear
		}
	}
	return font.NewFace(data, key.index)
}

// sfnt reports whether data begins as a TrueType or OpenType program
// does.
func sfnt(data []byte) bool {
	if len(data) < 4 {
		return false
	}
	switch string(data[:4]) {
	case "\x00\x01\x00\x00", "OTTO", "ttcf", "true":
		return true
	}
	return false
}

// deobfuscate returns the font program data of the part name with its
// first 32 bytes restored: they are combined with the 16 bytes that the
// 32 hexadecimal digits of the name's last segment spell, the last of
// them first, twice over. data is returned as it is when the name is no
// such identifier.
func deobfuscate(data []byte, name string) []byte {
	if i := strings.LastIndexByte(name, '/'); i >= 0 {
		name = name[i+1:]
	}
	if i := strings.IndexByte(name, '.'); i >= 0 {
		name = name[:i]
	}
	var key [16]byte
	n := 0
	for i := 0; i < len(name); i++ {
		c := name[i]
		var v byte
		switch {
		case c >= '0' && c <= '9':
			v = c - '0'
		case c >= 'a' && c <= 'f':
			v = c - 'a' + 10
		case c >= 'A' && c <= 'F':
			v = c - 'A' + 10
		case c == '-' || c == '{' || c == '}':
			continue
		default:
			return data
		}
		if n >= 32 {
			return data
		}
		if n%2 == 0 {
			key[n/2] = v << 4
		} else {
			key[n/2] |= v
		}
		n++
	}
	if n != 32 || len(data) < 32 {
		return data
	}
	out := append([]byte(nil), data...)
	for i := 0; i < 16; i++ {
		out[i] ^= key[15-i]
		out[i+16] ^= key[15-i]
	}
	return out
}

// unknownFont returns the font of the text set in a font that cannot be
// read, one for each name: the text is read all the same, from what the
// page says it is.
func (d *Document) unknownFont(name string) *font.Font {
	d.mu.Lock()
	defer d.mu.Unlock()
	f := d.fallback[name]
	if f == nil {
		if d.fallback == nil {
			d.fallback = map[string]*font.Font{}
		}
		if len(d.fallback) >= maxFonts {
			name = ""
			if f = d.fallback[name]; f != nil {
				return f
			}
		}
		f = font.NewPlaced("", 0.25, false, false)
		d.fallback[name] = f
	}
	return f
}
