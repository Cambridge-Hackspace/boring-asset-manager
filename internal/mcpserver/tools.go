package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"sync"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// A tool is one thing an assistant can do, defined once and added to the
// server of each request whose caller may use it.
type tool struct {
	def *mcp.Tool
	// routes are the API routes it reaches, for the coverage test.
	routes []string
	// needs lists it for callers holding any one, in any scope; none lists
	// it for everyone.
	needs []string
	// needsAll, when set, lists it only for callers holding every one, in
	// any scope, as a route needing two permissions does.
	needsAll []string
	// person lists it only for a person: a personal token, acting as them.
	person bool
	// personToo lists it for any person as well as those holding needs.
	personToo bool
	add       func(*mcp.Server, *env)
}

// reply is what a tool answers: its structured result, a short summary for
// the model to tell the person, and any images or files besides.
type reply[Out any] struct {
	out   Out
	text  string
	extra []mcp.Content
}

func say[Out any](out Out, format string, args ...any) (reply[Out], error) {
	return reply[Out]{out: out, text: fmt.Sprintf(format, args...)}, nil
}

// schemaOptions infer a tool's schemas from its Go types, as the SDK would,
// except that a json.RawMessage is any JSON value. Left to the SDK it is a
// []byte's schema, an array of integers, and a tool answering one (a select
// field's options, a report's spec) fails its own output check.
var schemaOptions = &jsonschema.ForOptions{TypeSchemas: map[reflect.Type]*jsonschema.Schema{
	reflect.TypeFor[json.RawMessage](): {},
}}

// schemaFor is the schema of T, or of what T points to, as the SDK takes it.
func schemaFor[T any]() *jsonschema.Schema {
	rt := reflect.TypeFor[T]()
	if rt.Kind() == reflect.Pointer {
		rt = rt.Elem()
	}
	s, err := jsonschema.ForType(rt, schemaOptions)
	if err != nil {
		panic(fmt.Sprintf("schema of %v: %v", rt, err))
	}
	return s
}

// define makes a tool from a typed handler. A tool that changes things
// (ReadOnlyHint false) counts against the caller's change limit.
func define[In, Out any](def *mcp.Tool, routes, needs []string, h func(context.Context, *env, In) (reply[Out], error)) tool {
	if def.Annotations == nil {
		panic("tool " + def.Name + " has no annotations")
	}
	// Inferred once, here, so every server's copy shares the schemas, and the
	// SDK's cache, keyed by the schema, resolves each only once.
	if def.InputSchema == nil {
		def.InputSchema = schemaFor[In]()
	}
	if def.OutputSchema == nil {
		def.OutputSchema = schemaFor[Out]()
	}
	return tool{def: def, routes: routes, needs: needs, add: func(s *mcp.Server, e *env) {
		d := *def // AddTool fills in schemas; each server gets its own copy
		mcp.AddTool(s, &d, func(ctx context.Context, req *mcp.CallToolRequest, in In) (*mcp.CallToolResult, Out, error) {
			var zero Out
			ctx = WithVia(ctx, clientName(req))
			// What cannot be undone is confirmed first, where the client can ask.
			if ask, err := confirmation(def, req); err != nil {
				return nil, zero, err
			} else if ask != nil {
				return ask, zero, nil
			}
			if !def.Annotations.ReadOnlyHint {
				if wait := e.changes.take(e.caller.key()); wait > 0 {
					return nil, zero, fmt.Errorf("too many changes: at most %d a minute on this server. Try again in %d seconds",
						changesPerMinute, int(math.Ceil(wait.Seconds())))
				}
			}
			r, err := h(ctx, e, in)
			if err != nil {
				return nil, zero, err
			}
			return &mcp.CallToolResult{Content: append([]mcp.Content{&mcp.TextContent{Text: r.text}}, r.extra...)}, r.out, nil
		})
	}}
}

// readOnly, changes and destroys are tools' annotations: whether they change
// anything, and whether what they change can be lost.
func readOnly(title string) *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{Title: title, ReadOnlyHint: true, OpenWorldHint: ptr(false)}
}

func changes(title string, idempotent bool) *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{Title: title, IdempotentHint: idempotent, DestructiveHint: ptr(false), OpenWorldHint: ptr(false)}
}

func destroys(title string, idempotent bool) *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{Title: title, IdempotentHint: idempotent, DestructiveHint: ptr(true), OpenWorldHint: ptr(false)}
}

func ptr[T any](v T) *T { return &v }

// maxItems is the most one call may change. More is refused whole, never
// done in part, so a model looping over the catalog stops at once.
const maxItems = 50

func checkItems(n int) error {
	if n > maxItems {
		return fmt.Errorf("%d items is too many for one call: at most %d. Split the work into several calls", n, maxItems)
	}
	return nil
}

// changesPerMinute is how many changing calls one caller may make a minute,
// on each server.
const changesPerMinute = 60

// changeLimiter counts each caller's changing calls over the last minute.
type changeLimiter struct {
	mu   sync.Mutex
	seen map[string][]time.Time
	now  func() time.Time
}

func newChangeLimiter() *changeLimiter {
	return &changeLimiter{seen: map[string][]time.Time{}, now: time.Now}
}

// take counts a call, or, when the caller has made its share, returns how
// long until it may make another.
func (l *changeLimiter) take(key string) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	recent := l.seen[key]
	for len(recent) > 0 && now.Sub(recent[0]) >= time.Minute {
		recent = recent[1:]
	}
	if len(recent) >= changesPerMinute {
		l.seen[key] = recent
		return recent[0].Add(time.Minute).Sub(now)
	}
	l.seen[key] = append(recent, now)
	return 0
}
