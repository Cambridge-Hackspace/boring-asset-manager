package generators

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

const (
	TypeQR      = "qr"
	TypeMicroQR = "micro_qr"
	TypeCode128 = "code128"
)

var (
	ErrInvalidGeneratorType = errors.New("unsupported generator type: must be 'qr', 'micro_qr', or 'code128'")
	ErrInvalidCodeLength    = errors.New("code length violates generator type capacity constraints")
	ErrInvalidCharacterSet  = errors.New("code contains invalid characters for generator type")

	// Micro QR alphanumeric charset: 0-9, A-Z, space, $, %, *, +, -, ., /, :
	microQRCharsetRegex = regexp.MustCompile(`^[0-9A-Z $%*+\-./:]+$`)
	microQRNumericRegex = regexp.MustCompile(`^[0-9]+$`)
)

// Symbol capacity limits at the lowest error-correction level.
const (
	// MicroQRMaxNumeric is the M4-L numeric-mode capacity.
	MicroQRMaxNumeric = 35
	// MicroQRMaxAlphanumeric is the M4-L alphanumeric-mode capacity.
	MicroQRMaxAlphanumeric = 21
	// QRMaxBytes is the version 40-L byte-mode capacity.
	QRMaxBytes = 2953
)

// AllowedGeneratorTypes returns supported generator type identifiers.
func AllowedGeneratorTypes() []string {
	return []string{TypeQR, TypeMicroQR, TypeCode128}
}

// IsValidGeneratorType checks whether a type is supported.
func IsValidGeneratorType(t string) bool {
	switch t {
	case TypeQR, TypeMicroQR, TypeCode128:
		return true
	default:
		return false
	}
}

// ValidateGeneratorCode validates code length and character set against the generator type's physical matrix and density constraints.
func ValidateGeneratorCode(genType, code string) error {
	code = strings.TrimSpace(code)
	if code == "" {
		return errors.New("assigned code cannot be empty")
	}

	switch genType {
	case TypeMicroQR:
		if !microQRCharsetRegex.MatchString(code) {
			return fmt.Errorf("%w: micro_qr only supports uppercase alphanumeric and ' $%%*+-./:' characters", ErrInvalidCharacterSet)
		}
		// The largest symbol (M4-L) holds 35 digits in numeric mode but only 21
		// characters once any non-digit forces alphanumeric mode.
		length := len(code)
		if microQRNumericRegex.MatchString(code) {
			if length > MicroQRMaxNumeric {
				return fmt.Errorf("%w: numeric micro_qr capacity is 1-%d digits (got %d)", ErrInvalidCodeLength, MicroQRMaxNumeric, length)
			}
			return nil
		}
		if length > MicroQRMaxAlphanumeric {
			return fmt.Errorf("%w: alphanumeric micro_qr capacity is 1-%d characters (got %d)", ErrInvalidCodeLength, MicroQRMaxAlphanumeric, length)
		}
		return nil

	case TypeQR:
		if !utf8.ValidString(code) {
			return fmt.Errorf("%w: qr payload must be valid UTF-8", ErrInvalidCharacterSet)
		}
		// Capacity is bounded by encoded bytes, not characters: multi-byte UTF-8
		// runes consume several byte-mode codewords each.
		if length := len(code); length > QRMaxBytes {
			return fmt.Errorf("%w: qr payload must be 1-%d bytes (got %d)", ErrInvalidCodeLength, QRMaxBytes, length)
		}
		return nil

	case TypeCode128:
		length := len(code)
		if length < 1 || length > 80 {
			return fmt.Errorf("%w: code128 length must be 1-80 characters (got %d)", ErrInvalidCodeLength, length)
		}
		for i := 0; i < len(code); i++ {
			c := code[i]
			if c < 32 || c > 126 {
				return fmt.Errorf("%w: code128 only supports standard printable ASCII (32-126)", ErrInvalidCharacterSet)
			}
		}
		return nil

	default:
		return fmt.Errorf("%w: %q", ErrInvalidGeneratorType, genType)
	}
}
