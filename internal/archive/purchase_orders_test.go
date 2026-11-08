package archive

import (
	"context"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/filestore"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// TestBackupsCarryPurchaseOrders: a backup holds purchase orders and their
// lines with the assets, and how their numbers are shown with the
// settings; a restore puts them back as they were. A package never holds
// them, nor do the events it carries name them.
func TestBackupsCarryPurchaseOrders(t *testing.T) {
	ctx := context.Background()
	src := setupStore(t)
	blobs := filestore.NewLocal(t.TempDir())
	s := seed(t, src, blobs)
	f := seedCatalog(t, src, blobs, s.user.ID)
	order, err := src.As(s.user.ID).CreatePurchaseOrder(store.OrderInput{VendorID: f.vendor.ID, DeliverTo: "The Hackspace",
		Lines: []store.OrderLineInput{{CatalogEntryID: f.screws.ID, Quantity: 2}, {CatalogEntryID: f.solderly.ID, Quantity: 1}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := src.PlacePurchaseOrder(order.ID, "2026-11-01"); err != nil {
		t.Fatal(err)
	}
	if _, err := src.ReceivePurchaseOrder(order.ID, store.ReceiptInput{ReceivedOn: "2026-11-03", Lines: []store.ReceiptLine{{LineID: order.Lines[0].ID, Quantity: 1}}}); err != nil {
		t.Fatal(err)
	}
	if err := src.SetOrderNumbering(store.OrderNumbering{Prefix: "HS-", Padding: 3}); err != nil {
		t.Fatal(err)
	}
	want, err := src.GetPurchaseOrder(order.ID)
	if err != nil {
		t.Fatal(err)
	}

	b := backup(t, src, blobs, Options{})
	r := readArchive(t, b)
	for dir, n := range map[string]int{"purchase_orders": 1, "purchase_order_lines": 2} {
		if got := len(r.rows(t, DataDir+dir+"/")); got != n {
			t.Errorf("%s: %d rows, want %d", dir, got, n)
		}
	}
	dst := emptyStore(t, envDialect())
	if _, err := Restore(ctx, dst, filestore.NewLocal(t.TempDir()), opener(b), RestoreOptions{}); err != nil {
		t.Fatal(err)
	}
	got, err := dst.GetPurchaseOrder(order.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Reference != "HS-001" || got.State != want.State || got.Total != want.Total || got.DeliverTo != want.DeliverTo || len(got.Lines) != 2 ||
		got.Lines[0].QuantityReceived != 1 || got.Lines[0].CatalogEntryID != f.screws.ID || got.Lines[1].AssetID != f.solder.Asset.ID {
		t.Errorf("restored %+v, want %+v", got, want)
	}
	history, _, err := dst.ListOrderEvents(order.ID, "", 10)
	if err != nil || len(history) != 3 {
		t.Errorf("restored history %+v %v", history, err)
	}

	sel := Selection{Categories: []string{f.cat.ID}, Assets: &AssetsSelection{}}
	p := packageOf(t, src, blobs, sel, nil)
	for _, dir := range []string{"purchase_orders", "purchase_order_lines", "purchase_order_attachments"} {
		if rows := p.rows(t, DataDir+dir+"/"); len(rows) != 0 {
			t.Errorf("%s packaged: %v", dir, rows)
		}
	}
	for _, e := range p.rows(t, DataDir+"events/") {
		if e["purchase_order_id"] != nil {
			t.Errorf("a packaged event names an order: %v", e)
		}
	}
}
