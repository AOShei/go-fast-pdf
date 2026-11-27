package gofastpdf_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	gofastcore "github.com/AOShei/go-fast-core"
	"github.com/AOShei/go-fast-core/cfb"
	gofastpdf "github.com/AOShei/go-fast-pdf"
	"github.com/AOShei/go-fast-pdf/internal/xpstest"
)

// xpsDoc extracts an XPS document made of the pages given, each the
// inside of a FixedPage of 816 by 1056 units.
func xpsDoc(t *testing.T, opts *gofastcore.Options, bodies ...string) *gofastcore.Document {
	t.Helper()
	pages := make([]string, len(bodies))
	for i, b := range bodies {
		pages[i] = xpstest.Page(xpstest.XPS, 816, 1056, b)
	}
	return xpsExtract(t, opts, xpstest.Document(xpstest.XPS, pages...).Bytes())
}

func xpsExtract(t *testing.T, opts *gofastcore.Options, data []byte) *gofastcore.Document {
	t.Helper()
	doc, err := gofastpdf.Extract(context.Background(), bytes.NewReader(data), int64(len(data)), opts)
	if err != nil {
		t.Fatal(err)
	}
	if err := doc.Validate(); err != nil {
		t.Fatalf("the document is not valid: %v", err)
	}
	return doc
}

// xpsBlocks returns the texts of the blocks of a part, with their
// kinds.
func xpsBlocks(p *gofastcore.Part) []string {
	var out []string
	for i := range p.Blocks {
		out = append(out, fmt.Sprint(p.Blocks[i].Kind)+": "+p.Blocks[i].Text)
	}
	return out
}

func wantBlocks(t *testing.T, p *gofastcore.Part, want ...string) {
	t.Helper()
	got := xpsBlocks(p)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("blocks:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func xpsWarned(ws []gofastcore.Warning, code string) bool {
	for _, w := range ws {
		if w.Code == code {
			return true
		}
	}
	return false
}

// TestXPSText: the text of a page, placed by the advances of the font,
// comes out in lines and paragraphs as that of a PDF page does; the page
// has its size in points.
func TestXPSText(t *testing.T) {
	g := xpstest.Glyphs
	doc := xpsDoc(t, nil,
		g(96, 120, 16, "A first line of the paragraph that goes", "")+
			g(96, 140, 16, "on in a second line and ends here.", "")+
			g(96, 200, 16, "Another paragraph.", ""))
	if doc.Source.Format != "xps" || doc.Source.MIME != "application/vnd.ms-xpsdocument" {
		t.Errorf("source: %q %q", doc.Source.Format, doc.Source.MIME)
	}
	if len(doc.Parts) != 1 {
		t.Fatalf("%d parts", len(doc.Parts))
	}
	p := &doc.Parts[0]
	if p.Kind != gofastcore.PartPage || p.Number != 1 || p.Width != 612 || p.Height != 792 {
		t.Errorf("part: %v %d, %v by %v", p.Kind, p.Number, p.Width, p.Height)
	}
	wantBlocks(t, p,
		"paragraph: A first line of the paragraph that goes on in a second line and ends here.",
		"paragraph: Another paragraph.")
	// Where the first block is: 96 units are 72 points, and the line is
	// as wide as its glyphs advance (39 characters, 7 of them spaces of a
	// quarter of the size, at 16 units).
	box := p.Blocks[0].Loc.BBox
	width := (32*0.5 + 7*0.25) * 16 * 0.75
	if d := box.X0 - 72; d < -0.01 || d > 0.01 {
		t.Errorf("x0 = %v", box.X0)
	}
	if d := box.X1 - box.X0 - width; d < -0.01 || d > 0.01 {
		t.Errorf("width = %v, want %v", box.X1-box.X0, width)
	}
	if box.Y0 > 90 || box.Y1 < 105 || box.Y1 > 112 {
		t.Errorf("y: %v to %v", box.Y0, box.Y1)
	}
}

// TestXPSFormats: which of the two formats a file is, is said by the
// relationship to its start part and not by anything else; the parts
// are read the same way.
func TestXPSFormats(t *testing.T) {
	page := xpstest.Page(xpstest.OpenXPS, 816, 1056, xpstest.Glyphs(96, 120, 16, "An OpenXPS page.", ""))
	doc := xpsExtract(t, &gofastcore.Options{Name: "named.xps"}, xpstest.Document(xpstest.OpenXPS, page).Bytes())
	if doc.Source.Format != "oxps" || doc.Source.MIME != "application/oxps" {
		t.Errorf("source: %q %q", doc.Source.Format, doc.Source.MIME)
	}
	wantBlocks(t, &doc.Parts[0], "paragraph: An OpenXPS page.")

	if got := (&gofastpdf.Extractor{}).Formats(); strings.Join(got, " ") != "pdf xps oxps" {
		t.Errorf("formats: %v", got)
	}
	// A zip archive that is no XPS document is not read as one.
	other := xpstest.New().Add("word/document.xml", []byte("<a/>")).Bytes()
	if _, err := gofastpdf.Extract(context.Background(), bytes.NewReader(other), int64(len(other)), nil); !errors.Is(err, gofastcore.ErrMalformed) {
		t.Errorf("another zip archive: %v", err)
	}
}

// TestXPSIndices: the advances and the clusters that the indices state.
func TestXPSIndices(t *testing.T) {
	g := xpstest.Glyphs
	// A glyph that advances further than it is wide leaves a gap, which
	// is a space where the text has none.
	doc := xpsDoc(t, nil, g(96, 120, 20, "onetwo", `Indices=";;,150;;;"`))
	wantBlocks(t, &doc.Parts[0], "paragraph: one two")

	// One glyph for two characters, and two glyphs for one.
	doc = xpsDoc(t, nil, g(96, 120, 20, "office x", fmt.Sprintf(`Indices=";(2:1)%d,60;;;;(1:2)%d;%d;"`, xpstest.Glyph('f'), xpstest.Glyph('e'), xpstest.Glyph('e'))))
	wantBlocks(t, &doc.Parts[0], "paragraph: office x")

	// A string that begins with a brace is written after an empty pair.
	doc = xpsDoc(t, nil, g(96, 120, 20, "{braces}", ""))
	wantBlocks(t, &doc.Parts[0], "paragraph: {braces}")

	// Spaces set wider than the font size stand between two entries.
	doc = xpsDoc(t, nil, g(96, 120, 20, "left right", `Indices=";;;;,400"`))
	if got := doc.Parts[0].Blocks[0].Text; got != "left\tright" && got != "left right" {
		t.Errorf("wide space: %q", got)
	}
}

// TestXPSNoUnicodeString: glyphs without text are read by the table of
// characters of the font, turned round; one the table does not have is
// U+FFFD and a warning.
func TestXPSNoUnicodeString(t *testing.T) {
	ind := func(s string) string {
		var parts []string
		for _, r := range s {
			parts = append(parts, fmt.Sprint(xpstest.Glyph(r)))
		}
		return strings.Join(parts, ";")
	}
	body := fmt.Sprintf(`<Glyphs Fill="#FF000000" FontUri="%s" FontRenderingEmSize="16" OriginX="96" OriginY="120" Indices="%s"/>`, xpstest.FontName, ind("By glyphs"))
	doc := xpsDoc(t, nil, body)
	wantBlocks(t, &doc.Parts[0], "paragraph: By glyphs")

	body = fmt.Sprintf(`<Glyphs Fill="#FF000000" FontUri="%s" FontRenderingEmSize="16" OriginX="96" OriginY="120" Indices="%s;900"/>`, xpstest.FontName, ind("No"))
	doc = xpsDoc(t, nil, body)
	wantBlocks(t, &doc.Parts[0], "paragraph: No�")
	if !xpsWarned(doc.Parts[0].Warnings, gofastcore.WarnTextUnmappedGlyphs) {
		t.Errorf("no warning: %v", doc.Parts[0].Warnings)
	}
}

// TestXPSFonts: an obfuscated font is read, a font in a part written in
// UTF-16 too, and text in a font that is not there is read all the same,
// with a warning.
func TestXPSFonts(t *testing.T) {
	// Two words placed by the font's advances: with the font, the gap
	// between them is seen to be a space; the second begins 4.5 glyphs
	// of 10 units after the first.
	glyphs := func(font string) string {
		return fmt.Sprintf(`<Glyphs Fill="#FF000000" FontUri="%s" FontRenderingEmSize="20" OriginX="96" OriginY="120" UnicodeString="word"/>`+
			`<Glyphs Fill="#FF000000" FontUri="%s" FontRenderingEmSize="20" OriginX="141" OriginY="120" UnicodeString="next"/>`, font, font)
	}
	pkg := xpstest.Document(xpstest.XPS, xpstest.Page(xpstest.XPS, 816, 1056, glyphs(xpstest.FontGUID)))
	pkg.Remove(xpstest.FontName)
	pkg.Add(xpstest.FontGUID, xpstest.Obfuscate(xpstest.Font(nil), xpstest.FontGUID))
	doc := xpsExtract(t, nil, pkg.Bytes())
	wantBlocks(t, &doc.Parts[0], "paragraph: word next")
	if len(doc.Parts[0].Warnings) != 0 {
		t.Errorf("warnings: %v", doc.Parts[0].Warnings)
	}

	// The page in UTF-16.
	pkg = xpstest.Document(xpstest.XPS, "")
	pkg.Add("Documents/1/Pages/1.fpage", xpstest.UTF16(xpstest.Page(xpstest.XPS, 816, 1056, xpstest.Glyphs(96, 120, 16, "Écrit en UTF-16 — voilà.", ""))))
	doc = xpsExtract(t, nil, pkg.Bytes())
	wantBlocks(t, &doc.Parts[0], "paragraph: Écrit en UTF-16 — voilà.")

	// No font: the text is what the page says.
	pkg = xpstest.Document(xpstest.XPS, xpstest.Page(xpstest.XPS, 816, 1056, xpstest.Glyphs(96, 120, 16, "Text without its font.", "")))
	pkg.Remove(xpstest.FontName)
	doc = xpsExtract(t, nil, pkg.Bytes())
	wantBlocks(t, &doc.Parts[0], "paragraph: Text without its font.")
	if !xpsWarned(doc.Parts[0].Warnings, gofastpdf.WarnFontUnreadable) {
		t.Errorf("no warning: %v", doc.Parts[0].Warnings)
	}
}

// TestXPSTransforms: the transformations of the Canvas elements around
// a Glyphs element and its own, written as attributes or as elements,
// apply from the outermost to the innermost.
func TestXPSTransforms(t *testing.T) {
	g := xpstest.Glyphs
	body := `<Canvas RenderTransform="2,0,0,2,0,0">` +
		`<Canvas><Canvas.RenderTransform><MatrixTransform Matrix="1,0,0,1,10,20"/></Canvas.RenderTransform>` +
		g(38, 40, 8, "Moved", `RenderTransform="1,0,0,1,0,0"`) +
		`</Canvas></Canvas>` +
		`<Glyphs Fill="#FF000000" FontUri="` + xpstest.FontName + `" FontRenderingEmSize="16" OriginX="0" OriginY="0" UnicodeString="Own">` +
		`<Glyphs.RenderTransform><MatrixTransform Matrix="1,0,0,1,96,300"/></Glyphs.RenderTransform></Glyphs>`
	doc := xpsDoc(t, nil, body)
	p := &doc.Parts[0]
	wantBlocks(t, p, "paragraph: Moved", "paragraph: Own")
	// (38+10)*2 = 96 units, 72 points; the size is 16 units, 12 points:
	// five glyphs of half of it.
	box := p.Blocks[0].Loc.BBox
	if d := box.X0 - 72; d < -0.01 || d > 0.01 {
		t.Errorf("x0 = %v", box.X0)
	}
	if d := box.X1 - box.X0 - 30; d < -0.01 || d > 0.01 {
		t.Errorf("width = %v", box.X1-box.X0)
	}
	if box.Y1 < 88 || box.Y1 > 95 {
		t.Errorf("y1 = %v", box.Y1)
	}
	if box := p.Blocks[1].Loc.BBox; box.X0 < 71.9 || box.X0 > 72.1 || box.Y1 < 224 || box.Y1 > 230 {
		t.Errorf("own transform: %+v", box)
	}
}

// TestXPSRightToLeft: an element of an odd level is written from right
// to left from its origin, its text in the order it is read.
func TestXPSRightToLeft(t *testing.T) {
	g := xpstest.Glyphs
	// "shalom olam", the words in two elements, the first at the right.
	doc := xpsDoc(t, nil, g(300, 120, 20, "שלום ", `BidiLevel="1"`)+g(255, 120, 20, "עולם", `BidiLevel="1"`))
	wantBlocks(t, &doc.Parts[0], "paragraph: שלום עולם")
	// With a word in Latin letters between them, at the left of the
	// first and the right of the second.
	doc = xpsDoc(t, nil, g(300, 120, 20, "שלום ", `BidiLevel="1"`)+g(225, 120, 20, "abc", `BidiLevel="2"`)+g(220, 120, 20, "עולם", `BidiLevel="1"`))
	wantBlocks(t, &doc.Parts[0], "paragraph: שלום abc עולם")
}

// TestXPSDisplay: what the file declares of how text is drawn, and
// nothing else: an element without a fill has no visible effect, and
// neither has one that is fully transparent.
func TestXPSDisplay(t *testing.T) {
	glyphs := func(y float64, text, attrs string) string {
		return fmt.Sprintf(`<Glyphs FontUri="%s" FontRenderingEmSize="16" OriginX="96" OriginY="%g" UnicodeString="%s" %s/>`, xpstest.FontName, y, text, attrs)
	}
	body := glyphs(100, "Drawn in black.", `Fill="#FF000000"`) +
		glyphs(150, "No fill at all.", ``) +
		glyphs(200, "A fill without alpha.", `Fill="#00FF0000"`) +
		glyphs(250, "Opacity of nothing.", `Fill="#FF000000" Opacity="0"`) +
		`<Canvas Opacity="0">` + glyphs(300, "In a canvas of no opacity.", `Fill="#FF000000"`) + `</Canvas>` +
		glyphs(350, "A colour like the page.", `Fill="#FFFFFFFF"`) +
		glyphs(400, "Half there.", `Fill="#80000000" Opacity="0.5"`) +
		glyphs(450, "Alpha first.", `Fill="sc#0,1,1,1"`)
	doc := xpsDoc(t, nil, body)
	want := map[string]gofastcore.Display{
		"Drawn in black.":            0,
		"No fill at all.":            gofastcore.DisplayNoInk,
		"A fill without alpha.":      gofastcore.DisplayNoInk,
		"Opacity of nothing.":        gofastcore.DisplayNoInk,
		"In a canvas of no opacity.": gofastcore.DisplayNoInk,
		"A colour like the page.":    0,
		"Half there.":                0,
		"Alpha first.":               gofastcore.DisplayNoInk,
	}
	p := &doc.Parts[0]
	if len(p.Blocks) != len(want) {
		t.Fatalf("blocks: %v", xpsBlocks(p))
	}
	for i := range p.Blocks {
		b := &p.Blocks[i]
		if d, ok := want[b.Text]; !ok || d != b.Display {
			t.Errorf("%q: display %v", b.Text, b.Display)
		}
	}
}

// TestXPSDamaged: a page whose markup breaks off keeps what was read
// before the damage and says so; a page that the package does not have
// is a failed part; the pages around them are read.
func TestXPSDamaged(t *testing.T) {
	g := xpstest.Glyphs
	good := xpstest.Page(xpstest.XPS, 816, 1056, g(96, 120, 16, "A whole page.", ""))
	cut := xpstest.Page(xpstest.XPS, 816, 1056, g(96, 120, 16, "Before the damage.", "")+`<Glyphs Fill="#FF000000" UnicodeString="lost`)
	pkg := xpstest.Document(xpstest.XPS, good, cut, good, good)
	pkg.Remove("Documents/1/Pages/3.fpage")
	doc := xpsExtract(t, nil, pkg.Bytes())
	if len(doc.Parts) != 4 {
		t.Fatalf("%d parts", len(doc.Parts))
	}
	wantBlocks(t, &doc.Parts[0], "paragraph: A whole page.")
	wantBlocks(t, &doc.Parts[1], "paragraph: Before the damage.")
	if !xpsWarned(doc.Parts[1].Warnings, gofastcore.WarnInputStreamDamaged) {
		t.Errorf("page 2: %v", doc.Parts[1].Warnings)
	}
	if !xpsWarned(doc.Parts[2].Warnings, gofastcore.WarnPartFailed) || len(doc.Parts[2].Blocks) != 0 {
		t.Errorf("page 3: %v %v", doc.Parts[2].Warnings, xpsBlocks(&doc.Parts[2]))
	}
	wantBlocks(t, &doc.Parts[3], "paragraph: A whole page.")

	// The parts of the package itself are not read past damage: a
	// document whose list of pages is broken is no document.
	pkg = xpstest.Document(xpstest.XPS, good)
	pkg.Add("Documents/1/FixedDoc.fdoc", []byte(`<FixedDocument xmlns="`+xpstest.XPS+`"><PageContent Source="Pages/1.fpage"/><PageContent`))
	data := pkg.Bytes()
	if _, err := gofastpdf.Extract(context.Background(), bytes.NewReader(data), int64(len(data)), nil); !errors.Is(err, gofastcore.ErrMalformed) {
		t.Errorf("a broken list of pages: %v", err)
	}
	// A package without pages.
	data = xpstest.Document(xpstest.XPS).Bytes()
	if _, err := gofastpdf.Extract(context.Background(), bytes.NewReader(data), int64(len(data)), nil); !errors.Is(err, gofastcore.ErrMalformed) {
		t.Errorf("no pages: %v", err)
	}
}

// TestXPSInvalidText: bytes of a page's text that are no characters are
// U+FFFD each, and stay where they are whatever is made of the text
// around them.
func TestXPSInvalidText(t *testing.T) {
	page := xpstest.Page(xpstest.XPS, 816, 1056, `<Glyphs Fill="#FF000000" FontUri="`+xpstest.FontName+`" FontRenderingEmSize="16" OriginX="96" OriginY="120" UnicodeString="half \xd7    \x95 a letter"/>`)
	page = strings.NewReplacer(`\xd7`, "\xd7", `\x95`, "\x95").Replace(page)
	doc := xpsExtract(t, nil, xpstest.Document(xpstest.XPS, page).Bytes())
	p := &doc.Parts[0]
	if len(p.Blocks) != 1 || strings.Count(p.Blocks[0].Text, "\uFFFD") != 2 || !strings.Contains(p.Blocks[0].Text, "a letter") {
		t.Errorf("blocks: %q", xpsBlocks(p))
	}
	if !xpsWarned(p.Warnings, gofastcore.WarnTextUnmappedGlyphs) {
		t.Errorf("warnings: %v", p.Warnings)
	}
}

// TestXPSLigatures: ligatures and presentation forms are written as
// their letters, in the text of a page and in what the document says of
// itself, as for PDF.
func TestXPSLigatures(t *testing.T) {
	page := xpstest.Page(xpstest.XPS, 816, 1056, xpstest.Glyphs(96, 120, 16, "An o\uFB03ce \uFB01le.", `Indices=";;;(1:1)5;;;;(1:1)6"`))
	pkg := xpstest.Document(xpstest.XPS, page)
	pkg.Add("docProps/core.xml", []byte(`<cp:coreProperties xmlns:cp="http://schemas.openxmlformats.org/package/2006/metadata/core-properties" xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:title>O`+"\uFB03"+`cial</dc:title></cp:coreProperties>`))
	doc := xpsExtract(t, nil, pkg.Bytes())
	wantBlocks(t, &doc.Parts[0], "paragraph: An office file.")
	if doc.Metadata.Title != "Official" {
		t.Errorf("title %q", doc.Metadata.Title)
	}
}

// TestXPSDepth: Canvas elements nested beyond the bound are not read,
// and the page says so; what is around them is read.
func TestXPSDepth(t *testing.T) {
	g := xpstest.Glyphs
	body := g(96, 100, 16, "Before the canvases.", "") + strings.Repeat("<Canvas>", 300) + g(96, 200, 16, "Too deep.", "") + strings.Repeat("</Canvas>", 300) + g(96, 300, 16, "After them.", "")
	doc := xpsDoc(t, nil, body)
	wantBlocks(t, &doc.Parts[0], "paragraph: Before the canvases.", "paragraph: After them.")
	if !xpsWarned(doc.Parts[0].Warnings, gofastcore.WarnLimitExceeded) {
		t.Errorf("warnings: %v", doc.Parts[0].Warnings)
	}
	// Within the bound they are read.
	body = strings.Repeat("<Canvas>", 200) + g(96, 200, 16, "Deep enough.", "") + strings.Repeat("</Canvas>", 200)
	doc = xpsDoc(t, nil, body)
	wantBlocks(t, &doc.Parts[0], "paragraph: Deep enough.")
}

// TestXPSCutArchive: an archive that is cut short is read as far as it
// goes, and the document says that it was repaired.
func TestXPSCutArchive(t *testing.T) {
	g := xpstest.Glyphs
	pkg := xpstest.Document(xpstest.XPS, xpstest.Page(xpstest.XPS, 816, 1056, g(96, 120, 16, "Read from a cut archive.", "")))
	pkg.Store = true
	pkg.Add("Resources/padding.bin", bytes.Repeat([]byte{'x'}, 4096))
	data := pkg.Bytes()
	data = data[:len(data)-2048]
	doc := xpsExtract(t, nil, data)
	wantBlocks(t, &doc.Parts[0], "paragraph: Read from a cut archive.")
	if !xpsWarned(doc.Warnings, gofastcore.WarnInputRepaired) {
		t.Errorf("warnings: %v", doc.Warnings)
	}
}

// TestXPSPieces: a part stored in pieces is read as one part.
func TestXPSPieces(t *testing.T) {
	page := xpstest.Page(xpstest.XPS, 816, 1056, xpstest.Glyphs(96, 120, 16, "A page stored in three pieces.", ""))
	pkg := xpstest.Document(xpstest.XPS, page)
	pkg.Remove("Documents/1/Pages/1.fpage")
	a, b := len(page)/3, 2*len(page)/3
	pkg.Add("Documents/1/Pages/1.fpage/[0].piece", []byte(page[:a]))
	pkg.Add("Documents/1/Pages/1.fpage/[1].piece", []byte(page[a:b]))
	pkg.Add("Documents/1/Pages/1.fpage/[2].last.piece", []byte(page[b:]))
	doc := xpsExtract(t, nil, pkg.Bytes())
	wantBlocks(t, &doc.Parts[0], "paragraph: A page stored in three pieces.")
}

// TestXPSMetadata: the core properties of the package.
func TestXPSMetadata(t *testing.T) {
	pkg := xpstest.Document(xpstest.XPS, xpstest.Page(xpstest.XPS, 816, 1056, xpstest.Glyphs(96, 120, 16, "Text.", "")))
	pkg.Add("docProps/core.xml", []byte(`<?xml version="1.0" encoding="UTF-8"?>`+
		`<cp:coreProperties xmlns:cp="http://schemas.openxmlformats.org/package/2006/metadata/core-properties" xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:dcterms="http://purl.org/dc/terms/" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">`+
		`<dc:title>The title</dc:title><dc:subject>Its subject</dc:subject><dc:creator>A. Writer; B. Writer</dc:creator>`+
		`<cp:keywords>one, two</cp:keywords><dc:language>fr-CA</dc:language><dc:description>Described.</dc:description>`+
		`<cp:lastModifiedBy>An editor</cp:lastModifiedBy><cp:category>Reports</cp:category>`+
		`<dcterms:created xsi:type="dcterms:W3CDTF">2006-10-19T01:21:08Z</dcterms:created><dcterms:modified xsi:type="dcterms:W3CDTF">2007-01-02</dcterms:modified>`+
		`</cp:coreProperties>`))
	md := xpsExtract(t, nil, pkg.Bytes()).Metadata
	if md.Title != "The title" || md.Subject != "Its subject" || md.Language != "fr-CA" ||
		len(md.Authors) != 1 || md.Authors[0] != "A. Writer; B. Writer" || len(md.Keywords) != 1 || md.Keywords[0] != "one, two" {
		t.Errorf("metadata: %+v", md)
	}
	if md.Extra["description"] != "Described." || md.Extra["last_modified_by"] != "An editor" || md.Extra["category"] != "Reports" {
		t.Errorf("extra: %v", md.Extra)
	}
	if got := md.Created.UTC().Format("2006-01-02 15:04:05"); got != "2006-10-19 01:21:08" {
		t.Errorf("created: %v", got)
	}
	if got := md.Modified.Format("2006-01-02"); got != "2007-01-02" {
		t.Errorf("modified: %v", got)
	}
}

// TestXPSEncrypted: the envelope of an encrypted package is refused,
// with what it is; a compound file that is no such envelope is not an
// XPS document, and is refused as any file that is not a PDF.
func TestXPSEncrypted(t *testing.T) {
	stream := func(name string) *cfb.Node { return &cfb.Node{Name: name, Data: []byte("data")} }
	storage := func(name string, kids ...*cfb.Node) *cfb.Node { return &cfb.Node{Name: name, Children: kids} }
	cases := []struct {
		name string
		root *cfb.Node
		want string
		err  error
	}{
		{"rights management, as Office writes it",
			storage("Root Entry", storage("\x06DataSpaces", storage("DataSpaceInfo", stream("\x09DRMEncryptedDataSpace"))), stream("EncryptedPackage")),
			"rights management", gofastcore.ErrUnsupported},
		{"rights management, as the .NET packaging classes write it",
			storage("Root Entry", storage("\x06DataSpaces", storage("TransformInfo", storage("EncryptionTransform", stream("EUL-PCOF64OLAHYHETKLVEGTG3H6ZD"), stream("\x06Primary")))), stream("EncryptedPackage")),
			"rights management", gofastcore.ErrUnsupported},
		{"a password",
			storage("Root Entry", stream("EncryptionInfo"), stream("EncryptedPackage")),
			"password", gofastcore.ErrUnsupported},
		{"unstated",
			storage("Root Entry", stream("EncryptedPackage")),
			"not stated", gofastcore.ErrUnsupported},
		{"another compound file",
			storage("Root Entry", stream("WordDocument")),
			"", gofastcore.ErrMalformed},
	}
	for _, c := range cases {
		data := cfb.Write(c.root)
		_, err := gofastpdf.Extract(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
		if !errors.Is(err, c.err) || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v", c.name, err)
		}
	}
}

// TestXPSHandle: the pages of an XPS document through the handle: their
// number, a range of them, one page by itself, its lines.
func TestXPSHandle(t *testing.T) {
	var pages []string
	for i := 1; i <= 5; i++ {
		pages = append(pages, xpstest.Page(xpstest.XPS, 816, 1056, xpstest.Glyphs(96, 120, 16, fmt.Sprintf("The text of page %d.", i), "")))
	}
	data := xpstest.Document(xpstest.XPS, pages...).Bytes()
	f, err := gofastpdf.OpenBytes(data, &gofastcore.Options{Parts: gofastcore.PartRange{First: 2, Last: 4}})
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if f.NumPages() != 5 || f.Format() != gofastpdf.FormatXPS {
		t.Errorf("%d pages, %q", f.NumPages(), f.Format())
	}
	var numbers []int
	for part, err := range f.Parts(context.Background()) {
		if err != nil {
			t.Fatal(err)
		}
		numbers = append(numbers, part.Number)
		if want := fmt.Sprintf("The text of page %d.", part.Number); len(part.Blocks) != 1 || part.Blocks[0].Text != want {
			t.Errorf("page %d: %v", part.Number, xpsBlocks(&part))
		}
	}
	if fmt.Sprint(numbers) != "[2 3 4]" {
		t.Errorf("pages: %v", numbers)
	}
	page, err := f.Page(5)
	if err != nil {
		t.Fatal(err)
	}
	part, err := page.Part()
	if err != nil || page.Number() != 5 || len(part.Blocks) != 1 || part.Blocks[0].Text != "The text of page 5." {
		t.Errorf("page 5: %v %v", err, xpsBlocks(&part))
	}
	lines, err := page.Lines()
	if err != nil || len(lines) != 1 || lines[0].Text != "The text of page 5." || len(lines[0].Words) != 5 || lines[0].Font != "Test Sans" || lines[0].Size != 12 {
		t.Fatalf("lines: %v %+v", err, lines)
	}
	// "text" begins 21 points into the line, after "The " (3 glyphs of 6
	// points and a space of 3), and is 4 glyphs wide.
	if w := lines[0].Words[1]; w.Text != "text" || w.BBox.X0 < 92.9 || w.BBox.X0 > 93.1 || w.BBox.X1 < 116.9 || w.BBox.X1 > 117.1 {
		t.Errorf("word: %+v", w)
	}
	if _, err := f.Page(6); !errors.Is(err, gofastcore.ErrNoPartsSelected) {
		t.Errorf("page 6: %v", err)
	}
	doc, err := f.Document(context.Background())
	if err != nil || len(doc.Parts) != 3 {
		t.Fatalf("document: %v", err)
	}
}

// TestXPSPagesTogether: the pages of an XPS document are compared as
// those of a PDF are: a line that runs through them is a header or a
// footer, and a paragraph that goes on over the page break is marked.
func TestXPSPagesTogether(t *testing.T) {
	g := xpstest.Glyphs
	var pages []string
	for i := 1; i <= 6; i++ {
		body := g(96, 60, 12, "The Running Title of the Book", "") +
			g(96, 150, 16, fmt.Sprintf("Page %d begins with a sentence of its own, which is long enough", i), "") +
			g(96, 170, 16, "to fill two lines of the page and so to be a paragraph of it.", "") +
			g(96, 900, 16, fmt.Sprintf("The last paragraph of page %d does not end on it, for it goes", i), "") +
			g(96, 920, 16, "on over the break between this page and the one after and", "") +
			g(400, 1000, 12, fmt.Sprint(i), "")
		if i > 1 {
			body = g(96, 110, 16, "ends at the top of the next one, in the middle of a line.", "") + body
		}
		pages = append(pages, xpstest.Page(xpstest.XPS, 816, 1056, body))
	}
	doc := xpsExtract(t, nil, xpstest.Document(xpstest.XPS, pages...).Bytes())
	for i := range doc.Parts {
		p := &doc.Parts[i]
		kinds := map[gofastcore.Kind]int{}
		for k := range p.Blocks {
			kinds[p.Blocks[k].Kind]++
		}
		if kinds[gofastcore.KindHeader] != 1 || kinds[gofastcore.KindFooter] != 1 {
			t.Errorf("page %d: %v", p.Number, xpsBlocks(p))
			continue
		}
		if i > 0 {
			continued := false
			for k := range p.Blocks {
				if b := &p.Blocks[k]; b.Kind == gofastcore.KindParagraph {
					continued = b.Continued && strings.HasPrefix(b.Text, "ends at the top")
					break
				}
			}
			if !continued {
				t.Errorf("page %d: its first paragraph does not continue: %v", p.Number, xpsBlocks(p))
			}
		}
	}
}

// TestXPSLimits: the limit on decompressed bytes ends the document.
func TestXPSLimits(t *testing.T) {
	var pages []string
	for i := 1; i <= 8; i++ {
		pages = append(pages, xpstest.Page(xpstest.XPS, 816, 1056, strings.Repeat(xpstest.Glyphs(96, 120, 16, fmt.Sprintf("The text of page %d.", i), ""), 40)))
	}
	data := xpstest.Document(xpstest.XPS, pages...).Bytes()
	doc, err := gofastpdf.Extract(context.Background(), bytes.NewReader(data), int64(len(data)), &gofastcore.Options{Workers: 1, Limits: gofastcore.Limits{MaxDecodedBytes: 30_000}})
	if err != nil {
		t.Fatal(err)
	}
	if n := len(doc.Parts); n == 0 || n >= 8 || !xpsWarned(doc.Warnings, gofastcore.WarnLimitExceeded) {
		t.Errorf("%d parts, warnings %v", n, doc.Warnings)
	}
}

// xpsSeeds are bodies of pages for the fuzz targets: text placed by the
// font and by indices, clusters, text from right to left, transforms,
// what is declared not drawn, and what is not text.
func xpsSeeds() []string {
	g := xpstest.Glyphs
	return []string{
		g(96, 120, 16, "A first line of the paragraph that goes", "") + g(96, 140, 16, "on in a second line and ends here.", ""),
		g(96, 120, 20, "office x", `Indices=";(2:1)71,60;;;;(1:2)70;70;"`) + g(96, 160, 20, "onetwo", `Indices=";;,150;;;"`),
		g(300, 120, 20, "שלום ", `BidiLevel="1"`) + g(225, 120, 20, "abc", `BidiLevel="2"`) + g(220, 120, 20, "עולם", `BidiLevel="1"`),
		`<Canvas RenderTransform="2,0,0,2,0,0" Opacity="0.5"><Canvas><Canvas.RenderTransform><MatrixTransform Matrix="0,1,-1,0,10,20"/></Canvas.RenderTransform>` +
			g(38, 40, 8, "Turned", `IsSideways="true" StyleSimulations="BoldItalicSimulation"`) + `</Canvas></Canvas>`,
		g(96, 200, 16, "An o\uFB03ce \uFB01le.", "") +
			`<Glyphs FontUri="` + xpstest.FontName + `#0" FontRenderingEmSize="16" OriginX="96" OriginY="120" Indices="35;36;37,,1,2;900"/>` +
			`<Path Data="M 0,0 L 10,0 10,10 Z" Fill="#FF000000"/><Glyphs Fill="#00000000" FontUri="/none.ttf" FontRenderingEmSize="1e2" OriginX="1" OriginY="2" UnicodeString="{}{x}"><Glyphs.Fill><SolidColorBrush Color="#FF000000"/></Glyphs.Fill></Glyphs>`,
	}
}

// FuzzXPSPage feeds the markup of pages to the whole library: the
// reader of XPS, the layout and the assembly of the document. Whatever
// a page says, the document keeps the contract of the shared model and
// is the same for any number of workers.
func FuzzXPSPage(f *testing.F) {
	for _, seed := range xpsSeeds() {
		f.Add(xpstest.Page(xpstest.XPS, 816, 1056, seed))
	}
	font := xpstest.Font(nil)
	f.Fuzz(func(t *testing.T, page string) {
		pkg := xpstest.Document(xpstest.XPS, page, page, xpstest.Page(xpstest.XPS, 816, 1056, xpstest.Glyphs(96, 120, 16, "A page that is whole.", "")))
		pkg.Store = true
		pkg.Add(xpstest.FontName, font)
		data := pkg.Bytes()
		spans := len(page)%2 == 1
		one, err := extractBytes(data, extractOpts{Images: true, Spans: spans})
		if errors.Is(err, errContract) {
			t.Fatal(err)
		}
		if err != nil {
			t.Fatalf("a page ended the document: %v", err)
		}
		many, err := extractBytes(data, extractOpts{Images: true, Workers: 4, Spans: spans})
		if err != nil {
			t.Fatalf("with 4 workers: %v", err)
		}
		if diff := sameDocument(one.Doc, many.Doc); diff != "" {
			t.Fatalf("1 worker and 4 workers: %s", diff)
		}
	})
}

// TestXPSCorpus reads the XPS files of the shared corpus of the go-fast
// libraries, when GOFAST_CORPUS names it: each is read or refused for a
// stated reason, keeps the contract of the shared model, and is the same
// document for one worker and for four. Only the files for development
// are read here; those held out are counted elsewhere and never looked
// at.
func TestXPSCorpus(t *testing.T) {
	root := os.Getenv("GOFAST_CORPUS")
	if root == "" {
		t.Skip("GOFAST_CORPUS is not set")
	}
	files, _ := filepath.Glob(filepath.Join(root, "dev", "xps", "*"))
	if len(files) == 0 {
		t.Skipf("no XPS files under %s", root)
	}
	read, refused := 0, 0
	for _, path := range files {
		if strings.HasSuffix(path, ".tsv") {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		name := filepath.Base(path)
		one, err := extractBytes(data, extractOpts{Images: true, Spans: true, Workers: 1})
		switch {
		case errors.Is(err, errContract):
			t.Errorf("%s: %v", name, err)
			continue
		case errors.Is(err, gofastcore.ErrUnsupported):
			refused++
			continue
		case err != nil:
			t.Errorf("%s: %v", name, err)
			continue
		}
		read++
		if f := one.Doc.Source.Format; f != gofastpdf.FormatXPS && f != gofastpdf.FormatOpenXPS {
			t.Errorf("%s: format %q", name, f)
		}
		many, err := extractBytes(data, extractOpts{Images: true, Spans: true, Workers: 4})
		if err != nil {
			t.Errorf("%s: with 4 workers: %v", name, err)
			continue
		}
		if diff := sameDocument(one.Doc, many.Doc); diff != "" {
			t.Errorf("%s: 1 worker and 4 workers: %s", name, diff)
		}
	}
	t.Logf("%d files read, %d refused as encrypted", read, refused)
}
