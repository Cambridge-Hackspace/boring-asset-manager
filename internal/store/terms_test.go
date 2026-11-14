package store

import (
	"errors"
	"reflect"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
)

func TestTermsKeepWhoAddedThemAndTheirLinks(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	ada, err := st.CreateUser("ada@hackspace.org", "Password123!", true)
	if err != nil {
		t.Fatal(err)
	}
	as := st.As(ada.ID)

	flex, err := as.CreateTerm(TermInput{Label: "  Flexible   filament ", AltLabels: []string{"flex filament", "FLEXIBLE FILAMENT", " ", "flex filament"}, Note: "Bends."})
	if err != nil {
		t.Fatal(err)
	}
	if flex.Label != "Flexible filament" || !reflect.DeepEqual(flex.AltLabels, []string{"flex filament"}) || flex.Note != "Bends." {
		t.Errorf("saved as %+v", flex)
	}
	if flex.AddedBy != ada.ID || flex.AddedByToken || flex.Source != "person" || flex.AddedByName != "ada@hackspace.org" {
		t.Errorf("added by %+v", flex.TermSource)
	}
	// Through an assistant, it says which.
	tpu, err := as.Via("Claude Desktop").CreateTerm(TermInput{Label: "TPU", AltLabels: []string{"thermoplastic polyurethane"}})
	if err != nil {
		t.Fatal(err)
	}
	if tpu.Via != "Claude Desktop" || tpu.Source != "agent" {
		t.Errorf("an assistant's term: %+v", tpu.TermSource)
	}
	// Preferred labels are each a term's own, however they are written.
	var taken *TermLabelTaken
	if _, err := as.CreateTerm(TermInput{Label: "tpu"}); !errors.As(err, &taken) || taken.TermID != tpu.ID {
		t.Errorf("a second TPU: %v", err)
	}
	var problem *TermProblem
	if _, err := as.CreateTerm(TermInput{Label: "   "}); !errors.As(err, &problem) {
		t.Errorf("a term with no label: %v", err)
	}
	tpe, err := as.CreateTerm(TermInput{Label: "TPE"})
	if err != nil {
		t.Fatal(err)
	}

	if err := as.LinkTerms(tpu.ID, models.TermBroader, flex.ID); err != nil {
		t.Fatal(err)
	}
	// Narrower is broader the other way.
	if err := as.LinkTerms(flex.ID, models.TermNarrower, tpe.ID); err != nil {
		t.Fatal(err)
	}
	if err := as.LinkTerms(tpu.ID, models.TermRelated, tpe.ID); err != nil {
		t.Fatal(err)
	}
	// Again, nothing changes.
	if err := as.LinkTerms(tpe.ID, models.TermRelated, tpu.ID); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ a, rel, b string }{
		{tpu.ID, models.TermBroader, tpu.ID},
		{flex.ID, models.TermBroader, tpu.ID},
		{flex.ID, "cousin", tpu.ID},
	} {
		if err := as.LinkTerms(c.a, c.rel, c.b); !errors.As(err, &problem) {
			t.Errorf("linking %v: %v", c, err)
		}
	}
	if err := as.LinkTerms(tpu.ID, models.TermBroader, "nope"); !errors.Is(err, ErrTermNotFound) {
		t.Errorf("linking to no term: %v", err)
	}

	g, err := st.TermGraph()
	if err != nil {
		t.Fatal(err)
	}
	var labels []string
	for _, e := range g.Expand("flex filament") {
		labels = append(labels, e.Label)
	}
	if !reflect.DeepEqual(labels, []string{"Flexible filament", "TPE", "TPU", "thermoplastic polyurethane"}) {
		t.Errorf("flex filament expands to %v", labels)
	}
	if got := g.Related("tpu"); !reflect.DeepEqual(got, []string{"TPE"}) {
		t.Errorf("related to TPU: %v", got)
	}

	// Changing a term keeps who added it.
	changed, err := st.UpdateTerm(tpu.ID, TermInput{Label: "TPU", AltLabels: []string{"thermoplastic polyurethane", "Ninjaflex"}})
	if err != nil {
		t.Fatal(err)
	}
	if changed.Via != "Claude Desktop" || len(changed.AltLabels) != 2 {
		t.Errorf("changed to %+v", changed)
	}
	if _, err := st.UpdateTerm(tpu.ID, TermInput{Label: "TPE"}); !errors.As(err, &taken) {
		t.Errorf("renamed onto TPE: %v", err)
	}

	if err := st.UnlinkTerms(tpe.ID, models.TermRelated, tpu.ID); err != nil {
		t.Fatal(err)
	}
	if err := st.UnlinkTerms(tpe.ID, models.TermRelated, tpu.ID); !errors.Is(err, ErrTermNotFound) {
		t.Errorf("unlinking again: %v", err)
	}
	if err := st.DeleteTerm(flex.ID); err != nil {
		t.Fatal(err)
	}
	list, links, err := st.ListTerms()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].Label != "TPE" || list[1].Label != "TPU" || len(links) != 0 {
		t.Errorf("after deleting flexible filament: %+v %+v", list, links)
	}
	if err := st.DeleteTerm(flex.ID); !errors.Is(err, ErrTermNotFound) {
		t.Errorf("deleting again: %v", err)
	}
}

func TestATermImportIsRemovedAsOne(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	own, err := st.CreateTerm(TermInput{Label: "Filament"})
	if err != nil {
		t.Fatal(err)
	}
	imp, err := st.ImportTerms("Filament words", []TermImportItem{
		{TermInput: TermInput{Label: "Flexible filament"}, Broader: []string{"filament"}},
		{TermInput: TermInput{Label: "TPU", AltLabels: []string{"thermoplastic polyurethane"}}, Broader: []string{"Flexible filament"}, Related: []string{"TPE"}},
		// Already a term: only its links are added.
		{TermInput: TermInput{Label: "FILAMENT", AltLabels: []string{"ignored"}}, Broader: []string{"Filament"}},
		// A loop is left out.
		{TermInput: TermInput{Label: "Filament"}, Broader: []string{"TPU"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	// Flexible filament, TPU, and TPE, named by TPU.
	if imp.Name != "Filament words" || imp.Terms != 3 || imp.Links != 3 {
		t.Errorf("imported %+v", imp)
	}
	list, links, err := st.ListTerms()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 4 || len(links) != 3 {
		t.Fatalf("%d terms and %d links", len(list), len(links))
	}
	for _, tm := range list {
		if (tm.ID == own.ID) != (tm.ImportID == "") || tm.ID != own.ID && tm.Source != "import" {
			t.Errorf("%s came from %q", tm.Label, tm.ImportID)
		}
		if tm.ID == own.ID && len(tm.AltLabels) != 0 {
			t.Errorf("an import changed a term it didn't add: %+v", tm)
		}
	}
	imports, err := st.ListTermImports()
	if err != nil || len(imports) != 1 || imports[0].ID != imp.ID {
		t.Fatalf("imports: %+v, %v", imports, err)
	}
	// A link added by hand to an imported term goes with it.
	tpu := ""
	for _, tm := range list {
		if tm.Label == "TPU" {
			tpu = tm.ID
		}
	}
	if err := st.LinkTerms(tpu, models.TermBroader, own.ID); err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteTermImport(imp.ID); err != nil {
		t.Fatal(err)
	}
	list, links, err = st.ListTerms()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].ID != own.ID || len(links) != 0 {
		t.Errorf("after removing the import: %+v %+v", list, links)
	}
	if err := st.DeleteTermImport(imp.ID); !errors.Is(err, ErrTermImportNotFound) {
		t.Errorf("removing it again: %v", err)
	}
	var problem *TermProblem
	if _, err := st.ImportTerms("", []TermImportItem{{TermInput: TermInput{Label: "x"}}}); !errors.As(err, &problem) {
		t.Errorf("an import with no name: %v", err)
	}
	if _, err := st.ImportTerms("Empty", nil); !errors.As(err, &problem) {
		t.Errorf("an empty import: %v", err)
	}
	if _, err := st.ImportTerms("Bad", []TermImportItem{{TermInput: TermInput{Label: ""}}}); !errors.As(err, &problem) {
		t.Errorf("an import with a term with no label: %v", err)
	}
}
