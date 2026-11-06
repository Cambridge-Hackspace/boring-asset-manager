package store

import (
	"errors"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store/migrations"
)

// changesOf lists an event's changes as what:from>to.
func changesOf(e models.Event) string {
	var out []string
	for _, c := range e.Payload.Changes {
		out = append(out, c.What+":"+c.From+">"+c.To)
	}
	return strings.Join(out, " ")
}

// TestPurchaseDetailsSaveAndShow: an asset is made with where it came
// from, which reads back, with the price exactly; an edit replaces it, and
// its history says what changed; emptying every detail forgets them.
func TestPurchaseDetailsSaveAndShow(t *testing.T) {
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
	a, err := st.CreateAssetWithPurchase("Drill", nil, nil, nil, nil, "", &PurchaseInput{VendorID: acme.ID, OrderDate: "2026-01-05",
		ReceivedDate: " 2026-01-09 ", Price: "129.5", Currency: "gbp", OrderNumber: " PO-17 ", WarrantyEnd: "2028-01-09"})
	if err != nil {
		t.Fatal(err)
	}
	want := models.Purchase{VendorID: acme.ID, VendorName: "Acme", OrderDate: "2026-01-05", ReceivedDate: "2026-01-09", WarrantyEnd: "2028-01-09",
		Price: "129.50", Currency: "GBP", OrderNumber: "PO-17"}
	if a.Purchase != want {
		t.Errorf("made: %+v", a.Purchase)
	}
	listed, err := st.ListAssets("", "")
	if err != nil || len(listed) != 1 || listed[0].Purchase != want {
		t.Errorf("listed: %+v %v", listed, err)
	}
	events, _, err := st.ListAssetEvents(a.Asset.ID, "", 10)
	if err != nil || len(events) != 1 || !strings.Contains(changesOf(events[0]), "price:>129.50 GBP") || !strings.Contains(changesOf(events[0]), "vendor:>Acme") {
		t.Errorf("made, in its history: %+v %v", events, err)
	}

	// Another vendor and price; the dates left out are forgotten.
	a, err = st.UpdateAssetWithPurchase(a.Asset.ID, "", nil, nil, nil, nil, nil, &PurchaseInput{VendorID: bolts.ID, Price: "1300", Currency: "JPY", WarrantyEnd: "2028-01-09"})
	if err != nil {
		t.Fatal(err)
	}
	if p := a.Purchase; p.VendorName != "Bolts R Us" || p.Price != "1300" || p.Currency != "JPY" || p.OrderDate != "" || p.OrderNumber != "" || p.WarrantyEnd != "2028-01-09" {
		t.Errorf("edited: %+v", p)
	}
	events, _, _ = st.ListAssetEvents(a.Asset.ID, "", 10)
	if got := changesOf(events[0]); got != "vendor:Acme>Bolts R Us order_date:2026-01-05> received_date:2026-01-09> price:129.50 GBP>1300 JPY order_number:PO-17>" {
		t.Errorf("edited, in its history: %s", got)
	}

	// Nothing given changes nothing; an edit of something else keeps them.
	if a, err = st.UpdateAsset(a.Asset.ID, "Big drill", nil, nil, nil); err != nil || a.Purchase.Price != "1300" {
		t.Errorf("renamed: %+v %v", a.Purchase, err)
	}
	if a, err = st.UpdateAssetWithPurchase(a.Asset.ID, "", nil, nil, nil, nil, nil, &PurchaseInput{}); err != nil || !a.Purchase.IsZero() {
		t.Errorf("emptied: %+v %v", a.Purchase, err)
	}
	var rows int
	if err := st.QueryRow("SELECT COUNT(*) FROM asset_purchases").Scan(&rows); err != nil || rows != 0 {
		t.Errorf("rows kept for nothing: %d %v", rows, err)
	}
}

// TestPurchaseDetailsAreChecked: dates, prices, currencies and order
// numbers are checked, the vendor must exist, and the invoice must be one of
// the asset's own attachments; nothing is changed by a refusal.
func TestPurchaseDetailsAreChecked(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	a, err := st.CreateAsset("Drill", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	other, err := st.CreateAsset("Saw", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	theirs, err := st.AddAttachments(models.OwnerAsset, other.Asset.ID, []models.Attachment{{ID: "att-saw", Kind: models.AttachmentDocument, Filename: "saw.pdf", ContentType: "application/pdf", ByteSize: 3, CreatedAt: time.Now()}}, 10)
	if err != nil {
		t.Fatal(err)
	}
	for what, in := range map[string]PurchaseInput{
		"a date not YYYY-MM-DD":     {OrderDate: "5/1/2026"},
		"no such day":               {WarrantyEnd: "2026-02-30"},
		"a price without currency":  {Price: "12"},
		"a float's worth of places": {Price: "12.345", Currency: "GBP"},
		"a negative price":          {Price: "-1", Currency: "GBP"},
		"not a currency":            {Currency: "POUNDS"},
		"an order number too long":  {OrderNumber: strings.Repeat("x", 300)},
	} {
		if _, err := st.UpdateAssetWithPurchase(a.Asset.ID, "", nil, nil, nil, nil, nil, &in); !errors.Is(err, ErrInvalidPurchase) {
			t.Errorf("%s: %v", what, err)
		}
	}
	if _, err := st.UpdateAssetWithPurchase(a.Asset.ID, "", nil, nil, nil, nil, nil, &PurchaseInput{VendorID: "nope"}); !errors.Is(err, ErrVendorNotFound) {
		t.Errorf("an unknown vendor: %v", err)
	}
	if _, err := st.UpdateAssetWithPurchase(a.Asset.ID, "", nil, nil, nil, nil, nil, &PurchaseInput{InvoiceID: theirs[0].ID}); !errors.Is(err, ErrInvoiceNotAttached) {
		t.Errorf("another asset's attachment as the invoice: %v", err)
	}
	if _, err := st.CreateAssetWithPurchase("Lathe", nil, nil, nil, nil, "", &PurchaseInput{InvoiceID: theirs[0].ID}); !errors.Is(err, ErrInvoiceNotAttached) {
		t.Errorf("an invoice for an asset being made: %v", err)
	}
	if got, _ := st.GetAssetByID(a.Asset.ID); !got.Purchase.IsZero() {
		t.Errorf("changed by a refusal: %+v", got.Purchase)
	}
}

// TestTheInvoiceIsAnAttachment: an invoice is one of the asset's
// attachments; removing that attachment forgets it, in the history, and
// deleting the vendor forgets the vendor, keeping the rest.
func TestTheInvoiceIsAnAttachment(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	acme, err := st.CreateVendor(VendorInput{Name: "Acme"})
	if err != nil {
		t.Fatal(err)
	}
	a, err := st.CreateAssetWithPurchase("Drill", nil, nil, nil, nil, "", &PurchaseInput{VendorID: acme.ID, Price: "10", Currency: "EUR"})
	if err != nil {
		t.Fatal(err)
	}
	files, err := st.AddAttachments(models.OwnerAsset, a.Asset.ID, []models.Attachment{{ID: "att-invoice", Kind: models.AttachmentDocument, Name: "Invoice 17",
		Filename: "invoice.pdf", ContentType: "application/pdf", ByteSize: 3, CreatedAt: time.Now()}}, 10)
	if err != nil {
		t.Fatal(err)
	}
	in := PurchaseInputOf(a.Purchase)
	in.InvoiceID = files[0].ID
	if a, err = st.UpdateAssetWithPurchase(a.Asset.ID, "", nil, nil, nil, nil, nil, &in); err != nil || a.Purchase.InvoiceID != "att-invoice" || a.Purchase.InvoiceName != "Invoice 17" {
		t.Fatalf("with its invoice: %+v %v", a.Purchase, err)
	}
	if _, err := st.RemoveAttachment(models.OwnerAsset, a.Asset.ID, "att-invoice"); err != nil {
		t.Fatal(err)
	}
	a, _ = st.GetAssetByID(a.Asset.ID)
	if a.Purchase.InvoiceID != "" || a.Purchase.Price != "10.00" {
		t.Errorf("once the invoice is removed: %+v", a.Purchase)
	}
	events, _, _ := st.ListAssetEvents(a.Asset.ID, "", 10)
	if got := changesOf(events[0]); got != "attachment:Invoice 17> invoice:Invoice 17>" {
		t.Errorf("the removal, in its history: %s", got)
	}

	if err := st.DeleteVendor(acme.ID); err != nil {
		t.Fatal(err)
	}
	a, _ = st.GetAssetByID(a.Asset.ID)
	if a.Purchase.VendorID != "" || a.Purchase.Price != "10.00" {
		t.Errorf("once the vendor is deleted: %+v", a.Purchase)
	}
}

// TestAssetsBoughtFromAVendor: an asset list keeps those bought from a
// vendor.
func TestAssetsBoughtFromAVendor(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	acme, _ := st.CreateVendor(VendorInput{Name: "Acme"})
	bolts, _ := st.CreateVendor(VendorInput{Name: "Bolts R Us"})
	for name, vendor := range map[string]string{"Drill": acme.ID, "Saw": acme.ID, "Bolt": bolts.ID, "Rope": ""} {
		if _, err := st.CreateAssetWithPurchase(name, nil, nil, nil, nil, "", &PurchaseInput{VendorID: vendor}); err != nil {
			t.Fatal(err)
		}
	}
	got, err := st.FilterAssets(AssetFilter{VendorID: acme.ID})
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, a := range got {
		names = append(names, a.Asset.Name)
	}
	sort.Strings(names)
	if strings.Join(names, ", ") != "Drill, Saw" {
		t.Errorf("bought from Acme: %v", names)
	}
}

// TestWarrantyExpiringIsRecordedOnce: warranty.expiring is recorded the
// number of days before a warranty ends, not before, once for each end, and
// again for a new end; not for a warranty that had ended when first seen,
// nor for an archived asset.
func TestWarrantyExpiringIsRecordedOnce(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	day := func(s string) time.Time {
		d, err := time.Parse(DateLayout, s)
		if err != nil {
			t.Fatal(err)
		}
		return d.Add(9 * time.Hour)
	}
	drill, err := st.CreateAssetWithPurchase("Drill", nil, nil, nil, nil, "", &PurchaseInput{WarrantyEnd: "2027-03-31"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateAssetWithPurchase("Old saw", nil, nil, nil, nil, "", &PurchaseInput{WarrantyEnd: "2027-02-01"}); err != nil {
		t.Fatal(err)
	}
	gone, err := st.CreateAssetWithPurchase("Gone", nil, nil, nil, nil, "", &PurchaseInput{WarrantyEnd: "2027-03-20"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteAsset(gone.Asset.ID); err != nil {
		t.Fatal(err)
	}
	expiring := func() []models.Event {
		events, _, err := st.ListAssetEvents(drill.Asset.ID, "", 50)
		if err != nil {
			t.Fatal(err)
		}
		var out []models.Event
		for _, e := range events {
			if e.Kind == models.EventWarrantyExpiring {
				out = append(out, e)
			}
		}
		return out
	}
	for _, c := range []struct {
		now  string
		want int
	}{{"2027-02-28", 0}, {"2027-03-01", 1}, {"2027-03-02", 0}, {"2027-03-31", 0}} {
		n, err := st.RecordExpiringWarranties(day(c.now), 30)
		if err != nil || n != c.want {
			t.Errorf("on %s: recorded %d, %v; want %d", c.now, n, err, c.want)
		}
	}
	got := expiring()
	if len(got) != 1 || got[0].Payload.DueAt == nil || got[0].Payload.DueAt.Format(DateLayout) != "2027-03-31" || got[0].Payload.Asset == nil ||
		got[0].Payload.Asset.Name != "Drill" || got[0].Payload.Trigger != models.TriggerSchedule {
		t.Fatalf("recorded: %+v", got)
	}

	// A warranty extended is announced again, at its new lead time.
	if _, err := st.UpdateAssetWithPurchase(drill.Asset.ID, "", nil, nil, nil, nil, nil, &PurchaseInput{WarrantyEnd: "2028-03-31"}); err != nil {
		t.Fatal(err)
	}
	if n, _ := st.RecordExpiringWarranties(day("2027-04-01"), 30); n != 0 {
		t.Errorf("a year early: %d", n)
	}
	if n, _ := st.RecordExpiringWarranties(day("2028-03-01"), 30); n != 1 || len(expiring()) != 2 {
		t.Errorf("the new end: %d, %d in all", n, len(expiring()))
	}
	// On the day, with no lead time.
	if _, err := st.UpdateAssetWithPurchase(drill.Asset.ID, "", nil, nil, nil, nil, nil, &PurchaseInput{WarrantyEnd: "2028-06-30"}); err != nil {
		t.Fatal(err)
	}
	if n, _ := st.RecordExpiringWarranties(day("2028-06-29"), 0); n != 0 {
		t.Errorf("the day before, with no lead time: %d", n)
	}
	if n, _ := st.RecordExpiringWarranties(day("2028-06-30"), 0); n != 1 {
		t.Errorf("on the day, with no lead time: %d", n)
	}
}

// TestWarrantyExpiringOnceAcrossServers: workers polling at once, as
// servers sharing a database do, record a warranty's event once.
func TestWarrantyExpiringOnceAcrossServers(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	a, err := st.CreateAssetWithPurchase("Drill", nil, nil, nil, nil, "", &PurchaseInput{WarrantyEnd: "2027-03-31"})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2027, 3, 15, 9, 0, 0, 0, time.UTC)
	var (
		wg    sync.WaitGroup
		mu    sync.Mutex
		total int
	)
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			n, err := st.RecordExpiringWarranties(now, 30)
			if err != nil {
				t.Error(err)
			}
			mu.Lock()
			total += n
			mu.Unlock()
		}()
	}
	wg.Wait()
	var events int
	if err := st.QueryRow("SELECT COUNT(*) FROM events WHERE asset_id = ? AND kind = ?", a.Asset.ID, models.EventWarrantyExpiring).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if total != 1 || events != 1 {
		t.Errorf("recorded %d times, %d events", total, events)
	}
}

// TestPurchasesReadIsGrantedWhereAssetsAreRead: migration 070 grants
// purchases.read to every role that reads assets, where it reads them: in
// every scope, or in its scopes, the public role's among them.
func TestPurchasesReadIsGrantedWhereAssetsAreRead(t *testing.T) {
	st := openBlank(t)
	if err := migrations.MigrateTo(st.DB, st.Dialect, 69); err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		"INSERT INTO permissions (id, permission_key, description) SELECT 'p-read', 'assets.read', 'x' FROM (SELECT 1 AS one) AS seed WHERE NOT EXISTS (SELECT 1 FROM permissions WHERE permission_key = 'assets.read')",
		"INSERT INTO permissions (id, permission_key, description) SELECT 'p-write', 'assets.write', 'x' FROM (SELECT 1 AS one) AS seed WHERE NOT EXISTS (SELECT 1 FROM permissions WHERE permission_key = 'assets.write')",
		"INSERT INTO scopes (id, name, created_at) VALUES ('s-kitchen', 'kitchen', CURRENT_TIMESTAMP)",
		"INSERT INTO roles (id, name, description) VALUES ('r-reader', 'Reader', ''), ('r-kitchen', 'Kitchen', ''), ('r-writer', 'Writer', '')",
		"INSERT INTO role_permissions (role_id, permission_id) SELECT 'r-reader', id FROM permissions WHERE permission_key = 'assets.read'",
		"INSERT INTO role_permission_scopes (role_id, permission_id, scope_id) SELECT 'r-kitchen', id, 's-kitchen' FROM permissions WHERE permission_key = 'assets.read'",
		"INSERT INTO role_permissions (role_id, permission_id) SELECT 'r-writer', id FROM permissions WHERE permission_key = 'assets.write'",
	} {
		if _, err := st.Exec(stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	held := func(query string) []string {
		rows, err := st.Query(query)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		cols, _ := rows.Columns()
		var out []string
		for rows.Next() {
			vals := make([]string, len(cols))
			ptrs := make([]any, len(cols))
			for i := range vals {
				ptrs[i] = &vals[i]
			}
			if err := rows.Scan(ptrs...); err != nil {
				t.Fatal(err)
			}
			out = append(out, strings.Join(vals, ":"))
		}
		sort.Strings(out)
		return out
	}
	everywhere := held(`SELECT r.name FROM role_permissions rp JOIN roles r ON r.id = rp.role_id JOIN permissions p ON p.id = rp.permission_id
		WHERE p.permission_key = 'purchases.read'`)
	scoped := held(`SELECT r.name, s.name FROM role_permission_scopes rps JOIN roles r ON r.id = rps.role_id
		JOIN permissions p ON p.id = rps.permission_id JOIN scopes s ON s.id = rps.scope_id WHERE p.permission_key = 'purchases.read'`)
	if strings.Join(everywhere, ",") != "Reader" {
		t.Errorf("held in every scope by %v", everywhere)
	}
	if strings.Join(scoped, ",") != "Kitchen:kitchen,public:public" {
		t.Errorf("held in some scopes by %v", scoped)
	}
}
