package gofastpdf_test

import (
	"os"
	"reflect"
	"strings"
	"testing"

	gofastcore "github.com/AOShei/go-fast-core"
)

// Control characters in the strings of a file are removed and counted
// with text.control_characters_removed: once for the document, its
// information, outline and names; once for each page, for what the page
// says outside its content; once for the stored part. A vertical tab, a
// form feed and U+0085 are white space and become a space, a carriage
// return a line break, uncounted; so is a language escape, which is no
// text.
func TestControlCharactersRemoved(t *testing.T) {
	removed := func(ws []gofastcore.Warning) (gofastcore.Warning, bool) {
		return warning(ws, gofastcore.WarnTextControlCharactersRemoved)
	}

	t.Run("metadata", func(t *testing.T) {
		d := synthDoc{
			Pages: []synthPage{{Content: visibleLines}},
			// Title: SOH and DEL (0x1B is a letter in PDFDocEncoding). Author: UTF-16 with a NUL inside.
			// Subject: two NULs at the end, as C leaves them. Keywords: a
			// vertical tab, which is a space. Creator: a language escape
			// and a C1 control in UTF-16.
			Info: `/Title (A\001B\177C) /Author <FEFF 0041 0000 0042> /Subject (Sub\000\000)
				/Keywords (x\013y) /Creator <FEFF 001B 6A61 001B 0057 0080 0072>`,
		}
		doc := mustExtract(t, d.bytes(), nil)
		md := doc.Metadata
		want := gofastcore.Metadata{Title: "ABC", Authors: []string{"AB"}, Subject: "Sub", Keywords: []string{"x y"}, Application: "Wr"}
		md.Created, md.Modified = want.Created, want.Modified
		if !reflect.DeepEqual(md, want) {
			t.Errorf("metadata %+v\nwant     %+v", md, want)
		}
		w, ok := removed(doc.Warnings)
		if !ok || w.Severity != gofastcore.SeverityInfo || !strings.HasPrefix(w.Message, "6 control characters were removed from the strings of the document") {
			t.Errorf("document warnings %+v", doc.Warnings)
		}
		for _, p := range doc.Parts {
			if _, ok := removed(p.Warnings); ok {
				t.Errorf("part %d has %s", p.Number, gofastcore.WarnTextControlCharactersRemoved)
			}
		}
	})

	t.Run("escape", func(t *testing.T) {
		// A language escape alone: nothing is removed.
		d := synthDoc{Pages: []synthPage{{Content: visibleLines}}, Info: `/Title <FEFF 001B 656E 5553 001B 0054>`}
		doc := mustExtract(t, d.bytes(), nil)
		if doc.Metadata.Title != "T" {
			t.Errorf("title %q", doc.Metadata.Title)
		}
		if w, ok := removed(doc.Warnings); ok {
			t.Errorf("%+v", w)
		}
	})

	t.Run("outline", func(t *testing.T) {
		d := synthDoc{
			Objects: []string{
				`<< /Type /Outlines /First 2 0 R /Last 2 0 R /Count 1 >>`,
				`<< /Title (Chap\002ter) /Parent 1 0 R >>`,
			},
			Catalog: "/Outlines 1 0 R",
			Pages:   []synthPage{{Content: visibleLines}},
		}
		doc := mustExtract(t, d.bytes(), nil)
		if len(doc.Outline) != 1 || doc.Outline[0].Title != "Chapter" {
			t.Fatalf("outline %+v", doc.Outline)
		}
		if w, ok := removed(doc.Warnings); !ok || !strings.HasPrefix(w.Message, "1 control character was removed") {
			t.Errorf("document warnings %+v", doc.Warnings)
		}
	})

	t.Run("page", func(t *testing.T) {
		// The text of a note is the page's.
		d := synthDoc{
			Objects: []string{`<< /Type /Annot /Subtype /Text /Rect [10 10 50 50] /Contents (no\001\002te) >>`},
			Pages:   []synthPage{{Content: visibleLines, Extra: "/Annots [1 0 R]"}},
		}
		doc := mustExtract(t, d.bytes(), nil)
		p := doc.Parts[0]
		if !strings.Contains(describe(&p), "note") || strings.ContainsRune(describe(&p), '\uFFFD') {
			t.Errorf("blocks:\n%s", describe(&p))
		}
		if w, ok := removed(p.Warnings); !ok || w.Severity != gofastcore.SeverityInfo || w.Message != "2 control characters were removed from the strings of the page" {
			t.Errorf("page warnings %+v", p.Warnings)
		}
		if w, ok := removed(doc.Warnings); ok {
			t.Errorf("document %+v", w)
		}
	})

	t.Run("stored", func(t *testing.T) {
		// An entry of the document information of the file's own making
		// is stored data.
		d := synthDoc{Pages: []synthPage{{Content: visibleLines}}, Info: `/Batch (v\001alue)`}
		doc := mustExtract(t, d.bytes(), nil)
		last := doc.Parts[len(doc.Parts)-1]
		if last.Kind != gofastcore.PartStored || len(last.Blocks) != 1 || last.Blocks[0].Text != "Batch: value" {
			t.Fatalf("last part %s:\n%s", last.Kind, describe(&last))
		}
		if w, ok := removed(last.Warnings); !ok || w.Message != "1 control character was removed from the stored data" {
			t.Errorf("stored part warnings %+v", last.Warnings)
		}
		if w, ok := removed(doc.Warnings); ok {
			t.Errorf("document %+v", w)
		}
	})

	t.Run("none", func(t *testing.T) {
		d := synthDoc{Pages: []synthPage{{Content: visibleLines}}, Info: `/Title (Plain) /Author (Tab\there)`}
		doc := mustExtract(t, d.bytes(), nil)
		if w, ok := removed(doc.Warnings); ok {
			t.Errorf("%+v", w)
		}
	})
}

// A file that was decrypted says so with input.decrypted, which names
// the security handler, the cipher and the password that opened it, and
// is Encrypted; one that was not has neither.
func TestDecrypted(t *testing.T) {
	for _, c := range []struct{ name, password, want string }{
		{"enc-rc4-40", "", "the standard security handler, revision 2, RC4 with a 40-bit key; opened with the empty user password"},
		{"enc-rc4-128", "", "the standard security handler, revision 3, RC4 with a 128-bit key; opened with the empty user password"},
		{"enc-rc4-128-v4", "", "the standard security handler, revision 4, RC4 with a 128-bit key; opened with the empty user password"},
		{"enc-aes-128", "", "the standard security handler, revision 4, AES-128; opened with the empty user password"},
		{"enc-aes-256", "", "the standard security handler, revision 6, AES-256; opened with the empty user password"},
		{"enc-aes-256-r5", "", "the standard security handler, revision 5, AES-256; opened with the empty user password"},
		{"enc-aes-128-userpw", "user", "the standard security handler, revision 4, AES-128; opened with the user password given"},
		{"enc-aes-256-userpw", "owner", "the standard security handler, revision 6, AES-256; opened with the owner password given"},
		{"enc-rc4-40-userpw", "owner", "the standard security handler, revision 2, RC4 with a 40-bit key; opened with the owner password given"},
		{"enc-aes-128", "wrong", "the standard security handler, revision 4, AES-128; opened with the empty user password; the password given opens nothing"},
		{"enc-aes-256", "owner", "the standard security handler, revision 6, AES-256; opened with the owner password given"},
	} {
		t.Run(c.name+"/"+c.password, func(t *testing.T) {
			data, err := os.ReadFile(fixture(c.name))
			if err != nil {
				t.Fatal(err)
			}
			doc := mustExtract(t, data, &gofastcore.Options{Password: c.password})
			w, ok := warning(doc.Warnings, gofastcore.WarnInputDecrypted)
			if !ok || w.Severity != gofastcore.SeverityInfo || w.Message != "the file was decrypted: "+c.want {
				t.Errorf("input.decrypted %+v\nwant message %q", w, "the file was decrypted: "+c.want)
			}
			if !doc.Metadata.Encrypted {
				t.Error("not Encrypted")
			}
		})
	}

	t.Run("plain", func(t *testing.T) {
		data, err := os.ReadFile(fixture("english"))
		if err != nil {
			t.Fatal(err)
		}
		doc := mustExtract(t, data, nil)
		if w, ok := warning(doc.Warnings, gofastcore.WarnInputDecrypted); ok || doc.Metadata.Encrypted {
			t.Errorf("%+v, Encrypted %v", w, doc.Metadata.Encrypted)
		}
	})

	t.Run("null", func(t *testing.T) {
		// An encryption dictionary of null is none.
		b := onePage(visibleLines).bytes()
		b = []byte(strings.Replace(string(b), "trailer\n<<", "trailer\n<< /Encrypt null", 1))
		if !strings.Contains(string(b), "/Encrypt null") {
			t.Skip("the trailer is not written as expected")
		}
		doc := mustExtract(t, b, nil)
		if w, ok := warning(doc.Warnings, gofastcore.WarnInputDecrypted); ok || doc.Metadata.Encrypted {
			t.Errorf("%+v, Encrypted %v", w, doc.Metadata.Encrypted)
		}
	})
}
