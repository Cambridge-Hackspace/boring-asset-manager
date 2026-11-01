package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Live events reach assistants as resources they subscribe to
// (docs/mcp.md): bam://events, narrowed as a live connection is, and
// bam://asset/{id}. A subscription asks the API every second, as its
// caller, whether anything new happened, and tells the client which
// resource changed; reading it gives what did.

const (
	eventsURI       = "bam://events"
	eventsTemplate  = "bam://events{?after,kinds,hooks,categories}"
	assetURIPrefix  = "bam://asset/"
	assetTemplate   = "bam://asset/{id}"
	maxSubscriptons = 5
)

// pollEvery is how often a subscription asks for news; tests shorten it.
var pollEvery = time.Second

// subscriptions counts each caller's open subscriptions on this server.
type subscriptions struct {
	mu   sync.Mutex
	open map[string]int
}

func newSubscriptions() *subscriptions { return &subscriptions{open: map[string]int{}} }

func (s *subscriptions) take(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.open[key] >= maxSubscriptons {
		return false
	}
	s.open[key]++
	return true
}

func (s *subscriptions) give(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.open[key]--; s.open[key] <= 0 {
		delete(s.open, key)
	}
}

// eventsQuery is the API query a bam://events URI asks for.
func eventsQuery(uri string) (url.Values, error) {
	u, err := url.Parse(uri)
	if err != nil || u.Scheme+"://"+u.Host+u.Path != eventsURI {
		return nil, fmt.Errorf("not an events resource: %s", uri)
	}
	q := url.Values{}
	for _, k := range []string{"after", "kinds", "hooks", "categories"} {
		if v := u.Query().Get(k); v != "" {
			q.Set(k, v)
		}
	}
	return q, nil
}

type eventList struct {
	Events []struct {
		ID string `json:"id"`
	} `json:"events"`
	More bool `json:"more"`
}

// addEventResources adds what the caller may read: events with
// events.read, assets with assets.read.
func addEventResources(s *mcp.Server, e *env) {
	if e.caller.may([]string{rbac.PermEventsRead}) {
		s.AddResourceTemplate(&mcp.ResourceTemplate{URITemplate: eventsTemplate, Name: "events", Title: "Events", MIMEType: "application/json",
			Description: "What happened, as live connections are sent it, oldest first: after an event's ID, or the last hour's; kinds, hooks and categories narrow it, comma-separated. Subscribe to be told of news."},
			func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
				q, err := eventsQuery(req.Params.URI)
				if err != nil {
					return nil, mcp.ResourceNotFoundError(req.Params.URI)
				}
				res := e.api(ctx, http.MethodGet, "/api/v1/events?"+q.Encode(), nil, "")
				if res.Status != http.StatusOK {
					return nil, apiError(res)
				}
				return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{URI: req.Params.URI, MIMEType: "application/json", Text: string(res.Body)}}}, nil
			})
	}
	if e.caller.may([]string{rbac.PermAssetsRead}) {
		s.AddResourceTemplate(&mcp.ResourceTemplate{URITemplate: assetTemplate, Name: "asset", Title: "Asset", MIMEType: "application/json",
			Description: "Everything about an asset, as get_asset tells it. Subscribe to be told when it changes."},
			func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
				id := strings.TrimPrefix(req.Params.URI, assetURIPrefix)
				a, err := e.asset(ctx, id)
				if err != nil {
					return nil, err
				}
				d, err := e.detail(ctx, a)
				if err != nil {
					return nil, err
				}
				raw, _ := json.Marshal(d)
				return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{URI: req.Params.URI, MIMEType: "application/json", Text: string(raw)}}}, nil
			})
	}
}

// subscribe starts watching a resource for the life of the request that
// listens for it, telling the client of each change.
func (e *env) subscribe(s *mcp.Server) func(context.Context, *mcp.SubscribeRequest) error {
	return func(ctx context.Context, req *mcp.SubscribeRequest) error {
		uri := req.Params.URI
		var next func(context.Context) (bool, error)
		switch {
		case strings.HasPrefix(uri, eventsURI):
			q, err := eventsQuery(uri)
			if err != nil {
				return err
			}
			if !e.caller.may([]string{rbac.PermEventsRead}) {
				return fmt.Errorf("events need the events.read permission")
			}
			next = e.eventsWatcher(q)
		case strings.HasPrefix(uri, assetURIPrefix):
			id, err := e.assetID(ctx, strings.TrimPrefix(uri, assetURIPrefix))
			if err != nil {
				return err
			}
			next = e.assetWatcher(id)
		default:
			return fmt.Errorf("only bam://events and bam://asset/{id} can be subscribed to")
		}
		// The first look sets where news starts.
		if _, err := next(ctx); err != nil {
			return err
		}
		key := e.caller.key()
		if e.subs != nil && !e.subs.take(key) {
			return fmt.Errorf("at most %d subscriptions at once", maxSubscriptons)
		}
		go func() {
			if e.subs != nil {
				defer e.subs.give(key)
			}
			tick := time.NewTicker(pollEvery)
			defer tick.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-tick.C:
				}
				changed, err := next(ctx)
				if err != nil {
					// Revoked, or no longer allowed: the subscription ends.
					if e.cancel != nil {
						e.cancel()
					}
					return
				}
				if changed {
					_ = s.ResourceUpdated(ctx, &mcp.ResourceUpdatedNotificationParams{URI: uri})
				}
			}
		}()
		return nil
	}
}

// eventsWatcher reports whether events matching q happened since it last
// looked.
func (e *env) eventsWatcher(q url.Values) func(context.Context) (bool, error) {
	after, first := q.Get("after"), true
	return func(ctx context.Context) (bool, error) {
		look := url.Values{}
		for k, v := range q {
			look[k] = v
		}
		look.Set("limit", "500")
		if after != "" {
			look.Set("after", after)
		}
		var list eventList
		if err := e.call(ctx, http.MethodGet, "/api/v1/events?"+look.Encode(), nil, &list); err != nil {
			return false, err
		}
		news := len(list.Events) > 0
		if news {
			after = list.Events[len(list.Events)-1].ID
		}
		if first {
			first = false
			// Only what happens from now on is news.
			return false, nil
		}
		return news, nil
	}
}

// assetWatcher reports whether an asset's history grew since it last
// looked: anything done to it is recorded there.
func (e *env) assetWatcher(id string) func(context.Context) (bool, error) {
	seen, first := "", true
	return func(ctx context.Context) (bool, error) {
		var h struct {
			Events []struct {
				ID string `json:"id"`
			} `json:"events"`
		}
		if err := e.call(ctx, http.MethodGet, assetPath(id, "/history"), nil, &h); err != nil {
			return false, err
		}
		newest := ""
		if len(h.Events) > 0 {
			newest = h.Events[0].ID // newest first
		}
		changed := !first && newest != seen
		first, seen = false, newest
		return changed, nil
	}
}

func eventTools() []tool {
	return []tool{
		simple(&mcp.Tool{Name: "list_events", Description: "What happened lately that you may see, as live connections are sent it, oldest first: after an event's ID, or the last hour's. " +
			"Subscribe to bam://events to be told of news as it happens.", Annotations: readOnly("List events")}, "GET /api/v1/events", []string{rbac.PermEventsRead},
			func(in struct {
				After      string `json:"after,omitempty" jsonschema:"an event's ID: only those since"`
				Kinds      string `json:"kinds,omitempty" jsonschema:"comma-separated event kinds"`
				Hooks      string `json:"hooks,omitempty" jsonschema:"comma-separated hook keys"`
				Categories string `json:"categories,omitempty" jsonschema:"comma-separated category IDs, each with its subcategories"`
				Limit      int    `json:"limit,omitempty" jsonschema:"1 to 500 (100 if not given)"`
			}) (request, error) {
				q := url.Values{}
				for k, v := range map[string]string{"after": in.After, "kinds": in.Kinds, "hooks": in.Hooks, "categories": in.Categories} {
					if v != "" {
						q.Set(k, v)
					}
				}
				if in.Limit > 0 {
					q.Set("limit", fmt.Sprint(in.Limit))
				}
				return request{path: "/api/v1/events", query: q}, nil
			},
			func(_ struct {
				After      string `json:"after,omitempty" jsonschema:"an event's ID: only those since"`
				Kinds      string `json:"kinds,omitempty" jsonschema:"comma-separated event kinds"`
				Hooks      string `json:"hooks,omitempty" jsonschema:"comma-separated hook keys"`
				Categories string `json:"categories,omitempty" jsonschema:"comma-separated category IDs, each with its subcategories"`
				Limit      int    `json:"limit,omitempty" jsonschema:"1 to 500 (100 if not given)"`
			}, out map[string]any) string {
				return fmt.Sprintf("%d events.", count(out, "events"))
			}),
		simple(&mcp.Tool{Name: "reply_to_hook", Description: "Answer a hook triggered on an asset, as a device acting on it does (\"Lit drawer A3\"), once. The person who triggered it sees the answer.",
			Annotations: changes("Answer a hook", false)}, "POST /api/v1/events/{id}/reply", []string{rbac.PermEventsRead},
			func(in struct {
				EventID string `json:"event_id"`
				OK      bool   `json:"ok" jsonschema:"whether it was done"`
				Message string `json:"message,omitempty" jsonschema:"up to 255 characters"`
			}) (request, error) {
				return request{path: pathOf("/api/v1/events/{id}/reply", in.EventID), body: map[string]any{"ok": in.OK, "message": in.Message}}, need(in.EventID, "event_id")
			},
			func(struct {
				EventID string `json:"event_id"`
				OK      bool   `json:"ok" jsonschema:"whether it was done"`
				Message string `json:"message,omitempty" jsonschema:"up to 255 characters"`
			}, map[string]any) string {
				return "Answered."
			}),
	}
}
