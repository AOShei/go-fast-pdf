//go:build ignore

// Command gen writes internal/font/cjk.bin, the CMaps that the PDF
// specification predefines for Chinese, Japanese and Korean, and the
// maps from the CIDs of their character collections to Unicode:
//
//	go run internal/font/gen/gen.go -cmaps DIR -unicode DIR -out internal/font/cjk.bin
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
// The notice the sources ask to be kept is in NOTICE at the root of the
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
	flag.Parse()
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
