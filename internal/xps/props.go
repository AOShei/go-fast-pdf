package xps

import (
	"github.com/AOShei/go-fast-core/charset"
	"github.com/AOShei/go-fast-core/xmlscan"
)

const (
	spCore = xmlscan.FirstSpace + 8 + iota
	spDC
	spDCTerms
)

var propSpaces = map[string]xmlscan.Space{
	"http://schemas.openxmlformats.org/package/2006/metadata/core-properties": spCore,
	"http://purl.org/dc/elements/1.1/":                                        spDC,
	"http://purl.org/dc/terms/":                                               spDCTerms,
}

var propOptions = xmlscan.Options{Spaces: propSpaces}

// maxPropertyBytes bounds the value of one core property.
const maxPropertyBytes = 64 << 10

// Properties are the core properties of the package (OPC), as written.
type Properties struct {
	Title, Creator, Subject, Keywords, Language string
	Created, Modified                           string
	Description, LastModifiedBy, Revision       string
	Category, ContentStatus                     string
	// Cut is the number of values longer than is kept.
	Cut int
}

// Properties reads the core properties part. ok is false when the
// package has none or it cannot be read.
func (d *Document) Properties() (p Properties, ok bool) {
	if d.Core == "" || !d.pkg.Has(d.Core) {
		return p, false
	}
	var s xmlscan.Scanner
	closer, err := d.pkg.Scan(d.Core, &s, &propOptions)
	if err != nil {
		return p, false
	}
	defer closer.Close()
	defer d.noteRefs(&s)
	var buf []byte
	for s.Next() != xmlscan.EOF {
		if s.Kind() != xmlscan.Start || s.Depth() != 2 {
			continue
		}
		var dst *string
		space, local := s.Space(), string(s.Local())
		switch {
		case space == spDC && local == "title":
			dst = &p.Title
		case space == spDC && local == "creator":
			dst = &p.Creator
		case space == spDC && local == "subject":
			dst = &p.Subject
		case space == spCore && local == "keywords":
			dst = &p.Keywords
		case space == spDC && local == "language":
			dst = &p.Language
		case space == spDCTerms && local == "created":
			dst = &p.Created
		case space == spDCTerms && local == "modified":
			dst = &p.Modified
		case space == spDC && local == "description":
			dst = &p.Description
		case space == spCore && local == "lastModifiedBy":
			dst = &p.LastModifiedBy
		case space == spCore && local == "revision":
			dst = &p.Revision
		case space == spCore && local == "category":
			dst = &p.Category
		case space == spCore && local == "contentStatus":
			dst = &p.ContentStatus
		default:
			s.Skip()
			continue
		}
		// The text of the element, whatever is nested in it.
		buf, cut := s.AppendText(buf[:0]), false
		if len(buf) > maxPropertyBytes {
			// Not inside a character.
			buf, cut = charset.CutUTF8(buf, maxPropertyBytes), true
		}
		if cut {
			p.Cut++
		}
		*dst = string(buf)
	}
	return p, true
}
