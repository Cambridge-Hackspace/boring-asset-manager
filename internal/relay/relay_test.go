package relay

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"forge.axonibyte.com/axonibyte/bonemesh/go/v3/node"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

const testMesh = "bam-test"

// testTrust is pairing as a test sets it up: credentials and identity
// keys it trusts, and the node's own BoneMesh identity.
type testTrust struct {
	mu    sync.Mutex
	creds map[string]string
	keys  map[string][]byte
	mesh  *node.Config
}

func newTestTrust() *testTrust {
	return &testTrust{creds: map[string]string{}, keys: map[string][]byte{}}
}

func (t *testTrust) Credential(secret string) (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if name, ok := t.creds[secret]; ok {
		return name, nil
	}
	return "", ErrUntrusted
}

func (t *testTrust) IdentityKey(name string) ([]byte, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if key, ok := t.keys[name]; ok {
		return key, nil
	}
	return nil, ErrUntrusted
}

func (t *testTrust) Mesh() (node.Config, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.mesh == nil {
		return node.Config{}, ErrNotPaired
	}
	return *t.mesh, nil
}

// revoke stops trusting a relay, on both transports.
func (t *testTrust) revoke(name string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.keys, name)
	for secret, n := range t.creds {
		if n == name {
			delete(t.creds, secret)
		}
	}
}

// world is a mesh root and the nodes paired under it, as pairing would
// leave them: the main node trusting each relay, and each relay the main
// node.
type world struct {
	t                     *testing.T
	rootPublic, rootPriv  []byte
	main                  *testTrust
	mainKey               []byte
	credentials, relayIDs map[string]string
}

func newWorld(t *testing.T) *world {
	t.Helper()
	pub, priv := NewMeshRoot()
	w := &world{t: t, rootPublic: pub, rootPriv: priv, main: newTestTrust(), credentials: map[string]string{}}
	cfg, key := w.identity(Main, time.Now().Add(-time.Hour), time.Now().Add(time.Hour), priv)
	w.main.mesh, w.mainKey = &cfg, key
	return w
}

// identity issues a node an identity and a certificate, signed by root.
func (w *world) identity(label string, from, until time.Time, root []byte) (node.Config, []byte) {
	w.t.Helper()
	pub, priv := NewIdentity()
	c, err := IssueCertificate(root, testMesh, label, pub, from, until)
	if err != nil {
		w.t.Fatal(err)
	}
	return node.Config{Label: label, Mesh: testMesh, RootPublic: w.rootPublic, Cert: c, IDPrivate: priv}, pub
}

// pair pairs a relay: its credential and key trusted by the main node,
// and its trust holding the main node's key and its own identity.
func (w *world) pair(name string) *testTrust {
	w.t.Helper()
	cfg, key := w.identity(name, time.Now().Add(-time.Hour), time.Now().Add(time.Hour), w.rootPriv)
	return w.trustFor(name, cfg, key)
}

func (w *world) trustFor(name string, cfg node.Config, key []byte) *testTrust {
	secret := "cred-" + name + "-" + newID()
	w.main.mu.Lock()
	w.main.creds[secret], w.main.keys[name] = name, key
	w.main.mu.Unlock()
	w.credentials[name] = secret
	tr := newTestTrust()
	tr.keys[Main] = w.mainKey
	tr.mesh = &cfg
	return tr
}

// quick makes the bus's own timings short, for the test.
func quick(t *testing.T) {
	saved := holdKeep
	holdKeep = 100 * time.Millisecond
	t.Cleanup(func() { holdKeep = saved })
}

// transports says which transports a node turns on.
type transports struct{ ws, bm bool }

func (tr transports) String() string {
	switch {
	case tr.ws && tr.bm:
		return "both"
	case tr.bm:
		return "bonemesh"
	}
	return "websocket"
}

var each = []transports{{ws: true}, {bm: true}, {ws: true, bm: true}}

// mainNode is a main node's server under test.
type mainNode struct {
	bus  *Bus
	ts   *httptest.Server
	mesh *Mesh
}

func startMain(t *testing.T, w *world, on transports, cluster Cluster, server string) *mainNode {
	t.Helper()
	b := New(Options{Name: Main, Server: server, Trust: w.main, Cluster: cluster, Retry: 200 * time.Millisecond, Poll: 40 * time.Millisecond, Timeout: 20 * time.Second})
	m := &mainNode{bus: b}
	if on.ws {
		m.ts = httptest.NewServer(http.HandlerFunc(b.ServeWebSocket))
	}
	if on.bm {
		mesh, err := StartBoneMesh(b, BoneMeshOptions{})
		if err != nil {
			t.Fatal(err)
		}
		m.mesh = mesh
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		b.Run(ctx)
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
		if m.mesh != nil {
			m.mesh.Close()
		}
		if m.ts != nil {
			m.ts.CloseClientConnections()
			m.ts.Close()
		}
	})
	return m
}

// relayNode is a relay under test.
type relayNode struct {
	bus    *Bus
	mesh   *Mesh
	cancel context.CancelFunc
}

// startRelay starts a relay linked to the main node over on, and waits
// until both ends see every link.
func startRelay(t *testing.T, w *world, name string, trust *testTrust, m *mainNode, on transports) *relayNode {
	t.Helper()
	r := dialRelay(t, w, name, trust, m, on)
	waitFor(t, "links between "+name+" and the main node", func() bool {
		return len(r.bus.Transports(Main)) == count(on) && len(m.bus.Transports(name)) == count(on)
	})
	return r
}

// dialRelay starts a relay dialing the main node, without waiting.
func dialRelay(t *testing.T, w *world, name string, trust *testTrust, m *mainNode, on transports) *relayNode {
	t.Helper()
	b := New(Options{Name: name, Trust: trust, Retry: 200 * time.Millisecond, Poll: 40 * time.Millisecond, Timeout: 20 * time.Second})
	ctx, cancel := context.WithCancel(context.Background())
	r := &relayNode{bus: b, cancel: cancel}
	var wg sync.WaitGroup
	if on.ws {
		wg.Add(1)
		go func() {
			defer wg.Done()
			DialWebSocket(ctx, b, m.ts.URL, w.credentials[name])
		}()
	}
	if on.bm {
		mesh, err := StartBoneMesh(b, BoneMeshOptions{Dial: "127.0.0.1:" + strconv.Itoa(m.mesh.Port())})
		if err != nil {
			t.Fatal(err)
		}
		r.mesh = mesh
	}
	t.Cleanup(func() {
		cancel()
		if r.mesh != nil {
			r.mesh.Close()
		}
		b.Close()
		wg.Wait()
	})
	return r
}

func count(on transports) int {
	n := 0
	if on.ws {
		n++
	}
	if on.bm {
		n++
	}
	return n
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

// printer is a relay's job handler, keeping what it was sent.
type printer struct {
	mu   sync.Mutex
	jobs []Job
	b    *Bus
}

func (p *printer) handle(ctx context.Context, m Message) {
	var job Job
	if err := m.Decode(&job); err != nil {
		_ = p.b.Reply(ctx, m, TypeJobResult, JobResult{Reason: err.Error()})
		return
	}
	p.mu.Lock()
	p.jobs = append(p.jobs, job)
	p.mu.Unlock()
	_ = p.b.Reply(ctx, m, TypeJobResult, JobResult{OK: true})
}

func (p *printer) got() []Job {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]Job(nil), p.jobs...)
}

func addPrinter(b *Bus) *printer {
	p := &printer{b: b}
	b.Handle(TypeJob, p.handle)
	return p
}

// sendJob asks a relay to deliver data, and reads its answer.
func sendJob(t *testing.T, from *Bus, to string, data []byte) JobResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	reply, err := from.Request(ctx, to, TypeJob, Job{Destination: Destination{Type: "tcp", Address: "192.168.1.40:9100"}, Data: data})
	if err != nil {
		t.Fatalf("job to %s: %v", to, err)
	}
	if reply.Type != TypeJobResult {
		t.Fatalf("a job was answered with %s", reply.Type)
	}
	var res JobResult
	if err := reply.Decode(&res); err != nil {
		t.Fatal(err)
	}
	return res
}

func randomBytes(n int) []byte {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return b
}

// The protocol suite runs over each transport, and over both at once.

func TestJobRoundTrip(t *testing.T) {
	for _, on := range each {
		t.Run(on.String(), func(t *testing.T) {
			w := newWorld(t)
			m := startMain(t, w, on, nil, "")
			r := startRelay(t, w, "r1", w.pair("r1"), m, on)
			p := addPrinter(r.bus)

			if res := sendJob(t, m.bus, "r1", []byte("^XA^FDhello^XZ")); !res.OK {
				t.Fatalf("the job failed: %s", res.Reason)
			}
			if jobs := p.got(); len(jobs) != 1 || string(jobs[0].Data) != "^XA^FDhello^XZ" || jobs[0].Destination.Address != "192.168.1.40:9100" {
				t.Fatalf("the relay got %+v", jobs)
			}

			// And the other way: a relay's status reaches the main node.
			got := make(chan Status, 1)
			m.bus.Handle(TypeStatus, func(_ context.Context, msg Message) {
				var s Status
				if msg.From == "r1" && msg.Decode(&s) == nil {
					got <- s
				}
			})
			if _, err := r.bus.Send(context.Background(), Main, TypeStatus, Status{Version: "dev", Protocol: Version, Allowlist: []string{"192.168.1.0/24:9100"}}); err != nil {
				t.Fatal(err)
			}
			select {
			case s := <-got:
				if s.Protocol != Version || len(s.Allowlist) != 1 {
					t.Fatalf("status arrived as %+v", s)
				}
			case <-time.After(10 * time.Second):
				t.Fatal("the status never arrived")
			}
		})
	}
}

func TestLargeJobsInParts(t *testing.T) {
	for _, on := range each {
		t.Run(on.String(), func(t *testing.T) {
			w := newWorld(t)
			m := startMain(t, w, on, nil, "")
			r := startRelay(t, w, "r1", w.pair("r1"), m, on)
			p := addPrinter(r.bus)

			// Base64 of a megabyte is 85 parts.
			data := randomBytes(1 << 20)
			if res := sendJob(t, m.bus, "r1", data); !res.OK {
				t.Fatalf("the job failed: %s", res.Reason)
			}
			jobs := p.got()
			if len(jobs) != 1 || !bytes.Equal(jobs[0].Data, data) {
				t.Fatalf("the relay got %d jobs, not the megabyte sent", len(jobs))
			}
		})
	}
}

func TestDuplicatesDeliveredOnce(t *testing.T) {
	for _, on := range each {
		t.Run(on.String(), func(t *testing.T) {
			w := newWorld(t)
			m := startMain(t, w, on, nil, "")
			r := startRelay(t, w, "r1", w.pair("r1"), m, on)
			var delivered atomic.Int32
			r.bus.Handle(TypeEvent, func(context.Context, Message) { delivered.Add(1) })

			// One message, sent three times over every link it has, as
			// retries and failover send it.
			msg, err := m.bus.message("r1", TypeEvent, "", map[string]string{"kind": "test"})
			if err != nil {
				t.Fatal(err)
			}
			data, _ := json.Marshal(msg)
			var links []link
			m.bus.mu.Lock()
			for _, l := range m.bus.links["r1"] {
				links = append(links, l)
			}
			m.bus.mu.Unlock()
			for range 3 {
				for _, l := range links {
					if err := l.send(data); err != nil {
						t.Fatal(err)
					}
				}
			}
			// And through Send, which waits for its ack.
			if _, err := m.bus.Send(context.Background(), "r1", TypeEvent, map[string]string{"kind": "after"}); err != nil {
				t.Fatal(err)
			}
			time.Sleep(300 * time.Millisecond)
			if n := delivered.Load(); n != 2 {
				t.Fatalf("two messages were delivered %d times", n)
			}
		})
	}
}

func TestDroppedLinkFailsOver(t *testing.T) {
	both := transports{ws: true, bm: true}

	t.Run("bonemesh drops", func(t *testing.T) {
		w := newWorld(t)
		m := startMain(t, w, both, nil, "")
		r := startRelay(t, w, "r1", w.pair("r1"), m, both)
		p := addPrinter(r.bus)
		r.mesh.Close()
		if res := sendJob(t, m.bus, "r1", []byte("over WebSocket")); !res.OK {
			t.Fatal(res.Reason)
		}
		if len(p.got()) != 1 {
			t.Fatal("the job did not arrive once")
		}
	})

	t.Run("websocket drops", func(t *testing.T) {
		w := newWorld(t)
		m := startMain(t, w, both, nil, "")
		r := startRelay(t, w, "r1", w.pair("r1"), m, both)
		p := addPrinter(r.bus)
		// The relay stops dialing, and its connection is cut.
		r.cancel()
		m.ts.CloseClientConnections()
		if res := sendJob(t, m.bus, "r1", []byte("over BoneMesh")); !res.OK {
			t.Fatal(res.Reason)
		}
		if len(p.got()) != 1 {
			t.Fatal("the job did not arrive once")
		}
	})

	t.Run("a link goes quiet", func(t *testing.T) {
		w := newWorld(t)
		m := startMain(t, w, both, nil, "")
		r := startRelay(t, w, "r1", w.pair("r1"), m, both)
		p := addPrinter(r.bus)
		// BoneMesh, preferred, takes messages and loses them; the retry
		// goes over WebSocket.
		m.bus.mu.Lock()
		m.bus.links["r1"][BoneMesh] = blackHole{}
		m.bus.mu.Unlock()
		if res := sendJob(t, m.bus, "r1", []byte("over WebSocket")); !res.OK {
			t.Fatal(res.Reason)
		}
		if len(p.got()) != 1 {
			t.Fatal("the job did not arrive once")
		}
	})
}

// blackHole takes messages and loses them.
type blackHole struct{}

func (blackHole) send([]byte) error { return nil }
func (blackHole) close(string)      {}
func (blackHole) check() error      { return nil }

func TestRoutingBetweenTransports(t *testing.T) {
	w := newWorld(t)
	m := startMain(t, w, transports{ws: true, bm: true}, nil, "")
	onWS := startRelay(t, w, "front-desk", w.pair("front-desk"), m, transports{ws: true})
	onMesh := startRelay(t, w, "workshop", w.pair("workshop"), m, transports{bm: true})
	pw, pf := addPrinter(onMesh.bus), addPrinter(onWS.bus)

	if res := sendJob(t, onWS.bus, "workshop", []byte("from the front desk")); !res.OK {
		t.Fatal(res.Reason)
	}
	if res := sendJob(t, onMesh.bus, "front-desk", []byte("from the workshop")); !res.OK {
		t.Fatal(res.Reason)
	}
	if len(pw.got()) != 1 || len(pf.got()) != 1 {
		t.Fatalf("each relay should have one job: workshop %d, front desk %d", len(pw.got()), len(pf.got()))
	}

	// A relay that is not connected is said to be, at once.
	_, err := onWS.bus.Send(context.Background(), "cellar", TypeJob, Job{})
	var refused *Refused
	if !errors.As(err, &refused) || !strings.Contains(refused.Reason, "cellar is not connected") {
		t.Fatalf("a send to a relay not connected ended with %v", err)
	}
}

func TestUntrustedRelaysAreRefused(t *testing.T) {
	quick(t)

	t.Run("websocket: an unknown credential", func(t *testing.T) {
		w := newWorld(t)
		m := startMain(t, w, transports{ws: true}, nil, "")
		b := New(Options{Name: "r1", Trust: newTestTrust()})
		defer b.Close()
		err := dialWebSocket(context.Background(), b, m.ts.URL, "not-a-credential")
		if err == nil || !strings.Contains(err.Error(), "401") {
			t.Fatalf("an unknown credential was answered %v", err)
		}
	})

	t.Run("websocket: a revoked credential", func(t *testing.T) {
		w := newWorld(t)
		m := startMain(t, w, transports{ws: true}, nil, "")
		startRelay(t, w, "r1", w.pair("r1"), m, transports{ws: true})
		secret := w.credentials["r1"]
		w.main.revoke("r1")
		waitFor(t, "the revoked relay's link to close", func() bool { return len(m.bus.Transports("r1")) == 0 })
		b := New(Options{Name: "r1", Trust: newTestTrust()})
		defer b.Close()
		if err := dialWebSocket(context.Background(), b, m.ts.URL, secret); err == nil || !strings.Contains(err.Error(), "401") {
			t.Fatalf("a revoked credential was answered %v", err)
		}
	})

	refusedOverMesh := func(t *testing.T, w *world, m *mainNode, trust *testTrust) {
		t.Helper()
		r := dialRelay(t, w, "r1", trust, m, transports{bm: true})
		time.Sleep(1500 * time.Millisecond)
		if got := m.bus.Transports("r1"); len(got) != 0 {
			t.Fatalf("the main node linked to the relay over %v", got)
		}
		got := make(chan Message, 1)
		m.bus.Handle(TypeStatus, func(_ context.Context, msg Message) { got <- msg })
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if _, err := r.bus.Send(ctx, Main, TypeStatus, Status{}); err == nil {
			t.Fatal("the relay's message was acknowledged")
		}
		select {
		case <-got:
			t.Fatal("the relay's message was delivered")
		default:
		}
	}

	t.Run("bonemesh: an expired certificate", func(t *testing.T) {
		w := newWorld(t)
		m := startMain(t, w, transports{bm: true}, nil, "")
		cfg, key := w.identity("r1", time.Now().Add(-2*time.Hour), time.Now().Add(-time.Hour), w.rootPriv)
		refusedOverMesh(t, w, m, w.trustFor("r1", cfg, key))
	})

	t.Run("bonemesh: a foreign certificate", func(t *testing.T) {
		w := newWorld(t)
		m := startMain(t, w, transports{bm: true}, nil, "")
		_, foreign := NewMeshRoot()
		cfg, key := w.identity("r1", time.Now().Add(-time.Hour), time.Now().Add(time.Hour), foreign)
		refusedOverMesh(t, w, m, w.trustFor("r1", cfg, key))
	})

	t.Run("bonemesh: a revoked relay", func(t *testing.T) {
		w := newWorld(t)
		m := startMain(t, w, transports{bm: true}, nil, "")
		trust := w.pair("r1")
		w.main.revoke("r1")
		refusedOverMesh(t, w, m, trust)
	})

	t.Run("bonemesh: a relay speaking as another", func(t *testing.T) {
		w := newWorld(t)
		m := startMain(t, w, transports{bm: true}, nil, "")
		startRelay(t, w, "victim", w.pair("victim"), m, transports{bm: true})
		r := startRelay(t, w, "liar", w.pair("liar"), m, transports{bm: true})
		got := make(chan Message, 1)
		m.bus.Handle(TypeStatus, func(_ context.Context, msg Message) { got <- msg })
		msg := Message{V: Version, ID: newID(), Type: TypeStatus, From: "victim", To: Main, Body: json.RawMessage(`{}`)}
		data, _ := json.Marshal(msg)
		r.bus.mu.Lock()
		l := r.bus.links[Main][BoneMesh]
		r.bus.mu.Unlock()
		if err := l.send(data); err != nil {
			t.Fatal(err)
		}
		select {
		case <-got:
			t.Fatal("a message from one relay was taken as another's")
		case <-time.After(500 * time.Millisecond):
		}
	})
}

func TestSeveralServers(t *testing.T) {
	quick(t)
	st, err := store.OpenMigratedSQLite(filepath.Join(t.TempDir(), "bam.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	w := newWorld(t)
	on := transports{ws: true, bm: true}
	a := startMain(t, w, on, st, "a")
	b := startMain(t, w, on, st, "b")
	r := startRelay(t, w, "r1", w.pair("r1"), a, on)
	p := addPrinter(r.bus)
	waitFor(t, "server a to claim the relay", func() bool {
		holder, err := st.RelayHolder("r1", time.Now().Add(-time.Minute))
		return err == nil && holder == "a"
	})

	// Server b reaches the relay through server a, and the relay's answer
	// finds its way back to b.
	if res := sendJob(t, b.bus, "r1", []byte("through a")); !res.OK {
		t.Fatal(res.Reason)
	}
	if len(p.got()) != 1 {
		t.Fatal("the job did not arrive once")
	}

	// Large ones too, part by part.
	data := randomBytes(200 << 10)
	if res := sendJob(t, b.bus, "r1", data); !res.OK {
		t.Fatal(res.Reason)
	}
	if jobs := p.got(); len(jobs) != 2 || !bytes.Equal(jobs[1].Data, data) {
		t.Fatal("the large job did not arrive whole")
	}

	// A relay neither holds is not connected.
	_, err = b.bus.Send(context.Background(), "cellar", TypeJob, Job{})
	var refused *Refused
	if !errors.As(err, &refused) {
		t.Fatalf("a send to a relay no server holds ended with %v", err)
	}
}

func TestSeveralServersTakeOver(t *testing.T) {
	quick(t)
	st, err := store.OpenMigratedSQLite(filepath.Join(t.TempDir(), "bam.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	// A relay that dials again reaches whichever server it reaches, which
	// takes it; the server it left closes its link.
	w := newWorld(t)
	a := startMain(t, w, transports{ws: true}, st, "a")
	b := startMain(t, w, transports{ws: true}, st, "b")
	trust := w.pair("r1")
	// The relay's first link, to a, is dialed once, and not again.
	first := New(Options{Name: "r1", Trust: trust})
	defer first.Close()
	go func() { _ = dialWebSocket(context.Background(), first, a.ts.URL, w.credentials["r1"]) }()
	waitFor(t, "the relay's link to a", func() bool { return len(a.bus.Transports("r1")) == 1 })
	again := startRelay(t, w, "r1", trust, b, transports{ws: true})
	p := addPrinter(again.bus)
	waitFor(t, "server a to let the relay go", func() bool { return len(a.bus.Transports("r1")) == 0 })
	if res := sendJob(t, a.bus, "r1", []byte("through b")); !res.OK {
		t.Fatal(res.Reason)
	}
	if len(p.got()) != 1 {
		t.Fatal("the job did not arrive once")
	}
}

func TestVersionsAndTypesAreRefused(t *testing.T) {
	w := newWorld(t)
	m := startMain(t, w, transports{ws: true}, nil, "")
	r := startRelay(t, w, "r1", w.pair("r1"), m, transports{ws: true})

	// A type the relay does not handle is refused, saying so.
	_, err := m.bus.Send(context.Background(), "r1", TypeJob, Job{})
	var refused *Refused
	if !errors.As(err, &refused) || !strings.Contains(refused.Reason, "does not handle job") {
		t.Fatalf("an unhandled type ended with %v", err)
	}

	// A version the main node does not speak is refused, saying which it
	// speaks.
	ack := make(chan error, 1)
	id := newID()
	r.bus.mu.Lock()
	r.bus.acks[id] = ack
	l := r.bus.links[Main][WebSocket]
	r.bus.mu.Unlock()
	data, _ := json.Marshal(Message{V: Version + 1, ID: id, Type: TypeStatus, From: "r1", To: Main})
	if err := l.send(data); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-ack:
		if !errors.As(err, &refused) || !strings.Contains(refused.Reason, "speaks version 1") {
			t.Fatalf("another version was answered %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("another version was not answered")
	}
}

// countingHole takes messages, loses them, and counts them.
type countingHole struct{ sent *atomic.Int32 }

func (c countingHole) send([]byte) error { c.sent.Add(1); return nil }
func (countingHole) close(string)        {}
func (countingHole) check() error        { return nil }

func TestRetriesBackOff(t *testing.T) {
	b := New(Options{Name: Main, Trust: newTestTrust(), Retry: 20 * time.Millisecond})
	defer b.Close()
	var sent atomic.Int32
	b.mu.Lock()
	b.links["r1"] = map[string]link{WebSocket: countingHole{&sent}}
	b.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 700*time.Millisecond)
	defer cancel()
	if _, err := b.Send(ctx, "r1", TypeStatus, Status{}); !errors.Is(err, ErrUnreachable) {
		t.Fatalf("a send never acknowledged ended with %v", err)
	}
	// Waits of 20, 40, 80 and 160 ms, then 160 ms each: about seven sends
	// in 700 ms, where waiting 20 ms each time would send 35.
	if n := sent.Load(); n < 4 || n > 10 {
		t.Errorf("sent %d times in 700 ms", n)
	}
}

func TestNoLinkGivesUp(t *testing.T) {
	b := New(Options{Name: "r1", Retry: 20 * time.Millisecond})
	defer b.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if _, err := b.Send(ctx, Main, TypeStatus, Status{}); !errors.Is(err, ErrUnreachable) {
		t.Fatalf("a send with no link ended with %v", err)
	}
}

func TestBoneMeshNeedsAnIdentity(t *testing.T) {
	b := New(Options{Name: Main})
	defer b.Close()
	if _, err := StartBoneMesh(b, BoneMeshOptions{}); !errors.Is(err, ErrNotPaired) {
		t.Fatalf("BoneMesh without an identity started, or ended with %v", err)
	}

	// A certificate for another node is refused.
	w := newWorld(t)
	b = New(Options{Name: "r1", Trust: w.main})
	defer b.Close()
	if _, err := StartBoneMesh(b, BoneMeshOptions{}); err == nil || !strings.Contains(err.Error(), `not "r1"`) {
		t.Fatalf("a certificate for main started r1's BoneMesh, or ended with %v", err)
	}
}

func TestLiveURL(t *testing.T) {
	for base, want := range map[string]string{
		"https://bam.example.org":         "wss://bam.example.org/api/v1/relay/live",
		"https://bam.example.org/":        "wss://bam.example.org/api/v1/relay/live",
		"http://localhost:8080/bam?x=1#y": "ws://localhost:8080/bam/api/v1/relay/live",
	} {
		if got, err := liveURL(base); err != nil || got != want {
			t.Errorf("liveURL(%q) = %q, %v; want %q", base, got, err, want)
		}
	}
	for _, bad := range []string{"", "bam.example.org", "ftp://bam.example.org"} {
		if _, err := liveURL(bad); err == nil {
			t.Errorf("liveURL(%q) was taken", bad)
		}
	}
}

func TestServeWebSocketRefusesPlainRequests(t *testing.T) {
	w := newWorld(t)
	m := startMain(t, w, transports{ws: true}, nil, "")
	resp, err := http.Get(m.ts.URL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUpgradeRequired {
		t.Fatalf("a plain request was answered %d", resp.StatusCode)
	}
	// Without a credential, a WebSocket is refused before it opens.
	req, _ := http.NewRequest(http.MethodGet, m.ts.URL, nil)
	for k, v := range map[string]string{"Connection": "Upgrade", "Upgrade": "websocket", "Sec-WebSocket-Version": "13", "Sec-WebSocket-Key": "dGhlIHNhbXBsZSBub25jZQ=="} {
		req.Header.Set(k, v)
	}
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("a WebSocket without a credential was answered %d", resp.StatusCode)
	}
}

func TestRelayNames(t *testing.T) {
	for name, ok := range map[string]bool{"r1": true, "front-desk": true, "main": false, "Main": false, "-x": false, "a.b": false, "": false, strings.Repeat("a", 64): false} {
		if ValidRelayName(name) != ok {
			t.Errorf("ValidRelayName(%q) = %v", name, !ok)
		}
	}
}
