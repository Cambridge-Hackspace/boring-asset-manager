package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// containsType is the seeded "contains" relation type (migration 015).
const containsType = "0d3f1c6a-5b2e-4f7a-9c8d-1e2f3a4b5c15"

// TestScopedAccessToAssets follows a keeper of the tools, who reads and
// edits the assets in the tools scope only, through every way of reaching
// an asset.
func TestScopedAccessToAssets(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	adminCookie := sessionCookie(t, srv, admin.ID)
	st := srv.store

	tools, err := st.CreateScope("tools")
	if err != nil {
		t.Fatal(err)
	}
	toolsCat, _ := st.CreateCategory("Tools", nil, "", false, false)
	if _, err := st.SetCategoryScopes(toolsCat.ID, []string{tools.ID}); err != nil {
		t.Fatal(err)
	}
	drill, _ := st.CreateAsset("Drill", []string{toolsCat.ID}, nil, nil)
	// The kettle is in a category with no scope, so in the default one.
	kitchen, err := st.CreateCategoryFrom(store.CategoryInput{Name: "Kitchen", IsLendable: true})
	if err != nil {
		t.Fatal(err)
	}
	kettle, _ := st.CreateAsset("Kettle", []string{kitchen.ID}, nil, nil)
	// The drill is kept in the kettle, which the keeper cannot see.
	if _, err := st.AddLink(kettle.Asset.ID, containsType, drill.Asset.ID, true); err != nil {
		t.Fatal(err)
	}

	role, err := st.CreateRole(store.RoleInput{Name: "Tool keepers", Permissions: []string{
		"tools:" + rbac.PermAssetsRead, "tools:" + rbac.PermAssetsWrite, rbac.PermLabelsPrint,
	}})
	if err != nil {
		t.Fatal(err)
	}
	keeperUser, _ := st.CreateNamedUser("keeper@hackspace.org", "Keeper", "UserSecret123!", true)
	if err := st.SetUserRoles(keeperUser.ID, []string{role.ID}); err != nil {
		t.Fatal(err)
	}
	keeper := sessionCookie(t, srv, keeperUser.ID)
	expect := func(what string, cookie *http.Cookie, method, path, body string, want int) string {
		t.Helper()
		rec := doJSON(t, srv, cookie, method, path, body)
		if rec.Code != want {
			t.Errorf("%s: %d, want %d: %s", what, rec.Code, want, rec.Body)
		}
		return rec.Body.String()
	}
	d, k := "/api/v1/assets/"+drill.Asset.ID, "/api/v1/assets/"+kettle.Asset.ID

	// What they hold, as the web app learns it.
	if body := expect("me", keeper, http.MethodGet, "/api/v1/auth/me", "", http.StatusOK); !strings.Contains(body, `"tools:assets.write"`) {
		t.Errorf("me: %s", body)
	}

	// Reading: the drill, without the kettle it is in.
	body := expect("the list", keeper, http.MethodGet, "/api/v1/assets", "", http.StatusOK)
	if !strings.Contains(body, "Drill") || strings.Contains(body, "Kettle") {
		t.Errorf("the list: %s", body)
	}
	body = expect("the drill", keeper, http.MethodGet, d, "", http.StatusOK)
	var got struct {
		Asset struct {
			Scopes   []string          `json:"scopes"`
			Links    []json.RawMessage `json:"links"`
			Location []json.RawMessage `json:"location"`
		} `json:"asset"`
	}
	if err := json.Unmarshal([]byte(body), &got); err != nil || len(got.Asset.Scopes) != 1 || got.Asset.Scopes[0] != "tools" || len(got.Asset.Links) != 0 || len(got.Asset.Location) != 0 {
		t.Errorf("the drill: %s", body)
	}
	if body := expect("the drill, by the admin", adminCookie, http.MethodGet, d, "", http.StatusOK); !strings.Contains(body, "Kettle") {
		t.Errorf("the admin should see the kettle: %s", body)
	}
	for what, path := range map[string]string{
		"the kettle":            k,
		"its history":           k + "/history",
		"its label":             k + "/label?template_id=none",
		"another asset, absent": "/api/v1/assets/missing",
	} {
		expect(what, keeper, http.MethodGet, path, "", http.StatusNotFound)
	}
	if body := expect("export", keeper, http.MethodGet, "/api/v1/assets/export.csv", "", http.StatusOK); !strings.Contains(body, "Drill") || strings.Contains(body, "Kettle") {
		t.Errorf("export: %s", body)
	}

	// Loans and reports leave the kettle out.
	if _, err := st.CheckOut(kettle.Asset.ID, admin.ID, nil); err != nil {
		t.Fatal(err)
	}
	if body := expect("loans", keeper, http.MethodGet, "/api/v1/loans", "", http.StatusOK); strings.Contains(body, "Kettle") {
		t.Errorf("loans: %s", body)
	}
	report, err := st.CreateReport("Everything", []byte(`{"sections":[{"type":"table","columns":[{"heading":"Asset","binding":"{{asset.name}}"}]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if body := expect("a report", keeper, http.MethodGet, "/api/v1/reports/"+report.ID+"/run", "", http.StatusOK); !strings.Contains(body, `"assets":1`) {
		t.Errorf("a report: %s", body)
	}

	// Writing: in the tools scope, and only there.
	expect("editing the drill", keeper, http.MethodPut, d, `{"name":"Hammer drill"}`, http.StatusOK)
	expect("editing the kettle", keeper, http.MethodPut, k, `{"name":"Teapot"}`, http.StatusNotFound)
	expect("taking the drill out of the tools", keeper, http.MethodPut, d, `{"category_ids":[]}`, http.StatusForbidden)
	expect("making a tool", keeper, http.MethodPost, "/api/v1/assets", `{"name":"Saw","category_ids":["`+toolsCat.ID+`"]}`, http.StatusCreated)
	expect("making something else", keeper, http.MethodPost, "/api/v1/assets", `{"name":"Mug"}`, http.StatusForbidden)
	expect("archiving, without assets.delete", keeper, http.MethodDelete, d, "", http.StatusForbidden)
	expect("linking to the kettle", keeper, http.MethodPost, d+"/links", `{"type_id":"`+containsType+`","asset_id":"`+kettle.Asset.ID+`"}`, http.StatusNotFound)
	expect("reporting the kettle broken", keeper, http.MethodPut, k+"/condition", `{"condition":"out_of_service"}`, http.StatusNotFound)
	expect("using kettle stock", keeper, http.MethodPost, k+"/quantity", `{"adjust":-1}`, http.StatusForbidden)

	// One who reads everything but writes the tools only is told no, not
	// that the kettle is missing.
	readers, _ := st.CreateRole(store.RoleInput{Name: "Readers", Permissions: []string{rbac.PermAssetsRead}})
	if err := st.SetUserRoles(keeperUser.ID, []string{role.ID, readers.ID}); err != nil {
		t.Fatal(err)
	}
	expect("editing the kettle, seen", keeper, http.MethodPut, k, `{"name":"Teapot"}`, http.StatusForbidden)
	if body := expect("the drill, seeing everything", keeper, http.MethodGet, d, "", http.StatusOK); !strings.Contains(body, "Kettle") {
		t.Errorf("a reader of everything sees the kettle: %s", body)
	}
}

func TestScopedImports(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	st := srv.store
	tools, _ := st.CreateScope("tools")
	toolsCat, _ := st.CreateCategory("Tools", nil, "", false, false)
	if _, err := st.SetCategoryScopes(toolsCat.ID, []string{tools.ID}); err != nil {
		t.Fatal(err)
	}
	drill, _ := st.CreateAsset("Drill", []string{toolsCat.ID}, nil, nil)
	kettle, _ := st.CreateAsset("Kettle", nil, nil, nil)
	role, _ := st.CreateRole(store.RoleInput{Name: "Tool keepers", Permissions: []string{"tools:" + rbac.PermAssetsRead, "tools:" + rbac.PermAssetsWrite}})
	u, _ := st.CreateNamedUser("keeper@hackspace.org", "Keeper", "UserSecret123!", true)
	if err := st.SetUserRoles(u.ID, []string{role.ID}); err != nil {
		t.Fatal(err)
	}
	keeper := sessionCookie(t, srv, u.ID)
	_ = admin

	preview := func(file string) map[string]any {
		t.Helper()
		got := postLinkImport(t, srv, keeper, "/api/v1/assets/import/preview", file, "")
		var plan map[string]any
		if err := json.Unmarshal(got.Bytes(), &plan); err != nil {
			t.Fatalf("%s: %s", file, got)
		}
		return plan
	}
	problems := func(plan map[string]any) string {
		b, _ := json.Marshal(plan["problems"])
		return string(b)
	}
	for what, c := range map[string]struct{ file, want string }{
		"updating the kettle":         {"id,name\n" + kettle.Asset.ID + ",Teapot\n", "no asset has the ID"},
		"making an uncategorized one": {"name\nMug\n", "you cannot change assets in default"},
		"moving the drill out":        {"id,categories\n" + drill.Asset.ID + ",\n", ""},
		"linking the drill to it":     {"id,links\n" + drill.Asset.ID + ",contained_in:Kettle\n", `no asset is \"Kettle\"`},
		"making a tool":               {"name,categories\nSaw,Tools\n", ""},
	} {
		plan := preview(c.file)
		if got := problems(plan); (c.want == "") != (got == "[]") || !strings.Contains(got, c.want) {
			t.Errorf("%s: %s", what, got)
		}
	}

	// A link to the kettle, made by someone who can, cannot be removed by
	// the keeper, nor does their file show it.
	if _, err := st.AddLink(kettle.Asset.ID, containsType, drill.Asset.ID, true); err != nil {
		t.Fatal(err)
	}
	got := postLinkImport(t, srv, keeper, "/api/v1/assets/import/preview", "id,links\n"+drill.Asset.ID+",\n", "true")
	if !strings.Contains(got.String(), `"links_removed":0`) {
		t.Errorf("removing a hidden link: %s", got)
	}
}

func TestManagingScopesThroughTheAPI(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	adminCookie := sessionCookie(t, srv, admin.ID)
	cat, _ := srv.store.CreateCategory("Tools", nil, "", false, false)
	categorizer := userWithPermissions(t, srv, "cat@hackspace.org", rbac.PermCategoriesManage)
	reader := userWithPermissions(t, srv, "reader@hackspace.org", rbac.PermAssetsRead)
	expect := func(what string, cookie *http.Cookie, method, path, body string, want int) string {
		t.Helper()
		rec := doJSON(t, srv, cookie, method, path, body)
		if rec.Code != want {
			t.Errorf("%s: %d, want %d: %s", what, rec.Code, want, rec.Body)
		}
		return rec.Body.String()
	}

	var created struct {
		Scope struct {
			ID string `json:"id"`
		} `json:"scope"`
	}
	_ = json.Unmarshal([]byte(expect("creating", adminCookie, http.MethodPost, "/api/v1/scopes", `{"name":"tools"}`, http.StatusCreated)), &created)
	id := created.Scope.ID
	expect("a name in use", adminCookie, http.MethodPost, "/api/v1/scopes", `{"name":"tools"}`, http.StatusConflict)
	expect("a bad name", adminCookie, http.MethodPost, "/api/v1/scopes", `{"name":"Tools!"}`, http.StatusBadRequest)
	expect("by a category manager", categorizer, http.MethodPost, "/api/v1/scopes", `{"name":"x"}`, http.StatusForbidden)
	if body := expect("listing, as a category manager", categorizer, http.MethodGet, "/api/v1/scopes", "", http.StatusOK); !strings.Contains(body, `"default"`) || !strings.Contains(body, `"tools"`) {
		t.Errorf("listing: %s", body)
	}
	expect("listing, as a reader", reader, http.MethodGet, "/api/v1/scopes", "", http.StatusForbidden)

	path := "/api/v1/categories/" + cat.ID + "/scopes"
	expect("labeling, as a category manager", categorizer, http.MethodPut, path, `{"scope_ids":["`+id+`"]}`, http.StatusForbidden)
	if body := expect("labeling", adminCookie, http.MethodPut, path, `{"scope_ids":["`+id+`"]}`, http.StatusOK); !strings.Contains(body, id) {
		t.Errorf("labeling: %s", body)
	}
	expect("reading the labels", categorizer, http.MethodGet, path, "", http.StatusOK)
	expect("an unknown scope", adminCookie, http.MethodPut, path, `{"scope_ids":["nope"]}`, http.StatusNotFound)
	expect("an unknown category", adminCookie, http.MethodGet, "/api/v1/categories/nope/scopes", "", http.StatusNotFound)
	expect("deleting in use", adminCookie, http.MethodDelete, "/api/v1/scopes/"+id, "", http.StatusConflict)
	expect("renaming", adminCookie, http.MethodPut, "/api/v1/scopes/"+id, `{"name":"workshop"}`, http.StatusOK)

	scopes, _ := srv.store.ListScopes()
	def := scopes[0].ID
	expect("renaming the default", adminCookie, http.MethodPut, "/api/v1/scopes/"+def, `{"name":"x"}`, http.StatusConflict)
	expect("the default on a category", adminCookie, http.MethodPut, path, `{"scope_ids":["`+def+`"]}`, http.StatusConflict)

	// Roles hold permissions in scopes by name.
	expect("a scoped role", adminCookie, http.MethodPost, "/api/v1/roles", `{"name":"Keepers","permissions":["workshop:assets.read"]}`, http.StatusCreated)
	expect("an unknown scope in a role", adminCookie, http.MethodPost, "/api/v1/roles", `{"name":"Others","permissions":["kitchen:assets.read"]}`, http.StatusBadRequest)
	expect("a scoped unscopable permission", adminCookie, http.MethodPost, "/api/v1/roles", `{"name":"Others","permissions":["workshop:users.manage"]}`, http.StatusBadRequest)
}
