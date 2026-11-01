package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Flows (docs/flows.md): guided pages for doing one everyday thing with
// assets, managed with flows.manage and run with flows.run, as a person.

type flowIn struct {
	Slug string `json:"slug" jsonschema:"the flow's address, from list_flows"`
}

type saveFlowIn struct {
	Slug       string          `json:"slug,omitempty" jsonschema:"the flow to change; empty to make one"`
	NewSlug    string          `json:"new_slug,omitempty" jsonschema:"its address, /f/<slug>: lowercase letters, digits, - and _; for a new flow, required"`
	Name       *string         `json:"name,omitempty"`
	Enabled    *bool           `json:"enabled,omitempty"`
	Definition json.RawMessage `json:"definition,omitempty" jsonschema:"what it does, the whole of it (docs/flows.md): {version: 2, description, category_ids, steps}; a form asks its own questions, and a set_fields step puts answers or other values into the asset's fields: {kind: set_fields, fields: [{field, value}], photo}; start from flow_starters"`
}

type runFlowIn struct {
	Slug   string          `json:"slug" jsonschema:"the flow's address, from list_flows"`
	RunID  string          `json:"run_id,omitempty" jsonschema:"the run to go on with; empty to start one"`
	From   string          `json:"from_asset_id,omitempty" jsonschema:"starting: an asset to start from, for a flow that begins by finding something; it starts with that found"`
	Answer json.RawMessage `json:"answer,omitempty" jsonschema:"the answer to the step the run is on, naming it: {step, category_id | answers (by question key) | asset_id | code | place_code}; just {step} tries a step that failed again"`
	Search string          `json:"search,omitempty" jsonschema:"with run_id and no answer: narrows a Find step's list"`
	End    bool            `json:"end,omitempty" jsonschema:"true to give the run up"`
}

func flowTools() []tool {
	manage := []string{rbac.PermFlowsManage}
	see := []string{rbac.PermFlowsManage, rbac.PermFlowsRun}
	run := define(&mcp.Tool{Name: "run_flow", Description: "Run a flow, step by step, as your person: start it (no run_id), see the step it is on (run_id), answer that step (run_id, answer), or give it up (end). " +
		"Each answer names the step it answers; steps that do something, such as filing or archiving, are done as your person, held to their permissions, and recorded as made through the flow.",
		Annotations: destroys("Run a flow", false)},
		[]string{"POST /api/v1/flows/{slug}/runs", "GET /api/v1/flows/{slug}/runs/{id}", "POST /api/v1/flows/{slug}/runs/{id}", "DELETE /api/v1/flows/{slug}/runs/{id}"}, []string{rbac.PermFlowsRun},
		func(ctx context.Context, e *env, in runFlowIn) (reply[map[string]any], error) {
			if err := need(in.Slug, "slug"); err != nil {
				return reply[map[string]any]{}, err
			}
			out := map[string]any{}
			runs := pathOf("/api/v1/flows/{slug}/runs", in.Slug)
			switch {
			case in.RunID == "":
				var body any
				if in.From != "" {
					body = map[string]string{"asset_id": in.From}
				}
				if err := e.call(ctx, http.MethodPost, runs, body, &out); err != nil {
					return reply[map[string]any]{}, err
				}
			case in.End:
				if err := e.call(ctx, http.MethodDelete, runs+"/"+pathOf("{id}", in.RunID), nil, &out); err != nil {
					return reply[map[string]any]{}, err
				}
				return say(out, "Gave the run up.")
			case len(in.Answer) > 0:
				if err := e.call(ctx, http.MethodPost, runs+"/"+pathOf("{id}", in.RunID), in.Answer, &out); err != nil {
					return reply[map[string]any]{}, err
				}
			default:
				path := runs + "/" + pathOf("{id}", in.RunID)
				if in.Search != "" {
					path += "?search=" + url.QueryEscape(in.Search)
				}
				if err := e.call(ctx, http.MethodGet, path, nil, &out); err != nil {
					return reply[map[string]any]{}, err
				}
			}
			return say(out, "%s", promptSummary(out))
		})
	run.person = true
	return []tool{
		define(&mcp.Tool{Name: "list_flows", Description: "Flows: with flows.manage every one, with its steps; otherwise those your person may run; each with what it filed that is past its date to collect by. " +
			"With slug, the one flow; with asset_id, the flows your person may start from that asset (run_flow with from_asset_id).",
			Annotations: readOnly("List flows")}, []string{"GET /api/v1/flows", "GET /api/v1/flows/{slug}", "GET /api/v1/assets/{id}/flows"}, see,
			func(ctx context.Context, e *env, in struct {
				Slug    string `json:"slug,omitempty" jsonschema:"one flow, by its address"`
				AssetID string `json:"asset_id,omitempty" jsonschema:"an asset: the flows that start from it"`
			}) (reply[map[string]any], error) {
				out := map[string]any{}
				if in.AssetID != "" {
					if err := e.call(ctx, http.MethodGet, pathOf("/api/v1/assets/{id}/flows", in.AssetID), nil, &out); err != nil {
						return reply[map[string]any]{}, err
					}
					return say(out, "%d flows start from it.", count(out, "flows"))
				}
				if in.Slug != "" {
					if err := e.call(ctx, http.MethodGet, pathOf("/api/v1/flows/{slug}", in.Slug), nil, &out); err != nil {
						return reply[map[string]any]{}, err
					}
					return say(out, "The flow %q.", nameOf(out, "flow"))
				}
				if err := e.call(ctx, http.MethodGet, "/api/v1/flows", nil, &out); err != nil {
					return reply[map[string]any]{}, err
				}
				return say(out, "%d flows.", count(out, "flows"))
			}),
		simple(&mcp.Tool{Name: "flow_starters", Description: "The ready-made flows to adjust before saving, over the categories given: Leave something (choose, form, set fields, file, label) and Collect it (find, confirm, archive). Definitions are version 2.",
			Annotations: readOnly("Flow starters")}, "GET /api/v1/flow-starters", manage,
			func(in struct {
				CategoryIDs []string `json:"category_ids" jsonschema:"the categories it files into or takes from"`
			}) (request, error) {
				return request{path: "/api/v1/flow-starters?category_ids=" + url.QueryEscape(strings.Join(in.CategoryIDs, ","))}, nil
			},
			func(_ struct {
				CategoryIDs []string `json:"category_ids" jsonschema:"the categories it files into or takes from"`
			}, out map[string]any) string {
				return fmt.Sprintf("%d starters.", count(out, "starters"))
			}),
		define(&mcp.Tool{Name: "save_flow", Description: "Make a flow, or change one (only what is given). It is checked first: every category, field, template and printer it names must exist, " +
			"templates may name only values set before them, and your person must hold every permission its steps use; refused, it says what stops it, by step.",
			Annotations: changes("Save a flow", true)}, []string{"POST /api/v1/flows", "PUT /api/v1/flows/{slug}"}, manage,
			func(ctx context.Context, e *env, in saveFlowIn) (reply[map[string]any], error) {
				body := map[string]any{}
				if in.Slug != "" {
					var current struct {
						Flow map[string]any `json:"flow"`
					}
					if err := e.call(ctx, http.MethodGet, pathOf("/api/v1/flows/{slug}", in.Slug), nil, &current); err != nil {
						return reply[map[string]any]{}, err
					}
					for _, k := range []string{"slug", "name", "enabled", "definition"} {
						body[k] = current.Flow[k]
					}
				}
				if in.NewSlug != "" {
					body["slug"] = in.NewSlug
				}
				if in.Name != nil {
					body["name"] = *in.Name
				}
				if in.Enabled != nil {
					body["enabled"] = *in.Enabled
				}
				if len(in.Definition) > 0 {
					body["definition"] = in.Definition
				}
				out := map[string]any{}
				method, path := http.MethodPost, "/api/v1/flows"
				if in.Slug != "" {
					method, path = http.MethodPut, pathOf("/api/v1/flows/{slug}", in.Slug)
				}
				if err := e.call(ctx, method, path, body, &out); err != nil {
					return reply[map[string]any]{}, err
				}
				return say(out, "Saved the flow %q.", nameOf(out, "flow"))
			}),
		simple(&mcp.Tool{Name: "delete_flow", Description: "Delete a flow, its runs and its record of what it filed; the assets stay.",
			Annotations: destroys("Delete a flow", true)}, "DELETE /api/v1/flows/{slug}", manage,
			func(in flowIn) (request, error) {
				return request{path: pathOf("/api/v1/flows/{slug}", in.Slug)}, need(in.Slug, "slug")
			},
			func(flowIn, map[string]any) string { return "Deleted the flow." }),
		run,
	}
}

// promptSummary says where a run is, in a line: the step and what it asks,
// or what the run did when it is done.
func promptSummary(out map[string]any) string {
	p, _ := out["prompt"].(map[string]any)
	kind, _ := p["kind"].(string)
	step, _ := p["step"].(string)
	if kind == "done" {
		var said []string
		done, _ := p["done"].([]any)
		for _, d := range done {
			if m, ok := d.(map[string]any); ok {
				if s, ok := m["said"].(string); ok {
					said = append(said, s)
				}
			}
		}
		return "Done. " + strings.Join(said, " ")
	}
	where := ""
	if pr, ok := out["progress"].(map[string]any); ok {
		n, _ := pr["step"].(float64)
		of, _ := pr["of"].(float64)
		where = fmt.Sprintf(", %d of %d", int(n), int(of))
	}
	// A form is answered by its questions' keys (#174).
	var keys []string
	questions, _ := p["questions"].([]any)
	for _, q := range questions {
		if m, ok := q.(map[string]any); ok {
			if k, ok := m["key"].(string); ok {
				keys = append(keys, fmt.Sprintf("%q: ...", k))
			}
		}
	}
	if kind == "form" && len(keys) > 0 {
		return fmt.Sprintf("On step %q (%s%s); answer it with {\"step\": %q, \"answers\": {%s}}.", step, kind, where, step, strings.Join(keys, ", "))
	}
	return fmt.Sprintf("On step %q (%s%s); answer it with {\"step\": %q, ...}.", step, kind, where, step)
}
