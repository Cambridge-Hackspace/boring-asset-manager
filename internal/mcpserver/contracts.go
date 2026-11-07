package mcpserver

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Contracts (docs/contracts.md): warranties, service and maintenance
// agreements, leases and licenses with vendors, and what they cover. Their
// files are attachments, which the attachment tools reach with
// contract_id.

type listContractsIn struct {
	VendorID   string `json:"vendor_id,omitempty" jsonschema:"only the contracts with this vendor"`
	CategoryID string `json:"category_id,omitempty" jsonschema:"only the contracts naming this category"`
	Asset      string `json:"asset,omitempty" jsonschema:"only the contracts covering this asset, by ID or code, whether they name it or one of its categories"`
	Query      string `json:"query,omitempty" jsonschema:"words every contract listed has in its name, kind, reference, vendor or notes"`
}

type contractIn struct {
	ID          string    `json:"id,omitempty" jsonschema:"the contract to change; empty to make one"`
	VendorID    *string   `json:"vendor_id,omitempty" jsonschema:"the vendor it is with"`
	Kind        *string   `json:"kind,omitempty" jsonschema:"warranty, service, maintenance, lease, license or other"`
	Name        *string   `json:"name,omitempty"`
	Reference   *string   `json:"reference,omitempty" jsonschema:"the vendor's reference for it, such as an agreement number"`
	StartDate   *string   `json:"start_date,omitempty" jsonschema:"when it starts, YYYY-MM-DD; empty if not known"`
	EndDate     *string   `json:"end_date,omitempty" jsonschema:"when it ends, YYYY-MM-DD; empty for one that runs until it is ended"`
	AutoRenews  *bool     `json:"auto_renews,omitempty" jsonschema:"whether it renews by itself at its end unless notice is given; it then needs an end date"`
	NoticeDays  *int      `json:"notice_days,omitempty" jsonschema:"how many days before its end notice must be given to stop it renewing"`
	Cost        *string   `json:"cost,omitempty" jsonschema:"what it costs, a decimal such as 1200.00, in currency"`
	Currency    *string   `json:"currency,omitempty" jsonschema:"the cost's ISO 4217 currency, such as GBP"`
	Billing     *string   `json:"billing,omitempty" jsonschema:"how often the cost is paid: once, monthly, quarterly, yearly, or empty when not said"`
	Notes       *string   `json:"notes,omitempty" jsonschema:"notes, in Markdown"`
	AssetIDs    *[]string `json:"asset_ids,omitempty" jsonschema:"the whole list of assets it covers, by ID"`
	CategoryIDs *[]string `json:"category_ids,omitempty" jsonschema:"the whole list of categories it covers, by ID: every asset in them, or below them"`
}

func (in contractIn) changes() map[string]any {
	out := given(map[string]any{
		"vendor_id": in.VendorID, "kind": in.Kind, "name": in.Name, "reference": in.Reference, "start_date": in.StartDate,
		"end_date": in.EndDate, "auto_renews": in.AutoRenews, "notice_days": in.NoticeDays, "cost": in.Cost, "currency": in.Currency,
		"billing": in.Billing, "notes": in.Notes,
	})
	if in.AssetIDs != nil {
		out["asset_ids"] = *in.AssetIDs
	}
	if in.CategoryIDs != nil {
		out["category_ids"] = *in.CategoryIDs
	}
	return out
}

type contractHistoryIn struct {
	ID     string `json:"id"`
	Before string `json:"before,omitempty" jsonschema:"the last event of the previous page, for older ones"`
}

func contractTools() []tool {
	manage := []string{rbac.PermVendorsManage}
	read := []string{rbac.PermAssetsRead}
	return []tool{
		define(&mcp.Tool{Name: "list_contracts", Description: "The contracts the caller may see (warranties, service and maintenance agreements, leases and licenses), " +
			"by when they end, soonest first; optionally only those with a vendor, naming a category, covering an asset, or matching a search.",
			Annotations: readOnly("List contracts")}, []string{"GET /api/v1/contracts"}, read,
			func(ctx context.Context, e *env, in listContractsIn) (reply[map[string]any], error) {
				q := url.Values{}
				for k, v := range map[string]string{"vendor_id": in.VendorID, "category_id": in.CategoryID, "q": in.Query} {
					if v != "" {
						q.Set(k, v)
					}
				}
				if in.Asset != "" {
					id, err := e.assetID(ctx, in.Asset)
					if err != nil {
						return reply[map[string]any]{}, err
					}
					q.Set("asset_id", id)
				}
				path := "/api/v1/contracts"
				if len(q) > 0 {
					path += "?" + q.Encode()
				}
				out := map[string]any{}
				if err := e.call(ctx, http.MethodGet, path, nil, &out); err != nil {
					return reply[map[string]any]{}, err
				}
				return say(out, "%d contracts.", count(out, "contracts"))
			}),
		simple(&mcp.Tool{Name: "get_contract", Description: "One contract: its vendor, kind, reference, dates, whether it renews automatically and its notice deadline, " +
			"its cost where the caller may see it, its notes (Markdown), the assets and categories it covers, and whether the caller may change it.",
			Annotations: readOnly("Get a contract")}, "GET /api/v1/contracts/{id}", read,
			func(in idIn) (request, error) {
				return request{path: pathOf("/api/v1/contracts/{id}", in.ID)}, need(in.ID, "id")
			},
			func(_ idIn, out map[string]any) string { return fmt.Sprintf("%q.", nameOf(out, "contract")) }),
		define(&mcp.Tool{Name: "save_contract", Description: "Make a contract with a vendor, or change one: only what is given changes. " +
			"The assets and categories it covers, when given, replace what it covers. Needs vendors.manage in the vendor's scopes.",
			Annotations: changes("Save a contract", true)}, []string{"POST /api/v1/contracts", "PUT /api/v1/contracts/{id}"}, manage,
			func(ctx context.Context, e *env, in contractIn) (reply[map[string]any], error) {
				out := map[string]any{}
				if in.ID == "" {
					if in.Name == nil || in.VendorID == nil || in.Kind == nil {
						return reply[map[string]any]{}, fmt.Errorf("a new contract needs a name, a vendor_id and a kind")
					}
					if err := e.call(ctx, http.MethodPost, "/api/v1/contracts", in.changes(), &out); err != nil {
						return reply[map[string]any]{}, err
					}
					return say(out, "Made %q.", nameOf(out, "contract"))
				}
				var got struct{ Contract models.Contract }
				if err := e.call(ctx, http.MethodGet, pathOf("/api/v1/contracts/{id}", in.ID), nil, &got); err != nil {
					return reply[map[string]any]{}, err
				}
				body, err := merged(got.Contract, in.changes())
				if err != nil {
					return reply[map[string]any]{}, err
				}
				if err := e.call(ctx, http.MethodPut, pathOf("/api/v1/contracts/{id}", in.ID), body, &out); err != nil {
					return reply[map[string]any]{}, err
				}
				return say(out, "Saved %q.", nameOf(out, "contract"))
			}),
		simple(&mcp.Tool{Name: "delete_contract", Description: "Delete a contract, with its files; the assets and categories it covered stay.",
			Annotations: destroys("Delete a contract", true)}, "DELETE /api/v1/contracts/{id}", manage,
			func(in idIn) (request, error) {
				return request{path: pathOf("/api/v1/contracts/{id}", in.ID)}, need(in.ID, "id")
			},
			func(idIn, map[string]any) string { return "Deleted the contract." }),
		simple(&mcp.Tool{Name: "contract_history", Description: "A contract's history, newest first, a page at a time: when it was made, each change to it, " +
			"what it covers and its files, and the reminders recorded as it nears its end or notice deadline and ends.",
			Annotations: readOnly("Contract history")}, "GET /api/v1/contracts/{id}/history", read,
			func(in contractHistoryIn) (request, error) {
				q := url.Values{}
				if in.Before != "" {
					q.Set("before", in.Before)
				}
				return request{path: pathOf("/api/v1/contracts/{id}/history", in.ID), query: q}, need(in.ID, "id")
			},
			func(_ contractHistoryIn, out map[string]any) string {
				more := ""
				if m, _ := out["more"].(bool); m {
					more = " Older ones remain."
				}
				return fmt.Sprintf("%d events.%s", count(out, "events"), more)
			}),
	}
}
