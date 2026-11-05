package store

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/fields"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
)

// sealedAs stands in for sealing with [auth] secret_key: what is sealed is
// kept as given, marked, so a test can tell it was sealed.
func sealedAs(id, secret string) (string, error) { return "sealed:" + id + ":" + secret, nil }

func TestIncomingHooksKeepTheirSecretsUnreadable(t *testing.T) {
	st := setupTestStore(t)
	role, err := st.CreateRole(RoleInput{Name: "Counters", Permissions: []string{rbac.PermAssetsRead, rbac.PermStockRecord, rbac.PermSettingsManage}})
	if err != nil {
		t.Fatal(err)
	}
	tools, err := st.CreateScope("tools")
	if err != nil {
		t.Fatal(err)
	}
	in := IncomingInput{Name: " Scale ", Enabled: true, Auth: models.IncomingBearer, Action: models.IncomingQuantity,
		Settings: models.IncomingSettings{Asset: models.IncomingAsset{By: models.IncomingAssetCode, Path: "code"}},
		Roles:    []models.RoleAssignment{{RoleID: role.ID, ScopeIDs: []string{tools.ID}}}}
	h, secret, err := st.CreateIncomingHook(in, "")
	if err != nil {
		t.Fatal(err)
	}
	if h.Name != "Scale" || !h.HasSecret || h.RateLimit != DefaultIncomingRate || len(h.ID) != 48 || !strings.HasPrefix(secret, IncomingSecretPrefix) {
		t.Fatalf("made %+v, %q", h, secret)
	}
	if h.Asset.Path != "code" || len(h.Roles) != 1 || h.Roles[0].ScopeIDs[0] != tools.ID {
		t.Errorf("settings and roles: %+v", h)
	}

	// The bearer secret is kept as its hash, never itself.
	var hash string
	if err := st.QueryRow("SELECT secret_hash FROM incoming_hooks WHERE id = ?", h.ID).Scan(&hash); err != nil {
		t.Fatal(err)
	}
	if hash == secret || strings.Contains(hash, secret) || len(hash) != 64 {
		t.Errorf("kept as %q", hash)
	}
	cred, err := st.IncomingHookCredential(h.ID)
	if err != nil || !cred.MatchesBearer(secret) || cred.MatchesBearer(secret+"x") || cred.MatchesBearer("") {
		t.Errorf("matching: %+v %v", cred, err)
	}

	// What it holds is its role's, narrowed: no settings.manage, which is
	// not scopable, and stock.record in tools only.
	keys, err := st.IncomingHookKeys(h.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !keys["tools:"+rbac.PermStockRecord] || keys[rbac.PermStockRecord] || keys[rbac.PermSettingsManage] {
		t.Errorf("keys: %v", keys)
	}
	if would, err := st.AssignmentKeys(in.Roles); err != nil || len(would) != len(keys) {
		t.Errorf("would hold %v, holds %v: %v", would, keys, err)
	}
	// The scope is in use by it.
	if err := st.DeleteScope(tools.ID); !errors.Is(err, ErrScopeInUse) {
		t.Errorf("deleting its scope: %v", err)
	}

	// Signing: the secret itself is kept, sealed. A change of how it
	// authenticates gives it a new secret; the old no longer matches.
	in.Auth, in.Seal = models.IncomingHMAC, sealedAs
	changed, next, err := st.UpdateIncomingHook(h.ID, in, false)
	if err != nil || next == "" || next == secret || changed.Auth != models.IncomingHMAC {
		t.Fatalf("changed: %+v %q %v", changed, next, err)
	}
	if cred, err = st.IncomingHookCredential(h.ID); err != nil || cred.Hash != "" || cred.Sealed != "sealed:"+h.ID+":"+next || cred.MatchesBearer(secret) {
		t.Errorf("sealed: %+v %v", cred, err)
	}
	// Left alone, the secret is kept.
	in.Name = "Shelf scale"
	if _, again, err := st.UpdateIncomingHook(h.ID, in, false); err != nil || again != "" {
		t.Errorf("kept: %q %v", again, err)
	}
	if _, again, err := st.UpdateIncomingHook(h.ID, in, true); err != nil || again == "" || again == next {
		t.Errorf("a new one asked for: %q %v", again, err)
	}

	// What cannot be saved.
	for what, bad := range map[string]IncomingInput{
		"no name":      {Auth: models.IncomingBearer, Action: models.IncomingQuantity, Roles: in.Roles},
		"no auth":      {Name: "x", Auth: "basic", Action: models.IncomingQuantity, Roles: in.Roles},
		"no action":    {Name: "x", Auth: models.IncomingBearer, Action: "delete", Roles: in.Roles},
		"no role":      {Name: "x", Auth: models.IncomingBearer, Action: models.IncomingQuantity},
		"too fast":     {Name: "x", Auth: models.IncomingBearer, Action: models.IncomingQuantity, Roles: in.Roles, RateLimit: MaxIncomingRate + 1},
		"cannot seal":  {Name: "x", Auth: models.IncomingHMAC, Action: models.IncomingQuantity, Roles: in.Roles},
		"unknown role": {Name: "x", Auth: models.IncomingBearer, Action: models.IncomingQuantity, Roles: []models.RoleAssignment{{RoleID: "nope"}}},
	} {
		if _, _, err := st.CreateIncomingHook(bad, ""); err == nil {
			t.Errorf("%s: saved", what)
		}
	}
	list, err := st.ListIncomingHooks()
	if err != nil || len(list) != 1 || list[0].Name != "Shelf scale" {
		t.Errorf("list: %+v %v", list, err)
	}

	if err := st.DeleteIncomingHook(h.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetIncomingHook(h.ID); !errors.Is(err, ErrIncomingNotFound) {
		t.Errorf("after deleting: %v", err)
	}
	if err := st.DeleteIncomingHook(h.ID); !errors.Is(err, ErrIncomingNotFound) {
		t.Errorf("deleting again: %v", err)
	}
	if err := st.DeleteScope(tools.ID); err != nil {
		t.Errorf("its scope, once it is gone: %v", err)
	}
}

func TestAnIncomingDeliveryIsTakenOnce(t *testing.T) {
	st := setupTestStore(t)
	role, err := st.CreateRole(RoleInput{Name: "Hookers", Permissions: []string{rbac.PermAssetsHook}})
	if err != nil {
		t.Fatal(err)
	}
	mk := func(name string) *models.IncomingHook {
		h, _, err := st.CreateIncomingHook(IncomingInput{Name: name, Auth: models.IncomingHMAC, Action: models.IncomingTriggerHook, Seal: sealedAs,
			Settings: models.IncomingSettings{Hook: "identify", Asset: models.IncomingAsset{By: models.IncomingAssetFixed, AssetID: "a"}},
			Roles:    []models.RoleAssignment{{RoleID: role.ID}}}, "")
		if err != nil {
			t.Fatal(err)
		}
		return h
	}
	a, b := mk("A"), mk("B")
	until := time.Now().Add(5 * time.Minute)
	if err := st.ClaimIncomingDelivery(a.ID, "d1", until); err != nil {
		t.Fatal(err)
	}
	if err := st.ClaimIncomingDelivery(a.ID, "d1", until); !errors.Is(err, ErrDeliveryUsed) {
		t.Errorf("again: %v", err)
	}
	if err := st.ClaimIncomingDelivery(b.ID, "d1", until); err != nil {
		t.Errorf("another hook's: %v", err)
	}
	// Those too old to be taken anyway are forgotten.
	if err := st.ClaimIncomingDelivery(a.ID, "old", time.Now().Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := st.ClaimIncomingDelivery(a.ID, "d2", until); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := st.QueryRow("SELECT COUNT(*) FROM incoming_deliveries WHERE delivery_id = ?", "old").Scan(&n); err != nil || n != 0 {
		t.Errorf("old kept: %d %v", n, err)
	}
	if err := st.DeleteIncomingHook(a.ID); err != nil {
		t.Fatal(err)
	}
	if err := st.QueryRow("SELECT COUNT(*) FROM incoming_deliveries WHERE hook_id = ?", a.ID).Scan(&n); err != nil || n != 0 {
		t.Errorf("a deleted hook's kept: %d %v", n, err)
	}
}

func TestChangesByAnIncomingHookAreItsOwn(t *testing.T) {
	st := setupTestStore(t)
	role, err := st.CreateRole(RoleInput{Name: "Counters", Permissions: []string{rbac.PermStockRecord}})
	if err != nil {
		t.Fatal(err)
	}
	h, _, err := st.CreateIncomingHook(IncomingInput{Name: "Scale", Auth: models.IncomingBearer, Action: models.IncomingQuantity,
		Settings: models.IncomingSettings{Asset: models.IncomingAsset{By: models.IncomingAssetFixed, AssetID: "a"}},
		Roles:    []models.RoleAssignment{{RoleID: role.ID}}}, "")
	if err != nil {
		t.Fatal(err)
	}
	ten := 10
	low := 9
	a, err := st.CreateAssetWithStock("Screws", nil, nil, nil, &StockInput{Quantity: &ten, LowStockThreshold: &low})
	if err != nil {
		t.Fatal(err)
	}
	log := &EventLog{}
	if _, err := st.AsIncoming(h.ID).Logging(log).AdjustQuantity(a.Asset.ID, -2, nil); err != nil {
		t.Fatal(err)
	}
	events, _, err := st.ListAssetEvents(a.Asset.ID, "", 10)
	if err != nil {
		t.Fatal(err)
	}
	// A use, and running low because of it, both logged and both the hook's.
	ids := log.IDs()
	if len(ids) != 2 {
		t.Fatalf("logged %v", ids)
	}
	logged := map[string]bool{ids[0]: true, ids[1]: true}
	seen := 0
	for _, e := range events {
		if !logged[e.ID] {
			continue
		}
		seen++
		if e.Payload.Actor == nil || e.Payload.Actor.ID != h.ID || !e.Payload.Actor.Incoming || e.Payload.Actor.Token || e.Payload.Actor.Name != "Scale" {
			t.Errorf("%s by %+v", e.Kind, e.Payload.Actor)
		}
	}
	if seen != 2 {
		t.Errorf("logged events not recorded: %v", ids)
	}
	// A view of the store logs nothing unless asked.
	if _, err := st.AsIncoming(h.ID).AdjustQuantity(a.Asset.ID, -1, nil); err != nil {
		t.Fatal(err)
	}
	if len(log.IDs()) != 2 {
		t.Errorf("logged by another view: %v", log.IDs())
	}
}

func TestAssetsByFieldValue(t *testing.T) {
	st := setupTestStore(t)
	bin, err := st.CreateField("bin", "Bin", fields.TypeText, nil)
	if err != nil {
		t.Fatal(err)
	}
	cat, err := st.CreateCategory("Parts", nil, "", false, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AssignCategoryField(cat.ID, bin.ID, false); err != nil {
		t.Fatal(err)
	}
	a3, err := st.CreateAsset("Resistor", []string{cat.ID}, nil, map[string]string{"bin": "A3"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateAsset("Capacitor", []string{cat.ID}, nil, map[string]string{"bin": "a3"}); err != nil {
		t.Fatal(err)
	}
	gone, err := st.CreateAsset("Diode", []string{cat.ID}, nil, map[string]string{"bin": "A3"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteAsset(gone.Asset.ID); err != nil {
		t.Fatal(err)
	}
	// Exactly, among active assets.
	if ids, err := st.AssetsByFieldValue("bin", "A3"); err != nil || len(ids) != 1 || ids[0] != a3.Asset.ID {
		t.Errorf("A3: %v %v", ids, err)
	}
	if ids, err := st.AssetsByFieldValue("other", "A3"); err != nil || len(ids) != 0 {
		t.Errorf("another field: %v %v", ids, err)
	}
}
