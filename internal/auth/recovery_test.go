package auth

import "testing"

func TestRecoveryCodes(t *testing.T) {
	codes, err := NewRecoveryCodes(10)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, c := range codes {
		if len(c) != RecoveryCodeLength+1 || c[5] != '-' {
			t.Errorf("shown as %q", c)
		}
		n := NormalizeRecoveryCode(c)
		if n == "" || seen[n] {
			t.Errorf("%q read back as %q", c, n)
		}
		seen[n] = true
	}
}

func TestNormalizeRecoveryCode(t *testing.T) {
	for typed, want := range map[string]string{
		"abcde-12345":    "abcde12345",
		" ABCDE 12345 ":  "abcde12345",
		"oil00-11111":    "0110011111",
		"abcde-1234":     "",
		"abcde-123456":   "",
		"abcdu-12345":    "",
		"123456":         "",
		"abcde-12345-xy": "",
	} {
		if got := NormalizeRecoveryCode(typed); got != want {
			t.Errorf("%q: %q, want %q", typed, got, want)
		}
	}
}
