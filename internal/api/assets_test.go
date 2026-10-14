package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/generators"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
)

func TestUpdateAssetOmittedAndEmptyLists(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	cat, err := srv.store.CreateCategory("Tools", nil, "#336699", false, false)
	if err != nil {
		t.Fatal(err)
	}
	asset, err := srv.store.CreateAsset("Drill", []string{cat.ID},
		[]models.AssetGeneratorKey{{GeneratorType: generators.TypeQR, AssignedCode: "DRILL-01"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	put := func(body string) models.AssetWithDetails {
		t.Helper()
		rec := doJSON(t, srv, cookie, http.MethodPut, "/api/v1/assets/"+asset.Asset.ID, body)
		if rec.Code != http.StatusOK {
			t.Fatalf("PUT %s: %d %s", body, rec.Code, rec.Body.String())
		}
		var resp struct {
			Asset models.AssetWithDetails `json:"asset"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
		return resp.Asset
	}

	renamed := put(`{"name":"Cordless Drill"}`)
	if renamed.Asset.Name != "Cordless Drill" || len(renamed.Categories) != 1 || renamed.Categories[0].ID != cat.ID || len(renamed.GeneratorKeys) != 1 {
		t.Errorf("rename changed more than the name: %+v", renamed)
	}
	cleared := put(`{"generator_keys":[]}`)
	if len(cleared.GeneratorKeys) != 0 || len(cleared.Categories) != 1 || cleared.Categories[0].ID != cat.ID {
		t.Errorf(`"generator_keys":[] gave %+v`, cleared)
	}
}

// Empty lists are sent as [], not null: the web interface iterates them.
func TestAssetListsAreNeverNull(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	asset, err := srv.store.CreateAsset("Plain box", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/v1/assets/" + asset.Asset.ID, "/api/v1/assets"} {
		rec := doJSON(t, srv, cookie, http.MethodGet, path, "")
		body := rec.Body.String()
		for _, key := range []string{"categories", "generator_keys", "effective_fields", "field_values"} {
			if strings.Contains(body, `"`+key+`":null`) {
				t.Errorf("GET %s: %s is null: %s", path, key, body)
			}
		}
	}
}
