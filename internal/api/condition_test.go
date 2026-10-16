package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
)

func TestAssetConditionAPI(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	adminCookie := sessionCookie(t, srv, admin.ID)
	reader := userWithPermissions(t, srv, "reader@hackspace.org", rbac.PermAssetsRead)
	reporter := userWithPermissions(t, srv, "member@hackspace.org", rbac.PermAssetsRead, rbac.PermAssetsReport)
	writer := userWithPermissions(t, srv, "writer@hackspace.org", rbac.PermAssetsRead, rbac.PermAssetsWrite)

	rec := doJSON(t, srv, adminCookie, http.MethodPost, "/api/v1/assets", `{"name":"Drill"}`)
	var created struct{ Asset models.AssetWithDetails }
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	path := "/api/v1/assets/" + created.Asset.Asset.ID + "/condition"

	if rec := doJSON(t, srv, reader, http.MethodPut, path, `{"condition":"out_of_service"}`); rec.Code != http.StatusForbidden {
		t.Errorf("a reader reported a problem: %d", rec.Code)
	}
	for _, bad := range []string{`{}`, `{"condition":"broken"}`, `nope`} {
		if rec := doJSON(t, srv, reporter, http.MethodPut, path, bad); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: %d", bad, rec.Code)
		}
	}
	// A member reports it, but only someone who edits assets clears it.
	rec = doJSON(t, srv, reporter, http.MethodPut, path, `{"condition":"out_of_service","note":"Chuck slips"}`)
	var set struct {
		Asset   models.AssetWithDetails
		Changed bool
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &set)
	if rec.Code != http.StatusOK || !set.Changed || set.Asset.Asset.Condition != "out_of_service" || set.Asset.Asset.ConditionNote != "Chuck slips" {
		t.Fatalf("report: %d %s", rec.Code, rec.Body)
	}
	if rec := doJSON(t, srv, reporter, http.MethodPut, path, `{"condition":"ok"}`); rec.Code != http.StatusForbidden {
		t.Errorf("a reporter cleared it: %d", rec.Code)
	}

	// The list finds it among those needing attention.
	list := func(query string) int {
		t.Helper()
		rec := doJSON(t, srv, reader, http.MethodGet, "/api/v1/assets"+query, "")
		var body struct{ Assets []models.AssetWithDetails }
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if rec.Code != http.StatusOK {
			return -rec.Code
		}
		return len(body.Assets)
	}
	if n := list("?condition=problem"); n != 1 {
		t.Errorf("problems: %d", n)
	}
	if n := list("?condition=needs_maintenance"); n != 0 {
		t.Errorf("needing maintenance: %d", n)
	}
	if n := list("?condition=broken"); n != -http.StatusBadRequest {
		t.Errorf("an unknown condition: %d", n)
	}

	if rec := doJSON(t, srv, writer, http.MethodPut, path, `{"condition":"ok","note":"ignored"}`); rec.Code != http.StatusOK {
		t.Errorf("a writer cleared it: %d %s", rec.Code, rec.Body)
	}
	if rec := doJSON(t, srv, writer, http.MethodPut, "/api/v1/assets/missing/condition", `{"condition":"ok"}`); rec.Code != http.StatusNotFound {
		t.Errorf("a missing asset: %d", rec.Code)
	}
}
