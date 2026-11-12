package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/reports"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/xlsx"
)

func decodeAsset(t *testing.T, rec *httptest.ResponseRecorder) models.AssetWithDetails {
	t.Helper()
	var out struct {
		Asset models.AssetWithDetails `json:"asset"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("%d %s: %v", rec.Code, rec.Body, err)
	}
	return out.Asset
}

func decodeAssets(t *testing.T, rec *httptest.ResponseRecorder) map[string]models.AssetWithDetails {
	t.Helper()
	var out struct {
		Assets []models.AssetWithDetails `json:"assets"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("%d %s: %v", rec.Code, rec.Body, err)
	}
	byName := map[string]models.AssetWithDetails{}
	for _, a := range out.Assets {
		byName[a.Asset.Name] = a
	}
	return byName
}

// purchaseScene is an instance with a vendor and two assets bought from
// it: a drill in the tools scope and a kettle in the kitchen scope.
type purchaseScene struct {
	srv           *Server
	admin         *http.Cookie
	adminID       string
	acme          *models.Vendor
	drill, kettle *models.AssetWithDetails
	tools         *models.Category
}

func newPurchaseScene(t *testing.T) purchaseScene {
	t.Helper()
	srv, admin, _ := setupTestServer(t)
	st := srv.store
	sc := purchaseScene{srv: srv, admin: sessionCookie(t, srv, admin.ID), adminID: admin.ID}
	var err error
	if sc.acme, err = st.CreateVendor(store.VendorInput{Name: "Acme"}); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name, scope string
		into        **models.AssetWithDetails
		price       string
	}{{"Tools", "tools", &sc.drill, "129.99"}, {"Kitchen", "kitchen", &sc.kettle, "20"}} {
		scope, err := st.CreateScope(c.scope)
		if err != nil {
			t.Fatal(err)
		}
		cat, err := st.CreateCategory(c.name, nil, "#336699", false, false)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := st.SetCategoryScopes(cat.ID, []string{scope.ID}); err != nil {
			t.Fatal(err)
		}
		if c.scope == "tools" {
			sc.tools = cat
		}
		name := map[string]string{"tools": "Drill", "kitchen": "Kettle"}[c.scope]
		body, _ := json.Marshal(map[string]any{"name": name, "category_ids": []string{cat.ID}, "purchase": map[string]string{
			"vendor_id": sc.acme.ID, "order_date": "2026-01-05", "received_date": "2026-01-09", "price": c.price, "currency": "GBP",
			"order_number": "PO-" + c.scope, "warranty_end": "2028-01-09"}})
		rec := doJSON(t, srv, sc.admin, http.MethodPost, "/api/v1/assets", string(body))
		if rec.Code != http.StatusCreated {
			t.Fatalf("making %s: %d %s", name, rec.Code, rec.Body)
		}
		a := decodeAsset(t, rec)
		*c.into = &a
	}
	return sc
}

// TestPurchaseDetailsThroughTheAPI: an asset is made and edited with where
// it came from, which its page shows; an invoice is one of its attachments;
// what is wrong is refused.
func TestPurchaseDetailsThroughTheAPI(t *testing.T) {
	sc := newPurchaseScene(t)
	srv, admin := sc.srv, sc.admin
	p := sc.drill.Purchase
	if p.VendorID != sc.acme.ID || p.VendorName != "Acme" || p.Price != "129.99" || p.Currency != "GBP" || p.OrderNumber != "PO-tools" ||
		p.OrderDate != "2026-01-05" || p.ReceivedDate != "2026-01-09" || p.WarrantyEnd != "2028-01-09" || p.PricesHidden {
		t.Errorf("made: %+v", p)
	}
	path := "/api/v1/assets/" + sc.drill.Asset.ID
	if got := decodeAsset(t, doJSON(t, srv, admin, http.MethodGet, path, "")).Purchase; got != p {
		t.Errorf("read: %+v", got)
	}

	// An attachment becomes its invoice.
	rec := attach(t, srv, admin, path+"/attachments", "invoice.pdf", []byte("%PDF-1.4\n"), nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("attaching: %d %s", rec.Code, rec.Body)
	}
	invoice := decodeAttached(t, rec).Attachment
	rec = doJSON(t, srv, admin, http.MethodPut, path, `{"purchase":{"vendor_id":"`+sc.acme.ID+`","price":"99","currency":"EUR","invoice_attachment_id":"`+invoice.ID+`"}}`)
	got := decodeAsset(t, rec).Purchase
	if rec.Code != http.StatusOK || got.Price != "99.00" || got.Currency != "EUR" || got.InvoiceID != invoice.ID || got.InvoiceName != "invoice.pdf" || got.OrderDate != "" {
		t.Errorf("edited: %d %+v", rec.Code, got)
	}
	// Leaving purchase out keeps it.
	if got := decodeAsset(t, doJSON(t, srv, admin, http.MethodPut, path, `{"name":"Big drill"}`)).Purchase; got.Price != "99.00" {
		t.Errorf("renamed: %+v", got)
	}

	for what, body := range map[string]string{
		"a bad date":                   `{"purchase":{"order_date":"yesterday"}}`,
		"a price with no currency":     `{"purchase":{"price":"12"}}`,
		"too many places":              `{"purchase":{"price":"1.5","currency":"JPY"}}`,
		"an unknown vendor":            `{"purchase":{"vendor_id":"nope"}}`,
		"another asset's attachment":   `{"purchase":{"invoice_attachment_id":"nope"}}`,
		"an order number of 300 chars": `{"purchase":{"order_number":"` + strings.Repeat("x", 300) + `"}}`,
	} {
		if rec := doJSON(t, srv, admin, http.MethodPut, path, body); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: %d %s", what, rec.Code, rec.Body)
		}
	}
}

// TestPurchasesReadHidesPrices: without purchases.read for an asset, its
// price, currency, order number and invoice are left out of its page, the
// asset list, its history, exports, reports and labels, in the scopes where
// it is not held; its vendor and dates still show. Editing it keeps what
// they cannot see.
func TestPurchasesReadHidesPrices(t *testing.T) {
	sc := newPurchaseScene(t)
	srv := sc.srv
	// Prices in the kitchen only.
	who := holderOf(t, srv, "cook@hackspace.org", rbac.PermAssetsRead, rbac.PermAssetsWrite, "kitchen:"+rbac.PermPurchasesRead, rbac.PermReportsManage)

	listed := decodeAssets(t, doJSON(t, srv, who, http.MethodGet, "/api/v1/assets", ""))
	drill, kettle := listed["Drill"].Purchase, listed["Kettle"].Purchase
	if drill.Price != "" || drill.Currency != "" || drill.OrderNumber != "" || !drill.PricesHidden || drill.VendorName != "Acme" || drill.WarrantyEnd != "2028-01-09" {
		t.Errorf("the drill, in the list: %+v", drill)
	}
	if kettle.Price != "20.00" || kettle.OrderNumber != "PO-kitchen" || kettle.PricesHidden {
		t.Errorf("the kettle, in the list: %+v", kettle)
	}
	drillPath := "/api/v1/assets/" + sc.drill.Asset.ID
	page := doJSON(t, srv, who, http.MethodGet, drillPath, "")
	if strings.Contains(page.Body.String(), "129.99") || strings.Contains(page.Body.String(), "PO-tools") || !strings.Contains(page.Body.String(), `"prices_hidden":true`) {
		t.Errorf("the drill's page: %s", page.Body)
	}

	// Its history leaves out the price, and an edit of nothing else.
	if _, err := srv.store.UpdateAssetWithPurchase(sc.drill.Asset.ID, "", nil, nil, nil, nil, nil, &store.PurchaseInput{VendorID: sc.acme.ID,
		OrderDate: "2026-01-05", ReceivedDate: "2026-01-09", Price: "149.99", Currency: "GBP", OrderNumber: "PO-tools", WarrantyEnd: "2028-01-09"}); err != nil {
		t.Fatal(err)
	}
	history := doJSON(t, srv, who, http.MethodGet, drillPath+"/history", "").Body.String()
	if strings.Contains(history, "129.99") || strings.Contains(history, "149.99") || strings.Count(history, `"kind":"asset.`) != 1 || !strings.Contains(history, "Acme") {
		t.Errorf("the drill's history: %s", history)
	}
	if adminHistory := doJSON(t, srv, sc.admin, http.MethodGet, drillPath+"/history", "").Body.String(); !strings.Contains(adminHistory, "129.99 GBP") || !strings.Contains(adminHistory, "149.99 GBP") {
		t.Errorf("the drill's history, to an admin: %s", adminHistory)
	}

	// The export has the kettle's price and not the drill's.
	rows := readCSV(t, doJSON(t, srv, who, http.MethodGet, "/api/v1/assets/export.csv", "").Body.String())
	col := map[string]int{}
	for i, h := range rows[0] {
		col[h] = i
	}
	for _, row := range rows[1:] {
		price := row[col["price"]]
		if row[col["name"]] == "Drill" && (price != "" || row[col["order_number"]] != "" || row[col["vendor"]] != "Acme") ||
			row[col["name"]] == "Kettle" && price != "20.00" {
			t.Errorf("exported: %v %v", rows[0], row)
		}
	}
	// So does a report, and a label's data.
	spec := `{"filter":{},"sections":[{"type":"table","columns":[{"heading":"Name","binding":"{{asset.name}}"},{"heading":"Price","binding":"{{asset.purchase.price}} {{asset.purchase.currency}}"},` +
		`{"heading":"Vendor","binding":"{{asset.purchase.vendor}}"}]}]}`
	rec := doJSON(t, srv, who, http.MethodPost, "/api/v1/reports/preview", `{"spec":`+spec+`}`)
	if body := rec.Body.String(); rec.Code != http.StatusOK || strings.Contains(body, "149.99") || !strings.Contains(body, `["Drill"," ","Acme"]`) ||
		!strings.Contains(body, `["Kettle","20.00 GBP","Acme"]`) {
		t.Errorf("a report: %d %s", rec.Code, body)
	}

	// An edit by them keeps the drill's price, which they cannot see.
	rec = doJSON(t, srv, who, http.MethodPut, drillPath, `{"purchase":{"vendor_id":"`+sc.acme.ID+`","warranty_end":"2029-01-09","price":"1","currency":"USD"}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("editing: %d %s", rec.Code, rec.Body)
	}
	stored, _ := srv.store.GetAssetByID(sc.drill.Asset.ID)
	if p := stored.Purchase; p.Price != "149.99" || p.Currency != "GBP" || p.OrderNumber != "PO-tools" || p.WarrantyEnd != "2029-01-09" || p.OrderDate != "" {
		t.Errorf("after their edit: %+v", p)
	}
}

// TestVendorsHiddenFromAReader: a vendor the requester may not see is left
// out of an asset, kept when they edit it, and cannot be named or listed
// by.
func TestVendorsHiddenFromAReader(t *testing.T) {
	sc := newPurchaseScene(t)
	srv, st := sc.srv, sc.srv.store
	secret, err := st.CreateScope("secret")
	if err != nil {
		t.Fatal(err)
	}
	hidden, err := st.CreateVendor(store.VendorInput{Name: "Hidden", ScopeIDs: []string{secret.ID}})
	if err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/assets/" + sc.drill.Asset.ID
	if rec := doJSON(t, srv, sc.admin, http.MethodPut, path, `{"purchase":{"vendor_id":"`+hidden.ID+`"}}`); rec.Code != http.StatusOK {
		t.Fatalf("an admin naming it: %d %s", rec.Code, rec.Body)
	}
	who := holderOf(t, srv, "fixer@hackspace.org", "tools:"+rbac.PermAssetsRead, "tools:"+rbac.PermAssetsWrite)
	if got := decodeAsset(t, doJSON(t, srv, who, http.MethodGet, path, "")).Purchase; got.VendorID != "" || got.VendorName != "" {
		t.Errorf("seen: %+v", got)
	}
	if rec := doJSON(t, srv, who, http.MethodPut, path, `{"purchase":{"order_date":"2026-02-01"}}`); rec.Code != http.StatusOK {
		t.Fatalf("editing: %d %s", rec.Code, rec.Body)
	}
	if stored, _ := st.GetAssetByID(sc.drill.Asset.ID); stored.Purchase.VendorID != hidden.ID || stored.Purchase.OrderDate != "2026-02-01" {
		t.Errorf("the hidden vendor is not kept: %+v", stored.Purchase)
	}
	if rec := doJSON(t, srv, who, http.MethodPut, path, `{"purchase":{"vendor_id":"`+hidden.ID+`x"}}`); rec.Code != http.StatusBadRequest {
		t.Errorf("naming one it cannot see: %d %s", rec.Code, rec.Body)
	}
	if rec := doJSON(t, srv, who, http.MethodGet, "/api/v1/assets?vendor_id="+hidden.ID, ""); rec.Code != http.StatusNotFound {
		t.Errorf("listing by it: %d %s", rec.Code, rec.Body)
	}
}

// TestAVendorsAssets: the assets bought from a vendor, as its page lists
// them, are those the requester may read.
func TestAVendorsAssets(t *testing.T) {
	sc := newPurchaseScene(t)
	srv := sc.srv
	if _, err := srv.store.CreateAsset("Rope", nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/assets?vendor_id=" + sc.acme.ID
	if got := decodeAssets(t, doJSON(t, srv, sc.admin, http.MethodGet, path, "")); len(got) != 2 || got["Drill"].Asset.ID == "" || got["Kettle"].Asset.ID == "" {
		t.Errorf("to an admin: %v", got)
	}
	cook := holderOf(t, srv, "cook@hackspace.org", "kitchen:"+rbac.PermAssetsRead, "kitchen:"+rbac.PermPurchasesRead)
	got := decodeAssets(t, doJSON(t, srv, cook, http.MethodGet, path, ""))
	if len(got) != 1 || got["Kettle"].Purchase.Price != "20.00" {
		t.Errorf("to the cook: %v", got)
	}
}

// TestReportsFilterAndBindPurchases: a report keeps the assets bought from
// a vendor, ordered between two dates, or whose warranty ends before one,
// and binds their purchase details.
func TestReportsFilterAndBindPurchases(t *testing.T) {
	sc := newPurchaseScene(t)
	srv, st := sc.srv, sc.srv.store
	bolts, _ := st.CreateVendor(store.VendorInput{Name: "Bolts R Us"})
	if _, err := st.CreateAssetWithPurchase("Bolt", nil, nil, nil, nil, "", &store.PurchaseInput{VendorID: bolts.ID, ReceivedDate: "2026-06-01", WarrantyEnd: "2026-12-31"}); err != nil {
		t.Fatal(err)
	}
	columns := `[{"heading":"Name","binding":"{{asset.name}}"},{"heading":"Bought","binding":"{{asset.purchase.order_date}}|{{asset.purchase.received_date}}"},` +
		`{"heading":"From","binding":"{{asset.purchase.vendor}}"},{"heading":"Cost","binding":"{{asset.purchase.price}} {{asset.purchase.currency}}"},` +
		`{"heading":"Order","binding":"{{asset.purchase.order_number}}"},{"heading":"Warranty","binding":"{{asset.purchase.warranty_end}}"}]`
	run := func(filter string) string {
		t.Helper()
		rec := doJSON(t, srv, sc.admin, http.MethodPost, "/api/v1/reports/preview", `{"spec":{"filter":`+filter+`,"sections":[{"type":"table","columns":`+columns+`}]}}`)
		var out struct {
			Result reports.Result `json:"result"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || rec.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", filter, rec.Code, rec.Body)
		}
		var names []string
		for _, g := range out.Result.Sections[0].Groups {
			for _, r := range g.Rows {
				names = append(names, r.Cells[0])
				if r.Cells[0] == "Drill" && strings.Join(r.Cells, ";") != "Drill;2026-01-05|2026-01-09;Acme;129.99 GBP;PO-tools;2028-01-09" {
					t.Errorf("the drill's row: %v", r.Cells)
				}
			}
		}
		return strings.Join(names, ",")
	}
	for filter, want := range map[string]string{
		`{"vendor":"` + sc.acme.ID + `"}`:                                     "Drill,Kettle",
		`{"vendor":"` + bolts.ID + `"}`:                                       "Bolt",
		`{"purchased_between":{"from":"2026-05-01"}}`:                         "Bolt",
		`{"purchased_between":{"from":"2026-01-01","to":"2026-01-31"}}`:       "Drill,Kettle",
		`{"purchased_between":{"to":"2025-12-31"}}`:                           "",
		`{"warranty_ends_before":"2027-01-01"}`:                               "Bolt",
		`{"warranty_ends_before":"2030-01-01","vendor":"` + sc.acme.ID + `"}`: "Drill,Kettle",
	} {
		if got := run(filter); got != want {
			t.Errorf("%s: %q, want %q", filter, got, want)
		}
	}
	for _, bad := range []string{`{"purchased_between":{"from":"May"}}`, `{"purchased_between":{"from":"2026-02-01","to":"2026-01-01"}}`, `{"warranty_ends_before":"soon"}`} {
		rec := doJSON(t, srv, sc.admin, http.MethodPost, "/api/v1/reports/preview", `{"spec":{"filter":`+bad+`,"sections":[{"type":"table","columns":`+columns+`}]}}`)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: %d %s", bad, rec.Code, rec.Body)
		}
	}
}

// TestDuplicatingDoesNotCopyPurchases: an asset's copy, as duplicating
// starts from, has none of where it came from.
func TestDuplicatingDoesNotCopyPurchases(t *testing.T) {
	sc := newPurchaseScene(t)
	rec := doJSON(t, sc.srv, sc.admin, http.MethodGet, "/api/v1/assets/"+sc.drill.Asset.ID+"/copy", "")
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "purchase") || strings.Contains(rec.Body.String(), "129.99") || strings.Contains(rec.Body.String(), sc.acme.ID) {
		t.Errorf("the copy: %d %s", rec.Code, rec.Body)
	}
	// Made from it, the new asset has none.
	var copied struct {
		Copy json.RawMessage `json:"copy"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &copied)
	rec = doJSON(t, sc.srv, sc.admin, http.MethodPost, "/api/v1/assets", string(copied.Copy))
	if a := decodeAsset(t, rec); rec.Code != http.StatusCreated || !a.Purchase.IsZero() {
		t.Errorf("made from the copy: %d %+v", rec.Code, a.Purchase)
	}
}

// TestPurchasesRoundTripThroughCSVAndExcel: an export imports back
// unchanged, and an import sets where assets came from, naming the vendor
// by name; a blank cell keeps what an asset has, and prices are given only
// where the importer may see them.
func TestPurchasesRoundTripThroughCSVAndExcel(t *testing.T) {
	sc := newPurchaseScene(t)
	srv, admin := sc.srv, sc.admin
	exported := doJSON(t, srv, admin, http.MethodGet, "/api/v1/assets/export.csv", "").Body.String()
	for _, col := range []string{"vendor", "order_date", "received_date", "price", "currency", "order_number", "warranty_end"} {
		if !strings.Contains(strings.SplitN(exported, "\n", 2)[0], col) {
			t.Errorf("no %s column: %s", col, exported)
		}
	}
	rec := postImport(t, srv, admin, "/api/v1/assets/import/preview", exported, nil)
	if !strings.Contains(rec.Body.String(), `"unchanged":2`) || !strings.Contains(rec.Body.String(), `"problem_count":0`) {
		t.Errorf("the export, imported back: %s", rec.Body)
	}
	book := doJSON(t, srv, admin, http.MethodGet, "/api/v1/assets/export.xlsx", "").Body.Bytes()
	wb, err := xlsx.Open(book)
	if err != nil {
		t.Fatal(err)
	}
	if data, err := wb.CSV("All"); err != nil || !strings.Contains(string(data), "129.99") || !strings.Contains(string(data), "2028-01-09") {
		t.Errorf("the workbook: %s %v", data, err)
	}
	if rec := postWorkbook(t, srv, admin, "/api/v1/assets/import/preview", book, "All", nil); !strings.Contains(rec.Body.String(), `"unchanged":2`) ||
		!strings.Contains(rec.Body.String(), `"problem_count":0`) {
		t.Errorf("the workbook, imported back: %s", rec.Body)
	}

	// Importing sets them, and keeps what is blank.
	file := "id,name,vendor,price,currency,warranty_end\n" + sc.drill.Asset.ID + ",Drill,acme,135,,\n,Lathe,Acme,2500,eur,2030-05-01\n"
	rec = postImport(t, srv, admin, "/api/v1/assets/import", file, []string{"id", "name", "vendor", "price", "currency", "warranty_end"})
	if rec.Code != http.StatusOK {
		t.Fatalf("importing: %d %s", rec.Code, rec.Body)
	}
	drill, _ := srv.store.GetAssetByID(sc.drill.Asset.ID)
	if p := drill.Purchase; p.Price != "135.00" || p.Currency != "GBP" || p.WarrantyEnd != "2028-01-09" || p.OrderNumber != "PO-tools" {
		t.Errorf("the drill, imported: %+v", p)
	}
	lathes, _ := srv.store.ListAssets("", "Lathe")
	if len(lathes) != 1 || lathes[0].Purchase.VendorID != sc.acme.ID || lathes[0].Purchase.Price != "2500.00" || lathes[0].Purchase.Currency != "EUR" {
		t.Errorf("the lathe, imported: %+v", lathes)
	}

	for what, c := range map[string]struct{ file, problem string }{
		"an unknown vendor":         {"name,vendor\nX,Nobody\n", `no vendor is \"Nobody\"`},
		"a price with no currency":  {"name,price\nX,12\n", "a price needs its currency"},
		"a date written otherwise":  {"name,order_date\nX,05/01/2026\n", "YYYY-MM-DD"},
		"a price too exact for JPY": {"name,price,currency\nX,1.5,JPY\n", "no minor unit"},
	} {
		rec := postImport(t, srv, admin, "/api/v1/assets/import/preview", c.file, nil)
		if !strings.Contains(rec.Body.String(), c.problem) {
			t.Errorf("%s: %s", what, rec.Body)
		}
	}
	// Prices only where the importer may see them.
	who := holderOf(t, srv, "cook@hackspace.org", rbac.PermAssetsRead, rbac.PermAssetsWrite, "kitchen:"+rbac.PermPurchasesRead)
	rec = postImport(t, srv, who, "/api/v1/assets/import/preview", "id,price,currency\n"+sc.drill.Asset.ID+",1,GBP\n"+sc.kettle.Asset.ID+",21,GBP\n", nil)
	if !strings.Contains(rec.Body.String(), "you cannot see the prices of assets in tools") || strings.Contains(rec.Body.String(), "kitchen") {
		t.Errorf("prices by the cook: %s", rec.Body)
	}
}

// TestWarrantyEventsReachChannels: warranty.expiring is an event channels
// subscribe to, and an asset's history shows it.
func TestWarrantyEventsReachChannels(t *testing.T) {
	sc := newPurchaseScene(t)
	srv := sc.srv
	rec := doJSON(t, srv, sc.admin, http.MethodPost, "/api/v1/notifiers", `{"name":"Warranties","driver":"webhook","options":{"url":"https://example.com/hook"},"events":["warranty.expiring"],"enabled":true}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("subscribing: %d %s", rec.Code, rec.Body)
	}
	if n, err := srv.store.RecordExpiringWarranties(time.Date(2027, 12, 20, 9, 0, 0, 0, time.UTC), 30); err != nil || n != 2 {
		t.Fatalf("recorded %d: %v", n, err)
	}
	var queued int
	if err := srv.store.QueryRow("SELECT COUNT(*) FROM notification_deliveries").Scan(&queued); err != nil || queued != 2 {
		t.Errorf("queued %d: %v", queued, err)
	}
	if h := doJSON(t, srv, sc.admin, http.MethodGet, "/api/v1/assets/"+sc.drill.Asset.ID+"/history", "").Body.String(); !strings.Contains(h, `"kind":"warranty.expiring"`) {
		t.Errorf("history: %s", h)
	}
}

// invoiced gives an asset a photo, a receipt photographed and a manual,
// in that order, and names the receipt its invoice, answering their IDs.
func invoiced(t *testing.T, sc purchaseScene, a *models.AssetWithDetails) (photo, receipt, manual string) {
	t.Helper()
	path := "/api/v1/assets/" + a.Asset.ID
	ids := map[string]string{}
	for _, f := range []struct {
		name string
		data []byte
	}{{"photo.jpg", testJPEG(t, 8, 8)}, {"receipt.jpg", testJPEG(t, 9, 9)}, {"manual.pdf", testPDF}} {
		rec := attach(t, sc.srv, sc.admin, path+"/attachments", f.name, f.data, nil)
		if rec.Code != http.StatusCreated {
			t.Fatalf("attaching %s: %d %s", f.name, rec.Code, rec.Body)
		}
		ids[f.name] = decodeAttached(t, rec).Attachment.ID
	}
	body, _ := json.Marshal(map[string]any{"purchase": map[string]string{"vendor_id": sc.acme.ID, "price": "5", "currency": "GBP", "invoice_attachment_id": ids["receipt.jpg"]}})
	if rec := doJSON(t, sc.srv, sc.admin, http.MethodPut, path, string(body)); rec.Code != http.StatusOK {
		t.Fatalf("naming the invoice: %d %s", rec.Code, rec.Body)
	}
	return ids["photo.jpg"], ids["receipt.jpg"], ids["manual.pdf"]
}

// attachmentNames are the names of an asset's attachments, in order, as
// who sees them.
func attachmentNames(t *testing.T, srv *Server, who *http.Cookie, assetID string) string {
	t.Helper()
	var names []string
	for _, a := range decodeAttached(t, doJSON(t, srv, who, http.MethodGet, "/api/v1/assets/"+assetID+"/attachments", "")).Attachments {
		names = append(names, a.Name)
	}
	return strings.Join(names, ",")
}

// TestPurchasesReadHidesTheInvoice: without purchases.read for an asset,
// the attachment named as its invoice is left out of its attachments and
// photos, on its page and in the list, and refused as though it were not
// there: downloading, renaming, removing, ordering and copying it. In the
// scopes where it is held, and once it is no longer the invoice, it is an
// ordinary attachment.
func TestPurchasesReadHidesTheInvoice(t *testing.T) {
	sc := newPurchaseScene(t)
	srv := sc.srv
	photo, receipt, manual := invoiced(t, sc, sc.drill)
	_, kettleReceipt, _ := invoiced(t, sc, sc.kettle)
	// Prices in the kitchen only.
	who := holderOf(t, srv, "cook@hackspace.org", rbac.PermAssetsRead, rbac.PermAssetsWrite, "kitchen:"+rbac.PermPurchasesRead)
	drill, kettle := "/api/v1/assets/"+sc.drill.Asset.ID, "/api/v1/assets/"+sc.kettle.Asset.ID

	if got := attachmentNames(t, srv, who, sc.drill.Asset.ID); got != "photo.jpg,manual.pdf" {
		t.Errorf("the drill's attachments: %s", got)
	}
	if got := attachmentNames(t, srv, sc.admin, sc.drill.Asset.ID); got != "photo.jpg,receipt.jpg,manual.pdf" {
		t.Errorf("the drill's attachments, to an admin: %s", got)
	}
	if got := attachmentNames(t, srv, who, sc.kettle.Asset.ID); got != "photo.jpg,receipt.jpg,manual.pdf" {
		t.Errorf("the kettle's attachments: %s", got)
	}
	shown := func(photos []models.AssetPhoto) string {
		var ids []string
		for _, p := range photos {
			ids = append(ids, p.ID)
		}
		return strings.Join(ids, ",")
	}
	if got := shown(decodeAsset(t, doJSON(t, srv, who, http.MethodGet, drill, "")).Photos); got != photo {
		t.Errorf("the drill's page's photos: %s", got)
	}
	if got := shown(decodeAssets(t, doJSON(t, srv, who, http.MethodGet, "/api/v1/assets", ""))["Drill"].Photos); got != photo {
		t.Errorf("the drill's photos, in the list: %s", got)
	}
	if got := decodeAsset(t, doJSON(t, srv, who, http.MethodGet, kettle, "")).Photos; len(got) != 2 {
		t.Errorf("the kettle's page's photos: %+v", got)
	}

	for what, req := range map[string]struct{ method, path, body string }{
		"downloading":           {http.MethodGet, drill + "/attachments/" + receipt, ""},
		"its thumbnail":         {http.MethodGet, drill + "/attachments/" + receipt + "?size=thumb", ""},
		"as a photo":            {http.MethodGet, drill + "/photos/" + receipt, ""},
		"renaming":              {http.MethodPut, drill + "/attachments/" + receipt, `{"name":"Bill"}`},
		"removing":              {http.MethodDelete, drill + "/attachments/" + receipt, ""},
		"removing as a photo":   {http.MethodDelete, drill + "/photos/" + receipt, ""},
		"ordering it":           {http.MethodPut, drill + "/attachments", `{"attachment_ids":["` + receipt + `","` + photo + `","` + manual + `"]}`},
		"ordering it as photos": {http.MethodPut, drill + "/photos", `{"photo_ids":["` + receipt + `","` + photo + `"]}`},
	} {
		want := http.StatusNotFound
		if strings.HasPrefix(what, "ordering") {
			want = http.StatusBadRequest
		}
		if rec := doJSON(t, srv, who, req.method, req.path, req.body); rec.Code != want {
			t.Errorf("%s: %d %s", what, rec.Code, rec.Body)
		}
	}
	if rec := doJSON(t, srv, sc.admin, http.MethodGet, drill+"/attachments/"+receipt, ""); rec.Code != http.StatusOK {
		t.Errorf("downloading, as an admin: %d", rec.Code)
	}
	if rec := doJSON(t, srv, who, http.MethodGet, kettle+"/attachments/"+kettleReceipt, ""); rec.Code != http.StatusOK {
		t.Errorf("downloading the kettle's: %d", rec.Code)
	}

	// Ordering what they see keeps the invoice where it is.
	if rec := doJSON(t, srv, who, http.MethodPut, drill+"/attachments", `{"attachment_ids":["`+manual+`","`+photo+`"]}`); rec.Code != http.StatusOK ||
		strings.Contains(rec.Body.String(), "receipt") {
		t.Errorf("ordering: %d %s", rec.Code, rec.Body)
	}
	if got := attachmentNames(t, srv, sc.admin, sc.drill.Asset.ID); got != "manual.pdf,receipt.jpg,photo.jpg" {
		t.Errorf("ordered, to an admin: %s", got)
	}
	if rec := doJSON(t, srv, who, http.MethodPut, drill+"/photos", `{"photo_ids":["`+photo+`"]}`); rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), receipt) {
		t.Errorf("ordering the photos: %d %s", rec.Code, rec.Body)
	}

	// Copying the drill's attachments leaves it out.
	made := decodeAsset(t, doJSON(t, srv, who, http.MethodPost, "/api/v1/assets", `{"name":"Spare drill","category_ids":["`+sc.tools.ID+`"]}`))
	for _, route := range []string{"/attachments/copy", "/photos/copy"} {
		if rec := doJSON(t, srv, who, http.MethodPost, "/api/v1/assets/"+made.Asset.ID+route, `{"from_asset_id":"`+sc.drill.Asset.ID+`"}`); rec.Code != http.StatusOK {
			t.Fatalf("copying, %s: %d %s", route, rec.Code, rec.Body)
		}
	}
	if got := attachmentNames(t, srv, sc.admin, made.Asset.ID); got != "manual.pdf,photo.jpg,photo.jpg" {
		t.Errorf("copied: %s", got)
	}

	// Once it is no longer the invoice, it is an ordinary attachment.
	if rec := doJSON(t, srv, sc.admin, http.MethodPut, drill, `{"purchase":{"vendor_id":"`+sc.acme.ID+`"}}`); rec.Code != http.StatusOK {
		t.Fatalf("forgetting the invoice: %d %s", rec.Code, rec.Body)
	}
	if got := attachmentNames(t, srv, who, sc.drill.Asset.ID); got != "manual.pdf,receipt.jpg,photo.jpg" {
		t.Errorf("no longer the invoice: %s", got)
	}
	if rec := doJSON(t, srv, who, http.MethodGet, drill+"/photos/"+receipt, ""); rec.Code != http.StatusOK {
		t.Errorf("downloading it, no longer the invoice: %d", rec.Code)
	}
	if rec := doJSON(t, srv, who, http.MethodDelete, drill+"/attachments/"+receipt, ""); rec.Code != http.StatusOK {
		t.Errorf("removing it, no longer the invoice: %d %s", rec.Code, rec.Body)
	}
}
