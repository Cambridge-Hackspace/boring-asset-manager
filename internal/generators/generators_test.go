package generators

import (
	"strings"
	"testing"
)

func TestValidateGeneratorCode(t *testing.T) {
	// Micro QR tests
	if err := ValidateGeneratorCode(TypeMicroQR, "MQR-12345"); err != nil {
		t.Errorf("valid micro_qr code rejected: %v", err)
	}
	if err := ValidateGeneratorCode(TypeMicroQR, "lowercase-bad"); err == nil {
		t.Errorf("expected error for lowercase micro_qr, got nil")
	}
	longMicro := strings.Repeat("A", 36)
	if err := ValidateGeneratorCode(TypeMicroQR, longMicro); err == nil {
		t.Errorf("expected error for exceeding micro_qr capacity > 35, got nil")
	}
	// M4-L capacity: 35 numeric digits, but only 21 alphanumeric characters.
	if err := ValidateGeneratorCode(TypeMicroQR, strings.Repeat("7", 35)); err != nil {
		t.Errorf("35-digit numeric micro_qr rejected: %v", err)
	}
	if err := ValidateGeneratorCode(TypeMicroQR, strings.Repeat("7", 36)); err == nil {
		t.Errorf("expected error for 36-digit numeric micro_qr, got nil")
	}
	if err := ValidateGeneratorCode(TypeMicroQR, strings.Repeat("A", 21)); err != nil {
		t.Errorf("21-character alphanumeric micro_qr rejected: %v", err)
	}
	if err := ValidateGeneratorCode(TypeMicroQR, strings.Repeat("A", 22)); err == nil {
		t.Errorf("expected error for 22-character alphanumeric micro_qr (exceeds M4-L), got nil")
	}
	if err := ValidateGeneratorCode(TypeMicroQR, "1234567890-1234567890-12"); err == nil {
		t.Errorf("expected error for 24-character mixed micro_qr (alphanumeric mode), got nil")
	}

	// Standard QR tests
	if err := ValidateGeneratorCode(TypeQR, "https://example.org/a/uuid-1234-5678"); err != nil {
		t.Errorf("valid qr code rejected: %v", err)
	}
	if err := ValidateGeneratorCode(TypeQR, ""); err == nil {
		t.Errorf("expected error for empty qr code, got nil")
	}
	if err := ValidateGeneratorCode(TypeQR, strings.Repeat("a", 2953)); err != nil {
		t.Errorf("2953-byte qr payload rejected: %v", err)
	}
	// 1000 three-byte runes: under any rune limit, but 3000 bytes exceeds version 40-L.
	if err := ValidateGeneratorCode(TypeQR, strings.Repeat("€", 1000)); err == nil {
		t.Errorf("expected error for 3000-byte UTF-8 qr payload, got nil")
	}
	if err := ValidateGeneratorCode(TypeQR, "bad\xffutf8"); err == nil {
		t.Errorf("expected error for invalid UTF-8 qr payload, got nil")
	}

	// Code128 tests
	if err := ValidateGeneratorCode(TypeCode128, "BAM-ASSET-001"); err != nil {
		t.Errorf("valid code128 rejected: %v", err)
	}
	if err := ValidateGeneratorCode(TypeCode128, "Non-Ascii-ñ"); err == nil {
		t.Errorf("expected error for non-ASCII code128, got nil")
	}

	// Unknown type
	if err := ValidateGeneratorCode("unknown_engine", "123"); err == nil {
		t.Errorf("expected error for unknown generator type, got nil")
	}
}
