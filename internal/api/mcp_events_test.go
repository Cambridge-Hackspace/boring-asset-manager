package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestEventsToPoll lists the events a live connection would be sent, and
// takes answers to hooks from those who poll.
func TestEventsToPoll(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	scope := mustID(t, doJSON(t, srv, cookie, http.MethodPost, "/api/v1/scopes", `{"name":"lab"}`).Body.Bytes(), "scope")
	cat := mustID(t, doJSON(t, srv, cookie, http.MethodPost, "/api/v1/categories", `{"name":"Lab"}`).Body.Bytes(), "category")
	doJSON(t, srv, cookie, http.MethodPut, "/api/v1/categories/"+cat+"/scopes", `{"scope_ids":["`+scope+`"]}`)
	hook := mustID(t, doJSON(t, srv, cookie, http.MethodPost, "/api/v1/hooks", `{"key":"identify","label":"Identify"}`).Body.Bytes(), "hook")
	doJSON(t, srv, cookie, http.MethodPut, "/api/v1/categories/"+cat+"/hooks", `{"hooks":[{"hook_id":"`+hook+`","cascades":true}]}`)
	scope2 := mustID(t, doJSON(t, srv, cookie, http.MethodPost, "/api/v1/assets", `{"name":"Scope","category_ids":["`+cat+`"]}`).Body.Bytes(), "asset")
	doJSON(t, srv, cookie, http.MethodPost, "/api/v1/assets", `{"name":"Elsewhere"}`)

	lab := tokenWith(t, srv, admin.ID, "Lab cabinet", "lab:events.read")
	var list struct {
		Events []struct {
			ID   string
			Kind string
		}
		More bool
	}
	_ = json.Unmarshal(withBearer(t, srv, lab, http.MethodGet, "/api/v1/events", "").Body.Bytes(), &list)
	if len(list.Events) != 1 || list.Events[0].Kind != "asset.created" {
		t.Fatalf("events in scope: %+v", list)
	}
	first := list.Events[0].ID

	pressed := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/assets/"+scope2+"/hooks/identify", "")
	var press struct {
		EventID string `json:"event_id"`
	}
	_ = json.Unmarshal(pressed.Body.Bytes(), &press)
	_ = json.Unmarshal(withBearer(t, srv, lab, http.MethodGet, "/api/v1/events?after="+first+"&kinds=asset.hook", "").Body.Bytes(), &list)
	if len(list.Events) != 1 || list.Events[0].ID != press.EventID {
		t.Fatalf("after: %+v", list)
	}
	if rec := withBearer(t, srv, lab, http.MethodGet, "/api/v1/events?after=nope", ""); rec.Code != http.StatusBadRequest {
		t.Errorf("after an unknown event: %d", rec.Code)
	}

	// Answered once; someone who cannot see it is told it is not there.
	if rec := withBearer(t, srv, lab, http.MethodPost, "/api/v1/events/"+press.EventID+"/reply", `{"ok":true,"message":"Lit drawer A3"}`); rec.Code != http.StatusCreated {
		t.Fatalf("reply: %d %s", rec.Code, rec.Body)
	}
	if rec := withBearer(t, srv, lab, http.MethodPost, "/api/v1/events/"+press.EventID+"/reply", `{"ok":true}`); rec.Code != http.StatusConflict {
		t.Errorf("a second reply: %d", rec.Code)
	}
	other := tokenWith(t, srv, admin.ID, "Other cabinet", "default:events.read")
	if rec := withBearer(t, srv, other, http.MethodPost, "/api/v1/events/"+press.EventID+"/reply", `{"ok":true}`); rec.Code != http.StatusNotFound {
		t.Errorf("out of scope: %d", rec.Code)
	}
	rec := doJSON(t, srv, cookie, http.MethodGet, "/api/v1/assets/"+scope2+"/hooks/replies?event_id="+press.EventID, "")
	if !strings.Contains(rec.Body.String(), "Lit drawer A3") {
		t.Errorf("replies: %s", rec.Body)
	}
}

func TestMCPSubscriptions(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	drill := mustID(t, doJSON(t, srv, cookie, http.MethodPost, "/api/v1/assets", `{"name":"Drill"}`).Body.Bytes(), "asset")
	secret := tokenWith(t, srv, admin.ID, "Watcher", rbac.PermAssetsRead, rbac.PermEventsRead)

	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	updated := make(chan string, 10)
	client := mcp.NewClient(&mcp.Implementation{Name: "watcher", Version: "1"}, &mcp.ClientOptions{
		ResourceUpdatedHandler: func(_ context.Context, req *mcp.ResourceUpdatedNotificationRequest) { updated <- req.Params.URI },
	})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cs, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: ts.URL + "/api/v1/mcp", HTTPClient: &http.Client{Transport: bearerTransport{secret}}, DisableStandaloneSSE: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	for _, uri := range []string{"bam://asset/" + drill, "bam://events?kinds=asset.edited"} {
		if err := cs.Subscribe(ctx, &mcp.SubscribeParams{URI: uri}); err != nil {
			t.Fatalf("subscribing to %s: %v", uri, err)
		}
	}
	// Nothing yet: what happened before is not news.
	select {
	case uri := <-updated:
		t.Fatalf("told of %s before anything changed", uri)
	case <-time.After(1500 * time.Millisecond):
	}

	doJSON(t, srv, cookie, http.MethodPut, "/api/v1/assets/"+drill, `{"name":"Big drill"}`)
	got := map[string]bool{}
	for len(got) < 2 {
		select {
		case uri := <-updated:
			got[uri] = true
		case <-ctx.Done():
			t.Fatalf("told of %v only", got)
		}
	}
	read, err := cs.ReadResource(ctx, &mcp.ReadResourceParams{URI: "bam://asset/" + drill})
	if err != nil || !strings.Contains(read.Contents[0].Text, "Big drill") {
		t.Errorf("reading the asset: %v", err)
	}
	read, err = cs.ReadResource(ctx, &mcp.ReadResourceParams{URI: "bam://events?kinds=asset.edited"})
	if err != nil || !strings.Contains(read.Contents[0].Text, `"asset.edited"`) {
		t.Errorf("reading events: %v", err)
	}
	if res := callTool(t, cs, "list_events", map[string]any{"kinds": "asset.edited"}); !strings.Contains(toolText(res), "1 events") {
		t.Errorf("list_events: %s", toolText(res))
	}
	// Nothing else is there to read, or to watch.
	if _, err := cs.ReadResource(ctx, &mcp.ReadResourceParams{URI: "bam://somewhere"}); err == nil {
		t.Error("read something else")
	}
}
