package auth

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// RFC 6238's test vectors for SHA-1, as six digits: the RFC's eight-digit
// codes, less their first two.
func TestTOTPCodesMatchRFC6238(t *testing.T) {
	secret := []byte("12345678901234567890")
	for unix, want := range map[int64]string{
		59:          "287082",
		1111111109:  "081804",
		1111111111:  "050471",
		1234567890:  "005924",
		2000000000:  "279037",
		20000000000: "353130",
	} {
		if got := TOTPCode(secret, TOTPStep(time.Unix(unix, 0))); got != want {
			t.Errorf("at %d: %s, want %s", unix, got, want)
		}
	}
}

func TestCheckTOTPAcceptsACodeOnceWithinItsWindow(t *testing.T) {
	secret := []byte("12345678901234567890")
	now := time.Unix(1234567890, 0)
	at := TOTPStep(now)

	step, ok := CheckTOTP(secret, TOTPCode(secret, at), now, 0)
	if !ok || step != at {
		t.Fatalf("the current code: %d, %v", step, ok)
	}
	if _, ok := CheckTOTP(secret, TOTPCode(secret, at), now, at); ok {
		t.Error("a code was accepted twice")
	}
	if _, ok := CheckTOTP(secret, TOTPCode(secret, at-1), now, at); ok {
		t.Error("an earlier code was accepted after a later one")
	}
	for _, s := range []int64{at - 1, at + 1} {
		if got, ok := CheckTOTP(secret, TOTPCode(secret, s), now, 0); !ok || got != s {
			t.Errorf("a code one step off (%d): %d, %v", s-at, got, ok)
		}
	}
	for _, s := range []int64{at - 2, at + 2} {
		if _, ok := CheckTOTP(secret, TOTPCode(secret, s), now, 0); ok {
			t.Errorf("a code %d steps off was accepted", s-at)
		}
	}
	code := TOTPCode(secret, at)
	if _, ok := CheckTOTP(secret, code[:3]+" "+code[3:], now, 0); !ok {
		t.Error("a code with a space in it, as apps show it, was refused")
	}
	for _, bad := range []string{"", "12345", "1234567", "abcdef"} {
		if _, ok := CheckTOTP(secret, bad, now, 0); ok {
			t.Errorf("%q was accepted", bad)
		}
	}
}

func TestTOTPURIAndText(t *testing.T) {
	secret := []byte("12345678901234567890")
	uri := TOTPURI("Hackspace BAM!", "ada@example.org", secret)
	if !strings.HasPrefix(uri, "otpauth://totp/Hackspace%20BAM%21:ada@example.org?") ||
		!strings.Contains(uri, "secret=GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ") || !strings.Contains(uri, "issuer=Hackspace+BAM%21") {
		t.Errorf("uri %s", uri)
	}
	if got := TOTPSecretText(secret); got != "GEZD GNBV GY3T QOJQ GEZD GNBV GY3T QOJQ" {
		t.Errorf("text %q", got)
	}
}

func TestSealerKeepsSecretsFromAnotherKey(t *testing.T) {
	key := []byte(strings.Repeat("k", 32))
	s, err := NewSealer(key)
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := s.Seal([]byte("the secret"), "factor-1")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(sealed, "the secret") {
		t.Errorf("sealed in the clear: %s", sealed)
	}
	if got, err := s.Open(sealed, "factor-1"); err != nil || string(got) != "the secret" {
		t.Errorf("opened %q, %v", got, err)
	}
	if _, err := s.Open(sealed, "factor-2"); !errors.Is(err, ErrUnsealable) {
		t.Errorf("opened for another factor: %v", err)
	}
	other, _ := NewSealer([]byte(strings.Repeat("o", 32)))
	if _, err := other.Open(sealed, "factor-1"); !errors.Is(err, ErrUnsealable) {
		t.Errorf("opened with another key: %v", err)
	}
	none, _ := NewSealer(nil)
	if _, err := none.Seal([]byte("x"), "p"); !errors.Is(err, ErrNoSecretKey) {
		t.Errorf("sealed with no key: %v", err)
	}
	if _, err := NewSealer([]byte("short")); err == nil {
		t.Error("a short key was accepted")
	}
	if k, err := NewSecretKey(); err != nil || len(k) != 44 {
		t.Errorf("new key %q, %v", k, err)
	}
}
