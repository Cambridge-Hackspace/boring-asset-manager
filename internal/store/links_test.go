package store

import (
	"errors"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
)

// relation finds a seeded relation type by name.
func relation(t *testing.T, st *Store, name string) models.RelationType {
	t.Helper()
	types, err := st.ListRelationTypes()
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range types {
		if r.Name == name {
			return r
		}
	}
	t.Fatalf("no relation type %q in %+v", name, types)
	return models.RelationType{}
}

// link links two assets, reading from the first.
func link(t *testing.T, st *Store, assetID string, r models.RelationType, otherID string, outgoing bool) *models.AssetWithDetails {
	t.Helper()
	a, err := st.AddLink(assetID, r.ID, otherID, outgoing)
	if err != nil {
		t.Fatalf("linking %s %s %s: %v", assetID, r.Name, otherID, err)
	}
	return a
}

func names(list []models.LinkedAsset) []string {
	out := make([]string, len(list))
	for i, a := range list {
		out[i] = a.Name
	}
	return out
}

func TestContainersMakeALocation(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	contains := relation(t, st, "contains")
	shelf, bin, screws, other := asset(t, st, "Shelf 2"), asset(t, st, "Bin A3"), asset(t, st, "M3 screws"), asset(t, st, "Bin B1")

	link(t, st, shelf, contains, bin, true)
	// Read from the other end: the screws are contained in the bin.
	a := link(t, st, screws, contains, bin, false)
	if got := names(a.Location); len(got) != 2 || got[0] != "Shelf 2" || got[1] != "Bin A3" {
		t.Errorf("location %v", got)
	}
	if len(a.Links) != 1 || a.Links[0].Label != "contained in" || a.Links[0].Asset.Name != "Bin A3" || a.Links[0].Outgoing {
		t.Errorf("links %+v", a.Links)
	}
	b, _ := st.GetAssetByID(bin)
	if len(b.Links) != 2 || b.Links[0].Label != "contained in" || b.Links[1].Label != "contains" || b.Links[1].Asset.Name != "M3 screws" {
		t.Errorf("the bin's links %+v", b.Links)
	}

	// A thing is in one place: putting it in another bin moves it.
	link(t, st, other, contains, screws, true)
	a, _ = st.GetAssetByID(screws)
	if got := names(a.Location); len(got) != 1 || got[0] != "Bin B1" {
		t.Errorf("moved to %v", got)
	}
	if b, _ := st.GetAssetByID(bin); len(b.Links) != 1 {
		t.Errorf("the old bin still holds it: %+v", b.Links)
	}
	expectChanges(t, "moved", history(t, st, screws)[0], models.EventAssetEdited,
		models.EventChange{What: "link", Key: bin, Label: "contained in", From: "Bin A3"},
		models.EventChange{What: "link", Key: other, Label: "contained in", To: "Bin B1"})
	expectChanges(t, "emptied", history(t, st, bin)[0], models.EventAssetEdited,
		models.EventChange{What: "link", Key: screws, Label: "contains", From: "M3 screws"})

	// Loops and repeats are refused.
	if _, err := st.AddLink(bin, contains.ID, shelf, true); !errors.Is(err, ErrLinkCycle) {
		t.Errorf("a shelf inside its own bin: %v", err)
	}
	if _, err := st.AddLink(shelf, contains.ID, shelf, true); !errors.Is(err, ErrLinkToSelf) {
		t.Errorf("a shelf inside itself: %v", err)
	}
	if _, err := st.AddLink(other, contains.ID, screws, true); !errors.Is(err, ErrLinkExists) {
		t.Errorf("linking twice: %v", err)
	}

	// A container holding active assets cannot be archived; empty, it can.
	if err := st.DeleteAsset(other); !errors.Is(err, ErrContainerNotEmpty) {
		t.Errorf("archiving a full bin: %v", err)
	}
	archive(t, st, screws)
	archive(t, st, other)
	// Archived assets keep their links, marked as archived.
	if a, _ := st.GetAssetByID(screws); len(a.Links) != 1 || !a.Links[0].Asset.Archived {
		t.Errorf("archived screws: %+v", a.Links)
	}
	if _, err := st.AddLink(shelf, contains.ID, screws, true); !errors.Is(err, ErrAssetArchived) {
		t.Errorf("linking an archived asset: %v", err)
	}
}

func TestSymmetricLinksAndRemoving(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	usedWith := relation(t, st, "used with")
	drill, bits := asset(t, st, "Drill"), asset(t, st, "Drill bits")
	a := link(t, st, drill, usedWith, bits, false)
	if len(a.Links) != 1 || a.Links[0].Label != "used with" {
		t.Fatalf("links %+v", a.Links)
	}
	// Stored once, it reads the same from the other end, and cannot be
	// made again from there.
	if _, err := st.AddLink(bits, usedWith.ID, drill, true); !errors.Is(err, ErrLinkExists) {
		t.Errorf("the same link from the other end: %v", err)
	}
	b, _ := st.GetAssetByID(bits)
	if len(b.Links) != 1 || b.Links[0].Label != "used with" || b.Links[0].Asset.Name != "Drill" {
		t.Errorf("from the bits: %+v", b.Links)
	}
	if got := relation(t, st, "used with").LinkCount; got != 1 {
		t.Errorf("link count %d", got)
	}

	// Removing it, from either end, records it on both.
	if _, err := st.RemoveLink(drill, "no-such-link"); !errors.Is(err, ErrLinkNotFound) {
		t.Errorf("a missing link: %v", err)
	}
	if _, err := st.RemoveLink(asset(t, st, "Saw"), b.Links[0].ID); !errors.Is(err, ErrLinkNotFound) {
		t.Errorf("another asset's link: %v", err)
	}
	a, err := st.RemoveLink(bits, b.Links[0].ID)
	if err != nil || len(a.Links) != 0 {
		t.Fatalf("removed: %+v, %v", a, err)
	}
	expectChanges(t, "removed", history(t, st, drill)[0], models.EventAssetEdited,
		models.EventChange{What: "link", Key: bits, Label: "used with", From: "Drill bits"})
}

func TestRelationTypes(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	if types, _ := st.ListRelationTypes(); len(types) != 3 {
		t.Fatalf("seeded %+v", types)
	}
	powers, err := st.CreateRelationType(RelationTypeInput{Name: " powers ", InverseName: "powered by", IsSingle: true})
	if err != nil || powers.Name != "powers" || !powers.IsSingle {
		t.Fatalf("created %+v, %v", powers, err)
	}
	pairs, err := st.CreateRelationType(RelationTypeInput{Name: "pairs with", InverseName: "ignored", IsSymmetric: true})
	if err != nil || pairs.InverseName != "pairs with" {
		t.Errorf("a symmetric type %+v, %v", pairs, err)
	}
	for what, in := range map[string]RelationTypeInput{
		"no name":                  {InverseName: "x"},
		"no inverse":               {Name: "x"},
		"symmetric and single":     {Name: "x", IsSymmetric: true, IsSingle: true},
		"a location that is many":  {Name: "x", InverseName: "y", IsLocation: true},
		"a name too long to store": {Name: string(make([]byte, 65)), InverseName: "y"},
	} {
		if _, err := st.CreateRelationType(in); !errors.Is(err, ErrInvalidRelationType) {
			t.Errorf("%s: %v", what, err)
		}
	}
	if _, err := st.CreateRelationType(RelationTypeInput{Name: "powers", InverseName: "z"}); !IsUniqueViolation(err) {
		t.Errorf("a name in use: %v", err)
	}

	// In use, only its names change, and it cannot be deleted.
	link(t, st, asset(t, st, "Generator"), *powers, asset(t, st, "Lamp"), true)
	if _, err := st.UpdateRelationType(powers.ID, RelationTypeInput{Name: "supplies", InverseName: "supplied by", IsSingle: true}); err != nil {
		t.Errorf("renaming: %v", err)
	}
	if _, err := st.UpdateRelationType(powers.ID, RelationTypeInput{Name: "supplies", InverseName: "supplied by"}); !errors.Is(err, ErrRelationTypeInUse) {
		t.Errorf("changing how a type in use behaves: %v", err)
	}
	if err := st.DeleteRelationType(powers.ID); !errors.Is(err, ErrRelationTypeInUse) {
		t.Errorf("deleting a type in use: %v", err)
	}
	if err := st.DeleteRelationType(pairs.ID); err != nil {
		t.Errorf("deleting an unused type: %v", err)
	}

	// Categories suggest types to their assets and their subcategories'.
	tools, err := st.CreateCategoryFrom(CategoryInput{Name: "Tools"})
	if err != nil {
		t.Fatal(err)
	}
	drills, err := st.CreateCategoryFrom(CategoryInput{Name: "Drills", ParentID: &tools.ID})
	if err != nil {
		t.Fatal(err)
	}
	if ids, err := st.SetCategoryRelationTypes(tools.ID, []string{powers.ID, powers.ID}); err != nil || len(ids) != 1 {
		t.Fatalf("suggested %v, %v", ids, err)
	}
	if _, err := st.SetCategoryRelationTypes(tools.ID, []string{"no-such-type"}); !errors.Is(err, ErrRelationTypeNotFound) {
		t.Errorf("an unknown type: %v", err)
	}
	if a, _ := st.GetAssetByID(asset(t, st, "Cordless drill", drills.ID)); len(a.SuggestedRelationTypeIDs) != 1 || a.SuggestedRelationTypeIDs[0] != powers.ID {
		t.Errorf("suggested to a drill: %v", a.SuggestedRelationTypeIDs)
	}
}
