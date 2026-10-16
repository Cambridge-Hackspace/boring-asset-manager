package reports

import (
	"bytes"
	"errors"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/labels"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
)

func known(key string) bool { return key == "serial" }

func TestParseChecksSpecs(t *testing.T) {
	good := `{"filter":{"loan":"open"},"sections":[{"type":"table","columns":[{"heading":"Asset","binding":"{{asset.name}} ({{field.serial}})"}]}]}`
	if _, err := Parse([]byte(good), known); err != nil {
		t.Fatalf("a good spec: %v", err)
	}
	table := func(section string) string {
		return `{"sections":[` + section + `]}`
	}
	for what, raw := range map[string]string{
		"not JSON":             `nope`,
		"an unknown key":       `{"sections":[],"colour":"red"}`,
		"no sections":          `{"sections":[]}`,
		"a bad loan":           `{"filter":{"loan":"lost"},"sections":[{"type":"table","columns":[{"heading":"A","binding":"x"}]}]}`,
		"a bad condition":      `{"filter":{"condition":"broken"},"sections":[{"type":"table","columns":[{"heading":"A","binding":"x"}]}]}`,
		"an unknown type":      table(`{"type":"pie","columns":[{"heading":"A","binding":"x"}]}`),
		"no columns":           table(`{"type":"table","columns":[]}`),
		"sorting by nothing":   table(`{"type":"table","sort_by":1,"columns":[{"heading":"A","binding":"x"}]}`),
		"an odd grouping":      table(`{"type":"table","group_by":"colour","columns":[{"heading":"A","binding":"x"}]}`),
		"an unknown binding":   table(`{"type":"table","columns":[{"heading":"A","binding":"{{asset.colour}}"}]}`),
		"an unknown field":     table(`{"type":"table","columns":[{"heading":"A","binding":"{{field.weight}}"}]}`),
		"an unclosed binding":  table(`{"type":"table","columns":[{"heading":"A","binding":"{{asset.name"}]}`),
		"a heading with a NUL": table(`{"type":"table","columns":[{"heading":"A\u0000","binding":"x"}]}`),
	} {
		if _, err := Parse([]byte(raw), known); !errors.Is(err, ErrInvalidSpec) {
			t.Errorf("%s: %v", what, err)
		}
	}
}

// fixtures: two categories, one flagged and below another, and three
// assets.
func fixtures() ([]models.Category, []models.AssetWithDetails) {
	materials := "materials"
	cats := []models.Category{
		{ID: materials, Name: "Materials", IsConsumable: true},
		{ID: "filament", Name: "Filament", ParentID: &materials, IsConsumable: true, IsRunningLow: true},
		{ID: "tools", Name: "Tools"},
	}
	asset := func(id, name string, cats ...models.Category) models.AssetWithDetails {
		return models.AssetWithDetails{Asset: models.Asset{ID: id, Name: name}, Categories: cats}
	}
	low := asset("screws", "screws", cats[2])
	low.Asset.Stock = &models.AssetStock{Quantity: 9, Unit: "pcs", IsRunningLow: true}
	return cats, []models.AssetWithDetails{
		asset("pla", "Blue PLA", cats[1]),
		asset("drill", "Drill", cats[2]),
		low,
		asset("petg", "PETG", cats[1], cats[2]),
	}
}

func names(list []models.AssetWithDetails, f Filter, cats []models.Category) []string {
	var out []string
	for i := range list {
		if f.Matches(&list[i], cats) {
			out = append(out, list[i].Asset.Name)
		}
	}
	return out
}

func TestFiltersByCategoryAndRunningLow(t *testing.T) {
	cats, assets := fixtures()
	if got := names(assets, Filter{CategoryIDs: []string{"materials"}}, cats); !reflect.DeepEqual(got, []string{"Blue PLA", "PETG"}) {
		t.Errorf("in materials, below it too: %v", got)
	}
	// Running low: in a flagged category, or flagged itself.
	if got := names(assets, Filter{RunningLow: true}, cats); !reflect.DeepEqual(got, []string{"Blue PLA", "screws", "PETG"}) {
		t.Errorf("running low: %v", got)
	}
	if got := names(assets, Filter{RunningLow: true, CategoryIDs: []string{"tools"}}, cats); !reflect.DeepEqual(got, []string{"screws", "PETG"}) {
		t.Errorf("running low among tools: %v", got)
	}
	if got := names(assets, Filter{}, cats); len(got) != 4 {
		t.Errorf("everything: %v", got)
	}
}

func TestRunSortsAndGroups(t *testing.T) {
	cats, assets := fixtures()
	assets[0].Asset.Stock = &models.AssetStock{Quantity: 1000, Unit: "g"}
	assets[3].Asset.Stock = &models.AssetStock{Quantity: 250, Unit: "g"}
	spec, err := Parse([]byte(`{"sections":[
		{"type":"table","title":"By name","columns":[{"heading":"Asset","binding":"{{asset.name}}"}]},
		{"type":"table","title":"By stock","columns":[{"heading":"Asset","binding":"{{asset.name}}"},{"heading":"Left","binding":"{{stock.quantity}}"}],"sort_by":1,"descending":true},
		{"type":"table","title":"Shopping","columns":[{"heading":"Asset","binding":"{{asset.name}}"}],"group_by":"category"}
	]}`), known)
	if err != nil {
		t.Fatal(err)
	}
	data := func(a *models.AssetWithDetails) labels.Data {
		d := labels.Data{AssetID: a.Asset.ID, AssetName: a.Asset.Name}
		if a.Asset.Stock != nil {
			d.Quantity = strconv.Itoa(a.Asset.Stock.Quantity)
		}
		return d
	}
	res := spec.Run(assets, cats, data, nil, time.Now())
	if res.Assets != 4 || len(res.Sections) != 3 {
		t.Fatalf("result %+v", res)
	}
	column := func(rows []Row, i int) []string {
		var out []string
		for _, r := range rows {
			out = append(out, r.Cells[i])
		}
		return out
	}
	// Text ignores case; numbers sort as numbers, largest first here.
	if got := column(res.Sections[0].Groups[0].Rows, 0); !reflect.DeepEqual(got, []string{"Blue PLA", "Drill", "PETG", "screws"}) {
		t.Errorf("by name: %v", got)
	}
	if got := column(res.Sections[1].Groups[0].Rows, 1); !reflect.DeepEqual(got, []string{"1000", "250", "9", ""}) {
		t.Errorf("by stock: %v", got)
	}
	// Grouped under each category an asset is in, groups by name, flags kept.
	groups := res.Sections[2].Groups
	if len(groups) != 2 || groups[0].Title != "Filament" || !groups[0].RunningLow || groups[1].Title != "Tools" || groups[1].RunningLow {
		t.Fatalf("groups %+v", groups)
	}
	if got := column(groups[1].Rows, 0); !reflect.DeepEqual(got, []string{"Drill", "PETG", "screws"}) {
		t.Errorf("tools: %v", got)
	}
}

func TestWriteCSV(t *testing.T) {
	sec := SectionResult{Columns: []string{"Asset", "Note"}, Groups: []GroupResult{
		{Title: "Filament", Rows: []Row{{Cells: []string{"Blue PLA", "=1+1"}}}},
		{Title: "Tools", Rows: []Row{{Cells: []string{"Drill", "-5"}}}},
	}}
	var buf bytes.Buffer
	if err := WriteCSV(&buf, sec); err != nil {
		t.Fatal(err)
	}
	want := "\xef\xbb\xbfGroup,Asset,Note\nFilament,Blue PLA,'=1+1\nTools,Drill,-5\n"
	if buf.String() != want {
		t.Errorf("got %q\nwant %q", buf.String(), want)
	}
	buf.Reset()
	sec.Groups = []GroupResult{{Rows: []Row{{Cells: []string{"Drill", "fine"}}}}}
	if err := WriteCSV(&buf, sec); err != nil || buf.String() != "\xef\xbb\xbfAsset,Note\nDrill,fine\n" {
		t.Errorf("ungrouped: %q, %v", buf.String(), err)
	}
}
