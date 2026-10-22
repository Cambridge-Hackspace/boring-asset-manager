package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/mcpserver"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// slowTransport takes delay over each job, as a printer printing a long one
// does, and records whether the job's context had ended by then.
type slowTransport struct {
	printers.Transport
	delay time.Duration
	mu    sync.Mutex
	ended []error
}

func (s *slowTransport) Send(ctx context.Context, c printers.Connection, job []byte) error {
	time.Sleep(s.delay)
	s.mu.Lock()
	s.ended = append(s.ended, ctx.Err())
	s.mu.Unlock()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return s.Transport.Send(ctx, c, job)
}

// withShortTimeouts serves srv over HTTP with read and write timeouts far
// shorter than printing takes.
func withShortTimeouts(t *testing.T, srv *Server) *httptest.Server {
	t.Helper()
	ts := httptest.NewUnstartedServer(srv.Handler())
	ts.Config.ReadTimeout, ts.Config.WriteTimeout = 150*time.Millisecond, 150*time.Millisecond
	ts.Start()
	t.Cleanup(ts.Close)
	return ts
}

// TestPrintingOutlastsTheServerTimeouts checks printing that takes longer
// than the server's timeouts finishes, directly or through MCP: once the
// read timeout passed, the server ended the request's context, cutting a
// job off part way, and the caller got no answer.
func TestPrintingOutlastsTheServerTimeouts(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	ql := newFakeQL(t)
	printerID := createPrinter(t, srv, cookie, ql.port(), "dk-11201")
	ql90 := createTemplate(t, srv, cookie, landscapeTemplate)
	drill, _ := srv.store.CreateAsset("Drill", nil, nil, nil)
	slow := &slowTransport{Transport: srv.transport, delay: 500 * time.Millisecond}
	srv.transport = slow
	ts := withShortTimeouts(t, srv)

	body := `{"template_id":"` + ql90 + `","asset_ids":["` + drill.Asset.ID + `"]}`
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/printers/"+printerID+"/print", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("printing directly: %v", err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Errorf("printing directly: %d", res.StatusCode)
	}
	if jobs := ql.waitForJobs(t, 1, 3*time.Second); len(jobs) != 1 {
		t.Errorf("printing directly: the printer received %d jobs", len(jobs))
	}

	// Through MCP, as a tool asking the API in process.
	secret := tokenWith(t, srv, admin.ID, "Printer", rbac.PermAssetsRead, rbac.PermLabelsPrint)
	client := mcp.NewClient(&mcp.Implementation{Name: "test client", Version: "1"}, nil)
	cs, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{
		Endpoint: ts.URL + "/api/v1/mcp", HTTPClient: &http.Client{Transport: bearerTransport{secret}}, DisableStandaloneSSE: true,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	got, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "print_labels", Arguments: map[string]any{
		"printer_id": printerID, "template_id": ql90, "assets": []string{drill.Asset.ID},
	}})
	if err != nil || got.IsError {
		t.Fatalf("printing through MCP: %v %+v", err, got)
	}
	if jobs := ql.waitForJobs(t, 2, 3*time.Second); len(jobs) != 2 {
		t.Errorf("printing through MCP: the printer received %d jobs", len(jobs))
	}

	slow.mu.Lock()
	defer slow.mu.Unlock()
	for i, err := range slow.ended {
		if err != nil {
			t.Errorf("job %d was sent after its request ended: %v", i, err)
		}
	}
}

// TestMCPToolsKeepTheirCallerThroughout checks the API calls a tool makes
// act as the caller its request came in as: they read its token again, so
// a long tool failed part way, "Invalid, expired or revoked API token",
// when the token expired or an OAuth client refreshed it meanwhile.
func TestMCPToolsKeepTheirCallerThroughout(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	secret := tokenWith(t, srv, admin.ID, "Helper", rbac.PermAssetsRead)
	var (
		api     mcpserver.Backend
		tokenID string
	)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/mcp", nil)
	req.Header.Set("Authorization", "Bearer "+secret)
	Authenticate(srv.store)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tokenID = CurrentToken(r.Context()).ID
		_, api, _ = srv.mcpCaller(w, r)
	})).ServeHTTP(httptest.NewRecorder(), req)
	if api == nil {
		t.Fatal("the request was not taken")
	}

	// Gone between one call of the tool's and the next.
	if _, err := srv.store.RevokeToken(tokenID); err != nil {
		t.Fatal(err)
	}
	if res := api(context.Background(), http.MethodGet, "/api/v1/assets", nil, ""); res.Status != http.StatusOK {
		t.Errorf("a call later in the same request: %d %s", res.Status, res.Body)
	}
	// A new request is refused.
	if rec := doBearer(t, srv, secret, http.MethodGet, "/api/v1/assets"); rec.Code != http.StatusUnauthorized {
		t.Errorf("a new request with the revoked token: %d", rec.Code)
	}
}

func doBearer(t *testing.T, srv *Server, secret, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	req.Header.Set("Authorization", "Bearer "+secret)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec
}
