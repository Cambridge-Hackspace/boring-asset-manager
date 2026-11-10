package assistant

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fake answers each request with answer, keeping what it was sent.
func fake(t *testing.T, status int, answer string) (*httptest.Server, *[]map[string]any, *[]http.Header) {
	t.Helper()
	var bodies []map[string]any
	var headers []http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Errorf("not JSON: %s", raw)
		}
		bodies = append(bodies, body)
		headers = append(headers, r.Header.Clone())
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, answer)
	}))
	t.Cleanup(srv.Close)
	return srv, &bodies, &headers
}

var conversation = []Message{
	{Role: RoleUser, Blocks: []Block{{Type: BlockText, Text: "Add a band saw"}}},
	{Role: RoleAssistant, Blocks: []Block{{Type: BlockText, Text: "Adding it."}, {Type: BlockToolCall, ID: "call_1", Name: "create_asset", Input: json.RawMessage(`{"assets":[{"name":"Band saw"}]}`)}}},
	{Role: RoleTool, Blocks: []Block{{Type: BlockToolResult, ID: "call_1", Name: "create_asset", Content: "Added Band saw"}}},
	{Role: RoleUser, Blocks: []Block{{Type: BlockText, Text: "Thanks"}}},
}

// TestAnthropicSpeaksTheMessagesAPI sends tools and their answers as the
// Messages API takes them, with the key in its header, and reads its reply
// and its tokens.
func TestAnthropicSpeaksTheMessagesAPI(t *testing.T) {
	srv, bodies, headers := fake(t, 200, `{"content":[{"type":"thinking","thinking":"","signature":"sig"},{"type":"text","text":"Which shelf?"},{"type":"tool_use","id":"toolu_2","name":"search_assets","input":{"q":"shelf"}}],
		"stop_reason":"tool_use","usage":{"input_tokens":10,"output_tokens":5,"cache_read_input_tokens":100,"cache_creation_input_tokens":1}}`)
	p, err := New(Config{Kind: "anthropic", Endpoint: srv.URL + "/", APIKey: "sk-secret"})
	if err != nil {
		t.Fatal(err)
	}
	res, err := p.Complete(context.Background(), Request{Model: "m", System: "be brief", Messages: conversation, MaxTokens: 100,
		Tools: []Tool{{Name: "search_assets", Description: "Find", Schema: json.RawMessage(`{"type":"object"}`)}}})
	if err != nil {
		t.Fatal(err)
	}
	if h := (*headers)[0]; h.Get("x-api-key") != "sk-secret" || h.Get("anthropic-version") == "" {
		t.Errorf("headers %v", h)
	}
	body := (*bodies)[0]
	msgs := body["messages"].([]any)
	if len(msgs) != 3 {
		t.Fatalf("messages %v: want the tool's answer and the person's text joined", msgs)
	}
	last := msgs[2].(map[string]any)["content"].([]any)
	if first := last[0].(map[string]any); last[0].(map[string]any)["type"] != "tool_result" || first["tool_use_id"] != "call_1" || last[1].(map[string]any)["type"] != "text" {
		t.Errorf("last message %v", last)
	}
	if tools := body["tools"].([]any); tools[0].(map[string]any)["input_schema"] == nil {
		t.Errorf("tools %v", tools)
	}
	if res.Stop != StopTools || res.Tokens != 116 || len(res.ToolCalls()) != 1 || res.ToolCalls()[0].Name != "search_assets" || res.Blocks[0].Text != "Which shelf?" {
		t.Errorf("response %+v", res)
	}
	// Sent back, the reply is as it came, its thinking included.
	_, err = p.Complete(context.Background(), Request{Model: "m", Messages: append(conversation, Message{Role: RoleAssistant, Raw: res.Raw, Provider: "anthropic", Blocks: res.Blocks})})
	if err != nil {
		t.Fatal(err)
	}
	again := (*bodies)[1]["messages"].([]any)
	if c := again[len(again)-1].(map[string]any)["content"].([]any); c[0].(map[string]any)["signature"] != "sig" {
		t.Errorf("sent back %v", c)
	}
}

// TestOpenAISpeaksChatCompletions sends tools and their answers as chat
// completions take them, and reads its reply.
func TestOpenAISpeaksChatCompletions(t *testing.T) {
	srv, bodies, headers := fake(t, 200, `{"choices":[{"message":{"role":"assistant","content":null,"tool_calls":[{"id":"c2","type":"function","function":{"name":"get_asset","arguments":"{\"asset\":\"A1\"}"}}]},"finish_reason":"tool_calls"}],"usage":{"total_tokens":42}}`)
	p, _ := New(Config{Kind: "openai", Endpoint: srv.URL, APIKey: "k"})
	res, err := p.Complete(context.Background(), Request{Model: "m", System: "s", Messages: conversation, MaxTokens: 10,
		Tools: []Tool{{Name: "get_asset", Schema: json.RawMessage(`{"type":"object"}`)}}})
	if err != nil {
		t.Fatal(err)
	}
	if (*headers)[0].Get("Authorization") != "Bearer k" {
		t.Error("no key")
	}
	var roles []string
	for _, m := range (*bodies)[0]["messages"].([]any) {
		roles = append(roles, m.(map[string]any)["role"].(string))
	}
	if strings.Join(roles, ",") != "system,user,assistant,tool,user" {
		t.Errorf("roles %v", roles)
	}
	if res.Stop != StopTools || res.Tokens != 42 || string(res.ToolCalls()[0].Input) != `{"asset":"A1"}` {
		t.Errorf("response %+v", res)
	}
}

// TestAProvidersRefusalIsItsOwnWords reads a provider's error message, never
// echoing the request, which holds the key.
func TestAProvidersRefusalIsItsOwnWords(t *testing.T) {
	srv, _, _ := fake(t, 401, `{"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key"}}`)
	p, _ := New(Config{Kind: "anthropic", Endpoint: srv.URL, APIKey: "sk-secret"})
	_, err := p.Complete(context.Background(), Request{Model: "m", Messages: conversation[:1]})
	if err == nil || !strings.Contains(err.Error(), "invalid x-api-key") || strings.Contains(err.Error(), "sk-secret") {
		t.Errorf("error %v", err)
	}
	if _, err := New(Config{Kind: "other"}); err == nil {
		t.Error("an unknown provider was taken")
	}
}
