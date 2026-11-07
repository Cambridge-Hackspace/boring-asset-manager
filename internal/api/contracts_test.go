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
)

type contractBody struct {
	Contract  models.Contract   `json:"contract"`
	Contracts []models.Contract `json:"contracts"`
	CanManage bool              `json:"can_manage"`
}

func decodeContract(t *testing.T, rec *httptest.ResponseRecorder) contractBody {
	t.Helper()
	var out contractBody
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("%s: %v", rec.Body, err)
	}
	return out
}

func contractNames(cs []models.Contract) string {
	var names []string
	for _, c := range cs {
		names = append(names, c.Name)
	}
	return strings.Join(names, ", ")
}

// contractJSON is a contract as the API takes it.
func contractJSON(fields map[string]any) string {
	b, _ := json.Marshal(fields)
	return string(b)
}

// TestContractsThroughTheAPI: a contract is made with a vendor, read,
// listed by when it ends, found by vendor, category, asset and search, and
// shown on the assets it covers, directly and through their categories;
// it is edited, with its history, and deleted; what is wrong is refused,
// and its vendor is not deleted while it has contracts.
func TestContractsThroughTheAPI(t *testing.T) {
	sc := newPurchaseScene(t)
	srv := sc.srv
	kitchen := sc.kettle.Categories[0]

	rec := doJSON(t, srv, sc.admin, http.MethodPost, "/api/v1/contracts", contractJSON(map[string]any{"vendor_id": sc.acme.ID, "kind": "maintenance",
		"name": "Workshop service", "reference": "AG-1", "start_date": "2026-01-01", "end_date": "2027-06-30", "auto_renews": true, "notice_days": 30,
		"cost": "1200", "currency": "GBP", "billing": "yearly", "notes": "Annual", "category_ids": []string{sc.tools.ID}}))
	if rec.Code != http.StatusCreated {
		t.Fatalf("making: %d %s", rec.Code, rec.Body)
	}
	service := decodeContract(t, rec).Contract
	if service.VendorName != "Acme" || service.Cost != "1200.00" || service.NoticeDeadline != "2027-05-31" || len(service.CategoryIDs) != 1 {
		t.Errorf("made: %+v", service)
	}
	lease := decodeContract(t, doJSON(t, srv, sc.admin, http.MethodPost, "/api/v1/contracts", contractJSON(map[string]any{"vendor_id": sc.acme.ID,
		"kind": "lease", "name": "Kettle lease", "end_date": "2026-12-31", "asset_ids": []string{sc.kettle.Asset.ID}}))).Contract

	got := decodeContract(t, doJSON(t, srv, sc.admin, http.MethodGet, "/api/v1/contracts/"+service.ID, ""))
	if got.Contract.Notes != "Annual" || !got.CanManage || got.Contract.Categories[0].Name != "Tools" {
		t.Errorf("read: %+v", got)
	}
	for query, want := range map[string]string{
		"":                                "Kettle lease, Workshop service",
		"?vendor_id=" + sc.acme.ID:        "Kettle lease, Workshop service",
		"?vendor_id=nope":                 "",
		"?category_id=" + sc.tools.ID:     "Workshop service",
		"?category_id=" + kitchen.ID:      "",
		"?asset_id=" + sc.drill.Asset.ID:  "Workshop service",
		"?asset_id=" + sc.kettle.Asset.ID: "Kettle lease",
		"?q=ag-1":                         "Workshop service",
		"?q=lease+acme":                   "Kettle lease",
	} {
		rec := doJSON(t, srv, sc.admin, http.MethodGet, "/api/v1/contracts"+query, "")
		if names := contractNames(decodeContract(t, rec).Contracts); rec.Code != http.StatusOK || names != want {
			t.Errorf("listing %q: %d %q, want %q", query, rec.Code, names, want)
		}
	}

	// The drill is covered through its category, the kettle by name.
	drill := decodeAsset(t, doJSON(t, srv, sc.admin, http.MethodGet, "/api/v1/assets/"+sc.drill.Asset.ID, ""))
	if len(drill.Contracts) != 1 || drill.Contracts[0].ID != service.ID || drill.Contracts[0].CategoryID != sc.tools.ID || drill.Contracts[0].VendorName != "Acme" {
		t.Errorf("the drill's contracts: %+v", drill.Contracts)
	}
	listed := decodeAssets(t, doJSON(t, srv, sc.admin, http.MethodGet, "/api/v1/assets", ""))
	if k := listed["Kettle"]; len(k.Contracts) != 1 || k.Contracts[0].ID != lease.ID || k.Contracts[0].CategoryID != "" {
		t.Errorf("the kettle, listed: %+v", k.Contracts)
	}

	// Leaving out what it covers keeps it.
	edit := map[string]any{"vendor_id": sc.acme.ID, "kind": "maintenance", "name": "Workshop service", "end_date": "2027-06-30", "cost": "1250", "currency": "GBP"}
	rec = doJSON(t, srv, sc.admin, http.MethodPut, "/api/v1/contracts/"+service.ID, contractJSON(edit))
	if c := decodeContract(t, rec).Contract; rec.Code != http.StatusOK || c.Cost != "1250.00" || len(c.CategoryIDs) != 1 || c.AutoRenews || c.Reference != "" {
		t.Errorf("edited: %d %s", rec.Code, rec.Body)
	}
	var history struct {
		Events []models.Event `json:"events"`
	}
	rec = doJSON(t, srv, sc.admin, http.MethodGet, "/api/v1/contracts/"+service.ID+"/history", "")
	if err := json.Unmarshal(rec.Body.Bytes(), &history); err != nil || len(history.Events) != 2 || history.Events[0].Kind != models.EventContractEdited ||
		history.Events[0].Payload.Actor == nil || !strings.Contains(rec.Body.String(), `"what":"cost"`) {
		t.Errorf("history: %s", rec.Body)
	}

	for what, c := range map[string]struct {
		method, path, body string
		want               int
	}{
		"no vendor":                {http.MethodPost, "/api/v1/contracts", `{"name":"X","kind":"lease"}`, http.StatusBadRequest},
		"an unknown vendor":        {http.MethodPost, "/api/v1/contracts", `{"name":"X","kind":"lease","vendor_id":"nope"}`, http.StatusNotFound},
		"an unknown kind":          {http.MethodPost, "/api/v1/contracts", `{"name":"X","kind":"rental","vendor_id":"` + sc.acme.ID + `"}`, http.StatusBadRequest},
		"an unknown asset":         {http.MethodPost, "/api/v1/contracts", `{"name":"X","kind":"lease","vendor_id":"` + sc.acme.ID + `","asset_ids":["nope"]}`, http.StatusBadRequest},
		"an unknown category":      {http.MethodPost, "/api/v1/contracts", `{"name":"X","kind":"lease","vendor_id":"` + sc.acme.ID + `","category_ids":["nope"]}`, http.StatusBadRequest},
		"renewing with no end":     {http.MethodPost, "/api/v1/contracts", `{"name":"X","kind":"lease","vendor_id":"` + sc.acme.ID + `","auto_renews":true}`, http.StatusBadRequest},
		"a missing contract":       {http.MethodGet, "/api/v1/contracts/nope", "", http.StatusNotFound},
		"a missing history":        {http.MethodGet, "/api/v1/contracts/nope/history", "", http.StatusNotFound},
		"a bad page":               {http.MethodGet, "/api/v1/contracts/" + service.ID + "/history?before=nope", "", http.StatusBadRequest},
		"its files":                {http.MethodGet, "/api/v1/contracts/" + service.ID + "/attachments", "", http.StatusOK},
		"deleting its vendor":      {http.MethodDelete, "/api/v1/vendors/" + sc.acme.ID, "", http.StatusConflict},
		"deleting the lease":       {http.MethodDelete, "/api/v1/contracts/" + lease.ID, "", http.StatusOK},
		"an asset that is missing": {http.MethodGet, "/api/v1/contracts?asset_id=nope", "", http.StatusNotFound},
	} {
		if rec := doJSON(t, srv, sc.admin, c.method, c.path, c.body); rec.Code != c.want {
			t.Errorf("%s: %d, want %d: %s", what, rec.Code, c.want, rec.Body)
		}
	}
	if rec := doJSON(t, srv, sc.admin, http.MethodGet, "/api/v1/contracts/"+lease.ID, ""); rec.Code != http.StatusNotFound {
		t.Errorf("the lease, deleted: %d", rec.Code)
	}
	if k := decodeAsset(t, doJSON(t, srv, sc.admin, http.MethodGet, "/api/v1/assets/"+sc.kettle.Asset.ID, "")); len(k.Contracts) != 0 {
		t.Errorf("the kettle after: %+v", k.Contracts)
	}
}

// TestContractsFollowTheirVendors: who sees a contract follows who sees its
// vendor, its cost needs purchases.read there, and the assets it covers
// show only to those who read them; vendors.manage in the vendor's scopes
// changes it, keeping what the editor cannot see.
func TestContractsFollowTheirVendors(t *testing.T) {
	sc := newPurchaseScene(t)
	srv, st := sc.srv, sc.srv.store
	kitchen := scopeID(t, srv, "kitchen")
	supplies, err := st.CreateVendor(store.VendorInput{Name: "Kitchen supplies", ScopeIDs: []string{kitchen}})
	if err != nil {
		t.Fatal(err)
	}
	both, err := st.CreateContract(store.ContractInput{VendorID: sc.acme.ID, Kind: "service", Name: "Everything", Cost: "100", Currency: "GBP",
		AssetIDs: []string{sc.drill.Asset.ID, sc.kettle.Asset.ID}})
	if err != nil {
		t.Fatal(err)
	}
	ovens, err := st.CreateContract(store.ContractInput{VendorID: supplies.ID, Kind: "maintenance", Name: "Ovens", Cost: "50", Currency: "GBP",
		Billing: "monthly", AssetIDs: []string{sc.kettle.Asset.ID}})
	if err != nil {
		t.Fatal(err)
	}

	fixer := holderOf(t, srv, "fixer@hackspace.org", "tools:"+rbac.PermAssetsRead)
	cook := holderOf(t, srv, "cook@hackspace.org", "kitchen:"+rbac.PermAssetsRead, "kitchen:"+rbac.PermPurchasesRead)
	chef := holderOf(t, srv, "chef@hackspace.org", "kitchen:"+rbac.PermAssetsRead, "kitchen:"+rbac.PermVendorsManage)

	// The fixer sees the contract with Acme, in no scope, without its cost
	// or the kettle; not the kitchen's.
	list := decodeContract(t, doJSON(t, srv, fixer, http.MethodGet, "/api/v1/contracts", "")).Contracts
	if contractNames(list) != "Everything" || list[0].Cost != "" || !list[0].CostsHidden || len(list[0].AssetIDs) != 1 || list[0].AssetIDs[0] != sc.drill.Asset.ID {
		t.Errorf("the fixer's contracts: %+v", list)
	}
	if rec := doJSON(t, srv, fixer, http.MethodGet, "/api/v1/contracts/"+ovens.ID, ""); rec.Code != http.StatusNotFound {
		t.Errorf("the fixer reading the kitchen's: %d", rec.Code)
	}
	// The cook sees both, with the kitchen's cost; the cost of the one in no
	// scope, which all who read assets see, needs purchases.read in every
	// scope. On the kettle, both.
	list = decodeContract(t, doJSON(t, srv, cook, http.MethodGet, "/api/v1/contracts", "")).Contracts
	costs := map[string]string{}
	for _, c := range list {
		costs[c.Name] = c.Cost + " " + c.Billing
	}
	if len(list) != 2 || costs["Ovens"] != "50.00 monthly" || costs["Everything"] != " " {
		t.Errorf("the cook's contracts: %+v", costs)
	}
	kettle := decodeAsset(t, doJSON(t, srv, cook, http.MethodGet, "/api/v1/assets/"+sc.kettle.Asset.ID, ""))
	if len(kettle.Contracts) != 2 {
		t.Errorf("the kettle's contracts, to the cook: %+v", kettle.Contracts)
	}
	for _, c := range kettle.Contracts {
		if c.ID == both.ID && c.Cost != "" || c.ID == ovens.ID && c.Cost != "50.00" {
			t.Errorf("the kettle's %s, to the cook: %+v", c.Name, c)
		}
	}
	// The fixer's drill shows the one it sees.
	if d := decodeAsset(t, doJSON(t, srv, fixer, http.MethodGet, "/api/v1/assets/"+sc.drill.Asset.ID, "")); len(d.Contracts) != 1 || d.Contracts[0].ID != both.ID || d.Contracts[0].Cost != "" {
		t.Errorf("the drill's contracts, to the fixer: %+v", d.Contracts)
	}

	ovensEdit := contractJSON(map[string]any{"vendor_id": supplies.ID, "kind": "maintenance", "name": "Ovens and hobs", "cost": "1", "currency": "EUR",
		"asset_ids": []string{}})
	for _, c := range []struct {
		what               string
		cookie             *http.Cookie
		method, path, body string
		want               int
	}{
		{"a reader cannot make one", cook, http.MethodPost, "/api/v1/contracts", `{"vendor_id":"` + supplies.ID + `","kind":"lease","name":"X"}`, http.StatusForbidden},
		{"the chef makes one with the kitchen's vendor", chef, http.MethodPost, "/api/v1/contracts", `{"vendor_id":"` + supplies.ID + `","kind":"lease","name":"Fridge"}`, http.StatusCreated},
		{"but not with one in no scope", chef, http.MethodPost, "/api/v1/contracts", `{"vendor_id":"` + sc.acme.ID + `","kind":"lease","name":"X"}`, http.StatusForbidden},
		{"nor change one with it", chef, http.MethodPut, "/api/v1/contracts/" + both.ID, contractJSON(map[string]any{"vendor_id": sc.acme.ID, "kind": "service", "name": "X"}), http.StatusForbidden},
		{"nor move one to it", chef, http.MethodPut, "/api/v1/contracts/" + ovens.ID, contractJSON(map[string]any{"vendor_id": sc.acme.ID, "kind": "service", "name": "X"}), http.StatusForbidden},
		{"nor delete one with it", chef, http.MethodDelete, "/api/v1/contracts/" + both.ID, "", http.StatusForbidden},
		{"the chef changes the kitchen's", chef, http.MethodPut, "/api/v1/contracts/" + ovens.ID, ovensEdit, http.StatusOK},
	} {
		if rec := doJSON(t, srv, c.cookie, c.method, c.path, c.body); rec.Code != c.want {
			t.Errorf("%s: %d, want %d: %s", c.what, rec.Code, c.want, rec.Body)
		}
	}
	// The chef, without purchases.read, kept its cost.
	kept, err := st.GetContract(ovens.ID)
	if err != nil || kept.Name != "Ovens and hobs" || kept.Cost != "50.00" || kept.Currency != "GBP" || kept.Billing != "monthly" || len(kept.AssetIDs) != 0 {
		t.Errorf("after the chef: %+v %v", kept, err)
	}
	// An editor who cannot read the drill keeps it covered.
	rec := doJSON(t, srv, sc.admin, http.MethodPut, "/api/v1/contracts/"+ovens.ID, contractJSON(map[string]any{"vendor_id": supplies.ID, "kind": "maintenance",
		"name": "Ovens", "asset_ids": []string{sc.drill.Asset.ID}}))
	if rec.Code != http.StatusOK {
		t.Fatalf("covering the drill: %d %s", rec.Code, rec.Body)
	}
	if rec := doJSON(t, srv, chef, http.MethodPut, "/api/v1/contracts/"+ovens.ID, contractJSON(map[string]any{"vendor_id": supplies.ID, "kind": "maintenance",
		"name": "Ovens", "asset_ids": []string{sc.kettle.Asset.ID}})); rec.Code != http.StatusOK {
		t.Fatalf("the chef covering the kettle: %d %s", rec.Code, rec.Body)
	}
	if kept, _ := st.GetContract(ovens.ID); len(kept.AssetIDs) != 2 {
		t.Errorf("the drill is no longer covered: %+v", kept.AssetIDs)
	}
	// Nor may it name an asset it cannot read.
	if rec := doJSON(t, srv, cook, http.MethodGet, "/api/v1/contracts/"+ovens.ID, ""); len(decodeContract(t, rec).Contract.AssetIDs) != 1 {
		t.Errorf("the cook sees the drill: %s", rec.Body)
	}
}

// TestReportsFilterAndBindContracts: a report keeps the assets a contract
// covers, binding that contract; otherwise an asset binds the contract in
// force that covers it longest.
func TestReportsFilterAndBindContracts(t *testing.T) {
	sc := newPurchaseScene(t)
	srv, st := sc.srv, sc.srv.store
	now := time.Now().UTC()
	soon, later := now.AddDate(0, 1, 0).Format(store.DateLayout), now.AddDate(2, 0, 0).Format(store.DateLayout)
	short, err := st.CreateContract(store.ContractInput{VendorID: sc.acme.ID, Kind: "warranty", Name: "Short", EndDate: soon, Cost: "10", Currency: "GBP",
		AssetIDs: []string{sc.drill.Asset.ID, sc.kettle.Asset.ID}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateContract(store.ContractInput{VendorID: sc.acme.ID, Kind: "maintenance", Name: "Long", Reference: "L-1", EndDate: later,
		CategoryIDs: []string{sc.tools.ID}}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateContract(store.ContractInput{VendorID: sc.acme.ID, Kind: "lease", Name: "Over", EndDate: "2001-01-01",
		AssetIDs: []string{sc.kettle.Asset.ID}}); err != nil {
		t.Fatal(err)
	}
	columns := `[{"heading":"Name","binding":"{{asset.name}}"},{"heading":"Contract","binding":"{{asset.contract.name}}|{{asset.contract.kind}}|{{asset.contract.vendor}}|{{asset.contract.reference}}|{{asset.contract.end_date}}|{{asset.contract.cost}} {{asset.contract.currency}}"}]`
	run := func(filter string) string {
		t.Helper()
		rec := doJSON(t, srv, sc.admin, http.MethodPost, "/api/v1/reports/preview", `{"spec":{"filter":`+filter+`,"sections":[{"type":"table","columns":`+columns+`}]}}`)
		var out struct {
			Result reports.Result `json:"result"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || rec.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", filter, rec.Code, rec.Body)
		}
		var rows []string
		for _, g := range out.Result.Sections[0].Groups {
			for _, r := range g.Rows {
				rows = append(rows, strings.Join(r.Cells, "="))
			}
		}
		return strings.Join(rows, ";")
	}
	for filter, want := range map[string]string{
		`{}`:                              "Drill=Long|maintenance|Acme|L-1|" + later + "| ;Kettle=Short|warranty|Acme||" + soon + "|10.00 GBP",
		`{"contract":"` + short.ID + `"}`: "Drill=Short|warranty|Acme||" + soon + "|10.00 GBP;Kettle=Short|warranty|Acme||" + soon + "|10.00 GBP",
		`{"contract":"nope"}`:             "",
	} {
		if got := run(filter); got != want {
			t.Errorf("%s:\n got %q\nwant %q", filter, got, want)
		}
	}
}

// TestContractEventsReachChannels: a contract's reminders are queued for
// the channels subscribed to them, and are in its history.
func TestContractEventsReachChannels(t *testing.T) {
	sc := newPurchaseScene(t)
	srv := sc.srv
	rec := doJSON(t, srv, sc.admin, http.MethodPost, "/api/v1/notifiers", `{"name":"Contracts","driver":"webhook","options":{"url":"https://example.com/hook"},"events":["contract.expiring","contract.expired"],"enabled":true}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("subscribing: %d %s", rec.Code, rec.Body)
	}
	c, err := srv.store.CreateContract(store.ContractInput{VendorID: sc.acme.ID, Kind: "lease", Name: "Lease", EndDate: "2027-06-30"})
	if err != nil {
		t.Fatal(err)
	}
	for _, day := range []time.Time{time.Date(2027, 6, 20, 9, 0, 0, 0, time.UTC), time.Date(2027, 6, 30, 9, 0, 0, 0, time.UTC)} {
		if n, err := srv.store.RecordContractReminders(day, 30); err != nil || n != 1 {
			t.Fatalf("recorded %d: %v", n, err)
		}
	}
	var queued int
	if err := srv.store.QueryRow("SELECT COUNT(*) FROM notification_deliveries").Scan(&queued); err != nil || queued != 2 {
		t.Errorf("queued %d: %v", queued, err)
	}
	h := doJSON(t, srv, sc.admin, http.MethodGet, "/api/v1/contracts/"+c.ID+"/history", "").Body.String()
	if !strings.Contains(h, `"kind":"contract.expiring"`) || !strings.Contains(h, `"kind":"contract.expired"`) {
		t.Errorf("history: %s", h)
	}
}
