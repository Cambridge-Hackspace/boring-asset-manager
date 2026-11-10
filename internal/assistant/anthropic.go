package assistant

import (
	"context"
	"encoding/json"
	"net/http"
)

// anthropic is Anthropic's Messages API.
type anthropic struct{ c Config }

// anthropicVersion is the API version requests ask for.
const anthropicVersion = "2026-10-21"

func (a *anthropic) Name() string { return "anthropic" }

type anthropicBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   string          `json:"content,omitempty"`
	IsError   bool            `json:"is_error,omitempty"`
}

type anthropicMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type anthropicTool struct {
	Name         string          `json:"name"`
	Description  string          `json:"description,omitempty"`
	InputSchema  json.RawMessage `json:"input_schema"`
	CacheControl *cacheControl   `json:"cache_control,omitempty"`
}

type cacheControl struct {
	Type string `json:"type"`
}

type systemBlock struct {
	Type         string        `json:"type"`
	Text         string        `json:"text"`
	CacheControl *cacheControl `json:"cache_control,omitempty"`
}

func (a *anthropic) Complete(ctx context.Context, req Request) (*Response, error) {
	var msgs []anthropicMessage
	// Tools' answers go back as the person's message.
	for _, group := range merged(req.Messages, func(m Message) string {
		if m.Role == RoleAssistant {
			return RoleAssistant
		}
		return RoleUser
	}) {
		var content []json.RawMessage
		role := RoleUser
		for _, m := range group {
			if m.Role == RoleAssistant {
				role = RoleAssistant
			}
			if m.Role == RoleAssistant && m.Provider == a.Name() && len(m.Raw) > 0 {
				// The reply as it came, its thinking included.
				var raw []json.RawMessage
				if json.Unmarshal(m.Raw, &raw) == nil {
					content = append(content, raw...)
					continue
				}
			}
			for _, b := range m.Blocks {
				var ab anthropicBlock
				switch b.Type {
				case BlockText:
					if b.Text == "" {
						continue
					}
					ab = anthropicBlock{Type: "text", Text: b.Text}
				case BlockToolCall:
					ab = anthropicBlock{Type: "tool_use", ID: b.ID, Name: b.Name, Input: inputOf(b)}
				case BlockToolResult:
					ab = anthropicBlock{Type: "tool_result", ToolUseID: b.ID, Content: b.Content, IsError: b.IsError}
				default:
					continue
				}
				raw, _ := json.Marshal(ab)
				content = append(content, raw)
			}
		}
		if len(content) == 0 {
			continue
		}
		raw, _ := json.Marshal(content)
		msgs = append(msgs, anthropicMessage{Role: role, Content: raw})
	}
	tools := make([]anthropicTool, 0, len(req.Tools))
	for _, t := range req.Tools {
		tools = append(tools, anthropicTool{Name: t.Name, Description: t.Description, InputSchema: t.Schema})
	}
	// The tools and the instructions are the same from one call to the
	// next, and the most of what is sent: cached, they cost a tenth.
	if n := len(tools); n > 0 {
		tools[n-1].CacheControl = &cacheControl{Type: "ephemeral"}
	}
	body := map[string]any{
		"model":      req.Model,
		"max_tokens": req.MaxTokens,
		"system":     []systemBlock{{Type: "text", Text: req.System, CacheControl: &cacheControl{Type: "ephemeral"}}},
		"messages":   msgs,
	}
	if len(tools) > 0 {
		body["tools"] = tools
	}
	h := http.Header{}
	h.Set("x-api-key", a.c.APIKey)
	h.Set("anthropic-version", anthropicVersion)
	var res struct {
		Content    []json.RawMessage `json:"content"`
		StopReason string            `json:"stop_reason"`
		Usage      struct {
			Input         int64 `json:"input_tokens"`
			Output        int64 `json:"output_tokens"`
			CacheCreation int64 `json:"cache_creation_input_tokens"`
			CacheRead     int64 `json:"cache_read_input_tokens"`
		} `json:"usage"`
	}
	if err := post(ctx, a.c.Client, a.c.Endpoint+"/v1/messages", h, body, &res); err != nil {
		return nil, err
	}
	out := &Response{Tokens: res.Usage.Input + res.Usage.Output + res.Usage.CacheCreation + res.Usage.CacheRead}
	out.Raw, _ = json.Marshal(res.Content)
	for _, raw := range res.Content {
		var b anthropicBlock
		if json.Unmarshal(raw, &b) != nil {
			continue
		}
		switch b.Type {
		case "text":
			out.Blocks = append(out.Blocks, Block{Type: BlockText, Text: b.Text})
		case "tool_use":
			out.Blocks = append(out.Blocks, Block{Type: BlockToolCall, ID: b.ID, Name: b.Name, Input: inputOf(Block{Input: b.Input})})
		}
	}
	switch res.StopReason {
	case "tool_use":
		out.Stop = StopTools
	case "max_tokens":
		out.Stop = StopLength
	case "refusal":
		out.Stop = StopRefused
	default:
		out.Stop = StopEnd
	}
	return out, nil
}
