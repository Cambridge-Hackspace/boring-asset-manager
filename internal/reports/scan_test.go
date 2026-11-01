package reports

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/labels"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
)

const binScan = `{"sections":[
	{"type":"details","title":"Bin","columns":[{"heading":"Name","binding":"{{asset.name}}"}]},
	{"type":"table","title":"In it","source":"contents","columns":[{"heading":"What","binding":"{{asset.name}}"}]},
	{"type":"actions","actions":["open","use"]}
]}`

func TestParsingScanTemplates(t *testing.T) {
	if _, err := ParseScan([]byte(binScan), nil); err != nil {
		t.Fatalf("a bin's template: %v", err)
	}
	for what, c := range map[string]struct{ spec, want string }{
		"a filter":                {`{"filter":{"search":"x"},"sections":[{"type":"actions","actions":["open"]}]}`, "no filter"},
		"no sections":             {`{"sections":[]}`, "1 to"},
		"a chart":                 {`{"sections":[{"type":"chart","chart":"bar","measure":"assets","group_by":"category"}]}`, "no charts"},
		"a table of nothing":      {`{"sections":[{"type":"table","columns":[{"heading":"A","binding":"{{asset.name}}"}]}]}`, "source must be contents"},
		"sorted details":          {`{"sections":[{"type":"details","sort_by":1,"columns":[{"heading":"A","binding":"x"},{"heading":"B","binding":"y"}]}]}`, "asset alone"},
		"details of nothing":      {`{"sections":[{"type":"details","columns":[]}]}`, "1 to 30 columns"},
		"an unknown action":       {`{"sections":[{"type":"actions","actions":["fly"]}]}`, `unknown action "fly"`},
		"an action twice":         {`{"sections":[{"type":"actions","actions":["use","use"]}]}`, "listed twice"},
		"no actions":              {`{"sections":[{"type":"actions","actions":[]}]}`, "at least one"},
		"hooks by their old name": {`{"sections":[{"type":"actions","actions":["actions"]}]}`, `unknown action "actions"`},
		"an unknown field":        {`{"sections":[{"type":"details","columns":[{"heading":"A","binding":"{{field.nope}}"}]}]}`, "unknown field"},
		"a report's own table":    {`{"sections":[{"type":"table","source":"contents","group_by":"category","columns":[{"heading":"A","binding":"x"}]}]}`, "no actions or groups"},
	} {
		_, err := ParseScan([]byte(c.spec), func(string) bool { return false })
		if !errors.Is(err, ErrInvalidSpec) || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v", what, err)
		}
	}
	// Reports keep to their own sections.
	if _, err := Parse([]byte(binScan), nil); !errors.Is(err, ErrInvalidSpec) {
		t.Errorf("a scan template as a report: %v", err)
	}
	if _, err := Parse([]byte(`{"sections":[{"type":"table","source":"contents","columns":[{"heading":"A","binding":"x"}]}]}`), nil); !errors.Is(err, ErrInvalidSpec) {
		t.Errorf("a report's table with a source: %v", err)
	}
}

func TestRunningAScanTemplate(t *testing.T) {
	spec, err := ParseScan([]byte(binScan), nil)
	if err != nil {
		t.Fatal(err)
	}
	asset := func(id, name string) models.AssetWithDetails {
		return models.AssetWithDetails{Asset: models.Asset{ID: id, Name: name}}
	}
	bin := asset("b", "Bin A3")
	contents := []models.AssetWithDetails{asset("s", "Screws"), asset("n", "Nuts")}
	res := spec.RunScan(&bin, contents, func(a *models.AssetWithDetails) labels.Data {
		return labels.Data{AssetID: a.Asset.ID, AssetName: a.Asset.Name}
	})
	if res.Assets != 1 || len(res.Sections) != 3 {
		t.Fatalf("result: %+v", res)
	}
	if d := res.Sections[0]; d.Type != "details" || d.Title != "Bin" || !reflect.DeepEqual(d.Groups[0].Rows, []Row{{AssetID: "b", Cells: []string{"Bin A3"}}}) {
		t.Errorf("details: %+v", d)
	}
	// Contents sort by their first column.
	if tbl := res.Sections[1]; !reflect.DeepEqual(tbl.Groups[0].Rows, []Row{{AssetID: "n", Cells: []string{"Nuts"}}, {AssetID: "s", Cells: []string{"Screws"}}}) {
		t.Errorf("contents: %+v", tbl)
	}
	if a := res.Sections[2]; a.Type != "actions" || !reflect.DeepEqual(a.Actions, []string{"open", "use"}) {
		t.Errorf("actions: %+v", a)
	}
	// An empty bin has an empty table, not none.
	if empty := spec.RunScan(&bin, nil, func(*models.AssetWithDetails) labels.Data { return labels.Data{} }); empty.Sections[1].Groups[0].Rows == nil {
		t.Errorf("an empty bin: %+v", empty.Sections[1])
	}
}
