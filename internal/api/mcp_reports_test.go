package api

import (
	"slices"
	"strings"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMCPReportTools(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	if _, err := srv.store.CreateAsset("Drill", nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	cs := mcpSession(t, srv, tokenWith(t, srv, admin.ID, "Reporter", rbac.PermAssetsRead, rbac.PermReportsManage))
	spec := map[string]any{"sections": []any{map[string]any{"type": "table", "columns": []any{map[string]any{"heading": "Asset", "binding": "{{asset.name}}"}}}}}

	var out map[string]any
	structured(t, callTool(t, cs, "save_report", map[string]any{"name": "Everything", "spec": spec}), &out)
	id := out["report"].(map[string]any)["id"].(string)
	structured(t, callTool(t, cs, "save_report", map[string]any{"id": id, "name": "All of it"}), &out)
	if r := out["report"].(map[string]any); r["name"] != "All of it" || r["spec"] == nil {
		t.Errorf("renamed: %v", r)
	}
	if res := callTool(t, cs, "run_report", map[string]any{"id": id}); res.IsError || !strings.Contains(toolText(res), "All of it") {
		t.Errorf("run_report: %s", toolText(res))
	}
	res := callTool(t, cs, "report_csv", map[string]any{"id": id, "section": 1})
	csv := ""
	for _, c := range res.Content {
		if er, ok := c.(*mcp.EmbeddedResource); ok {
			csv = er.Resource.Text
		}
	}
	if !strings.Contains(csv, "Drill") {
		t.Errorf("report_csv: %q %s", csv, toolText(res))
	}
	if res := callTool(t, cs, "preview_report", map[string]any{"spec": spec}); res.IsError {
		t.Errorf("preview_report: %s", toolText(res))
	}
	if res := callTool(t, cs, "get_report", map[string]any{"id": id}); res.IsError {
		t.Errorf("get_report: %s", toolText(res))
	}

	// Scan templates: made, made the default, and the old one deletable.
	var scans struct {
		Reports []struct {
			ID      string
			Default bool
		}
	}
	structured(t, callTool(t, cs, "list_reports", map[string]any{"kind": "scan"}), &scans)
	if len(scans.Reports) != 1 || !scans.Reports[0].Default {
		t.Fatalf("scan templates: %+v", scans)
	}
	old := scans.Reports[0].ID
	scanSpec := map[string]any{"sections": []any{map[string]any{"type": "actions", "actions": []string{"open"}}}}
	structured(t, callTool(t, cs, "save_report", map[string]any{"name": "Just open", "kind": "scan", "spec": scanSpec}), &out)
	scan := out["report"].(map[string]any)["id"].(string)
	if msg := toolError(t, cs, "delete_report", map[string]any{"id": old}); !strings.Contains(msg, "409") {
		t.Errorf("deleting the default: %s", msg)
	}
	if res := callTool(t, cs, "set_default_scan_template", map[string]any{"id": scan}); res.IsError {
		t.Errorf("set_default_scan_template: %s", toolText(res))
	}
	for _, r := range []string{old, id} {
		if res := callTool(t, cs, "delete_report", map[string]any{"id": r}); res.IsError {
			t.Errorf("delete_report: %s", toolText(res))
		}
	}

	names := toolNames(t, mcpSession(t, srv, tokenWith(t, srv, admin.ID, "Reader", rbac.PermAssetsRead)))
	if !slices.Contains(names, "run_report") || slices.Contains(names, "save_report") {
		t.Errorf("a reader lists %v", names)
	}
}
