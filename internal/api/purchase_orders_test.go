package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

type orderBody struct {
	Order     models.PurchaseOrder   `json:"order"`
	Orders    []models.PurchaseOrder `json:"orders"`
	Created   []string               `json:"created_asset_ids"`
	Restocked []store.Restocked      `json:"restocked"`
}

func decodeOrder(t *testing.T, rec *httptest.ResponseRecorder) orderBody {
	t.Helper()
	var out orderBody
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("%d %s: %v", rec.Code, rec.Body, err)
	}
	return out
}

func orderRefs(os []models.PurchaseOrder) string {
	var refs []string
	for _, o := range os {
		refs = append(refs, o.Reference)
	}
	return strings.Join(refs, ", ")
}

// orderScene is the purchase scene with a bin of bolts counted by
// quantity among the tools, running low, and Acme's catalog: the bolts,
// and drills.
type orderScene struct {
	purchaseScene
	bin                *models.AssetWithDetails
	bolts, drillsEntry *models.CatalogEntry
}

func newOrderScene(t *testing.T) orderScene {
	t.Helper()
	sc := orderScene{purchaseScene: newPurchaseScene(t)}
	st := sc.srv.store
	q, low := 50, 200
	var err error
	if sc.bin, err = st.CreateAssetWithStock("M5 bolts", []string{sc.tools.ID}, nil, nil, &store.StockInput{Quantity: &q, Unit: "pcs", LowStockThreshold: &low}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.SetAssetRunningLow(sc.bin.Asset.ID, true); err != nil {
		t.Fatal(err)
	}
	hundred, one := 100, 1
	if sc.bolts, err = st.CreateCatalogEntry(store.CatalogEntryInput{VendorID: sc.acme.ID, AssetID: sc.bin.Asset.ID, PartNumber: "M5-100", Description: "M5 bolts",
		PackSize: &hundred, PackUnit: "pcs", Price: "4.50", Currency: "GBP", Preferred: true}); err != nil {
		t.Fatal(err)
	}
	if sc.drillsEntry, err = st.CreateCatalogEntry(store.CatalogEntryInput{VendorID: sc.acme.ID, CategoryID: sc.tools.ID, PartNumber: "DR-18",
		Description: "Cordless drill", PackSize: &one, PackUnit: "drill", Price: "99.00", Currency: "GBP", Preferred: true}); err != nil {
		t.Fatal(err)
	}
	return sc
}

// TestPurchaseOrdersThroughTheAPI: what runs low is suggested, by vendor;
// an order is made from the catalog, listed and found, edited, placed,
// sent as a PDF, and received in parts, restocking the bin and making
// drills like the drill with the order's purchase details; its history
// says so; and what is wrong is refused.
func TestPurchaseOrdersThroughTheAPI(t *testing.T) {
	sc := newOrderScene(t)
	srv, admin := sc.srv, sc.admin

	var suggested struct {
		Vendors []OrderSuggestion `json:"vendors"`
		OnOrder []SuggestedLine   `json:"on_order"`
	}
	rec := doJSON(t, srv, admin, http.MethodGet, "/api/v1/purchase-orders/suggestions", "")
	if err := json.Unmarshal(rec.Body.Bytes(), &suggested); err != nil || rec.Code != http.StatusOK || len(suggested.Vendors) != 1 ||
		suggested.Vendors[0].VendorID != sc.acme.ID || len(suggested.Vendors[0].Lines) != 1 || suggested.Vendors[0].Lines[0].Quantity != 2 ||
		suggested.Vendors[0].Lines[0].CatalogEntryID != sc.bolts.ID || suggested.Vendors[0].Lines[0].UnitPrice != "4.50" {
		t.Fatalf("suggested: %d %s", rec.Code, rec.Body)
	}

	rec = doJSON(t, srv, admin, http.MethodPost, "/api/v1/purchase-orders", `{"vendor_id":"`+sc.acme.ID+`","deliver_to":"The Hackspace","lines":[`+
		`{"catalog_entry_id":"`+sc.bolts.ID+`","quantity":2},{"catalog_entry_id":"`+sc.drillsEntry.ID+`","quantity":2},{"description":"Delivery","quantity":1,"unit_price":"5"}]}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("making: %d %s", rec.Code, rec.Body)
	}
	o := decodeOrder(t, rec).Order
	if o.Reference != "PO-0001" || o.State != models.OrderDraft || o.Currency != "GBP" || o.Total != "212.00" || !o.CanManage || len(o.Lines) != 3 {
		t.Fatalf("made: %+v", o)
	}
	// It is on order now.
	rec = doJSON(t, srv, admin, http.MethodGet, "/api/v1/purchase-orders/suggestions", "")
	if err := json.Unmarshal(rec.Body.Bytes(), &suggested); err != nil || len(suggested.Vendors) != 0 || len(suggested.OnOrder) != 1 || suggested.OnOrder[0].Order.ID != o.ID {
		t.Errorf("suggested again: %s", rec.Body)
	}
	for query, want := range map[string]string{
		"":                               "PO-0001",
		"?state=draft":                   "PO-0001",
		"?state=open":                    "PO-0001",
		"?state=received":                "",
		"?vendor_id=" + sc.acme.ID:       "PO-0001",
		"?category_id=" + sc.tools.ID:    "PO-0001",
		"?asset_id=" + sc.bin.Asset.ID:   "PO-0001",
		"?asset_id=" + sc.drill.Asset.ID: "",
		"?q=dr-18":                       "PO-0001",
		"?q=po-0001+acme":                "PO-0001",
		"?q=nothing":                     "",
	} {
		rec := doJSON(t, srv, admin, http.MethodGet, "/api/v1/purchase-orders"+query, "")
		if refs := orderRefs(decodeOrder(t, rec).Orders); rec.Code != http.StatusOK || refs != want {
			t.Errorf("listing %q: %d %q, want %q", query, rec.Code, refs, want)
		}
	}

	// Leaving lines out keeps them; the delivery's price changes.
	rec = doJSON(t, srv, admin, http.MethodPut, "/api/v1/purchase-orders/"+o.ID, `{"vendor_id":"`+sc.acme.ID+`","currency":"GBP","notes":"Ring first"}`)
	if got := decodeOrder(t, rec).Order; rec.Code != http.StatusOK || len(got.Lines) != 3 || got.Notes != "Ring first" || got.DeliverTo != "" {
		t.Errorf("edited: %d %s", rec.Code, rec.Body)
	}
	lines := `[{"id":"` + o.Lines[0].ID + `","catalog_entry_id":"` + sc.bolts.ID + `","quantity":2},{"id":"` + o.Lines[1].ID + `","catalog_entry_id":"` +
		sc.drillsEntry.ID + `","quantity":2},{"id":"` + o.Lines[2].ID + `","description":"Delivery","quantity":1,"unit_price":"6.50"}]`
	rec = doJSON(t, srv, admin, http.MethodPut, "/api/v1/purchase-orders/"+o.ID, `{"vendor_id":"`+sc.acme.ID+`","currency":"GBP","deliver_to":"The Hackspace","lines":`+lines+`}`)
	if got := decodeOrder(t, rec).Order; rec.Code != http.StatusOK || got.Total != "213.50" || got.Lines[0].UnitPrice != "4.50" {
		t.Errorf("edited lines: %d %s", rec.Code, rec.Body)
	}
	rec = doJSON(t, srv, admin, http.MethodPost, "/api/v1/purchase-orders/"+o.ID+"/place", `{"ordered_on":"2026-11-01"}`)
	if got := decodeOrder(t, rec).Order; rec.Code != http.StatusOK || got.State != models.OrderOrdered || got.OrderedOn != "2026-11-01" {
		t.Fatalf("placed: %d %s", rec.Code, rec.Body)
	}

	rec = doJSON(t, srv, admin, http.MethodGet, "/api/v1/purchase-orders/"+o.ID+"/export.pdf", "")
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "application/pdf" || !bytes.HasPrefix(rec.Body.Bytes(), []byte("%PDF-1.4")) ||
		!strings.Contains(rec.Header().Get("Content-Disposition"), "PO-0001") {
		t.Errorf("the PDF: %d %v", rec.Code, rec.Header())
	}

	// The bolts and one drill come.
	rec = doJSON(t, srv, admin, http.MethodPost, "/api/v1/purchase-orders/"+o.ID+"/receive", `{"received_on":"2026-11-08","lines":[`+
		`{"line_id":"`+o.Lines[0].ID+`","quantity":2},{"line_id":"`+o.Lines[1].ID+`","quantity":1,"like":"`+sc.drill.Asset.ID+`","warranty_end":"2028-11-08"}]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("receiving: %d %s", rec.Code, rec.Body)
	}
	got := decodeOrder(t, rec)
	if got.Order.State != models.OrderPartlyReceived || len(got.Created) != 1 || len(got.Restocked) != 1 || got.Restocked[0].By != 200 {
		t.Fatalf("received: %s", rec.Body)
	}
	drill := decodeAsset(t, doJSON(t, srv, admin, http.MethodGet, "/api/v1/assets/"+got.Created[0], ""))
	if p := drill.Purchase; drill.Asset.Name != "Drill" || p.VendorID != sc.acme.ID || p.Price != "99.00" || p.OrderNumber != "PO-0001" ||
		p.OrderDate != "2026-11-01" || p.ReceivedDate != "2026-11-08" || p.WarrantyEnd != "2028-11-08" {
		t.Errorf("the drill made: %+v %+v", drill.Asset, drill.Purchase)
	}
	if bin := decodeAsset(t, doJSON(t, srv, admin, http.MethodGet, "/api/v1/assets/"+sc.bin.Asset.ID, "")); bin.Asset.Stock.Quantity != 250 || bin.Asset.Stock.IsRunningLow {
		t.Errorf("the bin: %+v", bin.Asset.Stock)
	}
	var history struct {
		Events []models.Event `json:"events"`
	}
	rec = doJSON(t, srv, admin, http.MethodGet, "/api/v1/purchase-orders/"+o.ID+"/history", "")
	if err := json.Unmarshal(rec.Body.Bytes(), &history); err != nil || len(history.Events) != 5 || history.Events[0].Kind != models.EventOrderReceived ||
		history.Events[0].Payload.Actor == nil || len(history.Events[0].Payload.OrderLines) != 2 {
		t.Errorf("history: %s", rec.Body)
	}

	for _, step := range []struct {
		what               string
		method, path, body string
		want               int
	}{
		{"no vendor", http.MethodPost, "/api/v1/purchase-orders", `{}`, http.StatusBadRequest},
		{"an unknown vendor", http.MethodPost, "/api/v1/purchase-orders", `{"vendor_id":"nope"}`, http.StatusNotFound},
		{"an unknown entry", http.MethodPost, "/api/v1/purchase-orders", `{"vendor_id":"` + sc.acme.ID + `","lines":[{"catalog_entry_id":"nope","quantity":1}]}`, http.StatusBadRequest},
		{"a bad state", http.MethodGet, "/api/v1/purchase-orders?state=lost", "", http.StatusBadRequest},
		{"a missing order", http.MethodGet, "/api/v1/purchase-orders/nope", "", http.StatusNotFound},
		{"placing twice", http.MethodPost, "/api/v1/purchase-orders/" + o.ID + "/place", "", http.StatusConflict},
		{"changing its currency", http.MethodPut, "/api/v1/purchase-orders/" + o.ID, `{"vendor_id":"` + sc.acme.ID + `","currency":"EUR","deliver_to":"The Hackspace"}`, http.StatusConflict},
		{"too much", http.MethodPost, "/api/v1/purchase-orders/" + o.ID + "/receive", `{"lines":[{"line_id":"` + o.Lines[1].ID + `","quantity":2}]}`, http.StatusConflict},
		{"an unknown line", http.MethodPost, "/api/v1/purchase-orders/" + o.ID + "/receive", `{"lines":[{"line_id":"nope","quantity":1}]}`, http.StatusBadRequest},
		{"a bad page", http.MethodGet, "/api/v1/purchase-orders/" + o.ID + "/history?before=nope", "", http.StatusBadRequest},
		{"its files", http.MethodGet, "/api/v1/purchase-orders/" + o.ID + "/attachments", "", http.StatusOK},
		{"deleting its vendor", http.MethodDelete, "/api/v1/vendors/" + sc.acme.ID, "", http.StatusConflict},
		{"canceling it", http.MethodPost, "/api/v1/purchase-orders/" + o.ID + "/cancel", "", http.StatusOK},
		{"canceling it again", http.MethodPost, "/api/v1/purchase-orders/" + o.ID + "/cancel", "", http.StatusConflict},
		{"receiving it canceled", http.MethodPost, "/api/v1/purchase-orders/" + o.ID + "/receive", `{"lines":[{"line_id":"` + o.Lines[1].ID + `","quantity":1}]}`, http.StatusConflict},
		{"deleting it", http.MethodDelete, "/api/v1/purchase-orders/" + o.ID, "", http.StatusOK},
		{"deleted", http.MethodGet, "/api/v1/purchase-orders/" + o.ID, "", http.StatusNotFound},
	} {
		if rec := doJSON(t, srv, admin, step.method, step.path, step.body); rec.Code != step.want {
			t.Errorf("%s: %d, want %d: %s", step.what, rec.Code, step.want, rec.Body)
		}
	}
	// What was received stays.
	if a := decodeAsset(t, doJSON(t, srv, admin, http.MethodGet, "/api/v1/assets/"+got.Created[0], "")); a.Purchase.OrderNumber != "PO-0001" {
		t.Errorf("the drill after: %+v", a.Purchase)
	}
}

// TestPurchaseOrdersFollowTheirVendors: who sees an order follows who sees
// its vendor, its prices need purchases.read there (in every scope for a
// vendor in none), and vendors.manage there changes it, keeping the
// prices an editor cannot see; receiving needs stock.record on an asset
// restocked, and assets.write where the assets made go.
func TestPurchaseOrdersFollowTheirVendors(t *testing.T) {
	sc := newOrderScene(t)
	srv, st := sc.srv, sc.srv.store
	kitchen := scopeID(t, srv, "kitchen")
	supplies, err := st.CreateVendor(store.VendorInput{Name: "Kitchen supplies", ScopeIDs: []string{kitchen}})
	if err != nil {
		t.Fatal(err)
	}
	price := "4.00"
	acmeOrder, err := st.CreatePurchaseOrder(store.OrderInput{VendorID: sc.acme.ID, Lines: []store.OrderLineInput{
		{CatalogEntryID: sc.bolts.ID, Quantity: 1}, {CatalogEntryID: sc.drillsEntry.ID, Quantity: 1}, {Description: "Glue", Quantity: 1, Price: &price}}})
	if err != nil {
		t.Fatal(err)
	}
	if acmeOrder, err = st.PlacePurchaseOrder(acmeOrder.ID, ""); err != nil {
		t.Fatal(err)
	}
	kettles, err := st.CreatePurchaseOrder(store.OrderInput{VendorID: supplies.ID, Currency: "GBP", Lines: []store.OrderLineInput{
		{AssetID: sc.kettle.Asset.ID, Description: "Kettle descaler", Quantity: 3, Price: &price}}})
	if err != nil {
		t.Fatal(err)
	}

	fixer := holderOf(t, srv, "fixer@hackspace.org", "tools:"+rbac.PermAssetsRead, "tools:"+rbac.PermPurchasesRead)
	cook := holderOf(t, srv, "cook@hackspace.org", "kitchen:"+rbac.PermAssetsRead, "kitchen:"+rbac.PermPurchasesRead)
	chef := holderOf(t, srv, "chef@hackspace.org", "kitchen:"+rbac.PermAssetsRead, "kitchen:"+rbac.PermVendorsManage)
	stocker := holderOf(t, srv, "stocker@hackspace.org", "tools:"+rbac.PermAssetsRead, "tools:"+rbac.PermStockRecord)
	maker := holderOf(t, srv, "maker@hackspace.org", "tools:"+rbac.PermAssetsRead, "tools:"+rbac.PermAssetsWrite, "tools:"+rbac.PermStockRecord)

	// The fixer sees Acme's, in no scope, without prices, which need
	// purchases.read in every scope; not the kitchen's.
	list := decodeOrder(t, doJSON(t, srv, fixer, http.MethodGet, "/api/v1/purchase-orders", "")).Orders
	if orderRefs(list) != acmeOrder.Reference || list[0].Total != "" || !list[0].PricesHidden || list[0].Lines[0].UnitPrice != "" || list[0].CanManage {
		t.Errorf("the fixer's orders: %+v", list)
	}
	if rec := doJSON(t, srv, fixer, http.MethodGet, "/api/v1/purchase-orders/"+kettles.ID, ""); rec.Code != http.StatusNotFound {
		t.Errorf("the fixer reading the kitchen's: %d", rec.Code)
	}
	// The cook sees the kitchen's prices, and Acme's order without the bin,
	// which the cook may not read.
	list = decodeOrder(t, doJSON(t, srv, cook, http.MethodGet, "/api/v1/purchase-orders", "")).Orders
	byRef := map[string]models.PurchaseOrder{}
	for _, o := range list {
		byRef[o.Reference] = o
	}
	if k := byRef[kettles.Reference]; k.Total != "12.00" || k.Lines[0].AssetID != sc.kettle.Asset.ID {
		t.Errorf("the kitchen's, to the cook: %+v", k)
	}
	if a := byRef[acmeOrder.Reference]; a.Lines[0].AssetID != "" || a.Lines[0].Description != "M5 bolts" || a.Total != "" {
		t.Errorf("Acme's, to the cook: %+v", a)
	}

	for _, c := range []struct {
		what               string
		cookie             *http.Cookie
		method, path, body string
		want               int
	}{
		{"a reader cannot make one", cook, http.MethodPost, "/api/v1/purchase-orders", `{"vendor_id":"` + supplies.ID + `"}`, http.StatusForbidden},
		{"the chef makes one to the kitchen's vendor", chef, http.MethodPost, "/api/v1/purchase-orders", `{"vendor_id":"` + supplies.ID + `"}`, http.StatusCreated},
		{"but not to one in no scope", chef, http.MethodPost, "/api/v1/purchase-orders", `{"vendor_id":"` + sc.acme.ID + `"}`, http.StatusForbidden},
		{"nor place one to it", chef, http.MethodPost, "/api/v1/purchase-orders/" + acmeOrder.ID + "/cancel", "", http.StatusForbidden},
		{"the chef changes the kitchen's, prices unseen", chef, http.MethodPut, "/api/v1/purchase-orders/" + kettles.ID,
			`{"vendor_id":"` + supplies.ID + `","currency":"EUR","lines":[{"id":"` + kettles.Lines[0].ID + `","asset_id":"` + sc.kettle.Asset.ID + `","description":"Descaler","quantity":5,"unit_price":"1"}]}`, http.StatusOK},
		{"the stocker cannot make drills", stocker, http.MethodPost, "/api/v1/purchase-orders/" + acmeOrder.ID + "/receive",
			`{"lines":[{"line_id":"` + acmeOrder.Lines[1].ID + `","quantity":1}]}`, http.StatusForbidden},
		{"the cook cannot restock the bin", cook, http.MethodPost, "/api/v1/purchase-orders/" + acmeOrder.ID + "/receive",
			`{"lines":[{"line_id":"` + acmeOrder.Lines[0].ID + `","quantity":1}]}`, http.StatusForbidden},
		{"the stocker restocks the bin, and counts in the glue", stocker, http.MethodPost, "/api/v1/purchase-orders/" + acmeOrder.ID + "/receive",
			`{"lines":[{"line_id":"` + acmeOrder.Lines[0].ID + `","quantity":1},{"line_id":"` + acmeOrder.Lines[2].ID + `","quantity":1}]}`, http.StatusOK},
		{"the maker makes the drill", maker, http.MethodPost, "/api/v1/purchase-orders/" + acmeOrder.ID + "/receive",
			`{"lines":[{"line_id":"` + acmeOrder.Lines[1].ID + `","quantity":1,"name":"New drill"}]}`, http.StatusOK},
	} {
		if rec := doJSON(t, srv, c.cookie, c.method, c.path, c.body); rec.Code != c.want {
			t.Errorf("%s: %d, want %d: %s", c.what, rec.Code, c.want, rec.Body)
		}
	}
	// The chef, without purchases.read, kept its prices and currency.
	kept, err := st.GetPurchaseOrder(kettles.ID)
	if err != nil || kept.Currency != "GBP" || kept.Lines[0].UnitPrice != "4.00" || kept.Lines[0].Quantity != 5 || kept.Lines[0].Description != "Descaler" {
		t.Errorf("after the chef: %+v %v", kept, err)
	}
	if done, _ := st.GetPurchaseOrder(acmeOrder.ID); done.State != models.OrderReceived {
		t.Errorf("Acme's after: %+v", done)
	}
}

// TestOrderNumberingThroughTheAPI: everyone who reads assets reads how
// orders' numbers are shown, those who manage settings set it, a prefix
// unsafe in a file's name or on a label is refused, and orders are shown
// with it at once while assets received keep the number they were given.
func TestOrderNumberingThroughTheAPI(t *testing.T) {
	sc := newOrderScene(t)
	srv, st, admin := sc.srv, sc.srv.store, sc.admin
	reader := holderOf(t, srv, "reader@hackspace.org", rbac.PermAssetsRead)
	rec := doJSON(t, srv, reader, http.MethodGet, "/api/v1/settings/purchase-orders", "")
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != `{"prefix":"PO-","padding":4}` {
		t.Errorf("read: %d %s", rec.Code, rec.Body)
	}
	o, err := st.CreatePurchaseOrder(store.OrderInput{VendorID: sc.acme.ID, Lines: []store.OrderLineInput{{CatalogEntryID: sc.drillsEntry.ID, Quantity: 1}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.PlacePurchaseOrder(o.ID, ""); err != nil {
		t.Fatal(err)
	}
	r, err := st.ReceivePurchaseOrder(o.ID, store.ReceiptInput{Lines: []store.ReceiptLine{{LineID: o.Lines[0].ID, Quantity: 1}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		cookie *http.Cookie
		body   string
		want   int
	}{
		{reader, `{"prefix":"ORD-","padding":2}`, http.StatusForbidden},
		{admin, `{"prefix":"ORD/","padding":2}`, http.StatusBadRequest},
		{admin, `{"prefix":"ORDERS-AND-MORE-ORDERS","padding":2}`, http.StatusBadRequest},
		{admin, `{"prefix":"ORD-","padding":10}`, http.StatusBadRequest},
		{admin, `{"prefix":" ORD- ","padding":2}`, http.StatusOK},
	} {
		if rec := doJSON(t, srv, c.cookie, http.MethodPut, "/api/v1/settings/purchase-orders", c.body); rec.Code != c.want {
			t.Errorf("%s: %d, want %d: %s", c.body, rec.Code, c.want, rec.Body)
		}
	}
	if got := decodeOrder(t, doJSON(t, srv, admin, http.MethodGet, "/api/v1/purchase-orders/"+o.ID, "")).Order; got.Reference != "ORD-01" {
		t.Errorf("shown as %q", got.Reference)
	}
	if a := decodeAsset(t, doJSON(t, srv, admin, http.MethodGet, "/api/v1/assets/"+r.Created[0], "")); a.Purchase.OrderNumber != "PO-0001" {
		t.Errorf("the asset's order number became %q", a.Purchase.OrderNumber)
	}
	rec = doJSON(t, srv, admin, http.MethodGet, "/api/v1/purchase-orders/"+o.ID+"/export.pdf", "")
	if !strings.Contains(rec.Header().Get("Content-Disposition"), "ORD-01") {
		t.Errorf("the PDF's name: %v", rec.Header())
	}
}
