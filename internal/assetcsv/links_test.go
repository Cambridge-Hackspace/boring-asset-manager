package assetcsv

import (
	"errors"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// linksOf lists an asset's links as an export would write them, sorted.
func linksOf(t *testing.T, st *store.Store, name string) []string {
	t.Helper()
	list, err := st.ListAssets("", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range list {
		if a.Asset.Name == name {
			out := []string{}
			for _, l := range a.Links {
				out = append(out, store.RelationSlug(l.Label)+":"+l.Asset.Name)
			}
			sort.Strings(out)
			return out
		}
	}
	t.Fatalf("no asset %q", name)
	return nil
}

// idOf finds an active asset's ID by name.
func idOf(t *testing.T, st *store.Store, name string) string {
	t.Helper()
	list, err := st.ListAssets("", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range list {
		if a.Asset.Name == name {
			return a.Asset.ID
		}
	}
	t.Fatalf("no asset %q", name)
	return ""
}

func applyWith(t *testing.T, w *world, file string, opts Options) *Plan {
	t.Helper()
	plan, err := PrepareWith(w.st, []byte(file), nil, opts)
	if err != nil {
		t.Fatal(err)
	}
	if plan.ProblemCount > 0 {
		t.Fatalf("problems: %+v", plan.Problems)
	}
	if _, err := w.st.ImportAssets(plan.Changes, plan.Links); err != nil {
		t.Fatal(err)
	}
	return plan
}

func TestImportingLinks(t *testing.T) {
	w := newWorld(t)
	// A shelf, a bin on it and screws in the bin, all made by the file, the
	// links reading from either end and naming the drill by its code.
	plan := applyWith(t, w, "name,links\n"+
		"Shelf 2,contains:Bin A3\n"+
		"Bin A3,\n"+
		"M3 screws,Contained In:Bin A3;used_with:D1\n", Options{})
	if plan.Create != 3 || plan.LinksAdded != 3 || plan.LinksMoved != 0 {
		t.Fatalf("plan %+v", plan)
	}
	if got := linksOf(t, w.st, "M3 screws"); !reflect.DeepEqual(got, []string{"contained_in:Bin A3", "used_with:Drill"}) {
		t.Errorf("screws: %v", got)
	}
	if got := linksOf(t, w.st, "Bin A3"); !reflect.DeepEqual(got, []string{"contained_in:Shelf 2", "contains:M3 screws"}) {
		t.Errorf("bin: %v", got)
	}
	a, _ := w.st.ListAssets("", "M3 screws")
	if h, _, err := w.st.ListAssetEvents(a[0].Asset.ID, "", 1); err != nil || h[0].Payload.Trigger != models.TriggerImport {
		t.Errorf("history: %+v", h[0])
	}

	// Moved to another bin; without the option, nothing else goes.
	screws := idOf(t, w.st, "M3 screws")
	plan = applyWith(t, w, "id,name,links\n,Bin B1,\n"+screws+",M3 screws,contained_in:Bin B1\n", Options{})
	if plan.LinksMoved != 1 || plan.LinksAdded != 0 || plan.LinksRemoved != 0 || plan.LinkChanges[0] != (LinkChange{Row: 3, Change: "move", Asset: "Bin B1", Label: "contains", Other: "M3 screws"}) {
		t.Errorf("move: %+v", plan)
	}
	if got := linksOf(t, w.st, "M3 screws"); !reflect.DeepEqual(got, []string{"contained_in:Bin B1", "used_with:Drill"}) {
		t.Errorf("after the move: %v", got)
	}

	// With the option, links no row states go, but only the file's assets'.
	plan = applyWith(t, w, "id,links\n"+screws+",contained_in:Bin B1\n", Options{RemoveLinks: true})
	if plan.LinksRemoved != 1 || plan.LinkChanges[0].Other != "Drill" {
		t.Errorf("remove: %+v", plan)
	}
	if got := linksOf(t, w.st, "Bin A3"); !reflect.DeepEqual(got, []string{"contained_in:Shelf 2"}) {
		t.Errorf("an asset not in the file: %v", got)
	}
	if got := linksOf(t, w.st, "M3 screws"); !reflect.DeepEqual(got, []string{"contained_in:Bin B1"}) {
		t.Errorf("after removing: %v", got)
	}
}

func TestLinkProblems(t *testing.T) {
	w := newWorld(t)
	applyWith(t, w, "name,links\nShelf,contains:Bin\nBin,\nTwin,\nTwin,\n", Options{})
	gone, err := w.st.CreateAsset("Old bin", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.st.DeleteAsset(gone.Asset.ID); err != nil {
		t.Fatal(err)
	}
	drill, shelf := w.drill.Asset.ID, idOf(t, w.st, "Shelf")
	for what, c := range map[string]struct{ row, want string }{
		"no colon":          {drill + ",contained_in", "is not verb:asset"},
		"an unknown verb":   {drill + ",stored_in:Bin", "no relation is written"},
		"an unknown asset":  {drill + ",contained_in:Nowhere", "no asset is"},
		"an ambiguous name": {drill + ",used_with:Twin", "names 2 assets"},
		"itself":            {drill + ",used_with:Drill", "linked to itself"},
		"a loop":            {shelf + ",contained_in:Bin", "would make a loop"},
		"an archived asset": {drill + ",contained_in:" + gone.Asset.ID, "is archived"},
	} {
		plan, err := Prepare(w.st, []byte("id,links\n"+c.row+"\n"), nil)
		if err != nil {
			t.Fatal(err)
		}
		if plan.ProblemCount != 1 || plan.Links != nil || plan.Problems[0].Column != "links" || !strings.Contains(plan.Problems[0].Message, c.want) {
			t.Errorf("%s: %+v", what, plan.Problems)
		}
	}
	// Put in two places by two rows.
	plan, _ := Prepare(w.st, []byte("id,links\n"+drill+",contained_in:Bin\n"+w.hammer.Asset.ID+",contains:Drill\n"), nil)
	if plan.ProblemCount != 1 || !strings.Contains(plan.Problems[0].Message, "contained in both") {
		t.Errorf("two places: %+v", plan.Problems)
	}
}

func TestLinksRoundTrip(t *testing.T) {
	w := newWorld(t)
	applyWith(t, w, "name,links\n"+
		"Shelf,contains:Bin\n"+
		"Bin,\n"+
		"Bits,used_with:Drill\n"+
		"Twin,\nTwin,contained_in:Bin\n", Options{})
	all, err := w.st.ListAssets("", "")
	if err != nil {
		t.Fatal(err)
	}
	cats, _ := w.st.ListAllCategories()
	var b strings.Builder
	if err := Export(&b, all, NewCategories(cats), []string{"code128"}, NewAssetRefs(all)); err != nil {
		t.Fatal(err)
	}
	file := b.String()
	// Names where they name one asset; IDs for the twins.
	if !strings.Contains(file, "contained_in:Shelf") || !strings.Contains(file, "used_with:Drill") || strings.Contains(file, ":Twin") {
		t.Errorf("export:\n%s", file)
	}
	// Planned again, even removing what no row states, it changes nothing.
	plan, err := PrepareWith(w.st, []byte(file), nil, Options{RemoveLinks: true})
	if err != nil {
		t.Fatal(err)
	}
	if plan.ProblemCount > 0 || plan.LinksAdded+plan.LinksMoved+plan.LinksRemoved != 0 || plan.Update+plan.Create != 0 {
		t.Errorf("round trip: %+v", plan)
	}
}

func TestOutdatedLinkImportsChangeNothing(t *testing.T) {
	w := newWorld(t)
	applyWith(t, w, "name,links\nBin A,contains:Screws\nBin B,\nScrews,\n", Options{})
	screws := idOf(t, w.st, "Screws")
	plan, err := Prepare(w.st, []byte("id,links\n"+screws+",contained_in:Bin B\n"), nil)
	if err != nil || plan.LinksMoved != 1 {
		t.Fatalf("plan %+v, %v", plan, err)
	}
	// Someone takes the screws out of Bin A after the check.
	a, _ := w.st.GetAssetByID(screws)
	if _, err := w.st.RemoveLink(screws, a.Links[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := w.st.ImportAssets(plan.Changes, plan.Links); !errors.Is(err, store.ErrImportOutdated) {
		t.Fatalf("applying an outdated plan: %v", err)
	}
	if got := linksOf(t, w.st, "Screws"); len(got) != 0 {
		t.Errorf("an outdated import changed links: %v", got)
	}
}
