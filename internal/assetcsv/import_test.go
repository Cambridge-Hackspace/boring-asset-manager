package assetcsv

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/config"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// openStore opens a migrated store on SQLite, or on the server named by
// BAM_TEST_DIALECT and BAM_TEST_DSN.
func openStore(t testing.TB) *store.Store {
	t.Helper()
	var st *store.Store
	if dialect := os.Getenv(store.TestDialectEnv); dialect != "" && dialect != "sqlite" {
		s, cleanup, err := store.OpenEphemeral(dialect, os.Getenv(store.TestDSNEnv))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(cleanup)
		st = s
	} else {
		s, err := store.Open(&config.DatabaseConfig{Type: "sqlite", DSN: filepath.Join(t.TempDir(), "test.db")})
		if err != nil {
			t.Fatal(err)
		}
		st = s
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	if err := st.SeedDefaultPermissionsAndRoles(); err != nil {
		t.Fatal(err)
	}
	return st
}

// world is a small inventory to import into.
type world struct {
	st                  *store.Store
	tools, drills, tape string
	drill, hammer       *models.AssetWithDetails
}

func newWorld(t testing.TB) *world {
	t.Helper()
	st := openStore(t)
	w := &world{st: st}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	tools, err := st.CreateCategory("Tools", nil, "", false, false)
	must(err)
	drills, err := st.CreateCategory("Drills", &tools.ID, "", false, false)
	must(err)
	tape, err := st.CreateCategory("Tape", nil, "", true, false)
	must(err)
	w.tools, w.drills, w.tape = tools.ID, drills.ID, tape.ID
	serial, err := st.CreateField("serial", "Serial number", "text", nil)
	must(err)
	_, err = st.CreateField("condition", "Condition", "select", []byte(`{"choices":[{"label":"Good","value":"good"},{"label":"Broken","value":"broken"}]}`))
	must(err)
	_, err = st.CreateField("weight", "Weight", "number", nil)
	must(err)
	must(st.AssignCategoryField(drills.ID, serial.ID, true))
	w.drill, err = st.CreateAsset("Drill", []string{drills.ID}, []models.AssetGeneratorKey{{GeneratorType: "code128", AssignedCode: "D1"}}, map[string]string{"serial": "SN1", "condition": "good"})
	must(err)
	w.hammer, err = st.CreateAsset("Hammer", []string{tools.ID}, []models.AssetGeneratorKey{{GeneratorType: "code128", AssignedCode: "H1"}}, nil)
	must(err)
	return w
}

func (w *world) prepare(t *testing.T, file string, mapping []string) *Plan {
	t.Helper()
	plan, err := Prepare(w.st, []byte(file), mapping)
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

// apply plans a file with the suggested mapping and imports it.
func (w *world) apply(t *testing.T, file string) *Plan {
	t.Helper()
	plan := w.prepare(t, file, nil)
	if plan.ProblemCount > 0 {
		t.Fatalf("problems: %+v", plan.Problems)
	}
	if _, err := w.st.ImportAssets(plan.Changes); err != nil {
		t.Fatal(err)
	}
	return plan
}

func hasProblem(p *Plan, row int, column, text string) bool {
	for _, pr := range p.Problems {
		if pr.Row == row && pr.Column == column && strings.Contains(pr.Message, text) {
			return true
		}
	}
	return false
}

func TestSuggestMapping(t *testing.T) {
	corpus := []models.Field{{Key: "serial", Label: "Serial number"}, {Key: "notes", Label: "Notes"}}
	got := Suggest([]string{"ID", " Name ", "categories", "QR", "code.code128", "Serial Number", "field.notes", "serial", "Colour"}, corpus)
	want := []string{"id", "name", "categories", "code.qr", "code.code128", "field.serial", "field.notes", "", ""}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

func TestMappingProblems(t *testing.T) {
	w := newWorld(t)
	file := "name,colour,a,b,c\nDrill,red,1,2,3\n"
	plan := w.prepare(t, file, []string{"name", "", "field.nope", "code.qr", "code.qr"})
	for _, want := range []struct{ col, text string }{
		{"colour", "not mapped"},
		{"a", `no field has the key "nope"`},
		{"c", `"b" and "c" both map to code.qr`},
	} {
		if !hasProblem(plan, 1, want.col, want.text) {
			t.Errorf("missing %+v in %+v", want, plan.Problems)
		}
	}
	if plan.Create != 0 || plan.Changes != nil {
		t.Error("rows planned despite mapping problems")
	}
	plan = w.prepare(t, "colour\nred\n", []string{"ignore"})
	if !hasProblem(plan, 1, "", "map a column to name") {
		t.Errorf("no name or id: %+v", plan.Problems)
	}
	plan = w.prepare(t, "name\nx\n", []string{"bogus"})
	if !hasProblem(plan, 1, "name", "unknown target") {
		t.Errorf("bogus target: %+v", plan.Problems)
	}
	if _, err := Prepare(w.st, []byte("name,x\nDrill,1\n"), []string{"name"}); !errors.Is(err, ErrUnreadable) {
		t.Errorf("short mapping: %v", err)
	}
}

func TestUnreadableFiles(t *testing.T) {
	w := newWorld(t)
	var big strings.Builder
	big.WriteString("name\n")
	for i := 0; i <= MaxRows; i++ {
		fmt.Fprintf(&big, "Asset %d\n", i)
	}
	for name, file := range map[string]string{
		"empty":     "",
		"bad quote": "name\n\"Drill\n",
		"too long":  big.String(),
		"too big":   "name\n" + strings.Repeat("x", MaxBytes),
	} {
		if _, err := Prepare(w.st, []byte(file), nil); !errors.Is(err, ErrUnreadable) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestRowProblems(t *testing.T) {
	w := newWorld(t)
	file := "id,name,categories,code.code128,code.micro_qr,field.serial,field.condition,field.weight\n" +
		",New drill,Tools > Drills,N1,,,,\n" + // 2: required serial missing
		",Thing,Tools > Hammers; Garden,,,,,\n" + // 3: unknown categories
		",,Tools,,,,,\n" + // 4: no name
		",Bad,Tools,,lower case,,meh,heavy\n" + // 5: bad micro QR, select and number
		",Short\n" + // 6: wrong cell count
		"00000000-0000-0000-0000-000000000000,X,,,,,,\n" + // 7: unknown ID
		"not-a-uuid,X,,,,,,\n" + // 8: unknown ID
		w.hammer.Asset.ID + ",,,,,,,\n" + // 9: fine (unchanged)
		w.hammer.Asset.ID + ",,,,,,,\n" // 10: listed twice
	plan := w.prepare(t, file, nil)
	for _, want := range []struct {
		row       int
		col, text string
	}{
		{2, "field.serial", `required field "Serial number" (serial) is missing`},
		{3, "categories", `unknown category "Tools > Hammers"`},
		{3, "categories", `unknown category "Garden"`},
		{4, "name", "needs a name"},
		{5, "code.micro_qr", ""},
		{5, "field.condition", ""},
		{5, "field.weight", ""},
		{6, "", "2 cells but the header has 8"},
		{7, "id", "no asset has the ID"},
		{8, "id", "no asset has the ID"},
		{10, "id", "also on line 9"},
	} {
		if !hasProblem(plan, want.row, want.col, want.text) {
			t.Errorf("missing %+v", want)
		}
	}
	if plan.ProblemCount != 11 || len(plan.Problems) != 11 {
		t.Errorf("%d problems: %+v", plan.ProblemCount, plan.Problems)
	}
	if strings.Join(plan.UnknownCategories, "|") != "Garden|Tools > Hammers" {
		t.Errorf("unknown categories %q", plan.UnknownCategories)
	}
	if plan.Changes != nil || plan.Create+plan.Update+plan.Unchanged != 0 {
		t.Error("a plan with problems has changes")
	}

	// Archived assets are named as such.
	if err := w.st.DeleteAsset(w.hammer.Asset.ID); err != nil {
		t.Fatal(err)
	}
	plan = w.prepare(t, "id,name\n"+w.hammer.Asset.ID+",Hammer\n", nil)
	if !hasProblem(plan, 2, "id", "is archived") {
		t.Errorf("archived: %+v", plan.Problems)
	}
}

func TestCreateAndUpdate(t *testing.T) {
	w := newWorld(t)
	file := "id,name,categories,code.code128,field.serial,field.condition\n" +
		",Saw,Tools,S1,,\n" +
		",Glue,,,,\n" +
		w.drill.Asset.ID + ",,,,,broken\n" + // only the condition changes
		w.hammer.Asset.ID + ",Hammer,Tools,H1,,\n" // unchanged
	plan := w.apply(t, file)
	if plan.Create != 2 || plan.Update != 1 || plan.Unchanged != 1 {
		t.Fatalf("plan %+v", plan)
	}
	drill, _ := w.st.GetAssetByID(w.drill.Asset.ID)
	if drill.Asset.Name != "Drill" || drill.FieldValues["serial"] != "SN1" || drill.FieldValues["condition"] != "broken" ||
		len(drill.Categories) != 1 || drill.Categories[0].ID != w.drills || drill.GeneratorKeys[0].AssignedCode != "D1" {
		t.Errorf("drill %+v", drill)
	}
	saw, err := w.st.ListAssets("", "Saw")
	if err != nil || len(saw) != 1 || saw[0].Categories[0].ID != w.tools || saw[0].GeneratorKeys[0].AssignedCode != "S1" {
		t.Errorf("saw %+v %v", saw, err)
	}
	glue, _ := w.st.ListAssets("", "Glue")
	if len(glue) != 1 || glue[0].Categories[0].ID != "uncategorized" {
		t.Errorf("glue %+v", glue)
	}
}

func TestCodesMustEndUpUnique(t *testing.T) {
	w := newWorld(t)
	// Against another asset, and twice within the file.
	plan := w.prepare(t, "name,code.code128\nA,H1\nB,X\nC,X\n", nil)
	if !hasProblem(plan, 2, "code.code128", `"H1" is also assigned to asset "Hammer"`) ||
		!hasProblem(plan, 3, "code.code128", "also assigned to line 4") ||
		!hasProblem(plan, 4, "code.code128", "also assigned to line 3") {
		t.Errorf("%+v", plan.Problems)
	}

	// Moving codes between assets in one file is fine, and so is a new asset
	// taking a code another row gives up.
	file := "id,name,code.code128\n" +
		w.drill.Asset.ID + ",,H1\n" +
		w.hammer.Asset.ID + ",,D2\n" +
		",Chisel,D1\n"
	w.apply(t, file)
	codes := map[string]string{}
	for _, a := range mustList(t, w.st) {
		codes[a.Asset.Name] = a.GeneratorKeys[0].AssignedCode
	}
	if codes["Drill"] != "H1" || codes["Hammer"] != "D2" || codes["Chisel"] != "D1" {
		t.Errorf("codes %v", codes)
	}
}

func mustList(t *testing.T, st *store.Store) []models.AssetWithDetails {
	t.Helper()
	list, err := st.ListAssets("", "")
	if err != nil {
		t.Fatal(err)
	}
	return list
}

// TestExportThenImportChangesNothing is the round trip.
func TestExportThenImportChangesNothing(t *testing.T) {
	w := newWorld(t)
	// Awkward values: formulas, quotes, separators, a category named like a path.
	odd, _ := w.st.CreateCategory("A > B; C", nil, "", false, false)
	if _, err := w.st.CreateAsset(`=cmd|' /C calc'!A0`, []string{odd.ID, w.tools}, nil, map[string]string{"weight": "-5"}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.st.CreateAsset("Quote \"me\", please", nil, nil, map[string]string{"condition": "good"}); err != nil {
		t.Fatal(err)
	}
	before := mustList(t, w.st)
	cats, _ := w.st.ListAllCategories()
	var b strings.Builder
	if err := Export(&b, before, NewCategories(cats), []string{"qr", "micro_qr", "code128"}); err != nil {
		t.Fatal(err)
	}
	plan := w.prepare(t, b.String(), nil)
	if plan.ProblemCount != 0 || plan.Unchanged != len(before) || plan.Create+plan.Update != 0 {
		t.Fatalf("plan %+v", plan)
	}
}

// TestImportIsAllOrNothing makes the last row fail at write time, after the
// check passed, and checks nothing from the earlier rows remains.
func TestImportIsAllOrNothing(t *testing.T) {
	w := newWorld(t)
	plan := w.prepare(t, "name,code.code128\nOne,O1\nTwo,O2\nThree,O3\n", nil)
	if plan.ProblemCount != 0 {
		t.Fatal(plan.Problems)
	}
	// Someone takes O3 between the check and the import.
	if _, err := w.st.CreateAsset("Sneaky", nil, []models.AssetGeneratorKey{{GeneratorType: "code128", AssignedCode: "O3"}}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := w.st.ImportAssets(plan.Changes); !errors.Is(err, store.ErrGeneratorCodeExists) {
		t.Fatalf("got %v", err)
	}
	for _, a := range mustList(t, w.st) {
		if a.Asset.Name == "One" || a.Asset.Name == "Two" {
			t.Errorf("%s was imported", a.Asset.Name)
		}
	}
}

// TestImportChecksThresholdsOnce imports many assets into a category that is
// running low; it restocks once.
func TestImportChecksThresholdsOnce(t *testing.T) {
	w := newWorld(t)
	two := 2
	if _, err := w.st.UpdateCategoryFrom(w.tape, store.CategoryInput{Name: "Tape", IsConsumable: true, LowStockThreshold: &two}); err != nil {
		t.Fatal(err)
	}
	before, _ := w.st.ListEvents(100)
	w.apply(t, "name,categories\nRoll 1,Tape\nRoll 2,Tape\nRoll 3,Tape\n")
	after, _ := w.st.ListEvents(100)
	if len(after) != len(before)+1 || after[0].Kind != models.EventRestocked || *after[0].Payload.ActiveCount != 3 {
		t.Errorf("events %+v", after[:len(after)-len(before)])
	}
}

func TestColumnsCarrySamples(t *testing.T) {
	w := newWorld(t)
	plan := w.prepare(t, "name,notes,empty\nSaw,,\nGlue,sticky,\n", []string{"name", "ignore", "ignore"})
	got := []string{plan.Columns[0].Sample, plan.Columns[1].Sample, plan.Columns[2].Sample}
	if strings.Join(got, "|") != "Saw|sticky|" {
		t.Errorf("samples %q", got)
	}
}
