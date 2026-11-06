package archive

import (
	"bytes"
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

// vendorFixture is a vendor with two contacts, in a scope, with a
// document, named by a category.
type vendorFixture struct {
	vendor *models.Vendor
	scope  *models.Scope
	cat    *models.Category
	doc    models.Attachment
}

func seedVendor(t *testing.T, st *store.Store, blobs filestore.Blobs, by string) vendorFixture {
	t.Helper()
	var f vendorFixture
	var err error
	if f.scope, err = st.CreateScope("suppliers"); err != nil {
		t.Fatal(err)
	}
	if f.vendor, err = st.As(by).CreateVendor(store.VendorInput{Name: "Acme Fasteners", Website: "https://acme.example/reorder", AccountNumber: "AC-1001",
		Address: "1 Bolt Lane\nCambridge", Notes: "**Net 30.** Ask for *trade* prices.",
		Contacts: []models.VendorContact{{Name: "Wile", Role: "sales", Email: "wile@acme.example", Phone: "+44 1223 000000"}, {Name: "Road", Role: "support", Email: "road@acme.example"}},
		ScopeIDs: []string{f.scope.ID}}); err != nil {
		t.Fatal(err)
	}
	if f.cat, err = st.CreateCategoryFrom(store.CategoryInput{Name: "Screws", IsConsumable: true}); err != nil {
		t.Fatal(err)
	}
	if f.cat, err = st.SetCategoryVendors(f.cat.ID, []string{f.vendor.ID}); err != nil {
		t.Fatal(err)
	}
	f.doc = models.Attachment{ID: uuid.NewString(), Kind: models.AttachmentDocument, Name: "price list.pdf", Description: "2026 prices", Filename: "price list.pdf",
		ContentType: "application/pdf", ByteSize: 13, CreatedAt: time.Now()}
	if err := blobs.Put(context.Background(), "photos/"+f.doc.ID, strings.NewReader("%PDF-prices.."), -1, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := st.As(by).AddAttachments(models.OwnerVendor, f.vendor.ID, []models.Attachment{f.doc}, store.DefaultMaxAttachments); err != nil {
		t.Fatal(err)
	}
	return f
}

// TestBackupsCarryVendors: a backup holds vendors, their contacts, scopes,
// files and history, and the categories naming them, and a restore puts
// them back as they were.
func TestBackupsCarryVendors(t *testing.T) {
	src := setupStore(t)
	blobs := filestore.NewLocal(t.TempDir())
	s := seed(t, src, blobs)
	f := seedVendor(t, src, blobs, s.user.ID)

	b := backup(t, src, blobs, Options{})
	r := readArchive(t, b)
	for dir, want := range map[string]int{"vendors": 1, "vendor_contacts": 2, "vendor_scopes": 1, "vendor_attachments": 1, "category_vendors": 1} {
		if got := len(r.rows(t, DataDir+dir+"/")); got != want {
			t.Errorf("%s: %d rows, want %d", dir, got, want)
		}
	}
	if _, ok := r.members[FilesDir+"photos/"+f.doc.ID]; !ok {
		t.Error("the vendor's file is not held")
	}

	dst := emptyStore(t, envDialect())
	dstBlobs := filestore.NewLocal(t.TempDir())
	if _, err := Restore(context.Background(), dst, dstBlobs, opener(b), RestoreOptions{}); err != nil {
		t.Fatal(err)
	}
	got, err := dst.GetVendor(f.vendor.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != f.vendor.Name || got.Notes != f.vendor.Notes || got.Address != f.vendor.Address || len(got.Contacts) != 2 ||
		got.Contacts[0] != f.vendor.Contacts[0] || got.Contacts[1] != f.vendor.Contacts[1] || len(got.ScopeIDs) != 1 || got.ScopeIDs[0] != f.scope.ID {
		t.Errorf("restored: %+v, want %+v", got, f.vendor)
	}
	if ids, err := dst.CategoryVendorIDs(f.cat.ID); err != nil || len(ids) != 1 || ids[0] != f.vendor.ID {
		t.Errorf("the category's vendors, restored: %+v %v", ids, err)
	}
	files, err := dst.ListAttachments(models.OwnerVendor, f.vendor.ID)
	if err != nil || len(files) != 1 || files[0].Description != "2026 prices" {
		t.Errorf("its files, restored: %+v %v", files, err)
	}
	if rc, err := dstBlobs.Get(context.Background(), "photos/"+f.doc.ID); err != nil {
		t.Errorf("its file: %v", err)
	} else {
		rc.Close()
	}
	events, _, err := dst.ListVendorEvents(f.vendor.ID, "", 10)
	if err != nil || len(events) != 2 {
		t.Errorf("its history, restored: %+v %v", events, err)
	}

	// Without documents, its document stays behind.
	r = readArchive(t, backup(t, src, blobs, Options{NoDocuments: true}))
	if got := r.rows(t, DataDir+"vendor_attachments/"); len(got) != 0 {
		t.Errorf("without documents: %v", got)
	}
}

// TestPackagesCarryVendors: a category brings the vendors it is bought
// from, with their contacts and files; imported, they are created, and
// imported again, the same vendors are used as they are.
func TestPackagesCarryVendors(t *testing.T) {
	ctx := context.Background()
	src := setupStore(t)
	blobs := filestore.NewLocal(t.TempDir())
	s := seed(t, src, blobs)
	f := seedVendor(t, src, blobs, s.user.ID)

	plan, err := Resolve(ctx, src, src.DB, Selection{Categories: []string{f.cat.ID}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, it := range plan.Items {
		if it.Kind == ItemVendor && it.ID == f.vendor.ID {
			found = len(it.NeededBy) == 1 && it.NeededBy[0].ID == f.cat.ID && !it.Chosen
		}
	}
	if !found {
		t.Errorf("the category's vendor is not needed by it: %+v", plan.Items)
	}

	r := packageOf(t, src, blobs, Selection{Categories: []string{f.cat.ID}}, nil)
	if got := ids(t, r, "vendors", "id"); len(got) != 1 || got[0] != f.vendor.ID {
		t.Errorf("vendors packaged: %v", got)
	}
	if got := r.rows(t, DataDir+"vendor_contacts/"); len(got) != 2 {
		t.Errorf("contacts packaged: %v", got)
	}
	for _, row := range r.rows(t, DataDir+"vendor_attachments/") {
		if row["added_by"] != nil {
			t.Errorf("who added a file is packaged: %v", row)
		}
	}
	// Its scope is not chosen, so the vendor goes without it.
	if got := r.rows(t, DataDir+"vendor_scopes/"); len(got) != 0 {
		t.Errorf("scopes packaged without being chosen: %v", got)
	}

	pkg := exportPackage(t, src, blobs, Selection{Categories: []string{f.cat.ID}, Scopes: []string{f.scope.ID}})
	into := migratedStore(t, envDialect())
	intoBlobs := filestore.NewLocal(t.TempDir())
	if _, err := Import(ctx, into, intoBlobs, opener(pkg), ImportOptions{}); err != nil {
		t.Fatal(err)
	}
	vs, err := into.ListVendors()
	if err != nil || len(vs) != 1 {
		t.Fatalf("imported: %+v %v", vs, err)
	}
	v := vs[0]
	if v.ID == f.vendor.ID || v.Name != "Acme Fasteners" || len(v.Contacts) != 2 || v.Contacts[0].Email != "wile@acme.example" || len(v.ScopeIDs) != 1 {
		t.Errorf("imported: %+v", v)
	}
	files, err := into.ListAttachments(models.OwnerVendor, v.ID)
	if err != nil || len(files) != 1 || files[0].ID == f.doc.ID || files[0].AddedBy != "" {
		t.Errorf("its files, imported: %+v %v", files, err)
	} else if rc, err := intoBlobs.Get(ctx, "photos/"+files[0].ID); err != nil {
		t.Errorf("its file: %v", err)
	} else {
		rc.Close()
	}
	var catID string
	if err := into.QueryRow("SELECT id FROM categories WHERE name = 'Screws'").Scan(&catID); err != nil {
		t.Fatal(err)
	}
	if ids, err := into.CategoryVendorIDs(catID); err != nil || len(ids) != 1 || ids[0] != v.ID {
		t.Errorf("the category's vendors, imported: %+v %v", ids, err)
	}

	// Again: the vendor here is the same, and used.
	check, err := CheckImport(ctx, into, opener(pkg), ImportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	it := itemOf(t, check, ItemVendor, "Acme Fasteners")
	if it.Action != ChoiceExisting || !it.Identical || it.Existing == nil || it.Existing.ID != v.ID {
		t.Errorf("imported again: %+v", it)
	}
	if _, err := Import(ctx, into, intoBlobs, opener(pkg), ImportOptions{}); err != nil {
		t.Fatal(err)
	}
	if n := count(t, into, "vendors"); n != 1 {
		t.Errorf("%d vendors after importing twice", n)
	}
	if n := countWhere(t, into, "SELECT COUNT(*) FROM category_vendors WHERE vendor_id = '"+v.ID+"'"); n != 2 {
		t.Errorf("%d categories name the vendor after importing twice", n)
	}
}

// TestAScopedReaderPackagesTheVendorsItSees: a vendor in a scope the
// requester cannot read stays behind, listed, and choosing it is refused.
func TestAScopedReaderPackagesTheVendorsItSees(t *testing.T) {
	ctx := context.Background()
	src := setupStore(t)
	blobs := filestore.NewLocal(t.TempDir())
	s := seed(t, src, blobs)
	f := seedVendor(t, src, blobs, s.user.ID)
	elsewhere := func(perm string, scopes []string) bool {
		if scopes == nil {
			return perm == rbac.PermAssetsRead
		}
		for _, sc := range scopes {
			if sc == "tools" && perm == rbac.PermAssetsRead {
				return true
			}
		}
		return false
	}
	plan, err := Resolve(ctx, src, src.DB, Selection{Categories: []string{f.cat.ID}}, elsewhere)
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range plan.Items {
		if it.Kind == ItemVendor {
			t.Errorf("a vendor it cannot see is packaged: %+v", it)
		}
	}
	left := false
	for _, l := range plan.LeftOut {
		left = left || l.Kind == ItemVendor && l.ID == f.vendor.ID
	}
	if !left {
		t.Errorf("the vendor is not listed as left out: %+v", plan.LeftOut)
	}
	if _, err := Resolve(ctx, src, src.DB, Selection{Vendors: []string{f.vendor.ID}}, elsewhere); err == nil {
		t.Error("choosing a vendor it cannot see was not refused")
	}
	// In the vendor's scope, it may.
	inScope := func(perm string, scopes []string) bool {
		if scopes == nil {
			return perm == rbac.PermAssetsRead
		}
		for _, sc := range scopes {
			if sc == "suppliers" && perm == rbac.PermAssetsRead {
				return true
			}
		}
		return false
	}
	if _, err := Resolve(ctx, src, src.DB, Selection{Vendors: []string{f.vendor.ID}}, inScope); err != nil {
		t.Errorf("choosing a vendor it can see: %v", err)
	}
}

// beforeVendors rewrites an archive as one written before vendors
// (migration 068): with none of their tables, and events without
// vendor_id.
func beforeVendors(t *testing.T, b []byte) []byte {
	t.Helper()
	gone := map[string]bool{}
	for table, since := range tablesSince {
		if since == VendorsVersion {
			gone[table] = true
		}
	}
	var out []member
	for _, m := range unpack(t, b) {
		if strings.HasPrefix(m.name, DataDir) {
			table := strings.TrimPrefix(m.name, DataDir)
			table = table[:strings.Index(table, "/")]
			if gone[table] {
				continue
			}
			if table == "events" {
				var buf bytes.Buffer
				for _, line := range strings.Split(strings.TrimSpace(string(m.data)), "\n") {
					var row map[string]any
					if err := json.Unmarshal([]byte(line), &row); err != nil {
						t.Fatal(err)
					}
					if row["vendor_id"] != nil {
						continue
					}
					delete(row, "vendor_id")
					data, _ := json.Marshal(row)
					buf.Write(append(data, '\n'))
				}
				m.data = buf.Bytes()
			}
		}
		out = append(out, m)
	}
	return pack(t, reseal(t, out, func(h *Header, man *Manifest) {
		h.SchemaVersion = VendorsVersion - 1
		for table := range gone {
			delete(man.Tables, table)
		}
		if _, ok := man.Tables["events"]; ok {
			man.Tables["events"] = 0
			for _, m := range out {
				if strings.HasPrefix(m.name, DataDir+"events/") {
					man.Tables["events"] += strings.Count(string(m.data), "\n")
				}
			}
		}
		man.Header.SchemaVersion = h.SchemaVersion
	}))
}

// TestArchivesFromBeforeVendors: a backup or a package written before
// vendors still checks, restores and imports.
func TestArchivesFromBeforeVendors(t *testing.T) {
	ctx := context.Background()
	src := setupStore(t)
	blobs := filestore.NewLocal(t.TempDir())
	s := seed(t, src, blobs)
	f := seedVendor(t, src, blobs, s.user.ID)

	old := beforeVendors(t, backup(t, src, blobs, Options{}))
	if _, err := Check(bytes.NewReader(old)); err != nil {
		t.Fatalf("an archive from before vendors: %v", err)
	}
	dst := emptyStore(t, envDialect())
	sum, err := Restore(ctx, dst, filestore.NewLocal(t.TempDir()), opener(old), RestoreOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !sum.Migrated {
		t.Errorf("not migrated: %+v", sum)
	}
	if n := count(t, dst, "vendors"); n != 0 {
		t.Errorf("%d vendors", n)
	}
	if _, err := dst.GetCategoryByID(f.cat.ID); err != nil {
		t.Errorf("the category: %v", err)
	}

	pkg := beforeVendors(t, exportPackage(t, src, blobs, Selection{Categories: []string{f.cat.ID}}))
	into := migratedStore(t, envDialect())
	if _, err := Import(ctx, into, filestore.NewLocal(t.TempDir()), opener(pkg), ImportOptions{}); err != nil {
		t.Fatal(err)
	}
	if n := count(t, into, "vendors"); n != 0 {
		t.Errorf("%d vendors imported", n)
	}
	if n := countWhere(t, into, "SELECT COUNT(*) FROM categories WHERE name = 'Screws'"); n != 1 {
		t.Errorf("%d categories imported", n)
	}
}
