package api

import (
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
)

// TestPurchasesOverMCP: an assistant sets where an asset came from, a
// detail at a time, reads it back, and finds the assets bought from a
// vendor; one without purchases.read sees no price, and leaves it as it is.
func TestPurchasesOverMCP(t *testing.T) {
	sc := newPurchaseScene(t)
	srv := sc.srv
	cs := mcpSession(t, srv, tokenWith(t, srv, sc.adminID, "assistant", rbac.PermAssetsRead, rbac.PermAssetsWrite, rbac.PermPurchasesRead))

	type detail struct {
		Purchase *models.Purchase `json:"purchase"`
	}
	var got detail
	res := callTool(t, cs, "update_asset", map[string]any{"asset": sc.drill.Asset.ID, "purchase": map[string]any{"price": "99.5", "order_number": ""}})
	if res.IsError {
		t.Fatalf("update_asset: %s", toolText(res))
	}
	structured(t, callTool(t, cs, "get_asset", map[string]any{"asset": sc.drill.Asset.ID}), &got)
	if p := got.Purchase; p == nil || p.Price != "99.50" || p.Currency != "GBP" || p.OrderNumber != "" || p.VendorName != "Acme" || p.WarrantyEnd != "2028-01-09" {
		t.Errorf("read back: %+v", got.Purchase)
	}
	var found struct {
		Total int `json:"total"`
	}
	structured(t, callTool(t, cs, "search_assets", map[string]any{"vendor_id": sc.acme.ID}), &found)
	if found.Total != 2 {
		t.Errorf("bought from Acme: %d", found.Total)
	}

	blind := mcpSession(t, srv, tokenWith(t, srv, sc.adminID, "blind", rbac.PermAssetsRead, rbac.PermAssetsWrite))
	got = detail{}
	structured(t, callTool(t, blind, "get_asset", map[string]any{"asset": sc.drill.Asset.ID}), &got)
	if p := got.Purchase; p == nil || p.Price != "" || !p.PricesHidden || p.VendorName != "Acme" {
		t.Errorf("without purchases.read: %+v", got.Purchase)
	}
	if res := callTool(t, blind, "update_asset", map[string]any{"asset": sc.drill.Asset.ID, "purchase": map[string]any{"price": "1", "received_date": "2026-02-02"}}); res.IsError {
		t.Fatalf("update_asset without purchases.read: %s", toolText(res))
	}
	stored, _ := srv.store.GetAssetByID(sc.drill.Asset.ID)
	if p := stored.Purchase; p.Price != "99.50" || p.ReceivedDate != "2026-02-02" {
		t.Errorf("after their change: %+v", p)
	}
}
