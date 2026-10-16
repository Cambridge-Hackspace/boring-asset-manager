package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

const binTemplate = `{"name":"Bins","kind":"scan","spec":{"sections":[
	{"type":"details","columns":[{"heading":"Bin","binding":"{{asset.name}}"}]},
	{"type":"table","title":"In it","source":"contents","columns":[{"heading":"What","binding":"{{asset.name}}"}]},
	{"type":"actions","actions":["open","use"]}]}}`

func TestScanTemplatesThroughTheAPI(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	adminCookie := sessionCookie(t, srv, admin.ID)
	st := srv.store
	categorizer := userWithPermissions(t, srv, "cat@hackspace.org", rbac.PermAssetsRead, rbac.PermCategoriesManage)
	expect := func(what string, cookie *http.Cookie, method, path, body string, want int) string {
		t.Helper()
		rec := doJSON(t, srv, cookie, method, path, body)
		if rec.Code != want {
			t.Errorf("%s: %d, want %d: %s", what, rec.Code, want, rec.Body)
		}
		return rec.Body.String()
	}

	var saved struct{ Report models.Report }
	_ = json.Unmarshal([]byte(expect("creating one", adminCookie, http.MethodPost, "/api/v1/reports", binTemplate, http.StatusCreated)), &saved)
	bins := saved.Report
	if bins.Kind != "scan" || bins.Default {
		t.Errorf("created: %+v", bins)
	}
	expect("a report's spec as a scan template", adminCookie, http.MethodPost, "/api/v1/reports",
		`{"name":"X","kind":"scan","spec":{"sections":[{"type":"table","columns":[{"heading":"A","binding":"x"}]}]}}`, http.StatusBadRequest)
	expect("an unknown kind", adminCookie, http.MethodPost, "/api/v1/reports", `{"name":"X","kind":"poster","spec":{"sections":[]}}`, http.StatusBadRequest)
	expect("changing its kind", adminCookie, http.MethodPut, "/api/v1/reports/"+bins.ID, strings.Replace(binTemplate, `"kind":"scan"`, `"kind":"report"`, 1), http.StatusBadRequest)
	expect("editing it", adminCookie, http.MethodPut, "/api/v1/reports/"+bins.ID, strings.Replace(binTemplate, `"Bins"`, `"Bins and boxes"`, 1), http.StatusOK)
	if body := expect("listing scan templates", adminCookie, http.MethodGet, "/api/v1/reports?kind=scan", "", http.StatusOK); !strings.Contains(body, "Bins and boxes") || !strings.Contains(body, `"name":"Scanned"`) || strings.Contains(body, "Running low") {
		t.Errorf("listing scan templates: %s", body)
	}
	expect("running it as a report", adminCookie, http.MethodGet, "/api/v1/reports/"+bins.ID+"/run", "", http.StatusConflict)
	expect("its CSV", adminCookie, http.MethodGet, "/api/v1/reports/"+bins.ID+"/sections/1/csv", "", http.StatusConflict)

	// A bin and what is in it, previewed.
	bin, _ := st.CreateAsset("Bin A3", nil, nil, nil)
	screws, _ := st.CreateAsset("Screws", nil, nil, nil)
	if _, err := st.AddLink(bin.Asset.ID, containsType, screws.Asset.ID, true); err != nil {
		t.Fatal(err)
	}
	body := expect("previewing", adminCookie, http.MethodPost, "/api/v1/reports/preview",
		`{"kind":"scan","asset_id":"`+bin.Asset.ID+`","spec":`+binTemplate[strings.Index(binTemplate, `{"sections"`):len(binTemplate)-1]+`}`, http.StatusOK)
	if !strings.Contains(body, `"cells":["Bin A3"]`) || !strings.Contains(body, `"cells":["Screws"]`) || !strings.Contains(body, `"actions":["open","use"]`) {
		t.Errorf("previewing: %s", body)
	}
	expect("previewing without an asset", adminCookie, http.MethodPost, "/api/v1/reports/preview", `{"kind":"scan","spec":{"sections":[{"type":"actions","actions":["open"]}]}}`, http.StatusNotFound)

	// Categories name templates; conflicts show, and are found.
	tools, _ := st.CreateCategory("Tools", nil, "", false, false)
	kitchen, _ := st.CreateCategory("Kitchen", nil, "", false, false)
	expect("naming one, as a reader", nil, http.MethodPut, "/api/v1/categories/"+tools.ID+"/scan-template", `{"scan_template_id":"`+bins.ID+`"}`, http.StatusUnauthorized)
	expect("naming one", categorizer, http.MethodPut, "/api/v1/categories/"+tools.ID+"/scan-template", `{"scan_template_id":"`+bins.ID+`"}`, http.StatusOK)
	expect("naming a report", categorizer, http.MethodPut, "/api/v1/categories/"+kitchen.ID+"/scan-template", `{"scan_template_id":"3a6c4e8b-0d2f-4b7a-9c1e-5f7a9b1d3e17"}`, http.StatusBadRequest)
	expect("naming nothing that exists", categorizer, http.MethodPut, "/api/v1/categories/"+kitchen.ID+"/scan-template", `{"scan_template_id":"nope"}`, http.StatusBadRequest)
	both, _ := st.CreateAsset("Kettle", []string{tools.ID, kitchen.ID}, nil, nil)
	if body := expect("before the kitchen names one", categorizer, http.MethodGet, "/api/v1/categories/scan-conflicts?category_id="+kitchen.ID+"&scan_template_id="+defaultScanTemplate, "", http.StatusOK); !strings.Contains(body, kitchen.ID) || !strings.Contains(body, tools.ID) {
		t.Errorf("the warning before saving: %s", body)
	}
	expect("the kitchen naming the default", categorizer, http.MethodPut, "/api/v1/categories/"+kitchen.ID+"/scan-template", `{"scan_template_id":"`+defaultScanTemplate+`"}`, http.StatusOK)
	if body := expect("conflicts", categorizer, http.MethodGet, "/api/v1/categories/scan-conflicts", "", http.StatusOK); !strings.Contains(body, `"`+kitchen.ID+`":1`) {
		t.Errorf("conflicts: %s", body)
	}
	if body := expect("conflicted assets", adminCookie, http.MethodGet, "/api/v1/assets?conflict=scan_template", "", http.StatusOK); !strings.Contains(body, both.Asset.ID) || strings.Contains(body, "Bin A3") {
		t.Errorf("conflicted assets: %s", body)
	}
	expect("an unknown conflict", adminCookie, http.MethodGet, "/api/v1/assets?conflict=other", "", http.StatusBadRequest)
	expect("clearing it", categorizer, http.MethodPut, "/api/v1/categories/"+kitchen.ID+"/scan-template", `{"scan_template_id":null}`, http.StatusOK)

	// The default: another can be made it; one in use, or the default, stays.
	expect("deleting one in use", adminCookie, http.MethodDelete, "/api/v1/reports/"+bins.ID, "", http.StatusConflict)
	expect("deleting the default", adminCookie, http.MethodDelete, "/api/v1/reports/"+defaultScanTemplate, "", http.StatusConflict)
	expect("a report as the default", adminCookie, http.MethodPost, "/api/v1/reports/3a6c4e8b-0d2f-4b7a-9c1e-5f7a9b1d3e17/default", "", http.StatusBadRequest)
	if body := expect("a new default", adminCookie, http.MethodPost, "/api/v1/reports/"+bins.ID+"/default", "", http.StatusOK); !strings.Contains(body, `"default":true`) {
		t.Errorf("a new default: %s", body)
	}
	expect("deleting the old default", adminCookie, http.MethodDelete, "/api/v1/reports/"+defaultScanTemplate, "", http.StatusOK)
}

// defaultScanTemplate is the scan template migration 022 seeds.
const defaultScanTemplate = "bc4e2a6d-8f0b-4d5c-9e9a-d15c7d9f1a22"

func TestScanningACode(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	adminCookie := sessionCookie(t, srv, admin.ID)
	st := srv.store
	expect := func(what string, cookie *http.Cookie, path string, want int) string {
		t.Helper()
		rec := doJSON(t, srv, cookie, http.MethodGet, path, "")
		if rec.Code != want {
			t.Errorf("%s: %d, want %d: %s", what, rec.Code, want, rec.Body)
		}
		return rec.Body.String()
	}
	scopes, _ := st.ListScopes()
	var public string
	for _, sc := range scopes {
		if sc.Name == rbac.PublicScope {
			public = sc.ID
		}
	}
	lobby, _ := st.CreateCategory("Lobby", nil, "", false, false)
	if _, err := st.SetCategoryScopes(lobby.ID, []string{public}); err != nil {
		t.Fatal(err)
	}
	bin, _ := st.CreateAsset("Bin A3", []string{lobby.ID}, []models.AssetGeneratorKey{{GeneratorType: "code128", AssignedCode: "BIN-A3"}}, nil)
	screws, _ := st.CreateAssetWithStock("Screws", []string{lobby.ID}, nil, nil, &store.StockInput{Unit: "pcs"})
	if _, err := st.AddLink(bin.Asset.ID, containsType, screws.Asset.ID, true); err != nil {
		t.Fatal(err)
	}
	safe, _ := st.CreateAsset("Safe", nil, []models.AssetGeneratorKey{{GeneratorType: "code128", AssignedCode: "SAFE"}}, nil)

	// By code, or by ID; the default template shows what the bin holds.
	body := expect("by code", adminCookie, "/api/v1/scan/BIN-A3", http.StatusOK)
	if !strings.Contains(body, `"name":"Bin A3"`) || !strings.Contains(body, `"cells":["Screws",`) || !strings.Contains(body, `"template":{"id":"`+defaultScanTemplate+`","name":"Scanned"}`) {
		t.Errorf("by code: %s", body)
	}
	expect("by ID", adminCookie, "/api/v1/scan/"+bin.Asset.ID, http.StatusOK)
	expect("an unknown code", adminCookie, "/api/v1/scan/NOPE", http.StatusNotFound)
	// The admin may take every action that applies.
	if body := expect("the screws", adminCookie, "/api/v1/scan/"+screws.Asset.ID, http.StatusOK); !strings.Contains(body, `"actions":["open","use","report"]`) || !strings.Contains(body, `"counted":true,"unit":"pcs"`) {
		t.Errorf("the screws, as the admin: %s", body)
	}

	// Signed out, the public role scans what is public, and may only look.
	if body := expect("signed out", nil, "/api/v1/scan/BIN-A3", http.StatusOK); !strings.Contains(body, `"actions":["open"]`) {
		t.Errorf("signed out: %s", body)
	}
	expect("the safe, signed out", nil, "/api/v1/scan/SAFE", http.StatusNotFound)
	expect("the safe, as the admin", adminCookie, "/api/v1/scan/SAFE", http.StatusOK)

	// Without assets.scan at all, it is refused.
	nothingPublic(t, srv)
	expect("signed out, nothing public", nil, "/api/v1/scan/BIN-A3", http.StatusUnauthorized)

	// Archived, it is gone; two assets with the code, as codes of
	// different kinds, are named apart.
	if err := st.DeleteAsset(safe.Asset.ID); err != nil {
		t.Fatal(err)
	}
	expect("archived", adminCookie, "/api/v1/scan/SAFE", http.StatusNotFound)
	if _, err := st.CreateAsset("Box", nil, []models.AssetGeneratorKey{{GeneratorType: "qr", AssignedCode: "BIN-A3"}}, nil); err != nil {
		t.Fatal(err)
	}
	expect("a code two assets have", adminCookie, "/api/v1/scan/BIN-A3", http.StatusConflict)
}
