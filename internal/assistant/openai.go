package assistant

import (
	"context"
	"encoding/json"
	"net/http"
)

// openAI is a chat completions API that takes tools, as OpenAI's does, and
// many others that copy it.
type openAI struct{ c Config }

func (o *openAI) Name() string { return "openai" }

type openAIToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type openAIMessage struct {
	Role       string           `json:"role"`
	Content    *string          `json:"content"`
	ToolCalls  []openAIToolCall `json:"tool_calls,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
}

func (o *openAI) Complete(ctx context.Context, req Request) (*Response, error) {
	str := func(s string) *string { return &s }
	msgs := []openAIMessage{{Role: "system", Content: str(req.System)}}
	for _, m := range req.Messages {
		switch m.Role {
		case RoleAssistant:
			am := openAIMessage{Role: "assistant"}
			text := ""
			for _, b := range m.Blocks {
				switch b.Type {
				case BlockText:
					text += b.Text
				case BlockToolCall:
					tc := openAIToolCall{ID: b.ID, Type: "function"}
					tc.Function.Name, tc.Function.Arguments = b.Name, string(inputOf(b))
					am.ToolCalls = append(am.ToolCalls, tc)
				}
			}
			if text != "" || len(am.ToolCalls) == 0 {
				am.Content = str(text)
			}
			msgs = append(msgs, am)
		default:
			// Each tool's answer is a message of its own; the person's text
			// after them is one more.
			text := ""
			for _, b := range m.Blocks {
				switch b.Type {
				case BlockToolResult:
					content := b.Content
					if b.IsError {
						content = "Error: " + content
					}
					msgs = append(msgs, openAIMessage{Role: "tool", ToolCallID: b.ID, Content: str(content)})
				case BlockText:
					if text != "" {
						text += "\n\n"
					}
					text += b.Text
				}
			}
			if text != "" {
				msgs = append(msgs, openAIMessage{Role: "user", Content: str(text)})
			}
		}
	}
	type function struct {
		Name        string          `json:"name"`
		Description string          `json:"description,omitempty"`
		Parameters  json.RawMessage `json:"parameters"`
	}
	type tool struct {
		Type     string   `json:"type"`
		Function function `json:"function"`
	}
	body := map[string]any{"model": req.Model, "messages": msgs, "max_tokens": req.MaxTokens}
	if len(req.Tools) > 0 {
		tools := make([]tool, 0, len(req.Tools))
		for _, t := range req.Tools {
			tools = append(tools, tool{Type: "function", Function: function{Name: t.Name, Description: t.Description, Parameters: t.Schema}})
		}
		body["tools"] = tools
	}
	h := http.Header{}
	if o.c.APIKey != "" {
		h.Set("Authorization", "Bearer "+o.c.APIKey)
	}
	var res struct {
		Choices []struct {
			Message      openAIMessage `json:"message"`
			FinishReason string        `json:"finish_reason"`
		} `json:"choices"`
		Usage struct {
			Total int64 `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := post(ctx, o.c.Client, o.c.Endpoint+"/chat/completions", h, body, &res); err != nil {
		return nil, err
	}
	out := &Response{Tokens: res.Usage.Total, Stop: StopEnd}
	if len(res.Choices) == 0 {
		return out, nil
	}
	c := res.Choices[0]
	out.Raw, _ = json.Marshal(c.Message)
	if c.Message.Content != nil && *c.Message.Content != "" {
		out.Blocks = append(out.Blocks, Block{Type: BlockText, Text: *c.Message.Content})
	}
	for _, tc := range c.Message.ToolCalls {
		out.Blocks = append(out.Blocks, Block{Type: BlockToolCall, ID: tc.ID, Name: tc.Function.Name, Input: inputOf(Block{Input: json.RawMessage(tc.Function.Arguments)})})
	}
	switch {
	case len(c.Message.ToolCalls) > 0:
		out.Stop = StopTools
	case c.FinishReason == "length":
		out.Stop = StopLength
	case c.FinishReason == "content_filter":
		out.Stop = StopRefused
	}
	return out, nil
}
