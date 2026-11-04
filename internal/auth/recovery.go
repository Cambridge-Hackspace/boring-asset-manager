package auth

import (
	"crypto/rand"
	"fmt"
	"strings"
)

// Recovery codes (docs/access.md#recovery-codes): each signs in once in
// place of a second factor. They are ten characters of Crockford's base32,
// 50 bits, written in two groups of five; read back, case, spaces and
// dashes do not matter, and the letters o, i and l are read as 0 and 1.

const recoveryAlphabet = "0123456789abcdefghjkmnpqrstvwxyz"

// RecoveryCodeLength is how many characters a recovery code has.
const RecoveryCodeLength = 10

// NewRecoveryCodes makes n codes, written as they are shown.
func NewRecoveryCodes(n int) ([]string, error) {
	codes := make([]string, n)
	buf := make([]byte, RecoveryCodeLength)
	for i := range codes {
		if _, err := rand.Read(buf); err != nil {
			return nil, fmt.Errorf("reading random bytes for a recovery code: %w", err)
		}
		var b strings.Builder
		for j, c := range buf {
			if j == RecoveryCodeLength/2 {
				b.WriteByte('-')
			}
			// 256 is a multiple of 32, so every character is as likely.
			b.WriteByte(recoveryAlphabet[c%32])
		}
		codes[i] = b.String()
	}
	return codes, nil
}

// NormalizeRecoveryCode reads a code as typed, or "" when it cannot be one.
func NormalizeRecoveryCode(code string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(code) {
		switch {
		case r == ' ' || r == '-':
			continue
		case r == 'o':
			r = '0'
		case r == 'i' || r == 'l':
			r = '1'
		}
		if !strings.ContainsRune(recoveryAlphabet, r) {
			return ""
		}
		b.WriteRune(r)
	}
	if b.Len() != RecoveryCodeLength {
		return ""
	}
	return b.String()
}
