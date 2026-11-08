package store

import (
	"errors"
	"strings"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/fields"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
)

// pool creates a consumable category weighing in unit, with an optional
// threshold.
func pool(t *testing.T, st *Store, name, unit string, parent *string, threshold *int) *models.Category {
	t.Helper()
	cat, err := st.CreateCategoryFrom(CategoryInput{Name: name, ParentID: parent, IsConsumable: true, Unit: unit, LowStockThreshold: threshold})
	if err != nil {
		t.Fatalf("creating %s: %v", name, err)
	}
	return cat
}

func plainCategory(t *testing.T, st *Store, name string, parent *string) *models.Category {
	t.Helper()
	cat, err := st.CreateCategoryFrom(CategoryInput{Name: name, ParentID: parent})
	if err != nil {
		t.Fatalf("creating %s: %v", name, err)
	}
	return cat
}

// implies links from to the categories given, replacing its links.
func implies(t *testing.T, st *Store, from string, to ...string) {
	t.Helper()
	if to == nil {
		to = []string{}
	}
	if _, err := st.SetCategoryLinks(from, CategoryLinks{Implies: &to}); err != nil {
		t.Fatalf("linking %s to %v: %v", from, to, err)
	}
}

// bin creates an asset counted by quantity in the categories given.
func bin(t *testing.T, st *Store, name string, quantity int, unit string, threshold *int, cats ...string) string {
	t.Helper()
	a, err := st.CreateAssetWithStock(name, cats, nil, nil, &StockInput{Quantity: intp(quantity), Unit: unit, LowStockThreshold: threshold})
	if err != nil {
		t.Fatalf("creating %s: %v", name, err)
	}
	return a.Asset.ID
}

// alerts lists the events channels can subscribe to, oldest first.
func alerts(t *testing.T, st *Store) []models.Event {
	t.Helper()
	var out []models.Event
	for _, e := range events(t, st) {
		if !models.HistoryOnly(e.Kind) {
			out = append(out, e)
		}
	}
	return out
}

func total(t *testing.T, st *Store, id, unit string) int {
	t.Helper()
	n, err := st.CategoryTotal(id, unit)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func filtered(t *testing.T, st *Store, category string) []string {
	t.Helper()
	list, err := st.FilterAssets(AssetFilter{CategoryID: category, Loan: LoansAll})
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, a := range list {
		names = append(names, a.Asset.Name)
	}
	return names
}

func TestLinksPutAMembersAssetsInTheCategoryItImplies(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()

	workshop := plainCategory(t, st, "Workshop", nil)
	m5 := plainCategory(t, st, "M5", &workshop.ID)
	laser := plainCategory(t, st, "Laser bay", nil)
	stock := plainCategory(t, st, "Stock", nil)
	bolts := pool(t, st, "M5 bolts", "pcs", &stock.ID, nil)
	fasteners := pool(t, st, "Fasteners", "pcs", nil, nil)

	drawer := bin(t, st, "Drawer", 140, "pcs", nil, m5.ID)
	bay := bin(t, st, "Bay bin", 300, "pcs", nil, laser.ID)
	implies(t, st, m5.ID, bolts.ID)
	implies(t, st, laser.ID, bolts.ID)

	if n := total(t, st, bolts.ID, "pcs"); n != 440 {
		t.Errorf("the pool totals %d, want 440", n)
	}
	if got := strings.Join(filtered(t, st, bolts.ID), ","); got != "Bay bin,Drawer" && got != "Drawer,Bay bin" {
		t.Errorf("filtering by the pool finds %q", got)
	}
	// A chain: the pool implies another, which totals all of them.
	implies(t, st, bolts.ID, fasteners.ID)
	if n := total(t, st, fasteners.ID, "pcs"); n != 440 {
		t.Errorf("through a chain, the total is %d, want 440", n)
	}
	if got := filtered(t, st, fasteners.ID); len(got) != 2 {
		t.Errorf("filtering through a chain finds %v", got)
	}
	// The category above the one implied counts them too, as it counts its
	// subcategories'.
	if n, err := st.ActiveCount(stock.ID); err != nil || n != 2 {
		t.Errorf("above the pool, %d assets (%v), want 2", n, err)
	}

	// Its page says where else it counts, and through which category.
	a, err := st.GetAssetByID(drawer)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, c := range a.CountsIn {
		got[c.CategoryName] = c.ThroughName
	}
	if got["M5 bolts"] != "M5" || got["Fasteners"] != "M5" || len(got) != 2 {
		t.Errorf("counts in %+v", a.CountsIn)
	}

	// Taken out of the category, it is out of the pool at once.
	if _, err := st.UpdateAsset(drawer, "", []string{workshop.ID}, nil, nil); err != nil {
		t.Fatal(err)
	}
	if n := total(t, st, bolts.ID, "pcs"); n != 300 {
		t.Errorf("after leaving its category, the pool totals %d, want 300", n)
	}
	// Unless it is in the pool itself as well.
	if _, err := st.UpdateAsset(bay, "", []string{bolts.ID}, nil, nil); err != nil {
		t.Fatal(err)
	}
	if n := total(t, st, bolts.ID, "pcs"); n != 300 {
		t.Errorf("an asset in the pool directly totals %d, want 300", n)
	}
	// Taking the link away leaves what is in it directly.
	implies(t, st, laser.ID)
	if n := total(t, st, bolts.ID, "pcs"); n != 300 {
		t.Errorf("unlinked, an asset in the pool directly totals %d, want 300", n)
	}
	if got := filtered(t, st, bolts.ID); len(got) != 1 || got[0] != "Bay bin" {
		t.Errorf("unlinked, filtering finds %v", got)
	}

	// Either side edits the same link.
	if _, err := st.SetCategoryLinks(bolts.ID, CategoryLinks{IncludedBy: &[]string{laser.ID, m5.ID}}); err != nil {
		t.Fatal(err)
	}
	c, err := st.GetCategoryByID(laser.ID)
	if err != nil || len(c.Implies) != 1 || c.Implies[0] != bolts.ID {
		t.Errorf("the laser bay implies %v (%v)", c.Implies, err)
	}
	c, err = st.GetCategoryByID(bolts.ID)
	if err != nil || len(c.IncludedBy) != 2 || len(c.Implies) != 1 {
		t.Errorf("the pool is included by %v and implies %v (%v)", c.IncludedBy, c.Implies, err)
	}
}

func TestLinksThatWouldMakeACycleAreRefused(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()

	x := plainCategory(t, st, "X", nil)
	child := plainCategory(t, st, "X child", &x.ID)
	y := plainCategory(t, st, "Y", nil)
	z := plainCategory(t, st, "Z", nil)
	implies(t, st, x.ID, y.ID)
	implies(t, st, y.ID, z.ID)

	for name, try := range map[string]func() error{
		"back to the start": func() error {
			_, err := st.SetCategoryLinks(z.ID, CategoryLinks{Implies: &[]string{x.ID}})
			return err
		},
		"itself": func() error {
			_, err := st.SetCategoryLinks(y.ID, CategoryLinks{Implies: &[]string{y.ID}})
			return err
		},
		"one inside it": func() error {
			_, err := st.SetCategoryLinks(x.ID, CategoryLinks{Implies: &[]string{child.ID}})
			return err
		},
		"from the other side": func() error {
			_, err := st.SetCategoryLinks(x.ID, CategoryLinks{IncludedBy: &[]string{z.ID}})
			return err
		},
		"moving a category under one leading to it": func() error {
			_, err := st.UpdateCategoryFrom(z.ID, CategoryInput{Name: "Z", ParentID: &x.ID})
			return err
		},
	} {
		if err := try(); !errors.Is(err, ErrImpliesCycle) {
			t.Errorf("%s: %v, want a refusal", name, err)
		}
	}
	if _, err := st.SetCategoryLinks(UncategorizedCategoryID, CategoryLinks{Implies: &[]string{y.ID}}); !errors.Is(err, ErrImpliesSystem) {
		t.Errorf("Uncategorized implying: %v", err)
	}
	if _, err := st.SetCategoryLinks(x.ID, CategoryLinks{Implies: &[]string{"missing"}}); !errors.Is(err, ErrCategoryNotFound) {
		t.Errorf("a missing category: %v", err)
	}
	// What was refused changed nothing.
	c, err := st.GetCategoryByID(x.ID)
	if err != nil || len(c.Implies) != 1 || c.Implies[0] != y.ID || c.ParentID != nil {
		t.Errorf("after the refusals X implies %v under %v (%v)", c.Implies, c.ParentID, err)
	}
}

func TestAnAssetReachedTwiceCountsOnce(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()

	a := plainCategory(t, st, "A", nil)
	b := plainCategory(t, st, "B", nil)
	y := pool(t, st, "Y", "pcs", nil, nil)
	both := bin(t, st, "In both", 10, "pcs", nil, a.ID, b.ID)
	direct := bin(t, st, "Direct too", 5, "pcs", nil, a.ID, y.ID)
	implies(t, st, a.ID, y.ID)
	implies(t, st, b.ID, y.ID)

	if n := total(t, st, y.ID, "pcs"); n != 15 {
		t.Errorf("total %d, want 15: each asset once", n)
	}
	if n, err := st.ActiveCount(y.ID); err != nil || n != 2 {
		t.Errorf("count %d (%v), want 2", n, err)
	}
	if got := filtered(t, st, y.ID); len(got) != 2 {
		t.Errorf("filter finds %v", got)
	}
	members, err := st.CategoryAssets(y.ID, "pcs")
	if err != nil || len(members) != 2 || members[both] != 10 || members[direct] != 5 {
		t.Errorf("members %v (%v)", members, err)
	}
	// One in the pool directly does not count in it through a link.
	asset, err := st.GetAssetByID(direct)
	if err != nil {
		t.Fatal(err)
	}
	if len(asset.CountsIn) != 0 {
		t.Errorf("an asset in the pool directly counts in %+v through links", asset.CountsIn)
	}
}

func TestLinkingRechecksTheThreshold(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()

	drawer := plainCategory(t, st, "Drawer", nil)
	bay := plainCategory(t, st, "Bay", nil)
	bolts := pool(t, st, "M5 bolts", "pcs", nil, intp(200))
	bin(t, st, "Drawer bin", 140, "pcs", nil, drawer.ID)
	bin(t, st, "Bay bin", 300, "pcs", nil, bay.ID)
	if !flag(t, st, bolts.ID) {
		t.Fatal("an empty pool with a threshold is not running low")
	}
	before := len(events(t, st))

	implies(t, st, drawer.ID, bolts.ID)
	if !flag(t, st, bolts.ID) {
		t.Error("140 of 200 cleared the flag")
	}
	implies(t, st, bay.ID, bolts.ID)
	if flag(t, st, bolts.ID) {
		t.Error("440 of 200 left the pool running low")
	}
	got := events(t, st)[before:]
	if kinds(got) != models.EventRestocked {
		t.Errorf("linking recorded %q", kinds(got))
	} else if p := got[0].Payload; *p.ActiveCount != 440 || *p.Threshold != 200 || p.Unit != "pcs" || p.Trigger != models.TriggerThreshold {
		t.Errorf("restocked with %+v", p)
	}
	// Taking a link away drops it back below.
	if _, err := st.SetCategoryLinks(bolts.ID, CategoryLinks{IncludedBy: &[]string{drawer.ID}}); err != nil {
		t.Fatal(err)
	}
	if !flag(t, st, bolts.ID) {
		t.Error("unlinking to 140 of 200 left it in stock")
	}
	got = events(t, st)[before:]
	if kinds(got) != models.EventRestocked+" "+models.EventRunningLow {
		t.Errorf("unlinking recorded %q", kinds(got))
	}
}

func TestALinkGivesOnlyMembership(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()

	x := plainCategory(t, st, "X", nil)
	y := pool(t, st, "Y", "pcs", nil, nil)
	scope, err := st.CreateScope("lab")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.SetCategoryScopes(y.ID, []string{scope.ID}); err != nil {
		t.Fatal(err)
	}
	field, err := st.CreateField("serial", "Serial", fields.TypeText, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AssignCategoryField(y.ID, field.ID, true); err != nil {
		t.Fatal(err)
	}
	implies(t, st, x.ID, y.ID)

	// No field is asked for, so this succeeds without a serial.
	id := bin(t, st, "Bin", 3, "pcs", nil, x.ID)
	a, err := st.GetAssetByID(id)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range a.Scopes {
		if s == "lab" {
			t.Errorf("the asset took Y's scope: %v", a.Scopes)
		}
	}
	if len(a.EffectiveFields) != 0 {
		t.Errorf("the asset took Y's fields: %+v", a.EffectiveFields)
	}
	if a.Weighed {
		t.Error("a link made the asset weighed, which would let it be lent")
	}
	if len(a.CountsIn) != 1 || a.CountsIn[0].CategoryID != y.ID {
		t.Errorf("it counts in %+v", a.CountsIn)
	}
}

func TestQuietBinsAlertOnlyWhenTheirPoolIsShort(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()

	drawer := plainCategory(t, st, "Drawer", nil)
	bay := plainCategory(t, st, "Bay", nil)
	bolts := pool(t, st, "M5 bolts", "pcs", nil, intp(200))
	implies(t, st, drawer.ID, bolts.ID)
	implies(t, st, bay.ID, bolts.ID)
	here := bin(t, st, "Drawer bin", 140, "pcs", intp(50), drawer.ID)
	there := bin(t, st, "Bay bin", 300, "pcs", intp(50), bay.ID)
	other := bin(t, st, "Washers", 100, "pcs", intp(50))
	before := len(alerts(t, st))

	// Emptying a bin while the pool holds enough raises nothing for it.
	adjust(t, st, here, -140)
	if stockOf(t, st, here).IsRunningLow {
		t.Error("an empty bin in a full pool flagged itself")
	}
	if got := alerts(t, st)[before:]; kinds(got) != "" {
		t.Errorf("emptying a bin in a full pool recorded %q", kinds(got))
	}
	// A bin in no pool alerts as before.
	adjust(t, st, other, -60)
	if !stockOf(t, st, other).IsRunningLow {
		t.Error("a bin in no pool did not flag itself")
	}

	// Its page says where else to look.
	pools, err := st.AssetPools(here)
	if err != nil {
		t.Fatal(err)
	}
	if len(pools) != 1 || pools[0].Name != "M5 bolts" || pools[0].Total != 300 || len(pools[0].Places) != 2 {
		t.Fatalf("pools %+v", pools)
	}
	if p := pools[0].Places; p[0].AssetID != here || p[0].Quantity != 0 || p[1].AssetID != there || p[1].Quantity != 300 {
		t.Errorf("places %+v: here first, then where else", p)
	}
	if pools, err := st.AssetPools(other); err != nil || len(pools) != 0 {
		t.Errorf("a bin in no pool is in %+v (%v)", pools, err)
	}

	// When the pool runs short, the pool and the bins below their own
	// thresholds alert.
	before = len(alerts(t, st))
	adjust(t, st, there, -260)
	got := alerts(t, st)[before:]
	if kinds(got) != models.EventRunningLow+" "+models.EventAssetRunningLow+" "+models.EventAssetRunningLow {
		t.Errorf("a real shortage recorded %q", kinds(got))
	}
	if !stockOf(t, st, here).IsRunningLow || !stockOf(t, st, there).IsRunningLow || !flag(t, st, bolts.ID) {
		t.Error("a real shortage left something unflagged")
	}
	// Restocked elsewhere, the empty bin is quiet again.
	adjust(t, st, there, 400)
	if stockOf(t, st, here).IsRunningLow || flag(t, st, bolts.ID) {
		t.Error("restocking the pool left the empty bin or the pool flagged")
	}
}

func TestOnlyStockInAWeighingCategorysUnitAddsToItsTotal(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()

	bolts := plainCategory(t, st, "Bolts", nil)
	tape := plainCategory(t, st, "Tape", nil)
	weighed := pool(t, st, "Metal", "g", nil, nil)
	bin(t, st, "Bolt bin", 140, "pcs", nil, bolts.ID)
	bin(t, st, "Bolt jar", 500, "g", nil, bolts.ID)
	asset(t, st, "Tape roll", tape.ID)
	implies(t, st, bolts.ID, weighed.ID)
	implies(t, st, tape.ID, weighed.ID)

	if n := total(t, st, weighed.ID, "g"); n != 500 {
		t.Errorf("total %d, want only the 500 g", n)
	}
	if got := filtered(t, st, weighed.ID); len(got) != 3 {
		t.Errorf("all three are members, but the filter finds %v", got)
	}
	warnings, err := st.CategoryLinkWarnings(weighed.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 1 || warnings[0].CategoryID != tape.ID || warnings[0].Unit != "g" ||
		warnings[0].Message != "None of Tape's assets count in g, so they add nothing to Metal's total." {
		t.Errorf("warnings %+v", warnings)
	}
	// From the other side too.
	if w, err := st.CategoryLinkWarnings(tape.ID); err != nil || len(w) != 1 {
		t.Errorf("from the tape's side %+v (%v)", w, err)
	}
	if w, err := st.CategoryLinkWarnings(bolts.ID); err != nil || len(w) != 0 {
		t.Errorf("the bolts warn %+v (%v)", w, err)
	}
}

func TestMovingStockBetweenBins(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()

	drawer := plainCategory(t, st, "Drawer", nil)
	bay := plainCategory(t, st, "Bay", nil)
	bolts := pool(t, st, "M5 bolts", "pcs", nil, intp(200))
	implies(t, st, drawer.ID, bolts.ID)
	implies(t, st, bay.ID, bolts.ID)
	here := bin(t, st, "Drawer bin", 140, "pcs", intp(100), drawer.ID)
	there := bin(t, st, "Bay bin", 100, "pcs", intp(50), bay.ID)
	grams := bin(t, st, "Solder", 100, "g", nil)

	if _, _, err := st.MoveStock(here, there, 141); !errors.Is(err, ErrNotEnoughStock) {
		t.Errorf("moving more than there is: %v", err)
	}
	if _, _, err := st.MoveStock(here, grams, 1); !errors.Is(err, ErrMoveUnits) {
		t.Errorf("moving between units: %v", err)
	}
	if _, _, err := st.MoveStock(here, here, 1); !errors.Is(err, ErrMoveSameAsset) {
		t.Errorf("moving to itself: %v", err)
	}
	if _, _, err := st.MoveStock(here, there, 0); !errors.Is(err, ErrMoveAmount) {
		t.Errorf("moving nothing: %v", err)
	}
	if s := stockOf(t, st, here); s.Quantity != 140 {
		t.Errorf("a refused move left %d", s.Quantity)
	}

	before := len(events(t, st))
	from, to, err := st.MoveStock(here, there, 100)
	if err != nil {
		t.Fatal(err)
	}
	if from.Asset.Stock.Quantity != 40 || to.Asset.Stock.Quantity != 200 {
		t.Errorf("after the move %d and %d", from.Asset.Stock.Quantity, to.Asset.Stock.Quantity)
	}
	// The pool's total is unchanged, so it records nothing; the drawer's
	// own flag stays quiet while the pool holds enough.
	for _, e := range events(t, st)[before:] {
		if e.Kind != models.EventQuantityChanged {
			t.Errorf("the move recorded %s", e.Kind)
		}
	}
	for id, change := range map[string]int{here: -100, there: 100} {
		list, _, err := st.ListAssetEvents(id, "", 10)
		if err != nil {
			t.Fatal(err)
		}
		e := list[0]
		if e.Kind != models.EventQuantityChanged || e.Payload.Move == nil || *e.Payload.Change != change ||
			e.Payload.Move.From.ID != here || e.Payload.Move.To.ID != there || e.Payload.Move.To.Name != "Bay bin" {
			t.Errorf("%s's history has %+v", id, e.Payload)
		}
	}
}

func TestCloningACategoryKeepsWhereItCounts(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()

	drawer := plainCategory(t, st, "Drawer", nil)
	bolts := pool(t, st, "M5 bolts", "pcs", nil, nil)
	other := plainCategory(t, st, "Other", nil)
	implies(t, st, drawer.ID, bolts.ID)
	if _, err := st.SetCategoryLinks(drawer.ID, CategoryLinks{IncludedBy: &[]string{other.ID}}); err != nil {
		t.Fatal(err)
	}
	clone, err := st.CloneCategory(drawer.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(clone.Implies) != 1 || clone.Implies[0] != bolts.ID || len(clone.IncludedBy) != 0 {
		t.Errorf("the copy implies %v and is included by %v", clone.Implies, clone.IncludedBy)
	}
	// Deleting a category takes its links with it.
	if err := st.DeleteCategory(drawer.ID); err != nil {
		t.Fatal(err)
	}
	c, err := st.GetCategoryByID(bolts.ID)
	if err != nil || len(c.IncludedBy) != 1 || c.IncludedBy[0] != clone.ID {
		t.Errorf("after deleting, the pool is included by %v (%v)", c.IncludedBy, err)
	}
}
