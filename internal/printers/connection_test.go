package printers

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalizeConnection(t *testing.T) {
	for _, c := range []struct {
		in   Connection
		want string // normalized address, or "" when invalid
	}{
		{Connection{ConnTCP, "192.168.1.40"}, "192.168.1.40:9100"},
		{Connection{ConnTCP, " ql.lan:9101 "}, "ql.lan:9101"},
		{Connection{ConnTCP, "fd00::40"}, "[fd00::40]:9100"},
		{Connection{ConnTCP, "[fd00::40]:515"}, "[fd00::40]:515"},
		{Connection{ConnTCP, "tcp://10.0.0.1"}, ""},
		{Connection{ConnTCP, "10.0.0.1:70000"}, ""},
		{Connection{ConnTCP, "bad host"}, ""},
		{Connection{ConnDevice, "/dev/usb/../usb/lp0"}, "/dev/usb/lp0"},
		{Connection{ConnDevice, "dev/usb/lp0"}, ""},
		{Connection{ConnDevice, "/dev/usb/lp0\x00"}, ""},
		{Connection{ConnIPP, "ipp://cups.lan/printers/dymo"}, "ipp://cups.lan/printers/dymo"},
		{Connection{ConnIPP, "https://print.example/ipp/print"}, "https://print.example/ipp/print"},
		{Connection{ConnIPP, "ftp://cups.lan/printers/dymo"}, ""},
		{Connection{ConnIPP, "ipp://user:pw@cups.lan/printers/dymo"}, ""},
		{Connection{ConnIPP, "/printers/dymo"}, ""},
		{Connection{ConnWindows, `\\desk-pc\DYMO LabelWriter 450`}, `\\desk-pc\DYMO LabelWriter 450`},
		{Connection{ConnWindows, ""}, ""},
		{Connection{"usb", "/dev/usb/lp0"}, ""},
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
