package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers/tspl"
)

// blePrinter is a simulated Bluetooth LE printer, keeping what is written
// to it.
type blePrinter struct {
	mu      sync.Mutex
	written []byte
	profile printers.BLEProfile
}

func (b *blePrinter) dial(_ context.Context, _ string, p printers.BLEProfile) (printers.BLEPeripheral, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.profile = p
	return b, nil
}
func (b *blePrinter) MTU() int                     { return 185 }
func (b *blePrinter) Notifications() <-chan []byte { return nil }
func (b *blePrinter) Close() error                 { return nil }
func (b *blePrinter) Write(_ context.Context, p []byte) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.written = append(b.written, p...)
	return nil
}

func TestPrintOverBluetoothLE(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	body := func(address string) string {
		return `{"name":"Bluetooth Rollo","driver":"tspl","model":"Rollo X1038","connection":{"type":"ble","address":"` + address + `"},"media":"2x1"}`
	}
	address := "7c:2f:80:a1:b2:c3?service=fff0&write=fff2"

	// The default configuration allows no Bluetooth printers.
	rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/printers", body(address))
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "[printers] bluetooth") {
		t.Fatalf("Bluetooth printer with none allowed: %d %s", rec.Code, rec.Body.String())
	}

	ble := &blePrinter{}
	d := printers.NewDispatcher(nil)
	d.AllowedBluetooth = []string{"7C:2F:80:*"}
	d.DialBLE = ble.dial
	srv.transport = d
	// tspl has no Bluetooth LE profile of its own, so the connection gives
	// one.
	rec = doJSON(t, srv, cookie, http.MethodPost, "/api/v1/printers", body("7C:2F:80:A1:B2:C3"))
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "service and write characteristic") {
		t.Fatalf("Bluetooth printer with no profile: %d %s", rec.Code, rec.Body.String())
	}
	rec = doJSON(t, srv, cookie, http.MethodPost, "/api/v1/printers", body(address))
	if rec.Code != http.StatusCreated {
		t.Fatalf("creating printer: %d %s", rec.Code, rec.Body.String())
	}
	var created struct {
		Printer models.Printer `json:"printer"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	if want := "7C:2F:80:A1:B2:C3?service=0000fff0-0000-1000-8000-00805f9b34fb&write=0000fff2-0000-1000-8000-00805f9b34fb"; created.Printer.Connection.Address != want {
		t.Errorf("saved address %q, want %q", created.Printer.Connection.Address, want)
	}

	tmpl := `{"id":"rollo2x1","name":"Rollo 2x1","units":"in","width":2,"height":1,"dpi":203,"elements":[
  {"type":"text","x":0.1,"y":0.2,"width":1.8,"height":0.4,"font_size":10,"content_binding":"{{asset.name}}"}]}`
	rollo2x1 := createTemplate(t, srv, cookie, tmpl)
	a, _ := srv.store.CreateAsset("Drill", nil, nil, nil)
	rec = doJSON(t, srv, cookie, http.MethodPost, "/api/v1/printers/"+created.Printer.ID+"/print", `{"template_id":"`+rollo2x1+`","asset_ids":["`+a.Asset.ID+`"]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("print: %d %s", rec.Code, rec.Body.String())
	}
	ble.mu.Lock()
	written, profile := ble.written, ble.profile
	ble.mu.Unlock()
	if job, err := tspl.DecodeJob(written); err != nil || len(job.Labels) != 1 {
		t.Errorf("the printer received %d bytes that decode to %+v, %v", len(written), job, err)
	}
	if profile.Write != "0000fff2-0000-1000-8000-00805f9b34fb" {
		t.Errorf("dialed with %+v", profile)
	}

	// A printer later dropped from the configuration is refused at print
	// time.
	d.AllowedBluetooth = []string{"00:11:*"}
	rec = doJSON(t, srv, cookie, http.MethodPost, "/api/v1/printers/"+created.Printer.ID+"/print", `{"template_id":"`+rollo2x1+`","asset_ids":["`+a.Asset.ID+`"]}`)
	if rec.Code != http.StatusBadGateway {
		t.Errorf("print to a no longer allowed printer: %d %s", rec.Code, rec.Body.String())
	}
}
