package font

import (
	_ "embed"
	"encoding/binary"
	"sync"

	"github.com/AOShei/go-fast-core/inflate"
)

// The CMaps that the PDF specification predefines for Chinese, Japanese
// and Korean, and the maps from the CIDs of their character collections
// (Adobe-GB1, Adobe-CNS1, Adobe-Japan1, Adobe-Korea1 and Adobe-KR) to
// Unicode, as Adobe publishes them for the extraction of text. They are
// in cjk.bin, made by gen/gen.go, which names the sources; each is read
// and parsed the first time a font asks for it, and kept.
//
// A font whose encoding is such a CMap has the CIDs of its codes, and
// with them its widths. A font whose CIDs are those of one of the
// collections, by its CMap or by its CIDSystemInfo, has text for each
// CID that the collection gives a character, when nothing else says what
// the code is.

//go:embed cjk.bin
var cjkData []byte

// cjkEntry is a CMap of cjk.bin: where its compressed data is, the
// collection whose CIDs it gives ("" for a map to Unicode), and the
// CMap once it is read.
type cjkEntry struct {
	off, n     uint32
	collection string
	once       sync.Once
	m          *cmap
}

// maxCMapBytes bounds a CMap of cjk.bin decompressed. The largest is
// 324 KB; the bound only keeps a damaged cjk.bin from taking memory
// without end.
const maxCMapBytes = 16 << 20

// maxCMapDepth bounds the CMaps that one uses in turn.
const maxCMapDepth = 4

var cjkIndex = func() map[string]*cjkEntry {
	out := map[string]*cjkEntry{}
	d := cjkData
	if len(d) < 10 || string(d[:8]) != "GFPCMAP1" {
		return out
	}
	n := int(binary.BigEndian.Uint16(d[8:]))
	p := 10
	str := func() (string, bool) {
		if p >= len(d) || p+1+int(d[p]) > len(d) {
			return "", false
		}
		s := string(d[p+1 : p+1+int(d[p])])
		p += 1 + int(d[p])
		return s, true
	}
	entries := make([]*cjkEntry, 0, n)
	names := make([]string, 0, n)
	for i := 0; i < n; i++ {
		name, ok1 := str()
		collection, ok2 := str()
		if !ok1 || !ok2 || p+8 > len(d) {
			return map[string]*cjkEntry{}
		}
		e := &cjkEntry{off: binary.BigEndian.Uint32(d[p:]), n: binary.BigEndian.Uint32(d[p+4:]), collection: collection}
		p += 8
		entries, names = append(entries, e), append(names, name)
	}
	for i, e := range entries {
		if int64(p)+int64(e.off)+int64(e.n) > int64(len(d)) {
			return map[string]*cjkEntry{}
		}
		e.off += uint32(p)
		out[names[i]] = e
	}
	return out
}()

// predefinedCMap returns the predefined CMap of that name, with the CMap
// it uses as its parent; nil when this library does not have it.
func predefinedCMap(name string) *cmap {
	return predefinedAt(name, 0)
}

func predefinedAt(name string, depth int) *cmap {
	e := cjkIndex[name]
	if e == nil || depth > maxCMapDepth {
		return nil
	}
	e.once.Do(func() {
		data, err := inflate.Raw(nil, cjkData[e.off:e.off+e.n], maxCMapBytes)
		if err != nil {
			return
		}
		m := parseCMap(data)
		if m.parent != "" && m.parent != name {
			m.parentMap = predefinedAt(m.parent, depth+1)
			if len(m.space.ranges) == 0 && m.parentMap != nil {
				m.space = m.parentMap.space
			}
		}
		m.collection = e.collection
		e.m = m
	})
	return e.m
}

// collectionUnicode returns the map from the CIDs of a character
// collection, as its registry and ordering name it, to Unicode; nil for
// one this library does not have.
func collectionUnicode(registry, ordering string) *cmap {
	if registry != "Adobe" {
		return nil
	}
	switch ordering {
	case "GB1", "CNS1", "Japan1", "Korea1", "KR":
		return predefinedCMap("Adobe-" + ordering + "-UCS2")
	}
	return nil
}

// cidOf returns the CID of a code by a CMap and the CMaps it uses; ok is
// false when none of them maps it.
func (c *cmap) cidOf(code uint32, n int) (cid uint32, ok bool) {
	for m, depth := c, 0; m != nil && depth <= maxCMapDepth; m, depth = m.parentMap, depth+1 {
		if e := m.find(code, n); e != nil && e.kind == kindCID {
			return e.val + (code - e.lo), true
		}
	}
	return 0, false
}
