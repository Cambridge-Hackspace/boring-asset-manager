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

// TestWarrantyNoticesAreSentOnce: workers polling at once, as servers
// sharing a database do, record a warranty about to end once, and its
// channel is sent it once, saying when it ends.
func TestWarrantyNoticesAreSentOnce(t *testing.T) {
	f := newFixture(t)
	f.channel(t, "warranties", true, models.EventWarrantyExpiring)
	ends := f.clock.Now().AddDate(0, 0, 10).Format(store.DateLayout)
	if _, err := f.st.CreateAssetWithPurchase("Drill", nil, nil, nil, nil, "", &store.PurchaseInput{WarrantyEnd: ends}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		w := f.worker()
		w.WarrantyDays, w.Poll = 30, 10*time.Millisecond
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
	// Long enough for several more polls each.
	time.Sleep(200 * time.Millisecond)
	cancel()
	wg.Wait()
	if n := f.driver.count(); n != 1 {
		t.Fatalf("sent %d times", n)
	}
	n := f.driver.sent[0]
	if n.Event.Kind != models.EventWarrantyExpiring || n.Event.Payload.DueAt == nil || n.Event.Payload.DueAt.Format(store.DateLayout) != ends ||
		n.Event.Payload.Asset == nil || n.Event.Payload.Asset.Name != "Drill" {
		t.Errorf("sent %+v", n.Event)
	}
}

// TestWarrantyNoticeSays: Discord and email say whose warranty ends, and
// when.
func TestWarrantyNoticeSays(t *testing.T) {
	ends := time.Date(2027, 3, 31, 0, 0, 0, 0, time.UTC)
	n := Notification{BaseURL: "https://bam.example", Event: models.Event{
		Kind:    models.EventWarrantyExpiring,
		Payload: models.EventPayload{Asset: &models.EventAsset{ID: "a1", Name: "Drill_press"}, Trigger: models.TriggerSchedule, DueAt: &ends},
	}}
	e := discordPayload(n).Embeds[0]
	if e.Title != `Warranty ending: Drill\_press` || e.Description != "Its warranty ends on 31 March 2027." || e.Color != discordWarning || e.URL != "https://bam.example/assets/a1" {
		t.Errorf("Discord: %+v", e)
	}
	m, err := emailMessage(n)
	if err != nil || !strings.Contains(m.Subject, "Warranty ending: Drill_press") || !strings.Contains(m.Text, "31 March 2027") {
		t.Errorf("email: %+v %v", m, err)
	}
	known := false
	for _, k := range EventKinds {
		known = known || k.Kind == models.EventWarrantyExpiring
	}
	if !known {
		t.Error("channels cannot subscribe to it")
	}
}
