package auth

import (
	"errors"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func TestPasswordHashingAndVerification(t *testing.T) {
	password := "correct-horse-battery-staple-123"

	hash, err := HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword failed: %v", err)
	}

	if !CheckPassword(password, hash) {
		t.Error("expected CheckPassword to succeed with correct password")
	}

	if CheckPassword("wrong-password", hash) {
		t.Error("expected CheckPassword to fail with incorrect password")
	}

	if _, err := HashPassword(""); err == nil {
		t.Error("expected empty password to produce error")
	}
}

// TestHashesUseCost12 checks new hashes use cost 12. Other packages' tests
// lower BcryptCost for speed; this package never does.
func TestHashesUseCost12(t *testing.T) {
	hash, err := HashPassword("correct-horse-battery-staple-123")
	if err != nil {
		t.Fatal(err)
	}
	if cost, err := bcrypt.Cost([]byte(hash)); err != nil || cost != 12 {
		t.Errorf("hash cost %d (%v), want 12", cost, err)
	}
}

func TestGenerateSessionToken(t *testing.T) {
	token1, err := GenerateSessionToken()
	if err != nil {
		t.Fatalf("GenerateSessionToken failed: %v", err)
	}
	if len(token1) != 64 {
		t.Errorf("expected 64 character hex string, got length %d", len(token1))
	}

	token2, err := GenerateSessionToken()
	if err != nil {
		t.Fatalf("second GenerateSessionToken failed: %v", err)
	}
	if token1 == token2 {
		t.Error("expected random session tokens to be distinct")
	}
}

func TestNormalizeEmail(t *testing.T) {
	cases := []struct {
		input    string
		expected string
	}{
		{"Admin@Example.COM", "admin@example.com"},
		{"  user@Domain.Org  ", "user@domain.org"},
		{"test.account+tag@domain.co.uk", "test.account+tag@domain.co.uk"},
	}

	for _, c := range cases {
		out := NormalizeEmail(c.input)
		if out != c.expected {
			t.Errorf("NormalizeEmail(%q) = %q, expected %q", c.input, out, c.expected)
		}
	}
}

func TestPasswordLengthLimits(t *testing.T) {
	for _, pw := range []string{"", "short12", strings.Repeat("x", 73)} {
		if _, err := HashPassword(pw); !errors.Is(err, ErrPasswordPolicy) {
			t.Errorf("%d-byte password: got %v, want ErrPasswordPolicy", len(pw), err)
		}
	}
	for _, pw := range []string{"eight888", strings.Repeat("x", 72)} {
		if _, err := HashPassword(pw); err != nil {
			t.Errorf("%d-byte password refused: %v", len(pw), err)
		}
	}
}
