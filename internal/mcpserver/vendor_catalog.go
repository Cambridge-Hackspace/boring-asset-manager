package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Vendors' catalogs (docs/vendors.md#catalogs): what each vendor sells,
// with part numbers and prices, each entry for a category or an asset.

type listCatalogIn struct {
	VendorID   string `json:"vendor_id,omitempty" jsonschema:"only this vendor's entries"`
	CategoryID string `json:"category_id,omitempty" jsonschema:"only the entries for this category"`
	AssetID    string `json:"asset_id,omitempty" jsonschema:"only the entries for this asset"`
	Preferred  bool   `json:"preferred,omitempty" jsonschema:"only the preferred entries, those to reorder from"`
}

type catalogEntryIn struct {
	ID           string  `json:"id,omitempty" jsonschema:"the entry to change; empty to make one"`
	VendorID     *string `json:"vendor_id,omitempty" jsonschema:"the vendor that sells it; required for a new entry"`
	CategoryID   *string `json:"category_id,omitempty" jsonschema:"the category it is for, for things counted by items; or give asset_id"`
	AssetID      *string `json:"asset_id,omitempty" jsonschema:"the asset it is for, for things counted by quantity; or give category_id"`
	PartNumber   *string `json:"part_number,omitempty" jsonschema:"the vendor's part number"`
	Description  *string `json:"description,omitempty"`
	URL          *string `json:"url,omitempty" jsonschema:"the product page, an http:// or https:// address"`
	PackSize     *int    `json:"pack_size,omitempty" jsonschema:"how many a pack holds, such as 100"`
	PackUnit     *string `json:"pack_unit,omitempty" jsonschema:"what a pack holds, such as screws"`
	Price        *string `json:"price,omitempty" jsonschema:"the price of a pack, as a decimal such as 12.50, in currency"`
	Currency     *string `json:"currency,omitempty" jsonschema:"the price's ISO 4217 currency, such as GBP"`
	PriceChecked *string `json:"price_checked,omitempty" jsonschema:"when the price was checked, YYYY-MM-DD"`
	Preferred    *bool   `json:"preferred,omitempty" jsonschema:"the one to reorder from, of its category's or asset's entries; the others stop being"`
}

func (in catalogEntryIn) changes() map[string]any {
	out := given(map[string]any{
		"vendor_id": in.VendorID, "category_id": in.CategoryID, "asset_id": in.AssetID, "part_number": in.PartNumber,
		"description": in.Description, "url": in.URL, "pack_size": in.PackSize, "pack_unit": in.PackUnit, "price": in.Price,
		"currency": in.Currency, "price_checked": in.PriceChecked, "preferred": in.Preferred,
	})
	// an entry is for one or the other, so naming one forgets the other
	if in.CategoryID != nil && *in.CategoryID != "" {
		out["asset_id"] = ""
	}
	if in.AssetID != nil && *in.AssetID != "" {
		out["category_id"] = ""
	}
	return out
}

type catalogCSVIn struct {
	VendorID string `json:"vendor_id"`
	CSV      string `json:"csv" jsonschema:"the file's text, with a header row, as export_vendor_catalog gives it"`
	Confirm  bool   `json:"confirm,omitempty" jsonschema:"import, rather than check what it would do"`
}

func vendorCatalogTools() []tool {
	manage := []string{rbac.PermVendorsManage}
	read := []string{rbac.PermAssetsRead}
	return []tool{
		simple(&mcp.Tool{Name: "list_catalog", Description: "What vendors sell, as the caller may see it: each entry's vendor, the category or asset it is for, " +
			"part number, description, product page, pack, price as last checked, and whether it is the preferred one to reorder from.",
			Annotations: readOnly("List catalog entries")}, "GET /api/v1/catalog", read,
			func(in listCatalogIn) (request, error) {
				q := url.Values{}
				for k, v := range map[string]string{"vendor_id": in.VendorID, "category_id": in.CategoryID, "asset_id": in.AssetID} {
					if v != "" {
						q.Set(k, v)
					}
				}
				if in.Preferred {
					q.Set("preferred", "true")
				}
				return request{path: "/api/v1/catalog", query: q}, nil
			},
			func(_ listCatalogIn, out map[string]any) string {
				return fmt.Sprintf("%d catalog entries.", count(out, "entries"))
			}),
		simple(&mcp.Tool{Name: "get_catalog_entry", Description: "One thing a vendor sells: its part number, description, product page, pack, price and whether it is preferred.",
			Annotations: readOnly("Get a catalog entry")}, "GET /api/v1/catalog/{id}", read,
			func(in idIn) (request, error) {
				return request{path: pathOf("/api/v1/catalog/{id}", in.ID)}, need(in.ID, "id")
			},
			func(idIn, map[string]any) string { return "The catalog entry." }),
		define(&mcp.Tool{Name: "save_catalog_entry", Description: "Record something a vendor sells, or change an entry: only what is given changes. " +
			"An entry is for a category (things counted by items) or an asset (things counted by quantity), and needs a part number or a description. " +
			"Marking it preferred makes it the one the running-low list reorders from.",
			Annotations: changes("Save a catalog entry", true)}, []string{"POST /api/v1/catalog", "PUT /api/v1/catalog/{id}"}, manage,
			func(ctx context.Context, e *env, in catalogEntryIn) (reply[map[string]any], error) {
				out := map[string]any{}
				if in.ID == "" {
					if err := e.call(ctx, http.MethodPost, "/api/v1/catalog", in.changes(), &out); err != nil {
						return reply[map[string]any]{}, err
					}
					return say(out, "Made the catalog entry.")
				}
				var got struct{ Entry models.CatalogEntry }
				if err := e.call(ctx, http.MethodGet, pathOf("/api/v1/catalog/{id}", in.ID), nil, &got); err != nil {
					return reply[map[string]any]{}, err
				}
				body, err := merged(got.Entry, in.changes())
				if err != nil {
					return reply[map[string]any]{}, err
				}
				if err := e.call(ctx, http.MethodPut, pathOf("/api/v1/catalog/{id}", in.ID), body, &out); err != nil {
					return reply[map[string]any]{}, err
				}
				return say(out, "Saved the catalog entry.")
			}),
		simple(&mcp.Tool{Name: "delete_catalog_entry", Description: "Take something off a vendor's catalog.",
			Annotations: destroys("Delete a catalog entry", true)}, "DELETE /api/v1/catalog/{id}", manage,
			func(in idIn) (request, error) {
				return request{path: pathOf("/api/v1/catalog/{id}", in.ID)}, need(in.ID, "id")
			},
			func(idIn, map[string]any) string { return "Deleted the catalog entry." }),
		define(&mcp.Tool{Name: "export_vendor_catalog", Description: "A vendor's catalog as a CSV file, as people download it, to read or to change and import again with import_vendor_catalog.",
			Annotations: readOnly("Export a vendor's catalog")}, []string{"GET /api/v1/vendors/{id}/catalog.csv"}, read,
			func(ctx context.Context, e *env, in idIn) (reply[exportOut], error) {
				if err := need(in.ID, "id"); err != nil {
					return reply[exportOut]{}, err
				}
				res := e.api(ctx, http.MethodGet, pathOf("/api/v1/vendors/{id}/catalog.csv", in.ID), nil, "")
				if res.Status != http.StatusOK {
					return reply[exportOut]{}, apiError(res)
				}
				rows := strings.Count(strings.TrimRight(string(res.Body), "\r\n"), "\n")
				out := exportOut{Rows: rows, Bytes: len(res.Body)}
				r, _ := say(out, "%d catalog entries, as CSV.", rows)
				r.extra = []mcp.Content{&mcp.EmbeddedResource{Resource: &mcp.ResourceContents{URI: "bam://vendors/" + in.ID + "/catalog.csv", MIMEType: "text/csv", Text: string(res.Body)}}}
				return r, nil
			}),
		define(&mcp.Tool{Name: "import_vendor_catalog", Description: "Import a vendor's catalog from CSV, with the columns export_vendor_catalog writes: " +
			"a row with an id replaces that entry, and one without makes one. First without confirm, to see what it would do and its problems; " +
			"then with confirm to import it, all or nothing. Up to 50 entries made or changed at a time: split a larger file.",
			Annotations: changes("Import a vendor's catalog", false)}, []string{"POST /api/v1/vendors/{id}/catalog.csv"}, manage,
			func(ctx context.Context, e *env, in catalogCSVIn) (reply[map[string]any], error) {
				if err := need(in.VendorID, "vendor_id"); err != nil {
					return reply[map[string]any]{}, err
				}
				body, contentType, err := multipartBody("file", "catalog.csv", []byte(in.CSV), map[string]string{"check": "true"})
				if err != nil {
					return reply[map[string]any]{}, err
				}
				path := pathOf("/api/v1/vendors/{id}/catalog.csv", in.VendorID)
				res := e.api(ctx, http.MethodPost, path, body, contentType)
				if res.Status != http.StatusOK {
					return reply[map[string]any]{}, apiError(res)
				}
				var plan map[string]any
				_ = json.Unmarshal(res.Body, &plan)
				var counts struct {
					Create       int `json:"create"`
					Update       int `json:"update"`
					ProblemCount int `json:"problem_count"`
				}
				_ = json.Unmarshal(res.Body, &counts)
				if !in.Confirm || counts.ProblemCount > 0 {
					return say(plan, "Importing would make %d entries and change %d, with %d problems.", counts.Create, counts.Update, counts.ProblemCount)
				}
				if err := checkItems(counts.Create + counts.Update); err != nil {
					return reply[map[string]any]{}, err
				}
				if body, contentType, err = multipartBody("file", "catalog.csv", []byte(in.CSV), nil); err != nil {
					return reply[map[string]any]{}, err
				}
				res = e.api(ctx, http.MethodPost, path, body, contentType)
				if res.Status != http.StatusOK {
					return reply[map[string]any]{}, apiError(res)
				}
				_ = json.Unmarshal(res.Body, &plan)
				return say(plan, "Imported: %d entries made, %d changed.", counts.Create, counts.Update)
			}),
	}
}
