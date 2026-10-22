package api

import (
	"encoding/json"
	"net/http"
	"reflect"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
)

func TestScanningOffersToAddLikeItOrInIt(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	adminCookie := sessionCookie(t, srv, admin.ID)
	st := srv.store
	tmpl, err := st.CreateScanTemplate("Adding", []byte(`{"sections":[{"type":"actions","actions":["open","add_similar","add_inside"]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	bins, _ := st.CreateCategory("Bins", nil, "", false, false)
	if _, err := st.SetCategoryScanTemplate(bins.ID, &tmpl.ID); err != nil {
		t.Fatal(err)
	}
	bin, err := st.CreateAsset("Bin A3", []string{bins.ID}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	actions := func(cookie *http.Cookie) []string {
		t.Helper()
		rec := doJSON(t, srv, cookie, http.MethodGet, "/api/v1/scan/"+bin.Asset.ID, "")
		var got struct {
			Result struct {
				Sections []struct {
					Actions []string `json:"actions"`
				} `json:"sections"`
			} `json:"result"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || rec.Code != http.StatusOK || len(got.Result.Sections) != 1 {
			t.Fatalf("scan: %d %s", rec.Code, rec.Body)
		}
		return got.Result.Sections[0].Actions
	}

	if got := actions(adminCookie); !reflect.DeepEqual(got, []string{"open", "add_similar", "add_inside"}) {
		t.Errorf("the admin: %v", got)
	}
	// Adding needs assets.write.
	looker := userWithPermissions(t, srv, "looker@example.org", rbac.PermAssetsRead, rbac.PermAssetsScan)
	if got := actions(looker); !reflect.DeepEqual(got, []string{"open"}) {
		t.Errorf("one who only looks: %v", got)
	}
	// Nothing is in anything without a link type that makes locations.
	if err := st.DeleteRelationType(containsType); err != nil {
		t.Fatal(err)
	}
	if got := actions(adminCookie); !reflect.DeepEqual(got, []string{"open", "add_similar"}) {
		t.Errorf("without locations: %v", got)
	}
}
