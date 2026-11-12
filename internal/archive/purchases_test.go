package archive

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/filestore"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// purchaseFixture is seedVendor's vendor, and an asset bought from it in
// its category, with a price, a warranty announced, and a document as its
// invoice.
type purchaseFixture struct {
	vendorFixture
	asset   *models.AssetWithDetails
	invoice models.Attachment
}

func seedPurchase(t *testing.T, st *store.Store, blobs filestore.Blobs, by string) purchaseFixture {
	t.Helper()
	f := purchaseFixture{vendorFixture: seedVendor(t, st, blobs, by)}
	a, err := st.CreateAssetWithPurchase("Box of screws", []string{f.cat.ID}, nil, nil, nil, "", &store.PurchaseInput{VendorID: f.vendor.ID,
		OrderDate: "2026-01-05", Price: "12.34", Currency: "GBP", OrderNumber: "PO-1", WarrantyEnd: "2026-02-01"})
	if err != nil {
		t.Fatal(err)
	}
	f.invoice = models.Attachment{ID: uuid.NewString(), Kind: models.AttachmentDocument, Name: "invoice.pdf", Filename: "invoice.pdf",
		ContentType: "application/pdf", ByteSize: 9, CreatedAt: time.Now()}
	if err := blobs.Put(context.Background(), "photos/"+f.invoice.ID, strings.NewReader("%PDF-inv."), -1, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddAttachments(models.OwnerAsset, a.Asset.ID, []models.Attachment{f.invoice}, store.DefaultMaxAttachments); err != nil {
		t.Fatal(err)
	}
	in := store.PurchaseInputOf(a.Purchase)
	in.InvoiceID = f.invoice.ID
	if f.asset, err = st.UpdateAssetWithPurchase(a.Asset.ID, "", nil, nil, nil, nil, nil, &in); err != nil {
		t.Fatal(err)
	}
	if n, err := st.RecordExpiringWarranties(time.Date(2026, 1, 20, 0, 0, 0, 0, time.UTC), 30); err != nil || n != 1 {
		t.Fatalf("announcing the warranty: %d %v", n, err)
	}
	return f
}

// TestBackupsCarryPurchases: a backup holds where assets came from and the
// warranties announced, which a restore puts back, so they are not
// announced again; one without documents does not name an invoice it
// leaves out.
func TestBackupsCarryPurchases(t *testing.T) {
	ctx := context.Background()
	src := setupStore(t)
	blobs := filestore.NewLocal(t.TempDir())
	s := seed(t, src, blobs)
	f := seedPurchase(t, src, blobs, s.user.ID)

	b := backup(t, src, blobs, Options{})
	dst := emptyStore(t, envDialect())
	if _, err := Restore(ctx, dst, filestore.NewLocal(t.TempDir()), opener(b), RestoreOptions{}); err != nil {
		t.Fatal(err)
	}
	got, err := dst.GetAssetByID(f.asset.Asset.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Purchase != f.asset.Purchase {
		t.Errorf("restored: %+v, want %+v", got.Purchase, f.asset.Purchase)
	}
	if n, err := dst.RecordExpiringWarranties(time.Date(2026, 1, 21, 0, 0, 0, 0, time.UTC), 30); err != nil || n != 0 {
		t.Errorf("announced again once restored: %d %v", n, err)
	}

	// Without documents, the invoice goes, and is not named.
	b = backup(t, src, blobs, Options{NoDocuments: true})
	rows := readArchive(t, b).rows(t, DataDir+"asset_purchases/")
	if len(rows) != 1 || rows[0]["invoice_attachment_id"] != nil || rows[0]["price_minor"] == nil {
		t.Errorf("without documents: %v", rows)
	}
	dst = emptyStore(t, envDialect())
	if _, err := Restore(ctx, dst, filestore.NewLocal(t.TempDir()), opener(b), RestoreOptions{}); err != nil {
		t.Fatal(err)
	}
	if got, err := dst.GetAssetByID(f.asset.Asset.ID); err != nil || got.Purchase.InvoiceID != "" || got.Purchase.Price != "12.34" {
		t.Errorf("restored without documents: %+v %v", got, err)
	}
}

// TestPackagesCarryPurchases: an asset brings the vendor it was bought
// from, and its purchase details, its invoice among its attachments;
// imported, they name the vendor and invoice as imported. A package by
// someone without purchases.read leaves out the price, order number and
// invoice.
func TestPackagesCarryPurchases(t *testing.T) {
	ctx := context.Background()
	src := setupStore(t)
	blobs := filestore.NewLocal(t.TempDir())
	s := seed(t, src, blobs)
	f := seedPurchase(t, src, blobs, s.user.ID)
	// The category no longer names the vendor: the asset brings it.
	if _, err := src.SetCategoryVendors(f.cat.ID, nil); err != nil {
		t.Fatal(err)
	}
	sel := Selection{Categories: []string{f.cat.ID}, Assets: &AssetsSelection{Photos: true}}

	r := packageOf(t, src, blobs, sel, nil)
	if got := ids(t, r, "vendors", "id"); len(got) != 1 || got[0] != f.vendor.ID {
		t.Errorf("vendors packaged: %v", got)
	}
	if rows := r.rows(t, DataDir+"warranty_notices/"); len(rows) != 0 {
		t.Errorf("warranty notices packaged: %v", rows)
	}
	rows := r.rows(t, DataDir+"asset_purchases/")
	if len(rows) != 1 || rows[0]["vendor_id"] != f.vendor.ID || rows[0]["invoice_attachment_id"] != f.invoice.ID || rows[0]["order_number"] != "PO-1" {
		t.Errorf("purchases packaged: %v", rows)
	}

	pkg := exportPackage(t, src, blobs, sel)
	into := migratedStore(t, envDialect())
	if _, err := Import(ctx, into, filestore.NewLocal(t.TempDir()), opener(pkg), ImportOptions{}); err != nil {
		t.Fatal(err)
	}
	list, err := into.ListAssets("", "Box of screws")
	if err != nil || len(list) != 1 {
		t.Fatalf("imported: %+v %v", list, err)
	}
	p := list[0].Purchase
	vendors, _ := into.ListVendors()
	files, _ := into.ListAttachments(models.OwnerAsset, list[0].Asset.ID)
	if len(vendors) != 1 || p.VendorID != vendors[0].ID || p.Price != "12.34" || p.Currency != "GBP" || p.OrderNumber != "PO-1" || p.WarrantyEnd != "2026-02-01" ||
		len(files) != 1 || p.InvoiceID != files[0].ID || p.InvoiceID == f.invoice.ID {
		t.Errorf("imported: %+v, vendors %+v, files %+v", p, vendors, files)
	}

	// Without its photos, no invoice is named; without purchases.read, no
	// price.
	r = packageOf(t, src, blobs, Selection{Categories: []string{f.cat.ID}, Assets: &AssetsSelection{}}, nil)
	if rows := r.rows(t, DataDir+"asset_purchases/"); len(rows) != 1 || rows[0]["invoice_attachment_id"] != nil || rows[0]["price_minor"] == nil {
		t.Errorf("without its attachments: %v", rows)
	}
	noPrices := func(perm string, _ []string) bool { return perm != rbac.PermPurchasesRead }
	r = packageOf(t, src, blobs, sel, noPrices)
	if rows := r.rows(t, DataDir+"asset_purchases/"); len(rows) != 1 || rows[0]["price_minor"] != nil || rows[0]["currency"] != "" ||
		rows[0]["order_number"] != "" || rows[0]["invoice_attachment_id"] != nil || rows[0]["warranty_end"] != "2026-02-01" || rows[0]["vendor_id"] != f.vendor.ID {
		t.Errorf("without purchases.read: %v", rows)
	}
	// Nor the invoice itself, nor its file, while it is the invoice.
	if got := ids(t, r, "asset_attachments", "id"); len(got) != 0 {
		t.Errorf("attachments packaged without purchases.read: %v", got)
	}
	if _, ok := r.members[FilesDir+"photos/"+f.invoice.ID]; ok {
		t.Error("the invoice's file packaged without purchases.read")
	}
	for _, c := range []struct {
		may    Reader
		photos int
	}{{noPrices, 0}, {nil, 1}} {
		if plan, err := Resolve(ctx, src, src.DB, sel, c.may); err != nil || plan.Assets == nil || plan.Assets.Photos != c.photos {
			t.Errorf("planned, %d attachments wanted: %+v %v", c.photos, plan, err)
		}
	}
	// Nor its history's changes to them.
	r = packageOf(t, src, blobs, Selection{Categories: []string{f.cat.ID}, Assets: &AssetsSelection{History: true}}, noPrices)
	events := r.rows(t, DataDir+"events/")
	if len(events) == 0 {
		t.Fatal("no history packaged")
	}
	for _, e := range events {
		if b, _ := json.Marshal(e); strings.Contains(string(b), "12.34") || strings.Contains(string(b), "PO-1") {
			t.Errorf("a price in the history packaged without purchases.read: %s", b)
		}
	}

	// Once it is no longer the invoice, it is an ordinary attachment.
	if _, err := src.UpdateAssetWithPurchase(f.asset.Asset.ID, "", nil, nil, nil, nil, nil, &store.PurchaseInput{VendorID: f.vendor.ID}); err != nil {
		t.Fatal(err)
	}
	r = packageOf(t, src, blobs, sel, noPrices)
	if got := ids(t, r, "asset_attachments", "id"); len(got) != 1 || got[0] != f.invoice.ID {
		t.Errorf("no longer the invoice, packaged without purchases.read: %v", got)
	}
	if _, ok := r.members[FilesDir+"photos/"+f.invoice.ID]; !ok {
		t.Error("no longer the invoice, its file was not packaged")
	}
}
