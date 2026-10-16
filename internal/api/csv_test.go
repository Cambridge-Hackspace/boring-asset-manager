package api

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/assetcsv"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// readCSV parses an exported file, checking it starts with a byte order mark.
func readCSV(t *testing.T, body string) [][]string {
	t.Helper()
	if !strings.HasPrefix(body, assetcsv.BOM) {
		t.Fatalf("no byte order mark: %q", body[:min(len(body), 20)])
	}
	rows, err := csv.NewReader(strings.NewReader(strings.TrimPrefix(body, assetcsv.BOM))).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

func TestExportAssetsCSV(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	tools, _ := srv.store.CreateCategory("Tools", nil, "", false, false)
	drills, _ := srv.store.CreateCategory("Drills", &tools.ID, "", false, false)
	if _, err := srv.store.CreateField("serial", "Serial", "text", nil); err != nil {
		t.Fatal(err)
	}
	drill, err := srv.store.CreateAsset("=Drill", []string{drills.ID}, []models.AssetGeneratorKey{{GeneratorType: "code128", AssignedCode: "D1"}}, map[string]string{"serial": "SN-1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := srv.store.CreateAsset("Hammer", nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	gone, _ := srv.store.CreateAsset("Gone", []string{drills.ID}, nil, nil)
	if err := srv.store.DeleteAsset(gone.Asset.ID); err != nil {
		t.Fatal(err)
	}

	rec := doJSON(t, srv, cookie, http.MethodGet, "/api/v1/assets/export.csv", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/csv; charset=utf-8" {
		t.Errorf("content type %s", ct)
	}
	if cd := rec.Header().Get("Content-Disposition"); !strings.HasPrefix(cd, `attachment; filename="assets-`) {
		t.Errorf("disposition %s", cd)
	}
	rows := readCSV(t, rec.Body.String())
	wantHeader := "id,name,categories,code.qr,code.micro_qr,code.code128,field.serial"
	if strings.Join(rows[0], ",") != wantHeader || len(rows) != 3 {
		t.Fatalf("rows %q", rows)
	}
	byName := map[string][]string{}
	for _, r := range rows[1:] {
		byName[r[1]] = r
	}
	if r := byName["'=Drill"]; r == nil || r[0] != drill.Asset.ID || r[2] != "Tools > Drills" || r[5] != "D1" || r[6] != "SN-1" {
		t.Errorf("drill row %q", r)
	}
	if r := byName["Hammer"]; r == nil || r[2] != "" {
		t.Errorf("hammer row %q", r)
	}

	// The list's filters apply.
	rows = readCSV(t, doJSON(t, srv, cookie, http.MethodGet, "/api/v1/assets/export.csv?category_id="+drills.ID, "").Body.String())
	if len(rows) != 2 || rows[1][0] != drill.Asset.ID {
		t.Errorf("filtered by category: %q", rows)
	}
	rows = readCSV(t, doJSON(t, srv, cookie, http.MethodGet, "/api/v1/assets/export.csv?search=hamm", "").Body.String())
	if len(rows) != 2 || rows[1][1] != "Hammer" {
		t.Errorf("filtered by search: %q", rows)
	}

	nobody := userWithPermissions(t, srv, "nobody@hackspace.org", rbac.PermLabelsPrint)
	if rec := doJSON(t, srv, nobody, http.MethodGet, "/api/v1/assets/export.csv", ""); rec.Code != http.StatusForbidden {
		t.Errorf("without assets.read: %d", rec.Code)
	}
}

// postImport sends a CSV, and a mapping if one is given, as multipart form data.
func postImport(t *testing.T, srv *Server, cookie *http.Cookie, path, file string, mapping []string) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, err := mw.CreateFormFile("file", "assets.csv")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = fw.Write([]byte(file))
	if mapping != nil {
		raw, _ := json.Marshal(mapping)
		_ = mw.WriteField("mapping", string(raw))
	}
	_ = mw.Close()
	return do(t, srv, cookie, http.MethodPost, path, mw.FormDataContentType(), &body)
}

func TestImportAssetsAPI(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	tools, _ := srv.store.CreateCategory("Tools", nil, "", false, false)
	file := "Name,Categories,Colour\nSaw,Tools,red\nPliers,Tools > Hand,blue\n"

	// The preview suggests a mapping and reports problems without writing.
	rec := postImport(t, srv, cookie, "/api/v1/assets/import/preview", file, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("preview: %d %s", rec.Code, rec.Body)
	}
	var plan assetcsv.Plan
	_ = json.Unmarshal(rec.Body.Bytes(), &plan)
	if len(plan.Columns) != 3 || plan.Columns[0].Suggested != "name" || plan.Columns[2].Suggested != "" || plan.Rows != 2 {
		t.Fatalf("columns %+v", plan)
	}
	if plan.ProblemCount != 1 || plan.Problems[0].Column != "Colour" {
		t.Errorf("problems %+v", plan.Problems)
	}

	// Importing needs a confirmed mapping.
	if rec := postImport(t, srv, cookie, "/api/v1/assets/import", file, nil); rec.Code != http.StatusBadRequest {
		t.Errorf("without a mapping: %d", rec.Code)
	}
	mapping := []string{"name", "categories", "ignore"}
	rec = postImport(t, srv, cookie, "/api/v1/assets/import", file, mapping)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("unknown category: %d %s", rec.Code, rec.Body)
	}
	var failed struct {
		Error   string
		Details assetcsv.Plan
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &failed)
	if !strings.Contains(failed.Error, "nothing was imported") || len(failed.Details.UnknownCategories) != 1 || failed.Details.UnknownCategories[0] != "Tools > Hand" {
		t.Errorf("422 body %s", rec.Body)
	}
	if list, _ := srv.store.ListAssets("", ""); len(list) != 0 {
		t.Fatalf("%d assets after a refused import", len(list))
	}

	if _, err := srv.store.CreateCategory("Hand", &tools.ID, "", false, false); err != nil {
		t.Fatal(err)
	}
	rec = postImport(t, srv, cookie, "/api/v1/assets/import", file, mapping)
	if rec.Code != http.StatusOK {
		t.Fatalf("import: %d %s", rec.Code, rec.Body)
	}
	var done struct {
		Created, Updated, Unchanged int
		CreatedIDs                  []string `json:"created_ids"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &done)
	if done.Created != 2 || len(done.CreatedIDs) != 2 {
		t.Errorf("result %s", rec.Body)
	}
	pliers, err := srv.store.GetAssetByID(done.CreatedIDs[1])
	if err != nil || pliers.Asset.Name != "Pliers" || pliers.Categories[0].Name != "Hand" {
		t.Errorf("pliers %+v %v", pliers, err)
	}

	// Bad requests.
	for name, rec := range map[string]*httptest.ResponseRecorder{
		"bad mapping":   postImport(t, srv, cookie, "/api/v1/assets/import/preview", file, []string{"name"}),
		"not multipart": doJSON(t, srv, cookie, http.MethodPost, "/api/v1/assets/import/preview", `{}`),
		"empty file":    postImport(t, srv, cookie, "/api/v1/assets/import/preview", "", nil),
	} {
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: %d %s", name, rec.Code, rec.Body)
		}
	}
	big := "name\n" + strings.Repeat("x", assetcsv.MaxBytes)
	if rec := postImport(t, srv, cookie, "/api/v1/assets/import/preview", big, nil); rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("big file: %d", rec.Code)
	}

	reader := userWithPermissions(t, srv, "reader@hackspace.org", rbac.PermAssetsRead)
	for _, path := range []string{"/api/v1/assets/import/preview", "/api/v1/assets/import"} {
		if rec := postImport(t, srv, reader, path, file, mapping); rec.Code != http.StatusForbidden {
			t.Errorf("%s without assets.write: %d", path, rec.Code)
		}
	}
}

// TestImportIsAttributedToTheUser checks threshold events from an import
// name the importer.
func TestImportIsAttributedToTheUser(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	one := 1
	if _, err := srv.store.CreateCategoryFrom(store.CategoryInput{Name: "Tape", IsConsumable: true, LowStockThreshold: &one}); err != nil {
		t.Fatal(err)
	}
	rec := postImport(t, srv, cookie, "/api/v1/assets/import", "name,categories\nRoll,Tape\n", []string{"name", "categories"})
	if rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	events := notices(t, srv, 1)
	if len(events) != 1 || events[0].Kind != models.EventRestocked || events[0].Payload.Actor == nil || events[0].Payload.Actor.ID != admin.ID {
		t.Errorf("events %+v", events)
	}
}
