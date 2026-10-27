package store

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/isolex"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/isolex/ir"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
)

var (
	iconCompiler     *isolex.Compiler
	iconCompilerOnce sync.Once
)

// compilerFor returns the icon compiler built into the tests. Without it
// they skip, unless BAM_REQUIRE_ISOLEX is set (README §8.1).
func compilerFor(t *testing.T) *isolex.Compiler {
	t.Helper()
	iconCompilerOnce.Do(func() { iconCompiler = isolex.New(isolex.Options{}) })
	if !iconCompiler.Available() {
		if os.Getenv("BAM_REQUIRE_ISOLEX") != "" {
			t.Fatal("BAM_REQUIRE_ISOLEX is set, but the icon compiler is not built in")
		}
		t.Skip("the icon compiler is not built in (tools/isolex/fetch.sh)")
	}
	return iconCompiler
}

func newCategory(t *testing.T, st *Store, name string) string {
	t.Helper()
	c, err := st.CreateCategoryFrom(CategoryInput{Name: name, ColorHex: "#336699"})
	if err != nil {
		t.Fatal(err)
	}
	return c.ID
}

func saveIcon(t *testing.T, st *Store, c IconCompiler, categoryID, name, source string) *models.IconSave {
	t.Helper()
	save, err := st.SaveCategoryIcon(context.Background(), c, categoryID, name, source)
	if err != nil {
		t.Fatal(err)
	}
	return save
}

func buildErrors(t *testing.T, err error) []models.IconDiag {
	t.Helper()
	var be *IconBuildError
	if !errors.As(err, &be) {
		t.Fatalf("want a refusal for the build, got %v", err)
	}
	return be.Errors
}

const crate = `icon "crate" { category "storage"; footprint 1 1 1; part "box" { box 0 0 0 1 1 1 } }`

func TestIconsStartWithAPaletteAndNoBuild(t *testing.T) {
	st := setupTestStore(t)
	palette, err := st.GetIconPalette()
	if err != nil {
		t.Fatal(err)
	}
	if len(palette) != 10 || palette[0] != (models.IconColor{Token: "body-top", Light: "#f4f4f5", Dark: "#71717a"}) {
		t.Errorf("palette: %+v", palette)
	}
	settings, err := st.GetIconSettings()
	if err != nil || len(settings.StrokeWidths) != 0 || len(settings.Reserved) != 0 || len(settings.Limits) != 0 {
		t.Errorf("settings: %+v, %v", settings, err)
	}
	if b, err := st.CurrentIconBuild(); b != nil || err != nil {
		t.Errorf("a build before any icon: %+v, %v", b, err)
	}
	if icons, err := st.ListCategoryIcons(); len(icons) != 0 || err != nil {
		t.Errorf("icons: %+v, %v", icons, err)
	}
}

// A source that compiles is published and builds; one that does not is
// a draft, and the published version and the build stay as they were.
func TestACategoryIconIsPublishedOrDrafted(t *testing.T) {
	st := setupTestStore(t)
	c := compilerFor(t)
	cat := newCategory(t, st, "Storage")

	first := saveIcon(t, st, c, cat, "crate", crate)
	if !first.Published || first.Build == nil || len(first.Errors) != 0 {
		t.Fatalf("first save: %+v", first)
	}
	if !strings.Contains(string(first.Build.IR), `"name":"crate"`) || first.Build.IsolexVersion != isolex.Pinned().Version {
		t.Errorf("build: %s", first.Build.IR)
	}

	broken := strings.Replace(crate, "box 0 0 0 1 1 1", "box 0 0 0 1 1", 1)
	second := saveIcon(t, st, c, cat, "crate", broken)
	if second.Published || second.Build != nil || len(second.Errors) == 0 {
		t.Fatalf("a broken source was published: %+v", second)
	}
	if e := second.Errors[0]; e.File != "categories/crate.ilx" || e.Line != 1 || !strings.Contains(e.Message, "box takes 6") {
		t.Errorf("error: %+v", e)
	}
	ic, err := st.GetCategoryIcon(cat)
	if err != nil {
		t.Fatal(err)
	}
	if *ic.Source != crate || ic.Draft == nil || *ic.Draft != broken || len(ic.DraftErrors) == 0 {
		t.Errorf("after a broken save: %+v", ic)
	}
	if b, _ := st.CurrentIconBuild(); b == nil || b.Hash != first.Build.Hash {
		t.Error("a broken save changed the build")
	}

	fixed := strings.Replace(crate, `"box" {`, `"box" { shadow 0.5 0.5 0.4 0.4;`, 1)
	third := saveIcon(t, st, c, cat, "crate", fixed)
	if !third.Published || third.Build.Hash == first.Build.Hash {
		t.Fatalf("third save: %+v", third)
	}
	if ic, _ := st.GetCategoryIcon(cat); ic.Draft != nil || len(ic.DraftErrors) != 0 || *ic.Source != fixed {
		t.Errorf("publishing kept the draft: %+v", ic)
	}
	revs, err := st.ListCategoryIconRevisions(cat)
	if err != nil || len(revs) != 2 || revs[0].Source != fixed || revs[1].Source != crate {
		t.Errorf("revisions: %+v, %v", revs, err)
	}

	events, err := st.ListEvents(10)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, e := range events {
		if e.Kind == models.EventCategoryIconChanged {
			n++
		}
	}
	if n != 2 {
		t.Errorf("%d icon events, want one per publish", n)
	}
}

func TestACategoryMustDrawItsOwnIcon(t *testing.T) {
	st := setupTestStore(t)
	c := compilerFor(t)
	cat := newCategory(t, st, "Storage")
	save := saveIcon(t, st, c, cat, "bin", crate)
	if save.Published || len(save.Errors) != 1 || !strings.Contains(save.Errors[0].Message, `named "bin", but nothing here draws an icon by that name`) {
		t.Errorf("%+v", save)
	}
	if ic, err := st.GetCategoryIcon(cat); err != nil || ic.Source != nil || ic.Draft == nil {
		t.Errorf("a first save that failed: %+v, %v", ic, err)
	}
	// a draft never published is dropped whole
	if err := st.DiscardCategoryIconDraft(cat); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetCategoryIcon(cat); !errors.Is(err, ErrCategoryIconNotFound) {
		t.Errorf("after discarding: %v", err)
	}
}

func TestIconNames(t *testing.T) {
	st := setupTestStore(t)
	c := compilerFor(t)
	a, b := newCategory(t, st, "A"), newCategory(t, st, "B")
	for _, name := range []string{"", ".hidden", "a/b", "sp ace", "bam-unknown", strings.Repeat("x", 65)} {
		if _, err := st.SaveCategoryIcon(context.Background(), c, a, name, crate); err == nil {
			t.Errorf("accepted the name %q", name)
		}
	}
	saveIcon(t, st, c, a, "crate", crate)
	if _, err := st.SaveCategoryIcon(context.Background(), c, b, "crate", crate); !errors.Is(err, ErrIconNameTaken) {
		t.Errorf("a second category took the name: %v", err)
	}
	if _, err := st.SaveCategoryIcon(context.Background(), c, a, "box", crate); !errors.Is(err, ErrIconRename) {
		t.Errorf("a save renamed the icon: %v", err)
	}
	if _, err := st.SaveCategoryIcon(context.Background(), c, "no-such-category", "crate2", crate); !errors.Is(err, ErrCategoryNotFound) {
		t.Errorf("an icon for no category: %v", err)
	}
}

// One icon extends another: what the child needs cannot be taken away.
func TestWhatOthersUseCannotBeRemoved(t *testing.T) {
	st := setupTestStore(t)
	c := compilerFor(t)
	ctx := context.Background()
	parent, child := newCategory(t, st, "Storage"), newCategory(t, st, "Small storage")
	saveIcon(t, st, c, parent, "crate", crate)
	if s := saveIcon(t, st, c, child, "small-crate", `icon "small-crate" { extends "crate"; footprint 1 1 0.5 }`); !s.Published {
		t.Fatalf("child: %+v", s.Errors)
	}

	_, err := st.DeleteCategoryIcon(ctx, c, parent)
	if errs := buildErrors(t, err); !strings.Contains(errs[0].Message, "crate") || errs[0].File != "categories/small-crate.ilx" {
		t.Errorf("deleting the parent: %+v", errs)
	}
	_, err = st.RenameCategoryIcon(ctx, c, parent, "box")
	buildErrors(t, err)
	if ic, _ := st.GetCategoryIcon(parent); ic.IconName != "crate" {
		t.Errorf("a refused rename renamed it: %+v", ic)
	}

	if _, err := st.DeleteCategoryIcon(ctx, c, child); err != nil {
		t.Fatal(err)
	}
	if _, err := st.DeleteCategoryIcon(ctx, c, parent); err != nil {
		t.Fatal(err)
	}
	b, _ := st.CurrentIconBuild()
	if b == nil || strings.Contains(string(b.IR), "crate") {
		t.Errorf("the build after deleting both: %+v", b)
	}
}

func TestRenamingAnIcon(t *testing.T) {
	st := setupTestStore(t)
	c := compilerFor(t)
	cat := newCategory(t, st, "Storage")
	saveIcon(t, st, c, cat, "crate", crate)
	// the source must draw it under the new name: refused until it does
	_, err := st.RenameCategoryIcon(context.Background(), c, cat, "box")
	if errs := buildErrors(t, err); !strings.Contains(errs[0].Message, `named "box"`) {
		t.Errorf("%+v", errs)
	}
	both := crate + "\n" + strings.Replace(crate, `icon "crate"`, `icon "box"`, 1)
	saveIcon(t, st, c, cat, "crate", both)
	if _, err := st.RenameCategoryIcon(context.Background(), c, cat, "box"); err != nil {
		t.Fatal(err)
	}
	if ic, _ := st.GetCategoryIcon(cat); ic.IconName != "box" {
		t.Errorf("%+v", ic)
	}
}

// The palette is the vocabulary: a token taken out of it while an icon
// paints with it is refused, and "category" is always there.
func TestThePaletteIsTheVocabulary(t *testing.T) {
	st := setupTestStore(t)
	c := compilerFor(t)
	ctx := context.Background()
	cat := newCategory(t, st, "Storage")
	tinted := strings.Replace(crate, "box 0 0 0 1 1 1", `box 0 0 0 1 1 1; tile 0 0 1 1 1 fill="category"`, 1)
	if s := saveIcon(t, st, c, cat, "crate", tinted); !s.Published {
		t.Fatalf("%+v", s.Errors)
	}

	palette, _ := st.GetIconPalette()
	var withoutTop []models.IconColor
	for _, col := range palette {
		if col.Token != "body-top" {
			withoutTop = append(withoutTop, col)
		}
	}
	_, err := st.SetIconPalette(ctx, c, withoutTop)
	if errs := buildErrors(t, err); !strings.Contains(errs[0].Message, "'body-top'") {
		t.Errorf("%+v", errs)
	}
	if p, _ := st.GetIconPalette(); len(p) != len(palette) {
		t.Error("a refused palette was kept")
	}

	for _, bad := range [][]models.IconColor{
		{{Token: "Body", Light: "#fff", Dark: "#000"}},
		{{Token: "none", Light: "#fff", Dark: "#000"}},
		{{Token: "category", Light: "#fff", Dark: "#000"}},
		{{Token: "ink", Light: "red", Dark: "#000"}},
		{{Token: "ink", Light: "#fff", Dark: "#000"}, {Token: "ink", Light: "#fff", Dark: "#000"}},
	} {
		if _, err := st.SetIconPalette(ctx, c, bad); err == nil {
			t.Errorf("accepted %+v", bad)
		}
	}

	brand := append(append([]models.IconColor{}, palette...), models.IconColor{Token: "brand", Light: "#C00", Dark: "#F66"})
	if _, err := st.SetIconPalette(ctx, c, brand); err != nil {
		t.Fatal(err)
	}
	if p, _ := st.GetIconPalette(); p[len(p)-1] != (models.IconColor{Token: "brand", Light: "#c00", Dark: "#f66"}) {
		t.Errorf("%+v", p[len(p)-1])
	}
}

func TestSettingsHoldIconsToTheirRules(t *testing.T) {
	st := setupTestStore(t)
	c := compilerFor(t)
	ctx := context.Background()
	cat := newCategory(t, st, "Storage")
	saveIcon(t, st, c, cat, "crate", strings.Replace(crate, "box 0 0 0 1 1 1", `repeat 3 "i" { box 0 0 "i" 1 1 1 }`, 1))

	// a box's outline is drawn at 1.2
	_, err := st.SetIconSettings(ctx, c, models.IconSettings{StrokeWidths: []float64{2}})
	buildErrors(t, err)
	if _, err := st.SetIconSettings(ctx, c, models.IconSettings{StrokeWidths: []float64{1.2, 2}, Limits: map[string]int64{"shape-limit": 100, "icon-limit": 0}}); err != nil {
		t.Fatal(err)
	}
	s, _ := st.GetIconSettings()
	if len(s.StrokeWidths) != 2 || s.Limits["shape-limit"] != 100 || s.Limits["icon-limit"] != 0 || len(s.Limits) != 2 {
		t.Errorf("%+v", s)
	}
	_, err = st.SetIconSettings(ctx, c, models.IconSettings{Limits: map[string]int64{"shape-limit": 2}})
	if errs := buildErrors(t, err); !strings.Contains(errs[0].Message, "shape-limit") {
		t.Errorf("%+v", errs)
	}
	if _, err := st.SetIconSettings(ctx, c, models.IconSettings{Limits: map[string]int64{"shape-limt": 2}}); err == nil {
		t.Error("accepted an unknown limit")
	}
}

func TestLibraryFiles(t *testing.T) {
	st := setupTestStore(t)
	c := compilerFor(t)
	ctx := context.Background()
	for _, path := range []string{"", "a.kdl", ".x.ilx", "a//b.ilx", "../a.ilx", "sp ace.ilx", ".ilx"} {
		if _, _, err := st.CreateIconLibraryFile(ctx, c, path, ""); err == nil {
			t.Errorf("accepted the path %q", path)
		}
	}
	lib, save, err := st.CreateIconLibraryFile(ctx, c, "shared/crates.ilx", `base "crate-base" { category "storage"; footprint 1 1 1; part "box" { box 0 0 0 1 1 1 } }`)
	if err != nil || !save.Published || lib.Source == nil {
		t.Fatalf("%+v %+v %v", lib, save, err)
	}
	if _, _, err := st.CreateIconLibraryFile(ctx, c, "shared/crates.ilx", ""); !errors.Is(err, ErrIconPathTaken) {
		t.Errorf("a second file at the path: %v", err)
	}
	cat := newCategory(t, st, "Storage")
	if s := saveIcon(t, st, c, cat, "crate", `icon "crate" { extends "crate-base" }`); !s.Published {
		t.Fatalf("%+v", s.Errors)
	}

	_, err = st.DeleteIconLibraryFile(ctx, c, lib.ID)
	if errs := buildErrors(t, err); errs[0].File != "categories/crate.ilx" {
		t.Errorf("%+v", errs)
	}
	draft, err := st.SaveIconLibraryFile(ctx, c, lib.ID, `base "crate-base" {`)
	if err != nil || draft.Published || !strings.HasPrefix(draft.Errors[0].File, "library/shared/crates.ilx") {
		t.Fatalf("%+v %v", draft, err)
	}
	if err := st.DiscardIconLibraryDraft(lib.ID); err != nil {
		t.Fatal(err)
	}
	if f, _ := st.GetIconLibraryFile(lib.ID); f.Draft != nil || f.Source == nil {
		t.Errorf("%+v", f)
	}
	if _, err := st.RenameIconLibraryFile(ctx, c, lib.ID, "crates.ilx"); err != nil {
		t.Fatal(err)
	}
	if revs, err := st.ListIconLibraryRevisions(lib.ID); err != nil || len(revs) != 1 {
		t.Errorf("%+v %v", revs, err)
	}
}

// A check compiles with the source in place and keeps nothing.
func TestCheckKeepsNothing(t *testing.T) {
	st := setupTestStore(t)
	c := compilerFor(t)
	cat := newCategory(t, st, "Storage")
	ok, err := st.CheckCategoryIcon(context.Background(), c, cat, "crate", crate)
	if err != nil || len(ok.Errors) != 0 || !strings.Contains(string(ok.IR), `"crate"`) {
		t.Fatalf("%+v %v", ok, err)
	}
	bad, err := st.CheckCategoryIcon(context.Background(), c, cat, "crate", "icon {")
	if err != nil || len(bad.Errors) == 0 || bad.IR != nil {
		t.Fatalf("%+v %v", bad, err)
	}
	if _, err := st.GetCategoryIcon(cat); !errors.Is(err, ErrCategoryIconNotFound) {
		t.Error("a check kept the icon")
	}
	if b, _ := st.CurrentIconBuild(); b != nil {
		t.Error("a check built")
	}
}

func TestRebuildOnlyWhenTheInputsChanged(t *testing.T) {
	st := setupTestStore(t)
	c := compilerFor(t)
	ctx := context.Background()
	first, err := st.RebuildIcons(ctx, c)
	if err != nil || !first.Published || first.Build == nil {
		t.Fatalf("%+v %v", first, err)
	}
	again, _ := st.RebuildIcons(ctx, c)
	if !again.Build.BuiltAt.Equal(first.Build.BuiltAt) || again.Build.Hash != first.Build.Hash {
		t.Errorf("rebuilt what was already built: %v, then %v", first.Build.BuiltAt, again.Build.BuiltAt)
	}
	if _, err := st.Exec(`UPDATE icon_build SET source_hash = 'old'`); err != nil {
		t.Fatal(err)
	}
	if third, _ := st.RebuildIcons(ctx, c); third.Build.Hash != first.Build.Hash {
		t.Errorf("rebuilt to %s, want %s", third.Build.Hash, first.Build.Hash)
	}
}

// refuser is a compiler of the same version that compiles nothing: a new
// isolex that does not take what is published.
type refuser struct{ IconCompiler }

func (refuser) Compile(context.Context, []isolex.File) (*isolex.Result, error) {
	return &isolex.Result{Errors: []isolex.Diag{{File: "crate.ilx", Line: 1, Col: 1, Message: "refused"}}, Warnings: []isolex.Diag{}}, nil
}

// A build an older isolex made, in IR version 1, is drawn until a new
// isolex's build replaces it, and stays when the new one does not compile
// what is published (README §3.7).
func TestABuildOfAnOlderIsolexIsDrawnUntilReplaced(t *testing.T) {
	st := setupTestStore(t)
	c := compilerFor(t)
	ctx := context.Background()
	saveIcon(t, st, c, newCategory(t, st, "Storage"), "crate", crate)

	// as 0.3.1 left it: its IR, its version, inputs that no longer match
	old, err := os.ReadFile("../isolex/testdata/sample.ir.golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var v1 bytes.Buffer
	if err := json.Compact(&v1, old); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Exec(`UPDATE icon_build SET ir = ?, isolex_version = '0.3.1', source_hash = 'old'`, v1.String()); err != nil {
		t.Fatal(err)
	}
	drawn := func() *ir.Document {
		t.Helper()
		b, err := st.CurrentIconBuild()
		if err != nil || b == nil {
			t.Fatalf("%+v %v", b, err)
		}
		doc, err := ir.Parse(b.IR)
		if err != nil {
			t.Fatal(err)
		}
		return doc
	}
	if doc := drawn(); doc.IRVersion != 1 || doc.Icon("switch-l3") == nil {
		t.Fatalf("the old build is not drawn: version %d", doc.IRVersion)
	}

	refused, err := st.RebuildIcons(ctx, refuser{c})
	if err != nil || refused.Published || len(refused.Errors) != 1 {
		t.Fatalf("%+v %v", refused, err)
	}
	if doc := drawn(); doc.IRVersion != 1 {
		t.Fatalf("a refused rebuild replaced the old build: version %d", doc.IRVersion)
	}

	rebuilt, err := st.RebuildIcons(ctx, c)
	if err != nil || !rebuilt.Published || rebuilt.Build.IsolexVersion != isolex.Pinned().Version {
		t.Fatalf("%+v %v", rebuilt, err)
	}
	if doc := drawn(); doc.IRVersion != ir.Version || doc.Icon("crate") == nil {
		t.Fatalf("not rebuilt: version %d", doc.IRVersion)
	}
}

// meddler is a compiler that, while compiling, lets another change land.
type meddler struct {
	IconCompiler
	meddle func(n int)
	calls  int
}

func (m *meddler) Compile(ctx context.Context, files []isolex.File) (*isolex.Result, error) {
	m.calls++
	m.meddle(m.calls)
	return m.IconCompiler.Compile(ctx, files)
}

// A change compiles outside the transaction that keeps it, so another
// can land meanwhile: it is then compiled again against what is there,
// and never kept against what was.
func TestAChangeIsNeverKeptAgainstInputsThatMoved(t *testing.T) {
	st := setupTestStore(t)
	c := compilerFor(t)
	ctx := context.Background()
	parent, child := newCategory(t, st, "Storage"), newCategory(t, st, "Small storage")
	saveIcon(t, st, c, parent, "crate", crate)

	// the child extends crate; while it compiles, crate is deleted
	m := &meddler{IconCompiler: c, meddle: func(n int) {
		if n == 1 {
			if _, err := st.Exec(`DELETE FROM category_icons WHERE category_id = ?`, parent); err != nil {
				t.Error(err)
			}
		}
	}}
	save, err := st.SaveCategoryIcon(ctx, m, child, "small-crate", `icon "small-crate" { extends "crate" }`)
	if err != nil {
		t.Fatal(err)
	}
	if m.calls != 2 || save.Published {
		t.Errorf("after %d compiles: %+v", m.calls, save)
	}

	// and a change that never stops moving gives up
	always := &meddler{IconCompiler: c, meddle: func(n int) {
		if _, err := st.Exec(`UPDATE icon_settings SET limits = ? WHERE id = 'icons'`, `{"shape-limit": `+strings.Repeat("9", n)+`}`); err != nil {
			t.Error(err)
		}
	}}
	if _, err := st.SaveCategoryIcon(ctx, always, parent, "crate", crate); !errors.Is(err, ErrIconsChanging) {
		t.Errorf("err = %v after %d compiles", err, always.calls)
	}
}

// Deleting a category takes its icon and the icon's history with it.
func TestDeletingACategoryTakesItsIcon(t *testing.T) {
	st := setupTestStore(t)
	c := compilerFor(t)
	cat := newCategory(t, st, "Storage")
	saveIcon(t, st, c, cat, "crate", crate)
	if err := st.DeleteCategory(cat); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := st.QueryRow(`SELECT COUNT(*) FROM icon_revisions`).Scan(&n); err != nil || n != 0 {
		t.Errorf("%d revisions left, %v", n, err)
	}
	if icons, _ := st.ListCategoryIcons(); len(icons) != 0 {
		t.Errorf("%+v", icons)
	}
}

// Migration 035 grants icons.manage to every role that manages
// categories, and only those, once however often it runs.
func TestIconsManageIsGrantedToRolesThatManageCategories(t *testing.T) {
	st := setupTestStore(t)
	curators, err := st.CreateRole(RoleInput{Name: "Curators", Permissions: []string{rbac.PermCategoriesManage}})
	if err != nil {
		t.Fatal(err)
	}
	readers, err := st.CreateRole(RoleInput{Name: "Readers", Permissions: []string{rbac.PermAssetsRead}})
	if err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		"DELETE FROM permissions WHERE permission_key = 'icons.manage'",
		"DELETE FROM schema_migrations WHERE version = 35",
	} {
		if _, err := st.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 2; i++ {
		if err := st.Migrate(); err != nil {
			t.Fatal(err)
		}
		if _, err := st.Exec("DELETE FROM schema_migrations WHERE version = 35"); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		id   string
		want string
	}{
		{curators.ID, rbac.PermCategoriesManage + "," + rbac.PermIconsManage},
		{readers.ID, rbac.PermAssetsRead},
	} {
		role, err := st.GetRole(c.id)
		if err != nil {
			t.Fatal(err)
		}
		got := append([]string(nil), role.Permissions...)
		sort.Strings(got)
		if strings.Join(got, ",") != c.want {
			t.Errorf("%s: %v, want %s", role.Name, got, c.want)
		}
	}
}
