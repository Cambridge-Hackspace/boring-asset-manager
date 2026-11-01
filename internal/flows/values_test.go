package flows

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"testing"
)

// valueCases are the cases in testdata/values.json, which the editor's
// list of values a step can name (web/src/lib/flowEditor.ts valuesBefore)
// is tested against too, so the two cannot disagree (#169).
type valueCases struct {
	Cases []struct {
		Name   string `json:"name"`
		Fields []struct {
			Key string `json:"key"`
		} `json:"fields"`
		Definition Definition          `json:"definition"`
		Available  map[string][]string `json:"available"`
	} `json:"cases"`
}

// TestValuesEachStepCanName checks that saving accepts, in each step's
// templates, exactly the values the shared cases say it can name: each of
// them, and none of the others any case names, nor one no step sets.
func TestValuesEachStepCanName(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "values.json"))
	if err != nil {
		t.Fatal(err)
	}
	var file valueCases
	if err := json.Unmarshal(raw, &file); err != nil {
		t.Fatal(err)
	}
	if len(file.Cases) == 0 {
		t.Fatal("no cases")
	}
	for _, tc := range file.Cases {
		t.Run(tc.Name, func(t *testing.T) {
			c := Catalog{Fields: map[string]bool{}}
			// Every name a case could mean, to try at each step.
			names := map[string]bool{"nope": true}
			for _, f := range tc.Fields {
				c.Fields[f.Key] = true
				names[f.Key] = true
			}
			for _, list := range tc.Available {
				for _, n := range list {
					names[n] = true
				}
			}
			for _, s := range tc.Definition.Steps {
				for _, q := range s.Questions {
					names[q.Key] = true
				}
				for _, row := range s.Fields {
					names[row.Field] = true
				}
			}
			if len(tc.Available) != len(tc.Definition.Steps) {
				t.Fatalf("the case lists %d steps' values, of %d steps", len(tc.Available), len(tc.Definition.Steps))
			}
			for i, s := range tc.Definition.Steps {
				want, ok := tc.Available[s.ID]
				if !ok {
					t.Fatalf("the case does not list what step %s can name", s.ID)
				}
				var got []string
				for n := range names {
					if acceptsInTitle(tc.Definition, c, i, n) {
						got = append(got, n)
					}
				}
				sort.Strings(got)
				want = slices.Clone(want)
				sort.Strings(want)
				if !slices.Equal(got, want) {
					t.Errorf("step %s: saving accepts %v, the case says %v", s.ID, got, want)
				}
			}
		})
	}
}

// acceptsInTitle reports whether saving accepts the step at i naming n in
// its heading.
func acceptsInTitle(d Definition, c Catalog, i int, n string) bool {
	d.Steps = slices.Clone(d.Steps)
	d.Steps[i].Title = "{" + n + "}"
	refused := fmt.Sprintf("its title names {%s}, which no earlier step sets", n)
	for _, p := range d.Check(c) {
		if p.Step == d.Steps[i].ID && p.Message == refused {
			return false
		}
	}
	return true
}
