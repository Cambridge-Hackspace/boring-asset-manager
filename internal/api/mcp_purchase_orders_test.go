package api

import (
	"strings"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
)

// TestPurchaseOrdersOverMCP: an assistant is suggested what to order,
// makes a draft from the catalog, changes only what it gives, places it,
// receives it, restocking the bin and queueing the drill's label, reads
// its history and PDF, and reads and sets how numbers are shown.
func TestPurchaseOrdersOverMCP(t *testing.T) {
	sc := newOrderScene(t)
	srv := sc.srv
	secret := tokenWith(t, srv, sc.adminID, "assistant", rbac.PermAssetsRead, rbac.PermAssetsWrite, rbac.PermVendorsManage, rbac.PermPurchasesRead,
		rbac.PermStockRecord, rbac.PermLabelsPrint, rbac.PermSettingsManage)
	cs := mcpSession(t, srv, secret)

	if res := callTool(t, cs, "suggest_purchase_orders", map[string]any{}); res.IsError || !strings.Contains(toolText(res), "1 vendors to order from") {
		t.Fatalf("suggested: %s", toolText(res))
	}
	type order struct {
		ID        string
		Reference string
		State     string
		Notes     string
		Total     string
		Lines     []struct {
			ID       string
			Quantity int
		}
	}
	var made struct{ Order order }
	structured(t, callTool(t, cs, "save_purchase_order", map[string]any{"vendor_id": sc.acme.ID, "lines": []map[string]any{
		{"catalog_entry_id": sc.bolts.ID, "quantity": 2}, {"catalog_entry_id": sc.drillsEntry.ID, "quantity": 1}}}), &made)
	if made.Order.Reference != "PO-0001" || made.Order.Total != "108.00" || len(made.Order.Lines) != 2 {
		t.Fatalf("made: %+v", made)
	}
	id := made.Order.ID
	structured(t, callTool(t, cs, "save_purchase_order", map[string]any{"id": id, "notes": "Ring first"}), &made)
	if made.Order.Notes != "Ring first" || made.Order.Total != "108.00" || len(made.Order.Lines) != 2 {
		t.Errorf("saved: %+v", made)
	}
	structured(t, callTool(t, cs, "place_purchase_order", map[string]any{"id": id}), &made)
	if made.Order.State != "ordered" {
		t.Fatalf("placed: %+v", made)
	}
	res := callTool(t, cs, "receive_purchase_order", map[string]any{"id": id, "queue_labels": true, "lines": []map[string]any{
		{"line_id": made.Order.Lines[0].ID, "quantity": 2}, {"line_id": made.Order.Lines[1].ID, "quantity": 1, "name": "New drill"}}})
	if res.IsError || !strings.Contains(toolText(res), "1 assets made, 1 restocked") ||
		!strings.Contains(toolText(res), "labels queued") && !strings.Contains(toolText(res), "could not be queued") {
		t.Fatalf("received: %s", toolText(res))
	}
	var list struct{ Orders []order }
	structured(t, callTool(t, cs, "list_purchase_orders", map[string]any{"state": "received"}), &list)
	if len(list.Orders) != 1 || list.Orders[0].ID != id {
		t.Errorf("listed: %+v", list)
	}
	if res := callTool(t, cs, "purchase_order_history", map[string]any{"id": id}); res.IsError || !strings.Contains(toolText(res), "4 events") {
		t.Errorf("history: %s", toolText(res))
	}
	if res := callTool(t, cs, "purchase_order_pdf", map[string]any{"id": id}); res.IsError || !strings.Contains(toolText(res), "A PDF of") {
		t.Errorf("PDF: %s", toolText(res))
	}
	if res := callTool(t, cs, "set_order_numbering", map[string]any{"prefix": "HS-", "padding": 2}); res.IsError {
		t.Fatalf("numbering: %s", toolText(res))
	}
	if res := callTool(t, cs, "get_order_numbering", map[string]any{}); res.IsError || !strings.Contains(toolText(res), `"HS-"`) {
		t.Errorf("numbering read: %s", toolText(res))
	}
	var got struct{ Order order }
	structured(t, callTool(t, cs, "get_purchase_order", map[string]any{"id": id}), &got)
	if got.Order.Reference != "HS-01" {
		t.Errorf("got: %+v", got)
	}
	if res := callTool(t, cs, "cancel_purchase_order", map[string]any{"id": id}); !res.IsError {
		t.Errorf("canceled a received order: %s", toolText(res))
	}
	if res := callTool(t, cs, "delete_purchase_order", map[string]any{"id": id}); res.IsError {
		t.Errorf("deleted: %s", toolText(res))
	}
}
