//go:build ignore

// Command gen writes the tables of package font that come from data
// published by others:
//
//	go run internal/font/gen/gen.go -cmaps DIR -unicode DIR -out internal/font/cjk.bin
//	go run internal/font/gen/gen.go -cff FILE -strings internal/font/cffstrings_gen.go
//	go run internal/font/gen/gen.go -mac FILE -macout internal/font/macnames_gen.go
//
// The first writes cjk.bin: the CMaps that the PDF specification
// predefines for Chinese, Japanese and Korean, and the maps from the CIDs
// of their character collections to Unicode. The second writes the
// standard strings of the Compact Font Format: the glyph names that a
// CFF font program refers to by number. The third writes the 258 glyph
// names of the standard Macintosh order, which the 'post' table of a
// TrueType program refers to by number.
//
// The sources are not kept in this repository. They are Adobe's, under
// the BSD 3-Clause licence:
//
//   - -cmaps: https://github.com/adobe-type-tools/cmap-resources at commit
//     f5cf3bca7fdfeaceb77aa82847e974f2306c20b4 (2023-11-15): the CMaps
//     that map the codes of an encoding to CIDs, in Adobe-CNS1-7/CMap,
//     Adobe-GB1-6/CMap, Adobe-Japan1-7/CMap and Adobe-Korea1-2/CMap.
//   - -unicode: https://github.com/adobe-type-tools/mapping-resources-pdf
//     at commit 2dd5e53fb74a01718b9dfd448a0d1cce6fff2aa5 (2023-01-17),
//     directory pdf2unicode: Adobe-CNS1-UCS2, Adobe-GB1-UCS2,
//     Adobe-Japan1-UCS2, Adobe-Korea1-UCS2 and Adobe-KR-UCS2, which map
//     the CIDs of each collection to Unicode for the extraction of text.
//
// For -cff: Adobe Technical Note #5176, "The Compact Font Format
// Specification" (4 December 2003), as Adobe publishes it in
// https://github.com/adobe-type-tools/font-tech-notes (pdfs/5176.CFF.pdf,
// SHA-256 60e0696c4fac326de597722af37bcdbbd688e0337a548709a287405ff3b3996a),
// made into text with "pdftotext -layout" (Poppler 26.10). Only Appendix A
// is read: which number stands for which glyph name. The table is checked
// as it is read: 391 names, numbered from 0 without a gap.
//
// For -mac: Lib/fontTools/ttLib/standardGlyphOrder.py of fontTools
// (https://github.com/fonttools/fonttools, MIT License) at commit
// f3f799e1b6c090096d7dc9a6ca22f26dd25a4c81, which follows Apple's
// TrueType Reference Manual for the 'post' table. Each name is checked
// against the number in the comment after it.
//
// The notices the sources ask to be kept are in NOTICE at the root of the
// repository.
//
// The file is: "GFPCMAP1", the number of CMaps (2 bytes), then for each
// its name (a byte of length, then the name), the character collection
// whose CIDs it gives (likewise; empty for a map to Unicode), and where
// its data begins and how long it is (4 bytes each, from the start of the
// data). The data of each is its CMap without comments and blank lines,
// compressed with DEFLATE (compress/flate, best compression). All numbers
// are big-endian.
package main

import (
	"bufio"
	"bytes"
	"compress/flate"
	"encoding/binary"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// predefined are the CMaps of ISO 32000-2, Table 116, other than the
// Identity ones, by character collection.
var predefined = map[string][]string{
	"Adobe-GB1-6": {"GB-EUC-H", "GB-EUC-V", "GBpc-EUC-H", "GBpc-EUC-V", "GBK-EUC-H", "GBK-EUC-V",
		"GBKp-EUC-H", "GBKp-EUC-V", "GBK2K-H", "GBK2K-V", "UniGB-UCS2-H", "UniGB-UCS2-V",
		"UniGB-UTF16-H", "UniGB-UTF16-V"},
	"Adobe-CNS1-7": {"B5pc-H", "B5pc-V", "HKscs-B5-H", "HKscs-B5-V", "ETen-B5-H", "ETen-B5-V",
		"ETenms-B5-H", "ETenms-B5-V", "CNS-EUC-H", "CNS-EUC-V", "UniCNS-UCS2-H", "UniCNS-UCS2-V",
		"UniCNS-UTF16-H", "UniCNS-UTF16-V"},
	"Adobe-Japan1-7": {"83pv-RKSJ-H", "90ms-RKSJ-H", "90ms-RKSJ-V", "90msp-RKSJ-H", "90msp-RKSJ-V",
		"90pv-RKSJ-H", "Add-RKSJ-H", "Add-RKSJ-V", "EUC-H", "EUC-V", "Ext-RKSJ-H", "Ext-RKSJ-V",
		"H", "V", "UniJIS-UCS2-H", "UniJIS-UCS2-V", "UniJIS-UCS2-HW-H", "UniJIS-UCS2-HW-V",
		"UniJIS-UTF16-H", "UniJIS-UTF16-V"},
	"Adobe-Korea1-2": {"KSC-EUC-H", "KSC-EUC-V", "KSCms-UHC-H", "KSCms-UHC-V", "KSCms-UHC-HW-H",
		"KSCms-UHC-HW-V", "KSCpc-EUC-H", "UniKS-UCS2-H", "UniKS-UCS2-V", "UniKS-UTF16-H", "UniKS-UTF16-V"},
}

// toUnicode are the maps of the collections' CIDs to Unicode.
var toUnicode = []string{"Adobe-CNS1-UCS2", "Adobe-GB1-UCS2", "Adobe-Japan1-UCS2", "Adobe-Korea1-UCS2", "Adobe-KR-UCS2"}

var usecmap = regexp.MustCompile(`/([A-Za-z0-9-]+)\s+usecmap`)

type entry struct {
	name, collection string
	data             []byte
}

func main() {
	cmaps := flag.String("cmaps", "", "the cmap-resources repository")
	unicode := flag.String("unicode", "", "the pdf2unicode directory of mapping-resources-pdf")
	out := flag.String("out", "", "the file to write")
	cff := flag.String("cff", "", "Adobe Technical Note #5176 as text, from pdftotext -layout")
	stringsOut := flag.String("strings", "", "the Go file to write the CFF standard strings to")
	mac := flag.String("mac", "", "fontTools' standardGlyphOrder.py")
	macOut := flag.String("macout", "", "the Go file to write the standard Macintosh glyph names to")
	flag.Parse()
	if *cff != "" && *stringsOut != "" {
		writeCFFStrings(*cff, *stringsOut)
	}
	if *mac != "" && *macOut != "" {
		writeMacNames(*mac, *macOut)
	}
	if *cmaps == "" && *unicode == "" && *out == "" {
		return
	}
	if *cmaps == "" || *unicode == "" || *out == "" {
		flag.Usage()
		os.Exit(2)
	}
	var entries []entry
	seen := map[string]bool{}
	var add func(dir, collection, name string)
	add = func(dir, collection, name string) {
		if seen[name] {
			return
		}
		seen[name] = true
		data := strip(read(filepath.Join(dir, name)))
		// What a CMap uses comes with it.
		for _, m := range usecmap.FindAllSubmatch(data, -1) {
			add(dir, collection, string(m[1]))
		}
		entries = append(entries, entry{name, collection[:strings.LastIndex(collection, "-")], data})
	}
	for collection, names := range predefined {
		for _, name := range names {
			add(filepath.Join(*cmaps, collection, "CMap"), collection, name)
		}
	}
	for _, name := range toUnicode {
		if seen[name] {
			log.Fatalf("%s twice", name)
		}
		seen[name] = true
		entries = append(entries, entry{name, "", strip(read(filepath.Join(*unicode, name)))})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].name < entries[j].name })

	var index, data bytes.Buffer
	index.WriteString("GFPCMAP1")
	binary.Write(&index, binary.BigEndian, uint16(len(entries)))
	raw := 0
	for _, e := range entries {
		var z bytes.Buffer
		w, _ := flate.NewWriter(&z, flate.BestCompression)
		w.Write(e.data)
		w.Close()
		for _, s := range []string{e.name, e.collection} {
			if len(s) > 255 {
				log.Fatalf("name %q too long", s)
			}
			index.WriteByte(byte(len(s)))
			index.WriteString(s)
		}
		binary.Write(&index, binary.BigEndian, uint32(data.Len()))
		binary.Write(&index, binary.BigEndian, uint32(z.Len()))
		data.Write(z.Bytes())
		raw += len(e.data)
	}
	if err := os.WriteFile(*out, append(index.Bytes(), data.Bytes()...), 0o644); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("%d CMaps, %d bytes of text, %d written\n", len(entries), raw, index.Len()+data.Len())
}

// writeMacNames reads the standard Macintosh glyph order and writes it
// as a Go table.
func writeMacNames(src, out string) {
	entry := regexp.MustCompile(`^\s*"([^"]+)",\s*#\s*(\d+)\s*$`)
	var names []string
	for _, line := range strings.Split(string(read(src)), "\n") {
		m := entry.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		var n int
		fmt.Sscan(m[2], &n)
		if n != len(names) {
			log.Fatalf("name %q is numbered %d, want %d", m[1], n, len(names))
		}
		names = append(names, m[1])
	}
	if len(names) != 258 {
		log.Fatalf("%d names, want 258", len(names))
	}
	var b bytes.Buffer
	b.WriteString("// Code generated by internal/font/gen/gen.go; DO NOT EDIT.\n\n")
	b.WriteString("package font\n\n")
	b.WriteString("// macGlyphNames are the names of the glyphs of the standard Macintosh\n")
	b.WriteString("// order, which a TrueType 'post' table refers to by number.\n")
	b.WriteString("var macGlyphNames = [258]string{\n")
	for _, n := range names {
		fmt.Fprintf(&b, "\t%q,\n", n)
	}
	b.WriteString("}\n")
	if err := os.WriteFile(out, b.Bytes(), 0o644); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("%d Macintosh glyph names\n", len(names))
}

// writeCFFStrings reads Appendix A of the CFF specification, as text,
// and writes the standard strings as a Go table.
func writeCFFStrings(text, out string) {
	data := string(read(text))
	start := strings.Index(data, "(SID / name)")
	end := -1
	if start >= 0 {
		end = strings.Index(data[start:], "Appendix B\n")
	}
	if start < 0 || end < 0 {
		log.Fatal("Appendix A not found")
	}
	pair := regexp.MustCompile(`(\d+)\s+(\S+)`)
	names := map[int]string{}
	for _, line := range strings.Split(data[start+len("(SID / name)"):start+end], "\n") {
		if strings.Contains(line, "Appendix") || strings.Contains(line, "Dec 03") {
			continue // a page header
		}
		for _, m := range pair.FindAllStringSubmatch(line, -1) {
			var sid int
			fmt.Sscan(m[1], &sid)
			if old, dup := names[sid]; dup && old != m[2] {
				log.Fatalf("SID %d is %q and %q", sid, old, m[2])
			}
			names[sid] = m[2]
		}
	}
	if len(names) != 391 {
		log.Fatalf("%d standard strings, want 391", len(names))
	}
	var b bytes.Buffer
	b.WriteString("// Code generated by internal/font/gen/gen.go; DO NOT EDIT.\n\n")
	b.WriteString("package font\n\n")
	b.WriteString("// cffStandardStrings are the glyph names that the SIDs 0 to 390 of a\n")
	b.WriteString("// CFF font program stand for (CFF specification, Appendix A).\n")
	b.WriteString("var cffStandardStrings = [391]string{\n")
	for sid := 0; sid < 391; sid++ {
		name, ok := names[sid]
		if !ok {
			log.Fatalf("SID %d missing", sid)
		}
		fmt.Fprintf(&b, "\t%q,\n", name)
	}
	b.WriteString("}\n")
	if err := os.WriteFile(out, b.Bytes(), 0o644); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("%d CFF standard strings\n", len(names))
}

func read(path string) []byte {
	data, err := os.ReadFile(path)
	if err != nil {
		log.Fatal(err)
	}
	return data
}

// strip drops the comments, the blank lines and the spaces that start a
// line: what is left is what parseCMap reads.
func strip(data []byte) []byte {
	var out bytes.Buffer
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "%") {
			continue
		}
		out.WriteString(line)
		out.WriteByte('\n')
	}
	if err := sc.Err(); err != nil {
		log.Fatal(err)
	}
	return out.Bytes()
}
