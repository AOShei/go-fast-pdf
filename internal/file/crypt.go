package file

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"crypto/rc4"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/subtle"
	"fmt"
	"unicode/utf8"

	gofastcore "github.com/AOShei/go-fast-core"
	"github.com/AOShei/go-fast-pdf/internal/syntax"
)

// The standard security handler (ISO 32000-1, 7.6, and ISO 32000-2 for
// revision 6): RC4 with 40 to 128 bit keys, AES-128 and AES-256, with
// crypt filters.
//
// A password is always checked before anything is decrypted, so a wrong
// key can never turn into text: the file either authenticates or Open
// returns ErrPasswordRequired or ErrWrongPassword.

// cipherKind is how a string or a stream is encrypted.
type cipherKind uint8

const (
	// cipherNone: not encrypted (the Identity filter).
	cipherNone cipherKind = iota
	// cipherRC4: RC4 with a key derived per object (crypt filter V2).
	cipherRC4
	// cipherAESV2: AES-128 in CBC mode with a key derived per object.
	cipherAESV2
	// cipherAESV3: AES-256 in CBC mode with the file key itself.
	cipherAESV3
)

type decrypter struct {
	key []byte
	// stm and str are the ciphers for streams and strings by default.
	stm, str cipherKind
	// named holds the crypt filters of /CF by name, for a stream that
	// selects one through a /Crypt filter of its own.
	named map[string]cipherKind
	// encryptMetadata is false when the document's metadata stream is
	// left in the clear.
	encryptMetadata bool
}

var padding = []byte{
	0x28, 0xBF, 0x4E, 0x5E, 0x4E, 0x75, 0x8A, 0x41, 0x64, 0x00, 0x4E, 0x56, 0xFF, 0xFA, 0x01, 0x08,
	0x2E, 0x2E, 0x00, 0xB6, 0xD0, 0x68, 0x3E, 0x80, 0x2F, 0x0C, 0xA9, 0xFE, 0x64, 0x53, 0x69, 0x7A,
}

// encryptDict is the standard security handler's dictionary, read and
// checked.
type encryptDict struct {
	v, r            int64
	keyLen          int // bytes, for revisions 2 to 4
	o, u, oe, ue    []byte
	p               uint32
	id              []byte
	encryptMetadata bool
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
	switch filter := f.Resolve(dict.Get("Filter")); {
	case filter.IsName("Adobe.PubSec"):
		return f.setupPublicKey(dict)
	case !filter.IsName("Standard"):
		return fmt.Errorf("%w: security handler /%s", gofastcore.ErrUnsupported, filter.NameString())
	}

	e := encryptDict{encryptMetadata: true}
	e.v, _ = f.Resolve(dict.Get("V")).Integer()
	e.r, _ = f.Resolve(dict.Get("R")).Integer()
	p, okP := f.Resolve(dict.Get("P")).Integer()
	str := func(key string) []byte {
		if v := f.Resolve(dict.Get(key)); v.Kind == syntax.String {
			return v.Data
		}
		return nil
	}
	e.o, e.u, e.oe, e.ue = str("O"), str("U"), str("OE"), str("UE")
	if e.o == nil || e.u == nil || !okP {
		return fmt.Errorf("%w: the encryption dictionary lacks /O, /U or /P", gofastcore.ErrMalformed)
	}
	// /P is a 32-bit pattern that some producers write as an unsigned
	// number.
	e.p = uint32(p)
	if em := f.Resolve(dict.Get("EncryptMetadata")); em.Kind == syntax.Bool {
		e.encryptMetadata = em.N == 1
	}
	if ids := f.Resolve(f.trailer.Get("ID")).Elems(); len(ids) > 0 {
		e.id = f.Resolve(ids[0]).Data
	}

	d := &decrypter{encryptMetadata: e.encryptMetadata}
	switch {
	case (e.v == 1 || e.v == 2) && (e.r == 2 || e.r == 3):
		d.stm, d.str = cipherRC4, cipherRC4
	case e.v == 4 && e.r == 4, e.v == 5 && (e.r == 5 || e.r == 6):
		// The crypt filters named by /StmF and /StrF say which cipher.
		if err := f.cryptFilters(dict, e.v, d); err != nil {
			return err
		}
	default:
		return fmt.Errorf("%w: encryption version %d, revision %d", gofastcore.ErrUnsupported, e.v, e.r)
	}

	if e.r <= 4 {
		bits, ok := f.Resolve(dict.Get("Length")).Integer()
		if !ok {
			bits = 128
			if e.r == 2 {
				bits = 40
			}
		}
		if bits < 40 || bits > 128 || bits%8 != 0 {
			return fmt.Errorf("%w: encryption key length %d", gofastcore.ErrMalformed, bits)
		}
		e.keyLen = int(bits / 8)
		if e.r == 2 {
			e.keyLen = 5 // revision 2 always uses 40 bits
		}
		// /U is compared in full for revision 2 and in its first 16 bytes
		// after that. A short /O still enters the key as it is; it only
		// rules out the owner password.
		if need := map[bool]int{true: 32, false: 16}[e.r == 2]; len(e.u) < need {
			return fmt.Errorf("%w: /U is %d bytes, shorter than the %d that are compared", gofastcore.ErrMalformed, len(e.u), need)
		}
	} else if len(e.o) < 48 || len(e.u) < 48 || len(e.oe) < 32 || len(e.ue) < 32 {
		return fmt.Errorf("%w: /O, /U, /OE or /UE is too short for AES-256", gofastcore.ErrMalformed)
	}

	key, opener, err := e.authenticate(f.password)
	if err != nil {
		if f.rec.lostTrailer && e.r <= 4 && e.id == nil {
			// Up to AES-128 the key depends on the file identifier,
			// which was in the trailer. No password can open the file
			// now; asking for one would mislead.
			return fmt.Errorf("%w: the file is encrypted and the trailer that held its identifier is lost", gofastcore.ErrMalformed)
		}
		return err
	}
	d.key = key
	f.enc = d
	keyLen := e.keyLen
	if e.r >= 5 {
		keyLen = 32
	}
	f.decrypted = fmt.Sprintf("the standard security handler, revision %d, %s; opened with %s", e.r, d.ciphers(keyLen), opener)
	return nil
}

// ciphers names the ciphers of d, whose keys for RC4 are keyLen bytes
// long.
func (d *decrypter) ciphers(keyLen int) string {
	name := func(k cipherKind) string {
		switch k {
		case cipherRC4:
			return fmt.Sprintf("RC4 with a %d-bit key", 8*keyLen)
		case cipherAESV2:
			return "AES-128"
		case cipherAESV3:
			return "AES-256"
		}
		return "no cipher"
	}
	if d.stm == d.str {
		return name(d.stm)
	}
	return name(d.stm) + " for streams and " + name(d.str) + " for strings"
}

// Decrypted says how the file was decrypted: the security handler, its
// ciphers, and the password or key that opened it. "" when the file is
// not encrypted.
func (f *File) Decrypted() string { return f.decrypted }

// cryptFilters reads the crypt filters of /CF for encryption version v
// into d, and the ciphers that /StmF and /StrF choose.
func (f *File) cryptFilters(dict syntax.Value, v int64, d *decrypter) error {
	cf := f.Resolve(dict.Get("CF"))
	d.named = map[string]cipherKind{}
	for i := range cf.Entries() {
		name, filter := cf.Entry(i)
		switch method := f.Resolve(f.Resolve(filter).Get("CFM")); {
		case method.IsName("V2") && v == 4:
			d.named[string(name)] = cipherRC4
		case method.IsName("AESV2") && v == 4:
			d.named[string(name)] = cipherAESV2
		case method.IsName("AESV3") && v == 5:
			d.named[string(name)] = cipherAESV3
		case method.IsName("None"), method.IsNull():
			// Decryption left to the application: not something this
			// reader can do. Streams that use it are refused.
		default:
			return fmt.Errorf("%w: crypt filter method /%s with encryption version %d",
				gofastcore.ErrUnsupported, method.NameString(), v)
		}
	}
	pick := func(key string) (cipherKind, error) {
		name := f.Resolve(dict.Get(key))
		if name.Kind != syntax.Name || name.IsName("Identity") {
			return cipherNone, nil
		}
		kind, ok := d.named[name.NameString()]
		if !ok {
			return 0, fmt.Errorf("%w: /%s names the crypt filter /%s, which is not usable", gofastcore.ErrUnsupported, key, name.NameString())
		}
		return kind, nil
	}
	var err error
	if d.stm, err = pick("StmF"); err != nil {
		return err
	}
	d.str, err = pick("StrF")
	return err
}

// authenticate finds the file key. It tries the password as the user
// password and as the owner password, and the empty user password, which
// opens most encrypted files. opener says which of them opened it.
func (e *encryptDict) authenticate(password string) (key []byte, opener string, err error) {
	for _, pw := range passwordForms(password, e.r) {
		if key, owner := e.tryPassword(pw); key != nil {
			switch {
			case password == "" && owner:
				return key, "the empty owner password", nil
			case password == "":
				return key, "the empty user password", nil
			case owner:
				return key, "the owner password given", nil
			}
			return key, "the user password given", nil
		}
	}
	if password == "" {
		return nil, "", fmt.Errorf("%w", gofastcore.ErrPasswordRequired)
	}
	if key, owner := e.tryPassword(nil); key != nil {
		// The file needs no password after all.
		if owner {
			return key, "the empty owner password; the password given opens nothing", nil
		}
		return key, "the empty user password; the password given opens nothing", nil
	}
	return nil, "", fmt.Errorf("%w", gofastcore.ErrWrongPassword)
}

// passwordForms returns the byte strings a password may have been stored
// as. Revisions up to 4 use PDFDocEncoding, which agrees with Latin-1
// for the characters people type; some producers used UTF-8 anyway.
// Revisions 5 and 6 use UTF-8, at most 127 bytes. (They also specify a
// normalisation, SASLprep, which needs Unicode tables that are not in
// the standard library; a password that is already in composed form,
// as typed passwords are, is unaffected.)
func passwordForms(password string, r int64) [][]byte {
	raw := []byte(password)
	if r >= 5 {
		for len(raw) > 127 {
			_, size := utf8.DecodeLastRune(raw)
			raw = raw[:len(raw)-size]
		}
		return [][]byte{raw}
	}
	forms := [][]byte{}
	latin := make([]byte, 0, len(password))
	for _, c := range password {
		if c > 0xFF {
			latin = nil
			break
		}
		latin = append(latin, byte(c))
	}
	if latin != nil {
		forms = append(forms, latin)
	}
	if latin == nil || !bytes.Equal(latin, raw) {
		forms = append(forms, raw)
	}
	return forms
}

// tryPassword returns the file key if pw is the user password or the
// owner password, and owner when it is the owner password.
func (e *encryptDict) tryPassword(pw []byte) (key []byte, owner bool) {
	if e.r >= 5 {
		return e.tryAES256(pw)
	}
	if key := e.tryUser(padPassword(pw)); key != nil {
		return key, false
	}
	// As the owner password, it decrypts /O to the user password.
	if len(e.o) < 32 {
		return nil, false
	}
	key = e.tryUser(e.userFromOwner(pw))
	return key, key != nil
}

func padPassword(pw []byte) []byte {
	out := make([]byte, 32)
	n := copy(out, pw)
	copy(out[n:], padding)
	return out
}

// tryUser derives the key from a padded user password (Algorithm 2) and
// returns it if it reproduces /U (Algorithms 4 to 6).
func (e *encryptDict) tryUser(padded []byte) []byte {
	h := md5.New()
	h.Write(padded)
	h.Write(e.o[:min(len(e.o), 32)])
	h.Write([]byte{byte(e.p), byte(e.p >> 8), byte(e.p >> 16), byte(e.p >> 24)})
	h.Write(e.id)
	if e.r >= 4 && !e.encryptMetadata {
		h.Write([]byte{0xFF, 0xFF, 0xFF, 0xFF})
	}
	digest := h.Sum(nil)
	if e.r >= 3 {
		for range 50 {
			sum := md5.Sum(digest[:e.keyLen])
			digest = sum[:]
		}
	}
	key := digest[:e.keyLen]

	if e.r == 2 {
		want := make([]byte, 32)
		rc4Apply(key, want, padding)
		if subtle.ConstantTimeCompare(want, e.u[:32]) == 1 {
			return key
		}
		return nil
	}
	h = md5.New()
	h.Write(padding)
	h.Write(e.id)
	want := h.Sum(nil)
	rc4Rounds(key, want, 0, 19)
	// Only the first 16 bytes of /U are defined; the rest is arbitrary.
	if subtle.ConstantTimeCompare(want, e.u[:16]) == 1 {
		return key
	}
	return nil
}

// userFromOwner decrypts /O with a key made from the owner password,
// which gives the padded user password (Algorithm 7).
func (e *encryptDict) userFromOwner(owner []byte) []byte {
	digest := md5.Sum(padPassword(owner))
	sum := digest[:]
	if e.r >= 3 {
		for range 50 {
			d := md5.Sum(sum)
			sum = d[:]
		}
	}
	key := sum[:e.keyLen]
	user := append([]byte{}, e.o[:32]...)
	if e.r == 2 {
		rc4Apply(key, user, user)
	} else {
		rc4Rounds(key, user, 19, 0)
	}
	return user
}

func rc4Apply(key, dst, src []byte) {
	c, err := rc4.NewCipher(key)
	if err != nil {
		return // cannot happen: the key length was checked
	}
	c.XORKeyStream(dst, src)
}

// rc4Rounds applies RC4 to data twenty times, with the key XORed with
// each round number from first to last.
func rc4Rounds(key, data []byte, first, last int) {
	step := 1
	if last < first {
		step = -1
	}
	round := make([]byte, len(key))
	for i := first; ; i += step {
		for k := range key {
			round[k] = key[k] ^ byte(i)
		}
		rc4Apply(round, data, data)
		if i == last {
			return
		}
	}
}

// tryAES256 checks a password against /U and then /O of a revision 5 or
// 6 dictionary and returns the file key from /UE or /OE (Algorithm 2.A).
func (e *encryptDict) tryAES256(pw []byte) (key []byte, owner bool) {
	hash := hashR5
	if e.r == 6 {
		hash = hashR6
	}
	u := e.u[:48]
	if subtle.ConstantTimeCompare(hash(pw, u[32:40], nil), u[:32]) == 1 {
		return aes256NoIV(hash(pw, u[40:48], nil), e.ue[:32]), false
	}
	o := e.o[:48]
	if subtle.ConstantTimeCompare(hash(pw, o[32:40], u), o[:32]) == 1 {
		key = aes256NoIV(hash(pw, o[40:48], u), e.oe[:32])
		return key, key != nil
	}
	return nil, false
}

// aes256NoIV decrypts 32 bytes with AES-256 in CBC mode, a zero
// initialisation vector and no padding.
func aes256NoIV(key, data []byte) []byte {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil
	}
	out := make([]byte, len(data))
	cipher.NewCBCDecrypter(block, make([]byte, 16)).CryptBlocks(out, data)
	return out
}

// hashR5 is the hash of the deprecated revision 5: one SHA-256.
func hashR5(password, salt, udata []byte) []byte {
	h := sha256.New()
	h.Write(password)
	h.Write(salt)
	h.Write(udata)
	return h.Sum(nil)
}

// hashR6 is Algorithm 2.B of ISO 32000-2: at least 64 rounds in which
// the running hash, repeated 64 times with the password, is encrypted
// with AES-128 and hashed again with SHA-256, SHA-384 or SHA-512,
// chosen by the encrypted data itself.
func hashR6(password, salt, udata []byte) []byte {
	k := hashR5(password, salt, udata)
	var e []byte
	for i := 0; i < 64 || int(e[len(e)-1]) > i-32; i++ {
		block := make([]byte, 0, len(password)+len(k)+len(udata))
		block = append(append(append(block, password...), k...), udata...)
		k1 := bytes.Repeat(block, 64)
		c, err := aes.NewCipher(k[:16])
		if err != nil {
			return nil
		}
		e = make([]byte, len(k1))
		cipher.NewCBCEncrypter(c, k[16:32]).CryptBlocks(e, k1)
		// The first 16 bytes as a number, modulo 3. 256 is 1 modulo 3,
		// so the sum of the bytes has the same remainder.
		sum := 0
		for _, b := range e[:16] {
			sum += int(b)
		}
		switch sum % 3 {
		case 0:
			h := sha256.Sum256(e)
			k = h[:]
		case 1:
			h := sha512.Sum384(e)
			k = h[:]
		default:
			h := sha512.Sum512(e)
			k = h[:]
		}
	}
	return k[:32]
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

// decrypt returns the plaintext of data. For damaged AES data it returns
// the blocks that could be decrypted, with an error.
func (d *decrypter) decrypt(kind cipherKind, data []byte, obj uint32, gen uint16) ([]byte, error) {
	if len(data) == 0 || kind == cipherNone {
		return data, nil
	}
	if kind == cipherRC4 {
		out := make([]byte, len(data))
		rc4Apply(d.objectKey(obj, gen, false), out, data)
		return out, nil
	}
	key := d.key
	if kind == cipherAESV2 {
		key = d.objectKey(obj, gen, true)
	}
	// 16 bytes of initialisation vector, then whole blocks.
	if len(data) < 16 {
		return nil, fmt.Errorf("%w: %d bytes of AES data, less than an initialisation vector", gofastcore.ErrMalformed, len(data))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", gofastcore.ErrMalformed, err)
	}
	body := data[16:]
	whole := len(body) &^ 15
	out := make([]byte, whole)
	cipher.NewCBCDecrypter(block, data[:16]).CryptBlocks(out, body[:whole])
	if whole != len(body) {
		return out, fmt.Errorf("%w: AES data of %d bytes is not a whole number of blocks", gofastcore.ErrMalformed, len(data))
	}
	// The last block ends with 1 to 16 bytes that each hold the count.
	// Where that is not so the data is kept whole: a decoder downstream
	// copes with a few extra bytes better than with missing ones.
	if n := len(out); n > 0 {
		pad := int(out[n-1])
		if pad >= 1 && pad <= 16 && bytes.Count(out[n-pad:], out[n-1:]) == pad {
			out = out[:n-pad]
		}
	}
	return out, nil
}

// streamCipher says how a stream is encrypted: by the default for
// streams, unless the stream names a crypt filter of its own or is the
// metadata stream of a file that leaves metadata in the clear.
func (f *File) streamCipher(stm syntax.Value, depth int) (cipherKind, error) {
	d := f.enc
	if !d.encryptMetadata && stm.Get("Type").IsName("Metadata") {
		return cipherNone, nil
	}
	filters := f.resolve(stm.Get("Filter"), depth+1)
	names := []syntax.Value{filters}
	if filters.Kind == syntax.Array {
		names = filters.List
	}
	parms := f.resolve(stm.Get("DecodeParms"), depth+1)
	for i, name := range names {
		if !f.resolve(name, depth+1).IsName("Crypt") {
			continue
		}
		parm := parms
		if parms.Kind == syntax.Array {
			parm = syntax.Value{}
			if i < len(parms.List) {
				parm = f.resolve(parms.List[i], depth+1)
			}
		}
		filter := f.resolve(parm.Get("Name"), depth+1)
		if filter.IsNull() || filter.IsName("Identity") {
			return cipherNone, nil
		}
		kind, ok := d.named[filter.NameString()]
		if !ok {
			return 0, fmt.Errorf("%w: crypt filter /%s on a stream", gofastcore.ErrUnsupported, filter.NameString())
		}
		return kind, nil
	}
	return d.stm, nil
}

// decryptStrings returns v with every string in it decrypted, and the
// number of strings that could not be. Such a string becomes empty:
// ciphertext is never passed on as if it were text.
func (d *decrypter) decryptStrings(v syntax.Value, obj uint32, gen uint16) (syntax.Value, int) {
	if d.str == cipherNone {
		return v, 0
	}
	bad := 0
	switch v.Kind {
	case syntax.String:
		out, err := d.decrypt(d.str, v.Data, obj, gen)
		if err != nil {
			out, bad = nil, 1
		}
		v.Data = out
	case syntax.Array, syntax.Dict, syntax.Stream:
		// The parsed value is not shared yet, so its list can be
		// changed in place.
		for i := range v.List {
			if k := v.List[i].Kind; k == syntax.String || k == syntax.Array || k == syntax.Dict {
				var n int
				v.List[i], n = d.decryptStrings(v.List[i], obj, gen)
				bad += n
			}
		}
	}
	return v, bad
}
