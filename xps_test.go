package gofastpdf_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/png"
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
	outline := `<OutlineEntry OutlineLevel="1" Description="The ` + "\uFB01" + `rst" OutlineTarget="#1"/>`
	pkg := xpsStructured([]string{page}, nil, outline, nil)
	pkg.Add("docProps/core.xml", []byte(`<cp:coreProperties xmlns:cp="http://schemas.openxmlformats.org/package/2006/metadata/core-properties" xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:title>O`+"\uFB03"+`cial</dc:title></cp:coreProperties>`))
	doc := xpsExtract(t, nil, pkg.Bytes())
	wantBlocks(t, &doc.Parts[0], "paragraph: An office file.")
	if len(doc.Outline) != 1 || doc.Outline[0].Title != "The first" || doc.Metadata.Title != "Official" {
		t.Errorf("outline %+v, title %q", doc.Outline, doc.Metadata.Title)
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

// TestXPSLimits: the limit on decompressed bytes ends the document, at
// the same page for any number of workers: what a page costs is counted
// for the page, and a font with the first page that uses it.
func TestXPSLimits(t *testing.T) {
	var pages []string
	for i := 1; i <= 24; i++ {
		// Pages of different sizes, and a second font from page 9 on.
		font := xpstest.FontName
		if i >= 9 {
			font = "/Resources/second.ttf"
		}
		line := strings.Replace(xpstest.Glyphs(96, 120, 16, fmt.Sprintf("The text of page %d.", i), ""), xpstest.FontName, font, 1)
		pages = append(pages, xpstest.Page(xpstest.XPS, 816, 1056, strings.Repeat(line, 10+7*(i%5))))
	}
	pkg := xpstest.Document(xpstest.XPS, pages...)
	pkg.Add("Resources/second.ttf", xpstest.Font(&xpstest.FontOptions{Name: "Second"}))
	data := pkg.Bytes()
	read := func(limit int64, workers int) (int, bool) {
		doc, err := gofastpdf.Extract(context.Background(), bytes.NewReader(data), int64(len(data)), &gofastcore.Options{Workers: workers, Limits: gofastcore.Limits{MaxDecodedBytes: limit}})
		if err != nil {
			t.Fatalf("limit %d, %d workers: %v", limit, workers, err)
		}
		return len(doc.Parts), xpsWarned(doc.Warnings, gofastcore.WarnLimitExceeded)
	}
	whole, cut := read(1<<30, 1)
	if whole != 24 || cut {
		t.Fatalf("without a limit: %d parts, cut %v", whole, cut)
	}
	last, cuts := -1, 0
	for limit := int64(4_000); limit <= 200_000; limit += 6_500 {
		one, cut := read(limit, 1)
		if one < last {
			t.Errorf("limit %d: %d parts, fewer than with a lower limit (%d)", limit, one, last)
		}
		last = one
		if cut != (one < 24) {
			t.Errorf("limit %d: %d parts, warning %v", limit, one, cut)
		}
		if cut {
			cuts++
		}
		for try := 0; try < 3; try++ {
			if many, cutMany := read(limit, 8); many != one || cutMany != cut {
				t.Fatalf("limit %d: %d parts with 1 worker and %d with 8", limit, one, many)
			}
		}
	}
	if cuts < 5 || last != 24 {
		t.Errorf("%d limits cut the document, and the largest left %d parts", cuts, last)
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
			`<Path Data="M 0,0 L 10,0 10,10 Z" Fill="#FF000000"/><Glyphs Fill="#00000000" FontUri="/none.ttf" FontRenderingEmSize="1e2" OriginX="1" OriginY="2" UnicodeString="{}{x}"><Glyphs.Fill><SolidColorBrush Color="#FF000000"/></Glyphs.Fill></Glyphs>`, // A ruled grid with text in its cells, an image, resources and
		// the visual of a brush.
		xpsGrid([]float64{96, 296, 496}, []float64{200, 240, 300}) + g(104, 226, 14, "Region", "") + g(304, 226, 14, "Units", "") + g(104, 266, 14, "North", "") + g(304, 266, 14, "1 200", ""),
		`<FixedPage.Resources><ResourceDictionary xmlns:x="` + xpstest.XPS + `/resourcedictionary-key"><SolidColorBrush x:Key="a" Color="#FF000000"/><MatrixTransform x:Key="m" Matrix="1,0,0,1,5,5"/>` +
			`<PathGeometry x:Key="g" Figures="M 100,200 H 400 V 201 H 100 Z"/></ResourceDictionary><ResourceDictionary Source="/Resources/none.dict"/></FixedPage.Resources>` +
			`<Path Data="{StaticResource g}" Fill="{StaticResource a}" RenderTransform="{StaticResource m}"/>` +
			`<Path Data="M 96,96 L 296,96 L 296,196 L 96,196 Z" AutomationProperties.HelpText="An image"><Path.Fill><ImageBrush ImageSource="/Resources/font.ttf" Viewbox="0,0,1,1" Viewport="0,0,1,1"/></Path.Fill></Path>` +
			`<Path Stroke="#FF000000" StrokeThickness="2"><Path.Data><PathGeometry><PathFigure StartPoint="1,2" IsClosed="true"><PolyLineSegment Points="3,2 3,9"/><ArcSegment Point="1,2" Size="2,2"/></PathFigure></PathGeometry></Path.Data>` +
			`<Path.Fill><VisualBrush Viewbox="0,0,100,50" Viewport="96,96,200,100"><VisualBrush.Visual><Canvas>` + g(0, 10, 10, "In a brush.", "") + `</Canvas></VisualBrush.Visual></VisualBrush></Path.Fill></Path>`,
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

// xpsGrid draws the rules of a grid as thin filled boxes: the vertical
// ones at xs, the horizontal ones at ys.
func xpsGrid(xs, ys []float64) string {
	var b strings.Builder
	for _, y := range ys {
		fmt.Fprintf(&b, `<Path Data="M %g,%g L %g,%g L %g,%g L %g,%g Z" Fill="#FF000000"/>`, xs[0], y, xs[len(xs)-1], y, xs[len(xs)-1], y+1, xs[0], y+1)
	}
	for _, x := range xs {
		fmt.Fprintf(&b, `<Path Data="M %g,%g L %g,%g L %g,%g L %g,%g Z" Fill="#FF000000"/>`, x, ys[0], x+1, ys[0], x+1, ys[len(ys)-1], x, ys[len(ys)-1])
	}
	return b.String()
}

// TestXPSRuledTable: a table drawn with rules is found by them, as in a
// PDF: the cells are those of the grid, whatever stands in them.
func TestXPSRuledTable(t *testing.T) {
	g := xpstest.Glyphs
	xs, ys := []float64{96, 296, 496, 696}, []float64{200, 240, 300, 340}
	body := g(96, 150, 16, "Before the table.", "") + xpsGrid(xs, ys) +
		g(104, 226, 14, "Region", "") + g(304, 226, 14, "Units sold", "") + g(504, 226, 14, "Note", "") +
		g(104, 266, 14, "North and the", "") + g(104, 286, 14, "islands", "") + g(304, 266, 14, "1 200", "") + g(504, 266, 14, "estimated", "") +
		g(104, 326, 14, "South", "") + g(304, 326, 14, "975", "") +
		g(96, 400, 16, "After the table.", "")
	doc := xpsDoc(t, nil, body)
	p := &doc.Parts[0]
	if len(p.Blocks) != 3 || p.Blocks[1].Kind != gofastcore.KindTable {
		t.Fatalf("blocks: %q", xpsBlocks(p))
	}
	var rows []string
	for _, row := range p.Blocks[1].Table.Rows {
		rows = append(rows, strings.Join(row, " | "))
	}
	want := "Region | Units sold | Note\nNorth and the islands | 1 200 | estimated\nSouth | 975 | "
	if got := strings.Join(rows, "\n"); got != want {
		t.Errorf("table:\n%s\nwant:\n%s", got, want)
	}

	// The same rules declared to paint nothing are no table.
	doc = xpsDoc(t, nil, strings.ReplaceAll(body, `Fill="#FF000000"/>`, `Fill="#00000000"/>`))
	for i := range doc.Parts[0].Blocks {
		if doc.Parts[0].Blocks[i].Kind == gofastcore.KindTable {
			t.Errorf("a table of rules that paint nothing: %q", xpsBlocks(&doc.Parts[0]))
		}
	}
	// With Extractor.NoRuledTables the rules are not looked at.
	data := xpstest.Document(xpstest.XPS, xpstest.Page(xpstest.XPS, 816, 1056, body)).Bytes()
	plain, err := (&gofastpdf.Extractor{NoRuledTables: true}).Extract(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatal(err)
	}
	for i := range plain.Parts[0].Blocks {
		if b := &plain.Parts[0].Blocks[i]; b.Kind == gofastcore.KindTable && len(b.Table.Rows) == 3 && len(b.Table.Rows[0]) == 3 && strings.Contains(b.Table.Rows[1][0], "islands") {
			t.Errorf("NoRuledTables: the grid was followed: %q", xpsBlocks(&plain.Parts[0]))
		}
	}
}

// xpsPNG returns a PNG image of the size given.
func xpsPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewGray(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// TestXPSImages: an image of a page is a block where it is drawn, named
// by its part; its bytes are those of the part, on the first block that
// shows it.
func TestXPSImages(t *testing.T) {
	g := xpstest.Glyphs
	img := func(y float64, src, attrs string) string {
		return fmt.Sprintf(`<Path Data="M 96,%g L 296,%g L 296,%g L 96,%g Z" %s><Path.Fill><ImageBrush ImageSource="%s" Viewbox="0,0,1,1" Viewport="0,0,1,1"/></Path.Fill></Path>`, y, y, y+100, y+100, attrs, src)
	}
	picture := xpsPNG(t, 40, 20)
	tiff := append([]byte("II*\x00\x08\x00\x00\x00\x02\x00"), []byte("\x00\x01\x03\x00\x01\x00\x00\x00\x40\x01\x00\x00"+"\x01\x01\x03\x00\x01\x00\x00\x00\xF0\x00\x00\x00"+"\x00\x00\x00\x00")...)
	build := func() *xpstest.Package {
		pkg := xpstest.Document(xpstest.XPS,
			xpstest.Page(xpstest.XPS, 816, 1056, g(96, 80, 16, "A picture follows.", "")+img(100, "/Resources/Images/Chart.PNG", `AutomationProperties.HelpText="Sales by region"`)+g(96, 260, 16, "And text after it.", "")),
			xpstest.Page(xpstest.XPS, 816, 1056, img(100, "/Resources/Images/Chart.PNG", "")+img(300, "/Resources/Images/scan.tif", "")))
		pkg.Types["png"], pkg.Types["tif"] = "image/png", "image/tiff"
		pkg.Add("Resources/Images/Chart.PNG", picture)
		pkg.Add("Resources/Images/scan.tif", tiff)
		return pkg
	}
	doc := xpsExtract(t, nil, build().Bytes())
	p := &doc.Parts[0]
	if len(p.Blocks) != 3 || p.Blocks[1].Kind != gofastcore.KindImage {
		t.Fatalf("page 1: %q", xpsBlocks(p))
	}
	b := &p.Blocks[1]
	if b.Text != "Sales by region" || b.Image.ID != "Resources/Images/Chart.PNG" || b.Image.MIME != "image/png" || b.Image.Width != 40 || b.Image.Height != 20 || b.Image.Data != nil {
		t.Errorf("image: %q %+v", b.Text, *b.Image)
	}
	if box := b.Loc.BBox; box.X0 != 72 || box.X1 != 222 || box.Y0 != 75 || box.Y1 != 150 {
		t.Errorf("box: %+v", box)
	}
	// A page of images and no text says so.
	q := &doc.Parts[1]
	if len(q.Blocks) != 2 || !xpsWarned(q.Warnings, gofastcore.WarnPartImageOnly) {
		t.Errorf("page 2: %q %v", xpsBlocks(q), q.Warnings)
	}
	if im := q.Blocks[1].Image; im.MIME != "image/tiff" || im.Width != 320 || im.Height != 240 {
		t.Errorf("TIFF: %+v", *im)
	}

	// With the data: once for each image, on the first block.
	doc = xpsExtract(t, &gofastcore.Options{Images: gofastcore.ImagesData}, build().Bytes())
	if got := doc.Parts[0].Blocks[1].Image.Data; !bytes.Equal(got, picture) {
		t.Errorf("data of %d bytes, want %d", len(got), len(picture))
	}
	if doc.Parts[1].Blocks[0].Image.Data != nil || !bytes.Equal(doc.Parts[1].Blocks[1].Image.Data, tiff) {
		t.Error("the data of the images of page 2")
	}
	// Without images.
	doc = xpsExtract(t, &gofastcore.Options{Images: gofastcore.ImagesNone}, build().Bytes())
	wantBlocks(t, &doc.Parts[0], "paragraph: A picture follows.", "paragraph: And text after it.")
}

// TestXPSTextOfBlocks: what the file says an image shows, and what a
// signature is to mean, are the text of blocks: without white space
// around them, also where only a control character stood before it, and
// no text at all when there is nothing else.
func TestXPSTextOfBlocks(t *testing.T) {
	img := func(y float64, alt string) string {
		return fmt.Sprintf(`<Path Data="M 96,%g L 296,%g L 296,%g L 96,%g Z" AutomationProperties.HelpText="%s"><Path.Fill><ImageBrush ImageSource="/Resources/a.png" Viewbox="0,0,1,1" Viewport="0,0,1,1"/></Path.Fill></Path>`, y, y, y+100, y+100, alt)
	}
	pkg := xpstest.Document(xpstest.XPS, xpstest.Page(xpstest.XPS, 816, 1056, img(100, " ")+img(300, "&#9;  A chart  ")+img(500, "&#x1; after a control character ")))
	pkg.Add("Resources/a.png", xpsPNG(t, 4, 4))
	pkg.Add("Documents/1/_rels/FixedDoc.fdoc.rels", []byte(`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`+
		`<Relationship Id="R1" Type="`+xpstest.XPS+`/signature-definitions" Target="defs.xml"/></Relationships>`))
	pkg.Add("Documents/1/defs.xml", []byte(`<SignatureDefinitions xmlns="`+xpstest.XPS+`/signature-definitions"><SignatureDefinition><Intent>&#x1;  Agreed.  </Intent></SignatureDefinition></SignatureDefinitions>`))
	doc := xpsExtract(t, nil, pkg.Bytes())
	wantBlocks(t, &doc.Parts[0], "image: ", "image: A chart", "image: after a control character", "note: Agreed.")
}

// TestXPSCharacterReferences: a character reference that names no
// character is read as U+FFFD, and the page or the document says how
// many there were, in the words the go-fast libraries share.
func TestXPSCharacterReferences(t *testing.T) {
	g := xpstest.Glyphs
	count := func(ws []gofastcore.Warning) string {
		for _, w := range ws {
			if w.Code == gofastcore.WarnTextUnmappedGlyphs && strings.Contains(w.Message, "character reference") {
				return w.Message
			}
		}
		return ""
	}
	pages := []string{
		xpstest.Page(xpstest.XPS, 816, 1056, strings.Replace(g(96, 120, 16, "One XX here.", ""), "XX", "&#0;", 1)),
		xpstest.Page(xpstest.XPS, 816, 1056, strings.Replace(g(96, 120, 16, "Three XX here.", `Name="aXX"`), "XX", "&#xD800;&#x110000;", 1)+strings.Replace(g(96, 160, 16, "And XX.", ""), "XX", "&#0;", 1)),
		xpstest.Page(xpstest.XPS, 816, 1056, g(96, 120, 16, "None &#65; here.", "")),
	}
	pkg := xpsStructured(pages, nil, `<OutlineEntry Description="A title &#0; of the outline" OutlineTarget="#1"/>`, nil)
	doc := xpsExtract(t, nil, pkg.Bytes())
	const tail = " no character (&#0;, a surrogate or a number past U+10FFFF) and "
	if got, want := count(doc.Parts[0].Warnings), "1 character reference names"+tail+"is read as U+FFFD"; got != want {
		t.Errorf("page 1: %q", got)
	}
	if !strings.Contains(doc.Parts[0].Blocks[0].Text, "One \uFFFD here.") {
		t.Errorf("page 1: %q", xpsBlocks(&doc.Parts[0]))
	}
	if got, want := count(doc.Parts[1].Warnings), "3 character references name"+tail+"are read as U+FFFD"; got != want {
		t.Errorf("page 2: %q", got)
	}
	if got := count(doc.Parts[2].Warnings); got != "" {
		t.Errorf("page 3: %q", got)
	}
	if got, want := count(doc.Warnings), "1 character reference names"+tail+"is read as U+FFFD"; got != want {
		t.Errorf("document: %q", got)
	}
}

// TestXPSExternal: a page that names a resource outside the package is
// read without it, and the document says that it points outside itself.
func TestXPSExternal(t *testing.T) {
	body := `<FixedPage.Resources><ResourceDictionary Source="http://example.com/shared.dict"/></FixedPage.Resources>` + xpstest.Glyphs(96, 120, 16, "The text of the page.", "")
	doc := xpsDoc(t, nil, body)
	wantBlocks(t, &doc.Parts[0], "paragraph: The text of the page.")
	if !xpsWarned(doc.Warnings, gofastcore.WarnInputExternalReference) {
		t.Errorf("warnings: %v", doc.Warnings)
	}
}

// xpsStructured returns a package of the pages given whose fixed
// document lists the names of targets as addressable, each on the page
// (from 1) it is given with, and has the outline entries and the story
// fragments given: fragments[i] is the inside of the StoryFragments part
// of page i+1, "" for none.
func xpsStructured(pages []string, targets map[string]int, outline string, fragments []string) *xpstest.Package {
	pkg := xpstest.Document(xpstest.XPS, pages...)
	var doc strings.Builder
	fmt.Fprintf(&doc, `<FixedDocument xmlns="%s">`, xpstest.XPS)
	for i := range pages {
		fmt.Fprintf(&doc, `<PageContent Source="Pages/%d.fpage"><PageContent.LinkTargets>`, i+1)
		for name, page := range targets {
			if page == i+1 {
				fmt.Fprintf(&doc, `<LinkTarget Name="%s"/>`, name)
			}
		}
		doc.WriteString(`</PageContent.LinkTargets></PageContent>`)
	}
	doc.WriteString(`</FixedDocument>`)
	pkg.Add("Documents/1/FixedDoc.fdoc", []byte(doc.String()))
	rels := func(kind, target string) []byte {
		return []byte(fmt.Sprintf(`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="R1" Type="%s/%s" Target="%s"/></Relationships>`, xpstest.XPS, kind, target))
	}
	if outline != "" {
		pkg.Add("Documents/1/_rels/FixedDoc.fdoc.rels", rels("documentstructure", "Structure/DocStructure.struct"))
		pkg.Add("Documents/1/Structure/DocStructure.struct", []byte(fmt.Sprintf(
			`<DocumentStructure xmlns="%s/documentstructure"><DocumentStructure.Outline><DocumentOutline xml:lang="en-US">%s</DocumentOutline></DocumentStructure.Outline></DocumentStructure>`, xpstest.XPS, outline)))
	}
	for i, frag := range fragments {
		if frag == "" {
			continue
		}
		pkg.Add(fmt.Sprintf("Documents/1/Pages/_rels/%d.fpage.rels", i+1), rels("storyfragments", fmt.Sprintf("/Documents/1/Structure/Fragments/%d.frag", i+1)))
		pkg.Add(fmt.Sprintf("Documents/1/Structure/Fragments/%d.frag", i+1), []byte(fmt.Sprintf(`<StoryFragments xmlns="%s/documentstructure">%s</StoryFragments>`, xpstest.XPS, frag)))
	}
	return pkg
}

// TestXPSOutline: the outline of the structure a document declares, its
// entries pointing at pages by a name the document lists or by number.
func TestXPSOutline(t *testing.T) {
	g := xpstest.Glyphs
	page := func(body string) string { return xpstest.Page(xpstest.XPS, 816, 1056, body) }
	pages := []string{
		page(g(96, 120, 16, "The first page.", "")),
		page(g(96, 120, 24, "Chapter One", `Name="chap1"`) + g(96, 180, 16, "The text of the chapter goes on for a line or two here.", "")),
		page(g(96, 120, 16, "The third page.", "")),
	}
	outline := `<OutlineEntry OutlineLevel="1" Description="The Book" OutlineTarget="../FixedDoc.fdoc#top"/>` +
		`<OutlineEntry OutlineLevel="2" Description="Chapter One" OutlineTarget="../FixedDoc.fdoc#chap1"/>` +
		`<OutlineEntry OutlineLevel="2" Description="By its number" OutlineTarget="../../../FixedDocSeq.fdseq#3"/>` +
		`<OutlineEntry Description="No level, and a place that is not there" OutlineTarget="../FixedDoc.fdoc#nowhere"/>` +
		`<OutlineEntry OutlineLevel="0" Description="A level of nothing" OutlineTarget="#99"/>`
	doc := xpsExtract(t, nil, xpsStructured(pages, map[string]int{"top": 1, "chap1": 2}, outline, nil).Bytes())
	var got []string
	for _, o := range doc.Outline {
		got = append(got, fmt.Sprintf("%d %q page %d", o.Level, o.Title, o.Part))
	}
	want := []string{`1 "The Book" page 1`, `2 "Chapter One" page 2`, `2 "By its number" page 3`, `1 "No level, and a place that is not there" page 0`, `1 "A level of nothing" page 0`}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("outline:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	// The entry of the outline that points at page 2 makes the line that
	// says the same its heading.
	if b := &doc.Parts[1].Blocks[0]; b.Kind != gofastcore.KindHeading || b.Text != "Chapter One" {
		t.Errorf("page 2: %q", xpsBlocks(&doc.Parts[1]))
	}
	// The outline through the handle, without a page being read.
	data := xpsStructured(pages, map[string]int{"top": 1, "chap1": 2}, outline, nil).Bytes()
	f, err := gofastpdf.OpenBytes(data, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if n := len(f.Outline()); n != 5 {
		t.Errorf("%d entries through the handle", n)
	}
}

// TestXPSLinks: with Options.Spans, the elements that name an address
// give the words they cover a link, and the names a document lists as
// addressable are anchors of the blocks that begin there.
func TestXPSLinks(t *testing.T) {
	g := xpstest.Glyphs
	page := func(body string) string { return xpstest.Page(xpstest.XPS, 816, 1056, body) }
	pages := []string{
		page(g(96, 120, 16, "Outside", `FixedPage.NavigateUri="https://example.com/a?b=1"`) +
			g(96, 180, 16, "By number", `FixedPage.NavigateUri="../../../FixedDocSeq.fdseq#2"`) +
			g(96, 240, 16, "By name", `FixedPage.NavigateUri="../FixedDoc.fdoc#chap"`) +
			g(96, 300, 16, "Nowhere", `FixedPage.NavigateUri="../FixedDoc.fdoc#gone"`) +
			`<Canvas FixedPage.NavigateUri="mailto:a@example.com">` + g(96, 360, 16, "In a canvas", "") + `</Canvas>` +
			// An area over text, painted with nothing: how Word writes a link.
			g(96, 420, 16, "Under an area and beside it", "") +
			`<Path Data="M 96,404 L 157,404 L 157,424 L 96,424 Z" Fill="#00000000" FixedPage.NavigateUri="https://example.org/"/>`),
		page(g(96, 120, 16, "Above the place that is named.", "") + g(96, 300, 16, "The chapter that the link names begins here.", `Name="chap"`) +
			g(96, 500, 16, "A name the document does not list.", `Name="private"`)),
	}
	doc := xpsExtract(t, &gofastcore.Options{Spans: true}, xpsStructured(pages, map[string]int{"chap": 2}, "", nil).Bytes())
	var got []string
	for i := range doc.Parts[0].Blocks {
		b := &doc.Parts[0].Blocks[i]
		for _, sp := range b.Spans {
			if sp.Href != "" {
				got = append(got, fmt.Sprintf("%q -> %s", b.Text[sp.Start:sp.End], sp.Href))
			}
		}
	}
	want := []string{`"Outside" -> https://example.com/a?b=1`, `"By number" -> #page=2`, `"By name" -> #page=2`, `"In a canvas" -> mailto:a@example.com`, `"Under an" -> https://example.org/`}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("links:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	var anchors []string
	for i := range doc.Parts[1].Blocks {
		if b := &doc.Parts[1].Blocks[i]; b.Loc.Anchor != "" {
			anchors = append(anchors, b.Loc.Anchor+": "+b.Text)
		}
	}
	if strings.Join(anchors, "\n") != "chap: The chapter that the link names begins here." {
		t.Errorf("anchors: %q", anchors)
	}
	// Without spans there are neither.
	doc = xpsExtract(t, nil, xpsStructured(pages, map[string]int{"chap": 2}, "", nil).Bytes())
	for i := range doc.Parts {
		for k := range doc.Parts[i].Blocks {
			if b := &doc.Parts[i].Blocks[k]; len(b.Spans) != 0 || b.Loc.Anchor != "" {
				t.Errorf("without spans: %+v", b.Loc)
			}
		}
	}
}

// TestXPSFragments: what the story fragments of a page declare of its
// elements is followed as the structure tree of a tagged PDF is: its
// paragraphs, lists and tables, their order, and the header and the
// footer.
func TestXPSFragments(t *testing.T) {
	g := xpstest.Glyphs
	n := func(name string) string { return `<NamedElement NameReference="` + name + `"/>` }
	para := func(names ...string) string {
		out := "<ParagraphStructure>"
		for _, name := range names {
			out += n(name)
		}
		return out + "</ParagraphStructure>"
	}
	story := func(kind, body string) string {
		return `<StoryFragment StoryName="s" FragmentName="f" FragmentType="` + kind + `">` + body + `</StoryFragment>`
	}
	// Paragraphs: two lines set as one paragraph that are two, and two
	// set apart that are one. The order of the fragments is evidence of
	// the order of reading where the page leaves it open, as the order
	// of a structure tree is, and no more: the second pair is declared
	// first and stays where it stands.
	body := g(96, 100, 16, "Set as one paragraph with the line below it,", `Name="a"`) +
		g(96, 120, 16, "but declared to be a paragraph of its own.", `Name="b"`) +
		g(96, 300, 16, "Set far from the line that follows", `Name="c"`) +
		g(96, 380, 16, "and declared one paragraph with it.", `Name="d"`) +
		g(96, 500, 16, "A line that nothing is declared of.", "")
	frag := story("Content", "<SectionStructure>"+para("c", "d")+para("a")+para("b")+"</SectionStructure>")
	page := xpstest.Page(xpstest.XPS, 816, 1056, body)
	doc := xpsExtract(t, nil, xpsStructured([]string{page}, nil, "", []string{frag}).Bytes())
	wantBlocks(t, &doc.Parts[0],
		"paragraph: Set as one paragraph with the line below it,",
		"paragraph: but declared to be a paragraph of its own.",
		"paragraph: Set far from the line that follows and declared one paragraph with it.",
		"paragraph: A line that nothing is declared of.")
	// Without the fragments the page is read as it is set.
	doc = xpsExtract(t, nil, xpsStructured([]string{page}, nil, "", nil).Bytes())
	wantBlocks(t, &doc.Parts[0],
		"paragraph: Set as one paragraph with the line below it, but declared to be a paragraph of its own.",
		"paragraph: Set far from the line that follows",
		"paragraph: and declared one paragraph with it.",
		"paragraph: A line that nothing is declared of.")

	// A list with markers, a table with a cell over two columns, and the
	// header and the footer of the page.
	body = g(96, 60, 12, "Declared the header", `Name="h"`) +
		g(96, 150, 16, "A", `Name="m1"`) + g(130, 150, 16, "the first item, with a letter for its marker", `Name="i1"`) +
		g(96, 180, 16, "B", `Name="m2"`) + g(130, 180, 16, "the second item", `Name="i2"`) +
		g(96, 300, 14, "Both columns", `Name="c1"`) +
		g(96, 330, 14, "Left", `Name="c2"`) + g(300, 330, 14, "Right", `Name="c3"`) +
		g(96, 360, 14, "Below left", `Name="c4"`) + g(300, 360, 14, "Below right", `Name="c5"`) +
		g(96, 1000, 12, "Declared the footer", `Name="f"`)
	cell := func(attrs string, names ...string) string {
		return "<TableCellStructure" + attrs + ">" + para(names...) + "</TableCellStructure>"
	}
	frag = story("Header", para("h")) +
		story("Content",
			`<ListStructure><ListItemStructure Marker="m1">`+para("m1", "i1")+`</ListItemStructure><ListItemStructure Marker="m2">`+para("m2", "i2")+`</ListItemStructure></ListStructure>`+
				`<TableStructure><TableRowGroupStructure>`+
				`<TableRowStructure>`+cell(` ColumnSpan="2"`, "c1")+`</TableRowStructure>`+
				`<TableRowStructure>`+cell("", "c2")+cell("", "c3")+`</TableRowStructure>`+
				`<TableRowStructure>`+cell("", "c4")+cell("", "c5")+`</TableRowStructure>`+
				`</TableRowGroupStructure></TableStructure>`) +
		story("Footer", para("f"))
	page = xpstest.Page(xpstest.XPS, 816, 1056, body)
	doc = xpsExtract(t, nil, xpsStructured([]string{page}, nil, "", []string{frag}).Bytes())
	p := &doc.Parts[0]
	var got []string
	for i := range p.Blocks {
		b := &p.Blocks[i]
		line := fmt.Sprintf("%v %v", b.Kind, b.Origin)
		switch {
		case b.Table != nil:
			for _, row := range b.Table.Rows {
				line += " [" + strings.Join(row, " | ") + "]"
			}
			for _, m := range b.Table.Merges {
				line += fmt.Sprintf(" merge %d,%d %dx%d", m.Row, m.Col, m.RowSpan, m.ColSpan)
			}
		case b.Kind == gofastcore.KindListItem:
			line += fmt.Sprintf(" %q %q", b.Marker, b.Text)
		default:
			line += " " + b.Text
		}
		got = append(got, line)
	}
	want := []string{
		"header native Declared the header",
		`list_item native "A" "the first item, with a letter for its marker"`,
		`list_item native "B" "the second item"`,
		"table native [Both columns | ] [Left | Right] [Below left | Below right] merge 0,0 1x2",
		"footer native Declared the footer",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("blocks:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}

	// A table that the page does not bear out is its text again: here
	// the cells of one declared row stand one under the other.
	body = g(96, 300, 14, "Declared left", `Name="c1"`) + g(96, 340, 14, "Declared right, but set below", `Name="c2"`) +
		g(96, 380, 14, "Declared in the second row", `Name="c3"`) + g(96, 320, 14, "but set between the two", `Name="c4"`)
	frag = story("Content", `<TableStructure><TableRowStructure>`+cell("", "c1")+cell("", "c2")+`</TableRowStructure><TableRowStructure>`+cell("", "c3")+cell("", "c4")+`</TableRowStructure></TableStructure>`)
	doc = xpsExtract(t, nil, xpsStructured([]string{xpstest.Page(xpstest.XPS, 816, 1056, body)}, nil, "", []string{frag}).Bytes())
	for i := range doc.Parts[0].Blocks {
		if doc.Parts[0].Blocks[i].Kind == gofastcore.KindTable {
			t.Errorf("a table the page does not bear out: %q", xpsBlocks(&doc.Parts[0]))
		}
	}

	// Fragments that break off: what stands before the damage is used,
	// and the page says so.
	page = xpstest.Page(xpstest.XPS, 816, 1056, g(96, 100, 16, "One line of the page,", `Name="a"`)+g(96, 120, 16, "and the next.", `Name="b"`))
	pkg := xpsStructured([]string{page}, nil, "", []string{story("Content", para("a")+para("b"))})
	whole := pkg.Get("Documents/1/Structure/Fragments/1.frag")
	pkg.Add("Documents/1/Structure/Fragments/1.frag", whole[:len(whole)-60])
	doc = xpsExtract(t, nil, pkg.Bytes())
	if !xpsWarned(doc.Parts[0].Warnings, gofastcore.WarnInputStreamDamaged) || len(doc.Parts[0].Blocks) == 0 {
		t.Errorf("damaged fragments: %v %q", doc.Parts[0].Warnings, xpsBlocks(&doc.Parts[0]))
	}
}

// FuzzXPSStructure feeds the parts that say what a document declares of
// itself to the whole library: the story fragments of a page, and the
// outline. Whatever they say, the document keeps the contract of the
// shared model and is the same for any number of workers.
func FuzzXPSStructure(f *testing.F) {
	n := func(name string) string { return `<NamedElement NameReference="` + name + `"/>` }
	f.Add(`<StoryFragment FragmentType="Content"><SectionStructure><ParagraphStructure>`+n("c")+n("d")+`</ParagraphStructure><ParagraphStructure>`+n("a")+`</ParagraphStructure></SectionStructure></StoryFragment>`,
		`<OutlineEntry OutlineLevel="2" Description="Chapter One" OutlineTarget="../FixedDoc.fdoc#c"/><OutlineEntry Description="By number" OutlineTarget="#2"/>`)
	f.Add(`<StoryFragment FragmentType="Header"><ParagraphStructure>`+n("a")+`</ParagraphStructure></StoryFragment><StoryFragment FragmentType="Content">`+
		`<ListStructure><ListItemStructure Marker="b"><ParagraphStructure>`+n("b")+n("c")+`</ParagraphStructure></ListItemStructure></ListStructure>`+
		`<TableStructure><TableRowGroupStructure><TableRowStructure><TableCellStructure ColumnSpan="2" RowSpan="3"><ParagraphStructure>`+n("d")+`</ParagraphStructure></TableCellStructure>`+
		`<TableCellStructure><TableStructure><TableRowStructure><TableCellStructure>`+n("e")+`</TableCellStructure></TableRowStructure></TableStructure></TableCellStructure></TableRowStructure></TableRowGroupStructure></TableStructure>`+
		`<FigureStructure>`+n("f")+`</FigureStructure></StoryFragment><StoryFragment FragmentType="Footer">`+n("g")+`</StoryFragment>`, "")
	g := xpstest.Glyphs
	body := ""
	for i, name := range []string{"a", "b", "c", "d", "e", "f", "g"} {
		body += g(96+float64(i%3)*200, 100+float64(i/3)*40, 16, "The text of element "+name+".", `Name="`+name+`"`)
	}
	page := xpstest.Page(xpstest.XPS, 816, 1056, body)
	font := xpstest.Font(nil)
	f.Fuzz(func(t *testing.T, fragment, outline string) {
		if outline == "" {
			outline = " "
		}
		pkg := xpsStructured([]string{page, page}, map[string]int{"c": 1}, outline, []string{fragment, fragment})
		pkg.Store = true
		pkg.Add(xpstest.FontName, font)
		data := pkg.Bytes()
		spans := len(fragment)%2 == 1
		one, err := extractBytes(data, extractOpts{Images: true, Spans: spans})
		if errors.Is(err, errContract) {
			t.Fatal(err)
		}
		if err != nil {
			t.Fatalf("a part of the structure ended the document: %v", err)
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

// TestXPSSignatureIntent: what a signature definition says its signature
// is to mean is a note of the page the definition names.
func TestXPSSignatureIntent(t *testing.T) {
	g := xpstest.Glyphs
	page := func(text string) string { return xpstest.Page(xpstest.XPS, 816, 1056, g(96, 120, 16, text, "")) }
	pkg := xpstest.Document(xpstest.XPS, page("The first page."), page("The second page."), xpstest.Page(xpstest.XPS, 816, 1056, ""))
	pkg.Add("Documents/1/_rels/FixedDoc.fdoc.rels", []byte(`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`+
		`<Relationship Id="R1" Type="`+xpstest.XPS+`/signature-definitions" Target="Signatures/defs.xml"/></Relationships>`))
	pkg.Add("Documents/1/Signatures/defs.xml", []byte(`<SignatureDefinitions xmlns="`+xpstest.XPS+`/signature-definitions">`+
		`<SignatureDefinition SignerName="A. Signer" SpotID="0947a67d-27e0-4a2d-9a0c-000000000001"><SpotLocation PageURI="/Documents/1/Pages/2.fpage" StartX="10" StartY="20"/>`+
		`<Intent>I have read and agree to the terms of page two.</Intent><SignBy>2026-12-31T00:00:00Z</SignBy><SigningLocation>Montr`+"é"+`al</SigningLocation></SignatureDefinition>`+
		`<SignatureDefinition SignerName="B. Signer"><Intent>Approved for release.</Intent></SignatureDefinition>`+
		`<SignatureDefinition SignerName="C. Signer"><SpotLocation PageURI="/Documents/1/Pages/3.fpage"/><Intent>The only thing on its page.</Intent></SignatureDefinition>`+
		`<SignatureDefinition SignerName="D. Signer"><Intent>  </Intent></SignatureDefinition>`+
		`</SignatureDefinitions>`))
	doc := xpsExtract(t, nil, pkg.Bytes())
	wantBlocks(t, &doc.Parts[0], "paragraph: The first page.", "note: Approved for release.")
	wantBlocks(t, &doc.Parts[1], "paragraph: The second page.", "note: I have read and agree to the terms of page two.")
	wantBlocks(t, &doc.Parts[2], "note: The only thing on its page.")
	if b := &doc.Parts[1].Blocks[1]; b.Attrs[gofastcore.AttrNoteType] != gofastcore.NoteComment || b.Origin != gofastcore.OriginNative {
		t.Errorf("note: %v %v", b.Attrs, b.Origin)
	}
	// A page with a note and no other text is not a page without text.
	if len(doc.Parts[2].Warnings) != 0 {
		t.Errorf("page 3: %v", doc.Parts[2].Warnings)
	}
}

// TestXPSAlternateContent: markup of a later version of the format comes
// with what a reader that does not know it is to read in its place.
func TestXPSAlternateContent(t *testing.T) {
	g := xpstest.Glyphs
	body := g(96, 100, 16, "Before the alternatives.", "") +
		`<mc:AlternateContent xmlns:mc="http://schemas.openxmlformats.org/markup-compatibility/2006" xmlns:v2="http://example.com/xps/v2">` +
		`<mc:Choice Requires="v2"><v2:Sparkle>` + g(96, 200, 16, "For a reader of version two.", "") + `</v2:Sparkle>` + g(96, 220, 16, "Also for that reader.", "") + `</mc:Choice>` +
		`<mc:Fallback><Canvas>` + g(96, 200, 16, "For every other reader.", "") + `</Canvas></mc:Fallback>` +
		`</mc:AlternateContent>` +
		`<v2:Unknown xmlns:v2="http://example.com/xps/v2">` + g(96, 300, 16, "In an element of another format.", "") + `</v2:Unknown>` +
		g(96, 400, 16, "After them.", "")
	doc := xpsDoc(t, nil, body)
	wantBlocks(t, &doc.Parts[0], "paragraph: Before the alternatives.", "paragraph: For every other reader.", "paragraph: After them.")
}

// TestXPSSidewaysAndFaces: glyphs set sideways advance by their height,
// and a font named with a number is that face of a collection.
func TestXPSSidewaysAndFaces(t *testing.T) {
	// Upright, "abcd" at 16 units is 4 glyphs of 8 units, 24 points;
	// sideways each advances by the height of the font's line, 16 units.
	doc := xpsDoc(t, nil, xpstest.Glyphs(96, 120, 16, "abcd", `IsSideways="true"`))
	box := doc.Parts[0].Blocks[0].Loc.BBox
	if w := box.X1 - box.X0; w < 47.9 || w > 48.1 {
		t.Errorf("sideways: %v wide", w)
	}

	// The second face of a collection has wide glyphs: two words placed
	// by its advances stand apart, and by those of the first would run
	// into each other.
	wide := map[rune]int{}
	for r := rune('a'); r <= 'z'; r++ {
		wide[r] = 1000
	}
	ttc := xpstest.Collection(xpstest.Font(&xpstest.FontOptions{Name: "First"}), xpstest.Font(&xpstest.FontOptions{Name: "Second Wide", Advances: wide}))
	glyphs := func(x float64, text string) string {
		return fmt.Sprintf(`<Glyphs Fill="#FF000000" FontUri="/Resources/both.ttc#1" FontRenderingEmSize="20" OriginX="%g" OriginY="120" UnicodeString="%s"/>`, x, text)
	}
	pkg := xpstest.Document(xpstest.XPS, xpstest.Page(xpstest.XPS, 816, 1056, glyphs(96, "wide")+glyphs(176, "word")))
	pkg.Add("Resources/both.ttc", ttc)
	data := pkg.Bytes()
	f, err := gofastpdf.OpenBytes(data, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	page, _ := f.Page(1)
	lines, err := page.Lines()
	if err != nil || len(lines) != 1 || lines[0].Text != "wideword" || lines[0].Font != "Second Wide" {
		t.Errorf("lines: %v %+v", err, lines)
	}
}

// TestXPSOpenXPSStructure: an OpenXPS document writes its structure, its
// fragments and its resources in namespaces of its own, and is read as
// an XPS document is.
func TestXPSOpenXPSStructure(t *testing.T) {
	ox := func(s string) string { return strings.ReplaceAll(s, xpstest.XPS, xpstest.OpenXPS) }
	g := xpstest.Glyphs
	body := `<FixedPage.Resources><ResourceDictionary xmlns:x="` + xpstest.XPS + `/resourcedictionary-key"><SolidColorBrush x:Key="none" Color="#00000000"/></ResourceDictionary></FixedPage.Resources>` +
		g(96, 100, 16, "Set as one paragraph with the line below it,", `Name="a"`) + g(96, 120, 16, "but declared to be a paragraph of its own.", `Name="b"`) +
		strings.Replace(g(96, 300, 16, "Not drawn.", ""), `Fill="#FF000000"`, `Fill="{StaticResource none}"`, 1)
	frag := `<StoryFragment FragmentType="Content"><ParagraphStructure><NamedElement NameReference="a"/></ParagraphStructure><ParagraphStructure><NamedElement NameReference="b"/></ParagraphStructure></StoryFragment>`
	outline := `<OutlineEntry OutlineLevel="1" Description="The page" OutlineTarget="#1"/>`
	pkg := xpsStructured([]string{xpstest.Page(xpstest.XPS, 816, 1056, body)}, nil, outline, []string{frag})
	for _, name := range []string{"_rels/.rels", "FixedDocSeq.fdseq", "Documents/1/FixedDoc.fdoc", "Documents/1/_rels/FixedDoc.fdoc.rels", "Documents/1/Structure/DocStructure.struct",
		"Documents/1/Pages/1.fpage", "Documents/1/Pages/_rels/1.fpage.rels", "Documents/1/Structure/Fragments/1.frag"} {
		pkg.Add(name, []byte(ox(string(pkg.Get(name)))))
	}
	doc := xpsExtract(t, nil, pkg.Bytes())
	if doc.Source.Format != "oxps" || len(doc.Outline) != 1 || doc.Outline[0].Part != 1 {
		t.Errorf("format %q, outline %+v", doc.Source.Format, doc.Outline)
	}
	p := &doc.Parts[0]
	wantBlocks(t, p, "paragraph: Set as one paragraph with the line below it,", "paragraph: but declared to be a paragraph of its own.", "paragraph: Not drawn.")
	if p.Blocks[2].Display != gofastcore.DisplayNoInk {
		t.Errorf("display %v", p.Blocks[2].Display)
	}
}

// TestXPSTrailingData: bytes after the end of the archive are reported.
func TestXPSTrailingData(t *testing.T) {
	data := xpstest.Document(xpstest.XPS, xpstest.Page(xpstest.XPS, 816, 1056, xpstest.Glyphs(96, 120, 16, "The page.", ""))).Bytes()
	doc := xpsExtract(t, nil, append(data, []byte("what was appended to the file")...))
	wantBlocks(t, &doc.Parts[0], "paragraph: The page.")
	if !xpsWarned(doc.Warnings, gofastcore.WarnInputTrailingData) {
		t.Errorf("warnings: %v", doc.Warnings)
	}
	if doc = xpsExtract(t, nil, data); len(doc.Warnings) != 0 {
		t.Errorf("warnings of a file with nothing after it: %v", doc.Warnings)
	}
}

// TestXPSDamagedCorpus cuts and changes the XPS files of the shared
// corpus that are for development, when GOFAST_CORPUS names it: every
// copy is read or refused, never with an internal error, and what is
// read keeps the contract of the shared model.
func TestXPSDamagedCorpus(t *testing.T) {
	root := os.Getenv("GOFAST_CORPUS")
	if root == "" {
		t.Skip("GOFAST_CORPUS is not set")
	}
	files, _ := filepath.Glob(filepath.Join(root, "dev", "xps", "*.xps"))
	read, refused := 0, 0
	for _, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if len(data) > 1<<20 {
			continue // the small files: each copy is read whole
		}
		try := func(what string, copy []byte) {
			_, err := extractBytes(copy, extractOpts{Images: true, Spans: true, Workers: 2})
			switch {
			case err == nil:
				read++
			case errors.Is(err, errContract) || strings.Contains(err.Error(), "internal error"):
				t.Errorf("%s, %s: %v", filepath.Base(path), what, err)
			default:
				refused++
			}
		}
		// Cut at 40 places, and 120 copies with 8 bytes changed each, the
		// same every time.
		for i := 1; i <= 40; i++ {
			try(fmt.Sprintf("cut to %d/40", i), data[:len(data)*i/41])
		}
		state := uint32(len(data))*2654435761 + 1
		next := func() uint32 {
			state ^= state << 13
			state ^= state >> 17
			state ^= state << 5
			return state
		}
		for i := 0; i < 120; i++ {
			copy := append([]byte(nil), data...)
			for k := 0; k < 8; k++ {
				copy[next()%uint32(len(copy))] ^= byte(1 << (next() % 8))
			}
			try(fmt.Sprintf("changed copy %d", i), copy)
		}
	}
	t.Logf("%d copies read, %d refused", read, refused)
}
