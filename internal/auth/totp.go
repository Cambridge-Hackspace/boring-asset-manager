package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// TOTP codes, as RFC 6238 and authenticator apps have them: HMAC-SHA-1,
// six digits, 30-second steps (docs/access.md).
const (
	TOTPDigits = 6
	TOTPPeriod = 30 * time.Second
	// TOTPSkew is how many steps either side of now a code may be from,
	// for a phone's clock a little out.
	TOTPSkew = 1
)

// totpEncoding is base32 as apps take a secret: no padding.
var totpEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// NewTOTPSecret makes a secret of 160 random bits, as RFC 4226 advises.
func NewTOTPSecret() ([]byte, error) {
	secret := make([]byte, 20)
	if _, err := rand.Read(secret); err != nil {
		return nil, fmt.Errorf("reading random bytes for a TOTP secret: %w", err)
	}
	return secret, nil
}

// TOTPSecretText is a secret as people type it into an app: base32, in
// groups of four.
func TOTPSecretText(secret []byte) string {
	s := totpEncoding.EncodeToString(secret)
	var b strings.Builder
	for i := 0; i < len(s); i += 4 {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(s[i:min(i+4, len(s))])
	}
	return b.String()
}

// TOTPURI is the otpauth:// URI an app scans: the account under the
// issuer's name.
func TOTPURI(issuer, account string, secret []byte) string {
	q := url.Values{}
	q.Set("secret", totpEncoding.EncodeToString(secret))
	q.Set("issuer", issuer)
	q.Set("algorithm", "SHA1")
	q.Set("digits", fmt.Sprint(TOTPDigits))
	q.Set("period", fmt.Sprint(int(TOTPPeriod.Seconds())))
	return "otpauth://totp/" + url.PathEscape(issuer+":"+account) + "?" + q.Encode()
}

// TOTPStep is the step a time falls in.
func TOTPStep(t time.Time) int64 {
	return t.Unix() / int64(TOTPPeriod.Seconds())
}

// TOTPCode is the code for a step.
func TOTPCode(secret []byte, step int64) string {
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(step))
	mac := hmac.New(sha1.New, secret)
	mac.Write(msg[:])
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	n := binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff
	return fmt.Sprintf("%0*d", TOTPDigits, n%1_000_000)
}

// CheckTOTP finds the step a code is for, within TOTPSkew of now and after
// lastUsed, so that a code is accepted once: the caller stores the step
// returned, and an earlier or equal one is refused after. Spaces in the
// code, as some apps show it, are ignored.
func CheckTOTP(secret []byte, code string, now time.Time, lastUsed int64) (int64, bool) {
	code = strings.ReplaceAll(strings.TrimSpace(code), " ", "")
	if len(code) != TOTPDigits {
		return 0, false
	}
	at := TOTPStep(now)
	found, ok := int64(0), false
	// Every step in the window is compared, so how long this takes does not
	// say which one matched.
	for step := at - TOTPSkew; step <= at+TOTPSkew; step++ {
		if subtle.ConstantTimeCompare([]byte(TOTPCode(secret, step)), []byte(code)) == 1 && step > lastUsed && !ok {
			found, ok = step, true
		}
	}
	return found, ok
}
