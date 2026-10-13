package generators

import (
	"fmt"
	"regexp"
	"unicode/utf8"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/generators/code128"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/generators/microqr"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/generators/qr"
)

// Symbol capacity limits at the lowest error-correction level.
const (
	// MicroQRMaxNumeric is the M4-L numeric-mode capacity.
	MicroQRMaxNumeric = 35
	// MicroQRMaxAlphanumeric is the M4-L alphanumeric-mode capacity.
	MicroQRMaxAlphanumeric = 21
	// QRMaxBytes is the version 40-L byte-mode capacity.
	QRMaxBytes = 2953
	// Code128MaxLength caps Code 128 codes so labels stay scannable.
	Code128MaxLength = 80
)

var (
	// Micro QR alphanumeric charset: 0-9, A-Z, space, $, %, *, +, -, ., /, :
	microQRCharsetRegex = regexp.MustCompile(`^[0-9A-Z $%*+\-./:]+$`)
	microQRNumericRegex = regexp.MustCompile(`^[0-9]+$`)
)

// --- QR Code ---

type qrGenerator struct{}

func (qrGenerator) Type() string { return TypeQR }

func (qrGenerator) Validate(payload string) error {
	if payload == "" {
		return ErrEmptyCode
	}
	if !utf8.ValidString(payload) {
		return fmt.Errorf("%w: qr payload must be valid UTF-8", ErrInvalidCharacterSet)
	}
	// Capacity is bounded by encoded bytes, not characters: multi-byte UTF-8
	// runes consume several byte-mode codewords each.
	if length := len(payload); length > QRMaxBytes {
		return fmt.Errorf("%w: qr payload must be 1-%d bytes (got %d)", ErrInvalidCodeLength, QRMaxBytes, length)
	}
	return nil
}

var qrLevels = map[string]qr.Level{"": qr.M, "L": qr.L, "M": qr.M, "Q": qr.Q, "H": qr.H}

func (qrGenerator) ErrorCorrectionLevels() []string { return []string{"M", "L", "Q", "H"} }

func (g qrGenerator) Encode(payload, errorCorrection string) (Symbol, error) {
	level, ok := qrLevels[errorCorrection]
	if !ok {
		return Symbol{}, fmt.Errorf("%w: qr supports L, M, Q and H (got %q)", ErrInvalidErrorCorrection, errorCorrection)
	}
	if err := g.Validate(payload); err != nil {
		return Symbol{}, err
	}
	code, err := qr.Encode(payload, level)
	if err != nil {
		return Symbol{}, fmt.Errorf("%w: %v", ErrInvalidCodeLength, err)
	}
	return Symbol{Cols: code.Size, Rows: code.Size, QuietZone: qr.QuietZone, Dark: code.Modules}, nil
}

// --- Micro QR Code ---

type microQRGenerator struct{}

func (microQRGenerator) Type() string { return TypeMicroQR }

func (microQRGenerator) Validate(payload string) error {
	if payload == "" {
		return ErrEmptyCode
	}
	if !microQRCharsetRegex.MatchString(payload) {
		return fmt.Errorf("%w: micro_qr only supports uppercase alphanumeric and ' $%%*+-./:' characters", ErrInvalidCharacterSet)
	}
	// The largest symbol (M4-L) holds 35 digits in numeric mode but only 21
	// characters once any non-digit forces alphanumeric mode.
	length := len(payload)
	if microQRNumericRegex.MatchString(payload) {
		if length > MicroQRMaxNumeric {
			return fmt.Errorf("%w: numeric micro_qr capacity is 1-%d digits (got %d)", ErrInvalidCodeLength, MicroQRMaxNumeric, length)
		}
		return nil
	}
	if length > MicroQRMaxAlphanumeric {
		return fmt.Errorf("%w: alphanumeric micro_qr capacity is 1-%d characters (got %d)", ErrInvalidCodeLength, MicroQRMaxAlphanumeric, length)
	}
	return nil
}

var microQRLevels = map[string]microqr.Level{"": microqr.L, "L": microqr.L, "M": microqr.M, "Q": microqr.Q}

func (microQRGenerator) ErrorCorrectionLevels() []string { return []string{"L", "M", "Q"} }

func (g microQRGenerator) Encode(payload, errorCorrection string) (Symbol, error) {
	level, ok := microQRLevels[errorCorrection]
	if !ok {
		return Symbol{}, fmt.Errorf("%w: micro_qr supports L, M and Q (got %q)", ErrInvalidErrorCorrection, errorCorrection)
	}
	if err := g.Validate(payload); err != nil {
		return Symbol{}, err
	}
	// Validation guarantees M4-L capacity; a higher level may still not fit.
	code, err := microqr.Encode(payload, level)
	if err != nil {
		return Symbol{}, fmt.Errorf("%w: %v", ErrInvalidCodeLength, err)
	}
	return Symbol{Cols: code.Size, Rows: code.Size, QuietZone: microqr.QuietZone, Dark: code.Modules}, nil
}

// --- Code 128 ---

type code128Generator struct{}

func (code128Generator) Type() string { return TypeCode128 }

func (code128Generator) Validate(payload string) error {
	if payload == "" {
		return ErrEmptyCode
	}
	if length := len(payload); length > Code128MaxLength {
		return fmt.Errorf("%w: code128 length must be 1-%d characters (got %d)", ErrInvalidCodeLength, Code128MaxLength, length)
	}
	for i := 0; i < len(payload); i++ {
		if c := payload[i]; c < 32 || c > 126 {
			return fmt.Errorf("%w: code128 only supports standard printable ASCII (32-126)", ErrInvalidCharacterSet)
		}
	}
	return nil
}

func (code128Generator) ErrorCorrectionLevels() []string { return nil }

func (g code128Generator) Encode(payload, errorCorrection string) (Symbol, error) {
	if errorCorrection != "" {
		return Symbol{}, fmt.Errorf("%w: code128 has no error correction levels (got %q)", ErrInvalidErrorCorrection, errorCorrection)
	}
	if err := g.Validate(payload); err != nil {
		return Symbol{}, err
	}
	code, err := code128.Encode(payload)
	if err != nil {
		return Symbol{}, fmt.Errorf("%w: %v", ErrInvalidCharacterSet, err)
	}
	return Symbol{Cols: len(code.Modules), Rows: 1, QuietZone: code128.QuietZone, Dark: code.Modules}, nil
}
