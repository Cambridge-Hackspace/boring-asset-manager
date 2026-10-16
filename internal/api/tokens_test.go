package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// withBearer sends a request with an API token instead of a session.
func withBearer(t *testing.T, srv *Server, secret, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+secret)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

func TestAPITokensAPI(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	adminCookie := sessionCookie(t, srv, admin.ID)
	writer := userWithPermissions(t, srv, "writer@hackspace.org", rbac.PermAssetsRead, rbac.PermAssetsWrite)

	counters, err := srv.store.CreateRole(store.RoleInput{Name: "Counters", Permissions: []string{rbac.PermAssetsRead, rbac.PermStockRecord}})
	if err != nil {
		t.Fatal(err)
	}
	body := `{"name":"Dashboard","role_ids":["` + counters.ID + `"]}`
	if rec := doJSON(t, srv, writer, http.MethodPost, "/api/v1/tokens", body); rec.Code != http.StatusForbidden {
		t.Errorf("created without users.manage: %d", rec.Code)
	}
	rec := doJSON(t, srv, adminCookie, http.MethodPost, "/api/v1/tokens", body)
	var created struct {
		Token  models.APIToken
		Secret string
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	if rec.Code != http.StatusCreated || !strings.HasPrefix(created.Secret, "bam_") || created.Token.CreatedBy != admin.ID {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	secret := created.Secret
	for bad := range map[string]bool{
		`{"name":""}`:                                      true,
		`{"name":"x","role_ids":["missing"]}`:              true,
		`{"name":"x","expires_at":"next tuesday"}`:         true,
		`{"name":"x","expires_at":"2000-01-01T00:00:00Z"}`: true,
	} {
		if rec := doJSON(t, srv, adminCookie, http.MethodPost, "/api/v1/tokens", bad); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: %d %s", bad, rec.Code, rec.Body)
		}
	}
	// The list never shows a secret.
	rec = doJSON(t, srv, adminCookie, http.MethodGet, "/api/v1/tokens", "")
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), secret) || !strings.Contains(rec.Body.String(), "Dashboard") {
		t.Errorf("list: %d %s", rec.Code, rec.Body)
	}

	// The token can do what its roles allow, and nothing more.
	rec = doJSON(t, srv, adminCookie, http.MethodPost, "/api/v1/assets", `{"name":"Screws","stock":{"quantity":10}}`)
	var a struct{ Asset models.AssetWithDetails }
	_ = json.Unmarshal(rec.Body.Bytes(), &a)
	id := a.Asset.Asset.ID
	if rec := withBearer(t, srv, secret, http.MethodGet, "/api/v1/assets", ""); rec.Code != http.StatusOK {
		t.Errorf("reading: %d", rec.Code)
	}
	if rec := withBearer(t, srv, secret, http.MethodPost, "/api/v1/assets/"+id+"/quantity", `{"adjust":-2}`); rec.Code != http.StatusOK {
		t.Errorf("using stock: %d %s", rec.Code, rec.Body)
	}
	if rec := withBearer(t, srv, secret, http.MethodPost, "/api/v1/assets", `{"name":"Nope"}`); rec.Code != http.StatusForbidden {
		t.Errorf("creating an asset: %d", rec.Code)
	}
	// Session-only endpoints refuse it, so a token cannot mint tokens.
	for _, r := range []struct{ method, path, body string }{
		{http.MethodGet, "/api/v1/auth/me", ""},
		{http.MethodPut, "/api/v1/auth/name", `{"name":"x"}`},
		{http.MethodGet, "/api/v1/tokens", ""},
		{http.MethodPost, "/api/v1/tokens", body},
	} {
		if rec := withBearer(t, srv, secret, r.method, r.path, r.body); rec.Code != http.StatusForbidden {
			t.Errorf("%s %s with a token: %d", r.method, r.path, rec.Code)
		}
	}
	// What it did is recorded as the token's.
	rec = doJSON(t, srv, adminCookie, http.MethodGet, "/api/v1/assets/"+id+"/history", "")
	var history struct{ Events []models.Event }
	_ = json.Unmarshal(rec.Body.Bytes(), &history)
	if a := history.Events[0].Payload.Actor; a == nil || !a.Token || a.Name != "Dashboard" {
		t.Errorf("history: %s", rec.Body)
	}

	// A bad token is refused outright, even with a valid session alongside.
	for _, wrong := range []string{"bam_nope", "", secret + "x"} {
		if rec := withBearer(t, srv, wrong, http.MethodGet, "/api/v1/assets", ""); rec.Code != http.StatusUnauthorized {
			t.Errorf("%q: %d", wrong, rec.Code)
		}
	}

	// Revoked, it stops working at once.
	if rec := doJSON(t, srv, adminCookie, http.MethodDelete, "/api/v1/tokens/"+created.Token.ID, ""); rec.Code != http.StatusOK {
		t.Fatalf("revoke: %d %s", rec.Code, rec.Body)
	}
	if rec := withBearer(t, srv, secret, http.MethodGet, "/api/v1/assets", ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("a revoked token: %d", rec.Code)
	}
	if rec := doJSON(t, srv, adminCookie, http.MethodDelete, "/api/v1/tokens/missing", ""); rec.Code != http.StatusNotFound {
		t.Errorf("a missing token: %d", rec.Code)
	}

	// An expiry in the future is kept.
	soon := time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339)
	rec = doJSON(t, srv, adminCookie, http.MethodPost, "/api/v1/tokens", `{"name":"Tomorrow","expires_at":"`+soon+`"}`)
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	if rec.Code != http.StatusCreated || created.Token.ExpiresAt == nil || created.Token.ExpiresAt.UTC().Format(time.RFC3339) != soon {
		t.Errorf("an expiring token: %d %s", rec.Code, rec.Body)
	}
}
