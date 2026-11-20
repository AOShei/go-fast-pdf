package gofastpdf

import (
	"strings"
	"time"

	"github.com/AOShei/go-fast-pdf/internal/file"
	"github.com/AOShei/go-fast-pdf/internal/syntax"

	gofastcore "github.com/AOShei/go-fast-core"
)

// The metadata packet (XMP) in the document information.
//
// A PDF says who wrote it and what it is about twice: in the document
// information, a dictionary of strings, and in the metadata packet of
// its catalog, XML whose properties can be lists. The information has
// one string for the authors and one for the keywords, which the shared
// model gives as one entry each, unsplit. The packet has a list of
// authors (dc:creator) and of keywords (dc:subject), each item as the
// file declares it; where it has them they are the authors and the
// keywords of the document. Where the packet has no list of keywords,
// its pdf:Keywords is one entry as the information's is. An entry of
// the information that is empty or missing is taken from the packet:
// the title (dc:title), the subject (dc:description), the program that
// made the file (xmp:CreatorTool), the one that wrote the PDF
// (pdf:Producer), the dates of creation and change (xmp:CreateDate,
// xmp:ModifyDate), and the language (dc:language) where the catalog
// gives none.
//
// The packet is the newer form, and PDF 2.0 keeps only it. A program
// that changes the information and leaves the packet as it was makes
// the information newer: when the information was changed after the
// packet, by ModDate against xmp:MetadataDate (or xmp:ModifyDate), its
// authors and keywords stay. Properties are known by their namespace,
// not by the prefix a packet happens to give it.
//
// What the packet says beyond this is in the stored part, as before;
// what the document information now says is not said there again.

// The namespaces of the properties read.
const (
	nsDC  = "http://purl.org/dc/elements/1.1/"
	nsPDF = "http://ns.adobe.com/pdf/1.3/"
	nsXMP = "http://ns.adobe.com/xap/1.0/"
)

// packet is the metadata packet of a document as read.
type packet struct {
	// read is set when the catalog has a packet, and whole when there
	// was room to read it.
	read, whole bool
	// size is the length of its text.
	size  int
	props []property
}

// readPacket reads the metadata packet of a catalog.
func readPacket(f *file.File, catalog syntax.Value) (pk packet) {
	v := f.Resolve(catalog.Get("Metadata"))
	if v.Kind != syntax.Stream {
		return pk
	}
	r := &reader{f: f, room: maxStoredBytes}
	src, ok := r.packet(v)
	pk = packet{read: true, whole: ok, size: len(src)}
	if ok {
		pk.props = packetProperties(src)
	}
	return pk
}

// fromPacket fills in the document information from the metadata
// packet.
func fromPacket(md *gofastcore.Metadata, pk packet) {
	var authors, subjects, keywords []string
	var title, description, language, tool, producer string
	// Dates are read only where they are wanted: a date with a zone
	// takes room of its own.
	var created, modified, changed string
	// The first value that is text, as a property of one value has it.
	first := func(values []string) string {
		for _, v := range values {
			if v = strings.TrimSpace(cleanString(v)); v != "" {
				return v
			}
		}
		return ""
	}
	for i := range pk.props {
		p := &pk.props[i]
		switch p.ns + local(p.name) {
		case nsDC + "creator":
			authors = p.values
		case nsDC + "subject":
			subjects = p.values
		case nsPDF + "Keywords":
			if k := first(p.values); k != "" {
				keywords = []string{k} // one string, one entry: it is not split
			}
		case nsDC + "title":
			title = first(p.values) // the first language, the default by convention
		case nsDC + "description":
			description = first(p.values)
		case nsDC + "language":
			language = first(p.values)
		case nsXMP + "CreatorTool":
			tool = first(p.values)
		case nsPDF + "Producer":
			producer = first(p.values)
		case nsXMP + "CreateDate":
			created = first(p.values)
		case nsXMP + "ModifyDate":
			modified = first(p.values)
		case nsXMP + "MetadataDate":
			changed = first(p.values)
		}
	}
	if changed == "" {
		changed = modified
	}
	// The information changed after the packet keeps its lists.
	newer := func() bool {
		at := xmpDate(changed)
		return !md.Modified.IsZero() && !at.IsZero() && md.Modified.After(at)
	}
	authors, subjects = cleanValues(authors), cleanValues(subjects)
	if len(subjects) > 0 {
		keywords = subjects
	}
	if len(authors) > 0 && (len(md.Authors) == 0 || !newer()) {
		md.Authors = authors
	}
	if len(keywords) > 0 && (len(md.Keywords) == 0 || !newer()) {
		md.Keywords = keywords
	}
	fill := func(s *string, v string) {
		if *s == "" {
			*s = v
		}
	}
	fill(&md.Title, title)
	fill(&md.Subject, description)
	fill(&md.Language, language)
	fill(&md.Application, tool)
	if producer != "" && md.Extra["producer"] == "" {
		if md.Extra == nil {
			md.Extra = map[string]string{}
		}
		md.Extra["producer"] = producer
	}
	if md.Created.IsZero() && created != "" {
		md.Created = xmpDate(created)
	}
	if md.Modified.IsZero() && modified != "" {
		md.Modified = xmpDate(modified)
	}
}

// cleanValues makes the values of a property fit the shared model's
// text rules, and leaves out those that are then empty.
func cleanValues(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, v := range values {
		if v = strings.TrimSpace(cleanString(v)); v != "" {
			out = append(out, v)
		}
	}
	return out
}

// xmpDate reads a date of the packet: ISO 8601 as XMP writes it, from a
// year alone to seconds with a fraction, with or without a zone (UTC
// without). The zero time when it is none of these. The zone is the
// offset alone, as for the dates of the document information, whatever
// the zone of the computer that reads it: time.Parse would give that
// zone where its offset is the same.
func xmpDate(s string) time.Time {
	s = strings.TrimSpace(s)
	for _, layout := range [...]string{
		time.RFC3339Nano, "2006-01-02T15:04Z07:00",
		"2006-01-02T15:04:05.999999999", "2006-01-02T15:04:05", "2006-01-02T15:04",
		"2006-01-02", "2006-01", "2006",
	} {
		t, err := time.Parse(layout, s)
		if err != nil {
			continue
		}
		if t.Location() != time.UTC {
			if _, off := t.Zone(); off == 0 {
				t = t.UTC()
			} else {
				t = t.In(time.FixedZone("", off))
			}
		}
		return t
	}
	return time.Time{}
}
