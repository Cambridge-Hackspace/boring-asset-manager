package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/relay"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/relayd"
)

// runRelay runs a paired relay, as bam relay does, allowed to reach allow,
// until stop is called or the test ends.
func runRelay(t *testing.T, e *pairingEnv, p *relay.Pairing, allow ...string) (stop func()) {
	t.Helper()
	a, err := relayd.ParseAllowlist(allow, nil)
	if err != nil {
		t.Fatal(err)
	}
	r, err := relayd.New(relayd.Config{Pairing: p, Version: "v-relay", WebSocket: true, Allow: a,
		Transport: printers.NewDispatcher(nil), Client: e.ts.Client(), Retry: 200 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := r.Run(ctx); err != nil {
			t.Error(err)
		}
	}()
	stopped := false
	stop = func() {
		if !stopped {
			stopped = true
			cancel()
			<-done
		}
	}
	t.Cleanup(stop)
	waitUntil(t, "the relay to link", func() bool { return len(e.srv.relays.Transports(p.Name)) > 0 })
	return stop
}

// relayPrinter adds a printer reached through a relay.
func relayPrinter(t *testing.T, e *pairingEnv, name, relayName, address string) models.Printer {
	t.Helper()
	body := `{"name":"` + name + `","driver":"tspl","model":"Rollo X1038","connection":{"type":"tcp","address":"` + address + `","relay":"` + relayName + `"},"media":"3x2"}`
	rec := doJSON(t, e.srv, e.cookie, http.MethodPost, "/api/v1/printers", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("adding %s: %d %s", name, rec.Code, rec.Body)
	}
	var out struct {
		Printer models.Printer `json:"printer"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || out.Printer.ID == "" {
		var p models.Printer
		_ = json.Unmarshal(rec.Body.Bytes(), &p)
		out.Printer = p
	}
	if out.Printer.Connection.Relay != relayName {
		t.Fatalf("the printer's connection: %s", rec.Body)
	}
	return out.Printer
}

func TestAPrinterBehindARelayPrints(t *testing.T) {
	e := pairingServer(t)
	code, _ := e.add(t, "front-desk")
	p, err := e.join(code)
	if err != nil {
		t.Fatal(err)
	}
	fake, outside := newFakeQL(t), newFakeQL(t)
	addr := "127.0.0.1:" + strconv.Itoa(fake.port())
	runRelay(t, e, p, addr)
	printer := relayPrinter(t, e, "Front desk", "front-desk", addr)

	// The main node encodes; the relay delivers the bytes.
	if err := e.srv.transport.Send(context.Background(), printer.Connection, []byte("SIZE 3,2\r\nPRINT 1\r\n")); err != nil {
		t.Fatal(err)
	}
	if jobs := fake.waitForJobs(t, 1, 10*time.Second); len(jobs) != 1 || string(jobs[0]) != "SIZE 3,2\r\nPRINT 1\r\n" {
		t.Fatalf("the printer took %q", jobs)
	}

	// A job larger than one message goes in pieces, and arrives whole.
	saved := relayJobPiece
	relayJobPiece = 100 << 10
	t.Cleanup(func() { relayJobPiece = saved })
	big := bytes.Repeat([]byte("0123456789"), 25<<10)
	if err := e.srv.transport.Send(context.Background(), printer.Connection, big); err != nil {
		t.Fatal(err)
	}
	if jobs := fake.waitForJobs(t, 2, 10*time.Second); len(jobs) != 2 || !bytes.Equal(jobs[1], big) {
		t.Fatal("the large job did not arrive whole")
	}

	// The relay's allowlist decides, even when the main node asks.
	elsewhere := printer.Connection
	elsewhere.Address = "127.0.0.1:" + strconv.Itoa(outside.port())
	if err := e.srv.transport.Send(context.Background(), elsewhere, []byte("scan")); err == nil || !strings.Contains(err.Error(), "outside this relay's allowlist") {
		t.Fatalf("a printer outside the allowlist: %v", err)
	}
	if jobs := outside.waitForJobs(t, 1, 300*time.Millisecond); len(jobs) != 0 {
		t.Fatal("a printer outside the allowlist was reached")
	}

	// Admin › Relays lists the printer, with its status kept from the relay.
	waitUntil(t, "the relay's status", func() bool {
		rl, err := e.srv.store.RelayByName("front-desk")
		return err == nil && rl.Version == "v-relay" && len(rl.Allowlist) == 1
	})
	rec := doJSON(t, e.srv, e.cookie, http.MethodGet, "/api/v1/relays", "")
	var list struct {
		Relays []relayView `json:"relays"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &list)
	if len(list.Relays) != 1 || len(list.Relays[0].Printers) != 1 || list.Relays[0].Printers[0].Name != "Front desk" {
		t.Fatalf("relays: %s", rec.Body)
	}
}

func TestAnOfflineRelayFailsJobsUntilItIsBack(t *testing.T) {
	e := pairingServer(t)
	code, _ := e.add(t, "front-desk")
	p, err := e.join(code)
	if err != nil {
		t.Fatal(err)
	}
	fake := newFakeQL(t)
	addr := "127.0.0.1:" + strconv.Itoa(fake.port())
	stop := runRelay(t, e, p, addr)
	printer := relayPrinter(t, e, "Front desk", "front-desk", addr)
	stop()
	waitUntil(t, "the relay to go", func() bool { return len(e.srv.relays.Transports("front-desk")) == 0 })

	err = e.srv.transport.Send(context.Background(), printer.Connection, []byte("PRINT 1\r\n"))
	if err == nil || !strings.Contains(err.Error(), "relay front-desk is offline") {
		t.Fatalf("a job to an offline relay ended with %v", err)
	}

	// Back, it prints again.
	runRelay(t, e, p, addr)
	if err := e.srv.transport.Send(context.Background(), printer.Connection, []byte("PRINT 1\r\n")); err != nil {
		t.Fatal(err)
	}
	if jobs := fake.waitForJobs(t, 1, 10*time.Second); len(jobs) != 1 {
		t.Fatalf("after the relay came back, the printer took %d jobs", len(jobs))
	}
}

func TestAPrinterNamesARelayThatExists(t *testing.T) {
	e := pairingServer(t)
	body := `{"name":"Nowhere","driver":"tspl","model":"Rollo X1038","connection":{"type":"tcp","address":"192.168.1.40","relay":"cellar"},"media":"3x2"}`
	if rec := doJSON(t, e.srv, e.cookie, http.MethodPost, "/api/v1/printers", body); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "no relay named cellar") {
		t.Fatalf("a printer through a relay that does not exist: %d %s", rec.Code, rec.Body)
	}
	// A device through a relay is the relay's to allow, not this server's.
	e.add(t, "workshop")
	device := `{"name":"USB","driver":"tspl","model":"Rollo X1038","connection":{"type":"device","address":"/dev/usb/lp0","relay":"workshop"},"media":"3x2"}`
	if rec := doJSON(t, e.srv, e.cookie, http.MethodPost, "/api/v1/printers", device); rec.Code != http.StatusCreated {
		t.Fatalf("a device through a relay: %d %s", rec.Code, rec.Body)
	}
}
