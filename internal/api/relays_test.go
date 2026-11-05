package api

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"forge.axonibyte.com/axonibyte/bonemesh/go/v3/node"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/config"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/relay"
	"github.com/coder/websocket"
)

// relayCredentials is pairing as these tests need it: credentials, by
// the relay holding each.
type relayCredentials map[string]string

func (c relayCredentials) Credential(secret string) (string, error) {
	if name, ok := c[secret]; ok {
		return name, nil
	}
	return "", relay.ErrUntrusted
}

func (relayCredentials) IdentityKey(string) ([]byte, error) { return nil, relay.ErrUntrusted }
func (relayCredentials) Mesh() (node.Config, error)         { return node.Config{}, relay.ErrNotPaired }

// relayServer serves a server with [relay] as on says, until the test
// ends; with trust, its bus trusts that.
func relayServer(t *testing.T, on config.RelayConfig, trust relay.Trust) (*Server, *httptest.Server) {
	t.Helper()
	cfg := config.DefaultConfig()
	testStorage(t, cfg)
	if on.BoneMeshPort == 0 {
		on.BoneMeshPort = config.DefaultBoneMeshPort
	}
	cfg.Relay = on
	srv := NewServer(cfg, setupTestStore(t))
	if trust != nil {
		srv.relays = relay.New(relay.Options{Name: relay.Main, Trust: trust})
		t.Cleanup(srv.relays.Close)
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return srv, ts
}

// dialRelay opens the relay WebSocket with a credential, saying what it
// was answered.
func dialRelay(t *testing.T, ts *httptest.Server, credential string) int {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	h := http.Header{"Authorization": {"Relay " + credential}}
	c, resp, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(ts.URL, "http")+"/api/v1/relay/live", &websocket.DialOptions{HTTPHeader: h})
	if err == nil {
		c.Close(websocket.StatusNormalClosure, "")
		return http.StatusSwitchingProtocols
	}
	if resp == nil {
		t.Fatal(err)
	}
	return resp.StatusCode
}

func TestRelayTransportsTurnOff(t *testing.T) {
	t.Run("both off", func(t *testing.T) {
		srv, ts := relayServer(t, config.RelayConfig{}, nil)
		if srv.relays != nil {
			t.Fatal("a bus was made with both transports off")
		}
		if code := dialRelay(t, ts, "anything"); code != http.StatusNotFound {
			t.Fatalf("the relay WebSocket was answered %d", code)
		}
	})

	t.Run("websocket off", func(t *testing.T) {
		_, ts := relayServer(t, config.RelayConfig{BoneMesh: true}, relayCredentials{"good": "r1"})
		if code := dialRelay(t, ts, "good"); code != http.StatusNotFound {
			t.Fatalf("the relay WebSocket was answered %d with WebSocket off", code)
		}
	})

	t.Run("websocket on", func(t *testing.T) {
		_, ts := relayServer(t, config.RelayConfig{WebSocket: true}, nil)
		if code := dialRelay(t, ts, "unpaired"); code != http.StatusUnauthorized {
			t.Fatalf("with no relay paired, a credential was answered %d", code)
		}
	})

	t.Run("websocket on, paired", func(t *testing.T) {
		// Paired, the relay links, past the API's own sign-in.
		srv, ts := relayServer(t, config.RelayConfig{WebSocket: true}, relayCredentials{"good": "r1"})
		if code := dialRelay(t, ts, "bad"); code != http.StatusUnauthorized {
			t.Fatalf("a wrong credential was answered %d", code)
		}
		r1 := relay.New(relay.Options{Name: "r1"})
		defer r1.Close()
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go relay.DialWebSocket(ctx, r1, ts.URL, "good")
		deadline := time.Now().Add(10 * time.Second)
		for len(srv.relays.Transports("r1")) == 0 {
			if time.Now().After(deadline) {
				t.Fatal("the relay never linked")
			}
			time.Sleep(20 * time.Millisecond)
		}
		got := make(chan relay.Message, 1)
		srv.relays.Handle(relay.TypeStatus, func(_ context.Context, m relay.Message) { got <- m })
		if _, err := r1.Send(ctx, relay.Main, relay.TypeStatus, relay.Status{Protocol: relay.Version}); err != nil {
			t.Fatal(err)
		}
		if m := <-got; m.From != "r1" {
			t.Fatalf("the status came from %q", m.From)
		}
	})

	t.Run("bonemesh off", func(t *testing.T) {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		port := l.Addr().(*net.TCPAddr).Port
		l.Close()
		srv, _ := relayServer(t, config.RelayConfig{WebSocket: true, BoneMeshPort: port}, nil)
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() {
			srv.RunRelays(ctx)
			close(done)
		}()
		defer func() {
			cancel()
			<-done
		}()
		time.Sleep(200 * time.Millisecond)
		if c, err := net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(port), time.Second); err == nil {
			c.Close()
			t.Fatal("BoneMesh took a connection while it is off")
		}
	})
}
