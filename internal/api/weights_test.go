package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
)

func TestWeightsAPI(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)

	if rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/categories", `{"name":"Tools","unit":"g"}`); rec.Code != http.StatusBadRequest {
		t.Errorf("a unit on a category that is not consumable: %d %s", rec.Code, rec.Body)
	}
	rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/categories", `{"name":"Filament","is_consumable":true,"is_lendable":true,"unit":"g","low_stock_threshold":1000}`)
	var created struct{ Category models.Category }
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	if rec.Code != http.StatusCreated || created.Category.Unit != "g" {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	cat := created.Category.ID
	rec = doJSON(t, srv, cookie, http.MethodPost, "/api/v1/assets", `{"name":"Blue PLA","category_ids":["`+cat+`"],"stock":{"quantity":1000,"unit":"g"}}`)
	var a struct{ Asset models.AssetWithDetails }
	_ = json.Unmarshal(rec.Body.Bytes(), &a)
	if !a.Asset.Weighed || !a.Asset.Lendable {
		t.Fatalf("a spool: %s", rec.Body)
	}
	doJSON(t, srv, cookie, http.MethodPost, "/api/v1/assets", `{"name":"Red PLA","category_ids":["`+cat+`"],"stock":{"quantity":2000,"unit":"g"}}`)

	// The category reports its total in its unit, beside its count.
	rec = doJSON(t, srv, cookie, http.MethodGet, "/api/v1/categories/"+cat, "")
	var got struct {
		ActiveCount int  `json:"active_count"`
		Total       *int `json:"total"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got.ActiveCount != 2 || got.Total == nil || *got.Total != 3000 {
		t.Errorf("category: %s", rec.Body)
	}

	// A spool lent out comes back with what is left.
	id := a.Asset.Asset.ID
	if rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/assets/"+id+"/checkout", ""); rec.Code != http.StatusOK {
		t.Fatalf("lend: %d %s", rec.Code, rec.Body)
	}
	for body, want := range map[string]int{`{"left":-5}`: http.StatusBadRequest, `{"left":"lots"}`: http.StatusBadRequest} {
		if rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/assets/"+id+"/checkin", body); rec.Code != want {
			t.Errorf("%s: %d %s", body, rec.Code, rec.Body)
		}
	}
	rec = doJSON(t, srv, cookie, http.MethodPost, "/api/v1/assets/"+id+"/checkin", `{"left":250}`)
	_ = json.Unmarshal(rec.Body.Bytes(), &a)
	if rec.Code != http.StatusOK || a.Asset.Loan != nil || a.Asset.Asset.Stock.Quantity != 250 {
		t.Errorf("check in: %d %s", rec.Code, rec.Body)
	}
}
