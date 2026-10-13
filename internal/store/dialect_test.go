package store

import (
	"testing"
)

func TestRebind(t *testing.T) {
	cases := []struct {
		name     string
		dialect  string
		query    string
		expected string
	}{
		{
			name:     "sqlite unchanged",
			dialect:  "sqlite",
			query:    "SELECT * FROM users WHERE email = ? AND is_active = ?",
			expected: "SELECT * FROM users WHERE email = ? AND is_active = ?",
		},
		{
			name:     "mysql unchanged",
			dialect:  "mariadb",
			query:    "SELECT * FROM users WHERE email = ? AND is_active = ?",
			expected: "SELECT * FROM users WHERE email = ? AND is_active = ?",
		},
		{
			name:     "postgres positional placeholders",
			dialect:  "postgres",
			query:    "INSERT INTO users (id, email, password_hash) VALUES (?, ?, ?)",
			expected: "INSERT INTO users (id, email, password_hash) VALUES ($1, $2, $3)",
		},
		{
			name:     "postgres preserves question mark in literal string",
			dialect:  "postgres",
			query:    "SELECT * FROM settings WHERE setting_key = ? AND description = 'is this valid?' AND active = ?",
			expected: "SELECT * FROM settings WHERE setting_key = $1 AND description = 'is this valid?' AND active = $2",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := Rebind(tc.query, tc.dialect)
			if result != tc.expected {
				t.Errorf("Rebind(%q, %q) = %q, expected %q", tc.query, tc.dialect, result, tc.expected)
			}
		})
	}
}
