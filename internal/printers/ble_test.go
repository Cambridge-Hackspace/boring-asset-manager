package printers

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// peripheral is a simulated GATT peripheral: a Bluetooth LE printer that
// keeps every write, and acknowledges each with a notification when ack
// is set.
type peripheral struct {
	mtu int
	// ack acknowledges each write; with acks held, the test sends them
	// on release instead.
	ack, holdAcks bool
	// stall makes writes wait until they time out.
	stall bool

	mu      sync.Mutex
	writes  [][]byte
	notes   chan []byte
	release chan struct{}
	closed  bool
	dialed  BLEProfile
}

func newPeripheral(mtu int) *peripheral {
	return &peripheral{mtu: mtu, notes: make(chan []byte, 1), release: make(chan struct{})}
}

func (p *peripheral) dial(ctx context.Context, mac string, profile BLEProfile) (BLEPeripheral, error) {
	p.mu.Lock()
	p.dialed = profile
	p.mu.Unlock()
	return p, nil
}

func (p *peripheral) MTU() int { return p.mtu }

func (p *peripheral) Write(ctx context.Context, chunk []byte) error {
	if p.stall {
		<-ctx.Done()
		return ctx.Err()
	}
	p.mu.Lock()
	p.writes = append(p.writes, append([]byte{}, chunk...))
	p.mu.Unlock()
	if p.ack {
		go func() {
			if p.holdAcks {
				<-p.release
			}
			p.notes <- []byte{0x01}
		}()
	}
	return nil
}

func (p *peripheral) Notifications() <-chan []byte {
	if !p.ack {
		return nil
	}
	return p.notes
}

func (p *peripheral) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed = true
	return nil
}

func (p *peripheral) written() [][]byte {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([][]byte{}, p.writes...)
}

const printerMAC = "7C:2F:80:A1:B2:C3"

// bleDispatcher allows one printer, reached through p.
func bleDispatcher(p *peripheral) *Dispatcher {
	d := NewDispatcher(nil)
	d.AllowedBluetooth = []string{"7c:2f:80:*"}
	d.DialBLE = p.dial
	d.WriteTimeout = 100 * time.Millisecond
	return d
}

func TestParseBLEAddress(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"7c:2f:80:a1:b2:c3", printerMAC},
		{"7C-2F-80-A1-B2-C3", printerMAC},
		{printerMAC + "?write=FFF2&service=0xfff0", printerMAC + "?service=0000fff0-0000-1000-8000-00805f9b34fb&write=0000fff2-0000-1000-8000-00805f9b34fb"},
		{printerMAC + "?service=E7810A7173AE499D8C15FAA9AEF0C3F2&notify=bef8d6c9-9c21-4c9e-b632-bd58c1009f9f&ack=true",
			printerMAC + "?service=e7810a71-73ae-499d-8c15-faa9aef0c3f2&notify=bef8d6c9-9c21-4c9e-b632-bd58c1009f9f&ack=true"},
		{printerMAC + "?ack=false", printerMAC + "?ack=false"},
		{"7C:2F:80:A1:B2", ""},
		{"printer.lan", ""},
		{printerMAC + "?service=fff", ""},
		{printerMAC + "?service=fff0&service=fff1", ""},
		{printerMAC + "?ack=yes", ""},
		{printerMAC + "?mtu=512", ""},
	} {
		a, err := ParseBLEAddress(c.in)
		switch {
		case c.want == "" && err == nil:
			t.Errorf("%q: got %q, want an error", c.in, a)
		case c.want != "" && (err != nil || a.String() != c.want):
			t.Errorf("%q: got %q, %v; want %q", c.in, a, err, c.want)
		}
	}
	// As a connection, the address is kept in that form.
	got, err := NormalizeConnection(Connection{Type: ConnBLE, Address: " 7c:2f:80:a1:b2:c3?write=fff2 "})
	if err != nil || got.Address != printerMAC+"?write=0000fff2-0000-1000-8000-00805f9b34fb" {
		t.Errorf("normalized to %+v, %v", got, err)
	}
	if _, err := NormalizeConnection(Connection{Type: ConnBLE, Address: "/dev/rfcomm0"}); !errors.Is(err, ErrInvalidPrinter) {
		t.Errorf("a device path taken as a Bluetooth address: %v", err)
	}
}

// bleDriver is a driver whose printers connect over Bluetooth LE.
type bleDriver struct{ tsplDriver }

func (bleDriver) BLE() BLEProfile {
	return BLEProfile{Service: "0000fff0-0000-1000-8000-00805f9b34fb", Write: "0000fff2-0000-1000-8000-00805f9b34fb",
		Notify: "0000fff1-0000-1000-8000-00805f9b34fb", Ack: true}
}

func TestTheDriverGivesTheBLEProfile(t *testing.T) {
	// The connection takes the driver's profile where it sets none.
	got, err := connectionFor(bleDriver{}, Connection{Type: ConnBLE, Address: printerMAC})
	want := printerMAC + "?service=0000fff0-0000-1000-8000-00805f9b34fb&write=0000fff2-0000-1000-8000-00805f9b34fb&notify=0000fff1-0000-1000-8000-00805f9b34fb&ack=true"
	if err != nil || got.Address != want {
		t.Errorf("with the driver's profile: %q, %v", got.Address, err)
	}
	// And overrides it where it does.
	got, err = connectionFor(bleDriver{}, Connection{Type: ConnBLE, Address: printerMAC + "?write=ae01&ack=false"})
	want = printerMAC + "?service=0000fff0-0000-1000-8000-00805f9b34fb&write=0000ae01-0000-1000-8000-00805f9b34fb&notify=0000fff1-0000-1000-8000-00805f9b34fb"
	if err != nil || got.Address != want {
		t.Errorf("overridden: %q, %v", got.Address, err)
	}
	// A driver with no profile needs the connection to give one.
	if _, err := connectionFor(tsplDriver{}, Connection{Type: ConnBLE, Address: printerMAC}); !errors.Is(err, ErrInvalidPrinter) {
		t.Errorf("no service or characteristic: %v", err)
	}
	if _, err := connectionFor(tsplDriver{}, Connection{Type: ConnBLE, Address: printerMAC + "?service=fff0&write=fff2"}); err != nil {
		t.Errorf("a profile on the connection: %v", err)
	}
	if _, err := connectionFor(tsplDriver{}, Connection{Type: ConnBLE, Address: printerMAC + "?service=fff0&write=fff2&ack=true"}); !errors.Is(err, ErrInvalidPrinter) {
		t.Errorf("acknowledgments with nothing to notify them: %v", err)
	}
	// Validating a printer checks the same.
	if err := Validate("Bluetooth", "tspl", Connection{Type: ConnBLE, Address: printerMAC}, Settings{}); !errors.Is(err, ErrInvalidPrinter) {
		t.Errorf("validated with no profile: %v", err)
	}
	// Other connections are as they were.
	tcp := Connection{Type: ConnTCP, Address: "10.0.0.5:9100"}
	if got, err := ConnectionFor("tspl", tcp); err != nil || got != tcp {
		t.Errorf("tcp became %+v, %v", got, err)
	}
}

func TestBLEConnectionsMustBeAllowed(t *testing.T) {
	p := newPeripheral(23)
	conn := Connection{Type: ConnBLE, Address: printerMAC + "?service=fff0&write=fff2"}

	d := NewDispatcher(nil)
	d.DialBLE = p.dial
	if err := d.Check(conn); !errors.Is(err, ErrInvalidPrinter) || !strings.Contains(err.Error(), "[printers] bluetooth") {
		t.Errorf("allowed with no addresses configured: %v", err)
	}
	d.AllowedBluetooth = []string{"7C:2F:80:A1:B2:C4", "00:11:*"}
	if err := d.Check(conn); !errors.Is(err, ErrInvalidPrinter) {
		t.Errorf("allowed outside the patterns: %v", err)
	}
	if err := d.Send(context.Background(), conn, []byte("job")); !errors.Is(err, ErrUnreachable) {
		t.Errorf("sent outside the patterns: %v", err)
	}
	if len(p.written()) != 0 {
		t.Fatal("a printer outside the patterns was written to")
	}
	for _, allow := range []string{printerMAC, "7c:2f:80:a1:b2:c3", "7C:2F:80:*", "*"} {
		d.AllowedBluetooth = []string{allow}
		if err := d.Check(conn); err != nil {
			t.Errorf("%s refused: %v", allow, err)
		}
	}

	// Without Bluetooth LE on the system, it says so.
	old := systemBLE
	t.Cleanup(func() { systemBLE = old })
	systemBLE = nil
	d.DialBLE = nil
	if err := d.Check(conn); !errors.Is(err, ErrInvalidPrinter) || !strings.Contains(err.Error(), "Bluetooth LE isn't supported on this system") {
		t.Errorf("with no Bluetooth LE: %v", err)
	}
}

func TestBLEWritesInChunksTheMTUAllows(t *testing.T) {
	job := bytes.Repeat([]byte("0123456789"), 120) // 1200 bytes
	for _, c := range []struct{ mtu, chunk int }{
		{23, 20},    // every link's least
		{0, 20},     // unknown is the least
		{185, 182},  // as phones and most adapters agree
		{1024, 512}, // never more than a value holds
	} {
		p := newPeripheral(c.mtu)
		conn := Connection{Type: ConnBLE, Address: printerMAC + "?service=fff0&write=fff2"}
		if err := bleDispatcher(p).Send(context.Background(), conn, job); err != nil {
			t.Fatalf("MTU %d: %v", c.mtu, err)
		}
		writes := p.written()
		if len(writes) != (len(job)+c.chunk-1)/c.chunk {
			t.Errorf("MTU %d: %d writes", c.mtu, len(writes))
		}
		for i, w := range writes[:len(writes)-1] {
			if len(w) != c.chunk {
				t.Errorf("MTU %d: write %d is %d bytes, want %d", c.mtu, i, len(w), c.chunk)
			}
		}
		if got := bytes.Join(writes, nil); !bytes.Equal(got, job) {
			t.Errorf("MTU %d: the printer took %d bytes, not the job", c.mtu, len(got))
		}
		if !p.closed || p.dialed.Service != "0000fff0-0000-1000-8000-00805f9b34fb" || p.dialed.Write != "0000fff2-0000-1000-8000-00805f9b34fb" {
			t.Errorf("MTU %d: dialed %+v, closed %v", c.mtu, p.dialed, p.closed)
		}
	}
}

func TestBLEWaitsForEachAcknowledgment(t *testing.T) {
	p := newPeripheral(23)
	p.ack, p.holdAcks = true, true
	d := bleDispatcher(p)
	d.WriteTimeout = 5 * time.Second
	conn := Connection{Type: ConnBLE, Address: printerMAC + "?service=fff0&write=fff2&notify=fff1&ack=true"}
	done := make(chan error, 1)
	go func() { done <- d.Send(context.Background(), conn, bytes.Repeat([]byte{'x'}, 50)) }()

	// One write at a time: the next waits for the printer to acknowledge
	// the last.
	for want := 1; want <= 3; want++ {
		deadline := time.Now().Add(5 * time.Second)
		for len(p.written()) < want && time.Now().Before(deadline) {
			time.Sleep(5 * time.Millisecond)
		}
		time.Sleep(30 * time.Millisecond)
		if n := len(p.written()); n != want {
			t.Fatalf("%d writes before acknowledgment %d", n, want)
		}
		p.release <- struct{}{}
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if got := bytes.Join(p.written(), nil); len(got) != 50 {
		t.Errorf("the printer took %d bytes", len(got))
	}
}

func TestBLETimesOut(t *testing.T) {
	conn := Connection{Type: ConnBLE, Address: printerMAC + "?service=fff0&write=fff2&notify=fff1&ack=true"}

	// A printer that never acknowledges.
	p := newPeripheral(23)
	p.ack, p.holdAcks = true, true
	t.Cleanup(func() { close(p.release) })
	err := bleDispatcher(p).Send(context.Background(), conn, []byte("job"))
	if !errors.Is(err, ErrUnreachable) || !strings.Contains(err.Error(), "did not acknowledge a write within 100ms") {
		t.Errorf("no acknowledgment: %v", err)
	}

	// A printer that stops taking writes.
	p = newPeripheral(23)
	p.stall = true
	err = bleDispatcher(p).Send(context.Background(), Connection{Type: ConnBLE, Address: printerMAC + "?service=fff0&write=fff2"}, []byte("job"))
	if !errors.Is(err, ErrUnreachable) || !strings.Contains(err.Error(), "did not take a write within 100ms") {
		t.Errorf("a stalled write: %v", err)
	}

	// A printer that cannot be connected to.
	d := bleDispatcher(newPeripheral(23))
	d.BLEDialTimeout = 50 * time.Millisecond
	d.DialBLE = func(ctx context.Context, mac string, p BLEProfile) (BLEPeripheral, error) {
		<-ctx.Done()
		return nil, errors.New("the printer was not found")
	}
	err = d.Send(context.Background(), conn, []byte("job"))
	if !errors.Is(err, ErrUnreachable) || !strings.Contains(err.Error(), "could not be connected to in 50ms") {
		t.Errorf("not found: %v", err)
	}

	// With acknowledgments asked for but none to be had.
	p = newPeripheral(23)
	err = bleDispatcher(p).Send(context.Background(), conn, []byte("job"))
	if !errors.Is(err, ErrUnreachable) || !strings.Contains(err.Error(), "notify characteristic") {
		t.Errorf("no notifications: %v", err)
	}
}

func TestBLEJobsTakeTurnsPerPrinter(t *testing.T) {
	// However its profile is given, a printer is one destination.
	a := bleSlotKey(Connection{Type: ConnBLE, Address: printerMAC + "?service=fff0&write=fff2"})
	b := bleSlotKey(Connection{Type: ConnBLE, Address: printerMAC})
	if a != b {
		t.Errorf("%+v and %+v are different destinations", a, b)
	}
}
