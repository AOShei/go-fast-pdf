package file

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	gofastcore "github.com/AOShei/go-fast-core"
	"github.com/AOShei/go-fast-pdf/internal/pdftest"
	"github.com/AOShei/go-fast-pdf/internal/syntax"
)

func fixtureData(t testing.TB, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "fixtures", name+".pdf"))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// allContent returns the decoded content of every page.
func allContent(t *testing.T, f *File) string {
	t.Helper()
	var sb strings.Builder
	for i := range f.Pages() {
		sb.WriteString(content(t, f, i))
		sb.WriteString("\f")
	}
	return sb.String()
}

// The encryption matrix: one document encrypted by qpdf in every
// supported way. Each variant must open with its user password and with
// its owner password, give the content and the document information of
// the unencrypted original, and derive the file key that qpdf reports.
func TestEncryptionMatrix(t *testing.T) {
	keys := map[string]string{}
	listing, err := os.ReadFile(filepath.Join("..", "..", "testdata", "fixtures", "KEYS.txt"))
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(listing)), "\n") {
		name, key, _ := strings.Cut(line, " ")
		keys[name] = key
	}
	want := allContent(t, open(t, fixtureData(t, "english")))

	variants := []struct{ name, user string }{
		{"enc-rc4-40", ""}, {"enc-rc4-128", ""}, {"enc-rc4-128-v4", ""}, {"enc-aes-128", ""},
		{"enc-aes-256", ""}, {"enc-aes-256-r5", ""},
		{"enc-aes-128-cleartext", ""}, {"enc-aes-256-cleartext", ""},
		{"enc-rc4-40-userpw", "user"}, {"enc-rc4-128-userpw", "user"}, {"enc-rc4-128-v4-userpw", "user"},
		{"enc-aes-128-userpw", "user"}, {"enc-aes-256-userpw", "user"}, {"enc-aes-256-r5-userpw", "user"},
		{"enc-aes-128-unicodepw", "pässwörd"}, {"enc-aes-256-unicodepw", "pässwörd"},
	}
	if len(variants) != len(keys) {
		t.Errorf("%d variants tested, %d fixtures have a key in KEYS.txt", len(variants), len(keys))
	}
	for _, v := range variants {
		t.Run(v.name, func(t *testing.T) {
			data := fixtureData(t, v.name)
			for role, password := range map[string]string{"user": v.user, "owner": "owner"} {
				f, err := OpenPassword(data, Limits{}, password)
				if err != nil {
					t.Fatalf("%s password: %v", role, err)
				}
				if !f.IsEncrypted() {
					t.Errorf("%s password: not reported as encrypted", role)
				}
				if got := allContent(t, f); got != want {
					t.Errorf("%s password: content differs from the unencrypted file", role)
				}
				info := f.Info()
				if title, author := info.Get("Title"), info.Get("Author"); string(title.Data) != "Sample document" || string(author.Data) != "go-fast-pdf tests" {
					t.Errorf("%s password: title %q, author %q", role, title.Data, author.Data)
				}
				if got := hex.EncodeToString(f.enc.key); got != keys[v.name] {
					t.Errorf("%s password: file key %s, qpdf derives %s", role, got, keys[v.name])
				}
				if w := f.Warnings(); len(w) != 0 {
					t.Errorf("%s password: warnings %v", role, w)
				}
			}

			if v.user == "" {
				// A file that needs no password opens whatever is given.
				if _, err := OpenPassword(data, Limits{}, "not needed"); err != nil {
					t.Errorf("with a password that is not needed: %v", err)
				}
				return
			}
			if _, err := Open(data, Limits{}); !errors.Is(err, gofastcore.ErrPasswordRequired) {
				t.Errorf("without a password: %v, want ErrPasswordRequired", err)
			}
			for _, wrong := range []string{"wrong", "User", "user ", "owner2", strings.Repeat("x", 200), "\x00"} {
				if _, err := OpenPassword(data, Limits{}, wrong); !errors.Is(err, gofastcore.ErrWrongPassword) {
					t.Errorf("with the password %q: %v, want ErrWrongPassword", wrong, err)
				}
			}
		})
	}
}

// An encrypted file that lost its trailer. With AES-256 the key does not
// depend on anything in the trailer, and the file is read. Before that
// it depends on the file identifier, which is gone: the answer is an
// error, never the ciphertext read as if it were content.
func TestEncryptedFileWithoutTrailer(t *testing.T) {
	want := allContent(t, open(t, fixtureData(t, "english")))
	for _, name := range []string{"enc-aes-256", "enc-aes-256-r5", "enc-aes-256-cleartext"} {
		data := fixtureData(t, name)
		f, err := Open(withoutXRef(data), Limits{})
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if !f.IsEncrypted() || allContent(t, f) != want || !repaired(f) {
			t.Errorf("%s: content differs, or no input.repaired warning", name)
		}
	}
	if f, err := OpenPassword(withoutXRef(fixtureData(t, "enc-aes-256-userpw")), Limits{}, "user"); err != nil || allContent(t, f) != want {
		t.Errorf("AES-256 with a user password: %v", err)
	}
	if _, err := Open(withoutXRef(fixtureData(t, "enc-aes-256-userpw")), Limits{}); !errors.Is(err, gofastcore.ErrPasswordRequired) {
		t.Errorf("AES-256 with a user password, none given: %v", err)
	}
	for _, name := range []string{"enc-rc4-40", "enc-rc4-128", "enc-rc4-128-v4", "enc-aes-128", "enc-aes-128-userpw"} {
		for _, password := range []string{"", "user", "owner"} {
			f, err := OpenPassword(withoutXRef(fixtureData(t, name)), Limits{}, password)
			if !errors.Is(err, gofastcore.ErrMalformed) {
				got := ""
				if f != nil {
					got = fmt.Sprintf("%.40q", content(t, f, 0))
				}
				t.Errorf("%s, password %q: error %v, content %s; want ErrMalformed", name, password, err, got)
			}
		}
	}
}

// withoutXRef removes the cross-reference data and the trailer: a
// classic table and what follows it, or the cross-reference stream
// object, whose dictionary is the trailer.
func withoutXRef(data []byte) []byte {
	if i := bytes.LastIndex(data, []byte("\nxref")); i >= 0 && bytes.LastIndex(data, []byte("/Type /XRef")) < i {
		return append([]byte{}, data[:i+1]...)
	}
	i := bytes.LastIndex(data, []byte("/XRef"))
	i = bytes.LastIndex(data[:i], []byte(" 0 obj"))
	i = bytes.LastIndexAny(data[:i], "\r\n") + 1
	return append([]byte{}, data[:i]...)
}

func TestPasswordForms(t *testing.T) {
	show := func(forms [][]byte) string {
		var parts []string
		for _, f := range forms {
			parts = append(parts, fmt.Sprintf("%x", f))
		}
		return strings.Join(parts, " ")
	}
	cases := []struct {
		password string
		r        int64
		want     string
	}{
		{"", 4, ""},
		{"abc", 3, "616263"},
		{"é", 4, "e9 c3a9"}, // Latin-1 first, then UTF-8
		{"€", 4, "e282ac"},  // not in Latin-1: UTF-8 only
		{"é", 6, "c3a9"},    // revisions 5 and 6: UTF-8
		{"", 6, ""},
		{strings.Repeat("é", 100), 6, strings.Repeat("c3a9", 63)}, // 127 bytes, cut on a character
	}
	for _, c := range cases {
		if got := show(passwordForms(c.password, c.r)); got != c.want {
			t.Errorf("%q, revision %d: %s, want %s", c.password, c.r, got, c.want)
		}
	}
}

// encryptAES is the reverse of what the reader does: a fixed
// initialisation vector, the padding, AES in CBC mode.
func encryptAES(key, plain []byte) []byte {
	pad := 16 - len(plain)%16
	plain = append(append([]byte{}, plain...), bytes.Repeat([]byte{byte(pad)}, pad)...)
	iv := []byte("0123456789abcdef")
	block, _ := aes.NewCipher(key)
	out := make([]byte, len(plain))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(out, plain)
	return append(iv, out...)
}

// C5 and the other ways encrypted data can be damaged: an error and what
// could be decrypted, never a panic.
func TestDamagedCiphertext(t *testing.T) {
	d := &decrypter{key: bytes.Repeat([]byte{7}, 16)}
	objKey := d.objectKey(5, 0, true)
	good := encryptAES(objKey, []byte("thirty-two bytes of plain text.."))

	pdftest.Defect(t, "C5", func() error {
		for _, n := range []int{1, 15} {
			if out, err := d.decrypt(cipherAESV2, good[:n], 5, 0); !errors.Is(err, gofastcore.ErrMalformed) || len(out) != 0 {
				return fmt.Errorf("%d bytes: %d bytes out, error %v", n, len(out), err)
			}
		}
		// Not a whole number of blocks: the whole blocks are decrypted.
		out, err := d.decrypt(cipherAESV2, good[:16+16+5], 5, 0)
		if !errors.Is(err, gofastcore.ErrMalformed) || string(out) != "thirty-two bytes" {
			return fmt.Errorf("cut inside a block: %q, error %v", out, err)
		}
		return nil
	})

	if out, err := d.decrypt(cipherAESV2, good, 5, 0); err != nil || string(out) != "thirty-two bytes of plain text.." {
		t.Errorf("undamaged: %q, %v", out, err)
	}
	if out, err := d.decrypt(cipherAESV2, good[:16], 5, 0); err != nil || len(out) != 0 {
		t.Errorf("an initialisation vector and no data: %q, %v", out, err)
	}
	// Bad padding: the data is kept whole, without an error.
	bad := encryptAES(objKey, []byte("sixteen bytes!!!"))
	bad = bad[:32] // drop the padding block: the last byte of the data is '!'
	if out, err := d.decrypt(cipherAESV2, bad, 5, 0); err != nil || string(out) != "sixteen bytes!!!" {
		t.Errorf("no padding block: %q, %v", out, err)
	}
	// The wrong object number gives noise, not the text.
	if out, _ := d.decrypt(cipherAESV2, good, 6, 0); bytes.Contains(out, []byte("plain")) {
		t.Error("the object number does not enter the key")
	}
	// RC4 has no structure to damage; empty data stays empty.
	if out, err := d.decrypt(cipherRC4, nil, 5, 0); err != nil || len(out) != 0 {
		t.Errorf("empty RC4 data: %q, %v", out, err)
	}
}

// encDoc builds an encrypted file by hand (revision 4, empty user
// password), so that tests can choose exactly what is encrypted how.
// qpdf's fixtures prove the algorithms; this proves which cipher is
// applied to which object.
type encDoc struct {
	b    *pdftest.Builder
	d    *decrypter
	dict string // the encryption dictionary's entries after /O /U /P
}

func newEncDoc(cf, stmF, strF string, encryptMetadata bool) *encDoc {
	id := []byte("0123456789abcdef")
	e := &encryptDict{v: 4, r: 4, keyLen: 16, p: 0xFFFFFFFC, id: id, encryptMetadata: encryptMetadata}
	// /O: the padded user password encrypted with a key made from the
	// owner password (Algorithm 3).
	sum := md5.Sum(padPassword([]byte("owner")))
	ownerKey := sum[:]
	for range 50 {
		s := md5.Sum(ownerKey)
		ownerKey = s[:]
	}
	e.o = padPassword(nil)
	rc4Rounds(ownerKey[:16], e.o, 0, 19)
	// /U: found by asking which key the empty password gives (Algorithm
	// 2) and encrypting the check value with it (Algorithm 5).
	e.u = make([]byte, 32)
	key := userKey(e)
	h := md5.New()
	h.Write(padding)
	h.Write(id)
	check := h.Sum(nil)
	rc4Rounds(key, check, 0, 19)
	copy(e.u, check)

	doc := &encDoc{b: pdftest.New(), d: &decrypter{key: key}}
	doc.dict = fmt.Sprintf("/Filter /Standard /V 4 /R 4 /Length 128 /O <%x> /U <%x> /P -4 /CF << %s >> /StmF /%s /StrF /%s /EncryptMetadata %v",
		e.o, e.u, cf, stmF, strF, encryptMetadata)
	doc.b.Trailer = fmt.Sprintf("/ID [<%x> <%x>]", id, id)
	return doc
}

// userKey computes the file key for the empty user password without
// checking it against /U, which the test is still building.
func userKey(e *encryptDict) []byte {
	h := md5.New()
	h.Write(padding)
	h.Write(e.o[:32])
	h.Write([]byte{byte(e.p), byte(e.p >> 8), byte(e.p >> 16), byte(e.p >> 24)})
	h.Write(e.id)
	if !e.encryptMetadata {
		h.Write([]byte{0xFF, 0xFF, 0xFF, 0xFF})
	}
	digest := h.Sum(nil)
	for range 50 {
		s := md5.Sum(digest[:e.keyLen])
		digest = s[:]
	}
	return digest[:e.keyLen]
}

// seal encrypts data for object num with the given cipher.
func (doc *encDoc) seal(kind cipherKind, num int, data string) string {
	switch kind {
	case cipherRC4:
		out := make([]byte, len(data))
		rc4Apply(doc.d.objectKey(uint32(num), 0, false), out, []byte(data))
		return string(out)
	case cipherAESV2:
		return string(encryptAES(doc.d.objectKey(uint32(num), 0, true), []byte(data)))
	}
	return data
}

func hexString(s string) string { return "<" + hex.EncodeToString([]byte(s)) + ">" }

// build lays out a one-page document: content stream, document
// information with a title, and whatever the test adds, and returns the
// opened file.
func (doc *encDoc) build(t *testing.T, stm, str cipherKind, add func(b *pdftest.Builder)) *File {
	t.Helper()
	b := doc.b
	catalog, pages, font, content, page, info, enc := b.Reserve(), b.Reserve(), b.Reserve(), b.Reserve(), b.Reserve(), b.Reserve(), b.Reserve()
	b.Root = catalog
	b.Set(catalog, fmt.Sprintf("<< /Type /Catalog /Pages %d 0 R >>", pages))
	b.Set(pages, fmt.Sprintf("<< /Type /Pages /Kids [%d 0 R] /Count 1 >>", page))
	b.Set(font, pdftest.FontHelvetica)
	b.Set(content, pdftest.Stream("", doc.seal(stm, content, "the content stream")))
	b.Set(page, fmt.Sprintf("<< /Type /Page /Parent %d 0 R /MediaBox [0 0 612 792] /Contents %d 0 R >>", pages, content))
	b.Set(info, fmt.Sprintf("<< /Title %s /Nested [ << /Deep %s >> ] >>", hexString(doc.seal(str, info, "the title")), hexString(doc.seal(str, info, "deep"))))
	b.Set(enc, "<< "+doc.dict+" >>")
	b.Trailer += fmt.Sprintf(" /Encrypt %d 0 R /Info %d 0 R", enc, info)
	if add != nil {
		add(b)
	}
	f, err := Open(b.Bytes(), Limits{})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return f
}

const stdCF = "/StdCF << /CFM /AESV2 /AuthEvent /DocOpen /Length 16 >>"

func TestCryptFilters(t *testing.T) {
	check := func(t *testing.T, f *File) {
		t.Helper()
		if got := content(t, f, 0); got != "the content stream" {
			t.Errorf("content %q", got)
		}
		info := f.Info()
		if got := string(info.Get("Title").Data); got != "the title" {
			t.Errorf("title %q", got)
		}
		if got := string(info.Get("Nested").Elems()[0].Get("Deep").Data); got != "deep" {
			t.Errorf("a string nested in an array and a dictionary: %q", got)
		}
	}
	t.Run("AES for both", func(t *testing.T) {
		check(t, newEncDoc(stdCF, "StdCF", "StdCF", true).build(t, cipherAESV2, cipherAESV2, nil))
	})
	t.Run("RC4 crypt filter", func(t *testing.T) {
		check(t, newEncDoc("/StdCF << /CFM /V2 /Length 16 >>", "StdCF", "StdCF", true).build(t, cipherRC4, cipherRC4, nil))
	})
	t.Run("streams in the clear, strings encrypted", func(t *testing.T) {
		check(t, newEncDoc(stdCF, "Identity", "StdCF", true).build(t, cipherNone, cipherAESV2, nil))
	})
	t.Run("strings in the clear, streams encrypted", func(t *testing.T) {
		check(t, newEncDoc(stdCF, "StdCF", "Identity", true).build(t, cipherAESV2, cipherNone, nil))
	})
	t.Run("different filters for streams and strings", func(t *testing.T) {
		cf := "/A << /CFM /AESV2 /Length 16 >> /R << /CFM /V2 /Length 16 >>"
		check(t, newEncDoc(cf, "A", "R", true).build(t, cipherAESV2, cipherRC4, nil))
	})
	t.Run("no /StmF and no /StrF means Identity", func(t *testing.T) {
		doc := newEncDoc(stdCF, "Identity", "Identity", true)
		doc.dict = strings.Replace(strings.Replace(doc.dict, "/StmF /Identity", "", 1), "/StrF /Identity", "", 1)
		check(t, doc.build(t, cipherNone, cipherNone, nil))
	})

	t.Run("a stream that names its own crypt filter", func(t *testing.T) {
		// The default for streams is AES; three streams choose
		// otherwise through a /Crypt filter.
		cf := stdCF + " /Alt << /CFM /V2 /Length 16 >>"
		doc := newEncDoc(cf, "StdCF", "StdCF", true)
		var rc4Stream, clear, flate int
		f := doc.build(t, cipherAESV2, cipherAESV2, func(b *pdftest.Builder) {
			rc4Stream, clear, flate = b.Reserve(), b.Reserve(), b.Reserve()
			b.Set(rc4Stream, pdftest.Stream("/Filter /Crypt /DecodeParms << /Name /Alt >>", doc.seal(cipherRC4, rc4Stream, "encrypted with the other filter")))
			b.Set(clear, pdftest.Stream("/Filter [/Crypt] /DecodeParms [<< /Name /Identity >>]", "left in the clear"))
			b.Set(flate, pdftest.Stream("/Filter [/Crypt /FlateDecode] /DecodeParms [<< /Name /Alt >> null]",
				doc.seal(cipherRC4, flate, string(pdftest.Deflate([]byte("compressed, then encrypted"))))))
		})
		check(t, f)
		for num, want := range map[int]string{rc4Stream: "encrypted with the other filter", clear: "left in the clear", flate: "compressed, then encrypted"} {
			got, err := f.Decode(f.Object(int64(num)), nil, 0)
			if err != nil || string(got) != want {
				t.Errorf("object %d: %q, %v; want %q", num, got, err, want)
			}
		}
	})
	t.Run("a stream names a crypt filter that does not exist", func(t *testing.T) {
		doc := newEncDoc(stdCF, "StdCF", "StdCF", true)
		var odd int
		f := doc.build(t, cipherAESV2, cipherAESV2, func(b *pdftest.Builder) {
			odd = b.Reserve()
			b.Set(odd, pdftest.Stream("/Filter /Crypt /DecodeParms << /Name /Nope >>", "data"))
		})
		if got, err := f.Decode(f.Object(int64(odd)), nil, 0); !errors.Is(err, gofastcore.ErrUnsupported) || len(got) != 0 {
			t.Errorf("%q, error %v; want nothing and ErrUnsupported", got, err)
		}
	})

	t.Run("metadata left in the clear", func(t *testing.T) {
		doc := newEncDoc(stdCF, "StdCF", "StdCF", false)
		var meta, other int
		f := doc.build(t, cipherAESV2, cipherAESV2, func(b *pdftest.Builder) {
			meta, other = b.Reserve(), b.Reserve()
			b.Set(meta, pdftest.Stream("/Type /Metadata /Subtype /XML", "<x:xmpmeta>readable by anyone</x:xmpmeta>"))
			b.Set(other, pdftest.Stream("/Type /NotMetadata", doc.seal(cipherAESV2, other, "still encrypted")))
		})
		check(t, f)
		if got, err := f.Decode(f.Object(int64(meta)), nil, 0); err != nil || string(got) != "<x:xmpmeta>readable by anyone</x:xmpmeta>" {
			t.Errorf("metadata stream: %q, %v", got, err)
		}
		if got, err := f.Decode(f.Object(int64(other)), nil, 0); err != nil || string(got) != "still encrypted" {
			t.Errorf("another stream: %q, %v", got, err)
		}
	})
	t.Run("metadata encrypted like everything else", func(t *testing.T) {
		doc := newEncDoc(stdCF, "StdCF", "StdCF", true)
		var meta int
		f := doc.build(t, cipherAESV2, cipherAESV2, func(b *pdftest.Builder) {
			meta = b.Reserve()
			b.Set(meta, pdftest.Stream("/Type /Metadata /Subtype /XML", doc.seal(cipherAESV2, meta, "<x:xmpmeta/>")))
		})
		if got, err := f.Decode(f.Object(int64(meta)), nil, 0); err != nil || string(got) != "<x:xmpmeta/>" {
			t.Errorf("metadata stream: %q, %v", got, err)
		}
	})

	t.Run("strings inside an object stream are not decrypted again", func(t *testing.T) {
		// The object stream as a whole is encrypted; the strings of the
		// objects inside it are plain once it is decrypted.
		doc := newEncDoc(stdCF, "StdCF", "StdCF", true)
		var stm int
		f := doc.build(t, cipherAESV2, cipherAESV2, func(b *pdftest.Builder) {
			stm = b.Reserve()
			body := "50 0 51 9 (inside) [(also inside)]"
			b.Set(stm, pdftest.Stream("/Type /ObjStm /N 2 /First 10", doc.seal(cipherAESV2, stm, body)))
		})
		// The classic table does not list objects 50 and 51: they are
		// found through the scan, which also covers that path.
		if got := f.Object(50); string(got.Data) != "inside" {
			t.Errorf("object 50 is %v", got)
		}
		if got := f.Object(51); len(got.List) != 1 || string(got.List[0].Data) != "also inside" {
			t.Errorf("object 51 is %v", got)
		}
	})

	t.Run("a damaged string becomes empty, not ciphertext", func(t *testing.T) {
		doc := newEncDoc(stdCF, "StdCF", "StdCF", true)
		var holder int
		f := doc.build(t, cipherAESV2, cipherAESV2, func(b *pdftest.Builder) {
			holder = b.Reserve()
			b.Set(holder, "[<0102030405> "+hexString(doc.seal(cipherAESV2, holder, "whole"))+"]")
		})
		got := f.Object(int64(holder))
		if len(got.List) != 2 || len(got.List[0].Data) != 0 || string(got.List[1].Data) != "whole" {
			t.Errorf("got %v", got)
		}
		if w := f.Warnings(); len(w) != 1 || w[0].Code != "pdf.string_undecryptable" {
			t.Errorf("warnings %v", w)
		}
	})

	t.Run("a damaged stream gives what could be decrypted, with an error", func(t *testing.T) {
		doc := newEncDoc(stdCF, "StdCF", "StdCF", true)
		var cutStream int
		f := doc.build(t, cipherAESV2, cipherAESV2, func(b *pdftest.Builder) {
			cutStream = b.Reserve()
			sealed := doc.seal(cipherAESV2, cutStream, strings.Repeat("0123456789abcdef", 4))
			b.Set(cutStream, pdftest.Stream("", sealed[:16+32+7]))
		})
		got, err := f.Decode(f.Object(int64(cutStream)), nil, 0)
		if !errors.Is(err, gofastcore.ErrMalformed) || string(got) != strings.Repeat("0123456789abcdef", 2) {
			t.Errorf("%q, error %v", got, err)
		}
	})
}

func TestEncryptionDictionaryErrors(t *testing.T) {
	doc := func(dict string) []byte {
		return pdftest.Doc(pdftest.DocOptions{Configure: func(b *pdftest.Builder) {
			b.Trailer = fmt.Sprintf("/Encrypt %d 0 R", b.Add(dict))
		}}, pdftest.PageSpec{Content: "x"}).Bytes()
	}
	o32 := "/O (" + strings.Repeat("o", 32) + ") /U (" + strings.Repeat("u", 32) + ") /P -4"
	o48 := "/O (" + strings.Repeat("o", 48) + ") /U (" + strings.Repeat("u", 48) + ") /OE (" + strings.Repeat("e", 32) + ") /UE (" + strings.Repeat("e", 32) + ") /P -4"
	cases := []struct {
		name, dict string
		want       error
	}{
		{"not a dictionary", "42", gofastcore.ErrMalformed},
		{"public-key handler", "<< /Filter /Adobe.PubSec /V 2 " + o32 + " >>", gofastcore.ErrUnsupported},
		{"no filter", "<< /V 2 /R 3 " + o32 + " >>", gofastcore.ErrUnsupported},
		{"version 3", "<< /Filter /Standard /V 3 /R 3 " + o32 + " >>", gofastcore.ErrUnsupported},
		{"version 6", "<< /Filter /Standard /V 6 /R 7 " + o48 + " >>", gofastcore.ErrUnsupported},
		{"version and revision that do not go together", "<< /Filter /Standard /V 5 /R 3 " + o32 + " >>", gofastcore.ErrUnsupported},
		{"no /O", "<< /Filter /Standard /V 2 /R 3 /U (u) /P -4 >>", gofastcore.ErrMalformed},
		{"/P is a name", "<< /Filter /Standard /V 2 /R 3 /O (o) /U (u) /P /All >>", gofastcore.ErrMalformed},
		{"/U of 5 bytes", "<< /Filter /Standard /V 2 /R 3 /O (" + strings.Repeat("o", 32) + ") /U (short) /P -4 >>", gofastcore.ErrMalformed},
		{"/O of 5 bytes", "<< /Filter /Standard /V 2 /R 3 /O (short) /U (" + strings.Repeat("u", 32) + ") /P -4 >>", gofastcore.ErrPasswordRequired},
		{"key of 7 bits", "<< /Filter /Standard /V 2 /R 3 /Length 7 " + o32 + " >>", gofastcore.ErrMalformed},
		{"key of 4096 bits", "<< /Filter /Standard /V 2 /R 3 /Length 4096 " + o32 + " >>", gofastcore.ErrMalformed},
		{"AES-256 without /OE and /UE", "<< /Filter /Standard /V 5 /R 6 /CF << /StdCF << /CFM /AESV3 >> >> /StmF /StdCF /StrF /StdCF " + o32 + " >>", gofastcore.ErrMalformed},
		{"crypt filter method this reader does not know", "<< /Filter /Standard /V 4 /R 4 /CF << /StdCF << /CFM /AESV9 >> >> /StmF /StdCF /StrF /StdCF " + o32 + " >>", gofastcore.ErrUnsupported},
		{"AES-256 method in a 128-bit file", "<< /Filter /Standard /V 4 /R 4 /CF << /StdCF << /CFM /AESV3 >> >> /StmF /StdCF /StrF /StdCF " + o32 + " >>", gofastcore.ErrUnsupported},
		{"/StmF names a filter that is not defined", "<< /Filter /Standard /V 4 /R 4 /CF << >> /StmF /Missing /StrF /Identity " + o32 + " >>", gofastcore.ErrUnsupported},
		// Well formed, and no password opens it: /O and /U are not the
		// result of any password.
		{"values that match no password, revision 3", "<< /Filter /Standard /V 2 /R 3 /Length 128 " + o32 + " >>", gofastcore.ErrPasswordRequired},
		{"values that match no password, revision 2", "<< /Filter /Standard /V 1 /R 2 " + o32 + " >>", gofastcore.ErrPasswordRequired},
		{"values that match no password, revision 6", "<< /Filter /Standard /V 5 /R 6 /CF << /StdCF << /CFM /AESV3 >> >> /StmF /StdCF /StrF /StdCF " + o48 + " >>", gofastcore.ErrPasswordRequired},
	}
	for _, c := range cases {
		if _, err := Open(doc(c.dict), Limits{}); !errors.Is(err, c.want) {
			t.Errorf("%s: error %v, want %v", c.name, err, c.want)
		}
	}
	direct := pdftest.Doc(pdftest.DocOptions{Configure: func(b *pdftest.Builder) { b.Trailer = "/Encrypt 12" }}, pdftest.PageSpec{Content: "x"}).Bytes()
	if _, err := Open(direct, Limits{}); !errors.Is(err, gofastcore.ErrMalformed) {
		t.Errorf("/Encrypt 12: error %v, want ErrMalformed", err)
	}
	// With a password given, the same unusable values are a wrong password.
	bad := doc("<< /Filter /Standard /V 2 /R 3 /Length 128 " + o32 + " >>")
	if _, err := OpenPassword(bad, Limits{}, "guess"); !errors.Is(err, gofastcore.ErrWrongPassword) {
		t.Errorf("a password for values that match none: %v, want ErrWrongPassword", err)
	}
}

// E5: /P is a 32-bit pattern. 4294967292 and -4 are the same pattern and
// must give the same key.
func TestE5PermissionsAsUnsigned(t *testing.T) {
	pdftest.Defect(t, "E5", func() error {
		key := func(p string) ([]byte, error) {
			doc := newEncDoc(stdCF, "StdCF", "StdCF", true)
			doc.dict = strings.Replace(doc.dict, "/P -4", "/P "+p, 1)
			f, err := Open(func() []byte {
				b := doc.b
				b.Set(1, "<< /Type /Catalog >>")
				b.Trailer += fmt.Sprintf(" /Encrypt %d 0 R", b.Add("<< "+doc.dict+" >>"))
				return b.Bytes()
			}(), Limits{})
			if err != nil {
				return nil, err
			}
			return f.enc.key, nil
		}
		signed, err := key("-4")
		if err != nil {
			return fmt.Errorf("/P -4: %v", err)
		}
		unsigned, err := key("4294967292")
		if err != nil {
			return fmt.Errorf("/P 4294967292: %v", err)
		}
		if !bytes.Equal(signed, unsigned) {
			return fmt.Errorf("keys differ: %x and %x", signed, unsigned)
		}
		// Another pattern no longer matches /U, which was made for -4.
		if _, err := key("-8"); !errors.Is(err, gofastcore.ErrPasswordRequired) {
			return fmt.Errorf("/P -8: %v, want the password check to fail", err)
		}
		return nil
	})
}

func FuzzDecrypt(f *testing.F) {
	f.Add([]byte("0123456789abcdef0123456789abcdef"), uint8(2), uint32(5))
	f.Add([]byte("short"), uint8(3), uint32(1))
	f.Add([]byte{}, uint8(1), uint32(0))
	f.Fuzz(func(t *testing.T, data []byte, kind uint8, obj uint32) {
		d := &decrypter{key: bytes.Repeat([]byte{1}, 16), str: cipherKind(kind % 4)}
		if d.str == cipherAESV3 {
			d.key = bytes.Repeat([]byte{1}, 32)
		}
		out, err := d.decrypt(d.str, data, obj, uint16(obj))
		if len(out) > len(data) {
			t.Fatalf("%d bytes out of %d", len(out), len(data))
		}
		if err == nil && d.str >= cipherAESV2 && len(data) > 0 && (len(data) < 16 || len(data)%16 != 0) {
			t.Fatalf("no error for %d bytes of AES data", len(data))
		}
		v := syntax.Value{Kind: syntax.Array, List: []syntax.Value{
			{Kind: syntax.String, Data: data},
			{Kind: syntax.Dict, List: []syntax.Value{{Kind: syntax.Name, Data: []byte("K")}, {Kind: syntax.String, Data: data}}},
		}}
		d.decryptStrings(v, obj, 0)
	})
}

// An encrypted file with its encryption dictionary fuzzed must never
// yield content unless it authenticates.
func FuzzEncryptedOpen(f *testing.F) {
	for _, name := range []string{"enc-rc4-40", "enc-aes-128", "enc-aes-256", "enc-aes-256-r5-userpw", "enc-rc4-128-v4-userpw"} {
		data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "fixtures", name+".pdf"))
		if err != nil {
			f.Fatal(err)
		}
		f.Add(data, "user")
	}
	f.Fuzz(func(t *testing.T, data []byte, password string) {
		file, err := OpenPassword(data, Limits{MaxStreamBytes: 1 << 20, MaxPages: 16}, password)
		if err != nil {
			return
		}
		for _, p := range file.Pages() {
			file.Decode(file.Resolve(p.Dict.Get("Contents")), nil, 0)
		}
		file.Info()
	})
}
