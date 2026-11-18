package file

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"

	gofastcore "github.com/AOShei/go-fast-core"
	"github.com/AOShei/go-fast-pdf/internal/pdftest"
)

// C5: AES data that is not a whole number of blocks is an error, not a
// panic inside crypto/cipher.
func TestC5AESCiphertextNotBlockAligned(t *testing.T) {
	d := &decrypter{key: make([]byte, 16), stmCipher: cipherAES, strCipher: cipherAES}
	pdftest.Defect(t, "C5", func() error {
		for _, n := range []int{1, 15, 16 + 21, 33} {
			if _, err := d.decryptStream(make([]byte, n), 5, 0); !errors.Is(err, gofastcore.ErrMalformed) {
				return fmt.Errorf("%d bytes of AES data: error %v, want ErrMalformed", n, err)
			}
		}
		if out, err := d.decryptStream(make([]byte, 32), 5, 0); err != nil || len(out) > 16 {
			return fmt.Errorf("two blocks: %d bytes, %v", len(out), err)
		}
		return nil
	})
}

func encryptedDoc(p string) []byte {
	key := strings.Repeat("78", 32) // 32 bytes of 'x'
	return pdftest.Doc(pdftest.DocOptions{Configure: func(b *pdftest.Builder) {
		enc := b.Add(fmt.Sprintf("<< /Filter /Standard /V 2 /R 3 /Length 128 /O <%s> /U <%s> /P %s >>", key, key, p))
		b.Trailer = fmt.Sprintf("/Encrypt %d 0 R /ID [<0102030405060708> <0102030405060708>]", enc)
	}}, pdftest.PageSpec{Content: "x"}).Bytes()
}

// E5: /P is a 32-bit pattern. 4294967292 and -4 are the same pattern and
// must give the same key.
func TestE5PermissionsAsUnsigned(t *testing.T) {
	pdftest.Defect(t, "E5", func() error {
		signed, err1 := Open(encryptedDoc("-4"), Limits{})
		unsigned, err2 := Open(encryptedDoc("4294967292"), Limits{})
		if err1 != nil || err2 != nil {
			return fmt.Errorf("open: %v, %v", err1, err2)
		}
		if signed.enc == nil || !bytes.Equal(signed.enc.key, unsigned.enc.key) {
			return fmt.Errorf("keys differ: %x and %x", signed.enc.key, unsigned.enc.key)
		}
		other, err := Open(encryptedDoc("-8"), Limits{})
		if err != nil || bytes.Equal(signed.enc.key, other.enc.key) {
			return fmt.Errorf("a different /P gave the same key (%v)", err)
		}
		return nil
	})
}

func TestEncryptionDictionaryErrors(t *testing.T) {
	doc := func(dict string) []byte {
		return pdftest.Doc(pdftest.DocOptions{Configure: func(b *pdftest.Builder) {
			b.Trailer = fmt.Sprintf("/Encrypt %d 0 R", b.Add(dict))
		}}, pdftest.PageSpec{Content: "x"}).Bytes()
	}
	ok := "/O (" + strings.Repeat("o", 32) + ") /U (" + strings.Repeat("u", 32) + ") /P -4"
	cases := []struct {
		name, dict string
		want       error
	}{
		{"not a dictionary", "42", gofastcore.ErrMalformed},
		{"public-key handler", "<< /Filter /Adobe.PubSec /V 2 " + ok + " >>", gofastcore.ErrUnsupported},
		{"no filter", "<< /V 2 " + ok + " >>", gofastcore.ErrUnsupported},
		{"AES-256", "<< /Filter /Standard /V 5 /R 6 " + ok + " >>", gofastcore.ErrUnsupported},
		{"version 3", "<< /Filter /Standard /V 3 /R 3 " + ok + " >>", gofastcore.ErrUnsupported},
		{"no /O", "<< /Filter /Standard /V 2 /R 3 /U (u) /P -4 >>", gofastcore.ErrMalformed},
		{"/P is a name", "<< /Filter /Standard /V 2 /R 3 /O (o) /U (u) /P /All >>", gofastcore.ErrMalformed},
		{"key of 7 bits", "<< /Filter /Standard /V 2 /R 3 /Length 7 " + ok + " >>", gofastcore.ErrMalformed},
		{"key of 4096 bits", "<< /Filter /Standard /V 2 /R 3 /Length 4096 " + ok + " >>", gofastcore.ErrMalformed},
	}
	for _, c := range cases {
		if _, err := Open(doc(c.dict), Limits{}); !errors.Is(err, c.want) {
			t.Errorf("%s: error %v, want %v", c.name, err, c.want)
		}
	}
	// A direct encryption dictionary and a number given for /Encrypt.
	direct := pdftest.Doc(pdftest.DocOptions{Configure: func(b *pdftest.Builder) { b.Trailer = "/Encrypt 12" }}, pdftest.PageSpec{Content: "x"}).Bytes()
	if _, err := Open(direct, Limits{}); !errors.Is(err, gofastcore.ErrMalformed) {
		t.Errorf("/Encrypt 12: error %v, want ErrMalformed", err)
	}
}
