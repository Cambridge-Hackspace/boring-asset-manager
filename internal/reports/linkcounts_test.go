package reports

import "testing"

// TestASpecSaysWhetherItCountsLinks: a report counts followed links only
// when a column binds a count.
func TestASpecSaysWhetherItCountsLinks(t *testing.T) {
	for raw, want := range map[string]bool{
		`{"sections":[{"type":"table","columns":[{"heading":"A","binding":"{{asset.name}}"}]}]}`:                                                                           false,
		`{"sections":[{"type":"table","columns":[{"heading":"A","binding":"{{asset.name}}"},{"heading":"B","binding":"{{asset.needed_by_all}}"}]}]}`:                       true,
		`{"sections":[{"type":"chart","chart":"bar","group_by":"category","measure":"assets"},{"type":"table","columns":[{"heading":"A","binding":"x{{asset.needs}}"}]}]}`: true,
	} {
		spec, err := Parse([]byte(raw), known)
		if err != nil {
			t.Fatalf("%s: %v", raw, err)
		}
		if spec.UsesLinkCounts() != want {
			t.Errorf("%s: uses link counts %v", raw, !want)
		}
	}
}
