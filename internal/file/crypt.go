package file

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"crypto/rc4"
	"errors"
	"fmt"

	gofastcore "github.com/AOShei/go-fast-core"
	"github.com/AOShei/go-fast-pdf/internal/syntax"
)

// Encryption as v0.1.1 supported it, on the new engine: the standard
// security handler with RC4 or AES-128, opened with the empty user
// password. Phase 3 of the implementation plan replaces this file:
// the key is not yet checked against /U, there is no password
// parameter, and AES-256 is refused.

// Ciphers a crypt filter can select.
const (
	cipherNone = iota
	cipherRC4
	cipherAES
)

type decrypter struct {
	key       []byte
	stmCipher int
	strCipher int
}

var padding = []byte{
	0x28, 0xBF, 0x4E, 0x5E, 0x4E, 0x75, 0x8A, 0x41, 0x64, 0x00, 0x4E, 0x56, 0xFF, 0xFA, 0x01, 0x08,
	0x2E, 0x2E, 0x00, 0xB6, 0xD0, 0x68, 0x3E, 0x80, 0x2F, 0x0C, 0xA9, 0xFE, 0x64, 0x53, 0x69, 0x7A,
}

func (f *File) setupEncryption() error {
	ref := f.trailer.Get("Encrypt")
	if ref.IsNull() {
		return nil
	}
	f.encObj = RefNumber(ref)
	if f.encObj == 0 {
		f.encObj = -1
	}
	dict := f.Resolve(ref)
	if !dict.IsDict() {
		return fmt.Errorf("%w: the encryption dictionary is a %s", gofastcore.ErrMalformed, dict.Kind)
	}
	if filter := f.Resolve(dict.Get("Filter")); !filter.IsName("Standard") {
		return fmt.Errorf("%w: security handler /%s", gofastcore.ErrUnsupported, filter.NameString())
	}
	v, _ := f.Resolve(dict.Get("V")).Integer()
	r, _ := f.Resolve(dict.Get("R")).Integer()
	o, u := f.Resolve(dict.Get("O")), f.Resolve(dict.Get("U"))
	p, okP := f.Resolve(dict.Get("P")).Integer()
	if o.Kind != syntax.String || u.Kind != syntax.String || !okP {
		return fmt.Errorf("%w: the encryption dictionary lacks /O, /U or /P", gofastcore.ErrMalformed)
	}

	d := &decrypter{}
	switch v {
	case 1, 2:
		d.stmCipher, d.strCipher = cipherRC4, cipherRC4
	case 4:
		// The crypt filters named by /StmF and /StrF say which cipher.
		cf := f.Resolve(dict.Get("CF"))
		pick := func(key string) int {
			name := f.Resolve(dict.Get(key))
			if name.Kind != syntax.Name || name.IsName("Identity") {
				return cipherNone
			}
			method := f.Resolve(f.Resolve(cf.Get(name.NameString())).Get("CFM"))
			switch {
			case method.IsName("V2"):
				return cipherRC4
			case method.IsName("AESV2"):
				return cipherAES
			}
			return cipherNone
		}
		d.stmCipher, d.strCipher = pick("StmF"), pick("StrF")
	default:
		return fmt.Errorf("%w: encryption version %d", gofastcore.ErrUnsupported, v)
	}

	bits, ok := f.Resolve(dict.Get("Length")).Integer()
	if !ok {
		bits = 128
		if r == 2 {
			bits = 40
		}
	}
	if bits < 40 || bits > 128 || bits%8 != 0 {
		return fmt.Errorf("%w: encryption key length %d", gofastcore.ErrMalformed, bits)
	}
	encryptMetadata := true
	if em := f.Resolve(dict.Get("EncryptMetadata")); em.Kind == syntax.Bool {
		encryptMetadata = em.N == 1
	}
	var id []byte
	if ids := f.Resolve(f.trailer.Get("ID")).Elems(); len(ids) > 0 {
		id = f.Resolve(ids[0]).Data
	}

	// Algorithm 2 with the empty user password. /P is a 32-bit pattern
	// that some producers write as an unsigned number.
	perm := uint32(p)
	h := md5.New()
	h.Write(padding)
	h.Write(o.Data)
	h.Write([]byte{byte(perm), byte(perm >> 8), byte(perm >> 16), byte(perm >> 24)})
	h.Write(id)
	if r >= 4 && !encryptMetadata {
		h.Write([]byte{0xFF, 0xFF, 0xFF, 0xFF})
	}
	digest := h.Sum(nil)
	n := int(bits / 8)
	if r >= 3 {
		for range 50 {
			sum := md5.Sum(digest[:n])
			digest = sum[:]
		}
	}
	d.key = digest[:n]
	f.enc = d
	return nil
}

// objectKey derives the key for one object (Algorithm 1).
func (d *decrypter) objectKey(obj uint32, gen uint16, aesSalt bool) []byte {
	buf := make([]byte, 0, len(d.key)+9)
	buf = append(buf, d.key...)
	buf = append(buf, byte(obj), byte(obj>>8), byte(obj>>16), byte(gen), byte(gen>>8))
	if aesSalt {
		buf = append(buf, 0x73, 0x41, 0x6C, 0x54)
	}
	sum := md5.Sum(buf)
	return sum[:min(len(d.key)+5, 16)]
}

func (d *decrypter) decrypt(data []byte, obj uint32, gen uint16, which int) ([]byte, error) {
	if len(data) == 0 {
		return data, nil
	}
	switch which {
	case cipherRC4:
		c, err := rc4.NewCipher(d.objectKey(obj, gen, false))
		if err != nil {
			return nil, err
		}
		out := make([]byte, len(data))
		c.XORKeyStream(out, data)
		return out, nil
	case cipherAES:
		// 16 bytes of initialisation vector, then whole blocks.
		if len(data) < 16 || len(data)%16 != 0 {
			return nil, fmt.Errorf("%w: AES data of %d bytes is not a whole number of blocks", gofastcore.ErrMalformed, len(data))
		}
		block, err := aes.NewCipher(d.objectKey(obj, gen, true))
		if err != nil {
			return nil, err
		}
		out := make([]byte, len(data)-16)
		cipher.NewCBCDecrypter(block, data[:16]).CryptBlocks(out, data[16:])
		// Remove the padding if it is well formed.
		if n := len(out); n > 0 {
			pad := int(out[n-1])
			if pad >= 1 && pad <= 16 && pad <= n {
				out = out[:n-pad]
			}
		}
		return out, nil
	}
	return data, nil
}

func (d *decrypter) decryptStream(data []byte, obj uint32, gen uint16) ([]byte, error) {
	out, err := d.decrypt(data, obj, gen, d.stmCipher)
	if err != nil {
		return nil, errors.Join(errors.New("cannot decrypt stream"), err)
	}
	return out, nil
}

// decryptStrings returns v with every string in it decrypted. Strings
// that cannot be decrypted are left as they are.
func (d *decrypter) decryptStrings(v syntax.Value, obj uint32, gen uint16) syntax.Value {
	if d.strCipher == cipherNone {
		return v
	}
	switch v.Kind {
	case syntax.String:
		if out, err := d.decrypt(v.Data, obj, gen, d.strCipher); err == nil {
			v.Data = out
		}
	case syntax.Array, syntax.Dict, syntax.Stream:
		// The parsed value is not shared yet, so its list can be
		// changed in place.
		for i := range v.List {
			if k := v.List[i].Kind; k == syntax.String || k == syntax.Array || k == syntax.Dict {
				v.List[i] = d.decryptStrings(v.List[i], obj, gen)
			}
		}
	}
	return v
}
