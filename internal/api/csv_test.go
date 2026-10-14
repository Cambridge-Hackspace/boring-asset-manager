package api

import (
	"encoding/csv"
	"net/http"
	"strings"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/assetcsv"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
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
		t.Errorf("without assets:read: %d", rec.Code)
	}
}
