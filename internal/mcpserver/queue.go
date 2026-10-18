package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type entriesIn struct {
	EntryIDs []string `json:"entry_ids" jsonschema:"print queue entries, from list_print_queue"`
}

type queueAddIn struct {
	Assets        []string          `json:"assets,omitempty" jsonschema:"assets by ID or code, up to 50; or give category_id, or filter"`
	CategoryID    string            `json:"category_id,omitempty" jsonschema:"every asset in this category"`
	Subcategories bool              `json:"subcategories,omitempty" jsonschema:"and in its subcategories"`
	Filter        map[string]string `json:"filter,omitempty" jsonschema:"every asset a search selects, as search_assets takes it: search, category_id, condition, loan"`
	TemplateID    string            `json:"template_id,omitempty" jsonschema:"the template for all of them; otherwise each takes its category's"`
	Destination   *string           `json:"destination,omitempty" jsonschema:"printer:<id>, pool:<id>, download, or empty for none yet"`
	Copies        int               `json:"copies,omitempty"`
}

type queueUpdateIn struct {
	EntryIDs    []string `json:"entry_ids"`
	TemplateID  *string  `json:"template_id,omitempty" jsonschema:"the template; empty for none"`
	Destination *string  `json:"destination,omitempty" jsonschema:"printer:<id>, pool:<id>, download, or empty for none"`
	Copies      *int     `json:"copies,omitempty"`
}

type queueListIn struct {
	Printed bool `json:"printed,omitempty" jsonschema:"the labels printed, instead of those waiting"`
	Older   bool `json:"older,omitempty" jsonschema:"printed longer than 30 days ago too"`
	Mine    bool `json:"mine,omitempty" jsonschema:"only those you added"`
}

func queueTools() []tool {
	printing := []string{rbac.PermLabelsPrint}
	return []tool{
		simple(&mcp.Tool{Name: "list_print_queue", Description: "The print queue: labels waiting to print, each with its asset, template, destination and copies, and why one cannot print; or those printed.",
			Annotations: readOnly("Print queue")}, "GET /api/v1/spool", printing,
			func(in queueListIn) (request, error) {
				q := url.Values{}
				if in.Printed {
					q.Set("printed", "true")
				}
				if in.Older {
					q.Set("older", "true")
				}
				if in.Mine {
					q.Set("mine", "true")
				}
				return request{path: "/api/v1/spool", query: q}, nil
			},
			func(_ queueListIn, out map[string]any) string {
				return fmt.Sprintf("%d entries.", count(out, "entries"))
			}),
		define(&mcp.Tool{Name: "queue_labels", Description: "Add labels to the print queue, to print together later: for assets, a category's, or a search's. Each takes its category's template unless one is given.",
			Annotations: changes("Add to the print queue", false)}, []string{"POST /api/v1/spool"}, printing,
			func(ctx context.Context, e *env, in queueAddIn) (reply[map[string]any], error) {
				if err := checkItems(len(in.Assets)); err != nil {
					return reply[map[string]any]{}, err
				}
				ids, err := e.assetIDs(ctx, in.Assets)
				if err != nil {
					return reply[map[string]any]{}, err
				}
				// Exactly one of the assets, a category or a filter.
				body := map[string]any{"template_id": in.TemplateID, "destination": in.Destination, "copies": in.Copies}
				switch {
				case len(ids) > 0:
					body["asset_ids"] = ids
				case in.CategoryID != "":
					body["category_id"], body["subcategories"] = in.CategoryID, in.Subcategories
				case len(in.Filter) > 0:
					body["filter"] = in.Filter
				}
				out := map[string]any{}
				if err := e.call(ctx, http.MethodPost, "/api/v1/spool", body, &out); err != nil {
					return reply[map[string]any]{}, err
				}
				return say(out, "Added %d to the print queue; %d skipped.", count(out, "added"), count(out, "skipped"))
			}),
		simple(&mcp.Tool{Name: "update_print_queue", Description: "Change waiting entries' template, destination or copies.",
			Annotations: changes("Change queue entries", true)}, "PATCH /api/v1/spool", printing,
			func(in queueUpdateIn) (request, error) { return request{path: "/api/v1/spool", body: in}, nil },
			func(in queueUpdateIn, _ map[string]any) string {
				return fmt.Sprintf("Changed %d entries.", len(in.EntryIDs))
			}),
		simple(&mcp.Tool{Name: "remove_from_print_queue", Description: "Take entries out of the print queue without printing them.",
			Annotations: destroys("Remove from the queue", true)}, "POST /api/v1/spool/remove", printing,
			func(in entriesIn) (request, error) { return request{path: "/api/v1/spool/remove", body: in}, nil },
			func(in entriesIn, _ map[string]any) string {
				return fmt.Sprintf("Removed %d entries.", len(in.EntryIDs))
			}),
		simple(&mcp.Tool{Name: "put_back_in_print_queue", Description: "Put printed entries back in the print queue, to print again.",
			Annotations: changes("Put back in the queue", false)}, "POST /api/v1/spool/requeue", printing,
			func(in entriesIn) (request, error) { return request{path: "/api/v1/spool/requeue", body: in}, nil },
			func(in entriesIn, _ map[string]any) string {
				return fmt.Sprintf("Put %d entries back.", len(in.EntryIDs))
			}),
		simple(&mcp.Tool{Name: "print_queue", Description: "Print the waiting entries (all, or those named) on their destinations, making print jobs. Says which could not print, and why.",
			Annotations: changes("Print the queue", false)}, "POST /api/v1/spool/print", printing,
			func(in struct {
				EntryIDs []string `json:"entry_ids,omitempty" jsonschema:"only these; all those waiting with a printer if not given"`
			}) (request, error) {
				return request{path: "/api/v1/spool/print", body: map[string]any{"entry_ids": in.EntryIDs}}, nil
			},
			func(_ struct {
				EntryIDs []string `json:"entry_ids,omitempty" jsonschema:"only these; all those waiting with a printer if not given"`
			}, out map[string]any) string {
				return fmt.Sprintf("%d print jobs; %d entries not printed.", count(out, "jobs"), count(out, "skipped"))
			}),
		define(&mcp.Tool{Name: "download_print_queue", Description: "The waiting entries' labels as one file, a PDF or a ZIP of PNGs, to print elsewhere; they count as printed.",
			Annotations: changes("Download the queue", false)}, []string{"POST /api/v1/spool/download"}, printing,
			func(ctx context.Context, e *env, in struct {
				EntryIDs []string `json:"entry_ids"`
				Format   string   `json:"format" jsonschema:"pdf or zip"`
				Mono     bool     `json:"mono,omitempty"`
			}) (reply[map[string]any], error) {
				raw, _ := json.Marshal(in)
				res := e.api(ctx, http.MethodPost, "/api/v1/spool/download", raw, "application/json")
				if res.Status != http.StatusOK {
					return reply[map[string]any]{}, apiError(res)
				}
				out := map[string]any{"format": in.Format, "bytes": len(res.Body)}
				r, _ := say(out, "The labels, as %s.", in.Format)
				r.extra = []mcp.Content{fileContent("bam://print-queue/labels."+in.Format, res)}
				return r, nil
			}),
		simple(&mcp.Tool{Name: "list_print_jobs", Description: "Print jobs, newest first: what each printed, where, and whether it was sent, is waiting, or failed and why.",
			Annotations: readOnly("Print jobs")}, "GET /api/v1/spool/jobs", printing,
			func(in queueListIn) (request, error) {
				q := url.Values{}
				if in.Older {
					q.Set("older", "true")
				}
				return request{path: "/api/v1/spool/jobs", query: q}, nil
			},
			func(_ queueListIn, out map[string]any) string { return fmt.Sprintf("%d jobs.", count(out, "jobs")) }),
		simple(&mcp.Tool{Name: "cancel_print_job", Description: "Cancel a print job not yet sent.", Annotations: destroys("Cancel a print job", true)}, "POST /api/v1/spool/jobs/{id}/cancel", printing,
			func(in idIn) (request, error) {
				return request{path: pathOf("/api/v1/spool/jobs/{id}/cancel", in.ID)}, need(in.ID, "id")
			},
			func(idIn, map[string]any) string { return "Canceled the job." }),
		simple(&mcp.Tool{Name: "resend_print_job", Description: "Put a job's labels back in the queue and print them again, on its printer or another destination.",
			Annotations: changes("Send a job again", false)}, "POST /api/v1/spool/jobs/{id}/resend", printing,
			func(in struct {
				ID          string  `json:"id"`
				Destination *string `json:"destination,omitempty" jsonschema:"printer:<id> or pool:<id>; its own if not given"`
			}) (request, error) {
				return request{path: pathOf("/api/v1/spool/jobs/{id}/resend", in.ID), body: map[string]any{"destination": in.Destination}}, need(in.ID, "id")
			},
			func(_ struct {
				ID          string  `json:"id"`
				Destination *string `json:"destination,omitempty" jsonschema:"printer:<id> or pool:<id>; its own if not given"`
			}, out map[string]any) string {
				return fmt.Sprintf("Sent again: %d jobs.", count(out, "jobs"))
			}),
	}
}
