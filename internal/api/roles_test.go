package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
)

func TestRoleManagementAPI(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	adminCookie := sessionCookie(t, srv, admin.ID)
	reader := userWithPermissions(t, srv, "reader@hackspace.org", rbac.PermAssetsRead)

	roleOf := func(body []byte) models.RoleWithPermissions {
		t.Helper()
		var v struct{ Role models.RoleWithPermissions }
		if err := json.Unmarshal(body, &v); err != nil {
			t.Fatal(err)
		}
		return v.Role
	}

	body := `{"name":"Stores","description":"Records stock","permissions":["assets:read","stock:record"]}`
	if rec := doJSON(t, srv, reader, http.MethodPost, "/api/v1/roles", body); rec.Code != http.StatusForbidden {
		t.Errorf("a reader created a role: %d", rec.Code)
	}
	rec := doJSON(t, srv, adminCookie, http.MethodPost, "/api/v1/roles", body)
	stores := roleOf(rec.Body.Bytes())
	if rec.Code != http.StatusCreated || stores.Name != "Stores" || len(stores.Permissions) != 2 {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	for bad, want := range map[string]int{
		body:                           http.StatusConflict,
		`{"name":"","permissions":[]}`: http.StatusBadRequest,
		`{"name":"Odd","permissions":["assets:fly"]}`: http.StatusBadRequest,
		`{"name":"Odd","permissions":"assets:read"}`:  http.StatusBadRequest,
		`nope`: http.StatusBadRequest,
	} {
		if rec := doJSON(t, srv, adminCookie, http.MethodPost, "/api/v1/roles", bad); rec.Code != want {
			t.Errorf("%s: %d %s, want %d", bad, rec.Code, rec.Body, want)
		}
	}

	path := "/api/v1/roles/" + stores.ID
	rec = doJSON(t, srv, adminCookie, http.MethodPut, path, `{"name":"Counters","description":"","permissions":["stock:record"]}`)
	if r := roleOf(rec.Body.Bytes()); rec.Code != http.StatusOK || r.Name != "Counters" || len(r.Permissions) != 1 {
		t.Errorf("update: %d %s", rec.Code, rec.Body)
	}
	if rec := doJSON(t, srv, adminCookie, http.MethodPut, "/api/v1/roles/missing", `{"name":"x"}`); rec.Code != http.StatusNotFound {
		t.Errorf("a missing role: %d", rec.Code)
	}

	// The list says which role is protected and how many hold each.
	rec = doJSON(t, srv, adminCookie, http.MethodGet, "/api/v1/roles", "")
	var list struct{ Roles []models.RoleWithPermissions }
	_ = json.Unmarshal(rec.Body.Bytes(), &list)
	var adminRole *models.RoleWithPermissions
	for i := range list.Roles {
		if list.Roles[i].Name == "admin" {
			adminRole = &list.Roles[i]
		}
	}
	if adminRole == nil || !adminRole.Protected || adminRole.UserCount != 1 {
		t.Fatalf("roles: %s", rec.Body)
	}
	if rec := doJSON(t, srv, adminCookie, http.MethodPut, "/api/v1/roles/"+adminRole.ID, `{"name":"boss","permissions":[]}`); rec.Code != http.StatusConflict {
		t.Errorf("editing admin: %d %s", rec.Code, rec.Body)
	}
	if rec := doJSON(t, srv, adminCookie, http.MethodDelete, "/api/v1/roles/"+adminRole.ID, ""); rec.Code != http.StatusConflict {
		t.Errorf("deleting admin: %d %s", rec.Code, rec.Body)
	}

	if rec := doJSON(t, srv, adminCookie, http.MethodDelete, path, ""); rec.Code != http.StatusOK {
		t.Errorf("delete: %d %s", rec.Code, rec.Body)
	}
	if rec := doJSON(t, srv, adminCookie, http.MethodDelete, path, ""); rec.Code != http.StatusNotFound {
		t.Errorf("delete twice: %d", rec.Code)
	}
}
