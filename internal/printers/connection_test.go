package printers

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNormalizeConnection(t *testing.T) {
	for _, c := range []struct {
		in   Connection
		want string // normalized address, or "" when invalid
	}{
		{Connection{Type: ConnTCP, Address: "192.168.1.40"}, "192.168.1.40:9100"},
		{Connection{Type: ConnTCP, Address: " ql.lan:9101 "}, "ql.lan:9101"},
		{Connection{Type: ConnTCP, Address: "fd00::40"}, "[fd00::40]:9100"},
		{Connection{Type: ConnTCP, Address: "[fd00::40]:515"}, "[fd00::40]:515"},
		{Connection{Type: ConnTCP, Address: "tcp://10.0.0.1"}, ""},
		{Connection{Type: ConnTCP, Address: "10.0.0.1:70000"}, ""},
		{Connection{Type: ConnTCP, Address: "bad host"}, ""},
		{Connection{Type: ConnDevice, Address: "/dev/usb/../usb/lp0"}, "/dev/usb/lp0"},
		{Connection{Type: ConnDevice, Address: "dev/usb/lp0"}, ""},
		{Connection{Type: ConnDevice, Address: "/dev/usb/lp0\x00"}, ""},
		{Connection{Type: ConnIPP, Address: "ipp://cups.lan/printers/dymo"}, "ipp://cups.lan/printers/dymo"},
		{Connection{Type: ConnIPP, Address: "https://print.example/ipp/print"}, "https://print.example/ipp/print"},
		{Connection{Type: ConnIPP, Address: "ftp://cups.lan/printers/dymo"}, ""},
		{Connection{Type: ConnIPP, Address: "ipp://user:pw@cups.lan/printers/dymo"}, ""},
		{Connection{Type: ConnIPP, Address: "/printers/dymo"}, ""},
		{Connection{Type: ConnWindows, Address: `\\desk-pc\DYMO LabelWriter 450`}, `\\desk-pc\DYMO LabelWriter 450`},
		{Connection{Type: ConnWindows, Address: ""}, ""},
		{Connection{Type: ConnDevice, Address: "/dev/rfcomm0"}, "/dev/rfcomm0"},
		{Connection{Type: ConnBLE, Address: "7c:2f:80:a1:b2:c3"}, "7C:2F:80:A1:B2:C3"},
		{Connection{Type: ConnBLE, Address: "printer.lan"}, ""},
		{Connection{Type: "usb", Address: "/dev/usb/lp0"}, ""},
		// Through a relay, the address is on the relay's network.
		{Connection{Type: ConnTCP, Address: "192.168.1.40", Relay: "front-desk"}, "192.168.1.40:9100"},
		{Connection{Type: ConnTCP, Address: "192.168.1.40", Relay: "Front Desk"}, ""},
		{Connection{Type: ConnTCP, Address: "192.168.1.40", Relay: "main"}, ""},
	} {
		got, err := NormalizeConnection(c.in)
		switch {
		case c.want == "" && !errors.Is(err, ErrInvalidPrinter):
			t.Errorf("%+v: got %+v, %v; want ErrInvalidPrinter", c.in, got, err)
		case c.want != "" && (err != nil || got.Address != c.want):
			t.Errorf("%+v: got %+v, %v; want address %q", c.in, got, err, c.want)
		}
	}
}

func TestDeviceConnections(t *testing.T) {
	dir := t.TempDir()
	device := filepath.ToSlash(filepath.Join(dir, "lp0"))
	if err := os.WriteFile(device, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	conn := Connection{Type: ConnDevice, Address: device}

	if err := NewDispatcher(nil).Check(conn); !errors.Is(err, ErrInvalidPrinter) {
		t.Errorf("device allowed with no patterns configured: %v", err)
	}
	if err := NewDispatcher([]string{"/dev/usb/lp*"}).Send(context.Background(), conn, []byte("job")); !errors.Is(err, ErrUnreachable) {
		t.Errorf("send to a device outside the patterns: got %v", err)
	}

	d := NewDispatcher([]string{filepath.ToSlash(dir) + "/lp*"})
	if err := d.Check(conn); err != nil {
		t.Fatalf("allowed device refused: %v", err)
	}
	if err := d.Send(context.Background(), conn, []byte("raster job")); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(device); string(got) != "raster job" {
		t.Errorf("device received %q", got)
	}

	// A matching path that does not exist is never created.
	missing := Connection{Type: ConnDevice, Address: filepath.ToSlash(filepath.Join(dir, "lp1"))}
	if err := d.Send(context.Background(), missing, []byte("job")); !errors.Is(err, ErrUnreachable) {
		t.Errorf("send to a missing device: got %v", err)
	}
	if _, err := os.Stat(missing.Address); !os.IsNotExist(err) {
		t.Error("sending created the missing device as a file")
	}
}

// TestRFCOMMDeviceConnections checks the device connection reaches a
// Bluetooth Classic printer as Linux presents it once bound, /dev/rfcomm0,
// when [printers] devices allows it (docs/printing.md#bluetooth).
func TestRFCOMMDeviceConnections(t *testing.T) {
	conn, err := NormalizeConnection(Connection{Type: ConnDevice, Address: "/dev/rfcomm0"})
	if err != nil || conn.Address != "/dev/rfcomm0" {
		t.Fatalf("normalized to %+v, %v", conn, err)
	}
	if err := NewDispatcher([]string{"/dev/rfcomm*"}).Check(conn); err != nil {
		t.Errorf("an allowed rfcomm device refused: %v", err)
	}
	if err := NewDispatcher([]string{"/dev/usb/lp*"}).Check(conn); !errors.Is(err, ErrInvalidPrinter) {
		t.Errorf("an rfcomm device allowed by the USB pattern alone: %v", err)
	}

	// Written to as any device, standing in for the serial port a bound
	// rfcomm device is.
	dir := filepath.ToSlash(t.TempDir())
	rfcomm := Connection{Type: ConnDevice, Address: dir + "/rfcomm1"}
	if err := os.WriteFile(rfcomm.Address, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := NewDispatcher([]string{dir + "/rfcomm*"}).Send(context.Background(), rfcomm, []byte("ESC/POS job")); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(rfcomm.Address); string(got) != "ESC/POS job" {
		t.Errorf("rfcomm device received %q", got)
	}
}

// fakeIPP decodes Print-Job requests and answers with status.
type fakeIPP struct {
	status uint16
	attrs  map[string]string
	data   []byte
	path   string
}

func (f *fakeIPP) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	f.path = r.URL.Path
	if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/ipp" || len(body) < 9 ||
		body[0] != 1 || body[1] != 1 || binary.BigEndian.Uint16(body[2:4]) != ippOpPrintJob || body[8] != ippTagOperation {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	f.attrs = map[string]string{}
	s := body[9:]
	for len(s) > 0 && s[0] != ippTagEnd {
		nameLen := int(binary.BigEndian.Uint16(s[1:3]))
		name := string(s[3 : 3+nameLen])
		s = s[3+nameLen:]
		valueLen := int(binary.BigEndian.Uint16(s[:2]))
		f.attrs[name] = string(s[2 : 2+valueLen])
		s = s[2+valueLen:]
	}
	f.data = s[1:]
	w.Header().Set("Content-Type", "application/ipp")
	resp := []byte{1, 1, 0, 0, 0, 0, 0, 1, ippTagOperation, ippTagEnd}
	binary.BigEndian.PutUint16(resp[2:4], f.status)
	_, _ = w.Write(resp)
}

func TestIPPConnections(t *testing.T) {
	fake := &fakeIPP{}
	srv := httptest.NewServer(fake)
	defer srv.Close()

	uri := strings.Replace(srv.URL, "http://", "ipp://", 1) + "/printers/dymo"
	conn := Connection{Type: ConnIPP, Address: uri}
	d := NewDispatcher(nil)
	job := bytes.Repeat([]byte{0x1B, 0x00, 0xFF}, 1000)
	if err := d.Send(context.Background(), conn, job); err != nil {
		t.Fatal(err)
	}
	if fake.path != "/printers/dymo" || !bytes.Equal(fake.data, job) {
		t.Errorf("server got path %q and %d bytes", fake.path, len(fake.data))
	}
	for name, want := range map[string]string{
		"attributes-charset": "utf-8", "printer-uri": uri, "document-format": "application/octet-stream",
	} {
		if fake.attrs[name] != want {
			t.Errorf("%s = %q, want %q", name, fake.attrs[name], want)
		}
	}

	fake.status = 0x0400 // client-error-bad-request
	if err := d.Send(context.Background(), conn, job); !errors.Is(err, ErrUnreachable) || !strings.Contains(err.Error(), "0x0400") {
		t.Errorf("refused job: got %v", err)
	}
}

// slowConn writes a little at a time, as a connection to a printer that
// takes a job only as fast as it prints it does.
type slowConn struct{ net.Conn }

func (c slowConn) Write(b []byte) (int, error) {
	written := 0
	for len(b) > 0 {
		time.Sleep(5 * time.Millisecond)
		n, err := c.Conn.Write(b[:min(4<<10, len(b))])
		written += n
		if err != nil {
			return written, err
		}
		b = b[n:]
	}
	return written, nil
}

func TestIPPTakesAsLongAsThePrinterKeepsTakingTheJob(t *testing.T) {
	var got int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		got = len(data)
		_, _ = w.Write([]byte{1, 1, 0, 0, 0, 0, 0, 1, ippTagOperation, ippTagEnd})
	}))
	defer srv.Close()
	d := NewDispatcher(nil)
	d.WriteTimeout = 100 * time.Millisecond
	d.client = &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			c, err := (&net.Dialer{}).DialContext(ctx, network, addr)
			return slowConn{c}, err
		},
		WriteBufferSize: 4 << 10,
	}}
	conn := Connection{Type: ConnIPP, Address: strings.Replace(srv.URL, "http://", "ipp://", 1) + "/printers/ql"}
	// 64 pieces 5ms apart: several times WriteTimeout in all.
	job := bytes.Repeat([]byte("label"), 256<<10/5)
	start := time.Now()
	if err := d.Send(context.Background(), conn, job); err != nil {
		t.Fatalf("after %v: %v", time.Since(start), err)
	}
	if time.Since(start) < 3*d.WriteTimeout || got < len(job) {
		t.Errorf("took %d bytes in %v", got, time.Since(start))
	}

	// One that stops answering fails.
	hung := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		<-r.Context().Done()
	}))
	defer hung.Close()
	conn.Address = strings.Replace(hung.URL, "http://", "ipp://", 1) + "/printers/ql"
	if err := d.Send(context.Background(), conn, []byte("job")); !errors.Is(err, ErrUnreachable) {
		t.Errorf("a printer that never answers: %v", err)
	}
}

func TestIPPEndpoint(t *testing.T) {
	for in, want := range map[string]string{
		"ipp://cups.lan/printers/dymo":       "http://cups.lan:631/printers/dymo",
		"ipps://cups.lan:8631/printers/dymo": "https://cups.lan:8631/printers/dymo",
		"http://cups.lan:631/printers/dymo":  "http://cups.lan:631/printers/dymo",
	} {
		if got, err := ippEndpoint(in); err != nil || got != want {
			t.Errorf("ippEndpoint(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
}
