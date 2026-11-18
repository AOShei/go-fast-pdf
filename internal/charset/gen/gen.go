//go:build ignore

// Command gen writes the tables of package charset from their sources:
//
//	go run internal/charset/gen/gen.go -agl DIR -afm DIR -spec FILE -out internal/charset
//
// The sources are not kept in this repository. They are:
//
//   - glyphlist.txt and zapfdingbats.txt, the Adobe Glyph List and the
//     names of the ITC Zapf Dingbats glyphs, from
//     https://github.com/adobe-type-tools/agl-aglfn (BSD-3-Clause).
//     SHA-256 a3b2f61ced9f3644cc0d4ecde5c59df34ca286c689d9484a43a710a81c466789
//     and f6394e3cb8a447e84a1dad75d4baaf2aa7f45dc104faf369f4720e1a774ef2dc.
//   - The Adobe Font Metrics files of the 14 standard PDF fonts, as
//     Adobe published them ("Font Metrics for the 14 PDF Core Fonts").
//     They give the widths of the glyphs and the built-in encodings of
//     Symbol and ZapfDingbats.
//   - The text of the PDF specification, for the code assignments of
//     the encodings it defines in Annex D: PDF 32000-1:2008 as Adobe
//     publishes it (PDF32000_2008.pdf, SHA-256
//     9de0ca9e8570d6209e8bd48a355be8eb6ec376acfc3fc3ae97cd8730351417ff),
//     made into text with "pdftotext -layout" (Poppler 26.10). The
//     tables are checked as they are read: a different extraction that
//     loses a row or a column stops the program.
//
// The notices these sources ask to be kept are in NOTICE at the root of
// the repository.
package main

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"go/format"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
)

func main() {
	aglDir := flag.String("agl", "", "directory with glyphlist.txt and zapfdingbats.txt")
	afmDir := flag.String("afm", "", "directory with the AFM files of the 14 standard fonts")
	specText := flag.String("spec", "", "the PDF specification as text, from pdftotext -layout")
	out := flag.String("out", ".", "directory of package charset")
	flag.Parse()
	log.SetFlags(0)

	// --- glyph names ---
	glyphs := map[string][]rune{}
	for _, name := range []string{"glyphlist.txt", "zapfdingbats.txt"} {
		for _, line := range lines(filepath.Join(*aglDir, name)) {
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			glyph, hex, ok := strings.Cut(line, ";")
			if !ok {
				log.Fatalf("%s: bad line %q", name, line)
			}
			var rs []rune
			for _, h := range strings.Fields(hex) {
				v, err := strconv.ParseUint(h, 16, 32)
				if err != nil {
					log.Fatalf("%s: bad line %q", name, line)
				}
				rs = append(rs, rune(v))
			}
			if _, dup := glyphs[glyph]; !dup {
				glyphs[glyph] = rs
			}
		}
	}
	names := make([]string, 0, len(glyphs))
	for n := range glyphs {
		names = append(names, n)
	}
	sort.Strings(names)

	var g bytes.Buffer
	header(&g, "glyph names: the Adobe Glyph List and the names of the Zapf Dingbats glyphs")
	var blob strings.Builder
	offs := []int{0}
	var vals []uint32
	var seqs []rune
	for _, n := range names {
		blob.WriteString(n)
		offs = append(offs, blob.Len())
		if rs := glyphs[n]; len(rs) == 1 {
			vals = append(vals, uint32(rs[0]))
		} else {
			// Several characters: the position and length in glyphSeqs.
			vals = append(vals, 1<<31|uint32(len(seqs))<<4|uint32(len(rs)))
			seqs = append(seqs, rs...)
		}
	}
	if blob.Len() > 1<<16-1 {
		log.Fatal("the names no longer fit 16-bit offsets")
	}
	fmt.Fprintf(&g, "// glyphNames holds the %d names in sorted order, one after another;\n// glyphOffs[i] is where name i starts.\nconst glyphNames = %q\n\n", len(names), blob.String())
	fmt.Fprint(&g, "var glyphOffs = [...]uint16{")
	for i, o := range offs {
		sep(&g, i, 16)
		fmt.Fprintf(&g, "%d,", o)
	}
	fmt.Fprint(&g, "\n}\n\n// glyphVals[i] is the character of name i, or, with the top bit set, the\n// position (bits 4 and up) and length (low 4 bits) of its characters in\n// glyphSeqs.\nvar glyphVals = [...]uint32{")
	for i, v := range vals {
		sep(&g, i, 10)
		fmt.Fprintf(&g, "%#x,", v)
	}
	fmt.Fprint(&g, "\n}\n\nvar glyphSeqs = [...]rune{")
	for i, r := range seqs {
		sep(&g, i, 10)
		fmt.Fprintf(&g, "%#x,", r)
	}
	fmt.Fprint(&g, "\n}\n")
	write(filepath.Join(*out, "glyphlist_gen.go"), g.Bytes())

	one := func(name string) rune {
		if rs := glyphs[name]; len(rs) == 1 {
			return rs[0]
		}
		return 0
	}

	// --- the metrics files ---
	type metric struct {
		code  int
		width int
		name  string
	}
	afm := func(font string) []metric {
		var ms []metric
		for _, line := range lines(filepath.Join(*afmDir, font+".afm")) {
			if !strings.HasPrefix(line, "C ") {
				continue
			}
			m := metric{code: -1}
			for _, field := range strings.Split(line, ";") {
				f := strings.Fields(field)
				if len(f) < 2 {
					continue
				}
				switch f[0] {
				case "C":
					m.code, _ = strconv.Atoi(f[1])
				case "WX":
					m.width, _ = strconv.Atoi(f[1])
				case "N":
					m.name = f[1]
				}
			}
			ms = append(ms, m)
		}
		if len(ms) == 0 {
			log.Fatalf("no metrics in %s.afm", font)
		}
		return ms
	}

	// --- encodings ---
	// Annex D of the specification, as text. A table is the lines from
	// its heading to the next heading, without the page headers and
	// footers.
	spec := lines(*specText)
	section := func(from, to string) []string {
		var out []string
		in := false
		for _, l := range spec {
			switch {
			case l == from:
				in, out = true, nil
			case l == to && in:
				return out
			case in && !strings.Contains(l, "Adobe Systems Incorporated") && !strings.HasPrefix(l, "PDF 32000-1:2008"):
				out = append(out, l)
			}
		}
		log.Fatalf("%s: no section from %q to %q", *specText, from, to)
		return nil
	}
	octal := func(s string) int {
		if s == "\u2014" { // a dash: the encoding does not have the character
			return -1
		}
		v, err := strconv.ParseUint(s, 8, 8)
		if err != nil {
			log.Fatalf("bad code %q", s)
		}
		return int(v)
	}

	// D.2: of each character of the Latin character set, its name and
	// its code in StandardEncoding, MacRomanEncoding, WinAnsiEncoding
	// and PDFDocEncoding. The table is two such columns side by side; a
	// name can be followed by the number of a footnote.
	const code = `(\x{2014}|[0-7]{3})`
	latinRow := regexp.MustCompile(`([A-Za-z]+)(?: [1-6])? +` + code + ` +` + code + ` +` + code + ` +` + code + `(?: |$)`)
	var std, mac, win, pdfDocNames [256]string
	latin := map[string]bool{}
	for _, l := range section("D.2 Latin Character Set and Encodings", "D.3 PDFDocEncoding Character Set") {
		for _, m := range latinRow.FindAllStringSubmatch(l, -1) {
			name := m[1]
			if latin[name] {
				log.Fatalf("D.2: %s is listed twice", name)
			}
			latin[name] = true
			for i, enc := range []*[256]string{&std, &mac, &win, &pdfDocNames} {
				c := octal(m[2+i])
				if c < 0 {
					continue
				}
				if enc[c] != "" {
					log.Fatalf("D.2: code %o is both %s and %s", c, enc[c], name)
				}
				enc[c] = name
			}
		}
	}
	// The character set has 229 characters, and the table is in the
	// order of their names.
	if len(latin) != 229 {
		log.Fatalf("D.2: %d characters read, not 229", len(latin))
	}
	// The footnotes of the table, which add codes to it:
	//
	//	3. In WinAnsiEncoding, all unused codes greater than 40 map to
	//	   the bullet character.
	//	5. The hyphen character is also encoded as 255 in
	//	   WinAnsiEncoding.
	//	6. The space character shall also be encoded as 312 in
	//	   MacRomanEncoding and as 240 in WinAnsiEncoding.
	also := func(enc *[256]string, c int, name string) {
		if enc[c] != "" {
			log.Fatalf("code %o is %s already", c, enc[c])
		}
		enc[c] = name
	}
	also(&win, 0o255, "hyphen")
	also(&win, 0o240, "space")
	also(&mac, 0o312, "space")
	for c := 0o41; c < 256; c++ {
		if win[c] == "" {
			win[c] = "bullet"
		}
	}

	// Table 115 (9.6.6.4): "The standard Roman encoding that is used on
	// Mac OS is the same as the MacRomanEncoding described in Annex D,
	// with the addition of 15 entries and the replacement of the currency
	// glyph with the Euro glyph". A TrueType font shows these 15 codes,
	// and a font that has no glyph for them shows nothing, so they are
	// read in MacRomanEncoding as well. The currency sign stays: of that
	// code the annex says that it "shall continue to map" to it.
	macRow := regexp.MustCompile(`^([A-Za-z]+) +([0-7]{3}) +([0-9]{3})$`)
	added := 0
	const table115 = "Table 115 \u2013 Differences between MacRomanEncoding and Mac OS Roman encoding"
	at := slices.Index(spec, table115)
	if at < 0 {
		log.Fatalf("%s: no %q", *specText, table115)
	}
	// The table is short and on one page; a blank line follows each row.
	for _, l := range spec[at+1 : at+40] {
		m := macRow.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		c := octal(m[2])
		if strconv.Itoa(c) != m[3] {
			log.Fatalf("Table 115: %s: octal %s is not decimal %s", m[1], m[2], m[3])
		}
		if m[1] == "Euro" {
			if mac[c] != "currency" {
				log.Fatalf("Table 115: Euro is not at the code of currency")
			}
			continue
		}
		also(&mac, c, m[1])
		added++
	}
	if added != 15 {
		log.Fatalf("Table 115: %d additions read, not 15", added)
	}

	// D.4: the expert character set and MacExpertEncoding, as names and
	// codes in two columns side by side.
	expertRow := regexp.MustCompile(`([A-Za-z]+) +([0-7]{3})(?: |$)`)
	var expert [256]string
	expertCount := 0
	for _, l := range section("D.4 Expert Set and MacExpertEncoding", "D.5 Symbol Set and Encoding") {
		for _, m := range expertRow.FindAllStringSubmatch(l, -1) {
			c := octal(m[2])
			if expert[c] != "" {
				log.Fatalf("D.4: code %o is both %s and %s", c, expert[c], m[1])
			}
			expert[c] = m[1]
			expertCount++
		}
	}
	if expertCount != 165 {
		log.Fatalf("D.4: %d characters read, not 165", expertCount)
	}

	// The built-in encodings of Symbol and ZapfDingbats are in their
	// metrics files.
	builtin := func(font string) [256]string {
		var enc [256]string
		for _, m := range afm(font) {
			if m.code >= 0 {
				enc[m.code] = m.name
			}
		}
		return enc
	}
	symbol, zapf := builtin("Symbol"), builtin("ZapfDingbats")
	// The metrics files of the text fonts state StandardEncoding as
	// well; it must be the one of the specification.
	for _, font := range []string{"Helvetica", "Times-Roman", "Courier"} {
		if builtin(font) != std {
			log.Fatalf("%s.afm and the specification differ on StandardEncoding", font)
		}
	}

	var e bytes.Buffer
	header(&e, "the encodings named in the PDF specification, Annex D")
	runeTable := func(goName, doc string, enc [256]string) {
		fmt.Fprintf(&e, "// %s\nvar %s = [256]uint16{", doc, goName)
		for code, name := range enc {
			sep(&e, code, 12)
			r := one(name)
			if name != "" && r == 0 {
				log.Fatalf("%s: code %d: glyph %q has no single character", goName, code, name)
			}
			if r > 0xFFFF {
				log.Fatalf("%s: code %d: outside the basic plane", goName, code)
			}
			fmt.Fprintf(&e, "%#04x,", r)
		}
		fmt.Fprint(&e, "\n}\n\n")
	}
	runeTable("standardEncoding", "standardEncoding is StandardEncoding: the character of each code, 0 where there is none.", std)
	runeTable("winAnsiEncoding", "winAnsiEncoding is WinAnsiEncoding.", win)
	runeTable("macRomanEncoding", "macRomanEncoding is MacRomanEncoding.", mac)
	runeTable("symbolEncoding", "symbolEncoding is the built-in encoding of the Symbol font.", symbol)
	runeTable("zapfDingbatsEncoding", "zapfDingbatsEncoding is the built-in encoding of the ZapfDingbats font.", zapf)

	// MacExpertEncoding names small capitals, old-style figures and the
	// like, which have no character of their own. The names are kept and
	// resolved by GlyphText when a font asks for the encoding.
	fmt.Fprint(&e, "// macExpertNames is MacExpertEncoding: the glyph name of each code.\nvar macExpertNames = [256]string{")
	for code, name := range expert {
		sep(&e, code, 6)
		fmt.Fprintf(&e, "%q,", name)
	}
	fmt.Fprint(&e, "\n}\n\n")

	// PDFDocEncoding (PDF 32000-1, Table D.2): Latin-1 with the range
	// 0x18 to 0x1F and 0x80 to 0xA0 filled as below. Control codes other
	// than tab, line feed and carriage return are undefined.
	var pdfDoc [256]rune
	for c := 0x20; c < 0x7F; c++ {
		pdfDoc[c] = rune(c)
	}
	for c := 0xA1; c <= 0xFF; c++ {
		pdfDoc[c] = rune(c)
	}
	pdfDoc[0xAD] = 0
	pdfDoc[0x09], pdfDoc[0x0A], pdfDoc[0x0D] = 0x09, 0x0A, 0x0D
	for i, r := range []rune{0x02D8, 0x02C7, 0x02C6, 0x02D9, 0x02DD, 0x02DB, 0x02DA, 0x02DC} {
		pdfDoc[0x18+i] = r
	}
	for i, r := range []rune{0x2022, 0x2020, 0x2021, 0x2026, 0x2014, 0x2013, 0x0192, 0x2044, 0x2039, 0x203A, 0x2212, 0x2030,
		0x201E, 0x201C, 0x201D, 0x2018, 0x2019, 0x201A, 0x2122, 0xFB01, 0xFB02, 0x0141, 0x0152, 0x0160, 0x0178, 0x017D,
		0x0131, 0x0142, 0x0153, 0x0161, 0x017E, 0, 0x20AC} {
		pdfDoc[0x80+i] = r
	}
	// The table of D.2 gives PDFDocEncoding by name for the characters of
	// the Latin set; the two must agree.
	for c, name := range pdfDocNames {
		if name != "" && one(name) != pdfDoc[c] {
			log.Fatalf("PDFDocEncoding: code %o is %s in D.2 and U+%04X here", c, name, pdfDoc[c])
		}
	}
	fmt.Fprint(&e, "// pdfDocEncoding is PDFDocEncoding, the encoding of text strings outside\n// content streams.\nvar pdfDocEncoding = [256]uint16{")
	for code, r := range pdfDoc {
		sep(&e, code, 12)
		fmt.Fprintf(&e, "%#04x,", r)
	}
	fmt.Fprint(&e, "\n}\n")
	write(filepath.Join(*out, "encodings_gen.go"), e.Bytes())

	// --- widths of the standard fonts ---
	var w bytes.Buffer
	header(&w, "glyph widths of the 14 standard fonts, in thousandths of the font size")
	fmt.Fprint(&w, "// runeWidth is the width of the glyph for one character.\ntype runeWidth struct {\n\tr rune\n\tw uint16\n}\n\n")
	for _, font := range []string{"Helvetica", "Helvetica-Bold", "Times-Roman", "Times-Bold", "Times-Italic", "Times-BoldItalic"} {
		byRune := map[rune]int{}
		for _, m := range afm(font) {
			if r := one(m.name); r != 0 {
				if _, dup := byRune[r]; !dup {
					byRune[r] = m.width
				}
			}
		}
		// The oblique faces of Helvetica have the widths of the upright.
		rs := make([]rune, 0, len(byRune))
		for r := range byRune {
			rs = append(rs, r)
		}
		sort.Slice(rs, func(i, j int) bool { return rs[i] < rs[j] })
		fmt.Fprintf(&w, "// Sorted by character.\nvar widths%s = [...]runeWidth{", strings.ReplaceAll(font, "-", ""))
		for i, r := range rs {
			sep(&w, i, 8)
			fmt.Fprintf(&w, "{%#x, %d},", r, byRune[r])
		}
		fmt.Fprint(&w, "\n}\n\n")
	}
	for _, font := range []string{"Symbol", "ZapfDingbats"} {
		var byCode [256]int
		for _, m := range afm(font) {
			if m.code >= 0 {
				byCode[m.code] = m.width
			}
		}
		fmt.Fprintf(&w, "// By character code in the font's built-in encoding.\nvar widths%s = [256]uint16{", font)
		for code, width := range byCode {
			sep(&w, code, 16)
			fmt.Fprintf(&w, "%d,", width)
		}
		fmt.Fprint(&w, "\n}\n\n")
	}
	for _, m := range afm("Courier") {
		if m.width != 600 {
			log.Fatal("Courier is not 600 wide throughout")
		}
	}
	write(filepath.Join(*out, "corewidths_gen.go"), w.Bytes())
}

func lines(path string) []string {
	f, err := os.Open(path)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	var out []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		out = append(out, strings.TrimSpace(sc.Text()))
	}
	return out
}

func header(b *bytes.Buffer, what string) {
	fmt.Fprintf(b, "// Code generated by gen/gen.go. DO NOT EDIT.\n\n// This file holds %s.\n// See gen/gen.go for the sources and NOTICE for their terms.\n\npackage charset\n\n", what)
}

func sep(b *bytes.Buffer, i, perLine int) {
	if i%perLine == 0 {
		b.WriteString("\n\t")
	} else {
		b.WriteByte(' ')
	}
}

func write(path string, src []byte) {
	out, err := format.Source(src)
	if err != nil {
		log.Fatalf("%s: %v", path, err)
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		log.Fatal(err)
	}
}
