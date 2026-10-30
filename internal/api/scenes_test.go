package api

import (
	"bytes"
	"image/png"
	"net/http"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

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
	if save, err := st.SaveCategoryIcon(ctx, srv.icons, rooms.ID, "", "room", `icon "room" { category "places"; footprint 4 3 2; part "floor" { tile 0 0 0 4 3 } }`); err != nil || !save.Published {
		t.Fatalf("%+v %v", save, err)
	}
	servers, _ := st.CreateCategoryFrom(store.CategoryInput{Name: "Servers"})
	if save, err := st.SaveCategoryIcon(ctx, srv.icons, servers.ID, "", "server", `icon "server" { category "compute"; footprint 2 1 0.5; part "box" { box 0 0 0 2 1 0.5 } }`); err != nil || !save.Published {
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

// Arranging a scene: placing a thing keeps it where it was put and flags
// what it overlaps, answering with the scene; it is refused off the floor,
// for a thing in no location, and to one who cannot change it; clearing
// packs it again; and an assistant places things too.
func TestArrangingScenes(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	st := srv.store
	room, _ := st.CreateAsset("Lab", nil, nil, nil)
	bench, _ := st.CreateAsset("Bench", nil, nil, nil)
	crate, _ := st.CreateAsset("Crate", nil, nil, nil)
	loose, _ := st.CreateAsset("Loose", nil, nil, nil)
	for _, a := range []string{bench.Asset.ID, crate.Asset.ID} {
		if _, err := st.AddLink(room.Asset.ID, containsType, a, true); err != nil {
			t.Fatal(err)
		}
	}
	place := func(c *http.Cookie, id, body string) (int, SceneResponse) {
		var got SceneResponse
		method := "PUT"
		if body == "" {
			method = "DELETE"
		}
		rec := doJSON(t, srv, c, method, "/api/v1/assets/"+id+"/placement", body)
		if rec.Code == http.StatusOK {
			decodeBody(t, rec.Body.Bytes(), &got)
		}
		return rec.Code, got
	}
	item := func(s SceneResponse, id string) SceneItem {
		for _, it := range s.Items {
			if it.ID == id {
				return it
			}
		}
		t.Fatalf("no %s in %+v", id, s.Items)
		return SceneItem{}
	}

	code, got := place(cookie, crate.Asset.ID, `{"x": 3, "y": 0, "z": 0}`)
	if c := item(got, crate.Asset.ID); code != http.StatusOK || got.Location.ID != room.Asset.ID || c.X != 3 || !c.Arranged || !c.Movable {
		t.Fatalf("%d %+v", code, c)
	}
	// the bench is packed first in the row; the crate is put on it
	code, got = place(cookie, crate.Asset.ID, `{"in": "`+room.Asset.ID+`", "x": 0, "y": 0, "z": 0.5}`)
	if b, c := item(got, bench.Asset.ID), item(got, crate.Asset.ID); code != http.StatusOK || b.Arranged || b.X != 1 ||
		len(c.Overlaps) != 0 {
		t.Fatalf("%d %+v %+v", code, b, c)
	}
	code, got = place(cookie, bench.Asset.ID, `{"x": 0, "y": 0, "z": 0}`)
	if b := item(got, bench.Asset.ID); code != http.StatusOK || len(b.Overlaps) != 1 || b.Overlaps[0] != crate.Asset.ID {
		t.Errorf("%d %+v", code, b)
	}
	if code, _ := place(cookie, crate.Asset.ID, `{"x": -1, "y": 0, "z": 0}`); code != http.StatusBadRequest {
		t.Errorf("off the floor: %d", code)
	}
	if code, _ := place(cookie, crate.Asset.ID, `{"x": "far"}`); code != http.StatusBadRequest {
		t.Errorf("not a number: %d", code)
	}
	if code, _ := place(cookie, loose.Asset.ID, `{"x": 1, "y": 0, "z": 0}`); code != http.StatusConflict {
		t.Errorf("nowhere: %d", code)
	}
	var history struct {
		Events []struct {
			Payload struct {
				Changes []struct{ What, From, To string } `json:"changes"`
			} `json:"payload"`
		} `json:"events"`
	}
	decodeBody(t, doJSON(t, srv, cookie, "GET", "/api/v1/assets/"+crate.Asset.ID+"/history", "").Body.Bytes(), &history)
	if c := history.Events[0].Payload.Changes; len(c) != 1 || c[0].What != "place" || c[0].From != "3, 0, 0" || c[0].To != "0, 0, 0.5" {
		t.Errorf("history %+v", history.Events[0])
	}

	// one who can only look sees it, but may not move it
	role, _ := st.CreateRole(store.RoleInput{Name: "Visitors", Permissions: []string{rbac.DefaultScope + ":" + rbac.PermAssetsRead}})
	visitor, _ := st.CreateNamedUser("visitor@hackspace.org", "Visitor", "UserSecret123!", true)
	if err := st.SetUserRoles(visitor.ID, []string{role.ID}); err != nil {
		t.Fatal(err)
	}
	vc := sessionCookie(t, srv, visitor.ID)
	var seen SceneResponse
	decodeBody(t, doJSON(t, srv, vc, "GET", "/api/v1/assets/"+room.Asset.ID+"/scene", "").Body.Bytes(), &seen)
	if c := item(seen, crate.Asset.ID); c.Movable || c.Z != 0.5 {
		t.Errorf("a visitor sees %+v", c)
	}
	if code, _ := place(vc, crate.Asset.ID, `{"x": 1, "y": 1, "z": 0}`); code != http.StatusForbidden {
		t.Errorf("a visitor moved it: %d", code)
	}

	// cleared, it is packed again
	code, got = place(cookie, bench.Asset.ID, "")
	if b := item(got, bench.Asset.ID); code != http.StatusOK || b.Arranged {
		t.Errorf("%d %+v", code, b)
	}

	// an assistant places it, and clears it
	cs := mcpSession(t, srv, tokenWith(t, srv, admin.ID, "assistant", rbac.PermAssetsRead, rbac.PermAssetsWrite))
	var tool struct {
		Items []map[string]any `json:"items"`
	}
	structured(t, callTool(t, cs, "place_asset", map[string]any{"asset": bench.Asset.ID, "x": 2, "y": 1}), &tool)
	if len(tool.Items) != 2 || tool.Items[0]["tx"] != nil {
		t.Errorf("place_asset: %+v", tool.Items)
	}
	if at, _ := st.Placements(room.Asset.ID); at[bench.Asset.ID] != [3]float64{2, 1, 0} {
		t.Errorf("placed %v", at)
	}
	callTool(t, cs, "place_asset", map[string]any{"asset": bench.Asset.ID, "clear": true})
	if at, _ := st.Placements(room.Asset.ID); len(at) != 1 {
		t.Errorf("cleared %v", at)
	}
}

// A scene drawn as an image: as SVG or PNG, as high as asked, each thing
// in it moved to where it stands, one shown outlined in the accent color,
// what the caller cannot read left out; and drawn for an assistant.
func TestSceneImages(t *testing.T) {
	requireIcons(t)
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	st := srv.store
	ctx := t.Context()
	rooms, _ := st.CreateCategoryFrom(store.CategoryInput{Name: "Rooms"})
	if save, err := st.SaveCategoryIcon(ctx, srv.icons, rooms.ID, "", "room", `icon "room" { category "places"; footprint 3 2 2; part "floor" { tile 0 0 0 3 2 } }`); err != nil || !save.Published {
		t.Fatalf("%+v %v", save, err)
	}
	servers, _ := st.CreateCategoryFrom(store.CategoryInput{Name: "Servers", ColorHex: "#2266aa"})
	if save, err := st.SaveCategoryIcon(ctx, srv.icons, servers.ID, "", "server", `icon "server" { category "compute"; footprint 2 1 0.5; part "box" { box 0 0 0 2 1 0.5; tile 0 0 0.5 2 1 fill="category" } }`); err != nil || !save.Published {
		t.Fatalf("%+v %v", save, err)
	}
	secret, _ := st.CreateScope("secret")
	vault, _ := st.CreateCategory("Vault", nil, "", false, false)
	if _, err := st.SetCategoryScopes(vault.ID, []string{secret.ID}); err != nil {
		t.Fatal(err)
	}
	room, _ := st.CreateAsset("Lab", []string{rooms.ID}, nil, nil)
	web, _ := st.CreateAsset("Web", []string{servers.ID}, nil, nil)
	safe, _ := st.CreateAsset("Safe", []string{vault.ID}, nil, nil)
	for _, a := range []string{web.Asset.ID, safe.Asset.ID} {
		if _, err := st.AddLink(room.Asset.ID, containsType, a, true); err != nil {
			t.Fatal(err)
		}
	}
	palette, _ := st.GetIconPalette()
	accent := ""
	for _, c := range palette {
		if c.Token == "accent" {
			accent = c.Light
		}
	}
	path := "/api/v1/assets/" + room.Asset.ID + "/scene/image"

	rec := doJSON(t, srv, cookie, "GET", path, "")
	body := rec.Body.String()
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/svg+xml" || !strings.Contains(body, `height="512.00"`) ||
		!strings.Contains(body, "<title>Lab</title>") || strings.Count(body, "<g transform=") != 2 || strings.Contains(body, `stroke="`+accent+`"`) {
		t.Fatalf("%d %s", rec.Code, body)
	}
	// the server's top paints in its category's color
	if !strings.Contains(body, `fill="#2266aa"`) {
		t.Errorf("no category color: %s", body)
	}
	if body := doJSON(t, srv, cookie, "GET", path+"?show="+web.Asset.ID, "").Body.String(); !strings.Contains(body, `stroke="`+accent+`" stroke-width="3.00"`) {
		t.Errorf("not outlined: %s", body)
	}
	rec = doJSON(t, srv, cookie, "GET", path+"?format=png&height=100&theme=dark", "")
	if img, err := png.Decode(bytes.NewReader(rec.Body.Bytes())); rec.Code != http.StatusOK || err != nil || img.Bounds().Dy() != 100 {
		t.Errorf("png: %d %v", rec.Code, err)
	}
	for _, bad := range []string{"?format=gif", "?height=3000", "?theme=sepia"} {
		if rec := doJSON(t, srv, cookie, "GET", path+bad, ""); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: %d", bad, rec.Code)
		}
	}

	// one who cannot see the vault does not see the safe drawn
	role, _ := st.CreateRole(store.RoleInput{Name: "Visitors", Permissions: []string{rbac.DefaultScope + ":" + rbac.PermAssetsRead}})
	visitor, _ := st.CreateNamedUser("visitor@hackspace.org", "Visitor", "UserSecret123!", true)
	if err := st.SetUserRoles(visitor.ID, []string{role.ID}); err != nil {
		t.Fatal(err)
	}
	if body := doJSON(t, srv, sessionCookie(t, srv, visitor.ID), "GET", path, "").Body.String(); strings.Count(body, "<g transform=") != 1 {
		t.Errorf("a visitor sees %s", body)
	}

	// an assistant draws it, showing the server
	cs := mcpSession(t, srv, tokenWith(t, srv, admin.ID, "assistant", rbac.PermAssetsRead))
	res := callTool(t, cs, "render_scene", map[string]any{"asset": room.Asset.ID, "show": web.Asset.ID, "height": 64})
	var image *mcp.ImageContent
	for _, c := range res.Content {
		if ic, ok := c.(*mcp.ImageContent); ok {
			image = ic
		}
	}
	if res.IsError || image == nil || image.MIMEType != "image/png" || !strings.Contains(toolText(res), "outlined") {
		t.Fatalf("render_scene: %s", toolText(res))
	}
	if img, err := png.Decode(bytes.NewReader(image.Data)); err != nil || img.Bounds().Dy() != 64 {
		t.Errorf("%v", err)
	}
}
