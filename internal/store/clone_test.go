package store

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
)

func TestCloningACategory(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}

	parent, err := st.CreateCategory("Supplies", nil, "", false, false)
	must(err)
	three := 3
	src, err := st.CreateCategoryFrom(CategoryInput{Name: "Filament", ParentID: &parent.ID, ColorHex: "#ffcc00", IsConsumable: true, Unit: "g", IsLendable: true, LowStockThreshold: &three})
	must(err)
	child, err := st.CreateCategoryFrom(CategoryInput{Name: "PLA", ParentID: &src.ID})
	must(err)
	colour, err := st.CreateField("colour_name", "Color", "text", nil)
	must(err)
	weight, err := st.CreateField("weight_g", "Weight", "number", nil)
	must(err)
	must(st.AssignCategoryField(src.ID, colour.ID, true))
	must(st.AssignCategoryField(src.ID, weight.ID, false))
	usedWith := relation(t, st, "used with")
	_, err = st.SetCategoryRelationTypes(src.ID, []string{usedWith.ID})
	must(err)
	dry, err := st.SaveAction("", ActionInput{Key: "dry", Label: "Dry it"})
	must(err)
	_, err = st.SetCategoryActions(src.ID, []models.CategoryAction{{ActionID: dry.ID, Cascades: false}})
	must(err)
	scope, err := st.CreateScope("workshop")
	must(err)
	_, err = st.SetCategoryScopes(src.ID, []string{scope.ID})
	must(err)
	scan, err := st.CreateScanTemplate("Spool", []byte(`{"sections":[{"type":"actions","actions":["open"]}]}`))
	must(err)
	_, err = st.SetCategoryScanTemplate(src.ID, &scan.ID)
	must(err)
	wide, narrow := labelTemplate(t, st, "Wide"), labelTemplate(t, st, "Narrow")
	_, err = st.SetCategoryLabelTemplates(src.ID, []string{narrow, wide})
	must(err)
	spool, err := st.CreateAsset("Yellow spool", []string{src.ID}, nil, map[string]string{"colour_name": "Yellow"})
	must(err)

	clone, err := st.CloneCategory(src.ID, "")
	must(err)

	// Its own row: everything but its name, ID and dates, and not flagged by hand.
	if clone.ID == src.ID || clone.Name != "Filament copy" {
		t.Errorf("named %q (%s)", clone.Name, clone.ID)
	}
	if clone.ParentID == nil || *clone.ParentID != parent.ID || clone.ColorHex != "#ffcc00" || !clone.IsConsumable || clone.Unit != "g" ||
		!clone.IsLendable || clone.LowStockThreshold == nil || *clone.LowStockThreshold != 3 {
		t.Errorf("settings: %+v", clone)
	}
	// Empty, below its threshold, it flags itself, as a new category would.
	if !clone.IsRunningLow {
		t.Errorf("an empty copy with a threshold of 3 is not running low: %+v", clone)
	}
	// The same templates, not copies of them.
	if clone.ScanTemplateID == nil || *clone.ScanTemplateID != scan.ID || !reflect.DeepEqual(clone.LabelTemplateIDs, []string{narrow, wide}) {
		t.Errorf("templates: scan %v, labels %v", clone.ScanTemplateID, clone.LabelTemplateIDs)
	}

	fields, err := st.GetEffectiveFieldsForCategories([]string{clone.ID})
	must(err)
	required := map[string]bool{}
	for _, f := range fields {
		required[f.Field.Key] = f.IsRequired
	}
	if !reflect.DeepEqual(required, map[string]bool{"colour_name": true, "weight_g": false}) {
		t.Errorf("fields: %v", required)
	}
	if types, _ := st.CategoryRelationTypes(clone.ID); !reflect.DeepEqual(types, []string{usedWith.ID}) {
		t.Errorf("suggested link types: %v", types)
	}
	if actions, _ := st.CategoryActions(clone.ID); len(actions) != 1 || actions[0].ActionID != dry.ID || actions[0].Cascades {
		t.Errorf("actions: %+v", actions)
	}
	if scopes, _ := st.CategoryScopes(clone.ID); !reflect.DeepEqual(scopes, []string{scope.ID}) {
		t.Errorf("scopes: %v", scopes)
	}

	// Neither its subcategories nor its assets.
	all, err := st.ListCategories()
	must(err)
	for _, c := range all {
		if c.ParentID != nil && *c.ParentID == clone.ID {
			t.Errorf("the copy has a subcategory: %+v", c)
		}
	}
	if n, _ := st.ActiveCount(clone.ID); n != 0 {
		t.Errorf("the copy holds %d assets", n)
	}
	if got, _ := st.GetAssetByID(spool.Asset.ID); len(got.Categories) != 1 || got.Categories[0].ID != src.ID {
		t.Errorf("the asset's categories changed: %+v", got.Categories)
	}
	// The original is untouched.
	if again, _ := st.GetCategoryByID(src.ID); again.Name != "Filament" || !reflect.DeepEqual(again.LabelTemplateIDs, []string{narrow, wide}) {
		t.Errorf("the original changed: %+v", again)
	}
	if again, _ := st.GetCategoryByID(child.ID); *again.ParentID != src.ID {
		t.Errorf("the subcategory moved: %+v", again)
	}

	// Named as asked; a root stays a root.
	named, err := st.CloneCategory(parent.ID, "  Stores  ")
	must(err)
	if named.Name != "Stores" || named.ParentID != nil {
		t.Errorf("named copy: %+v", named)
	}

	if _, err := st.CloneCategory("nope", ""); !errors.Is(err, ErrCategoryNotFound) {
		t.Errorf("a missing category: %v", err)
	}
	if _, err := st.CloneCategory(UncategorizedCategoryID, ""); !errors.Is(err, ErrSystemCategoryInTree) {
		t.Errorf("Uncategorized: %v", err)
	}
	if _, err := st.CloneCategory(src.ID, strings.Repeat("x", 256)); err == nil {
		t.Error("a name too long was taken")
	}
}

func TestNextCodeLike(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	for _, code := range []string{"SP-014", "SP-009", "HS-105", "BIN-0099", "ODD"} {
		if _, err := st.CreateAsset(code, nil, []models.AssetGeneratorKey{{GeneratorType: "qr", AssignedCode: code}}, nil); err != nil {
			t.Fatal(err)
		}
	}
	for like, want := range map[string]string{
		// Its own series, however high another goes.
		"SP-009":   "SP-015",
		"SP-014":   "SP-015",
		"BIN-0099": "BIN-0100",
		// Nothing to follow.
		"ODD": "",
	} {
		if got, err := st.NextCodeLike("qr", like); err != nil || got != want {
			t.Errorf("after %s: %q, %v; want %q", like, got, err, want)
		}
	}
	// NextCode still follows the highest of all.
	if got, _ := st.NextCode("qr"); got != "HS-106" {
		t.Errorf("NextCode: %q", got)
	}
}
