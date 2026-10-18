package api

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
)

// raw sends a request with no session or token, as an assistant does
// before it signs in.
func raw(t *testing.T, srv *Server, method, target, contentType, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

func pkce(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

type tokens struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	Error        string `json:"error"`
}

func TestOAuthSignIn(t *testing.T) {
	srv, _, _ := setupTestServer(t)
	srv.cfg.Server.BaseURL = "https://bam.example"
	member := userWithPermissions(t, srv, "member@hackspace.org", rbac.PermAssetsRead, rbac.PermAssetsBorrow)
	memberUser, _ := srv.store.GetUserByEmail("member@hackspace.org")

	// Asked without a token, MCP says where to sign in.
	rec := raw(t, srv, http.MethodPost, "/api/v1/mcp", "application/json", `{}`)
	if rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Header().Get("WWW-Authenticate"), `resource_metadata="https://bam.example/.well-known/oauth-protected-resource/api/v1/mcp"`) {
		t.Fatalf("challenge: %d %v", rec.Code, rec.Header())
	}
	var meta map[string]any
	_ = json.Unmarshal(raw(t, srv, http.MethodGet, "/.well-known/oauth-protected-resource/api/v1/mcp", "", "").Body.Bytes(), &meta)
	if meta["resource"] != "https://bam.example/api/v1/mcp" || meta["authorization_servers"].([]any)[0] != "https://bam.example" {
		t.Errorf("resource metadata: %v", meta)
	}
	_ = json.Unmarshal(raw(t, srv, http.MethodGet, "/.well-known/oauth-authorization-server", "", "").Body.Bytes(), &meta)
	if meta["token_endpoint"] != "https://bam.example/oauth/token" || meta["code_challenge_methods_supported"].([]any)[0] != "S256" {
		t.Errorf("server metadata: %v", meta)
	}

	// Registering: https, or http on the machine itself; nothing else.
	for _, bad := range []string{`{"redirect_uris":["http://evil.example/cb"]}`, `{"redirect_uris":[]}`, `{"redirect_uris":["https://x.example/cb#frag"]}`} {
		if rec := raw(t, srv, http.MethodPost, "/oauth/register", "application/json", bad); rec.Code != http.StatusBadRequest {
			t.Errorf("registering %s: %d", bad, rec.Code)
		}
	}
	rec = raw(t, srv, http.MethodPost, "/oauth/register", "application/json", `{"client_name":"Claude","redirect_uris":["http://127.0.0.1:4711/callback"]}`)
	var client struct {
		ClientID string `json:"client_id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &client); err != nil || rec.Code != http.StatusCreated || client.ClientID == "" {
		t.Fatalf("register: %d %s", rec.Code, rec.Body)
	}
	cb := "http://127.0.0.1:4711/callback"
	verifier := strings.Repeat("v", 50)
	ask := url.Values{"response_type": {"code"}, "client_id": {client.ClientID}, "redirect_uri": {cb},
		"code_challenge": {pkce(verifier)}, "code_challenge_method": {"S256"}, "state": {"xyz"}, "resource": {"https://bam.example/api/v1/mcp"}}

	// The authorization endpoint sends the person to the consent page; a
	// redirect URI not registered is never followed.
	rec = raw(t, srv, http.MethodGet, "/oauth/authorize?"+ask.Encode(), "", "")
	if rec.Code != http.StatusFound || !strings.HasPrefix(rec.Header().Get("Location"), "/oauth/consent?") {
		t.Fatalf("authorize: %d %v", rec.Code, rec.Header())
	}
	bad := url.Values{}
	for k, v := range ask {
		bad[k] = v
	}
	bad.Set("redirect_uri", "https://evil.example/cb")
	if rec := raw(t, srv, http.MethodGet, "/oauth/authorize?"+bad.Encode(), "", ""); rec.Code != http.StatusBadRequest {
		t.Errorf("another redirect: %d %v", rec.Code, rec.Header())
	}
	bad = url.Values{}
	for k, v := range ask {
		bad[k] = v
	}
	bad.Del("code_challenge")
	if rec := raw(t, srv, http.MethodGet, "/oauth/authorize?"+bad.Encode(), "", ""); rec.Code != http.StatusFound || !strings.Contains(rec.Header().Get("Location"), "error=invalid_request") {
		t.Errorf("without PKCE: %d %v", rec.Code, rec.Header())
	}

	// The consent page names the client; the person approves.
	rec = doJSON(t, srv, member, http.MethodGet, "/api/v1/oauth/clients/"+client.ClientID+"?redirect_uri="+url.QueryEscape(cb), "")
	if !strings.Contains(rec.Body.String(), `"name":"Claude"`) || !strings.Contains(rec.Body.String(), `"redirect_host":"127.0.0.1:4711"`) {
		t.Errorf("consent client: %s", rec.Body)
	}
	consent := func(approve bool) url.Values {
		body, _ := json.Marshal(map[string]any{"client_id": client.ClientID, "redirect_uri": cb, "code_challenge": pkce(verifier),
			"code_challenge_method": "S256", "state": "xyz", "approve": approve})
		rec := doJSON(t, srv, member, http.MethodPost, "/api/v1/oauth/authorize", string(body))
		var out struct{ Redirect string }
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		u, err := url.Parse(out.Redirect)
		if err != nil || !strings.HasPrefix(out.Redirect, cb+"?") {
			t.Fatalf("consent: %d %s", rec.Code, rec.Body)
		}
		return u.Query()
	}
	if denied := consent(false); denied.Get("error") != "access_denied" || denied.Get("state") != "xyz" {
		t.Errorf("denied: %v", denied)
	}
	code := consent(true).Get("code")

	exchange := func(form url.Values) (*httptest.ResponseRecorder, tokens) {
		rec := raw(t, srv, http.MethodPost, "/oauth/token", "application/x-www-form-urlencoded", form.Encode())
		var got tokens
		_ = json.Unmarshal(rec.Body.Bytes(), &got)
		return rec, got
	}
	redeem := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {cb}, "client_id": {client.ClientID}, "code_verifier": {strings.Repeat("w", 50)}}
	if rec, got := exchange(redeem); rec.Code != http.StatusBadRequest || got.Error != "invalid_grant" {
		t.Errorf("the wrong verifier: %d %s", rec.Code, rec.Body)
	}
	// That spent the code: approve again.
	code = consent(true).Get("code")
	redeem.Set("code", code)
	redeem.Set("code_verifier", verifier)
	rec, got := exchange(redeem)
	if rec.Code != http.StatusOK || !strings.HasPrefix(got.AccessToken, "bam_") || got.RefreshToken == "" || got.ExpiresIn != 3600 || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("token: %d %s", rec.Code, rec.Body)
	}
	if rec, _ := exchange(redeem); rec.Code != http.StatusBadRequest {
		t.Errorf("a code used twice: %d", rec.Code)
	}

	// The access token acts as the person, over MCP too.
	cs := mcpSession(t, srv, got.AccessToken)
	var me struct{ Kind, ID, Token string }
	structured(t, callTool(t, cs, "whoami", nil), &me)
	if me.Kind != "person" || me.ID != memberUser.ID || me.Token != "Claude" {
		t.Errorf("whoami: %+v", me)
	}
	rec = doJSON(t, srv, member, http.MethodGet, "/api/v1/auth/tokens", "")
	if !strings.Contains(rec.Body.String(), `"oauth_client":"Claude"`) {
		t.Errorf("the grant among the person's assistants: %s", rec.Body)
	}

	// Refreshing gives new secrets; the old ones stop.
	rec, renewed := exchange(url.Values{"grant_type": {"refresh_token"}, "refresh_token": {got.RefreshToken}, "client_id": {client.ClientID}})
	if rec.Code != http.StatusOK || renewed.AccessToken == got.AccessToken || renewed.RefreshToken == got.RefreshToken {
		t.Fatalf("refresh: %d %s", rec.Code, rec.Body)
	}
	if rec := withBearer(t, srv, got.AccessToken, http.MethodGet, "/api/v1/assets", ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("the old access token: %d", rec.Code)
	}
	if rec, _ := exchange(url.Values{"grant_type": {"refresh_token"}, "refresh_token": {got.RefreshToken}, "client_id": {client.ClientID}}); rec.Code != http.StatusBadRequest {
		t.Errorf("the old refresh token: %d", rec.Code)
	}
	if rec := withBearer(t, srv, renewed.AccessToken, http.MethodGet, "/api/v1/assets", ""); rec.Code != http.StatusOK {
		t.Errorf("the new access token: %d", rec.Code)
	}

	// Revoked, it renews no more.
	var own struct {
		Tokens []struct{ ID string }
	}
	_ = json.Unmarshal(doJSON(t, srv, member, http.MethodGet, "/api/v1/auth/tokens", "").Body.Bytes(), &own)
	doJSON(t, srv, member, http.MethodDelete, "/api/v1/auth/tokens/"+own.Tokens[0].ID, "")
	if rec, _ := exchange(url.Values{"grant_type": {"refresh_token"}, "refresh_token": {renewed.RefreshToken}, "client_id": {client.ClientID}}); rec.Code != http.StatusBadRequest {
		t.Errorf("refreshing a revoked grant: %d", rec.Code)
	}

	// With MCP off, none of it is there.
	srv.cfg.MCP.Enabled = false
	off := NewServer(srv.cfg, srv.store)
	if rec := raw(t, off, http.MethodGet, "/.well-known/oauth-authorization-server", "", ""); rec.Code != http.StatusNotFound {
		t.Errorf("with MCP off: %d", rec.Code)
	}
}
