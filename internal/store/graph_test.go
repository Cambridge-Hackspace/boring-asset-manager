package store

import (
	"errors"
	"fmt"
	"reflect"
	"sort"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
)

// readsAll reads every asset.
func readsAll([]string) bool { return true }

// graphNames maps each asset's name to its side and hops, as "out 2".
func graphNames(g *models.AssetGraph) map[string]string {
	out := map[string]string{}
	for _, a := range g.Assets {
		out[a.Name] = fmt.Sprintf("%s %d", a.Side, a.Hops)
	}
	return out
}

// graphLinks lists a graph's links as "from > to", by name, sorted.
func graphLinks(g *models.AssetGraph) []string {
	names := map[string]string{}
	for _, a := range g.Assets {
		names[a.ID] = a.Name
	}
	var out []string
	for _, l := range g.Links {
		out = append(out, names[l.From]+" > "+names[l.To])
	}
	sort.Strings(out)
	return out
}

func follow(t *testing.T, st *Store, root string, opts GraphOptions, canRead CanRead) *models.AssetGraph {
	t.Helper()
	g, err := st.FollowLinks(root, opts, canRead)
	if err != nil {
		t.Fatalf("following %+v: %v", opts, err)
	}
	return g
}

func TestDependsOnIsSeededAndFollowed(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	dep := relation(t, st, "depends on")
	if dep.InverseName != "needed by" || !dep.IsFollowed || dep.IsSymmetric || dep.IsSingle || dep.IsLocation {
		t.Errorf("seeded %+v", dep)
	}
	for _, name := range []string{"contains", "has part", "used with"} {
		if relation(t, st, name).IsFollowed {
			t.Errorf("%s is followed", name)
		}
	}
	// Whether it is followed changes while links use it; how it behaves
	// does not.
	a, b := asset(t, st, "A"), asset(t, st, "B")
	used := relation(t, st, "used with")
	link(t, st, a, used, b, true)
	got, err := st.UpdateRelationType(used.ID, RelationTypeInput{Name: "used with", IsSymmetric: true, IsFollowed: true})
	if err != nil || !got.IsFollowed {
		t.Fatalf("following a type in use: %+v, %v", got, err)
	}
	if _, err := st.UpdateRelationType(used.ID, RelationTypeInput{Name: "used with", InverseName: "used with", IsFollowed: true}); !errors.Is(err, ErrRelationTypeInUse) {
		t.Errorf("made a type in use directed: %v", err)
	}
	made, err := st.CreateRelationType(RelationTypeInput{Name: "powered by", InverseName: "powers", IsFollowed: true})
	if err != nil || !made.IsFollowed {
		t.Fatalf("making a followed type: %+v, %v", made, err)
	}
	types, err := st.FollowedTypes(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(types) != 3 || types[0].Name != "depends on" || types[1].Name != "powered by" || types[2].Name != "used with" {
		t.Errorf("followed types %+v", types)
	}
	if _, err := st.FollowedTypes([]string{relation(t, st, "contains").ID}); !errors.Is(err, ErrInvalidGraph) {
		t.Errorf("followed a type that is not followed: %v", err)
	}
}

// workshop links the laser cutter's chain: Bench depends on Laser cutter,
// which depends on Extractor, which depends on Three-phase, which is
// powered by Main breaker; Extractor is used with Ducting, which is not
// followed. The breaker is archived.
func workshop(t *testing.T, st *Store) map[string]string {
	t.Helper()
	dep := relation(t, st, "depends on")
	powered, err := st.CreateRelationType(RelationTypeInput{Name: "powered by", InverseName: "powers", IsFollowed: true})
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]string{}
	for _, n := range []string{"Bench", "Laser cutter", "Extractor", "Three-phase", "Main breaker", "Ducting"} {
		ids[n] = asset(t, st, n)
	}
	link(t, st, ids["Bench"], dep, ids["Laser cutter"], true)
	link(t, st, ids["Laser cutter"], dep, ids["Extractor"], true)
	// read from the other end: the supply is needed by the extractor
	link(t, st, ids["Three-phase"], dep, ids["Extractor"], false)
	link(t, st, ids["Three-phase"], *powered, ids["Main breaker"], true)
	link(t, st, ids["Extractor"], relation(t, st, "used with"), ids["Ducting"], true)
	if _, _, err := st.SetAssetCondition(ids["Three-phase"], models.ConditionOutOfService, "tripped"); err != nil {
		t.Fatal(err)
	}
	archive(t, st, ids["Main breaker"])
	return ids
}

func TestFollowingLinksBothWaysToTheDepthAsked(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	ids := workshop(t, st)

	g := follow(t, st, ids["Extractor"], GraphOptions{}, readsAll)
	want := map[string]string{"Extractor": "root 0", "Three-phase": "out 1", "Main breaker": "out 2", "Laser cutter": "in 1", "Bench": "in 2"}
	if got := graphNames(g); !reflect.DeepEqual(got, want) {
		t.Errorf("both ways: %v", got)
	}
	if got := graphLinks(g); !reflect.DeepEqual(got, []string{"Bench > Laser cutter", "Extractor > Three-phase", "Laser cutter > Extractor", "Three-phase > Main breaker"}) {
		t.Errorf("links %v", got)
	}
	if g.Direction != models.GraphBoth || g.Depth != DefaultGraphDepth || g.Hidden != 0 || g.Truncated || len(g.Loops) != 0 || len(g.Types) != 2 {
		t.Errorf("graph %+v", g)
	}
	for _, a := range g.Assets {
		switch a.Name {
		case "Main breaker":
			if !a.Archived {
				t.Error("the breaker is not marked archived")
			}
		case "Three-phase":
			if a.Condition != models.ConditionOutOfService {
				t.Errorf("the supply's condition %q", a.Condition)
			}
		}
		if len(a.Categories) != 1 || a.Categories[0].Name != "Uncategorized" {
			t.Errorf("%s's categories %+v", a.Name, a.Categories)
		}
	}

	// What stops if the breaker trips: in, from it, through both types.
	g = follow(t, st, ids["Main breaker"], GraphOptions{Direction: models.GraphIn, Depth: 10}, readsAll)
	want = map[string]string{"Main breaker": "root 0", "Three-phase": "in 1", "Extractor": "in 2", "Laser cutter": "in 3", "Bench": "in 4"}
	if got := graphNames(g); !reflect.DeepEqual(got, want) {
		t.Errorf("what needs the breaker: %v", got)
	}
	// One hop, out.
	g = follow(t, st, ids["Laser cutter"], GraphOptions{Direction: models.GraphOut, Depth: 1}, readsAll)
	if got := graphNames(g); !reflect.DeepEqual(got, map[string]string{"Laser cutter": "root 0", "Extractor": "out 1"}) {
		t.Errorf("one hop out: %v", got)
	}
	// Only the types asked for: depends on stops at the supply.
	g = follow(t, st, ids["Main breaker"], GraphOptions{Direction: models.GraphIn, TypeIDs: []string{relation(t, st, "depends on").ID}}, readsAll)
	if got := graphNames(g); !reflect.DeepEqual(got, map[string]string{"Main breaker": "root 0"}) {
		t.Errorf("depends on alone, from the breaker: %v", got)
	}
	// A symmetric type, once followed, is followed both ways.
	used := relation(t, st, "used with")
	if _, err := st.UpdateRelationType(used.ID, RelationTypeInput{Name: "used with", IsSymmetric: true, IsFollowed: true}); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{models.GraphOut, models.GraphIn} {
		g = follow(t, st, ids["Ducting"], GraphOptions{Direction: dir, Depth: 1}, readsAll)
		if got := graphNames(g); got["Extractor"] != dir+" 1" {
			t.Errorf("used with, %s: %v", dir, got)
		}
	}

	for _, bad := range []GraphOptions{{Depth: 11}, {Depth: -1}, {Direction: "sideways"}} {
		if _, err := st.FollowLinks(ids["Bench"], bad, readsAll); !errors.Is(err, ErrInvalidGraph) {
			t.Errorf("%+v: %v", bad, err)
		}
	}
	if _, err := st.FollowLinks("missing", GraphOptions{}, readsAll); !errors.Is(err, ErrAssetNotFound) {
		t.Errorf("a missing asset: %v", err)
	}
}

func TestLoopsEndAndAreReported(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	dep := relation(t, st, "depends on")
	a, b, c, d := asset(t, st, "A"), asset(t, st, "B"), asset(t, st, "C"), asset(t, st, "D")
	link(t, st, a, dep, b, true)
	link(t, st, b, dep, c, true)
	link(t, st, c, dep, a, true) // round again: a loop, which depends on allows
	link(t, st, c, dep, d, true)
	g := follow(t, st, a, GraphOptions{Direction: models.GraphBoth, Depth: MaxGraphDepth}, readsAll)
	if got := graphNames(g); !reflect.DeepEqual(got, map[string]string{"A": "root 0", "B": "out 1", "C": "out 2", "D": "out 3"}) {
		t.Errorf("around the loop: %v", got)
	}
	if !reflect.DeepEqual(g.Loops, [][]string{{a, b, c}}) {
		t.Errorf("loops %v", g.Loops)
	}
	if got := graphLinks(g); len(got) != 4 {
		t.Errorf("links %v", got)
	}
	m, err := st.GraphAmong(mustList(t, st), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(m.Loops, [][]string{{a, b, c}}) || len(m.Links) != 4 {
		t.Errorf("the map's loops %v and links %v", m.Loops, graphLinks(m))
	}
}

func mustList(t *testing.T, st *Store) []models.AssetWithDetails {
	t.Helper()
	list, err := st.ListAssets("", "")
	if err != nil {
		t.Fatal(err)
	}
	return list
}

func TestALargeGraphIsCutShort(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	dep := relation(t, st, "depends on")
	root, next := asset(t, st, "Root"), asset(t, st, "Next")
	link(t, st, root, dep, next, true)
	for i := 0; i < 12; i++ {
		link(t, st, next, dep, asset(t, st, fmt.Sprintf("Part %02d", i)), true)
	}
	g := follow(t, st, root, GraphOptions{Direction: models.GraphOut, Limit: 8}, readsAll)
	if len(g.Assets) != 8 || !g.Truncated || g.TruncatedAt != 2 {
		t.Fatalf("cut short: %d assets, truncated %v at %d", len(g.Assets), g.Truncated, g.TruncatedAt)
	}
	// The first by name are kept, and Next is marked cut.
	got := graphNames(g)
	if got["Part 00"] != "out 2" || got["Part 05"] != "out 2" || got["Part 06"] != "" {
		t.Errorf("kept %v", got)
	}
	for _, a := range g.Assets {
		if a.Cut != (a.Name == "Next") {
			t.Errorf("%s cut %v", a.Name, a.Cut)
		}
	}
	// Every link drawn is between assets in the graph.
	if len(graphLinks(g)) != 7 {
		t.Errorf("links %v", graphLinks(g))
	}
	g = follow(t, st, root, GraphOptions{Direction: models.GraphOut}, readsAll)
	if len(g.Assets) != 14 || g.Truncated {
		t.Errorf("within the limit: %d assets, truncated %v", len(g.Assets), g.Truncated)
	}
}

func TestAnUnreadableAssetIsCountedNeverNamedAndNotFollowedThrough(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	secret, err := st.CreateScope("secret")
	if err != nil {
		t.Fatal(err)
	}
	vault, err := st.CreateCategory("Vault", nil, "#000000", false, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.SetCategoryScopes(vault.ID, []string{secret.ID}); err != nil {
		t.Fatal(err)
	}
	scopes, err := st.AssetScopes([]string{asset(t, st, "Probe", vault.ID)})
	if err != nil {
		t.Fatal(err)
	}
	var probe []string
	for _, s := range scopes {
		probe = s
	}
	// The scopes it is in name the secret scope, as roles hold it.
	notSecret := func(in []string) bool { return !reflect.DeepEqual(in, probe) }

	dep := relation(t, st, "depends on")
	root, hidden, beyond, open := asset(t, st, "Root"), asset(t, st, "Hidden", vault.ID), asset(t, st, "Beyond"), asset(t, st, "Open")
	link(t, st, root, dep, hidden, true)
	link(t, st, hidden, dep, beyond, true)
	link(t, st, root, dep, open, true)
	link(t, st, beyond, dep, root, true) // a way round, not through the hidden one, but in

	g := follow(t, st, root, GraphOptions{Direction: models.GraphOut, Depth: MaxGraphDepth}, notSecret)
	if got := graphNames(g); !reflect.DeepEqual(got, map[string]string{"Root": "root 0", "Open": "out 1"}) {
		t.Errorf("seen: %v", got)
	}
	if g.Hidden != 1 {
		t.Errorf("hidden %d", g.Hidden)
	}
	for _, l := range g.Links {
		if l.From == hidden || l.To == hidden || l.From == beyond || l.To == beyond {
			t.Errorf("a link through the hidden asset: %+v", l)
		}
	}
	// Following in reaches Beyond by its own link, but not through Hidden.
	g = follow(t, st, root, GraphOptions{Direction: models.GraphIn, Depth: MaxGraphDepth}, notSecret)
	if got := graphNames(g); !reflect.DeepEqual(got, map[string]string{"Root": "root 0", "Beyond": "in 1"}) || g.Hidden != 1 {
		t.Errorf("in: %v, hidden %d", got, g.Hidden)
	}
	if _, err := st.FollowLinks(hidden, GraphOptions{}, notSecret); !errors.Is(err, ErrAssetNotFound) {
		t.Errorf("following from an unreadable asset: %v", err)
	}

	counts, err := st.FollowCounts([]string{root, open}, notSecret)
	if err != nil {
		t.Fatal(err)
	}
	// Root needs Hidden and Open directly; in all, nothing more, as Hidden
	// is not followed through. Beyond needs Root, and Hidden needs Beyond.
	if c := counts[root]; c != (models.LinkCounts{Needs: 2, NeedsAll: 2, NeededBy: 1, NeededByAll: 2}) {
		t.Errorf("root's counts %+v", c)
	}
	if c := counts[open]; c != (models.LinkCounts{NeededBy: 1, NeededByAll: 3}) {
		t.Errorf("open's counts %+v", c)
	}
	counts, err = st.FollowCounts([]string{root}, readsAll)
	if err != nil {
		t.Fatal(err)
	}
	// Reading everything, the loop round through Hidden and Beyond counts
	// each once, and Root not at all.
	if c := counts[root]; c != (models.LinkCounts{Needs: 2, NeedsAll: 3, NeededBy: 1, NeededByAll: 2}) {
		t.Errorf("root's counts, reading all %+v", c)
	}
}

func TestTheMapOfAssetsHoldsTheLinksAmongThem(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	workshop(t, st)
	var some []models.AssetWithDetails
	for _, a := range mustList(t, st) {
		if a.Asset.Name != "Bench" {
			some = append(some, a)
		}
	}
	g, err := st.GraphAmong(some, nil)
	if err != nil {
		t.Fatal(err)
	}
	// The breaker is archived, so the list leaves it out, and the bench
	// was left out here.
	if got := graphLinks(g); !reflect.DeepEqual(got, []string{"Extractor > Three-phase", "Laser cutter > Extractor"}) {
		t.Errorf("links %v", got)
	}
	if len(g.Assets) != 4 || g.Root != "" || g.Truncated {
		t.Errorf("map %+v", g)
	}
}
