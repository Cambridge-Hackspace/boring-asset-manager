// Package textcheck holds the rules for text that BAM stores, which are the
// same on every database: it must be valid UTF-8, it may not contain a NUL
// character (PostgreSQL cannot store one), and lengths are counted in
// characters, as VARCHAR columns count them, not in bytes.
package textcheck

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

const (
	// MaxName is the longest name, label or code: VARCHAR(255).
	MaxName = 255
	// MaxValue is the longest field value. A field value of this many
	// four-byte characters still fits a MariaDB TEXT column.
	MaxValue = 10000
)

// Problem returns why s may not be stored where at most max characters fit,
// as a phrase to follow what s is ("is longer than 255 characters"), or ""
// when it may.
func Problem(s string, max int) string {
	switch {
	case !utf8.ValidString(s):
		return "is not valid UTF-8"
	case strings.IndexByte(s, 0) >= 0:
		return "contains a NUL character"
	case utf8.RuneCountInString(s) > max:
		return fmt.Sprintf("is longer than %d characters", max)
	}
	return ""
}

// Check returns an error naming what s is when s may not be stored.
func Check(what, s string, max int) error {
	if p := Problem(s, max); p != "" {
		return fmt.Errorf("%s %s", what, p)
	}
	return nil
}
