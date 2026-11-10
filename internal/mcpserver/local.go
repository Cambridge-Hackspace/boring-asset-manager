package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// The assistant beside the page (docs/assistant.md) uses the same tools,
// in process: a Local connection serves them to it as the person signed in,
// with their permissions as they are when it connects, so a role taken
// away is gone at the next connection.

var (
	localSchemas = mcp.NewSchemaCache()
	// localChanges holds the panel to the same change limit as any client.
	localChanges = newChangeLimiter()
)

// Local is a connection to the tools, in process, as a caller.
type Local struct {
	cs *mcp.ClientSession
	ss *mcp.ServerSession
}

// LocalTool is a tool a Local connection offers.
type LocalTool struct {
	Name        string
	Title       string
	Description string
	// Schema is its input's JSON schema.
	Schema json.RawMessage
	// ReadOnly tools change nothing; Destructive ones change what cannot
	// be put back.
	ReadOnly    bool
	Destructive bool
}

// Connect serves the tools the caller may use to a client named client,
// which history records changes as made through. It holds no elicitation:
// the panel asks the person itself.
func Connect(ctx context.Context, c Caller, api Backend, client, version string) (*Local, error) {
	e := &env{caller: c, api: api, changes: localChanges, subs: newSubscriptions(), cancel: func() {}, version: version}
	s := newServer(e, localSchemas, allTools())
	st, ct := mcp.NewInMemoryTransports()
	ss, err := s.Connect(ctx, st, nil)
	if err != nil {
		return nil, err
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: client, Title: client, Version: version}, nil).Connect(ctx, ct, nil)
	if err != nil {
		ss.Close()
		return nil, err
	}
	return &Local{cs: cs, ss: ss}, nil
}

// Close ends the connection.
func (l *Local) Close() {
	l.cs.Close()
	l.ss.Close()
}

// Tools are the tools the caller may use, in the order they are listed.
func (l *Local) Tools(ctx context.Context) ([]LocalTool, error) {
	var out []LocalTool
	for t, err := range l.cs.Tools(ctx, nil) {
		if err != nil {
			return nil, err
		}
		schema, err := json.Marshal(t.InputSchema)
		if err != nil {
			return nil, err
		}
		lt := LocalTool{Name: t.Name, Description: t.Description, Schema: schema, Title: t.Name}
		if a := t.Annotations; a != nil {
			lt.Title, lt.ReadOnly = a.Title, a.ReadOnlyHint
			lt.Destructive = !a.ReadOnlyHint && (a.DestructiveHint == nil || *a.DestructiveHint)
		}
		out = append(out, lt)
	}
	return out, nil
}

// Call calls a tool, answering its text, with its structured result after
// it as JSON, and whether it failed. err is for a call the protocol
// refused, such as to a tool not offered.
func (l *Local) Call(ctx context.Context, name string, args json.RawMessage) (string, bool, error) {
	var arguments any = map[string]any{}
	if len(args) > 0 {
		if err := json.Unmarshal(args, &arguments); err != nil {
			return "", true, fmt.Errorf("the arguments are not JSON: %w", err)
		}
	}
	res, err := l.cs.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: arguments})
	if err != nil {
		return "", true, err
	}
	var b strings.Builder
	for _, c := range res.Content {
		switch c := c.(type) {
		case *mcp.TextContent:
			b.WriteString(c.Text)
			b.WriteString("\n")
		case *mcp.ImageContent:
			b.WriteString("[an image, shown to the person in the app, not here]\n")
		}
	}
	if res.StructuredContent != nil {
		if raw, err := json.Marshal(res.StructuredContent); err == nil && string(raw) != "{}" && string(raw) != "null" {
			b.WriteString(string(raw))
		}
	}
	return strings.TrimSpace(b.String()), res.IsError, nil
}

// Instructions are what the server tells a client about itself and the
// caller, with the guide every assistant reads first.
func Instructions(c Caller) string {
	return instructions(c) + "\n\n" + guide
}
