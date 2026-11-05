package relayd

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"forge.axonibyte.com/axonibyte/bonemesh/go/v3/node"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/relay"
)

// mainTrust is the main node's trust, as pairing would leave it, for one
// relay over WebSocket.
type mainTrust struct{ credential, name string }

func (m mainTrust) Credential(s string) (string, error) {
	if s == m.credential {
		return m.name, nil
	}
	return "", relay.ErrUntrusted
}
func (mainTrust) IdentityKey(string) ([]byte, error) { return nil, relay.ErrUntrusted }
func (mainTrust) Mesh() (node.Config, error)         { return node.Config{}, relay.ErrNotPaired }

// printer is a fake TCP printer keeping each job it takes.
type printer struct {
	l    net.Listener
	mu   sync.Mutex
	jobs [][]byte
}

func newPrinter(t *testing.T) *printer {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p := &printer{l: l}
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			data, _ := io.ReadAll(c)
			c.Close()
			p.mu.Lock()
			p.jobs = append(p.jobs, data)
			p.mu.Unlock()
		}
	}()
	t.Cleanup(func() { l.Close() })
	return p
}

func (p *printer) addr() string { return p.l.Addr().String() }

func (p *printer) got() [][]byte {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([][]byte{}, p.jobs...)
}

// setup is a main node bus serving WebSocket and renewals, and a relay
// paired with it, allowed to reach allow, running until the test ends.
type setup struct {
	main    *relay.Bus
	relay   *Relay
	state   string
	renewed atomic.Int32
	cancel  context.CancelFunc
	done    chan struct{}
}

func start(t *testing.T, allow []string, expires time.Time) *setup {
	t.Helper()
	rootPublic, rootPrivate := relay.NewMeshRoot()
	public, private := relay.NewIdentity()
	issue := func(until time.Time) json.RawMessage {
		c, err := relay.IssueCertificate(rootPrivate, "bam-test", "r1", public, time.Now().Add(-time.Hour), until)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(c)
		return raw
	}
	s := &setup{}
	credential := relay.NewCredential()
	s.main = relay.New(relay.Options{Name: relay.Main, Trust: mainTrust{credential, "r1"}, Retry: 100 * time.Millisecond})
	t.Cleanup(s.main.Close)
	mux := http.NewServeMux()
	mux.HandleFunc(relay.LivePath, s.main.ServeWebSocket)
	mux.HandleFunc(relay.RenewPath, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != relay.CredentialScheme+" "+credential {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		s.renewed.Add(1)
		until := time.Now().Add(7 * 24 * time.Hour).UTC()
		_ = json.NewEncoder(w).Encode(relay.Issued{Name: "r1", Mesh: "bam-test", RootPublic: rootPublic, Certificate: issue(until), ExpiresAt: until, WebSocket: true})
	})
	ts := httptest.NewServer(mux)
	t.Cleanup(func() {
		ts.CloseClientConnections()
		ts.Close()
	})

	p := &relay.Pairing{Main: ts.URL, Issued: relay.Issued{Name: "r1", Mesh: "bam-test", RootPublic: rootPublic, Certificate: issue(expires),
		ExpiresAt: expires, Credential: credential, WebSocket: true}, IdentityPublic: public, IdentityPrivate: private}
	s.state = filepath.Join(t.TempDir(), "relay.json")
	if err := p.Save(s.state); err != nil {
		t.Fatal(err)
	}
	a, err := ParseAllowlist(allow, nil)
	if err != nil {
		t.Fatal(err)
	}
	s.relay, err = New(Config{Pairing: p, State: s.state, Version: "v-test", WebSocket: true, BoneMesh: true, Allow: a,
		Transport: printers.NewDispatcher(nil), Client: ts.Client(), Retry: 100 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel, s.done = cancel, make(chan struct{})
	go func() {
		defer close(s.done)
		if err := s.relay.Run(ctx); err != nil {
			t.Error(err)
		}
	}()
	t.Cleanup(func() {
		cancel()
		<-s.done
	})
	waitFor(t, "the relay to link", func() bool { return len(s.main.Transports("r1")) == 1 })
	return s
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// job asks the relay to deliver data, and reads its answer.
func (s *setup) job(t *testing.T, job relay.Job) relay.JobResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	reply, err := s.main.Request(ctx, "r1", relay.TypeJob, job)
	if err != nil {
		t.Fatal(err)
	}
	var res relay.JobResult
	if err := reply.Decode(&res); err != nil {
		t.Fatal(err)
	}
	return res
}

func TestARelayPrintsWithinItsAllowlist(t *testing.T) {
	ok, outside := newPrinter(t), newPrinter(t)
	s := start(t, []string{ok.addr()}, time.Now().Add(7*24*time.Hour))

	if res := s.job(t, relay.Job{Destination: relay.Destination{Type: "tcp", Address: ok.addr()}, Data: []byte("^XA^XZ")}); !res.OK {
		t.Fatalf("the job failed: %s", res.Reason)
	}
	waitFor(t, "the printer to take the job", func() bool { return len(ok.got()) == 1 })
	if string(ok.got()[0]) != "^XA^XZ" {
		t.Fatalf("the printer took %q", ok.got()[0])
	}

	// The main node asking does not widen the allowlist.
	res := s.job(t, relay.Job{Destination: relay.Destination{Type: "tcp", Address: outside.addr()}, Data: []byte("scan")})
	if res.OK || !strings.Contains(res.Reason, "outside this relay's allowlist") {
		t.Fatalf("a printer outside the allowlist: %+v", res)
	}
	if len(outside.got()) != 0 {
		t.Fatal("a printer outside the allowlist was reached")
	}
	// Nor reach a device, with none listed.
	res = s.job(t, relay.Job{Destination: relay.Destination{Type: "device", Address: "/dev/usb/lp0"}, Data: []byte("x")})
	if res.OK {
		t.Fatal("a device was written to with none allowed")
	}
}

func TestAJobInPieces(t *testing.T) {
	p := newPrinter(t)
	s := start(t, []string{p.addr()}, time.Now().Add(7*24*time.Hour))
	dest := relay.Destination{Type: "tcp", Address: p.addr()}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	for i, part := range []string{"one ", "two "} {
		if _, err := s.main.Send(ctx, "r1", relay.TypeJob, relay.Job{Destination: dest, Data: []byte(part), Piece: &relay.JobPiece{Of: "j1", I: i, N: 3}}); err != nil {
			t.Fatal(err)
		}
	}
	if res := s.job(t, relay.Job{Destination: dest, Data: []byte("three"), Piece: &relay.JobPiece{Of: "j1", I: 2, N: 3}}); !res.OK {
		t.Fatalf("the job failed: %s", res.Reason)
	}
	waitFor(t, "the whole job", func() bool { return len(p.got()) == 1 })
	if string(p.got()[0]) != "one two three" {
		t.Fatalf("the printer took %q", p.got()[0])
	}
}

func TestARelayReportsItsStatus(t *testing.T) {
	p := newPrinter(t)
	s := start(t, []string{p.addr(), "windows:Zebra*"}, time.Now().Add(7*24*time.Hour))
	// Asked, it says which destinations it can reach.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	closed, _ := net.Listen("tcp", "127.0.0.1:0")
	gone := closed.Addr().String()
	closed.Close()
	reply, err := s.main.Request(ctx, "r1", relay.TypeStatus, relay.StatusRequest{Destinations: []relay.Destination{
		{Type: "tcp", Address: p.addr()}, {Type: "tcp", Address: gone}, {Type: "tcp", Address: "192.168.99.1:9100"}}})
	if err != nil {
		t.Fatal(err)
	}
	var st relay.Status
	if err := reply.Decode(&st); err != nil {
		t.Fatal(err)
	}
	if st.Version != "v-test" || st.Protocol != relay.Version || len(st.Allowlist) != 2 || len(st.Destinations) != 3 {
		t.Fatalf("status: %+v", st)
	}
	if !st.Destinations[0].Reachable || st.Destinations[1].Reachable || st.Destinations[2].Reachable ||
		!strings.Contains(st.Destinations[2].Reason, "allowlist") {
		t.Fatalf("reachability: %+v", st.Destinations)
	}
}

func TestARelayRenewsBeforeItsCertificateExpires(t *testing.T) {
	saved := renewBefore
	renewBefore = 3 * 24 * time.Hour
	t.Cleanup(func() { renewBefore = saved })
	// A day left: it renews as it starts, and keeps the new pairing.
	s := start(t, nil, time.Now().Add(24*time.Hour))
	if s.renewed.Load() != 1 {
		t.Fatalf("renewed %d times", s.renewed.Load())
	}
	kept, err := relay.LoadPairing(s.state)
	if err != nil {
		t.Fatal(err)
	}
	if time.Until(kept.ExpiresAt) < 6*24*time.Hour || kept.Credential == "" {
		t.Fatalf("kept: until %s", kept.ExpiresAt)
	}
	if !bytes.Equal(kept.IdentityPrivate, s.relay.current().IdentityPrivate) {
		t.Fatal("the renewed pairing lost the relay's key")
	}
}

func TestARelayNeedsATransport(t *testing.T) {
	p := &relay.Pairing{Issued: relay.Issued{Name: "r1", WebSocket: true}}
	r, err := New(Config{Pairing: p, Transport: printers.NewDispatcher(nil)})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Run(context.Background()); err == nil || !strings.Contains(err.Error(), "no transport is on") {
		t.Fatalf("with WebSocket offered but not taken: %v", err)
	}
	if _, err := New(Config{Transport: printers.NewDispatcher(nil)}); err == nil {
		t.Fatal("a relay ran unpaired")
	}
}
