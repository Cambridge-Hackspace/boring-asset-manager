package webauthn

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
)

// SoftAuthenticator is a passkey in software, for tests: an ES256 key, and
// a signature counter that goes up with each use unless set.
type SoftAuthenticator struct {
	ID    []byte
	Count uint32
	key   *ecdsa.PrivateKey
}

// NewSoftAuthenticator makes one with a new key and credential ID.
func NewSoftAuthenticator() *SoftAuthenticator {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		panic(err)
	}
	id := make([]byte, 16)
	rand.Read(id)
	return &SoftAuthenticator{ID: id, key: key}
}

// ClientData is what a browser at origin would send for a ceremony.
func ClientData(ceremony, origin string, challenge []byte) []byte {
	b, _ := json.Marshal(clientData{Type: ceremony, Challenge: base64.RawURLEncoding.EncodeToString(challenge), Origin: origin})
	return b
}

func (a *SoftAuthenticator) authData(rpID string, attested []byte) []byte {
	hash := sha256.Sum256([]byte(rpID))
	flags := byte(flagUP | flagUV)
	if attested != nil {
		flags |= flagAT
	}
	out := append(hash[:], flags)
	out = binary.BigEndian.AppendUint32(out, a.Count)
	return append(out, attested...)
}

// Create answers navigator.credentials.create for rpID, at origin, with
// attestation "none".
func (a *SoftAuthenticator) Create(rpID, origin string, challenge []byte) (clientDataJSON, attestationObject []byte) {
	pub, err := a.key.PublicKey.Bytes()
	if err != nil {
		panic(err)
	}
	cose := encodeCBOR([]cborPair{
		{coseKty, ktyEC2}, {coseAlg, AlgES256}, {coseCrv, crvP256},
		{coseX, pub[1:33]}, {coseY, pub[33:65]},
	})
	attested := make([]byte, 16) // a zero AAGUID, as with no attestation
	attested = binary.BigEndian.AppendUint16(attested, uint16(len(a.ID)))
	attested = append(append(attested, a.ID...), cose...)
	attestationObject = encodeCBOR([]cborPair{
		{"fmt", "none"}, {"attStmt", []cborPair{}}, {"authData", a.authData(rpID, attested)},
	})
	return ClientData("webauthn.create", origin, challenge), attestationObject
}

// Get answers navigator.credentials.get for rpID, at origin, counting the
// use first.
func (a *SoftAuthenticator) Get(rpID, origin string, challenge []byte) (clientDataJSON, authenticatorData, signature []byte) {
	a.Count++
	clientDataJSON = ClientData("webauthn.get", origin, challenge)
	authenticatorData = a.authData(rpID, nil)
	return clientDataJSON, authenticatorData, a.Sign(authenticatorData, clientDataJSON)
}

// Sign signs authenticator data and client data as an assertion is signed.
func (a *SoftAuthenticator) Sign(authenticatorData, clientDataJSON []byte) []byte {
	hash := sha256.Sum256(clientDataJSON)
	digest := sha256.Sum256(append(append([]byte(nil), authenticatorData...), hash[:]...))
	sig, err := ecdsa.SignASN1(rand.Reader, a.key, digest[:])
	if err != nil {
		panic(err)
	}
	return sig
}
