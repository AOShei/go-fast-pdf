package gofastpdf_test

import (
	"bytes"
	"compress/lzw"
	"encoding/ascii85"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"

	"github.com/AOShei/go-fast-pdf/internal/pdftest"
)

// Defect tests for file structure (X), fonts (F) and performance (P).
// Encryption (E) is tested on generated fixtures in fixtures_test.go.

func textDoc(words string, configure func(*pdftest.Builder)) *pdftest.Builder {
	return pdftest.Doc(pdftest.DocOptions{Configure: configure}, pdftest.PageSpec{Content: pdftest.TextPage(words)})
}

func TestX1DamagedXRef(t *testing.T) {
	data := textDoc("recovered text", nil).Bytes()
	t.Run("no-xref", func(t *testing.T) {
		// The file is cut off before its cross-reference table.
		cut := data[:bytes.LastIndex(data, []byte("xref\n0 "))]
		// pdftotext gives up without a trailer; qpdf and mutool rebuild
		// from the catalog, which is what this library should do.
		textCase(t, "X1/no-xref", cut, "recovered text", noOracle)
	})
	t.Run("bad-startxref", func(t *testing.T) {
		i := bytes.LastIndex(data, []byte("startxref\n")) + len("startxref\n")
		bad := append(append([]byte{}, data[:i]...), "7\n%%EOF\n"...)
		textCase(t, "X1/bad-startxref", bad, "recovered text", withOracle)
	})
	t.Run("wrong-offsets", func(t *testing.T) {
		// Every offset in the table is off by a few bytes, as after an
		// edit in a text editor.
		i := bytes.Index(data, []byte("1 0 obj"))
		shifted := append(append(append([]byte{}, data[:i]...), "% inserted\n"...), data[i:]...)
		textCase(t, "X1/wrong-offsets", shifted, "recovered text", withOracle)
	})
}

// hybridFile builds a hybrid-reference file: the catalog, page tree, font
// and page are in an object stream that only the cross-reference stream
// named by /XRefStm knows about; the classic table lists them as free.
func hybridFile() []byte {
	var out bytes.Buffer
	out.WriteString("%PDF-1.5\n")
	offs := map[int]int{}
	write := func(n int, body string) {
		offs[n] = out.Len()
		fmt.Fprintf(&out, "%d 0 obj\n%s\nendobj\n", n, body)
	}
	packed := []struct {
		n    int
		body string
	}{
		{1, "<< /Type /Catalog /Pages 2 0 R >>"},
		{2, "<< /Type /Pages /Kids [5 0 R] /Count 1 >>"},
		{3, pdftest.FontHelvetica},
		{5, "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 3 0 R >> >> /Contents 4 0 R >>"},
	}
	var index, bodies strings.Builder
	var rows []byte
	for i, o := range packed {
		fmt.Fprintf(&index, "%d %d ", o.n, bodies.Len())
		bodies.WriteString(o.body + "\n")
		rows = append(rows, 2, 0, 0, 0, 6, 0, byte(i))
	}
	write(4, pdftest.Stream("", pdftest.TextPage("hybrid file")))
	write(6, pdftest.Stream(fmt.Sprintf("/Type /ObjStm /N %d /First %d", len(packed), index.Len()), index.String()+bodies.String()))
	write(7, pdftest.Stream("/Type /XRef /Size 8 /W [1 4 2] /Index [1 3 5 1] /Root 1 0 R", string(rows)))
	xref := out.Len()
	out.WriteString("xref\n0 8\n")
	for n := 0; n < 8; n++ {
		if off, ok := offs[n]; ok {
			fmt.Fprintf(&out, "%010d %05d n \n", off, 0)
		} else if n == 0 {
			out.WriteString("0000000000 65535 f \n")
		} else {
			out.WriteString("0000000000 00000 f \n")
		}
	}
	fmt.Fprintf(&out, "trailer\n<< /Size 8 /Root 1 0 R /XRefStm %d >>\nstartxref\n%d\n%%%%EOF\n", offs[7], xref)
	return out.Bytes()
}

func TestX2HybridFile(t *testing.T) {
	textCase(t, "X2", hybridFile(), "hybrid file", withOracle)
}

func TestX3XRefStreamVariants(t *testing.T) {
	t.Run("no-type-field", func(t *testing.T) {
		// /W [0 4 2]: the type field is absent and defaults to 1.
		data := textDoc("no type field", func(b *pdftest.Builder) { b.XRefStream, b.XRefNoType = true, true }).Bytes()
		textCase(t, "X3/no-type-field", data, "no type field", withOracle)
	})
	t.Run("unfiltered", func(t *testing.T) {
		data := textDoc("raw xref stream", func(b *pdftest.Builder) { b.XRefStream, b.XRefRaw = true, true }).Bytes()
		textCase(t, "X3/unfiltered", data, "raw xref stream", withOracle)
	})
	t.Run("odd-index", func(t *testing.T) {
		// A malformed /Index must not crash; recovery of the text is X1.
		data := textDoc("odd index", func(b *pdftest.Builder) { b.XRefStream, b.XRefIndex = true, "0 7 9" }).Bytes()
		pdftest.Defect(t, "X3/odd-index", func() error {
			extractBytes(data, extractOpts{})
			return nil
		})
	})
}

func TestX4XRefEntryWidth(t *testing.T) {
	for name, eol := range map[string]string{"19-bytes": "\n", "21-bytes": " \r\n", "cr-only": " \r"} {
		t.Run(name, func(t *testing.T) {
			data := textDoc("entry width", func(b *pdftest.Builder) { b.EntryEOL = eol }).Bytes()
			textCase(t, "X4/"+name, data, "entry width", noOracle)
		})
	}
}

func TestX5WrongStreamLength(t *testing.T) {
	// The declared /Length is wrong; the stream really ends at endstream.
	content := pdftest.TextPage("stream length")
	for name, length := range map[string]int{"zero": 0, "short": 9, "long": len(content) + 40} {
		t.Run(name, func(t *testing.T) {
			b := textDoc("", nil)
			b.Set(4, fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", length, content))
			textCase(t, "X5/"+name, b.Bytes(), "stream length", noOracle)
		})
	}
}

func TestX6JunkBeforeHeader(t *testing.T) {
	// Bytes before %PDF-, with every offset counted from the header, as
	// in a file saved from an email or an HTTP response with stray bytes.
	data := textDoc("after the junk", func(b *pdftest.Builder) {
		b.Prefix = []byte("HTTP/1.1 200 OK\r\nContent-Type: application/pdf\r\n\r\n")
	}).Bytes()
	textCase(t, "X6", data, "after the junk", withOracle)
}

func runLengthEncode(data []byte) []byte {
	var out []byte
	for len(data) > 0 {
		n := min(len(data), 128)
		out = append(out, byte(n-1))
		out = append(out, data[:n]...)
		data = data[n:]
	}
	return append(out, 128)
}

func TestX7Filters(t *testing.T) {
	lines := []string{"filtered content stream"}
	for range 12 {
		lines = append(lines, "padding padding padding padding padding")
	}
	content := []byte(pdftest.TextPage(lines...))
	want := strings.Join(lines, " ")

	var a85 bytes.Buffer
	w := ascii85.NewEncoder(&a85)
	w.Write(content)
	w.Close()
	a85.WriteString("~>")

	// compress/lzw writes the variant without the early code-size change,
	// which PDF selects with /EarlyChange 0.
	var lz bytes.Buffer
	lw := lzw.NewWriter(&lz, lzw.MSB, 8)
	lw.Write(content)
	lw.Close()

	cases := map[string]struct {
		dict string
		data []byte
	}{
		"asciihex":    {"/Filter /ASCIIHexDecode", []byte(hex.EncodeToString(content) + ">")},
		"ascii85":     {"/Filter /ASCII85Decode", a85.Bytes()},
		"runlength":   {"/Filter /RunLengthDecode", runLengthEncode(content)},
		"lzw":         {"/Filter /LZWDecode /DecodeParms << /EarlyChange 0 >>", lz.Bytes()},
		"chain":       {"/Filter [/ASCIIHexDecode /FlateDecode]", []byte(hex.EncodeToString(pdftest.Deflate(content)) + ">")},
		"abbreviated": {"/Filter [/AHx /Fl]", []byte(hex.EncodeToString(pdftest.Deflate(content)) + ">")},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			b := textDoc("", nil)
			b.Set(4, pdftest.Stream(c.dict, string(c.data)))
			// Abbreviated filter names are defined for inline images only,
			// but occur on streams and are accepted by other readers.
			textCase(t, "X7/"+name, b.Bytes(), want, name != "abbreviated")
		})
	}
}

func TestX8CorruptFlateKeepsPrefix(t *testing.T) {
	// The compressed stream is cut in half. What inflates before the cut
	// is real content and must be kept.
	var lines []string
	for i := range 400 {
		lines = append(lines, fmt.Sprintf("line %04d of the page", i))
	}
	full := pdftest.Deflate([]byte(pdftest.TextPage(lines...)))
	b := textDoc("", nil)
	b.Set(4, pdftest.Stream("/Filter /FlateDecode", string(full[:len(full)/2])))
	data := b.Bytes()
	pdftest.Defect(t, "X8", func() error {
		got, err := textOf(data)
		if err != nil {
			return err
		}
		if !strings.HasPrefix(got, "line 0000 of the page line 0001 of the page") {
			return fmt.Errorf("text starts with %.60q, want the lines before the cut", got)
		}
		return nil
	})
}

// type0Font adds a composite font with Identity-H encoding to b and
// returns its dictionary. widths holds the descendant font's /DW and /W
// entries; toUnicode is the CMap text, or empty for none.
func type0Font(b *pdftest.Builder, widths, toUnicode string) string {
	desc := b.Add("<< /Type /FontDescriptor /FontName /Test /Flags 4 /FontBBox [0 0 1000 1000] /ItalicAngle 0 /Ascent 800 /Descent -200 /CapHeight 700 /StemV 80 >>")
	cid := b.Add(fmt.Sprintf("<< /Type /Font /Subtype /CIDFontType2 /BaseFont /Test /CIDSystemInfo << /Registry (Adobe) /Ordering (Identity) /Supplement 0 >> /FontDescriptor %d 0 R /CIDToGIDMap /Identity %s >>", desc, widths))
	font := fmt.Sprintf("<< /Type /Font /Subtype /Type0 /BaseFont /Test /Encoding /Identity-H /DescendantFonts [%d 0 R]", cid)
	if toUnicode != "" {
		font += fmt.Sprintf(" /ToUnicode %d 0 R", b.AddStream("", []byte(toUnicode)))
	}
	return font + " >>"
}

var abcd = toUnicodeCMap("<0000> <FFFF>", "<0001> <0041>", "<0002> <0042>", "<0003> <0043>", "<0004> <0044>")

func TestF1CompositeFontWidths(t *testing.T) {
	run := func(t *testing.T, key, widths, content, want string) {
		b := pdftest.New()
		font := type0Font(b, widths, abcd)
		data := pdftest.Doc(pdftest.DocOptions{Base: b, Font: font}, pdftest.PageSpec{Content: content}).Bytes()
		textCase(t, key, data, want, withOracle)
	}
	t.Run("dw", func(t *testing.T) {
		// Glyphs are 1000 units wide (/DW). Two glyphs at size 10 advance
		// 20, so the second run starts exactly where the first ends.
		run(t, "F1/dw", "/DW 1000", "BT /F1 10 Tf 100 700 Td <00010002> Tj 20 0 Td <00030004> Tj ET", "ABCD")
	})
	t.Run("w", func(t *testing.T) {
		// Glyphs are 200 units wide (/W). The first run ends at 104 and
		// the second starts at 108: a gap of two glyph widths.
		run(t, "F1/w", "/DW 1000 /W [1 [200 200 200 200]]", "BT /F1 10 Tf 100 700 Td <00010002> Tj 8 0 Td <00030004> Tj ET", "AB CD")
	})
}

func TestF2CodespaceAlignment(t *testing.T) {
	// A two-byte font with one code that has no mapping. The decoder must
	// stay aligned on two-byte codes, so B after the unmapped code is
	// still B.
	b := pdftest.New()
	font := type0Font(b, "/DW 500", toUnicodeCMap("<0000> <FFFF>", "<0041> <0041>", "<0042> <0042>"))
	data := pdftest.Doc(pdftest.DocOptions{Base: b, Font: font},
		pdftest.PageSpec{Content: "BT /F1 10 Tf 100 700 Td <0041FF420042> Tj ET"}).Bytes()
	pdftest.Defect(t, "F2", func() error {
		got, err := textOf(data)
		if err != nil {
			return err
		}
		if got = strings.ReplaceAll(got, "�", ""); got != "AB" {
			return fmt.Errorf("text is %q, want A and B with at most a replacement character between", got)
		}
		return nil
	})
}

func TestF3CoreFontMetrics(t *testing.T) {
	// Helvetica without /Widths: the widths are the standard metrics
	// (i is 222, W is 944, space is 278 per 1000).
	t.Run("narrow", func(t *testing.T) {
		// "iiii" at size 10 is 8.88 wide; the next run starts one space
		// width after it.
		data := pdftest.OnePage("BT /F1 10 Tf 100 700 Td (iiii) Tj 11.66 0 Td (iiii) Tj ET")
		textCase(t, "F3/narrow", data, "iiii iiii", withOracle)
	})
	t.Run("wide", func(t *testing.T) {
		// "WWWW" at size 10 is 37.76 wide; the next run starts right there.
		data := pdftest.OnePage("BT /F1 10 Tf 100 700 Td (WWWW) Tj 37.76 0 Td (WWWW) Tj ET")
		textCase(t, "F3/wide", data, "WWWWWWWW", withOracle)
	})
}

func TestF4GlyphNames(t *testing.T) {
	cases := map[string]struct{ name, want string }{
		"uni":      {"/uni00E9", "é"},
		"u":        {"/u1F600", "😀"},
		"agl":      {"/Lslash", "Ł"},
		"euro":     {"/Euro", "€"},
		"afii":     {"/afii10017", "А"}, // Cyrillic A
		"suffix":   {"/a.alt", "a"},
		"ligature": {"/f_f_i", "ffi"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			font := fmt.Sprintf("<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding << /Type /Encoding /Differences [1 %s] >> >>", c.name)
			data := pdftest.Doc(pdftest.DocOptions{Font: font}, pdftest.PageSpec{Content: "BT /F1 12 Tf 72 720 Td <01> Tj ET"}).Bytes()
			textCase(t, "F4/"+name, data, c.want, withOracle)
		})
	}
}

func TestF5OutputIsValidUTF8(t *testing.T) {
	// extractBytes fails when a page's text is not valid UTF-8.
	valid := func(t *testing.T, key string, data []byte) {
		pdftest.Defect(t, key, func() error {
			_, err := extractBytes(data, extractOpts{})
			return err
		})
	}
	t.Run("composite-no-tounicode", func(t *testing.T) {
		b := pdftest.New()
		font := type0Font(b, "/DW 500", "")
		valid(t, "F5/composite-no-tounicode", pdftest.Doc(pdftest.DocOptions{Base: b, Font: font},
			pdftest.PageSpec{Content: "BT /F1 10 Tf 100 700 Td <E9FF0041C3> Tj ET"}).Bytes())
	})
	t.Run("simple-high-bytes", func(t *testing.T) {
		font := "<< /Type /Font /Subtype /Type1 /BaseFont /Unknown >>"
		valid(t, "F5/simple-high-bytes", pdftest.Doc(pdftest.DocOptions{Font: font},
			pdftest.PageSpec{Content: "BT /F1 10 Tf 100 700 Td <41E9FFC328> Tj ET"}).Bytes())
	})
	t.Run("tounicode-surrogates", func(t *testing.T) {
		// A lone surrogate and an odd-length UTF-16 target.
		b := pdftest.New()
		font := type0Font(b, "/DW 500", toUnicodeCMap("<0000> <FFFF>", "<0001> <D800>", "<0002> <00E9E9>", "<0003> <DC00D800>"))
		valid(t, "F5/tounicode-surrogates", pdftest.Doc(pdftest.DocOptions{Base: b, Font: font},
			pdftest.PageSpec{Content: "BT /F1 10 Tf 100 700 Td <000100020003> Tj ET"}).Bytes())
	})
}

// benchPages returns a document of n pages of running text, about 3 KB of
// content stream per page, compressed as real files are.
func benchPages(n int) []byte {
	var lines []string
	for i := range 40 {
		lines = append(lines, fmt.Sprintf("Line %d of the page carries ordinary words for the extractor to read.", i))
	}
	content := pdftest.TextPage(lines...)
	pages := make([]pdftest.PageSpec, n)
	for i := range pages {
		pages[i].Content = content
	}
	return pdftest.Doc(pdftest.DocOptions{Compress: true}, pages...).Bytes()
}

func TestP1AllocationsPerPage(t *testing.T) {
	// The budget is one fifth of the 975 allocations per page measured for
	// v0.1.1, the Phase 1 target.
	const pages, budget = 50, 200
	path := pdftest.TempFile(t, benchPages(pages))
	pdftest.Defect(t, "P1", func() error {
		allocs := testing.AllocsPerRun(3, func() {
			if _, err := extractFile(path, extractOpts{}); err != nil {
				panic(err)
			}
		})
		perPage := allocs / pages
		t.Logf("%.0f allocations per page", perPage)
		if perPage > budget {
			return fmt.Errorf("%.0f allocations per page, budget %d", perPage, budget)
		}
		return nil
	})
}

func TestP4ImageMetadataDoesNotInflate(t *testing.T) {
	// A 16-megapixel gray image that compresses to a few kilobytes. Its
	// size and position are in the dictionary and the content stream;
	// reporting them must not decode the pixels.
	const side = 4096
	b := pdftest.New()
	img := b.AddFlate(fmt.Sprintf("/Type /XObject /Subtype /Image /Width %d /Height %d /ColorSpace /DeviceGray /BitsPerComponent 8", side, side),
		make([]byte, side*side))
	data := pdftest.Doc(pdftest.DocOptions{Base: b}, pdftest.PageSpec{
		Content:   "q 200 0 0 200 72 500 cm /Im1 Do Q",
		Resources: fmt.Sprintf("/XObject << /Im1 %d 0 R >>", img),
	}).Bytes()
	path := pdftest.TempFile(t, data)
	pdftest.Defect(t, "P4", func() error {
		var e *extraction
		var err error
		n := pdftest.AllocBytes(func() { e, err = extractFile(path, extractOpts{Images: true}) })
		if err != nil {
			return err
		}
		if len(e.Pages) != 1 || e.Pages[0].Images != 1 {
			return fmt.Errorf("pages are %+v, want one page with one image", e.Pages)
		}
		if n > 4<<20 {
			return fmt.Errorf("allocated %d MB to report the metadata of one image", n>>20)
		}
		return nil
	})
}
