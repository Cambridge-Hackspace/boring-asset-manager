package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/archive"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// scopedReader is someone who reads assets in one scope only.
func scopedReader(t *testing.T, srv *Server, scope string) *http.Cookie {
	t.Helper()
	role, err := srv.store.CreateRole(store.RoleInput{Name: "Readers of " + scope, Permissions: []string{scope + rbac.ScopeSeparator + rbac.PermAssetsRead}})
	if err != nil {
		t.Fatal(err)
	}
	u, err := srv.store.CreateNamedUser(scope+"-reader@test.org", "Reader", "ReaderSecret123!", true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := srv.store.Exec("INSERT INTO user_roles (user_id, role_id) VALUES (?, ?)", u.ID, role.ID); err != nil {
		t.Fatal(err)
	}
	return sessionCookie(t, srv, u.ID)
}

// TestExportingAPackage: a selection is planned, posted, and downloaded by
// whoever posted it, holding what they may read and no people; what cannot
// be packaged is refused before anything is written.
func TestExportingAPackage(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	st := srv.store
	scope, err := st.CreateScope("workshop")
	if err != nil {
		t.Fatal(err)
	}
	tools, _ := st.CreateCategory("Tools", nil, "", false, false)
	if _, err := st.SetCategoryScopes(tools.ID, []string{scope.ID}); err != nil {
		t.Fatal(err)
	}
	drills, _ := st.CreateCategory("Drills", &tools.ID, "", false, false)
	kitchen, _ := st.CreateCategory("Kitchen", nil, "", false, false)
	drill, _ := st.CreateAsset("Drill", []string{drills.ID}, nil, nil)
	kettle, _ := st.CreateAsset("Kettle", []string{kitchen.ID}, nil, nil)

	sel := map[string]any{"subtrees": []string{tools.ID}, "assets": map[string]any{"all": true}}
	rec := postJSON(t, srv, cookie, "/api/v1/exports/plan", map[string]any{"selection": sel})
	var plan archive.Plan
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &plan) != nil || plan.Assets == nil || plan.Assets.Assets != 2 {
		t.Fatalf("plan: %d %s", rec.Code, rec.Body)
	}

	rec = postJSON(t, srv, cookie, "/api/v1/exports", map[string]any{"selection": sel, "name": "Workshop", "license": "CC0-1.0"})
	var made struct {
		ID, Download string
	}
	if rec.Code != http.StatusCreated || json.Unmarshal(rec.Body.Bytes(), &made) != nil || made.Download != "/api/v1/exports/"+made.ID {
		t.Fatalf("export: %d %s", rec.Code, rec.Body)
	}
	rec = do(t, srv, cookie, http.MethodGet, made.Download, "", nil)
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Disposition") != `attachment; filename="Workshop.bam.tar.gz"` {
		t.Fatalf("download: %d %v", rec.Code, rec.Header())
	}
	checked, err := archive.Check(bytes.NewReader(rec.Body.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if checked.Header.Type != "package" || checked.Header.Package.Name != "Workshop" || checked.Rows["assets"] != 2 || checked.Rows["categories"] != 3 || checked.Rows["users"] != 0 {
		t.Errorf("header %+v, rows %v", checked.Header, checked.Rows)
	}
	members := archiveMembers(t, rec.Body.Bytes())
	if bytes.Contains(members[archive.DataDir+"assets/0001.jsonl"], []byte(drill.Asset.ID)) == false {
		t.Error("the drill is not in the package")
	}

	// a reader of the workshop alone: its assets only, and someone else's
	// export is not theirs to download
	reader := scopedReader(t, srv, "workshop")
	rec = postJSON(t, srv, reader, "/api/v1/exports", map[string]any{"selection": sel, "name": "Mine"})
	if rec.Code != http.StatusCreated || json.Unmarshal(rec.Body.Bytes(), &made) != nil {
		t.Fatalf("a scoped export: %d %s", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), `"unreadable":1`) {
		t.Errorf("the kettle is not counted as unreadable: %s", rec.Body)
	}
	rec = do(t, srv, reader, http.MethodGet, made.Download, "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("a scoped download: %d %s", rec.Code, rec.Body)
	}
	members = archiveMembers(t, rec.Body.Bytes())
	if assets := members[archive.DataDir+"assets/0001.jsonl"]; !bytes.Contains(assets, []byte(drill.Asset.ID)) || bytes.Contains(assets, []byte(kettle.Asset.ID)) {
		t.Errorf("a scoped package's assets: %s", assets)
	}
	if rec := do(t, srv, cookie, http.MethodGet, made.Download, "", nil); rec.Code != http.StatusNotFound {
		t.Errorf("another's export: %d", rec.Code)
	}

	for what, c := range map[string]struct {
		cookie *http.Cookie
		body   map[string]any
		code   int
		says   string
	}{
		"no name":          {cookie, map[string]any{"selection": sel}, http.StatusBadRequest, "name"},
		"nothing chosen":   {cookie, map[string]any{"selection": map[string]any{}, "name": "x"}, http.StatusBadRequest, "nothing is chosen"},
		"no such category": {cookie, map[string]any{"selection": map[string]any{"categories": []string{"nope"}}, "name": "x"}, http.StatusBadRequest, "no category"},
		"a GIF preview":    {cookie, map[string]any{"selection": sel, "name": "x", "preview": []byte("GIF89a")}, http.StatusBadRequest, "PNG"},
		"scopes, unseen":   {reader, map[string]any{"selection": map[string]any{"scopes": []string{scope.ID}}, "name": "x"}, http.StatusBadRequest, "scopes"},
		"not signed in":    {nil, map[string]any{"selection": sel, "name": "x"}, http.StatusUnauthorized, "Authentication"},
	} {
		rec := postJSON(t, srv, c.cookie, "/api/v1/exports", c.body)
		if rec.Code != c.code || !strings.Contains(rec.Body.String(), c.says) {
			t.Errorf("%s: %d %s", what, rec.Code, rec.Body)
		}
	}
	if rec := do(t, srv, cookie, http.MethodGet, "/api/v1/exports/nope", "", nil); rec.Code != http.StatusNotFound {
		t.Errorf("no such export: %d", rec.Code)
	}
}

// TestExportingAPackageOverMCP: export_package plans, and posts an export
// to download, holding the caller's own permissions.
func TestExportingAPackageOverMCP(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	tools, _ := srv.store.CreateCategory("Tools", nil, "", false, false)
	cs := mcpSession(t, srv, tokenWith(t, srv, admin.ID, "Reader", rbac.PermAssetsRead))

	var out map[string]any
	structured(t, callTool(t, cs, "export_package", map[string]any{"subtrees": []string{tools.ID}, "plan_only": true}), &out)
	if items, _ := out["items"].([]any); len(items) != 1 {
		t.Errorf("the plan: %v", out)
	}
	structured(t, callTool(t, cs, "export_package", map[string]any{"subtrees": []string{tools.ID}, "name": "Tools"}), &out)
	download, _ := out["download"].(string)
	if !strings.HasPrefix(download, "/api/v1/exports/") {
		t.Fatalf("export: %v", out)
	}
	if res := callTool(t, cs, "export_package", map[string]any{"subtrees": []string{tools.ID}, "photos": true}); !res.IsError || !strings.Contains(toolText(res), "assets") {
		t.Errorf("photos without assets: %s", toolText(res))
	}
	if res := callTool(t, cs, "export_package", map[string]any{"subtrees": []string{tools.ID}}); !res.IsError || !strings.Contains(toolText(res), "name") {
		t.Errorf("without a name: %s", toolText(res))
	}
}
