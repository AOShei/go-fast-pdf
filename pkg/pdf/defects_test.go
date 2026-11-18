package pdf

import (
	"fmt"
	"strings"
	"testing"

	"github.com/AOShei/go-fast-pdf/internal/pdftest"
)

// Defect cases that need the package's internals. This package is replaced
// in Phase 1; these two cases move to the encryption package in Phase 3.

func TestC5AESCiphertextNotBlockAligned(t *testing.T) {
	// 16 bytes of IV followed by 21 bytes of ciphertext: not a multiple of
	// the AES block size. An error is the right answer.
	h := &EncryptionHandler{V: 4, R: 4, EncryptKey: make([]byte, 16), Dict: &EncryptDict{Length: 128}}
	pdftest.Defect(t, "C5", func() error {
		if _, err := h.Decrypt(make([]byte, 16+21), 5, 0); err == nil {
			return fmt.Errorf("no error for ciphertext that is not block aligned")
		}
		return nil
	})
}

func TestE5PermissionsAsUnsigned(t *testing.T) {
	// Some producers write /P as an unsigned 32-bit number. 4294967292 is
	// the same bit pattern as -4, and the key derivation needs those bits.
	key := StringObject(strings.Repeat("x", 32))
	dict := DictionaryObject{
		"/Filter": NameObject("/Standard"), "/V": NumberObject(2), "/R": NumberObject(3),
		"/Length": NumberObject(128), "/O": key, "/U": key, "/P": NumberObject(4294967292),
	}
	pdftest.Defect(t, "E5", func() error {
		enc, err := ParseEncryptDict(dict, nil)
		if err != nil {
			return err
		}
		if enc.P != -4 {
			return fmt.Errorf("/P 4294967292 is read as %d, want -4", enc.P)
		}
		return nil
	})
}
