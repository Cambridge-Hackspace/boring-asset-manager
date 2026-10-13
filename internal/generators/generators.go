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
		// Micro QR M1-M4 alphanumeric capacity is capped at 35 characters
		length := utf8.RuneCountInString(code)
		if length < 1 || length > 35 {
			return fmt.Errorf("%w: micro_qr capacity is 1-35 characters (got %d)", ErrInvalidCodeLength, length)
		}
		if !microQRCharsetRegex.MatchString(code) {
			return fmt.Errorf("%w: micro_qr only supports uppercase alphanumeric and ' $%%*+-./:' characters", ErrInvalidCharacterSet)
		}
		return nil

	case TypeQR:
		length := utf8.RuneCountInString(code)
		if length < 1 || length > 2048 {
			return fmt.Errorf("%w: qr payload must be 1-2048 characters (got %d)", ErrInvalidCodeLength, length)
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
