package fieldkey

import (
	"strings"
	"testing"
)

func TestValid(t *testing.T) {
	for key, want := range map[string]bool{
		"location": true, " room_2 ": true, strings.Repeat("a", 64): true,
		"": false, "  ": false, "Room": false, "a-b": false, "field.x": false, strings.Repeat("a", 65): false,
	} {
		if Valid(key) != want {
			t.Errorf("Valid(%q) = %v, want %v", key, !want, want)
		}
	}
}
