package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/mcpserver"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
)

// TestHistoryRecordsTheAssistant records the assistant a change came
// through, as MCP tools make them, and nothing for other changes.
func TestHistoryRecordsTheAssistant(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	adminCookie := sessionCookie(t, srv, admin.ID)
	secret := tokenWith(t, srv, admin.ID, "Front desk", rbac.PermAssetsRead, rbac.PermAssetsWrite)

	create := func(name string, ctx context.Context) string {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/assets", strings.NewReader(`{"name":"`+name+`"}`)).WithContext(ctx)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+secret)
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("creating %s: %d %s", name, rec.Code, rec.Body)
		}
		id := between(rec.Body.String(), `"id":"`, `"`)
		return doJSON(t, srv, adminCookie, http.MethodGet, "/api/v1/assets/"+id+"/history", "").Body.String()
	}
	if h := create("Drill", mcpserver.WithVia(context.Background(), "Claude Desktop")); !strings.Contains(h, `"name":"Front desk","token":true,"via":"Claude Desktop"`) {
		t.Errorf("through an assistant: %s", h)
	}
	if h := create("Saw", context.Background()); strings.Contains(h, `"via"`) {
		t.Errorf("without one: %s", h)
	}
	// A header claiming an assistant is not one.
	req := httptest.NewRequest(http.MethodPost, "/api/v1/assets", strings.NewReader(`{"name":"Plane"}`))
	req.Header.Set("Authorization", "Bearer "+secret)
	req.Header.Set("Via", "Claude Desktop")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	id := between(rec.Body.String(), `"id":"`, `"`)
	if h := doJSON(t, srv, adminCookie, http.MethodGet, "/api/v1/assets/"+id+"/history", "").Body.String(); strings.Contains(h, `"via"`) {
		t.Errorf("a Via header was recorded: %s", h)
	}
}

func between(s, start, end string) string {
	_, after, _ := strings.Cut(s, start)
	v, _, _ := strings.Cut(after, end)
	return v
}
