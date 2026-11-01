package api

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
)

func TestMCPCatalogTools(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	adminCookie := sessionCookie(t, srv, admin.ID)
	cs := mcpSession(t, srv, tokenWith(t, srv, admin.ID, "Curator", rbac.PermAssetsRead, rbac.PermCategoriesManage, rbac.PermStockRecord))
	id := func(out map[string]any, key string) string {
		t.Helper()
		m, _ := out[key].(map[string]any)
		s, _ := m["id"].(string)
		if s == "" {
			t.Fatalf("no %s ID in %v", key, out)
		}
		return s
	}
	var out map[string]any

	// Fields, categories, and what an asset in them records.
	structured(t, callTool(t, cs, "save_field", map[string]any{"key": "serial", "label": "Serial", "validation_type": "text"}), &out)
	serial := id(out, "field")
	structured(t, callTool(t, cs, "save_field", map[string]any{"id": serial, "label": "Serial number"}), &out)
	if f := out["field"].(map[string]any); f["label"] != "Serial number" || f["key"] != "serial" || f["validation_type"] != "text" {
		t.Errorf("changed field: %v", f)
	}
	structured(t, callTool(t, cs, "save_category", map[string]any{"name": "Tools", "is_lendable": true}), &out)
	tools := id(out, "category")
	structured(t, callTool(t, cs, "save_category", map[string]any{"name": "Drills", "parent_id": tools}), &out)
	drills := id(out, "category")
	// Only what is given changes.
	structured(t, callTool(t, cs, "save_category", map[string]any{"id": tools, "color_hex": "#3366ff"}), &out)
	if c := out["category"].(map[string]any); c["is_lendable"] != true || c["name"] != "Tools" || c["color_hex"] != "#3366ff" {
		t.Errorf("changed category: %v", c)
	}
	structured(t, callTool(t, cs, "assign_field", map[string]any{"category_id": tools, "field_id": serial, "required": true}), &out)
	var detail struct {
		Category struct{ Name string }
		Fields   []struct {
			Field      struct{ Key string }
			IsRequired bool `json:"is_required"`
		}
	}
	structured(t, callTool(t, cs, "get_category", map[string]any{"id": drills}), &detail)
	if detail.Category.Name != "Drills" || len(detail.Fields) != 1 || detail.Fields[0].Field.Key != "serial" || !detail.Fields[0].IsRequired {
		t.Errorf("drills: %+v", detail)
	}
	structured(t, callTool(t, cs, "fields_for", map[string]any{"category_ids": []string{drills}}), &out)
	if l, _ := out["fields"].([]any); len(l) != 1 {
		t.Errorf("fields_for: %v", out)
	}
	if res := callTool(t, cs, "list_categories", nil); !strings.Contains(toolText(res), "2 categories") {
		t.Errorf("list_categories: %s", toolText(res))
	}

	// Link types and hooks, made, changed, offered.
	structured(t, callTool(t, cs, "save_link_type", map[string]any{"name": "charges", "inverse_name": "charged by"}), &out)
	charges := id(out, "relation_type")
	structured(t, callTool(t, cs, "save_link_type", map[string]any{"id": charges, "inverse_name": "is charged by"}), &out)
	if rt := out["relation_type"].(map[string]any); rt["name"] != "charges" || rt["inverse_name"] != "is charged by" {
		t.Errorf("link type: %v", rt)
	}
	structured(t, callTool(t, cs, "set_suggested_link_types", map[string]any{"category_id": tools, "ids": []string{charges}}), &out)
	structured(t, callTool(t, cs, "save_hook", map[string]any{"key": "identify", "label": "Identify"}), &out)
	identify := id(out, "hook")
	structured(t, callTool(t, cs, "save_hook", map[string]any{"id": identify, "confirm": "Light it up?"}), &out)
	if a := out["hook"].(map[string]any); a["key"] != "identify" || a["confirm"] != "Light it up?" {
		t.Errorf("hook: %v", a)
	}
	structured(t, callTool(t, cs, "set_category_hooks", map[string]any{"category_id": tools, "hooks": []any{map[string]any{"hook_id": identify, "cascades": true}}}), &out)
	var offered struct {
		SuggestedLinkTypes []string `json:"suggested_link_type_ids"`
		Hooks              []any
	}
	structured(t, callTool(t, cs, "get_category", map[string]any{"id": tools}), &offered)
	if !slices.Equal(offered.SuggestedLinkTypes, []string{charges}) || len(offered.Hooks) != 1 {
		t.Errorf("offered: %+v", offered)
	}
	for _, name := range []string{"list_fields", "list_link_types", "list_hooks", "scan_template_conflicts", "label_template_conflicts"} {
		if res := callTool(t, cs, name, nil); res.IsError {
			t.Errorf("%s: %s", name, toolText(res))
		}
	}
	if res := callTool(t, cs, "get_field", map[string]any{"id": serial}); res.IsError {
		t.Errorf("get_field: %s", toolText(res))
	}

	// Templates chosen for a category.
	var reports struct {
		Reports []struct{ ID string }
	}
	_ = json.Unmarshal(doJSON(t, srv, adminCookie, http.MethodGet, "/api/v1/reports?kind=scan", "").Body.Bytes(), &reports)
	if res := callTool(t, cs, "set_scan_template", map[string]any{"category_id": tools, "template_id": reports.Reports[0].ID}); res.IsError {
		t.Errorf("set_scan_template: %s", toolText(res))
	}
	if res := callTool(t, cs, "set_scan_template", map[string]any{"category_id": tools}); res.IsError {
		t.Errorf("clearing the scan template: %s", toolText(res))
	}
	if res := callTool(t, cs, "set_label_templates", map[string]any{"category_id": tools, "ids": []string{}}); res.IsError {
		t.Errorf("set_label_templates: %s", toolText(res))
	}
	structured(t, callTool(t, cs, "save_category", map[string]any{"name": "Screws", "is_consumable": true}), &out)
	if res := callTool(t, cs, "set_category_running_low", map[string]any{"id": id(out, "category"), "running_low": true}); res.IsError {
		t.Errorf("set_category_running_low: %s", toolText(res))
	}

	// Taking it all apart.
	for _, c := range []struct {
		tool string
		args map[string]any
	}{
		{"unassign_field", map[string]any{"category_id": tools, "field_id": serial}},
		{"delete_category", map[string]any{"id": drills}},
		{"delete_field", map[string]any{"id": serial}},
		{"delete_link_type", map[string]any{"id": charges}},
		{"delete_hook", map[string]any{"id": identify}},
	} {
		if res := callTool(t, cs, c.tool, c.args); res.IsError {
			t.Errorf("%s: %s", c.tool, toolText(res))
		}
	}
	if msg := toolError(t, cs, "delete_category", map[string]any{"id": drills}); !strings.Contains(msg, "404") {
		t.Errorf("deleting twice: %s", msg)
	}

	// Readers read the catalog, and change none of it.
	names := toolNames(t, mcpSession(t, srv, tokenWith(t, srv, admin.ID, "Reader", rbac.PermAssetsRead)))
	if !slices.Contains(names, "get_category") || slices.Contains(names, "save_category") || slices.Contains(names, "delete_field") {
		t.Errorf("a reader lists %v", names)
	}

	// Hooks' tools go by hooks, and none by actions, as they did (#173).
	names = toolNames(t, mcpSession(t, srv, tokenWith(t, srv, admin.ID, "Hook keeper", rbac.PermAssetsRead, rbac.PermCategoriesManage, rbac.PermAssetsHook, rbac.PermEventsRead)))
	for _, name := range []string{"list_hooks", "save_hook", "delete_hook", "set_category_hooks", "trigger_hook", "hook_replies", "reply_to_hook"} {
		if !slices.Contains(names, name) {
			t.Errorf("no %s in %v", name, names)
		}
	}
	for _, name := range names {
		if strings.Contains(name, "action") {
			t.Errorf("a tool is still called %s", name)
		}
	}
}
