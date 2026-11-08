package store

import (
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/generators"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
)

// orderFixture is a vendor with a catalog: screws kept by quantity in a
// bin, and spools of filament kept by items, in a category that weighs
// them.
type orderFixture struct {
	st                    *Store
	acme                  *models.Vendor
	bin                   *models.AssetWithDetails
	filament              *models.Category
	screwEntry, pla       *models.CatalogEntry
	threshold, binLow     int
	shelf                 *models.AssetWithDetails
	location              *models.RelationType
	spool                 *models.AssetWithDetails
	spoolsWithoutCatalogs *models.Category
}

func newOrderFixture(t *testing.T) *orderFixture {
	t.Helper()
	st := setupTestStore(t)
	t.Cleanup(func() { st.Close() })
	f := &orderFixture{st: st, threshold: 3000, binLow: 200}
	var err error
	if f.acme, err = st.CreateVendor(VendorInput{Name: "Acme"}); err != nil {
		t.Fatal(err)
	}
	if f.filament, err = st.CreateCategoryFrom(CategoryInput{Name: "Filament", ColorHex: "#123456", IsConsumable: true, Unit: "g", LowStockThreshold: &f.threshold}); err != nil {
		t.Fatal(err)
	}
	if f.spoolsWithoutCatalogs, err = st.CreateCategoryFrom(CategoryInput{Name: "Saw blades", ColorHex: "#123456", IsConsumable: true}); err != nil {
		t.Fatal(err)
	}
	q := 500
	if f.bin, err = st.CreateAssetWithStock("M5 bolts", nil, nil, nil, &StockInput{Quantity: &q, Unit: "pcs", LowStockThreshold: &f.binLow}); err != nil {
		t.Fatal(err)
	}
	hundred, kilo := 100, 1000
	if f.screwEntry, err = st.CreateCatalogEntry(CatalogEntryInput{VendorID: f.acme.ID, AssetID: f.bin.Asset.ID, PartNumber: "M5-100", Description: "M5 bolts",
		PackSize: &hundred, PackUnit: "pcs", Price: "4.50", Currency: "GBP", Preferred: true}); err != nil {
		t.Fatal(err)
	}
	if f.pla, err = st.CreateCatalogEntry(CatalogEntryInput{VendorID: f.acme.ID, CategoryID: f.filament.ID, PartNumber: "PLA-1K", Description: "PLA spool",
		PackSize: &kilo, PackUnit: "g", Price: "20.00", Currency: "GBP", Preferred: true}); err != nil {
		t.Fatal(err)
	}
	types, err := st.ListRelationTypes()
	if err != nil {
		t.Fatal(err)
	}
	for i := range types {
		if types[i].Name == "contains" {
			f.location = &types[i]
		}
	}
	if f.location == nil || !f.location.IsSingle {
		t.Fatalf("no single contains type: %+v", types)
	}
	if f.shelf, err = st.CreateAsset("Shelf A", nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	spoolQ := 1000
	if f.spool, err = st.CreateAssetWithStock("PLA spool", []string{f.filament.ID}, []models.AssetGeneratorKey{{GeneratorType: generators.TypeMicroQR, AssignedCode: "SP-014"}},
		nil, &StockInput{Quantity: &spoolQ, Unit: "g"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddLink(f.spool.Asset.ID, f.location.ID, f.shelf.Asset.ID, false); err != nil {
		t.Fatal(err)
	}
	return f
}

func str(s string) *string { return &s }

// TestPurchaseOrdersNumberCopyAndPrice: orders are numbered one after
// another, PO-0001 on; a line drawn from a catalog entry takes its part
// number, pack, category or asset, and price, and gives a new order its
// currency; a free line keeps what it is given; totals are exact; a new
// order is delivered where the last was; and a line names what it is for.
func TestPurchaseOrdersNumberCopyAndPrice(t *testing.T) {
	f := newOrderFixture(t)
	st := f.st
	o, err := st.CreatePurchaseOrder(OrderInput{VendorID: f.acme.ID, DeliverTo: " The Hackspace ", Lines: []OrderLineInput{
		{CatalogEntryID: f.screwEntry.ID, Quantity: 3},
		{CatalogEntryID: f.pla.ID, Quantity: 2, Price: str("19.99")},
		{Description: "Delivery", Quantity: 1, Price: str("5")},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if o.Number != 1 || o.Reference != "PO-0001" || o.State != models.OrderDraft || o.Currency != "GBP" || o.DeliverTo != "The Hackspace" {
		t.Fatalf("order %+v", o)
	}
	if len(o.Lines) != 3 {
		t.Fatalf("lines %+v", o.Lines)
	}
	screws, spools, delivery := o.Lines[0], o.Lines[1], o.Lines[2]
	if screws.PartNumber != "M5-100" || screws.AssetID != f.bin.Asset.ID || screws.CatalogEntryID != f.screwEntry.ID || *screws.PackSize != 100 ||
		screws.UnitPrice != "4.50" || screws.Total != "13.50" || screws.LinkedName != "M5 bolts" {
		t.Errorf("screws %+v", screws)
	}
	if spools.CategoryID != f.filament.ID || spools.UnitPrice != "19.99" || spools.Total != "39.98" || spools.LinkedName != "Filament" {
		t.Errorf("spools %+v", spools)
	}
	if delivery.CatalogEntryID != "" || delivery.CategoryID != "" || delivery.AssetID != "" || delivery.Total != "5.00" {
		t.Errorf("delivery %+v", delivery)
	}
	if o.Total != "58.48" {
		t.Errorf("total %q", o.Total)
	}
	next, err := st.CreatePurchaseOrder(OrderInput{VendorID: f.acme.ID})
	if err != nil {
		t.Fatal(err)
	}
	if next.Reference != "PO-0002" || next.DeliverTo != "The Hackspace" || next.Currency != "" {
		t.Errorf("next %+v", next)
	}
	if OrderReference(12345) != "PO-12345" {
		t.Error(OrderReference(12345))
	}

	// Another currency's entry comes without its price.
	usd, err := st.CreatePurchaseOrder(OrderInput{VendorID: f.acme.ID, Currency: "usd", Lines: []OrderLineInput{{CatalogEntryID: f.screwEntry.ID, Quantity: 1}}})
	if err != nil {
		t.Fatal(err)
	}
	if usd.Currency != "USD" || usd.Lines[0].UnitPrice != "" || usd.Total != "" {
		t.Errorf("usd %+v", usd)
	}

	for _, bad := range []OrderInput{
		{},
		{VendorID: f.acme.ID, Lines: []OrderLineInput{{Quantity: 1}}},
		{VendorID: f.acme.ID, Lines: []OrderLineInput{{Description: "x", Quantity: 0}}},
		{VendorID: f.acme.ID, Lines: []OrderLineInput{{Description: "x", Quantity: 1, Price: str("1")}}},
		{VendorID: f.acme.ID, Currency: "GBP", Lines: []OrderLineInput{{Description: "x", Quantity: 1, Price: str("1.001")}}},
		{VendorID: f.acme.ID, Currency: "GBP", Lines: []OrderLineInput{{Description: "x", Quantity: 1000000, Price: str("999999999999")}}},
		{VendorID: f.acme.ID, Lines: []OrderLineInput{{Description: "x", Quantity: 1, CategoryID: f.filament.ID, AssetID: f.bin.Asset.ID}}},
		{VendorID: f.acme.ID, Lines: []OrderLineInput{{CatalogEntryID: f.pla.ID, AssetID: f.bin.Asset.ID, Quantity: 1}}},
		{VendorID: f.acme.ID, ExpectedOn: "tomorrow"},
	} {
		if _, err := st.CreatePurchaseOrder(bad); !errors.Is(err, ErrInvalidOrder) {
			t.Errorf("%+v: %v", bad, err)
		}
	}
	other, err := st.CreateVendor(VendorInput{Name: "Other"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreatePurchaseOrder(OrderInput{VendorID: other.ID, Lines: []OrderLineInput{{CatalogEntryID: f.pla.ID, Quantity: 1}}}); !errors.Is(err, ErrInvalidOrder) {
		t.Errorf("another vendor's entry: %v", err)
	}
	if err := st.DeleteVendor(f.acme.ID); !errors.Is(err, ErrVendorHasOrders) {
		t.Errorf("deleting the vendor: %v", err)
	}
}

// TestPurchaseOrderNumbersAreUnique: orders made at once are numbered
// apart.
func TestPurchaseOrderNumbersAreUnique(t *testing.T) {
	f := newOrderFixture(t)
	var wg sync.WaitGroup
	numbers := make([]int, 6)
	errs := make([]error, 6)
	for i := range numbers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			o, err := f.st.CreatePurchaseOrder(OrderInput{VendorID: f.acme.ID})
			if err == nil {
				numbers[i] = o.Number
			}
			errs[i] = err
		}()
	}
	wg.Wait()
	seen := map[int]bool{}
	for i, n := range numbers {
		if errs[i] != nil {
			t.Fatal(errs[i])
		}
		if seen[n] {
			t.Fatalf("numbers %v", numbers)
		}
		seen[n] = true
	}
}

// TestPurchaseOrderStates: a draft changes freely and is placed; a placed
// order keeps its vendor; it is received in parts, a line received
// keeping at least what came; a received order changes only its reference
// and notes; a draft is not canceled but a placed order is; and each
// change is in its history, prices apart.
func TestPurchaseOrderStates(t *testing.T) {
	f := newOrderFixture(t)
	st := f.st
	o, err := st.CreatePurchaseOrder(OrderInput{VendorID: f.acme.ID, Lines: []OrderLineInput{{CatalogEntryID: f.screwEntry.ID, Quantity: 4}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CancelPurchaseOrder(o.ID); !isStateError(err) {
		t.Errorf("canceling a draft: %v", err)
	}
	empty, err := st.CreatePurchaseOrder(OrderInput{VendorID: f.acme.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.PlacePurchaseOrder(empty.ID, ""); !isStateError(err) {
		t.Errorf("placing an empty order: %v", err)
	}
	in := OrderInputOf(*o)
	in.Lines[0].Quantity, in.Lines[0].Price, in.ExpectedOn, in.Notes = 5, str("4.25"), "2026-12-01", "Ring first"
	if o, err = st.UpdatePurchaseOrder(o.ID, in); err != nil {
		t.Fatal(err)
	}
	if o.Lines[0].Quantity != 5 || o.Lines[0].UnitPrice != "4.25" {
		t.Fatalf("edited %+v", o.Lines)
	}
	if o, err = st.PlacePurchaseOrder(o.ID, "2026-11-08"); err != nil {
		t.Fatal(err)
	}
	if o.State != models.OrderOrdered || o.OrderedOn != "2026-11-08" {
		t.Fatalf("placed %+v", o)
	}
	if _, err := st.PlacePurchaseOrder(o.ID, ""); !isStateError(err) {
		t.Errorf("placing twice: %v", err)
	}
	other, err := st.CreateVendor(VendorInput{Name: "Other"})
	if err != nil {
		t.Fatal(err)
	}
	moved := OrderInputOf(*o)
	moved.VendorID = other.ID
	if _, err := st.UpdatePurchaseOrder(o.ID, moved); !isStateError(err) {
		t.Errorf("moving a placed order: %v", err)
	}
	// A line added after placing, and received in part.
	in = OrderInputOf(*o)
	in.Lines = append(in.Lines, OrderLineInput{Description: "Washers", Quantity: 2})
	if o, err = st.UpdatePurchaseOrder(o.ID, in); err != nil {
		t.Fatal(err)
	}
	r, err := st.ReceivePurchaseOrder(o.ID, ReceiptInput{ReceivedOn: "2026-11-10", Lines: []ReceiptLine{{LineID: o.Lines[0].ID, Quantity: 2}}})
	if err != nil {
		t.Fatal(err)
	}
	o = r.Order
	if o.State != models.OrderPartlyReceived || o.Lines[0].QuantityReceived != 2 || o.ReceivedOn != "" {
		t.Fatalf("partly %+v", o)
	}
	fewer := OrderInputOf(*o)
	fewer.Lines[0].Quantity = 1
	if _, err := st.UpdatePurchaseOrder(o.ID, fewer); !isStateError(err) {
		t.Errorf("ordering fewer than came: %v", err)
	}
	fewer.Lines = fewer.Lines[1:]
	if _, err := st.UpdatePurchaseOrder(o.ID, fewer); !isStateError(err) {
		t.Errorf("removing a line received: %v", err)
	}
	if _, err := st.ReceivePurchaseOrder(o.ID, ReceiptInput{Lines: []ReceiptLine{{LineID: o.Lines[0].ID, Quantity: 4}}}); !isStateError(err) {
		t.Errorf("receiving more than outstanding: %v", err)
	}
	// Ordering only what came, and dropping the washers, receives it.
	in = OrderInputOf(*o)
	in.Lines = in.Lines[:1]
	in.Lines[0].Quantity = 2
	if o, err = st.UpdatePurchaseOrder(o.ID, in); err != nil {
		t.Fatal(err)
	}
	if o.State != models.OrderReceived || o.ReceivedOn == "" {
		t.Fatalf("received %+v", o)
	}
	in = OrderInputOf(*o)
	in.VendorReference, in.Notes = "INV-9", "Paid"
	if o, err = st.UpdatePurchaseOrder(o.ID, in); err != nil {
		t.Fatal(err)
	}
	in = OrderInputOf(*o)
	in.ExpectedOn = "2027-01-01"
	if _, err := st.UpdatePurchaseOrder(o.ID, in); !isStateError(err) {
		t.Errorf("changing a received order's dates: %v", err)
	}
	if _, err := st.CancelPurchaseOrder(o.ID); !isStateError(err) {
		t.Errorf("canceling a received order: %v", err)
	}

	// A placed order is canceled, and then only its notes change.
	c, err := st.CreatePurchaseOrder(OrderInput{VendorID: f.acme.ID, Lines: []OrderLineInput{{Description: "Glue", Quantity: 1}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.PlacePurchaseOrder(c.ID, ""); err != nil {
		t.Fatal(err)
	}
	if c, err = st.CancelPurchaseOrder(c.ID); err != nil || c.State != models.OrderCanceled {
		t.Fatalf("canceled %+v %v", c, err)
	}
	if _, err := st.ReceivePurchaseOrder(c.ID, ReceiptInput{Lines: []ReceiptLine{{LineID: c.Lines[0].ID, Quantity: 1}}}); !isStateError(err) {
		t.Errorf("receiving a canceled order: %v", err)
	}

	events, _, err := st.ListOrderEvents(o.ID, "", 50)
	if err != nil {
		t.Fatal(err)
	}
	var kinds []string
	var price bool
	for _, e := range events {
		kinds = append(kinds, e.Kind)
		if e.Payload.PurchaseOrder == nil || e.Payload.PurchaseOrder.Number != "PO-0001" || e.AssetID != nil {
			t.Errorf("event %+v", e)
		}
		for _, c := range e.Payload.Changes {
			if c.What == "price" && c.From == "4.50 GBP" && c.To == "4.25 GBP" {
				price = true
			}
			if c.What == "line" && strings.Contains(c.From+c.To, "4.") {
				t.Errorf("a line's change shows its price: %+v", c)
			}
		}
	}
	want := "purchase_order.edited purchase_order.edited purchase_order.received purchase_order.edited purchase_order.ordered purchase_order.edited purchase_order.created"
	if strings.Join(kinds, " ") != want {
		t.Errorf("history %v", kinds)
	}
	if !price {
		t.Error("the price change is not in the history")
	}
	if err := st.DeletePurchaseOrder(o.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetPurchaseOrder(o.ID); !errors.Is(err, ErrOrderNotFound) {
		t.Errorf("deleted: %v", err)
	}
}

func isStateError(err error) bool {
	var se *OrderStateError
	return errors.As(err, &se)
}

// TestReceivingAPurchaseOrder: one receipt restocks the bin by its packs,
// lifting it back over its threshold once, and makes spools like one on
// the shelf, with codes in series, the shelf, stock in the category's
// unit, and the order's purchase details, the price shared and rounded
// half up; each change names the order; and a receipt failing on one line
// changes nothing.
func TestReceivingAPurchaseOrder(t *testing.T) {
	f := newOrderFixture(t)
	st := f.st
	// The bin runs low.
	if _, err := st.AdjustQuantity(f.bin.Asset.ID, -400, nil); err != nil {
		t.Fatal(err)
	}
	o, err := st.CreatePurchaseOrder(OrderInput{VendorID: f.acme.ID, Lines: []OrderLineInput{
		{CatalogEntryID: f.screwEntry.ID, Quantity: 3},
		{CatalogEntryID: f.pla.ID, Quantity: 2, Price: str("20.00")},
		{Description: "Delivery", Quantity: 1},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if o, err = st.PlacePurchaseOrder(o.ID, "2026-11-01"); err != nil {
		t.Fatal(err)
	}
	screws, spools, delivery := o.Lines[0], o.Lines[1], o.Lines[2]
	like := &AssetTemplate{Name: "PLA spool", CategoryIDs: []string{f.filament.ID}, Stock: &StockInput{Unit: "g"},
		Codes: []models.AssetGeneratorKey{{GeneratorType: generators.TypeMicroQR, AssignedCode: "SP-014"}},
		Links: []TemplateLink{{TypeID: f.location.ID, OtherID: f.shelf.Asset.ID, Outgoing: false}}}

	// One bad line, and nothing changes.
	three := 3
	if _, err := st.ReceivePurchaseOrder(o.ID, ReceiptInput{Lines: []ReceiptLine{
		{LineID: screws.ID, Quantity: 3},
		{LineID: spools.ID, Quantity: 2, Assets: &three, Like: like},
	}}); !errors.Is(err, ErrInvalidOrder) {
		t.Fatalf("each unknown: %v", err)
	}
	if a, _ := st.GetAssetByID(f.bin.Asset.ID); a.Asset.Stock.Quantity != 100 {
		t.Fatalf("restocked by a failed receipt: %d", a.Asset.Stock.Quantity)
	}

	r, err := st.As("").ReceivePurchaseOrder(o.ID, ReceiptInput{ReceivedOn: "2026-11-08", Lines: []ReceiptLine{
		{LineID: screws.ID, Quantity: 3},
		{LineID: spools.ID, Quantity: 2, Like: like, WarrantyEnd: "2027-11-08"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if r.Order.State != models.OrderPartlyReceived || len(r.Created) != 2 || len(r.Restocked) != 1 || r.Restocked[0].By != 300 {
		t.Fatalf("receipt %+v", r)
	}
	bin, err := st.GetAssetByID(f.bin.Asset.ID)
	if err != nil {
		t.Fatal(err)
	}
	if bin.Asset.Stock.Quantity != 400 || bin.Asset.Stock.IsRunningLow {
		t.Errorf("bin %+v", bin.Asset.Stock)
	}
	codes := map[string]bool{}
	for _, id := range r.Created {
		a, err := st.GetAssetByID(id)
		if err != nil {
			t.Fatal(err)
		}
		p := a.Purchase
		if a.Asset.Name != "PLA spool" || a.Asset.Stock == nil || a.Asset.Stock.Quantity != 1000 || a.Asset.Stock.Unit != "g" ||
			p.VendorID != f.acme.ID || p.OrderDate != "2026-11-01" || p.ReceivedDate != "2026-11-08" || p.Price != "20.00" || p.Currency != "GBP" ||
			p.OrderNumber != "PO-0001" || p.WarrantyEnd != "2027-11-08" {
			t.Errorf("made %+v %+v %+v", a.Asset, a.Asset.Stock, p)
		}
		if len(a.Location) != 1 || a.Location[0].ID != f.shelf.Asset.ID {
			t.Errorf("made where %+v", a.Location)
		}
		for _, k := range a.GeneratorKeys {
			codes[k.AssignedCode] = true
		}
		events, _, err := st.ListAssetEvents(id, "", 10)
		if err != nil {
			t.Fatal(err)
		}
		var made bool
		for _, e := range events {
			if e.Kind == models.EventAssetCreated && e.Payload.Trigger == models.TriggerOrder && e.Payload.PurchaseOrder != nil && e.Payload.PurchaseOrder.ID == o.ID {
				made = true
			}
		}
		if !made {
			t.Errorf("history %+v", events)
		}
	}
	if !codes["SP-015"] || !codes["SP-016"] {
		t.Errorf("codes %v", codes)
	}
	events, _, err := st.ListAssetEvents(f.bin.Asset.ID, "", 10)
	if err != nil {
		t.Fatal(err)
	}
	var restocked, quantity int
	for _, e := range events {
		if e.Payload.PurchaseOrder == nil || e.Payload.PurchaseOrder.ID != o.ID {
			continue
		}
		switch e.Kind {
		case models.EventAssetRestocked:
			restocked++
		case models.EventQuantityChanged:
			if e.Payload.Trigger == models.TriggerOrder && *e.Payload.Change == 300 {
				quantity++
			}
		}
	}
	if restocked != 1 || quantity != 1 {
		t.Errorf("bin history %+v", events)
	}
	history, _, err := st.ListOrderEvents(o.ID, "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if history[0].Kind != models.EventOrderReceived || len(history[0].Payload.OrderLines) != 2 || history[0].Payload.OrderLines[0].Restocked != 300 ||
		len(history[0].Payload.OrderLines[1].Assets) != 2 {
		t.Errorf("order history %+v", history[0])
	}

	// The rest: delivery, counted in.
	if r, err = st.ReceivePurchaseOrder(o.ID, ReceiptInput{ReceivedOn: "2026-11-09", Lines: []ReceiptLine{{LineID: delivery.ID, Quantity: 1}}}); err != nil {
		t.Fatal(err)
	}
	if r.Order.State != models.OrderReceived || r.Order.ReceivedOn != "2026-11-09" {
		t.Errorf("received %+v", r.Order)
	}
}

// TestReceivingSharesPricesAndChecks: assets made per pack, or as many as
// asked, share the packs' price, rounded half up; a line with no pack size
// must say how much it restocks; an archived asset is refused; a category
// that does not weigh needs no stock; and required fields are asked for.
func TestReceivingSharesPricesAndChecks(t *testing.T) {
	f := newOrderFixture(t)
	st := f.st
	o, err := st.CreatePurchaseOrder(OrderInput{VendorID: f.acme.ID, Currency: "GBP", Lines: []OrderLineInput{
		{CategoryID: f.spoolsWithoutCatalogs.ID, Description: "Blades, pack of 3", Quantity: 1, Price: str("10.00")},
		{AssetID: f.bin.Asset.ID, Description: "Loose bolts", Quantity: 2},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if o, err = st.PlacePurchaseOrder(o.ID, ""); err != nil {
		t.Fatal(err)
	}
	blades, bolts := o.Lines[0], o.Lines[1]
	if _, err := st.ReceivePurchaseOrder(o.ID, ReceiptInput{Lines: []ReceiptLine{{LineID: bolts.ID, Quantity: 1}}}); !errors.Is(err, ErrInvalidOrder) {
		t.Errorf("no pack size: %v", err)
	}
	three, fifty := 3, 50
	r, err := st.ReceivePurchaseOrder(o.ID, ReceiptInput{Lines: []ReceiptLine{
		{LineID: blades.ID, Quantity: 1, Assets: &three, Name: "Saw blade"},
		{LineID: bolts.ID, Quantity: 1, Restock: &fifty},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Created) != 3 {
		t.Fatalf("made %v", r.Created)
	}
	for _, id := range r.Created {
		a, _ := st.GetAssetByID(id)
		if a.Asset.Name != "Saw blade" || a.Purchase.Price != "3.33" || a.Asset.Stock != nil {
			t.Errorf("blade %+v %+v", a.Asset, a.Purchase)
		}
	}
	if a, _ := st.GetAssetByID(f.bin.Asset.ID); a.Asset.Stock.Quantity != 550 {
		t.Errorf("bolts %d", a.Asset.Stock.Quantity)
	}
	if err := st.DeleteAsset(f.bin.Asset.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.ReceivePurchaseOrder(o.ID, ReceiptInput{Lines: []ReceiptLine{{LineID: bolts.ID, Quantity: 1, Restock: &fifty}}}); !isStateError(err) {
		t.Errorf("an archived asset: %v", err)
	}
}

// TestOrderCandidates: what runs low is listed with its preferred entry
// and level, and suggested enough packs to bring it back to its threshold.
func TestOrderCandidates(t *testing.T) {
	f := newOrderFixture(t)
	st := f.st
	if _, err := st.AdjustQuantity(f.bin.Asset.ID, -450, nil); err != nil {
		t.Fatal(err)
	}
	cands, err := st.OrderCandidates()
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]OrderCandidate{}
	for _, c := range cands {
		got[c.Name] = c
	}
	bin, filament := got["M5 bolts"], got["Filament"]
	if bin.AssetID != f.bin.Asset.ID || bin.Level != 50 || bin.Preferred == nil || bin.Preferred.ID != f.screwEntry.ID || SuggestedPacks(bin) != 2 {
		t.Errorf("bin %+v (%d packs)", bin, SuggestedPacks(bin))
	}
	// 1000 g of a 3000 g threshold: two 1000 g spools.
	if filament.CategoryID != f.filament.ID || !filament.Weighs || filament.Level != 1000 || SuggestedPacks(filament) != 2 {
		t.Errorf("filament %+v (%d packs)", filament, SuggestedPacks(filament))
	}
	five := 5
	if n := SuggestedPacks(OrderCandidate{CategoryID: "c", Level: 2, Threshold: &five, Preferred: &models.CatalogEntry{}}); n != 3 {
		t.Errorf("items: %d", n)
	}
	if n := SuggestedPacks(OrderCandidate{AssetID: "a", Preferred: &models.CatalogEntry{}}); n != 1 {
		t.Errorf("by hand: %d", n)
	}
}

// TestOrderNumbering: numbers are shown as PO-0012 unless set otherwise;
// a prefix may be empty, and only letters, digits, hyphens and
// underscores; padding is from 0 to 9; and changing it changes how orders
// are shown, but not the order numbers assets received already carry.
func TestOrderNumbering(t *testing.T) {
	f := newOrderFixture(t)
	st := f.st
	if n, err := st.OrderNumbering(); err != nil || n != DefaultOrderNumbering {
		t.Fatalf("default %+v %v", n, err)
	}
	for _, c := range []struct {
		n    OrderNumbering
		want string
	}{
		{OrderNumbering{Prefix: "PO-", Padding: 4}, "PO-0012"},
		{OrderNumbering{Prefix: "", Padding: 0}, "12"},
		{OrderNumbering{Prefix: "HS_ORD-", Padding: 6}, "HS_ORD-000012"},
		{OrderNumbering{Prefix: "X", Padding: 1}, "X12"},
	} {
		if err := c.n.Check(); err != nil {
			t.Errorf("%+v: %v", c.n, err)
		}
		if got := c.n.Format(12); got != c.want {
			t.Errorf("%+v: %q", c.n, got)
		}
	}
	for _, bad := range []OrderNumbering{
		{Prefix: "PO/"}, {Prefix: "P O"}, {Prefix: "PÖ"}, {Prefix: strings.Repeat("P", MaxOrderPrefix+1)}, {Padding: -1}, {Padding: MaxOrderPadding + 1},
	} {
		if err := st.SetOrderNumbering(bad); !errors.Is(err, ErrInvalidNumbering) {
			t.Errorf("%+v: %v", bad, err)
		}
	}
	o, err := st.CreatePurchaseOrder(OrderInput{VendorID: f.acme.ID, Lines: []OrderLineInput{{CatalogEntryID: f.pla.ID, Quantity: 1}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.PlacePurchaseOrder(o.ID, ""); err != nil {
		t.Fatal(err)
	}
	r, err := st.ReceivePurchaseOrder(o.ID, ReceiptInput{Lines: []ReceiptLine{{LineID: o.Lines[0].ID, Quantity: 1}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetOrderNumbering(OrderNumbering{Prefix: "ORD", Padding: 2}); err != nil {
		t.Fatal(err)
	}
	if o, err = st.GetPurchaseOrder(o.ID); err != nil || o.Reference != "ORD01" || o.Number != 1 {
		t.Errorf("shown %+v %v", o, err)
	}
	a, err := st.GetAssetByID(r.Created[0])
	if err != nil {
		t.Fatal(err)
	}
	if a.Purchase.OrderNumber != "PO-0001" {
		t.Errorf("the asset's order number became %q", a.Purchase.OrderNumber)
	}
}
