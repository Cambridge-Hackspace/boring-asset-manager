package notify

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// reportingDriver answers like a webhook's receiver: a status and a body,
// which echoes the channel's secrets.
type reportingDriver struct {
	status int
	err    error
}

func (*reportingDriver) Info() DriverInfo {
	return DriverInfo{Driver: "reporting", Options: []OptionInfo{{Name: "url"}, {Name: "key", Secret: true}, {Name: "headers", Secret: true, Multiline: true}}}
}

func (*reportingDriver) Validate(map[string]string) error { return nil }

func (d *reportingDriver) Send(_ context.Context, options map[string]string, n Notification) error {
	n.Report(d.status, "you sent key="+options["key"]+" and auth "+strings.Repeat("x", 2000)+" Bearer t0ken-123 ✓✓✓")
	return d.err
}

func TestTheLogKeepsWhatTheReceiverAnsweredWithoutSecrets(t *testing.T) {
	f := newFixture(t)
	d := &reportingDriver{status: 503, err: ErrTemporary}
	w := f.worker()
	w.Lookup = func(string) (Driver, bool) { return d, true }
	c, err := f.st.CreateChannel(models.NotificationChannel{Name: "Hook", Driver: "reporting", Enabled: true, Events: []string{models.EventRunningLow},
		Options: map[string]string{"url": "https://hooks.example", "key": "s3cret-key", "headers": "Authorization: Bearer t0ken-123\nX-Team: makers"}})
	if err != nil {
		t.Fatal(err)
	}
	f.flag(t, true)
	f.runOnce(t, w)

	list, err := f.st.ListChannelDeliveries(c.ID, 10)
	if err != nil || len(list) != 1 {
		t.Fatalf("deliveries %+v, %v", list, err)
	}
	got := list[0]
	if got.ResponseStatus == nil || *got.ResponseStatus != 503 || got.Status != models.DeliveryPending || got.Attempts != 1 {
		t.Errorf("delivery %+v", got.Delivery)
	}
	if strings.Contains(got.ResponseBody, "s3cret-key") || strings.Contains(got.ResponseBody, "t0ken-123") || !strings.HasPrefix(got.ResponseBody, "you sent key=[secret]") {
		t.Errorf("secrets kept: %.80s", got.ResponseBody)
	}
	if len(got.ResponseBody) > 1024 {
		t.Errorf("kept %d bytes", len(got.ResponseBody))
	}
	if other, _ := f.st.ListChannelDeliveries("someone-else", 10); len(other) != 0 {
		t.Errorf("another channel's: %+v", other)
	}
}

func TestAChannelThatKeepsFailingIsTurnedOffOnce(t *testing.T) {
	f := newFixture(t)
	w := f.worker()
	w.Policy = store.ChannelPolicy{Failures: 3}
	failing := f.channel(t, "Failing", true, models.EventRunningLow, models.EventRestocked)
	watcher := f.channel(t, "Watcher", true, models.EventNotifierDisabled)
	broken := errors.New("HTTP 404: not found")

	// A success between failures starts the count again.
	f.driver.fail = []error{broken, broken, nil, broken, broken, broken}
	for i, low := range []bool{true, false, true, false, true, false} {
		f.flag(t, low)
		f.runOnce(t, w)
		c, err := f.st.GetChannel(failing.ID)
		if err != nil {
			t.Fatal(err)
		}
		if want := []int{1, 2, 0, 1, 2, 3}[i]; c.Failures != want {
			t.Errorf("after attempt %d: %d failures, want %d", i+1, c.Failures, want)
		}
		if i < 5 && !c.Enabled {
			t.Fatalf("turned off after attempt %d", i+1)
		}
	}
	c, _ := f.st.GetChannel(failing.ID)
	if c.Enabled || c.DisabledReason != "Turned off after 3 failed attempts in a row." || c.FailingSince == nil {
		t.Errorf("after the third failure in a row: %+v", c)
	}
	events, err := f.st.ListEvents(20)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, e := range events {
		if e.Kind == models.EventNotifierDisabled {
			n++
			if e.Payload.Channel == nil || e.Payload.Channel.ID != failing.ID || e.Payload.Note != c.DisabledReason {
				t.Errorf("event %+v", e.Payload)
			}
		}
	}
	if n != 1 {
		t.Errorf("%d notifier.disabled events", n)
	}
	// The other channels hear of it.
	f.clock.catchUp()
	sent := f.driver.count()
	f.runOnce(t, w)
	if f.driver.count() != sent+1 || f.driver.to[len(f.driver.to)-1] != watcher.Name {
		t.Errorf("the watcher was not told: %v", f.driver.to)
	}

	// Turned on again, it starts its count again.
	c.Enabled = true
	c, err = f.st.UpdateChannel(*c)
	if err != nil {
		t.Fatal(err)
	}
	if c.Failures != 0 || c.FailingSince != nil || c.DisabledReason != "" {
		t.Errorf("turned on again: %+v", c)
	}
}

func TestAChannelFailingForADayIsTurnedOff(t *testing.T) {
	f := newFixture(t)
	w := f.worker()
	w.Policy = store.ChannelPolicy{FailingFor: 24 * time.Hour}
	failing := f.channel(t, "Failing", true, models.EventRunningLow, models.EventRestocked)
	broken := errors.New("HTTP 404: not found")
	f.driver.fail = []error{broken, broken}

	f.flag(t, true)
	f.runOnce(t, w)
	f.clock.advance(23 * time.Hour)
	if c, _ := f.st.GetChannel(failing.ID); !c.Enabled {
		t.Fatal("off within the day")
	}
	f.clock.advance(time.Hour)
	f.flag(t, false)
	f.runOnce(t, w)
	c, _ := f.st.GetChannel(failing.ID)
	if c.Enabled || !strings.HasPrefix(c.DisabledReason, "Turned off after failing every attempt for 24 hours, since ") {
		t.Errorf("a day of failures: %+v", c)
	}
}

func TestAFailedDeliveryIsSentAgainByHand(t *testing.T) {
	f := newFixture(t)
	w := f.worker()
	ch := f.channel(t, "Channel", true, models.EventRunningLow)
	f.driver.fail = []error{errors.New("HTTP 404: not found")}
	f.flag(t, true)
	f.runOnce(t, w)
	d := f.deliveries(t)[0]
	if d.Status != models.DeliveryFailed {
		t.Fatalf("delivery %+v", d.Delivery)
	}
	if err := f.st.RetryDelivery("nothing", f.clock.Now()); !errors.Is(err, store.ErrDeliveryNotFound) {
		t.Errorf("retrying nothing: %v", err)
	}

	// Not while the channel is off.
	ch.Enabled = false
	if _, err := f.st.UpdateChannel(*ch); err != nil {
		t.Fatal(err)
	}
	if err := f.st.RetryDelivery(d.ID, f.clock.Now()); !errors.Is(err, store.ErrChannelOff) {
		t.Errorf("retrying on a channel off: %v", err)
	}
	ch.Enabled = true
	if _, err := f.st.UpdateChannel(*ch); err != nil {
		t.Fatal(err)
	}

	if err := f.st.RetryDelivery(d.ID, f.clock.Now()); err != nil {
		t.Fatal(err)
	}
	f.runOnce(t, w)
	d = f.deliveries(t)[0]
	if d.Status != models.DeliverySent || d.Attempts != 1 || f.driver.count() != 2 {
		t.Errorf("sent again: %+v, %d sends", d.Delivery, f.driver.count())
	}
	if err := f.st.RetryDelivery(d.ID, f.clock.Now()); !errors.Is(err, store.ErrDeliveryNotFailed) {
		t.Errorf("retrying one sent: %v", err)
	}
}

func TestOldDeliveriesArePruned(t *testing.T) {
	f := newFixture(t)
	w := f.worker()
	w.KeepFor = 30 * 24 * time.Hour
	f.channel(t, "Sent", true, models.EventRunningLow)
	f.channel(t, "Pending", true, models.EventRunningLow)
	f.driver.fail = []error{nil, ErrTemporary}
	f.flag(t, true)
	f.runOnce(t, w)
	if _, err := f.st.Exec("UPDATE notification_deliveries SET finished_at = ?, created_at = ?", f.clock.Now().Add(-31*24*time.Hour), f.clock.Now().Add(-31*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	w.prune()
	list := f.deliveries(t)
	if len(list) != 1 || list[0].Status != models.DeliveryPending {
		t.Errorf("after pruning: %+v", list)
	}
	// Once an hour, not on every pass.
	if _, err := f.st.Exec("UPDATE notification_deliveries SET status = ?", models.DeliverySent); err != nil {
		t.Fatal(err)
	}
	w.prune()
	if len(f.deliveries(t)) != 1 {
		t.Error("pruned again within the hour")
	}
	f.clock.advance(time.Hour)
	w.prune()
	if len(f.deliveries(t)) != 0 {
		t.Error("not pruned an hour later")
	}
}
