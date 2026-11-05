package relay

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// LivePath is where the main node takes relays' WebSocket connections.
const LivePath = "/api/v1/relay/live"

// CredentialScheme is the Authorization scheme a relay presents its
// credential with: "Authorization: Relay <credential>".
const CredentialScheme = "Relay"

var (
	// wsPing is how often a WebSocket link is pinged, to find dead ones.
	wsPing = 30 * time.Second
	// wsRedial bounds how long a relay waits before dialing again.
	wsRedial = 30 * time.Second
)

// wsQueue is how many messages may wait for a WebSocket link; one further
// behind refuses more, and the bus sends them again.
const wsQueue = 256

// errLinkClosed is a send over a link that has closed.
var errLinkClosed = errors.New("the link has closed")

// wsLink is a WebSocket connection between a relay and the main node.
type wsLink struct {
	ws      *websocket.Conn
	out     chan []byte
	done    chan struct{}
	closing sync.Once
	// recheck, on the main node, says whether the relay's credential
	// still holds.
	recheck func() error
}

func newWSLink(ws *websocket.Conn, recheck func() error) *wsLink {
	return &wsLink{ws: ws, out: make(chan []byte, wsQueue), done: make(chan struct{}), recheck: recheck}
}

func (l *wsLink) send(data []byte) error {
	select {
	case <-l.done:
		return errLinkClosed
	default:
	}
	select {
	case l.out <- data:
		return nil
	default:
		return errors.New("the WebSocket link is behind")
	}
}

// close closes the connection from this end, saying why. It ends the
// connection's reading, and with it the link.
func (l *wsLink) close(reason string) {
	l.closing.Do(func() {
		close(l.done)
		// Closing waits for the other end to answer; nothing else need wait.
		go l.ws.Close(websocket.StatusPolicyViolation, reason)
	})
}

func (l *wsLink) check() error {
	if l.recheck == nil {
		return nil
	}
	return l.recheck()
}

// run carries messages over the link until it ends: from peer to the bus,
// and from the bus to peer, pinging it to find it dead.
func (l *wsLink) run(ctx context.Context, b *Bus, peer string) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		ping := time.NewTicker(wsPing)
		defer ping.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-l.done:
				return
			case data := <-l.out:
				wctx, done := context.WithTimeout(ctx, 10*time.Second)
				err := l.ws.Write(wctx, websocket.MessageText, data)
				done()
				if err != nil {
					l.close("it could not be written to")
					return
				}
			case <-ping.C:
				pctx, done := context.WithTimeout(ctx, 10*time.Second)
				err := l.ws.Ping(pctx)
				done()
				if err != nil {
					l.close("it stopped answering pings")
					return
				}
			}
		}
	}()
	b.linkUp(WebSocket, peer, l)
	defer b.linkDown(WebSocket, peer, l)
	for {
		_, data, err := l.ws.Read(ctx)
		if err != nil {
			break
		}
		b.receive(WebSocket, peer, data)
	}
	l.closing.Do(func() {
		close(l.done)
		l.ws.Close(websocket.StatusNormalClosure, "")
	})
}

// ServeWebSocket takes a relay's WebSocket connection, on the main node:
// GET /api/v1/relay/live, with the relay's credential from pairing as
// "Authorization: Relay <credential>". A newer connection from the same
// relay replaces an older one.
func (b *Bus) ServeWebSocket(w http.ResponseWriter, r *http.Request) {
	if !b.main() {
		writeError(w, http.StatusNotFound, "Only the main node takes relays' connections")
		return
	}
	if !webSocketRequest(r) {
		w.Header().Set("Upgrade", "websocket")
		writeError(w, http.StatusUpgradeRequired, "Connect with a WebSocket (version 13) to join the relay bus")
		return
	}
	scheme, secret, ok := strings.Cut(r.Header.Get("Authorization"), " ")
	secret = strings.TrimSpace(secret)
	if !ok || !strings.EqualFold(scheme, CredentialScheme) || secret == "" {
		writeError(w, http.StatusUnauthorized, "Connect with the relay's credential, as Authorization: Relay <credential>")
		return
	}
	name, err := b.opts.Trust.Credential(secret)
	if errors.Is(err, ErrUntrusted) || (err == nil && !ValidRelayName(name)) {
		writeError(w, http.StatusUnauthorized, "This credential is not a paired relay's, or the relay was revoked")
		return
	}
	if err != nil {
		log.Printf("relays: checking a credential: %v", err)
		writeError(w, http.StatusInternalServerError, "The credential could not be checked")
		return
	}
	// A link outlasts the server's read and write timeouts.
	rc := http.NewResponseController(w)
	_ = rc.SetReadDeadline(time.Time{})
	_ = rc.SetWriteDeadline(time.Time{})
	ws, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	ws.SetReadLimit(maxFrame)
	l := newWSLink(ws, func() error {
		holder, err := b.opts.Trust.Credential(secret)
		if err == nil && holder != name {
			err = ErrUntrusted
		}
		return err
	})
	// The request's context ends when the server stops; the bus's when it
	// closes.
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	go func() {
		select {
		case <-b.ctx.Done():
			l.close("the node is stopping")
		case <-ctx.Done():
		}
	}()
	l.run(ctx, b, name)
}

// DialWebSocket links a relay to the main node at mainURL, such as
// https://bam.example.org, over WebSocket with the relay's credential, and
// dials again whenever the link drops, until ctx ends.
func DialWebSocket(ctx context.Context, b *Bus, mainURL, credential string) {
	wait := time.Second
	var last string
	for {
		start := time.Now()
		err := dialWebSocket(ctx, b, mainURL, credential)
		if ctx.Err() != nil || b.ctx.Err() != nil {
			return
		}
		if time.Since(start) > wsRedial {
			wait = time.Second
		}
		if err != nil && err.Error() != last {
			log.Printf("relays: the WebSocket link to %s: %v", mainURL, err)
		}
		if err != nil {
			last = err.Error()
		}
		select {
		case <-ctx.Done():
			return
		case <-b.ctx.Done():
			return
		case <-time.After(wait):
		}
		wait = min(wait*2, wsRedial)
	}
}

// dialWebSocket links a relay to the main node once, until the link ends.
func dialWebSocket(ctx context.Context, b *Bus, mainURL, credential string) error {
	u, err := liveURL(mainURL)
	if err != nil {
		return err
	}
	dctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	ws, resp, err := websocket.Dial(dctx, u, &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": {CredentialScheme + " " + credential}},
	})
	cancel()
	if err != nil {
		if resp != nil {
			var answer struct {
				Error string `json:"error"`
			}
			if resp.Body != nil {
				_ = json.NewDecoder(resp.Body).Decode(&answer)
			}
			if answer.Error != "" {
				return fmt.Errorf("refused (%d): %s", resp.StatusCode, answer.Error)
			}
			return fmt.Errorf("refused (%d)", resp.StatusCode)
		}
		return err
	}
	ws.SetReadLimit(maxFrame)
	l := newWSLink(ws, nil)
	go func() {
		select {
		case <-b.ctx.Done():
			l.close("the node is stopping")
		case <-ctx.Done():
			l.close("the node is stopping")
		case <-l.done:
		}
	}()
	l.run(ctx, b, Main)
	return nil
}

// liveURL is where a relay dials the main node at base.
func liveURL(base string) (string, error) {
	u, err := url.Parse(base)
	if err != nil || u.Host == "" {
		return "", fmt.Errorf("%q is not the main node's address, such as https://bam.example.org", base)
	}
	switch u.Scheme {
	case "https", "wss":
		u.Scheme = "wss"
	case "http", "ws":
		u.Scheme = "ws"
	default:
		return "", fmt.Errorf("%q is not the main node's address, such as https://bam.example.org", base)
	}
	u.Path = strings.TrimSuffix(u.Path, "/") + LivePath
	u.RawQuery, u.Fragment = "", ""
	return u.String(), nil
}

// writeError answers with the API's JSON error.
func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// webSocketRequest reports whether a request asks to open a WebSocket as
// RFC 6455 says: an upgrade to websocket, version 13, with a key.
func webSocketRequest(r *http.Request) bool {
	hasToken := func(header, token string) bool {
		for _, v := range r.Header.Values(header) {
			for _, part := range strings.Split(v, ",") {
				if strings.EqualFold(strings.TrimSpace(part), token) {
					return true
				}
			}
		}
		return false
	}
	return hasToken("Connection", "upgrade") && hasToken("Upgrade", "websocket") &&
		r.Header.Get("Sec-WebSocket-Version") == "13" && r.Header.Get("Sec-WebSocket-Key") != ""
}
