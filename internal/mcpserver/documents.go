package mcpserver

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// documentIn is how a PDF is made (docs/reports.md#pdf).
type documentIn struct {
	PageSize    string `json:"page_size,omitempty" jsonschema:"letter or a4; the instance's when not given"`
	Orientation string `json:"orientation,omitempty" jsonschema:"portrait (the default) or landscape"`
	Photos      bool   `json:"photos,omitempty" jsonschema:"each row with its asset's photo"`
	Labels      bool   `json:"labels,omitempty" jsonschema:"each row with its asset's label"`
}

func (in documentIn) query(q url.Values) string {
	for k, v := range map[string]string{"page_size": in.PageSize, "orientation": in.Orientation} {
		if v != "" {
			q.Set(k, v)
		}
	}
	if in.Photos {
		q.Set("photos", "1")
	}
	if in.Labels {
		q.Set("labels", "1")
	}
	if len(q) == 0 {
		return ""
	}
	return "?" + q.Encode()
}

// pageIn is how a sheet's pages are made.
type pageIn struct {
	PageSize    string `json:"page_size,omitempty" jsonschema:"letter or a4; the instance's when not given"`
	Orientation string `json:"orientation,omitempty" jsonschema:"portrait (the default) or landscape"`
}

func (in pageIn) query(q url.Values) string {
	return documentIn{PageSize: in.PageSize, Orientation: in.Orientation}.query(q)
}

// filters are the asset list's search and category, as a query.
func filters(search, categoryID string) url.Values {
	q := url.Values{}
	for k, v := range map[string]string{"search": search, "category_id": categoryID} {
		if v != "" {
			q.Set(k, v)
		}
	}
	return q
}

// pdfReply is a PDF the API made, for the client to keep.
func pdfReply(res Response, uri string) (reply[map[string]any], error) {
	if res.Status != http.StatusOK {
		return reply[map[string]any]{}, apiError(res)
	}
	r, _ := say(map[string]any{"bytes": len(res.Body)}, "A PDF of %d bytes.", len(res.Body))
	r.extra = []mcp.Content{fileContent(uri, res)}
	return r, nil
}

func documentTools() []tool {
	read := []string{rbac.PermAssetsRead}
	return []tool{
		define(&mcp.Tool{Name: "report_pdf", Description: "A saved report as a PDF, as people download it: every section, tables and charts, " +
			"to print or share.", Annotations: readOnly("Report as PDF")}, []string{"GET /api/v1/reports/{id}/export.pdf"}, read,
			func(ctx context.Context, e *env, in struct {
				ID string `json:"id"`
				documentIn
			}) (reply[map[string]any], error) {
				if err := need(in.ID, "id"); err != nil {
					return reply[map[string]any]{}, err
				}
				res := e.api(ctx, http.MethodGet, pathOf("/api/v1/reports/{id}/export.pdf", in.ID)+in.query(url.Values{}), nil, "")
				return pdfReply(res, "bam://reports/"+in.ID+".pdf")
			}),
		define(&mcp.Tool{Name: "assets_pdf", Description: "The assets a search selects as a PDF table, as people download it, " +
			"with the columns chosen from export_assets' columns.", Annotations: readOnly("Assets as PDF")}, []string{"GET /api/v1/assets/export.pdf"}, read,
			func(ctx context.Context, e *env, in struct {
				Search     string   `json:"search,omitempty" jsonschema:"words to find in an asset's name, its codes, or the name of a place it is in"`
				CategoryID string   `json:"category_id,omitempty" jsonschema:"only assets in this category or below it"`
				Columns    []string `json:"columns,omitempty" jsonschema:"export_assets' columns to show, such as name, categories, quantity, code.qr or field.serial; name, categories and quantity if not given"`
				documentIn
			}) (reply[map[string]any], error) {
				q := url.Values{}
				for k, v := range map[string]string{"search": in.Search, "category_id": in.CategoryID, "columns": strings.Join(in.Columns, ",")} {
					if v != "" {
						q.Set(k, v)
					}
				}
				res := e.api(ctx, http.MethodGet, "/api/v1/assets/export.pdf"+in.query(q), nil, "")
				return pdfReply(res, "bam://export/assets.pdf")
			}),
		define(&mcp.Tool{Name: "asset_sheets_pdf", Description: "Assets' sheets as a PDF, one page each, as people download them: " +
			"photo, codes drawn, label, categories, place, condition, loan, fields, links and recent history, for insurance records, " +
			"handovers or a machine's binder. Give id for one asset, or a search and category for many.",
			Annotations: readOnly("Asset sheets")}, []string{"GET /api/v1/assets/{id}/sheet.pdf", "GET /api/v1/assets/sheets.pdf"}, read,
			func(ctx context.Context, e *env, in struct {
				ID         string `json:"id,omitempty" jsonschema:"one asset's sheet"`
				Search     string `json:"search,omitempty" jsonschema:"words to find in an asset's name, its codes, or the name of a place it is in"`
				CategoryID string `json:"category_id,omitempty" jsonschema:"only assets in this category or below it"`
				pageIn
			}) (reply[map[string]any], error) {
				if in.ID != "" {
					res := e.api(ctx, http.MethodGet, pathOf("/api/v1/assets/{id}/sheet.pdf", in.ID)+in.query(url.Values{}), nil, "")
					return pdfReply(res, "bam://assets/"+in.ID+"/sheet.pdf")
				}
				res := e.api(ctx, http.MethodGet, "/api/v1/assets/sheets.pdf"+in.query(filters(in.Search, in.CategoryID)), nil, "")
				return pdfReply(res, "bam://export/asset-sheets.pdf")
			}),
		define(&mcp.Tool{Name: "stocktake_pdf", Description: "A stocktake checklist as a PDF: the assets a search selects, grouped by the place " +
			"they are in, outermost first, in order to walk the rooms, each with a box to tick, the quantity expected and a blank for the count.",
			Annotations: readOnly("Stocktake checklist")}, []string{"GET /api/v1/assets/stocktake.pdf"}, read,
			func(ctx context.Context, e *env, in struct {
				Search     string `json:"search,omitempty" jsonschema:"words to find in an asset's name, its codes, or the name of a place it is in"`
				CategoryID string `json:"category_id,omitempty" jsonschema:"only assets in this category or below it"`
				pageIn
			}) (reply[map[string]any], error) {
				res := e.api(ctx, http.MethodGet, "/api/v1/assets/stocktake.pdf"+in.query(filters(in.Search, in.CategoryID)), nil, "")
				return pdfReply(res, "bam://export/stocktake.pdf")
			}),
		simple(&mcp.Tool{Name: "get_document_settings", Description: "The instance's name, at the top of every page of a PDF, and the page size PDFs are made in unless asked otherwise.",
			Annotations: readOnly("Document settings")}, "GET /api/v1/settings/documents", read,
			func(none) (request, error) { return request{path: "/api/v1/settings/documents"}, nil },
			func(_ none, out map[string]any) string {
				return fmt.Sprintf("%v, on %v pages.", out["instance_name"], out["page_size"])
			}),
		simple(&mcp.Tool{Name: "set_document_settings", Description: "Set the instance's name, at the top of every page of a PDF, and the page size PDFs are made in.",
			Annotations: changes("Set document settings", true)}, "PUT /api/v1/settings/documents", []string{rbac.PermSettingsManage},
			func(in struct {
				InstanceName string `json:"instance_name"`
				PageSize     string `json:"page_size" jsonschema:"letter or a4"`
			}) (request, error) {
				return request{path: "/api/v1/settings/documents", body: in}, nil
			},
			func(_ struct {
				InstanceName string `json:"instance_name"`
				PageSize     string `json:"page_size" jsonschema:"letter or a4"`
			}, out map[string]any) string {
				return fmt.Sprintf("Saved: %v, on %v pages.", out["instance_name"], out["page_size"])
			}),
	}
}
