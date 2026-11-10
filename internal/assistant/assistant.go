// Package assistant talks to the model behind the assistant beside the page
// (docs/assistant.md): Anthropic's Messages API, or any chat completions API
// that takes tools, as OpenAI's does. It speaks to each over the standard
// library's HTTP client, in one shape of its own, so the conversation is
// kept the same whichever an admin chooses.
package assistant

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Block is one part of a message.
type Block struct {
	// Type is BlockText, BlockToolCall or BlockToolResult.
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
	// ID, Name and Input are a tool call's; ID, Name, Content and IsError
	// its result's.
	ID   string `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
	// Title is a tool call's tool as people read its name; never sent.
	Title   string          `json:"title,omitempty"`
	Input   json.RawMessage `json:"input,omitempty"`
	Content string          `json:"content,omitempty"`
	IsError bool            `json:"is_error,omitempty"`
}

// The kinds of block.
const (
	BlockText       = "text"
	BlockToolCall   = "tool_call"
	BlockToolResult = "tool_result"
)

// The roles of message.
const (
	RoleUser      = "user"
	RoleAssistant = "assistant"
	// RoleTool is what tools answered, sent to the model as its provider
	// takes it.
	RoleTool = "tool"
)

// Message is one message of a conversation.
type Message struct {
	Role   string
	Blocks []Block
	// Raw is the model's reply as Provider gave it, sent back to the same
	// provider unchanged: some keep more than Blocks hold, such as the
	// model's thinking, which they need again.
	Raw      json.RawMessage
	Provider string
}

// Tool is a tool offered to the model.
type Tool struct {
	Name        string
	Description string
	// Schema is the JSON schema of its input.
	Schema json.RawMessage
}

// Request asks the model for its next message.
type Request struct {
	Model     string
	System    string
	Messages  []Message
	Tools     []Tool
	MaxTokens int
}

// Response is the model's next message.
type Response struct {
	Blocks []Block
	// Raw is the message as the provider gave it, to be sent back.
	Raw json.RawMessage
	// Stop is why it stopped: StopEnd, StopTools, StopLength or
	// StopRefused.
	Stop string
	// Tokens is every token the request and the reply counted.
	Tokens int64
}

// Why the model stopped.
const (
	StopEnd     = "end"
	StopTools   = "tools"
	StopLength  = "length"
	StopRefused = "refused"
)

// ToolCalls are the tool calls in a response.
func (r *Response) ToolCalls() []Block {
	var out []Block
	for _, b := range r.Blocks {
		if b.Type == BlockToolCall {
			out = append(out, b)
		}
	}
	return out
}

// Provider is a model's API.
type Provider interface {
	// Name is the provider's kind, as Message.Provider names it.
	Name() string
	Complete(ctx context.Context, req Request) (*Response, error)
}

// Config chooses a provider.
type Config struct {
	// Kind is "anthropic" or "openai".
	Kind     string
	Endpoint string
	APIKey   string
	// Client sends the requests; one with a timeout of Timeout unless set.
	Client *http.Client
}

// Timeout is how long one request to a provider may take.
const Timeout = 5 * time.Minute

// ErrProvider is wrapped by the provider refusing or failing a request.
var ErrProvider = errors.New("the assistant's provider")

// New is the provider a configuration chooses.
func New(c Config) (Provider, error) {
	if c.Client == nil {
		c.Client = &http.Client{Timeout: Timeout}
	}
	c.Endpoint = strings.TrimRight(c.Endpoint, "/")
	switch c.Kind {
	case "anthropic":
		return &anthropic{c}, nil
	case "openai":
		return &openAI{c}, nil
	}
	return nil, fmt.Errorf("no provider is %q", c.Kind)
}

// post sends body as JSON to url, decoding a success into out. A failure is
// the provider's own message, without the request, which holds the key.
func post(ctx context.Context, client *http.Client, url string, header http.Header, body, out any) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("%w: %v", ErrProvider, err)
	}
	req.Header = header
	req.Header.Set("Content-Type", "application/json")
	res, err := client.Do(req)
	if err != nil {
		// Its URL holds no secret; the error names it.
		return fmt.Errorf("%w could not be reached: %v", ErrProvider, err)
	}
	defer res.Body.Close()
	answer, err := io.ReadAll(io.LimitReader(res.Body, 32<<20))
	if err != nil {
		return fmt.Errorf("%w did not answer: %v", ErrProvider, err)
	}
	if res.StatusCode < 200 || res.StatusCode > 299 {
		return fmt.Errorf("%w answered %d: %s", ErrProvider, res.StatusCode, errorText(answer))
	}
	if err := json.Unmarshal(answer, out); err != nil {
		return fmt.Errorf("%w answered what is not JSON: %v", ErrProvider, err)
	}
	return nil
}

// errorText is a provider's error message, from either kind's error shape,
// or the start of its answer.
func errorText(body []byte) string {
	var e struct {
		Error json.RawMessage `json:"error"`
	}
	if json.Unmarshal(body, &e) == nil && len(e.Error) > 0 {
		var obj struct {
			Message string `json:"message"`
		}
		if json.Unmarshal(e.Error, &obj) == nil && obj.Message != "" {
			return obj.Message
		}
		var s string
		if json.Unmarshal(e.Error, &s) == nil && s != "" {
			return s
		}
	}
	text := strings.TrimSpace(string(body))
	if r := []rune(text); len(r) > 300 {
		text = string(r[:300]) + "…"
	}
	return text
}

// merged joins consecutive messages of the same role, as both APIs want
// one message where several follow each other.
func merged(in []Message, role func(Message) string) [][]Message {
	var out [][]Message
	for _, m := range in {
		if n := len(out); n > 0 && role(out[n-1][0]) == role(m) {
			out[n-1] = append(out[n-1], m)
			continue
		}
		out = append(out, []Message{m})
	}
	return out
}

func inputOf(b Block) json.RawMessage {
	if len(b.Input) == 0 || !json.Valid(b.Input) {
		return json.RawMessage(`{}`)
	}
	return b.Input
}
