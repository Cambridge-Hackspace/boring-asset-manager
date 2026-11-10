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

// loanReminderSettingsIn are the instance's loan reminder settings.
type loanReminderSettingsIn struct {
	On           bool `json:"on"`
	DaysBefore   int  `json:"days_before" jsonschema:"days before a loan is due to remind, 0 to 30; 0 sends none before"`
	OverdueTimes int  `json:"overdue_times" jsonschema:"reminders once overdue, the day it is and weekly after, 0 to 52"`
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
		simple(&mcp.Tool{Name: "get_loan_reminder_settings", Description: "Whether borrowers are emailed about their loans, how many days " +
			"before a loan is due, and how many times once it is overdue (the day it is, then weekly); and whether email can carry them.",
			Annotations: readOnly("Loan reminder settings")}, "GET /api/v1/settings/loan-reminders", []string{rbac.PermSettingsManage},
			func(none) (request, error) { return request{path: "/api/v1/settings/loan-reminders"}, nil },
			func(_ none, out map[string]any) string {
				return fmt.Sprintf("Reminders on: %v; %v days before; %v times overdue.", out["on"], out["days_before"], out["overdue_times"])
			}),
		simple(&mcp.Tool{Name: "set_loan_reminder_settings", Description: "Turn loan reminders by email on or off, and set how many days " +
			"before a loan is due the first goes (0 for none), and how many go once it is overdue (0 for none). Email must be set up to turn them on.",
			Annotations: changes("Set loan reminder settings", true)}, "PUT /api/v1/settings/loan-reminders", []string{rbac.PermSettingsManage},
			func(in loanReminderSettingsIn) (request, error) {
				return request{path: "/api/v1/settings/loan-reminders", body: in}, nil
			},
			func(_ loanReminderSettingsIn, out map[string]any) string {
				return fmt.Sprintf("Saved: reminders on: %v; %v days before; %v times overdue.", out["on"], out["days_before"], out["overdue_times"])
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
		simple(&mcp.Tool{Name: "get_app_settings", Description: "How BAM! looks installed on a phone or computer (docs/installing.md#on-a-phone): " +
			"its name and short name (empty follows the instance's name), the colors of the bar above it in light and dark, " +
			"the background its icon and opening screen show, and the library image its icon is drawn from (empty for BAM!'s own).",
			Annotations: readOnly("Installed app settings")}, "GET /api/v1/settings/app", nil,
			func(none) (request, error) { return request{path: "/api/v1/settings/app"}, nil },
			func(_ none, out map[string]any) string {
				return fmt.Sprintf("Name %q (instance %q), short name %q; theme %v, dark %v, background %v; icon image %q.",
					out["name"], out["instance_name"], out["short_name"], out["theme_color"], out["theme_color_dark"], out["background_color"], out["icon_image_id"])
			}),
		define(&mcp.Tool{Name: "set_app_settings", Description: "Change how BAM! looks installed on a phone or computer. Give only what changes: " +
			"name and short_name (empty to follow the instance's name), theme_color, theme_color_dark and background_color as #rrggbb, " +
			"and icon_image_id, an image in the library (upload_image adds one), or empty for BAM!'s own icon.",
			Annotations: changes("Set installed app settings", true)}, []string{"PUT /api/v1/settings/app"}, []string{rbac.PermSettingsManage},
			func(ctx context.Context, e *env, in appSettingsIn) (reply[map[string]any], error) {
				var cur map[string]any
				if err := e.call(ctx, http.MethodGet, "/api/v1/settings/app", nil, &cur); err != nil {
					return reply[map[string]any]{}, err
				}
				body := map[string]any{}
				for _, k := range []string{"name", "short_name", "theme_color", "theme_color_dark", "background_color", "icon_image_id"} {
					body[k] = cur[k]
				}
				for k, v := range map[string]*string{"name": in.Name, "short_name": in.ShortName, "theme_color": in.ThemeColor,
					"theme_color_dark": in.ThemeColorDark, "background_color": in.BackgroundColor, "icon_image_id": in.IconImageID} {
					if v != nil {
						body[k] = *v
					}
				}
				out := map[string]any{}
				if err := e.call(ctx, http.MethodPut, "/api/v1/settings/app", body, &out); err != nil {
					return reply[map[string]any]{}, err
				}
				return say(out, "Saved: name %q, short name %q; theme %v, dark %v, background %v; icon image %q.",
					out["name"], out["short_name"], out["theme_color"], out["theme_color_dark"], out["background_color"], out["icon_image_id"])
			}),
	}
}

// appSettingsIn changes the installed app's settings; one left out stays as
// it is.
type appSettingsIn struct {
	Name            *string `json:"name,omitempty" jsonschema:"the app's name; empty follows the instance's name"`
	ShortName       *string `json:"short_name,omitempty" jsonschema:"the name under its icon, about 12 characters; empty follows the name"`
	ThemeColor      *string `json:"theme_color,omitempty" jsonschema:"the bar above the app in light mode, as #rrggbb"`
	ThemeColorDark  *string `json:"theme_color_dark,omitempty" jsonschema:"the bar above the app in dark mode, as #rrggbb"`
	BackgroundColor *string `json:"background_color,omitempty" jsonschema:"behind the icon's logo and on the opening screen, as #rrggbb"`
	IconImageID     *string `json:"icon_image_id,omitempty" jsonschema:"a library image to draw the icon from; empty for BAM!'s own"`
}
