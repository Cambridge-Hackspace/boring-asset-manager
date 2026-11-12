package printers

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeAIMS is SoluM's AIMS, as its documentation describes it: tokens, a
// label's type, image pushes and their history.
type fakeAIMS struct {
	mu        sync.Mutex
	password  string
	tokens    int    // tokens given, the last of them good
	expire    bool   // the next push finds the token expired
	label     string // the label's type, as JSON
	failPush  []int  // statuses the next pushes answer
	results   []string
	pushes    []map[string]any
	queries   []string
	signIns   []string
	refreshes int
}

func (f *fakeAIMS) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	answer := func(code int, v any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(v)
	}
	give := func() {
		f.tokens++
		answer(200, map[string]any{"responseCode": "200", "responseMessage": map[string]any{
			"access_token": "access" + string(rune('0'+f.tokens)), "refresh_token": "refresh" + string(rune('0'+f.tokens)), "token_type": "Bearer", "expires_in": 86400}})
	}
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	switch r.URL.Path {
	case "/common/api/v2/token":
		f.signIns = append(f.signIns, body["username"].(string))
		if body["password"] != f.password {
			answer(401, map[string]any{"responseCode": "401", "responseMessage": "Unauthorized"})
			return
		}
		give()
		return
	case "/common/api/v2/token/refresh":
		f.refreshes++
		if body["refreshToken"] != "refresh"+string(rune('0'+f.tokens)) {
			answer(401, map[string]any{"responseCode": "401", "responseMessage": "Unauthorized"})
			return
		}
		give()
		return
	}
	if r.Header.Get("Authorization") != "Bearer access"+string(rune('0'+f.tokens)) {
		answer(401, map[string]any{"responseCode": "401", "responseMessage": "Unauthorized"})
		return
	}
	switch r.URL.Path {
	case "/common/api/v2/common/labels/type/info":
		f.queries = append(f.queries, r.URL.RawQuery)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, f.label)
	case "/common/api/v2/common/labels/image":
		if f.expire {
			f.expire = false
			answer(401, map[string]any{"responseCode": "401", "responseMessage": "Token expired"})
			return
		}
		f.queries = append(f.queries, r.URL.RawQuery)
		if len(f.failPush) > 0 {
			code := f.failPush[0]
			f.failPush = f.failPush[1:]
			answer(code, map[string]any{"responseCode": "405", "responseMessage": "Invalid Parameter"})
			return
		}
		f.pushes = append(f.pushes, body)
		answer(200, map[string]any{"responseCode": "200", "responseMessage": "SUCCESS", "customBatchId": "batch-1"})
	case "/common/api/v2/common/articles/history/detail":
		f.queries = append(f.queries, r.URL.RawQuery)
		result := "PROCESSING"
		if len(f.results) > 0 {
			result, f.results = f.results[0], f.results[1:]
		}
		answer(200, map[string]any{"updateHistoryList": []map[string]any{{"result": result, "errorCode": "0"}}})
	default:
		answer(404, map[string]any{"responseCode": "404", "responseMessage": "Not found"})
	}
}

const label29BWR = `{"name":"Newton 2.9","displayWidth":296,"displayHeight":128,"colorType":"TERNARY_RED","color":"BWR","resolution":111,"totalPage":7}`

func solumPNG(t *testing.T, panel Panel) []byte {
	t.Helper()
	plan, err := solumDisplay.Plan(Settings{Model: panel.ID, Media: panel.ID}, Fit{})
	if err != nil {
		t.Fatal(err)
	}
	b, err := plan.Encode([]*image.Gray{image.NewGray(image.Rect(0, 0, panel.Width, panel.Height))})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func withQuickSolum(t *testing.T) {
	t.Helper()
	wait, poll := solumWait, solumPoll
	solumWait, solumPoll = 200*time.Millisecond, 10*time.Millisecond
	t.Cleanup(func() { solumWait, solumPoll = wait, poll })
}

func TestALabelIsPushedThroughAIMS(t *testing.T) {
	withQuickSolum(t)
	aims := &fakeAIMS{password: "pw", label: label29BWR, results: []string{"PROCESSING", "SUCCESS"}}
	srv := httptest.NewServer(aims)
	defer srv.Close()
	panel, ok := DisplayPanel(DriverSolum, "2.9in-296x128-bwr")
	if !ok {
		t.Fatal("no panel")
	}
	img := solumPNG(t, panel)
	c := &SolumClient{HTTP: srv.Client()}
	account := SolumAccount{Base: srv.URL + "/", Company: "TST", Store: "S001", Username: "ops@example.org", Password: "pw"}
	if err := c.PushSolum(context.Background(), account, "04507B0AC391?page=2", panel, img); err != nil {
		t.Fatal(err)
	}
	if len(aims.pushes) != 1 || aims.tokens != 1 {
		t.Fatalf("pushes %d, tokens %d", len(aims.pushes), aims.tokens)
	}
	p := aims.pushes[0]
	if p["labelCode"] != "04507B0AC391" || p["page"] != float64(2) || p["frontPage"] != float64(2) || p["dithering"] != false {
		t.Fatalf("push %v", p)
	}
	sent, _ := base64.StdEncoding.DecodeString(p["image"].(string))
	if got, err := png.Decode(bytes.NewReader(sent)); err != nil || got.Bounds().Dx() != 296 || got.Bounds().Dy() != 128 {
		t.Fatalf("image %v", err)
	}
	if q := strings.Join(aims.queries, " | "); !strings.Contains(q, "labelCode=04507B0AC391") || !strings.Contains(q, "company=TST") || !strings.Contains(q, "store=S001") ||
		!strings.Contains(q, "cbId=yes") || !strings.Contains(q, "name=batch-1") {
		t.Fatalf("queries %s", q)
	}

	// The token is kept; one that has expired is refreshed, once.
	aims.expire, aims.results = true, []string{"SUCCESS"}
	if err := c.PushSolum(context.Background(), account, "04507B0AC391", panel, img); err != nil {
		t.Fatal(err)
	}
	if len(aims.signIns) != 1 || aims.refreshes != 1 || aims.tokens != 2 || aims.pushes[1]["page"] != float64(1) {
		t.Fatalf("sign-ins %v, refreshes %d, tokens %d", aims.signIns, aims.refreshes, aims.tokens)
	}
}

func TestAIMSRefusalsSayWhatWasWrong(t *testing.T) {
	withQuickSolum(t)
	aims := &fakeAIMS{password: "pw", label: label29BWR}
	srv := httptest.NewServer(aims)
	defer srv.Close()
	c := &SolumClient{HTTP: srv.Client()}
	account := SolumAccount{Base: srv.URL, Company: "TST", Store: "S001", Username: "ops", Password: "pw"}
	bwr, _ := DisplayPanel(DriverSolum, "2.9in-296x128-bwr")
	img := solumPNG(t, bwr)

	// A label that is not its model's is refused before anything is pushed.
	for _, model := range []string{"2.9in-296x128-bwy", "4.2in-400x300-bwr"} {
		p, _ := DisplayPanel(DriverSolum, model)
		if err := c.PushSolum(context.Background(), account, "04507B0AC391", p, solumPNG(t, p)); err == nil || !strings.Contains(err.Error(), "choose the model that matches it") {
			t.Errorf("%s: %v", model, err)
		}
	}
	if err := c.PushSolum(context.Background(), account, "04507B0AC391?page=8", bwr, img); err == nil || !strings.Contains(err.Error(), "has 7 pages") {
		t.Errorf("page 8: %v", err)
	}
	if len(aims.pushes) != 0 {
		t.Fatalf("pushed %d", len(aims.pushes))
	}
	// AIMS refusing the push is final; failing, it is tried again.
	aims.failPush = []int{405}
	if err := c.PushSolum(context.Background(), account, "04507B0AC391", bwr, img); err == nil || !strings.Contains(err.Error(), "Invalid Parameter") || len(aims.pushes) != 0 {
		t.Errorf("405: %v", err)
	}
	aims.failPush, aims.results = []int{502}, []string{"SUCCESS"}
	if err := c.PushSolum(context.Background(), account, "04507B0AC391", bwr, img); err != nil || len(aims.pushes) != 1 {
		t.Errorf("502 then success: %v", err)
	}
	// A gateway that cannot reach the label fails the job; one still on its
	// way does not.
	aims.results = []string{"FAILED TO TRANSMIT"}
	if err := c.PushSolum(context.Background(), account, "04507B0AC391", bwr, img); err == nil || !strings.Contains(err.Error(), "gateway did not reach it") {
		t.Errorf("failed to transmit: %v", err)
	}
	aims.results = []string{"TIMEOUT", "TIMEOUT", "TIMEOUT", "TIMEOUT", "TIMEOUT", "TIMEOUT", "TIMEOUT", "TIMEOUT", "TIMEOUT", "TIMEOUT", "TIMEOUT", "TIMEOUT", "TIMEOUT", "TIMEOUT", "TIMEOUT", "TIMEOUT", "TIMEOUT", "TIMEOUT", "TIMEOUT", "TIMEOUT", "TIMEOUT", "TIMEOUT", "TIMEOUT", "TIMEOUT"}
	if err := c.PushSolum(context.Background(), account, "04507B0AC391", bwr, img); err != nil {
		t.Errorf("timeout: %v", err)
	}
	// A wrong password says so, and never says it.
	account.Password = "hunter2"
	if err := (&SolumClient{HTTP: srv.Client()}).PushSolum(context.Background(), account, "04507B0AC391", bwr, img); err == nil || !strings.Contains(err.Error(), "refused the username or password") || strings.Contains(err.Error(), "hunter2") {
		t.Errorf("wrong password: %v", err)
	}
	// Without an account, nothing is asked.
	if err := c.PushSolum(context.Background(), SolumAccount{Base: srv.URL}, "04507B0AC391", bwr, img); err == nil || !strings.Contains(err.Error(), "not set up") {
		t.Errorf("no account: %v", err)
	}
}

func TestSolumConnections(t *testing.T) {
	for in, want := range map[string]string{"04507b0ac391": "04507B0AC391", "04507B0AC391?page=1": "04507B0AC391", " 04507B0AC391?page=10 ": "04507B0AC391?page=10"} {
		c, err := NormalizeConnection(Connection{Type: ConnSolum, Address: in})
		if err != nil || c.Address != want {
			t.Errorf("%q: %q %v", in, c.Address, err)
		}
	}
	for _, bad := range []Connection{
		{Type: ConnSolum, Address: "0450"},
		{Type: ConnSolum, Address: "04507B0AC391?page=11"},
		{Type: ConnSolum, Address: "04507B0AC391?flash=red"},
		{Type: ConnSolum, Address: "04507B0AC391", Relay: "workshop"},
	} {
		if _, err := NormalizeConnection(bad); !errors.Is(err, ErrInvalidPrinter) {
			t.Errorf("%+v: %v", bad, err)
		}
	}
	if err := Validate("Bin A1", DriverSolum, Connection{Type: ConnPull}, Settings{Model: "2.9in-296x128-bwr", Media: "2.9in-296x128-bwr"}); err == nil {
		t.Error("a SoluM label that pulls")
	}
	if err := NewDispatcher(nil).Send(context.Background(), Connection{Type: ConnSolum, Address: "04507B0AC391"}, nil); !errors.Is(err, ErrUnreachable) {
		t.Errorf("send: %v", err)
	}
}
