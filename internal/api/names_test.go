package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
)

func TestUserNamesAPI(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	adminCookie := sessionCookie(t, srv, admin.ID)
	reader := userWithPermissions(t, srv, "reader@hackspace.org", rbac.PermAssetsRead)

	userOf := func(body []byte) models.User {
		t.Helper()
		var v struct{ User models.User }
		if err := json.Unmarshal(body, &v); err != nil {
			t.Fatal(err)
		}
		return v.User
	}

	rec := doJSON(t, srv, adminCookie, http.MethodPost, "/api/v1/users", `{"email":"grace@hackspace.org","name":" Grace Hopper ","password":"Secret123!"}`)
	grace := userOf(rec.Body.Bytes())
	if rec.Code != http.StatusCreated || grace.Name != "Grace Hopper" {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	long := `{"email":"long@hackspace.org","name":"` + strings.Repeat("x", 256) + `","password":"Secret123!"}`
	if rec := doJSON(t, srv, adminCookie, http.MethodPost, "/api/v1/users", long); rec.Code != http.StatusBadRequest {
		t.Errorf("an overlong name: %d %s", rec.Code, rec.Body)
	}

	// An admin renames anyone; others cannot.
	path := "/api/v1/users/" + grace.ID + "/name"
	if rec := doJSON(t, srv, reader, http.MethodPut, path, `{"name":"Amazing Grace"}`); rec.Code != http.StatusForbidden {
		t.Errorf("a reader renamed someone: %d", rec.Code)
	}
	rec = doJSON(t, srv, adminCookie, http.MethodPut, path, `{"name":"Rear Admiral Hopper"}`)
	if rec.Code != http.StatusOK || userOf(rec.Body.Bytes()).Name != "Rear Admiral Hopper" {
		t.Errorf("rename: %d %s", rec.Code, rec.Body)
	}
	for _, bad := range []string{`{}`, `{"name":3}`, `nope`} {
		if rec := doJSON(t, srv, adminCookie, http.MethodPut, path, bad); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: %d", bad, rec.Code)
		}
	}
	if rec := doJSON(t, srv, adminCookie, http.MethodPut, "/api/v1/users/missing/name", `{"name":"x"}`); rec.Code != http.StatusNotFound {
		t.Errorf("a missing user: %d", rec.Code)
	}

	// Everyone names themselves, and sees it on /auth/me.
	if rec := doJSON(t, srv, reader, http.MethodPut, "/api/v1/auth/name", `{"name":"Rita Reader"}`); rec.Code != http.StatusOK {
		t.Fatalf("own name: %d %s", rec.Code, rec.Body)
	}
	rec = doJSON(t, srv, reader, http.MethodGet, "/api/v1/auth/me", "")
	if userOf(rec.Body.Bytes()).Name != "Rita Reader" {
		t.Errorf("me: %s", rec.Body)
	}
	if rec := doJSON(t, srv, nil, http.MethodPut, "/api/v1/auth/name", `{"name":"Nobody"}`); rec.Code != http.StatusUnauthorized {
		t.Errorf("signed out: %d", rec.Code)
	}
}
