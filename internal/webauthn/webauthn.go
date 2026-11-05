// Package webauthn checks passkeys and security keys (WebAuthn Level 3)
// as a second factor (docs/access.md#passkeys): a credential registered,
// then an assertion from it at sign-in. It is narrow on purpose. No
// attestation is asked for, so none is checked: a credential is trusted
// as the signed-in person registering it, not by who made the device.
package webauthn

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

var (
	ErrChallenge       = errors.New("the passkey answered another challenge")
	ErrOrigin          = errors.New("the passkey was used from another site")
	ErrRelyingParty    = errors.New("the passkey is for another site")
	ErrUserPresence    = errors.New("the passkey did not confirm someone was present")
	ErrSignature       = errors.New("the passkey's signature is not right")
	ErrCounterBackward = errors.New("the passkey's counter went backward, so it may have been copied")
)

// Authenticator data flags.
const (
	flagUP = 0x01 // user present
	flagUV = 0x04 // user verified
	flagAT = 0x40 // attested credential data follows
	flagED = 0x80 // extensions follow
)

// maxCredentialIDLength is the longest credential ID taken.
const maxCredentialIDLength = 1023

// ChallengeSize is how many random bytes a challenge has.
const ChallengeSize = 32

// NewChallenge makes a challenge for one ceremony.
func NewChallenge() ([]byte, error) {
	b := make([]byte, ChallengeSize)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	return b, nil
}

// RelyingParty is this server as passkeys know it: its ID is the host of
// base_url, which they are bound to, and its origin is where the browser
// must be using them.
type RelyingParty struct {
	ID     string
	Origin string
}

// NewRelyingParty reads the relying party from base_url.
func NewRelyingParty(baseURL string) (RelyingParty, error) {
	u, err := url.Parse(baseURL)
	if err != nil || u.Hostname() == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return RelyingParty{}, errors.New("passkeys need [server] base_url, with the scheme and host people use")
	}
	return RelyingParty{ID: strings.ToLower(u.Hostname()), Origin: u.Scheme + "://" + strings.ToLower(u.Host)}, nil
}

// Credential is a registered passkey as stored: its ID, its public key in
// COSE form, and the last value of its signature counter.
type Credential struct {
	ID        []byte
	PublicKey []byte
	SignCount uint32
}

// clientData is what the browser says it asked the authenticator to sign.
type clientData struct {
	Type        string `json:"type"`
	Challenge   string `json:"challenge"`
	Origin      string `json:"origin"`
	CrossOrigin bool   `json:"crossOrigin"`
}

func (rp RelyingParty) checkClientData(raw []byte, ceremony string, challenge []byte) error {
	var cd clientData
	if err := json.Unmarshal(raw, &cd); err != nil {
		return errors.New("bad client data")
	}
	if cd.Type != ceremony {
		return fmt.Errorf("client data is for %q, not %q", cd.Type, ceremony)
	}
	got, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(cd.Challenge, "="))
	if err != nil || subtle.ConstantTimeCompare(got, challenge) != 1 {
		return ErrChallenge
	}
	if cd.Origin != rp.Origin || cd.CrossOrigin {
		return ErrOrigin
	}
	return nil
}

// authenticatorData is the authenticator's own account of a ceremony.
type authenticatorData struct {
	rpIDHash  []byte
	flags     byte
	signCount uint32
	// With flagAT: the credential made.
	credentialID []byte
	publicKey    []byte
}

func parseAuthenticatorData(data []byte) (*authenticatorData, error) {
	if len(data) < 37 {
		return nil, errors.New("authenticator data too short")
	}
	ad := &authenticatorData{rpIDHash: data[:32], flags: data[32], signCount: binary.BigEndian.Uint32(data[33:37])}
	rest := data[37:]
	if ad.flags&flagAT != 0 {
		if len(rest) < 18 {
			return nil, errors.New("attested credential data too short")
		}
		n := int(binary.BigEndian.Uint16(rest[16:18]))
		rest = rest[18:]
		if n == 0 || n > maxCredentialIDLength || len(rest) < n {
			return nil, errors.New("bad credential ID")
		}
		ad.credentialID, rest = rest[:n], rest[n:]
		_, after, err := decodeCBOR(rest)
		if err != nil {
			return nil, fmt.Errorf("reading the public key: %w", err)
		}
		ad.publicKey, rest = rest[:len(rest)-len(after)], after
	}
	if ad.flags&flagED != 0 {
		ext, after, err := decodeCBOR(rest)
		if err != nil {
			return nil, fmt.Errorf("reading extensions: %w", err)
		}
		if _, ok := ext.(map[any]any); !ok {
			return nil, errors.New("extensions are not a map")
		}
		rest = after
	}
	if len(rest) != 0 {
		return nil, errors.New("authenticator data followed by more")
	}
	return ad, nil
}

func (rp RelyingParty) checkAuthenticatorData(ad *authenticatorData) error {
	want := sha256.Sum256([]byte(rp.ID))
	if !bytes.Equal(ad.rpIDHash, want[:]) {
		return ErrRelyingParty
	}
	if ad.flags&flagUP == 0 {
		return ErrUserPresence
	}
	return nil
}

// VerifyRegistration checks a credential made in answer to challenge, as
// navigator.credentials.create returned it, and answers it to be stored.
// The attestation statement is not checked, since none was asked for.
func (rp RelyingParty) VerifyRegistration(challenge, clientDataJSON, attestationObject []byte) (*Credential, error) {
	if err := rp.checkClientData(clientDataJSON, "webauthn.create", challenge); err != nil {
		return nil, err
	}
	v, rest, err := decodeCBOR(attestationObject)
	if err != nil || len(rest) != 0 {
		return nil, errors.New("bad attestation object")
	}
	obj, ok := v.(map[any]any)
	if !ok {
		return nil, errors.New("bad attestation object")
	}
	raw, ok := obj["authData"].([]byte)
	if !ok {
		return nil, errors.New("attestation object has no authenticator data")
	}
	ad, err := parseAuthenticatorData(raw)
	if err != nil {
		return nil, err
	}
	if err := rp.checkAuthenticatorData(ad); err != nil {
		return nil, err
	}
	if ad.credentialID == nil {
		return nil, errors.New("no credential was made")
	}
	if _, err := parsePublicKey(ad.publicKey); err != nil {
		return nil, err
	}
	return &Credential{ID: ad.credentialID, PublicKey: ad.publicKey, SignCount: ad.signCount}, nil
}

// VerifyAssertion checks that cred signed in answer to challenge, as
// navigator.credentials.get returned it, and answers the signature
// counter to store. A counter that does not go forward is refused, for an
// authenticator that keeps one: those that do not always send zero.
func (rp RelyingParty) VerifyAssertion(cred Credential, challenge, clientDataJSON, authenticatorDataRaw, signature []byte) (uint32, error) {
	if err := rp.checkClientData(clientDataJSON, "webauthn.get", challenge); err != nil {
		return 0, err
	}
	ad, err := parseAuthenticatorData(authenticatorDataRaw)
	if err != nil {
		return 0, err
	}
	if err := rp.checkAuthenticatorData(ad); err != nil {
		return 0, err
	}
	key, err := parsePublicKey(cred.PublicKey)
	if err != nil {
		return 0, err
	}
	hash := sha256.Sum256(clientDataJSON)
	if !key.verify(append(append([]byte(nil), authenticatorDataRaw...), hash[:]...), signature) {
		return 0, ErrSignature
	}
	if (ad.signCount != 0 || cred.SignCount != 0) && ad.signCount <= cred.SignCount {
		return 0, ErrCounterBackward
	}
	return ad.signCount, nil
}
