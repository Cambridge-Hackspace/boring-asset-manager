package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/reports"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// poolFixture is bolts kept in two places, each place's category implying
// the pool "M5 bolts": 140 in the workshop drawer, in the workshop scope,
// and 300 in the laser bay.
type poolFixture struct {
	srv                   *Server
	admin                 *http.Cookie
	adminUser             *models.User
	drawer, laser, bolts  *models.Category
	drawerBin, laserBin   string
	workshopScope, public string
}

func newPoolFixture(t *testing.T) *poolFixture {
	t.Helper()
	srv, admin, _ := setupTestServer(t)
	st := srv.store
	f := &poolFixture{srv: srv, admin: sessionCookie(t, srv, admin.ID), adminUser: admin}
	mk := func(in store.CategoryInput) *models.Category {
		c, err := st.CreateCategoryFrom(in)
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	f.drawer = mk(store.CategoryInput{Name: "Workshop drawer"})
	f.laser = mk(store.CategoryInput{Name: "Laser bay"})
	f.bolts = mk(store.CategoryInput{Name: "M5 bolts", IsConsumable: true, Unit: "pcs", LowStockThreshold: intp(200)})
	scope, err := st.CreateScope("workshop")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.SetCategoryScopes(f.drawer.ID, []string{scope.ID}); err != nil {
		t.Fatal(err)
	}
	other, err := st.CreateScope("laser")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.SetCategoryScopes(f.laser.ID, []string{other.ID}); err != nil {
		t.Fatal(err)
	}
	f.workshopScope = "workshop"
	bin := func(name string, q int, cat string) string {
		a, err := st.CreateAssetWithStock(name, []string{cat}, nil, nil, &store.StockInput{Quantity: intp(q), Unit: "pcs", LowStockThreshold: intp(50)})
		if err != nil {
			t.Fatal(err)
		}
		return a.Asset.ID
	}
	f.drawerBin = bin("Drawer bin", 140, f.drawer.ID)
	f.laserBin = bin("Laser bay bin", 300, f.laser.ID)
	return f
}

func intp(n int) *int { return &n }

func TestLinkingCategoriesOverTheAPI(t *testing.T) {
	f := newPoolFixture(t)
	srv, admin := f.srv, f.admin
	reader := userWithPermissions(t, srv, "reader@hackspace.org", rbac.PermAssetsRead)

	// Only those who manage categories link them.
	path := "/api/v1/categories/" + f.bolts.ID + "/links"
	body := `{"included_by":["` + f.drawer.ID + `","` + f.laser.ID + `"]}`
	if rec := doJSON(t, srv, reader, http.MethodPut, path, body); rec.Code != http.StatusForbidden {
		t.Errorf("a reader linked categories: %d", rec.Code)
	}
	rec := doJSON(t, srv, admin, http.MethodPut, path, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("linking: %d %s", rec.Code, rec.Body)
	}
	var linked struct {
		Category     models.Category
		LinkWarnings []store.LinkWarning `json:"link_warnings"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &linked); err != nil {
		t.Fatal(err)
	}
	if len(linked.Category.IncludedBy) != 2 || len(linked.LinkWarnings) != 0 {
		t.Errorf("linked %+v", linked)
	}
	// The pool's page totals both places, and the asset list finds both.
	var level struct {
		ActiveCount int `json:"active_count"`
		Total       int
	}
	rec = doJSON(t, srv, admin, http.MethodGet, "/api/v1/categories/"+f.bolts.ID, "")
	if err := json.Unmarshal(rec.Body.Bytes(), &level); err != nil || level.Total != 440 || level.ActiveCount != 2 {
		t.Errorf("the pool's page: %s", rec.Body)
	}
	var list struct{ Assets []models.AssetWithDetails }
	rec = doJSON(t, srv, admin, http.MethodGet, "/api/v1/assets?category_id="+f.bolts.ID, "")
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil || len(list.Assets) != 2 {
		t.Errorf("filtering by the pool: %s", rec.Body)
	}

	// A cycle is refused, from either side, and so is a move making one.
	back := `{"implies":["` + f.drawer.ID + `"]}`
	if rec := doJSON(t, srv, admin, http.MethodPut, path, back); rec.Code != http.StatusConflict {
		t.Errorf("a cycle: %d %s", rec.Code, rec.Body)
	}
	move := `{"name":"Workshop drawer","parent_id":"` + f.bolts.ID + `"}`
	if rec := doJSON(t, srv, admin, http.MethodPut, "/api/v1/categories/"+f.drawer.ID, move); rec.Code != http.StatusOK {
		t.Errorf("moving the drawer under the pool, which leads nowhere back: %d %s", rec.Code, rec.Body)
	}
	moveBack := `{"name":"M5 bolts","is_consumable":true,"unit":"pcs","low_stock_threshold":200,"parent_id":"` + f.drawer.ID + `"}`
	if rec := doJSON(t, srv, admin, http.MethodPut, "/api/v1/categories/"+f.bolts.ID, moveBack); rec.Code != http.StatusBadRequest && rec.Code != http.StatusConflict {
		t.Errorf("moving the pool under the drawer: %d %s", rec.Code, rec.Body)
	}
	if rec := doJSON(t, srv, admin, http.MethodPut, "/api/v1/categories/missing/links", `{"implies":[]}`); rec.Code != http.StatusNotFound {
		t.Errorf("a missing category: %d", rec.Code)
	}
	if rec := doJSON(t, srv, admin, http.MethodPut, path, `{"implies":["missing"]}`); rec.Code != http.StatusBadRequest {
		t.Errorf("naming a missing category: %d", rec.Code)
	}

	// A link adding nothing to the total warns, where it is made and on the
	// pool's page.
	tape, err := srv.store.CreateCategoryFrom(store.CategoryInput{Name: "Tape"})
	if err != nil {
		t.Fatal(err)
	}
	rec = doJSON(t, srv, admin, http.MethodPut, "/api/v1/categories/"+tape.ID+"/links", `{"implies":["`+f.bolts.ID+`"]}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "None of Tape's assets count in pcs, so they add nothing to M5 bolts' total.") {
		t.Errorf("an empty link: %d %s", rec.Code, rec.Body)
	}
	rec = doJSON(t, srv, admin, http.MethodGet, "/api/v1/categories/"+f.bolts.ID, "")
	if !strings.Contains(rec.Body.String(), "None of Tape's assets") {
		t.Errorf("the pool's page does not warn: %s", rec.Body)
	}
}

func TestPoolsShowWhereElseToLookToThoseWhoMaySee(t *testing.T) {
	f := newPoolFixture(t)
	srv, admin := f.srv, f.admin
	if _, err := srv.store.SetCategoryLinks(f.bolts.ID, store.CategoryLinks{IncludedBy: &[]string{f.drawer.ID, f.laser.ID}}); err != nil {
		t.Fatal(err)
	}
	poolsOf := func(cookie *http.Cookie, id string) []models.AssetPool {
		t.Helper()
		rec := doJSON(t, srv, cookie, http.MethodGet, "/api/v1/assets/"+id, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("reading %s: %d %s", id, rec.Code, rec.Body)
		}
		var body struct{ Asset models.AssetWithDetails }
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		return body.Asset.Pools
	}
	pools := poolsOf(admin, f.drawerBin)
	if len(pools) != 1 || pools[0].Total != 440 || len(pools[0].Places) != 2 || pools[0].Places[1].Name != "Laser bay bin" || pools[0].Places[1].Quantity != 300 {
		t.Errorf("the drawer's pools: %+v", pools)
	}
	// Someone who sees only the workshop sees only its bin, and a total of
	// only it.
	workshop := scopedReader(t, srv, f.workshopScope)
	pools = poolsOf(workshop, f.drawerBin)
	if len(pools) != 1 || pools[0].Total != 140 || len(pools[0].Places) != 1 {
		t.Errorf("seeing only the workshop: %+v", pools)
	}
	rec := doJSON(t, srv, workshop, http.MethodGet, "/api/v1/categories/"+f.bolts.ID, "")
	var level struct {
		ActiveCount int `json:"active_count"`
		Total       int
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &level); err != nil || level.Total != 140 || level.ActiveCount != 1 {
		t.Errorf("the pool's page seeing only the workshop: %s", rec.Body)
	}
	// The scan page says where else to look too.
	rec = doJSON(t, srv, admin, http.MethodGet, "/api/v1/scan/"+f.drawerBin, "")
	var scan ScanResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &scan); err != nil || len(scan.Asset.Pools) != 1 || scan.Asset.Pools[0].Total != 440 {
		t.Errorf("the scan: %s", rec.Body)
	}

	// Alerts use the real total: the pool is fine at 440, so emptying the
	// drawer raises nothing for it, whoever does it.
	before, err := srv.store.ListEvents(100)
	if err != nil {
		t.Fatal(err)
	}
	if rec := doJSON(t, srv, admin, http.MethodPost, "/api/v1/assets/"+f.drawerBin+"/quantity", `{"adjust":-140}`); rec.Code != http.StatusOK {
		t.Fatalf("using: %d %s", rec.Code, rec.Body)
	}
	after, err := srv.store.ListEvents(100)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range after[:len(after)-len(before)] {
		if e.Kind == models.EventAssetRunningLow || e.Kind == models.EventRunningLow {
			t.Errorf("emptying a bin in a full pool alerted %s", e.Kind)
		}
	}
}

func TestMovingStockOverTheAPI(t *testing.T) {
	f := newPoolFixture(t)
	srv, admin := f.srv, f.admin
	if _, err := srv.store.SetCategoryLinks(f.bolts.ID, store.CategoryLinks{IncludedBy: &[]string{f.drawer.ID, f.laser.ID}}); err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/assets/" + f.laserBin + "/move"
	// stock.record in both bins' scopes.
	workshopOnly, err := srv.store.CreateRole(store.RoleInput{Name: "Workshop stores", Permissions: []string{
		"workshop" + rbac.ScopeSeparator + rbac.PermAssetsRead, "workshop" + rbac.ScopeSeparator + rbac.PermStockRecord,
		"laser" + rbac.ScopeSeparator + rbac.PermAssetsRead}})
	if err != nil {
		t.Fatal(err)
	}
	u, err := srv.store.CreateNamedUser("stores@test.org", "Stores", "StoresSecret123!", true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := srv.store.Exec("INSERT INTO user_roles (user_id, role_id) VALUES (?, ?)", u.ID, workshopOnly.ID); err != nil {
		t.Fatal(err)
	}
	stores := sessionCookie(t, srv, u.ID)
	if rec := doJSON(t, srv, stores, http.MethodPost, path, `{"to":"`+f.drawerBin+`","amount":10}`); rec.Code != http.StatusForbidden {
		t.Errorf("moving out of a bin without stock.record there: %d %s", rec.Code, rec.Body)
	}
	if rec := doJSON(t, srv, admin, http.MethodPost, path, `{"to":"`+f.drawerBin+`","amount":301}`); rec.Code != http.StatusConflict {
		t.Errorf("moving more than there is: %d %s", rec.Code, rec.Body)
	}
	if rec := doJSON(t, srv, admin, http.MethodPost, path, `{"amount":1}`); rec.Code != http.StatusBadRequest {
		t.Errorf("moving nowhere: %d %s", rec.Code, rec.Body)
	}
	rec := doJSON(t, srv, admin, http.MethodPost, path, `{"to":"`+f.drawerBin+`","amount":100}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("moving: %d %s", rec.Code, rec.Body)
	}
	var moved struct{ From, To models.AssetWithDetails }
	if err := json.Unmarshal(rec.Body.Bytes(), &moved); err != nil {
		t.Fatal(err)
	}
	if moved.From.Asset.Stock.Quantity != 200 || moved.To.Asset.Stock.Quantity != 240 {
		t.Errorf("moved %d and %d", moved.From.Asset.Stock.Quantity, moved.To.Asset.Stock.Quantity)
	}
	rec = doJSON(t, srv, admin, http.MethodGet, "/api/v1/assets/"+f.drawerBin+"/history", "")
	if !strings.Contains(rec.Body.String(), `"move":{"from":{"id":"`+f.laserBin+`"`) {
		t.Errorf("the drawer's history: %s", rec.Body)
	}
}

func TestReportsByPoolAndByBin(t *testing.T) {
	f := newPoolFixture(t)
	srv, admin := f.srv, f.admin
	if _, err := srv.store.SetCategoryLinks(f.bolts.ID, store.CategoryLinks{IncludedBy: &[]string{f.drawer.ID, f.laser.ID}}); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.store.CreateAsset("Drill", nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	run := func(cookie *http.Cookie, groupBy string, filter string) reports.SectionResult {
		t.Helper()
		spec := `{"filter":` + filter + `,"sections":[{"type":"table","group_by":"` + groupBy + `","columns":[{"heading":"Place","binding":"{{asset.name}}"},{"heading":"Left","binding":"{{stock.quantity}}"}]}]}`
		rep, err := srv.store.CreateReport("By "+groupBy+filter, []byte(spec))
		if err != nil {
			t.Fatal(err)
		}
		rec := doJSON(t, srv, cookie, http.MethodGet, "/api/v1/reports/"+rep.ID+"/run", "")
		if rec.Code != http.StatusOK {
			t.Fatalf("running: %d %s", rec.Code, rec.Body)
		}
		var body struct{ Result reports.Result }
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		return body.Result.Sections[0]
	}
	// By pool: one line for the pool, with its total and threshold, and its
	// places under it; what is in no pool last.
	sec := run(admin, "pool", `{}`)
	if len(sec.Groups) != 2 || sec.Groups[0].Title != "M5 bolts" || sec.Groups[0].Pool == nil || sec.Groups[0].Pool.Total != 440 ||
		*sec.Groups[0].Pool.LowStockThreshold != 200 || len(sec.Groups[0].Rows) != 2 || sec.Groups[1].Title != reports.NotInAPool {
		t.Fatalf("by pool: %+v", sec.Groups)
	}
	if label := sec.Groups[0].Label(); label != "M5 bolts: 440 pcs (threshold 200 pcs)" {
		t.Errorf("the pool's line reads %q", label)
	}
	// A report on the pool finds its members through the links.
	sec = run(admin, "", `{"category_ids":["`+f.bolts.ID+`"]}`)
	if len(sec.Groups) != 1 || len(sec.Groups[0].Rows) != 2 {
		t.Errorf("by bin, filtered by the pool: %+v", sec.Groups)
	}
	// Someone seeing only the workshop sees its bin, and a total of it.
	sec = run(scopedReader(t, srv, f.workshopScope), "pool", `{}`)
	if len(sec.Groups) != 1 || sec.Groups[0].Pool.Total != 140 || len(sec.Groups[0].Rows) != 1 {
		t.Errorf("by pool, seeing only the workshop: %+v", sec.Groups)
	}
}
