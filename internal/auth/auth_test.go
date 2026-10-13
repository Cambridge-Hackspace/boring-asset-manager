package auth

import (
	"testing"
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
