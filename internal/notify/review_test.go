package notify

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// TestReviewsDueAreSentOnce: workers polling at once, as servers sharing a
// database do, record an asset's review falling due once, and its channel
// is sent it once, saying by when.
func TestReviewsDueAreSentOnce(t *testing.T) {
	f := newFixture(t)
	f.channel(t, "reviews", true, models.EventAssetReviewDue)
	cat, err := f.st.CreateCategoryFrom(store.CategoryInput{Name: "Tools"})
	if err != nil {
		t.Fatal(err)
	}
	on, months := true, 12
	if _, err := f.st.SetCategoryReview(cat.ID, models.CategoryReview{Enabled: &on, Months: &months}); err != nil {
		t.Fatal(err)
	}
	a, err := f.st.Confirming().CreateAsset("Saw", []string{cat.ID}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	due := f.clock.Now().UTC().AddDate(0, 0, 5).Truncate(time.Second)
	if _, err := f.st.Exec("UPDATE assets SET review_due = ? WHERE id = ?", due, a.Asset.ID); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		w := f.worker()
		w.Poll = 10 * time.Millisecond
		wg.Add(1)
		go func() {
			defer wg.Done()
			w.Run(ctx)
		}()
	}
	for deadline := time.Now().Add(30 * time.Second); f.driver.count() == 0 && time.Now().Before(deadline); {
		time.Sleep(10 * time.Millisecond)
		f.clock.catchUp()
	}
	time.Sleep(200 * time.Millisecond)
	cancel()
	wg.Wait()
	if n := f.driver.count(); n != 1 {
		t.Fatalf("sent %d times", n)
	}
	n := f.driver.sent[0]
	if n.Event.Kind != models.EventAssetReviewDue || n.Event.Payload.DueAt == nil || !n.Event.Payload.DueAt.Equal(due) ||
		n.Event.Payload.Asset == nil || n.Event.Payload.Asset.Name != "Saw" {
		t.Errorf("sent %+v", n.Event)
	}
}

// TestReviewDueNoticeSays: Discord and email say whose review is due, and
// by when.
func TestReviewDueNoticeSays(t *testing.T) {
	due := time.Date(2027, 3, 31, 0, 0, 0, 0, time.UTC)
	n := Notification{BaseURL: "https://bam.example", Event: models.Event{
		Kind:    models.EventAssetReviewDue,
		Payload: models.EventPayload{Asset: &models.EventAsset{ID: "a1", Name: "Band_saw"}, Trigger: models.TriggerSchedule, DueAt: &due},
	}}
	e := discordPayload(n).Embeds[0]
	if e.Title != `Review due: Band\_saw` || e.Description != "It is due to be checked again by 31 March 2027." || e.URL != "https://bam.example/assets/a1" {
		t.Errorf("Discord: %+v", e)
	}
	m, err := emailMessage(n)
	if err != nil || !strings.Contains(m.Subject, "Review due: Band_saw") || !strings.Contains(m.Text, "31 March 2027") {
		t.Errorf("email: %+v %v", m, err)
	}
}
