package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/archive"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
)

// writeArchive writes an archive of the server's instance.
func writeArchive(t *testing.T, srv *Server, opts archive.Options) []byte {
	t.Helper()
	var buf strings.Builder
	if _, err := archive.Write(context.Background(), srv.store, srv.blobs, func(int) (io.WriteCloser, error) { return nopWriteCloser{&buf}, nil }, opts); err != nil {
		t.Fatal(err)
	}
	return []byte(buf.String())
}

// awaitImport asks after an import's job until it is done.
func awaitImport(t *testing.T, srv *Server, cookie *http.Cookie, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	if rec.Code != http.StatusAccepted {
		t.Fatalf("starting a job: %d %s", rec.Code, rec.Body)
	}
	var job struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &job)
	for deadline := time.Now().Add(time.Minute); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		r := do(t, srv, cookie, http.MethodGet, "/api/v1/import/jobs/"+job.ID, "", nil)
		var out map[string]any
		_ = json.Unmarshal(r.Body.Bytes(), &out)
		if out["done"] == true {
			return out
		}
	}
	t.Fatal("the job did not finish")
	return nil
}

func planOf(t *testing.T, job map[string]any) map[string]any {
	t.Helper()
	res, _ := job["result"].(map[string]any)
	plan, _ := res["plan"].(map[string]any)
	if plan == nil {
		t.Fatalf("no plan: %v", job)
	}
	return plan
}

// TestImportingAPackage: an uploaded package is checked, saying what it
// would do, then imported as checked, under the category chosen; what the
// importer may not do is refused at check, and authentication never comes
// in through the web.
func TestImportingAPackage(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	st := srv.store
	tools, err := st.CreateCategory("Tools", nil, "#123456", false, false)
	if err != nil {
		t.Fatal(err)
	}
	lab, err := st.CreateCategory("Lab", nil, "#654321", false, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateAsset("Lathe", []string{tools.ID}, nil, nil); err != nil {
		t.Fatal(err)
	}
	pkg := writeArchive(t, srv, archive.Options{Select: &archive.Selection{Categories: []string{tools.ID}, Assets: &archive.AssetsSelection{}}, Info: &archive.PackageInfo{Name: "Tools"}})
	upload := uploadArchive(t, srv, cookie, pkg)

	job := awaitImport(t, srv, cookie, postJSON(t, srv, cookie, "/api/v1/import/check", map[string]any{"upload_id": upload, "anchor": lab.ID}))
	if job["error"] != nil {
		t.Fatalf("check: %v", job)
	}
	plan := planOf(t, job)
	if anchor, _ := plan["anchor"].(map[string]any); anchor["name"] != "Lab" || plan["fingerprint"] == "" {
		t.Errorf("plan %v", plan)
	}
	if rec := postJSON(t, srv, cookie, "/api/v1/import", map[string]any{"upload_id": upload, "anchor": lab.ID}); rec.Code != http.StatusBadRequest {
		t.Errorf("an import unchecked: %d %s", rec.Code, rec.Body)
	}
	job = awaitImport(t, srv, cookie, postJSON(t, srv, cookie, "/api/v1/import", map[string]any{"upload_id": upload, "anchor": lab.ID, "fingerprint": plan["fingerprint"]}))
	if job["error"] != nil {
		t.Fatalf("import: %v", job)
	}
	var under string
	if err := st.QueryRow("SELECT name FROM categories WHERE parent_id = ?", lab.ID).Scan(&under); err != nil || under != "Tools" {
		t.Errorf("under Lab: %q %v", under, err)
	}
	var lathes int
	if err := st.QueryRow("SELECT COUNT(*) FROM assets WHERE name = 'Lathe'").Scan(&lathes); err != nil || lathes != 2 {
		t.Errorf("%d lathes, %v", lathes, err)
	}
	// a fingerprint from another check is refused, the instance having
	// changed since
	job = awaitImport(t, srv, cookie, postJSON(t, srv, cookie, "/api/v1/import", map[string]any{"upload_id": upload, "anchor": lab.ID, "fingerprint": "stale"}))
	if e, _ := job["error"].(string); !strings.Contains(e, "changed since") {
		t.Errorf("a stale fingerprint: %v", job)
	}

	// someone who manages settings, and not categories, is refused at check
	settings := sessionCookie(t, srv, userWith(t, srv, rbac.PermSettingsManage).ID)
	theirs := uploadArchive(t, srv, settings, pkg)
	rec := postJSON(t, srv, settings, "/api/v1/import/check", map[string]any{"upload_id": theirs})
	job = awaitImport(t, srv, settings, rec)
	if e, _ := job["error"].(string); !strings.Contains(e, "permission") {
		t.Errorf("without categories.manage: %v", job)
	}
	if missing, _ := planOf(t, job)["missing"].([]any); len(missing) == 0 || missing[0] != rbac.PermCategoriesManage {
		t.Errorf("missing %v", missing)
	}
	// and without settings.manage, there is no import at all
	if rec := postJSON(t, srv, sessionCookie(t, srv, userWith(t, srv, rbac.PermCategoriesManage).ID), "/api/v1/import/check", map[string]any{"upload_id": upload}); rec.Code != http.StatusForbidden {
		t.Errorf("without settings.manage: %d", rec.Code)
	}

	// a backup holding authentication is imported without it
	backup := uploadArchive(t, srv, cookie, writeArchive(t, srv, archive.Options{Secrets: true}))
	if rec := postJSON(t, srv, cookie, "/api/v1/import/check", map[string]any{"upload_id": backup, "kinds": []string{"auth"}}); rec.Code != http.StatusBadRequest {
		t.Errorf("auth through the web: %d", rec.Code)
	}
	job = awaitImport(t, srv, cookie, postJSON(t, srv, cookie, "/api/v1/import/check", map[string]any{"upload_id": backup, "kinds": []string{"catalog"}}))
	warnings, _ := planOf(t, job)["warnings"].([]any)
	found := false
	for _, w := range warnings {
		found = found || strings.Contains(w.(string), "never imported through the web")
	}
	if !found {
		t.Errorf("no warning of authentication: %v", warnings)
	}
}

// TestImportingAPackageOverMCP: check_import says what an upload of the
// person's would do, and import_package does it, holding the caller's own
// permissions.
func TestImportingAPackageOverMCP(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	tools, err := srv.store.CreateCategory("Tools", nil, "#123456", false, false)
	if err != nil {
		t.Fatal(err)
	}
	upload := uploadArchive(t, srv, cookie, writeArchive(t, srv, archive.Options{Select: &archive.Selection{Categories: []string{tools.ID}}, Info: &archive.PackageInfo{Name: "Tools"}}))

	// an upload is a person's, so is an import of it: a personal token's
	_, secret := personalToken(t, srv, cookie, "Importer")
	cs := mcpSession(t, srv, secret)
	var plan map[string]any
	structured(t, callTool(t, cs, "check_import", map[string]any{"upload_id": upload}), &plan)
	if plan["fingerprint"] == "" || len(plan["items"].([]any)) != 1 {
		t.Fatalf("the plan: %v", plan)
	}
	var done map[string]any
	structured(t, callTool(t, cs, "import_package", map[string]any{"upload_id": upload, "fingerprint": plan["fingerprint"]}), &done)
	var n int
	if err := srv.store.QueryRow("SELECT COUNT(*) FROM categories WHERE name = 'Tools'").Scan(&n); err != nil || n != 2 {
		t.Errorf("%d categories named Tools, %v", n, err)
	}

	settings := userWithPermissions(t, srv, "settings@test.org", rbac.PermSettingsManage)
	theirs := uploadArchive(t, srv, settings, writeArchive(t, srv, archive.Options{Select: &archive.Selection{Categories: []string{tools.ID}}, Info: &archive.PackageInfo{Name: "Tools"}}))
	_, secret = personalToken(t, srv, settings, "Settings only")
	weak := mcpSession(t, srv, secret)
	if res := callTool(t, weak, "check_import", map[string]any{"upload_id": theirs}); !res.IsError || !strings.Contains(toolText(res), "permission") {
		t.Errorf("without categories.manage: %s", toolText(res))
	}
}
