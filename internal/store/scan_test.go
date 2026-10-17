package store

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
)

// defaultScanID is the scan template migration 022 seeds as the default.
const defaultScanID = "bc4e2a6d-8f0b-4d5c-9e9a-d15c7d9f1a22"

func TestChoosingAScanTemplate(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	tmpl := func(name string) string {
		r, err := st.CreateScanTemplate(name, []byte(`{"sections":[{"type":"actions","actions":["open"]}]}`))
		if err != nil {
			t.Fatal(err)
		}
		return r.ID
	}
	ta, tb, tx, ty := tmpl("A"), tmpl("B"), tmpl("X"), tmpl("Y")
	cat := func(name string, parent *models.Category, template string, age int) *models.Category {
		var p *string
		if parent != nil {
			p = &parent.ID
		}
		c, err := st.CreateCategory(name, p, "", false, false)
		if err != nil {
			t.Fatal(err)
		}
		if template != "" {
			if _, err := st.SetCategoryScanTemplate(c.ID, &template); err != nil {
				t.Fatal(err)
			}
		}
		// Ages apart, as databases that keep whole seconds would not tell.
		if _, err := st.Exec("UPDATE categories SET created_at = ? WHERE id = ?", time.Date(2020, 1, age, 0, 0, 0, 0, time.UTC), c.ID); err != nil {
			t.Fatal(err)
		}
		return c
	}
	a := cat("A", nil, ta, 5)
	a1 := cat("A1", a, "", 6)
	a11 := cat("A11", a1, "", 7)
	b := cat("B", nil, tb, 4)
	b1 := cat("B1", b, "", 8)
	r := cat("R", nil, "", 1)
	x := cat("X", r, tx, 9)
	x1 := cat("X1", x, "", 10)
	y := cat("Y", nil, ty, 11)
	y1 := cat("Y1", y, "", 12)
	c := cat("C", nil, tx, 2)
	d := cat("D", nil, ty, 3)
	e := cat("E", nil, ta, 13)
	plain := cat("Plain", nil, "", 14)

	for what, w := range map[string]struct {
		cats []*models.Category
		want models.TemplateChoice
	}{
		"no template anywhere": {[]*models.Category{plain}, models.TemplateChoice{TemplateID: defaultScanID}},
		"inherited":            {[]*models.Category{a11}, models.TemplateChoice{TemplateID: ta, SourceCategoryID: a.ID}},
		"(a) the closest wins, even from another tree": {[]*models.Category{a11, b1}, models.TemplateChoice{TemplateID: tb, SourceCategoryID: b.ID}},
		"(b) then the longest ancestry":                {[]*models.Category{x1, y1}, models.TemplateChoice{TemplateID: tx, SourceCategoryID: x.ID, Conflict: true, Contenders: sorted(x.ID, y.ID)}},
		"(c) then the fewest descendants":              {[]*models.Category{a1, b1}, models.TemplateChoice{TemplateID: tb, SourceCategoryID: b.ID, Conflict: true, Contenders: sorted(a.ID, b.ID)}},
		"(d) then the oldest":                          {[]*models.Category{c, d}, models.TemplateChoice{TemplateID: tx, SourceCategoryID: c.ID, Conflict: true, Contenders: sorted(c.ID, d.ID)}},
		"a tie on one template is no conflict":         {[]*models.Category{a, e}, models.TemplateChoice{TemplateID: ta, SourceCategoryID: e.ID}},
	} {
		ids := []string{}
		for _, c := range w.cats {
			ids = append(ids, c.ID)
		}
		asset, err := st.CreateAsset(what, ids, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(asset.ScanTemplate, w.want) {
			t.Errorf("%s: %+v, want %+v", what, asset.ScanTemplate, w.want)
		}
		if list, _ := st.ListAssets("", what); len(list) != 1 || !reflect.DeepEqual(list[0].ScanTemplate, w.want) {
			t.Errorf("%s, in a list: %+v", what, list)
		}
	}

	// A subcategory naming its own overrides its parent's, and ends that
	// asset's conflict.
	if _, err := st.SetCategoryScanTemplate(a1.ID, &tx); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.ListAssets("", "(c) then the fewest descendants"); !reflect.DeepEqual(got[0].ScanTemplate, models.TemplateChoice{TemplateID: tx, SourceCategoryID: a1.ID}) {
		t.Errorf("overridden: %+v", got[0].ScanTemplate)
	}

	// The categories in conflicts, now and as a change would leave them. A1
	// naming X's template has tied it with B for the asset in A11 and B1.
	involved, err := st.ScanConflicts("", nil)
	if err != nil || !reflect.DeepEqual(involved, map[string]int{x.ID: 1, y.ID: 1, c.ID: 1, d.ID: 1, a1.ID: 1, b.ID: 1}) {
		t.Errorf("conflicts: %v, %v", involved, err)
	}
	if involved, _ := st.ScanConflicts(y.ID, &tx); involved[y.ID] != 0 || involved[c.ID] != 1 {
		t.Errorf("naming X's template on Y settles theirs: %v", involved)
	}
	if involved, _ := st.ScanConflicts(a1.ID, nil); involved[a.ID] != 1 || involved[b.ID] != 1 {
		t.Errorf("clearing A1 brings back A and B's: %v", involved)
	}
}

func sorted(ids ...string) []string {
	if ids[0] > ids[1] {
		ids[0], ids[1] = ids[1], ids[0]
	}
	return ids
}

func TestManagingScanTemplates(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	def, err := st.GetReport(defaultScanID)
	if err != nil || def.Kind != ReportKindScan || !def.Default {
		t.Fatalf("the seeded default: %+v, %v", def, err)
	}
	report, _ := st.CreateReport("A report", []byte(`{"sections":[]}`))
	if report.Kind != ReportKindReport || report.Default {
		t.Errorf("a report: %+v", report)
	}
	shelf, _ := st.CreateCategory("Shelf", nil, "", false, false)
	if _, err := st.SetCategoryScanTemplate(shelf.ID, &report.ID); !errors.Is(err, ErrNotScanTemplate) {
		t.Errorf("a report as a scan template: %v", err)
	}
	if err := st.SetDefaultScanTemplate(report.ID); !errors.Is(err, ErrNotScanTemplate) {
		t.Errorf("a report as the default: %v", err)
	}
	missing := "nope"
	if _, err := st.SetCategoryScanTemplate(shelf.ID, &missing); !errors.Is(err, ErrReportNotFound) {
		t.Errorf("a missing template: %v", err)
	}

	bins, _ := st.CreateScanTemplate("Bins", []byte(`{"sections":[{"type":"actions","actions":["open"]}]}`))
	if got, err := st.SetCategoryScanTemplate(shelf.ID, &bins.ID); err != nil || got.ScanTemplateID == nil || *got.ScanTemplateID != bins.ID {
		t.Errorf("naming one: %+v, %v", got, err)
	}
	if err := st.DeleteReport(bins.ID); !errors.Is(err, ErrReportInUse) {
		t.Errorf("deleting one in use: %v", err)
	}
	if err := st.DeleteReport(defaultScanID); !errors.Is(err, ErrReportInUse) {
		t.Errorf("deleting the default: %v", err)
	}
	// Another default; then the old one goes.
	if err := st.SetDefaultScanTemplate(bins.ID); err != nil {
		t.Fatal(err)
	}
	if r, _ := st.GetReport(defaultScanID); r.Default {
		t.Error("two defaults")
	}
	if err := st.DeleteReport(defaultScanID); err != nil {
		t.Errorf("deleting the old default: %v", err)
	}
	if got, err := st.SetCategoryScanTemplate(shelf.ID, nil); err != nil || got.ScanTemplateID != nil {
		t.Errorf("naming none: %+v, %v", got, err)
	}
	if a, _ := st.CreateAsset("Plate", []string{shelf.ID}, nil, nil); !reflect.DeepEqual(a.ScanTemplate, models.TemplateChoice{TemplateID: bins.ID}) {
		t.Errorf("the new default: %+v", a.ScanTemplate)
	}
}
