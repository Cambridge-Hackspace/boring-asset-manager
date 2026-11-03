package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

// ErrNoSecretKey is returned when sealing or opening without the
// configuration's [auth] secret_key.
var ErrNoSecretKey = errors.New("the configuration has no [auth] secret_key")

// ErrUnsealable is returned for a sealed secret this key cannot open: made
// with another key, as after restoring a backup into another instance, or
// changed.
var ErrUnsealable = errors.New("sealed with another secret key, or changed")

// sealedPrefix marks the format of a sealed secret, should it change.
const sealedPrefix = "v1:"

// Sealer encrypts secrets kept in the database, such as second factors', with
// the configuration's key, so that a copy of the database alone cannot use
// them (docs/access.md). The zero Sealer has no key, and refuses.
type Sealer struct {
	aead cipher.AEAD
}

// NewSealer makes a Sealer from a 32-byte key; with no key, the Sealer
// refuses.
func NewSealer(key []byte) (*Sealer, error) {
	if len(key) == 0 {
		return &Sealer{}, nil
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("a secret key is 32 bytes, not %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Sealer{aead: aead}, nil
}

// HasKey reports whether the Sealer can seal and open.
func (s *Sealer) HasKey() bool { return s != nil && s.aead != nil }

// Seal encrypts plaintext, bound to what it is for (such as a factor's ID),
// so that one sealed secret cannot be copied over another.
func (s *Sealer) Seal(plaintext []byte, purpose string) (string, error) {
	if !s.HasKey() {
		return "", ErrNoSecretKey
	}
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("reading random bytes for a nonce: %w", err)
	}
	sealed := s.aead.Seal(nonce, nonce, plaintext, []byte(purpose))
	return sealedPrefix + base64.RawStdEncoding.EncodeToString(sealed), nil
}

// Open decrypts what Seal made for the same purpose.
func (s *Sealer) Open(sealed, purpose string) ([]byte, error) {
	if !s.HasKey() {
		return nil, ErrNoSecretKey
	}
	raw, ok := strings.CutPrefix(sealed, sealedPrefix)
	if !ok {
		return nil, ErrUnsealable
	}
	b, err := base64.RawStdEncoding.DecodeString(raw)
	if err != nil || len(b) < s.aead.NonceSize() {
		return nil, ErrUnsealable
	}
	plaintext, err := s.aead.Open(nil, b[:s.aead.NonceSize()], b[s.aead.NonceSize():], []byte(purpose))
	if err != nil {
		return nil, ErrUnsealable
	}
	return plaintext, nil
}

// NewSecretKey makes a key for [auth] secret_key, as base64.
func NewSecretKey() (string, error) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return "", fmt.Errorf("reading random bytes for a key: %w", err)
	}
	return base64.StdEncoding.EncodeToString(key), nil
}
