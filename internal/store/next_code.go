package store

import (
	"fmt"
	"regexp"
	"strconv"
)

// trailingNumber splits a code into what comes before its last run of
// digits, and the digits.
var trailingNumber = regexp.MustCompile(`^(.*?)([0-9]+)$`)

// NextCode suggests the code to give the next asset of a generator type:
// one more than the highest code of that type ending in a number, keeping
// its prefix and zero-padding (after HS-105, HS-106; after BIN-0099,
// BIN-0100), and not taken. It is empty when no code of the type ends in a
// number.
func (s *Store) NextCode(generatorType string) (string, error) {
	return s.nextCode(generatorType, nil)
}

// NextCodeLike suggests the next code of a generator type in the series of
// code: one more than the highest code with its prefix, as NextCode does,
// so that after SP-014 comes SP-015 however high the HS- codes go. It is
// empty when code does not end in a number.
func (s *Store) NextCodeLike(generatorType, code string) (string, error) {
	m := trailingNumber.FindStringSubmatch(code)
	if m == nil {
		return "", nil
	}
	return s.nextCode(generatorType, &m[1])
}

// nextCode is NextCode among the codes with prefix, or all when it is nil.
func (s *Store) nextCode(generatorType string, only *string) (string, error) {
	rows, err := s.Query("SELECT assigned_code FROM asset_generator_keys WHERE generator_type = ?", generatorType)
	if err != nil {
		return "", fmt.Errorf("reading codes: %w", err)
	}
	defer rows.Close()
	taken := map[string]bool{}
	var prefix, digits string
	var highest uint64
	for rows.Next() {
		var code string
		if err := rows.Scan(&code); err != nil {
			return "", fmt.Errorf("scanning code: %w", err)
		}
		taken[code] = true
		m := trailingNumber.FindStringSubmatch(code)
		if m == nil || (only != nil && m[1] != *only) {
			continue
		}
		n, err := strconv.ParseUint(m[2], 10, 64)
		if err != nil {
			continue
		}
		if digits == "" || n > highest {
			prefix, digits, highest = m[1], m[2], n
		}
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	if digits == "" {
		return "", nil
	}
	for n := highest + 1; ; n++ {
		code := fmt.Sprintf("%s%0*d", prefix, len(digits), n)
		if !taken[code] {
			return code, nil
		}
	}
}
