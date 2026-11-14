package terms

import (
	"reflect"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
)

func term(id, label string, alts ...string) models.Term {
	return models.Term{ID: id, Label: label, AltLabels: alts}
}

func broader(narrow, broad string) models.TermLink {
	return models.TermLink{TermID: narrow, OtherID: broad, Relation: models.TermBroader}
}

func filament() *Graph {
	return New([]models.Term{
		term("fil", "Filament"),
		term("flex", "Flexible filament", "flex filament"),
		term("tpu", "TPU", "thermoplastic polyurethane"),
		term("tpe", "TPE"),
		term("tpu95", "TPU 95A"),
		term("torch", "Torch", "flashlight"),
	}, []models.TermLink{
		broader("flex", "fil"),
		broader("tpu", "flex"),
		broader("tpe", "flex"),
		broader("tpu95", "tpu"),
		{TermID: "tpe", OtherID: "tpu", Relation: models.TermRelated},
	})
}

func TestExpandingFindsOtherNamesAndEverythingNarrower(t *testing.T) {
	g := filament()
	got := g.Expand("flexible  FILAMENT")
	want := []Expansion{
		{"flex filament", "flex filament, another name for flexible  FILAMENT"},
		{"TPE", "TPE, narrower than Flexible filament"},
		{"TPU", "TPU, narrower than Flexible filament"},
		{"thermoplastic polyurethane", "thermoplastic polyurethane (another name for TPU), narrower than Flexible filament"},
		{"TPU 95A", "TPU 95A, narrower than TPU, narrower than Flexible filament"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("expanded to\n%v\nwant\n%v", got, want)
	}
	// Broader terms are not looked for: TPU doesn't find all filament.
	for _, e := range g.Expand("tpu") {
		if e.Label == "Flexible filament" || e.Label == "Filament" || e.Label == "TPE" {
			t.Errorf("searching TPU looks for %q", e.Label)
		}
	}
	// An alternative label expands as its term does.
	if got := g.Expand("flashlight"); !reflect.DeepEqual(got, []Expansion{{"Torch", "Torch, another name for flashlight"}}) {
		t.Errorf("flashlight expanded to %v", got)
	}
	if got := g.Expand("nothing called this"); got != nil {
		t.Errorf("an unknown word expanded to %v", got)
	}
	if got := New(nil, nil).Expand("tpu"); got != nil || !New(nil, nil).Empty() {
		t.Errorf("an empty graph expanded to %v", got)
	}
}

func TestRelatedTermsAreSuggestedNotSearched(t *testing.T) {
	g := filament()
	if got := g.Related("TPU"); !reflect.DeepEqual(got, []string{"TPE"}) {
		t.Errorf("related to TPU: %v", got)
	}
	// TPE is already looked for under flexible filament.
	if got := g.Related("flexible filament"); got != nil {
		t.Errorf("related to flexible filament: %v", got)
	}
}

func TestLoopsAreFollowedOnce(t *testing.T) {
	g := New([]models.Term{term("a", "A"), term("b", "B")}, []models.TermLink{broader("a", "b"), broader("b", "a")})
	if got := g.Expand("a"); len(got) != 1 || got[0].Label != "B" {
		t.Errorf("a loop expanded to %v", got)
	}
}

func TestDistance(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want int
	}{
		{"filament", "filament", 0},
		{"filamnet", "filament", 1},
		{"filment", "filament", 1},
		{"filamenta", "filament", 1},
		{"flament", "filament", 1},
		{"kitten", "sitting", 3},
		{"", "abc", 3},
		{"çafé", "cafe", 2},
	} {
		if got := Distance(c.a, c.b); got != c.want {
			t.Errorf("Distance(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestNearForgivesTypingMistakes(t *testing.T) {
	texts := []string{"Spool of flexible filament", "Shelf B"}
	for search, want := range map[string]string{
		"filamnet":           "filament",
		"flexibel filamnet":  "flexible filament",
		"flexible filamen":   "", // found as typed: an exact search finds it
		"spool filamnet":     "spool filament",
		"filamnet shelf":     "filament shelf",
		"fillament":          "filament",
		"shef":               "", // too short to forgive
		"filamnet sprocket":  "", // every word must be found
		"":                   "",
		"thermoplastics xyz": "",
	} {
		got, ok := Near(search, texts)
		if ok != (want != "") || got != want {
			t.Errorf("Near(%q) = %q, %v; want %q", search, got, ok, want)
		}
	}
}
