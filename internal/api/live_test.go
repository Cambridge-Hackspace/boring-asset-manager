package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/config"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
	"github.com/coder/websocket"
)

// liveServer serves srv over HTTP, reading events often, until the test
// ends.
func liveServer(t *testing.T, srv *Server) *httptest.Server {
	t.Helper()
	saved := livePoll
	livePoll = 20 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		srv.RunLive(ctx)
		close(done)
	}()
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(func() {
		ts.Close()
		cancel()
		<-done
		livePoll = saved
	})
	return ts
}

// deviceToken makes an API token whose role holds perms.
func deviceToken(t *testing.T, st *store.Store, name string, perms ...string) (*models.APIToken, string) {
	t.Helper()
	role, err := st.CreateRole(store.RoleInput{Name: name + " role", Permissions: perms})
	if err != nil {
		t.Fatal(err)
	}
	admins, _ := st.ListUsers()
	tok, secret, err := st.CreateToken(name, []string{role.ID}, nil, admins[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	return tok, secret
}

// device is a live connection as a device holds it: its messages are read
// as they come, since a read given up closes a WebSocket.
type device struct {
	conn *websocket.Conn
	msgs chan liveMsg
	err  chan error
}

// dial opens a live connection with a token.
func dial(t *testing.T, ts *httptest.Server, secret, query string) (*device, *http.Response, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	h := http.Header{}
	if secret != "" {
		h.Set("Authorization", "Bearer "+secret)
	}
	url := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/v1/events/live" + query
	c, resp, err := websocket.Dial(ctx, url, &websocket.DialOptions{HTTPHeader: h})
	if err != nil {
		return nil, resp, err
	}
	d := &device{conn: c, msgs: make(chan liveMsg, 100), err: make(chan error, 1)}
	go func() {
		for {
			_, data, err := c.Read(context.Background())
			if err != nil {
				d.err <- err
				return
			}
			var m liveMsg
			if err := json.Unmarshal(data, &m); err != nil {
				d.err <- err
				return
			}
			d.msgs <- m
		}
	}()
	t.Cleanup(func() { c.CloseNow() })
	return d, resp, nil
}

// liveMsg is a message from the server.
type liveMsg struct {
	Type         string       `json:"type"`
	ConnectionID string       `json:"connection_id"`
	Event        models.Event `json:"event"`
	EventID      string       `json:"event_id"`
	Message      string       `json:"message"`
}

// next waits for the next message.
func next(t *testing.T, d *device) liveMsg {
	t.Helper()
	select {
	case m := <-d.msgs:
		return m
	case err := <-d.err:
		t.Fatalf("reading: %v", err)
	case <-time.After(3 * time.Second):
		t.Fatal("no message came")
	}
	return liveMsg{}
}

// quiet checks nothing comes for a moment.
func quiet(t *testing.T, d *device) {
	t.Helper()
	select {
	case m := <-d.msgs:
		t.Errorf("unexpected message: %+v", m)
	case <-time.After(200 * time.Millisecond):
	}
}

func write(t *testing.T, d *device, msg string) {
	t.Helper()
	if err := d.conn.Write(context.Background(), websocket.MessageText, []byte(msg)); err != nil {
		t.Fatal(err)
	}
}

// actionFixture is a drawer in a cabinet with an Identify action offered.
func actionFixture(t *testing.T, st *store.Store, categories ...string) (drawer *models.AssetWithDetails) {
	t.Helper()
	identify, err := st.SaveAction("", store.ActionInput{Key: "identify", Label: "Identify"})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range categories {
		if _, err := st.SetCategoryActions(c, []models.CategoryAction{{ActionID: identify.ID, Cascades: true}}); err != nil {
			t.Fatal(err)
		}
	}
	cabinet, _ := st.CreateAsset("Cabinet A", nil, []models.AssetGeneratorKey{{GeneratorType: "qr", AssignedCode: "CAB-A"}}, nil)
	drawer, _ = st.CreateAsset("Drawer A3", categories, nil, nil)
	if _, err := st.AddLink(cabinet.Asset.ID, containsType, drawer.Asset.ID, true); err != nil {
		t.Fatal(err)
	}
	return drawer
}

func TestLiveEventsReachDevicesThatAnswer(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	st := srv.store
	parts, _ := st.CreateCategory("Parts", nil, "", false, false)
	drawer := actionFixture(t, st, parts.ID)
	ts := liveServer(t, srv)
	_, secret := deviceToken(t, st, "Cabinet A", rbac.PermEventsRead)

	c, _, err := dial(t, ts, secret, "?kinds=asset.action&actions=identify")
	if err != nil {
		t.Fatal(err)
	}
	if hello := next(t, c); hello.Type != "hello" || hello.ConnectionID == "" {
		t.Fatalf("hello: %+v", hello)
	}

	// Something else happening is not sent; the action is, at once.
	if rec := doJSON(t, srv, cookie, http.MethodPut, "/api/v1/assets/"+drawer.Asset.ID+"/condition", `{"condition":"needs_maintenance","note":"Sticks"}`); rec.Code != http.StatusOK {
		t.Fatal(rec.Body)
	}
	rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/assets/"+drawer.Asset.ID+"/actions/identify", "")
	if rec.Code != http.StatusCreated {
		t.Fatal(rec.Body)
	}
	var pressed struct {
		EventID string `json:"event_id"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &pressed)
	got := next(t, c)
	if got.Type != "event" || got.Event.ID != pressed.EventID || got.Event.Payload.Action == nil || got.Event.Payload.Action.Key != "identify" {
		t.Fatalf("the action: %+v", got)
	}
	if loc := got.Event.Payload.Action.Location; len(loc) != 1 || loc[0].Codes["qr"] != "CAB-A" {
		t.Errorf("its location: %+v", loc)
	}
	// A device that does not track people is not told who pressed it.
	if got.Event.Payload.Actor != nil || got.Event.ActorUserID != nil {
		t.Errorf("who pressed it: %+v", got.Event.Payload.Actor)
	}
	quiet(t, c)

	// It answers; whoever pressed sees the answer.
	write(t, c, `{"type":"reply","event_id":"`+pressed.EventID+`","ok":true,"message":"Lit drawer A3"}`)
	if ack := next(t, c); ack.Type != "replied" || ack.EventID != pressed.EventID {
		t.Fatalf("the acknowledgment: %+v", ack)
	}
	write(t, c, `{"type":"reply","event_id":"`+pressed.EventID+`","ok":true}`)
	if again := next(t, c); again.Type != "error" {
		t.Errorf("answering twice: %+v", again)
	}
	rec = doJSON(t, srv, cookie, http.MethodGet, "/api/v1/assets/"+drawer.Asset.ID+"/actions/replies?event_id="+pressed.EventID, "")
	if !strings.Contains(rec.Body.String(), `"name":"Cabinet A","token":true,"ok":true,"message":"Lit drawer A3"`) {
		t.Errorf("the answers: %s", rec.Body)
	}
	if rec := doJSON(t, srv, cookie, http.MethodGet, "/api/v1/assets/"+drawer.Asset.ID+"/actions/replies?event_id=nope", ""); rec.Code != http.StatusNotFound {
		t.Errorf("answers to a missing event: %d", rec.Code)
	}

	// A new filter: everything now.
	write(t, c, `{"type":"filter","filter":{}}`)
	if f := next(t, c); f.Type != "filter" {
		t.Fatalf("the filter: %+v", f)
	}
	if rec := doJSON(t, srv, cookie, http.MethodPut, "/api/v1/assets/"+drawer.Asset.ID+"/condition", `{"condition":"ok"}`); rec.Code != http.StatusOK {
		t.Fatal(rec.Body)
	}
	if e := next(t, c); e.Event.Kind != models.EventAssetConditionChanged {
		t.Errorf("after widening the filter: %+v", e)
	}
	write(t, c, `{"type":"shout"}`)
	if e := next(t, c); e.Type != "error" {
		t.Errorf("an unknown message: %+v", e)
	}
	write(t, c, `{"type":"reply","event_id":"nope","ok":true}`)
	if e := next(t, c); e.Type != "error" {
		t.Errorf("answering an action never sent: %+v", e)
	}

	// The tokens page lists it.
	rec = doJSON(t, srv, cookie, http.MethodGet, "/api/v1/tokens/connections", "")
	if !strings.Contains(rec.Body.String(), `"token_id"`) {
		t.Errorf("connections: %s", rec.Body)
	}
}

func TestLiveConnectionsAreLimited(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	st := srv.store
	ts := liveServer(t, srv)

	if _, resp, err := dial(t, ts, "", ""); err == nil || resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("signed out: %v %v", resp, err)
	}
	_, reader := deviceToken(t, st, "Reader", rbac.PermAssetsRead)
	if _, resp, err := dial(t, ts, reader, ""); err == nil || resp == nil || resp.StatusCode != http.StatusForbidden {
		t.Errorf("without events.read: %v %v", resp, err)
	}

	tok, secret := deviceToken(t, st, "Cabinet", rbac.PermEventsRead)
	var conns []*device
	for i := 0; i < maxLivePerHolder; i++ {
		c, _, err := dial(t, ts, secret, "")
		if err != nil {
			t.Fatalf("connection %d: %v", i, err)
		}
		next(t, c)
		conns = append(conns, c)
	}
	if _, resp, err := dial(t, ts, secret, ""); err == nil || resp == nil || resp.StatusCode != http.StatusTooManyRequests {
		t.Errorf("one too many: %v %v", resp, err)
	}

	// Revoking the token closes them, saying why.
	if rec := doJSON(t, srv, cookie, http.MethodDelete, "/api/v1/tokens/"+tok.ID, ""); rec.Code != http.StatusOK {
		t.Fatal(rec.Body)
	}
	var err error
	select {
	case err = <-conns[0].err:
	case <-time.After(3 * time.Second):
		t.Fatal("still open after revoking")
	}
	var closed websocket.CloseError
	if !errors.As(err, &closed) || closed.Code != websocket.StatusPolicyViolation || closed.Reason != "the token was revoked" {
		t.Errorf("after revoking: %v", err)
	}
	// Closed connections leave the list.
	deadline := time.Now().Add(3 * time.Second)
	for {
		open, _ := st.ListLiveConnections(time.Now().Add(-time.Minute), tok.ID, "")
		if len(open) == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("still listed: %d", len(open))
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestLiveEventsKeepToTheirScopes checks a device is sent only events about
// assets in the scopes it holds events.read in.
func TestLiveEventsKeepToTheirScopes(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	st := srv.store
	tools, _ := st.CreateScope("tools")
	toolsCat, _ := st.CreateCategory("Tools", nil, "", false, false)
	if _, err := st.SetCategoryScopes(toolsCat.ID, []string{tools.ID}); err != nil {
		t.Fatal(err)
	}
	kitchen, _ := st.CreateCategory("Kitchen", nil, "", false, false)
	drill := actionFixture(t, st, toolsCat.ID, kitchen.ID)
	kettle, _ := st.CreateAsset("Kettle", []string{kitchen.ID}, nil, nil)
	ts := liveServer(t, srv)
	_, secret := deviceToken(t, st, "Tool cabinet", "tools:"+rbac.PermEventsRead)
	c, _, err := dial(t, ts, secret, "?kinds=asset.action")
	if err != nil {
		t.Fatal(err)
	}
	next(t, c)
	as := st.As(admin.ID)
	if _, err := as.RecordAssetAction(kettle.Asset.ID, "identify"); err != nil {
		t.Fatal(err)
	}
	if _, err := as.RecordAssetAction(drill.Asset.ID, "identify"); err != nil {
		t.Fatal(err)
	}
	// Recorded outside a request: read on the next poll.
	if e := next(t, c); e.Event.AssetID == nil || *e.Event.AssetID != drill.Asset.ID {
		t.Errorf("sent: %+v", e.Event)
	}
	quiet(t, c)
}

// TestLiveEventsAcrossServers checks an event recorded on one server
// reaches a device connected to another sharing its database, and that a
// category filter takes in subcategories.
func TestLiveEventsAcrossServers(t *testing.T) {
	first, admin, _ := setupTestServer(t)
	cfg := config.DefaultConfig()
	testStorage(t, cfg)
	second := NewServer(cfg, first.store)
	st := first.store
	parts, _ := st.CreateCategory("Parts", nil, "", false, false)
	small, _ := st.CreateCategory("Small parts", &parts.ID, "", false, false)
	drawer := actionFixture(t, st, small.ID)
	ts := liveServer(t, second)
	_, secret := deviceToken(t, st, "Cabinet", rbac.PermEventsRead)
	c, _, err := dial(t, ts, secret, "?categories="+parts.ID)
	if err != nil {
		t.Fatal(err)
	}
	next(t, c)
	if rec := doJSON(t, first, sessionCookie(t, first, admin.ID), http.MethodPost, "/api/v1/assets/"+drawer.Asset.ID+"/actions/identify", ""); rec.Code != http.StatusCreated {
		t.Fatal(rec.Body)
	}
	if e := next(t, c); e.Event.Kind != models.EventAssetAction {
		t.Errorf("from the other server: %+v", e)
	}
}

func TestLiveFilters(t *testing.T) {
	q := liveFilterOf(map[string][]string{"kinds": {"asset.action, asset.running_low"}, "actions": {""}, "categories": {"c1"}})
	if len(q.Kinds) != 2 || q.Actions != nil || len(q.Categories) != 1 {
		t.Errorf("parsed: %+v", q)
	}
	action := models.Event{Kind: models.EventAssetAction, Payload: models.EventPayload{Action: &models.EventAction{Key: "identify"}}}
	for name, c := range map[string]struct {
		f    liveFilter
		cats map[string]bool
		want bool
	}{
		"nothing narrows":       {liveFilter{}, nil, true},
		"its kind":              {liveFilter{Kinds: []string{"asset.action"}}, nil, true},
		"another kind":          {liveFilter{Kinds: []string{"asset.overdue"}}, nil, false},
		"its action":            {liveFilter{Actions: []string{"identify"}}, nil, true},
		"another action":        {liveFilter{Actions: []string{"open"}}, nil, false},
		"in the category":       {liveFilter{Categories: []string{"c1"}}, map[string]bool{"c1": true}, true},
		"not in the category":   {liveFilter{Categories: []string{"c1"}}, map[string]bool{"c2": true}, false},
		"an action filter only": {liveFilter{Actions: []string{"identify"}, Kinds: []string{"asset.action"}}, nil, true},
	} {
		if got := c.f.matches(action, c.cats); got != c.want {
			t.Errorf("%s: %v", name, got)
		}
	}
	if (liveFilter{Actions: []string{"identify"}}).matches(models.Event{Kind: models.EventAssetOverdue}, nil) {
		t.Error("an action filter let a non-action through")
	}
}

// TestLiveConnectionsOutlastServerTimeouts checks a connection lives past
// the server's read and write timeouts, which bam serve sets to 15 seconds.
func TestLiveConnectionsOutlastServerTimeouts(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	st := srv.store
	parts, _ := st.CreateCategory("Parts", nil, "", false, false)
	drawer := actionFixture(t, st, parts.ID)
	saved := livePoll
	livePoll = 20 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		srv.RunLive(ctx)
		close(done)
	}()
	ts := httptest.NewUnstartedServer(srv.Handler())
	ts.Config.ReadTimeout, ts.Config.WriteTimeout = 300*time.Millisecond, 300*time.Millisecond
	ts.Start()
	t.Cleanup(func() {
		ts.Close()
		cancel()
		<-done
		livePoll = saved
	})
	_, secret := deviceToken(t, st, "Cabinet", rbac.PermEventsRead)
	d, _, err := dial(t, ts, secret, "")
	if err != nil {
		t.Fatal(err)
	}
	next(t, d)
	time.Sleep(time.Second)
	if _, err := st.As(admin.ID).RecordAssetAction(drawer.Asset.ID, "identify"); err != nil {
		t.Fatal(err)
	}
	if e := next(t, d); e.Event.Kind != models.EventAssetAction {
		t.Errorf("after the timeouts: %+v", e)
	}
}

// TestLiveConnectionsCloseWhenPermissionGoes checks a device whose role
// no longer holds events.read is closed at the next check.
func TestLiveConnectionsCloseWhenPermissionGoes(t *testing.T) {
	srv, _, _ := setupTestServer(t)
	st := srv.store
	saved := liveRefresh
	liveRefresh = 50 * time.Millisecond
	t.Cleanup(func() { liveRefresh = saved })
	ts := liveServer(t, srv)
	role, _ := st.CreateRole(store.RoleInput{Name: "Cabinets", Permissions: []string{rbac.PermEventsRead}})
	admins, _ := st.ListUsers()
	_, secret, err := st.CreateToken("Cabinet", []string{role.ID}, nil, admins[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	d, _, err := dial(t, ts, secret, "")
	if err != nil {
		t.Fatal(err)
	}
	next(t, d)
	if _, err := st.UpdateRole(role.ID, store.RoleInput{Name: "Cabinets", Permissions: []string{rbac.PermAssetsRead}}); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-d.err:
		var closed websocket.CloseError
		if !errors.As(err, &closed) || closed.Reason != "it may no longer read events" {
			t.Errorf("closed: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Error("still open")
	}
}

func TestWebSocketRequests(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/api/v1/events/live", nil)
	if webSocketRequest(r) {
		t.Error("a plain GET")
	}
	r.Header.Set("Connection", "keep-alive, Upgrade")
	r.Header.Set("Upgrade", "WebSocket")
	r.Header.Set("Sec-WebSocket-Version", "13")
	r.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
	if !webSocketRequest(r) {
		t.Error("a WebSocket request")
	}
	r.Header.Set("Sec-WebSocket-Version", "8")
	if webSocketRequest(r) {
		t.Error("an old version")
	}
}
