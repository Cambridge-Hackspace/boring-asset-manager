package api

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
	"github.com/coder/websocket"
)

// Live events (README §5.4): devices, such as a cabinet that lights the
// drawer an asset is in, open a WebSocket with an API token holding
// events.read, and are sent each event they may see as it is recorded.
//
// Every server reads new events from the database while it holds
// connections: at once after a request that changed something, and every
// livePoll otherwise, which also brings what other servers sharing the
// database recorded. Clocks differ between servers, so each read looks back
// liveLookBack and skips the events already sent.

var (
	livePoll     = time.Second
	liveLookBack = 5 * time.Second
	// liveRefresh is how often a connection says it is still open and
	// checks its token or user still may read events.
	liveRefresh = 10 * time.Second
	// livePing is how often a connection is pinged, to find dead ones.
	livePing = 30 * time.Second
	// liveStale is how long a listed connection goes unseen before it
	// counts as left by a stopped server.
	liveStale = time.Minute
	// maxLivePerHolder bounds the connections one token or user holds.
	maxLivePerHolder = 5
)

const (
	// liveQueue is how many messages may wait for a connection; one that
	// falls further behind is closed.
	liveQueue = 256
	// liveReadLimit bounds a message from a device.
	liveReadLimit = 16 << 10
	// liveRemembered bounds the action events a connection may answer.
	liveRemembered = 1000
)

// liveFilter narrows what a connection is sent: event kinds, action keys,
// and categories with their subcategories. An empty list does not narrow.
type liveFilter struct {
	Kinds      []string `json:"kinds"`
	Actions    []string `json:"actions"`
	Categories []string `json:"categories"`
}

func (f liveFilter) matches(e models.Event, categories map[string]bool) bool {
	has := func(list []string, v string) bool {
		for _, x := range list {
			if x == v {
				return true
			}
		}
		return false
	}
	if len(f.Kinds) > 0 && !has(f.Kinds, e.Kind) {
		return false
	}
	if len(f.Actions) > 0 && (e.Payload.Action == nil || !has(f.Actions, e.Payload.Action.Key)) {
		return false
	}
	if len(f.Categories) > 0 {
		in := false
		for _, c := range f.Categories {
			in = in || categories[c]
		}
		return in
	}
	return true
}

// liveFilterOf reads a filter from a connection's query: comma-separated
// kinds, actions and categories.
func liveFilterOf(q url.Values) liveFilter {
	list := func(key string) []string {
		var out []string
		for _, v := range strings.Split(q.Get(key), ",") {
			if v = strings.TrimSpace(v); v != "" {
				out = append(out, v)
			}
		}
		return out
	}
	return liveFilter{Kinds: list("kinds"), Actions: list("actions"), Categories: list("categories")}
}

// liveHub holds a server's live connections.
type liveHub struct {
	mu    sync.Mutex
	conns map[*liveConn]struct{}
	poke  chan struct{}
	// last is the newest event sent; sent holds the events sent lately,
	// by when they were recorded, so a read looking back skips them.
	last time.Time
	sent map[string]time.Time
}

func newLiveHub() *liveHub {
	return &liveHub{conns: map[*liveConn]struct{}{}, poke: make(chan struct{}, 1), sent: map[string]time.Time{}}
}

// liveConn is one open connection.
type liveConn struct {
	id              string
	tokenID, userID string
	ws              *websocket.Conn
	out             chan []byte
	// opened is when it opened: it is sent only what was recorded since.
	opened  time.Time
	closing sync.Once

	mu sync.Mutex
	// req carries the identity and permissions it is sent events with,
	// refreshed every liveRefresh.
	req     *http.Request
	filter  liveFilter
	actions map[string]bool
}

// close closes the connection from the server's side, telling the device
// why. It ends the connection's reading, and with it the connection.
func (c *liveConn) close(reason string) {
	c.closing.Do(func() {
		// Closing waits for the device to answer; nothing else need wait.
		go c.ws.Close(websocket.StatusPolicyViolation, reason)
	})
}

func (h *liveHub) add(c *liveConn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.conns) == 0 {
		// Nothing was being read: start from now, not from long ago.
		h.last, h.sent = time.Now().UTC(), map[string]time.Time{}
	}
	h.conns[c] = struct{}{}
}

func (h *liveHub) remove(c *liveConn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.conns, c)
}

func (h *liveHub) snapshot() []*liveConn {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]*liveConn, 0, len(h.conns))
	for c := range h.conns {
		out = append(out, c)
	}
	return out
}

// pokeNow asks for a read of new events at once, when anyone listens.
func (h *liveHub) pokeNow() {
	h.mu.Lock()
	listening := len(h.conns) > 0
	h.mu.Unlock()
	if !listening {
		return
	}
	select {
	case h.poke <- struct{}{}:
	default:
	}
}

// closeToken closes a token's connections, as when it is revoked.
func (h *liveHub) closeToken(tokenID, reason string) {
	for _, c := range h.snapshot() {
		if c.tokenID == tokenID {
			c.close(reason)
		}
	}
}

// PokeLive wraps a handler so requests that may have recorded events have
// them read at once.
func (s *Server) PokeLive(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r)
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			s.live.pokeNow()
		}
	})
}

// RunLive sends new events to live connections until ctx ends, then closes
// them.
func (s *Server) RunLive(ctx context.Context) {
	ticker := time.NewTicker(livePoll)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			for _, c := range s.live.snapshot() {
				c.close("the server is stopping")
			}
			return
		case <-ticker.C:
		case <-s.live.poke:
		}
		s.liveRound()
	}
}

// liveRound reads the events recorded since the last read and sends each
// to the connections that may see it and want it.
func (s *Server) liveRound() {
	conns := s.live.snapshot()
	if len(conns) == 0 {
		return
	}
	s.live.mu.Lock()
	since := s.live.last.Add(-liveLookBack)
	s.live.mu.Unlock()
	events, err := s.store.EventsSince(since, 1000)
	if err != nil {
		log.Printf("live events: %v", err)
		return
	}
	s.live.mu.Lock()
	var fresh []models.Event
	for _, e := range events {
		if _, done := s.live.sent[e.ID]; done {
			continue
		}
		s.live.sent[e.ID] = e.CreatedAt
		if e.CreatedAt.After(s.live.last) {
			s.live.last = e.CreatedAt
		}
		fresh = append(fresh, e)
	}
	for id, at := range s.live.sent {
		if at.Before(s.live.last.Add(-2 * liveLookBack)) {
			delete(s.live.sent, id)
		}
	}
	s.live.mu.Unlock()
	if len(fresh) == 0 {
		return
	}
	categories, scopes, err := s.store.EventPlaces(fresh)
	if err != nil {
		log.Printf("live events: %v", err)
		return
	}
	for _, e := range fresh {
		place := ""
		switch {
		case e.AssetID != nil:
			place = *e.AssetID
		case e.CategoryID != nil:
			place = *e.CategoryID
		default:
			continue
		}
		for _, c := range conns {
			c.deliver(e, categories[place], scopes[place])
		}
	}
}

// deliver sends an event to the connection, if it may see it and wants
// it: people by name only, and only to those who track them.
func (c *liveConn) deliver(e models.Event, categories map[string]bool, scopes []string) {
	c.mu.Lock()
	req, filter := c.req, c.filter
	c.mu.Unlock()
	if e.CreatedAt.Before(c.opened) || !filter.matches(e, categories) || !Allows(req.Context(), rbac.PermEventsRead, scopes) {
		return
	}
	shown := redactEvent(e, Allows(req.Context(), rbac.PermAssetsTrack, scopes))
	data, err := json.Marshal(map[string]any{"type": "event", "event": shown})
	if err != nil {
		return
	}
	if e.Kind == models.EventAssetAction {
		c.mu.Lock()
		if len(c.actions) >= liveRemembered {
			c.actions = map[string]bool{}
		}
		c.actions[e.ID] = true
		c.mu.Unlock()
	}
	select {
	case c.out <- data:
	default:
		c.close("it fell too far behind")
	}
}

// liveMessage is a message from a device: a new filter, or an answer to an
// action event.
type liveMessage struct {
	Type    string      `json:"type"`
	Filter  *liveFilter `json:"filter,omitempty"`
	EventID string      `json:"event_id,omitempty"`
	OK      bool        `json:"ok"`
	Message string      `json:"message,omitempty"`
}

// handleLiveEvents opens a live connection: GET /api/v1/events/live, with
// an API token (or a session), and optionally ?kinds=, ?actions= and
// ?categories=, each comma-separated.
func (s *Server) handleLiveEvents(w http.ResponseWriter, r *http.Request) {
	// Answered here rather than by the WebSocket library, so the answer is
	// the API's JSON.
	if !webSocketRequest(r) {
		w.Header().Set("Upgrade", "websocket")
		writeJSONError(w, http.StatusUpgradeRequired, "Connect with a WebSocket (version 13) to receive events live")
		return
	}
	var tokenID, userID string
	if t := CurrentToken(r.Context()); t != nil {
		tokenID = t.ID
	} else if u := CurrentUser(r.Context()); u != nil {
		userID = u.ID
	}
	open, err := s.store.ListLiveConnections(time.Now().Add(-liveStale), tokenID, userID)
	if err != nil {
		writeServerError(w, err, "count live connections")
		return
	}
	if len(open) >= maxLivePerHolder {
		writeJSONError(w, http.StatusTooManyRequests, "This token already holds as many live connections as it may; close one first")
		return
	}
	// A live connection outlasts the server's read and write timeouts.
	rc := http.NewResponseController(w)
	_ = rc.SetReadDeadline(time.Time{})
	_ = rc.SetWriteDeadline(time.Time{})
	ws, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	ws.SetReadLimit(liveReadLimit)
	id, err := s.store.OpenLiveConnection(tokenID, userID, r.RemoteAddr)
	if err != nil {
		log.Printf("live events: %v", err)
		ws.Close(websocket.StatusInternalError, "could not open the connection")
		return
	}
	defer func() {
		if err := s.store.CloseLiveConnection(id, time.Now().Add(-liveStale)); err != nil {
			log.Printf("live events: %v", err)
		}
	}()
	// Writing and upkeep end when the connection does; reading ends it.
	ctx, cancel := context.WithCancel(context.WithoutCancel(r.Context()))
	defer cancel()
	c := &liveConn{id: id, tokenID: tokenID, userID: userID, ws: ws, out: make(chan []byte, liveQueue), opened: time.Now().UTC().Truncate(time.Microsecond),
		req: r, filter: liveFilterOf(r.URL.Query()), actions: map[string]bool{}}
	s.live.add(c)
	defer s.live.remove(c)

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case data := <-c.out:
				wctx, done := context.WithTimeout(ctx, 10*time.Second)
				err := ws.Write(wctx, websocket.MessageText, data)
				done()
				if err != nil {
					c.close("it could not be written to")
					return
				}
			}
		}
	}()
	go s.keepLive(ctx, ws, c)

	c.send(map[string]any{"type": "hello", "connection_id": id, "filter": c.filter})
	for {
		// The request's context: the server stopping ends it.
		_, data, err := ws.Read(r.Context())
		if err != nil {
			break
		}
		s.liveMessage(c, data)
	}
	c.closing.Do(func() { ws.Close(websocket.StatusNormalClosure, "") })
}

// send queues a message for the connection.
func (c *liveConn) send(msg any) {
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}
	select {
	case c.out <- data:
	default:
		c.close("it fell too far behind")
	}
}

// keepLive keeps a connection listed and its identity fresh, and pings it,
// until it ends.
func (s *Server) keepLive(ctx context.Context, ws *websocket.Conn, c *liveConn) {
	refresh, ping := time.NewTicker(liveRefresh), time.NewTicker(livePing)
	defer refresh.Stop()
	defer ping.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-refresh.C:
			if err := s.store.TouchLiveConnections([]string{c.id}); err != nil {
				log.Printf("live events: %v", err)
			}
			holder, token := c.holder()
			req, err := s.requestAs(context.WithoutCancel(ctx), holder, token)
			switch {
			case err != nil:
				c.close(err.Error())
				return
			case !HasPermission(req.Context(), rbac.PermEventsRead):
				c.close("it may no longer read events")
				return
			}
			c.mu.Lock()
			c.req = req
			c.mu.Unlock()
		case <-ping.C:
			pctx, done := context.WithTimeout(ctx, 10*time.Second)
			err := ws.Ping(pctx)
			done()
			if err != nil {
				c.close("it stopped answering pings")
				return
			}
		}
	}
}

// holder is the token or user the connection is for.
func (c *liveConn) holder() (string, bool) {
	if c.tokenID != "" {
		return c.tokenID, true
	}
	return c.userID, false
}

// liveMessage handles a message from a device.
func (s *Server) liveMessage(c *liveConn, data []byte) {
	var m liveMessage
	if err := json.Unmarshal(data, &m); err != nil {
		c.send(map[string]string{"type": "error", "message": "messages are JSON objects"})
		return
	}
	switch m.Type {
	case "filter":
		if m.Filter == nil {
			m.Filter = &liveFilter{}
		}
		c.mu.Lock()
		c.filter = *m.Filter
		c.mu.Unlock()
		c.send(map[string]any{"type": "filter", "filter": *m.Filter})
	case "reply":
		c.mu.Lock()
		sent := c.actions[m.EventID]
		delete(c.actions, m.EventID)
		c.mu.Unlock()
		if !sent {
			c.send(map[string]string{"type": "error", "message": "answer only the actions this connection was sent, once each"})
			return
		}
		acting := s.store.As(c.userID)
		if c.tokenID != "" {
			acting = s.store.AsToken(c.tokenID)
		}
		if _, err := acting.RecordActionReply(m.EventID, m.OK, m.Message); err != nil {
			if !errors.Is(err, store.ErrEventNotFound) {
				log.Printf("live events: %v", err)
			}
			c.send(map[string]string{"type": "error", "message": "the answer could not be kept"})
			return
		}
		c.send(map[string]string{"type": "replied", "event_id": m.EventID})
	default:
		c.send(map[string]string{"type": "error", "message": `type must be "filter" or "reply"`})
	}
}

// handleActionReplies lists the answers devices gave to an action pressed
// on an asset: GET /api/v1/assets/{id}/actions/replies?event_id=
func (s *Server) handleActionReplies(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, ok := s.assetFor(w, r, id, rbac.PermAssetsAct); !ok {
		return
	}
	eventID := r.URL.Query().Get("event_id")
	asset, err := s.store.EventAsset(eventID)
	if errors.Is(err, store.ErrEventNotFound) || (err == nil && asset != id) {
		writeJSONError(w, http.StatusNotFound, "Event not found")
		return
	}
	if err != nil {
		writeServerError(w, err, "read the event")
		return
	}
	replies, err := s.store.ActionReplies(eventID)
	if err != nil {
		writeServerError(w, err, "read the answers")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"replies": replies})
}

// handleLiveConnections lists the open live connections, on every server:
// GET /api/v1/tokens/connections
func (s *Server) handleLiveConnections(w http.ResponseWriter, r *http.Request) {
	conns, err := s.store.ListLiveConnections(time.Now().Add(-liveStale), "", "")
	if err != nil {
		writeServerError(w, err, "list live connections")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"connections": conns})
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
