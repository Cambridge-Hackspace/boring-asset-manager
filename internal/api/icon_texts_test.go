package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// A font of a few strokes, and a tag that writes its label in it.
const (
	textFont = `font "mini" { em 10; ascent 7; descent 3; kind "stroke"
  glyph "A" advance=5 { path d="M0,0 L2,7 L4,0" }
  glyph "B" advance=5 { path d="M0,0 V7 H3 L0,3.5 L3,0 Z" }
  notdef advance=5 { path d="M0,0 H4 V7 H0 Z" }
}`
	textTag = `icon "tag" { category "labels"; footprint 1 1 0.5
  param label (text)"?"
  part "box" { box 0 0 0 1 1 0.5 }
  part "label" { at 0.5 0.5 0.5 { text 0 0 "{label}" font="mini" size=8 } }
}`
	// the A glyph's stroke, as SVG draws it
	glyphA = `d="M0,0 L2,7 L4,0"`
)

// An icon's text param bound to an asset's name draws each asset with its
// own: on the asset and in lists, as an image, on labels and in scenes.
func TestAssetTextsThroughTheAPI(t *testing.T) {
	requireIcons(t)
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	st, ctx := srv.store, t.Context()
	if _, save, err := st.CreateIconLibraryFile(ctx, srv.icons, "fonts/mini.ilx", textFont); err != nil || !save.Published {
		t.Fatalf("%+v %v", save, err)
	}
	tags, _ := st.CreateCategoryFrom(store.CategoryInput{Name: "Tags"})
	if save, err := st.SaveCategoryIcon(ctx, srv.icons, tags.ID, "tag", textTag); err != nil || !save.Published {
		t.Fatalf("%+v %v", save, err)
	}
	count, _ := st.CreateField("count", "Count", "number", nil)

	path := "/api/v1/categories/" + tags.ID + "/icon/params"
	if rec := doJSON(t, srv, cookie, "PUT", path, `{"params":{"label":"`+count.ID+`"}}`); rec.Code != http.StatusBadRequest ||
		!strings.Contains(rec.Body.String(), "is text; bind it to the asset's name, a code or a field that is not a number") {
		t.Errorf("a number field: %d %s", rec.Code, rec.Body)
	}
	if rec := doJSON(t, srv, cookie, "PUT", path, `{"params":{"label":"name"}}`); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"source":"name"`) {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}

	// made, read, and listed: with its name, and drawn with it
	rec := doJSON(t, srv, cookie, "POST", "/api/v1/assets", `{"name":"AB","category_ids":["`+tags.ID+`"]}`)
	var a assetAnswer
	decodeBody(t, rec.Body.Bytes(), &a)
	ic := a.Asset.ResolvedIcon
	if rec.Code != http.StatusCreated || ic == nil || ic.Texts["label"] != "AB" || !bytes.Contains(ic.Drawn["tag"], []byte(`"M0,0 L2,7 L4,0"`)) {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	ab := a.Asset.Asset.ID
	// an assistant is told them
	cs := mcpSession(t, srv, tokenWith(t, srv, admin.ID, "assistant", rbac.PermAssetsRead))
	var view struct {
		Icon struct {
			Texts map[string]string `json:"texts"`
		} `json:"icon"`
	}
	structured(t, callTool(t, cs, "get_asset", map[string]any{"asset": ab}), &view)
	if view.Icon.Texts["label"] != "AB" {
		t.Errorf("get_asset: %+v", view.Icon)
	}
	b, _ := st.CreateAsset("B", []string{tags.ID}, nil, nil)
	var list struct {
		Assets []struct {
			Asset        struct{ Name string } `json:"asset"`
			ResolvedIcon struct {
				Drawn map[string]json.RawMessage `json:"drawn"`
			} `json:"resolved_icon"`
		} `json:"assets"`
	}
	decodeBody(t, doJSON(t, srv, cookie, "GET", "/api/v1/assets?category_id="+tags.ID, "").Body.Bytes(), &list)
	if len(list.Assets) != 2 {
		t.Fatalf("%+v", list)
	}
	for _, it := range list.Assets {
		if has := bytes.Contains(it.ResolvedIcon.Drawn["tag"], []byte("L2,7")); has != (it.Asset.Name == "AB") {
			t.Errorf("%s in a list: %s", it.Asset.Name, it.ResolvedIcon.Drawn["tag"])
		}
	}

	// as an image
	image := func(id string) string {
		t.Helper()
		rec := doJSON(t, srv, cookie, "GET", "/api/v1/icons/image?asset="+id, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("%d %s", rec.Code, rec.Body)
		}
		return rec.Body.String()
	}
	if !strings.Contains(image(ab), glyphA) || strings.Contains(image(b.Asset.ID), glyphA) {
		t.Errorf("images:\n%s\n%s", image(ab), image(b.Asset.ID))
	}

	// on a label: its data carries the icon drawn, and it prints
	if rec := doJSON(t, srv, cookie, "POST", "/api/v1/templates/preview-data", `{"asset_id":"`+ab+`"}`); !strings.Contains(rec.Body.String(), `"drawn_icons":{"tag":`) {
		t.Errorf("preview data: %s", rec.Body)
	}
	el := map[string]any{"type": "icon", "x": 0, "y": 0, "width": 20, "height": 20, "icon": "{{asset.icon}}"}
	body, _ := json.Marshal(map[string]any{"name": "Tag", "units": "mm", "width": 20, "height": 20, "dpi": 203, "layering": "list", "elements": []any{el}})
	var created struct {
		Template struct {
			ID string `json:"id"`
		} `json:"template"`
	}
	decodeBody(t, doJSON(t, srv, cookie, "POST", "/api/v1/templates", string(body)).Body.Bytes(), &created)
	label := func(id string) string {
		t.Helper()
		rec := doJSON(t, srv, cookie, "GET", "/api/v1/assets/"+id+"/label?format=png&template_id="+created.Template.ID, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("%d %s", rec.Code, rec.Body)
		}
		return rec.Body.String()
	}
	if label(ab) == label(b.Asset.ID) {
		t.Error("AB and B print the same label")
	}

	// in a scene
	room, _ := st.CreateAsset("Room", nil, nil, nil)
	if _, err := st.AddLink(room.Asset.ID, containsType, ab, true); err != nil {
		t.Fatal(err)
	}
	if scene := doJSON(t, srv, cookie, "GET", "/api/v1/assets/"+room.Asset.ID+"/scene/image", "").Body.String(); !strings.Contains(scene, glyphA) {
		t.Errorf("scene: %s", scene)
	}
}
