package archive

import (
	"context"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/filestore"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// catalogFixture is a vendor's catalog: an entry for a category of screws,
// preferred, and one for a reel of solder in it.
type catalogFixture struct {
	vendorFixture
	solder           *models.AssetWithDetails
	screws, solderly *models.CatalogEntry
}

func seedCatalog(t *testing.T, st *store.Store, blobs filestore.Blobs, by string) catalogFixture {
	t.Helper()
	f := catalogFixture{vendorFixture: seedVendor(t, st, blobs, by)}
	var err error
	if f.solder, err = st.CreateAsset("Solder reel", []string{f.cat.ID}, nil, nil); err != nil {
		t.Fatal(err)
	}
	hundred := 100
	if f.screws, err = st.CreateCatalogEntry(store.CatalogEntryInput{VendorID: f.vendor.ID, CategoryID: f.cat.ID, PartNumber: "SC-100", Description: "M3 screws",
		URL: "https://acme.example/sc-100", PackSize: &hundred, PackUnit: "screws", Price: "4.50", Currency: "GBP", PriceChecked: "2026-11-01", Preferred: true}); err != nil {
		t.Fatal(err)
	}
	if f.solderly, err = st.CreateCatalogEntry(store.CatalogEntryInput{VendorID: f.vendor.ID, AssetID: f.solder.Asset.ID, PartNumber: "SR-500",
		Price: "1300", Currency: "JPY"}); err != nil {
		t.Fatal(err)
	}
	return f
}

// sameEntry reports whether two entries hold the same, but for their IDs
// and what they name.
func sameEntry(a, b models.CatalogEntry) bool {
	pack := func(p *int) int {
		if p == nil {
			return 0
		}
		return *p
	}
	return a.PartNumber == b.PartNumber && a.Description == b.Description && a.URL == b.URL && pack(a.PackSize) == pack(b.PackSize) &&
		a.PackUnit == b.PackUnit && a.Price == b.Price && a.Currency == b.Currency && a.PriceChecked == b.PriceChecked && a.Preferred == b.Preferred &&
		a.LinkedName == b.LinkedName && a.VendorName == b.VendorName
}

// TestBackupsCarryCatalogs: a backup holds what vendors sell, the
// categories' entries with the catalog and the assets' with the assets,
// and a restore puts them back as they were.
func TestBackupsCarryCatalogs(t *testing.T) {
	src := setupStore(t)
	blobs := filestore.NewLocal(t.TempDir())
	s := seed(t, src, blobs)
	f := seedCatalog(t, src, blobs, s.user.ID)

	b := backup(t, src, blobs, Options{})
	r := readArchive(t, b)
	for dir, want := range map[string]int{"category_catalog_entries": 1, "asset_catalog_entries": 1} {
		if got := len(r.rows(t, DataDir+dir+"/")); got != want {
			t.Errorf("%s: %d rows, want %d", dir, got, want)
		}
	}
	dst := emptyStore(t, envDialect())
	if _, err := Restore(context.Background(), dst, filestore.NewLocal(t.TempDir()), opener(b), RestoreOptions{}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []*models.CatalogEntry{f.screws, f.solderly} {
		got, err := dst.GetCatalogEntry(want.ID)
		if err != nil || !sameEntry(*got, *want) || got.CategoryID != want.CategoryID || got.AssetID != want.AssetID || got.VendorID != want.VendorID {
			t.Errorf("restored: %+v %v, want %+v", got, err, want)
		}
	}
}

// TestPackagesCarryCatalogs: a category brings the vendors that sell it,
// and an asset those that sell it, with their entries; imported, the
// entries name the vendor, category and asset as imported. A package by
// someone without purchases.read leaves out the prices.
func TestPackagesCarryCatalogs(t *testing.T) {
	ctx := context.Background()
	src := setupStore(t)
	blobs := filestore.NewLocal(t.TempDir())
	s := seed(t, src, blobs)
	f := seedCatalog(t, src, blobs, s.user.ID)
	// The category no longer names the vendor: its catalog brings it.
	if _, err := src.SetCategoryVendors(f.cat.ID, nil); err != nil {
		t.Fatal(err)
	}
	plan, err := Resolve(ctx, src, src.DB, Selection{Categories: []string{f.cat.ID}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, it := range plan.Items {
		found = found || it.Kind == ItemVendor && it.ID == f.vendor.ID && len(it.NeededBy) == 1 && it.NeededBy[0].ID == f.cat.ID
	}
	if !found {
		t.Errorf("the vendor that sells the category is not needed by it: %+v", plan.Items)
	}

	sel := Selection{Categories: []string{f.cat.ID}, Assets: &AssetsSelection{}}
	r := packageOf(t, src, blobs, sel, nil)
	if rows := r.rows(t, DataDir+"category_catalog_entries/"); len(rows) != 1 || rows[0]["part_number"] != "SC-100" || rows[0]["price_minor"] == nil {
		t.Errorf("the category's entries packaged: %v", rows)
	}
	if rows := r.rows(t, DataDir+"asset_catalog_entries/"); len(rows) != 1 || rows[0]["part_number"] != "SR-500" {
		t.Errorf("the asset's entries packaged: %v", rows)
	}
	// Without the assets, the asset's entry stays behind.
	r = packageOf(t, src, blobs, Selection{Categories: []string{f.cat.ID}}, nil)
	if rows := r.rows(t, DataDir+"asset_catalog_entries/"); len(rows) != 0 {
		t.Errorf("the asset's entries packaged without it: %v", rows)
	}
	noPrices := func(perm string, _ []string) bool { return perm != rbac.PermPurchasesRead }
	r = packageOf(t, src, blobs, sel, noPrices)
	for _, dir := range []string{"category_catalog_entries", "asset_catalog_entries"} {
		if rows := r.rows(t, DataDir+dir+"/"); len(rows) != 1 || rows[0]["price_minor"] != nil || rows[0]["currency"] != "" || rows[0]["price_checked"] != nil {
			t.Errorf("%s without purchases.read: %v", dir, rows)
		}
	}

	pkg := exportPackage(t, src, blobs, sel)
	into := migratedStore(t, envDialect())
	if _, err := Import(ctx, into, filestore.NewLocal(t.TempDir()), opener(pkg), ImportOptions{}); err != nil {
		t.Fatal(err)
	}
	entries, err := into.ListCatalogEntries(store.CatalogFilter{})
	if err != nil || len(entries) != 2 {
		t.Fatalf("imported: %+v %v", entries, err)
	}
	vendors, _ := into.ListVendors()
	for i, want := range []*models.CatalogEntry{f.screws, f.solderly} {
		got := entries[i]
		if !sameEntry(got, *want) || got.ID == want.ID || len(vendors) != 1 || got.VendorID != vendors[0].ID {
			t.Errorf("imported: %+v, want %+v", got, want)
		}
	}
	if entries[0].CategoryID == "" || entries[0].CategoryID == f.cat.ID || entries[1].AssetID == "" || entries[1].AssetID == f.solder.Asset.ID {
		t.Errorf("imported, naming: %+v", entries)
	}
}
