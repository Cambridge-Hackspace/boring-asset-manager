package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
)

func TestRunningLowAndThresholdsAPI(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	adminCookie := sessionCookie(t, srv, admin.ID)
	// Stores record stock without being able to edit anything; writers edit
	// assets without recording stock.
	stores := userWithPermissions(t, srv, "stores@hackspace.org", rbac.PermAssetsRead, rbac.PermStockRecord)
	writer := userWithPermissions(t, srv, "writer@hackspace.org", rbac.PermAssetsRead, rbac.PermAssetsWrite)
	reader := userWithPermissions(t, srv, "reader@hackspace.org", rbac.PermAssetsRead)

	// Thresholds go through the category form.
	rec := doJSON(t, srv, adminCookie, http.MethodPost, "/api/v1/categories", `{"name":"Tape","is_consumable":true,"low_stock_threshold":1}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	var created struct{ Category models.Category }
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	cat := created.Category
	if cat.LowStockThreshold == nil || *cat.LowStockThreshold != 1 || !cat.IsRunningLow {
		t.Fatalf("created %+v: an empty category with a threshold of 1 is running low", cat)
	}
	rec = doJSON(t, srv, adminCookie, http.MethodPost, "/api/v1/categories", `{"name":"Drills","low_stock_threshold":1}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("threshold on a non-consumable: %d %s", rec.Code, rec.Body)
	}

	rec = doJSON(t, srv, adminCookie, http.MethodPost, "/api/v1/assets", `{"name":"Roll","category_ids":["`+cat.ID+`"]}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("asset: %d %s", rec.Code, rec.Body)
	}
	var get struct {
		Category    models.Category
		ActiveCount int `json:"active_count"`
	}
	rec = doJSON(t, srv, reader, http.MethodGet, "/api/v1/categories/"+cat.ID, "")
	_ = json.Unmarshal(rec.Body.Bytes(), &get)
	if get.ActiveCount != 1 || get.Category.IsRunningLow {
		t.Fatalf("after one asset: %s", rec.Body)
	}

	// The running-low action needs stock:record, not assets:write.
	path := "/api/v1/categories/" + cat.ID + "/running-low"
	for _, who := range []*http.Cookie{reader, writer} {
		if rec := doJSON(t, srv, who, http.MethodPut, path, `{"running_low":true}`); rec.Code != http.StatusForbidden {
			t.Errorf("without stock:record: %d", rec.Code)
		}
	}
	for _, bad := range []string{`{}`, `{"running_low":"yes"}`, `nope`} {
		if rec := doJSON(t, srv, stores, http.MethodPut, path, bad); rec.Code != http.StatusBadRequest {
			t.Errorf("body %s: %d", bad, rec.Code)
		}
	}
	if rec := doJSON(t, srv, stores, http.MethodPut, "/api/v1/categories/missing/running-low", `{"running_low":true}`); rec.Code != http.StatusNotFound {
		t.Errorf("missing category: %d", rec.Code)
	}
	var set struct {
		Category models.Category
		Changed  bool
	}
	rec = doJSON(t, srv, stores, http.MethodPut, path, `{"running_low":true}`)
	_ = json.Unmarshal(rec.Body.Bytes(), &set)
	if rec.Code != http.StatusOK || !set.Changed || !set.Category.IsRunningLow {
		t.Fatalf("flagging: %d %s", rec.Code, rec.Body)
	}
	rec = doJSON(t, srv, stores, http.MethodPut, path, `{"running_low":true}`)
	_ = json.Unmarshal(rec.Body.Bytes(), &set)
	if rec.Code != http.StatusOK || set.Changed {
		t.Fatalf("flagging again: %d %s", rec.Code, rec.Body)
	}

	plain, err := srv.store.CreateCategory("Drills", nil, "", false, false)
	if err != nil {
		t.Fatal(err)
	}
	if rec := doJSON(t, srv, stores, http.MethodPut, "/api/v1/categories/"+plain.ID+"/running-low", `{"running_low":true}`); rec.Code != http.StatusBadRequest {
		t.Errorf("non-consumable: %d", rec.Code)
	}

	// Events name the signed-in user who caused them, whether by hand or by
	// archiving an asset.
	list := notices(t, srv, 10)
	if len(list) != 3 {
		t.Fatalf("%d events, want 3 (created low, restocked, flagged)", len(list))
	}
	byStores, byAdmin := 0, 0
	for _, e := range list {
		switch {
		case e.Payload.Actor == nil:
			t.Errorf("event without an actor: %+v", e.Payload)
		case e.Payload.Actor.Email == "stores@hackspace.org" && e.Payload.Trigger == models.TriggerManual:
			byStores++
		case e.Payload.Actor.ID == admin.ID && e.Payload.Trigger == models.TriggerThreshold:
			byAdmin++
		}
	}
	if byStores != 1 || byAdmin != 2 {
		t.Errorf("%d manual events by stores (want 1), %d threshold events by the admin (want 2)", byStores, byAdmin)
	}
}

// TestArchivingThroughTheAPICrossesThresholds checks the asset handlers
// attribute threshold events to the user.
func TestArchivingThroughTheAPICrossesThresholds(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/categories", `{"name":"Tape","is_consumable":true}`)
	var created struct{ Category models.Category }
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	id := created.Category.ID
	rec = doJSON(t, srv, cookie, http.MethodPost, "/api/v1/assets", `{"name":"Roll","category_ids":["`+id+`"]}`)
	var a struct{ Asset models.AssetWithDetails }
	_ = json.Unmarshal(rec.Body.Bytes(), &a)
	rec = doJSON(t, srv, cookie, http.MethodPut, "/api/v1/categories/"+id, `{"name":"Tape","is_consumable":true,"low_stock_threshold":1}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("threshold: %d %s", rec.Code, rec.Body)
	}

	if rec := doJSON(t, srv, cookie, http.MethodDelete, "/api/v1/assets/"+a.Asset.Asset.ID, ""); rec.Code != http.StatusOK {
		t.Fatalf("archive: %d %s", rec.Code, rec.Body)
	}
	list := notices(t, srv, 10)
	if len(list) != 1 {
		t.Fatalf("events %+v", list)
	}
	if e := list[0]; e.Kind != models.EventRunningLow || e.Payload.Trigger != models.TriggerThreshold || e.Payload.Actor == nil || e.Payload.Actor.ID != admin.ID {
		t.Errorf("event %+v", e.Payload)
	}

	// A full-replacement update without the threshold removes it.
	rec = doJSON(t, srv, cookie, http.MethodPut, "/api/v1/categories/"+id, `{"name":"Tape","is_consumable":true,"is_running_low":true}`)
	var updated struct{ Category models.Category }
	_ = json.Unmarshal(rec.Body.Bytes(), &updated)
	if updated.Category.LowStockThreshold != nil {
		t.Errorf("threshold kept: %s", rec.Body)
	}
}

// notices lists the newest events channels can be told of, leaving out the
// ones kept only for an asset's history.
func notices(t *testing.T, srv *Server, limit int) []models.Event {
	t.Helper()
	all, err := srv.store.ListEvents(100)
	if err != nil {
		t.Fatal(err)
	}
	var list []models.Event
	for _, e := range all {
		if !models.HistoryOnly(e.Kind) && len(list) < limit {
			list = append(list, e)
		}
	}
	return list
}
