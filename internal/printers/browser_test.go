package printers

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestBrowserConnections(t *testing.T) {
	for _, tc := range []struct {
		address, want, err string
		relay              string
	}{
		{address: "", want: ""},
		{address: "  ", want: ""},
		{address: "?write=fff2&service=fff0&chunk=180", want: "?service=0000fff0-0000-1000-8000-00805f9b34fb&write=0000fff2-0000-1000-8000-00805f9b34fb&chunk=180"},
		{address: "?ack=true&notify=fff1", want: "?notify=0000fff1-0000-1000-8000-00805f9b34fb&ack=true"},
		{address: "?protocol=niimbot", want: "?protocol=niimbot"},
		// The printer is the one picked at the browser, so it has no
		// Bluetooth address.
		{address: "7C:2F:80:A1:B2:C3", err: "must be empty, or start with ?"},
		{address: "?chunk=19", err: "chunk must be 20-512 bytes"},
		{address: "?chunk=513", err: "chunk must be 20-512 bytes"},
		{address: "?mtu=185", err: `"mtu" is not one of service, write, notify, ack, protocol and chunk`},
		{address: "?service=fff0&service=fff1", err: "gives service more than once"},
		{address: "", relay: "workshop", err: "the browser's own, never through a relay"},
	} {
		c, err := NormalizeConnection(Connection{Type: ConnBrowser, Address: tc.address, Relay: tc.relay})
		switch {
		case tc.err != "":
			if !errors.Is(err, ErrInvalidPrinter) || !strings.Contains(err.Error(), tc.err) {
				t.Errorf("%q: got %v, want %q", tc.address, err, tc.err)
			}
		case err != nil || c.Address != tc.want:
			t.Errorf("%q: got %q, %v, want %q", tc.address, c.Address, err, tc.want)
		}
	}

	// Only a browser sets the chunk: the server uses the link's MTU.
	if _, err := NormalizeConnection(Connection{Type: ConnBLE, Address: "7C:2F:80:A1:B2:C3?chunk=180"}); err == nil || !strings.Contains(err.Error(), `"chunk" is not one of service, write, notify, ack and protocol`) {
		t.Errorf("a ble connection's chunk: %v", err)
	}
}

func TestBrowserProfiles(t *testing.T) {
	// The driver's profile, with the connection's over it, writing as
	// little as every link takes unless the connection says otherwise.
	p, err := BrowserProfile(DriverPhomemo, Connection{Type: ConnBrowser})
	if err != nil || p.Service == "" || p.Write == "" || p.Chunk != 20 {
		t.Errorf("Phomemo: %+v, %v", p, err)
	}
	p, err = BrowserProfile(DriverPhomemo, Connection{Type: ConnBrowser, Address: "?write=ff03&chunk=100"})
	if err != nil || p.Write != "0000ff03-0000-1000-8000-00805f9b34fb" || p.Chunk != 100 {
		t.Errorf("Phomemo, its write characteristic set: %+v, %v", p, err)
	}
	// A driver with no profile needs the connection's, and is shown for
	// what it has without one.
	p, err = BrowserProfile(DriverESCPOS, Connection{Type: ConnBrowser, Address: "?service=fff0"})
	if !errors.Is(err, ErrInvalidPrinter) || !strings.Contains(err.Error(), "service and write characteristic") || p.Service != "0000fff0-0000-1000-8000-00805f9b34fb" {
		t.Errorf("ESC/POS with no write characteristic: %+v, %v", p, err)
	}
	// As it is sent on, the connection carries the whole profile.
	c, err := ConnectionFor(DriverNiimbot, Connection{Type: ConnBrowser})
	if err != nil || !strings.HasPrefix(c.Address, "?service=") || !strings.Contains(c.Address, "protocol=niimbot") || !strings.HasSuffix(c.Address, "chunk=20") {
		t.Errorf("Niimbot as sent on: %q, %v", c.Address, err)
	}
}

func TestTheServerDoesNotPrintToABrowsersPrinter(t *testing.T) {
	d := NewDispatcher(nil)
	c := Connection{Type: ConnBrowser}
	if err := d.Check(c); err != nil {
		t.Errorf("check: %v", err)
	}
	if err := d.Send(context.Background(), c, []byte("job")); !errors.Is(err, ErrUnreachable) || !strings.Contains(err.Error(), "printed to from a browser") {
		t.Errorf("send: %v", err)
	}
}
