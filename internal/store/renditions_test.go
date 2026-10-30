package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/config"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store/migrations"
)

// Renditions (README §3.7): a category's icon for labels, beside its icon
// for the screen, resolved for an asset by the same rule, and falling back
// to the screen's.

const (
	switchLabelSrc = `icon "switch-label" { category "net"; footprint 1 1 1; axes "state"; part "box" { box 0 0 0 1 1 1 } }`
	firewallSrc    = `icon "firewall" { category "net"; footprint 1 1 1; part "box" { box 0 0 0 1 1 1 } }`
	patchSrc       = `icon "patch" { category "net"; footprint 1 1 1; part "box" { box 0 0 0 1 1 1 } }`
)

func saveRendition(t *testing.T, st *Store, c IconCompiler, categoryID, rendition, name, source string) {
	t.Helper()
	save, err := st.SaveCategoryIcon(context.Background(), c, categoryID, rendition, name, source)
	if err != nil {
		t.Fatal(err)
	}
	if !save.Published {
		t.Fatalf("%s: %+v", name, save.Errors)
	}
}

func iconsOf(t *testing.T, st *Store, id string) (screen, label *models.ResolvedIcon) {
	t.Helper()
	a, err := st.GetAssetByID(id)
	if err != nil {
		t.Fatal(err)
	}
	if a.ResolvedIcons[models.RenditionScreen] != a.ResolvedIcon {
		t.Errorf("resolved_icons' screen icon is not resolved_icon")
	}
	return a.ResolvedIcon, a.ResolvedIcons[models.RenditionLabel]
}

func TestAnAssetsLabelIconResolvesAsItsScreenIconDoes(t *testing.T) {
	w := newIconWorld(t)
	ctx := context.Background()
	saveRendition(t, w.st, w.c, w.networking, models.RenditionLabel, "switch-label", switchLabelSrc)
	firewalls, err := w.st.CreateCategoryFrom(CategoryInput{Name: "Firewalls", ParentID: &w.networking})
	if err != nil {
		t.Fatal(err)
	}
	saveRendition(t, w.st, w.c, firewalls.ID, "", "firewall", firewallSrc)
	patch := newCategory(t, w.st, "Patch panels")
	saveRendition(t, w.st, w.c, patch, models.RenditionScreen, "patch", patchSrc)

	asset := func(name string, cats ...string) string {
		t.Helper()
		a, err := w.st.CreateAsset(name, cats, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		return a.Asset.ID
	}
	check := func(what string, got *models.ResolvedIcon, name, from, rendition, source string) {
		t.Helper()
		if got == nil {
			t.Errorf("%s: no icon, want %s", what, name)
			return
		}
		if got.Name != name || got.From != from || got.Rendition != rendition || got.SourceCategoryID != source {
			t.Errorf("%s: %s from %s (%s, category %q), want %s from %s (%s, category %q)", what, got.Name, got.From, got.Rendition, got.SourceCategoryID, name, from, rendition, source)
		}
	}

	// a subcategory with none of its own takes its parent's, each rendition
	router := asset("Edge router", w.routers)
	screen, label := iconsOf(t, w.st, router)
	check("router on screen", screen, "switch", models.IconFromCategory, models.RenditionScreen, w.networking)
	check("router on labels", label, "switch-label", models.IconFromCategory, models.RenditionLabel, w.networking)

	// the nearest category up with an icon for the rendition, each
	// rendition on its own
	fw := asset("Firewall", firewalls.ID)
	screen, label = iconsOf(t, w.st, fw)
	check("firewall on screen", screen, "firewall", models.IconFromCategory, models.RenditionScreen, firewalls.ID)
	check("firewall on labels", label, "switch-label", models.IconFromCategory, models.RenditionLabel, w.networking)

	// with no label icon anywhere up, labels draw the screen icon: the same
	panel := asset("Panel", patch)
	screen, label = iconsOf(t, w.st, panel)
	check("panel on labels", label, "patch", models.IconFromCategory, models.RenditionScreen, patch)
	if label != screen {
		t.Error("a label drawn with the screen icon is not the screen icon")
	}

	// an asset's own screen icon wins over its categories' label icon: it
	// was made to look different on purpose
	a, err := w.st.CreateAssetWithIcon("Odd router", []string{w.routers}, nil, nil, nil, "switch-down")
	if err != nil {
		t.Fatal(err)
	}
	_, label = iconsOf(t, w.st, a.Asset.ID)
	check("own screen icon on labels", label, "switch-down", models.IconFromAsset, models.RenditionScreen, "")

	// and an own label icon wins over that
	if _, err := w.st.UpdateAssetWithIcon(a.Asset.ID, "", nil, nil, nil, nil, &IconUpdate{Name: "patch", Rendition: models.RenditionLabel}); err != nil {
		t.Fatal(err)
	}
	screen, label = iconsOf(t, w.st, a.Asset.ID)
	check("own label icon", label, "patch", models.IconFromAsset, models.RenditionLabel, "")
	check("own screen icon, kept", screen, "switch-down", models.IconFromAsset, models.RenditionScreen, "")
	got, err := w.st.GetAssetByID(a.Asset.ID)
	if err != nil || got.Asset.LabelIconName == nil || *got.Asset.LabelIconName != "patch" {
		t.Errorf("label_icon_name: %+v %v", got.Asset, err)
	}
	if _, err := w.st.UpdateAssetWithIcon(a.Asset.ID, "", nil, nil, nil, nil, &IconUpdate{Name: "patch", Rendition: "sticker"}); !errors.Is(err, ErrUnknownRendition) {
		t.Errorf("an unknown rendition: %v", err)
	}

	// ties are broken as templates' are: the older of two categories
	// alike
	cups := newCategory(t, w.st, "Cups")
	mugs := newCategory(t, w.st, "Mugs")
	saveRendition(t, w.st, w.c, cups, models.RenditionLabel, "cup-label", `icon "cup-label" { category "net"; footprint 1 1 1; part "box" { box 0 0 0 1 1 1 } }`)
	saveRendition(t, w.st, w.c, mugs, models.RenditionLabel, "mug-label", `icon "mug-label" { category "net"; footprint 1 1 1; part "box" { box 0 0 0 1 1 1 } }`)
	_, label = iconsOf(t, w.st, asset("Cup or mug", mugs, cups))
	check("a tie", label, "cup-label", models.IconFromCategory, models.RenditionLabel, cups)

	// the label icon's state variant, as the screen icon's
	if _, _, err := w.st.SetAssetCondition(router, models.ConditionOutOfService, "Dead"); err != nil {
		t.Fatal(err)
	}
	screen, label = iconsOf(t, w.st, router)
	if screen.Name != "switch-down" || label.Name != "switch-label-down" || label.Plain() != "switch-label" {
		t.Errorf("states: screen %+v, label %+v", screen, label)
	}

	// shown per rendition, a category with no label icon shown with its
	// screen icon
	shown, err := w.st.ShownCategoryIcons(models.RenditionLabel)
	if err != nil {
		t.Fatal(err)
	}
	if shown[w.routers] != "switch-label" || shown[firewalls.ID] != "switch-label" || shown[patch] != "patch" {
		t.Errorf("shown on labels: %+v", shown)
	}
	if _, err := w.st.ShownCategoryIcons("sticker"); !errors.Is(err, ErrUnknownRendition) {
		t.Errorf("an unknown rendition: %v", err)
	}
	_ = ctx
}

// Each rendition's icon is saved, checked, renamed and deleted on its own;
// a name is one icon's, whatever its rendition.
func TestACategorysIconsForEachRendition(t *testing.T) {
	w := newIconWorld(t)
	ctx := context.Background()

	// the label icon extends the screen one
	if check, err := w.st.CheckCategoryIcon(ctx, w.c, w.networking, models.RenditionLabel, "switch-label", `icon "switch-label" { extends "switch" }`); err != nil || len(check.Errors) > 0 {
		t.Fatalf("%+v %v", check, err)
	}
	saveRendition(t, w.st, w.c, w.networking, models.RenditionLabel, "switch-label", `icon "switch-label" { extends "switch" }`)
	screen, err := w.st.GetCategoryIcon(w.networking, "")
	if err != nil || screen.IconName != "switch" || screen.Rendition != models.RenditionScreen {
		t.Fatalf("%+v %v", screen, err)
	}
	label, err := w.st.GetCategoryIcon(w.networking, models.RenditionLabel)
	if err != nil || label.IconName != "switch-label" || label.Rendition != models.RenditionLabel {
		t.Fatalf("%+v %v", label, err)
	}
	if revs, err := w.st.ListCategoryIconRevisions(w.networking, models.RenditionLabel); err != nil || len(revs) != 1 {
		t.Errorf("label revisions: %+v %v", revs, err)
	}
	if revs, err := w.st.ListCategoryIconRevisions(w.networking, ""); err != nil || len(revs) != 1 {
		t.Errorf("screen revisions: %+v %v", revs, err)
	}
	all, err := w.st.ListCategoryIcons()
	if err != nil || len(all) != 2 {
		t.Errorf("%+v %v", all, err)
	}

	// names are unique across renditions
	if _, err := w.st.SaveCategoryIcon(ctx, w.c, w.routers, models.RenditionLabel, "switch", switchSrc); !errors.Is(err, ErrIconNameTaken) {
		t.Errorf("a label icon named as a screen icon: %v", err)
	}
	if _, err := w.st.SaveCategoryIcon(ctx, w.c, w.networking, "sticker", "switch-sticker", switchSrc); !errors.Is(err, ErrUnknownRendition) {
		t.Errorf("an unknown rendition: %v", err)
	}

	// the screen icon cannot go while the label one extends it
	if _, err := w.st.DeleteCategoryIcon(ctx, w.c, w.networking, ""); buildErrors(t, err) == nil {
		t.Error("deleted an icon another extends")
	}

	// an asset's own label icon follows a rename of it
	a, err := w.st.CreateAsset("Switch", []string{w.routers}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.st.UpdateAssetWithIcon(a.Asset.ID, "", nil, nil, nil, nil, &IconUpdate{Name: "switch-label", Rendition: models.RenditionLabel}); err != nil {
		t.Fatal(err)
	}
	saveRendition(t, w.st, w.c, w.networking, models.RenditionLabel, "switch-label", `icon "switch-label" { extends "switch" }
icon "switch-tape" { extends "switch" }`)
	if _, err := w.st.RenameCategoryIcon(ctx, w.c, w.networking, models.RenditionLabel, "switch-tape"); err != nil {
		t.Fatal(err)
	}
	got, err := w.st.GetAssetByID(a.Asset.ID)
	if err != nil || got.Asset.LabelIconName == nil || *got.Asset.LabelIconName != "switch-tape" || got.Asset.IconName != nil {
		t.Errorf("after the rename: %+v %v", got.Asset, err)
	}
	if screen, err := w.st.GetCategoryIcon(w.networking, ""); err != nil || screen.IconName != "switch" {
		t.Errorf("the screen icon renamed too: %+v %v", screen, err)
	}

	// deleted, the label icon goes alone, and its asset's own label icon
	// is missing
	if _, err := w.st.DeleteCategoryIcon(ctx, w.c, w.networking, models.RenditionLabel); err != nil {
		t.Fatal(err)
	}
	if _, err := w.st.GetCategoryIcon(w.networking, models.RenditionLabel); !errors.Is(err, ErrCategoryIconNotFound) {
		t.Errorf("the label icon is still there: %v", err)
	}
	if _, err := w.st.GetCategoryIcon(w.networking, ""); err != nil {
		t.Errorf("the screen icon went with it: %v", err)
	}
	missing, err := w.st.MissingAssetIcons()
	if err != nil || len(missing) != 1 || missing[0].Name != "switch-tape" || missing[0].Assets != 1 {
		t.Errorf("missing: %+v %v", missing, err)
	}
	_, labelIcon := iconsOf(t, w.st, a.Asset.ID)
	if labelIcon == nil || !labelIcon.Missing || labelIcon.Name != "switch-tape" {
		t.Errorf("a missing own label icon: %+v", labelIcon)
	}
}

// openBlank opens a store with no schema: of the test dialect, as
// setupTestStore does, but not migrated.
func openBlank(t *testing.T) *Store {
	t.Helper()
	if dialect := os.Getenv(TestDialectEnv); dialect != "" && dialect != "sqlite" {
		st, cleanup, err := OpenEphemeral(dialect, os.Getenv(TestDSNEnv))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(cleanup)
		return st
	}
	st, err := Open(&config.DatabaseConfig{Type: "sqlite", DSN: filepath.Join(t.TempDir(), "blank.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

// Migrating keeps every category icon, its history and its bindings as
// the screen's, and every asset's own icon as its screen icon.
func TestEveryIconBeforeRenditionsIsTheScreens(t *testing.T) {
	st := openBlank(t)
	if err := migrations.MigrateTo(st.DB, st.Dialect, 46); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	src := `icon "crate" { footprint 1 1 1; part "box" { box 0 0 0 1 1 1 } }`
	for _, q := range []struct {
		sql  string
		args []any
	}{
		{"INSERT INTO categories (id, name, created_at, updated_at) VALUES (?, ?, ?, ?)", []any{"c1", "Crates", now, now}},
		{"INSERT INTO fields (id, field_key, label, validation_type, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)", []any{"f1", "height", "Height", "number", now, now}},
		{"INSERT INTO category_icons (category_id, icon_name, source, updated_at) VALUES (?, ?, ?, ?)", []any{"c1", "crate", src, now}},
		{"INSERT INTO icon_revisions (id, category_id, source, created_at) VALUES (?, ?, ?, ?)", []any{"r1", "c1", src, now}},
		{"INSERT INTO icon_library (id, path, source, updated_at) VALUES (?, ?, ?, ?)", []any{"l1", "shared.ilx", "", now}},
		{"INSERT INTO icon_revisions (id, library_id, source, created_at) VALUES (?, ?, ?, ?)", []any{"r2", "l1", "", now}},
		{"INSERT INTO icon_param_bindings (category_id, param, field_id) VALUES (?, ?, ?)", []any{"c1", "h", "f1"}},
		{"INSERT INTO icon_text_bindings (category_id, param, source) VALUES (?, ?, ?)", []any{"c1", "tag", TextFromName}},
		{"INSERT INTO assets (id, name, created_at, updated_at, icon_name) VALUES (?, ?, ?, ?, ?)", []any{"a1", "Crate 1", now, now, "crate"}},
	} {
		if _, err := st.Exec(q.sql, q.args...); err != nil {
			t.Fatalf("%s: %v", q.sql, err)
		}
	}
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}

	ic, err := st.GetCategoryIcon("c1", "")
	if err != nil || ic.IconName != "crate" || ic.Rendition != models.RenditionScreen || ic.Source == nil || *ic.Source != src {
		t.Fatalf("%+v %v", ic, err)
	}
	if revs, err := st.ListCategoryIconRevisions("c1", models.RenditionScreen); err != nil || len(revs) != 1 || revs[0].ID != "r1" {
		t.Errorf("revisions: %+v %v", revs, err)
	}
	if revs, err := st.ListIconLibraryRevisions("l1"); err != nil || len(revs) != 1 || revs[0].ID != "r2" {
		t.Errorf("library revisions: %+v %v", revs, err)
	}
	bindings, err := st.IconParamBindings("c1", "")
	if err != nil || len(bindings) != 2 || bindings[0].Param != "h" || bindings[0].FieldID != "f1" || bindings[1].Param != "tag" || bindings[1].Source != TextFromName {
		t.Errorf("bindings: %+v %v", bindings, err)
	}
	if _, err := st.GetCategoryIcon("c1", models.RenditionLabel); !errors.Is(err, ErrCategoryIconNotFound) {
		t.Errorf("a label icon: %v", err)
	}
	var icon, label *string
	if err := st.QueryRow("SELECT icon_name, label_icon_name FROM assets WHERE id = ?", "a1").Scan(&icon, &label); err != nil {
		t.Fatal(err)
	}
	if icon == nil || *icon != "crate" || label != nil {
		t.Errorf("the asset's own icons: %v %v", icon, label)
	}

	// deleting the category takes its icons and their bindings with it
	if _, err := st.Exec("DELETE FROM categories WHERE id = ?", "c1"); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"category_icons", "icon_param_bindings", "icon_text_bindings"} {
		var n int
		if err := st.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); err != nil || n != 0 {
			t.Errorf("%s: %d left, %v", table, n, err)
		}
	}
	var n int
	if err := st.QueryRow("SELECT COUNT(*) FROM icon_revisions").Scan(&n); err != nil || n != 1 {
		t.Errorf("icon_revisions: %d left, want the library's, %v", n, err)
	}
}
