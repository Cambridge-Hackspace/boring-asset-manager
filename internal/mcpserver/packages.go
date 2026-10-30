package mcpserver

import (
	"context"
	"fmt"
	"net/http"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Packages (README §2.3, #144): part of the instance, chosen with what it
// needs, to share. A package holds only what the caller may read.

type packageIn struct {
	Categories     []string `json:"categories,omitempty" jsonschema:"IDs of single categories"`
	Subtrees       []string `json:"subtrees,omitempty" jsonschema:"IDs of categories to package with every category under them"`
	Fields         []string `json:"fields,omitempty" jsonschema:"field IDs"`
	Actions        []string `json:"actions,omitempty" jsonschema:"action IDs"`
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
		"categories": in.Categories, "subtrees": in.Subtrees, "fields": in.Fields, "actions": in.Actions,
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

func packageTools() []tool {
	read := []string{rbac.PermAssetsRead}
	return []tool{
		define(&mcp.Tool{Name: "export_package", Description: "Package part of the instance to share, as an archive another instance imports: categories (single, or with those under them), " +
			"fields, actions, link types, label and scan templates, reports, flows, scopes, the icon library, and assets, each by ID. What these need comes too " +
			"(a category brings its fields, actions, link types, templates, icon and flows; an icon what it extends and inserts; a flow its templates and actions), " +
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
