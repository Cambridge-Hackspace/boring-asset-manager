package idp

import (
	"crypto"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rsa"
	_ "crypto/sha1" // OAEP's SHA-1, the default of rsa-oaep-mgf1p
	_ "crypto/sha256"
	_ "crypto/sha512"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"github.com/beevik/etree"
	"github.com/russellhaering/goxmldsig/etreeutils"
)

// XML Encryption, as far as SAML's encrypted assertions use it: an AES key
// sealed to BAM!'s key with RSA-OAEP, and the assertion sealed with it.
// RSA PKCS #1 v1.5 key transport is refused, as it leaks the key to anyone
// who can see which answers fail (Bleichenbacher's attack).
const (
	xmlencNS   = "http://www.w3.org/2001/04/xmlenc#"
	xmlenc11NS = "http://www.w3.org/2009/xmlenc11#"
)

var (
	xmlencBlockCiphers = map[string]struct {
		keySize int
		gcm     bool
	}{
		xmlencNS + "aes128-cbc":   {16, false},
		xmlencNS + "aes192-cbc":   {24, false},
		xmlencNS + "aes256-cbc":   {32, false},
		xmlenc11NS + "aes128-gcm": {16, true},
		xmlenc11NS + "aes192-gcm": {24, true},
		xmlenc11NS + "aes256-gcm": {32, true},
	}
	xmlencDigests = map[string]crypto.Hash{
		"":                                       crypto.SHA1,
		"http://www.w3.org/2000/09/xmldsig#sha1": crypto.SHA1,
		xmlencNS + "sha256":                      crypto.SHA256,
		"http://www.w3.org/2001/04/xmldsig-more#sha384": crypto.SHA384,
		xmlencNS + "sha512":                             crypto.SHA512,
	}
	xmlencMGFs = map[string]crypto.Hash{
		"":                        crypto.SHA1,
		xmlenc11NS + "mgf1sha1":   crypto.SHA1,
		xmlenc11NS + "mgf1sha256": crypto.SHA256,
		xmlenc11NS + "mgf1sha384": crypto.SHA384,
		xmlenc11NS + "mgf1sha512": crypto.SHA512,
	}
)

// decryptAssertion opens an EncryptedAssertion, answering the Assertion in
// it, detached with the namespaces it was given.
func decryptAssertion(enc *etree.Element, key *rsa.PrivateKey) (*etree.Element, error) {
	data := child(enc, xmlencNS, "EncryptedData")
	if data == nil {
		return nil, errors.New("it has no EncryptedData")
	}
	// The sealed key is in the data's KeyInfo, or beside the data.
	ek := child(child(data, "http://www.w3.org/2000/09/xmldsig#", "KeyInfo"), xmlencNS, "EncryptedKey")
	if ek == nil {
		ek = child(enc, xmlencNS, "EncryptedKey")
	}
	if ek == nil {
		return nil, errors.New("it has no EncryptedKey")
	}
	aesKey, err := openKey(ek, key)
	if err != nil {
		return nil, err
	}
	method := child(data, xmlencNS, "EncryptionMethod").SelectAttrValue("Algorithm", "")
	bc, ok := xmlencBlockCiphers[method]
	if !ok {
		return nil, fmt.Errorf("it is encrypted with %q, which BAM! does not take", method)
	}
	if len(aesKey) != bc.keySize {
		return nil, errors.New("its key is the wrong size")
	}
	sealed, err := cipherValue(data)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(aesKey)
	if err != nil {
		return nil, err
	}
	var plain []byte
	if bc.gcm {
		aead, err := cipher.NewGCM(block)
		if err != nil {
			return nil, err
		}
		if len(sealed) < aead.NonceSize()+aead.Overhead() {
			return nil, errors.New("it is too short")
		}
		if plain, err = aead.Open(nil, sealed[:aead.NonceSize()], sealed[aead.NonceSize():], nil); err != nil {
			return nil, errors.New("it does not open")
		}
	} else {
		if len(sealed) < 2*aes.BlockSize || len(sealed)%aes.BlockSize != 0 {
			return nil, errors.New("it is the wrong length")
		}
		plain = make([]byte, len(sealed)-aes.BlockSize)
		cipher.NewCBCDecrypter(block, sealed[:aes.BlockSize]).CryptBlocks(plain, sealed[aes.BlockSize:])
		// XML Encryption's padding: its last byte is its length, and the
		// rest of it may be anything.
		pad := int(plain[len(plain)-1])
		if pad < 1 || pad > aes.BlockSize {
			return nil, errors.New("it does not open")
		}
		plain = plain[:len(plain)-pad]
	}
	return parseDecrypted(enc, plain)
}

// openKey opens the AES key sealed to BAM!'s key.
func openKey(ek *etree.Element, key *rsa.PrivateKey) ([]byte, error) {
	method := child(ek, xmlencNS, "EncryptionMethod")
	alg := method.SelectAttrValue("Algorithm", "")
	if alg != xmlencNS+"rsa-oaep-mgf1p" && alg != xmlenc11NS+"rsa-oaep" {
		return nil, fmt.Errorf("its key is sealed with %q, which BAM! does not take", alg)
	}
	hash, ok := xmlencDigests[child(method, "http://www.w3.org/2000/09/xmldsig#", "DigestMethod").SelectAttrValue("Algorithm", "")]
	if !ok {
		return nil, errors.New("its key is sealed with a digest BAM! does not take")
	}
	mgf := crypto.SHA1
	if alg == xmlenc11NS+"rsa-oaep" {
		if mgf, ok = xmlencMGFs[child(method, xmlenc11NS, "MGF").SelectAttrValue("Algorithm", "")]; !ok {
			return nil, errors.New("its key is sealed with a mask BAM! does not take")
		}
	}
	var label []byte
	if p := text(child(method, xmlencNS, "OAEPparams")); p != "" {
		var err error
		if label, err = base64.StdEncoding.DecodeString(p); err != nil {
			return nil, errors.New("its OAEPparams are not base64")
		}
	}
	sealed, err := cipherValue(ek)
	if err != nil {
		return nil, err
	}
	out, err := key.Decrypt(nil, sealed, &rsa.OAEPOptions{Hash: hash, MGFHash: mgf, Label: label})
	if err != nil {
		return nil, errors.New("its key does not open with this provider's key")
	}
	return out, nil
}

func cipherValue(el *etree.Element) ([]byte, error) {
	v := child(child(el, xmlencNS, "CipherData"), xmlencNS, "CipherValue")
	if v == nil {
		return nil, errors.New("it has no CipherValue")
	}
	out, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(v.Text()), ""))
	if err != nil {
		return nil, errors.New("its CipherValue is not base64")
	}
	return out, nil
}

// parseDecrypted reads the opened assertion. It may use prefixes declared
// around the EncryptedAssertion, so it is read inside an element declaring
// them, then detached.
func parseDecrypted(enc *etree.Element, plain []byte) (*etree.Element, error) {
	ctx, err := etreeutils.NSBuildParentContext(enc)
	if err != nil {
		return nil, err
	}
	wrapper := etree.NewDocument()
	w := wrapper.CreateElement("wrapper")
	for prefix, ns := range ctx.Prefixes() {
		if prefix == "xml" || prefix == "xmlns" {
			continue
		}
		if prefix == "" {
			w.CreateAttr("xmlns", ns)
		} else {
			w.CreateAttr("xmlns:"+prefix, ns)
		}
	}
	head, err := wrapper.WriteToString()
	if err != nil {
		return nil, err
	}
	// The wrapper is written empty, as <wrapper .../>: open it up.
	head = strings.TrimSuffix(strings.TrimSpace(head), "/>") + ">"
	doc := etree.NewDocument()
	if err := doc.ReadFromString(head + string(plain) + "</wrapper>"); err != nil {
		return nil, fmt.Errorf("it is not XML: %w", err)
	}
	for _, t := range doc.Root().Child {
		if _, ok := t.(*etree.Directive); ok {
			return nil, errors.New("it has a DOCTYPE")
		}
	}
	found := doc.Root().ChildElements()
	if len(found) != 1 || !isSAML(found[0], samlAssertionNS, "Assertion") {
		return nil, errors.New("it does not hold one assertion")
	}
	return detach(found[0])
}
