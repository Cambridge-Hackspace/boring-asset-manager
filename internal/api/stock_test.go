package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
)

func TestAssetStockAPI(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	adminCookie := sessionCookie(t, srv, admin.ID)
	stores := userWithPermissions(t, srv, "stores@hackspace.org", rbac.PermAssetsRead, rbac.PermAssetsWrite)
	reader := userWithPermissions(t, srv, "reader@hackspace.org", rbac.PermAssetsRead)

	assetOf := func(rec interface{ Bytes() []byte }) models.Asset {
		t.Helper()
		var body struct{ Asset models.AssetWithDetails }
		if err := json.Unmarshal(rec.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		return body.Asset.Asset
	}

	for _, bad := range []string{
		`{"name":"Screws","stock":{"quantity":-1}}`,
		`{"name":"Screws","stock":{"quantity":1,"low_stock_threshold":0}}`,
		`{"name":"Screws","stock":{"quantity":"lots"}}`,
		`{"name":"Screws","stock":{"quantity":1.5}}`,
	} {
		if rec := doJSON(t, srv, adminCookie, http.MethodPost, "/api/v1/assets", bad); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: %d %s", bad, rec.Code, rec.Body)
		}
	}
	rec := doJSON(t, srv, adminCookie, http.MethodPost, "/api/v1/assets", `{"name":"M3 screws","stock":{"quantity":200,"unit":"pcs","low_stock_threshold":50}}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	a := assetOf(rec.Body)
	if s := a.Stock; s == nil || s.Quantity != 200 || s.Unit != "pcs" || *s.LowStockThreshold != 50 || s.IsRunningLow {
		t.Fatalf("created %+v", a.Stock)
	}
	rec = doJSON(t, srv, adminCookie, http.MethodPost, "/api/v1/assets", `{"name":"Drill"}`)
	drill := assetOf(rec.Body)
	if raw := rec.Body.String(); drill.Stock != nil || !json.Valid([]byte(raw)) {
		t.Fatalf("an asset without stock: %s", raw)
	}

	// Using and restocking need assets:write only.
	path := "/api/v1/assets/" + a.ID + "/quantity"
	if rec := doJSON(t, srv, reader, http.MethodPost, path, `{"adjust":-1}`); rec.Code != http.StatusForbidden {
		t.Errorf("a reader used stock: %d", rec.Code)
	}
	for _, bad := range []string{`{}`, `{"adjust":-1,"set":3}`, `{"adjust":"-1"}`, `nope`, `{"set":-1}`} {
		if rec := doJSON(t, srv, stores, http.MethodPost, path, bad); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: %d %s", bad, rec.Code, rec.Body)
		}
	}
	rec = doJSON(t, srv, stores, http.MethodPost, path, `{"adjust":-160}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("using 160: %d %s", rec.Code, rec.Body)
	}
	if s := assetOf(rec.Body).Stock; s.Quantity != 40 || !s.IsRunningLow {
		t.Errorf("after using 160: %+v", s)
	}
	if rec := doJSON(t, srv, stores, http.MethodPost, path, `{"adjust":-41}`); rec.Code != http.StatusConflict {
		t.Errorf("using more than there is: %d %s", rec.Code, rec.Body)
	}
	if rec := doJSON(t, srv, stores, http.MethodPost, "/api/v1/assets/missing/quantity", `{"adjust":1}`); rec.Code != http.StatusNotFound {
		t.Errorf("a missing asset: %d", rec.Code)
	}
	if rec := doJSON(t, srv, stores, http.MethodPost, "/api/v1/assets/"+drill.ID+"/quantity", `{"adjust":1}`); rec.Code != http.StatusBadRequest {
		t.Errorf("an asset not counted by quantity: %d", rec.Code)
	}
	rec = doJSON(t, srv, stores, http.MethodPost, path, `{"set":500}`)
	if s := assetOf(rec.Body).Stock; rec.Code != http.StatusOK || s.Quantity != 500 || s.IsRunningLow {
		t.Errorf("setting 500: %d %s", rec.Code, rec.Body)
	}

	// The flag by hand.
	flagPath := "/api/v1/assets/" + a.ID + "/running-low"
	if rec := doJSON(t, srv, reader, http.MethodPut, flagPath, `{"running_low":true}`); rec.Code != http.StatusForbidden {
		t.Errorf("a reader flagged stock: %d", rec.Code)
	}
	if rec := doJSON(t, srv, stores, http.MethodPut, flagPath, `{}`); rec.Code != http.StatusBadRequest {
		t.Errorf("no flag: %d", rec.Code)
	}
	rec = doJSON(t, srv, stores, http.MethodPut, flagPath, `{"running_low":true}`)
	var flagged struct {
		Asset   models.AssetWithDetails
		Changed bool
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &flagged)
	if rec.Code != http.StatusOK || !flagged.Changed || !flagged.Asset.Asset.Stock.IsRunningLow {
		t.Errorf("flagging: %d %s", rec.Code, rec.Body)
	}
	if rec := doJSON(t, srv, stores, http.MethodPut, "/api/v1/assets/"+drill.ID+"/running-low", `{"running_low":true}`); rec.Code != http.StatusBadRequest {
		t.Errorf("flagging an asset not counted by quantity: %d", rec.Code)
	}

	// Editing: absent stock keeps it, an object without a quantity keeps the
	// quantity, and null stops counting.
	assetPath := "/api/v1/assets/" + a.ID
	rec = doJSON(t, srv, stores, http.MethodPut, assetPath, `{"name":"M3x8 screws"}`)
	if s := assetOf(rec.Body).Stock; rec.Code != http.StatusOK || s == nil || s.Quantity != 500 {
		t.Fatalf("an edit without stock: %d %s", rec.Code, rec.Body)
	}
	rec = doJSON(t, srv, stores, http.MethodPut, assetPath, `{"stock":{"unit":"screws","low_stock_threshold":600}}`)
	if s := assetOf(rec.Body).Stock; rec.Code != http.StatusOK || s.Quantity != 500 || s.Unit != "screws" || *s.LowStockThreshold != 600 {
		t.Fatalf("an edit without a quantity: %d %s", rec.Code, rec.Body)
	}
	if rec := doJSON(t, srv, stores, http.MethodPut, assetPath, `{"stock":[1]}`); rec.Code != http.StatusBadRequest {
		t.Errorf("a stock that is not an object: %d", rec.Code)
	}

	// The history, newest first, for anyone who can read assets.
	rec = doJSON(t, srv, reader, http.MethodGet, assetPath+"/history", "")
	var history struct{ Events []models.Event }
	_ = json.Unmarshal(rec.Body.Bytes(), &history)
	if rec.Code != http.StatusOK || len(history.Events) < 4 || history.Events[0].Kind != models.EventAssetEdited || history.Events[2].Kind != models.EventAssetRunningLow {
		t.Fatalf("history: %d %s", rec.Code, rec.Body)
	}
	if c := history.Events[0].Payload.Changes; len(c) != 2 || c[0].What != "unit" || c[1] != (models.EventChange{What: "threshold", From: "50", To: "600"}) {
		t.Errorf("the stock edit: %+v", c)
	}
	if e := history.Events[len(history.Events)-1]; e.Kind != models.EventAssetCreated || e.Payload.Actor == nil || e.Payload.Actor.ID != admin.ID {
		t.Errorf("the first event: %+v", e)
	}
	// Older events a page at a time.
	for i := 0; i < historyPage; i++ {
		if rec := doJSON(t, srv, stores, http.MethodPost, assetPath+"/quantity", `{"adjust":1}`); rec.Code != http.StatusOK {
			t.Fatalf("restocking: %d %s", rec.Code, rec.Body)
		}
	}
	var page struct {
		Events []models.Event
		More   bool
	}
	rec = doJSON(t, srv, reader, http.MethodGet, assetPath+"/history", "")
	_ = json.Unmarshal(rec.Body.Bytes(), &page)
	if len(page.Events) != historyPage || !page.More {
		t.Fatalf("the first page: %d events, more %v", len(page.Events), page.More)
	}
	rec = doJSON(t, srv, reader, http.MethodGet, assetPath+"/history?before="+page.Events[historyPage-1].ID, "")
	page.More = true
	_ = json.Unmarshal(rec.Body.Bytes(), &page)
	if rec.Code != http.StatusOK || len(page.Events) != len(history.Events) || page.More || page.Events[0].ID != history.Events[0].ID {
		t.Errorf("the second page: %d, %d events, more %v", rec.Code, len(page.Events), page.More)
	}
	if rec := doJSON(t, srv, reader, http.MethodGet, assetPath+"/history?before=nope", ""); rec.Code != http.StatusBadRequest {
		t.Errorf("a page after an unknown event: %d", rec.Code)
	}
	if rec := doJSON(t, srv, reader, http.MethodGet, "/api/v1/assets/missing/history", ""); rec.Code != http.StatusNotFound {
		t.Errorf("a missing asset's history: %d", rec.Code)
	}

	rec = doJSON(t, srv, stores, http.MethodPut, assetPath, `{"stock":null}`)
	if rec.Code != http.StatusOK || assetOf(rec.Body).Stock != nil {
		t.Errorf("stopping counting: %d %s", rec.Code, rec.Body)
	}
}
