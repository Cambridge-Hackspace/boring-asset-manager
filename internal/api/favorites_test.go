package api

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/flows"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// favoriteKinds reads GET /api/v1/auth/me/favorites as kind/name pairs.
func favoriteKinds(t *testing.T, srv *Server, cookie *http.Cookie) []string {
	t.Helper()
	rec := doJSON(t, srv, cookie, http.MethodGet, "/api/v1/auth/me/favorites", "")
	var got struct {
		Favorites []FavoriteResponse `json:"favorites"`
	}
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &got) != nil {
		t.Fatalf("favorites: %d %s", rec.Code, rec.Body)
	}
	out := []string{}
	for _, f := range got.Favorites {
		s := f.Kind + "/" + f.Name
		if f.Archived {
			s += " (archived)"
		}
		out = append(out, s)
	}
	return out
}

func TestFavoritesStarEachKind(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	st := srv.store
	cat, _ := st.CreateCategory("Saws", nil, "", false, false)
	saw, _ := st.CreateAsset("Bandsaw", []string{cat.ID}, nil, nil)
	file, _ := st.CreateAsset("File", []string{cat.ID}, nil, nil)
	report, err := st.CreateReport("Everything", []byte(`{"sections":[{"type":"table","columns":[{"heading":"Asset","binding":"{{asset.name}}"}]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	flow, err := st.CreateFlow(store.FlowInput{Slug: "lend-a-saw", Name: "Lend a saw", Enabled: true, Definition: flows.Definition{CategoryIDs: []string{cat.ID}}}, admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	ada := userWithPermissions(t, srv, "ada@hackspace.org", rbac.PermAssetsRead, rbac.PermFlowsRun)
	bob := userWithPermissions(t, srv, "bob@hackspace.org", rbac.PermAssetsRead)

	star := func(cookie *http.Cookie, kind, id string, want int) {
		t.Helper()
		if rec := doJSON(t, srv, cookie, http.MethodPut, "/api/v1/auth/me/favorites/"+kind+"/"+id, ""); rec.Code != want {
			t.Errorf("starring %s %s: %d, want %d: %s", kind, id, rec.Code, want, rec.Body)
		}
	}
	star(ada, "asset", saw.Asset.ID, http.StatusOK)
	star(ada, "asset", saw.Asset.ID, http.StatusOK) // again: as asked
	star(ada, "category", cat.ID, http.StatusOK)
	star(ada, "report", report.ID, http.StatusOK)
	star(ada, "flow", flow.ID, http.StatusOK)
	star(ada, "asset", "missing", http.StatusNotFound)
	star(ada, "user", admin.ID, http.StatusBadRequest)
	// Bob runs no flows, so cannot see the flow to star it.
	star(bob, "flow", flow.ID, http.StatusNotFound)

	if got := strings.Join(favoriteKinds(t, srv, ada), ", "); got != "asset/Bandsaw, category/Saws, report/Everything, flow/Lend a saw" {
		t.Errorf("Ada's favorites: %s", got)
	}
	// Favorites are each person's own.
	if got := favoriteKinds(t, srv, bob); len(got) != 0 {
		t.Errorf("Bob's favorites: %v", got)
	}

	// The asset list's filter keeps the starred.
	rec := doJSON(t, srv, ada, http.MethodGet, "/api/v1/assets?favorite=true", "")
	if body := rec.Body.String(); rec.Code != http.StatusOK || !strings.Contains(body, "Bandsaw") || strings.Contains(body, `"File"`) {
		t.Errorf("Ada's starred assets: %d %s", rec.Code, body)
	}
	if rec := doJSON(t, srv, ada, http.MethodGet, "/api/v1/assets?favorite=maybe", ""); rec.Code != http.StatusBadRequest {
		t.Errorf("favorite=maybe: %d", rec.Code)
	}
	if rec := doJSON(t, srv, bob, http.MethodGet, "/api/v1/assets?favorite=true", ""); strings.Contains(rec.Body.String(), "Bandsaw") {
		t.Errorf("Bob's starred assets: %s", rec.Body)
	}
	_ = file

	// Archived, an asset is still a favorite, marked so the home page can
	// leave it out.
	if err := st.DeleteAsset(saw.Asset.ID); err != nil {
		t.Fatal(err)
	}
	if got := favoriteKinds(t, srv, ada); len(got) == 0 || got[0] != "asset/Bandsaw (archived)" {
		t.Errorf("after archiving: %v", got)
	}

	// Unstarring each kind.
	for kind, id := range map[string]string{"asset": saw.Asset.ID, "category": cat.ID, "report": report.ID, "flow": flow.ID} {
		if rec := doJSON(t, srv, ada, http.MethodDelete, "/api/v1/auth/me/favorites/"+kind+"/"+id, ""); rec.Code != http.StatusNoContent {
			t.Errorf("unstarring %s: %d %s", kind, rec.Code, rec.Body)
		}
	}
	if got := favoriteKinds(t, srv, ada); len(got) != 0 {
		t.Errorf("after unstarring: %v", got)
	}
}

// TestFavoritesHideWhatCannotBeSeen follows a keeper of the tools who stars
// a drill, loses the tools scope, and gets it back.
func TestFavoritesHideWhatCannotBeSeen(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	adminCookie := sessionCookie(t, srv, admin.ID)
	st := srv.store
	tools, err := st.CreateScope("tools")
	if err != nil {
		t.Fatal(err)
	}
	toolsCat, _ := st.CreateCategory("Tools", nil, "", false, false)
	if _, err := st.SetCategoryScopes(toolsCat.ID, []string{tools.ID}); err != nil {
		t.Fatal(err)
	}
	drill, _ := st.CreateAsset("Drill", []string{toolsCat.ID}, nil, nil)
	keepers, err := st.CreateRole(store.RoleInput{Name: "Tool keepers", Permissions: []string{"tools:" + rbac.PermAssetsRead}})
	if err != nil {
		t.Fatal(err)
	}
	others, err := st.CreateRole(store.RoleInput{Name: "Others", Permissions: []string{"default:" + rbac.PermAssetsRead}})
	if err != nil {
		t.Fatal(err)
	}
	user, _ := st.CreateNamedUser("keeper@hackspace.org", "Keeper", "UserSecret123!", true)
	assign := func(role string) {
		t.Helper()
		if rec := doJSON(t, srv, adminCookie, http.MethodPut, "/api/v1/users/"+user.ID+"/roles", `{"roles":[{"role_id":"`+role+`"}]}`); rec.Code != http.StatusOK {
			t.Fatalf("assigning: %d %s", rec.Code, rec.Body)
		}
	}
	assign(keepers.ID)
	keeper := sessionCookie(t, srv, user.ID)
	if rec := doJSON(t, srv, keeper, http.MethodPut, "/api/v1/auth/me/favorites/asset/"+drill.Asset.ID, ""); rec.Code != http.StatusOK {
		t.Fatalf("starring the drill: %d %s", rec.Code, rec.Body)
	}

	assign(others.ID)
	if got := favoriteKinds(t, srv, keeper); len(got) != 0 {
		t.Errorf("without the tools scope: %v", got)
	}
	if rec := doJSON(t, srv, keeper, http.MethodGet, "/api/v1/assets?favorite=true", ""); strings.Contains(rec.Body.String(), "Drill") {
		t.Errorf("the filter without the tools scope: %s", rec.Body)
	}
	// Nor can it be starred again unseen; but it is kept.
	if rec := doJSON(t, srv, keeper, http.MethodPut, "/api/v1/auth/me/favorites/asset/"+drill.Asset.ID, ""); rec.Code != http.StatusNotFound {
		t.Errorf("starring unseen: %d", rec.Code)
	}

	assign(keepers.ID)
	if got := favoriteKinds(t, srv, keeper); len(got) != 1 || got[0] != "asset/Drill" {
		t.Errorf("with the tools scope again: %v", got)
	}
}

// TestFavoritesNeedAPerson: an API token holding roles is no one, so has
// no favorites; a personal token has its person's.
func TestFavoritesNeedAPerson(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	adminCookie := sessionCookie(t, srv, admin.ID)
	cat, _ := srv.store.CreateCategory("Saws", nil, "", false, false)
	saw, _ := srv.store.CreateAsset("Bandsaw", []string{cat.ID}, nil, nil)

	secret := tokenWith(t, srv, admin.ID, "Reader", rbac.PermAssetsRead)
	if rec := doBearer(t, srv, secret, http.MethodGet, "/api/v1/auth/me/favorites"); rec.Code != http.StatusForbidden {
		t.Errorf("a token's favorites: %d", rec.Code)
	}
	if rec := doBearer(t, srv, secret, http.MethodGet, "/api/v1/assets?favorite=true"); rec.Code != http.StatusBadRequest {
		t.Errorf("a token's starred assets: %d", rec.Code)
	}

	if rec := doJSON(t, srv, adminCookie, http.MethodPut, "/api/v1/auth/me/favorites/asset/"+saw.Asset.ID, ""); rec.Code != http.StatusOK {
		t.Fatalf("starring: %d %s", rec.Code, rec.Body)
	}
	_, personal := personalToken(t, srv, adminCookie, "Assistant")
	if rec := doBearer(t, srv, personal, http.MethodGet, "/api/v1/auth/me/favorites"); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Bandsaw") {
		t.Errorf("a personal token's favorites: %d %s", rec.Code, rec.Body)
	}
}

// TestFavoritesOverMCP: the tools star, list and unstar as the API does,
// for the person a personal token acts for.
func TestFavoritesOverMCP(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	adminCookie := sessionCookie(t, srv, admin.ID)
	cat, _ := srv.store.CreateCategory("Saws", nil, "", false, false)
	saw, _ := srv.store.CreateAsset("Bandsaw", []string{cat.ID}, nil, nil)
	_, secret := personalToken(t, srv, adminCookie, "Claude")
	cs := mcpSession(t, srv, secret)

	var added struct{ Kind, ID, Name string }
	structured(t, callTool(t, cs, "add_favorite", map[string]any{"kind": "asset", "id": saw.Asset.ID}), &added)
	if added.Kind != "asset" || added.ID != saw.Asset.ID || added.Name != "Bandsaw" {
		t.Errorf("add_favorite: %+v", added)
	}
	structured(t, callTool(t, cs, "add_favorite", map[string]any{"kind": "category", "id": cat.ID}), &added)
	if got := strings.Join(favoriteKinds(t, srv, adminCookie), ", "); got != "asset/Bandsaw, category/Saws" {
		t.Errorf("the API after add_favorite: %s", got)
	}
	var listed struct {
		Favorites []struct{ Kind, Name string }
	}
	structured(t, callTool(t, cs, "list_favorites", nil), &listed)
	if len(listed.Favorites) != 2 || listed.Favorites[0].Name != "Bandsaw" || listed.Favorites[1].Kind != "category" {
		t.Errorf("list_favorites: %+v", listed)
	}
	var searched struct {
		Total int
	}
	structured(t, callTool(t, cs, "search_assets", map[string]any{"favorite": true}), &searched)
	if searched.Total != 1 {
		t.Errorf("search_assets, favorite: %+v", searched)
	}
	callTool(t, cs, "remove_favorite", map[string]any{"kind": "asset", "id": saw.Asset.ID})
	if got := strings.Join(favoriteKinds(t, srv, adminCookie), ", "); got != "category/Saws" {
		t.Errorf("the API after remove_favorite: %s", got)
	}

	// A token holding roles is no one, so has no favorites.
	names := toolNames(t, mcpSession(t, srv, tokenWith(t, srv, admin.ID, "Script", rbac.PermAssetsRead)))
	for _, name := range []string{"list_favorites", "add_favorite", "remove_favorite"} {
		if slices.Contains(names, name) {
			t.Errorf("a role token lists %s", name)
		}
	}
}
