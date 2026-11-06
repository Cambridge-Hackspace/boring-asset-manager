// Package money keeps prices exactly (docs/data-model.md#purchases): as a
// whole number of a currency's minor units, such as pence or cents, never as
// a float, and written as a decimal with as many places as the currency has.
package money

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// ErrInvalidCurrency is a currency that is not three letters A to Z.
var ErrInvalidCurrency = errors.New("a currency is its three-letter ISO 4217 code, such as GBP, EUR or USD")

// ErrInvalidPrice is a price that is not a decimal of at most the places
// its currency has, or too large to keep.
var ErrInvalidPrice = errors.New("invalid price")

// exponents are the ISO 4217 currencies whose minor unit is not a
// hundredth: the number of decimal places each has. Every other code has
// two.
var exponents = map[string]int{
	"BIF": 0, "CLP": 0, "DJF": 0, "GNF": 0, "ISK": 0, "JPY": 0, "KMF": 0, "KRW": 0,
	"PYG": 0, "RWF": 0, "UGX": 0, "UYI": 0, "VND": 0, "VUV": 0, "XAF": 0, "XOF": 0, "XPF": 0,
	"BHD": 3, "IQD": 3, "JOD": 3, "KWD": 3, "LYD": 3, "OMR": 3, "TND": 3,
	"CLF": 4, "UYW": 4,
}

// Places is how many decimal places currency has: 2 for most.
func Places(currency string) int {
	if p, ok := exponents[currency]; ok {
		return p
	}
	return 2
}

// NormalizeCurrency trims and capitalizes a currency code and checks it is
// three letters; an empty one stays empty.
func NormalizeCurrency(c string) (string, error) {
	c = strings.ToUpper(strings.TrimSpace(c))
	if c == "" {
		return "", nil
	}
	if len(c) != 3 {
		return "", ErrInvalidCurrency
	}
	for _, r := range c {
		if r < 'A' || r > 'Z' {
			return "", ErrInvalidCurrency
		}
	}
	return c, nil
}

// Parse reads a price written as a decimal, such as 12.5 or 1299.99, into
// currency's minor units. It may have fewer places than the currency, but
// not more, and no sign, grouping or symbol.
func Parse(s, currency string) (int64, error) {
	s = strings.TrimSpace(s)
	places := Places(currency)
	whole, frac, dotted := strings.Cut(s, ".")
	if whole == "" && frac == "" || dotted && frac == "" {
		return 0, fmt.Errorf("%w: write it as a number such as 12.50", ErrInvalidPrice)
	}
	for _, part := range []string{whole, frac} {
		for _, r := range part {
			if r < '0' || r > '9' {
				return 0, fmt.Errorf("%w: write it as a number such as 12.50, without a currency symbol", ErrInvalidPrice)
			}
		}
	}
	if len(frac) > places {
		if places == 0 {
			return 0, fmt.Errorf("%w: %s has no minor unit, so a price is whole", ErrInvalidPrice, currency)
		}
		return 0, fmt.Errorf("%w: %s has %d decimal places", ErrInvalidPrice, currency, places)
	}
	digits := strings.TrimLeft(whole+frac+strings.Repeat("0", places-len(frac)), "0")
	if digits == "" {
		return 0, nil
	}
	n, err := strconv.ParseInt(digits, 10, 64)
	if err != nil || n > MaxMinor {
		return 0, fmt.Errorf("%w: it is too large", ErrInvalidPrice)
	}
	return n, nil
}

// MaxMinor is the largest price kept, in minor units: fifteen digits, which
// a spreadsheet keeps exactly.
const MaxMinor = int64(999_999_999_999_999)

// Format writes minor units of currency as a decimal with the currency's
// places, such as 1299.99, or 1300 for yen.
func Format(minor int64, currency string) string {
	places := Places(currency)
	if places == 0 {
		return strconv.FormatInt(minor, 10)
	}
	sign := ""
	if minor < 0 {
		sign, minor = "-", -minor
	}
	unit := int64(math.Pow10(places))
	return fmt.Sprintf("%s%d.%0*d", sign, minor/unit, places, minor%unit)
}
