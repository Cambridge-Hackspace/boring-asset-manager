package notify

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// Worker sends queued deliveries. Several workers, in one server or in
// several sharing a database, can run at once: each delivery is leased to
// one of them at a time.
type Worker struct {
	Store   *store.Store
	BaseURL string
	// Poll is how often to look for due deliveries (default 5s).
	Poll time.Duration
	// Lease is how long a claimed delivery is reserved (default 2m). It
	// outlasts SendTimeout, so a lease only lapses if its worker died.
	Lease time.Duration
	// SendTimeout bounds one attempt (default 30s).
	SendTimeout time.Duration
	// GiveUpAfter is how long after an event to stop retrying (default 24h).
	GiveUpAfter time.Duration
	// Now and Lookup are replaced by tests.
	Now    func() time.Time
	Lookup func(string) (Driver, bool)
}

const batchSize = 20

func (w *Worker) defaults() {
	if w.Poll == 0 {
		w.Poll = 5 * time.Second
	}
	if w.Lease == 0 {
		w.Lease = 2 * time.Minute
	}
	if w.SendTimeout == 0 {
		w.SendTimeout = 30 * time.Second
	}
	if w.GiveUpAfter == 0 {
		w.GiveUpAfter = 24 * time.Hour
	}
	if w.Now == nil {
		w.Now = time.Now
	}
	if w.Lookup == nil {
		w.Lookup = Lookup
	}
}

// Run sends deliveries until ctx ends.
func (w *Worker) Run(ctx context.Context) {
	w.defaults()
	ticker := time.NewTicker(w.Poll)
	defer ticker.Stop()
	for {
		// Loans past due, and things left past their date, become events
		// first, so their notices go out now.
		if _, err := w.Store.RecordOverdueLoans(w.Now()); err != nil {
			log.Printf("notifications: %v", err)
		}
		if _, err := w.Store.RecordOverdueFilings(w.Now()); err != nil {
			log.Printf("notifications: %v", err)
		}
		for {
			n, err := w.RunOnce(ctx)
			if err != nil {
				log.Printf("notifications: %v", err)
			}
			// A full batch may mean more are due.
			if err != nil || n < batchSize || ctx.Err() != nil {
				break
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// RunOnce claims and sends one batch of due deliveries, returning how many
// it claimed.
func (w *Worker) RunOnce(ctx context.Context) (int, error) {
	w.defaults()
	claims, err := w.Store.ClaimDeliveries(w.Now(), w.Lease, batchSize)
	if err != nil {
		return 0, err
	}
	for _, c := range claims {
		outcome := w.attempt(ctx, c)
		ok, err := w.Store.FinishDelivery(c.Delivery.ID, c.Token, w.Now(), outcome)
		if err != nil {
			return len(claims), err
		}
		if !ok {
			log.Printf("notifications: lost the lease on delivery %s", c.Delivery.ID)
		}
	}
	return len(claims), nil
}

// attempt sends one delivery and decides what becomes of it.
func (w *Worker) attempt(ctx context.Context, c store.Claim) store.Outcome {
	if !c.Channel.Enabled {
		return store.Outcome{Status: models.DeliveryFailed, Error: "the channel was disabled"}
	}
	d, ok := w.Lookup(c.Channel.Driver)
	if !ok {
		return store.Outcome{Status: models.DeliveryFailed, Error: fmt.Sprintf("unknown driver %q", c.Channel.Driver)}
	}
	sendCtx, cancel := context.WithTimeout(ctx, w.SendTimeout)
	err := d.Send(sendCtx, c.Channel.Options, Notification{Event: c.Event, BaseURL: w.BaseURL})
	cancel()
	if err == nil {
		return store.Outcome{Status: models.DeliverySent}
	}

	now := w.Now()
	var next time.Time
	var after *RetryAfterError
	switch {
	case errors.As(err, &after):
		next = now.Add(max(after.After, time.Second))
	case errors.Is(err, ErrTemporary):
		next = now.Add(backoff(c.Delivery.Attempts))
	default:
		return store.Outcome{Status: models.DeliveryFailed, Error: err.Error()}
	}
	if next.Sub(c.Delivery.CreatedAt) > w.GiveUpAfter {
		return store.Outcome{Status: models.DeliveryFailed, Error: "gave up after " + w.GiveUpAfter.String() + ": " + err.Error()}
	}
	return store.Outcome{Status: models.DeliveryPending, Error: err.Error(), NextAttemptAt: next}
}

// backoff is the wait before retrying after the given number of earlier
// attempts: 30 seconds, doubling, at most an hour.
func backoff(attempts int) time.Duration {
	d := 30 * time.Second
	for i := 0; i < attempts && d < time.Hour; i++ {
		d *= 2
	}
	return min(d, time.Hour)
}

// SendTest sends a channel's test message straight away, outside the queue.
func SendTest(ctx context.Context, c models.NotificationChannel, baseURL string) error {
	d, ok := Lookup(c.Driver)
	if !ok {
		return fmt.Errorf("unknown driver %q", c.Driver)
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	return d.Send(ctx, c.Options, Notification{
		Event:   models.Event{Kind: "test", CreatedAt: time.Now().UTC(), Payload: models.EventPayload{Trigger: models.TriggerManual}},
		BaseURL: baseURL,
		Test:    true,
	})
}
