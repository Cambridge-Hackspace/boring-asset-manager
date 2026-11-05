package webauthn

import (
	"bytes"
	"crypto"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"errors"
	"math/big"
	"strings"
	"testing"
)

var rp = RelyingParty{ID: "bam.example.org", Origin: "https://bam.example.org"}

func register(t *testing.T, a *SoftAuthenticator) *Credential {
	t.Helper()
	challenge, err := NewChallenge()
	if err != nil {
		t.Fatal(err)
	}
	cd, att := a.Create(rp.ID, rp.Origin, challenge)
	cred, err := rp.VerifyRegistration(challenge, cd, att)
	if err != nil {
		t.Fatalf("registering: %v", err)
	}
	return cred
}

func TestRegisteringThenSigningIn(t *testing.T) {
	a := NewSoftAuthenticator()
	cred := register(t, a)
	if !bytes.Equal(cred.ID, a.ID) || cred.SignCount != 0 {
		t.Fatalf("registered %x at %d, want %x at 0", cred.ID, cred.SignCount, a.ID)
	}
	for want := uint32(1); want <= 2; want++ {
		challenge, _ := NewChallenge()
		cd, ad, sig := a.Get(rp.ID, rp.Origin, challenge)
		n, err := rp.VerifyAssertion(*cred, challenge, cd, ad, sig)
		if err != nil {
			t.Fatalf("signing in: %v", err)
		}
		if n != want {
			t.Fatalf("counter %d, want %d", n, want)
		}
		cred.SignCount = n
	}
}

func TestRegisteringIsRefusedForAnotherSite(t *testing.T) {
	a := NewSoftAuthenticator()
	challenge, _ := NewChallenge()
	cases := []struct {
		name      string
		rpID      string
		origin    string
		challenge []byte
		want      error
	}{
		{"a wrong origin", rp.ID, "https://evil.example.org", challenge, ErrOrigin},
		{"a wrong relying party ID", "evil.example.org", rp.Origin, challenge, ErrRelyingParty},
		{"another challenge", rp.ID, rp.Origin, []byte("another challenge"), ErrChallenge},
	}
	for _, c := range cases {
		cd, att := a.Create(c.rpID, c.origin, c.challenge)
		if _, err := rp.VerifyRegistration(challenge, cd, att); !errors.Is(err, c.want) {
			t.Errorf("%s: %v, want %v", c.name, err, c.want)
		}
	}
	// An assertion is not a registration.
	cd, _ := a.Create(rp.ID, rp.Origin, challenge)
	cd = bytes.Replace(cd, []byte("webauthn.create"), []byte("webauthn.get"), 1)
	_, att := a.Create(rp.ID, rp.Origin, challenge)
	if _, err := rp.VerifyRegistration(challenge, cd, att); err == nil {
		t.Error("an assertion's client data registered a passkey")
	}
}

func TestSigningInIsRefused(t *testing.T) {
	a := NewSoftAuthenticator()
	cred := register(t, a)
	challenge, _ := NewChallenge()

	cd, ad, sig := a.Get(rp.ID, "https://evil.example.org", challenge)
	if _, err := rp.VerifyAssertion(*cred, challenge, cd, ad, sig); !errors.Is(err, ErrOrigin) {
		t.Errorf("a wrong origin: %v", err)
	}
	cd, ad, sig = a.Get("evil.example.org", rp.Origin, challenge)
	if _, err := rp.VerifyAssertion(*cred, challenge, cd, ad, sig); !errors.Is(err, ErrRelyingParty) {
		t.Errorf("a wrong relying party ID: %v", err)
	}
	// Answering one challenge does not answer the next.
	cd, ad, sig = a.Get(rp.ID, rp.Origin, challenge)
	next, _ := NewChallenge()
	if _, err := rp.VerifyAssertion(*cred, next, cd, ad, sig); !errors.Is(err, ErrChallenge) {
		t.Errorf("a replayed answer: %v", err)
	}
	// Another key's signature.
	other := NewSoftAuthenticator()
	if _, err := rp.VerifyAssertion(*cred, challenge, cd, ad, other.Sign(ad, cd)); !errors.Is(err, ErrSignature) {
		t.Errorf("another key's signature: %v", err)
	}
	// Authenticator data changed after signing.
	tampered := append([]byte(nil), ad...)
	tampered[36]++
	if _, err := rp.VerifyAssertion(*cred, challenge, cd, tampered, sig); !errors.Is(err, ErrSignature) {
		t.Errorf("tampered authenticator data: %v", err)
	}
	// No one present.
	absent := append([]byte(nil), ad...)
	absent[32] &^= flagUP
	if _, err := rp.VerifyAssertion(*cred, challenge, cd, absent, a.Sign(absent, cd)); !errors.Is(err, ErrUserPresence) {
		t.Errorf("no one present: %v", err)
	}
}

func TestACounterGoingBackwardIsRefused(t *testing.T) {
	a := NewSoftAuthenticator()
	cred := register(t, a)
	a.Count = 5
	challenge, _ := NewChallenge()
	cd, ad, sig := a.Get(rp.ID, rp.Origin, challenge)
	n, err := rp.VerifyAssertion(*cred, challenge, cd, ad, sig)
	if err != nil || n != 6 {
		t.Fatalf("signing in: %d, %v", n, err)
	}
	cred.SignCount = n
	for _, count := range []uint32{6, 2, 0} {
		a.Count = count - 1
		if count == 0 {
			a.Count = ^uint32(0) // goes round to zero
		}
		cd, ad, sig := a.Get(rp.ID, rp.Origin, challenge)
		if _, err := rp.VerifyAssertion(*cred, challenge, cd, ad, sig); !errors.Is(err, ErrCounterBackward) {
			t.Errorf("a counter of %d after 6: %v", count, err)
		}
	}
}

func TestAnAuthenticatorWithoutACounterIsTaken(t *testing.T) {
	a := NewSoftAuthenticator()
	cred := register(t, a)
	for range 2 {
		challenge, _ := NewChallenge()
		cd := ClientData("webauthn.get", rp.Origin, challenge)
		ad := a.authData(rp.ID, nil) // always zero
		n, err := rp.VerifyAssertion(*cred, challenge, cd, ad, a.Sign(ad, cd))
		if err != nil || n != 0 {
			t.Fatalf("signing in without a counter: %d, %v", n, err)
		}
	}
}

func TestEdDSAAndRS256Keys(t *testing.T) {
	msg := []byte("signed")
	edPub, edPriv, _ := ed25519.GenerateKey(rand.Reader)
	ed, err := parsePublicKey(encodeCBOR([]cborPair{{coseKty, ktyOKP}, {coseAlg, AlgEdDSA}, {coseCrv, crvEd25519}, {coseX, []byte(edPub)}}))
	if err != nil {
		t.Fatal(err)
	}
	if !ed.verify(msg, ed25519.Sign(edPriv, msg)) || ed.verify([]byte("other"), ed25519.Sign(edPriv, msg)) {
		t.Error("EdDSA signatures not checked")
	}

	rsaKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	rsaCOSE := func(k *rsa.PublicKey) []byte {
		return encodeCBOR([]cborPair{{coseKty, ktyRSA}, {coseAlg, AlgRS256}, {coseCrv, k.N.Bytes()}, {coseX, big.NewInt(int64(k.E)).Bytes()}})
	}
	rs, err := parsePublicKey(rsaCOSE(&rsaKey.PublicKey))
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(msg)
	sig, _ := rsa.SignPKCS1v15(rand.Reader, rsaKey, crypto.SHA256, digest[:])
	if !rs.verify(msg, sig) || rs.verify([]byte("other"), sig) {
		t.Error("RS256 signatures not checked")
	}
	small, _ := rsa.GenerateKey(rand.Reader, 1024)
	if _, err := parsePublicKey(rsaCOSE(&small.PublicKey)); err == nil {
		t.Error("a 1024-bit RSA key was taken")
	}
}

func TestKeysNotTakenAreRefused(t *testing.T) {
	cases := map[string][]byte{
		"ES384":             encodeCBOR([]cborPair{{coseKty, ktyEC2}, {coseAlg, -35}, {coseCrv, 2}, {coseX, make([]byte, 48)}, {coseY, make([]byte, 48)}}),
		"a point off P-256": encodeCBOR([]cborPair{{coseKty, ktyEC2}, {coseAlg, AlgES256}, {coseCrv, crvP256}, {coseX, make([]byte, 32)}, {coseY, bytes.Repeat([]byte{1}, 32)}}),
		"ES256 on Ed25519":  encodeCBOR([]cborPair{{coseKty, ktyOKP}, {coseAlg, AlgES256}, {coseCrv, crvEd25519}, {coseX, make([]byte, 32)}}),
		"not a map":         encodeCBOR([]any{1, 2}),
	}
	for name, data := range cases {
		if _, err := parsePublicKey(data); err == nil {
			t.Errorf("%s was taken", name)
		}
	}
}

func TestCBOR(t *testing.T) {
	in := []cborPair{{1, 2}, {-1, -300}, {"bytes", []byte{1, 2, 3}}, {"text", strings.Repeat("é", 100)}, {"list", []any{int64(1) << 40, "x"}}}
	v, rest, err := decodeCBOR(append(encodeCBOR(in), 0xff))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(rest, []byte{0xff}) {
		t.Errorf("left %x", rest)
	}
	m := v.(map[any]any)
	if m[int64(1)] != int64(2) || m[int64(-1)] != int64(-300) || !bytes.Equal(m["bytes"].([]byte), []byte{1, 2, 3}) ||
		m["text"] != strings.Repeat("é", 100) || m["list"].([]any)[0] != int64(1)<<40 {
		t.Errorf("read back %v", m)
	}
	if v, _, err := decodeCBOR([]byte{0xf5, 0xf4, 0xf6}); err != nil || v != true {
		t.Errorf("true: %v, %v", v, err)
	}
	if v, _, err := decodeCBOR([]byte{0xc1, 0x1a, 0, 0, 0, 1}); err != nil || v != int64(1) {
		t.Errorf("a tagged integer: %v, %v", v, err)
	}

	bad := map[string][]byte{
		"empty":                 {},
		"a string ending early": {0x45, 1, 2},
		"an indefinite array":   {0x9f, 0xff},
		"a float":               {0xf9, 0x3c, 0x00},
		"a repeated key":        {0xa2, 0x01, 0x01, 0x01, 0x02},
		"a key that is a list":  {0xa1, 0x80, 0x01},
		"bad UTF-8":             {0x61, 0xff},
		"a huge array":          {0x9b, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff},
		"a huge integer":        {0x1b, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff},
		"too deep":              bytes.Repeat([]byte{0x81}, cborMaxDepth+2),
	}
	for name, data := range bad {
		if _, _, err := decodeCBOR(data); err == nil {
			t.Errorf("%s was read", name)
		}
	}
}

func TestTheRelyingPartyIsBaseURLsHost(t *testing.T) {
	got, err := NewRelyingParty("https://BAM.Example.org:8443/inventory/")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "bam.example.org" || got.Origin != "https://bam.example.org:8443" {
		t.Errorf("got %+v", got)
	}
	for _, bad := range []string{"", "bam.example.org", "ftp://bam.example.org"} {
		if _, err := NewRelyingParty(bad); err == nil {
			t.Errorf("%q was taken", bad)
		}
	}
}
