package epc

import (
	"errors"
	"strings"
	"testing"
)

// The GIAI examples are published ones: Zebra's DS8 RFID data transmission
// format table (docs.zebra.com, "RFID Data Transmission Format") and
// epcpy's GIAI documentation (epcpy-tools.readthedocs.io).
func TestGIAIMatchesPublishedExamples(t *testing.T) {
	for _, c := range []struct {
		name   string
		scheme Scheme
		code   string
		epc    string
	}{
		{"Zebra GIAI-96", Scheme{Kind: SchemeGIAI96, CompanyPrefix: "000123456789"}, "123456789", "3400001D6F345400075BCD15"},
		{"epcpy GIAI-96", Scheme{Kind: SchemeGIAI96, CompanyPrefix: "000200"}, "50", "341800320000000000000032"},
		{"Zebra GIAI-202", Scheme{Kind: SchemeGIAI202, CompanyPrefix: "0614141", Filter: 3}, "32a/b", "3874257BF59B2C2BF10000000000000000000000000000000000"},
	} {
		got, err := c.scheme.Encode(c.code)
		if err != nil || got != c.epc {
			t.Errorf("%s: Encode(%q) = %s, %v; want %s", c.name, c.code, got, err, c.epc)
		}
		if back, ok := c.scheme.Decode(c.epc); !ok || back != c.code {
			t.Errorf("%s: Decode = %q, %v; want %q", c.name, back, ok, c.code)
		}
		// A reader may report it in lowercase.
		if back, ok := c.scheme.Decode(strings.ToLower(c.epc)); !ok || back != c.code {
			t.Errorf("%s: Decode in lowercase = %q, %v", c.name, back, ok)
		}
	}
}

func TestTextEPCsAreTheCodesOwnCharacters(t *testing.T) {
	s := Scheme{Kind: SchemeText}
	for code, epc := range map[string]string{
		"HS-105":                 "48532D313035",
		"BIN-0099":               "42494E2D30303939",
		"A":                      "4100",
		"drill 3":                "6472696C6C203300",
		strings.Repeat("x", 62):  strings.Repeat("78", 62),
		"https://bam.example/a1": "68747470733A2F2F62616D2E6578616D706C652F6131",
	} {
		got, err := s.Encode(code)
		if err != nil || got != epc {
			t.Errorf("Encode(%q) = %s, %v; want %s", code, got, err, epc)
		}
		if back, ok := s.Decode(epc); !ok || back != code {
			t.Errorf("Decode(%s) = %q, %v; want %q", epc, back, ok, code)
		}
	}
}

func TestCodesThatDoNotFitAreRefused(t *testing.T) {
	giai96 := Scheme{Kind: SchemeGIAI96, CompanyPrefix: "0614141"}
	giai202 := Scheme{Kind: SchemeGIAI202, CompanyPrefix: "0614141"}
	text := Scheme{Kind: SchemeText}
	for _, c := range []struct {
		scheme Scheme
		code   string
	}{
		{giai96, "HS-105"},
		{giai96, "0105"},
		// 58 bits hold up to 288230376151711743.
		{giai96, "288230376151711744"},
		{giai202, "HS 105"},
		{giai202, "é"},
		// A 7-digit prefix leaves 23 characters.
		{giai202, strings.Repeat("A", 24)},
		{text, strings.Repeat("x", 63)},
		{text, "café"},
		{text, ""},
	} {
		if got, err := c.scheme.Encode(c.code); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s Encode(%q) = %s, %v; want an error", c.scheme.Kind, c.code, got, err)
		}
	}
	if got, err := giai96.Encode("288230376151711743"); err != nil || len(got) != 24 {
		t.Errorf("the largest GIAI-96 reference = %s, %v", got, err)
	}
	if got, err := giai202.Encode(strings.Repeat("A", 23)); err != nil || len(got) != 52 {
		t.Errorf("the longest GIAI-202 reference = %s, %v", got, err)
	}
	if got, err := giai96.Encode("0"); err != nil {
		t.Errorf("GIAI-96 of 0 = %s, %v", got, err)
	}
}

func TestSchemesAreChecked(t *testing.T) {
	for _, s := range []Scheme{
		{Kind: "sgtin-96"},
		{Kind: SchemeGIAI96},
		{Kind: SchemeGIAI96, CompanyPrefix: "12345"},
		{Kind: SchemeGIAI96, CompanyPrefix: "1234567890123"},
		{Kind: SchemeGIAI202, CompanyPrefix: "06141A1"},
		{Kind: SchemeGIAI202, CompanyPrefix: "0614141", Filter: 8},
		{Kind: SchemeGIAI202, CompanyPrefix: "0614141", Filter: -1},
	} {
		if err := s.Check(); !errors.Is(err, ErrInvalid) {
			t.Errorf("%+v: Check = %v", s, err)
		}
		if _, ok := s.Decode("3400001D6F345400075BCD15"); ok {
			t.Errorf("%+v decoded an EPC", s)
		}
	}
}

func TestOnlyThisInstancesTagsDecode(t *testing.T) {
	ours := Scheme{Kind: SchemeGIAI96, CompanyPrefix: "000123456789"}
	for _, epc := range []string{
		// Another company's prefix.
		"341800320000000000000032",
		// The right prefix, but filter 1.
		"3420001D6F345400075BCD15",
		// A GIAI-202 under a GIAI-96 scheme.
		"3874257BF59B2C2BF10000000000000000000000000000000000",
		// Not hexadecimal, odd, or the wrong length.
		"HS-105", "3400001D6F345400075BCD1", "3400001D6F345400075BCD1500",
	} {
		if code, ok := ours.Decode(epc); ok {
			t.Errorf("Decode(%s) = %q", epc, code)
		}
	}
	text := Scheme{Kind: SchemeText}
	for _, epc := range []string{"0000", "FF00", "48530A31"} {
		if code, ok := text.Decode(epc); ok {
			t.Errorf("text Decode(%s) = %q", epc, code)
		}
	}
	giai202 := Scheme{Kind: SchemeGIAI202, CompanyPrefix: "0614141", Filter: 3}
	if code, ok := giai202.Decode("3874257BF4" + strings.Repeat("0", 42)); ok {
		t.Errorf("a GIAI-202 with no reference decoded as %q", code)
	}
}
