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
	"encoding/asn1"
	"errors"
	"fmt"

	gofastcore "github.com/AOShei/go-fast-core"
	"github.com/AOShei/go-fast-pdf/internal/syntax"
)

// The public-key security handler (ISO 32000-1, 7.6.4; ISO 32000-2,
// 7.6.5): a file encrypted for one or more certificates.
//
// For each recipient the file holds a CMS envelope (RFC 5652) that only
// the recipient's private key opens. Inside it are a 20-byte seed and 4
// bytes of permissions. The file key is a hash of the seed and of every
// envelope, in the order of the file; after that the file is decrypted
// as one encrypted with a password is, with the same ciphers.
//
// Only envelopes whose content key is encrypted with RSA (key transport)
// are opened: crypto.Decrypter decrypts with RSA, and that is what
// Acrobat and the programs that read such files write. The envelope's
// content may be encrypted with AES or with triple DES. RC2, which old
// versions of Acrobat used, is not in the standard library and is
// refused as unsupported.
//
// A key does not say which recipient it is for, since a crypto.Decrypter
// has no certificate. Every key is tried on every envelope whose RSA
// modulus is its size. What it gives is taken only when both layers of
// padding are right and the content has the length of a seed: a wrong
// key is not taken for a right one.

// Limits on what a file can make this handler do.
const (
	// maxRecipients is the number of envelopes that are tried.
	maxRecipients = 1024
	// maxEnvelopeBytes bounds one envelope.
	maxEnvelopeBytes = 1 << 20
)

var (
	oidEnvelopedData = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 7, 3}
	oidRSA           = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 1, 1}
	oidRSAOAEP       = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 1, 7}
	oidMGF1          = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 1, 8}
	oidAES128CBC     = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 1, 2}
	oidAES192CBC     = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 1, 22}
	oidAES256CBC     = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 1, 42}
	oidDESEDE3CBC    = asn1.ObjectIdentifier{1, 2, 840, 113549, 3, 7}
	oidRC2CBC        = asn1.ObjectIdentifier{1, 2, 840, 113549, 3, 2}
	oidSHA1          = asn1.ObjectIdentifier{1, 3, 14, 3, 2, 26}
	oidSHA256        = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 2, 1}
	oidSHA384        = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 2, 2}
	oidSHA512        = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 2, 3}
)

// errNotOpened is what an envelope that a key does not open gives.
var errNotOpened = errors.New("the key does not open the envelope")

func (f *File) setupPublicKey(dict syntax.Value) error {
	v, _ := f.Resolve(dict.Get("V")).Integer()
	sub := f.Resolve(dict.Get("SubFilter"))
	d := &decrypter{encryptMetadata: true}
	if em := f.Resolve(dict.Get("EncryptMetadata")); em.Kind == syntax.Bool {
		d.encryptMetadata = em.N == 1
	}

	// The envelopes, and how long a key the file uses.
	var recipients [][]byte
	keyLen := 0
	sha := sha1.New
	switch {
	case (sub.IsName("adbe.pkcs7.s3") || sub.IsName("adbe.pkcs7.s4")) && (v == 1 || v == 2):
		// All strings and streams with RC4; the envelopes in the
		// encryption dictionary.
		d.stm, d.str = cipherRC4, cipherRC4
		recipients = f.recipients(dict.Get("Recipients"))
		keyLen = 5
		if bits, ok := f.Resolve(dict.Get("Length")).Integer(); ok && v == 2 {
			if bits < 40 || bits > 128 || bits%8 != 0 {
				return fmt.Errorf("%w: encryption key length %d", gofastcore.ErrMalformed, bits)
			}
			keyLen = int(bits / 8)
		}
	case sub.IsName("adbe.pkcs7.s5") && (v == 4 || v == 5):
		// Crypt filters; the envelopes in the filter that streams use.
		// A file whose strings use a filter with other recipients would
		// need two keys, and is not read.
		if err := f.cryptFilters(dict, v, d); err != nil {
			return err
		}
		cf := f.Resolve(dict.Get("CF"))
		filterOf := func(key string) syntax.Value {
			if name := f.Resolve(dict.Get(key)); name.Kind == syntax.Name {
				return f.Resolve(cf.Get(name.NameString()))
			}
			return syntax.Value{}
		}
		stm, str := filterOf("StmF"), filterOf("StrF")
		filter := stm
		if !filter.IsDict() {
			filter = str
		}
		if !filter.IsDict() {
			return fmt.Errorf("%w: the file is encrypted for a certificate and names no crypt filter", gofastcore.ErrMalformed)
		}
		recipients = f.recipients(filter.Get("Recipients"))
		if str.IsDict() && stm.IsDict() {
			other := f.recipients(str.Get("Recipients"))
			if len(other) != len(recipients) || !bytes.Equal(bytes.Join(other, nil), bytes.Join(recipients, nil)) {
				return fmt.Errorf("%w: strings and streams are encrypted for different recipients", gofastcore.ErrUnsupported)
			}
		}
		if em := f.Resolve(filter.Get("EncryptMetadata")); em.Kind == syntax.Bool {
			d.encryptMetadata = em.N == 1
		}
		switch {
		case v == 5:
			keyLen, sha = 32, sha256.New
		case d.stm == cipherAESV2 || d.str == cipherAESV2:
			keyLen = 16
		default:
			// RC4: the filter's /Length, in bits, which some programs
			// write in bytes.
			keyLen = 16
			if n, ok := f.Resolve(filter.Get("Length")).Integer(); ok {
				if n >= 40 {
					n /= 8
				}
				if n < 5 || n > 16 {
					return fmt.Errorf("%w: crypt filter key length %d", gofastcore.ErrMalformed, n)
				}
				keyLen = int(n)
			}
		}
	default:
		return fmt.Errorf("%w: the public-key security handler /%s with encryption version %d", gofastcore.ErrUnsupported, sub.NameString(), v)
	}
	if len(recipients) == 0 {
		return fmt.Errorf("%w: the file is encrypted for a certificate and names no recipient", gofastcore.ErrMalformed)
	}

	seed, err := openEnvelopes(recipients, f.keys)
	if err != nil {
		return err
	}
	// The file key (ISO 32000-2, 7.6.5.3): the hash of the seed, of
	// every envelope, and, where the metadata is left in the clear, of
	// four bytes 0xFF.
	h := sha()
	h.Write(seed[:20])
	for _, r := range recipients {
		h.Write(r)
	}
	if !d.encryptMetadata && v >= 4 {
		h.Write([]byte{0xFF, 0xFF, 0xFF, 0xFF})
	}
	d.key = h.Sum(nil)[:keyLen]
	f.enc = d
	return nil
}

// recipients reads the envelopes of /Recipients: an array of strings,
// or one string.
func (f *File) recipients(v syntax.Value) [][]byte {
	v = f.Resolve(v)
	list := []syntax.Value{v}
	if v.Kind == syntax.Array {
		list = v.List
	}
	var out [][]byte
	for _, r := range list {
		if r = f.Resolve(r); r.Kind == syntax.String && len(r.Data) > 0 && len(r.Data) <= maxEnvelopeBytes && len(out) < maxRecipients {
			out = append(out, r.Data)
		}
	}
	return out
}

// openEnvelopes finds an envelope that one of the keys opens and returns
// what is in it: the seed and the permissions.
func openEnvelopes(recipients [][]byte, keys []crypto.Decrypter) ([]byte, error) {
	if len(keys) == 0 {
		return nil, fmt.Errorf("%w: the file is encrypted for a certificate", gofastcore.ErrKeyRequired)
	}
	var unsupported error
	for _, r := range recipients {
		for _, key := range keys {
			seed, err := openEnvelope(r, key)
			switch {
			case err == nil:
				return seed, nil
			case errors.Is(err, gofastcore.ErrUnsupported):
				unsupported = err
			}
		}
	}
	if unsupported != nil {
		return nil, unsupported
	}
	return nil, fmt.Errorf("%w: none of the %d keys opens any of the %d envelopes of the file", gofastcore.ErrKeyRequired, len(keys), len(recipients))
}

// openEnvelope opens one envelope (a CMS ContentInfo of EnvelopedData)
// with a key. It returns errNotOpened when the key is not for it.
func openEnvelope(der []byte, key crypto.Decrypter) ([]byte, error) {
	pub, ok := key.Public().(*rsa.PublicKey)
	if !ok {
		return nil, errNotOpened
	}
	// ContentInfo: the type, and [0] the content.
	info, err := sequence(der)
	if err != nil || len(info) < 2 {
		return nil, errNotOpened
	}
	var kind asn1.ObjectIdentifier
	if _, err := asn1.Unmarshal(info[0].FullBytes, &kind); err != nil || !kind.Equal(oidEnvelopedData) {
		return nil, errNotOpened
	}
	if info[1].Class != asn1.ClassContextSpecific || info[1].Tag != 0 {
		return nil, errNotOpened
	}
	// EnvelopedData: version, [0] originator info, the recipient infos,
	// the encrypted content, [1] attributes.
	env, err := sequence(info[1].Bytes)
	if err != nil || len(env) < 3 {
		return nil, errNotOpened
	}
	at := 1
	if env[at].Class == asn1.ClassContextSpecific && env[at].Tag == 0 {
		at++
	}
	if at+1 >= len(env) || env[at].Tag != asn1.TagSet {
		return nil, errNotOpened
	}
	infos, err := elements(env[at].Bytes)
	if err != nil {
		return nil, errNotOpened
	}
	content := env[at+1]

	for _, ri := range infos {
		// Key transport is a SEQUENCE; the other kinds of recipient are
		// context-tagged and need what a crypto.Decrypter cannot do.
		if ri.Class != asn1.ClassUniversal || ri.Tag != asn1.TagSequence {
			continue
		}
		fields, err := elements(ri.Bytes)
		if err != nil || len(fields) != 4 {
			continue
		}
		alg, err := sequence(fields[2].FullBytes)
		if err != nil || len(alg) == 0 {
			continue
		}
		var algOID asn1.ObjectIdentifier
		if _, err := asn1.Unmarshal(alg[0].FullBytes, &algOID); err != nil {
			continue
		}
		var encryptedKey []byte
		if _, err := asn1.Unmarshal(fields[3].FullBytes, &encryptedKey); err != nil || len(encryptedKey) != pub.Size() {
			continue
		}
		var opts crypto.DecrypterOpts
		switch {
		case algOID.Equal(oidRSA):
			opts = &rsa.PKCS1v15DecryptOptions{}
		case algOID.Equal(oidRSAOAEP):
			var params asn1.RawValue
			if len(alg) > 1 {
				params = alg[1]
			}
			o, err := oaepOptions(params)
			if err != nil {
				return nil, err
			}
			opts = o
		default:
			continue
		}
		cek, err := key.Decrypt(rand.Reader, encryptedKey, opts)
		if err != nil {
			continue
		}
		seed, err := decryptContent(content, cek)
		if errors.Is(err, gofastcore.ErrUnsupported) {
			return nil, err
		}
		if err == nil && len(seed) >= 20 {
			return seed, nil
		}
	}
	return nil, errNotOpened
}

// decryptContent decrypts the content of an envelope (EncryptedContent-
// Info) with its content key.
func decryptContent(eci asn1.RawValue, cek []byte) ([]byte, error) {
	fields, err := elements(eci.Bytes)
	if err != nil || len(fields) < 3 {
		return nil, errNotOpened
	}
	alg, err := sequence(fields[1].FullBytes)
	if err != nil || len(alg) < 2 {
		return nil, errNotOpened
	}
	var oid asn1.ObjectIdentifier
	var iv []byte
	if _, err := asn1.Unmarshal(alg[0].FullBytes, &oid); err != nil {
		return nil, errNotOpened
	}
	// [0] the encrypted content, primitive, or constructed of pieces.
	enc := fields[2]
	if enc.Class != asn1.ClassContextSpecific || enc.Tag != 0 {
		return nil, errNotOpened
	}
	data := enc.Bytes
	if enc.IsCompound {
		pieces, err := elements(enc.Bytes)
		if err != nil {
			return nil, errNotOpened
		}
		data = nil
		for _, p := range pieces {
			data = append(data, p.Bytes...)
		}
	}

	var block cipher.Block
	switch {
	case oid.Equal(oidAES128CBC), oid.Equal(oidAES192CBC), oid.Equal(oidAES256CBC):
		block, err = aes.NewCipher(cek)
	case oid.Equal(oidDESEDE3CBC):
		block, err = des.NewTripleDESCipher(cek)
	case oid.Equal(oidRC2CBC):
		return nil, fmt.Errorf("%w: the envelope is encrypted with RC2", gofastcore.ErrUnsupported)
	default:
		return nil, fmt.Errorf("%w: the envelope is encrypted with the algorithm %s", gofastcore.ErrUnsupported, oid)
	}
	if err != nil {
		return nil, errNotOpened // a content key of the wrong size: not this key's envelope
	}
	if _, err := asn1.Unmarshal(alg[1].FullBytes, &iv); err != nil || len(iv) != block.BlockSize() {
		return nil, errNotOpened
	}
	n := block.BlockSize()
	if len(data) == 0 || len(data)%n != 0 {
		return nil, errNotOpened
	}
	out := make([]byte, len(data))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(out, data)
	pad := int(out[len(out)-1])
	if pad < 1 || pad > n || !bytes.Equal(out[len(out)-pad:], bytes.Repeat(out[len(out)-1:], pad)) {
		return nil, errNotOpened
	}
	return out[:len(out)-pad], nil
}

// oaepOptions reads the parameters of RSAES-OAEP (RFC 8017, A.2.1):
// [0] the hash, [1] the mask generation function, [2] the label. The
// defaults are SHA-1, MGF1 with SHA-1, and an empty label.
func oaepOptions(params asn1.RawValue) (*rsa.OAEPOptions, error) {
	o := &rsa.OAEPOptions{Hash: crypto.SHA1, MGFHash: crypto.SHA1}
	if params.Tag != asn1.TagSequence || params.Class != asn1.ClassUniversal {
		return o, nil // absent, or NULL
	}
	fields, err := elements(params.Bytes)
	if err != nil {
		return nil, fmt.Errorf("%w: RSA-OAEP parameters", gofastcore.ErrMalformed)
	}
	hashOf := func(der []byte) (crypto.Hash, error) {
		alg, err := sequence(der)
		if err != nil || len(alg) == 0 {
			return 0, err
		}
		var oid asn1.ObjectIdentifier
		if _, err := asn1.Unmarshal(alg[0].FullBytes, &oid); err != nil {
			return 0, err
		}
		switch {
		case oid.Equal(oidSHA1):
			return crypto.SHA1, nil
		case oid.Equal(oidSHA256):
			return crypto.SHA256, nil
		case oid.Equal(oidSHA384):
			return crypto.SHA384, nil
		case oid.Equal(oidSHA512):
			return crypto.SHA512, nil
		case oid.Equal(oidMGF1) && len(alg) > 1:
			// MGF1 with a hash as its parameter.
			var inner asn1.ObjectIdentifier
			if inner2, err := sequence(alg[1].FullBytes); err == nil && len(inner2) > 0 {
				if _, err := asn1.Unmarshal(inner2[0].FullBytes, &inner); err == nil {
					switch {
					case inner.Equal(oidSHA1):
						return crypto.SHA1, nil
					case inner.Equal(oidSHA256):
						return crypto.SHA256, nil
					case inner.Equal(oidSHA384):
						return crypto.SHA384, nil
					case inner.Equal(oidSHA512):
						return crypto.SHA512, nil
					}
				}
			}
		}
		return 0, fmt.Errorf("%w: RSA-OAEP with the algorithm %s", gofastcore.ErrUnsupported, oid)
	}
	for _, field := range fields {
		if field.Class != asn1.ClassContextSpecific {
			continue
		}
		switch field.Tag {
		case 0:
			h, err := hashOf(field.Bytes)
			if err != nil {
				return nil, err
			}
			o.Hash = h
		case 1:
			h, err := hashOf(field.Bytes)
			if err != nil {
				return nil, err
			}
			o.MGFHash = h
		case 2:
			// pSpecified with a label.
			alg, err := sequence(field.Bytes)
			if err == nil && len(alg) > 1 {
				var label []byte
				if _, err := asn1.Unmarshal(alg[1].FullBytes, &label); err == nil {
					o.Label = label
				}
			}
		}
	}
	return o, nil
}

// sequence reads one DER SEQUENCE and returns its elements.
func sequence(der []byte) ([]asn1.RawValue, error) {
	var seq asn1.RawValue
	rest, err := asn1.Unmarshal(der, &seq)
	if err != nil {
		return nil, err
	}
	if len(rest) > 0 && len(bytes.TrimRight(rest, "\x00")) > 0 || seq.Tag != asn1.TagSequence || !seq.IsCompound {
		return nil, errNotOpened
	}
	return elements(seq.Bytes)
}

// elements reads the DER elements that follow one another in der.
func elements(der []byte) ([]asn1.RawValue, error) {
	var out []asn1.RawValue
	for len(der) > 0 && len(out) < 4096 {
		var v asn1.RawValue
		rest, err := asn1.Unmarshal(der, &v)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
		der = rest
	}
	return out, nil
}
