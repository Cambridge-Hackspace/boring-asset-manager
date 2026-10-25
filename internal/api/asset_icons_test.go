package api

import (
	"bytes"
	"encoding/json"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

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

// Icons drawn as images: by name, a category's (inherited too) or an
// asset's, as SVG or PNG, light or dark, at a height, in the category's
// color, and the unknown icon for a name the build does not draw.
func TestIconImages(t *testing.T) {
	requireIcons(t)
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	storage, bins := iconedWorld(t, srv)
	// the crate paints a tile in its category's color
	painted := strings.Replace(crateSource, "box 0 0 0 1 1 1", `box 0 0 0 1 1 1; tile 0 0 1 1 1 fill="category"`, 1)
	if save, err := srv.store.SaveCategoryIcon(t.Context(), srv.icons, storage, "crate", painted); err != nil || !save.Published {
		t.Fatalf("%+v %v", save, err)
	}
	get := func(query string) *httptest.ResponseRecorder {
		t.Helper()
		return doJSON(t, srv, cookie, "GET", "/api/v1/icons/image?"+query, "")
	}

	rec := get("name=crate&height=128")
	body := rec.Body.String()
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/svg+xml" || !strings.Contains(body, `height="128.00"`) ||
		!strings.Contains(body, `fill="#336699"`) || !strings.Contains(body, `fill="#f4f4f5"`) {
		t.Fatalf("svg: %d %s %.600s", rec.Code, rec.Header(), body)
	}
	if dark := get("name=crate&height=128&theme=dark").Body.String(); dark == body || !strings.Contains(dark, `fill="#71717a"`) {
		t.Errorf("dark: %.300s", dark)
	}
	// Bins draws Storage's crate, in Bins' own color
	if b := get("category=" + bins).Body.String(); !strings.Contains(b, `fill="#aa0000"`) {
		t.Errorf("inherited: %.600s", b)
	}
	if b := get("name=crate&color=%23123456").Body.String(); !strings.Contains(b, `fill="#123456"`) {
		t.Errorf("color: %.600s", b)
	}

	rec = get("category=" + bins + "&format=png&height=40")
	img, err := png.Decode(rec.Body)
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/png" || err != nil || img.Bounds().Dy() != 40 {
		t.Fatalf("png: %d %v", rec.Code, err)
	}
	etag := rec.Header().Get("ETag")
	req := httptest.NewRequest("GET", "/api/v1/icons/image?category="+bins+"&format=png&height=40", nil)
	req.Header.Set("If-None-Match", etag)
	req.AddCookie(cookie)
	again := httptest.NewRecorder()
	srv.Handler().ServeHTTP(again, req)
	if etag == "" || again.Code != http.StatusNotModified {
		t.Errorf("unchanged: %q %d", etag, again.Code)
	}

	// an asset's, and the unknown icon for a name nothing draws
	a, err := srv.store.CreateAsset("Bin", []string{bins}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if rec := get("asset=" + a.Asset.ID); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "<title>crate</title>") {
		t.Errorf("asset: %d %.200s", rec.Code, rec.Body)
	}
	rec = get("name=gone")
	if rec.Code != http.StatusOK || rec.Header().Get("X-Icon-Missing") != "gone" || !strings.Contains(rec.Body.String(), "<title>bam-unknown</title>") {
		t.Errorf("unknown: %d %s", rec.Code, rec.Header())
	}

	plain, _ := srv.store.CreateAsset("Chair", nil, nil, nil)
	other, _ := srv.store.CreateCategoryFrom(store.CategoryInput{Name: "Chairs"})
	for query, code := range map[string]int{
		"":                               http.StatusBadRequest,
		"name=crate&category=" + storage: http.StatusBadRequest,
		"name=crate&format=gif":          http.StatusBadRequest,
		"name=crate&theme=sepia":         http.StatusBadRequest,
		"name=crate&height=0":            http.StatusBadRequest,
		"name=crate&height=2000":         http.StatusBadRequest,
		"name=crate&color=red":           http.StatusBadRequest,
		"asset=" + plain.Asset.ID:        http.StatusNotFound,
		"asset=nope":                     http.StatusNotFound,
		"category=" + other.ID:           http.StatusNotFound,
		"category=nope":                  http.StatusNotFound,
	} {
		if rec := get(query); rec.Code != code {
			t.Errorf("%q: %d, want %d: %s", query, rec.Code, code, rec.Body)
		}
	}
}

// Names assets have as their own that the build stops drawing are
// reported, and the assets found by them.
func TestMissingIconsAreReported(t *testing.T) {
	requireIcons(t)
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	_, bins := iconedWorld(t, srv)
	barrels, _ := srv.store.CreateCategoryFrom(store.CategoryInput{Name: "Barrels"})
	if save, err := srv.store.SaveCategoryIcon(t.Context(), srv.icons, barrels.ID, "barrel", strings.ReplaceAll(crateSource, "crate", "barrel")); err != nil || !save.Published {
		t.Fatalf("%+v %v", save, err)
	}
	var ids []string
	for _, name := range []string{"One", "Two"} {
		a, err := srv.store.CreateAssetWithIcon(name, []string{bins}, nil, nil, nil, "barrel")
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, a.Asset.ID)
	}
	overview := func() []store.MissingIcon {
		var got struct {
			MissingIcons []store.MissingIcon `json:"missing_icons"`
		}
		decodeBody(t, doJSON(t, srv, cookie, "GET", "/api/v1/icons", "").Body.Bytes(), &got)
		return got.MissingIcons
	}
	if m := overview(); len(m) != 0 {
		t.Errorf("before: %+v", m)
	}
	if _, err := srv.store.DeleteCategoryIcon(t.Context(), srv.icons, barrels.ID); err != nil {
		t.Fatal(err)
	}
	if m := overview(); len(m) != 1 || m[0] != (store.MissingIcon{Name: "barrel", Assets: 2}) {
		t.Errorf("after: %+v", m)
	}
	rec := doJSON(t, srv, cookie, "GET", "/api/v1/assets?icon=barrel", "")
	var list struct {
		Assets []models.AssetWithDetails `json:"assets"`
	}
	decodeBody(t, rec.Body.Bytes(), &list)
	if len(list.Assets) != 2 || !list.Assets[0].ResolvedIcon.Missing {
		t.Errorf("%d %+v", rec.Code, list.Assets)
	}

	// an assistant sees the report, finds them, and draws what they show
	cs := mcpSession(t, srv, tokenWith(t, srv, admin.ID, "assistant", rbac.PermAssetsRead))
	if text := toolText(callTool(t, cs, "icons_overview", map[string]any{})); !strings.Contains(text, "1 icon names that assets have as their own are not drawn") {
		t.Errorf("overview: %s", text)
	}
	var found struct {
		Total int `json:"total"`
	}
	structured(t, callTool(t, cs, "search_assets", map[string]any{"icon": "barrel"}), &found)
	if found.Total != 2 {
		t.Errorf("search: %d", found.Total)
	}
	res := callTool(t, cs, "draw_icon", map[string]any{"asset": ids[0], "height": 32})
	var image *mcp.ImageContent
	for _, c := range res.Content {
		if ic, ok := c.(*mcp.ImageContent); ok {
			image = ic
		}
	}
	if res.IsError || image == nil || image.MIMEType != "image/png" || !strings.Contains(toolText(res), `"barrel"`) {
		t.Fatalf("draw: %s", toolText(res))
	}
	if img, err := png.Decode(bytes.NewReader(image.Data)); err != nil || img.Bounds().Dy() != 32 {
		t.Errorf("%v", err)
	}
	if res := callTool(t, cs, "draw_icon", map[string]any{}); !res.IsError {
		t.Error("drew nothing named")
	}
}
