package notify

import (
	"strings"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
)

// TestOrderNoticesSay: Discord, email and MQTT say which order was placed
// or received, from whom, and what came, never a price, and link to the
// order; channels can subscribe to both.
func TestOrderNoticesSay(t *testing.T) {
	for _, c := range []struct {
		kind, title, says string
	}{
		{models.EventOrderOrdered, "Order placed: PO-0012", "Placed with Acme. 3 × M5-100 M5 bolts; 1 × Delivery."},
		{models.EventOrderReceived, "Delivery received: PO-0012", "Received from Acme. 3 × M5-100 M5 bolts; 1 × Delivery."},
	} {
		n := Notification{BaseURL: "https://bam.example", Event: models.Event{Kind: c.kind, Payload: models.EventPayload{
			Trigger:       models.TriggerManual,
			PurchaseOrder: &models.EventOrder{ID: "o1", Number: "PO-0012", VendorID: "v1", VendorName: "Acme"},
			OrderLines:    []models.EventOrderLine{{PartNumber: "M5-100", Description: "M5 bolts", Quantity: 3}, {Description: "Delivery", Quantity: 1}},
		}}}
		e := discordPayload(n).Embeds[0]
		if e.Title != c.title || !strings.HasPrefix(e.Description, c.says) || e.URL != "https://bam.example/orders/o1" {
			t.Errorf("Discord, %s: %+v", c.kind, e)
		}
		m, err := emailMessage(n)
		if err != nil || !strings.Contains(m.Subject, c.title) || !strings.Contains(m.Text, c.says) {
			t.Errorf("email, %s: %+v %v", c.kind, m, err)
		}
		msg, err := mqttMessageFor("bam", n)
		if err != nil || msg.Topic != "bam/"+c.kind+"/o1" || !strings.Contains(string(msg.Payload), `"url":"https://bam.example/orders/o1"`) {
			t.Errorf("MQTT, %s: %+v %v", c.kind, msg, err)
		}
	}
	known := map[string]bool{}
	for _, k := range EventKinds {
		known[k.Kind] = true
	}
	if !known[models.EventOrderOrdered] || !known[models.EventOrderReceived] || known[models.EventOrderEdited] {
		t.Error("channels subscribe to the wrong order events")
	}
}
