package api

import (
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
)

// TestCredentialsOverTokens lets tokens list and revoke tokens, never make
// them or change a password (#43), over HTTP and MCP alike.
func TestCredentialsOverTokens(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	adminCookie := sessionCookie(t, srv, admin.ID)
	member := userWithPermissions(t, srv, "member@hackspace.org", rbac.PermAssetsRead)
	other := userWithPermissions(t, srv, "other@hackspace.org", rbac.PermAssetsRead)

	mine, secret := personalToken(t, srv, member, "Claude")
	theirs, _ := personalToken(t, srv, other, "Theirs")
	manager := tokenWith(t, srv, admin.ID, "User desk", rbac.PermUsersManage)
	reader := tokenWith(t, srv, admin.ID, "Reader", rbac.PermAssetsRead)

	// A person's token sees only their own personal tokens.
	rec := withBearer(t, srv, secret, http.MethodGet, "/api/v1/auth/tokens", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Claude") || strings.Contains(rec.Body.String(), "Theirs") {
		t.Errorf("own tokens: %d %s", rec.Code, rec.Body)
	}
	if rec := withBearer(t, srv, secret, http.MethodDelete, "/api/v1/auth/tokens/"+theirs.ID, ""); rec.Code != http.StatusNotFound {
		t.Errorf("revoking another's: %d", rec.Code)
	}
	// With users.manage a token sees and revokes any; without, none.
	if rec := withBearer(t, srv, manager, http.MethodGet, "/api/v1/tokens", ""); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Theirs") {
		t.Errorf("all tokens: %d", rec.Code)
	}
	if rec := withBearer(t, srv, reader, http.MethodGet, "/api/v1/tokens", ""); rec.Code != http.StatusForbidden {
		t.Errorf("all tokens without users.manage: %d", rec.Code)
	}
	if rec := withBearer(t, srv, manager, http.MethodDelete, "/api/v1/tokens/"+theirs.ID, ""); rec.Code != http.StatusOK {
		t.Errorf("revoking with users.manage: %d", rec.Code)
	}
	// Never made by a token, nor a password changed through one.
	for _, r := range []struct{ secret, method, path, body string }{
		{manager, http.MethodPost, "/api/v1/tokens", `{"name":"Another"}`},
		{secret, http.MethodPost, "/api/v1/auth/tokens", `{"name":"Another"}`},
		{secret, http.MethodPut, "/api/v1/auth/password", `{"current_password":"UserSecret123!","new_password":"another-one"}`},
		{secret, http.MethodPost, "/api/v1/auth/logout", ""},
	} {
		if rec := withBearer(t, srv, r.secret, r.method, r.path, r.body); rec.Code != http.StatusForbidden {
			t.Errorf("%s %s with a token: %d", r.method, r.path, rec.Code)
		}
	}
	if rec := doJSON(t, srv, adminCookie, http.MethodPost, "/api/v1/tokens", `{"name":"From a session"}`); rec.Code != http.StatusCreated {
		t.Errorf("a session still makes tokens: %d", rec.Code)
	}

	// Over MCP: listed for a person and for users.manage, revoking ends its
	// own access, and nothing makes tokens or changes passwords.
	cs := mcpSession(t, srv, secret)
	names := toolNames(t, cs)
	for _, n := range names {
		if strings.Contains(n, "password") || strings.Contains(n, "create_token") || strings.Contains(n, "make_token") {
			t.Errorf("a person lists %s", n)
		}
	}
	if !slices.Contains(names, "list_tokens") || !slices.Contains(names, "revoke_token") {
		t.Errorf("a person lists %v", names)
	}
	if res := callTool(t, cs, "list_tokens", nil); !strings.Contains(toolText(res), "1 tokens") {
		t.Errorf("list_tokens: %s", toolText(res))
	}
	if res := callTool(t, cs, "revoke_token", map[string]any{"id": mine.ID}); res.IsError {
		t.Errorf("revoking itself: %s", toolText(res))
	}
	if rec := withBearer(t, srv, secret, http.MethodGet, "/api/v1/assets", ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("after revoking itself: %d", rec.Code)
	}
	if names := toolNames(t, mcpSession(t, srv, reader)); slices.Contains(names, "list_tokens") {
		t.Errorf("a role token without users.manage lists list_tokens")
	}
	if res := callTool(t, mcpSession(t, srv, manager), "list_tokens", nil); res.IsError || !strings.Contains(toolText(res), "tokens") {
		t.Errorf("list_tokens with users.manage: %s", toolText(res))
	}
	if res := callTool(t, mcpSession(t, srv, manager), "list_live_connections", nil); res.IsError {
		t.Errorf("list_live_connections with users.manage: %s", toolText(res))
	}
}
