package store

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
)

// A switch, drawn down too through the state axis in the library.
const (
	stateAxis = `axis "state" { variant "down" { part "flag" { tile 0 0 1 0.3 0.3 } } }`
	switchSrc = `icon "switch" { category "net"; footprint 1 1 1; axes "state"; part "box" { box 0 0 0 1 1 1 } }`
)

// iconWorld is Networking, drawn as a switch with a state axis, and its
// subcategory Routers, which has no icon of its own.
type iconWorld struct {
	st                  *Store
	c                   IconCompiler
	networking, routers string
}

func newIconWorld(t *testing.T) iconWorld {
	t.Helper()
	st := setupTestStore(t)
	c := compilerFor(t)
	if _, save, err := st.CreateIconLibraryFile(context.Background(), c, "state.ilx", stateAxis); err != nil || !save.Published {
		t.Fatalf("%+v %v", save, err)
	}
	net, err := st.CreateCategoryFrom(CategoryInput{Name: "Networking", ColorHex: "#112233"})
	if err != nil {
		t.Fatal(err)
	}
	routers, err := st.CreateCategoryFrom(CategoryInput{Name: "Routers", ParentID: &net.ID, ColorHex: "#445566"})
	if err != nil {
		t.Fatal(err)
	}
	if s := saveIcon(t, st, c, net.ID, "switch", switchSrc); !s.Published {
		t.Fatalf("%+v", s.Errors)
	}
	return iconWorld{st: st, c: c, networking: net.ID, routers: routers.ID}
}

func resolved(t *testing.T, st *Store, id string) *models.ResolvedIcon {
	t.Helper()
	a, err := st.GetAssetByID(id)
	if err != nil {
		t.Fatal(err)
	}
	return a.ResolvedIcon
}

// An asset with no icon of its own is drawn with its nearest category's,
// in its own category's color; one in no category with an icon, with
// none.
func TestAnAssetIsDrawnWithItsCategoriesIcon(t *testing.T) {
	w := newIconWorld(t)
	router, err := w.st.CreateAsset("Edge router", []string{w.routers}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := models.ResolvedIcon{Name: "switch", From: models.IconFromCategory, SourceCategoryID: w.networking, SourceCategoryName: "Networking", Color: "#445566"}
	if got := router.ResolvedIcon; got == nil || !reflect.DeepEqual(*got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
	plain, err := w.st.CreateAsset("Chair", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if plain.ResolvedIcon != nil {
		t.Errorf("an asset in no category with an icon: %+v", plain.ResolvedIcon)
	}

	shown, err := w.st.ShownCategoryIcons()
	if err != nil {
		t.Fatal(err)
	}
	if len(shown) != 2 || shown[w.networking] != "switch" || shown[w.routers] != "switch" {
		t.Errorf("%+v", shown)
	}

	// a list reads the same
	list, err := w.st.ListAssets(w.routers, "")
	if err != nil || len(list) != 1 || list[0].ResolvedIcon == nil || !reflect.DeepEqual(*list[0].ResolvedIcon, want) {
		t.Errorf("%+v %v", list, err)
	}
}

// An asset's own icon is any name the build draws, checked when it is
// set; dropping it gives it its categories' again, and both show in its
// history.
func TestAnAssetsOwnIcon(t *testing.T) {
	w := newIconWorld(t)
	if _, err := w.st.CreateAssetWithIcon("Router", []string{w.routers}, nil, nil, nil, "nope"); err == nil || !strings.Contains(err.Error(), `"nope"`) {
		t.Errorf("an icon the build does not draw: %v", err)
	}
	a, err := w.st.CreateAssetWithIcon("Router", []string{w.routers}, nil, nil, nil, "switch-down")
	if err != nil {
		t.Fatal(err)
	}
	want := models.ResolvedIcon{Name: "switch-down", From: models.IconFromAsset, Color: "#445566"}
	if got := a.ResolvedIcon; got == nil || !reflect.DeepEqual(*got, want) || a.Asset.IconName == nil || *a.Asset.IconName != "switch-down" {
		t.Errorf("got %+v (%v), want %+v", got, a.Asset.IconName, want)
	}
	id := a.Asset.ID

	// an edit without an icon keeps it
	if a, err = w.st.UpdateAssetWithIcon(id, "Core router", nil, nil, nil, nil, nil); err != nil || a.ResolvedIcon.Name != "switch-down" {
		t.Fatalf("%+v %v", a, err)
	}
	if _, err := w.st.UpdateAssetWithIcon(id, "", nil, nil, nil, nil, &IconUpdate{Name: "nope"}); err == nil {
		t.Error("changed to an icon the build does not draw")
	}
	if a, err = w.st.UpdateAssetWithIcon(id, "", nil, nil, nil, nil, &IconUpdate{}); err != nil {
		t.Fatal(err)
	}
	if a.Asset.IconName != nil || a.ResolvedIcon.From != models.IconFromCategory {
		t.Errorf("dropped: %v %+v", a.Asset.IconName, a.ResolvedIcon)
	}

	events, _, err := w.st.ListAssetEvents(id, "", 10)
	if err != nil {
		t.Fatal(err)
	}
	var icons []models.EventChange
	for _, e := range events {
		for _, c := range e.Payload.Changes {
			if c.What == "icon" {
				icons = append(icons, c)
			}
		}
	}
	// newest first: dropped, then set when it was made
	if len(icons) != 2 || icons[0] != (models.EventChange{What: "icon", From: "switch-down"}) || icons[1] != (models.EventChange{What: "icon", To: "switch-down"}) {
		t.Errorf("%+v", icons)
	}
}

// An icon taken away later is drawn as the unknown icon, not quietly as
// the categories'; the asset can still be changed in other ways.
func TestAnOwnIconThatGoesAwayIsMissing(t *testing.T) {
	w := newIconWorld(t)
	storage := newCategory(t, w.st, "Storage")
	saveIcon(t, w.st, w.c, storage, "crate", crate)
	a, err := w.st.CreateAssetWithIcon("Router", []string{w.routers}, nil, nil, nil, "crate")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.st.DeleteCategoryIcon(context.Background(), w.c, storage); err != nil {
		t.Fatal(err)
	}
	if got := resolved(t, w.st, a.Asset.ID); got == nil || got.Name != "crate" || got.From != models.IconFromAsset || !got.Missing {
		t.Errorf("%+v", got)
	}
	if _, err := w.st.UpdateAssetWithIcon(a.Asset.ID, "Renamed", nil, nil, nil, nil, &IconUpdate{Name: "crate"}); err != nil {
		t.Errorf("keeping the icon it has: %v", err)
	}
}

// Renaming a category's icon carries the assets that name it, or its
// variants, along; another icon whose name starts the same stays.
func TestRenamingAnIconCarriesAssetsAlong(t *testing.T) {
	w := newIconWorld(t)
	l3 := newCategory(t, w.st, "Layer 3")
	saveIcon(t, w.st, w.c, l3, "switch-l3", strings.ReplaceAll(switchSrc, `"switch"`, `"switch-l3"`))
	ids := map[string]string{}
	for _, icon := range []string{"switch", "switch-down", "switch-l3"} {
		a, err := w.st.CreateAssetWithIcon(icon, []string{w.routers}, nil, nil, nil, icon)
		if err != nil {
			t.Fatal(err)
		}
		ids[icon] = a.Asset.ID
	}
	both := switchSrc + "\n" + strings.Replace(switchSrc, `icon "switch"`, `icon "net-switch"`, 1)
	saveIcon(t, w.st, w.c, w.networking, "switch", both)
	if _, err := w.st.RenameCategoryIcon(context.Background(), w.c, w.networking, "net-switch"); err != nil {
		t.Fatal(err)
	}
	for was, now := range map[string]string{"switch": "net-switch", "switch-down": "net-switch-down", "switch-l3": "switch-l3"} {
		if got := resolved(t, w.st, ids[was]); got.Name != now || got.Missing {
			t.Errorf("%s: %+v, want %s", was, got, now)
		}
	}
}
