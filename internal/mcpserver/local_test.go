package mcpserver

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// TestALocalConnectionOffersTheCallersTools serves the tools in process to
// the assistant beside the page: only those the caller may use, each
// answering through the API as them, through the assistant by its name.
func TestALocalConnectionOffersTheCallersTools(t *testing.T) {
	var via []string
	api := func(ctx context.Context, method, path string, _ []byte, _ string) Response {
		via = append(via, ViaFrom(ctx))
		return Response{Status: 200, Body: []byte(`{"assets":[],"total":0}`)}
	}
	c := Caller{Kind: "person", ID: "u", Name: "Ada", Permissions: map[string]bool{"assets.read": true}}
	l, err := Connect(context.Background(), c, api, "Shop helper", "test")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	tools, err := l.Tools(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]LocalTool{}
	for _, tl := range tools {
		byName[tl.Name] = tl
	}
	if _, ok := byName["search_assets"]; !ok {
		t.Error("search_assets is not offered to a reader")
	}
	for _, name := range []string{"create_asset", "list_users", "save_role"} {
		if _, ok := byName[name]; ok {
			t.Errorf("%s is offered to a reader", name)
		}
	}
	if s := byName["search_assets"]; !s.ReadOnly || s.Destructive || s.Title == "" || !json.Valid(s.Schema) {
		t.Errorf("search_assets %+v", s)
	}
	text, failed, err := l.Call(context.Background(), "search_assets", json.RawMessage(`{"search":"saw"}`))
	if err != nil || failed || text == "" {
		t.Errorf("calling: %q %v %v", text, failed, err)
	}
	if len(via) == 0 || via[0] != "Shop helper" {
		t.Errorf("via %v", via)
	}
	if _, failed, err := l.Call(context.Background(), "create_asset", json.RawMessage(`{"assets":[{"name":"x"}]}`)); err == nil && !failed {
		t.Error("a tool not offered was called")
	}
	if got := Instructions(c); !strings.Contains(got, "assistant panel") || !strings.Contains(got, "data") {
		t.Errorf("instructions %q", got)
	}
}
