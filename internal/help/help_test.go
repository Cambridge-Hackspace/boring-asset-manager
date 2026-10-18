package help

import (
	"strings"
	"testing"
)

func TestTopics(t *testing.T) {
	topics := Topics()
	slugs := map[string]bool{}
	for _, tp := range topics {
		slugs[tp.Slug] = true
		if tp.Title == "" || strings.TrimSpace(tp.Body) == "" || strings.HasPrefix(tp.Body, "#") {
			t.Errorf("%s: title %q, body %q", tp.Slug, tp.Title, tp.Body[:min(len(tp.Body), 40)])
		}
	}
	for _, want := range []string{"start", "assets", "labels", "account"} {
		if !slugs[want] {
			t.Errorf("no %s topic among %d", want, len(topics))
		}
	}
	if s := Topics()[0]; s.Slug != "account" || s.Title != "Your account" {
		t.Errorf("first topic: %s %q", s.Slug, s.Title)
	}
}
