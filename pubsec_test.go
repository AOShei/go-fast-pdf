package gofastpdf_test

import (
	"bytes"
	"context"
	"crypto"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	gofastcore "github.com/AOShei/go-fast-core"
	gofastpdf "github.com/AOShei/go-fast-pdf"
	"github.com/AOShei/go-fast-pdf/internal/pdftest"
)

// Files encrypted for a certificate (the public-key security handler).

// pubsecFiles are four files of PDFBox's tests, encrypted by programs
// other than PDFBox, with the key that opens each and the text that
// PDFBox's own tests find in it. testdata/fetch.sh pubsec puts them in
// the cache.
var pubsecFiles = []struct{ file, key, text string }{
	{"AESkeylength128.pdf", "PDFBOX-4421.pem", "Key length: 128"},
	{"AESkeylength256.pdf", "PDFBOX-4421.pem", "Key length: 256"},
	{"AES128ExposedMeta.pdf", "PDFBOX-5249.pem", "AES key length: 128 with exposed Metadata"},
	{"AES256ExposedMeta.pdf", "PDFBOX-5249.pem", "AES key length: 256 with exposed Metadata"},
}

func pubsecDir(t *testing.T) string {
	dir := filepath.Join(corpusCache(), ".pubsec")
	if _, err := os.Stat(filepath.Join(dir, pubsecFiles[0].file)); err != nil {
		t.Skipf("no files encrypted for a certificate in %s; run testdata/fetch.sh pubsec", dir)
	}
	return dir
}

func pemKey(t *testing.T, path string) crypto.Decrypter {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for {
		var block *pem.Block
		if block, raw = pem.Decode(raw); block == nil {
			t.Fatalf("%s: no private key", path)
		}
		if key, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
			return key.(crypto.Decrypter)
		}
	}
}

func TestPublicKeyFiles(t *testing.T) {
	dir := pubsecDir(t)
	keys := map[string]crypto.Decrypter{}
	for _, f := range pubsecFiles {
		if keys[f.key] == nil {
			keys[f.key] = pemKey(t, filepath.Join(dir, f.key))
		}
	}
	for _, f := range pubsecFiles {
		t.Run(f.file, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(dir, f.file))
			if err != nil {
				t.Fatal(err)
			}
			// With the key: the text PDFBox finds, a document within the
			// contract, for any number of workers.
			for _, workers := range []int{1, 4} {
				doc := mustExtract(t, data, &gofastcore.Options{Keys: []crypto.Decrypter{keys[f.key]}, Workers: workers})
				if got := strings.Join(strings.Fields(doc.Text()), " "); got != f.text || !doc.Metadata.Encrypted {
					t.Errorf("%d workers: %q, encrypted %v", workers, got, doc.Metadata.Encrypted)
				}
			}
			// Without a key, with the other key, with a password: no key.
			var other crypto.Decrypter
			for name, k := range keys {
				if name != f.key {
					other = k
				}
			}
			for _, opts := range []*gofastcore.Options{nil, {Keys: []crypto.Decrypter{other}}, {Password: "w!z%C*F-JaNdRgUk"}} {
				_, err := gofastpdf.Extract(context.Background(), bytes.NewReader(data), int64(len(data)), opts)
				if !errors.Is(err, gofastcore.ErrKeyRequired) {
					t.Errorf("%+v: %v", opts, err)
				}
			}
			// The right key among others, which input.decrypted names.
			doc, err := gofastpdf.Extract(context.Background(), bytes.NewReader(data), int64(len(data)), &gofastcore.Options{Keys: []crypto.Decrypter{other, keys[f.key]}})
			if err != nil {
				t.Fatalf("the key second of two: %v", err)
			}
			w, ok := warning(doc.Warnings, gofastcore.WarnInputDecrypted)
			if !ok || w.Severity != gofastcore.SeverityInfo || !strings.Contains(w.Message, "the public-key security handler, adbe.pkcs7.s") ||
				!strings.HasSuffix(w.Message, "; opened with private key 2 of the 2 given") {
				t.Errorf("input.decrypted: %+v", w)
			}
		})
	}
}

// A file encrypted for a certificate and attached to a document is
// opened with the keys of the document's options, which core hands to
// the extractor of the attached file.
func TestPublicKeyFileAttached(t *testing.T) {
	dir := pubsecDir(t)
	inner, err := os.ReadFile(filepath.Join(dir, pubsecFiles[0].file))
	if err != nil {
		t.Fatal(err)
	}
	key := pemKey(t, filepath.Join(dir, pubsecFiles[0].key))
	data := synthDoc{
		Objects: []string{
			pdftest.Stream(fmt.Sprintf("/Type /EmbeddedFile /Subtype /application#2Fpdf /Params << /Size %d >>", len(inner)), string(inner)),
			"<< /Type /Filespec /F (locked.pdf) /EF << /F 1 0 R >> >>",
		},
		Catalog: "/Names << /EmbeddedFiles << /Names [(locked.pdf) 2 0 R] >> >>",
		Pages:   []synthPage{{Content: visibleLines}},
	}.bytes()
	child := func(ctx context.Context, name, mime string, r io.ReaderAt, size int64, opts *gofastcore.Options) (*gofastcore.Document, error) {
		return gofastpdf.Extract(ctx, r, size, opts)
	}
	doc := mustExtract(t, data, &gofastcore.Options{Child: child, Keys: []crypto.Decrypter{key}})
	if len(doc.Children) != 1 || strings.TrimSpace(doc.Children[0].Text()) != pubsecFiles[0].text {
		t.Errorf("with the key: %+v", doc.Children)
	}
	doc = mustExtract(t, data, &gofastcore.Options{Child: child})
	if w, ok := warning(doc.Children[0].Warnings, gofastcore.WarnChildEncrypted); !ok || w.Severity != gofastcore.SeverityError {
		t.Errorf("without a key: %+v", doc.Children[0].Warnings)
	}
}
