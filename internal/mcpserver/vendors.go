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

// Vendors (docs/vendors.md): the suppliers a space buys from, and how to
// reach them. Their files are attachments, which the attachment tools
// reach with vendor_id.

type listVendorsIn struct {
	Query string `json:"query,omitempty" jsonschema:"words every vendor listed has in its name, website, account number, address, notes or contacts"`
}

type vendorIn struct {
	ID            string       `json:"id,omitempty" jsonschema:"the vendor to change; empty to make one"`
	Name          *string      `json:"name,omitempty"`
	Website       *string      `json:"website,omitempty" jsonschema:"its website, an http:// or https:// address, where things are reordered"`
	AccountNumber *string      `json:"account_number,omitempty" jsonschema:"the space's account number with it"`
	Address       *string      `json:"address,omitempty"`
	Notes         *string      `json:"notes,omitempty" jsonschema:"notes, in Markdown"`
	Contacts      *[]contactIn `json:"contacts,omitempty" jsonschema:"the whole list of contacts, in order, each with a name, a role such as sales or support, an email and a phone; give a contact's id to keep it"`
	ScopeIDs      *[]string    `json:"scope_ids,omitempty" jsonschema:"the scopes it is in, which decide who sees it; none, and everyone who reads assets does"`
}

func (in vendorIn) changes() map[string]any {
	out := given(map[string]any{
		"name": in.Name, "website": in.Website, "account_number": in.AccountNumber, "address": in.Address, "notes": in.Notes,
	})
	if in.Contacts != nil {
		out["contacts"] = *in.Contacts
	}
	if in.ScopeIDs != nil {
		out["scope_ids"] = *in.ScopeIDs
	}
	return out
}

// contactIn is a vendor's contact, as given: any of it may be left out.
type contactIn struct {
	ID    string `json:"id,omitempty" jsonschema:"the contact's ID, to keep it"`
	Name  string `json:"name,omitempty"`
	Role  string `json:"role,omitempty" jsonschema:"what they are for, such as sales or support"`
	Email string `json:"email,omitempty"`
	Phone string `json:"phone,omitempty"`
}

type vendorHistoryIn struct {
	ID     string `json:"id"`
	Before string `json:"before,omitempty" jsonschema:"the last event of the previous page, for older ones"`
}

type categoryVendorsIn struct {
	CategoryID string   `json:"category_id"`
	VendorIDs  []string `json:"vendor_ids" jsonschema:"the vendors it is bought from, in order of preference; the whole list"`
}

func vendorTools() []tool {
	manage := []string{rbac.PermVendorsManage}
	read := []string{rbac.PermAssetsRead}
	return []tool{
		simple(&mcp.Tool{Name: "list_vendors", Description: "The vendors the space buys from that the caller may see, by name, with their contacts; optionally only those matching a search.",
			Annotations: readOnly("List vendors")}, "GET /api/v1/vendors", read,
			func(in listVendorsIn) (request, error) {
				q := url.Values{}
				if in.Query != "" {
					q.Set("q", in.Query)
				}
				return request{path: "/api/v1/vendors", query: q}, nil
			},
			func(_ listVendorsIn, out map[string]any) string {
				return fmt.Sprintf("%d vendors.", count(out, "vendors"))
			}),
		simple(&mcp.Tool{Name: "get_vendor", Description: "One vendor: its website, account number, address, notes (Markdown), contacts and scopes, and whether the caller may change it.",
			Annotations: readOnly("Get a vendor")}, "GET /api/v1/vendors/{id}", read,
			func(in idIn) (request, error) {
				return request{path: pathOf("/api/v1/vendors/{id}", in.ID)}, need(in.ID, "id")
			},
			func(_ idIn, out map[string]any) string { return fmt.Sprintf("%q.", nameOf(out, "vendor")) }),
		define(&mcp.Tool{Name: "save_vendor", Description: "Make a vendor, or change one: only what is given changes. Contacts and scopes, when given, replace the vendor's. " +
			"A vendor in no scope is seen by everyone who reads assets, and changing it needs vendors.manage in every scope.",
			Annotations: changes("Save a vendor", true)}, []string{"POST /api/v1/vendors", "PUT /api/v1/vendors/{id}"}, manage,
			func(ctx context.Context, e *env, in vendorIn) (reply[map[string]any], error) {
				out := map[string]any{}
				if in.ID == "" {
					if in.Name == nil {
						return reply[map[string]any]{}, fmt.Errorf("a new vendor needs a name")
					}
					if err := e.call(ctx, http.MethodPost, "/api/v1/vendors", in.changes(), &out); err != nil {
						return reply[map[string]any]{}, err
					}
					return say(out, "Made %q.", nameOf(out, "vendor"))
				}
				var got struct{ Vendor models.Vendor }
				if err := e.call(ctx, http.MethodGet, pathOf("/api/v1/vendors/{id}", in.ID), nil, &got); err != nil {
					return reply[map[string]any]{}, err
				}
				body, err := merged(got.Vendor, in.changes())
				if err != nil {
					return reply[map[string]any]{}, err
				}
				if err := e.call(ctx, http.MethodPut, pathOf("/api/v1/vendors/{id}", in.ID), body, &out); err != nil {
					return reply[map[string]any]{}, err
				}
				return say(out, "Saved %q.", nameOf(out, "vendor"))
			}),
		simple(&mcp.Tool{Name: "delete_vendor", Description: "Delete a vendor, with its contacts and files. Categories stop naming it.",
			Annotations: destroys("Delete a vendor", true)}, "DELETE /api/v1/vendors/{id}", manage,
			func(in idIn) (request, error) {
				return request{path: pathOf("/api/v1/vendors/{id}", in.ID)}, need(in.ID, "id")
			},
			func(idIn, map[string]any) string { return "Deleted the vendor." }),
		simple(&mcp.Tool{Name: "vendor_history", Description: "A vendor's history, newest first, a page at a time: when it was made, and each change to its details, contacts, scopes and files.",
			Annotations: readOnly("Vendor history")}, "GET /api/v1/vendors/{id}/history", read,
			func(in vendorHistoryIn) (request, error) {
				q := url.Values{}
				if in.Before != "" {
					q.Set("before", in.Before)
				}
				return request{path: pathOf("/api/v1/vendors/{id}/history", in.ID), query: q}, need(in.ID, "id")
			},
			func(_ vendorHistoryIn, out map[string]any) string {
				more := ""
				if m, _ := out["more"].(bool); m {
					more = " Older ones remain."
				}
				return fmt.Sprintf("%d events.%s", count(out, "events"), more)
			}),
		simple(&mcp.Tool{Name: "set_category_vendors", Description: "Name the vendors a category is bought from, in order of preference; they show on the running-low list with a link to reorder.",
			Annotations: changes("Set a category's vendors", true)}, "PUT /api/v1/categories/{id}/vendors", []string{rbac.PermCategoriesManage},
			func(in categoryVendorsIn) (request, error) {
				ids := in.VendorIDs
				if ids == nil {
					ids = []string{}
				}
				return request{path: pathOf("/api/v1/categories/{id}/vendors", in.CategoryID), body: map[string]any{"vendor_ids": ids}}, need(in.CategoryID, "category_id")
			},
			func(in categoryVendorsIn, _ map[string]any) string {
				return fmt.Sprintf("The category names %d vendors.", len(in.VendorIDs))
			}),
	}
}
