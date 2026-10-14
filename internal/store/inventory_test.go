package store

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
)

func intp(n int) *int { return &n }

// consumable creates a consumable category with an optional threshold.
func consumable(t *testing.T, st *Store, name string, parent *string, threshold *int) *models.Category {
	t.Helper()
	cat, err := st.CreateCategoryFrom(CategoryInput{Name: name, ParentID: parent, IsConsumable: true, LowStockThreshold: threshold})
	if err != nil {
		t.Fatalf("creating %s: %v", name, err)
	}
	return cat
}

func asset(t *testing.T, st *Store, name string, cats ...string) string {
	t.Helper()
	a, err := st.CreateAsset(name, cats, nil, nil)
	if err != nil {
		t.Fatalf("creating %s: %v", name, err)
	}
	return a.Asset.ID
}

func archive(t *testing.T, st *Store, id string) {
	t.Helper()
	if err := st.DeleteAsset(id); err != nil {
		t.Fatalf("archiving %s: %v", id, err)
	}
}

// flag reads a category's running-low flag.
func flag(t *testing.T, st *Store, id string) bool {
	t.Helper()
	c, err := st.GetCategoryByID(id)
	if err != nil {
		t.Fatal(err)
	}
	return c.IsRunningLow
}

// events lists every event, oldest first.
func events(t *testing.T, st *Store) []models.Event {
	t.Helper()
	list, err := st.ListEvents(1000)
	if err != nil {
		t.Fatal(err)
	}
	for i, j := 0, len(list)-1; i < j; i, j = i+1, j-1 {
		list[i], list[j] = list[j], list[i]
	}
	return list
}

func TestThresholdValidation(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()

	_, err := st.CreateCategoryFrom(CategoryInput{Name: "Bolts", LowStockThreshold: intp(3)})
	if !errors.Is(err, ErrThresholdRequiresConsumable) {
		t.Errorf("threshold on a non-consumable: got %v", err)
	}
	for _, bad := range []int{0, -1, MaxLowStockThreshold + 1} {
		_, err := st.CreateCategoryFrom(CategoryInput{Name: "Bolts", IsConsumable: true, LowStockThreshold: intp(bad)})
		if !errors.Is(err, ErrThresholdOutOfRange) {
			t.Errorf("threshold %d: got %v", bad, err)
		}
	}
	cat := consumable(t, st, "Bolts", nil, intp(MaxLowStockThreshold))
	if cat.LowStockThreshold == nil || *cat.LowStockThreshold != MaxLowStockThreshold {
		t.Errorf("stored threshold %v", cat.LowStockThreshold)
	}
	// Turning consumable off must also drop the threshold.
	_, err = st.UpdateCategoryFrom(cat.ID, CategoryInput{Name: "Bolts", LowStockThreshold: intp(3)})
	if !errors.Is(err, ErrThresholdRequiresConsumable) {
		t.Errorf("keeping a threshold on a non-consumable: got %v", err)
	}
}

// TestThresholdCountsDistinctActiveAssetsInTheSubtree archives assets until
// the count crosses, then restocks, checking each event.
func TestThresholdCountsDistinctActiveAssetsInTheSubtree(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	user, err := st.CreateAdminUser("stores@hackspace.org", "Pass1234!")
	if err != nil {
		t.Fatal(err)
	}
	as := st.As(user.ID)

	tape := consumable(t, st, "Tape", nil, intp(2))
	if !flag(t, st, tape.ID) {
		t.Fatal("an empty category with a threshold of 2 should start running low")
	}
	duct := consumable(t, st, "Duct tape", &tape.ID, nil)
	other := consumable(t, st, "Glue", nil, nil)

	a1 := asset(t, st, "Roll 1", tape.ID)
	a2 := asset(t, st, "Roll 2", duct.ID)
	// In both the parent and the child: counts once.
	a3 := asset(t, st, "Roll 3", tape.ID, duct.ID)
	if flag(t, st, tape.ID) {
		t.Fatal("2 active assets should have restocked a threshold of 2")
	}
	if n, _ := st.ActiveCount(tape.ID); n != 3 {
		t.Fatalf("active count %d, want 3", n)
	}
	asset(t, st, "Unrelated", other.ID)

	before := len(events(t, st))
	if err := as.DeleteAsset(a1); err != nil {
		t.Fatal(err)
	}
	if flag(t, st, tape.ID) || len(events(t, st)) != before {
		t.Fatal("3 to 2 does not cross a threshold of 2")
	}
	if err := as.DeleteAsset(a2); err != nil {
		t.Fatal(err)
	}
	if !flag(t, st, tape.ID) {
		t.Fatal("2 to 1 crosses a threshold of 2")
	}
	got := events(t, st)
	if len(got) != before+1 {
		t.Fatalf("%d new events, want 1", len(got)-before)
	}
	e := got[len(got)-1]
	if e.Kind != models.EventRunningLow || e.Payload.Trigger != models.TriggerThreshold ||
		*e.Payload.ActiveCount != 1 || *e.Payload.Threshold != 2 ||
		e.Payload.Category.Name != "Tape" || e.Payload.Actor == nil || e.Payload.Actor.Email != "stores@hackspace.org" {
		t.Errorf("event %+v, payload %+v", e, e.Payload)
	}

	// Restocking crosses back and clears the flag.
	asset(t, st, "Roll 4", duct.ID)
	if flag(t, st, tape.ID) {
		t.Fatal("1 to 2 should restock")
	}
	got = events(t, st)
	if last := got[len(got)-1]; last.Kind != models.EventRestocked || *last.Payload.ActiveCount != 2 || last.Payload.Actor != nil {
		t.Errorf("restock event %+v, payload %+v", last, last.Payload)
	}
	_ = a3
}

// TestManualFlagStandsUntilTheNextCrossing checks that the threshold rule
// reacts to crossings, not to levels.
func TestManualFlagStandsUntilTheNextCrossing(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()

	cat := consumable(t, st, "Screws", nil, nil)
	ids := []string{asset(t, st, "Box 1", cat.ID), asset(t, st, "Box 2", cat.ID), asset(t, st, "Box 3", cat.ID)}
	// Setting a threshold above the count flags at once.
	in := CategoryInput{Name: "Screws", IsConsumable: true, LowStockThreshold: intp(5)}
	if _, err := st.UpdateCategoryFrom(cat.ID, in); err != nil {
		t.Fatal(err)
	}
	if !flag(t, st, cat.ID) {
		t.Fatal("a threshold above the count should flag at once")
	}

	// Cleared by hand while still below: archiving more stays below, so no crossing.
	if _, changed, err := st.SetRunningLow(cat.ID, false); err != nil || !changed {
		t.Fatalf("clearing: changed %v, %v", changed, err)
	}
	n := len(events(t, st))
	archive(t, st, ids[0])
	if flag(t, st, cat.ID) || len(events(t, st)) != n {
		t.Fatal("a manual restock must stand while the count stays below")
	}

	// Lowering the threshold below the count crosses upward; the flag is already clear.
	in.LowStockThreshold = intp(1)
	if _, err := st.UpdateCategoryFrom(cat.ID, in); err != nil {
		t.Fatal(err)
	}
	if flag(t, st, cat.ID) || len(events(t, st)) != n {
		t.Fatal("crossing upward with the flag already clear should do nothing")
	}

	// Flagged by hand while above; then crossing downward keeps it set, silently.
	in.IsRunningLow = true
	if _, err := st.UpdateCategoryFrom(cat.ID, in); err != nil {
		t.Fatal(err)
	}
	got := events(t, st)
	if len(got) != n+1 || got[n].Kind != models.EventRunningLow || got[n].Payload.Trigger != models.TriggerManual {
		t.Fatalf("manual flag through the category form: %+v", got[n:])
	}
	archive(t, st, ids[1])
	archive(t, st, ids[2])
	if !flag(t, st, cat.ID) || len(events(t, st)) != n+1 {
		t.Fatal("crossing downward with the flag already set should do nothing")
	}

	// Removing the threshold never changes the flag.
	in.LowStockThreshold = nil
	if _, err := st.UpdateCategoryFrom(cat.ID, in); err != nil {
		t.Fatal(err)
	}
	if !flag(t, st, cat.ID) || len(events(t, st)) != n+1 {
		t.Fatal("removing the threshold changed the flag")
	}
}

// TestMovesAndRecategorizationCrossThresholds covers the changes that move a
// count without archiving anything.
func TestMovesAndRecategorizationCrossThresholds(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()

	paint := consumable(t, st, "Paint", nil, nil)
	red := consumable(t, st, "Red", &paint.ID, nil)
	tins := []string{asset(t, st, "Tin 1", red.ID), asset(t, st, "Tin 2", red.ID), asset(t, st, "Tin 3", paint.ID)}
	if _, err := st.UpdateCategoryFrom(paint.ID, CategoryInput{Name: "Paint", IsConsumable: true, LowStockThreshold: intp(3)}); err != nil {
		t.Fatal(err)
	}
	if flag(t, st, paint.ID) {
		t.Fatal("3 is not below 3")
	}

	// Moving Red out of Paint takes two tins with it.
	if _, err := st.UpdateCategoryFrom(red.ID, CategoryInput{Name: "Red", IsConsumable: true}); err != nil {
		t.Fatal(err)
	}
	if !flag(t, st, paint.ID) {
		t.Fatal("moving a subcategory out should cross")
	}
	// Moving it back restocks.
	if _, err := st.UpdateCategoryFrom(red.ID, CategoryInput{Name: "Red", ParentID: &paint.ID, IsConsumable: true}); err != nil {
		t.Fatal(err)
	}
	if flag(t, st, paint.ID) {
		t.Fatal("moving a subcategory back should restock")
	}

	// Recategorizing a tin out of the tree crosses.
	elsewhere := consumable(t, st, "Elsewhere", nil, nil)
	if _, err := st.UpdateAsset(tins[2], "", []string{elsewhere.ID}, nil, nil); err != nil {
		t.Fatal(err)
	}
	if !flag(t, st, paint.ID) {
		t.Fatal("recategorizing an asset out should cross")
	}
	if _, err := st.UpdateAsset(tins[2], "", []string{red.ID}, nil, nil); err != nil {
		t.Fatal(err)
	}
	if flag(t, st, paint.ID) {
		t.Fatal("recategorizing it back should restock")
	}

	// Deleting the subcategory leaves its tins in Uncategorized.
	if err := st.DeleteCategory(red.ID); err != nil {
		t.Fatal(err)
	}
	if !flag(t, st, paint.ID) {
		t.Fatal("deleting a subcategory should cross")
	}
	if n, _ := st.ActiveCount(paint.ID); n != 0 {
		t.Errorf("active count %d after deleting the subcategory, want 0", n)
	}
}

func TestSetRunningLowByHand(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	user, err := st.CreateAdminUser("stores@hackspace.org", "Pass1234!")
	if err != nil {
		t.Fatal(err)
	}
	cat := consumable(t, st, "Zip ties", nil, nil)
	plain, err := st.CreateCategory("Drills", nil, "", false, false)
	if err != nil {
		t.Fatal(err)
	}

	if _, _, err := st.SetRunningLow(plain.ID, true); !errors.Is(err, ErrRunningLowRequiresConsumable) {
		t.Errorf("non-consumable: got %v", err)
	}
	if _, _, err := st.SetRunningLow("missing", true); !errors.Is(err, ErrCategoryNotFound) {
		t.Errorf("missing: got %v", err)
	}

	got, changed, err := st.As(user.ID).SetRunningLow(cat.ID, true)
	if err != nil || !changed || !got.IsRunningLow {
		t.Fatalf("flagging: %+v changed %v, %v", got, changed, err)
	}
	if _, changed, err := st.SetRunningLow(cat.ID, true); err != nil || changed {
		t.Fatalf("flagging again: changed %v, %v", changed, err)
	}
	list := events(t, st)
	if len(list) != 1 {
		t.Fatalf("%d events, want 1", len(list))
	}
	if e := list[0]; e.Kind != models.EventRunningLow || e.Payload.Trigger != models.TriggerManual ||
		e.ActorUserID == nil || *e.ActorUserID != user.ID || e.Payload.Category.ID != cat.ID {
		t.Errorf("event %+v", e)
	}
}

// TestRolledBackChangesRecordNoEvents makes an asset change fail after its
// writes, and checks the flag and events are untouched.
func TestRolledBackChangesRecordNoEvents(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	cat := consumable(t, st, "Fuses", nil, nil)
	a := asset(t, st, "Fuse 1", cat.ID)
	b, err := st.CreateAsset("Fuse 2", []string{cat.ID}, []models.AssetGeneratorKey{{GeneratorType: "code128", AssignedCode: "F2"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpdateCategoryFrom(cat.ID, CategoryInput{Name: "Fuses", IsConsumable: true, LowStockThreshold: intp(2)}); err != nil {
		t.Fatal(err)
	}
	// Moving Fuse 1 out would cross, but its new code clashes, so it rolls back.
	other := consumable(t, st, "Other", nil, nil)
	_, err = st.UpdateAsset(a, "", []string{other.ID}, []models.AssetGeneratorKey{{GeneratorType: "code128", AssignedCode: "F2"}}, nil)
	if !errors.Is(err, ErrGeneratorCodeExists) {
		t.Fatalf("got %v, want a code clash", err)
	}
	if flag(t, st, cat.ID) || len(events(t, st)) != 0 {
		t.Error("a rolled-back change flagged the category")
	}
	_ = b
}

// TestSimultaneousArchivesCannotBothMissACrossing archives two assets at once.
// Each alone moves the count 4 to 3, which does not cross a threshold of 3;
// together they reach 2, which does. Without serialization both would read 4
// before either committed, and the crossing would be missed.
func TestSimultaneousArchivesCannotBothMissACrossing(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	cat := consumable(t, st, "Gloves", nil, nil)
	var ids []string
	for _, n := range []string{"Pair 1", "Pair 2", "Pair 3", "Pair 4"} {
		ids = append(ids, asset(t, st, n, cat.ID))
	}
	if _, err := st.UpdateCategoryFrom(cat.ID, CategoryInput{Name: "Gloves", IsConsumable: true, LowStockThreshold: intp(3)}); err != nil {
		t.Fatal(err)
	}

	reached, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	st.beforeInventoryCommit = func() {
		once.Do(func() {
			close(reached)
			<-release
		})
	}
	first := make(chan error, 1)
	go func() { first <- st.DeleteAsset(ids[0]) }()
	select {
	case <-reached:
	case err := <-first:
		t.Fatalf("first archive finished without reaching the commit hook: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("first archive never reached the commit hook")
	}
	second := make(chan error, 1)
	go func() { second <- st.DeleteAsset(ids[1]) }()
	select {
	case err := <-second:
		close(release)
		<-first
		t.Fatalf("second archive finished (%v) while the first was uncommitted; it must wait", err)
	case <-time.After(300 * time.Millisecond):
	}
	close(release)
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	if err := <-second; err != nil {
		t.Fatal(err)
	}
	if !flag(t, st, cat.ID) {
		t.Fatal("4 to 2 crosses a threshold of 3")
	}
	if list := events(t, st); len(list) != 1 || *list[0].Payload.ActiveCount != 2 {
		t.Errorf("events %+v", list)
	}
}
