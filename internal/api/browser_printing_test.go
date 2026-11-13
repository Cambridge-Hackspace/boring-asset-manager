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

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers/niimbot"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers/tspl"
	"github.com/coder/websocket"
)

// browserTab is the browser's side of printing from it: it sends the
// print request, and keeps what the server says and sends.
type browserTab struct {
	t    *testing.T
	conn *websocket.Conn
}

func openBrowserPrint(t *testing.T, ts *httptest.Server, cookie *http.Cookie, printerID string) *browserTab {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	h := http.Header{}
	h.Set("Cookie", cookie.String())
	url := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/v1/printers/" + printerID + "/print/browser"
	c, resp, err := websocket.Dial(ctx, url, &websocket.DialOptions{HTTPHeader: h})
	if err != nil {
		t.Fatalf("dial: %v (%+v)", err, resp)
	}
	t.Cleanup(func() { c.CloseNow() })
	return &browserTab{t: t, conn: c}
}

func (b *browserTab) send(msg any) {
	b.t.Helper()
	data, _ := json.Marshal(msg)
	if err := b.conn.Write(context.Background(), websocket.MessageText, data); err != nil {
		b.t.Fatal(err)
	}
}

// next reads the next message: text decoded into m, or binary returned.
func (b *browserTab) next() (m map[string]any, binary []byte) {
	b.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	typ, data, err := b.conn.Read(ctx)
	if err != nil {
		b.t.Fatalf("read: %v", err)
	}
	if typ == websocket.MessageBinary {
		return nil, data
	}
	if err := json.Unmarshal(data, &m); err != nil {
		b.t.Fatal(err)
	}
	return m, nil
}

func createBrowserPrinter(t *testing.T, srv *Server, cookie *http.Cookie, body string) models.Printer {
	t.Helper()
	rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/printers", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("creating printer: %d %s", rec.Code, rec.Body.String())
	}
	var created struct {
		Printer models.Printer `json:"printer"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	return created.Printer
}

func TestPrintFromTheBrowser(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// A browser connection needs no Bluetooth on the server, nor any
	// allowed: the person at the browser picks the printer. TSPL has no
	// profile of its own, so the connection gives one.
	p := createBrowserPrinter(t, srv, cookie, `{"name":"Phone Rollo","driver":"tspl","model":"Rollo X1038","media":"2x1",
		"connection":{"type":"browser","address":"?service=fff0&write=fff2&chunk=180"}}`)
	if want := "?service=0000fff0-0000-1000-8000-00805f9b34fb&write=0000fff2-0000-1000-8000-00805f9b34fb&chunk=180"; p.Connection.Address != want {
		t.Errorf("saved address %q, want %q", p.Connection.Address, want)
	}
	rollo2x1 := createTemplate(t, srv, cookie, `{"id":"rollo2x1","name":"Rollo 2x1","units":"in","width":2,"height":1,"dpi":203,"elements":[
  {"type":"text","x":0.1,"y":0.2,"width":1.8,"height":0.4,"font_size":10,"content_binding":"{{asset.name}}"}]}`)
	a, _ := srv.store.CreateAsset("Drill", nil, nil, nil)
	request := map[string]any{"type": "print", "template_id": rollo2x1, "asset_ids": []string{a.Asset.ID}, "copies": 2}

	// The print form learns how to reach it.
	rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/labels/options", `{"asset_ids":["`+a.Asset.ID+`"]}`)
	var opts LabelOptionsResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &opts)
	if len(opts.Printers) != 1 || opts.Printers[0].Browser == nil || opts.Printers[0].Browser.Write != "0000fff2-0000-1000-8000-00805f9b34fb" || opts.Printers[0].Browser.Chunk != 180 {
		t.Fatalf("label options' printers: %s", rec.Body.String())
	}

	// The server cannot print to it itself.
	rec = doJSON(t, srv, cookie, http.MethodPost, "/api/v1/printers/"+p.ID+"/print", `{"template_id":"`+rollo2x1+`","asset_ids":["`+a.Asset.ID+`"]}`)
	if rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "from a browser") {
		t.Fatalf("print from the server: %d %s", rec.Code, rec.Body.String())
	}

	// The browser is sent the job, writes it, and says so.
	tab := openBrowserPrint(t, ts, cookie, p.ID)
	tab.send(request)
	first, _ := tab.next()
	if first["type"] != "job" || first["labels"] != float64(2) {
		t.Fatalf("first message: %v", first)
	}
	var written []byte
	for {
		m, data := tab.next()
		if m == nil {
			written = append(written, data...)
			continue
		}
		if m["type"] != "sent" {
			t.Fatalf("before the end of the job: %v", m)
		}
		break
	}
	if job, err := tspl.DecodeJob(written); err != nil || len(job.Labels) == 0 || len(written) != int(first["bytes"].(float64)) {
		t.Errorf("the browser was sent %d bytes that decode to %+v, %v", len(written), job, err)
	}
	tab.send(map[string]string{"type": "done"})
	if m, _ := tab.next(); m["type"] != "printed" || m["labels"] != float64(2) {
		t.Fatalf("after writing: %v", m)
	}
	events, _, err := srv.store.ListAssetEvents(a.Asset.ID, "", 1)
	if err != nil || len(events) != 1 || events[0].Kind != models.EventAssetLabelPrinted ||
		*events[0].Payload.Label != (models.EventLabel{Template: "Rollo 2x1", Printer: "Phone Rollo", Copies: 2}) {
		t.Errorf("the asset's history: %+v, %v", events, err)
	}

	// A browser that cannot write to the printer fails the job, saying
	// why.
	tab = openBrowserPrint(t, ts, cookie, p.ID)
	tab.send(request)
	tab.next()
	tab.send(map[string]string{"type": "failed", "message": "GATT operation failed"})
	for {
		m, _ := tab.next()
		if m == nil || m["type"] == "sent" {
			continue
		}
		if m["type"] != "error" || !strings.Contains(m["message"].(string), "could not write to the printer: GATT operation failed") {
			t.Fatalf("after failing: %v", m)
		}
		break
	}

	// A request the server refuses is answered with its reason.
	tab = openBrowserPrint(t, ts, cookie, p.ID)
	tab.send(map[string]any{"type": "print", "template_id": "nonesuch", "asset_ids": []string{a.Asset.ID}})
	if m, _ := tab.next(); m["type"] != "error" || !strings.Contains(strings.ToLower(m["message"].(string)), "template") {
		t.Fatalf("an unknown template: %v", m)
	}

	// Only a browser connection is printed to from a browser, and only
	// over a WebSocket.
	other := createBrowserPrinter(t, srv, cookie, `{"name":"Desk Rollo","driver":"tspl","model":"Rollo X1038","media":"2x1","connection":{"type":"tcp","address":"192.0.2.1"}}`)
	if rec := doJSON(t, srv, cookie, http.MethodGet, "/api/v1/printers/"+p.ID+"/print/browser", ""); rec.Code != http.StatusUpgradeRequired {
		t.Errorf("without a WebSocket: %d %s", rec.Code, rec.Body.String())
	}
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/printers/"+other.ID+"/print/browser", nil)
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Sec-WebSocket-Version", "13")
	req.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
	req.AddCookie(cookie)
	if resp, err := http.DefaultClient.Do(req); err != nil || resp.StatusCode != http.StatusConflict {
		t.Errorf("a printer on the network: %v %v", resp, err)
	}
}

// niimbotReplies is the command each answers with.
var niimbotReplies = map[byte]byte{0xC1: 0xC2, 0x1A: 0x1B, 0x21: 0x31, 0x23: 0x33, 0x01: 0x02, 0x20: 0x30, 0x03: 0x04,
	0x13: 0x14, 0x15: 0x16, 0xE3: 0xE4, 0xA3: 0xB3, 0xF3: 0xF4}

func TestAConversationIsHeldThroughTheBrowser(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	saved := browserTimeout
	browserTimeout = 2 * time.Second
	defer func() { browserTimeout = saved }()

	// The driver gives the profile, and its protocol.
	p := createBrowserPrinter(t, srv, cookie, `{"name":"Phone Niimbot","driver":"niimbot","model":"B1","media":"40x30","connection":{"type":"browser","address":""}}`)
	profile, err := printers.BrowserProfile(p.Driver, p.Connection)
	if err != nil || profile.Protocol != printers.ProtocolNiimbot || profile.Chunk != 20 {
		t.Fatalf("profile %+v, %v", profile, err)
	}
	tmpl := createTemplate(t, srv, cookie, `{"id":"nb4030","name":"Niimbot 40x30","units":"mm","width":40,"height":30,"dpi":203,"elements":[
  {"type":"text","x":2,"y":5,"width":36,"height":10,"font_size":10,"content_binding":"{{asset.name}}"}]}`)
	a, _ := srv.store.CreateAsset("Drill", nil, nil, nil)

	tab := openBrowserPrint(t, ts, cookie, p.ID)
	tab.send(map[string]any{"type": "print", "template_id": tmpl, "asset_ids": []string{a.Asset.ID}, "copies": 2})
	if m, _ := tab.next(); m["type"] != "job" {
		t.Fatalf("first message: %v", m)
	}
	// The browser relays the printer, which answers each command, a page
	// printed at a time.
	var reader niimbot.Reader
	var got []byte
	ended, printed := 0, 0
	for {
		m, data := tab.next()
		if m != nil {
			if m["type"] != "printed" || m["labels"] != float64(2) {
				t.Fatalf("at the end: %v", m)
			}
			break
		}
		for _, pk := range reader.Add(data) {
			got = append(got, pk.Cmd)
			reply, ok := niimbotReplies[pk.Cmd]
			if !ok {
				continue
			}
			answer := []byte{1}
			switch pk.Cmd {
			case niimbot.CmdRFIDInfo:
				answer = []byte{0}
			case niimbot.CmdPageEnd:
				ended++
			case niimbot.CmdPrintStatus:
				printed = min(printed+1, ended)
				answer = []byte{0, byte(printed), 100, 100}
			}
			if err := tab.conn.Write(context.Background(), websocket.MessageBinary, niimbot.Packet{Cmd: reply, Data: answer}.Bytes()); err != nil {
				t.Fatal(err)
			}
		}
	}
	if printed != 2 || got[len(got)-1] != niimbot.CmdPrintEnd {
		t.Errorf("printed %d, commands % x", printed, got)
	}

	// A printer that stops answering fails the job, saying which command
	// it did not answer.
	tab = openBrowserPrint(t, ts, cookie, p.ID)
	tab.send(map[string]any{"type": "print", "template_id": tmpl, "asset_ids": []string{a.Asset.ID}})
	tab.next()
	for {
		m, _ := tab.next()
		if m == nil {
			continue
		}
		if m["type"] != "error" || !strings.Contains(m["message"].(string), "did not answer") {
			t.Fatalf("a silent printer: %v", m)
		}
		break
	}
}

// TestAJobStoppedMidWriteStillSaysWhy stops a job while the server is
// writing it to a browser that isn't reading yet, as when the browser says
// it can't write to the printer while pieces are still on their way. The
// connection must outlast the stop, so that the browser is told why: a
// write whose context ends closes the WebSocket.
func TestAJobStoppedMidWriteStillSaysWhy(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer ws.CloseNow()
		link := &browserLink{ws: ws, received: make(chan []byte, 64), done: make(chan struct{})}
		job, stop := context.WithCancelCause(context.Background())
		// Far more than the connection buffers, so that a write is under
		// way, waiting on the browser, when the job stops.
		go func() {
			time.Sleep(200 * time.Millisecond)
			stop(errors.New("the browser could not write to the printer: GATT operation failed"))
		}()
		if err := link.stream(job, make([]byte, 64<<20)); err == nil {
			t.Error("a stopped job was streamed whole")
		}
		_ = writeBrowserMessage(context.Background(), ws, map[string]any{"type": "error", "message": context.Cause(job).Error()})
		ws.Close(websocket.StatusNormalClosure, "")
	}))
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(ts.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	time.Sleep(400 * time.Millisecond) // the browser, busy, reads nothing until the job has stopped
	for {
		typ, data, err := c.Read(ctx)
		if err != nil {
			t.Fatalf("the connection ended before the browser was told why: %v", err)
		}
		if typ == websocket.MessageBinary {
			continue
		}
		if !strings.Contains(string(data), "GATT operation failed") {
			t.Fatalf("told %s", data)
		}
		return
	}
}
