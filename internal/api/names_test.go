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
	// Asset pages show people by name, so everyone has one.
	for _, body := range []string{`{"email":"anon@hackspace.org","password":"Secret123!"}`, `{"email":"anon@hackspace.org","name":"  ","password":"Secret123!"}`} {
		if rec := doJSON(t, srv, adminCookie, http.MethodPost, "/api/v1/users", body); rec.Code != http.StatusBadRequest {
			t.Errorf("no name, %s: %d %s", body, rec.Code, rec.Body)
		}
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
	for _, bad := range []string{`{}`, `{"name":3}`, `nope`, `{"name":""}`, `{"name":" "}`} {
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

// TestOwnThemeAPI: a person chooses whether the interface is light or
// dark, and /auth/me says which, "system" until they choose.
func TestOwnThemeAPI(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	reader := userWithPermissions(t, srv, "reader@hackspace.org", rbac.PermAssetsRead)

	me := func(cookie *http.Cookie) models.User {
		t.Helper()
		rec := doJSON(t, srv, cookie, http.MethodGet, "/api/v1/auth/me", "")
		var v struct{ User models.User }
		if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil || rec.Code != http.StatusOK {
			t.Fatalf("me: %d %s", rec.Code, rec.Body)
		}
		return v.User
	}
	if got := me(reader).Theme; got != "system" {
		t.Errorf("before choosing: %q", got)
	}
	if rec := doJSON(t, srv, reader, http.MethodGet, "/api/v1/auth/me", ""); !strings.Contains(rec.Body.String(), `"theme":"system"`) {
		t.Errorf("me does not say the theme: %s", rec.Body)
	}

	for _, theme := range []string{"dark", "light", "system", "dark"} {
		rec := doJSON(t, srv, reader, http.MethodPut, "/api/v1/auth/theme", `{"theme":"`+theme+`"}`)
		var v struct{ User models.User }
		_ = json.Unmarshal(rec.Body.Bytes(), &v)
		if rec.Code != http.StatusOK || v.User.Theme != theme || v.User.Email != "reader@hackspace.org" {
			t.Fatalf("%s: %d %s", theme, rec.Code, rec.Body)
		}
		if got := me(reader).Theme; got != theme {
			t.Errorf("me after %s: %q", theme, got)
		}
	}
	// It is theirs alone.
	if got := me(sessionCookie(t, srv, admin.ID)).Theme; got != "system" {
		t.Errorf("the admin's: %q", got)
	}

	for _, bad := range []string{`{}`, `{"theme":null}`, `{"theme":""}`, `{"theme":"sepia"}`, `{"theme":"Dark"}`, `{"theme":1}`, `nope`} {
		if rec := doJSON(t, srv, reader, http.MethodPut, "/api/v1/auth/theme", bad); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: %d %s", bad, rec.Code, rec.Body)
		}
	}
	if got := me(reader).Theme; got != "dark" {
		t.Errorf("a bad theme changed it to %q", got)
	}

	// A personal token acts as its person, as it does for their name; a
	// token holding roles is no one, and no one signed in is refused.
	_, secret := personalToken(t, srv, reader, "Claude")
	if rec := withBearer(t, srv, secret, http.MethodPut, "/api/v1/auth/theme", `{"theme":"light"}`); rec.Code != http.StatusOK {
		t.Errorf("a personal token: %d %s", rec.Code, rec.Body)
	}
	roleSecret := tokenWith(t, srv, admin.ID, "Script", rbac.PermAssetsRead)
	if rec := withBearer(t, srv, roleSecret, http.MethodPut, "/api/v1/auth/theme", `{"theme":"dark"}`); rec.Code != http.StatusForbidden {
		t.Errorf("a role token: %d %s", rec.Code, rec.Body)
	}
	if rec := doJSON(t, srv, nil, http.MethodPut, "/api/v1/auth/theme", `{"theme":"dark"}`); rec.Code != http.StatusUnauthorized {
		t.Errorf("signed out: %d", rec.Code)
	}
	if got := me(reader).Theme; got != "light" {
		t.Errorf("after the tokens: %q", got)
	}
}
