// Package xpstest assembles XPS and OpenXPS documents in memory for the
// tests: the package, its parts, and a font program made for the purpose.
//
// It is imported by tests only. Like the builder of PDF files it does no
// validation, so that damaged documents are made as readily as sound
// ones.
package xpstest

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"fmt"
	"sort"
	"strings"
	"unicode/utf16"
)

// The namespaces of the two formats.
const (
	XPS     = "http://schemas.microsoft.com/xps/2005/06"
	OpenXPS = "http://schemas.openxps.org/oxps/v1.0"
)

// FontName is the part name of the font that Document puts in a
// package, and FontGUID the name of the same font obfuscated.
const (
	FontName = "/Resources/font.ttf"
	FontGUID = "/Resources/2B6D30FE-2857-7BE6-8635-2A72642C3567.odttf"
)

// Package is a package being assembled: its members in the order they
// were added.
type Package struct {
	// NoTypes leaves [Content_Types].xml out.
	NoTypes bool
	// Types are further Default entries of the content types, by
	// extension, and Overrides the Override entries, by part name.
	Types, Overrides map[string]string
	// Store keeps the members uncompressed.
	Store bool

	names []string
	data  map[string][]byte
}

// New returns an empty package.
func New() *Package {
	return &Package{data: map[string][]byte{}, Types: map[string]string{}, Overrides: map[string]string{}}
}

// Add puts a member in the package under name, which has no leading
// slash; a member of that name is replaced.
func (p *Package) Add(name string, data []byte) *Package {
	name = strings.TrimPrefix(name, "/")
	if _, ok := p.data[name]; !ok {
		p.names = append(p.names, name)
	}
	p.data[name] = data
	return p
}

// Remove takes a member out of the package.
func (p *Package) Remove(name string) *Package {
	name = strings.TrimPrefix(name, "/")
	delete(p.data, name)
	for i, n := range p.names {
		if n == name {
			p.names = append(p.names[:i], p.names[i+1:]...)
			break
		}
	}
	return p
}

// Get returns the data of a member.
func (p *Package) Get(name string) []byte { return p.data[strings.TrimPrefix(name, "/")] }

// Bytes returns the package as a zip archive.
func (p *Package) Bytes() []byte {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	write := func(name string, data []byte) {
		method := zip.Deflate
		if p.Store {
			method = zip.Store
		}
		w, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: method})
		if err != nil {
			panic(err)
		}
		w.Write(data)
	}
	if _, own := p.data["[Content_Types].xml"]; !p.NoTypes && !own {
		write("[Content_Types].xml", []byte(p.contentTypes()))
	}
	for _, name := range p.names {
		write(name, p.data[name])
	}
	if err := zw.Close(); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

func (p *Package) contentTypes() string {
	types := map[string]string{
		"rels":   "application/vnd.openxmlformats-package.relationships+xml",
		"fdseq":  "application/vnd.ms-package.xps-fixeddocumentsequence+xml",
		"fdoc":   "application/vnd.ms-package.xps-fixeddocument+xml",
		"fpage":  "application/vnd.ms-package.xps-fixedpage+xml",
		"odttf":  "application/vnd.ms-package.obfuscated-opentype",
		"ttf":    "application/vnd.ms-opentype",
		"xml":    "application/vnd.openxmlformats-package.core-properties+xml",
		"struct": "application/vnd.ms-package.xps-documentstructure+xml",
		"frag":   "application/vnd.ms-package.xps-storyfragments+xml",
	}
	for k, v := range p.Types {
		types[k] = v
	}
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">`)
	for _, k := range sorted(types) {
		fmt.Fprintf(&b, `<Default Extension="%s" ContentType="%s"/>`, k, types[k])
	}
	for _, k := range sorted(p.Overrides) {
		fmt.Fprintf(&b, `<Override PartName="%s" ContentType="%s"/>`, k, p.Overrides[k])
	}
	b.WriteString(`</Types>`)
	return b.String()
}

func sorted(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Document returns a package of one fixed document with the pages
// given, each the markup of a FixedPage part (see Page), in the
// namespace ns (XPS or OpenXPS), with the font of Font at FontName.
func Document(ns string, pages ...string) *Package {
	p := New()
	p.Add("_rels/.rels", []byte(fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`+
		`<Relationship Id="R1" Type="%s/fixedrepresentation" Target="/FixedDocSeq.fdseq"/>`+
		`<Relationship Id="R2" Type="http://schemas.openxmlformats.org/package/2006/relationships/metadata/core-properties" Target="/docProps/core.xml"/>`+
		`</Relationships>`, ns)))
	p.Add("FixedDocSeq.fdseq", []byte(fmt.Sprintf(`<FixedDocumentSequence xmlns="%s"><DocumentReference Source="/Documents/1/FixedDoc.fdoc"/></FixedDocumentSequence>`, ns)))
	var doc strings.Builder
	fmt.Fprintf(&doc, `<FixedDocument xmlns="%s">`, ns)
	for i, page := range pages {
		fmt.Fprintf(&doc, `<PageContent Source="Pages/%d.fpage"/>`, i+1)
		p.Add(fmt.Sprintf("Documents/1/Pages/%d.fpage", i+1), []byte(page))
	}
	doc.WriteString(`</FixedDocument>`)
	p.Add("Documents/1/FixedDoc.fdoc", []byte(doc.String()))
	p.Add(FontName, Font(nil))
	return p
}

// Page returns the markup of a FixedPage of the size given, in units of
// 1/96 inch, in the namespace ns, with body inside it.
func Page(ns string, width, height float64, body string) string {
	return fmt.Sprintf(`<FixedPage xmlns="%s" Width="%g" Height="%g" xml:lang="en-US">%s</FixedPage>`, ns, width, height, body)
}

// Glyphs returns a Glyphs element that shows text at (x, y) at the font
// size given, filled black, in the font at FontName. extra is further
// attributes, written as they are.
func Glyphs(x, y, size float64, text, extra string) string {
	return fmt.Sprintf(`<Glyphs Fill="#FF000000" FontUri="%s" FontRenderingEmSize="%g" OriginX="%g" OriginY="%g" UnicodeString="%s" %s/>`,
		FontName, size, x, y, Escape(text), extra)
}

// Escape writes s as the value of an attribute.
func Escape(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")
	s = r.Replace(s)
	if strings.HasPrefix(s, "{") {
		s = "{}" + s
	}
	return s
}

// UTF16 returns the markup s as a part written in UTF-16, little endian,
// with a byte order mark.
func UTF16(s string) []byte {
	units := utf16.Encode([]rune(s))
	out := make([]byte, 2, 2+2*len(units))
	out[0], out[1] = 0xFF, 0xFE
	for _, u := range units {
		out = append(out, byte(u), byte(u>>8))
	}
	return out
}

// Obfuscate returns the font program data as the part named name holds
// it obfuscated: its first 32 bytes combined with the identifier that
// is the last segment of the name.
func Obfuscate(data []byte, name string) []byte {
	if i := strings.LastIndexByte(name, '/'); i >= 0 {
		name = name[i+1:]
	}
	if i := strings.IndexByte(name, '.'); i >= 0 {
		name = name[:i]
	}
	hex := strings.ReplaceAll(name, "-", "")
	var key [16]byte
	b, _ := decodeHex(hex)
	copy(key[:], b)
	out := append([]byte(nil), data...)
	for i := 0; i < 16 && i+16 < len(out); i++ {
		out[i] ^= key[15-i]
		out[i+16] ^= key[15-i]
	}
	return out
}

func decodeHex(s string) ([]byte, error) {
	out := make([]byte, 0, len(s)/2)
	for i := 0; i+1 < len(s); i += 2 {
		var v byte
		if _, err := fmt.Sscanf(s[i:i+2], "%02x", &v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

// FontOptions says what the font of Font is.
type FontOptions struct {
	// Name is the full name of the font, "Test Sans" if empty.
	Name string
	// Bold and Italic set the bits of the OS/2 table.
	Bold, Italic bool
	// Advances are the advances of further characters, in thousandths
	// of the font size; a character named here that the font has
	// already takes the advance given.
	Advances map[rune]int
	// Symbol writes the cmap as that of a symbol font (platform 3,
	// encoding 0), its characters at U+F000 and on.
	Symbol bool
}

// Font returns a TrueType program with no outlines: the tables that say
// which glyph a character has and how far it advances. It has the
// printable characters of ASCII, each advancing by 500 thousandths of
// the font size, the space by 250, and the Hebrew letters alef to tav
// and the Arabic letters alef, beh, lam and meem, each by 500. The glyph
// of a character is its place in the order of the characters, from 1.
func Font(opts *FontOptions) []byte {
	if opts == nil {
		opts = &FontOptions{}
	}
	adv := map[rune]int{}
	for r := rune(0x21); r < 0x7F; r++ {
		adv[r] = 500
	}
	adv[' '] = 250
	for r := rune(0x05D0); r <= 0x05EA; r++ {
		adv[r] = 500
	}
	for _, r := range []rune{0x0627, 0x0628, 0x0644, 0x0645} {
		adv[r] = 500
	}
	for r, a := range opts.Advances {
		adv[r] = a
	}
	runes := make([]rune, 0, len(adv))
	for r := range adv {
		runes = append(runes, r)
	}
	sort.Slice(runes, func(i, j int) bool { return runes[i] < runes[j] })

	be16 := func(b []byte, v int) []byte { return binary.BigEndian.AppendUint16(b, uint16(v)) }
	n := len(runes) + 1 // with .notdef

	head := make([]byte, 54)
	binary.BigEndian.PutUint32(head[0:], 0x00010000)
	binary.BigEndian.PutUint32(head[12:], 0x5F0F3CF5)
	binary.BigEndian.PutUint16(head[18:], 1000)
	style := 0
	if opts.Bold {
		style |= 1
	}
	if opts.Italic {
		style |= 2
	}
	binary.BigEndian.PutUint16(head[44:], uint16(style))

	hhea := make([]byte, 36)
	binary.BigEndian.PutUint32(hhea[0:], 0x00010000)
	binary.BigEndian.PutUint16(hhea[4:], 800)
	binary.BigEndian.PutUint16(hhea[6:], 0x10000-200)
	binary.BigEndian.PutUint16(hhea[34:], uint16(n))

	maxp := make([]byte, 6)
	binary.BigEndian.PutUint32(maxp[0:], 0x00005000)
	binary.BigEndian.PutUint16(maxp[4:], uint16(n))

	hmtx := be16(be16(nil, 500), 0)
	for _, r := range runes {
		hmtx = be16(be16(hmtx, adv[r]), 0)
	}

	// cmap, format 4: a segment for each character.
	segs := len(runes) + 1
	var end, start, delta, offs []byte
	for i, r := range runes {
		code := int(r)
		if opts.Symbol {
			code = 0xF000 + int(r)&0xFF
		}
		end, start = be16(end, code), be16(start, code)
		delta, offs = be16(delta, (i+1-code)&0xFFFF), be16(offs, 0)
	}
	end, start, delta, offs = be16(end, 0xFFFF), be16(start, 0xFFFF), be16(delta, 1), be16(offs, 0)
	sub := be16(be16(be16(nil, 4), 16+8*segs), 0)
	sub = be16(be16(be16(be16(sub, 2*segs), 0), 0), 0)
	sub = append(sub, end...)
	sub = be16(sub, 0)
	sub = append(append(append(sub, start...), delta...), offs...)
	encoding := 1
	if opts.Symbol {
		encoding = 0
	}
	cmap := be16(be16(nil, 0), 1)
	cmap = be16(be16(cmap, 3), encoding)
	cmap = binary.BigEndian.AppendUint32(cmap, 12)
	cmap = append(cmap, sub...)

	name := opts.Name
	if name == "" {
		name = "Test Sans"
	}
	var str []byte
	for _, u := range utf16.Encode([]rune(name)) {
		str = be16(str, int(u))
	}
	nameTable := be16(be16(be16(nil, 0), 1), 18)
	for _, v := range []int{3, 1, 0x409, 4, len(str), 0} {
		nameTable = be16(nameTable, v)
	}
	nameTable = append(nameTable, str...)

	os2 := make([]byte, 78)
	sel := 0x40
	if opts.Bold {
		sel = sel&^0x40 | 0x20
	}
	if opts.Italic {
		sel = sel&^0x40 | 0x01
	}
	binary.BigEndian.PutUint16(os2[62:], uint16(sel))

	tables := []struct {
		tag  string
		data []byte
	}{{"OS/2", os2}, {"cmap", cmap}, {"head", head}, {"hhea", hhea}, {"hmtx", hmtx}, {"maxp", maxp}, {"name", nameTable}}
	out := binary.BigEndian.AppendUint32(nil, 0x00010000)
	out = be16(be16(be16(be16(out, len(tables)), 0), 0), 0)
	off := 12 + 16*len(tables)
	var body []byte
	for _, t := range tables {
		out = append(out, t.tag...)
		out = binary.BigEndian.AppendUint32(out, 0)
		out = binary.BigEndian.AppendUint32(out, uint32(off+len(body)))
		out = binary.BigEndian.AppendUint32(out, uint32(len(t.data)))
		body = append(body, t.data...)
		for len(body)%4 != 0 {
			body = append(body, 0)
		}
	}
	return append(out, body...)
}

// Glyph returns the glyph of a character in the font of Font(nil), 0
// for one it does not have.
func Glyph(r rune) int {
	switch {
	case r == ' ':
		return 1
	case r >= 0x21 && r < 0x7F:
		return int(r-0x21) + 2
	case r >= 0x05D0 && r <= 0x05EA:
		return int(r-0x05D0) + 2 + 94
	}
	for i, a := range []rune{0x0627, 0x0628, 0x0644, 0x0645} {
		if r == a {
			return 2 + 94 + 27 + i
		}
	}
	return 0
}
