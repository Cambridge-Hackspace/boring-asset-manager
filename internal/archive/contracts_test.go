package archive

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/filestore"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// contractFixture is seedPurchase's vendor, category and asset, and a
// contract with the vendor covering both, with a cost, a file, and a
// reminder recorded.
type contractFixture struct {
	purchaseFixture
	contract *models.Contract
	terms    models.Attachment
}

func seedContract(t *testing.T, st *store.Store, blobs filestore.Blobs, by string) contractFixture {
	t.Helper()
	f := contractFixture{purchaseFixture: seedPurchase(t, st, blobs, by)}
	c, err := st.As(by).CreateContract(store.ContractInput{VendorID: f.vendor.ID, Kind: "maintenance", Name: "Screw service", Reference: "SS-1",
		StartDate: "2026-01-01", EndDate: "2026-12-31", AutoRenews: true, NoticeDays: 30, Cost: "99.50", Currency: "GBP", Billing: "yearly",
		Notes: "*Annual*", AssetIDs: []string{f.asset.Asset.ID}, CategoryIDs: []string{f.cat.ID}})
	if err != nil {
		t.Fatal(err)
	}
	f.terms = models.Attachment{ID: uuid.NewString(), Kind: models.AttachmentDocument, Name: "terms.pdf", Filename: "terms.pdf",
		ContentType: "application/pdf", ByteSize: 11, CreatedAt: time.Now()}
	if err := blobs.Put(context.Background(), "photos/"+f.terms.ID, strings.NewReader("%PDF-terms."), -1, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := st.As(by).AddAttachments(models.OwnerContract, c.ID, []models.Attachment{f.terms}, store.DefaultMaxAttachments); err != nil {
		t.Fatal(err)
	}
	if n, err := st.RecordContractReminders(time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC), 30); err != nil || n != 1 {
		t.Fatalf("reminding: %d %v", n, err)
	}
	if f.contract, err = st.GetContract(c.ID); err != nil {
		t.Fatal(err)
	}
	return f
}

// sameContract reports how two contracts differ, but for when they were
// made and changed.
func sameContract(a, b *models.Contract) bool {
	x, y := *a, *b
	x.CreatedAt, x.UpdatedAt, y.CreatedAt, y.UpdatedAt = time.Time{}, time.Time{}, time.Time{}, time.Time{}
	return strings.Join([]string{x.ID, x.VendorID, x.Kind, x.Name, x.Reference, x.StartDate, x.EndDate, x.Cost, x.Currency, x.Billing, x.Notes,
		x.NoticeDeadline, strings.Join(x.AssetIDs, ","), strings.Join(x.CategoryIDs, ",")}, "|") ==
		strings.Join([]string{y.ID, y.VendorID, y.Kind, y.Name, y.Reference, y.StartDate, y.EndDate, y.Cost, y.Currency, y.Billing, y.Notes,
			y.NoticeDeadline, strings.Join(y.AssetIDs, ","), strings.Join(y.CategoryIDs, ",")}, "|") && x.AutoRenews == y.AutoRenews && x.NoticeDays == y.NoticeDays
}

// TestBackupsCarryContracts: a backup holds contracts, what they cover,
// their files, their history and the reminders recorded, which a restore
// puts back, so they are not recorded again.
func TestBackupsCarryContracts(t *testing.T) {
	ctx := context.Background()
	src := setupStore(t)
	blobs := filestore.NewLocal(t.TempDir())
	s := seed(t, src, blobs)
	f := seedContract(t, src, blobs, s.user.ID)

	b := backup(t, src, blobs, Options{})
	dst := emptyStore(t, envDialect())
	into := filestore.NewLocal(t.TempDir())
	if _, err := Restore(ctx, dst, into, opener(b), RestoreOptions{}); err != nil {
		t.Fatal(err)
	}
	got, err := dst.GetContract(f.contract.ID)
	if err != nil || !sameContract(got, f.contract) {
		t.Fatalf("restored: %+v %v, want %+v", got, err, f.contract)
	}
	a, err := dst.GetAssetByID(f.asset.Asset.ID)
	if err != nil || len(a.Contracts) != 1 || a.Contracts[0].ID != f.contract.ID {
		t.Errorf("the asset restored: %+v %v", a, err)
	}
	files, err := dst.ListAttachments(models.OwnerContract, f.contract.ID)
	if err != nil || len(files) != 1 || files[0].ID != f.terms.ID {
		t.Errorf("its files: %+v %v", files, err)
	}
	if rc, err := into.Get(ctx, "photos/"+f.terms.ID); err != nil {
		t.Errorf("its file's bytes: %v", err)
	} else {
		rc.Close()
	}
	events, _, err := dst.ListContractEvents(f.contract.ID, "", 10)
	if err != nil || len(events) != 3 {
		t.Errorf("its history: %d %v", len(events), err)
	}
	if n, err := dst.RecordContractReminders(time.Date(2026, 11, 16, 9, 0, 0, 0, time.UTC), 30); err != nil || n != 0 {
		t.Errorf("reminded again once restored: %d %v", n, err)
	}
	// Its vendor is not deleted while it has contracts.
	if err := dst.DeleteVendor(f.vendor.ID); err == nil {
		t.Error("deleted the vendor of a restored contract")
	}
}

// TestPackagesCarryContracts: a vendor brings its contracts, with their
// files and what they cover of the package, but not the reminders
// recorded; imported, they are new, with the vendor, category and asset
// as imported. A package by someone without purchases.read leaves out
// their costs.
func TestPackagesCarryContracts(t *testing.T) {
	ctx := context.Background()
	src := setupStore(t)
	blobs := filestore.NewLocal(t.TempDir())
	s := seed(t, src, blobs)
	f := seedContract(t, src, blobs, s.user.ID)
	sel := Selection{Categories: []string{f.cat.ID}, Assets: &AssetsSelection{Photos: true}}

	r := packageOf(t, src, blobs, sel, nil)
	if got := ids(t, r, "contracts", "id"); len(got) != 1 || got[0] != f.contract.ID {
		t.Errorf("contracts packaged: %v", got)
	}
	if rows := r.rows(t, DataDir+"contracts/"); len(rows) != 1 || rows[0]["cost_minor"] == nil || rows[0]["billing"] != "yearly" {
		t.Errorf("its row: %v", rows)
	}
	for table, want := range map[string]int{"contract_assets": 1, "contract_categories": 1, "contract_attachments": 1, "contract_notices": 0} {
		if rows := r.rows(t, DataDir+table+"/"); len(rows) != want {
			t.Errorf("%s packaged: %v", table, rows)
		}
	}

	pkg := exportPackage(t, src, blobs, sel)
	into := migratedStore(t, envDialect())
	if _, err := Import(ctx, into, filestore.NewLocal(t.TempDir()), opener(pkg), ImportOptions{}); err != nil {
		t.Fatal(err)
	}
	list, err := into.ListContracts()
	if err != nil || len(list) != 1 {
		t.Fatalf("imported: %+v %v", list, err)
	}
	c := list[0]
	vendors, _ := into.ListVendors()
	assets, _ := into.ListAssets("", "Box of screws")
	files, _ := into.ListAttachments(models.OwnerContract, c.ID)
	if c.ID == f.contract.ID || len(vendors) != 1 || c.VendorID != vendors[0].ID || c.Cost != "99.50" || c.NoticeDeadline != "2026-12-01" ||
		len(assets) != 1 || len(c.AssetIDs) != 1 || c.AssetIDs[0] != assets[0].Asset.ID || len(c.CategoryIDs) != 1 || c.Categories[0].Name != "Screws" ||
		len(files) != 1 {
		t.Errorf("imported: %+v, vendors %+v, assets %+v, files %+v", c, vendors, assets, files)
	}
	// Its reminder was this instance's own: the new one records its own.
	if n, err := into.RecordContractReminders(time.Date(2026, 11, 16, 9, 0, 0, 0, time.UTC), 30); err != nil || n != 1 {
		t.Errorf("reminding once imported: %d %v", n, err)
	}

	noPrices := func(perm string, _ []string) bool { return perm != rbac.PermPurchasesRead }
	r = packageOf(t, src, blobs, sel, noPrices)
	if rows := r.rows(t, DataDir+"contracts/"); len(rows) != 1 || rows[0]["cost_minor"] != nil || rows[0]["currency"] != "" || rows[0]["billing"] != "" ||
		rows[0]["name"] != "Screw service" {
		t.Errorf("without purchases.read: %v", rows)
	}
}
