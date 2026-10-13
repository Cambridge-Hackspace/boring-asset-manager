// Package generators maps generator types to barcode encoders behind one
// interface, and owns the rules for which codes an asset may be assigned.
//
// The encoders live in their own packages (qr, microqr, code128) and know
// nothing of this registry; each adapter here translates between them and the
// common Generator interface.
package generators

import (
	"errors"
	"fmt"
	"strings"
)

const (
	TypeQR      = "qr"
	TypeMicroQR = "micro_qr"
	TypeCode128 = "code128"
)

var (
	ErrEmptyCode              = errors.New("assigned code cannot be empty")
	ErrInvalidGeneratorType   = errors.New("unsupported generator type: must be 'qr', 'micro_qr', or 'code128'")
	ErrInvalidCodeLength      = errors.New("code length violates generator type capacity constraints")
	ErrInvalidCharacterSet    = errors.New("code contains invalid characters for generator type")
	ErrInvalidErrorCorrection = errors.New("unsupported error correction level for generator type")
)

// Symbol is an encoded barcode as a grid of modules. Linear symbologies
// (Code 128) have a single row, which layout stretches to the element height.
type Symbol struct {
	Cols, Rows int
	// QuietZone is the standard light margin on every side, in modules.
	QuietZone int
	// Dark holds Cols×Rows module colors in row-major order.
	Dark []bool
}

// Generator validates and encodes payloads for one symbology.
type Generator interface {
	// Type returns the generator type identifier, such as "qr".
	Type() string
	// Validate reports whether payload may be assigned to an asset as a code of
	// this type. Every payload it accepts encodes at the lowest error
	// correction level.
	Validate(payload string) error
	// Encode validates payload and encodes it at the given error correction
	// level; "" selects the generator's default.
	Encode(payload string, errorCorrection string) (Symbol, error)
}

var registry = map[string]Generator{
	TypeQR:      qrGenerator{},
	TypeMicroQR: microQRGenerator{},
	TypeCode128: code128Generator{},
}

// Lookup returns the generator for a type.
func Lookup(genType string) (Generator, error) {
	g, ok := registry[genType]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrInvalidGeneratorType, genType)
	}
	return g, nil
}

// AllowedGeneratorTypes returns supported generator type identifiers.
func AllowedGeneratorTypes() []string {
	return []string{TypeQR, TypeMicroQR, TypeCode128}
}

// IsValidGeneratorType checks whether a type is supported.
func IsValidGeneratorType(t string) bool {
	_, ok := registry[t]
	return ok
}

// ValidateGeneratorCode validates a code assigned to an asset. Surrounding
// whitespace is ignored, matching how the store normalizes codes.
func ValidateGeneratorCode(genType, code string) error {
	code = strings.TrimSpace(code)
	if code == "" {
		return ErrEmptyCode
	}
	g, err := Lookup(genType)
	if err != nil {
		return err
	}
	return g.Validate(code)
}
