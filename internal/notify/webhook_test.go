package notify

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
)

// receiver is an HTTP server standing in for a webhook's receiver.
type receiver struct {
	srv    *httptest.Server
	status int

	mu  sync.Mutex
	got []*http.Request
	raw [][]byte
}

func newReceiver(t *testing.T) *receiver {
	t.Helper()
	r := &receiver{status: http.StatusOK}
	r.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		body, _ := io.ReadAll(req.Body)
		r.mu.Lock()
		r.got, r.raw = append(r.got, req), append(r.raw, body)
		status := r.status
		r.mu.Unlock()
		if status == http.StatusFound {
			w.Header().Set("Location", "http://169.254.169.254/")
		}
		w.WriteHeader(status)
	}))
	t.Cleanup(r.srv.Close)
	return r
}

// webhookTo is a driver that resolves every name to the receiver, allowed
// as private (it is on 127.0.0.1) only when the channel says so.
func webhookTo(r *receiver) Webhook {
	addr := netip.MustParseAddrPort(r.srv.Listener.Addr().String())
	return Webhook{
		Resolve: func(context.Context, string) ([]netip.Addr, error) { return []netip.Addr{addr.Addr()}, nil },
		Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, addr.String())
		},
		Now: func() time.Time { return time.Unix(1762100000, 0) },
	}
}

func lent() Notification {
	return Notification{BaseURL: "https://bam.example", DeliveryID: "d1", Event: models.Event{
		ID: "e1", Kind: models.EventAssetCheckedOut, AssetID: ptr("a1"),
		Payload: models.EventPayload{Asset: &models.EventAsset{ID: "a1", Name: "Drill"},
			Actor: &models.EventActor{ID: "u1", Name: "Ada", Email: "ada@example.org"}},
	}}
}

func ptr(s string) *string { return &s }

func TestWebhookPostsSignedJSON(t *testing.T) {
	r := newReceiver(t)
	opts := map[string]string{"url": "http://hooks.example/bam?token=x", "secret": "s3cret", "headers": "Authorization: Bearer abc\nX-Team: makers", "allow_private_networks": "true"}
	if err := (Webhook{}).Validate(opts); err != nil {
		t.Fatal(err)
	}
	if err := webhookTo(r).Send(context.Background(), opts, lent()); err != nil {
		t.Fatal(err)
	}
	req, body := r.got[0], r.raw[0]
	if req.URL.RequestURI() != "/bam?token=x" || req.Host != "hooks.example" {
		t.Errorf("posted to %s on %s", req.URL, req.Host)
	}
	for name, want := range map[string]string{
		"Content-Type": "application/json", "Bam-Event": "asset.checked_out", "Bam-Delivery": "d1", "Bam-Timestamp": "1762100000",
		"Authorization": "Bearer abc", "X-Team": "makers",
	} {
		if got := req.Header.Get(name); got != want {
			t.Errorf("%s: %q, want %q", name, got, want)
		}
	}
	if got, want := req.Header.Get("Bam-Signature"), Signature("s3cret", "1762100000", body); got != want || !strings.HasPrefix(got, "v1=") {
		t.Errorf("signature %q, want %q", got, want)
	}
	if Signature("s3cret", "1762100001", body) == req.Header.Get("Bam-Signature") || Signature("s3cret", "1762100000", append(body, ' ')) == req.Header.Get("Bam-Signature") {
		t.Error("the signature does not depend on the timestamp and body")
	}
	var got struct {
		Version  int    `json:"version"`
		ID       string `json:"id"`
		Instance string `json:"instance"`
		Event    struct {
			Kind    string              `json:"kind"`
			URL     string              `json:"url"`
			Payload models.EventPayload `json:"payload"`
		} `json:"event"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	if got.Version != 1 || got.ID != "d1" || got.Instance != "https://bam.example" || got.Event.Kind != "asset.checked_out" ||
		got.Event.URL != "https://bam.example/assets/a1" || got.Event.Payload.Actor.Name != "Ada" || got.Event.Payload.Actor.Email != "" {
		t.Errorf("body %s", body)
	}
}

func TestWebhookBodyTemplate(t *testing.T) {
	r := newReceiver(t)
	opts := map[string]string{"url": "https://hooks.example/", "allow_private_networks": "true",
		"body": `{"text": {{json (printf "%s: %s" .event.kind .event.payload.asset.name)}}}`}
	r.srv.Close()
	r.srv = httptest.NewTLSServer(r.srv.Config.Handler)
	w := webhookTo(r)
	if err := w.Send(context.Background(), opts, lent()); err == nil {
		t.Fatal("a self-signed certificate was trusted")
	}
	// Plain HTTP for the rest.
	r2 := newReceiver(t)
	opts["url"] = "http://hooks.example/"
	if err := webhookTo(r2).Send(context.Background(), opts, lent()); err != nil {
		t.Fatal(err)
	}
	if string(r2.raw[0]) != `{"text": "asset.checked_out: Drill"}` {
		t.Errorf("templated body %s", r2.raw[0])
	}
	opts["body"] = `not {{.event.kind}} JSON`
	if err := webhookTo(r2).Send(context.Background(), opts, lent()); err == nil || !strings.Contains(err.Error(), "valid JSON") {
		t.Errorf("a template making no JSON: %v", err)
	}
	opts["content_type"] = "text/plain"
	if err := webhookTo(r2).Send(context.Background(), opts, lent()); err != nil {
		t.Errorf("text with a content type: %v", err)
	}
	if got := r2.got[len(r2.got)-1].Header.Get("Content-Type"); got != "text/plain" {
		t.Errorf("content type %q", got)
	}
	if err := (Webhook{}).Validate(map[string]string{"url": "https://hooks.example/", "body": "{{ .unclosed"}); err == nil {
		t.Error("a template that does not parse was accepted")
	}
}

func TestWebhooksRefusePrivateNetworksUnlessAllowed(t *testing.T) {
	r := newReceiver(t)
	opts := map[string]string{"url": "http://hooks.example/"}
	// The receiver is on 127.0.0.1: refused, and not retried.
	err := webhookTo(r).Send(context.Background(), opts, lent())
	if err == nil || errors.Is(err, ErrTemporary) || !strings.Contains(err.Error(), "private network") || len(r.got) != 0 {
		t.Errorf("a private address: %v, %d requests", err, len(r.got))
	}
	// A name resolving to public and private addresses alike is refused too.
	mixed := webhookTo(r)
	mixed.Resolve = func(context.Context, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("93.184.215.14"), netip.MustParseAddr("::ffff:10.0.0.1")}, nil
	}
	if err := mixed.Send(context.Background(), opts, lent()); err == nil || !strings.Contains(err.Error(), "private network") {
		t.Errorf("a public and a mapped private address: %v", err)
	}
	// Written as an address, it is refused when saved.
	for _, u := range []string{"http://127.0.0.1/", "http://169.254.169.254/latest/meta-data", "http://[::1]:8080/", "http://10.1.2.3/", "http://[::ffff:192.168.1.1]/"} {
		if err := (Webhook{}).Validate(map[string]string{"url": u}); err == nil {
			t.Errorf("%s accepted", u)
		}
		if err := (Webhook{}).Validate(map[string]string{"url": u, "allow_private_networks": "true"}); err != nil {
			t.Errorf("%s refused though allowed: %v", u, err)
		}
	}
}

func TestPrivateAddress(t *testing.T) {
	for _, a := range []string{"127.0.0.1", "10.0.0.1", "172.16.5.4", "192.168.1.1", "169.254.169.254", "100.64.0.1", "0.0.0.0", "224.0.0.1",
		"::1", "fe80::1", "fc00::1", "::", "ff02::1", "::ffff:127.0.0.1", "198.18.0.1"} {
		if !PrivateAddress(netip.MustParseAddr(a)) {
			t.Errorf("%s is not private", a)
		}
	}
	for _, a := range []string{"93.184.215.14", "8.8.8.8", "2606:4700::1111", "100.128.0.1"} {
		if PrivateAddress(netip.MustParseAddr(a)) {
			t.Errorf("%s is private", a)
		}
	}
}

func TestWebhookConnectsToTheAddressItChecked(t *testing.T) {
	r := newReceiver(t)
	w := webhookTo(r)
	// DNS says a public address; the dialer is asked for that one, not for
	// whatever the name resolves to by the time it connects.
	w.Resolve = func(context.Context, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("93.184.215.14")}, nil
	}
	var dialed string
	w.Dial = func(ctx context.Context, network, address string) (net.Conn, error) {
		dialed = address
		return (&net.Dialer{}).DialContext(ctx, network, r.srv.Listener.Addr().String())
	}
	if err := w.Send(context.Background(), map[string]string{"url": "http://hooks.example:8080/"}, lent()); err != nil {
		t.Fatal(err)
	}
	if dialed != "93.184.215.14:8080" {
		t.Errorf("dialed %q", dialed)
	}
}

func TestWebhookResponses(t *testing.T) {
	r := newReceiver(t)
	opts := map[string]string{"url": "http://hooks.example/", "allow_private_networks": "true"}
	send := func(status int) error {
		r.mu.Lock()
		r.status = status
		r.mu.Unlock()
		return webhookTo(r).Send(context.Background(), opts, lent())
	}
	if err := send(http.StatusFound); err == nil || errors.Is(err, ErrTemporary) || !strings.Contains(err.Error(), "redirects") {
		t.Errorf("a redirect: %v, want a permanent failure, not followed", err)
	}
	if err := send(http.StatusServiceUnavailable); !errors.Is(err, ErrTemporary) {
		t.Errorf("503: %v, want temporary", err)
	}
	var after *RetryAfterError
	if err := send(http.StatusTooManyRequests); !errors.As(err, &after) {
		t.Errorf("429: %v, want retry after", err)
	}
	if err := send(http.StatusNotFound); err == nil || errors.Is(err, ErrTemporary) {
		t.Errorf("404: %v, want permanent", err)
	}
}

func TestWebhookOptions(t *testing.T) {
	for name, opts := range map[string]map[string]string{
		"no URL":               {},
		"ftp":                  {"url": "ftp://hooks.example/"},
		"a user name":          {"url": "https://me:pw@hooks.example/"},
		"a bad header":         {"url": "https://hooks.example/", "headers": "no colon here"},
		"a reserved header":    {"url": "https://hooks.example/", "headers": "BAM-Signature: forged"},
		"a host header":        {"url": "https://hooks.example/", "headers": "Host: elsewhere"},
		"private network typo": {"url": "https://hooks.example/", "allow_private_networks": "yes please"},
	} {
		if err := (Webhook{}).Validate(opts); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}
