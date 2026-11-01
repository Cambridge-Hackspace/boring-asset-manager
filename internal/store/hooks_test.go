package store

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/flows"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store/migrations"
)

func TestDefiningHooks(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	a, err := st.SaveHook("", HookInput{Key: "identify", Label: " Identify ", Confirm: ""})
	if err != nil || a.Key != "identify" || a.Label != "Identify" {
		t.Fatalf("saving: %+v, %v", a, err)
	}
	for name, in := range map[string]HookInput{
		"no key":          {Label: "X"},
		"a capital":       {Key: "Identify", Label: "X"},
		"a space":         {Key: "light up", Label: "X"},
		"a leading digit": {Key: "1st", Label: "X"},
		"no label":        {Key: "x"},
	} {
		if _, err := st.SaveHook("", in); !errors.Is(err, ErrInvalidHook) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := st.SaveHook("", HookInput{Key: "identify", Label: "Again"}); !errors.Is(err, ErrHookKeyExists) {
		t.Errorf("a taken key: %v", err)
	}
	if b, err := st.SaveHook(a.ID, HookInput{Key: "identify", Label: "Light up", Confirm: "Light it?"}); err != nil || b.Label != "Light up" || b.Confirm != "Light it?" {
		t.Errorf("updating: %+v, %v", b, err)
	}
	if _, err := st.SaveHook("nope", HookInput{Key: "x", Label: "X"}); !errors.Is(err, ErrHookNotFound) {
		t.Errorf("updating a missing hook: %v", err)
	}
	if err := st.DeleteHook(a.ID); err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteHook(a.ID); !errors.Is(err, ErrHookNotFound) {
		t.Errorf("deleting twice: %v", err)
	}
}

func TestCategoriesOfferHooks(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	identify, _ := st.SaveHook("", HookInput{Key: "identify", Label: "Identify"})
	open, _ := st.SaveHook("", HookInput{Key: "open-drawer", Label: "Open drawer", Confirm: "Open it?"})
	cabinet, _ := st.CreateCategory("Cabinet", nil, "", false, false)
	drawers, _ := st.CreateCategory("Drawers", &cabinet.ID, "", false, false)
	small, _ := st.CreateCategory("Small drawers", &drawers.ID, "", false, false)
	if _, err := st.SetCategoryHooks(cabinet.ID, []models.CategoryHook{{HookID: identify.ID, Cascades: true}, {HookID: open.ID}}); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.CategoryHooks(cabinet.ID); len(got) != 2 {
		t.Errorf("the cabinet's hooks: %+v", got)
	}
	if _, err := st.SetCategoryHooks(drawers.ID, []models.CategoryHook{{HookID: "nope"}}); !errors.Is(err, ErrHookNotFound) {
		t.Errorf("offering a missing hook: %v", err)
	}
	if _, err := st.SetCategoryHooks(drawers.ID, []models.CategoryHook{{HookID: open.ID}, {HookID: open.ID}}); !errors.Is(err, ErrInvalidHook) {
		t.Errorf("offering a hook twice: %v", err)
	}
	if _, err := st.CategoryHooks("nope"); !errors.Is(err, ErrCategoryNotFound) {
		t.Errorf("a missing category: %v", err)
	}

	// The cabinet's own assets get both; its subcategories' only what
	// cascades.
	own, _ := st.CreateAsset("Cabinet A", []string{cabinet.ID}, nil, nil)
	deep, _ := st.CreateAsset("Drawer A3", []string{small.ID}, nil, nil)
	keys := func(a []models.OfferedHook) []string {
		var out []string
		for _, o := range a {
			out = append(out, o.Key)
		}
		return out
	}
	if got := keys(own.Hooks); !reflect.DeepEqual(got, []string{"identify", "open-drawer"}) {
		t.Errorf("the cabinet's: %v", got)
	}
	if got := keys(deep.Hooks); !reflect.DeepEqual(got, []string{"identify"}) {
		t.Errorf("a small drawer's: %v", got)
	}
	if own.Hooks[1].Confirm != "Open it?" {
		t.Errorf("the question: %+v", own.Hooks[1])
	}
	plain, _ := st.CreateAsset("Rag", nil, nil, nil)
	if plain.Hooks == nil || len(plain.Hooks) != 0 {
		t.Errorf("an asset offered nothing: %#v", plain.Hooks)
	}
	// Deleting a hook takes it off every category.
	if err := st.DeleteHook(identify.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.GetAssetByID(deep.Asset.ID); len(got.Hooks) != 0 {
		t.Errorf("after deleting: %+v", got.Hooks)
	}
}

func TestPressingAnHook(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	admin, _ := st.CreateAdminUser("admin@test.org", "AdminSecret123!")
	identify, _ := st.SaveHook("", HookInput{Key: "identify", Label: "Identify"})
	parts, _ := st.CreateCategory("Parts", nil, "", false, false)
	if _, err := st.SetCategoryHooks(parts.ID, []models.CategoryHook{{HookID: identify.ID, Cascades: true}}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateField("led", "LED", "number", nil); err != nil {
		t.Fatal(err)
	}
	cabinet, _ := st.CreateAsset("Cabinet A", nil, []models.AssetGeneratorKey{{GeneratorType: "qr", AssignedCode: "CAB-A"}}, nil)
	drawer, _ := st.CreateAsset("Drawer A3", nil, []models.AssetGeneratorKey{{GeneratorType: "qr", AssignedCode: "DRW-A3"}}, map[string]string{"led": "12"})
	resistors, _ := st.CreateAsset("10k resistors", []string{parts.ID}, nil, nil)
	contains := relation(t, st, "contains")
	link(t, st, cabinet.Asset.ID, contains, drawer.Asset.ID, true)
	link(t, st, drawer.Asset.ID, contains, resistors.Asset.ID, true)

	id, err := st.As(admin.ID).RecordAssetHook(resistors.Asset.ID, "identify")
	if err != nil || id == "" {
		t.Fatalf("pressing: %q, %v", id, err)
	}
	events, _, _ := st.ListAssetEvents(resistors.Asset.ID, "", 1)
	if len(events) != 1 || events[0].ID != id || events[0].Kind != models.EventAssetHook || events[0].Payload.Actor.ID != admin.ID {
		t.Fatalf("the event: %+v", events)
	}
	act := events[0].Payload.Hook
	want := &models.EventHook{
		Key: "identify", Label: "Identify",
		Asset: models.EventThing{ID: resistors.Asset.ID, Name: "10k resistors", Codes: map[string]string{}, Fields: map[string]string{}},
		Location: []models.EventThing{
			{ID: cabinet.Asset.ID, Name: "Cabinet A", Codes: map[string]string{"qr": "CAB-A"}, Fields: map[string]string{}},
			{ID: drawer.Asset.ID, Name: "Drawer A3", Codes: map[string]string{"qr": "DRW-A3"}, Fields: map[string]string{"led": "12"}},
		},
	}
	if !reflect.DeepEqual(act, want) {
		t.Errorf("the hook:\n%+v\nwant\n%+v", act, want)
	}
	// Channels can subscribe: it is not for history only.
	if models.HistoryOnly(models.EventAssetHook) {
		t.Error("asset.hook is history only")
	}

	if _, err := st.RecordAssetHook(drawer.Asset.ID, "identify"); !errors.Is(err, ErrHookNotOffered) {
		t.Errorf("a hook not offered: %v", err)
	}
	if err := st.DeleteAsset(resistors.Asset.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.RecordAssetHook(resistors.Asset.ID, "identify"); !errors.Is(err, ErrAssetArchived) {
		t.Errorf("an archived asset: %v", err)
	}
}

// TestHookingIsGrantedLikeReading checks migration 026 gave assets.act,
// assets.hook since migration 049, to the roles that read assets, in their
// scopes, but not the public role.
func TestHookingIsGrantedLikeReading(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	if !rbac.Scopable(rbac.PermAssetsHook) {
		t.Error("assets.hook is not scopable")
	}
	public, err := st.PublicPermissions()
	if err != nil {
		t.Fatal(err)
	}
	for key := range public {
		if _, perm := rbac.SplitKey(key); perm == rbac.PermAssetsHook {
			t.Errorf("the public role holds %s", key)
		}
	}
	var n int
	if err := st.QueryRow(`SELECT COUNT(*) FROM role_permissions rp JOIN permissions p ON p.id = rp.permission_id
		JOIN roles r ON r.id = rp.role_id WHERE p.permission_key = 'assets.hook' AND r.name = 'public'`).Scan(&n); err != nil || n != 0 {
		t.Errorf("public grants: %d, %v", n, err)
	}
}

// Migration 049 carries over what was stored under the names hooks had
// when they were actions (#173): roles holding assets.act, everywhere or
// in scopes, hold assets.hook; channels following asset.action follow
// asset.hook; action events are hook events; flows whose Change steps
// pressed an action trigger the same hook; and scan templates offering an
// asset's actions offer its hooks. Run again, it changes nothing.
func TestMigratingCarriesActionsOverToHooks(t *testing.T) {
	st := openBlank(t)
	if err := migrations.MigrateTo(st.DB, st.Dialect, migrations.HooksVersion-1); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	var act string
	if err := st.QueryRow("SELECT id FROM permissions WHERE permission_key = 'assets.act'").Scan(&act); err != nil {
		t.Fatal(err)
	}
	flow := `{"version":1,"category_ids":["c1"],"steps":[{"id":"find","kind":"find","find_by":["list"]},` +
		`{"id":"change","kind":"change","changes":[{"kind":"condition","condition":"ok"},{"kind":"action","action":"identify"}]},{"id":"done","kind":"done"}]}`
	for _, q := range []struct {
		sql  string
		args []any
	}{
		{"INSERT INTO roles (id, name, description) VALUES (?, ?, ?)", []any{"r1", "Members", ""}},
		{"INSERT INTO roles (id, name, description) VALUES (?, ?, ?)", []any{"r2", "Keepers", ""}},
		{"INSERT INTO scopes (id, name, created_at) VALUES (?, ?, ?)", []any{"s1", "tools", now}},
		{"INSERT INTO role_permissions (role_id, permission_id) VALUES (?, ?)", []any{"r1", act}},
		{"INSERT INTO role_permission_scopes (role_id, permission_id, scope_id) VALUES (?, ?, ?)", []any{"r2", act, "s1"}},
		{"INSERT INTO notification_channels (id, name, driver, options, enabled, events, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
			[]any{"n1", "Cabinet", "mqtt", "{}", true, `["asset.running_low","asset.action"]`, now, now}},
		{"INSERT INTO events (id, kind, payload, created_at) VALUES (?, ?, ?, ?)",
			[]any{"e1", "asset.action", `{"trigger":"manual","action":{"key":"identify","label":"Identify","asset":{"id":"a1","name":"Drawer","codes":{},"fields":{}},"location":[]}}`, now}},
		{"INSERT INTO events (id, kind, payload, created_at) VALUES (?, ?, ?, ?)", []any{"e2", "asset.overdue", `{"trigger":"schedule"}`, now}},
		{"INSERT INTO flows (id, slug, name, definition, enabled, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)",
			[]any{"f1", "light-it", "Light it", flow, true, now, now}},
		{"INSERT INTO reports (id, name, spec, kind, is_default, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)",
			[]any{"t1", "Drawers", `{"sections":[{"type":"actions","actions":["open","actions","flows"]}]}`, "scan", false, now, now}},
	} {
		if _, err := st.Exec(q.sql, q.args...); err != nil {
			t.Fatalf("%s: %v", q.sql, err)
		}
	}
	for run := 0; run < 2; run++ {
		if err := migrations.MigrateTo(st.DB, st.Dialect, 0); err != nil {
			t.Fatal(err)
		}
		if run == 1 {
			// run again, as a Go migration may be
			if _, err := st.Exec("DELETE FROM schema_migrations WHERE version = ?", migrations.HooksVersion); err != nil {
				t.Fatal(err)
			}
			if err := migrations.MigrateTo(st.DB, st.Dialect, 0); err != nil {
				t.Fatal(err)
			}
		}
		var n int
		if err := st.QueryRow("SELECT COUNT(*) FROM permissions WHERE permission_key = 'assets.act'").Scan(&n); err != nil || n != 0 {
			t.Errorf("assets.act is left: %d %v", n, err)
		}
		for role, query := range map[string]string{
			"r1": "SELECT COUNT(*) FROM role_permissions rp JOIN permissions p ON p.id = rp.permission_id WHERE rp.role_id = 'r1' AND p.permission_key = 'assets.hook'",
			"r2": "SELECT COUNT(*) FROM role_permission_scopes rps JOIN permissions p ON p.id = rps.permission_id WHERE rps.role_id = 'r2' AND rps.scope_id = 's1' AND p.permission_key = 'assets.hook'",
		} {
			if err := st.QueryRow(query).Scan(&n); err != nil || n != 1 {
				t.Errorf("%s holds assets.hook %d times, %v", role, n, err)
			}
		}
		var events, kind, payload, definition, spec string
		if err := st.QueryRow("SELECT events FROM notification_channels WHERE id = 'n1'").Scan(&events); err != nil || events != `["asset.running_low","asset.hook"]` {
			t.Errorf("the channel follows %s, %v", events, err)
		}
		if err := st.QueryRow("SELECT kind, payload FROM events WHERE id = 'e1'").Scan(&kind, &payload); err != nil || kind != models.EventAssetHook ||
			!strings.Contains(payload, `"hook":{"key":"identify"`) || strings.Contains(payload, `"action"`) {
			t.Errorf("the event: %s %s %v", kind, payload, err)
		}
		if err := st.QueryRow("SELECT kind FROM events WHERE id = 'e2'").Scan(&kind); err != nil || kind != models.EventAssetOverdue {
			t.Errorf("another event: %s %v", kind, err)
		}
		if err := st.QueryRow("SELECT definition FROM flows WHERE id = 'f1'").Scan(&definition); err != nil {
			t.Fatal(err)
		}
		var d flows.Definition
		if err := json.Unmarshal([]byte(definition), &d); err != nil || len(d.Steps) != 3 || !reflect.DeepEqual(d.Steps[1].Changes,
			[]flows.Change{{Kind: flows.ChangeCondition, Condition: "ok"}, {Kind: flows.ChangeHook, Hook: "identify"}}) {
			t.Errorf("the flow: %s %v", definition, err)
		}
		if err := st.QueryRow("SELECT spec FROM reports WHERE id = 't1'").Scan(&spec); err != nil || !strings.Contains(spec, `"actions":["open","hooks","flows"]`) {
			t.Errorf("the scan template: %s %v", spec, err)
		}
	}
	// what roles hold is read by the new name
	role, err := st.GetRole("r2")
	if err != nil || !reflect.DeepEqual(role.Permissions, []string{"tools:" + rbac.PermAssetsHook}) {
		t.Errorf("r2: %+v, %v", role, err)
	}
}
