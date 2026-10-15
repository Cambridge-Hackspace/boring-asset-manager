package store

import (
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
)

// stocked creates an asset counted by quantity.
func stocked(t *testing.T, st *Store, name string, quantity int, unit string, threshold *int) string {
	t.Helper()
	a, err := st.CreateAssetWithStock(name, nil, nil, nil, &StockInput{Quantity: intp(quantity), Unit: unit, LowStockThreshold: threshold})
	if err != nil {
		t.Fatalf("creating %s: %v", name, err)
	}
	return a.Asset.ID
}

// stockOf reads an asset's stock.
func stockOf(t *testing.T, st *Store, id string) *models.AssetStock {
	t.Helper()
	a, err := st.GetAssetByID(id)
	if err != nil {
		t.Fatal(err)
	}
	return a.Asset.Stock
}

func adjust(t *testing.T, st *Store, id string, delta int) {
	t.Helper()
	if _, err := st.AdjustQuantity(id, delta, nil); err != nil {
		t.Fatalf("adjusting by %d: %v", delta, err)
	}
}

// kinds lists the kinds of events, oldest first.
func kinds(list []models.Event) string {
	var k []string
	for _, e := range list {
		k = append(k, e.Kind)
	}
	return strings.Join(k, " ")
}

func TestAssetStockValidation(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()

	for _, in := range []StockInput{
		{Quantity: intp(-1)},
		{Quantity: intp(MaxQuantity + 1)},
		{Quantity: intp(1), LowStockThreshold: intp(0)},
		{Quantity: intp(1), LowStockThreshold: intp(MaxQuantity + 1)},
		{Quantity: intp(1), Unit: strings.Repeat("g", MaxStockUnit+1)},
		{Quantity: intp(1), Unit: "g\x00"},
	} {
		if _, err := st.CreateAssetWithStock("Screws", nil, nil, nil, &in); err == nil {
			t.Errorf("accepted %+v", in)
		}
	}
	id := stocked(t, st, "Screws", MaxQuantity, " "+strings.Repeat("µ", MaxStockUnit)+" ", intp(MaxQuantity))
	if s := stockOf(t, st, id); s.Quantity != MaxQuantity || s.Unit != strings.Repeat("µ", MaxStockUnit) || *s.LowStockThreshold != MaxQuantity {
		t.Errorf("stored %+v", s)
	}

	plain := asset(t, st, "Drill")
	if stockOf(t, st, plain) != nil {
		t.Error("an asset created without stock has some")
	}
	if _, err := st.AdjustQuantity(plain, -1, nil); !errors.Is(err, ErrNotStocked) {
		t.Errorf("using an asset not counted by quantity: %v", err)
	}
	if _, _, err := st.SetAssetRunningLow(plain, true); !errors.Is(err, ErrNotStocked) {
		t.Errorf("flagging an asset not counted by quantity: %v", err)
	}
	if _, err := st.AdjustQuantity("missing", 1, nil); !errors.Is(err, ErrAssetNotFound) {
		t.Errorf("adjusting a missing asset: %v", err)
	}
	if _, err := st.AdjustQuantity(id, 1, nil); !errors.Is(err, ErrQuantityOutOfRange) {
		t.Errorf("going over the maximum: %v", err)
	}
	if _, err := st.AdjustQuantity(id, 0, intp(-1)); !errors.Is(err, ErrQuantityOutOfRange) {
		t.Errorf("setting a negative quantity: %v", err)
	}
	archive(t, st, id)
	if _, err := st.AdjustQuantity(id, -1, nil); !errors.Is(err, ErrAssetArchived) {
		t.Errorf("using an archived asset: %v", err)
	}
}

// TestUsingAndRestocking walks a box of screws down through its threshold
// and back up.
func TestUsingAndRestocking(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	id := stocked(t, st, "M3 screws", 200, "pcs", intp(50))

	adjust(t, st, id, -100)
	adjust(t, st, id, -60)
	if s := stockOf(t, st, id); s.Quantity != 40 || !s.IsRunningLow {
		t.Fatalf("after using 160 of 200: %+v", s)
	}
	_, err := st.AdjustQuantity(id, -41, nil)
	if !errors.Is(err, ErrNotEnoughStock) || !strings.Contains(err.Error(), "40 left") {
		t.Errorf("using more than there is: %v", err)
	}
	adjust(t, st, id, -40)
	adjust(t, st, id, 70)
	if s := stockOf(t, st, id); s.Quantity != 70 || s.IsRunningLow {
		t.Fatalf("after restocking: %+v", s)
	}
	if _, err := st.AdjustQuantity(id, 0, intp(5)); err != nil {
		t.Fatal(err)
	}

	list := events(t, st)
	want := "asset.quantity_changed asset.quantity_changed asset.quantity_changed asset.running_low asset.quantity_changed asset.quantity_changed asset.restocked asset.quantity_changed asset.running_low"
	if got := kinds(list); got != want {
		t.Fatalf("events\n got %s\nwant %s", got, want)
	}
	first := list[0].Payload
	if first.Change != nil || *first.Quantity != 200 || first.Asset.Name != "M3 screws" || first.Unit != "pcs" || *list[0].AssetID != id {
		t.Errorf("starting to count: %+v", first)
	}
	if p := list[2].Payload; *p.Change != -60 || *p.Quantity != 40 {
		t.Errorf("using 60: %+v", p)
	}
	low := list[3]
	if low.CategoryID != nil || low.Payload.Trigger != models.TriggerThreshold || *low.Payload.Quantity != 40 || *low.Payload.Threshold != 50 || low.Payload.Unit != "pcs" {
		t.Errorf("running low: %+v", low)
	}
	if p := list[6].Payload; *p.Quantity != 70 || *p.Threshold != 50 {
		t.Errorf("restocked: %+v", p)
	}

	history, err := st.ListAssetEvents(id, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 3 || history[0].Kind != models.EventAssetRunningLow || *history[1].Payload.Quantity != 5 {
		t.Errorf("history, newest first: %s", kinds(history))
	}
	if other, _ := st.ListAssetEvents(asset(t, st, "Drill"), 10); len(other) != 0 {
		t.Errorf("another asset's history: %s", kinds(other))
	}
}

// TestAssetFlagByHandStandsUntilTheNextCrossing mirrors the category rule.
func TestAssetFlagByHandStandsUntilTheNextCrossing(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	id := stocked(t, st, "Solder", 10, "m", intp(5))

	a, changed, err := st.SetAssetRunningLow(id, true)
	if err != nil || !changed || !a.Asset.Stock.IsRunningLow {
		t.Fatalf("flagging by hand: %v %v %+v", err, changed, a)
	}
	if _, changed, _ := st.SetAssetRunningLow(id, true); changed {
		t.Error("flagging again changed something")
	}
	adjust(t, st, id, -1)
	if !stockOf(t, st, id).IsRunningLow {
		t.Error("a change that crosses nothing cleared the flag set by hand")
	}
	adjust(t, st, id, -6) // 9 to 3 crosses 5
	adjust(t, st, id, 10) // 3 to 13 crosses back
	if stockOf(t, st, id).IsRunningLow {
		t.Error("crossing back up left the flag")
	}

	list := events(t, st)
	var manual *models.Event
	for i := range list {
		if list[i].Kind == models.EventAssetRunningLow && list[i].Payload.Trigger == models.TriggerManual {
			manual = &list[i]
		}
	}
	if manual == nil || *manual.Payload.Quantity != 10 {
		t.Errorf("the manual flag's event: %+v", manual)
	}
}

func TestEditingAnAssetsStock(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	id := asset(t, st, "PLA, blue")

	// Starting to count with no quantity starts at 0, and a threshold above
	// it flags at once.
	a, err := st.UpdateAssetWithStock(id, "", nil, nil, nil, &StockUpdate{Stock: &StockInput{Unit: "g", LowStockThreshold: intp(200)}})
	if err != nil {
		t.Fatal(err)
	}
	if s := a.Asset.Stock; s == nil || s.Quantity != 0 || !s.IsRunningLow {
		t.Fatalf("started counting: %+v", s)
	}

	// An edit without a quantity keeps the one there is, even if it moved
	// since the edit began.
	adjust(t, st, id, 1000)
	a, err = st.UpdateAssetWithStock(id, "PLA, blue (1.75 mm)", nil, nil, nil, &StockUpdate{Stock: &StockInput{Unit: "g", LowStockThreshold: intp(250)}})
	if err != nil {
		t.Fatal(err)
	}
	if s := a.Asset.Stock; s.Quantity != 1000 || *s.LowStockThreshold != 250 || s.IsRunningLow {
		t.Errorf("after the edit: %+v", s)
	}
	// An update that does not mention stock leaves it.
	a, err = st.UpdateAsset(id, "PLA", nil, nil, nil)
	if err != nil || a.Asset.Stock == nil || a.Asset.Stock.Quantity != 1000 {
		t.Fatalf("a plain update: %v %+v", err, a)
	}
	n := len(events(t, st))
	a, err = st.UpdateAssetWithStock(id, "", nil, nil, nil, &StockUpdate{Stock: &StockInput{Quantity: intp(1000), Unit: "kg"}})
	if err != nil || len(events(t, st)) != n {
		t.Errorf("an edit leaving the quantity recorded a change: %v", err)
	}

	// Stopping counting clears everything, flag included.
	if _, _, err := st.SetAssetRunningLow(id, true); err != nil {
		t.Fatal(err)
	}
	a, err = st.UpdateAssetWithStock(id, "", nil, nil, nil, &StockUpdate{})
	if err != nil || a.Asset.Stock != nil {
		t.Fatalf("stopped counting: %v %+v", err, a.Asset.Stock)
	}
	a, err = st.UpdateAssetWithStock(id, "", nil, nil, nil, &StockUpdate{Stock: &StockInput{Quantity: intp(3)}})
	if err != nil || a.Asset.Stock.Unit != "" || a.Asset.Stock.LowStockThreshold != nil || a.Asset.Stock.IsRunningLow {
		t.Errorf("counting again kept old settings: %v %+v", err, a.Asset.Stock)
	}
}

// TestArchivedAssetsAreNotChecked: archiving a stocked asset raises no
// events, and its threshold no longer applies.
func TestArchivedAssetsAreNotChecked(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	id := stocked(t, st, "Batteries", 3, "", intp(2))
	n := len(events(t, st))
	archive(t, st, id)
	if list := events(t, st); len(list) != n {
		t.Errorf("archiving recorded %s", kinds(list[n:]))
	}
}

// TestAssetEventsReachSubscribedChannels: running low and restocked go to
// the channels subscribed to them; quantity changes go nowhere, even to a
// channel that somehow names them.
func TestAssetEventsReachSubscribedChannels(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	if _, err := st.CreateChannel(models.NotificationChannel{
		Name: "Stores", Driver: "discord", Options: map[string]string{}, Enabled: true,
		Events: []string{models.EventAssetRunningLow, models.EventQuantityChanged},
	}); err != nil {
		t.Fatal(err)
	}
	id := stocked(t, st, "Zip ties", 10, "", intp(5))
	adjust(t, st, id, -8)
	adjust(t, st, id, 8)

	list, err := st.ListDeliveries(100)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].EventKind != models.EventAssetRunningLow || list[0].AssetName != "Zip ties" || list[0].CategoryName != "" {
		t.Errorf("deliveries %+v", list)
	}
}

// TestSimultaneousUsesAllCount: people using stock at once each take their
// share, nobody takes what is not there, and the crossing is recorded once.
func TestSimultaneousUsesAllCount(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	id := stocked(t, st, "Nitrile gloves", 20, "pairs", intp(5))

	var wg sync.WaitGroup
	errs := make(chan error, 25)
	for i := 0; i < 25; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := st.AdjustQuantity(id, -1, nil)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	short := 0
	for err := range errs {
		switch {
		case errors.Is(err, ErrNotEnoughStock):
			short++
		case err != nil:
			t.Fatal(err)
		}
	}
	if s := stockOf(t, st, id); s.Quantity != 0 || short != 5 {
		t.Errorf("quantity %d, %d refused; want 0 and 5", s.Quantity, short)
	}
	if got := strings.Count(kinds(events(t, st)), models.EventAssetRunningLow); got != 1 {
		t.Errorf("%d running-low events, want 1", got)
	}
}
