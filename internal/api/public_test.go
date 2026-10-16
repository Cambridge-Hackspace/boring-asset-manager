package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// nothingPublic takes every permission from the public role, as an admin
// can, so a test can reach someone who holds none.
func nothingPublic(t *testing.T, srv *Server) {
	t.Helper()
	public, err := srv.store.GetRoleByName(rbac.PublicRole)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := srv.store.UpdateRole(public.ID, store.RoleInput{Name: rbac.PublicRole}); err != nil {
		t.Fatal(err)
	}
}

// TestThePublicRole follows signed-out visitors, and people with no role
// of their own, to the assets in the public scope and no further.
func TestThePublicRole(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	adminCookie := sessionCookie(t, srv, admin.ID)
	st := srv.store
	expect := func(what string, cookie *http.Cookie, method, path, body string, want int) string {
		t.Helper()
		rec := doJSON(t, srv, cookie, method, path, body)
		if rec.Code != want {
			t.Errorf("%s: %d, want %d: %s", what, rec.Code, want, rec.Body)
		}
		return rec.Body.String()
	}

	// What signed-out visitors hold.
	if body := expect("me, signed out", nil, http.MethodGet, "/api/v1/auth/me", "", http.StatusOK); !strings.Contains(body, `"user":null`) || !strings.Contains(body, `"permissions":["public:assets.read"]`) {
		t.Errorf("me, signed out: %s", body)
	}

	// Nothing is public until a category is.
	shelf, _ := st.CreateCategory("Shelf", nil, "", false, false)
	hammer, _ := st.CreateAsset("Hammer", []string{shelf.ID}, nil, nil)
	safe, _ := st.CreateAsset("Safe", nil, nil, nil)
	expect("the hammer, before", nil, http.MethodGet, "/api/v1/assets/"+hammer.Asset.ID, "", http.StatusNotFound)
	scopes, _ := st.ListScopes()
	var public string
	for _, sc := range scopes {
		if sc.Name == rbac.PublicScope {
			public = sc.ID
		}
	}
	expect("making the shelf public", adminCookie, http.MethodPut, "/api/v1/categories/"+shelf.ID+"/scopes", `{"scope_ids":["`+public+`"]}`, http.StatusOK)

	nobody, _ := st.CreateNamedUser("nobody@hackspace.org", "No Roles", "UserSecret123!", true)
	nobodyCookie := sessionCookie(t, srv, nobody.ID)
	// Signed in with no role of their own, people see the public assets
	// wherever assets show.
	if body := expect("the list, with no roles", nobodyCookie, http.MethodGet, "/api/v1/assets", "", http.StatusOK); !strings.Contains(body, "Hammer") || strings.Contains(body, "Safe") {
		t.Errorf("the list, with no roles: %s", body)
	}
	// Signed out, only an asset's own page and what it shows: not the list,
	// nor the catalog of categories, fields, templates and reports.
	for _, path := range []string{"/api/v1/assets", "/api/v1/categories", "/api/v1/fields", "/api/v1/templates", "/api/v1/reports", "/api/v1/relation-types"} {
		expect("signed out: "+path, nil, http.MethodGet, path, "", http.StatusUnauthorized)
	}
	expect("signed out: the hammer's history", nil, http.MethodGet, "/api/v1/assets/"+hammer.Asset.ID+"/history", "", http.StatusOK)
	expect("signed out: the code types", nil, http.MethodGet, "/api/v1/generators", "", http.StatusOK)
	for who, cookie := range map[string]*http.Cookie{"signed out": nil, "with no roles": nobodyCookie} {
		expect(who+": the hammer", cookie, http.MethodGet, "/api/v1/assets/"+hammer.Asset.ID, "", http.StatusOK)
		expect(who+": the safe", cookie, http.MethodGet, "/api/v1/assets/"+safe.Asset.ID, "", http.StatusNotFound)
	}
	// Looking is all: signed out, anything more asks to sign in.
	expect("editing, signed out", nil, http.MethodPut, "/api/v1/assets/"+hammer.Asset.ID, `{"name":"Mallet"}`, http.StatusUnauthorized)
	expect("editing, with no roles", nobodyCookie, http.MethodPut, "/api/v1/assets/"+hammer.Asset.ID, `{"name":"Mallet"}`, http.StatusForbidden)
	expect("loans, signed out", nil, http.MethodGet, "/api/v1/loans", "", http.StatusUnauthorized)
	expect("users, signed out", nil, http.MethodGet, "/api/v1/users", "", http.StatusUnauthorized)

	// API tokens hold it too.
	printers, _ := st.CreateRole(store.RoleInput{Name: "Printers", Permissions: []string{rbac.PermLabelsPrint}})
	_, secret, err := st.CreateToken("Kiosk", []string{printers.ID}, nil, admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	if rec := withBearer(t, srv, secret, http.MethodGet, "/api/v1/assets/"+hammer.Asset.ID, ""); rec.Code != http.StatusOK {
		t.Errorf("a token: %d %s", rec.Code, rec.Body)
	}

	// The role keeps its name, stays, and only ever reads.
	role, _ := st.GetRoleByName(rbac.PublicRole)
	path := "/api/v1/roles/" + role.ID
	if body := expect("listing roles", adminCookie, http.MethodGet, "/api/v1/roles", "", http.StatusOK); !strings.Contains(body, `"name":"public","description":"Everyone, signed in or not","permissions":["public:assets.read"],"user_count":0,"protected":false,"public":true`) {
		t.Errorf("listing roles: %s", body)
	}
	expect("renaming it", adminCookie, http.MethodPut, path, `{"name":"everyone","permissions":["public:assets.read"]}`, http.StatusConflict)
	expect("letting it write", adminCookie, http.MethodPut, path, `{"name":"public","permissions":["public:assets.write"]}`, http.StatusConflict)
	expect("deleting it", adminCookie, http.MethodDelete, path, "", http.StatusConflict)
	// Reading everything uncategorized, say, is up to the admin.
	expect("widening it", adminCookie, http.MethodPut, path, `{"name":"public","description":"Everyone","permissions":["public:assets.read","default:assets.read"]}`, http.StatusOK)
	expect("the safe, now", nil, http.MethodGet, "/api/v1/assets/"+safe.Asset.ID, "", http.StatusOK)
}
