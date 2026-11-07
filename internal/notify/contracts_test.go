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

// TestContractRemindersAreSentOnce: workers polling at once, as servers
// sharing a database do, record a contract about to end once, and its
// channel is sent it once, naming the contract.
func TestContractRemindersAreSentOnce(t *testing.T) {
	f := newFixture(t)
	f.channel(t, "contracts", true, models.EventContractExpiring, models.EventContractExpired)
	acme, err := f.st.CreateVendor(store.VendorInput{Name: "Acme"})
	if err != nil {
		t.Fatal(err)
	}
	ends := f.clock.Now().AddDate(0, 0, 10).Format(store.DateLayout)
	if _, err := f.st.CreateContract(store.ContractInput{VendorID: acme.ID, Kind: "lease", Name: "Printer lease", EndDate: ends}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		w := f.worker()
		w.ContractDays, w.Poll = 30, 10*time.Millisecond
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
	if n.Event.Kind != models.EventContractExpiring || n.Event.Payload.DueAt == nil || n.Event.Payload.DueAt.Format(store.DateLayout) != ends ||
		n.Event.Payload.Contract == nil || n.Event.Payload.Contract.Name != "Printer lease" || n.Event.Payload.Contract.VendorName != "Acme" {
		t.Errorf("sent %+v", n.Event)
	}
}

// TestContractRemindersSay: Discord and email say which contract ends, or
// must be given notice, with whom, and when, and link to it; channels can
// subscribe to both kinds; MQTT publishes them under the contract.
func TestContractRemindersSay(t *testing.T) {
	notice := time.Date(2027, 5, 31, 0, 0, 0, 0, time.UTC)
	ends := time.Date(2027, 6, 30, 0, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		kind        string
		contract    models.EventContract
		due         time.Time
		title, says string
	}{
		{models.EventContractExpiring, models.EventContract{Kind: "maintenance", EndDate: "2027-06-30"}, ends,
			"Contract ending: Workshop_service", "Its maintenance contract with Acme ends on 30 June 2027."},
		{models.EventContractExpiring, models.EventContract{Kind: "service", EndDate: "2027-06-30", AutoRenews: true, NoticeDays: 30}, notice,
			"Contract notice due: Workshop_service", "Its service contract with Acme renews automatically on 30 June 2027 unless notice is given by 31 May 2027."},
		{models.EventContractExpired, models.EventContract{Kind: "warranty", EndDate: "2027-06-30"}, ends,
			"Contract ended: Workshop_service", "Its warranty with Acme ended on 30 June 2027."},
		{models.EventContractExpired, models.EventContract{Kind: "license", EndDate: "2027-06-30", AutoRenews: true}, ends,
			"Contract renewed: Workshop_service", "Its license contract with Acme reached the end of its term on 30 June 2027, and renews automatically."},
	} {
		ec := c.contract
		ec.ID, ec.Name, ec.VendorName = "c1", "Workshop_service", "Acme"
		due := c.due
		n := Notification{BaseURL: "https://bam.example", Event: models.Event{
			Kind:    c.kind,
			Payload: models.EventPayload{Contract: &ec, Trigger: models.TriggerSchedule, DueAt: &due},
		}}
		e := discordPayload(n).Embeds[0]
		if e.Title != strings.ReplaceAll(c.title, "_", `\_`) || !strings.HasPrefix(e.Description, c.says) || e.URL != "https://bam.example/contracts/c1" {
			t.Errorf("Discord, %s: %+v", c.title, e)
		}
		m, err := emailMessage(n)
		if err != nil || !strings.Contains(m.Subject, c.title) || !strings.Contains(m.Text, c.says) {
			t.Errorf("email, %s: %+v %v", c.title, m, err)
		}
		msg, err := mqttMessageFor("bam", n)
		if err != nil || msg.Topic != "bam/"+c.kind+"/c1" || !strings.Contains(string(msg.Payload), `"url":"https://bam.example/contracts/c1"`) {
			t.Errorf("MQTT, %s: %+v %v", c.title, msg, err)
		}
	}
	known := map[string]bool{}
	for _, k := range EventKinds {
		known[k.Kind] = true
	}
	if !known[models.EventContractExpiring] || !known[models.EventContractExpired] {
		t.Error("channels cannot subscribe to them")
	}
}
