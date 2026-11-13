package generators

import (
	"fmt"
	"strings"
)

// TypeNFC is the code type of an NFC tag's serial number (UID). It is a code
// an asset is found by, not a symbology: nothing prints it, so it is not in
// the registry of generators that templates draw.
const TypeNFC = "nfc"

// nfcUIDLengths are the lengths, in bytes, of the UIDs ISO/IEC 14443 tags
// have: single, double and triple size.
var nfcUIDLengths = map[int]bool{4: true, 7: true, 10: true}

// CodeTypes lists every type of code an asset may have: the symbologies
// labels print, then an NFC tag's serial number.
func CodeTypes() []string {
	return append(AllowedGeneratorTypes(), TypeNFC)
}

// IsCodeType checks whether an asset may have a code of a type.
func IsCodeType(t string) bool {
	return t == TypeNFC || IsValidGeneratorType(t)
}

// HasSeries says whether codes of a type run in series, so that one can be
// followed by the next: an NFC tag's serial number is fixed by its maker.
func HasSeries(t string) bool {
	return t != TypeNFC
}

// NormalizeCode puts a code in the form it is stored and looked up in:
// trimmed, and an NFC tag's serial number as upper-case hex bytes joined by
// colons, however it was written. A code that is no NFC serial number is
// only trimmed, for validation to refuse.
func NormalizeCode(genType, code string) string {
	code = strings.TrimSpace(code)
	if genType != TypeNFC {
		return code
	}
	if uid, ok := ParseNFCUID(code); ok {
		return uid
	}
	return code
}

// ParseNFCUID reads an NFC tag's serial number written as hex, its bytes
// run together or separated by colons, spaces or dashes, in either case, and
// returns it as upper-case bytes joined by colons. It is false for anything
// else, or a length no tag has.
func ParseNFCUID(s string) (string, bool) {
	s = strings.TrimSpace(s)
	var bytes []string
	if strings.ContainsAny(s, ": -") {
		for _, b := range strings.FieldsFunc(s, func(r rune) bool { return r == ':' || r == ' ' || r == '-' }) {
			if len(b) == 1 {
				b = "0" + b
			}
			bytes = append(bytes, b)
		}
	} else {
		if len(s)%2 != 0 {
			return "", false
		}
		for i := 0; i < len(s); i += 2 {
			bytes = append(bytes, s[i:i+2])
		}
	}
	if !nfcUIDLengths[len(bytes)] {
		return "", false
	}
	for i, b := range bytes {
		if len(b) != 2 || !isHex(b[0]) || !isHex(b[1]) {
			return "", false
		}
		bytes[i] = strings.ToUpper(b)
	}
	return strings.Join(bytes, ":"), true
}

func isHex(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}

// validateNFC accepts an NFC tag's serial number in any form ParseNFCUID
// reads.
func validateNFC(code string) error {
	if _, ok := ParseNFCUID(code); !ok {
		return fmt.Errorf("%w: an NFC tag's serial number is 4, 7 or 10 bytes in hex, such as 04:A2:3B:1C:5D:80:00", ErrInvalidCharacterSet)
	}
	return nil
}
