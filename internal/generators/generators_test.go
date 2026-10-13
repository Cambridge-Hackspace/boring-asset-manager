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

	// Standard QR tests
	if err := ValidateGeneratorCode(TypeQR, "https://example.org/a/uuid-1234-5678"); err != nil {
		t.Errorf("valid qr code rejected: %v", err)
	}
	if err := ValidateGeneratorCode(TypeQR, ""); err == nil {
		t.Errorf("expected error for empty qr code, got nil")
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
