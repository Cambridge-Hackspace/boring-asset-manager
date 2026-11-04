package idp

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"testing"
)

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

// signedWith makes a token of alg, signed by sign over its first two parts.
func signedWith(alg string, sign func(signed []byte) []byte) string {
	h, _ := json.Marshal(map[string]string{"alg": alg, "kid": "k1"})
	signed := b64(h) + "." + b64([]byte(`{"sub":"ada"}`))
	return signed + "." + b64(sign([]byte(signed)))
}

func digestOf(hash crypto.Hash, b []byte) []byte {
	h := hash.New()
	h.Write(b)
	return h.Sum(nil)
}

func TestSignaturesOfEachAlgorithmCheck(t *testing.T) {
	rsaKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	ecKey, _ := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	edPub, edKey, _ := ed25519.GenerateKey(rand.Reader)
	ecRaw, _ := ecKey.PublicKey.Bytes()
	set, _ := json.Marshal(map[string]any{"keys": []map[string]string{
		{"kty": "RSA", "kid": "rsa", "n": b64(rsaKey.N.Bytes()), "e": b64(big.NewInt(int64(rsaKey.E)).Bytes())},
		{"kty": "EC", "kid": "ec", "crv": "P-384", "x": b64(ecRaw[1:49]), "y": b64(ecRaw[49:])},
		{"kty": "OKP", "kid": "ed", "crv": "Ed25519", "x": b64(edPub)},
		{"kty": "RSA", "kid": "enc", "use": "enc", "n": b64(rsaKey.N.Bytes()), "e": "AQAB"},
		{"kty": "oct", "kid": "hmac", "k": "c2VjcmV0"},
	}})
	keys, err := parseJWKS(set)
	if err != nil || len(keys) != 3 {
		t.Fatalf("keys %v, err %v: want the three signing keys, not the encryption or symmetric ones", keys, err)
	}
	byKid := map[string]crypto.PublicKey{}
	for _, k := range keys {
		byKid[k.Kid] = k.Key
	}

	tokens := map[string]struct {
		token string
		key   string
	}{
		"RS256": {signedWith("RS256", func(s []byte) []byte {
			sig, _ := rsa.SignPKCS1v15(rand.Reader, rsaKey, crypto.SHA256, digestOf(crypto.SHA256, s))
			return sig
		}), "rsa"},
		"PS512": {signedWith("PS512", func(s []byte) []byte {
			sig, _ := rsa.SignPSS(rand.Reader, rsaKey, crypto.SHA512, digestOf(crypto.SHA512, s), &rsa.PSSOptions{SaltLength: rsa.PSSSaltLengthEqualsHash})
			return sig
		}), "rsa"},
		"ES384": {signedWith("ES384", func(s []byte) []byte {
			r, ss, _ := ecdsa.Sign(rand.Reader, ecKey, digestOf(crypto.SHA384, s))
			out := make([]byte, 96)
			r.FillBytes(out[:48])
			ss.FillBytes(out[48:])
			return out
		}), "ec"},
		"EdDSA": {signedWith("EdDSA", func(s []byte) []byte { return ed25519.Sign(edKey, s) }), "ed"},
	}
	for alg, c := range tokens {
		tok, err := parseJWS(c.token)
		if err != nil {
			t.Fatalf("%s: %v", alg, err)
		}
		for kid, key := range byKid {
			if got := tok.verify(key); got != (kid == c.key) {
				t.Errorf("%s by the %s key: %v", alg, kid, got)
			}
		}
	}
	// ES256 claimed over a P-384 key does not check.
	es, _ := parseJWS(signedWith("ES256", func([]byte) []byte { return make([]byte, 64) }))
	if es.verify(byKid["ec"]) {
		t.Error("ES256 checked against a P-384 key")
	}
}

func TestKeysAndTokensNotTaken(t *testing.T) {
	small, _ := rsa.GenerateKey(rand.Reader, 1024)
	if _, err := (rawJWK{Kty: "RSA", N: b64(small.N.Bytes()), E: "AQAB"}).publicKey(); err == nil {
		t.Error("a 1024-bit RSA key was taken")
	}
	// A point not on the curve.
	if _, err := (rawJWK{Kty: "EC", Crv: "P-256", X: b64(make([]byte, 32)), Y: b64(append(make([]byte, 31), 1))}).publicKey(); err == nil {
		t.Error("a point off the curve was taken")
	}
	for _, alg := range []string{"none", "HS256", "HS512", ""} {
		if _, err := parseJWS(signedWith(alg, func([]byte) []byte { return []byte("x") })); err == nil {
			t.Errorf("alg %q was taken", alg)
		}
	}
	h, _ := json.Marshal(map[string]any{"alg": "ES256", "crit": []string{"exp"}})
	if _, err := parseJWS(b64(h) + ".e30.AA"); err == nil {
		t.Error("a critical extension was taken")
	}
	if _, err := parseJWS("only.two"); err == nil {
		t.Error("two parts were taken")
	}
}
