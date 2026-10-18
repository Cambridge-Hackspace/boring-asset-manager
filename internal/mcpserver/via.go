package mcpserver

import (
	"context"
	"strings"
	"unicode"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type viaKey struct{}

// WithVia notes on a context the assistant a request is made through.
func WithVia(ctx context.Context, assistant string) context.Context {
	return context.WithValue(ctx, viaKey{}, assistant)
}

// ViaFrom is the assistant a request is made through, or "" when it was not
// made by a tool. Only requests made in process carry it: a request from
// outside cannot set a context.
func ViaFrom(ctx context.Context) string {
	v, _ := ctx.Value(viaKey{}).(string)
	return v
}

// clientName is the name the calling MCP client gives, cleaned as uploaded
// image names are, or "MCP" when it gives none (as a client of the protocol
// before 2026-07-28 does not, on each request to a stateless server).
func clientName(req *mcp.CallToolRequest) string {
	var name string
	if req != nil {
		if ci := req.ClientInfo(); ci != nil {
			name = ci.Title
			if strings.TrimSpace(name) == "" {
				name = ci.Name
			}
		}
	}
	name = strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, strings.ToValidUTF8(name, "")))
	if r := []rune(name); len(r) > 255 {
		name = string(r[:255])
	}
	if name == "" {
		return "MCP"
	}
	return name
}
