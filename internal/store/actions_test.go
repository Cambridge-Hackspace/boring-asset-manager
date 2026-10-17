package store

import (
	"errors"
	"reflect"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
)

func TestDefiningActions(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	a, err := st.SaveAction("", ActionInput{Key: "identify", Label: " Identify ", Confirm: ""})
	if err != nil || a.Key != "identify" || a.Label != "Identify" {
		t.Fatalf("saving: %+v, %v", a, err)
	}
	for name, in := range map[string]ActionInput{
		"no key":          {Label: "X"},
		"a capital":       {Key: "Identify", Label: "X"},
		"a space":         {Key: "light up", Label: "X"},
		"a leading digit": {Key: "1st", Label: "X"},
		"no label":        {Key: "x"},
	} {
		if _, err := st.SaveAction("", in); !errors.Is(err, ErrInvalidAction) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := st.SaveAction("", ActionInput{Key: "identify", Label: "Again"}); !errors.Is(err, ErrActionKeyExists) {
		t.Errorf("a taken key: %v", err)
	}
	if b, err := st.SaveAction(a.ID, ActionInput{Key: "identify", Label: "Light up", Confirm: "Light it?"}); err != nil || b.Label != "Light up" || b.Confirm != "Light it?" {
		t.Errorf("updating: %+v, %v", b, err)
	}
	if _, err := st.SaveAction("nope", ActionInput{Key: "x", Label: "X"}); !errors.Is(err, ErrActionNotFound) {
		t.Errorf("updating a missing action: %v", err)
	}
	if err := st.DeleteAction(a.ID); err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteAction(a.ID); !errors.Is(err, ErrActionNotFound) {
		t.Errorf("deleting twice: %v", err)
	}
}

func TestCategoriesOfferActions(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	identify, _ := st.SaveAction("", ActionInput{Key: "identify", Label: "Identify"})
	open, _ := st.SaveAction("", ActionInput{Key: "open-drawer", Label: "Open drawer", Confirm: "Open it?"})
	cabinet, _ := st.CreateCategory("Cabinet", nil, "", false, false)
	drawers, _ := st.CreateCategory("Drawers", &cabinet.ID, "", false, false)
	small, _ := st.CreateCategory("Small drawers", &drawers.ID, "", false, false)
	if _, err := st.SetCategoryActions(cabinet.ID, []models.CategoryAction{{ActionID: identify.ID, Cascades: true}, {ActionID: open.ID}}); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.CategoryActions(cabinet.ID); len(got) != 2 {
		t.Errorf("the cabinet's actions: %+v", got)
	}
	if _, err := st.SetCategoryActions(drawers.ID, []models.CategoryAction{{ActionID: "nope"}}); !errors.Is(err, ErrActionNotFound) {
		t.Errorf("offering a missing action: %v", err)
	}
	if _, err := st.SetCategoryActions(drawers.ID, []models.CategoryAction{{ActionID: open.ID}, {ActionID: open.ID}}); !errors.Is(err, ErrInvalidAction) {
		t.Errorf("offering an action twice: %v", err)
	}
	if _, err := st.CategoryActions("nope"); !errors.Is(err, ErrCategoryNotFound) {
		t.Errorf("a missing category: %v", err)
	}

	// The cabinet's own assets get both; its subcategories' only what
	// cascades.
	own, _ := st.CreateAsset("Cabinet A", []string{cabinet.ID}, nil, nil)
	deep, _ := st.CreateAsset("Drawer A3", []string{small.ID}, nil, nil)
	keys := func(a []models.OfferedAction) []string {
		var out []string
		for _, o := range a {
			out = append(out, o.Key)
		}
		return out
	}
	if got := keys(own.Actions); !reflect.DeepEqual(got, []string{"identify", "open-drawer"}) {
		t.Errorf("the cabinet's: %v", got)
	}
	if got := keys(deep.Actions); !reflect.DeepEqual(got, []string{"identify"}) {
		t.Errorf("a small drawer's: %v", got)
	}
	if own.Actions[1].Confirm != "Open it?" {
		t.Errorf("the question: %+v", own.Actions[1])
	}
	plain, _ := st.CreateAsset("Rag", nil, nil, nil)
	if plain.Actions == nil || len(plain.Actions) != 0 {
		t.Errorf("an asset offered nothing: %#v", plain.Actions)
	}
	// Deleting an action takes it off every category.
	if err := st.DeleteAction(identify.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.GetAssetByID(deep.Asset.ID); len(got.Actions) != 0 {
		t.Errorf("after deleting: %+v", got.Actions)
	}
}

func TestPressingAnAction(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	admin, _ := st.CreateAdminUser("admin@test.org", "AdminSecret123!")
	identify, _ := st.SaveAction("", ActionInput{Key: "identify", Label: "Identify"})
	parts, _ := st.CreateCategory("Parts", nil, "", false, false)
	if _, err := st.SetCategoryActions(parts.ID, []models.CategoryAction{{ActionID: identify.ID, Cascades: true}}); err != nil {
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

	id, err := st.As(admin.ID).RecordAssetAction(resistors.Asset.ID, "identify")
	if err != nil || id == "" {
		t.Fatalf("pressing: %q, %v", id, err)
	}
	events, _, _ := st.ListAssetEvents(resistors.Asset.ID, "", 1)
	if len(events) != 1 || events[0].ID != id || events[0].Kind != models.EventAssetAction || events[0].Payload.Actor.ID != admin.ID {
		t.Fatalf("the event: %+v", events)
	}
	act := events[0].Payload.Action
	want := &models.EventAction{
		Key: "identify", Label: "Identify",
		Asset: models.EventThing{ID: resistors.Asset.ID, Name: "10k resistors", Codes: map[string]string{}, Fields: map[string]string{}},
		Location: []models.EventThing{
			{ID: cabinet.Asset.ID, Name: "Cabinet A", Codes: map[string]string{"qr": "CAB-A"}, Fields: map[string]string{}},
			{ID: drawer.Asset.ID, Name: "Drawer A3", Codes: map[string]string{"qr": "DRW-A3"}, Fields: map[string]string{"led": "12"}},
		},
	}
	if !reflect.DeepEqual(act, want) {
		t.Errorf("the action:\n%+v\nwant\n%+v", act, want)
	}
	// Channels can subscribe: it is not for history only.
	if models.HistoryOnly(models.EventAssetAction) {
		t.Error("asset.action is history only")
	}

	if _, err := st.RecordAssetAction(drawer.Asset.ID, "identify"); !errors.Is(err, ErrActionNotOffered) {
		t.Errorf("an action not offered: %v", err)
	}
	if err := st.DeleteAsset(resistors.Asset.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.RecordAssetAction(resistors.Asset.ID, "identify"); !errors.Is(err, ErrAssetArchived) {
		t.Errorf("an archived asset: %v", err)
	}
}

// TestActingIsGrantedLikeReading checks migration 026 gave assets.act to
// the roles that read assets, in their scopes, but not the public role.
func TestActingIsGrantedLikeReading(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	if !rbac.Scopable(rbac.PermAssetsAct) {
		t.Error("assets.act is not scopable")
	}
	public, err := st.PublicPermissions()
	if err != nil {
		t.Fatal(err)
	}
	for key := range public {
		if _, perm := rbac.SplitKey(key); perm == rbac.PermAssetsAct {
			t.Errorf("the public role holds %s", key)
		}
	}
	var n int
	if err := st.QueryRow(`SELECT COUNT(*) FROM role_permissions rp JOIN permissions p ON p.id = rp.permission_id
		JOIN roles r ON r.id = rp.role_id WHERE p.permission_key = 'assets.act' AND r.name = 'public'`).Scan(&n); err != nil || n != 0 {
		t.Errorf("public grants: %d, %v", n, err)
	}
}
