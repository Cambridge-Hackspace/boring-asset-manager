package store

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/labels"
)

func labelTemplate(t *testing.T, st *Store, name string) string {
	t.Helper()
	tmpl, err := st.CreateTemplate(&labels.Template{Name: name, Units: labels.UnitsMM, Width: 62, Height: 29, DPI: 300, Elements: []labels.Element{}})
	if err != nil {
		t.Fatal(err)
	}
	return tmpl.ID
}

func TestCategoriesNameLabelTemplatesInOrder(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	wide, narrow := labelTemplate(t, st, "Wide"), labelTemplate(t, st, "Narrow")
	bins, _ := st.CreateCategory("Bins", nil, "", false, false)
	small, _ := st.CreateCategoryFrom(CategoryInput{Name: "Small bins", ParentID: &bins.ID})

	cat, err := st.SetCategoryLabelTemplates(bins.ID, []string{wide, narrow})
	if err != nil || !reflect.DeepEqual(cat.LabelTemplateIDs, []string{wide, narrow}) || cat.LabelTemplateID == nil || *cat.LabelTemplateID != wide {
		t.Fatalf("set: %+v, %v", cat, err)
	}
	if listed, _ := st.ListCategories(); !reflect.DeepEqual(listed[0].LabelTemplateIDs, []string{wide, narrow}) {
		t.Errorf("listed: %+v", listed[0])
	}
	// A subcategory takes its parent's list, in order.
	bin, _ := st.CreateAsset("Bin A1", []string{small.ID}, nil, nil)
	if !reflect.DeepEqual(bin.LabelTemplate.TemplateIDs, []string{wide, narrow}) || bin.LabelTemplate.TemplateID != wide || bin.LabelTemplate.SourceCategoryID != bins.ID {
		t.Errorf("inherited: %+v", bin.LabelTemplate)
	}
	if got, _ := st.GetAssetByID(bin.Asset.ID); !reflect.DeepEqual(got.Categories[0].LabelTemplateIDs, []string{}) {
		t.Errorf("the asset's own category names none: %+v", got.Categories[0])
	}
	// Reordered, the first changes.
	if cat, err = st.SetCategoryLabelTemplates(bins.ID, []string{narrow, wide}); err != nil || *cat.LabelTemplateID != narrow {
		t.Errorf("reordered: %+v, %v", cat, err)
	}

	for why, ids := range map[string][]string{
		"named twice": {wide, wide},
		"too many":    strings.Split(strings.Repeat(wide+",", MaxCategoryLabelTemplates+1), ",")[:MaxCategoryLabelTemplates+1],
	} {
		if _, err := st.SetCategoryLabelTemplates(bins.ID, ids); !errors.Is(err, ErrInvalidLabelTemplates) {
			t.Errorf("%s: %v", why, err)
		}
	}
	if _, err := st.SetCategoryLabelTemplates(bins.ID, []string{"nope"}); !errors.Is(err, ErrTemplateNotFound) {
		t.Errorf("a missing template: %v", err)
	}

	// A template second in a category's list is still in use.
	if err := st.DeleteTemplate(wide); !errors.Is(err, ErrTemplateInUse) {
		t.Errorf("deleting a template a category names second: %v", err)
	}

	// Two categories as close naming the same list agree; different lists
	// are a conflict.
	tools, _ := st.CreateCategory("Tools", nil, "", false, false)
	if _, err := st.SetCategoryLabelTemplates(tools.ID, []string{narrow, wide}); err != nil {
		t.Fatal(err)
	}
	both, _ := st.CreateAsset("Toolbox", []string{bins.ID, tools.ID}, nil, nil)
	if both.LabelTemplate.Conflict {
		t.Errorf("the same list is a conflict: %+v", both.LabelTemplate)
	}
	if _, err := st.SetCategoryLabelTemplates(tools.ID, []string{wide, narrow}); err != nil {
		t.Fatal(err)
	}
	if again, _ := st.GetAssetByID(both.Asset.ID); !again.LabelTemplate.Conflict {
		t.Errorf("the same templates in another order are not a conflict: %+v", again.LabelTemplate)
	}
	if involved, _ := st.LabelConflicts(tools.ID, []string{narrow, wide}); len(involved) != 0 {
		t.Errorf("as if Tools named the same list: %v", involved)
	}

	// Naming none takes them all away.
	if cat, err = st.SetCategoryLabelTemplates(bins.ID, nil); err != nil || len(cat.LabelTemplateIDs) != 0 || cat.LabelTemplateID != nil {
		t.Errorf("cleared: %+v, %v", cat, err)
	}
}

// TestTheOneLabelTemplateFromBeforeComesFirst runs migration 030 again on
// a category naming one template the old way.
func TestTheOneLabelTemplateFromBeforeComesFirst(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	tmpl := labelTemplate(t, st, "Bin tag")
	bins, _ := st.CreateCategory("Bins", nil, "", false, false)
	for _, stmt := range []string{
		"DROP TABLE category_label_templates",
		"ALTER TABLE spool_entries DROP COLUMN template_from_category",
		"ALTER TABLE categories ADD COLUMN label_template_id VARCHAR(36)",
		"DELETE FROM schema_migrations WHERE version = 30",
	} {
		if _, err := st.Exec(stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	if _, err := st.Exec("UPDATE categories SET label_template_id = ? WHERE id = ?", tmpl, bins.ID); err != nil {
		t.Fatal(err)
	}
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	if cat, _ := st.GetCategoryByID(bins.ID); !reflect.DeepEqual(cat.LabelTemplateIDs, []string{tmpl}) {
		t.Errorf("after migrating: %+v", cat)
	}
}
