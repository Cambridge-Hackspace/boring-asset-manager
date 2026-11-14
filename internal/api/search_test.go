package api

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// filamentShelf is three spools of filament with a diameter and a
// material, one of them on a shelf in a room only the admin may see.
func filamentShelf(t *testing.T, srv *Server) (filament *models.Category, shelf *models.AssetWithDetails) {
	t.Helper()
	st := srv.store
	opts := json.RawMessage(`{"choices":[{"value":"tpu","label":"TPU (flexible)"},{"value":"pla","label":"PLA"}]}`)
	diameter, err := st.CreateField("diameter", "Diameter", "number", nil)
	if err != nil {
		t.Fatal(err)
	}
	material, err := st.CreateField("material", "Material", "select", opts)
	if err != nil {
		t.Fatal(err)
	}
	scope, _ := st.CreateScope("shop")
	filament, _ = st.CreateCategory("Filament", nil, "", false, false)
	_, _ = st.SetCategoryScopes(filament.ID, []string{scope.ID})
	for _, f := range []string{diameter.ID, material.ID} {
		if err := st.AssignCategoryField(filament.ID, f, false); err != nil {
			t.Fatal(err)
		}
	}
	for _, a := range []struct{ name, diameter, material string }{
		{"Black TPU", "1.75", "tpu"}, {"White PLA", "1.75", "pla"}, {"Thick PLA", "2.85", "pla"},
	} {
		if _, err := st.CreateAsset(a.name, []string{filament.ID}, nil, map[string]string{"diameter": a.diameter, "material": a.material}); err != nil {
			t.Fatal(err)
		}
	}
	shelf, _ = st.CreateAsset("Shelf A", []string{filament.ID}, nil, nil)
	room, _ := st.CreateAsset("Locked room", nil, nil, nil)
	if _, err := st.AddLink(room.Asset.ID, containsType, shelf.Asset.ID, true); err != nil {
		t.Fatal(err)
	}
	return filament, shelf
}

func searchPath(search string) string {
	return "/api/v1/assets?search=" + url.QueryEscape(search)
}

func TestSearchingByFieldValuesAndQueries(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	_, shelf := filamentShelf(t, srv)
	st := srv.store
	for _, a := range []string{"Black TPU", "Thick PLA"} {
		ids, _ := st.ListAssets("", a)
		if _, err := st.AddLink(shelf.Asset.ID, containsType, ids[0].Asset.ID, true); err != nil {
			t.Fatal(err)
		}
	}

	for search, want := range map[string]string{
		// Plain words find a field's value, and a choice's label.
		"2.85":     "Thick PLA",
		"flexible": "Black TPU",
		// A query.
		"field.diameter = 1.75 ORDER BY name":                                  "Black TPU,White PLA",
		"field.material = PLA AND field.diameter > 2":                          "Thick PLA",
		`in = "shelf a" ORDER BY field.diameter DESC`:                          "Thick PLA,Black TPU",
		`category = filament AND NOT in IS NOT EMPTY AND field.diameter < 2.5`: "White PLA",
		`"black" OR field.diameter >= 2.85`:                                    "Thick PLA,Black TPU",
	} {
		if got := strings.Join(assetNames(t, srv, cookie, searchPath(search)), ","); got != want {
			t.Errorf("%s found %q, want %q", search, got, want)
		}
	}
}

// A search that finds nothing, not even near its words, answers with an
// empty list rather than null, which clients read as a list.
func TestASearchThatFindsNothingListsNothing(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	filamentShelf(t, srv)
	for _, search := range []string{"qqqqzzzzxx", `name ~~ "qqqqzzzzxx"`} {
		rec := doJSON(t, srv, cookie, http.MethodGet, searchPath(search), "")
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", search, rec.Code, rec.Body)
		}
		var resp map[string]json.RawMessage
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if got := string(resp["assets"]); got != "[]" {
			t.Errorf("%s answered assets %s, want []", search, got)
		}
	}
}

func TestAQueryMistakeSaysWhere(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	filamentShelf(t, srv)
	for _, path := range []string{searchPath("field.colour = red"), "/api/v1/assets/export.csv?search=" + url.QueryEscape("field.colour = red"), "/api/v1/assets/graph?search=" + url.QueryEscape("field.colour = red")} {
		rec := doJSON(t, srv, cookie, http.MethodGet, path, "")
		var resp QueryErrorResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if rec.Code != http.StatusBadRequest || resp.QueryError.Start != 0 || resp.QueryError.End != len("field.colour") || !strings.Contains(resp.Error, `no field has the key "colour"`) {
			t.Errorf("%s: %d %s", path, rec.Code, rec.Body)
		}
	}
}

func TestAQueryCannotSeeWhatIsHidden(t *testing.T) {
	srv, _, _ := setupTestServer(t)
	_, _ = filamentShelf(t, srv)
	st := srv.store
	role, _ := st.CreateRole(store.RoleInput{Name: "Shop", Permissions: []string{"shop:" + rbac.PermAssetsRead}})
	user, _ := st.CreateNamedUser("keeper@hackspace.org", "Keeper", "UserSecret123!", true)
	_ = st.SetUserRoles(user.ID, []string{role.ID})
	keeper := sessionCookie(t, srv, user.ID)
	// The shelf is in a room the keeper may not see, so they can't find
	// it, or tell that it is somewhere, by the room.
	for search, want := range map[string]string{
		`in = "locked room"`:               "",
		`in ~ locked OR name ~ zz`:         "",
		`name = "shelf a" AND in IS EMPTY`: "Shelf A",
	} {
		if got := strings.Join(assetNames(t, srv, keeper, searchPath(search)), ","); got != want {
			t.Errorf("the keeper's %s found %q, want %q", search, got, want)
		}
	}
}

func TestAQueryFindsArchivedAssetsWhenAsked(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	filamentShelf(t, srv)
	gone, _ := srv.store.ListAssets("", "Thick PLA")
	if err := srv.store.DeleteAsset(gone[0].Asset.ID); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(assetNames(t, srv, cookie, searchPath("archived = true")), ","); got != "Thick PLA" {
		t.Errorf("archived = true found %q", got)
	}
	if got := strings.Join(assetNames(t, srv, cookie, searchPath("field.material = pla ORDER BY name")), ","); got != "White PLA" {
		t.Errorf("a query not about archiving found %q", got)
	}
	if got := strings.Join(assetNames(t, srv, cookie, searchPath("field.material = pla AND archived = false")+"&archived=true"), ","); got != "" {
		t.Errorf("archived=true with archived = false found %q", got)
	}
}

func TestReportsAndExportsTakeQueries(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	filamentShelf(t, srv)

	rec := doJSON(t, srv, cookie, http.MethodGet, "/api/v1/assets/export.csv?search="+url.QueryEscape("field.diameter > 2"), "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Thick PLA") || strings.Contains(rec.Body.String(), "White PLA") {
		t.Errorf("CSV export: %d %s", rec.Code, rec.Body)
	}

	table := `"sections":[{"type":"table","columns":[{"heading":"Asset","binding":"{{asset.name}}"}]}]`
	spec := `{"filter":{"search":"field.material = tpu"},` + table + `}`
	rec = doJSON(t, srv, cookie, http.MethodPost, "/api/v1/reports/preview", `{"spec":`+spec+`}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Black TPU") || strings.Contains(rec.Body.String(), "PLA") {
		t.Errorf("preview: %d %s", rec.Code, rec.Body)
	}
	bad := `{"filter":{"search":"field.material = abs"},` + table + `}`
	if rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/reports", `{"name":"ABS","spec":`+bad+`}`); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "choices") {
		t.Errorf("saving a report whose query can't run: %d %s", rec.Code, rec.Body)
	}
}

func TestAgentsSearchWithQueries(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	adminCookie := sessionCookie(t, srv, admin.ID)
	filamentShelf(t, srv)
	_, secret := personalToken(t, srv, adminCookie, "Claude")
	cs := mcpSession(t, srv, secret)

	var found struct {
		Assets []summary
	}
	structured(t, callTool(t, cs, "search_assets", map[string]any{"search": "field.diameter = 1.75 ORDER BY name DESC"}), &found)
	if len(found.Assets) != 2 || found.Assets[0].Name != "White PLA" || found.Assets[1].Name != "Black TPU" {
		t.Errorf("found %+v", found.Assets)
	}
	if msg := toolError(t, cs, "search_assets", map[string]any{"search": "field.diameter ~ 1"}); !strings.Contains(msg, "is a number") {
		t.Errorf("a mistake: %s", msg)
	}
}
