package money

import (
	"errors"
	"testing"
)

func TestParseAndFormatKeepPricesExactly(t *testing.T) {
	for _, c := range []struct {
		in, currency string
		minor        int64
		out          string
	}{
		{"12.50", "GBP", 1250, "12.50"},
		{"12.5", "GBP", 1250, "12.50"},
		{"12", "EUR", 1200, "12.00"},
		{"0.10", "USD", 10, "0.10"},
		{".1", "USD", 10, "0.10"},
		{"0", "USD", 0, "0.00"},
		{"0019.99", "USD", 1999, "19.99"},
		{"1300", "JPY", 1300, "1300"},
		{"1.234", "KWD", 1234, "1.234"},
		{"9999999999999.99", "GBP", 999_999_999_999_999, "9999999999999.99"},
		// The sum of ten 0.10s is 1.00 exactly, as a float's would not be.
		{"1.00", "GBP", 100, "1.00"},
	} {
		got, err := Parse(c.in, c.currency)
		if err != nil || got != c.minor {
			t.Errorf("Parse(%q, %s) = %d, %v; want %d", c.in, c.currency, got, err, c.minor)
			continue
		}
		if s := Format(got, c.currency); s != c.out {
			t.Errorf("Format(%d, %s) = %q, want %q", got, c.currency, s, c.out)
		}
	}
	var sum int64
	for i := 0; i < 10; i++ {
		tenth, _ := Parse("0.10", "GBP")
		sum += tenth
	}
	if Format(sum, "GBP") != "1.00" {
		t.Errorf("ten 0.10s make %s", Format(sum, "GBP"))
	}
}

func TestParseRefusesWhatIsNotAPrice(t *testing.T) {
	for _, c := range []struct{ in, currency string }{
		{"", "GBP"}, {"12.", "GBP"}, {"12.345", "GBP"}, {"12.5", "JPY"}, {"-1", "GBP"}, {"£12", "GBP"},
		{"1,200", "GBP"}, {"1e3", "GBP"}, {"12.5.0", "GBP"}, {"10000000000000.00", "GBP"}, {"99999999999999999999", "JPY"},
	} {
		if _, err := Parse(c.in, c.currency); !errors.Is(err, ErrInvalidPrice) {
			t.Errorf("Parse(%q, %s): %v, want ErrInvalidPrice", c.in, c.currency, err)
		}
	}
}

func TestNormalizeCurrency(t *testing.T) {
	for in, want := range map[string]string{"gbp": "GBP", " EUR ": "EUR", "": ""} {
		if got, err := NormalizeCurrency(in); err != nil || got != want {
			t.Errorf("NormalizeCurrency(%q) = %q, %v", in, got, err)
		}
	}
	for _, in := range []string{"GB", "GBPX", "G8P", "£££"} {
		if _, err := NormalizeCurrency(in); !errors.Is(err, ErrInvalidCurrency) {
			t.Errorf("NormalizeCurrency(%q): %v", in, err)
		}
	}
	if Places("JPY") != 0 || Places("GBP") != 2 || Places("BHD") != 3 || Places("XYZ") != 2 {
		t.Error("places")
	}
}
