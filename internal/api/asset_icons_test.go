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

// iconedWorld is Storage, drawn as a crate, and its subcategory Bins,
// which has no icon of its own.
func iconedWorld(t *testing.T, srv *Server) (storage, bins string) {
	t.Helper()
	st, err := srv.store.CreateCategoryFrom(store.CategoryInput{Name: "Storage", ColorHex: "#336699"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := srv.store.CreateCategoryFrom(store.CategoryInput{Name: "Bins", ParentID: &st.ID, ColorHex: "#aa0000"})
	if err != nil {
		t.Fatal(err)
	}
	if save, err := srv.store.SaveCategoryIcon(t.Context(), srv.icons, st.ID, "crate", crateSource); err != nil || !save.Published {
		t.Fatalf("%+v %v", save, err)
	}
	return st.ID, b.ID
}

type assetAnswer struct {
	Asset models.AssetWithDetails `json:"asset"`
}

func TestAssetIconsThroughTheAPI(t *testing.T) {
	requireIcons(t)
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	storage, bins := iconedWorld(t, srv)

	if rec := doJSON(t, srv, cookie, "POST", "/api/v1/assets", `{"name":"Bin","category_ids":["`+bins+`"],"icon_name":"nope"}`); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), `no icon is drawn by the name \"nope\"`) {
		t.Errorf("an icon the build does not draw: %d %s", rec.Code, rec.Body)
	}
	rec := doJSON(t, srv, cookie, "POST", "/api/v1/assets", `{"name":"Bin","category_ids":["`+bins+`"]}`)
	var got assetAnswer
	decodeBody(t, rec.Body.Bytes(), &got)
	want := models.ResolvedIcon{Name: "crate", From: models.IconFromCategory, SourceCategoryID: storage, SourceCategoryName: "Storage", Color: "#aa0000"}
	if rec.Code != http.StatusCreated || got.Asset.ResolvedIcon == nil || *got.Asset.ResolvedIcon != want || got.Asset.Asset.IconName != nil {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	path := "/api/v1/assets/" + got.Asset.Asset.ID

	put := func(body string, code int) *models.AssetWithDetails {
		t.Helper()
		rec := doJSON(t, srv, cookie, "PUT", path, body)
		if rec.Code != code {
			t.Fatalf("%s: %d %s", body, rec.Code, rec.Body)
		}
		var a assetAnswer
		if code == http.StatusOK {
			decodeBody(t, rec.Body.Bytes(), &a)
		}
		return &a.Asset
	}
	if a := put(`{"icon_name":"crate"}`, http.StatusOK); a.ResolvedIcon.From != models.IconFromAsset || *a.Asset.IconName != "crate" {
		t.Errorf("set: %+v", a.ResolvedIcon)
	}
	if a := put(`{"name":"Big bin"}`, http.StatusOK); a.Asset.IconName == nil {
		t.Error("an edit without icon_name dropped the icon")
	}
	put(`{"icon_name":5}`, http.StatusBadRequest)
	put(`{"icon_name":"nope"}`, http.StatusBadRequest)

	// a copy has the original's own icon
	rec = doJSON(t, srv, cookie, "GET", path+"/copy", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"icon_name":"crate"`) {
		t.Errorf("copy: %d %s", rec.Code, rec.Body)
	}
	// and a scan shows it
	rec = doJSON(t, srv, cookie, "GET", "/api/v1/scan/"+got.Asset.Asset.ID, "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"icon":{"name":"crate","from":"asset","color":"#aa0000"}`) {
		t.Errorf("scan: %d %s", rec.Code, rec.Body)
	}

	if a := put(`{"icon_name":null}`, http.StatusOK); a.Asset.IconName != nil || a.ResolvedIcon.From != models.IconFromCategory {
		t.Errorf("null: %v %+v", a.Asset.IconName, a.ResolvedIcon)
	}
	put(`{"icon_name":"crate"}`, http.StatusOK)
	if a := put(`{"icon_name":""}`, http.StatusOK); a.Asset.IconName != nil {
		t.Errorf(`"": %v`, *a.Asset.IconName)
	}
	rec = doJSON(t, srv, cookie, "GET", path+"/copy", "")
	if strings.Contains(rec.Body.String(), `"icon_name"`) {
		t.Errorf("copy of one drawn with its categories': %s", rec.Body)
	}
}

// The overview says which icon each category is drawn with, inherited
// too, and carries the unknown icon for pages to draw.
func TestTheOverviewNamesEachCategorysIcon(t *testing.T) {
	requireIcons(t)
	srv, admin, _ := setupTestServer(t)
	storage, bins := iconedWorld(t, srv)
	rec := doJSON(t, srv, sessionCookie(t, srv, admin.ID), "GET", "/api/v1/icons", "")
	var got struct {
		CategoryIcons map[string]string `json:"category_icons"`
		Unknown       struct {
			Name   string            `json:"name"`
			Shapes []json.RawMessage `json:"shapes"`
		} `json:"unknown"`
	}
	decodeBody(t, rec.Body.Bytes(), &got)
	if len(got.CategoryIcons) != 2 || got.CategoryIcons[storage] != "crate" || got.CategoryIcons[bins] != "crate" {
		t.Errorf("%+v", got.CategoryIcons)
	}
	if got.Unknown.Name != "bam-unknown" || len(got.Unknown.Shapes) == 0 {
		t.Errorf("%+v", got.Unknown)
	}
}

// An assistant gives an asset an icon of its own, sees where an icon comes
// from, and gives it back to its categories.
func TestAnAssistantSetsAnAssetsIcon(t *testing.T) {
	requireIcons(t)
	srv, admin, _ := setupTestServer(t)
	storage, bins := iconedWorld(t, srv)
	a, err := srv.store.CreateAsset("Bin", []string{bins}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	cs := mcpSession(t, srv, tokenWith(t, srv, admin.ID, "assistant", rbac.PermAssetsRead, rbac.PermAssetsWrite))

	var d struct {
		Icon *struct {
			Name     string `json:"name"`
			Own      bool   `json:"own"`
			Category *struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"category"`
		} `json:"icon"`
	}
	structured(t, callTool(t, cs, "get_asset", map[string]any{"asset": a.Asset.ID}), &d)
	// Storage is not one of its categories but Bins' parent
	if d.Icon == nil || d.Icon.Name != "crate" || d.Icon.Own || d.Icon.Category == nil || d.Icon.Category.ID != storage || d.Icon.Category.Name != "Storage" {
		t.Fatalf("%+v", d.Icon)
	}

	if res := callTool(t, cs, "update_asset", map[string]any{"asset": a.Asset.ID, "icon": "nope"}); !res.IsError {
		t.Errorf("an icon the build does not draw: %s", toolText(res))
	}
	callTool(t, cs, "update_asset", map[string]any{"asset": a.Asset.ID, "icon": "crate"})
	structured(t, callTool(t, cs, "get_asset", map[string]any{"asset": a.Asset.ID}), &d)
	if d.Icon == nil || !d.Icon.Own || d.Icon.Category != nil {
		t.Errorf("own: %+v", d.Icon)
	}
	callTool(t, cs, "update_asset", map[string]any{"asset": a.Asset.ID, "category_icon": true})
	structured(t, callTool(t, cs, "get_asset", map[string]any{"asset": a.Asset.ID}), &d)
	if d.Icon == nil || d.Icon.Own {
		t.Errorf("dropped: %+v", d.Icon)
	}
}
