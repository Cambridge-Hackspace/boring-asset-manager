package integration

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/notify"
)

// TestImportIsAtomic sends files with a bad row at the very end, found when
// checking and found only when writing, and checks nothing from the good
// rows before it remains; then imports the corrected file.
func TestImportIsAtomic(t *testing.T) {
	e := newEnv(t)
	c := e.admin("ada@hackspace.org")
	c.newCategory(map[string]any{"name": "Tools"})

	rows := func(last string) string {
		var b strings.Builder
		b.WriteString("name,categories,code.code128\n")
		for i := 1; i <= 300; i++ {
			fmt.Fprintf(&b, "Tool %d,Tools,T%d\n", i, i)
		}
		b.WriteString(last + "\n")
		return b.String()
	}
	mapping := []string{"name", "categories", "code.code128"}
	count := func() int {
		list, err := e.st.ListAssets("", "")
		if err != nil {
			t.Fatal(err)
		}
		return len(list)
	}

	for name, last := range map[string]string{
		"unknown category": "Tool 301,Garden,T301",
		"duplicate code":   "Tool 301,Tools,T7",
		"missing name":     ",Tools,T301",
	} {
		r := c.send(http.MethodPost, "/assets/import", csvForm(rows(last), mapping))
		if r.status != http.StatusUnprocessableEntity || !strings.Contains(string(r.body), `"row":302`) {
			t.Errorf("%s: %s", name, r)
		}
		if n := count(); n != 0 {
			t.Fatalf("%s: %d assets left behind", name, n)
		}
	}

	var done struct{ Created int }
	c.mustJSON(http.MethodPost, "/assets/import", csvForm(rows("Tool 301,Tools,T301"), mapping), &done, http.StatusOK)
	if done.Created != 301 || count() != 301 {
		t.Errorf("created %d, %d assets", done.Created, count())
	}
}

// TestThresholdCrossingsOverHTTP moves a count across a threshold by moving
// a subcategory, importing, and archiving several assets at once, and checks
// the flag and the events at each step.
func TestThresholdCrossingsOverHTTP(t *testing.T) {
	e := newEnv(t)
	c := e.admin("ada@hackspace.org")
	paint := c.newCategory(map[string]any{"name": "Paint", "is_consumable": true, "low_stock_threshold": 3})
	red := c.newCategory(map[string]any{"name": "Red", "parent_id": paint, "is_consumable": true})
	var tins []string
	for i := 0; i < 4; i++ {
		tins = append(tins, c.newAsset(map[string]any{"name": fmt.Sprintf("Tin %d", i), "category_ids": []string{red}}))
	}
	events := func() []models.Event {
		list, err := e.st.ListEvents(1000)
		if err != nil {
			t.Fatal(err)
		}
		return list
	}
	expect := func(step string, low bool, count, nEvents int) {
		t.Helper()
		got := c.getCategory(paint)
		if got.Category.IsRunningLow != low || got.ActiveCount != count {
			t.Fatalf("%s: running low %v with %d active, want %v with %d", step, got.Category.IsRunningLow, got.ActiveCount, low, count)
		}
		if n := len(events()); n != nEvents {
			t.Fatalf("%s: %d events, want %d", step, n, nEvents)
		}
	}
	// Created empty below 3: flagged, then restocked by the third tin.
	expect("start", false, 4, 2)

	c.mustJSON(http.MethodPut, "/categories/"+red, map[string]any{"name": "Red", "is_consumable": true}, nil, http.StatusOK)
	expect("moved out", true, 0, 3)
	c.mustJSON(http.MethodPut, "/categories/"+red, map[string]any{"name": "Red", "parent_id": paint, "is_consumable": true}, nil, http.StatusOK)
	expect("moved back", false, 4, 4)

	c.mustJSON(http.MethodPost, "/assets/import", csvForm("name,categories\nTin 4,Paint > Red\nTin 5,Paint\n", []string{"name", "categories"}), nil, http.StatusOK)
	expect("imported", false, 6, 4)

	// Four archives at once take 6 to 2, crossing 3 once whatever the order.
	var wg sync.WaitGroup
	start := make(chan struct{})
	for _, id := range tins {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			<-start
			if r := c.send(http.MethodDelete, "/assets/"+id, nil); r.status != http.StatusOK {
				t.Errorf("archiving %s: %s", id, r)
			}
		}(id)
	}
	close(start)
	wg.Wait()
	expect("archived four at once", true, 2, 5)
	latest := events()[0]
	if latest.Kind != models.EventRunningLow || latest.Payload.Trigger != models.TriggerThreshold || *latest.Payload.ActiveCount >= 3 {
		t.Errorf("latest event %+v", latest.Payload)
	}
}

// recorder is a notifier driver that counts what each channel receives.
type recorder struct {
	mu   sync.Mutex
	got  map[string]int // "event/channel" -> deliveries
	fail map[string]bool
}

var record = &recorder{got: map[string]int{}, fail: map[string]bool{}}

func (*recorder) Info() notify.DriverInfo {
	return notify.DriverInfo{Driver: "integration-recorder", Label: "Recorder", Options: []notify.OptionInfo{{Name: "name", Label: "Name", Required: true}}}
}

func (*recorder) Validate(map[string]string) error { return nil }

func (r *recorder) Send(ctx context.Context, options map[string]string, n notify.Notification) error {
	// Slow enough that workers overlap.
	time.Sleep(2 * time.Millisecond)
	r.mu.Lock()
	defer r.mu.Unlock()
	key := n.Event.ID + "/" + options["name"]
	r.got[key]++
	// The first attempt to one channel fails, to exercise retries too.
	if options["name"] == "flaky" && !r.fail[n.Event.ID] {
		r.fail[n.Event.ID] = true
		// Asking for the shortest wait the worker allows (a second).
		return &notify.RetryAfterError{After: time.Millisecond, Err: fmt.Errorf("flaky")}
	}
	return nil
}

func init() {
	if err := notify.Register(record); err != nil {
		panic(err)
	}
}

// TestDeliveriesAreExactlyOnce queues many events for several channels and
// lets three workers, as three servers sharing the database would run,
// deliver them at once. Each event reaches each subscribed channel once,
// apart from the deliberate retries.
func TestDeliveriesAreExactlyOnce(t *testing.T) {
	e := newEnv(t)
	c := e.admin("ada@hackspace.org")
	channel := func(name string, enabled bool, events ...string) {
		c.mustJSON(http.MethodPost, "/notifiers", map[string]any{
			"name": name, "driver": "integration-recorder", "options": map[string]string{"name": name}, "enabled": enabled, "events": events,
		}, nil, http.StatusCreated)
	}
	channel("all", true, models.EventRunningLow, models.EventRestocked)
	channel("restocks", true, models.EventRestocked)
	channel("flaky", true, models.EventRunningLow)
	channel("off", false, models.EventRunningLow, models.EventRestocked)

	tape := c.newCategory(map[string]any{"name": "Tape", "is_consumable": true})
	for i := 0; i < 20; i++ {
		c.mustJSON(http.MethodPut, "/categories/"+tape+"/running-low", map[string]bool{"running_low": i%2 == 0}, nil, http.StatusOK)
	}

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := &notify.Worker{Store: e.st, Poll: 5 * time.Millisecond}
			w.Run(ctx)
		}()
	}
	deadline := time.Now().Add(30 * time.Second)
	for {
		list, err := e.st.ListDeliveries(200)
		if err != nil {
			t.Fatal(err)
		}
		pending := 0
		for _, d := range list {
			if d.Status != models.DeliverySent {
				pending++
			}
		}
		if pending == 0 && len(list) == 40 {
			break
		}
		if time.Now().After(deadline) {
			cancel()
			wg.Wait()
			t.Fatalf("%d deliveries, %d not sent, after 30 seconds", len(list), pending)
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	wg.Wait()

	events, err := e.st.ListEvents(100)
	if err != nil {
		t.Fatal(err)
	}
	record.mu.Lock()
	defer record.mu.Unlock()
	want := 0
	for _, ev := range events {
		for _, ch := range []string{"all", "restocks", "flaky", "off"} {
			expected := 0
			switch {
			case ch == "all":
				expected = 1
			case ch == "restocks" && ev.Kind == models.EventRestocked:
				expected = 1
			case ch == "flaky" && ev.Kind == models.EventRunningLow:
				expected = 2 // one failure, one success
			}
			want += expected
			if got := record.got[ev.ID+"/"+ch]; got != expected {
				t.Errorf("event %s (%s) reached %s %d times, want %d", ev.ID, ev.Kind, ch, got, expected)
			}
		}
	}
	total := 0
	for _, n := range record.got {
		total += n
	}
	if len(events) != 20 || total != want {
		t.Errorf("%d events, %d sends, want 20 and %d", len(events), total, want)
	}
}

// TestQuantitiesOverHTTP has several people use and restock an asset counted
// by quantity at once, through the API, on whichever database the suite
// runs against: no use is lost, nothing goes below zero, and the crossing is
// recorded once.
func TestQuantitiesOverHTTP(t *testing.T) {
	e := newEnv(t)
	c := e.admin("ada@hackspace.org")
	id := c.newAsset(map[string]any{"name": "Nitrile gloves", "stock": map[string]any{"quantity": 30, "unit": "pairs", "low_stock_threshold": 10}})
	path := "/assets/" + id + "/quantity"

	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		statuses = map[int]int{}
	)
	start := make(chan struct{})
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			r := c.send(http.MethodPost, path, map[string]int{"adjust": -1})
			mu.Lock()
			statuses[r.status]++
			mu.Unlock()
		}()
	}
	close(start)
	wg.Wait()
	if statuses[http.StatusOK] != 30 || statuses[http.StatusConflict] != 10 {
		t.Fatalf("statuses %v, want 30 used and 10 refused", statuses)
	}

	var out struct{ Asset models.AssetWithDetails }
	c.mustJSON(http.MethodPost, path, map[string]int{"adjust": 12}, &out, http.StatusOK)
	if s := out.Asset.Asset.Stock; s == nil || s.Quantity != 12 || s.IsRunningLow {
		t.Fatalf("after restocking: %+v", s)
	}
	var history struct{ Events []models.Event }
	c.mustJSON(http.MethodGet, "/assets/"+id+"/history", nil, &history, http.StatusOK)
	counts := map[string]int{}
	for _, ev := range history.Events {
		counts[ev.Kind]++
	}
	if counts[models.EventQuantityChanged] != 32 || counts[models.EventAssetRunningLow] != 1 || counts[models.EventAssetRestocked] != 1 {
		t.Errorf("history %v", counts)
	}
	if latest := history.Events[0]; latest.Kind != models.EventAssetRestocked || *latest.Payload.Quantity != 12 || latest.Payload.Unit != "pairs" {
		t.Errorf("latest %+v", latest)
	}
}
