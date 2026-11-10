package api

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// createStocked makes an asset counted by quantity through the API, and
// returns its ID.
func createStocked(t *testing.T, srv *Server, cookie *http.Cookie, body string) string {
	t.Helper()
	rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/assets", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create %s: %d %s", body, rec.Code, rec.Body)
	}
	var got struct{ Asset models.AssetWithDetails }
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	return got.Asset.Asset.ID
}

// A restock recorded from a copy made before someone else restocked is
// refused and shown, never added to theirs; uses and moves in between are
// no conflict, and asking again without the guard records it.
func TestRestockUnlessRestocked(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	adminCookie := sessionCookie(t, srv, admin.ID)
	counter := userWithPermissions(t, srv, "counter@hackspace.org", rbac.PermAssetsRead, rbac.PermStockRecord)
	tracker := userWithPermissions(t, srv, "tracker@hackspace.org", rbac.PermAssetsRead, rbac.PermStockRecord, rbac.PermAssetsTrack)

	id := createStocked(t, srv, adminCookie, `{"name":"M3 screws","stock":{"quantity":10,"unit":"pcs","low_stock_threshold":50}}`)
	other := createStocked(t, srv, adminCookie, `{"name":"M3 screws, laser bay","stock":{"quantity":30,"unit":"pcs"}}`)
	path := "/api/v1/assets/" + id + "/quantity"
	guarded := func(amount int, since time.Time) string {
		return fmt.Sprintf(`{"adjust":%d,"unless_restocked_since":%q}`, amount, since.Format(time.RFC3339Nano))
	}
	quantity := func() int {
		a, err := srv.store.GetAssetByID(id)
		if err != nil {
			t.Fatal(err)
		}
		return a.Asset.Stock.Quantity
	}

	// The copy is made a second before anything else happens, so nothing
	// shares its second.
	copied := time.Now().UTC().Add(-2 * time.Second)

	// Using some, and moving stock in from another place, are no restock.
	if rec := doJSON(t, srv, adminCookie, http.MethodPost, path, `{"adjust":-2}`); rec.Code != http.StatusOK {
		t.Fatalf("using: %d %s", rec.Code, rec.Body)
	}
	if rec := doJSON(t, srv, adminCookie, http.MethodPost, "/api/v1/assets/"+other+"/move", `{"to":"`+id+`","amount":5}`); rec.Code != http.StatusOK {
		t.Fatalf("moving: %d %s", rec.Code, rec.Body)
	}
	if rec := doJSON(t, srv, counter, http.MethodPost, path, guarded(20, copied)); rec.Code != http.StatusOK {
		t.Fatalf("a restock with only uses and moves since: %d %s", rec.Code, rec.Body)
	}
	if q := quantity(); q != 33 {
		t.Fatalf("after the first restock: %d", q)
	}

	// That restock, after a second copy was made, conflicts with a restock
	// recorded from it: nothing changes, and the conflict says what came.
	for _, who := range []struct {
		name   string
		cookie *http.Cookie
		sees   bool
	}{{"without assets.track", counter, false}, {"with assets.track", tracker, true}} {
		rec := doJSON(t, srv, who.cookie, http.MethodPost, path, guarded(50, copied))
		if rec.Code != http.StatusConflict {
			t.Fatalf("%s: a restock after another: %d %s", who.name, rec.Code, rec.Body)
		}
		var conflict RestockConflict
		if err := json.Unmarshal(rec.Body.Bytes(), &conflict); err != nil {
			t.Fatal(err)
		}
		if len(conflict.Restocked) != 1 || *conflict.Restocked[0].Payload.Change != 20 || conflict.Asset == nil || conflict.Asset.Asset.Stock.Quantity != 33 {
			t.Fatalf("%s: the conflict: %s", who.name, rec.Body)
		}
		actor := conflict.Restocked[0].Payload.Actor
		if (actor != nil) != who.sees || who.sees && (actor.Name == "" && actor.ID == "" || actor.Email != "") {
			t.Errorf("%s: who restocked: %+v", who.name, actor)
		}
	}
	if q := quantity(); q != 33 {
		t.Fatalf("a refused restock changed the quantity to %d", q)
	}

	// A copy made after it knows it: no conflict. Nor without the guard.
	if rec := doJSON(t, srv, counter, http.MethodPost, path, guarded(5, time.Now().UTC().Add(2*time.Second))); rec.Code != http.StatusOK {
		t.Fatalf("a restock from a newer copy: %d %s", rec.Code, rec.Body)
	}
	if rec := doJSON(t, srv, counter, http.MethodPost, path, `{"adjust":50}`); rec.Code != http.StatusOK {
		t.Fatalf("recorded anyway: %d %s", rec.Code, rec.Body)
	}
	if q := quantity(); q != 88 {
		t.Fatalf("after recording anyway: %d", q)
	}

	// The guard goes with a restock only, and needs stock.record as ever.
	for _, bad := range []string{
		fmt.Sprintf(`{"adjust":-1,"unless_restocked_since":%q}`, copied.Format(time.RFC3339)),
		fmt.Sprintf(`{"set":3,"unless_restocked_since":%q}`, copied.Format(time.RFC3339)),
		`{"adjust":1,"unless_restocked_since":"yesterday"}`,
	} {
		if rec := doJSON(t, srv, counter, http.MethodPost, path, bad); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: %d %s", bad, rec.Code, rec.Body)
		}
	}
	reader := userWithPermissions(t, srv, "reader@hackspace.org", rbac.PermAssetsRead)
	if rec := doJSON(t, srv, reader, http.MethodPost, path, guarded(1, copied)); rec.Code != http.StatusForbidden {
		t.Errorf("without stock.record: %d", rec.Code)
	}
}

// offlineFixture is an instance with a tools scope, a keeper who reads and
// records stock in it alone, and assets in and out of it.
type offlineFixture struct {
	srv                 *Server
	admin, keeper       *http.Cookie
	keeperID            string
	drill, bits, kettle string
	tools, kitchen      *models.Category
}

func newOfflineFixture(t *testing.T) offlineFixture {
	t.Helper()
	srv, admin, _ := setupTestServer(t)
	f := offlineFixture{srv: srv, admin: sessionCookie(t, srv, admin.ID)}
	st := srv.store
	scope, err := st.CreateScope("tools")
	if err != nil {
		t.Fatal(err)
	}
	if f.tools, err = st.CreateCategoryFrom(store.CategoryInput{Name: "Tools", IsLendable: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SetCategoryScopes(f.tools.ID, []string{scope.ID}); err != nil {
		t.Fatal(err)
	}
	if f.kitchen, err = st.CreateCategoryFrom(store.CategoryInput{Name: "Kitchen", IsConsumable: true}); err != nil {
		t.Fatal(err)
	}
	f.drill = createStocked(t, srv, f.admin, `{"name":"Drill","category_ids":["`+f.tools.ID+`"],"generator_keys":[{"generator_type":"qr","assigned_code":"HS-100"}]}`)
	f.bits = createStocked(t, srv, f.admin, `{"name":"Drill bits","category_ids":["`+f.tools.ID+`"],"stock":{"quantity":3,"unit":"pcs","low_stock_threshold":10}}`)
	f.kettle = createStocked(t, srv, f.admin, `{"name":"Kettle descaler","category_ids":["`+f.kitchen.ID+`"],"stock":{"quantity":1,"low_stock_threshold":5}}`)

	role, err := st.CreateRole(store.RoleInput{Name: "Tool keepers", Permissions: []string{"tools:" + rbac.PermAssetsRead, "tools:" + rbac.PermStockRecord}})
	if err != nil {
		t.Fatal(err)
	}
	user, err := st.CreateNamedUser("keeper@hackspace.org", "Keeper", "UserSecret123!", true)
	if err != nil {
		t.Fatal(err)
	}
	f.keeperID = user.ID
	if rec := doJSON(t, srv, f.admin, http.MethodPut, "/api/v1/users/"+user.ID+"/roles", `{"roles":[{"role_id":"`+role.ID+`"}]}`); rec.Code != http.StatusOK {
		t.Fatalf("assigning: %d %s", rec.Code, rec.Body)
	}
	f.keeper = sessionCookie(t, srv, user.ID)
	return f
}

func (f offlineFixture) copyOf(t *testing.T, cookie *http.Cookie) (OfflineCopy, string) {
	t.Helper()
	rec := doJSON(t, f.srv, cookie, http.MethodGet, "/api/v1/offline", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("the copy: %d %s", rec.Code, rec.Body)
	}
	var c OfflineCopy
	if err := json.Unmarshal(rec.Body.Bytes(), &c); err != nil {
		t.Fatal(err)
	}
	return c, rec.Body.String()
}

// The copy holds only what its person may read, people only as they may
// see them, and is refused once they cannot read assets or copies are off.
func TestOfflineCopyHoldsOnlyWhatThePersonSees(t *testing.T) {
	f := newOfflineFixture(t)
	// The drill is lent to the admin, whom the keeper may not see.
	if rec := doJSON(t, f.srv, f.admin, http.MethodPut, "/api/v1/auth/name", `{"name":"Ada Admin"}`); rec.Code != http.StatusOK {
		t.Fatalf("naming the admin: %d %s", rec.Code, rec.Body)
	}
	if rec := doJSON(t, f.srv, f.admin, http.MethodPost, "/api/v1/assets/"+f.drill+"/checkout", ""); rec.Code != http.StatusOK {
		t.Fatalf("lending: %d %s", rec.Code, rec.Body)
	}

	c, raw := f.copyOf(t, f.keeper)
	if c.UserID != f.keeperID || c.MadeAt.IsZero() || !c.Settings.Enabled || c.Settings.RefreshMinutes != 60 {
		t.Errorf("the copy's header: %+v", c)
	}
	byName := map[string]OfflineAsset{}
	for _, a := range c.Assets {
		byName[a.Name] = a
	}
	if len(c.Assets) != 2 || strings.Contains(raw, "Kettle") || strings.Contains(raw, "Kitchen") {
		t.Fatalf("the keeper's copy holds what they cannot read: %s", raw)
	}
	drill, bits := byName["Drill"], byName["Drill bits"]
	if len(drill.Codes) != 1 || drill.Codes[0] != "HS-100" || drill.Loan == nil || drill.Loan.Borrower != "" || drill.Record {
		t.Errorf("the drill: %+v", drill)
	}
	if strings.Contains(raw, "Ada") || strings.Contains(raw, "@") {
		t.Errorf("the copy names a person the keeper may not see: %s", raw)
	}
	if bits.Stock == nil || bits.Stock.Quantity != 3 || !bits.Stock.Low || !bits.Record {
		t.Errorf("the bits: %+v", bits)
	}
	if len(c.Categories) != 1 || c.Categories[0].Path != "Tools" {
		t.Errorf("categories: %+v", c.Categories)
	}
	if items := c.Shopping.Items; len(items) != 1 || items[0].AssetID != f.bits || !items[0].CanRestock || items[0].Group != "Tools" {
		t.Errorf("the keeper's shopping list: %+v", items)
	}

	// The admin, who tracks it, sees who has it, and everything.
	all, _ := f.copyOf(t, f.admin)
	for _, a := range all.Assets {
		if a.Name == "Drill" && (a.Loan == nil || a.Loan.Borrower != "Ada Admin") {
			t.Errorf("the admin's drill: %+v", a.Loan)
		}
	}
	if len(all.Assets) != 3 {
		t.Errorf("the admin's copy: %d assets", len(all.Assets))
	}

	// Copies off: refused, so the app clears what it kept.
	if rec := doJSON(t, f.srv, f.admin, http.MethodPut, "/api/v1/settings/offline", `{"enabled":false,"refresh_minutes":30,"thumbnails":false}`); rec.Code != http.StatusOK {
		t.Fatalf("turning copies off: %d %s", rec.Code, rec.Body)
	}
	if rec := doJSON(t, f.srv, f.keeper, http.MethodGet, "/api/v1/offline", ""); rec.Code != http.StatusForbidden {
		t.Errorf("a copy while they are off: %d", rec.Code)
	}
	// Without assets.read, no copy either.
	nobody := userWithPermissions(t, f.srv, "nobody@hackspace.org", rbac.PermLabelsPrint)
	if rec := doJSON(t, f.srv, nobody, http.MethodGet, "/api/v1/offline", ""); rec.Code != http.StatusForbidden {
		t.Errorf("a copy without assets.read: %d", rec.Code)
	}
	if rec := doJSON(t, f.srv, nil, http.MethodGet, "/api/v1/offline", ""); rec.Code != http.StatusUnauthorized && rec.Code != http.StatusForbidden {
		t.Errorf("a copy signed out: %d", rec.Code)
	}
}

func TestOfflineSettings(t *testing.T) {
	f := newOfflineFixture(t)
	rec := doJSON(t, f.srv, f.keeper, http.MethodGet, "/api/v1/settings/offline", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"refresh_minutes":60`) || !strings.Contains(rec.Body.String(), `"thumbnails":true`) {
		t.Fatalf("the defaults: %d %s", rec.Code, rec.Body)
	}
	if rec := doJSON(t, f.srv, f.keeper, http.MethodPut, "/api/v1/settings/offline", `{"enabled":true,"refresh_minutes":30,"thumbnails":true}`); rec.Code != http.StatusForbidden {
		t.Errorf("set without settings.manage: %d", rec.Code)
	}
	for _, bad := range []string{`{"enabled":true,"refresh_minutes":4,"thumbnails":true}`, `{"enabled":true,"refresh_minutes":10081}`, `nope`} {
		if rec := doJSON(t, f.srv, f.admin, http.MethodPut, "/api/v1/settings/offline", bad); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: %d", bad, rec.Code)
		}
	}
	rec = doJSON(t, f.srv, f.admin, http.MethodPut, "/api/v1/settings/offline", `{"enabled":true,"refresh_minutes":15,"thumbnails":false}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"refresh_minutes":15`) || !strings.Contains(rec.Body.String(), `"thumbnails":false`) {
		t.Fatalf("set: %d %s", rec.Code, rec.Body)
	}
	// Thumbnails off: the copy names no photos.
	if err := f.srv.store.SetSetting(OfflineThumbnailsSetting, "false"); err != nil {
		t.Fatal(err)
	}
	a := models.AssetWithDetails{Asset: models.Asset{ID: "a", Name: "A"}, Photos: []models.AssetPhoto{{ID: "p"}}}
	if got := offlineAsset(&a, false, false); got.Photo != "" {
		t.Errorf("a photo with thumbnails off: %+v", got)
	}
	if got := offlineAsset(&a, true, false); got.Photo != "p" {
		t.Errorf("no photo with thumbnails on: %+v", got)
	}
}

// The shopping list groups what is running low by category, with where to
// buy it, its price only for those who may see prices, and categories
// counted by items, of the scopes the requester reads.
func TestShoppingList(t *testing.T) {
	f := newOfflineFixture(t)
	st := f.srv.store
	vendor, err := st.CreateVendor(store.VendorInput{Name: "Screwfix"})
	if err != nil {
		t.Fatal(err)
	}
	pack := 10
	if _, err := st.CreateCatalogEntry(store.CatalogEntryInput{VendorID: vendor.ID, AssetID: f.bits, PartNumber: "DB-10", PackSize: &pack, PackUnit: "pcs", Price: "4.50", Currency: "GBP", Preferred: true}); err != nil {
		t.Fatal(err)
	}
	// The kitchen runs low by count, and is in the default scope.
	threshold := 5
	if _, err := st.UpdateCategoryFrom(f.kitchen.ID, store.CategoryInput{Name: "Kitchen", IsConsumable: true, LowStockThreshold: &threshold}); err != nil {
		t.Fatal(err)
	}

	list := func(cookie *http.Cookie) (ShoppingList, string) {
		t.Helper()
		rec := doJSON(t, f.srv, cookie, http.MethodGet, "/api/v1/shopping", "")
		if rec.Code != http.StatusOK {
			t.Fatalf("the list: %d %s", rec.Code, rec.Body)
		}
		var l ShoppingList
		if err := json.Unmarshal(rec.Body.Bytes(), &l); err != nil {
			t.Fatal(err)
		}
		return l, rec.Body.String()
	}

	all, _ := list(f.admin)
	var names []string
	for _, it := range all.Items {
		names = append(names, it.Group+"/"+it.Name)
	}
	if got := strings.Join(names, ", "); got != "Kitchen/Kitchen, Kitchen/Kettle descaler, Tools/Drill bits" {
		t.Fatalf("the admin's list: %s", got)
	}
	if !all.CanOrder {
		t.Error("the admin cannot start orders")
	}
	bits := all.Items[2]
	if bits.Vendor == nil || bits.Vendor.VendorName != "Screwfix" || bits.Vendor.PartNumber != "DB-10" || bits.Vendor.Price != "4.50" || bits.Level != 3 {
		t.Errorf("the bits: %+v %+v", bits, bits.Vendor)
	}
	if k := all.Items[0]; k.CategoryID != f.kitchen.ID || k.Level != 1 || k.CanRestock {
		t.Errorf("the kitchen: %+v", k)
	}

	// The keeper sees the tools alone.
	mine, raw := list(f.keeper)
	if len(mine.Items) != 1 || mine.Items[0].AssetID != f.bits || mine.CanOrder || strings.Contains(raw, "Kitchen") {
		t.Fatalf("the keeper's list: %s", raw)
	}
	// Each sees the vendor's price as the catalog shows it to them: those
	// without purchases.read where the vendor is, none.
	pricey, err := st.CreateScope("buyers")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpdateVendor(vendor.ID, store.VendorInput{Name: "Screwfix", ScopeIDs: []string{pricey.ID}}); err != nil {
		t.Fatal(err)
	}
	for _, who := range []*http.Cookie{f.admin, f.keeper, userWithPermissions(t, f.srv, "reader@hackspace.org", rbac.PermAssetsRead)} {
		l, raw := list(who)
		rec := doJSON(t, f.srv, who, http.MethodGet, "/api/v1/catalog?preferred=true", "")
		var cat struct{ Entries []models.CatalogEntry }
		_ = json.Unmarshal(rec.Body.Bytes(), &cat)
		price := ""
		for _, e := range cat.Entries {
			if e.AssetID == f.bits {
				price = e.Price
			}
		}
		got := ""
		for _, it := range l.Items {
			if it.AssetID == f.bits && it.Vendor != nil {
				got = it.Vendor.Price
			}
		}
		if got != price {
			t.Errorf("the list's price %q, the catalog's %q: %s", got, price, raw)
		}
	}
}

// How big the copy is for a few thousand assets (docs/installing.md#offline):
// it must stay a few megabytes without photos. The figures are logged, and
// recorded in the docs.
func TestOfflineCopySize(t *testing.T) {
	const n = 3000
	cats := make([]OfflineCategory, 40)
	for i := range cats {
		cats[i] = OfflineCategory{ID: fmt.Sprintf("%08d-0000-4000-8000-000000000000", i), Path: fmt.Sprintf("Workshop › Fasteners › Category %d", i)}
	}
	c := OfflineCopy{MadeAt: time.Now(), UserID: "u", Categories: cats, Assets: make([]OfflineAsset, n), Shopping: &ShoppingList{Items: []ShoppingItem{}}}
	threshold := 50
	for i := range c.Assets {
		a := models.AssetWithDetails{
			Asset:         models.Asset{ID: fmt.Sprintf("%08d-1111-4111-8111-111111111111", i), Name: fmt.Sprintf("M%d stainless hex bolts, 20 mm (%d)", i%12, i), Condition: models.ConditionOK},
			Categories:    []models.Category{{ID: cats[i%40].ID}},
			GeneratorKeys: []models.AssetGeneratorKey{{GeneratorType: "qr", AssignedCode: fmt.Sprintf("HS-%05d", i)}},
			Location:      []models.LinkedAsset{{Name: "Workshop"}, {Name: fmt.Sprintf("Shelf %d", i%30)}, {Name: fmt.Sprintf("Drawer %d", i%12)}},
			Photos:        []models.AssetPhoto{{ID: fmt.Sprintf("%08d-2222-4222-8222-222222222222", i)}},
		}
		if i%2 == 0 {
			a.Asset.Stock = &models.AssetStock{Quantity: 240, Unit: "pcs", LowStockThreshold: &threshold}
		}
		c.Assets[i] = offlineAsset(&a, true, true)
	}
	raw, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	var gz bytes.Buffer
	w := gzip.NewWriter(&gz)
	_, _ = w.Write(raw)
	_ = w.Close()
	t.Logf("%d assets: %d bytes, %.0f bytes each; %d gzipped", n, len(raw), float64(len(raw))/n, gz.Len())
	if len(raw) > 2_000_000 {
		t.Errorf("%d assets take %d bytes, more than 2 MB", n, len(raw))
	}
}
