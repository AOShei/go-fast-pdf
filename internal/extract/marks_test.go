package extract

import (
	"testing"

	"github.com/AOShei/go-fast-pdf/internal/font"
	"github.com/AOShei/go-fast-pdf/internal/pdftest"
)

// A mark drawn by itself on the glyph before it, raised as a browser
// raises it, is part of that glyph's run, and with it the letter it
// makes; one after the glyph's advance, or far above it, is not.
func TestMarksOnGlyph(t *testing.T) {
	// A is alef, B hamza above, C beh. Helvetica's A is 8 points wide at
	// 12 points.
	toUni := pdftest.Stream("", "1 begincodespacerange <00> <FF> endcodespacerange 3 beginbfchar <41> <0627> <42> <0654> <43> <0628> endbfchar")
	for _, c := range []struct {
		name, content, want string
	}{
		{"on its alef", "BT /F1 12 Tf 100 700 Td (A) Tj 2 5 Td (B) Tj ET", "\u0623"},
		{"under it, before its start", "BT /F1 12 Tf 100 700 Td (A) Tj -6 -3 Td (B) Tj ET", "\u0623"},
		{"after its advance", "BT /F1 12 Tf 100 700 Td (A) Tj 10 0 Td (B) Tj ET", "\u0627|\u0654"},
		{"far above", "BT /F1 12 Tf 100 700 Td (A) Tj 2 20 Td (B) Tj ET", "\u0627|\u0654"},
		{"a letter, not a mark", "BT /F1 12 Tf 100 700 Td (A) Tj 2 5 Td (C) Tj ET", "\u0627|\u0628"},
	} {
		t.Run(c.name, func(t *testing.T) {
			d := doc{
				objects: []string{toUni},
				font:    "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /ToUnicode 1 0 R >>",
				content: c.content,
			}
			if got := d.run(t).texts(); got != c.want {
				t.Errorf("%+q, want %+q", got, c.want)
			}
		})
	}
}

// pieces returns the character of the body of beh, a dot under it, and
// beh: a composition of the table of known glyphs.
func pieces(t *testing.T) (body, dot rune) {
	t.Helper()
	for m := rune(font.MarkFirst); m < font.BodyFirst; m++ {
		if l, ok := font.Compose([]byte(string([]rune{0x066E, m}))); ok && l == "\u0628" {
			return 0x066E, m
		}
	}
	t.Fatal("no dot makes beh")
	return 0, 0
}

func TestComposeMarks(t *testing.T) {
	body, dot := pieces(t)
	b, d := string(body), string(dot)
	t.Run("in one string", func(t *testing.T) {
		in := &interp{text: []byte("x" + b + d + "\u064E" + b + d)}
		in.composeMarks(0, 0, 0)
		if string(in.text) != "x\u0628\u064E\u0628" || len(in.pending) != 0 {
			t.Errorf("%+q, pending %v", in.text, in.pending)
		}
	})
	t.Run("drawn after it", func(t *testing.T) {
		in := &interp{text: []byte("x" + b + d)}
		in.composeMarks(0, 1+len(b), 0)
		if string(in.text) != "x\u0628" {
			t.Errorf("%+q", in.text)
		}
	})
	t.Run("what comes after moves", func(t *testing.T) {
		in := &interp{text: []byte(b + d + "\u0644\u0627"), clusters: []Cluster{{Off: int32(len(b + d)), Len: 4}}}
		in.composeMarks(0, 0, 0)
		if string(in.text) != "\u0628\u0644\u0627" || in.clusters[0].Off != 2 {
			t.Errorf("%+q, cluster at %d", in.text, in.clusters[0].Off)
		}
	})
	t.Run("pieces left over", func(t *testing.T) {
		// A dot with no letter before it, and a body with no dot.
		in := &interp{text: []byte(d + "a" + string(rune(font.BodyFirst)) + "b")}
		in.composeMarks(0, 0, 0)
		in.composeMarks(0, 0, 0) // noted once
		if len(in.pending) != 2 {
			t.Fatalf("pending %v", in.pending)
		}
		in.settleMarks()
		if string(in.text) != "\uFFFDa\uFFFDb" || in.unmapped != 2 || len(in.pending) != 0 {
			t.Errorf("%+q, %d unmapped", in.text, in.unmapped)
		}
	})
}
