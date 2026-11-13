package printers

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeTPRT is a printer that takes jobs after the TPRT handshake, as
// hikalium/sr5900p describes it: UDP and TCP on the same port.
type fakeTPRT struct {
	udp net.PacketConn
	tcp net.Listener

	accepted chan struct{} // closed once a job's TCP connection is taken

	mu        sync.Mutex
	connected bool
	events    []string
	got       []byte // the job's bytes, as they arrive
	want      int    // the job's length: printing until it has all arrived
	err       byte   // the status's error byte
	refuse    bool   // answer 0x0100 with something unexpected
	printing  int    // statuses that still say printing after the job
}

func newFakeTPRT(t *testing.T) *fakeTPRT {
	t.Helper()
	f := &fakeTPRT{accepted: make(chan struct{})}
	// UDP and TCP on one port: try until a free TCP port is free for UDP too.
	for range 20 {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		pc, err := net.ListenPacket("udp", ln.Addr().String())
		if err != nil {
			ln.Close()
			continue
		}
		f.tcp, f.udp = ln, pc
		break
	}
	if f.tcp == nil {
		t.Fatal("no port free for both TCP and UDP")
	}
	t.Cleanup(func() { f.tcp.Close(); f.udp.Close() })
	go f.serveTCP()
	go f.serveUDP()
	return f
}

func (f *fakeTPRT) port() int { return f.tcp.Addr().(*net.TCPAddr).Port }

func (f *fakeTPRT) log(e string) {
	f.mu.Lock()
	f.events = append(f.events, e)
	f.mu.Unlock()
}

func (f *fakeTPRT) Events() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return strings.Join(f.events, " ")
}

func (f *fakeTPRT) serveTCP() {
	for {
		conn, err := f.tcp.Accept()
		if err != nil {
			return
		}
		f.mu.Lock()
		f.connected = true
		f.mu.Unlock()
		close(f.accepted)
		buf := make([]byte, 64<<10)
		for {
			n, err := conn.Read(buf)
			f.mu.Lock()
			f.got = append(f.got, buf[:n]...)
			if err == io.EOF {
				f.events = append(f.events, fmt.Sprintf("job:%d", len(f.got)))
			}
			f.mu.Unlock()
			if err != nil {
				break
			}
		}
		conn.Close()
		return
	}
}

func (f *fakeTPRT) serveUDP() {
	buf := make([]byte, 512)
	for {
		n, addr, err := f.udp.ReadFrom(buf)
		if err != nil {
			return
		}
		if n != 32 || string(buf[:4]) != "TPRT" {
			f.log(fmt.Sprintf("bad:% X", buf[:n]))
			continue
		}
		// The header's words after TPRT: 0, 1, 0x20, the command, 0, 0, 0.
		var words [7]uint32
		for i := range words {
			words[i] = binary.BigEndian.Uint32(buf[4+4*i:])
		}
		if words != [7]uint32{0, 1, 0x20, words[3], 0, 0, 0} {
			f.log(fmt.Sprintf("bad:%v", words))
			continue
		}
		cmd := words[3]
		if cmd == 0x0101 {
			// The data stream is announced once its connection is taken.
			select {
			case <-f.accepted:
				f.log("connected")
			case <-time.After(time.Second):
			}
		}
		f.mu.Lock()
		var data []byte
		switch cmd {
		case 1:
			f.events = append(f.events, "status")
			data = make([]byte, 20)
			data[0], data[2], data[3] = 20, f.err, 4
			switch {
			case f.connected && len(f.got) < f.want:
				data[1] = 2
			case f.connected && f.printing > 0:
				f.printing--
				data[1] = 2
			}
		case 2, 3:
			f.events = append(f.events, map[uint32]string{2: "start", 3: "stop"}[cmd])
			data = []byte{byte(cmd), 0, 0}
		case 0x0101:
			f.events = append(f.events, "notify")
			data = []byte{0}
		case 0x0100:
			f.events = append(f.events, "data")
			data = []byte{0x10}
			if f.refuse {
				data = []byte{0x7F}
			}
		}
		f.mu.Unlock()
		answer := append([]byte("tprt"), buf[4:32]...)
		_, _ = f.udp.WriteTo(append(answer, data...), addr)
	}
}

// tprtEvents says whether a fake's events are a whole job: the handshake,
// at least statuses asks for status after it, then the job stopped and its
// connection closed, in either order.
func tprtEvents(got string, statuses int, job string) bool {
	e := strings.Fields(got)
	if len(e) < 7 || strings.Join(e[:5], " ") != "status start connected notify data" {
		return false
	}
	rest := e[5:]
	n := 0
	for n < len(rest) && rest[n] == "status" {
		n++
	}
	end := strings.Join(rest[n:], " ")
	return n >= statuses && (end == "stop "+job || end == job+" stop")
}

func quickTPRT() *Dispatcher {
	d := NewDispatcher(nil)
	d.tprtTiming = &TPRTTiming{Pause: time.Millisecond, Poll: time.Millisecond, Reply: time.Second}
	return d
}

func tprtConn(port int) Connection {
	return Connection{Type: ConnTCP, Address: net.JoinHostPort("127.0.0.1", strconv.Itoa(port)) + "?protocol=tprt"}
}

func TestTPRTHandshakeComesBeforeTheJob(t *testing.T) {
	f := newFakeTPRT(t)
	job := bytes.Repeat([]byte("raster"), 40000)
	f.mu.Lock()
	f.want, f.printing = len(job), 2
	f.mu.Unlock()
	if err := quickTPRT().Send(context.Background(), tprtConn(f.port()), job); err != nil {
		t.Fatal(err)
	}
	// The status is asked until the printer stops printing: while the job
	// arrives, then twice more, and once to find it done.
	if got := f.Events(); !tprtEvents(got, 3, fmt.Sprintf("job:%d", len(job))) {
		t.Errorf("events %s", got)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if !bytes.Equal(f.got, job) {
		t.Error("the job arrived changed")
	}
}

func TestTPRTRefusals(t *testing.T) {
	// A printer with no tape is told nothing more.
	f := newFakeTPRT(t)
	f.mu.Lock()
	f.err = tprtErrNoTape
	f.mu.Unlock()
	if err := quickTPRT().Send(context.Background(), tprtConn(f.port()), []byte("job")); !errors.Is(err, ErrUnreachable) || !strings.Contains(err.Error(), "has no tape") {
		t.Errorf("no tape: %v", err)
	}
	if got := f.Events(); got != "status" {
		t.Errorf("no tape: events %s", got)
	}

	// One that refuses the data stream gets no job, and its job is stopped.
	f = newFakeTPRT(t)
	f.mu.Lock()
	f.refuse = true
	f.mu.Unlock()
	if err := quickTPRT().Send(context.Background(), tprtConn(f.port()), []byte("job")); err == nil || !strings.Contains(err.Error(), "command 0x0100") {
		t.Errorf("refused: %v", err)
	}
	if got := f.Events(); !tprtEvents(got, 0, "job:0") {
		t.Errorf("refused: events %s", got)
	}

	// One that never answers fails.
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer pc.Close()
	d := quickTPRT()
	d.tprtTiming.Reply = 50 * time.Millisecond
	if err := d.Send(context.Background(), tprtConn(pc.LocalAddr().(*net.UDPAddr).Port), []byte("job")); err == nil || !strings.Contains(err.Error(), "did not answer") {
		t.Errorf("silent: %v", err)
	}
}

// TestPlainTCPHasNoHandshake checks that a tcp connection naming no
// protocol sends the job alone, as every other printer takes it.
func TestPlainTCPHasNoHandshake(t *testing.T) {
	f := newFakeTPRT(t)
	if err := quickTPRT().Send(context.Background(), local(f.port()), []byte("job")); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for f.Events() != "job:3" && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if got := f.Events(); got != "job:3" {
		t.Errorf("events %s", got)
	}
}

func TestTCPProtocols(t *testing.T) {
	for in, want := range map[string]string{
		"lw.lan?protocol=tprt":         "lw.lan:9100?protocol=tprt",
		"10.0.0.5:9101?protocol=tprt":  "10.0.0.5:9101?protocol=tprt",
		"[fd00::5]:9100?protocol=tprt": "[fd00::5]:9100?protocol=tprt",
		"lw.lan":                       "lw.lan:9100",
	} {
		c, err := NormalizeConnection(Connection{Type: ConnTCP, Address: in})
		if err != nil || c.Address != want {
			t.Errorf("%q: %q %v", in, c.Address, err)
		}
		if got := TCPHostPort(c.Address); strings.Contains(got, "?") {
			t.Errorf("%q: host and port %q", in, got)
		}
	}
	for _, bad := range []string{"lw.lan?protocol=niimbot", "lw.lan?protocol=tprt&x=1", "lw.lan?handshake=tprt", "lw.lan:9100?"} {
		if _, err := NormalizeConnection(Connection{Type: ConnTCP, Address: bad}); !errors.Is(err, ErrInvalidPrinter) {
			t.Errorf("%q: %v", bad, err)
		}
	}
	lw := Settings{Model: "LW-PX800", Media: "lk-12"}
	if err := Validate("Tape", DriverEpsonLabelWorks, Connection{Type: ConnTCP, Address: "lw.lan?protocol=tprt"}, lw); err != nil {
		t.Errorf("LabelWorks over TPRT: %v", err)
	}
	zpl := Settings{Model: "Zebra GK420d", Media: "4x6", Options: json.RawMessage(`null`)}
	if err := Validate("Zebra", DriverZPL, Connection{Type: ConnTCP, Address: "zebra.lan"}, zpl); err != nil {
		t.Fatalf("a Zebra: %v", err)
	}
	if err := Validate("Zebra", DriverZPL, Connection{Type: ConnTCP, Address: "zebra.lan?protocol=tprt"}, zpl); !errors.Is(err, ErrInvalidPrinter) || !strings.Contains(err.Error(), "takes no ?protocol=tprt") {
		t.Errorf("a Zebra over TPRT: %v", err)
	}
}
