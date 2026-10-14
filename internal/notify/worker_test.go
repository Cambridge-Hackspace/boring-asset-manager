package notify

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/config"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// openStore opens a migrated store on SQLite, or on the server named by
// BAM_TEST_DIALECT and BAM_TEST_DSN.
func openStore(t *testing.T) *store.Store {
	t.Helper()
	var st *store.Store
	if dialect := os.Getenv(store.TestDialectEnv); dialect != "" && dialect != "sqlite" {
		s, cleanup, err := store.OpenEphemeral(dialect, os.Getenv(store.TestDSNEnv))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(cleanup)
		st = s
	} else {
		s, err := store.Open(&config.DatabaseConfig{Type: "sqlite", DSN: filepath.Join(t.TempDir(), "test.db")})
		if err != nil {
			t.Fatal(err)
		}
		st = s
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	if err := st.SeedDefaultPermissionsAndRoles(); err != nil {
		t.Fatal(err)
	}
	return st
}

// fakeDriver records what it sends and fails as told.
type fakeDriver struct {
	mu    sync.Mutex
	sent  []Notification
	to    []string
	fail  []error // returned in turn; nil once exhausted
	delay time.Duration
}

func (*fakeDriver) Info() DriverInfo {
	return DriverInfo{Driver: "fake", Options: []OptionInfo{{Name: "to"}}}
}

func (*fakeDriver) Validate(map[string]string) error { return nil }

func (f *fakeDriver) Send(ctx context.Context, options map[string]string, n Notification) error {
	time.Sleep(f.delay)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, n)
	f.to = append(f.to, options["to"])
	if len(f.fail) > 0 {
		err := f.fail[0]
		f.fail = f.fail[1:]
		return err
	}
	return nil
}

func (f *fakeDriver) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.sent)
}

// clock is a settable time.
type clock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *clock) advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

type fixture struct {
	st     *store.Store
	driver *fakeDriver
	clock  *clock
	cat    string
}

func newFixture(t *testing.T) *fixture {
	st := openStore(t)
	cat, err := st.CreateCategory("Tape", nil, "#ff8800", true, false)
	if err != nil {
		t.Fatal(err)
	}
	return &fixture{st: st, driver: &fakeDriver{}, clock: &clock{now: time.Now().UTC().Add(time.Second)}, cat: cat.ID}
}

func (f *fixture) worker() *Worker {
	return &Worker{
		Store:   f.st,
		BaseURL: "https://bam.example",
		Now:     f.clock.Now,
		Lookup: func(name string) (Driver, bool) {
			if name == "fake" {
				return f.driver, true
			}
			return nil, false
		},
	}
}

func (f *fixture) channel(t *testing.T, name string, enabled bool, events ...string) *models.NotificationChannel {
	t.Helper()
	c, err := f.st.CreateChannel(models.NotificationChannel{Name: name, Driver: "fake", Options: map[string]string{"to": name}, Enabled: enabled, Events: events})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// flag sets the category's flag, which records an event and queues it.
func (f *fixture) flag(t *testing.T, low bool) {
	t.Helper()
	if _, _, err := f.st.SetRunningLow(f.cat, low); err != nil {
		t.Fatal(err)
	}
}

func (f *fixture) deliveries(t *testing.T) []store.DeliveryView {
	t.Helper()
	list, err := f.st.ListDeliveries(100)
	if err != nil {
		t.Fatal(err)
	}
	return list
}

func (f *fixture) runOnce(t *testing.T, w *Worker) int {
	t.Helper()
	n, err := w.RunOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestEventsFanOutToSubscribedEnabledChannels(t *testing.T) {
	f := newFixture(t)
	f.channel(t, "both", true, models.EventRunningLow, models.EventRestocked)
	f.channel(t, "low-only", true, models.EventRunningLow)
	f.channel(t, "restock-only", true, models.EventRestocked)
	f.channel(t, "off", false, models.EventRunningLow)

	f.flag(t, true)
	if list := f.deliveries(t); len(list) != 2 {
		t.Fatalf("%d deliveries queued, want 2: %+v", len(list), list)
	}
	w := f.worker()
	if n := f.runOnce(t, w); n != 2 {
		t.Fatalf("claimed %d", n)
	}
	if n := f.runOnce(t, w); n != 0 {
		t.Fatalf("claimed %d more after sending", n)
	}
	got := map[string]bool{}
	for i, to := range f.driver.to {
		got[to] = true
		n := f.driver.sent[i]
		if n.BaseURL != "https://bam.example" || n.Event.Kind != models.EventRunningLow || n.Event.Payload.Category.Name != "Tape" {
			t.Errorf("sent %+v", n)
		}
	}
	if !got["both"] || !got["low-only"] {
		t.Errorf("sent to %v", f.driver.to)
	}
	for _, d := range f.deliveries(t) {
		if d.Status != models.DeliverySent || d.Attempts != 1 || d.FinishedAt == nil || d.ChannelName == "" || d.CategoryName != "Tape" {
			t.Errorf("delivery %+v", d)
		}
	}

	f.flag(t, false)
	f.runOnce(t, w)
	if f.driver.count() != 4 {
		t.Errorf("%d sent after restocking, want 4", f.driver.count())
	}

	// Deleting a channel takes its deliveries with it.
	list, _ := f.st.ListChannels()
	for _, c := range list {
		if c.Name == "both" {
			if err := f.st.DeleteChannel(c.ID); err != nil {
				t.Fatal(err)
			}
		}
	}
	if n := len(f.deliveries(t)); n != 2 {
		t.Errorf("%d deliveries left, want 2", n)
	}
}

func TestRetriesBackOffAndGiveUp(t *testing.T) {
	f := newFixture(t)
	f.channel(t, "stores", true, models.EventRunningLow)
	w := f.worker()

	// Temporary failure, then success after the backoff.
	f.driver.fail = []error{ErrTemporary}
	f.flag(t, true)
	f.runOnce(t, w)
	d := f.deliveries(t)[0]
	if d.Status != models.DeliveryPending || d.Attempts != 1 || d.LastError == "" {
		t.Fatalf("after a temporary failure: %+v", d)
	}
	if want := f.clock.Now().Add(30 * time.Second); d.NextAttemptAt.Sub(want).Abs() > time.Millisecond {
		t.Errorf("next attempt %s, want %s", d.NextAttemptAt, want)
	}
	f.clock.advance(29 * time.Second)
	if n := f.runOnce(t, w); n != 0 {
		t.Fatal("retried before the backoff ended")
	}
	f.clock.advance(2 * time.Second)
	f.runOnce(t, w)
	if d := f.deliveries(t)[0]; d.Status != models.DeliverySent || d.Attempts != 2 {
		t.Fatalf("after the retry: %+v", d)
	}
}

func TestRetryAfterIsHonored(t *testing.T) {
	f := newFixture(t)
	f.channel(t, "stores", true, models.EventRunningLow)
	w := f.worker()
	f.driver.fail = []error{&RetryAfterError{After: 10 * time.Minute, Err: errors.New("rate limited")}}
	f.flag(t, true)
	f.runOnce(t, w)
	f.clock.advance(9 * time.Minute)
	if n := f.runOnce(t, w); n != 0 {
		t.Fatal("retried before Retry-After")
	}
	f.clock.advance(2 * time.Minute)
	f.runOnce(t, w)
	if d := f.deliveries(t)[0]; d.Status != models.DeliverySent {
		t.Fatalf("%+v", d)
	}
}

func TestPermanentFailuresAndGivingUp(t *testing.T) {
	f := newFixture(t)
	f.channel(t, "stores", true, models.EventRunningLow)
	w := f.worker()

	f.driver.fail = []error{errors.New("HTTP 404: unknown webhook")}
	f.flag(t, true)
	f.runOnce(t, w)
	if d := f.deliveries(t)[0]; d.Status != models.DeliveryFailed || d.LastError != "HTTP 404: unknown webhook" || d.FinishedAt == nil {
		t.Fatalf("permanent failure: %+v", d)
	}

	// Always temporarily failing: retried until a retry would land more than
	// 24 hours after the event, then given up.
	f.flag(t, false)
	f.flag(t, true)
	f.driver.fail = make([]error, 100)
	for i := range f.driver.fail {
		f.driver.fail[i] = ErrTemporary
	}
	attempts := 0
	for i := 0; i < 100; i++ {
		attempts += f.runOnce(t, w)
		f.clock.advance(time.Hour)
		if d := f.deliveries(t)[0]; d.Status == models.DeliveryFailed {
			if d.Attempts != attempts || d.LastError[:15] != "gave up after 2" {
				t.Errorf("given up: %+v", d)
			}
			if attempts < 20 || attempts > 30 {
				t.Errorf("gave up after %d attempts", attempts)
			}
			return
		}
	}
	t.Fatal("never gave up")
}

func TestDisabledOrUnknownDriverChannelsFail(t *testing.T) {
	f := newFixture(t)
	c := f.channel(t, "stores", true, models.EventRunningLow)
	f.flag(t, true)
	c.Enabled = false
	if _, err := f.st.UpdateChannel(*c); err != nil {
		t.Fatal(err)
	}
	f.runOnce(t, f.worker())
	if d := f.deliveries(t)[0]; d.Status != models.DeliveryFailed || d.LastError != "the channel was disabled" {
		t.Errorf("%+v", d)
	}
	if f.driver.count() != 0 {
		t.Error("sent to a disabled channel")
	}
}

// TestWorkersNeverSendADeliveryTwice runs several workers at once over many
// deliveries, then checks a lapsed lease passes to another worker and the
// first can no longer finish the delivery.
func TestWorkersNeverSendADeliveryTwice(t *testing.T) {
	f := newFixture(t)
	f.channel(t, "a", true, models.EventRunningLow, models.EventRestocked)
	f.channel(t, "b", true, models.EventRunningLow, models.EventRestocked)
	for i := 0; i < 10; i++ {
		f.flag(t, i%2 == 0)
	}
	total := len(f.deliveries(t))
	if total != 20 {
		t.Fatalf("%d deliveries", total)
	}
	f.driver.delay = 5 * time.Millisecond
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := f.worker()
			for {
				n, err := w.RunOnce(context.Background())
				if err != nil {
					t.Error(err)
					return
				}
				if n == 0 {
					return
				}
			}
		}()
	}
	wg.Wait()
	if f.driver.count() != total {
		t.Errorf("sent %d times for %d deliveries", f.driver.count(), total)
	}
	seen := map[string]bool{}
	for i, n := range f.driver.sent {
		key := n.Event.ID + "/" + f.driver.to[i]
		if seen[key] {
			t.Errorf("sent %s twice", key)
		}
		seen[key] = true
	}

	// A worker that claims and then dies: its lease lapses and another
	// worker sends the delivery; the first cannot finish it any more.
	f.flag(t, true)
	claims, err := f.st.ClaimDeliveries(f.clock.Now(), 2*time.Minute, 10)
	if err != nil || len(claims) != 2 {
		t.Fatalf("claimed %d (%v)", len(claims), err)
	}
	w := f.worker()
	if n := f.runOnce(t, w); n != 0 {
		t.Fatal("claimed deliveries under someone else's lease")
	}
	f.clock.advance(2*time.Minute + time.Second)
	if n := f.runOnce(t, w); n != 2 {
		t.Fatalf("reclaimed %d lapsed deliveries, want 2", n)
	}
	ok, err := f.st.FinishDelivery(claims[0].Delivery.ID, claims[0].Token, f.clock.Now(), store.Outcome{Status: models.DeliveryFailed, Error: "late"})
	if err != nil || ok {
		t.Errorf("finished with a lost lease: %v %v", ok, err)
	}
	for _, d := range f.deliveries(t) {
		if d.Status != models.DeliverySent {
			t.Errorf("%+v", d)
		}
	}
}
