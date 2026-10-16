package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
)

func TestRestoreAssetAPI(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	adminCookie := sessionCookie(t, srv, admin.ID)
	writer := userWithPermissions(t, srv, "writer@hackspace.org", rbac.PermAssetsRead, rbac.PermAssetsWrite)
	reader := userWithPermissions(t, srv, "reader@hackspace.org", rbac.PermAssetsRead)

	rec := doJSON(t, srv, adminCookie, http.MethodPost, "/api/v1/assets", `{"name":"Saw","generator_keys":[{"generator_type":"qr","assigned_code":"S-1"},{"generator_type":"code128","assigned_code":"C-1"}]}`)
	var created struct{ Asset models.AssetWithDetails }
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil || rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	id := created.Asset.Asset.ID
	path := "/api/v1/assets/" + id + "/restore"
	if rec := doJSON(t, srv, adminCookie, http.MethodPost, path, ""); rec.Code != http.StatusConflict {
		t.Errorf("restoring an active asset: %d %s", rec.Code, rec.Body)
	}
	if rec := doJSON(t, srv, adminCookie, http.MethodDelete, "/api/v1/assets/"+id, ""); rec.Code != http.StatusOK {
		t.Fatalf("archive: %d %s", rec.Code, rec.Body)
	}

	// The archived list, for anyone who can read assets.
	list := func(query string) (int, []models.AssetWithDetails) {
		t.Helper()
		rec := doJSON(t, srv, reader, http.MethodGet, "/api/v1/assets"+query, "")
		var body struct{ Assets []models.AssetWithDetails }
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		return rec.Code, body.Assets
	}
	if code, assets := list("?archived=true"); code != http.StatusOK || len(assets) != 1 || assets[0].Asset.ID != id {
		t.Errorf("archived list: %d %+v", code, assets)
	}
	if code, assets := list("?archived=false"); code != http.StatusOK || len(assets) != 0 {
		t.Errorf("active list: %d %+v", code, assets)
	}
	if code, _ := list("?archived=yes"); code != http.StatusBadRequest {
		t.Errorf("archived=yes: %d", code)
	}

	// Restoring needs assets.delete, as archiving does.
	for _, who := range []*http.Cookie{reader, writer} {
		if rec := doJSON(t, srv, who, http.MethodPost, path, ""); rec.Code != http.StatusForbidden {
			t.Errorf("restored without assets.delete: %d", rec.Code)
		}
	}
	if rec := doJSON(t, srv, adminCookie, http.MethodPost, "/api/v1/assets/missing/restore", ""); rec.Code != http.StatusNotFound {
		t.Errorf("a missing asset: %d", rec.Code)
	}

	// A code taken meanwhile is reported, not restored.
	if rec := doJSON(t, srv, adminCookie, http.MethodPost, "/api/v1/assets", `{"name":"Other saw","generator_keys":[{"generator_type":"qr","assigned_code":"S-1"}]}`); rec.Code != http.StatusCreated {
		t.Fatalf("taking the code: %d %s", rec.Code, rec.Body)
	}
	rec = doJSON(t, srv, adminCookie, http.MethodPost, path, "")
	var restored struct {
		Asset        models.AssetWithDetails
		SkippedCodes []models.AssetGeneratorKey `json:"skipped_codes"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &restored)
	if rec.Code != http.StatusOK || restored.Asset.Asset.ArchivedAt != nil || len(restored.Asset.GeneratorKeys) != 1 ||
		len(restored.SkippedCodes) != 1 || restored.SkippedCodes[0].AssignedCode != "S-1" {
		t.Errorf("restore: %d %s", rec.Code, rec.Body)
	}

	// The history names who restored it.
	rec = doJSON(t, srv, reader, http.MethodGet, "/api/v1/assets/"+id+"/history", "")
	var history struct{ Events []models.Event }
	_ = json.Unmarshal(rec.Body.Bytes(), &history)
	if len(history.Events) == 0 || history.Events[0].Kind != models.EventAssetRestored || history.Events[0].Payload.Actor == nil || history.Events[0].Payload.Actor.ID != admin.ID {
		t.Errorf("history: %s", rec.Body)
	}
}
