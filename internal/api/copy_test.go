package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

func TestCopyingAnAsset(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	st := srv.store
	types, err := st.ListRelationTypes()
	if err != nil {
		t.Fatal(err)
	}
	typeNamed := map[string]models.RelationType{}
	for _, rt := range types {
		typeNamed[rt.Name] = rt
	}
	contains, usedWith := typeNamed["contains"], typeNamed["used with"]
	must := func(a *models.AssetWithDetails, err error) *models.AssetWithDetails {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return a
	}

	filament, err := st.CreateCategoryFrom(store.CategoryInput{Name: "Filament", IsConsumable: true, Unit: "g"})
	if err != nil {
		t.Fatal(err)
	}
	colour, err := st.CreateField("colour_name", "Color", "text", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AssignCategoryField(filament.ID, colour.ID, false); err != nil {
		t.Fatal(err)
	}
	shelf := must(st.CreateAsset("Shelf 2", nil, nil, nil))
	printer := must(st.CreateAsset("Prusa", nil, nil, nil))
	sample := must(st.CreateAsset("Test print", nil, nil, nil))
	old := must(st.CreateAsset("Old nozzle", nil, nil, nil))
	must(st.CreateAsset("Hammer", nil, []models.AssetGeneratorKey{{GeneratorType: "qr", AssignedCode: "HS-105"}}, nil))
	thousand := 1000
	spool := must(st.CreateAssetWithStock("Yellow PLA", []string{filament.ID}, []models.AssetGeneratorKey{{GeneratorType: "qr", AssignedCode: "SP-014"}},
		map[string]string{"colour_name": "Yellow"}, &store.StockInput{Quantity: &thousand, Unit: "g"}))
	must(st.AddLink(shelf.Asset.ID, contains.ID, spool.Asset.ID, true))    // the spool is on the shelf
	must(st.AddLink(spool.Asset.ID, usedWith.ID, printer.Asset.ID, true))  // and used with the printer
	must(st.AddLink(spool.Asset.ID, contains.ID, sample.Asset.ID, true))   // and holds a test print
	must(st.AddLink(spool.Asset.ID, usedWith.ID, old.Asset.ID, true))      // and an archived nozzle
	if err := st.DeleteAsset(old.Asset.ID); err != nil {
		t.Fatal(err)
	}

	rec := doJSON(t, srv, cookie, http.MethodGet, "/api/v1/assets/"+spool.Asset.ID+"/copy", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("copy: %d %s", rec.Code, rec.Body)
	}
	var got struct{ Copy AssetCopy }
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	c := got.Copy
	if c.Name != "Yellow PLA" || !reflect.DeepEqual(c.CategoryIDs, []string{filament.ID}) || !reflect.DeepEqual(c.FieldValues, map[string]string{"colour_name": "Yellow"}) {
		t.Errorf("copy: %+v", c)
	}
	if c.Stock == nil || c.Stock.Quantity == nil || *c.Stock.Quantity != 1000 || c.Stock.Unit != "g" {
		t.Errorf("stock: %+v", c.Stock)
	}
	// The next in its own series, not after the highest code of the type.
	if !reflect.DeepEqual(c.GeneratorKeys, []models.AssetGeneratorKey{{GeneratorType: "qr", AssignedCode: "SP-015"}}) {
		t.Errorf("codes: %+v", c.GeneratorKeys)
	}
	// On the same shelf and used with the same printer; not holding the
	// test print, which would leave the spool for it, nor the archived nozzle.
	var links []string
	for _, l := range c.Links {
		links = append(links, l.Label+" "+l.Asset.Name)
		if l.Asset.ID == shelf.Asset.ID && (!l.IsLocation || l.Outgoing) {
			t.Errorf("the shelf: %+v", l)
		}
	}
	sort.Strings(links)
	if !reflect.DeepEqual(links, []string{"contained in Shelf 2", "used with Prusa"}) {
		t.Errorf("links: %v", links)
	}
	// Nothing was made.
	if all, _ := st.ListAssets("", ""); len(all) != 5 {
		t.Errorf("%d assets after a copy was drafted", len(all))
	}

	// Without a code ending in a number, or a category, there is nothing to follow.
	plain := must(st.CreateAsset("Odd one", nil, []models.AssetGeneratorKey{{GeneratorType: "code128", AssignedCode: "ODD"}}, nil))
	rec = doJSON(t, srv, cookie, http.MethodGet, "/api/v1/assets/"+plain.Asset.ID+"/copy", "")
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || len(got.Copy.GeneratorKeys) != 0 || len(got.Copy.CategoryIDs) != 0 || got.Copy.Stock != nil {
		t.Errorf("plain copy: %+v, %v", got.Copy, err)
	}

	reader := userWithPermissions(t, srv, "reader@example.org", rbac.PermAssetsRead)
	if rec := doJSON(t, srv, reader, http.MethodGet, "/api/v1/assets/"+spool.Asset.ID+"/copy", ""); rec.Code != http.StatusForbidden {
		t.Errorf("a reader: %d", rec.Code)
	}
	if rec := doJSON(t, srv, cookie, http.MethodGet, "/api/v1/assets/nope/copy", ""); rec.Code != http.StatusNotFound {
		t.Errorf("a missing asset: %d", rec.Code)
	}
}

func TestMCPCopyAsset(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	st := srv.store
	cs := mcpSession(t, srv, tokenWith(t, srv, admin.ID, "Stocker", rbac.PermAssetsRead, rbac.PermAssetsScan, rbac.PermAssetsWrite))
	types, _ := st.ListRelationTypes()
	var contains, usedWith string
	for _, rt := range types {
		switch rt.Name {
		case "contains":
			contains = rt.ID
		case "used with":
			usedWith = rt.ID
		}
	}
	shelf, _ := st.CreateAsset("Shelf 2", nil, nil, nil)
	printer, _ := st.CreateAsset("Prusa", nil, nil, nil)
	thousand := 1000
	spool, err := st.CreateAssetWithStock("Yellow PLA", nil, []models.AssetGeneratorKey{{GeneratorType: "qr", AssignedCode: "SP-014"}}, nil, &store.StockInput{Quantity: &thousand, Unit: "g"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddLink(shelf.Asset.ID, contains, spool.Asset.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddLink(spool.Asset.ID, usedWith, printer.Asset.ID, true); err != nil {
		t.Fatal(err)
	}

	// Named by its code, with what differs.
	res := callTool(t, cs, "copy_asset", map[string]any{"asset": "SP-014", "name": "Blue PLA", "quantity": 750})
	var out struct {
		Asset struct {
			ID string `json:"id"`
		} `json:"asset"`
		Linked []string `json:"linked"`
	}
	structured(t, res, &out)
	made, err := st.GetAssetByID(out.Asset.ID)
	if err != nil {
		t.Fatalf("%v: %s", err, toolText(res))
	}
	if made.Asset.Name != "Blue PLA" || made.Asset.Stock == nil || made.Asset.Stock.Quantity != 750 || made.Asset.Stock.Unit != "g" ||
		len(made.GeneratorKeys) != 1 || made.GeneratorKeys[0].AssignedCode != "SP-015" {
		t.Errorf("made: %+v %+v", made.Asset, made.GeneratorKeys)
	}
	if len(made.Location) != 1 || made.Location[0].ID != shelf.Asset.ID || len(out.Linked) != 2 {
		t.Errorf("where: %+v, linked %v", made.Location, out.Linked)
	}
	if !strings.Contains(toolText(res), "used with Prusa") {
		t.Errorf("text: %s", toolText(res))
	}
	// The original is still on the shelf.
	if again, _ := st.GetAssetByID(spool.Asset.ID); len(again.Location) != 1 {
		t.Errorf("the original moved: %+v", again.Location)
	}
	// Given no code of a type, it has none.
	structured(t, callTool(t, cs, "copy_asset", map[string]any{"asset": spool.Asset.ID, "codes": []map[string]string{{"type": "qr", "code": ""}}}), &out)
	if again, _ := st.GetAssetByID(out.Asset.ID); len(again.GeneratorKeys) != 0 || len(again.Photos) != 0 {
		t.Errorf("codes: %+v, photos %d", again.GeneratorKeys, len(again.Photos))
	}

	// Its photos, when asked.
	body, ct := multipartBody(nil, false, map[string][]byte{"file": testJPEG(t, 20, 20)}, nil)
	if rec := do(t, srv, sessionCookie(t, srv, admin.ID), http.MethodPost, "/api/v1/assets/"+spool.Asset.ID+"/photos", ct, bytes.NewReader(body)); rec.Code != http.StatusCreated {
		t.Fatalf("upload: %d %s", rec.Code, rec.Body)
	}
	res = callTool(t, cs, "copy_asset", map[string]any{"asset": spool.Asset.ID, "photos": true})
	structured(t, res, &out)
	if again, _ := st.GetAssetByID(out.Asset.ID); len(again.Photos) != 1 || !strings.Contains(toolText(res), "a copy of the original's photo") {
		t.Errorf("photos %d: %s", len(again.Photos), toolText(res))
	}
}
