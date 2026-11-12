package printers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers/niimbot"
)

func TestNiimbotDriverInfo(t *testing.T) {
	d, err := Lookup(DriverNiimbot)
	if err != nil {
		t.Fatal(err)
	}
	info := d.Info()
	if len(info.Models) != 8 || info.ModelDPI["D11"] != 203 || info.ModelDPI["K3"] != 203 || len(info.Media) != len(niimbot.MediaIDs()) {
		t.Fatalf("info = %+v", info)
	}
	if len(info.Options) != 2 || info.Options[0].Name != "density" || info.Options[0].Default != nil ||
		strings.Join(info.Options[1].Values, ",") != "b1,b21_v1,d110" {
		t.Errorf("options = %+v", info.Options)
	}
	if c := info.CustomStock; c == nil || c.MaxWidthMM["D11"] != 15 || c.MaxWidthMM["B3S"] != 75 || c.Note == "" {
		t.Errorf("custom stock = %+v", c)
	}
	for _, m := range info.Media {
		switch m.ID {
		case "12x40":
			if strings.Join(m.Models, ",") != "D101,D11,D110" {
				t.Errorf("12 × 40 mm is for %v", m.Models)
			}
		case "50-continuous":
			if strings.Join(m.Models, ",") != "B21,B3S" {
				t.Errorf("50 mm continuous is for %v", m.Models)
			}
		}
	}
	// Listed with its Bluetooth LE profile, a conversation.
	for _, di := range Drivers() {
		if di.Driver == DriverNiimbot && (di.BLE == nil || di.BLE.Protocol != ProtocolNiimbot || di.BLE.Notify != di.BLE.Write) {
			t.Errorf("drivers endpoint lists %+v", di.BLE)
		}
	}
}

func TestNiimbotSettings(t *testing.T) {
	opts := func(s string) json.RawMessage { return json.RawMessage(s) }
	for _, c := range []struct {
		s  Settings
		ok bool
	}{
		{Settings{Model: "D11", Media: "12x40"}, true},
		{Settings{Model: "B21", Media: "50-continuous", Options: opts(`{"density": 5, "print_task": "b1"}`)}, true},
		{Settings{Model: "D11", Media: "12x40", Options: opts(`{"density": 4}`)}, false},
		{Settings{Model: "D11", Media: "12x40", Options: opts(`{"print_task": "d11_v1"}`)}, false},
		{Settings{Model: "D11", Media: "12x40", Options: opts(`{"speed": 1}`)}, false},
		{Settings{Model: "D11", Media: "50x30"}, false},
		{Settings{Model: "B1", Media: "50-continuous"}, false},
		{Settings{Model: "B1", Stock: &Stock{ID: "s", Name: "Bins", WidthMM: 50, LengthMM: 25, GapMM: 2}}, true},
		{Settings{Model: "B1", Stock: &Stock{ID: "s", Name: "Wide", WidthMM: 58, LengthMM: 40, GapMM: 2}}, false},
		{Settings{Model: "D12", Media: "12x40"}, false},
	} {
		if err := (niimbotDriver{}).Validate(c.s); (err == nil) != c.ok {
			t.Errorf("%+v: %v", c.s, err)
		}
	}
}

func TestNiimbotPlansALabel(t *testing.T) {
	// A 40 × 12 mm label on a D11's 12 × 40 mm roll is turned to print.
	plan, fit, err := PlanLabel(DriverNiimbot, Settings{Model: "D11", Media: "12x40"}, LabelSpec{WidthMM: 40, HeightMM: 12, DPI: 203})
	if err != nil || !fit.Rotate {
		t.Fatalf("plan: %+v, %v", fit, err)
	}
	label := image.NewGray(image.Rect(0, 0, 320, 96))
	job, err := plan.Encode([]*image.Gray{label, label})
	if err != nil {
		t.Fatal(err)
	}
	packets, err := niimbot.Split(job)
	if err != nil || len(packets) < 10 {
		t.Fatalf("%d packets, %v", len(packets), err)
	}
	// Its page is the head's 96 dots across and the label's 320 long.
	want := niimbot.Packet{Cmd: niimbot.CmdSetPageSize, Data: []byte{0x01, 0x40, 0x00, 0x60}}.Bytes()
	if !bytes.Contains(job, want) {
		t.Error("no 320 × 96 page")
	}
	// At 300 DPI it does not fit.
	if _, _, err := PlanLabel(DriverNiimbot, Settings{Model: "D11", Media: "12x40"}, LabelSpec{WidthMM: 40, HeightMM: 12, DPI: 300}); !errors.Is(err, ErrDoesNotFit) {
		t.Errorf("300 DPI: %v", err)
	}
	// The density option reaches the job.
	plan, _, _ = PlanLabel(DriverNiimbot, Settings{Model: "B1", Media: "40x30", Options: json.RawMessage(`{"density": 5}`)}, LabelSpec{WidthMM: 40, HeightMM: 30, DPI: 203})
	job, _ = plan.Encode([]*image.Gray{image.NewGray(image.Rect(0, 0, 320, 240))})
	if !bytes.Contains(job, niimbot.Packet{Cmd: niimbot.CmdSetDensity, Data: []byte{5}}.Bytes()) {
		t.Error("density 5 not set")
	}
}

func TestNiimbotConnectionsHoldAConversation(t *testing.T) {
	got, err := ConnectionFor(DriverNiimbot, Connection{Type: ConnBLE, Address: printerMAC})
	if err != nil || !strings.HasSuffix(got.Address, "&notify=bef8d6c9-9c21-4c9e-b632-bd58c1009f9f&protocol=niimbot") ||
		!strings.Contains(got.Address, "?service=e7810a71-73ae-499d-8c15-faa9aef0c3f2&write=bef8d6c9-9c21-4c9e-b632-bd58c1009f9f") {
		t.Errorf("ble: %q, %v", got.Address, err)
	}
	got, err = ConnectionFor(DriverNiimbot, Connection{Type: ConnDevice, Address: "/dev/ttyACM0"})
	if err != nil || got.Address != "/dev/ttyACM0?protocol=niimbot" {
		t.Errorf("device: %q, %v", got.Address, err)
	}
	// Other drivers' devices are as they were.
	if got, err := ConnectionFor(DriverTSPL, Connection{Type: ConnDevice, Address: "/dev/usb/lp0"}); err != nil || got.Address != "/dev/usb/lp0" {
		t.Errorf("tspl device: %q, %v", got.Address, err)
	}
	if err := Validate("Labels", DriverNiimbot, Connection{Type: ConnBLE, Address: printerMAC}, Settings{Model: "B1", Media: "40x30"}); err != nil {
		t.Errorf("validate: %v", err)
	}
}

func TestDeviceAddressesMayNameAProtocol(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"/dev/ttyACM0", "/dev/ttyACM0"},
		{"/dev//ttyACM0?protocol=niimbot", "/dev/ttyACM0?protocol=niimbot"},
		{"dev/ttyACM0", ""},
		{"/dev/ttyACM0?protocol=zpl", ""},
		{"/dev/ttyACM0?protocol=niimbot&baud=9600", ""},
		{"/dev/ttyACM0?baud=9600", ""},
	} {
		got, err := NormalizeConnection(Connection{Type: ConnDevice, Address: c.in})
		switch {
		case c.want == "" && err == nil:
			t.Errorf("%q: got %q, want an error", c.in, got.Address)
		case c.want != "" && (err != nil || got.Address != c.want):
			t.Errorf("%q: got %q, %v; want %q", c.in, got.Address, err, c.want)
		}
	}
	// The allowlist and the turns jobs take are the device's, whatever
	// protocol it is named with.
	d := NewDispatcher([]string{"/dev/ttyACM*"})
	if err := d.Check(Connection{Type: ConnDevice, Address: "/dev/ttyACM0?protocol=niimbot"}); err != nil {
		t.Errorf("refused: %v", err)
	}
	if d.slot(Connection{Type: ConnDevice, Address: "/dev/ttyACM0?protocol=niimbot"}) != d.slot(Connection{Type: ConnDevice, Address: "/dev/ttyACM0"}) {
		t.Error("two destinations for one device")
	}
}

// niimbotReplies is the command each answers with.
var niimbotReplies = map[byte]byte{0xC1: 0xC2, 0x1A: 0x1B, 0x21: 0x31, 0x23: 0x33, 0x01: 0x02, 0x20: 0x30, 0x03: 0x04,
	0x13: 0x14, 0x15: 0x16, 0xE3: 0xE4, 0xA3: 0xB3, 0xF3: 0xF4}

// niimbotPeripheral is a simulated Niimbot printer over Bluetooth LE: it
// answers each command, a printed page at a time, and can be told to stay
// silent after one.
type niimbotPeripheral struct {
	mtu    int
	silent byte

	mu      sync.Mutex
	reader  niimbot.Reader
	writes  [][]byte
	got     []byte
	ended   int
	printed int
	notes   chan []byte
}

func (p *niimbotPeripheral) dial(context.Context, string, BLEProfile) (BLEPeripheral, error) {
	return p, nil
}
func (p *niimbotPeripheral) MTU() int                     { return p.mtu }
func (p *niimbotPeripheral) Notifications() <-chan []byte { return p.notes }
func (p *niimbotPeripheral) Close() error                 { return nil }

func (p *niimbotPeripheral) Write(_ context.Context, chunk []byte) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.writes = append(p.writes, append([]byte(nil), chunk...))
	for _, pk := range p.reader.Add(chunk) {
		p.got = append(p.got, pk.Cmd)
		reply, ok := niimbotReplies[pk.Cmd]
		if !ok || pk.Cmd == p.silent {
			continue
		}
		data := []byte{1}
		switch pk.Cmd {
		case niimbot.CmdRFIDInfo:
			data = []byte{0}
		case niimbot.CmdPageEnd:
			p.ended++
		case niimbot.CmdPrintStatus:
			p.printed = min(p.printed+1, p.ended)
			data = []byte{0, byte(p.printed), 100, 100}
		}
		p.notes <- niimbot.Packet{Cmd: reply, Data: data}.Bytes()
	}
	return nil
}

func TestNiimbotPrintsOverBluetoothLE(t *testing.T) {
	plan, _, err := PlanLabel(DriverNiimbot, Settings{Model: "B1", Media: "40x30"}, LabelSpec{WidthMM: 40, HeightMM: 30, DPI: 203})
	if err != nil {
		t.Fatal(err)
	}
	label := image.NewGray(image.Rect(0, 0, 320, 240))
	job, err := plan.Encode([]*image.Gray{label, label})
	if err != nil {
		t.Fatal(err)
	}
	conn, err := ConnectionFor(DriverNiimbot, Connection{Type: ConnBLE, Address: printerMAC})
	if err != nil {
		t.Fatal(err)
	}

	p := &niimbotPeripheral{mtu: 23, notes: make(chan []byte, 64)}
	d := NewDispatcher(nil)
	d.AllowedBluetooth, d.DialBLE = []string{"*"}, p.dial
	d.conversationTiming = &niimbot.Timing{Timeout: time.Second, Poll: time.Millisecond}
	if err := d.Send(context.Background(), conn, job); err != nil {
		t.Fatal(err)
	}
	if p.printed != 2 || p.got[len(p.got)-1] != niimbot.CmdPrintEnd {
		t.Errorf("printed %d, commands % x", p.printed, p.got)
	}
	// Each write fits the link: 20 bytes, with no MTU agreed.
	for _, w := range p.writes {
		if len(w) > 20 {
			t.Fatalf("a %d-byte write", len(w))
		}
	}

	// A printer that stops answering fails the job, saying so.
	p = &niimbotPeripheral{mtu: 185, notes: make(chan []byte, 64), silent: niimbot.CmdSetLabelType}
	d.DialBLE = p.dial
	d.conversationTiming = &niimbot.Timing{Timeout: 50 * time.Millisecond, Poll: time.Millisecond}
	err = d.Send(context.Background(), conn, job)
	if !errors.Is(err, ErrUnreachable) || !strings.Contains(err.Error(), "the printer did not answer the label type within 50ms") {
		t.Errorf("silent printer: %v", err)
	}
}
