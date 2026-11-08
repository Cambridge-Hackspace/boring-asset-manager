package labels

import "testing"

// TestLinkCountBindings: {{asset.needs}} and the rest resolve to how many
// assets an asset needs and how many need it, and a template that binds
// one says so, so the counts are read for it.
func TestLinkCountBindings(t *testing.T) {
	d := Data{Links: LinkData{Needs: "1", NeedsAll: "4", NeededBy: "2", NeededByAll: "7"}}
	b, err := ParseBinding("{{asset.needs}}/{{asset.needs_all}} {{ asset.needed_by }}/{{asset.needed_by_all}}")
	if err != nil {
		t.Fatal(err)
	}
	if got := b.Resolve(d); got != "1/4 2/7" {
		t.Errorf("resolved: %q", got)
	}
	for _, ref := range b.Refs() {
		if !ref.IsLinkCount() {
			t.Errorf("%+v is not a link count", ref)
		}
	}
	plain := &Template{Elements: []Element{{Type: ElementText, ContentBinding: "{{asset.name}}"}}}
	counted := &Template{Elements: []Element{{Type: ElementText, ContentBinding: "{{asset.name}}",
		Alternative: &Element{Type: ElementText, ContentBinding: "needed by {{asset.needed_by_all}}"}}}}
	if plain.UsesLinkCounts() || !counted.UsesLinkCounts() {
		t.Errorf("uses link counts: %v, %v", plain.UsesLinkCounts(), counted.UsesLinkCounts())
	}
}
