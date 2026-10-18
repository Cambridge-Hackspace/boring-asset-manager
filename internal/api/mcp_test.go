package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/mcpserver"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// tokenWith makes an API token holding a role with the permissions, and
// returns its secret.
func tokenWith(t *testing.T, srv *Server, createdBy, name string, perms ...string) string {
	t.Helper()
	role, err := srv.store.CreateRole(store.RoleInput{Name: "role for " + name, Permissions: perms})
	if err != nil {
		t.Fatal(err)
	}
	_, secret, err := srv.store.CreateToken(name, []string{role.ID}, nil, createdBy)
	if err != nil {
		t.Fatal(err)
	}
	return secret
}

// bearerTransport sends an API token with every request.
type bearerTransport struct{ secret string }

func (b bearerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+b.secret)
	return http.DefaultTransport.RoundTrip(r)
}

// mcpSession connects an MCP client to the server as the token.
func mcpSession(t *testing.T, srv *Server, secret string) *mcp.ClientSession {
	t.Helper()
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	client := mcp.NewClient(&mcp.Implementation{Name: "test client", Version: "1"}, nil)
	cs, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{
		Endpoint:             ts.URL + "/api/v1/mcp",
		HTTPClient:           &http.Client{Transport: bearerTransport{secret}},
		DisableStandaloneSSE: true,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs
}

// callTool calls a tool, failing the test on a protocol error (a tool's own
// error is a result, with IsError set).
func callTool(t *testing.T, cs *mcp.ClientSession, name string, args any) *mcp.CallToolResult {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return res
}

// structured decodes a tool's structured result.
func structured(t *testing.T, res *mcp.CallToolResult, out any) {
	t.Helper()
	if res.IsError {
		t.Fatalf("tool error: %s", toolText(res))
	}
	raw, _ := json.Marshal(res.StructuredContent)
	if err := json.Unmarshal(raw, out); err != nil {
		t.Fatal(err)
	}
}

func toolText(res *mcp.CallToolResult) string {
	var parts []string
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			parts = append(parts, tc.Text)
		}
	}
	return strings.Join(parts, " ")
}

func toolNames(t *testing.T, cs *mcp.ClientSession) []string {
	t.Helper()
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tool := range res.Tools {
		names = append(names, tool.Name)
	}
	return names
}

func TestMCPFirstTools(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	if _, err := srv.store.CreateScope("tools"); err != nil {
		t.Fatal(err)
	}
	secret := tokenWith(t, srv, admin.ID, "Assistant", "assets.read", "tools:assets.write")
	cs := mcpSession(t, srv, secret)

	names := toolNames(t, cs)
	for _, want := range []string{"whoami", "server_info", "list_generators"} {
		if !slices.Contains(names, want) {
			t.Errorf("tools %v lack %s", names, want)
		}
	}

	var me struct {
		Kind        string
		Name        string
		Permissions []string
	}
	structured(t, callTool(t, cs, "whoami", nil), &me)
	if me.Kind != "token" || me.Name != "Assistant" || !slices.Contains(me.Permissions, "tools:assets.write") || !slices.Contains(me.Permissions, "assets.read") {
		t.Errorf("whoami: %+v", me)
	}

	var info struct {
		Version  string
		Database string
		Healthy  bool
		Ready    bool
	}
	structured(t, callTool(t, cs, "server_info", nil), &info)
	if info.Version != Version || info.Database != srv.store.Dialect || !info.Healthy || !info.Ready {
		t.Errorf("server_info: %+v", info)
	}

	// The same as the API answers the same token.
	var gens, viaAPI struct{ Generators []map[string]any }
	structured(t, callTool(t, cs, "list_generators", nil), &gens)
	_ = json.Unmarshal(withBearer(t, srv, secret, http.MethodGet, "/api/v1/generators", "").Body.Bytes(), &viaAPI)
	if len(gens.Generators) == 0 || !jsonEqual(gens, viaAPI) {
		t.Errorf("list_generators %v, the API %v", gens, viaAPI)
	}

	// The guide, and the help people read.
	resources, err := cs.ListResources(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var uris []string
	for _, r := range resources.Resources {
		uris = append(uris, r.URI)
	}
	for _, want := range []string{mcpserver.GuideURI, "bam://help/start", "bam://help/labels"} {
		if !slices.Contains(uris, want) {
			t.Errorf("resources %v lack %s", uris, want)
		}
	}
	read, err := cs.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: "bam://help/start"})
	if err != nil || len(read.Contents) != 1 || !strings.HasPrefix(read.Contents[0].Text, "# Getting started\n") {
		t.Errorf("help: %v %+v", err, read)
	}
	if init := cs.InitializeResult(); init == nil || !strings.Contains(init.Instructions, mcpserver.GuideURI) || !strings.Contains(init.Instructions, `"Assistant"`) {
		t.Errorf("instructions: %+v", init)
	}
}

func jsonEqual(a, b any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}

// TestMCPNeedsAToken refuses requests without one, with a session, and with
// one revoked; and answers 404 with MCP turned off.
func TestMCPNeedsAToken(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	initialize := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"x","version":"1"}}}`
	post := func(srv *Server, prepare func(*http.Request)) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/mcp", strings.NewReader(initialize))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		prepare(req)
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		return rec
	}

	if rec := post(srv, func(*http.Request) {}); rec.Code != http.StatusUnauthorized || !strings.HasPrefix(rec.Header().Get("WWW-Authenticate"), "Bearer") {
		t.Errorf("without a token: %d %v", rec.Code, rec.Header())
	}
	cookie := sessionCookie(t, srv, admin.ID)
	if rec := post(srv, func(r *http.Request) { r.AddCookie(cookie) }); rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), "API token") {
		t.Errorf("with a session: %d %s", rec.Code, rec.Body)
	}
	secret := tokenWith(t, srv, admin.ID, "Assistant", "assets.read")
	if rec := post(srv, func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+secret) }); rec.Code != http.StatusOK {
		t.Errorf("with a token: %d %s", rec.Code, rec.Body)
	}
	tokens, err := srv.store.ListTokens()
	if err != nil {
		t.Fatal(err)
	}
	for _, tok := range tokens {
		if _, err := srv.store.RevokeToken(tok.ID); err != nil {
			t.Fatal(err)
		}
	}
	if rec := post(srv, func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+secret) }); rec.Code != http.StatusUnauthorized {
		t.Errorf("with a revoked token: %d %s", rec.Code, rec.Body)
	}

	off, offAdmin, _ := setupTestServer(t)
	off.cfg.MCP.Enabled = false
	off = NewServer(off.cfg, off.store)
	offSecret := tokenWith(t, off, offAdmin.ID, "Assistant", "assets.read")
	for _, method := range []string{http.MethodPost, http.MethodGet, http.MethodDelete} {
		req := httptest.NewRequest(method, "/api/v1/mcp", strings.NewReader(initialize))
		req.Header.Set("Authorization", "Bearer "+offSecret)
		rec := httptest.NewRecorder()
		off.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "turned off") {
			t.Errorf("%s with MCP off: %d %s", method, rec.Code, rec.Body)
		}
	}
	var info struct{ MCP bool }
	_ = json.Unmarshal(withBearer(t, off, offSecret, http.MethodGet, "/api/v1/info", "").Body.Bytes(), &info)
	if info.MCP {
		t.Error("/info says MCP is on")
	}
	_ = json.Unmarshal(withBearer(t, srv, tokenWith(t, srv, admin.ID, "Other", "assets.read"), http.MethodGet, "/api/v1/info", "").Body.Bytes(), &info)
	if !info.MCP {
		t.Error("/info says MCP is off")
	}
}

// TestMCPCoversEveryRoute fails for an API route no tool reaches and no list
// names, so that MCP keeps doing everything the API does (#33).
func TestMCPCoversEveryRoute(t *testing.T) {
	srv, _, _ := setupTestServer(t)
	covered := mcpserver.Routes()
	registered := map[string]bool{}
	for _, route := range srv.routes {
		registered[route] = true
		_, byTool := covered[route]
		_, later := mcpserver.Uncovered[route]
		_, refused := mcpserver.Refused[route]
		switch {
		case !byTool && !later && !refused:
			t.Errorf("%s: no MCP tool reaches it; add one, or name it in mcpserver.Uncovered or Refused", route)
		case byTool && (later || refused):
			t.Errorf("%s: reached by %s, yet listed as uncovered or refused", route, covered[route])
		}
	}
	for _, list := range []map[string]string{covered, mcpserver.Uncovered, mcpserver.Refused} {
		for route := range list {
			if !registered[route] {
				t.Errorf("%s is listed for MCP but is not an API route", route)
			}
		}
	}
	if len(srv.routes) < 150 {
		t.Errorf("only %d routes recorded", len(srv.routes))
	}
}
