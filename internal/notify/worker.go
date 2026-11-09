package notify

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// Worker sends queued deliveries. Several workers, in one server or in
// several sharing a database, can run at once: each delivery is leased to
// one of them at a time.
type Worker struct {
	Store   *store.Store
	BaseURL string
	// Mailer sends email channels' messages; nil when email is not set up.
	Mailer Mailer
	// Updates is the check for a newer release; nil when it is off.
	Updates *UpdateCheck
	// WarrantyDays is how many days before an asset's warranty ends its
	// warranty.expiring event is recorded: 0 on the day it ends.
	WarrantyDays int
	// ContractDays is how many days before a contract ends, or before its
	// notice deadline, its contract.expiring event is recorded: 0 on the
	// day.
	ContractDays int
	// Poll is how often to look for due deliveries (default 5s).
	Poll time.Duration
	// Lease is how long a claimed delivery is reserved (default 2m). It
	// outlasts SendTimeout, so a lease only lapses if its worker died.
	Lease time.Duration
	// SendTimeout bounds one attempt (default 30s).
	SendTimeout time.Duration
	// GiveUpAfter is how long after an event to stop retrying (default 24h).
	GiveUpAfter time.Duration
	// Policy says when a channel that keeps failing is turned off (#196);
	// its zero value never turns one off.
	Policy store.ChannelPolicy
	// KeepFor is how long finished deliveries are kept in the log; zero
	// keeps them.
	KeepFor   time.Duration
	lastPrune time.Time
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
		// Loans past due, things left past their date, reviews falling due, and warranties and
		// contracts about to end become events first, so their notices go out now.
		if _, err := w.Store.RecordOverdueLoans(w.Now()); err != nil {
			log.Printf("notifications: %v", err)
		}
		if _, err := w.Store.RecordOverdueFilings(w.Now()); err != nil {
			log.Printf("notifications: %v", err)
		}
		if _, err := w.Store.RecordDueReviews(w.Now()); err != nil {
			log.Printf("notifications: %v", err)
		}
		if _, err := w.Store.RecordExpiringWarranties(w.Now(), w.WarrantyDays); err != nil {
			log.Printf("notifications: %v", err)
		}
		if _, err := w.Store.RecordContractReminders(w.Now(), w.ContractDays); err != nil {
			log.Printf("notifications: %v", err)
		}
		if _, err := w.SendLoanReminders(ctx); err != nil {
			log.Printf("loan reminders: %v", err)
		}
		if _, err := w.SendMentions(ctx); err != nil {
			log.Printf("mentions: %v", err)
		}
		if tag, err := w.CheckForUpdates(ctx); err != nil {
			log.Printf("update check: %v; it is tried again in a day", err)
		} else if tag != "" {
			log.Printf("update check: %s is available", tag)
		}
		w.prune()
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
			continue
		}
		// A delivery of a channel already off was not attempted, so it says
		// nothing of how the channel is faring.
		if !c.Channel.Enabled {
			continue
		}
		off, err := w.Store.RecordChannelResult(c.Channel.ID, outcome.Status == models.DeliverySent, w.Now(), w.Policy)
		if err != nil {
			return len(claims), err
		}
		if off {
			log.Printf("notifications: turned off %q, which keeps failing", c.Channel.Name)
		}
	}
	return len(claims), nil
}

// pruneEvery is how often the log is pruned.
const pruneEvery = time.Hour

// prune removes finished deliveries older than KeepFor, once an hour.
func (w *Worker) prune() {
	now := w.Now()
	if w.KeepFor <= 0 || now.Sub(w.lastPrune) < pruneEvery {
		return
	}
	w.lastPrune = now
	if _, err := w.Store.PruneDeliveries(now.Add(-w.KeepFor)); err != nil {
		log.Printf("notifications: %v", err)
	}
}

// responseLimit is how much of a receiver's answer the log keeps.
const responseLimit = 1024

// redact takes a channel's secrets out of what its receiver answered, as
// receivers sometimes echo what they were sent, and cuts it to
// responseLimit bytes.
func redact(body string, options map[string]string, info DriverInfo) string {
	var secrets []string
	for _, o := range info.Options {
		v := options[o.Name]
		if !o.Secret || strings.TrimSpace(v) == "" {
			continue
		}
		secrets = append(secrets, v)
		// A secret of several lines, such as extra headers, is secret line
		// by line, and so is each header's value.
		for _, line := range strings.Split(v, "\n") {
			line = strings.TrimSpace(line)
			secrets = append(secrets, line)
			if _, value, ok := strings.Cut(line, ":"); ok {
				secrets = append(secrets, strings.TrimSpace(value))
			}
		}
	}
	for _, s := range secrets {
		if len(s) >= 4 {
			body = strings.ReplaceAll(body, s, "[secret]")
		}
	}
	if len(body) > responseLimit {
		body = body[:responseLimit]
		for !utf8.ValidString(body) {
			body = body[:len(body)-1]
		}
	}
	return body
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
	var status int
	var body string
	sendCtx, cancel := context.WithTimeout(ctx, w.SendTimeout)
	err := d.Send(sendCtx, c.Channel.Options, Notification{Event: c.Event, BaseURL: w.BaseURL, DeliveryID: c.Delivery.ID, Mailer: w.Mailer,
		Recipients: func(roles []string) ([]string, error) { return w.Store.EmailRecipients(roles, c.Event, false) },
		Report:     func(s int, b string) { status, body = s, redact(b, c.Channel.Options, d.Info()) }})
	cancel()
	o := w.outcome(c, err)
	o.ResponseStatus, o.ResponseBody = status, body
	return o
}

// outcome decides what becomes of a delivery after an attempt.
func (w *Worker) outcome(c store.Claim, err error) store.Outcome {
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
// An email channel's reaches everyone holding its roles, wherever narrowed.
func SendTest(ctx context.Context, c models.NotificationChannel, baseURL string, mailer Mailer, st *store.Store) error {
	d, ok := Lookup(c.Driver)
	if !ok {
		return fmt.Errorf("unknown driver %q", c.Driver)
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	return d.Send(ctx, c.Options, Notification{
		Event:      models.Event{Kind: "test", CreatedAt: time.Now().UTC(), Payload: models.EventPayload{Trigger: models.TriggerManual}},
		BaseURL:    baseURL,
		Test:       true,
		DeliveryID: "test",
		Mailer:     mailer,
		Recipients: func(roles []string) ([]string, error) {
			return st.EmailRecipients(roles, models.Event{Kind: "test"}, true)
		},
	})
}
