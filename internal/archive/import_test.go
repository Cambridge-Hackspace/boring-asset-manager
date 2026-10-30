package archive

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/filestore"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/flows"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/isolex"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// exportPackage writes a package of a selection.
func exportPackage(t *testing.T, st *store.Store, blobs filestore.Blobs, sel Selection) []byte {
	t.Helper()
	var parts memParts
	if _, err := Write(context.Background(), st, blobs, parts.open, Options{Select: &sel, Info: &PackageInfo{Name: "Workshop", Version: "1.0"}}); err != nil {
		t.Fatal(err)
	}
	return parts.parts[1].Bytes()
}

// workshopPackage is seedPackaged's workshop subtree, with its assets'
// photos (their files stored), links and history.
func workshopPackage(t *testing.T) (packaged, []byte) {
	t.Helper()
	src := setupStore(t)
	blobs := filestore.NewLocal(t.TempDir())
	p := seedPackaged(t, src, blobs)
	rows, err := src.Query("SELECT id, thumb_id FROM asset_photos WHERE asset_id = ?", p.drill)
	if err != nil {
		t.Fatal(err)
	}
	var keys []string
	for rows.Next() {
		var id, thumb string
		if err := rows.Scan(&id, &thumb); err != nil {
			t.Fatal(err)
		}
		keys = append(keys, id, thumb)
	}
	rows.Close()
	for _, k := range keys {
		if err := blobs.Put(context.Background(), "photos/"+k, strings.NewReader("photo "+k), -1, ""); err != nil {
			t.Fatal(err)
		}
	}
	return p, exportPackage(t, src, blobs, Selection{Subtrees: []string{p.tools.ID}, Assets: &AssetsSelection{Photos: true, Links: true, History: true}})
}

func itemOf(t *testing.T, plan *ImportPlan, kind, name string) ImportItem {
	t.Helper()
	for _, it := range plan.Items {
		if it.Kind == kind && it.Name == name {
			return it
		}
	}
	t.Fatalf("the plan has no %s %q: %+v", kind, name, plan.Items)
	return ImportItem{}
}

func childrenOf(t *testing.T, st *store.Store, parent string) []string {
	t.Helper()
	rows, err := st.Query("SELECT name FROM categories WHERE parent_id = ? ORDER BY name", parent)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		out = append(out, n)
	}
	return out
}

func idOf(t *testing.T, st *store.Store, query string, args ...any) string {
	t.Helper()
	var id string
	if err := st.QueryRow(query, args...).Scan(&id); err != nil {
		t.Fatalf("%s %v: %v", query, args, err)
	}
	return id
}

// The same package imported under two categories makes two copies of its
// categories and assets, each its own, and uses what is the same; checking
// it says exactly what importing it does.
func TestAPackageImportedTwiceUnderTwoAnchors(t *testing.T) {
	_, pkg := workshopPackage(t)
	dst := setupStore(t)
	blobs := filestore.NewLocal(t.TempDir())
	ctx := context.Background()
	labA := must[*models.Category](t)(dst.CreateCategory("Lab A", nil, "#111111", false, false))
	labB := must[*models.Category](t)(dst.CreateCategory("Lab B", nil, "#222222", false, false))

	checked, err := CheckImport(ctx, dst, opener(pkg), ImportOptions{Anchor: labA.ID})
	if err != nil {
		t.Fatal(err)
	}
	if count(t, dst, "assets") != 0 || count(t, dst, "fields") != 0 {
		t.Fatal("checking changed the instance")
	}
	if len(checked.Problems) > 0 || checked.Anchor == nil || checked.Anchor.Name != "Lab A" || checked.Assets == nil || checked.Assets.Assets != 2 {
		t.Fatalf("plan %+v", checked)
	}
	if it := itemOf(t, checked, ItemField, "Serial (serial)"); it.Action != ChoiceCreate || len(it.Choices) != 0 {
		t.Errorf("a field nothing clashes with: %+v", it)
	}
	done, err := Import(ctx, dst, blobs, opener(pkg), ImportOptions{Anchor: labA.ID, Fingerprint: checked.Fingerprint})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(checked, done) {
		t.Errorf("the check said\n%+v\nand the import did\n%+v", checked, done)
	}

	second, err := Import(ctx, dst, blobs, opener(pkg), ImportOptions{Anchor: labB.ID})
	if err != nil {
		t.Fatal(err)
	}
	// what is the same is used; what differs is copied
	if it := itemOf(t, second, ItemField, "Serial (serial)"); it.Action != ChoiceExisting || !it.Identical {
		t.Errorf("an identical field the second time: %+v", it)
	}
	if it := itemOf(t, second, ItemIconFile, "shared/crates.ilx"); it.Action != ChoiceExisting {
		t.Errorf("an identical library file the second time: %+v", it)
	}
	if it := itemOf(t, second, ItemFlow, "Lend-a-tool"); it.Action != ChoiceCopy || it.As != "lend-a-tool-2" {
		t.Errorf("a flow at the same address the second time: %+v", it)
	}
	if it := itemOf(t, second, ItemCategoryIcon, "toolbox"); it.Action != ChoiceCopy || it.As != "toolbox-2" {
		t.Errorf("a category's icon of the same name the second time: %+v", it)
	}

	for _, lab := range []*models.Category{labA, labB} {
		if got := childrenOf(t, dst, lab.ID); !reflect.DeepEqual(got, []string{"Workshop tools"}) {
			t.Errorf("under %s: %v", lab.Name, got)
		}
		tools := idOf(t, dst, "SELECT id FROM categories WHERE parent_id = ?", lab.ID)
		if got := childrenOf(t, dst, tools); !reflect.DeepEqual(got, []string{"Drills"}) {
			t.Errorf("under %s's tools: %v", lab.Name, got)
		}
	}
	for table, want := range map[string]int{
		"categories": 6, "fields": 2, "assets": 4, "asset_photos": 2, "asset_links": 2, "flows": 2, "category_icons": 2, "icon_library": 2,
		"templates": 2, "images": 1, "reports": count(t, dst, "reports"), "users": 0,
	} {
		if got := count(t, dst, table); got != want {
			t.Errorf("%s: %d rows, want %d", table, got, want)
		}
	}
	// each copy's assets are in its own categories, with their values
	drills := idOf(t, dst, "SELECT c.id FROM categories c JOIN categories p ON p.id = c.parent_id WHERE p.parent_id = ?", labB.ID)
	drill := idOf(t, dst, "SELECT asset_id FROM asset_categories WHERE category_id = ?", drills)
	if got := idOf(t, dst, "SELECT v.value FROM asset_field_values v JOIN fields f ON f.id = v.field_id WHERE v.asset_id = ? AND f.field_key = 'serial'", drill); got != "SN-1" {
		t.Errorf("the drill's serial %q", got)
	}
	// its photo's files are stored under its new IDs
	var photo, thumb string
	if err := dst.QueryRow("SELECT id, thumb_id FROM asset_photos WHERE asset_id = ?", drill).Scan(&photo, &thumb); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{photo, thumb} {
		rc, err := blobs.Get(ctx, "photos/"+k)
		if err != nil {
			t.Errorf("photos/%s: %v", k, err)
			continue
		}
		rc.Close()
	}
	// its history: what it had, with no one, and its import
	var kinds []string
	imports := 0
	rows, err := dst.Query("SELECT kind, payload, actor_user_id FROM events WHERE asset_id = ? ORDER BY kind", drill)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var kind, payload string
		var actor *string
		if err := rows.Scan(&kind, &payload, &actor); err != nil {
			t.Fatal(err)
		}
		kinds = append(kinds, kind)
		if actor != nil || strings.Contains(payload, "ada@example.com") || strings.Contains(payload, "borrower") {
			t.Errorf("an imported event names who: %s", payload)
		}
		if kind == models.EventAssetCreated && strings.Contains(payload, `"trigger":"import"`) {
			imports++
		}
	}
	rows.Close()
	want := []string{models.EventAssetCreated, models.EventAssetCreated, models.EventAssetEdited, "asset.condition_changed"}
	sort.Strings(want)
	if !reflect.DeepEqual(kinds, want) || imports != 1 {
		t.Errorf("the drill's history %v, %d of it its import", kinds, imports)
	}
	// the copied flow files into its own copy, and queues what it printed
	var def string
	if err := dst.QueryRow("SELECT definition FROM flows WHERE slug = 'lend-a-tool-2'").Scan(&def); err != nil {
		t.Fatal(err)
	}
	var d flows.Definition
	if err := json.Unmarshal([]byte(def), &d); err != nil {
		t.Fatal(err)
	}
	toolsB := idOf(t, dst, "SELECT id FROM categories WHERE parent_id = ?", labB.ID)
	if !reflect.DeepEqual(d.CategoryIDs, []string{toolsB}) || d.Steps[0].Label.Mode != flows.LabelQueue || d.Steps[0].Label.Destination != "" {
		t.Errorf("the copied flow %s", def)
	}
	if src := idOf(t, dst, "SELECT source FROM category_icons WHERE icon_name = 'toolbox-2'"); !strings.Contains(src, `icon "toolbox-2"`) {
		t.Errorf("the copied icon's source %s", src)
	}
}

// Each clash resolves as chosen, and what names a copy names it by its
// new key or name; a choice that would leave something without what it
// needs is a problem, and refused.
func TestConflictsResolveAsChosen(t *testing.T) {
	_, pkg := workshopPackage(t)
	dst := setupStore(t)
	blobs := filestore.NewLocal(t.TempDir())
	ctx := context.Background()
	serial := must[*models.Field](t)(dst.CreateField("serial", "Serial number", "text", nil))
	open := must[*models.AssetAction](t)(dst.SaveAction("", store.ActionInput{Key: "open", Label: "Open"}))
	stores := must[*models.RelationType](t)(dst.CreateRelationType(store.RelationTypeInput{Name: "Stores", InverseName: "Stored in"}))
	scan := must[*models.Report](t)(dst.CreateScanTemplate("Tool scan", []byte(`{"sections":[{"type":"details","title":"Here"}]}`)))

	linkTypes := count(t, dst, "relation_types")

	plan, err := CheckImport(ctx, dst, opener(pkg), ImportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if it := itemOf(t, plan, ItemField, "Serial (serial)"); it.Action != ChoiceCopy || it.Identical || it.Existing.ID != serial.ID || it.As != "serial_2" {
		t.Errorf("a field that differs: %+v", it)
	}
	if it := itemOf(t, plan, ItemAction, "Open (open)"); it.Action != ChoiceExisting || !it.Identical || it.Existing.ID != open.ID {
		t.Errorf("an action that is the same: %+v", it)
	}

	choices := map[string]Choice{
		ItemAction + ":" + itemOf(t, plan, ItemAction, "Open (open)").ID:           ChoiceCopy,
		ItemRelationType + ":" + itemOf(t, plan, ItemRelationType, "Stores").ID:    ChoiceSkip,
		ItemScanTemplate + ":" + itemOf(t, plan, ItemScanTemplate, "Tool scan").ID: ChoiceExisting,
		ItemField + ":" + itemOf(t, plan, ItemField, "Serial (serial)").ID:         ChoiceCopy,
		// what clashes with nothing may be chosen to be created, as it is
		ItemCategory + ":" + itemOf(t, plan, ItemCategory, "Drills").ID: ChoiceCreate,
	}
	done, err := Import(ctx, dst, blobs, opener(pkg), ImportOptions{Choices: choices})
	if err != nil {
		t.Fatal(err)
	}
	if it := itemOf(t, done, ItemAction, "Open (open)"); it.Action != ChoiceCopy || it.As != "open_2" {
		t.Errorf("the action copied: %+v", it)
	}
	if done.Assets.Links != 0 || done.Assets.LinksDropped != 1 {
		t.Errorf("links of a link type skipped: %+v", done.Assets)
	}
	// the copies, named anew, and what names them
	if got := idOf(t, dst, "SELECT label FROM fields WHERE field_key = 'serial_2'"); got != "Serial" {
		t.Errorf("the field copied: %q", got)
	}
	drawer := idOf(t, dst, "SELECT id FROM assets WHERE name = 'Drawer'")
	if got := idOf(t, dst, "SELECT f.field_key FROM asset_field_values v JOIN fields f ON f.id = v.field_id WHERE v.asset_id = ?", drawer); got != "serial_2" {
		t.Errorf("the drawer's serial is in %q", got)
	}
	tools := idOf(t, dst, "SELECT id FROM categories WHERE name = 'Workshop tools'")
	if got := idOf(t, dst, "SELECT f.field_key FROM category_fields cf JOIN fields f ON f.id = cf.field_id WHERE cf.category_id = ?", tools); got != "serial_2" {
		t.Errorf("the category records %q", got)
	}
	if got := idOf(t, dst, "SELECT scan_template_id FROM categories WHERE id = ?", tools); got != scan.ID {
		t.Errorf("the category's scan template %q, want the existing %q", got, scan.ID)
	}
	if n := count(t, dst, "category_relation_types"); n != 0 {
		t.Errorf("the link type skipped is suggested %d times", n)
	}
	if n := count(t, dst, "relation_types"); n != linkTypes || idOf(t, dst, "SELECT id FROM relation_types WHERE name = 'Stores'") != stores.ID {
		t.Errorf("%d link types, of %d", n, linkTypes)
	}
	def := idOf(t, dst, "SELECT definition FROM flows WHERE slug = 'lend-a-tool'")
	if !strings.Contains(def, `"action":"open_2"`) {
		t.Errorf("the flow presses %s", def)
	}

	// a choice that is not one, or for what clashes with nothing
	for _, bad := range []map[string]Choice{
		{ItemCategoryIcon + ":" + itemOf(t, plan, ItemCategoryIcon, "toolbox").ID: ChoiceExisting},
		{ItemCategory + ":" + itemOf(t, plan, ItemCategory, "Drills").ID: ChoiceSkip},
		{"field:nothing": ChoiceSkip},
	} {
		if _, err := CheckImport(ctx, dst, opener(pkg), ImportOptions{Choices: bad}); !errors.Is(err, ErrImport) {
			t.Errorf("%v: %v", bad, err)
		}
	}
	// skipping the action a flow presses leaves it without it
	before := count(t, dst, "categories")
	skip := map[string]Choice{ItemAction + ":" + itemOf(t, plan, ItemAction, "Open (open)").ID: ChoiceSkip}
	again, err := CheckImport(ctx, dst, opener(pkg), ImportOptions{Choices: skip})
	if err != nil || len(again.Problems) == 0 || !strings.Contains(strings.Join(again.Problems, " "), `presses the action "open", which is skipped`) {
		t.Errorf("problems %v, %v", again.Problems, err)
	}
	if _, err := Import(ctx, dst, blobs, opener(pkg), ImportOptions{Choices: skip}); !errors.Is(err, ErrImport) {
		t.Errorf("imported with problems: %v", err)
	}
	if count(t, dst, "categories") != before {
		t.Error("a refused import changed the instance")
	}
}

// A package needing a permission the importer lacks is refused at check,
// and so is a corrupted one, before anything changes; assets need
// assets.write in the scopes they will be in.
func TestAnImportIsRefusedBeforeAnythingChanges(t *testing.T) {
	_, pkg := workshopPackage(t)
	dst := setupStore(t)
	blobs := filestore.NewLocal(t.TempDir())
	ctx := context.Background()
	lab := must[*models.Category](t)(dst.CreateCategory("Lab", nil, "#111111", false, false))
	scope := must[*models.Scope](t)(dst.CreateScope("lab"))
	if _, err := dst.SetCategoryScopes(lab.ID, []string{scope.ID}); err != nil {
		t.Fatal(err)
	}

	noFlows := func(perm string, _ []string) bool { return perm != rbac.PermFlowsManage }
	plan, err := CheckImport(ctx, dst, opener(pkg), ImportOptions{May: noFlows})
	if !errors.Is(err, ErrForbidden) || plan == nil || !reflect.DeepEqual(plan.Missing, []string{rbac.PermFlowsManage}) {
		t.Errorf("without flows.manage: %v, %+v", err, plan)
	}
	if _, err := Import(ctx, dst, blobs, opener(pkg), ImportOptions{May: noFlows}); !errors.Is(err, ErrForbidden) {
		t.Errorf("imported without flows.manage: %v", err)
	}

	inLab := func(perm string, scopes []string) bool {
		return perm != rbac.PermAssetsWrite || slices.Contains(scopes, "lab")
	}
	if _, err := CheckImport(ctx, dst, opener(pkg), ImportOptions{May: inLab}); !errors.Is(err, ErrForbidden) {
		t.Errorf("assets at the top level, writing only in lab: %v", err)
	}
	if plan, err := CheckImport(ctx, dst, opener(pkg), ImportOptions{May: inLab, Anchor: lab.ID}); err != nil || !slices.Contains(plan.Permissions, "assets.write in lab") {
		t.Errorf("assets under lab: %v, %+v", err, plan)
	}

	bad := append([]byte(nil), pkg...)
	bad[len(bad)/2] ^= 0xff
	if _, err := CheckImport(ctx, dst, opener(bad), ImportOptions{}); !errors.Is(err, ErrInvalid) {
		t.Errorf("checking a corrupted package: %v", err)
	}
	if _, err := Import(ctx, dst, blobs, opener(bad), ImportOptions{}); !errors.Is(err, ErrInvalid) {
		t.Errorf("importing a corrupted package: %v", err)
	}
	for _, table := range []string{"assets", "fields", "flows", "asset_actions", "templates"} {
		if n := count(t, dst, table); n != 0 {
			t.Errorf("%d rows of %s after refusals", n, table)
		}
	}
	if n := count(t, dst, "categories"); n != 1 {
		t.Errorf("%d categories after refusals", n)
	}
	if err := blobs.List(ctx, "", func(o filestore.Object) error { t.Errorf("a file stored: %s", o.Key); return nil }); err != nil {
		t.Fatal(err)
	}
}

// Icons copied are renamed, with what they define, wherever the package
// names them, and the icons compile after.
func TestIconsCopiedAreRenamedWhereverTheyAreNamed(t *testing.T) {
	c := isolex.Default()
	if !c.Available() {
		if os.Getenv("BAM_REQUIRE_ISOLEX") != "" {
			t.Fatal("isolex is not available")
		}
		t.Skip("isolex is not available")
	}
	ctx := context.Background()
	src := setupStore(t)
	srcBlobs := filestore.NewLocal(t.TempDir())
	if _, _, err := src.CreateIconLibraryFile(ctx, c, "shared/crates.ilx", `base "crate-base" { category "storage"; footprint 1 1 1; part "box" { box 0 0 0 1 1 1 } }`); err != nil {
		t.Fatal(err)
	}
	storage := must[*models.Category](t)(src.CreateCategory("Storage", nil, "#123456", false, false))
	if save, err := src.SaveCategoryIcon(ctx, c, storage.ID, "", "crate", `icon "crate" { extends "crate-base" }`); err != nil || !save.Published {
		t.Fatalf("%+v %v", save, err)
	}
	pkg := exportPackage(t, src, srcBlobs, Selection{Categories: []string{storage.ID}})

	dst := setupStore(t)
	if _, _, err := dst.CreateIconLibraryFile(ctx, c, "shared/crates.ilx", `base "crate-base" { category "storage"; footprint 2 2 2; part "box" { box 0 0 0 2 2 2 } }`); err != nil {
		t.Fatal(err)
	}
	bins := must[*models.Category](t)(dst.CreateCategory("Bins", nil, "#654321", false, false))
	if save, err := dst.SaveCategoryIcon(ctx, c, bins.ID, "", "crate", `icon "crate" { extends "crate-base" }`); err != nil || !save.Published {
		t.Fatalf("%+v %v", save, err)
	}

	plan, err := Import(ctx, dst, filestore.NewLocal(t.TempDir()), opener(pkg), ImportOptions{Icons: c})
	if err != nil {
		t.Fatalf("%v: %+v", err, plan)
	}
	if it := itemOf(t, plan, ItemIconFile, "shared/crates.ilx"); it.Action != ChoiceCopy || it.As != "shared/crates-2.ilx" {
		t.Errorf("the library file: %+v", it)
	}
	if got := idOf(t, dst, "SELECT source FROM icon_library WHERE path = 'shared/crates-2.ilx'"); !strings.Contains(got, `base "crate-base-2"`) {
		t.Errorf("the library file copied: %s", got)
	}
	if got := idOf(t, dst, "SELECT source FROM category_icons WHERE icon_name = 'crate-2'"); got != `icon "crate-2" { extends "crate-base-2" }` {
		t.Errorf("the icon copied: %s", got)
	}
	save, err := dst.RebuildIcons(ctx, c)
	if err != nil || !save.Published || len(save.Errors) > 0 {
		t.Errorf("the icons after: %+v %v", save, err)
	}

	// an icon that would not compile is a problem
	if _, err := src.Exec("UPDATE category_icons SET source = ? WHERE category_id = ?", `icon "crate" { extends "nowhere" }`, storage.ID); err != nil {
		t.Fatal(err)
	}
	broken := exportPackage(t, src, srcBlobs, Selection{Categories: []string{storage.ID}})
	plan, err = CheckImport(ctx, dst, opener(broken), ImportOptions{Icons: c})
	if err != nil || len(plan.Problems) == 0 || !strings.Contains(plan.Problems[0], "the icons would not compile") {
		t.Errorf("an icon that does not compile: %v %v", plan.Problems, err)
	}
}

var uuidPattern = regexp.MustCompile(`[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)

// Round trip: a package imported into a new instance, and exported again
// from it, holds the same rows, by their new IDs.
func TestARoundTripKeepsEveryRow(t *testing.T) {
	ns := uuid.New()
	importNamespace = &ns
	t.Cleanup(func() { importNamespace = nil })
	src := setupStore(t)
	srcBlobs := filestore.NewLocal(t.TempDir())
	p := seedPackaged(t, src, srcBlobs)
	sel := Selection{Subtrees: []string{p.tools.ID}, Assets: &AssetsSelection{Photos: true, Links: true}}
	first := exportPackage(t, src, srcBlobs, sel)

	dst := setupStore(t)
	dstBlobs := filestore.NewLocal(t.TempDir())
	if _, err := Import(context.Background(), dst, dstBlobs, opener(first), ImportOptions{}); err != nil {
		t.Fatal(err)
	}
	newID := func(id string) string { return uuid.NewSHA1(ns, []byte(id)).String() }
	sel.Subtrees = []string{newID(p.tools.ID)}
	second := exportPackage(t, dst, dstBlobs, sel)

	a, b := readArchive(t, first), readArchive(t, second)
	for _, table := range Tables {
		// a flow is changed coming in: what it printed is queued
		if table.Name == "flows" {
			continue
		}
		var want, got []string
		for _, row := range a.rows(t, DataDir+table.Name+"/") {
			line, _ := json.Marshal(row)
			want = append(want, uuidPattern.ReplaceAllStringFunc(string(line), newID))
		}
		for _, row := range b.rows(t, DataDir+table.Name+"/") {
			line, _ := json.Marshal(row)
			got = append(got, string(line))
		}
		sort.Strings(want)
		sort.Strings(got)
		if !reflect.DeepEqual(want, got) {
			t.Errorf("%s:\nexported %v\nagain    %v", table.Name, want, got)
		}
	}
	if len(a.rows(t, DataDir+"flows/")) != len(b.rows(t, DataDir+"flows/")) {
		t.Error("the flows did not come back")
	}
}

// A backup is merged as a package is, the kinds chosen of it; never its
// people, settings or authentication.
func TestMergingPartsOfABackup(t *testing.T) {
	src := setupStore(t)
	srcBlobs := filestore.NewLocal(t.TempDir())
	seedPackaged(t, src, srcBlobs)
	var parts memParts
	if _, err := Write(context.Background(), src, srcBlobs, parts.open, Options{Secrets: true}); err != nil {
		t.Fatal(err)
	}
	backup := parts.parts[1].Bytes()
	ctx := context.Background()

	dst := setupStore(t)
	plan, err := Import(ctx, dst, filestore.NewLocal(t.TempDir()), opener(backup), ImportOptions{Kinds: []Kind{KindCatalog}, Web: true})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(plan.Kinds, []Kind{KindCatalog}) || plan.Assets != nil {
		t.Errorf("kinds %v, assets %+v", plan.Kinds, plan.Assets)
	}
	warned := strings.Join(plan.Warnings, " ")
	for _, w := range []string{"never imported through the web", "People and roles", "Settings"} {
		if !strings.Contains(warned, w) {
			t.Errorf("no warning of %q: %v", w, plan.Warnings)
		}
	}
	if count(t, dst, "categories") != count(t, src, "categories") || count(t, dst, "assets") != 0 || count(t, dst, "flows") != 0 {
		t.Errorf("%d categories, %d assets, %d flows", count(t, dst, "categories"), count(t, dst, "assets"), count(t, dst, "flows"))
	}

	plan, err = Import(ctx, dst, filestore.NewLocal(t.TempDir()), opener(backup), ImportOptions{})
	if err != nil {
		t.Fatalf("%v %+v", err, plan)
	}
	for _, table := range []string{"users", "sessions", "notification_channels", "asset_loans", "api_tokens", "printers"} {
		if n := count(t, dst, table); n != 0 {
			t.Errorf("%d rows of %s merged", n, table)
		}
	}
	if count(t, dst, "assets") != count(t, src, "assets") {
		t.Errorf("%d assets, of %d", count(t, dst, "assets"), count(t, src, "assets"))
	}
	if _, err := CheckImport(ctx, dst, opener(backup), ImportOptions{Kinds: []Kind{KindPeople}}); !errors.Is(err, ErrImport) {
		t.Errorf("merging people: %v", err)
	}
}

// RenameIcons renames definitions where they are made and where they are
// named, and nothing else: not text, comments, or names it was not given.
func TestRenameIcons(t *testing.T) {
	src := `// "crate" in a comment
base "crate" { part "box" { insert "lid"; text 0 0 "crate" font="mono" } }
icon #"crate"# extends="crate" { badge "lid" corner="ne" }
`
	got := RenameIcons(src, map[string]string{"crate": "crate-2", "lid": "lid-2"})
	want := `// "crate" in a comment
base "crate-2" { part "box" { insert "lid-2"; text 0 0 "crate" font="mono" } }
icon "crate-2" extends="crate-2" { badge "lid-2" corner="ne" }
`
	if got != want {
		t.Errorf("renamed:\n%s\nwant:\n%s", got, want)
	}
	if again := ScanIcons(got); !reflect.DeepEqual(again.Defines, []string{"crate-2"}) || !reflect.DeepEqual(again.Uses, []string{"lid-2", "mono", "crate-2"}) {
		t.Errorf("after: %+v", again)
	}
}
