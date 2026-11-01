package archive

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/filestore"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/flows"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

func TestScanIcons(t *testing.T) {
	src := `// a crate, and what it uses
/* not icon "commented" { extends "nothing" } */
base "crate-base" { category "storage"; footprint 1 1 1; part "box" { box 0 0 0 1 1 1 } }
icon "crate" extends="crate-base" {
  axes "state" "size"
  part "lid" { insert "lid-fragment" w=1 }
  part "mark" { badge "warning" corner="ne" }
  part "words" { text 0 0 "Crate \"one\"" font="mono" size=2 }
}
icon #"raw "name""# {
  extends "crate"
  part "label" { insert (text)"plate" }
}
fragment "lid-fragment" { param w 1; tile 0 0 0 1 1 }
`
	got := ScanIcons(src)
	if want := []string{"crate-base", "crate", `raw "name"`, "lid-fragment"}; !reflect.DeepEqual(got.Defines, want) {
		t.Errorf("defines %q, want %q", got.Defines, want)
	}
	if want := []string{"crate-base", "state", "size", "lid-fragment", "warning", "mono", "crate", "plate"}; !reflect.DeepEqual(got.Uses, want) {
		t.Errorf("uses %q, want %q", got.Uses, want)
	}
}

// packaged is an instance with two category trees, and what each names.
type packaged struct {
	seeded
	tools, drills, kitchen       *models.Category
	serial, colour, purchased    *models.Field
	open, wash                   string
	stores                       *models.RelationType
	toolLabel, flowLabel, unused string
	scan                         *models.Report
	crate, lid, other            string
	flow, kitchenFlow            string
	drill, kettle, drawer        string
}

func libraryFile(t *testing.T, st *store.Store, path, source string) string {
	t.Helper()
	id := uuid.NewString()
	if _, err := st.Exec("INSERT INTO icon_library (id, path, source, draft, draft_errors, updated_at) VALUES (?, ?, ?, ?, ?, ?)",
		id, path, source, "a draft", "[]", time.Now()); err != nil {
		t.Fatal(err)
	}
	return id
}

func labelTemplate(t *testing.T, st *store.Store, name, imageID string) string {
	t.Helper()
	id := uuid.NewString()
	layout := `{"elements":[]}`
	if imageID != "" {
		layout = `{"elements":[{"type":"static_image","x":0,"y":0,"width":5,"height":5,"image_id":"` + imageID + `"}]}`
	}
	if _, err := st.Exec("INSERT INTO templates (id, name, units, width, height, dpi, layout_json, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
		id, name, "mm", 50.0, 25.0, 300, layout, time.Now(), time.Now()); err != nil {
		t.Fatal(err)
	}
	return id
}

func flow(t *testing.T, st *store.Store, slug string, d flows.Definition, by string) string {
	t.Helper()
	id := uuid.NewString()
	b, _ := json.Marshal(d)
	if _, err := st.Exec("INSERT INTO flows (id, slug, name, definition, enabled, created_by, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
		id, slug, strings.ToUpper(slug[:1])+slug[1:], string(b), true, by, time.Now(), time.Now()); err != nil {
		t.Fatal(err)
	}
	return id
}

func must[T any](t *testing.T) func(T, error) T {
	return func(v T, err error) T {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
}

func seedPackaged(t *testing.T, st *store.Store, blobs filestore.Blobs) packaged {
	t.Helper()
	p := packaged{seeded: seed(t, st, blobs)}
	cat := must[*models.Category](t)
	field := must[*models.Field](t)
	p.tools = cat(st.CreateCategory("Workshop tools", nil, "#123456", false, false))
	p.drills = cat(st.CreateCategory("Drills", &p.tools.ID, "#654321", false, false))
	p.kitchen = cat(st.CreateCategory("Kitchen", nil, "#abcdef", false, false))
	p.serial = field(st.CreateField("serial", "Serial", "text", nil))
	p.purchased = field(st.CreateField("purchased", "Purchased", "date", nil))
	p.colour = field(st.CreateField("colour", "Colour", "text", nil))
	for _, a := range []struct {
		cat, field string
		required   bool
	}{{p.tools.ID, p.serial.ID, true}, {p.drills.ID, p.purchased.ID, false}, {p.kitchen.ID, p.colour.ID, false}} {
		if err := st.AssignCategoryField(a.cat, a.field, a.required); err != nil {
			t.Fatal(err)
		}
	}
	p.open = must[*models.AssetHook](t)(st.SaveHook("", store.HookInput{Key: "open", Label: "Open"})).ID
	p.wash = must[*models.AssetHook](t)(st.SaveHook("", store.HookInput{Key: "wash", Label: "Wash"})).ID
	if _, err := st.SetCategoryHooks(p.tools.ID, []models.CategoryHook{{HookID: p.open}}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SetCategoryHooks(p.kitchen.ID, []models.CategoryHook{{HookID: p.wash}}); err != nil {
		t.Fatal(err)
	}
	p.stores = must[*models.RelationType](t)(st.CreateRelationType(store.RelationTypeInput{Name: "Stores", InverseName: "Stored in"}))
	if _, err := st.SetCategoryRelationTypes(p.tools.ID, []string{p.stores.ID}); err != nil {
		t.Fatal(err)
	}
	p.toolLabel = labelTemplate(t, st, "Tool label", p.image.ID)
	p.flowLabel = labelTemplate(t, st, "Flow label", "")
	p.unused = labelTemplate(t, st, "Unused", "")
	if _, err := st.SetCategoryLabelTemplates(p.tools.ID, []string{p.toolLabel}); err != nil {
		t.Fatal(err)
	}
	p.scan = must[*models.Report](t)(st.CreateScanTemplate("Tool scan", []byte(`{"sections":[]}`)))
	if _, err := st.SetCategoryScanTemplate(p.tools.ID, &p.scan.ID); err != nil {
		t.Fatal(err)
	}
	p.crate = libraryFile(t, st, "shared/crates.ilx", `base "crate-base" { category "storage"; footprint 1 1 1; part "box" { insert "lid" } }`)
	p.lid = libraryFile(t, st, "shared/lids.ilx", `fragment "lid" { tile 0 0 1 1 1 }`)
	p.other = libraryFile(t, st, "shared/other.ilx", `base "pot" { category "kitchen"; footprint 1 1 1 }`)
	if _, err := st.Exec("INSERT INTO category_icons (category_id, rendition, icon_name, source, draft, draft_errors, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)",
		p.tools.ID, "screen", "toolbox", `icon "toolbox" { extends "crate-base" }`, "a draft", "[]", time.Now()); err != nil {
		t.Fatal(err)
	}
	p.flow = flow(t, st, "lend-a-tool", flows.Definition{Version: 1, CategoryIDs: []string{p.tools.ID}, Steps: []flows.Step{
		{ID: "label", Kind: flows.KindLabel, Label: &flows.Label{Mode: flows.LabelPrint, Destination: "printer:" + uuid.NewString(), TemplateID: p.flowLabel}},
		{ID: "change", Kind: flows.KindChange, Changes: []flows.Change{{Kind: flows.ChangeHook, Hook: "open"}}},
	}}, p.user.ID)
	p.kitchenFlow = flow(t, st, "wash-up", flows.Definition{Version: 1, CategoryIDs: []string{p.kitchen.ID}}, p.user.ID)

	asset := must[*models.AssetWithDetails](t)
	p.drill = asset(st.CreateAsset("Hammer drill", []string{p.drills.ID}, nil, map[string]string{"serial": "SN-1", "purchased": "2026-01-02"})).Asset.ID
	p.drawer = asset(st.CreateAsset("Drawer", []string{p.tools.ID}, nil, map[string]string{"serial": "SN-2"})).Asset.ID
	p.kettle = asset(st.CreateAsset("Kettle", []string{p.kitchen.ID}, nil, map[string]string{"colour": "red"})).Asset.ID
	if _, err := st.AddPhoto(p.drill, models.AssetPhoto{ID: uuid.NewString(), ThumbID: uuid.NewString(), Filename: "d.jpg", ContentType: "image/jpeg", WidthPx: 1, HeightPx: 1, ByteSize: 1, CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Exec("INSERT INTO asset_links (id, relation_type_id, from_asset_id, to_asset_id, created_at) VALUES (?, ?, ?, ?, ?)",
		uuid.NewString(), p.stores.ID, p.drawer, p.drill, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Exec("INSERT INTO events (id, kind, category_id, actor_user_id, payload, created_at, asset_id) VALUES (?, ?, ?, ?, ?, ?, ?)",
		uuid.NewString(), "asset.condition_changed", p.kitchen.ID, p.user.ID, `{"trigger":"manual","actor":{"id":"`+p.user.ID+`","email":"ada@example.com"},"borrower":{"name":"Ada"},"note":"oiled"}`, time.Now(), p.drill); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Exec("INSERT INTO asset_loans (id, asset_id, borrower_id, lent_by, checked_out_at) VALUES (?, ?, ?, ?, ?)",
		uuid.NewString(), p.drill, p.user.ID, p.user.ID, time.Now()); err != nil {
		t.Fatal(err)
	}
	return p
}

// packageOf writes a package of a selection, and reads it back.
func packageOf(t *testing.T, st *store.Store, blobs filestore.Blobs, sel Selection, may Reader) *read {
	t.Helper()
	var parts memParts
	if _, err := Write(context.Background(), st, blobs, parts.open, Options{Select: &sel, May: may, Info: &PackageInfo{Name: "Workshop", Author: "Hackspace", License: "CC0-1.0", Version: "1.0"}}); err != nil {
		t.Fatal(err)
	}
	b := parts.parts[1].Bytes()
	if _, err := Check(bytes.NewReader(b)); err != nil {
		t.Fatalf("the package does not check out: %v", err)
	}
	return readArchive(t, b)
}

// ids are the values of a column in a table's rows in an archive.
func ids(t *testing.T, r *read, table, col string) []string {
	t.Helper()
	var out []string
	for _, row := range r.rows(t, DataDir+table+"/") {
		s, _ := row[col].(string)
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

func sorted(xs ...string) []string {
	sort.Strings(xs)
	return xs
}

// A subtree brings what its categories need, and what that needs, and
// nothing else: no people, no secrets, no other category's things.
func TestAPackageClosesOverWhatItNeeds(t *testing.T) {
	st := setupStore(t)
	blobs := filestore.NewLocal(t.TempDir())
	p := seedPackaged(t, st, blobs)

	r := packageOf(t, st, blobs, Selection{Subtrees: []string{p.tools.ID}, Assets: &AssetsSelection{Photos: true, Links: true, History: true}}, nil)
	if r.header.Type != "package" || r.header.Secrets || r.header.Package == nil || r.header.Package.Name != "Workshop" || r.manifest.Package == nil || r.manifest.Package.License != "CC0-1.0" {
		t.Errorf("header %+v, manifest's package %+v", r.header, r.manifest.Package)
	}
	for _, k := range r.header.Kinds {
		if k == KindPeople || k == KindAuth || k == KindSettings {
			t.Errorf("a package holds %s", k)
		}
	}
	for _, c := range []struct {
		table, col string
		want       []string
	}{
		{"categories", "id", sorted(p.tools.ID, p.drills.ID)},
		{"fields", "id", sorted(p.serial.ID, p.purchased.ID)},
		{"asset_actions", "id", []string{p.open}},
		{"relation_types", "id", []string{p.stores.ID}},
		{"templates", "id", sorted(p.toolLabel, p.flowLabel)},
		{"images", "id", []string{p.image.ID}},
		{"reports", "id", []string{p.scan.ID}},
		{"flows", "id", []string{p.flow}},
		{"icon_library", "id", sorted(p.crate, p.lid)},
		{"category_icons", "category_id", []string{p.tools.ID}},
		{"assets", "id", sorted(p.drill, p.drawer)},
		{"asset_field_values", "asset_id", sorted(p.drawer, p.drill, p.drill)},
		{"asset_links", "from_asset_id", []string{p.drawer}},
	} {
		if got := ids(t, r, c.table, c.col); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: %v, want %v", c.table, got, c.want)
		}
	}
	for _, table := range []string{"users", "sessions", "roles", "user_roles", "api_tokens", "asset_loans", "flow_filings", "notification_channels", "printers", "settings", "icon_palette", "icon_revisions", "scopes"} {
		if rows := r.rows(t, DataDir+table+"/"); len(rows) > 0 {
			t.Errorf("a package holds %d rows of %s", len(rows), table)
		}
	}
	for path := range r.members {
		if strings.HasPrefix(path, AuthDir) {
			t.Errorf("a package holds %s", path)
		}
	}
	if _, ok := r.members["files/images/"+p.image.ID]; !ok {
		t.Error("the label template's image is not in the package")
	}
	// who did things is not in it, nor drafts
	flowRow := r.rows(t, DataDir+"flows/")[0]
	if flowRow["created_by"] != nil {
		t.Errorf("the flow's maker: %v", flowRow["created_by"])
	}
	for _, row := range r.rows(t, DataDir+"icon_library/") {
		if row["draft"] != nil || row["draft_errors"] != nil {
			t.Errorf("a library file's draft: %v", row)
		}
	}
	var edited map[string]any
	for _, row := range r.rows(t, DataDir+"events/") {
		if row["actor_user_id"] != nil {
			t.Errorf("an event names who did it: %v", row)
		}
		if row["kind"] == "asset.condition_changed" {
			edited = row
		}
		if strings.Contains(string(mustJSON(t, row)), "ada@example.com") || strings.Contains(string(mustJSON(t, row)), p.user.ID) {
			t.Errorf("an event names a person: %v", row)
		}
	}
	if edited == nil || edited["category_id"] != nil || edited["payload"].(map[string]any)["note"] != "oiled" {
		t.Errorf("the edit's history: %v", edited)
	}
	// Drills is under Workshop tools; Workshop tools is at the top, its
	// parent gone
	for _, row := range r.rows(t, DataDir+"categories/") {
		if row["id"] == p.drills.ID && row["parent_id"] != p.tools.ID || row["id"] == p.tools.ID && row["parent_id"] != nil {
			t.Errorf("category %v", row)
		}
	}

	// the plan says what needs what, and what stays behind
	plan, err := Resolve(context.Background(), st, st.DB, Selection{Subtrees: []string{p.tools.ID}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	needed := map[string][]string{}
	for _, it := range plan.Items {
		for _, by := range it.NeededBy {
			needed[it.Kind+" "+it.Name] = append(needed[it.Kind+" "+it.Name], by.Kind+" "+by.Name)
		}
	}
	for item, by := range map[string]string{
		"field Serial":                        "category Workshop tools",
		"flow Lend-a-tool":                    "category Workshop tools",
		"label_template Flow label":           "flow Lend-a-tool",
		"hook Open":                           "category Workshop tools",
		"icon_library_file shared/lids.ilx":   "icon_library_file shared/crates.ilx",
		"icon_library_file shared/crates.ilx": "category Workshop tools",
		"image logo.png":                      "label_template Tool label",
	} {
		if !hasString(needed[item], by) {
			t.Errorf("%s is needed by %v, not %s", item, needed[item], by)
		}
	}
	if len(plan.LeftOut) != 1 || plan.LeftOut[0].Kind != ItemPrinter || plan.LeftOut[0].NeededBy[0].ID != p.flow || !strings.Contains(plan.LeftOut[0].Reason, "print queue") {
		t.Errorf("left out: %+v", plan.LeftOut)
	}

	// a package is imported, never restored
	var parts memParts
	if _, err := Write(context.Background(), st, blobs, parts.open, Options{Select: &Selection{Fields: []string{p.serial.ID}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := Restore(context.Background(), emptyStore(t, envDialect()), blobs, opener(parts.parts[1].Bytes()), RestoreOptions{}); !errors.Is(err, ErrNotBackup) {
		t.Errorf("restoring a package: %v", err)
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// A category under one left behind keeps what it inherits, as its own.
func TestATopCategoryKeepsWhatItInherits(t *testing.T) {
	st := setupStore(t)
	blobs := filestore.NewLocal(t.TempDir())
	p := seedPackaged(t, st, blobs)

	r := packageOf(t, st, blobs, Selection{Categories: []string{p.drills.ID}, Assets: &AssetsSelection{}}, nil)
	if got := ids(t, r, "categories", "id"); !reflect.DeepEqual(got, []string{p.drills.ID}) {
		t.Fatalf("categories %v", got)
	}
	fields := map[string]bool{}
	for _, row := range r.rows(t, DataDir+"category_fields/") {
		if row["category_id"] != p.drills.ID {
			t.Errorf("%v", row)
		}
		fields[row["field_id"].(string)] = row["is_required"].(bool)
	}
	if !reflect.DeepEqual(fields, map[string]bool{p.serial.ID: true, p.purchased.ID: false}) {
		t.Errorf("Drills' fields %v", fields)
	}
	if got := ids(t, r, "category_label_templates", "template_id"); !reflect.DeepEqual(got, []string{p.toolLabel}) {
		t.Errorf("Drills' label templates %v", got)
	}
	row := r.rows(t, DataDir+"categories/")[0]
	if row["parent_id"] != nil || row["scan_template_id"] != p.scan.ID {
		t.Errorf("Drills: %v", row)
	}
	// its asset keeps both its values
	if got := ids(t, r, "asset_field_values", "field_id"); !reflect.DeepEqual(got, sorted(p.serial.ID, p.purchased.ID)) {
		t.Errorf("values %v", got)
	}
	if got := ids(t, r, "assets", "id"); !reflect.DeepEqual(got, []string{p.drill}) {
		t.Errorf("assets %v", got)
	}
}

// Every asset, the whole icon library, and each kind chosen on its own.
func TestEachKindOfSelection(t *testing.T) {
	st := setupStore(t)
	blobs := filestore.NewLocal(t.TempDir())
	p := seedPackaged(t, st, blobs)
	scope := must[*models.Scope](t)(st.CreateScope("workshop"))
	if _, err := st.SetCategoryScopes(p.tools.ID, []string{scope.ID}); err != nil {
		t.Fatal(err)
	}

	r := packageOf(t, st, blobs, Selection{Assets: &AssetsSelection{All: true}}, nil)
	if got := ids(t, r, "assets", "id"); !reflect.DeepEqual(got, sorted(p.drill, p.drawer, p.kettle, assetNamed(t, st, "Drill"))) {
		t.Errorf("every asset: %v", got)
	}
	if photos := r.rows(t, DataDir+"asset_photos/"); len(photos) != 0 {
		t.Errorf("photos without asking: %v", photos)
	}
	if links := r.rows(t, DataDir+"asset_links/"); len(links) != 0 {
		t.Errorf("links without asking: %v", links)
	}

	r = packageOf(t, st, blobs, Selection{Icons: true}, nil)
	if got := ids(t, r, "icon_library", "id"); !reflect.DeepEqual(got, sorted(p.crate, p.lid, p.other)) {
		t.Errorf("the library: %v", got)
	}
	if n := len(r.rows(t, DataDir+"icon_palette/")); n == 0 || n != count(t, st, "icon_palette") || len(r.rows(t, DataDir+"icon_settings/")) != 1 {
		t.Errorf("the palette and settings: %d rows", n)
	}
	if rows := r.rows(t, DataDir+"categories/"); len(rows) != 0 {
		t.Errorf("categories with the library: %v", rows)
	}

	for _, c := range []struct {
		sel   Selection
		table string
		want  string
	}{
		{Selection{Fields: []string{p.colour.ID}}, "fields", p.colour.ID},
		{Selection{Hooks: []string{p.wash}}, "asset_actions", p.wash},
		{Selection{RelationTypes: []string{p.stores.ID}}, "relation_types", p.stores.ID},
		{Selection{LabelTemplates: []string{p.unused}}, "templates", p.unused},
		{Selection{ScanTemplates: []string{p.scan.ID}}, "reports", p.scan.ID},
		{Selection{Flows: []string{p.kitchenFlow}}, "flows", p.kitchenFlow},
		{Selection{Scopes: []string{scope.ID}}, "scopes", scope.ID},
	} {
		r := packageOf(t, st, blobs, c.sel, nil)
		if got := ids(t, r, c.table, "id"); !reflect.DeepEqual(got, []string{c.want}) {
			t.Errorf("%+v: %s %v", c.sel, c.table, got)
		}
	}
	// the kitchen's flow brings its category, which brings its own
	r = packageOf(t, st, blobs, Selection{Flows: []string{p.kitchenFlow}}, nil)
	if got := ids(t, r, "categories", "id"); !reflect.DeepEqual(got, []string{p.kitchen.ID}) {
		t.Errorf("the kitchen flow's categories %v", got)
	}
	// a scope chosen with its category keeps it there
	r = packageOf(t, st, blobs, Selection{Categories: []string{p.tools.ID}, Scopes: []string{scope.ID}}, nil)
	if got := ids(t, r, "category_scopes", "scope_id"); !reflect.DeepEqual(got, []string{scope.ID}) {
		t.Errorf("category scopes %v", got)
	}
}

// A label template brings the fields it binds and the icons it draws by
// name; a report, the fields it shows or groups by and the categories it
// looks in.
func TestTemplatesAndReportsBringWhatTheyName(t *testing.T) {
	st := setupStore(t)
	blobs := filestore.NewLocal(t.TempDir())
	p := seedPackaged(t, st, blobs)
	tmpl := uuid.NewString()
	layout := `{"elements":[{"type":"text","x":0,"y":0,"width":5,"height":5,"content_binding":"Colour: {{field.colour}}"},` +
		`{"type":"icon","x":0,"y":0,"width":5,"height":5,"icon":"pot"}]}`
	if _, err := st.Exec("INSERT INTO templates (id, name, units, width, height, dpi, layout_json, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
		tmpl, "Kitchen label", "mm", 50.0, 25.0, 300, layout, time.Now(), time.Now()); err != nil {
		t.Fatal(err)
	}
	r := packageOf(t, st, blobs, Selection{LabelTemplates: []string{tmpl}}, nil)
	if got := ids(t, r, "fields", "id"); !reflect.DeepEqual(got, []string{p.colour.ID}) {
		t.Errorf("the template's fields %v", got)
	}
	if got := ids(t, r, "icon_library", "id"); !reflect.DeepEqual(got, []string{p.other}) {
		t.Errorf("the template's icons %v", got)
	}

	report := must[*models.Report](t)(st.CreateReport("Serials", []byte(`{"filter":{"category_ids":["`+p.kitchen.ID+`"]},`+
		`"sections":[{"type":"table","columns":[{"heading":"Serial","binding":"{{field.serial}}"}]},{"type":"chart","chart":"bar","measure":"count","group_by":"field:purchased"}]}`)))
	r = packageOf(t, st, blobs, Selection{Reports: []string{report.ID}}, nil)
	if got := ids(t, r, "fields", "id"); !reflect.DeepEqual(got, sorted(p.serial.ID, p.purchased.ID, p.colour.ID)) {
		t.Errorf("the report's fields %v", got)
	}
	if got := ids(t, r, "categories", "id"); !reflect.DeepEqual(got, []string{p.kitchen.ID}) {
		t.Errorf("the report's categories %v", got)
	}
}

func assetNamed(t *testing.T, st *store.Store, name string) string {
	t.Helper()
	var id string
	if err := st.QueryRow("SELECT id FROM assets WHERE name = ?", name).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

// A reader with assets.read in one scope packages only the assets there,
// and without flows.run, no flows; what it cannot read, it cannot choose.
func TestAScopedReaderPackagesWhatItReads(t *testing.T) {
	st := setupStore(t)
	blobs := filestore.NewLocal(t.TempDir())
	p := seedPackaged(t, st, blobs)
	scope := must[*models.Scope](t)(st.CreateScope("workshop"))
	if _, err := st.SetCategoryScopes(p.tools.ID, []string{scope.ID}); err != nil {
		t.Fatal(err)
	}
	perms := map[string]bool{"workshop" + rbac.ScopeSeparator + rbac.PermAssetsRead: true}
	may := func(perm string, scopes []string) bool {
		if scopes == nil {
			for k := range perms {
				if _, p := rbac.SplitKey(k); p == perm {
					return true
				}
			}
			return false
		}
		return rbac.Allows(perms, perm, scopes)
	}

	r := packageOf(t, st, blobs, Selection{Subtrees: []string{p.tools.ID}, Assets: &AssetsSelection{All: true, History: true}}, may)
	if got := ids(t, r, "assets", "id"); !reflect.DeepEqual(got, sorted(p.drill, p.drawer)) {
		t.Errorf("a scoped reader's assets: %v", got)
	}
	if got := ids(t, r, "flows", "id"); len(got) != 0 {
		t.Errorf("flows without flows.run: %v", got)
	}
	if events := r.rows(t, DataDir+"events/"); len(events) != 0 {
		t.Errorf("history without events.read: %v", events)
	}
	plan, err := Resolve(context.Background(), st, st.DB, Selection{Assets: &AssetsSelection{All: true}}, may)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Assets.Assets != 2 || plan.Assets.Unreadable != 2 {
		t.Errorf("assets %+v", plan.Assets)
	}

	for _, sel := range []Selection{
		{},
		{Flows: []string{p.flow}},
		{Categories: []string{uuid.NewString()}},
		{Reports: []string{p.scan.ID}}, // a scan template, not a report
	} {
		if _, err := Resolve(context.Background(), st, st.DB, sel, may); !errors.Is(err, ErrSelection) {
			t.Errorf("%+v: %v", sel, err)
		}
	}
}

// A package's preview image is a member of it, by its type.
func TestAPackagesPreview(t *testing.T) {
	st := setupStore(t)
	blobs := filestore.NewLocal(t.TempDir())
	p := seedPackaged(t, st, blobs)
	png := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR")
	var parts memParts
	if _, err := Write(context.Background(), st, blobs, parts.open, Options{Select: &Selection{Fields: []string{p.serial.ID}}, Preview: png}); err != nil {
		t.Fatal(err)
	}
	r := readArchive(t, parts.parts[1].Bytes())
	if r.header.Package.Preview != "preview.png" || string(r.members["preview.png"]) != string(png) {
		t.Errorf("preview %q", r.header.Package.Preview)
	}
	if _, err := Check(bytes.NewReader(parts.parts[1].Bytes())); err != nil {
		t.Error(err)
	}
	if _, err := Write(context.Background(), st, blobs, (&memParts{}).open, Options{Select: &Selection{Fields: []string{p.serial.ID}}, Preview: []byte("GIF89a")}); !errors.Is(err, ErrSelection) {
		t.Errorf("a GIF preview: %v", err)
	}
}
