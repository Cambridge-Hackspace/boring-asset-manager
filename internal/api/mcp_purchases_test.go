package api

import (
	"strings"
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

// TestInvoiceOverMCP: an assistant without purchases.read for an asset
// neither lists its invoice among its attachments nor reads, changes or
// removes it; one with it does.
func TestInvoiceOverMCP(t *testing.T) {
	sc := newPurchaseScene(t)
	srv := sc.srv
	photo, receipt, manual := invoiced(t, sc, sc.drill)
	type listed struct {
		Attachments []struct{ ID, Name string }
	}
	names := func(l listed) string {
		var out []string
		for _, a := range l.Attachments {
			out = append(out, a.Name)
		}
		return strings.Join(out, ",")
	}
	at := map[string]any{"asset": sc.drill.Asset.ID}
	with := func(kv ...any) map[string]any {
		m := map[string]any{"asset": sc.drill.Asset.ID}
		for i := 0; i < len(kv); i += 2 {
			m[kv[i].(string)] = kv[i+1]
		}
		return m
	}

	blind := mcpSession(t, srv, tokenWith(t, srv, sc.adminID, "blind", rbac.PermAssetsRead, rbac.PermAssetsWrite))
	var l listed
	structured(t, callTool(t, blind, "list_attachments", at), &l)
	if names(l) != "photo.jpg,manual.pdf" {
		t.Errorf("listed without purchases.read: %s", names(l))
	}
	for _, c := range []struct {
		tool string
		args map[string]any
	}{
		{"read_attachment", with("attachment_id", receipt)},
		{"update_attachment", with("attachment_id", receipt, "name", "Bill")},
		{"remove_attachment", with("attachment_id", receipt)},
		{"reorder_attachments", with("attachment_ids", []string{receipt, photo, manual})},
	} {
		if res := callTool(t, blind, c.tool, c.args); !res.IsError {
			t.Errorf("%s without purchases.read: %s", c.tool, toolText(res))
		}
	}
	structured(t, callTool(t, blind, "reorder_attachments", with("attachment_ids", []string{manual, photo})), &l)
	if names(l) != "manual.pdf,photo.jpg" {
		t.Errorf("reordered without purchases.read: %s", names(l))
	}

	sees := mcpSession(t, srv, tokenWith(t, srv, sc.adminID, "sees", rbac.PermAssetsRead, rbac.PermAssetsWrite, rbac.PermPurchasesRead))
	structured(t, callTool(t, sees, "list_attachments", at), &l)
	if names(l) != "manual.pdf,receipt.jpg,photo.jpg" {
		t.Errorf("listed with purchases.read: %s", names(l))
	}
	if res := callTool(t, sees, "read_attachment", with("attachment_id", receipt)); res.IsError {
		t.Errorf("read_attachment with purchases.read: %s", toolText(res))
	}
	structured(t, callTool(t, sees, "remove_attachment", with("attachment_id", receipt)), &l)
	if names(l) != "manual.pdf,photo.jpg" {
		t.Errorf("removed with purchases.read: %s", names(l))
	}
}
