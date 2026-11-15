package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
)

type savedSearchesBody struct {
	SavedSearches []SavedSearchResponse `json:"saved_searches"`
	SavedSearch   SavedSearchResponse   `json:"saved_search"`
	Assets        []struct {
		Asset struct{ Name string } `json:"asset"`
	} `json:"assets"`
	QueryError *QueryError `json:"query_error"`
}

func savedCall(t *testing.T, srv *Server, cookie *http.Cookie, method, path, body string, want int) savedSearchesBody {
	t.Helper()
	rec := doJSON(t, srv, cookie, method, path, body)
	if rec.Code != want {
		t.Fatalf("%s %s: %d %s, want %d", method, path, rec.Code, rec.Body, want)
	}
	var out savedSearchesBody
	if rec.Body.Len() > 0 {
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}
	}
	return out
}

func savedNamesOf(list []SavedSearchResponse) string {
	names := []string{}
	for _, v := range list {
		names = append(names, v.Name)
	}
	return strings.Join(names, ",")
}

func TestSavingSearchesAndSharingThem(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	adminCookie := sessionCookie(t, srv, admin.ID)
	filamentShelf(t, srv)
	reader := userWithPermissions(t, srv, "reader@test.org", rbac.PermAssetsRead)
	other := userWithPermissions(t, srv, "other@test.org", rbac.PermAssetsRead)

	// Anyone who reads assets saves a search for themselves, plain words or a query.
	thin := savedCall(t, srv, reader, http.MethodPost, "/api/v1/searches", `{"name":"Thin filament","search":"field.diameter < 2 ORDER BY name"}`, http.StatusCreated).SavedSearch
	if !thin.Mine || thin.Shared || thin.Owner.Name != "reader@test.org" || thin.Problem != nil {
		t.Errorf("saved %+v", thin)
	}
	savedCall(t, srv, reader, http.MethodPost, "/api/v1/searches", `{"name":"PLA","search":"pla"}`, http.StatusCreated)
	// A second of one name, a query that cannot run, and sharing
	// without searches.share, are each refused.
	savedCall(t, srv, reader, http.MethodPost, "/api/v1/searches", `{"name":"pla","search":"tpu"}`, http.StatusConflict)
	if bad := savedCall(t, srv, reader, http.MethodPost, "/api/v1/searches", `{"name":"Bad","search":"field.colour = red"}`, http.StatusBadRequest); bad.QueryError == nil || bad.QueryError.Start != 0 || bad.QueryError.End != 12 {
		t.Errorf("a query that cannot run: %+v", bad.QueryError)
	}
	savedCall(t, srv, reader, http.MethodPost, "/api/v1/searches", `{"name":"Shared","search":"pla","shared":true}`, http.StatusForbidden)
	savedCall(t, srv, reader, http.MethodPost, "/api/v1/searches", `{"name":"","search":"pla"}`, http.StatusBadRequest)

	// Run, it finds what its search finds.
	ran := savedCall(t, srv, reader, http.MethodGet, "/api/v1/searches/"+thin.ID+"/assets", "", http.StatusOK)
	if names := assetNamesOf(ran); names != "Black TPU,White PLA" {
		t.Errorf("ran it and found %q", names)
	}

	// Unshared, it is the reader's alone; the admin, who holds
	// searches.share, shares one that everyone who reads assets sees.
	if got := savedNamesOf(savedCall(t, srv, other, http.MethodGet, "/api/v1/searches", "", http.StatusOK).SavedSearches); got != "" {
		t.Errorf("another sees %q", got)
	}
	savedCall(t, srv, other, http.MethodGet, "/api/v1/searches/"+thin.ID+"/assets", "", http.StatusNotFound)
	shared := savedCall(t, srv, adminCookie, http.MethodPost, "/api/v1/searches", `{"name":"All filament","search":"category = filament","shared":true}`, http.StatusCreated).SavedSearch
	list := savedCall(t, srv, reader, http.MethodGet, "/api/v1/searches", "", http.StatusOK).SavedSearches
	if got := savedNamesOf(list); got != "PLA,Thin filament,All filament" {
		t.Errorf("the reader sees %q", got)
	}
	if list[2].Mine || !list[2].Shared {
		t.Errorf("the shared one, to the reader: %+v", list[2])
	}
	// Only its person changes or deletes one.
	savedCall(t, srv, reader, http.MethodPut, "/api/v1/searches/"+shared.ID, `{"name":"Mine now","search":"pla"}`, http.StatusNotFound)
	savedCall(t, srv, reader, http.MethodDelete, "/api/v1/searches/"+shared.ID, "", http.StatusNotFound)
	renamed := savedCall(t, srv, reader, http.MethodPut, "/api/v1/searches/"+thin.ID, `{"name":"Thin ones","search":"field.diameter < 2"}`, http.StatusOK).SavedSearch
	if renamed.Name != "Thin ones" || renamed.Search != "field.diameter < 2" {
		t.Errorf("renamed to %+v", renamed)
	}
	savedCall(t, srv, reader, http.MethodPut, "/api/v1/searches/"+thin.ID, `{"name":"Thin ones","search":"pla","shared":true}`, http.StatusForbidden)

	// A field it names deleted, it says so, and where, rather than vanishing.
	fields, _ := srv.store.ListFields()
	for _, f := range fields {
		if f.Key == "diameter" {
			if err := srv.store.DeleteField(f.ID); err != nil {
				t.Fatal(err)
			}
		}
	}
	list = savedCall(t, srv, reader, http.MethodGet, "/api/v1/searches", "", http.StatusOK).SavedSearches
	if list[1].Name != "Thin ones" || list[1].Problem == nil || list[1].Problem.Start != 0 || list[1].Problem.End != len("field.diameter") {
		t.Errorf("broken, it is listed as %+v", list[1])
	}
	if ran := savedCall(t, srv, reader, http.MethodGet, "/api/v1/searches/"+thin.ID+"/assets", "", http.StatusBadRequest); ran.QueryError == nil {
		t.Error("run broken, it gave no query error")
	}

	savedCall(t, srv, reader, http.MethodDelete, "/api/v1/searches/"+thin.ID, "", http.StatusNoContent)
	if got := savedNamesOf(savedCall(t, srv, reader, http.MethodGet, "/api/v1/searches", "", http.StatusOK).SavedSearches); got != "PLA,All filament" {
		t.Errorf("after deleting, the reader sees %q", got)
	}
}

func assetNamesOf(b savedSearchesBody) string {
	names := []string{}
	for _, a := range b.Assets {
		names = append(names, a.Asset.Name)
	}
	return strings.Join(names, ",")
}

// Nothing saved lists as [], never null.
func TestNoSavedSearchesListAsEmpty(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	rec := doJSON(t, srv, sessionCookie(t, srv, admin.ID), http.MethodGet, "/api/v1/searches", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"saved_searches":[]`) {
		t.Errorf("listed %d %s", rec.Code, rec.Body)
	}
}

// An agent lists, saves, runs, changes and deletes saved searches as the
// person whose token it uses.
func TestAgentsUseSavedSearches(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	adminCookie := sessionCookie(t, srv, admin.ID)
	filamentShelf(t, srv)
	_, secret := personalToken(t, srv, adminCookie, "Claude")
	cs := mcpSession(t, srv, secret)

	var saved struct {
		ID   string
		Name string
		Mine bool
	}
	structured(t, callTool(t, cs, "save_search", map[string]any{"name": "Thin", "search": "field.diameter < 2 ORDER BY name"}), &saved)
	if saved.Name != "Thin" || !saved.Mine {
		t.Errorf("saved %+v", saved)
	}
	if msg := toolError(t, cs, "save_search", map[string]any{"name": "Bad", "search": "field.diameter ~ 1"}); !strings.Contains(msg, "is a number") {
		t.Errorf("a query with a mistake: %s", msg)
	}
	var list struct {
		SavedSearches []struct{ Name string } `json:"saved_searches"`
	}
	structured(t, callTool(t, cs, "list_saved_searches", map[string]any{}), &list)
	if len(list.SavedSearches) != 1 || list.SavedSearches[0].Name != "Thin" {
		t.Errorf("listed %+v", list.SavedSearches)
	}
	var ran struct {
		Total  int
		Assets []summary
	}
	structured(t, callTool(t, cs, "run_saved_search", map[string]any{"id": saved.ID}), &ran)
	if ran.Total != 2 || ran.Assets[0].Name != "Black TPU" || ran.Assets[1].Name != "White PLA" {
		t.Errorf("ran it: %+v", ran)
	}
	structured(t, callTool(t, cs, "change_saved_search", map[string]any{"id": saved.ID, "name": "Thick", "search": "field.diameter > 2", "shared": true}), &saved)
	if saved.Name != "Thick" {
		t.Errorf("changed to %+v", saved)
	}
	callTool(t, cs, "delete_saved_search", map[string]any{"id": saved.ID})
	structured(t, callTool(t, cs, "list_saved_searches", map[string]any{}), &list)
	if len(list.SavedSearches) != 0 {
		t.Errorf("after deleting, listed %+v", list.SavedSearches)
	}
}
