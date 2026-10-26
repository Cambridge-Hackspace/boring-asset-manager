package api

import (
	"bytes"
	"context"
	"encoding/json"
	"image/png"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

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
	if rec.Code != http.StatusCreated || got.Asset.ResolvedIcon == nil || !reflect.DeepEqual(*got.Asset.ResolvedIcon, want) || got.Asset.Asset.IconName != nil {
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

// Labels draw icons: the asset's, or one by name, which must be drawn
// when the template is saved; with no icon, its box stays blank.
func TestLabelsDrawIcons(t *testing.T) {
	requireIcons(t)
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	_, bins := iconedWorld(t, srv)
	tpl := func(icon string) string {
		return `{"name":"Bin","units":"mm","width":20,"height":20,"dpi":203,"layering":"list","elements":[{"type":"icon","x":0,"y":0,"width":20,"height":20,"icon":"` + icon + `"}]}`
	}
	if rec := doJSON(t, srv, cookie, "POST", "/api/v1/templates", tpl("barrel")); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), `"path":"elements[0].icon"`) {
		t.Fatalf("an icon nothing draws: %d %s", rec.Code, rec.Body)
	}
	if rec := doJSON(t, srv, cookie, "POST", "/api/v1/templates", tpl("crate")); rec.Code != http.StatusCreated {
		t.Fatalf("by name: %d %s", rec.Code, rec.Body)
	}
	rec := doJSON(t, srv, cookie, "POST", "/api/v1/templates", tpl("{{asset.icon}}"))
	var created struct {
		Template struct {
			ID string `json:"id"`
		} `json:"template"`
	}
	decodeBody(t, rec.Body.Bytes(), &created)

	iconed, _ := srv.store.CreateAsset("Bin", []string{bins}, nil, nil)
	plain, _ := srv.store.CreateAsset("Chair", nil, nil, nil)
	inked := func(id string) int {
		t.Helper()
		rec := doJSON(t, srv, cookie, "GET", "/api/v1/assets/"+id+"/label?format=png&template_id="+created.Template.ID, "")
		img, err := png.Decode(rec.Body)
		if rec.Code != http.StatusOK || err != nil {
			t.Fatalf("%d %v", rec.Code, err)
		}
		n := 0
		b := img.Bounds()
		for y := b.Min.Y; y < b.Max.Y; y++ {
			for x := b.Min.X; x < b.Max.X; x++ {
				if r, _, _, _ := img.At(x, y).RGBA(); r < 0xf000 {
					n++
				}
			}
		}
		return n
	}
	if n := inked(iconed.Asset.ID); n < 100 {
		t.Errorf("the asset's icon: %d dots inked", n)
	}
	if n := inked(plain.Asset.ID); n != 0 {
		t.Errorf("an asset with no icon: %d dots inked", n)
	}

	// previewed with no asset, the unknown icon stands for the asset's
	rec = doJSON(t, srv, cookie, "POST", "/api/v1/templates/preview-data", `{}`)
	if !strings.Contains(rec.Body.String(), `"icon":"bam-unknown"`) {
		t.Errorf("sample: %s", rec.Body)
	}
	rec = doJSON(t, srv, cookie, "POST", "/api/v1/templates/preview-data", `{"asset_id":"`+iconed.Asset.ID+`"}`)
	if !strings.Contains(rec.Body.String(), `"icon":"crate","icon_color":"#aa0000"`) {
		t.Errorf("an asset's: %s", rec.Body)
	}
}

// An asset's icon shows its state, by the variants chosen under Icons; a
// label shows it only when asked, and a copy takes the plain icon.
func TestIconStatesThroughTheAPI(t *testing.T) {
	requireIcons(t)
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	ctx := t.Context()
	if _, save, err := srv.store.CreateIconLibraryFile(ctx, srv.icons, "state.ilx",
		`axis "state" { variant "down" { part "flag" { tile 0 0 1 0.6 0.6 fill="accent" } } }`); err != nil || !save.Published {
		t.Fatalf("%+v %v", save, err)
	}
	tools, err := srv.store.CreateCategoryFrom(store.CategoryInput{Name: "Tools", ColorHex: "#336699"})
	if err != nil {
		t.Fatal(err)
	}
	if save, err := srv.store.SaveCategoryIcon(ctx, srv.icons, tools.ID, "drill",
		`icon "drill" { category "tools"; footprint 1 1 1; axes "state"; part "box" { box 0 0 0 1 1 1 } }`); err != nil || !save.Published {
		t.Fatalf("%+v %v", save, err)
	}

	type states struct {
		States   []string          `json:"states"`
		Variants map[string]string `json:"variants"`
		Starter  string            `json:"starter"`
	}
	var got states
	decodeBody(t, doJSON(t, srv, cookie, "GET", "/api/v1/icons/states", "").Body.Bytes(), &got)
	if len(got.States) != 6 || got.States[0] != models.IconStateOutOfService || got.Variants[models.IconStateOutOfService] != "down" ||
		!strings.Contains(got.Starter, `variant "down"`) {
		t.Errorf("the defaults: %+v", got)
	}
	if rec := doJSON(t, srv, cookie, "PUT", "/api/v1/icons/states", `{"variants":{"broken":"down"}}`); rec.Code != http.StatusBadRequest {
		t.Errorf("an unknown state: %d %s", rec.Code, rec.Body)
	}

	own := "drill"
	drill, err := srv.store.CreateAssetWithIcon("Drill", []string{tools.ID}, nil, nil, nil, own)
	if err != nil {
		t.Fatal(err)
	}
	id := drill.Asset.ID
	if rec := doJSON(t, srv, cookie, "PUT", "/api/v1/assets/"+id+"/condition", `{"condition":"out_of_service","note":"smoking"}`); rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	var a assetAnswer
	decodeBody(t, doJSON(t, srv, cookie, "GET", "/api/v1/assets/"+id, "").Body.Bytes(), &a)
	if ic := a.Asset.ResolvedIcon; ic == nil || ic.Name != "drill-down" || ic.Base != "drill" || ic.State != models.IconStateOutOfService || ic.Variant != "down" {
		t.Errorf("out of service: %+v", ic)
	}
	var copied struct {
		Copy AssetCopy `json:"copy"`
	}
	decodeBody(t, doJSON(t, srv, cookie, "GET", "/api/v1/assets/"+id+"/copy", "").Body.Bytes(), &copied)
	if copied.Copy.IconName != "drill" {
		t.Errorf("a copy takes %q", copied.Copy.IconName)
	}
	if rec := doJSON(t, srv, cookie, "POST", "/api/v1/templates/preview-data", `{"asset_id":"`+id+`"}`); !strings.Contains(rec.Body.String(), `"icon":"drill"`) ||
		!strings.Contains(rec.Body.String(), `"state_icon":"drill-down"`) {
		t.Errorf("preview data: %s", rec.Body)
	}

	label := func(state bool) []byte {
		t.Helper()
		el := map[string]any{"type": "icon", "x": 0, "y": 0, "width": 20, "height": 20, "icon": "{{asset.icon}}", "state": state}
		body, _ := json.Marshal(map[string]any{"name": "Tool", "units": "mm", "width": 20, "height": 20, "dpi": 203, "layering": "list", "elements": []any{el}})
		rec := doJSON(t, srv, cookie, "POST", "/api/v1/templates", string(body))
		var created struct {
			Template struct {
				ID string `json:"id"`
			} `json:"template"`
		}
		decodeBody(t, rec.Body.Bytes(), &created)
		rec = doJSON(t, srv, cookie, "GET", "/api/v1/assets/"+id+"/label?format=png&template_id="+created.Template.ID, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("%d %s", rec.Code, rec.Body)
		}
		return rec.Body.Bytes()
	}
	withState, without := label(true), label(false)
	if bytes.Equal(withState, without) {
		t.Error("the label drew the state either way")
	}

	// with no variant for out of service, it is drawn plain everywhere
	decodeBody(t, doJSON(t, srv, cookie, "PUT", "/api/v1/icons/states", `{"variants":{"needs_maintenance":"down"}}`).Body.Bytes(), &got)
	if got.Variants[models.IconStateOutOfService] != "" || got.Variants[models.IconStateNeedsMaintenance] != "down" {
		t.Errorf("chosen: %+v", got)
	}
	var plain assetAnswer
	decodeBody(t, doJSON(t, srv, cookie, "GET", "/api/v1/assets/"+id, "").Body.Bytes(), &plain)
	if ic := plain.Asset.ResolvedIcon; ic == nil || ic.Name != "drill" || ic.State != "" {
		t.Errorf("no variant: %+v", ic)
	}
	if !bytes.Equal(label(true), without) {
		t.Error("a label of an asset in no state drawn differs")
	}

	// an assistant chooses them back, and sees the asset drawn down
	cs := mcpSession(t, srv, tokenWith(t, srv, admin.ID, "assistant", rbac.PermAssetsRead, rbac.PermIconsManage))
	if res := callTool(t, cs, "set_icon_states", map[string]any{"variants": map[string]string{"out_of_service": "down"}}); res.IsError {
		t.Fatalf("set: %s", toolText(res))
	}
	var overview struct {
		States   []string          `json:"states"`
		Variants map[string]string `json:"state_variants"`
	}
	structured(t, callTool(t, cs, "icons_overview", map[string]any{}), &overview)
	if len(overview.States) != 6 || overview.Variants[models.IconStateOutOfService] != "down" || overview.Variants[models.IconStateNeedsMaintenance] != "" {
		t.Errorf("overview: %+v", overview)
	}
	var view struct {
		Icon struct {
			Name, Base, State string
		} `json:"icon"`
	}
	structured(t, callTool(t, cs, "get_asset", map[string]any{"asset": id}), &view)
	if view.Icon.Name != "drill-down" || view.Icon.Base != "drill" || view.Icon.State != models.IconStateOutOfService {
		t.Errorf("get_asset: %+v", view.Icon)
	}
}

// An icon's params bound to a field draw each asset with its value: at once
// for a change made to the asset, and soon after for others, as RunIcons
// builds; the wrappers are drawn but not listed as icons.
func TestIconParamsThroughTheAPI(t *testing.T) {
	requireIcons(t)
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	ctx, stop := context.WithCancel(t.Context())
	defer stop()
	go srv.RunIcons(ctx)

	servers, err := srv.store.CreateCategoryFrom(store.CategoryInput{Name: "Servers"})
	if err != nil {
		t.Fatal(err)
	}
	if save, err := srv.store.SaveCategoryIcon(ctx, srv.icons, servers.ID, "server",
		`icon "server" { category "compute"; param u 1; footprint 1 1 "u * 0.5"; part "box" { box 0 0 0 1 1 "u * 0.5" } }`); err != nil || !save.Published {
		t.Fatalf("%+v %v", save, err)
	}
	field, err := srv.store.CreateField("rack_units", "Rack units", "number", nil)
	if err != nil {
		t.Fatal(err)
	}
	notes, _ := srv.store.CreateField("notes", "Notes", "text", nil)
	path := "/api/v1/categories/" + servers.ID + "/icon/params"
	if rec := doJSON(t, srv, cookie, "PUT", path, `{"params":{"u":"`+notes.ID+`"}}`); rec.Code != http.StatusBadRequest {
		t.Errorf("a text field: %d %s", rec.Code, rec.Body)
	}
	if rec := doJSON(t, srv, cookie, "PUT", path, `{"params":{"u":"`+field.ID+`"}}`); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"field_key":"rack_units"`) {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if rec := doJSON(t, srv, cookie, "GET", path, ""); !strings.Contains(rec.Body.String(), `"param":"u"`) {
		t.Errorf("read back: %s", rec.Body)
	}

	// made with a value: drawn with it at once
	rec := doJSON(t, srv, cookie, "POST", "/api/v1/assets", `{"name":"Web","category_ids":["`+servers.ID+`"],"field_values":{"rack_units":"2"}}`)
	var a assetAnswer
	decodeBody(t, rec.Body.Bytes(), &a)
	if ic := a.Asset.ResolvedIcon; rec.Code != http.StatusCreated || ic == nil || ic.Name != "bam--server--u_2" || ic.Base != "server" || ic.Params["u"] != 2 {
		t.Fatalf("%d %+v", rec.Code, a.Asset.ResolvedIcon)
	}
	id := a.Asset.Asset.ID
	var changed assetAnswer
	decodeBody(t, doJSON(t, srv, cookie, "PUT", "/api/v1/assets/"+id, `{"field_values":{"rack_units":"4"}}`).Body.Bytes(), &changed)
	if ic := changed.Asset.ResolvedIcon; ic == nil || ic.Name != "bam--server--u_4" {
		t.Errorf("changed: %+v", ic)
	}

	// changed otherwise, as a copy made by an import would be: soon
	other, err := srv.store.CreateAsset("Db", []string{servers.ID}, nil, map[string]string{"rack_units": "3"})
	if err != nil {
		t.Fatal(err)
	}
	doJSON(t, srv, cookie, "PUT", "/api/v1/assets/"+other.Asset.ID+"/condition", `{"condition":"ok"}`)
	deadline := time.Now().Add(20 * time.Second)
	for {
		var got assetAnswer
		decodeBody(t, doJSON(t, srv, cookie, "GET", "/api/v1/assets/"+other.Asset.ID, "").Body.Bytes(), &got)
		if ic := got.Asset.ResolvedIcon; ic != nil && ic.Name == "bam--server--u_3" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("not built: %+v", got.Asset.ResolvedIcon)
		}
		time.Sleep(50 * time.Millisecond)
	}

	// the wrappers are drawn, not listed
	if body := doJSON(t, srv, cookie, "GET", "/api/v1/icons", "").Body.String(); strings.Contains(body, "bam--server") || !strings.Contains(body, `"name":"server"`) {
		t.Errorf("overview: %s", body)
	}
	if body := doJSON(t, srv, cookie, "GET", "/api/v1/icons/ir", "").Body.String(); !strings.Contains(body, `"name":"bam--server--u_4"`) {
		t.Error("not in the IR")
	}
	// labels draw it, and print its plain name
	if rec := doJSON(t, srv, cookie, "POST", "/api/v1/templates/preview-data", `{"asset_id":"`+id+`"}`); !strings.Contains(rec.Body.String(), `"icon":"server"`) ||
		!strings.Contains(rec.Body.String(), `"bound_icon":"bam--server--u_4"`) {
		t.Errorf("preview data: %s", rec.Body)
	}

	// an assistant reads and binds them
	cs := mcpSession(t, srv, tokenWith(t, srv, admin.ID, "assistant", rbac.PermAssetsRead, rbac.PermCategoriesManage))
	var icon struct {
		Params []models.IconParamBinding `json:"params"`
	}
	structured(t, callTool(t, cs, "get_category_icon", map[string]any{"category_id": servers.ID}), &icon)
	if len(icon.Params) != 1 || icon.Params[0].FieldLabel != "Rack units" {
		t.Errorf("get_category_icon: %+v", icon)
	}
	if res := callTool(t, cs, "bind_icon_params", map[string]any{"category_id": servers.ID}); res.IsError {
		t.Fatalf("unbind: %s", toolText(res))
	}
	var plain assetAnswer
	decodeBody(t, doJSON(t, srv, cookie, "GET", "/api/v1/assets/"+id, "").Body.Bytes(), &plain)
	if ic := plain.Asset.ResolvedIcon; ic == nil || ic.Name != "server" {
		t.Errorf("unbound: %+v", ic)
	}
}
