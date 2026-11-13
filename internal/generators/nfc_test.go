package generators

import (
	"errors"
	"slices"
	"testing"
)

func TestNFCSerialNumbers(t *testing.T) {
	for in, want := range map[string]string{
		"04:a2:3b:1c:5d:80:00":          "04:A2:3B:1C:5D:80:00",
		" 04A23B1C5D8000 ":              "04:A2:3B:1C:5D:80:00",
		"04 a2 3b 1c 5d 80 00":          "04:A2:3B:1C:5D:80:00",
		"de-ad-be-ef":                   "DE:AD:BE:EF",
		"4:a2:3b:1c":                    "04:A2:3B:1C",
		"0102030405060708090a":          "01:02:03:04:05:06:07:08:09:0A",
		"01:02:03:04:05:06:07:08:09:0a": "01:02:03:04:05:06:07:08:09:0A",
	} {
		if err := ValidateGeneratorCode(TypeNFC, in); err != nil {
			t.Errorf("ValidateGeneratorCode(nfc, %q): %v", in, err)
		}
		if got := NormalizeCode(TypeNFC, in); got != want {
			t.Errorf("NormalizeCode(nfc, %q) = %q, want %q", in, got, want)
		}
	}
	for _, in := range []string{"04A23B1C5", "04:A2:3B", "04:A2:3B:1C:5D", "ZZ:A2:3B:1C", "HS-106", "04:A2:3B:1C:5D:80:00:11", "04::A2"} {
		if err := ValidateGeneratorCode(TypeNFC, in); !errors.Is(err, ErrInvalidCharacterSet) {
			t.Errorf("ValidateGeneratorCode(nfc, %q) = %v, want ErrInvalidCharacterSet", in, err)
		}
	}
	// Other codes are only trimmed.
	if got := NormalizeCode(TypeCode128, " de:ad:be:ef "); got != "de:ad:be:ef" {
		t.Errorf("NormalizeCode(code128) = %q", got)
	}
}

// An asset may have an NFC tag's serial number as a code, but no label
// prints one.
func TestNFCIsACodeTypeButNoSymbology(t *testing.T) {
	if !IsCodeType(TypeNFC) || !slices.Contains(CodeTypes(), TypeNFC) {
		t.Error("nfc is not a code type")
	}
	if IsValidGeneratorType(TypeNFC) || slices.Contains(AllowedGeneratorTypes(), TypeNFC) {
		t.Error("nfc is a generator labels draw")
	}
	if _, err := Lookup(TypeNFC); !errors.Is(err, ErrInvalidGeneratorType) {
		t.Errorf("Lookup(nfc) = %v", err)
	}
	if HasSeries(TypeNFC) || !HasSeries(TypeQR) {
		t.Error("series")
	}
	if err := ValidateGeneratorCode("ean13", "123"); !errors.Is(err, ErrInvalidCodeType) {
		t.Errorf("ValidateGeneratorCode(ean13) = %v", err)
	}
	var found bool
	for _, g := range Describe() {
		if g.Type == TypeNFC {
			found = true
			if g.Printable || g.Name != "NFC tag" {
				t.Errorf("Describe nfc = %+v", g)
			}
		} else if !g.Printable {
			t.Errorf("%s is not printable", g.Type)
		}
	}
	if !found {
		t.Error("Describe leaves nfc out")
	}
}
