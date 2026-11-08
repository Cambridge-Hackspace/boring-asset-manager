package store

import (
	"errors"
	"strings"
	"testing"
)

// TestCatalogEntriesAreLinkedWithOnePreferred: entries are linked to a
// category or an asset, read back with prices exact, and choosing one as
// preferred makes it the only one for what it is for; the preferred list
// finds them, and history records each change on the vendor.
func TestCatalogEntriesAreLinkedWithOnePreferred(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	acme, err := st.CreateVendor(VendorInput{Name: "Acme"})
	if err != nil {
		t.Fatal(err)
	}
	bolts, err := st.CreateVendor(VendorInput{Name: "Bolts R Us"})
	if err != nil {
		t.Fatal(err)
	}
	screws, err := st.CreateCategory("Screws", nil, "", true, false)
	if err != nil {
		t.Fatal(err)
	}
	wire, err := st.CreateAsset("Solder wire", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	hundred := 100
	a, err := st.CreateCatalogEntry(CatalogEntryInput{VendorID: acme.ID, CategoryID: screws.ID, PartNumber: " SC-100 ", Description: "M3 screws",
		URL: "https://acme.example/sc-100", PackSize: &hundred, PackUnit: "screws", Price: "4.5", Currency: "gbp", PriceChecked: "2026-11-01", Preferred: true})
	if err != nil {
		t.Fatal(err)
	}
	if a.PartNumber != "SC-100" || a.Price != "4.50" || a.Currency != "GBP" || a.PriceChecked != "2026-11-01" || !a.Preferred ||
		a.VendorName != "Acme" || a.LinkedName != "Screws" || a.CategoryID != screws.ID || a.AssetID != "" || *a.PackSize != 100 {
		t.Errorf("made: %+v", a)
	}
	// Bolts R Us sells them too, and becomes preferred.
	b, err := st.CreateCatalogEntry(CatalogEntryInput{VendorID: bolts.ID, CategoryID: screws.ID, PartNumber: "B-3", Price: "500", Currency: "JPY", Preferred: true})
	if err != nil {
		t.Fatal(err)
	}
	if b.Price != "500" || !b.Preferred {
		t.Errorf("made: %+v", b)
	}
	if a, err = st.GetCatalogEntry(a.ID); err != nil || a.Preferred {
		t.Errorf("Acme's is still preferred: %+v %v", a, err)
	}
	// An asset's entry is preferred on its own.
	c, err := st.CreateCatalogEntry(CatalogEntryInput{VendorID: acme.ID, AssetID: wire.Asset.ID, Description: "Solder, 500 g", Preferred: true})
	if err != nil {
		t.Fatal(err)
	}
	if c.AssetID != wire.Asset.ID || c.LinkedName != "Solder wire" || c.PackSize != nil || c.Price != "" {
		t.Errorf("made: %+v", c)
	}
	preferred, err := st.ListCatalogEntries(CatalogFilter{Preferred: true})
	if err != nil || len(preferred) != 2 || preferred[0].ID != b.ID || preferred[1].ID != c.ID {
		t.Errorf("preferred: %+v %v", preferred, err)
	}
	if list, err := st.ListCatalogEntries(CatalogFilter{CategoryID: screws.ID}); err != nil || len(list) != 2 {
		t.Errorf("the category's: %+v %v", list, err)
	}
	if list, err := st.ListCatalogEntries(CatalogFilter{VendorID: acme.ID}); err != nil || len(list) != 2 {
		t.Errorf("Acme's: %+v %v", list, err)
	}
	if list, err := st.ListCatalogEntries(CatalogFilter{AssetID: wire.Asset.ID}); err != nil || len(list) != 1 || list[0].ID != c.ID {
		t.Errorf("the asset's: %+v %v", list, err)
	}

	// Choosing Acme's again; then moving it to the asset, keeping its ID.
	in := CatalogEntryInputOf(*a)
	in.Preferred = true
	if a, err = st.UpdateCatalogEntry(a.ID, in); err != nil || !a.Preferred {
		t.Fatalf("chosen: %+v %v", a, err)
	}
	if b, _ = st.GetCatalogEntry(b.ID); b.Preferred {
		t.Error("Bolts R Us's is still preferred")
	}
	in.CategoryID, in.AssetID, in.Preferred = "", wire.Asset.ID, false
	if a, err = st.UpdateCatalogEntry(a.ID, in); err != nil || a.AssetID != wire.Asset.ID || a.CategoryID != "" {
		t.Fatalf("moved: %+v %v", a, err)
	}
	if c, _ = st.GetCatalogEntry(c.ID); !c.Preferred {
		t.Error("an entry not preferred took the asset's preferred one away")
	}

	events, _, err := st.ListVendorEvents(acme.ID, "", 10)
	if err != nil || len(events) != 5 {
		t.Fatalf("Acme's history: %+v %v", events, err)
	}
	if got := changesOf(events[0]); got != "catalog:SC-100 (Screws)>SC-100 (Solder wire)" {
		t.Errorf("moved, in history: %s", got)
	}
	if got := changesOf(events[3]); got != "catalog:>SC-100 (Screws)" {
		t.Errorf("made, in history: %s", got)
	}

	if err := st.DeleteCatalogEntry(a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetCatalogEntry(a.ID); !errors.Is(err, ErrCatalogEntryNotFound) {
		t.Errorf("deleted, read: %v", err)
	}
	if err := st.DeleteCatalogEntry(a.ID); !errors.Is(err, ErrCatalogEntryNotFound) {
		t.Errorf("deleted twice: %v", err)
	}
	// Entries go with their vendor, and with what they are for; archiving
	// an asset keeps its.
	if err := st.DeleteVendor(bolts.ID); err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteAsset(wire.Asset.ID); err != nil {
		t.Fatal(err)
	}
	if list, err := st.ListCatalogEntries(CatalogFilter{}); err != nil || len(list) != 1 || list[0].ID != c.ID {
		t.Errorf("left: %+v %v", list, err)
	}
	if err := st.DeleteVendor(acme.ID); err != nil {
		t.Fatal(err)
	}
	if list, err := st.ListCatalogEntries(CatalogFilter{}); err != nil || len(list) != 0 {
		t.Errorf("left: %+v %v", list, err)
	}
}

// TestCatalogEntriesAreChecked refuses what an entry cannot be.
func TestCatalogEntriesAreChecked(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	v, err := st.CreateVendor(VendorInput{Name: "Acme"})
	if err != nil {
		t.Fatal(err)
	}
	cat, err := st.CreateCategory("Screws", nil, "", true, false)
	if err != nil {
		t.Fatal(err)
	}
	zero := 0
	for _, c := range []struct {
		in   CatalogEntryInput
		want string
	}{
		{CatalogEntryInput{CategoryID: cat.ID, PartNumber: "X"}, "needs its vendor"},
		{CatalogEntryInput{VendorID: v.ID, PartNumber: "X"}, "linked to a category or to an asset"},
		{CatalogEntryInput{VendorID: v.ID, CategoryID: cat.ID, AssetID: "a", PartNumber: "X"}, "not both"},
		{CatalogEntryInput{VendorID: v.ID, CategoryID: cat.ID}, "a part number or a description"},
		{CatalogEntryInput{VendorID: v.ID, CategoryID: cat.ID, PartNumber: "X", URL: "javascript:alert(1)"}, "http:// or https://"},
		{CatalogEntryInput{VendorID: v.ID, CategoryID: cat.ID, PartNumber: "X", PackSize: &zero}, "at least 1"},
		{CatalogEntryInput{VendorID: v.ID, CategoryID: cat.ID, PartNumber: "X", Price: "4.50"}, "needs its currency"},
		{CatalogEntryInput{VendorID: v.ID, CategoryID: cat.ID, PartNumber: "X", Price: "4.505", Currency: "GBP"}, "2 decimal places"},
		{CatalogEntryInput{VendorID: v.ID, CategoryID: cat.ID, PartNumber: "X", PriceChecked: "1/11/2026"}, "YYYY-MM-DD"},
		{CatalogEntryInput{VendorID: v.ID, CategoryID: cat.ID, PartNumber: strings.Repeat("x", 256)}, "longer than 255"},
	} {
		if _, err := st.CreateCatalogEntry(c.in); !errors.Is(err, ErrInvalidCatalogEntry) || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%+v: %v, want %q", c.in, err, c.want)
		}
	}
	for _, c := range []struct {
		in   CatalogEntryInput
		want error
	}{
		{CatalogEntryInput{VendorID: "nope", CategoryID: cat.ID, PartNumber: "X"}, ErrVendorNotFound},
		{CatalogEntryInput{VendorID: v.ID, CategoryID: "nope", PartNumber: "X"}, ErrCategoryNotFound},
		{CatalogEntryInput{VendorID: v.ID, AssetID: "nope", PartNumber: "X"}, ErrAssetNotFound},
	} {
		if _, err := st.CreateCatalogEntry(c.in); !errors.Is(err, c.want) {
			t.Errorf("%+v: %v, want %v", c.in, err, c.want)
		}
	}
	if _, err := st.UpdateCatalogEntry("nope", CatalogEntryInput{VendorID: v.ID, CategoryID: cat.ID, PartNumber: "X"}); !errors.Is(err, ErrCatalogEntryNotFound) {
		t.Errorf("updating none: %v", err)
	}
	if list, err := st.ListCatalogEntries(CatalogFilter{}); err != nil || len(list) != 0 {
		t.Errorf("kept: %+v %v", list, err)
	}
}

// TestCatalogWritesAreAllOrNone: one bad write of several keeps none.
func TestCatalogWritesAreAllOrNone(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	v, err := st.CreateVendor(VendorInput{Name: "Acme"})
	if err != nil {
		t.Fatal(err)
	}
	cat, err := st.CreateCategory("Screws", nil, "", true, false)
	if err != nil {
		t.Fatal(err)
	}
	_, err = st.WriteCatalogEntries([]CatalogWrite{
		{In: CatalogEntryInput{VendorID: v.ID, CategoryID: cat.ID, PartNumber: "A"}},
		{In: CatalogEntryInput{VendorID: v.ID, CategoryID: "nope", PartNumber: "B"}},
	})
	if !errors.Is(err, ErrCategoryNotFound) {
		t.Fatalf("written: %v", err)
	}
	if list, err := st.ListCatalogEntries(CatalogFilter{}); err != nil || len(list) != 0 {
		t.Errorf("kept: %+v %v", list, err)
	}
	ids, err := st.WriteCatalogEntries([]CatalogWrite{
		{In: CatalogEntryInput{VendorID: v.ID, CategoryID: cat.ID, PartNumber: "A"}},
		{In: CatalogEntryInput{VendorID: v.ID, CategoryID: cat.ID, PartNumber: "B", Preferred: true}},
	})
	if err != nil || len(ids) != 2 {
		t.Fatalf("written: %v %v", ids, err)
	}
	// One event for the vendor, with both.
	events, _, err := st.ListVendorEvents(v.ID, "", 10)
	if err != nil || len(events) != 2 || len(events[0].Payload.Changes) != 2 {
		t.Errorf("history: %+v %v", events, err)
	}
}
