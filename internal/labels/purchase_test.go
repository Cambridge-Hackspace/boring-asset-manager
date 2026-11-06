package labels

import "testing"

// TestPurchaseBindings: {{asset.purchase.*}} resolves to where the asset
// came from, and an unknown one is refused.
func TestPurchaseBindings(t *testing.T) {
	d := Data{Purchase: PurchaseData{Vendor: "Acme", OrderDate: "2026-01-05", ReceivedDate: "2026-01-09", Price: "12.50", Currency: "GBP",
		OrderNumber: "PO-17", WarrantyEnd: "2028-01-09"}}
	b, err := ParseBinding("{{asset.purchase.vendor}} {{asset.purchase.order_date}} {{asset.purchase.received_date}} {{ asset.purchase.price }} " +
		"{{asset.purchase.currency}} {{asset.purchase.order_number}} {{asset.purchase.warranty_end}}")
	if err != nil {
		t.Fatal(err)
	}
	if got := b.Resolve(d); got != "Acme 2026-01-05 2026-01-09 12.50 GBP PO-17 2028-01-09" {
		t.Errorf("resolved: %q", got)
	}
	if _, err := ParseBinding("{{asset.purchase.cost}}"); err == nil {
		t.Error("an unknown purchase detail is taken")
	}
}
