package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMCPPrompts(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cs := mcpSession(t, srv, tokenWith(t, srv, admin.ID, "Helper", rbac.PermAssetsRead))
	list, err := cs.ListPrompts(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, p := range list.Prompts {
		names = append(names, p.Name)
	}
	slices.Sort(names)
	if strings.Join(names, ",") != "add_things,design_label,find,needs_attention,set_up,stocktake" {
		t.Errorf("prompts: %v", names)
	}
	got, err := cs.GetPrompt(context.Background(), &mcp.GetPromptParams{Name: "find", Arguments: map[string]string{"what": "the M3 screws"}})
	if err != nil || !strings.Contains(got.Messages[0].Content.(*mcp.TextContent).Text, "the M3 screws") {
		t.Errorf("find: %v %+v", err, got)
	}
	if _, err := cs.GetPrompt(context.Background(), &mcp.GetPromptParams{Name: "find"}); err == nil {
		t.Error("find without what")
	}
}

// connectAsking connects a client that answers confirmations as answer says.
func connectAsking(t *testing.T, srv *Server, secret string, answer string, asked *[]string) *mcp.ClientSession {
	t.Helper()
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	client := mcp.NewClient(&mcp.Implementation{Name: "asking", Version: "1"}, &mcp.ClientOptions{
		ElicitationHandler: func(_ context.Context, req *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
			*asked = append(*asked, req.Params.Message)
			return &mcp.ElicitResult{Action: answer}, nil
		},
	})
	cs, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: ts.URL + "/api/v1/mcp",
		HTTPClient: &http.Client{Transport: bearerTransport{secret}}, DisableStandaloneSSE: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs
}

// TestDestructiveToolsAskFirst asks the person to confirm a deletion where
// the client can ask, and does nothing on a no.
func TestDestructiveToolsAskFirst(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	secret := tokenWith(t, srv, admin.ID, "Curator", rbac.PermAssetsRead, rbac.PermCategoriesManage)
	field := func(key string) string {
		return mustID(t, doJSON(t, srv, cookie, http.MethodPost, "/api/v1/fields", `{"key":"`+key+`","label":"X","validation_type":"text"}`).Body.Bytes(), "field")
	}
	exists := func(id string) bool {
		return doJSON(t, srv, cookie, http.MethodGet, "/api/v1/fields/"+id, "").Code == http.StatusOK
	}

	var asked []string
	kept := field("kept")
	res := callTool(t, connectAsking(t, srv, secret, "decline", &asked), "delete_field", map[string]any{"id": kept})
	if !res.IsError || !strings.Contains(toolText(res), "did not confirm") || !exists(kept) || len(asked) != 1 || !strings.Contains(asked[0], "Delete a field") {
		t.Errorf("declined: %s, asked %v", toolText(res), asked)
	}
	gone := field("gone")
	if res := callTool(t, connectAsking(t, srv, secret, "accept", &asked), "delete_field", map[string]any{"id": gone}); res.IsError || exists(gone) {
		t.Errorf("accepted: %s", toolText(res))
	}
	// Changes that lose nothing are not asked about.
	asked = nil
	if res := callTool(t, connectAsking(t, srv, secret, "decline", &asked), "save_field", map[string]any{"id": kept, "label": "Kept"}); res.IsError || len(asked) != 0 {
		t.Errorf("a plain change: %s, asked %v", toolText(res), asked)
	}
	// A client that cannot ask relies on its own approval.
	other := field("other")
	if res := callTool(t, mcpSession(t, srv, secret), "delete_field", map[string]any{"id": other}); res.IsError || exists(other) {
		t.Errorf("without asking: %s", toolText(res))
	}
}
