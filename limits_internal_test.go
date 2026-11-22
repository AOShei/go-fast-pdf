package gofastpdf

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"testing"

	gofastcore "github.com/AOShei/go-fast-core"
	"github.com/AOShei/go-fast-pdf/internal/content"
	"github.com/AOShei/go-fast-pdf/internal/extract"
	"github.com/AOShei/go-fast-pdf/internal/file"
	"github.com/AOShei/go-fast-pdf/internal/pdftest"
)

// What a fixed bound of the library leaves out is reported as
// limit.exceeded, an error, on the part or the document it is left out
// of: each bound, past it by a little, in a file made for it.

func limitsDoc(t *testing.T, e *Extractor, data []byte, opts *gofastcore.Options) *gofastcore.Document {
	t.Helper()
	if opts == nil {
		opts = &gofastcore.Options{}
	}
	doc, err := e.Extract(context.Background(), bytes.NewReader(data), int64(len(data)), opts)
	if err != nil {
		t.Fatal(err)
	}
	if err := doc.Validate(); err != nil {
		t.Fatal(err)
	}
	return doc
}

// limitIn reports whether ws has a limit.exceeded, an error, whose
// message has sub.
func limitIn(ws []gofastcore.Warning, sub string) bool {
	for _, w := range ws {
		if w.Code == gofastcore.WarnLimitExceeded && w.Severity == gofastcore.SeverityError && strings.Contains(w.Message, sub) {
			return true
		}
	}
	return false
}

func noLimits(t *testing.T, doc *gofastcore.Document) {
	t.Helper()
	ws := append([]gofastcore.Warning(nil), doc.Warnings...)
	for _, p := range doc.Parts {
		ws = append(ws, p.Warnings...)
	}
	for _, w := range ws {
		if w.Code == gofastcore.WarnLimitExceeded {
			t.Errorf("under the bound: %s", w.Message)
		}
	}
}

// noCut counts nothing.
var noCut cutFunc = func(*file.Bound, string, int64, int64) {}

// catalog rewrites the catalog of a document that pdftest.Doc made, with
// more entries.
func catalog(b *pdftest.Builder, entries string) {
	b.Set(b.Root, fmt.Sprintf("<< /Type /Catalog /Pages %d 0 R %s >>", b.Root+1, entries))
}

// The audit's first probe: a TJ array of more elements than are kept.
func TestLimitArrayElements(t *testing.T) {
	text := func(n int) []byte {
		return pdftest.OnePage("BT /F1 12 Tf 72 700 Td [" + strings.Repeat("(a)", n) + "] TJ ET")
	}
	doc := limitsDoc(t, &Extractor{}, text(content.MaxElems+1000), nil)
	if !limitIn(doc.Parts[0].Warnings, fmt.Sprintf("elements of an operator's arrays past %d were not read (1000 elements)", content.MaxElems)) {
		t.Errorf("past the bound: %v", doc.Parts[0].Warnings)
	}
	noLimits(t, limitsDoc(t, &Extractor{}, text(1000), nil))
}

// The second probe: a ToUnicode text longer than one code's text may be
// is cut, not dropped: the character is read as the beginning of its
// text, not by the encoding, and the page says so.
func TestLimitToUnicodeEntry(t *testing.T) {
	build := func(n int) []byte {
		b := pdftest.New()
		cmap := fmt.Sprintf("begincmap\n1 begincodespacerange <00> <FF> endcodespacerange\n1 beginbfchar\n<41> <%s>\nendbfchar\nendcmap", strings.Repeat("0042", n))
		tu := b.AddStream("", []byte(cmap))
		font := fmt.Sprintf("<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding /ToUnicode %d 0 R >>", tu)
		return pdftest.Doc(pdftest.DocOptions{Base: b, Font: font}, pdftest.PageSpec{Content: "BT /F1 12 Tf 72 700 Td (A) Tj ET"}).Bytes()
	}
	doc := limitsDoc(t, &Extractor{}, build(4200), nil)
	if got := strings.Count(doc.Text(), "B"); got != 4096 || strings.Contains(doc.Text(), "A") {
		t.Errorf("%d Bs, the A there: %v", got, strings.Contains(doc.Text(), "A"))
	}
	if !limitIn(doc.Parts[0].Warnings, "texts of character codes in ToUnicode maps longer than 4096 bytes were cut there (1 texts, 104 bytes left out)") {
		t.Errorf("no warning: %v", doc.Parts[0].Warnings)
	}
	doc = limitsDoc(t, &Extractor{}, build(4000), nil)
	if strings.Count(doc.Text(), "B") != 4000 {
		t.Errorf("under the bound: %d Bs", strings.Count(doc.Text(), "B"))
	}
	noLimits(t, doc)
}

// The third probe: an outline deeper than is read, and one longer.
func TestLimitOutline(t *testing.T) {
	deep := func(levels int) []byte {
		b := pdftest.Doc(pdftest.DocOptions{}, pdftest.PageSpec{Content: "BT /F1 12 Tf 72 700 Td (x) Tj ET"})
		first := b.Reserve()
		outlines := b.Add(fmt.Sprintf("<< /Type /Outlines /First %d 0 R >>", first))
		n := first
		for level := 1; level <= levels; level++ {
			next := 0
			if level < levels {
				next = b.Reserve()
			}
			child := ""
			if next != 0 {
				child = fmt.Sprintf("/First %d 0 R", next)
			}
			b.Set(n, fmt.Sprintf("<< /Title (L%d) /Parent %d 0 R %s >>", level, outlines, child))
			n = next
		}
		catalog(b, fmt.Sprintf("/Outlines %d 0 R", outlines))
		return b.Bytes()
	}
	doc := limitsDoc(t, &Extractor{}, deep(70), nil)
	if len(doc.Outline) != maxOutlineDepth || !limitIn(doc.Warnings, "the outline has more than 64 levels; the rest were not read") {
		t.Errorf("%d entries; %v", len(doc.Outline), doc.Warnings)
	}
	noLimits(t, limitsDoc(t, &Extractor{}, deep(20), nil))

	b := pdftest.Doc(pdftest.DocOptions{}, pdftest.PageSpec{Content: "BT /F1 12 Tf 72 700 Td (x) Tj ET"})
	outlines := b.Reserve()
	items := maxOutlineItems + 5
	nums := make([]int, items)
	for i := range nums {
		nums[i] = b.Reserve()
	}
	for i, n := range nums {
		next := ""
		if i+1 < items {
			next = fmt.Sprintf("/Next %d 0 R", nums[i+1])
		}
		b.Set(n, fmt.Sprintf("<< /Title (x) /Parent %d 0 R %s >>", outlines, next))
	}
	b.Set(outlines, fmt.Sprintf("<< /Type /Outlines /First %d 0 R >>", nums[0]))
	catalog(b, fmt.Sprintf("/Outlines %d 0 R", outlines))
	doc = limitsDoc(t, &Extractor{}, b.Bytes(), nil)
	if len(doc.Outline) != maxOutlineItems || !limitIn(doc.Warnings, "the outline has more than 131072 entries; the rest were not read") {
		t.Errorf("%d entries; %v", len(doc.Outline), doc.Warnings)
	}
}

// The fourth probe: a choice field with more options than are read.
func TestLimitChoices(t *testing.T) {
	build := func(n int) []byte {
		b := pdftest.New()
		field := b.Reserve()
		b = pdftest.Doc(pdftest.DocOptions{Base: b}, pdftest.PageSpec{Content: "BT /F1 12 Tf 72 700 Td (x) Tj ET", Extra: fmt.Sprintf("/Annots [%d 0 R]", field)})
		var opts strings.Builder
		for i := 0; i < n; i++ {
			fmt.Fprintf(&opts, "(opt%d) ", i)
		}
		b.Set(field, fmt.Sprintf("<< /Type /Annot /Subtype /Widget /FT /Ch /T (choice) /Rect [72 600 200 620] /Opt [%s] >>", opts.String()))
		catalog(b, fmt.Sprintf("/AcroForm << /Fields [%d 0 R] >>", field))
		return b.Bytes()
	}
	doc := limitsDoc(t, &Extractor{}, build(maxChoices+100), nil)
	if !limitIn(doc.Warnings, "options of choice fields past 4096 were not read (100 options)") || !strings.Contains(doc.Text(), fmt.Sprintf("opt%d", maxChoices-1)) ||
		strings.Contains(doc.Text(), fmt.Sprintf("opt%d", maxChoices)) {
		t.Errorf("past the bound: %v", doc.Warnings)
	}
	noLimits(t, limitsDoc(t, &Extractor{}, build(10), nil))
}

// The fifth probe: a name longer than is kept, a key of the document
// information.
func TestLimitName(t *testing.T) {
	build := func(n int) []byte {
		b := pdftest.Doc(pdftest.DocOptions{}, pdftest.PageSpec{Content: "BT /F1 12 Tf 72 700 Td (x) Tj ET"})
		info := b.Add(fmt.Sprintf("<< /%s (a value of the file's own) >>", strings.Repeat("K", n)))
		b.Trailer = fmt.Sprintf("/Info %d 0 R", info)
		return b.Bytes()
	}
	opts := &gofastcore.Options{}
	doc := limitsDoc(t, &Extractor{}, build(1500), opts)
	if !limitIn(doc.Warnings, "names longer than 1024 bytes were cut there (1 names, 476 bytes left out)") {
		t.Errorf("past the bound: %v", doc.Warnings)
	}
	noLimits(t, limitsDoc(t, &Extractor{}, build(100), opts))
}

// The sixth probe: more annotations on a page than are read. The page
// says how many are not.
func TestLimitAnnotations(t *testing.T) {
	build := func(n int) []byte {
		b := pdftest.New()
		square := b.Add("<< /Type /Annot /Subtype /Square /Rect [0 0 10 10] >>")
		note := b.Add("<< /Type /Annot /Subtype /Text /Rect [0 0 10 10] /Contents (the note) >>")
		annots := strings.Repeat(fmt.Sprintf("%d 0 R ", square), n) + fmt.Sprintf("%d 0 R", note)
		return pdftest.Doc(pdftest.DocOptions{Base: b}, pdftest.PageSpec{Content: "BT /F1 12 Tf 72 700 Td (x) Tj ET", Extra: "/Annots [" + annots + "]"}).Bytes()
	}
	doc := limitsDoc(t, &Extractor{}, build(extract.MaxAnnotations), nil)
	want := fmt.Sprintf("annotations of a page past %d were not read (1 annotations)", extract.MaxAnnotations)
	if !limitIn(doc.Parts[0].Warnings, want) || strings.Contains(doc.Text(), "the note") {
		t.Errorf("past the bound: %v", doc.Parts[0].Warnings)
	}
	doc = limitsDoc(t, &Extractor{}, build(100), nil)
	if !strings.Contains(doc.Text(), "the note") {
		t.Error("under the bound, the note is missing")
	}
	noLimits(t, doc)
}

// Bounds of the content of a page: nesting of q and of marked content, an
// inline image's dictionary.
func TestLimitPageContent(t *testing.T) {
	for _, c := range []struct {
		content, want string
	}{
		{strings.Repeat("/P BMC ", 4100) + "BT /F1 12 Tf 72 700 Td (x) Tj ET", "marked-content sequences nested past 4096 were not read (4 sequences)"},
		{"BT /F1 12 Tf 72 700 Td (x) Tj ET BI /W 1 /H 1 /CS /G /BPC 8 /X (" + strings.Repeat("x", 70000) + ") ID \x80 EI", "dictionaries of inline images longer than 65536 bytes were not read"},
	} {
		doc := limitsDoc(t, &Extractor{}, pdftest.OnePage(c.content), nil)
		if !limitIn(doc.Parts[0].Warnings, c.want) {
			t.Errorf("%q: %v", c.want, doc.Parts[0].Warnings)
		}
	}
	noLimits(t, limitsDoc(t, &Extractor{}, pdftest.OnePage(strings.Repeat("q ", 100)+strings.Repeat("/P BMC ", 100)+"BT /F1 12 Tf 72 700 Td (x) Tj ET"), nil))
	// Graphics states past the bound place text and are not in it: no
	// warning.
	noLimits(t, limitsDoc(t, &Extractor{}, pdftest.OnePage(strings.Repeat("q ", 4100)+"BT /F1 12 Tf 72 700 Td (x) Tj ET"), nil))
}

// Two bounds on one page give two warnings: limit.exceeded is kept once
// for each bound, not once for the page.
func TestLimitTwoBoundsOnAPage(t *testing.T) {
	doc := limitsDoc(t, &Extractor{}, pdftest.OnePage(strings.Repeat("/P BMC ", 4100)+"BT /F1 12 Tf 72 700 Td (x) Tj ET BI /W 1 /H 1 /CS /G /BPC 8 /X ("+strings.Repeat("x", 70000)+") ID \x80 EI"), nil)
	n := 0
	for _, w := range doc.Parts[0].Warnings {
		if w.Code == gofastcore.WarnLimitExceeded {
			n++
		}
	}
	if n != 2 {
		t.Errorf("%d limit warnings: %v", n, doc.Parts[0].Warnings)
	}
}

// A link's address longer than is kept.
func TestLimitHref(t *testing.T) {
	build := func(n int) []byte {
		b := pdftest.New()
		link := b.Add(fmt.Sprintf("<< /Type /Annot /Subtype /Link /Rect [72 690 200 710] /A << /S /URI /URI (https://example.com/%s) >> >>", strings.Repeat("a", n)))
		return pdftest.Doc(pdftest.DocOptions{Base: b}, pdftest.PageSpec{Content: "BT /F1 12 Tf 72 700 Td (linked) Tj ET", Extra: fmt.Sprintf("/Annots [%d 0 R]", link)}).Bytes()
	}
	opts := &gofastcore.Options{Spans: true}
	doc := limitsDoc(t, &Extractor{}, build(5000), opts)
	if !limitIn(doc.Parts[0].Warnings, "links whose address is longer than 4096 bytes were not kept (1 links)") {
		t.Errorf("past the bound: %v", doc.Parts[0].Warnings)
	}
	noLimits(t, limitsDoc(t, &Extractor{}, build(50), opts))
}

// Bounds of the structure tree: a figure's alternate description, and the
// identifiers of a page.
func TestLimitStructure(t *testing.T) {
	build := func(alt, mcids int) []byte {
		b := pdftest.New()
		root := b.Reserve()
		fig := b.Add(fmt.Sprintf("<< /Type /StructElem /S /Figure /P %d 0 R /Alt (%s) /K 0 >>", root, strings.Repeat("d", alt)))
		parents := strings.TrimSpace(strings.Repeat(fmt.Sprintf("%d 0 R ", fig), mcids))
		b.Set(root, fmt.Sprintf("<< /Type /StructTreeRoot /K [%d 0 R] /ParentTree << /Nums [0 [%s]] >> >>", fig, parents))
		page := "/P <</MCID 0>> BDC BT /F1 12 Tf 72 700 Td (figure) Tj ET EMC"
		d := pdftest.Doc(pdftest.DocOptions{Base: b}, pdftest.PageSpec{Content: page, Extra: "/StructParents 0"})
		catalog(d, fmt.Sprintf("/StructTreeRoot %d 0 R /MarkInfo << /Marked true >>", root))
		return d.Bytes()
	}
	doc := limitsDoc(t, &Extractor{}, build(70000, 1), nil)
	if !limitIn(doc.Parts[0].Warnings, "descriptions of figures longer than 65536 bytes were cut there (1 descriptions, 4464 bytes left out)") {
		t.Errorf("Alt: %v", doc.Parts[0].Warnings)
	}
	doc = limitsDoc(t, &Extractor{}, build(10, maxPageMCIDs+1), nil)
	if !limitIn(doc.Parts[0].Warnings, "marked-content identifiers of a page past 65536 were not looked up") || !limitIn(doc.Parts[0].Warnings, "(1 identifiers)") {
		t.Errorf("identifiers: %v", doc.Parts[0].Warnings)
	}
	noLimits(t, limitsDoc(t, &Extractor{}, build(10, 1), nil))
}

// An image larger than the images whose data is made: limit.exceeded, not
// an encoding that is not delivered.
func TestLimitImagePixels(t *testing.T) {
	b := pdftest.New()
	img := b.AddStream("/Type /XObject /Subtype /Image /Width 2000000 /Height 1 /ColorSpace /DeviceGray /BitsPerComponent 8", []byte("\x80"))
	data := pdftest.Doc(pdftest.DocOptions{Base: b}, pdftest.PageSpec{
		Content:   "q 100 0 0 100 72 500 cm /Im Do Q",
		Resources: fmt.Sprintf("/XObject << /Im %d 0 R >>", img),
	}).Bytes()
	doc := limitsDoc(t, &Extractor{}, data, &gofastcore.Options{Images: gofastcore.ImagesData})
	if !limitIn(doc.Parts[0].Warnings, "images of more than 268435456 pixels, or 1048576 on a side, were not given their data (1 images)") || doc.Parts[0].Has(gofastcore.WarnImageUnsupportedEncoding) {
		t.Errorf("%v", doc.Parts[0].Warnings)
	}
}

// Bounds of the document: a name tree too deep, a form field with too
// many fields above it, the entries of the document information, the
// scripts, the attached files and the fields of the form.
func TestLimitDocument(t *testing.T) {
	page := pdftest.PageSpec{Content: "BT /F1 12 Tf 72 700 Td (x) Tj ET"}
	stored := &gofastcore.Options{}

	// A name tree nested past its depth.
	b := pdftest.Doc(pdftest.DocOptions{}, page)
	leaf := b.Add("<< /Names [(f.txt) << /Type /Filespec /F (f.txt) >>] >>")
	node := leaf
	for i := 0; i < maxTreeDepth+3; i++ {
		node = b.Add(fmt.Sprintf("<< /Kids [%d 0 R] >>", node))
	}
	catalog(b, fmt.Sprintf("/Names << /EmbeddedFiles %d 0 R >>", node))
	if doc := limitsDoc(t, &Extractor{}, b.Bytes(), nil); !limitIn(doc.Warnings, "name trees and number trees past") || !limitIn(doc.Warnings, "(1 trees)") {
		t.Errorf("tree: %v", doc.Warnings)
	}

	// A field with more fields above it than are read.
	b = pdftest.New()
	widget := b.Reserve()
	b = pdftest.Doc(pdftest.DocOptions{Base: b}, pdftest.PageSpec{Content: page.Content, Extra: fmt.Sprintf("/Annots [%d 0 R]", widget)})
	parent := b.Add("<< /T (top) >>")
	for i := 0; i < maxFieldDepth+3; i++ {
		parent = b.Add(fmt.Sprintf("<< /T (f%d) /Kids [] /Parent %d 0 R >>", i, parent))
	}
	b.Set(widget, fmt.Sprintf("<< /Type /Annot /Subtype /Widget /FT /Tx /T (leaf) /V (the value) /Rect [72 600 200 620] /Parent %d 0 R >>", parent))
	catalog(b, fmt.Sprintf("/AcroForm << /Fields [%d 0 R] >>", widget))
	if doc := limitsDoc(t, &Extractor{}, b.Bytes(), nil); !limitIn(doc.Warnings, "the names of form fields past 32 levels of fields above them were not read (1 fields)") {
		t.Errorf("field depth: %v", doc.Warnings)
	}

	// More entries in the document information than are read.
	b = pdftest.Doc(pdftest.DocOptions{}, page)
	var info strings.Builder
	for i := 0; i < maxVariables+10; i++ {
		fmt.Fprintf(&info, "/Key%d (value %d) ", i, i)
	}
	b.Trailer = fmt.Sprintf("/Info %d 0 R", b.Add("<< "+info.String()+">>"))
	if doc := limitsDoc(t, &Extractor{}, b.Bytes(), stored); !limitIn(doc.Warnings, "entries of the document information past 1024 were not read (10 entries)") {
		t.Errorf("information: %v", doc.Warnings)
	}

	// More scripts, attached files and fields than are read.
	b = pdftest.Doc(pdftest.DocOptions{}, page)
	js := b.Add("<< /S /JavaScript /JS (app.alert(1)) >>")
	spec := b.Add("<< /Type /Filespec /F (f.txt) >>")
	field := b.Add("<< /FT /Tx /T (f) >>")
	var scripts, files strings.Builder
	for i := 0; i <= maxScripts; i++ {
		fmt.Fprintf(&scripts, "(s%05d) %d 0 R ", i, js)
		fmt.Fprintf(&files, "(f%05d) %d 0 R ", i, spec)
	}
	fields := strings.Repeat(fmt.Sprintf("%d 0 R ", field), maxFields+1)
	catalog(b, fmt.Sprintf("/Names << /JavaScript << /Names [%s] >> /EmbeddedFiles << /Names [%s] >> >> /AcroForm << /Fields [%s] >>",
		scripts.String(), files.String(), fields))
	doc := limitsDoc(t, &Extractor{}, b.Bytes(), stored)
	for _, want := range []string{"scripts past 4096 were not read (1 scripts)", "attached files past 4096 were not read (1 files)", "the form has more than 65536 fields; the rest were not read"} {
		if !limitIn(doc.Warnings, want) {
			t.Errorf("%s: %v", want, doc.Warnings)
		}
	}

	noLimits(t, limitsDoc(t, &Extractor{}, pdftest.Doc(pdftest.DocOptions{}, page).Bytes(), stored))
}

// An attached file over the bound on one stream is reported as that, not
// as the document's decoding budget.
func TestLimitAttachmentStream(t *testing.T) {
	b := pdftest.Doc(pdftest.DocOptions{}, pdftest.PageSpec{Content: "BT /F1 12 Tf 72 700 Td (x) Tj ET"})
	ef := b.AddStream("/Type /EmbeddedFile", bytes.Repeat([]byte("attached "), 100))
	spec := b.Add(fmt.Sprintf("<< /Type /Filespec /F (notes.txt) /EF << /F %d 0 R >> >>", ef))
	catalog(b, fmt.Sprintf("/Names << /EmbeddedFiles << /Names [(notes.txt) %d 0 R] >> >>", spec))
	child := func(ctx context.Context, name, mime string, r io.ReaderAt, size int64, opts *gofastcore.Options) (*gofastcore.Document, error) {
		d := gofastcore.New("text")
		d.Warn(gofastcore.SeverityInfo, gofastcore.WarnPartNoText, "a test child")
		return d, nil
	}
	doc := limitsDoc(t, &Extractor{MaxStreamBytes: 100}, b.Bytes(), &gofastcore.Options{Child: child})
	if !limitIn(doc.Warnings, "attached files that decode to more than the limit of 100 bytes for one stream were not extracted (1 files)") || limitIn(doc.Warnings, "the document decodes to more") {
		t.Errorf("%v", doc.Warnings)
	}
}

// The readers of XML: the values of a property, the attributes of an
// element and the properties of a metadata packet; the pieces of text of
// an XFA field and its nesting.
func TestLimitXML(t *testing.T) {
	var got []string
	warn := cutFunc(func(b *file.Bound, _ string, n, bytes int64) { got = append(got, b.Message(n, bytes)) })
	has := func(sub string) bool {
		for _, m := range got {
			if strings.Contains(m, sub) {
				return true
			}
		}
		return false
	}
	bag := "<rdf:RDF xmlns:rdf=\"http://www.w3.org/1999/02/22-rdf-syntax-ns#\" xmlns:dc=\"http://purl.org/dc/elements/1.1/\"><rdf:Description><dc:subject><rdf:Bag>" +
		strings.Repeat("<rdf:li>k</rdf:li>", maxXMPValues+5) + "</rdf:Bag></dc:subject></rdf:Description></rdf:RDF>"
	packetProperties(bag, warn)
	if !has("values") {
		t.Errorf("values: %v", got)
	}
	var attrs strings.Builder
	for i := 0; i < maxXMLAttrs+5; i++ {
		fmt.Fprintf(&attrs, " x:a%d=\"v\"", i)
	}
	got = nil
	packetProperties("<rdf:RDF><rdf:Description"+attrs.String()+"/></rdf:RDF>", warn)
	if !has("attributes") {
		t.Errorf("attributes: %v", got)
	}
	var props strings.Builder
	for i := 0; i < maxVariables+5; i++ {
		fmt.Fprintf(&props, "<x:p%d>v</x:p%d>", i, i)
	}
	got = nil
	packetProperties("<rdf:RDF><rdf:Description>"+props.String()+"</rdf:Description></rdf:RDF>", warn)
	if !has("properties") {
		t.Errorf("properties: %v", got)
	}
	var many strings.Builder
	for i := 0; i < 5; i++ {
		fmt.Fprintf(&many, "<x:q%d><rdf:Bag>%s</rdf:Bag></x:q%d>", i, strings.Repeat("<rdf:li>k</rdf:li>", maxXMPTotal/4), i)
	}
	got = nil
	packetProperties("<rdf:RDF><rdf:Description>"+many.String()+"</rdf:Description></rdf:RDF>", warn)
	if !has("values of the metadata packet past 65536") || has("values of one property") {
		t.Errorf("all the values: %v", got)
	}
	got = nil
	packetProperties("<rdf:RDF><rdf:Description>"+strings.Repeat("<x:p>v</x:p>", 10)+"</rdf:Description></rdf:RDF>", warn)
	if len(got) != 0 {
		t.Errorf("under the bounds: %v", got)
	}

	got = nil
	xfaValues("<xfa:datasets><xfa:data><f>"+strings.Repeat("a<b/>", maxXFAPieces+5)+"</f></xfa:data></xfa:datasets>", map[string]bool{}, warn)
	if !has("pieces of text") {
		t.Errorf("pieces: %v", got)
	}
	got = nil
	xfaValues("<xfa:datasets><xfa:data>"+strings.Repeat("<n>", maxXFADepth+5)+"v"+strings.Repeat("</n>", maxXFADepth+5)+"</xfa:data></xfa:datasets>", map[string]bool{}, warn)
	if !has("data has more than 64 levels") {
		t.Errorf("nesting: %v", got)
	}
}

// A font program longer than is read: the page that shows text in the
// font says that its text may be wrong. Zeros compress to little, so the
// file is small.
func TestLimitFontProgram(t *testing.T) {
	build := func(size int) []byte {
		b := pdftest.New()
		prog := b.AddFlate("", make([]byte, size))
		desc := b.Add(fmt.Sprintf("<< /Type /FontDescriptor /FontName /Embedded /Flags 32 /FontBBox [0 0 1000 1000] /ItalicAngle 0 /Ascent 800 /Descent -200 /CapHeight 700 /StemV 80 /FontFile2 %d 0 R >>", prog))
		font := fmt.Sprintf("<< /Type /Font /Subtype /TrueType /BaseFont /Embedded /FirstChar 65 /LastChar 66 /Widths [600 600] /FontDescriptor %d 0 R >>", desc)
		return pdftest.Doc(pdftest.DocOptions{Base: b, Font: font}, pdftest.PageSpec{Content: "BT /F1 12 Tf 72 700 Td (AB) Tj ET"}).Bytes()
	}
	doc := limitsDoc(t, &Extractor{}, build(33<<20), nil)
	if !limitIn(doc.Parts[0].Warnings, "font programs longer than 33554432 bytes were cut there") {
		t.Errorf("past the bound: %v", doc.Parts[0].Warnings)
	}
	noLimits(t, limitsDoc(t, &Extractor{}, build(1<<10), nil))
}

// The counting of the readers of XML goes to the document: by id once,
// without one each time.
func TestCutter(t *testing.T) {
	f, err := file.Open(pdftest.OnePage("BT ET"), file.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	c := cutter(f)
	c(boundXMLAttrs, "", 2, 0)
	c(boundXMLAttrs, "", 1, 0)
	c(boundName, "a", 1, 10)
	c(boundName, "a", 1, 10)
	var got []string
	for _, w := range f.Warnings() {
		got = append(got, w.Message)
	}
	want := []string{"names longer than 1024 bytes were cut there (1 names, 10 bytes left out)", "attributes of one XML element past 256 were not read (3 attributes)"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("%q\nwant %q", got, want)
	}
	cutDoc(nil, boundName, 1, 1) // no file: nothing, and no panic
}
