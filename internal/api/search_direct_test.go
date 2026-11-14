package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
)

// TestSearchDirect: the instance's setting is off until an admin turns it
// on, read by anyone who reads assets; each person chooses for themselves,
// or follows it, and their choice comes back with them when they sign in.
func TestSearchDirect(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	adminC := sessionCookie(t, srv, admin.ID)
	reader := userWithPermissions(t, srv, "reader@test.org", rbac.PermAssetsRead)

	settings := func(c *http.Cookie) (int, SearchSettings) {
		rec := doJSON(t, srv, c, http.MethodGet, "/api/v1/settings/search", "")
		var out SearchSettings
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return rec.Code, out
	}
	if code, got := settings(reader); code != http.StatusOK || got.Direct {
		t.Fatalf("the default: %d %+v", code, got)
	}
	if rec := doJSON(t, srv, reader, http.MethodPut, "/api/v1/settings/search", `{"direct":true}`); rec.Code != http.StatusForbidden {
		t.Errorf("set without settings.manage: %d", rec.Code)
	}
	for _, bad := range []string{`{}`, `{"direct":"yes"}`, `nope`} {
		if rec := doJSON(t, srv, adminC, http.MethodPut, "/api/v1/settings/search", bad); rec.Code != http.StatusBadRequest {
			t.Errorf("setting %s: %d", bad, rec.Code)
		}
	}
	if rec := doJSON(t, srv, adminC, http.MethodPut, "/api/v1/settings/search", `{"direct":true}`); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"direct":true`) {
		t.Fatalf("turning it on: %d %s", rec.Code, rec.Body)
	}
	if code, got := settings(reader); code != http.StatusOK || !got.Direct {
		t.Errorf("after turning it on: %d %+v", code, got)
	}

	// A person's own choice: unset, off, on, and back to following.
	me := func() *bool {
		var out struct{ User models.User }
		rec := doJSON(t, srv, reader, http.MethodGet, "/api/v1/auth/me", "")
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("me: %d %s", rec.Code, rec.Body)
		}
		return out.User.SearchDirect
	}
	if got := me(); got != nil {
		t.Errorf("before choosing: %v", *got)
	}
	for _, step := range []struct {
		body string
		want *bool
	}{{`{"direct":false}`, ptr(false)}, {`{"direct":true}`, ptr(true)}, {`{"direct":null}`, nil}} {
		rec := doJSON(t, srv, reader, http.MethodPut, "/api/v1/auth/search-direct", step.body)
		var out struct{ User models.User }
		if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &out) != nil || !sameBool(out.User.SearchDirect, step.want) {
			t.Errorf("%s: %d %s", step.body, rec.Code, rec.Body)
		}
		if got := me(); !sameBool(got, step.want) {
			t.Errorf("%s, read back: %v", step.body, got)
		}
	}
	for _, bad := range []string{`{}`, `{"direct":"on"}`, `nope`} {
		if rec := doJSON(t, srv, reader, http.MethodPut, "/api/v1/auth/search-direct", bad); rec.Code != http.StatusBadRequest {
			t.Errorf("choosing %s: %d", bad, rec.Code)
		}
	}
	if rec := doJSON(t, srv, nil, http.MethodPut, "/api/v1/auth/search-direct", `{"direct":true}`); rec.Code != http.StatusUnauthorized {
		t.Errorf("choosing signed out: %d", rec.Code)
	}
}

func ptr[T any](v T) *T { return &v }

func sameBool(a, b *bool) bool {
	return (a == nil) == (b == nil) && (a == nil || *a == *b)
}
