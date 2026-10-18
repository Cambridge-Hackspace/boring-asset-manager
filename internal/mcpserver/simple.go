package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// pathOf fills a route's {placeholders}, in order, each escaped:
// pathOf("/api/v1/categories/{id}/fields/{field_id}", cat, field).
func pathOf(pattern string, values ...string) string {
	var b strings.Builder
	rest := pattern
	for _, v := range values {
		i := strings.Index(rest, "{")
		j := strings.Index(rest, "}")
		if i < 0 || j < i {
			panic("pathOf: more values than placeholders in " + pattern)
		}
		b.WriteString(rest[:i])
		b.WriteString(url.PathEscape(v))
		rest = rest[j+1:]
	}
	if strings.Contains(rest, "{") {
		panic("pathOf: fewer values than placeholders in " + pattern)
	}
	b.WriteString(rest)
	return b.String()
}

// request is one API request a simple tool makes.
type request struct {
	path  string
	body  any
	query url.Values
}

// simple is a tool that makes one API request, on its route, and passes the
// answer on as it came: for the catalog and administration, whose answers
// are already what a person reads.
func simple[In any](def *mcp.Tool, route string, needs []string, req func(In) (request, error), done func(In, map[string]any) string) tool {
	method, _, _ := strings.Cut(route, " ")
	return define(def, []string{route}, needs, func(ctx context.Context, e *env, in In) (reply[map[string]any], error) {
		r, err := req(in)
		if err != nil {
			return reply[map[string]any]{}, err
		}
		path := r.path
		if len(r.query) > 0 {
			path += "?" + r.query.Encode()
		}
		out := map[string]any{}
		if err := e.call(ctx, method, path, r.body, &out); err != nil {
			return reply[map[string]any]{}, err
		}
		if out == nil {
			out = map[string]any{}
		}
		return say(out, "%s", done(in, out))
	})
}

// merged overlays the fields given on what is there, for an API that
// replaces every property on a change: current is the thing as the API
// reads it, changes the fields to change, by their JSON names.
func merged(current any, changes map[string]any) (map[string]any, error) {
	raw, err := json.Marshal(current)
	if err != nil {
		return nil, err
	}
	out := map[string]any{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	for k, v := range changes {
		out[k] = v
	}
	return out, nil
}

// given are the fields of a change that were given: those not nil.
func given(fields map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range fields {
		switch v := v.(type) {
		case nil:
		case *string:
			if v != nil {
				out[k] = *v
			}
		case *bool:
			if v != nil {
				out[k] = *v
			}
		case *int:
			if v != nil {
				out[k] = *v
			}
		default:
			out[k] = v
		}
	}
	return out
}

// count is how many items an answer's list holds, for a summary.
func count(out map[string]any, key string) int {
	l, _ := out[key].([]any)
	return len(l)
}

// nameOf is the name of the object an answer holds under key.
func nameOf(out map[string]any, key string) string {
	m, _ := out[key].(map[string]any)
	for _, k := range []string{"name", "label", "key", "email"} {
		if s, ok := m[k].(string); ok && s != "" {
			return s
		}
	}
	return key
}

func need(id, what string) error {
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("%s is required", what)
	}
	return nil
}
