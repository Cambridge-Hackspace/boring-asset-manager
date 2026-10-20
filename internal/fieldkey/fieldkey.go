// Package fieldkey says what a custom field's key may be. It depends on
// nothing, so label templates can check the fields they bind without
// bringing the rest of the server into the editor's live renderer.
package fieldkey

import (
	"regexp"
	"strings"
)

var pattern = regexp.MustCompile(`^[a-z0-9_]+$`)

// Valid reports whether key, trimmed, is a field key: 1 to 64 lowercase
// letters, digits and underscores.
func Valid(key string) bool {
	key = strings.TrimSpace(key)
	return key != "" && len(key) <= 64 && pattern.MatchString(key)
}
