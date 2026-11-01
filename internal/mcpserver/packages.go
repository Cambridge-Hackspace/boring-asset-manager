package mcpserver

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Packages (docs/packages.md, #144): part of the instance, chosen with what it
// needs, to share. A package holds only what the caller may read.

type packageIn struct {
	Categories     []string `json:"categories,omitempty" jsonschema:"IDs of single categories"`
	Subtrees       []string `json:"subtrees,omitempty" jsonschema:"IDs of categories to package with every category under them"`
	Fields         []string `json:"fields,omitempty" jsonschema:"field IDs"`
	Hooks          []string `json:"hooks,omitempty" jsonschema:"hook IDs"`
	RelationTypes  []string `json:"relation_types,omitempty" jsonschema:"link and relation type IDs"`
	LabelTemplates []string `json:"label_templates,omitempty" jsonschema:"label template IDs"`
	ScanTemplates  []string `json:"scan_templates,omitempty" jsonschema:"scan template IDs (reports of kind scan)"`
	Reports        []string `json:"reports,omitempty" jsonschema:"report IDs"`
	Flows          []string `json:"flows,omitempty" jsonschema:"flow IDs"`
	Scopes         []string `json:"scopes,omitempty" jsonschema:"scope IDs"`
	Icons          bool     `json:"icons,omitempty" jsonschema:"the whole icon library, with the palette and icon settings"`
	Assets         string   `json:"assets,omitempty" jsonschema:"none (the default), chosen (those in the categories chosen) or all"`
	Photos         bool     `json:"photos,omitempty" jsonschema:"with the assets' photos"`
	Links          bool     `json:"links,omitempty" jsonschema:"with the links between the assets packaged"`
	History        bool     `json:"history,omitempty" jsonschema:"with the assets' history, without who did what"`
	Name           string   `json:"name,omitempty" jsonschema:"the package's name; needed unless plan_only"`
	Description    string   `json:"description,omitempty"`
	Author         string   `json:"author,omitempty"`
	License        string   `json:"license,omitempty" jsonschema:"such as CC-BY-4.0"`
	Version        string   `json:"version,omitempty"`
	PlanOnly       bool     `json:"plan_only,omitempty" jsonschema:"say what the package would hold, and make none"`
}

func (in packageIn) selection() (map[string]any, error) {
	sel := map[string]any{
		"categories": in.Categories, "subtrees": in.Subtrees, "fields": in.Fields, "hooks": in.Hooks,
		"relation_types": in.RelationTypes, "label_templates": in.LabelTemplates, "scan_templates": in.ScanTemplates,
		"reports": in.Reports, "flows": in.Flows, "scopes": in.Scopes, "icons": in.Icons,
	}
	switch in.Assets {
	case "", "none":
		if in.Photos || in.Links || in.History {
			return nil, fmt.Errorf("photos, links and history come with assets: set assets to chosen or all")
		}
	case "chosen", "all":
		sel["assets"] = map[string]any{"all": in.Assets == "all", "photos": in.Photos, "links": in.Links, "history": in.History}
	default:
		return nil, fmt.Errorf("assets is none, chosen or all")
	}
	return sel, nil
}

// planSaid tells what a plan holds.
func planSaid(plan map[string]any) string {
	items, _ := plan["items"].([]any)
	left, _ := plan["left_out"].([]any)
	s := fmt.Sprintf("%d things", len(items))
	if a, ok := plan["assets"].(map[string]any); ok {
		s += fmt.Sprintf(", %v assets", a["assets"])
		if n, _ := a["unreadable"].(float64); n > 0 {
			s += fmt.Sprintf(" (%v more you cannot read are left out)", n)
		}
	}
	if len(left) > 0 {
		s += fmt.Sprintf("; %d things they name stay behind, each with why", len(left))
	}
	return s
}

// packageImportIn is what checking or importing an upload takes (#145).
type packageImportIn struct {
	UploadID string            `json:"upload_id" jsonschema:"the ID of a package or backup the person uploaded on the Import & export page"`
	Anchor   string            `json:"anchor,omitempty" jsonschema:"the ID of the category its top categories go under; by default the top level"`
	Choices  map[string]string `json:"choices,omitempty" jsonschema:"what to do with what clashes, by the plan's kind:id: existing, copy or skip"`
	Kinds    []string          `json:"kinds,omitempty" jsonschema:"of a backup, the kinds to merge: catalog, labels, reports, icons, flows, assets; by default all of them"`
	// Fingerprint, importing, is what check_import said.
	Fingerprint string `json:"fingerprint,omitempty" jsonschema:"importing, the fingerprint check_import gave, so that it does what was checked; without it, it is checked again first"`
}

func (in packageImportIn) body() map[string]any {
	return map[string]any{"upload_id": in.UploadID, "anchor": in.Anchor, "choices": in.Choices, "kinds": in.Kinds}
}

// importJob runs a check or an import, and waits for it.
func importJob(ctx context.Context, e *env, path string, body map[string]any) (map[string]any, error) {
	var job struct {
		ID string `json:"id"`
	}
	if err := e.call(ctx, http.MethodPost, path, body, &job); err != nil {
		return nil, err
	}
	for {
		var out struct {
			Done   bool           `json:"done"`
			Error  string         `json:"error"`
			Result map[string]any `json:"result"`
		}
		if err := e.call(ctx, http.MethodGet, "/api/v1/import/jobs/"+job.ID, nil, &out); err != nil {
			return nil, err
		}
		if out.Done {
			plan, _ := out.Result["plan"].(map[string]any)
			if out.Error != "" {
				if plan != nil {
					return nil, fmt.Errorf("%s. The plan: %s", out.Error, importSaid(plan))
				}
				return nil, fmt.Errorf("%s", out.Error)
			}
			return plan, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
}

// importSaid tells what an import plan does.
func importSaid(plan map[string]any) string {
	items, _ := plan["items"].([]any)
	counts := map[string]int{}
	clashes := 0
	for _, it := range items {
		it, _ := it.(map[string]any)
		counts[fmt.Sprint(it["action"])]++
		if cs, _ := it["choices"].([]any); len(cs) > 0 {
			clashes++
		}
	}
	s := fmt.Sprintf("%d created, %d used as they are here, %d copied, %d skipped; %d clash with what is here", counts["create"], counts["existing"], counts["copy"], counts["skip"], clashes)
	if a, ok := plan["assets"].(map[string]any); ok {
		s += fmt.Sprintf("; %v assets", a["assets"])
	}
	if p, _ := plan["problems"].([]any); len(p) > 0 {
		var ps []string
		for _, x := range p {
			ps = append(ps, fmt.Sprint(x))
		}
		s += ". Problems: " + strings.Join(ps, "; ")
	}
	return s
}

func packageTools() []tool {
	read := []string{rbac.PermAssetsRead}
	settings := []string{rbac.PermSettingsManage}
	return []tool{
		define(&mcp.Tool{Name: "check_import", Description: "Check a package, or a backup, that the person uploaded on the Import & export page, by its upload ID, and say what importing it would do, " +
			"changing nothing: what is created, what is used as it is here, what is copied under a new name and what is skipped. What clashes with something here by its key or name " +
			"lists its choices (existing, copy, skip); choose by the item's kind:id. Its top categories go under anchor, a category's ID, or the top level. " +
			"It is refused if it needs a permission you lack, and lists problems that stop an import.",
			Annotations: readOnly("Check an import")}, []string{"POST /api/v1/import/check", "GET /api/v1/import/jobs/{id}"}, settings,
			func(ctx context.Context, e *env, in packageImportIn) (reply[map[string]any], error) {
				plan, err := importJob(ctx, e, "/api/v1/import/check", in.body())
				if err != nil {
					return reply[map[string]any]{}, err
				}
				return say(plan, "Importing it would do this: %s.", importSaid(plan))
			}),
		define(&mcp.Tool{Name: "import_package", Description: "Import a package, or the kinds chosen of a backup, that the person uploaded, as check_import says, with the same anchor and choices: " +
			"it is checked again and refused if it would now do something else. Everything comes in with new IDs, in one go. People, settings and authentication never come in.",
			Annotations: changes("Import a package", false)}, []string{"POST /api/v1/import"}, settings,
			func(ctx context.Context, e *env, in packageImportIn) (reply[map[string]any], error) {
				body := in.body()
				body["fingerprint"] = in.Fingerprint
				if in.Fingerprint == "" {
					plan, err := importJob(ctx, e, "/api/v1/import/check", in.body())
					if err != nil {
						return reply[map[string]any]{}, err
					}
					body["fingerprint"] = plan["fingerprint"]
				}
				done, err := importJob(ctx, e, "/api/v1/import", body)
				if err != nil {
					return reply[map[string]any]{}, err
				}
				return say(done, "Imported: %s.", importSaid(done))
			}),
		define(&mcp.Tool{Name: "export_package", Description: "Package part of the instance to share, as an archive another instance imports: categories (single, or with those under them), " +
			"fields, hooks, link types, label and scan templates, reports, flows, scopes, the icon library, and assets, each by ID. What these need comes too " +
			"(a category brings its fields, hooks, link types, templates, icon and flows; an icon what it extends and inserts; a flow its templates and hooks), " +
			"listed in the plan as needed_by. Never in a package: people, passwords, sessions, tokens, notification secrets or printers. It holds only what you may read. " +
			"With plan_only, it says what it would hold; otherwise it answers where to download the package, within the hour, with this connection's credentials.",
			Annotations: readOnly("Export a package")}, []string{"POST /api/v1/exports/plan", "POST /api/v1/exports", "GET /api/v1/exports/{id}"}, read,
			func(ctx context.Context, e *env, in packageIn) (reply[map[string]any], error) {
				sel, err := in.selection()
				if err != nil {
					return reply[map[string]any]{}, err
				}
				if in.PlanOnly {
					out := map[string]any{}
					if err := e.call(ctx, http.MethodPost, "/api/v1/exports/plan", map[string]any{"selection": sel}, &out); err != nil {
						return reply[map[string]any]{}, err
					}
					return say(out, "The package would hold %s.", planSaid(out))
				}
				if in.Name == "" {
					return reply[map[string]any]{}, fmt.Errorf("name is needed: what the package is called")
				}
				out := map[string]any{}
				body := map[string]any{"selection": sel, "name": in.Name, "description": in.Description, "author": in.Author, "license": in.License, "version": in.Version}
				if err := e.call(ctx, http.MethodPost, "/api/v1/exports", body, &out); err != nil {
					return reply[map[string]any]{}, err
				}
				plan, _ := out["plan"].(map[string]any)
				return say(out, "The package holds %s. Download it from %v on this server, within the hour.", planSaid(plan), out["download"])
			}),
	}
}
