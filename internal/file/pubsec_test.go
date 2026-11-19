package file

import (
	"bytes"
	"crypto"
	"crypto/aes"
	"crypto/cipher"
	"crypto/des"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	gofastcore "github.com/AOShei/go-fast-core"
	"github.com/AOShei/go-fast-pdf/internal/pdftest"
)

// The public-key security handler. The files of PDFBox, encrypted by
// other programs, prove the derivation of the key (TestPublicKeyFiles
// in the package above). These prove which cipher goes with which kind
// of file, the envelopes in every form the handler reads, and what a
// key that does not fit, or a damaged envelope, gives. OpenSSL, where
// it is installed, checks the envelopes both ways.

var (
	testKeysOnce sync.Once
	testKeys     [2]*rsa.PrivateKey
)

func rsaKeys(t testing.TB) [2]*rsa.PrivateKey {
	testKeysOnce.Do(func() {
		for i := range testKeys {
			k, err := rsa.GenerateKey(rand.Reader, 2048)
			if err != nil {
				panic(err)
			}
			testKeys[i] = k
		}
	})
	return testKeys
}

// envelopeSpec says how an envelope is made.
type envelopeSpec struct {
	oaep    bool   // RSAES-OAEP with SHA-256, else PKCS #1 v1.5
	content string // aes128, aes256, 3des, rc2
}

type algorithmIdentifier struct {
	Algorithm  asn1.ObjectIdentifier
	Parameters asn1.RawValue `asn1:"optional"`
}

type issuerAndSerial struct {
	Issuer asn1.RawValue
	Serial *big.Int
}

type keyTransRecipient struct {
	Version   int
	Rid       issuerAndSerial
	Algorithm algorithmIdentifier
	Key       []byte
}

type encryptedContentInfo struct {
	ContentType asn1.ObjectIdentifier
	Algorithm   algorithmIdentifier
	Content     asn1.RawValue
}

type envelopedData struct {
	Version    int
	Recipients []keyTransRecipient `asn1:"set"`
	Content    encryptedContentInfo
}

type contentInfo struct {
	ContentType asn1.ObjectIdentifier
	Content     asn1.RawValue
}

// explicit wraps DER in an explicit context tag. encoding/asn1 does not
// add the tag of a field to a RawValue that is already encoded.
func explicit(tag int, der []byte) asn1.RawValue {
	return asn1.RawValue{Class: asn1.ClassContextSpecific, Tag: tag, IsCompound: true, Bytes: der}
}

func mustMarshal(t testing.TB, v any) []byte {
	t.Helper()
	der, err := asn1.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return der
}

// makeEnvelope encrypts content for pub, as a CMS ContentInfo of
// EnvelopedData.
func makeEnvelope(t testing.TB, pub *rsa.PublicKey, content []byte, spec envelopeSpec) []byte {
	t.Helper()
	var oid asn1.ObjectIdentifier
	var cek []byte
	switch spec.content {
	case "aes128", "":
		oid, cek = oidAES128CBC, make([]byte, 16)
	case "aes256":
		oid, cek = oidAES256CBC, make([]byte, 32)
	case "3des":
		oid, cek = oidDESEDE3CBC, make([]byte, 24)
	case "rc2":
		oid, cek = oidRC2CBC, make([]byte, 16)
	}
	rand.Read(cek)
	var block cipher.Block
	var err error
	switch spec.content {
	case "3des":
		block, err = des.NewTripleDESCipher(cek)
	default:
		block, err = aes.NewCipher(cek[:min(len(cek), 32)])
	}
	if err != nil {
		t.Fatal(err)
	}
	n := block.BlockSize()
	iv := make([]byte, n)
	rand.Read(iv)
	pad := n - len(content)%n
	plain := append(append([]byte(nil), content...), bytes.Repeat([]byte{byte(pad)}, pad)...)
	ct := make([]byte, len(plain))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(ct, plain)

	keyAlg := algorithmIdentifier{Algorithm: oidRSA, Parameters: asn1.NullRawValue}
	var encryptedKey []byte
	if spec.oaep {
		sha256Alg := mustMarshal(t, algorithmIdentifier{Algorithm: oidSHA256})
		mgf := mustMarshal(t, struct {
			Algorithm asn1.ObjectIdentifier
			Hash      asn1.RawValue
		}{oidMGF1, asn1.RawValue{FullBytes: sha256Alg}})
		params := mustMarshal(t, struct {
			Hash asn1.RawValue
			MGF  asn1.RawValue
		}{explicit(0, sha256Alg), explicit(1, mgf)})
		keyAlg = algorithmIdentifier{Algorithm: oidRSAOAEP, Parameters: asn1.RawValue{FullBytes: params}}
		encryptedKey, err = rsa.EncryptOAEP(sha256.New(), rand.Reader, pub, cek, nil)
	} else {
		encryptedKey, err = rsa.EncryptPKCS1v15(rand.Reader, pub, cek)
	}
	if err != nil {
		t.Fatal(err)
	}
	issuer := mustMarshal(t, pkix.Name{CommonName: "a recipient"}.ToRDNSequence())
	env := envelopedData{
		Version: 0,
		Recipients: []keyTransRecipient{{
			Rid:       issuerAndSerial{Issuer: asn1.RawValue{FullBytes: issuer}, Serial: big.NewInt(7)},
			Algorithm: keyAlg,
			Key:       encryptedKey,
		}},
		Content: encryptedContentInfo{
			ContentType: asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 7, 1},
			Algorithm:   algorithmIdentifier{Algorithm: oid, Parameters: asn1.RawValue{FullBytes: mustMarshal(t, iv)}},
			Content:     asn1.RawValue{Class: asn1.ClassContextSpecific, Tag: 0, Bytes: ct},
		},
	}
	return mustMarshal(t, contentInfo{ContentType: oidEnvelopedData, Content: explicit(0, mustMarshal(t, env))})
}

// pubsecDoc is a file encrypted for the recipients given. Of the seed,
// the envelopes and the kind of file it works out the file key the way
// the handler is specified to, and encrypts the file with it.
type pubsecSpec struct {
	sub             string // s3, s4, s5
	v               int
	cfm             string // for s5: V2, AESV2, AESV3
	encryptMetadata bool
	recipients      []*rsa.PublicKey
	envelope        envelopeSpec
}

func pubsecDoc(t *testing.T, spec pubsecSpec) *encDoc {
	t.Helper()
	seed := make([]byte, 20)
	rand.Read(seed)
	content := append(append([]byte(nil), seed...), 0xFF, 0xFF, 0xFF, 0xFC)
	var envelopes [][]byte
	for _, pub := range spec.recipients {
		envelopes = append(envelopes, makeEnvelope(t, pub, content, spec.envelope))
	}
	h := sha1.New()
	if spec.v == 5 {
		h = sha256.New()
	}
	h.Write(seed)
	for _, e := range envelopes {
		h.Write(e)
	}
	if !spec.encryptMetadata && spec.v >= 4 {
		h.Write([]byte{0xFF, 0xFF, 0xFF, 0xFF})
	}
	sum := h.Sum(nil)
	var list string
	for _, e := range envelopes {
		list += fmt.Sprintf("<%x>", e)
	}
	doc := &encDoc{b: pdftest.New()}
	switch spec.sub {
	case "s3", "s4":
		keyLen := 5
		if spec.v == 2 {
			keyLen = 16
		}
		doc.d = &decrypter{key: sum[:keyLen]}
		doc.dict = fmt.Sprintf("/Filter /Adobe.PubSec /SubFilter /adbe.pkcs7.%s /V %d /Length %d /Recipients [%s]", spec.sub, spec.v, keyLen*8, list)
	default:
		keyLen := 16
		if spec.v == 5 {
			keyLen = 32
		}
		doc.d = &decrypter{key: sum[:keyLen]}
		doc.dict = fmt.Sprintf("/Filter /Adobe.PubSec /SubFilter /adbe.pkcs7.s5 /V %d /CF << /DefaultCryptFilter << /CFM /%s /Recipients [%s] /EncryptMetadata %v >> >> /StmF /DefaultCryptFilter /StrF /DefaultCryptFilter",
			spec.v, spec.cfm, list, spec.encryptMetadata)
	}
	return doc
}

func TestPublicKeyHandler(t *testing.T) {
	keys := rsaKeys(t)
	mine, theirs := keys[0], keys[1]
	for _, c := range []struct {
		name     string
		spec     pubsecSpec
		stm, str cipherKind
	}{
		{"s3, RC4 of 40 bits", pubsecSpec{sub: "s3", v: 1}, cipherRC4, cipherRC4},
		{"s4, RC4 of 128 bits", pubsecSpec{sub: "s4", v: 2}, cipherRC4, cipherRC4},
		{"s5, RC4 in a crypt filter", pubsecSpec{sub: "s5", v: 4, cfm: "V2", encryptMetadata: true}, cipherRC4, cipherRC4},
		{"s5, AES-128", pubsecSpec{sub: "s5", v: 4, cfm: "AESV2", encryptMetadata: true}, cipherAESV2, cipherAESV2},
		{"s5, AES-128, metadata in the clear", pubsecSpec{sub: "s5", v: 4, cfm: "AESV2"}, cipherAESV2, cipherAESV2},
		{"s5, AES-256", pubsecSpec{sub: "s5", v: 5, cfm: "AESV3", encryptMetadata: true}, cipherAESV3, cipherAESV3},
		{"s5, AES-256, metadata in the clear", pubsecSpec{sub: "s5", v: 5, cfm: "AESV3"}, cipherAESV3, cipherAESV3},
		{"an envelope with OAEP", pubsecSpec{sub: "s5", v: 4, cfm: "AESV2", encryptMetadata: true, envelope: envelopeSpec{oaep: true}}, cipherAESV2, cipherAESV2},
		{"an envelope in triple DES", pubsecSpec{sub: "s5", v: 4, cfm: "AESV2", encryptMetadata: true, envelope: envelopeSpec{content: "3des"}}, cipherAESV2, cipherAESV2},
		{"an envelope in AES-256", pubsecSpec{sub: "s5", v: 5, cfm: "AESV3", encryptMetadata: true, envelope: envelopeSpec{content: "aes256"}}, cipherAESV3, cipherAESV3},
	} {
		t.Run(c.name, func(t *testing.T) {
			// Two recipients, and the key for the second.
			c.spec.recipients = []*rsa.PublicKey{&theirs.PublicKey, &mine.PublicKey}
			doc := pubsecDoc(t, c.spec)
			doc.keys = []crypto.Decrypter{mine}
			f := doc.build(t, c.stm, c.str, nil)
			if got := allContent(t, f); got != "the content stream\f" {
				t.Errorf("content %q", got)
			}
			if got := f.Resolve(f.Info().Get("Title")); string(got.Data) != "the title" {
				t.Errorf("title %q", got.Data)
			}
			if !f.IsEncrypted() {
				t.Error("not reported as encrypted")
			}
		})
	}

	t.Run("no key, a key that does not fit", func(t *testing.T) {
		doc := pubsecDoc(t, pubsecSpec{sub: "s5", v: 4, cfm: "AESV2", encryptMetadata: true, recipients: []*rsa.PublicKey{&theirs.PublicKey}})
		data := doc.layout(cipherAESV2, cipherAESV2, nil)
		for _, keys := range [][]crypto.Decrypter{nil, {mine}} {
			if _, err := OpenKeys(data, Limits{}, "", keys); !errors.Is(err, gofastcore.ErrKeyRequired) {
				t.Errorf("%d keys: %v", len(keys), err)
			}
		}
		// A password is no key.
		if _, err := OpenKeys(data, Limits{}, "secret", nil); !errors.Is(err, gofastcore.ErrKeyRequired) {
			t.Errorf("with a password: %v", err)
		}
	})

	t.Run("an envelope in RC2", func(t *testing.T) {
		doc := pubsecDoc(t, pubsecSpec{sub: "s4", v: 2, recipients: []*rsa.PublicKey{&mine.PublicKey}, envelope: envelopeSpec{content: "rc2"}})
		if _, err := OpenKeys(doc.layout(cipherRC4, cipherRC4, nil), Limits{}, "", []crypto.Decrypter{mine}); !errors.Is(err, gofastcore.ErrUnsupported) {
			t.Errorf("RC2: %v", err)
		}
	})
}

// Damaged envelopes, and envelopes that are not envelopes, are no key
// and no panic.
func TestDamagedEnvelopes(t *testing.T) {
	key := rsaKeys(t)[0]
	good := makeEnvelope(t, &key.PublicKey, bytes.Repeat([]byte{1}, 24), envelopeSpec{})
	if seed, err := openEnvelope(good, key); err != nil || len(seed) != 24 {
		t.Fatalf("the envelope as made: %d bytes, %v", len(seed), err)
	}
	for i := 0; i < len(good); i += 3 {
		for _, b := range []byte{0x00, 0xFF, good[i] ^ 0x80} {
			bad := append([]byte(nil), good...)
			bad[i] = b
			seed, err := openEnvelope(bad, key)
			if err == nil && len(seed) < 20 {
				t.Errorf("byte %d set to %#x: a seed of %d bytes", i, b, len(seed))
			}
		}
		if _, err := openEnvelope(good[:i], key); err == nil {
			t.Errorf("cut at %d: opened", i)
		}
	}
}

func FuzzEnvelope(f *testing.F) {
	key := rsaKeys(f)[0]
	f.Add(makeEnvelope(f, &key.PublicKey, bytes.Repeat([]byte{1}, 24), envelopeSpec{}))
	f.Add(makeEnvelope(f, &key.PublicKey, bytes.Repeat([]byte{2}, 24), envelopeSpec{oaep: true, content: "3des"}))
	f.Fuzz(func(t *testing.T, der []byte) {
		if seed, err := openEnvelope(der, key); err == nil && len(seed) < 20 {
			t.Fatalf("a seed of %d bytes", len(seed))
		}
	})
}

// OpenSSL, where it is installed: it opens the envelopes made here, and
// this package opens the envelopes it makes.
func TestEnvelopesAgainstOpenSSL(t *testing.T) {
	if _, _, err := pdftest.Run(10*time.Second, "openssl", "version"); err != nil {
		t.Skipf("openssl: %v", err)
	}
	key := rsaKeys(t)[0]
	dir := t.TempDir()
	keyDER, _ := x509.MarshalPKCS8PrivateKey(key)
	keyPath := filepath.Join(dir, "key.pem")
	os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), 0o600)
	template := &x509.Certificate{SerialNumber: big.NewInt(7), Subject: pkix.Name{CommonName: "a recipient"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour)}
	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certPath := filepath.Join(dir, "cert.pem")
	os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER}), 0o644)
	seed := []byte("twenty bytes of seed" + "\xff\xff\xff\xfc")
	seedPath := filepath.Join(dir, "seed")
	os.WriteFile(seedPath, seed, 0o644)

	t.Run("made here, opened by OpenSSL", func(t *testing.T) {
		for _, spec := range []envelopeSpec{{}, {content: "aes256"}, {content: "3des"}, {oaep: true}} {
			path := filepath.Join(dir, "mine.der")
			os.WriteFile(path, makeEnvelope(t, &key.PublicKey, seed, spec), 0o644)
			out, _, err := pdftest.Run(30*time.Second, "openssl", "cms", "-decrypt", "-binary", "-inform", "DER", "-in", path, "-inkey", keyPath)
			if err != nil || out != string(seed) {
				t.Errorf("%+v: OpenSSL gives %q, %v", spec, out, err)
			}
		}
	})

	t.Run("made by OpenSSL, opened here", func(t *testing.T) {
		for _, args := range [][]string{
			{"-aes128"}, {"-aes256"}, {"-des3"},
			{"-aes128", "-keyopt", "rsa_padding_mode:oaep"},
			{"-aes256", "-keyopt", "rsa_padding_mode:oaep", "-keyopt", "rsa_oaep_md:sha256", "-keyopt", "rsa_mgf1_md:sha256"},
		} {
			path := filepath.Join(dir, "theirs.der")
			cmd := append([]string{"cms", "-encrypt", "-binary", "-outform", "DER", "-in", seedPath, "-out", path, args[0], "-recip", certPath}, args[1:]...)
			if _, _, err := pdftest.Run(30*time.Second, "openssl", cmd...); err != nil {
				t.Fatalf("%v: %v", args, err)
			}
			der, _ := os.ReadFile(path)
			got, err := openEnvelope(der, key)
			if err != nil || !bytes.Equal(got, seed) {
				t.Errorf("%v: %q, %v", args, got, err)
			}
		}
	})
}
