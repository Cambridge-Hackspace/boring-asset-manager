package api

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/assetcsv"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

type catalogBody struct {
	Entry   models.CatalogEntry   `json:"entry"`
	Entries []models.CatalogEntry `json:"entries"`
}

func decodeCatalog(t *testing.T, rec *httptest.ResponseRecorder) catalogBody {
	t.Helper()
	var out catalogBody
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("%d %s: %v", rec.Code, rec.Body, err)
	}
	return out
}

// catalogScene is an instance with two vendors, Acme in no scope and
// Kitchenware in the kitchen scope, a consumable category of screws
// running low, and a bag of solder in the kitchen scope counted by
// quantity.
type catalogScene struct {
	srv               *Server
	admin             *http.Cookie
	adminID           string
	acme, kitchenware *models.Vendor
	screws            *models.Category
	solder            *models.AssetWithDetails
}

func newCatalogScene(t *testing.T) catalogScene {
	t.Helper()
	srv, admin, _ := setupTestServer(t)
	st := srv.store
	sc := catalogScene{srv: srv, admin: sessionCookie(t, srv, admin.ID), adminID: admin.ID}
	kitchen, err := st.CreateScope("kitchen")
	if err != nil {
		t.Fatal(err)
	}
	if sc.acme, err = st.CreateVendor(store.VendorInput{Name: "Acme", Website: "https://acme.example"}); err != nil {
		t.Fatal(err)
	}
	if sc.kitchenware, err = st.CreateVendor(store.VendorInput{Name: "Kitchenware", ScopeIDs: []string{kitchen.ID}}); err != nil {
		t.Fatal(err)
	}
	if sc.screws, err = st.CreateCategory("Screws", nil, "#336699", true, true); err != nil {
		t.Fatal(err)
	}
	pantry, err := st.CreateCategory("Pantry", nil, "#336699", false, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.SetCategoryScopes(pantry.ID, []string{kitchen.ID}); err != nil {
		t.Fatal(err)
	}
	if sc.solder, err = st.CreateAsset("Solder", []string{pantry.ID}, nil, nil); err != nil {
		t.Fatal(err)
	}
	return sc
}

// TestCatalogThroughTheAPI: entries are made for a category and an asset,
// one preferred for each, listed by vendor, by what they are for and by
// preference, changed, and deleted; what is wrong is refused.
func TestCatalogThroughTheAPI(t *testing.T) {
	sc := newCatalogScene(t)
	srv, admin := sc.srv, sc.admin
	rec := doJSON(t, srv, admin, http.MethodPost, "/api/v1/catalog", `{"vendor_id":"`+sc.acme.ID+`","category_id":"`+sc.screws.ID+`",
		"part_number":"SC-100","description":"M3 screws","url":"https://acme.example/sc-100","pack_size":100,"pack_unit":"screws",
		"price":"4.5","currency":"GBP","price_checked":"2026-11-01","preferred":true}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("making: %d %s", rec.Code, rec.Body)
	}
	a := decodeCatalog(t, rec).Entry
	if a.Price != "4.50" || a.VendorName != "Acme" || a.LinkedName != "Screws" || !a.Preferred || !a.CanManage || a.PricesHidden {
		t.Errorf("made: %+v", a)
	}
	rec = doJSON(t, srv, admin, http.MethodPost, "/api/v1/catalog", `{"vendor_id":"`+sc.kitchenware.ID+`","category_id":"`+sc.screws.ID+`","part_number":"K-1"}`)
	b := decodeCatalog(t, rec).Entry
	rec = doJSON(t, srv, admin, http.MethodPost, "/api/v1/catalog", `{"vendor_id":"`+sc.kitchenware.ID+`","asset_id":"`+sc.solder.Asset.ID+`","description":"Solder, 500 g","preferred":true}`)
	c := decodeCatalog(t, rec).Entry
	if rec.Code != http.StatusCreated || c.AssetID != sc.solder.Asset.ID {
		t.Fatalf("making for an asset: %d %s", rec.Code, rec.Body)
	}

	for query, want := range map[string]int{"": 3, "?vendor_id=" + sc.kitchenware.ID: 2, "?category_id=" + sc.screws.ID: 2,
		"?asset_id=" + sc.solder.Asset.ID: 1, "?preferred=true": 2} {
		if got := decodeCatalog(t, doJSON(t, srv, admin, http.MethodGet, "/api/v1/catalog"+query, "")).Entries; len(got) != want {
			t.Errorf("%s: %d entries, want %d", query, len(got), want)
		}
	}
	if rec := doJSON(t, srv, admin, http.MethodGet, "/api/v1/catalog?preferred=maybe", ""); rec.Code != http.StatusBadRequest {
		t.Errorf("preferred=maybe: %d", rec.Code)
	}

	// Kitchenware's becomes preferred for the screws, and Acme's stops.
	rec = doJSON(t, srv, admin, http.MethodPut, "/api/v1/catalog/"+b.ID, `{"vendor_id":"`+sc.kitchenware.ID+`","category_id":"`+sc.screws.ID+`","part_number":"K-2","preferred":true}`)
	if got := decodeCatalog(t, rec).Entry; rec.Code != http.StatusOK || got.PartNumber != "K-2" || !got.Preferred {
		t.Fatalf("changing: %d %s", rec.Code, rec.Body)
	}
	if got := decodeCatalog(t, doJSON(t, srv, admin, http.MethodGet, "/api/v1/catalog/"+a.ID, "")).Entry; got.Preferred {
		t.Error("Acme's is still preferred")
	}

	for _, c := range []struct{ body, want string }{
		{`{"vendor_id":"` + sc.acme.ID + `","part_number":"X"}`, "linked to a category or to an asset"},
		{`{"vendor_id":"` + sc.acme.ID + `","category_id":"` + sc.screws.ID + `","part_number":"X","price":"1.234","currency":"GBP"}`, "decimal places"},
		{`{"vendor_id":"` + sc.acme.ID + `","category_id":"nope","part_number":"X"}`, "category not found"},
		{`{"vendor_id":"` + sc.acme.ID + `","asset_id":"nope","part_number":"X"}`, "asset not found"},
		{`{"category_id":"` + sc.screws.ID + `","part_number":"X"}`, "vendor_id is required"},
	} {
		if rec := doJSON(t, srv, admin, http.MethodPost, "/api/v1/catalog", c.body); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), c.want) {
			t.Errorf("%s: %d %s, want %q", c.body, rec.Code, rec.Body, c.want)
		}
	}
	if rec := doJSON(t, srv, admin, http.MethodPost, "/api/v1/catalog", `{"vendor_id":"nope","category_id":"`+sc.screws.ID+`","part_number":"X"}`); rec.Code != http.StatusNotFound {
		t.Errorf("an unknown vendor: %d %s", rec.Code, rec.Body)
	}

	if rec := doJSON(t, srv, admin, http.MethodDelete, "/api/v1/catalog/"+a.ID, ""); rec.Code != http.StatusOK {
		t.Fatalf("deleting: %d %s", rec.Code, rec.Body)
	}
	if rec := doJSON(t, srv, admin, http.MethodGet, "/api/v1/catalog/"+a.ID, ""); rec.Code != http.StatusNotFound {
		t.Errorf("deleted, read: %d", rec.Code)
	}
	// Its vendor's history says so.
	rec = doJSON(t, srv, admin, http.MethodGet, "/api/v1/vendors/"+sc.acme.ID+"/history", "")
	if !strings.Contains(rec.Body.String(), `"what":"catalog"`) || !strings.Contains(rec.Body.String(), "SC-100 (Screws)") {
		t.Errorf("history: %s", rec.Body)
	}
}

// TestCatalogFollowsScopesAndPrices: an entry is seen by those who see its
// vendor and what it is for, its price by those who hold purchases.read
// for the vendor, and it is changed by those who manage the vendor; an
// edit by someone who cannot see prices keeps them.
func TestCatalogFollowsScopesAndPrices(t *testing.T) {
	sc := newCatalogScene(t)
	srv, admin := sc.srv, sc.admin
	newEntry := func(body string) models.CatalogEntry {
		rec := doJSON(t, srv, admin, http.MethodPost, "/api/v1/catalog", body)
		if rec.Code != http.StatusCreated {
			t.Fatalf("making: %d %s", rec.Code, rec.Body)
		}
		return decodeCatalog(t, rec).Entry
	}
	screws := newEntry(`{"vendor_id":"` + sc.acme.ID + `","category_id":"` + sc.screws.ID + `","part_number":"SC-100","price":"4.50","currency":"GBP","preferred":true}`)
	solder := newEntry(`{"vendor_id":"` + sc.kitchenware.ID + `","asset_id":"` + sc.solder.Asset.ID + `","part_number":"S-1","price":"9","currency":"EUR"}`)

	// Someone reading assets everywhere, without purchases.read of their
	// own: everyone signed in holds it in the public scope, and so sees the
	// prices of a vendor in no scope, as they see the vendor, but not those
	// of the kitchen's.
	reader := holderOf(t, srv, "reader@hackspace.org", rbac.PermAssetsRead)
	got := decodeCatalog(t, doJSON(t, srv, reader, http.MethodGet, "/api/v1/catalog", "")).Entries
	if len(got) != 2 || got[0].Price != "4.50" || got[0].CanManage || got[1].Price != "" || got[1].Currency != "" || !got[1].PricesHidden {
		t.Errorf("without purchases.read: %+v", got)
	}
	if rec := doJSON(t, srv, reader, http.MethodPost, "/api/v1/catalog", `{"vendor_id":"`+sc.acme.ID+`","category_id":"`+sc.screws.ID+`","part_number":"X"}`); rec.Code != http.StatusForbidden {
		t.Errorf("without vendors.manage: %d %s", rec.Code, rec.Body)
	}

	// Someone outside the kitchen sees neither Kitchenware nor the solder.
	outsider := holderOf(t, srv, "outsider@hackspace.org", "public:"+rbac.PermAssetsRead, rbac.PermPurchasesRead)
	got = decodeCatalog(t, doJSON(t, srv, outsider, http.MethodGet, "/api/v1/catalog", "")).Entries
	if len(got) != 1 || got[0].ID != screws.ID || got[0].Price != "4.50" {
		t.Errorf("outside the kitchen: %+v", got)
	}
	if rec := doJSON(t, srv, outsider, http.MethodGet, "/api/v1/catalog/"+solder.ID, ""); rec.Code != http.StatusNotFound {
		t.Errorf("the solder's, from outside: %d", rec.Code)
	}

	// A cook manages Kitchenware, and reads the kitchen, but sees no prices:
	// their edit keeps the price; Acme, in no scope, is not theirs to change.
	cook := holderOf(t, srv, "cook@hackspace.org", "kitchen:"+rbac.PermAssetsRead, "kitchen:"+rbac.PermVendorsManage)
	rec := doJSON(t, srv, cook, http.MethodPut, "/api/v1/catalog/"+solder.ID, `{"vendor_id":"`+sc.kitchenware.ID+`","asset_id":"`+sc.solder.Asset.ID+`","part_number":"S-2","price":"1","currency":"GBP"}`)
	if e := decodeCatalog(t, rec).Entry; rec.Code != http.StatusOK || e.PartNumber != "S-2" || !e.PricesHidden || !e.CanManage {
		t.Fatalf("the cook's edit: %d %s", rec.Code, rec.Body)
	}
	if e := decodeCatalog(t, doJSON(t, srv, admin, http.MethodGet, "/api/v1/catalog/"+solder.ID, "")).Entry; e.Price != "9.00" || e.Currency != "EUR" {
		t.Errorf("the price after the cook's edit: %+v", e)
	}
	if rec := doJSON(t, srv, cook, http.MethodDelete, "/api/v1/catalog/"+screws.ID, ""); rec.Code != http.StatusForbidden {
		t.Errorf("the cook deleting Acme's: %d %s", rec.Code, rec.Body)
	}
	// Nor may they move their entry to Acme.
	rec = doJSON(t, srv, cook, http.MethodPut, "/api/v1/catalog/"+solder.ID, `{"vendor_id":"`+sc.acme.ID+`","asset_id":"`+sc.solder.Asset.ID+`","part_number":"S-2"}`)
	if rec.Code != http.StatusForbidden {
		t.Errorf("the cook moving it to Acme: %d %s", rec.Code, rec.Body)
	}
}

// postCatalog sends a vendor's catalog as CSV, only to check it with check.
func postCatalog(t *testing.T, srv *Server, cookie *http.Cookie, vendorID, file string, check bool) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, err := mw.CreateFormFile("file", "catalog.csv")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = fw.Write([]byte(file))
	if check {
		_ = mw.WriteField("check", "true")
	}
	_ = mw.Close()
	return do(t, srv, cookie, http.MethodPost, "/api/v1/vendors/"+vendorID+"/catalog.csv", mw.FormDataContentType(), &body)
}

// TestCatalogCSV: a vendor's catalog is written as CSV and reads back
// unchanged; a file makes and changes entries, all or none, naming
// categories by path and assets by name; one with problems changes
// nothing.
func TestCatalogCSV(t *testing.T) {
	sc := newCatalogScene(t)
	srv, admin := sc.srv, sc.admin
	rec := postCatalog(t, srv, admin, sc.kitchenware.ID, "category,asset,part_number,description,pack_size,pack_unit,price,currency,price_checked,preferred\n"+
		"Screws,,K-1,M3 screws,100,screws,4.5,GBP,2026-11-01,yes\n"+
		",Solder,S-1,\"Solder, 500 g\",,,9,EUR,,\n", true)
	var plan assetcsv.CatalogPlan
	_ = json.Unmarshal(rec.Body.Bytes(), &plan)
	if rec.Code != http.StatusOK || plan.Create != 2 || plan.ProblemCount != 0 {
		t.Fatalf("checked: %d %s", rec.Code, rec.Body)
	}
	if got := decodeCatalog(t, doJSON(t, srv, admin, http.MethodGet, "/api/v1/catalog", "")).Entries; len(got) != 0 {
		t.Fatalf("checking made %d entries", len(got))
	}
	rec = postCatalog(t, srv, admin, sc.kitchenware.ID, "category,asset,part_number,description,pack_size,pack_unit,price,currency,price_checked,preferred\n"+
		"Screws,,K-1,M3 screws,100,screws,4.5,GBP,2026-11-01,yes\n"+
		",Solder,S-1,\"Solder, 500 g\",,,9,EUR,,\n", false)
	if rec.Code != http.StatusOK {
		t.Fatalf("imported: %d %s", rec.Code, rec.Body)
	}
	got := decodeCatalog(t, doJSON(t, srv, admin, http.MethodGet, "/api/v1/catalog?vendor_id="+sc.kitchenware.ID, "")).Entries
	if len(got) != 2 || got[0].LinkedName != "Screws" || got[0].Price != "4.50" || *got[0].PackSize != 100 || !got[0].Preferred ||
		got[1].AssetID != sc.solder.Asset.ID || got[1].Description != "Solder, 500 g" {
		t.Fatalf("imported: %+v", got)
	}

	// Exported, it reads back unchanged.
	rec = doJSON(t, srv, admin, http.MethodGet, "/api/v1/vendors/"+sc.kitchenware.ID+"/catalog.csv", "")
	exported := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.HasPrefix(exported, assetcsv.BOM+"id,category,asset,part_number") || !strings.Contains(exported, ",Screws,,K-1,M3 screws,,100,screws,4.50,GBP,2026-11-01,yes") ||
		!strings.Contains(rec.Header().Get("Content-Disposition"), "Kitchenware-catalog.csv") {
		t.Fatalf("exported: %d %q %v", rec.Code, exported, rec.Header())
	}
	rec = postCatalog(t, srv, admin, sc.kitchenware.ID, exported, false)
	_ = json.Unmarshal(rec.Body.Bytes(), &plan)
	if rec.Code != http.StatusOK || plan.Unchanged != 2 || plan.Create+plan.Update != 0 {
		t.Errorf("imported again: %d %s", rec.Code, rec.Body)
	}

	// An edited row changes its entry; a bad row stops the whole file.
	edited := strings.Replace(exported, "4.50,GBP", "5.00,GBP", 1)
	rec = postCatalog(t, srv, admin, sc.kitchenware.ID, edited+"x,Screws,,Z,,,,,,,\n,Nowhere,,Z,,,,,,,\n,Screws,Solder,Z,,,,,,,\n", false)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("with problems: %d %s", rec.Code, rec.Body)
	}
	for _, want := range []string{`no entry of this vendor has the ID \"x\"`, `unknown category \"Nowhere\"`, "not both"} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("problems lack %q: %s", want, rec.Body)
		}
	}
	rec = postCatalog(t, srv, admin, sc.kitchenware.ID, edited, false)
	_ = json.Unmarshal(rec.Body.Bytes(), &plan)
	if rec.Code != http.StatusOK || plan.Update != 1 || plan.Unchanged != 1 {
		t.Errorf("edited: %d %s", rec.Code, rec.Body)
	}

	// Without purchases.read, prices are blank, and giving one is a problem.
	cook := holderOf(t, srv, "cook@hackspace.org", "kitchen:"+rbac.PermAssetsRead, "kitchen:"+rbac.PermVendorsManage)
	rec = doJSON(t, srv, cook, http.MethodGet, "/api/v1/vendors/"+sc.kitchenware.ID+"/catalog.csv", "")
	if strings.Contains(rec.Body.String(), "5.00") || strings.Contains(rec.Body.String(), "EUR") {
		t.Errorf("the cook's export shows prices: %s", rec.Body)
	}
	rec = postCatalog(t, srv, cook, sc.kitchenware.ID, rec.Body.String(), false)
	_ = json.Unmarshal(rec.Body.Bytes(), &plan)
	if rec.Code != http.StatusOK || plan.Unchanged != 2 {
		t.Errorf("the cook's import of their export: %d %s", rec.Code, rec.Body)
	}
	rec = postCatalog(t, srv, cook, sc.kitchenware.ID, "category,part_number,price,currency\nScrews,Q-1,1,GBP\n", false)
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "purchases.read") {
		t.Errorf("the cook giving a price: %d %s", rec.Code, rec.Body)
	}
	// A reader may export but not import.
	reader := holderOf(t, srv, "reader@hackspace.org", rbac.PermAssetsRead)
	if rec := postCatalog(t, srv, reader, sc.kitchenware.ID, "category,part_number\nScrews,Q-1\n", false); rec.Code != http.StatusForbidden {
		t.Errorf("a reader importing: %d %s", rec.Code, rec.Body)
	}
}
