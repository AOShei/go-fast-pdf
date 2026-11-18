package gofastpdf_test

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	gofastcore "github.com/AOShei/go-fast-core"
	"github.com/AOShei/go-fast-pdf/internal/pdftest"
)

// Tests on the generated fixtures in testdata/fixtures (see testdata/gen.sh):
// structural variants and the encryption defects (E).

const fixtureDir = "testdata/fixtures"

func fixture(name string) string { return filepath.Join(fixtureDir, name+".pdf") }

// The committed fixtures must be the files gen.sh produced. A mismatch
// usually means a checkout converted line endings in a binary file.
func TestFixtureManifest(t *testing.T) {
	manifest, err := os.ReadFile(filepath.Join(fixtureDir, "MANIFEST.sha256"))
	if err != nil {
		t.Fatal(err)
	}
	listed := map[string]bool{}
	for _, line := range strings.Split(strings.TrimSpace(string(manifest)), "\n") {
		sum, name, ok := strings.Cut(strings.TrimSpace(line), "  ")
		if !ok {
			t.Fatalf("malformed manifest line %q", line)
		}
		listed[name] = true
		data, err := os.ReadFile(filepath.Join(fixtureDir, name))
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if got := sha256.Sum256(data); hex.EncodeToString(got[:]) != sum {
			t.Errorf("%s does not match MANIFEST.sha256", name)
		}
	}
	files, _ := filepath.Glob(filepath.Join(fixtureDir, "*.pdf"))
	for _, f := range files {
		if !listed[filepath.Base(f)] {
			t.Errorf("%s is not in MANIFEST.sha256; run testdata/gen.sh", filepath.Base(f))
		}
	}
}

// sameAs returns a check that the file extracts to the same pages as the
// base fixture.
func sameAs(base, name string, o extractOpts) func() error {
	return func() error {
		want, err := extractFile(fixture(base), extractOpts{})
		if err != nil {
			return fmt.Errorf("base fixture %s: %w", base, err)
		}
		got, err := extractFile(fixture(name), o)
		if err != nil {
			return err
		}
		if len(got.Pages) != len(want.Pages) {
			return fmt.Errorf("%d pages, want %d", len(got.Pages), len(want.Pages))
		}
		for i := range want.Pages {
			if got.Pages[i].Text != want.Pages[i].Text {
				return fmt.Errorf("page %d differs from %s: starts with %.50q, want %.50q",
					i+1, base, got.Pages[i].Text, want.Pages[i].Text)
			}
		}
		return nil
	}
}

// check runs fn as an ordinary test when key is empty and as a defect
// case otherwise.
func check(t *testing.T, key string, fn func() error) {
	t.Helper()
	if key != "" {
		pdftest.Defect(t, key, fn)
	} else if err := fn(); err != nil {
		t.Error(err)
	}
}

// The same document rewritten by qpdf in other container layouts must
// give exactly the same text.
func TestStructuralVariants(t *testing.T) {
	cases := []struct{ base, name, key string }{
		{"english", "english-objstm", ""},
		// B12: v0.1.1 loses the fi ligature in english.pdf and keeps it in
		// the linearized copy, so its output depends on object order.
		{"english", "english-linearized", "B12"},
		{"english", "english-uncompressed", ""},
		{"multilingual", "multilingual-objstm-linearized", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { check(t, c.key, sameAs(c.base, c.name, extractOpts{})) })
	}
}

func TestEncryptedFixtures(t *testing.T) {
	// Files that open with the empty user password must give the text of
	// the unencrypted original.
	open := []struct{ name, key string }{
		{"enc-rc4-40", ""},
		{"enc-rc4-128", ""},
		{"enc-rc4-128-v4", ""}, // V4 crypt filter that selects RC4 (E2)
		{"enc-aes-128", ""},
		{"enc-aes-256", ""},    // revision 6 (E3)
		{"enc-aes-256-r5", ""}, // revision 5 (E3)
		{"enc-aes-128-cleartext", ""},
		{"enc-aes-256-cleartext", ""},
	}
	for _, c := range open {
		t.Run(c.name, func(t *testing.T) { check(t, c.key, sameAs("english", c.name, extractOpts{})) })
	}

	// Files with a user password: without it the answer is an error, never
	// text (E1); with it, the original text (E4, which needs a password
	// parameter in the public API and stays open until Phase 5).
	locked := []struct{ name, password string }{
		{"enc-rc4-40-userpw", "user"},
		{"enc-rc4-128-userpw", "user"},
		{"enc-rc4-128-v4-userpw", "user"},
		{"enc-aes-128-userpw", "user"},
		{"enc-aes-256-userpw", "user"},
		{"enc-aes-256-r5-userpw", "user"},
		{"enc-aes-128-unicodepw", "pässwörd"},
		{"enc-aes-256-unicodepw", "pässwörd"},
	}
	for _, c := range locked {
		short := strings.TrimPrefix(c.name, "enc-")
		t.Run(c.name+"/no-password", func(t *testing.T) {
			e, err := extractFile(fixture(c.name), extractOpts{})
			if err == nil {
				t.Fatalf("no error without the password; returned %d pages starting %.40q", len(e.Pages), e.words())
			}
			if !errors.Is(err, gofastcore.ErrPasswordRequired) {
				t.Errorf("error %v, want ErrPasswordRequired", err)
			}
		})
		t.Run(c.name+"/password", func(t *testing.T) {
			pdftest.Defect(t, "E4/"+short, sameAs("english", c.name, extractOpts{Password: c.password}))
		})
	}

	t.Run("metadata", func(t *testing.T) {
		// Document information is encrypted too and must be decrypted.
		pdftest.Defect(t, "B10/encrypted", func() error {
			e, err := extractFile(fixture("enc-aes-128"), extractOpts{})
			if err != nil {
				return err
			}
			if !e.Encrypted {
				return errors.New("the document is not reported as encrypted")
			}
			if e.Title != "Sample document" || e.Author != "go-fast-pdf tests" {
				return fmt.Errorf("title %q, author %q; want those of english.pdf", e.Title, e.Author)
			}
			return nil
		})
	})
}

// An incremental update replaces an object by appending a new revision.
// The text of the latest revision is the document's text.
func TestIncrementalUpdate(t *testing.T) {
	for _, c := range []struct {
		name   string
		stream bool
	}{{"classic", false}, {"xref-stream", true}} {
		t.Run(c.name, func(t *testing.T) {
			b := pdftest.Doc(pdftest.DocOptions{Configure: func(b *pdftest.Builder) { b.XRefStream = c.stream }},
				pdftest.PageSpec{Content: pdftest.TextPage("old text")})
			b.Update()
			b.Set(4, pdftest.Stream("", pdftest.TextPage("new text")))
			textCase(t, "", b.Bytes(), "new text", withOracle)
		})
	}
}
