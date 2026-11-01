package api

import (
	"bytes"
	"encoding/json"
	"image/png"
	"net/http"
	"strings"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
)

// A flat crate for labels: a tile, where the screen's is a box.
const crateLabelSource = `icon "crate-label" { category "storage"; footprint 1 1 1; part "lid" { tile 0 0 1 1 1 } }`

// A category's label icon (docs/icons.md), through the routes, on the
// asset, on its labels and to an assistant.
func TestLabelIconsThroughTheAPI(t *testing.T) {
	requireIcons(t)
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	storage, bins := iconedWorld(t, srv)
	bin, err := srv.store.CreateAsset("Bin", []string{bins}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	// a label template drawing the asset's icon, and one its screen icon
	tplOf := func(rendition string) string {
		rec := doJSON(t, srv, cookie, "POST", "/api/v1/templates", `{"name":"Bin`+strings.Repeat(" screen", strings.Count(rendition, "screen"))+`","units":"mm","width":20,"height":20,"dpi":203,"layering":"list","elements":[{"type":"icon","x":0,"y":0,"width":20,"height":20,"icon":"{{asset.icon}}"`+rendition+`}]}`)
		var created struct {
			Template struct {
				ID string `json:"id"`
			} `json:"template"`
		}
		decodeBody(t, rec.Body.Bytes(), &created)
		if rec.Code != http.StatusCreated {
			t.Fatalf("%d %s", rec.Code, rec.Body)
		}
		return created.Template.ID
	}
	label, screen := tplOf(""), tplOf(`,"rendition":"screen"`)
	printed := func(tpl string) []byte {
		t.Helper()
		rec := doJSON(t, srv, cookie, "GET", "/api/v1/assets/"+bin.Asset.ID+"/label?format=png&template_id="+tpl, "")
		_, err := png.Decode(bytes.NewReader(rec.Body.Bytes()))
		if rec.Code != http.StatusOK || err != nil {
			t.Fatalf("%d %v", rec.Code, err)
		}
		return rec.Body.Bytes()
	}
	// with no label icon, labels print the screen icon
	before := printed(label)
	if !bytes.Equal(before, printed(screen)) {
		t.Error("with no label icon, a label printed other than the screen icon")
	}

	// the label icon, by its own routes
	icon := "/api/v1/categories/" + storage + "/icon"
	if rec := doJSON(t, srv, cookie, "PUT", icon+"?rendition=label", `{"icon_name":"crate-label","source":`+quote(crateLabelSource)+`}`); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"rendition":"label"`) || !strings.Contains(rec.Body.String(), `"published":true`) {
		t.Fatalf("saving it: %d %s", rec.Code, rec.Body)
	}
	if rec := doJSON(t, srv, cookie, "GET", icon, ""); !strings.Contains(rec.Body.String(), `"icon_name":"crate"`) {
		t.Errorf("the screen icon: %s", rec.Body)
	}
	if rec := doJSON(t, srv, cookie, "GET", icon+"?rendition=label", ""); !strings.Contains(rec.Body.String(), `"icon_name":"crate-label"`) {
		t.Errorf("the label icon: %s", rec.Body)
	}
	if rec := doJSON(t, srv, cookie, "GET", icon+"/revisions?rendition=label", ""); rec.Code != http.StatusOK || strings.Count(rec.Body.String(), `"source"`) != 1 {
		t.Errorf("its versions: %d %s", rec.Code, rec.Body)
	}
	if rec := doJSON(t, srv, cookie, "GET", icon+"?rendition=sticker", ""); rec.Code != http.StatusBadRequest {
		t.Errorf("an unknown rendition: %d %s", rec.Code, rec.Body)
	}
	if rec := doJSON(t, srv, cookie, "PUT", "/api/v1/categories/"+bins+"/icon?rendition=label", `{"icon_name":"crate","source":`+quote(crateSource)+`}`); rec.Code != http.StatusConflict {
		t.Errorf("a label icon named as a screen icon: %d %s", rec.Code, rec.Body)
	}

	// each category is shown with it, its subcategories too
	var overview struct {
		CategoryIcons  map[string]string            `json:"category_icons"`
		RenditionIcons map[string]map[string]string `json:"rendition_icons"`
	}
	decodeBody(t, doJSON(t, srv, cookie, "GET", "/api/v1/icons", "").Body.Bytes(), &overview)
	if overview.CategoryIcons[bins] != "crate" || overview.RenditionIcons["label"][bins] != "crate-label" || overview.RenditionIcons["screen"][bins] != "crate" {
		t.Errorf("overview: %+v", overview)
	}

	// the asset has both
	var got assetAnswer
	decodeBody(t, doJSON(t, srv, cookie, "GET", "/api/v1/assets/"+bin.Asset.ID, "").Body.Bytes(), &got)
	if l := got.Asset.ResolvedIcons["label"]; l == nil || l.Name != "crate-label" || l.Rendition != "label" || got.Asset.ResolvedIcon.Name != "crate" {
		t.Errorf("resolved: %+v, %+v", got.Asset.ResolvedIcon, l)
	}

	// its labels print the label icon, unless asked for the screen's
	after := printed(label)
	if bytes.Equal(after, before) {
		t.Error("a label printed the screen icon, with a label icon")
	}
	if !bytes.Equal(printed(screen), before) {
		t.Error("a label asking for the screen icon printed another")
	}
	rec := doJSON(t, srv, cookie, "POST", "/api/v1/templates/preview-data", `{"asset_id":"`+bin.Asset.ID+`"}`)
	if !strings.Contains(rec.Body.String(), `"icon":"crate","icon_color":"#aa0000"`) || !strings.Contains(rec.Body.String(), `"label_icon":{"icon":"crate-label"`) {
		t.Errorf("preview data: %s", rec.Body)
	}

	// drawn as an image, for a rendition
	if rec := doJSON(t, srv, cookie, "GET", "/api/v1/icons/image?asset="+bin.Asset.ID+"&rendition=label", ""); rec.Code != http.StatusOK {
		t.Errorf("an asset's label icon: %d", rec.Code)
	}
	if rec := doJSON(t, srv, cookie, "GET", "/api/v1/icons/image?category="+bins+"&rendition=sticker", ""); rec.Code != http.StatusBadRequest {
		t.Errorf("an unknown rendition: %d", rec.Code)
	}

	// an asset's own label icon, by the update's icon_rendition
	rec = doJSON(t, srv, cookie, "PUT", "/api/v1/assets/"+bin.Asset.ID, `{"icon_name":"crate","icon_rendition":"label"}`)
	decodeBody(t, rec.Body.Bytes(), &got)
	if rec.Code != http.StatusOK || got.Asset.Asset.LabelIconName == nil || *got.Asset.Asset.LabelIconName != "crate" || got.Asset.Asset.IconName != nil {
		t.Fatalf("own label icon: %d %s", rec.Code, rec.Body)
	}
	if l := got.Asset.ResolvedIcons["label"]; l == nil || l.Name != "crate" || l.From != models.IconFromAsset {
		t.Errorf("own label icon resolved: %+v", l)
	}
	if rec := doJSON(t, srv, cookie, "PUT", "/api/v1/assets/"+bin.Asset.ID, `{"icon_name":"crate","icon_rendition":"sticker"}`); rec.Code != http.StatusBadRequest {
		t.Errorf("an unknown rendition: %d %s", rec.Code, rec.Body)
	}

	// an assistant, by rendition
	cs := mcpSession(t, srv, tokenWith(t, srv, admin.ID, "assistant", rbac.PermAssetsRead, rbac.PermAssetsWrite, rbac.PermCategoriesManage))
	var ic struct {
		Icon struct {
			IconName  string `json:"icon_name"`
			Rendition string `json:"rendition"`
		} `json:"icon"`
	}
	structured(t, callTool(t, cs, "get_category_icon", map[string]any{"category_id": storage, "rendition": "label"}), &ic)
	if ic.Icon.IconName != "crate-label" || ic.Icon.Rendition != "label" {
		t.Errorf("get_category_icon: %+v", ic)
	}
	if res := callTool(t, cs, "save_category_icon", map[string]any{"category_id": bins, "rendition": "label", "icon_name": "bin-label", "source": strings.ReplaceAll(crateLabelSource, "crate-label", "bin-label")}); res.IsError {
		t.Fatalf("save_category_icon: %s", toolText(res))
	}
	callTool(t, cs, "update_asset", map[string]any{"asset": bin.Asset.ID, "category_icon": true, "icon_rendition": "label"})
	var d struct {
		Icon      *struct{ Name, Rendition string } `json:"icon"`
		LabelIcon *struct{ Name, Rendition string } `json:"label_icon"`
	}
	structured(t, callTool(t, cs, "get_asset", map[string]any{"asset": bin.Asset.ID}), &d)
	if d.Icon == nil || d.Icon.Name != "crate" || d.LabelIcon == nil || d.LabelIcon.Name != "bin-label" || d.LabelIcon.Rendition != "label" {
		t.Errorf("get_asset: %+v %+v", d.Icon, d.LabelIcon)
	}
	if res := callTool(t, cs, "update_asset", map[string]any{"asset": bin.Asset.ID, "icon_rendition": "label"}); !res.IsError {
		t.Errorf("icon_rendition alone: %s", toolText(res))
	}
	if res := callTool(t, cs, "delete_category_icon", map[string]any{"category_id": bins, "rendition": "label"}); res.IsError {
		t.Fatalf("delete_category_icon: %s", toolText(res))
	}
	structured(t, callTool(t, cs, "get_asset", map[string]any{"asset": bin.Asset.ID}), &d)
	if d.LabelIcon == nil || d.LabelIcon.Name != "crate-label" {
		t.Errorf("after deleting Bins' label icon, Storage's: %+v", d.LabelIcon)
	}
}

func quote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
