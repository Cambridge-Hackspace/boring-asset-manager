package printers

import (
	"bytes"
	"encoding/json"
	"errors"
	"image"
	"testing"
)

func TestESCPOSValidate(t *testing.T) {
	stock := &Stock{ID: "11111111-2222-3333-4444-555555555555", Name: "Custom", WidthMM: 12, LengthMM: 50}
	for _, c := range []struct {
		why, driver, model, media, options string
		stock                              *Stock
		ok                                 bool
	}{
		{"58 mm receipt", DriverESCPOS, "Generic ESC/POS 58 mm", "58mm", "", nil, true},
		{"80 mm receipt, options", DriverESCPOS, "Generic ESC/POS 80 mm", "80mm", `{"head_dots":512,"cut":"none","feed_mm":12}`, nil, true},
		{"80 mm paper in a 58 mm printer", DriverESCPOS, "Generic ESC/POS 58 mm", "80mm", "", nil, false},
		{"a Phomemo model", DriverESCPOS, "M110", "58mm", "", nil, false},
		{"a Phomemo roll", DriverESCPOS, "Generic ESC/POS 58 mm", "50x30", "", nil, false},
		{"cut", DriverESCPOS, "Generic ESC/POS 58 mm", "58mm", `{"cut":"half"}`, nil, false},
		{"feed", DriverESCPOS, "Generic ESC/POS 58 mm", "58mm", `{"feed_mm":101}`, nil, false},
		{"head", DriverESCPOS, "Generic ESC/POS 58 mm", "58mm", `{"head_dots":4}`, nil, false},
		{"a Phomemo option", DriverESCPOS, "Generic ESC/POS 58 mm", "58mm", `{"density":4}`, nil, false},
		{"M110", DriverPhomemo, "M110", "40x30", `{"density":15,"print_speed":1,"rotate_180":true}`, nil, true},
		{"M220's widest", DriverPhomemo, "M220", "70x80", "", nil, true},
		{"70 mm on the M110", DriverPhomemo, "M110", "70x80", "", nil, false},
		{"density", DriverPhomemo, "M110", "40x30", `{"density":16}`, nil, false},
		{"print speed", DriverPhomemo, "M110", "40x30", `{"print_speed":0}`, nil, false},
		{"density on a D30", DriverPhomemo, "D30", "12x40", `{"density":8}`, nil, false},
		{"D30 turned", DriverPhomemo, "D30", "12x40", `{"rotate_180":true}`, nil, true},
		{"tape on an M110", DriverPhomemo, "M110", "12x40", "", nil, false},
		{"labels on a T02", DriverPhomemo, "T02", "40x30", "", nil, false},
		{"T02 paper", DriverPhomemo, "T02", "53-continuous", "", nil, true},
		{"a generic model", DriverPhomemo, "Generic ESC/POS 58 mm", "58mm", "", nil, false},
		{"an ESC/POS option", DriverPhomemo, "M110", "40x30", `{"cut":"full"}`, nil, false},
		{"custom tape on a Q30", DriverPhomemo, "Q30", stock.ID, "", stock, true},
		{"custom labels on a receipt printer", DriverESCPOS, "Generic ESC/POS 58 mm", stock.ID, "", stock, true},
		{"custom tape on a T02", DriverPhomemo, "T02", stock.ID, "", stock, false},
	} {
		s := Settings{Model: c.model, Media: c.media, Stock: c.stock, Options: json.RawMessage(c.options)}
		err := Validate("P", c.driver, Connection{Type: ConnTCP, Address: "printer.lan"}, s)
		if c.ok && err != nil {
			t.Errorf("%s: %v", c.why, err)
		}
		if !c.ok && !errors.Is(err, ErrInvalidPrinter) {
			t.Errorf("%s: got %v, want ErrInvalidPrinter", c.why, err)
		}
	}
}

func TestESCPOSPlans(t *testing.T) {
	// A D30's 40 × 12 mm landscape label is turned to run along the tape,
	// as the head's 96 dots wide.
	d, _ := Lookup(DriverPhomemo)
	plan, err := planWith(d, Settings{Model: "D30", Media: "12x40"}, LabelSpec{WidthMM: 40, HeightMM: 12, DPI: 203})
	if err != nil {
		t.Fatal(err)
	}
	stream, err := plan.Encode([]*image.Gray{blankLabel(40, 12, 203), blankLabel(40, 12, 203)})
	if err != nil {
		t.Fatal(err)
	}
	// 1F 11 24 00, ESC @, GS v 0 of 12 bytes by 320 lines, twice.
	label := append([]byte{0x1f, 0x11, 0x24, 0x00, 0x1b, 0x40, 0x1d, 0x76, 0x30, 0x00, 12, 0, 0x40, 0x01}, make([]byte, 12*320)...)
	label = append(label, 0x1b, 0x64, 0x00)
	if !bytes.Equal(stream, append(append([]byte{}, label...), label...)) {
		t.Errorf("D30 stream of %d bytes", len(stream))
	}

	// The M110's options and media type reach the printer; its density
	// and speed are 10 and 5 unless set.
	plan, err = planWith(d, Settings{Model: "M110", Media: "50-continuous", Options: json.RawMessage(`{"density":12}`)}, LabelSpec{WidthMM: 50, HeightMM: 25, DPI: 203})
	if err != nil {
		t.Fatal(err)
	}
	stream, _ = plan.Encode([]*image.Gray{blankLabel(50, 25, 203)})
	if !bytes.HasPrefix(stream, []byte{0x1b, 0x4e, 0x0d, 5, 0x1b, 0x4e, 0x04, 12, 0x1f, 0x11, 0x0b, 0x1d, 0x76, 0x30, 0, 48, 0}) {
		t.Errorf("M110 stream starts % x", stream[:17])
	}

	// A head given in dots sets the raster's width, the paper cut to it
	// around its middle, and the cut is partial unless set.
	g, _ := Lookup(DriverESCPOS)
	settings := Settings{Model: "Generic ESC/POS 80 mm", Media: "80mm", Options: json.RawMessage(`{"head_dots":512}`)}
	geo, _ := g.Geometry(settings)
	if geo.ReachMM < 64 || geo.ReachMM > 64.1 || geo.DPI != 203 {
		t.Errorf("geometry %+v", geo)
	}
	plan, err = planWith(g, settings, LabelSpec{WidthMM: 80, HeightMM: 30, DPI: 203})
	if err != nil {
		t.Fatal(err)
	}
	stream, _ = plan.Encode([]*image.Gray{blankLabel(80, 30, 203)})
	if !bytes.HasPrefix(stream, []byte{0x1b, 0x40, 0x1d, 0x76, 0x30, 0, 64, 0, 240, 0}) || !bytes.HasSuffix(stream, []byte{0x1d, 0x56, 0x42, 0}) {
		t.Errorf("ESC/POS stream of %d bytes", len(stream))
	}
}

func TestPhomemoBluetooth(t *testing.T) {
	// A Phomemo needs only its address; the driver gives the profile.
	got, err := ConnectionFor(DriverPhomemo, Connection{Type: ConnBLE, Address: printerMAC})
	if err != nil || got.Address != printerMAC+"?service=0000ff00-0000-1000-8000-00805f9b34fb&write=0000ff02-0000-1000-8000-00805f9b34fb" {
		t.Errorf("ConnectionFor = %+v, %v", got, err)
	}
	// Plain ESC/POS printers' profiles differ by maker, so the connection
	// gives it.
	if _, err := ConnectionFor(DriverESCPOS, Connection{Type: ConnBLE, Address: printerMAC}); !errors.Is(err, ErrInvalidPrinter) {
		t.Errorf("ESC/POS without a profile: %v", err)
	}
	if _, err := ConnectionFor(DriverESCPOS, Connection{Type: ConnBLE, Address: printerMAC + "?service=18f0&write=2af1"}); err != nil {
		t.Error(err)
	}
	for _, info := range Drivers() {
		if info.Driver == DriverPhomemo && info.BLE == nil || info.Driver == DriverESCPOS && info.BLE != nil {
			t.Errorf("%s profile %+v", info.Driver, info.BLE)
		}
	}
}
