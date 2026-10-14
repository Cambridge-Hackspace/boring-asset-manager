package api

import (
	"strings"
	"testing"
)

func TestCleanFilename(t *testing.T) {
	wrench := strings.Repeat("🔧", 255)
	for in, want := range map[string]string{
		"logo.png":                            "logo.png",
		`C:\Users\ada\logo.png`:               "logo.png",
		"../../etc/logo.png":                  "logo.png",
		"lo" + string(rune(0)) + "go.png":     "logo.png",
		"tab\tbed.png":                        "tabbed.png",
		"bad" + string([]byte{0xff}) + ".png": "bad.png",
		"  ":                                  "image",
		wrench + "🔧.png":                      wrench,
	} {
		if got := cleanFilename(in); got != want {
			t.Errorf("cleanFilename(%q) = %q, want %q", in, got, want)
		}
	}
}
