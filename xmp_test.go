package gofastpdf_test

import (
	"strings"
	"testing"
	"time"

	"github.com/AOShei/go-fast-pdf/internal/pdftest"
)

// xmpDoc is a document of one page with a metadata packet whose
// rdf:Description holds body, and with info as its document
// information.
func xmpDoc(body, info string) synthDoc {
	packet := `<?xpacket begin="" id="W5M0MpCehiHzreSzNTczkc9d"?>
<x:xmpmeta xmlns:x="adobe:ns:meta/">
 <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
  <rdf:Description rdf:about=""
    xmlns:d="http://purl.org/dc/elements/1.1/"
    xmlns:dc="http://example.com/not-dublin-core/"
    xmlns:pdf="http://ns.adobe.com/pdf/1.3/"
    xmlns:xmp="http://ns.adobe.com/xap/1.0/">` + body + `
  </rdf:Description>
 </rdf:RDF>
</x:xmpmeta>
<?xpacket end="w"?>`
	return synthDoc{
		Objects: []string{pdftest.Stream("/Type /Metadata /Subtype /XML", packet)},
		Catalog: "/Metadata 1 0 R",
		Info:    info,
		Pages:   []synthPage{{Content: pdftest.TextPage("A page.")}},
	}
}

// The lists of the packet are the authors and the keywords; what the
// document information leaves out is taken from the packet.
func TestXMPMetadata(t *testing.T) {
	body := `
   <d:creator><rdf:Seq><rdf:li>First Author</rdf:li><rdf:li> Second Author </rdf:li><rdf:li> </rdf:li></rdf:Seq></d:creator>
   <d:subject><rdf:Bag><rdf:li>fish</rdf:li><rdf:li>chips, vinegar</rdf:li></rdf:Bag></d:subject>
   <pdf:Keywords>fish, chips, vinegar</pdf:Keywords>
   <d:description><rdf:Alt><rdf:li xml:lang="x-default">Fish &amp; <![CDATA[chips]]></rdf:li></rdf:Alt></d:description>
   <d:language><rdf:Bag><rdf:li>fr</rdf:li></rdf:Bag></d:language>
   <dc:title>Not a title: another namespace under the prefix dc</dc:title>
   <xmp:CreatorTool>Writer</xmp:CreatorTool>
   <xmp:CreateDate>2025-10-21T15:09:24+02:00</xmp:CreateDate>
   <xmp:MetadataDate>2025-10-21T15:09:24+02:00</xmp:MetadataDate>`
	doc := mustExtract(t, xmpDoc(body, "/Author (First Author) /Keywords (fish, chips)").bytes(), nil)
	md := doc.Metadata
	check := func(what string, got, want any) {
		t.Helper()
		if !equal(got, want) {
			t.Errorf("%s: got %q, want %q", what, got, want)
		}
	}
	check("authors", md.Authors, []string{"First Author", "Second Author"})
	check("keywords", md.Keywords, []string{"fish", "chips, vinegar"}) // as the list has them, not split further
	check("subject", md.Subject, "Fish & chips")
	check("language", md.Language, "fr")
	check("title", md.Title, "")
	check("application", md.Application, "Writer")
	want := time.Date(2025, 10, 21, 15, 9, 24, 0, time.FixedZone("", 2*3600))
	if !md.Created.Equal(want) || md.Created.Location().String() != "" {
		t.Errorf("created %v in %q", md.Created, md.Created.Location())
	}
	// Said once: what the information now has is not in the stored part.
	for _, p := range doc.Parts {
		for _, b := range p.Blocks {
			if strings.Contains(b.Text, "Second Author") || strings.Contains(b.Text, "chips") {
				t.Errorf("said again in the %s part: %q", p.Kind, b.Text)
			}
		}
	}

	t.Run("the information changed after the packet", func(t *testing.T) {
		doc := mustExtract(t, xmpDoc(body, "/Author (First Author) /Keywords (fish, chips) /ModDate (D:20260101000000Z)").bytes(), nil)
		md := doc.Metadata
		check("authors", md.Authors, []string{"First Author"})
		check("keywords", md.Keywords, []string{"fish, chips"})
	})

	t.Run("keywords as one string", func(t *testing.T) {
		doc := mustExtract(t, xmpDoc(`<pdf:Keywords>fish, chips</pdf:Keywords><pdf:Producer>A Producer</pdf:Producer>`, "").bytes(), nil)
		md := doc.Metadata
		check("keywords", md.Keywords, []string{"fish, chips"})
		check("producer", md.Extra["producer"], "A Producer")
		check("authors", md.Authors, []string(nil))
	})

	t.Run("a packet in another order of namespaces", func(t *testing.T) {
		// The prefix the packet gives the namespace does not matter.
		doc := mustExtract(t, xmpDoc(`<d:title><rdf:Alt><rdf:li xml:lang="x-default">The Title</rdf:li></rdf:Alt></d:title>`, "").bytes(), nil)
		check("title", doc.Metadata.Title, "The Title")
	})
}

func equal(a, b any) bool {
	switch a := a.(type) {
	case []string:
		b := b.([]string)
		if len(a) != len(b) {
			return false
		}
		for i := range a {
			if a[i] != b[i] {
				return false
			}
		}
		return true
	}
	return a == b
}

// A packet does not change what is extracted from the pages.
func TestXMPLeavesPages(t *testing.T) {
	with := mustExtract(t, xmpDoc(`<d:creator><rdf:Seq><rdf:li>A</rdf:li></rdf:Seq></d:creator>`, "").bytes(), nil)
	if len(with.Parts) != 1 || len(with.Parts[0].Blocks) != 1 || with.Parts[0].Blocks[0].Text != "A page." {
		t.Errorf("%+v", with.Parts)
	}
}
