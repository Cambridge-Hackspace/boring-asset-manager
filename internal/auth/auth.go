package auth

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

var (
	ErrInvalidCredentials = errors.New("invalid email or password")
	ErrUserInactive       = errors.New("account is deactivated")
)

const (
	BcryptCost = 12
)

// HashPassword hashes a raw password using bcrypt with standard secure cost.
func HashPassword(password string) (string, error) {
	if len(password) == 0 {
		return "", errors.New("password cannot be empty")
	}
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), BcryptCost)
	if err != nil {
		return "", fmt.Errorf("generating password hash: %w", err)
	}
	return string(bytes), nil
}

// CheckPassword verifies a raw password against the stored bcrypt hash.
func CheckPassword(password, hash string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	return err == nil
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
