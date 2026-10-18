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

func assetNames(t *testing.T, srv *Server, cookie *http.Cookie, path string) []string {
	t.Helper()
	rec := doJSON(t, srv, cookie, http.MethodGet, path, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("%s: %d %s", path, rec.Code, rec.Body)
	}
	var resp struct{ Assets []models.AssetWithDetails }
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	names := []string{}
	for _, a := range resp.Assets {
		names = append(names, a.Asset.Name)
	}
	return names
}

func TestSearchingAssetsFindsWhatIsInAPlace(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	st := srv.store

	// A drill in a box on Shelf A; the box is in a locked room only the
	// admin may see.
	tools, _ := st.CreateScope("tools")
	toolsCat, _ := st.CreateCategory("Tools", nil, "", false, false)
	_, _ = st.SetCategoryScopes(toolsCat.ID, []string{tools.ID})
	shelf, _ := st.CreateAsset("Shelf A", []string{toolsCat.ID}, nil, nil)
	box, _ := st.CreateAsset("Red box", []string{toolsCat.ID}, nil, nil)
	drill, _ := st.CreateAsset("Drill", []string{toolsCat.ID}, nil, nil)
	room, _ := st.CreateAsset("Locked room", nil, nil, nil)
	_, _ = st.CreateAsset("Saw", []string{toolsCat.ID}, nil, nil)
	for _, l := range [][2]string{{room.Asset.ID, shelf.Asset.ID}, {shelf.Asset.ID, box.Asset.ID}, {box.Asset.ID, drill.Asset.ID}} {
		if _, err := st.AddLink(l[0], containsType, l[1], true); err != nil {
			t.Fatal(err)
		}
	}

	// "shelf a" finds the shelf, and what is on it at any depth.
	if got := assetNames(t, srv, cookie, "/api/v1/assets?search=shelf%20a"); len(got) != 3 {
		t.Errorf("searching for the shelf: %v", got)
	}

	// Someone who may not read the room does not find things by its name.
	role, _ := st.CreateRole(store.RoleInput{Name: "Tool keepers", Permissions: []string{"tools:" + rbac.PermAssetsRead}})
	keeperUser, _ := st.CreateNamedUser("keeper@hackspace.org", "Keeper", "UserSecret123!", true)
	_ = st.SetUserRoles(keeperUser.ID, []string{role.ID})
	keeper := sessionCookie(t, srv, keeperUser.ID)
	if got := assetNames(t, srv, cookie, "/api/v1/assets?search=locked"); len(got) != 4 {
		t.Errorf("the admin searching for the room: %v", got)
	}
	if got := assetNames(t, srv, keeper, "/api/v1/assets?search=locked"); len(got) != 0 {
		t.Errorf("the keeper searching for the room they cannot see: %v", got)
	}
	if got := assetNames(t, srv, keeper, "/api/v1/assets?search=red%20box"); len(got) != 2 {
		t.Errorf("the keeper searching for the box: %v", got)
	}
	// The CSV export searches the same way.
	rec := doJSON(t, srv, keeper, http.MethodGet, "/api/v1/assets/export.csv?search=locked", "")
	if rec.Code != http.StatusOK || len(rec.Body.String()) == 0 || strings.Contains(rec.Body.String(), "Drill") || strings.Contains(rec.Body.String(), "Shelf A") {
		t.Errorf("the keeper exporting by the room's name: %d %s", rec.Code, rec.Body)
	}
}

func TestTheNextCodeFollowsTheHighest(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	next := func(genType string) (int, string) {
		rec := doJSON(t, srv, cookie, http.MethodGet, "/api/v1/generators/"+genType+"/next-code", "")
		var resp struct{ Code string }
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		return rec.Code, resp.Code
	}
	if code, got := next("qr"); code != http.StatusOK || got != "" {
		t.Errorf("with no codes: %d %q", code, got)
	}
	for _, c := range []string{"HS-099", "HS-105", "HS-7", "NOTE"} {
		if _, err := srv.store.CreateAsset(c, nil, []models.AssetGeneratorKey{{GeneratorType: "qr", AssignedCode: c}}, nil); err != nil {
			t.Fatal(err)
		}
	}
	if _, got := next("qr"); got != "HS-106" {
		t.Errorf("after HS-105: %q", got)
	}
	// Taken already, it skips on; and padding is kept.
	_, _ = srv.store.CreateAsset("Taken", nil, []models.AssetGeneratorKey{{GeneratorType: "qr", AssignedCode: "HS-106"}}, nil)
	_, _ = srv.store.CreateAsset("Bin", nil, []models.AssetGeneratorKey{{GeneratorType: "code128", AssignedCode: "BIN-0099"}}, nil)
	if _, got := next("qr"); got != "HS-107" {
		t.Errorf("after HS-106 too: %q", got)
	}
	if _, got := next("code128"); got != "BIN-0100" {
		t.Errorf("after BIN-0099: %q", got)
	}
	if code, _ := next("nope"); code != http.StatusNotFound {
		t.Errorf("an unknown type: %d", code)
	}
	reader := userWithPermissions(t, srv, "reader@example.com", "assets.read")
	if rec := doJSON(t, srv, reader, http.MethodGet, "/api/v1/generators/qr/next-code", ""); rec.Code != http.StatusForbidden {
		t.Errorf("for a reader: %d", rec.Code)
	}
}
