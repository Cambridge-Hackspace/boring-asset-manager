package textcheck

import (
	"strings"
	"testing"
)

func TestProblem(t *testing.T) {
	for _, c := range []struct {
		s    string
		max  int
		want string
	}{
		{"", 3, ""},
		{"abc", 3, ""},
		{"abcd", 3, "is longer than 3 characters"},
		{strings.Repeat("🔧", 3), 3, ""}, // 12 bytes, 3 characters
		{strings.Repeat("🔧", 4), 3, "is longer than 3 characters"},
		{"a" + string(rune(0)) + "b", 3, "contains a NUL character"},
		{"a" + string([]byte{0xff}), 3, "is not valid UTF-8"},
	} {
		if got := Problem(c.s, c.max); got != c.want {
			t.Errorf("Problem(%q, %d) = %q, want %q", c.s, c.max, got, c.want)
		}
	}
	if err := Check("name", "abcd", 3); err == nil || err.Error() != "name is longer than 3 characters" {
		t.Errorf("Check: %v", err)
	}
}
