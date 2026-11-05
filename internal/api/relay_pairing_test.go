package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/config"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/relay"
)

// bodyLog keeps every request body a server was sent.
type bodyLog struct {
	mu     sync.Mutex
	bodies [][]byte
}

func (l *bodyLog) wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		l.mu.Lock()
		l.bodies = append(l.bodies, data)
		l.mu.Unlock()
		r.Body = io.NopCloser(bytes.NewReader(data))
		next.ServeHTTP(w, r)
	})
}

func (l *bodyLog) holds(secret []byte) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, b := range l.bodies {
		if bytes.Contains(b, secret) || bytes.Contains(b, []byte(base64.StdEncoding.EncodeToString(secret))) {
			return true
		}
	}
	return false
}

// pairingEnv is a main node relays pair with: served, with relays on over
// WebSocket and BoneMesh, and its relays run, until the test ends.
type pairingEnv struct {
	srv    *Server
	ts     *httptest.Server
	cookie *http.Cookie
	log    *bodyLog
}

func pairingServer(t *testing.T) *pairingEnv {
	t.Helper()
	saved := relaySweep
	relaySweep = 50 * time.Millisecond
	t.Cleanup(func() { relaySweep = saved })

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()

	cfg := config.DefaultConfig()
	testStorage(t, cfg)
	cfg.Auth.SecretKey = testSecretKey
	cfg.Relay = config.RelayConfig{WebSocket: true, BoneMesh: true, BoneMeshPort: port}
	st := setupTestStore(t)
	admin, err := st.CreateAdminUser("admin@test.org", "AdminSecret123!")
	if err != nil {
		t.Fatal(err)
	}
	srv := NewServer(cfg, st)
	env := &pairingEnv{srv: srv, cookie: sessionCookie(t, srv, admin.ID), log: &bodyLog{}}
	env.ts = httptest.NewUnstartedServer(env.log.wrap(srv.Handler()))
	cfg.Server.BaseURL = "http://" + env.ts.Listener.Addr().String()
	env.ts.Start()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		srv.RunRelays(ctx)
		close(done)
	}()
	t.Cleanup(func() {
		env.ts.CloseClientConnections()
		env.ts.Close()
		cancel()
		<-done
	})
	return env
}

// add adds a relay as an admin does, and returns its code.
func (e *pairingEnv) add(t *testing.T, name string) (string, models.Relay) {
	t.Helper()
	rec := doJSON(t, e.srv, e.cookie, http.MethodPost, "/api/v1/relays", `{"name":"`+name+`"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("adding %s: %d %s", name, rec.Code, rec.Body)
	}
	var out struct {
		Relay   models.Relay `json:"relay"`
		Code    string       `json:"code"`
		Command string       `json:"command"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if out.Command != "bam relay join "+e.srv.cfg.Server.BaseURL+" "+out.Code {
		t.Fatalf("the command: %q", out.Command)
	}
	return out.Code, out.Relay
}

// join pairs a relay with a code.
func (e *pairingEnv) join(code string) (*relay.Pairing, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return relay.Join(ctx, e.ts.Client(), e.ts.URL, code, "test")
}

// link runs a paired relay's bus, linked over what its pairing offers,
// until the test ends or stop is called.
func (e *pairingEnv) link(t *testing.T, p *relay.Pairing, websocket, bonemesh bool) (b *relay.Bus, stop func()) {
	t.Helper()
	b = relay.New(relay.Options{Name: p.Name, Trust: p.Trust(), Retry: 200 * time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	if websocket {
		wg.Add(1)
		go func() {
			defer wg.Done()
			relay.DialWebSocket(ctx, b, e.ts.URL, p.Credential)
		}()
	}
	var mesh *relay.Mesh
	if bonemesh {
		var err error
		if mesh, err = relay.StartBoneMesh(b, relay.BoneMeshOptions{Dial: p.BoneMesh}); err != nil {
			t.Fatal(err)
		}
	}
	var once sync.Once
	stop = func() {
		once.Do(func() {
			cancel()
			if mesh != nil {
				mesh.Close()
			}
			b.Close()
			wg.Wait()
		})
	}
	t.Cleanup(stop)
	return b, stop
}

func waitUntil(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// relayEvents lists the kinds of the events recorded about a relay.
func relayEvents(t *testing.T, srv *Server, name string) []string {
	t.Helper()
	events, err := srv.store.EventsSince(time.Now().Add(-time.Hour), 1000)
	if err != nil {
		t.Fatal(err)
	}
	var kinds []string
	for _, e := range events {
		if e.Payload.Relay != nil && e.Payload.Relay.Name == name {
			kinds = append(kinds, e.Kind)
		}
	}
	return kinds
}

func TestACodePairsOnceAndExpires(t *testing.T) {
	e := pairingServer(t)
	code, _ := e.add(t, "front-desk")
	p, err := e.join(code)
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "front-desk" || !strings.HasPrefix(p.Credential, relay.CredentialPrefix) || p.BoneMesh == "" || !p.WebSocket {
		t.Fatalf("paired: %+v", p.Issued)
	}
	if !time.Now().Add(6 * 24 * time.Hour).Before(p.ExpiresAt) {
		t.Fatalf("the certificate lasts until %s, not a week", p.ExpiresAt)
	}
	// Used: refused.
	if _, err := e.join(code); err == nil || !strings.Contains(err.Error(), "not valid, or was used") {
		t.Fatalf("a reused code: %v", err)
	}
	// Past its hour: refused.
	code, r := e.add(t, "workshop")
	if _, err := e.srv.store.Exec("UPDATE relays SET code_expires_at = ? WHERE id = ?", time.Now().Add(-time.Minute).UTC(), r.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.join(code); err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("an expired code: %v", err)
	}
	// A name is one relay's.
	rec := doJSON(t, e.srv, e.cookie, http.MethodPost, "/api/v1/relays", `{"name":"front-desk"}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("a name taken: %d %s", rec.Code, rec.Body)
	}
	if rec := doJSON(t, e.srv, e.cookie, http.MethodPost, "/api/v1/relays", `{"name":"Front Desk"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("a name a relay cannot have: %d", rec.Code)
	}
}

func TestTheRelaysPrivateKeyNeverReachesTheMainNode(t *testing.T) {
	e := pairingServer(t)
	code, r := e.add(t, "front-desk")
	p, err := e.join(code)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Renew(context.Background(), e.ts.Client(), "test"); err != nil {
		t.Fatal(err)
	}
	if len(p.IdentityPrivate) == 0 || e.log.holds(p.IdentityPrivate) {
		t.Fatal("the relay's private key reached the main node")
	}
	key, err := e.srv.store.RelayIdentityKey(r.Name)
	if err != nil || !bytes.Equal(key, p.IdentityPublic) {
		t.Fatalf("the main node keeps %v, %v, not the relay's public key", key, err)
	}
}

func TestATooOldRelayIsRefused(t *testing.T) {
	e := pairingServer(t)
	code, _ := e.add(t, "front-desk")
	public, _ := relay.NewIdentity()
	body, _ := json.Marshal(relay.JoinRequest{Code: code, IdentityKey: public, Version: "v0.1.0", Protocol: relay.MinVersion - 1})
	resp, err := e.ts.Client().Post(e.ts.URL+relay.JoinPath, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	answer, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusConflict || !strings.Contains(string(answer), "update the relay") {
		t.Fatalf("a relay too old was answered %d %s", resp.StatusCode, answer)
	}
	// Its code is not used up by being refused.
	if _, err := e.join(code); err != nil {
		t.Fatalf("after: %v", err)
	}
}

func TestRevokingARelayEndsItAtOnce(t *testing.T) {
	e := pairingServer(t)
	code, r := e.add(t, "front-desk")
	p, err := e.join(code)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := e.link(t, p, true, true)
	waitUntil(t, "the relay to link over both transports", func() bool { return len(e.srv.relays.Transports(p.Name)) == 2 })

	// Its status is kept.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	if _, err := b.Send(ctx, relay.Main, relay.TypeStatus, relay.Status{Version: "v9", Protocol: relay.Version, Allowlist: []string{"10.0.0.0/8:9100"}}); err != nil {
		t.Fatal(err)
	}
	cancel()
	waitUntil(t, "the relay's status", func() bool {
		got, err := e.srv.store.GetRelay(r.ID)
		return err == nil && got.Version == "v9" && len(got.Allowlist) == 1
	})

	rec := doJSON(t, e.srv, e.cookie, http.MethodDelete, "/api/v1/relays/"+r.ID, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("revoking: %d %s", rec.Code, rec.Body)
	}
	// The WebSocket closes at once.
	waitUntil(t, "the WebSocket to close", func() bool {
		for _, tr := range e.srv.relays.Transports(p.Name) {
			if tr == relay.WebSocket {
				return false
			}
		}
		return true
	})
	// It cannot renew.
	if err := p.Renew(context.Background(), e.ts.Client(), "test"); err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("a revoked relay renewed: %v", err)
	}
	// Over BoneMesh, its messages are refused at once, though its session
	// lasts.
	ctx, cancel = context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	if _, err := b.Send(ctx, relay.Main, relay.TypeStatus, relay.Status{Version: "v10", Protocol: relay.Version}); !errors.Is(err, relay.ErrUnreachable) {
		t.Fatalf("a revoked relay's message over BoneMesh ended with %v", err)
	}
	if got, _ := e.srv.store.GetRelay(r.ID); got.Version != "v9" {
		t.Fatalf("a revoked relay's status was kept: %+v", got)
	}
	// Its name is held while its certificate is valid.
	if rec := doJSON(t, e.srv, e.cookie, http.MethodPost, "/api/v1/relays", `{"name":"front-desk"}`); rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "until its certificate expires") {
		t.Fatalf("the name of a revoked relay: %d %s", rec.Code, rec.Body)
	}
}

func TestRelayOnlineAndOfflineEvents(t *testing.T) {
	e := pairingServer(t)
	code, r := e.add(t, "front-desk")
	p, err := e.join(code)
	if err != nil {
		t.Fatal(err)
	}
	_, stop := e.link(t, p, true, false)
	waitUntil(t, "the relay online", func() bool {
		got, err := e.srv.store.GetRelay(r.ID)
		return err == nil && got.Online
	})
	stop()
	waitUntil(t, "the relay offline", func() bool {
		got, err := e.srv.store.GetRelay(r.ID)
		return err == nil && !got.Online
	})
	if kinds := relayEvents(t, e.srv, "front-desk"); len(kinds) != 2 || kinds[0] != models.EventRelayOnline || kinds[1] != models.EventRelayOffline {
		t.Fatalf("events: %v", kinds)
	}
}

func TestRotatingTheMeshRootUnpairsRelays(t *testing.T) {
	e := pairingServer(t)
	code, r := e.add(t, "front-desk")
	p, err := e.join(code)
	if err != nil {
		t.Fatal(err)
	}
	rec := doJSON(t, e.srv, e.cookie, http.MethodPost, "/api/v1/relays/mesh/rotate", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("rotating: %d %s", rec.Code, rec.Body)
	}
	if err := p.Renew(context.Background(), e.ts.Client(), "test"); err == nil {
		t.Fatal("a relay paired under the old root renewed")
	}
	// A new code pairs it again, under the new root.
	rec = doJSON(t, e.srv, e.cookie, http.MethodPost, "/api/v1/relays/"+r.ID+"/code", "")
	var out struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	again, err := e.join(out.Code)
	if err != nil {
		t.Fatal(err)
	}
	if again.Mesh == p.Mesh || bytes.Equal(again.RootPublic, p.RootPublic) {
		t.Fatal("the relay paired again under the old root")
	}
	// And links, as BoneMesh starts again with the new root.
	e.link(t, again, false, true)
	waitUntil(t, "the relay to link over BoneMesh", func() bool { return len(e.srv.relays.Transports(again.Name)) == 1 })
}

func TestPairingNeedsTheSecretKey(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	rec := doJSON(t, srv, sessionCookie(t, srv, admin.ID), http.MethodPost, "/api/v1/relays", `{"name":"front-desk"}`)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "secret_key") {
		t.Fatalf("without a secret key: %d %s", rec.Code, rec.Body)
	}
}

func TestListRelays(t *testing.T) {
	e := pairingServer(t)
	e.add(t, "front-desk")
	rec := doJSON(t, e.srv, e.cookie, http.MethodGet, "/api/v1/relays", "")
	var out struct {
		Relays    []relayView `json:"relays"`
		Mesh      any         `json:"mesh"`
		BoneMesh  bool        `json:"bonemesh"`
		WebSocket bool        `json:"websocket"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || len(out.Relays) != 1 || out.Mesh != nil || !out.BoneMesh || !out.WebSocket {
		t.Fatalf("list: %s", rec.Body)
	}
	if strings.Contains(rec.Body.String(), "code_hash") || strings.Contains(rec.Body.String(), "credential") {
		t.Fatalf("the list shows a secret: %s", rec.Body)
	}
}
