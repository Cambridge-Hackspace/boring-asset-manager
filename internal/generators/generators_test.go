package generators

import (
	"errors"
	"reflect"
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

func TestLookup(t *testing.T) {
	for _, genType := range AllowedGeneratorTypes() {
		g, err := Lookup(genType)
		if err != nil {
			t.Fatalf("Lookup(%q): %v", genType, err)
		}
		if g.Type() != genType {
			t.Errorf("Lookup(%q).Type() = %q", genType, g.Type())
		}
	}
	if _, err := Lookup("ean13"); !errors.Is(err, ErrInvalidGeneratorType) {
		t.Errorf("Lookup(ean13): got %v, want ErrInvalidGeneratorType", err)
	}
}

func TestEncodeSymbolShapes(t *testing.T) {
	cases := []struct {
		genType, payload, ec string
		cols, rows, quiet    int
	}{
		{TypeQR, "HELLO WORLD", "", 21, 21, 4},
		{TypeQR, "https://bam.example/a/7c9e6679-7425-40de-944b-e07fc1f90ae7", "H", 41, 41, 4},
		{TypeMicroQR, "BAM-42", "", 13, 13, 2},
		{TypeMicroQR, "BAM-42", "Q", 17, 17, 2},
		{TypeCode128, "1234", "", 11*4 + 13, 1, 10},
	}
	for _, c := range cases {
		g, err := Lookup(c.genType)
		if err != nil {
			t.Fatal(err)
		}
		sym, err := g.Encode(c.payload, c.ec)
		if err != nil {
			t.Fatalf("%s %q: %v", c.genType, c.payload, err)
		}
		if sym.Cols != c.cols || sym.Rows != c.rows || sym.QuietZone != c.quiet || len(sym.Dark) != sym.Cols*sym.Rows {
			t.Errorf("%s %q at %q: got %d×%d (quiet %d, %d modules), want %d×%d (quiet %d)",
				c.genType, c.payload, c.ec, sym.Cols, sym.Rows, sym.QuietZone, len(sym.Dark), c.cols, c.rows, c.quiet)
		}
	}
}

func TestEncodeRejectsBadInput(t *testing.T) {
	cases := []struct {
		genType, payload, ec string
		want                 error
	}{
		{TypeQR, "x", "X", ErrInvalidErrorCorrection},
		{TypeMicroQR, "A", "H", ErrInvalidErrorCorrection},
		{TypeCode128, "A", "L", ErrInvalidErrorCorrection},
		{TypeQR, "bad\xffutf8", "", ErrInvalidCharacterSet},
		{TypeMicroQR, "lower", "", ErrInvalidCharacterSet},
		{TypeCode128, "", "", ErrEmptyCode},
		// Valid as an asset key (M4-L), but too long once Q is requested.
		{TypeMicroQR, strings.Repeat("A", 21), "Q", ErrInvalidCodeLength},
	}
	for _, c := range cases {
		g, err := Lookup(c.genType)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := g.Encode(c.payload, c.ec); !errors.Is(err, c.want) {
			t.Errorf("%s %q at %q: got %v, want %v", c.genType, c.payload, c.ec, err, c.want)
		}
	}
}

// lowestLevel is the error correction level every validated payload must fit.
var lowestLevel = map[string]string{TypeQR: "L", TypeMicroQR: "L", TypeCode128: ""}

func TestValidatedCapacityBoundariesEncode(t *testing.T) {
	cases := map[string][]string{
		TypeQR:      {strings.Repeat("a", QRMaxBytes), strings.Repeat("€", QRMaxBytes/3)},
		TypeMicroQR: {strings.Repeat("7", MicroQRMaxNumeric), strings.Repeat("Z", MicroQRMaxAlphanumeric)},
		TypeCode128: {strings.Repeat("~", Code128MaxLength), strings.Repeat("9", Code128MaxLength)},
	}
	for genType, payloads := range cases {
		g, _ := Lookup(genType)
		for _, p := range payloads {
			if err := g.Validate(p); err != nil {
				t.Fatalf("%s: boundary payload rejected: %v", genType, err)
			}
			if _, err := g.Encode(p, lowestLevel[genType]); err != nil {
				t.Errorf("%s: validated %d-byte payload failed to encode: %v", genType, len(p), err)
			}
		}
	}
}

// FuzzValidatedPayloadsEncode checks the registry's promise: a code that may be
// assigned to an asset can always be printed.
func FuzzValidatedPayloadsEncode(f *testing.F) {
	for _, seed := range []string{"MQR-12345", "0123456789", "https://example.org/a/1", "BAM-ASSET-001", " $%*+-./:"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, payload string) {
		for _, genType := range AllowedGeneratorTypes() {
			g, _ := Lookup(genType)
			if g.Validate(payload) != nil {
				continue
			}
			if _, err := g.Encode(payload, lowestLevel[genType]); err != nil {
				t.Errorf("%s accepted %q but cannot encode it: %v", genType, payload, err)
			}
		}
	})
}

// TestErrorCorrectionLevelsAreAccepted keeps the advertised levels and Encode
// in agreement, including the default listed first.
func TestErrorCorrectionLevelsAreAccepted(t *testing.T) {
	payload := map[string]string{TypeQR: "BAM", TypeMicroQR: "BAM", TypeCode128: "BAM"}
	for _, genType := range AllowedGeneratorTypes() {
		g, _ := Lookup(genType)
		levels := g.ErrorCorrectionLevels()
		for _, level := range levels {
			if _, err := g.Encode(payload[genType], level); err != nil {
				t.Errorf("%s rejects advertised level %q: %v", genType, level, err)
			}
		}
		def, err := g.Encode(payload[genType], "")
		if err != nil {
			t.Fatalf("%s default: %v", genType, err)
		}
		if len(levels) > 0 {
			first, _ := g.Encode(payload[genType], levels[0])
			if !reflect.DeepEqual(def, first) {
				t.Errorf("%s: default level differs from first advertised level %q", genType, levels[0])
			}
		}
	}
}
