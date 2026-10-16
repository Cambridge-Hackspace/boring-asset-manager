package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/reports"
)

func TestReportsAPI(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	adminCookie := sessionCookie(t, srv, admin.ID)
	reader := userWithPermissions(t, srv, "reader@hackspace.org", rbac.PermAssetsRead)

	// Two reports to start from.
	rec := doJSON(t, srv, reader, http.MethodGet, "/api/v1/reports", "")
	var list struct{ Reports []models.Report }
	_ = json.Unmarshal(rec.Body.Bytes(), &list)
	if rec.Code != http.StatusOK || len(list.Reports) != 2 || list.Reports[0].Name != "On loan" || list.Reports[1].Name != "Running low" {
		t.Fatalf("reports: %d %s", rec.Code, rec.Body)
	}
	runningLow := list.Reports[1].ID

	// Something running low, in a flagged category, with a location.
	rec = doJSON(t, srv, adminCookie, http.MethodPost, "/api/v1/categories", `{"name":"Filament","is_consumable":true,"is_running_low":true}`)
	var cat struct{ Category models.Category }
	_ = json.Unmarshal(rec.Body.Bytes(), &cat)
	doJSON(t, srv, adminCookie, http.MethodPost, "/api/v1/assets", `{"name":"Blue PLA","category_ids":["`+cat.Category.ID+`"],"stock":{"quantity":200,"unit":"g"}}`)
	doJSON(t, srv, adminCookie, http.MethodPost, "/api/v1/assets", `{"name":"Drill"}`)

	rec = doJSON(t, srv, reader, http.MethodGet, "/api/v1/reports/"+runningLow+"/run", "")
	var run struct {
		Report models.Report
		Result reports.Result
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &run)
	if rec.Code != http.StatusOK || run.Result.Assets != 1 || len(run.Result.Sections) != 1 {
		t.Fatalf("run: %d %s", rec.Code, rec.Body)
	}
	g := run.Result.Sections[0].Groups
	if len(g) != 1 || g[0].Title != "Filament" || !g[0].RunningLow || g[0].Rows[0].Cells[0] != "Blue PLA" || g[0].Rows[0].Cells[1] != "200 g" {
		t.Errorf("shopping list: %+v", g)
	}

	// As CSV, named after the report.
	rec = doJSON(t, srv, reader, http.MethodGet, "/api/v1/reports/"+runningLow+"/sections/1/csv", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Header().Get("Content-Disposition"), `filename="Running-low.csv"`) ||
		rec.Body.String() != "\xef\xbb\xbfGroup,Asset,In stock,Where\nFilament,Blue PLA,200 g,\n" {
		t.Errorf("csv: %d %q %q", rec.Code, rec.Header().Get("Content-Disposition"), rec.Body)
	}
	for path, want := range map[string]int{
		"/api/v1/reports/" + runningLow + "/sections/2/csv": http.StatusNotFound,
		"/api/v1/reports/" + runningLow + "/sections/0/csv": http.StatusBadRequest,
		"/api/v1/reports/missing/sections/1/csv":            http.StatusNotFound,
		"/api/v1/reports/missing/run":                       http.StatusNotFound,
	} {
		if rec := doJSON(t, srv, reader, http.MethodGet, path, ""); rec.Code != want {
			t.Errorf("%s: %d, want %d", path, rec.Code, want)
		}
	}

	// Saving and previewing need reports:manage; specs are checked.
	rec = doJSON(t, srv, adminCookie, http.MethodPost, "/api/v1/fields", `{"key":"serial","label":"Serial","validation_type":"text"}`)
	var field struct{ Field models.Field }
	_ = json.Unmarshal(rec.Body.Bytes(), &field)
	spec := `{"filter":{"search":"dri"},"sections":[{"type":"table","columns":[{"heading":"Asset","binding":"{{asset.name}} {{field.serial}}"}]}]}`
	body := `{"name":"Drills","spec":` + spec + `}`
	if rec := doJSON(t, srv, reader, http.MethodPost, "/api/v1/reports", body); rec.Code != http.StatusForbidden {
		t.Errorf("a reader saved a report: %d", rec.Code)
	}
	if rec := doJSON(t, srv, reader, http.MethodPost, "/api/v1/reports/preview", `{"spec":`+spec+`}`); rec.Code != http.StatusForbidden {
		t.Errorf("a reader previewed: %d", rec.Code)
	}
	rec = doJSON(t, srv, adminCookie, http.MethodPost, "/api/v1/reports/preview", `{"spec":`+spec+`}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"cells":["Drill "]`) {
		t.Errorf("preview: %d %s", rec.Code, rec.Body)
	}
	for bad, want := range map[string]int{
		`{"name":"x"}`:                        http.StatusBadRequest,
		`{"name":"","spec":` + spec + `}`:     http.StatusBadRequest,
		`{"name":"x","spec":{"sections":[]}}`: http.StatusBadRequest,
		`{"name":"x","spec":{"sections":[{"type":"table","columns":[{"heading":"A","binding":"{{field.nope}}"}]}]}}`: http.StatusBadRequest,
	} {
		if rec := doJSON(t, srv, adminCookie, http.MethodPost, "/api/v1/reports", bad); rec.Code != want {
			t.Errorf("%s: %d %s", bad, rec.Code, rec.Body)
		}
	}
	rec = doJSON(t, srv, adminCookie, http.MethodPost, "/api/v1/reports", body)
	var saved struct{ Report models.Report }
	_ = json.Unmarshal(rec.Body.Bytes(), &saved)
	if rec.Code != http.StatusCreated || saved.Report.Name != "Drills" {
		t.Fatalf("save: %d %s", rec.Code, rec.Body)
	}
	path := "/api/v1/reports/" + saved.Report.ID
	if rec := doJSON(t, srv, adminCookie, http.MethodPut, path, `{"name":"Power drills","spec":`+spec+`}`); rec.Code != http.StatusOK {
		t.Errorf("edit: %d %s", rec.Code, rec.Body)
	}

	// A field the report names is deleted: it says so rather than failing.
	if rec := doJSON(t, srv, adminCookie, http.MethodDelete, "/api/v1/fields/"+field.Field.ID, ""); rec.Code != http.StatusOK {
		t.Fatalf("deleting the field: %d %s", rec.Code, rec.Body)
	}
	if rec := doJSON(t, srv, reader, http.MethodGet, path+"/run", ""); rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "Edit it") {
		t.Errorf("a report naming a deleted field: %d %s", rec.Code, rec.Body)
	}

	if rec := doJSON(t, srv, reader, http.MethodDelete, path, ""); rec.Code != http.StatusForbidden {
		t.Errorf("a reader deleted a report: %d", rec.Code)
	}
	if rec := doJSON(t, srv, adminCookie, http.MethodDelete, path, ""); rec.Code != http.StatusOK {
		t.Errorf("delete: %d", rec.Code)
	}
	if rec := doJSON(t, srv, reader, http.MethodGet, path, ""); rec.Code != http.StatusNotFound {
		t.Errorf("a deleted report: %d", rec.Code)
	}
}

func TestReportChartsAPI(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/assets", `{"name":"Screws","stock":{"quantity":100,"unit":"pcs"}}`)
	var a struct{ Asset models.AssetWithDetails }
	_ = json.Unmarshal(rec.Body.Bytes(), &a)
	doJSON(t, srv, cookie, http.MethodPost, "/api/v1/assets/"+a.Asset.Asset.ID+"/quantity", `{"adjust":-30}`)
	doJSON(t, srv, cookie, http.MethodPost, "/api/v1/assets", `{"name":"Drill"}`)

	spec := `{"sections":[
		{"type":"chart","title":"By unit","chart":"donut","group_by":"unit","measure":"assets"},
		{"type":"chart","title":"Used","chart":"line","measure":"used","period":"month"}
	]}`
	rec = doJSON(t, srv, cookie, http.MethodPost, "/api/v1/reports/preview", `{"spec":`+spec+`}`)
	var preview struct{ Result reports.Result }
	_ = json.Unmarshal(rec.Body.Bytes(), &preview)
	if rec.Code != http.StatusOK || len(preview.Result.Sections) != 2 {
		t.Fatalf("preview: %d %s", rec.Code, rec.Body)
	}
	donut := preview.Result.Sections[0]
	if donut.Chart != "donut" || len(donut.Series) != 1 || len(donut.Series[0].Points) != 2 {
		t.Errorf("donut: %+v", donut)
	}
	line := preview.Result.Sections[1].Series
	if len(line) != 1 || line[0].Label != "Used (pcs)" || line[0].Points[reports.Periods-1].Value != 30 {
		t.Errorf("line: %+v", line)
	}

	rec = doJSON(t, srv, cookie, http.MethodPost, "/api/v1/reports", `{"name":"Use","spec":`+spec+`}`)
	var saved struct{ Report models.Report }
	_ = json.Unmarshal(rec.Body.Bytes(), &saved)
	rec = doJSON(t, srv, cookie, http.MethodGet, "/api/v1/reports/"+saved.Report.ID+"/sections/2/csv", "")
	if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Body.String(), "\xef\xbb\xbfPeriod,Used (pcs)\n") || !strings.HasSuffix(rec.Body.String(), ",30\n") {
		t.Errorf("csv: %d %q", rec.Code, rec.Body)
	}
	if rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/reports/preview", `{"spec":{"sections":[{"type":"chart","chart":"pie"}]}}`); rec.Code != http.StatusBadRequest {
		t.Errorf("a pie: %d", rec.Code)
	}
}
