package mcpserver

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type reportIn struct {
	ID   string         `json:"id,omitempty" jsonschema:"the report to change; empty to make one"`
	Name string         `json:"name,omitempty"`
	Kind string         `json:"kind,omitempty" jsonschema:"for a new one: report (the default), or scan for a scan template"`
	Spec map[string]any `json:"spec,omitempty" jsonschema:"what it shows: a filter and sections (tables, groups, charts; for scan templates details, contents and actions), as get_report shows them"`
}

func reportTools() []tool {
	read := []string{rbac.PermAssetsRead}
	manage := []string{rbac.PermReportsManage}
	return []tool{
		simple(&mcp.Tool{Name: "list_reports", Description: "Saved reports, or scan templates (kind scan), with the default scan template marked.",
			Annotations: readOnly("List reports")}, "GET /api/v1/reports", read,
			func(in struct {
				Kind string `json:"kind,omitempty" jsonschema:"report or scan; both if not given"`
			}) (request, error) {
				q := url.Values{}
				if in.Kind != "" {
					q.Set("kind", in.Kind)
				}
				return request{path: "/api/v1/reports", query: q}, nil
			},
			func(_ struct {
				Kind string `json:"kind,omitempty" jsonschema:"report or scan; both if not given"`
			}, out map[string]any) string {
				return fmt.Sprintf("%d reports.", count(out, "reports"))
			}),
		simple(&mcp.Tool{Name: "get_report", Description: "A saved report or scan template, with its spec.", Annotations: readOnly("Get a report")}, "GET /api/v1/reports/{id}", read,
			func(in idIn) (request, error) {
				return request{path: pathOf("/api/v1/reports/{id}", in.ID)}, need(in.ID, "id")
			},
			func(_ idIn, out map[string]any) string { return fmt.Sprintf("%q.", nameOf(out, "report")) }),
		simple(&mcp.Tool{Name: "run_report", Description: "Run a saved report: each section's rows, groups or chart data, of the assets you may see.",
			Annotations: readOnly("Run a report")}, "GET /api/v1/reports/{id}/run", read,
			func(in idIn) (request, error) {
				return request{path: pathOf("/api/v1/reports/{id}/run", in.ID)}, need(in.ID, "id")
			},
			func(_ idIn, out map[string]any) string { return fmt.Sprintf("Ran %q.", nameOf(out, "report")) }),
		define(&mcp.Tool{Name: "report_csv", Description: "One section of a report as a CSV file, as people download it.",
			Annotations: readOnly("Report section as CSV")}, []string{"GET /api/v1/reports/{id}/sections/{n}/csv"}, read,
			func(ctx context.Context, e *env, in struct {
				ID      string `json:"id"`
				Section int    `json:"section" jsonschema:"the section, from 1"`
			}) (reply[map[string]any], error) {
				res := e.api(ctx, http.MethodGet, pathOf("/api/v1/reports/{id}/sections/{n}/csv", in.ID, strconv.Itoa(in.Section)), nil, "")
				if res.Status != http.StatusOK {
					return reply[map[string]any]{}, apiError(res)
				}
				rows := strings.Count(strings.TrimRight(string(res.Body), "\r\n"), "\n")
				r, _ := say(map[string]any{"rows": rows}, "%d rows, as CSV.", rows)
				r.extra = []mcp.Content{&mcp.EmbeddedResource{Resource: &mcp.ResourceContents{URI: "bam://reports/" + in.ID + "/" + strconv.Itoa(in.Section) + ".csv", MIMEType: "text/csv", Text: string(res.Body)}}}
				return r, nil
			}),
		simple(&mcp.Tool{Name: "preview_report", Description: "Run a report's spec without saving it: for a report, its results; for a scan template (kind scan), laid out for an asset.",
			Annotations: readOnly("Preview a report")}, "POST /api/v1/reports/preview", manage,
			func(in struct {
				Kind    string         `json:"kind,omitempty" jsonschema:"report (the default) or scan"`
				AssetID string         `json:"asset_id,omitempty" jsonschema:"for a scan template, the asset to lay it out for"`
				Spec    map[string]any `json:"spec"`
			}) (request, error) {
				return request{path: "/api/v1/reports/preview", body: in}, nil
			},
			func(_ struct {
				Kind    string         `json:"kind,omitempty" jsonschema:"report (the default) or scan"`
				AssetID string         `json:"asset_id,omitempty" jsonschema:"for a scan template, the asset to lay it out for"`
				Spec    map[string]any `json:"spec"`
			}, _ map[string]any) string {
				return "Previewed."
			}),
		define(&mcp.Tool{Name: "save_report", Description: "Make a report or scan template, or change one: its name or spec; what is not given is kept.",
			Annotations: changes("Save a report", true)}, []string{"POST /api/v1/reports", "PUT /api/v1/reports/{id}"}, manage,
			func(ctx context.Context, e *env, in reportIn) (reply[map[string]any], error) {
				out := map[string]any{}
				if in.ID == "" {
					if err := e.call(ctx, http.MethodPost, "/api/v1/reports", map[string]any{"name": in.Name, "kind": in.Kind, "spec": in.Spec}, &out); err != nil {
						return reply[map[string]any]{}, err
					}
					return say(out, "Made %q.", nameOf(out, "report"))
				}
				var got struct {
					Report map[string]any `json:"report"`
				}
				if err := e.call(ctx, http.MethodGet, pathOf("/api/v1/reports/{id}", in.ID), nil, &got); err != nil {
					return reply[map[string]any]{}, err
				}
				body := map[string]any{"name": got.Report["name"], "spec": got.Report["spec"]}
				if in.Name != "" {
					body["name"] = in.Name
				}
				if in.Spec != nil {
					body["spec"] = in.Spec
				}
				if err := e.call(ctx, http.MethodPut, pathOf("/api/v1/reports/{id}", in.ID), body, &out); err != nil {
					return reply[map[string]any]{}, err
				}
				return say(out, "Saved %q.", nameOf(out, "report"))
			}),
		simple(&mcp.Tool{Name: "delete_report", Description: "Delete a report, or a scan template that is not the default and no category names.",
			Annotations: destroys("Delete a report", true)}, "DELETE /api/v1/reports/{id}", manage,
			func(in idIn) (request, error) {
				return request{path: pathOf("/api/v1/reports/{id}", in.ID)}, need(in.ID, "id")
			},
			func(idIn, map[string]any) string { return "Deleted." }),
		simple(&mcp.Tool{Name: "set_default_scan_template", Description: "Make a scan template the default, shown for assets in no category that names one.",
			Annotations: changes("Set the default scan template", true)}, "POST /api/v1/reports/{id}/default", manage,
			func(in idIn) (request, error) {
				return request{path: pathOf("/api/v1/reports/{id}/default", in.ID)}, need(in.ID, "id")
			},
			func(_ idIn, out map[string]any) string {
				return fmt.Sprintf("%q is the default now.", nameOf(out, "report"))
			}),
	}
}
