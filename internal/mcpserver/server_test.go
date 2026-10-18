package mcpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// connect serves the tools to an in-memory client, as the caller, with a
// backend that answers every request with the status and body given.
func connect(t *testing.T, c Caller, tools []tool, status int, body string) (*mcp.ClientSession, *env) {
	t.Helper()
	e := &env{caller: c, changes: newChangeLimiter(), version: "test",
		api: func(context.Context, string, string, []byte, string) Response {
			return Response{Status: status, Body: []byte(body)}
		}}
	s := newServer(e, mcp.NewSchemaCache(), tools)
	st, ct := mcp.NewInMemoryTransports()
	if _, err := s.Connect(context.Background(), st, nil); err != nil {
		t.Fatal(err)
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).Connect(context.Background(), ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs, e
}

type countIn struct {
	Items []string `json:"items"`
}

type countOut struct {
	Count int `json:"count"`
}

// fakeTools are a tool anyone may use, one only for managing users, and one
// that changes things.
func fakeTools() []tool {
	return []tool{
		define(&mcp.Tool{Name: "look", Annotations: readOnly("Look")}, nil, nil,
			func(ctx context.Context, e *env, _ none) (reply[countOut], error) { return say(countOut{}, "looked") }),
		define(&mcp.Tool{Name: "manage", Annotations: readOnly("Manage")}, nil, []string{"users.manage"},
			func(ctx context.Context, e *env, _ none) (reply[countOut], error) { return say(countOut{}, "managed") }),
		define(&mcp.Tool{Name: "change", Annotations: changes("Change", false)}, nil, []string{"assets.write"},
			func(ctx context.Context, e *env, in countIn) (reply[countOut], error) {
				if err := checkItems(len(in.Items)); err != nil {
					return reply[countOut]{}, err
				}
				if err := e.call(ctx, http.MethodPost, "/api/v1/things", in, nil); err != nil {
					return reply[countOut]{}, err
				}
				return say(countOut{Count: len(in.Items)}, "changed %d", len(in.Items))
			}),
	}
}

func listed(t *testing.T, cs *mcp.ClientSession) []string {
	t.Helper()
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tl := range res.Tools {
		names = append(names, tl.Name)
	}
	slices.Sort(names)
	return names
}

func TestToolsListedByPermission(t *testing.T) {
	for _, tc := range []struct {
		perms []string
		want  []string
	}{
		{nil, []string{"look"}},
		{[]string{"users.manage"}, []string{"look", "manage"}},
		// A permission held in one scope opens a tool; the API decides per asset.
		{[]string{"tools:assets.write"}, []string{"change", "look"}},
	} {
		perms := map[string]bool{}
		for _, p := range tc.perms {
			perms[p] = true
		}
		cs, _ := connect(t, Caller{Kind: "token", ID: "t", Name: "T", Permissions: perms}, fakeTools(), 200, "{}")
		if got := listed(t, cs); !slices.Equal(got, tc.want) {
			t.Errorf("%v: listed %v, want %v", tc.perms, got, tc.want)
		}
		// A tool not listed cannot be called either.
		if !slices.Contains(tc.want, "manage") {
			if _, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "manage"}); err == nil {
				t.Errorf("%v: called manage", tc.perms)
			}
		}
	}
}

func TestChangeLimits(t *testing.T) {
	cs, e := connect(t, Caller{Kind: "token", ID: "t", Name: "T", Permissions: map[string]bool{"assets.write": true}}, fakeTools(), 200, "{}")
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	e.changes.now = func() time.Time { return now }
	call := func(name string, items int) *mcp.CallToolResult {
		t.Helper()
		args := map[string]any{}
		if name == "change" {
			args["items"] = make([]string, items)
		}
		res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil {
			t.Fatal(err)
		}
		return res
	}

	// At most 50 items in a call, refused whole.
	if res := call("change", 51); !res.IsError || !strings.Contains(text(res), "at most 50") {
		t.Errorf("51 items: %+v", res)
	}
	// 60 changes a minute (the refusal above counted as a call)...
	for i := 1; i < changesPerMinute; i++ {
		if res := call("change", 1); res.IsError {
			t.Fatalf("change %d refused: %s", i, text(res))
		}
	}
	res := call("change", 1)
	if !res.IsError || !strings.Contains(text(res), "at most 60 a minute") || !strings.Contains(text(res), "60 seconds") {
		t.Errorf("61st change: %s", text(res))
	}
	// ...while looking is not limited...
	if res := call("look", 0); res.IsError {
		t.Errorf("looking was limited: %s", text(res))
	}
	// ...and a minute on, there is room again.
	now = now.Add(time.Minute)
	if res := call("change", 1); res.IsError {
		t.Errorf("a minute on: %s", text(res))
	}
	// Each caller has its own share.
	if wait := e.changes.take("token:other"); wait != 0 {
		t.Errorf("another caller waits %v", wait)
	}
}

func TestAPIRefusalsReachTheModel(t *testing.T) {
	cs, _ := connect(t, Caller{Kind: "token", ID: "t", Name: "T", Permissions: map[string]bool{"assets.write": true}}, fakeTools(), 403,
		`{"error":"Forbidden: insufficient permissions","details":{"scope":"tools"}}`)
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "change", Arguments: map[string]any{"items": []string{"a"}}})
	if err != nil {
		t.Fatal(err)
	}
	want := `Forbidden: insufficient permissions (HTTP 403 Forbidden). Details: {"scope":"tools"}`
	if !res.IsError || text(res) != want {
		t.Errorf("got %q, want %q", text(res), want)
	}
}

func text(res *mcp.CallToolResult) string {
	var parts []string
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			parts = append(parts, tc.Text)
		}
	}
	return strings.Join(parts, " ")
}

func TestEveryToolIsDescribed(t *testing.T) {
	seen := map[string]bool{}
	for _, tl := range allTools() {
		d := tl.def
		if seen[d.Name] {
			t.Errorf("%s defined twice", d.Name)
		}
		seen[d.Name] = true
		if d.Description == "" || d.Annotations.Title == "" {
			t.Errorf("%s needs a description and a title", d.Name)
		}
		if !d.Annotations.ReadOnlyHint && d.Annotations.DestructiveHint == nil {
			t.Errorf("%s changes things but does not say whether destructively", d.Name)
		}
	}
}

// FuzzToolArguments calls every tool with arbitrary arguments, as anyone
// holding every permission, against an API that refuses everything: tools
// may answer errors, never fail the protocol or crash.
func FuzzToolArguments(f *testing.F) {
	for _, seed := range []string{`{}`, `{"id":"x"}`, `{"items":[1,2]}`, `{"limit":-1}`, `null`, `[]`, `{"a":{"b":"\u0000"}}`} {
		for _, tl := range allTools() {
			f.Add(tl.def.Name, seed)
		}
	}
	perms := map[string]bool{}
	for _, p := range []string{"assets.read", "assets.write", "assets.delete", "templates.manage", "categories.manage", "settings.manage",
		"users.manage", "labels.print", "stock.record", "assets.report", "assets.borrow", "assets.lend", "reports.manage", "assets.track",
		"assets.scan", "assets.act", "events.read"} {
		perms[p] = true
	}
	f.Fuzz(func(t *testing.T, name, args string) {
		var arguments any
		if json.Unmarshal([]byte(args), &arguments) != nil {
			return
		}
		cs, _ := connect(t, Caller{Kind: "person", ID: "t", Name: "T", Token: "Claude", Permissions: perms}, allTools(), 404, `{"error":"Not found"}`)
		_, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: arguments})
		if err != nil && slices.Contains(ToolNames(), name) && !strings.Contains(err.Error(), "invalid") && !strings.Contains(err.Error(), "validat") {
			t.Errorf("%s(%s): %v", name, args, err)
		}
	})
}
