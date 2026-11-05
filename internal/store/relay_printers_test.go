package store

import (
	"testing"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers"
)

func TestRelayPrinters(t *testing.T) {
	st := setupTestStore(t)
	add := func(name, relay string) *models.Printer {
		p, err := st.CreatePrinter(models.Printer{Name: name, Driver: "tspl", Model: "Rollo X1038", Media: "3x2",
			Connection: printers.Connection{Type: printers.ConnTCP, Address: "192.168.1.40", Relay: relay}})
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	desk, shelf := add("Desk", "front-desk"), add("Shelf", "front-desk")
	add("Local", "")
	if got, _ := st.GetPrinter(desk.ID); got.Connection.Relay != "front-desk" || got.Connection.Address != "192.168.1.40:9100" {
		t.Fatalf("read back: %+v", got.Connection)
	}

	job := func(id, printer, state, msg string, at time.Time) {
		_, err := st.Exec(`INSERT INTO spool_jobs (id, template_name, printer_id, target_name, state, error, labels, created_at, finished_at)
			VALUES (?, 'Shelf', ?, 'Desk', ?, ?, 1, ?, ?)`, id, printer, state, msg, dbTime(at), dbTime(at.Add(time.Second)))
		if err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now()
	job("j1", desk.ID, models.SpoolFailed, "relay front-desk is offline", now.Add(-time.Hour))
	job("j2", desk.ID, models.SpoolDone, "", now)

	got, err := st.RelayPrinters()
	if err != nil {
		t.Fatal(err)
	}
	list := got["front-desk"]
	if len(got) != 1 || len(list) != 2 || list[0].Name != "Desk" || list[1].Name != "Shelf" {
		t.Fatalf("printers through relays: %+v", got)
	}
	if list[0].LastJob == nil || list[0].LastJob.State != models.SpoolDone || list[1].LastJob != nil {
		t.Fatalf("last jobs: %+v, %+v", list[0].LastJob, list[1].LastJob)
	}
	_ = shelf
}
