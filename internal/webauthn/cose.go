package webauthn

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rsa"
	"crypto/sha256"
	"errors"
	"fmt"
	"math/big"
)

// A credential's public key is a COSE key (RFC 9052, 9053). These are the
// algorithms taken, the ones authenticators make: ES256, EdDSA and RS256.
const (
	AlgES256 = -7
	AlgEdDSA = -8
	AlgRS256 = -257
)

// Algorithms lists them in the order a browser is asked to prefer them.
var Algorithms = []int64{AlgES256, AlgEdDSA, AlgRS256}

// COSE key parameters and values.
const (
	coseKty = 1
	coseAlg = 3
	coseCrv = -1 // EC2 and OKP; RSA's modulus
	coseX   = -2 // EC2 and OKP; RSA's exponent
	coseY   = -3

	ktyOKP = 1
	ktyEC2 = 2
	ktyRSA = 3

	crvP256    = 1
	crvEd25519 = 6
)

// minRSABits is the smallest RSA key a signature is taken from.
const minRSABits = 2048

// publicKey is a credential's key, read from its COSE form.
type publicKey struct {
	alg int64
	key crypto.PublicKey
}

// parsePublicKey reads a COSE key as stored, refusing any algorithm not
// taken and any key not well formed.
func parsePublicKey(data []byte) (*publicKey, error) {
	v, rest, err := decodeCBOR(data)
	if err != nil {
		return nil, err
	}
	if len(rest) != 0 {
		return nil, errors.New("public key followed by more")
	}
	m, ok := v.(map[any]any)
	if !ok {
		return nil, errors.New("public key is not a COSE key")
	}
	return coseKey(m)
}

func coseKey(m map[any]any) (*publicKey, error) {
	kty, _ := m[int64(coseKty)].(int64)
	alg, _ := m[int64(coseAlg)].(int64)
	bytesAt := func(label int64) []byte { b, _ := m[label].([]byte); return b }
	switch alg {
	case AlgES256:
		crv, _ := m[int64(coseCrv)].(int64)
		x, y := bytesAt(coseX), bytesAt(coseY)
		if kty != ktyEC2 || crv != crvP256 || len(x) != 32 || len(y) != 32 {
			return nil, errors.New("bad ES256 key")
		}
		// A point that is not on the curve is refused.
		k, err := ecdsa.ParseUncompressedPublicKey(elliptic.P256(), append(append([]byte{4}, x...), y...))
		if err != nil {
			return nil, fmt.Errorf("bad ES256 key: %w", err)
		}
		return &publicKey{alg: alg, key: k}, nil
	case AlgEdDSA:
		crv, _ := m[int64(coseCrv)].(int64)
		x := bytesAt(coseX)
		if kty != ktyOKP || crv != crvEd25519 || len(x) != ed25519.PublicKeySize {
			return nil, errors.New("bad EdDSA key")
		}
		return &publicKey{alg: alg, key: ed25519.PublicKey(x)}, nil
	case AlgRS256:
		n, e := new(big.Int).SetBytes(bytesAt(coseCrv)), new(big.Int).SetBytes(bytesAt(coseX))
		if kty != ktyRSA {
			return nil, errors.New("bad RS256 key")
		}
		if !e.IsInt64() || e.Int64() < 3 || e.Int64() > 1<<31-1 {
			return nil, errors.New("bad RSA exponent")
		}
		if n.BitLen() < minRSABits {
			return nil, errors.New("RSA key too small")
		}
		return &publicKey{alg: alg, key: &rsa.PublicKey{N: n, E: int(e.Int64())}}, nil
	}
	return nil, fmt.Errorf("algorithm %d is not taken", alg)
}

// verify reports whether the key signed message.
func (k *publicKey) verify(message, sig []byte) bool {
	switch key := k.key.(type) {
	case *ecdsa.PublicKey:
		digest := sha256.Sum256(message)
		return ecdsa.VerifyASN1(key, digest[:], sig)
	case ed25519.PublicKey:
		return ed25519.Verify(key, message, sig)
	case *rsa.PublicKey:
		digest := sha256.Sum256(message)
		return rsa.VerifyPKCS1v15(key, crypto.SHA256, digest[:], sig) == nil
	}
	return false
}
