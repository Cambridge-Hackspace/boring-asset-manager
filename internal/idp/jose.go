package idp

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rsa"
	_ "crypto/sha256" // the hashes signatures name
	_ "crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
)

// What an ID token is signed with: only asymmetric algorithms, so never
// "none", nor HS256 keyed with the client secret, which anyone holding the
// secret could forge (docs/access.md#openid-connect).
var signingAlgs = map[string]crypto.Hash{
	"RS256": crypto.SHA256, "RS384": crypto.SHA384, "RS512": crypto.SHA512,
	"PS256": crypto.SHA256, "PS384": crypto.SHA384, "PS512": crypto.SHA512,
	"ES256": crypto.SHA256, "ES384": crypto.SHA384, "ES512": crypto.SHA512,
	"EdDSA": 0,
}

// minRSABits is the smallest RSA key a signature is taken from.
const minRSABits = 2048

// jwk is one of a provider's signing keys.
type jwk struct {
	Kid string
	Alg string
	Key crypto.PublicKey
}

type rawJWK struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Use string `json:"use"`
	Alg string `json:"alg"`
	N   string `json:"n"`
	E   string `json:"e"`
	Crv string `json:"crv"`
	X   string `json:"x"`
	Y   string `json:"y"`
}

// parseJWKS reads a key set, keeping the signing keys it can use and
// skipping the rest, such as encryption keys or kinds it does not know.
func parseJWKS(data []byte) ([]jwk, error) {
	var set struct {
		Keys []rawJWK `json:"keys"`
	}
	if err := json.Unmarshal(data, &set); err != nil {
		return nil, fmt.Errorf("reading the key set: %w", err)
	}
	var out []jwk
	for _, k := range set.Keys {
		if k.Use != "" && k.Use != "sig" {
			continue
		}
		key, err := k.publicKey()
		if err != nil {
			continue
		}
		out = append(out, jwk{Kid: k.Kid, Alg: k.Alg, Key: key})
	}
	return out, nil
}

func (k rawJWK) publicKey() (crypto.PublicKey, error) {
	switch k.Kty {
	case "RSA":
		n, err := b64Int(k.N)
		if err != nil {
			return nil, err
		}
		e, err := b64Int(k.E)
		if err != nil || !e.IsInt64() || e.Int64() < 3 || e.Int64() > 1<<31-1 {
			return nil, errors.New("bad RSA exponent")
		}
		if n.BitLen() < minRSABits {
			return nil, errors.New("RSA key too small")
		}
		return &rsa.PublicKey{N: n, E: int(e.Int64())}, nil
	case "EC":
		var curve elliptic.Curve
		switch k.Crv {
		case "P-256":
			curve = elliptic.P256()
		case "P-384":
			curve = elliptic.P384()
		case "P-521":
			curve = elliptic.P521()
		default:
			return nil, errors.New("unknown curve")
		}
		size := (curve.Params().BitSize + 7) / 8
		x, err := base64.RawURLEncoding.DecodeString(k.X)
		if err != nil || len(x) != size {
			return nil, errors.New("bad EC x")
		}
		y, err := base64.RawURLEncoding.DecodeString(k.Y)
		if err != nil || len(y) != size {
			return nil, errors.New("bad EC y")
		}
		// A point that is not on the curve is refused.
		return ecdsa.ParseUncompressedPublicKey(curve, append(append([]byte{4}, x...), y...))
	case "OKP":
		x, err := base64.RawURLEncoding.DecodeString(k.X)
		if k.Crv != "Ed25519" || err != nil || len(x) != ed25519.PublicKeySize {
			return nil, errors.New("bad OKP key")
		}
		return ed25519.PublicKey(x), nil
	}
	return nil, errors.New("unknown key type")
}

func b64Int(s string) (*big.Int, error) {
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil || len(b) == 0 {
		return nil, errors.New("bad number")
	}
	return new(big.Int).SetBytes(b), nil
}

// jws is a compact JWS taken apart, its signature not yet checked.
type jws struct {
	Alg     string
	Kid     string
	Payload []byte
	signed  []byte
	sig     []byte
}

func parseJWS(token string) (*jws, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, errors.New("not a signed token")
	}
	head, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, errors.New("bad token header")
	}
	var h struct {
		Alg  string   `json:"alg"`
		Kid  string   `json:"kid"`
		Crit []string `json:"crit"`
	}
	if err := json.Unmarshal(head, &h); err != nil {
		return nil, errors.New("bad token header")
	}
	if _, ok := signingAlgs[h.Alg]; !ok {
		return nil, fmt.Errorf("token signed with %q, which is not taken", h.Alg)
	}
	// Extensions it must understand, and does not.
	if len(h.Crit) > 0 {
		return nil, errors.New("token has critical extensions")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, errors.New("bad token payload")
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, errors.New("bad token signature")
	}
	return &jws{Alg: h.Alg, Kid: h.Kid, Payload: payload, signed: []byte(parts[0] + "." + parts[1]), sig: sig}, nil
}

// verify reports whether key signed the token, with its algorithm.
func (t *jws) verify(key crypto.PublicKey) bool {
	hash := signingAlgs[t.Alg]
	var digest []byte
	if hash != 0 {
		h := hash.New()
		h.Write(t.signed)
		digest = h.Sum(nil)
	}
	switch k := key.(type) {
	case *rsa.PublicKey:
		switch t.Alg[:2] {
		case "RS":
			return rsa.VerifyPKCS1v15(k, hash, digest, t.sig) == nil
		case "PS":
			return rsa.VerifyPSS(k, hash, digest, t.sig, &rsa.PSSOptions{SaltLength: rsa.PSSSaltLengthEqualsHash}) == nil
		}
	case *ecdsa.PublicKey:
		want := map[string]string{"ES256": "P-256", "ES384": "P-384", "ES512": "P-521"}[t.Alg]
		size := (k.Curve.Params().BitSize + 7) / 8
		if want != k.Curve.Params().Name || len(t.sig) != 2*size {
			return false
		}
		r, s := new(big.Int).SetBytes(t.sig[:size]), new(big.Int).SetBytes(t.sig[size:])
		return ecdsa.Verify(k, digest, r, s)
	case ed25519.PublicKey:
		return t.Alg == "EdDSA" && ed25519.Verify(k, t.signed, t.sig)
	}
	return false
}
