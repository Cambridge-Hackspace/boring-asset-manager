package api

import (
	"net/http"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// A location's scene: what is in it, each taking the room its icon does,
// or drawn as a box with none; laid out on its floor; and only what the
// caller can read.
func TestAssetScenes(t *testing.T) {
	requireIcons(t)
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	st := srv.store
	ctx := t.Context()

	rooms, _ := st.CreateCategoryFrom(store.CategoryInput{Name: "Rooms"})
	if save, err := st.SaveCategoryIcon(ctx, srv.icons, rooms.ID, "room", `icon "room" { category "places"; footprint 4 3 2; part "floor" { tile 0 0 0 4 3 } }`); err != nil || !save.Published {
		t.Fatalf("%+v %v", save, err)
	}
	servers, _ := st.CreateCategoryFrom(store.CategoryInput{Name: "Servers"})
	if save, err := st.SaveCategoryIcon(ctx, srv.icons, servers.ID, "server", `icon "server" { category "compute"; footprint 2 1 0.5; part "box" { box 0 0 0 2 1 0.5 } }`); err != nil || !save.Published {
		t.Fatalf("%+v %v", save, err)
	}
	secret, _ := st.CreateScope("secret")
	vault, _ := st.CreateCategory("Vault", nil, "", false, false)
	if _, err := st.SetCategoryScopes(vault.ID, []string{secret.ID}); err != nil {
		t.Fatal(err)
	}

	room, _ := st.CreateAsset("Lab", []string{rooms.ID}, nil, nil)
	web, _ := st.CreateAsset("Web", []string{servers.ID}, nil, nil)
	chair, _ := st.CreateAsset("Chair", nil, nil, nil)
	safe, _ := st.CreateAsset("Safe", []string{vault.ID}, nil, nil)
	gone, _ := st.CreateAsset("Gone", nil, nil, nil)
	for _, a := range []string{web.Asset.ID, chair.Asset.ID, safe.Asset.ID, gone.Asset.ID} {
		if _, err := st.AddLink(room.Asset.ID, containsType, a, true); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.DeleteAsset(gone.Asset.ID); err != nil {
		t.Fatal(err)
	}

	var got SceneResponse
	rec := doJSON(t, srv, cookie, "GET", "/api/v1/assets/"+room.Asset.ID+"/scene", "")
	decodeBody(t, rec.Body.Bytes(), &got)
	if rec.Code != http.StatusOK || got.Floor != [2]float64{4, 3} || len(got.Items) != 3 || len(got.Path) != 1 || got.Path[0].ID != room.Asset.ID {
		t.Fatalf("%d %+v", rec.Code, got)
	}
	byName := map[string]SceneItem{}
	for _, it := range got.Items {
		byName[it.Name] = it
	}
	if w := byName["Web"]; w.Size != [3]float64{2, 1, 0.5} || w.Icon == nil || w.Icon.Name != "server" || w.Shapes != nil {
		t.Errorf("web: %+v", w)
	}
	if c := byName["Chair"]; c.Size != [3]float64{1, 1, 1} || len(c.Shapes) != 3 || c.Icon != nil {
		t.Errorf("chair: %+v", c)
	}
	// by name across the floor: Chair, Safe, then Web, filling the row
	if c, s, w := byName["Chair"], byName["Safe"], byName["Web"]; c.X != 0 || s.X != 1 || w.X != 2 || w.Y != 0 {
		t.Errorf("packed: %+v %+v %+v", c.Placed, s.Placed, w.Placed)
	}
	var lab assetAnswer
	decodeBody(t, doJSON(t, srv, cookie, "GET", "/api/v1/assets/"+room.Asset.ID, "").Body.Bytes(), &lab)
	if lab.Asset.Holds != 3 {
		t.Errorf("holds %d", lab.Asset.Holds)
	}

	// one who cannot see the vault does not see the safe
	role, _ := st.CreateRole(store.RoleInput{Name: "Visitors", Permissions: []string{rbac.DefaultScope + ":" + rbac.PermAssetsRead}})
	visitor, _ := st.CreateNamedUser("visitor@hackspace.org", "Visitor", "UserSecret123!", true)
	if err := st.SetUserRoles(visitor.ID, []string{role.ID}); err != nil {
		t.Fatal(err)
	}
	var seen SceneResponse
	decodeBody(t, doJSON(t, srv, sessionCookie(t, srv, visitor.ID), "GET", "/api/v1/assets/"+room.Asset.ID+"/scene", "").Body.Bytes(), &seen)
	if len(seen.Items) != 2 {
		t.Errorf("a visitor sees %d", len(seen.Items))
	}

	// an assistant reads it
	cs := mcpSession(t, srv, tokenWith(t, srv, admin.ID, "assistant", rbac.PermAssetsRead))
	var tool struct {
		Items []map[string]any `json:"items"`
	}
	structured(t, callTool(t, cs, "get_scene", map[string]any{"asset": room.Asset.ID}), &tool)
	if len(tool.Items) != 3 || tool.Items[0]["tx"] != nil || tool.Items[0]["x"] == nil {
		t.Errorf("get_scene: %+v", tool.Items)
	}
}
