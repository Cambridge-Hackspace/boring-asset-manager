package auth

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"

	"golang.org/x/crypto/bcrypt"
)

var (
	ErrInvalidCredentials = errors.New("invalid email or password")
	ErrUserInactive       = errors.New("account is deactivated")
)

// BcryptCost is the cost new password hashes use. Test packages that sign
// people in lower it to bcrypt.MinCost: under the race detector, one hash or
// check at 12 takes seconds, and the API suite ran past go test's timeout.
var BcryptCost = 12

// Password length limits, in bytes. bcrypt cannot use more than 72.
const (
	MinPasswordLength = 8
	MaxPasswordLength = 72
)

// ErrPasswordPolicy is wrapped when a new password breaks the rules.
var ErrPasswordPolicy = errors.New("password does not meet the requirements")

// ValidatePassword checks a new password against the length limits.
func ValidatePassword(password string) error {
	if len(password) < MinPasswordLength || len(password) > MaxPasswordLength {
		return fmt.Errorf("%w: it must be %d-%d bytes long", ErrPasswordPolicy, MinPasswordLength, MaxPasswordLength)
	}
	return nil
}

// HashPassword hashes a new password using bcrypt with standard secure cost,
// after checking it with ValidatePassword.
func HashPassword(password string) (string, error) {
	if err := ValidatePassword(password); err != nil {
		return "", err
	}
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), BcryptCost)
	if err != nil {
		return "", fmt.Errorf("generating password hash: %w", err)
	}
	return string(bytes), nil
}

// CheckPassword verifies a raw password against the stored bcrypt hash.
func CheckPassword(password, hash string) bool {
	err := compareHash([]byte(hash), []byte(password))
	return err == nil
}

// compareHash is bcrypt's comparison, counted.
func compareHash(hash, password []byte) error {
	comparisons.Add(1)
	return bcrypt.CompareHashAndPassword(hash, password)
}

var comparisons atomic.Int64

// Comparisons is how many passwords have been checked against a hash, for
// tests that a sign-in does the same work whether or not its email has an
// account.
func Comparisons() int64 { return comparisons.Load() }

// dummy is a hash of no one's password, made at BcryptCost when first
// needed.
var dummy struct {
	once sync.Once
	hash []byte
}

// CheckNoPassword spends as long as CheckPassword does on a real account's
// hash, for a sign-in naming no account, so how long the answer takes does
// not say whether an email has one (#246). It always fails.
func CheckNoPassword(password string) bool {
	dummy.once.Do(func() {
		dummy.hash, _ = bcrypt.GenerateFromPassword([]byte("no one's password"), BcryptCost)
	})
	_ = compareHash(dummy.hash, []byte(password))
	return false
}

// GenerateSessionToken creates a cryptographically secure random session ID (32 bytes / 64 hex characters).
func GenerateSessionToken() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("reading random bytes for session token: %w", err)
	}
	return hex.EncodeToString(bytes), nil
}

// NormalizeEmail trims whitespace and converts email to lowercase for case-insensitive matching.
func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}
