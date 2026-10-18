package api

import (
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
)

func TestMCPAdminTools(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	adminCookie := sessionCookie(t, srv, admin.ID)
	cs := mcpSession(t, srv, tokenWith(t, srv, admin.ID, "Admin assistant", rbac.PermUsersManage, rbac.PermSettingsManage, rbac.PermCategoriesManage, rbac.PermAssetsRead))
	var out map[string]any
	idOf := func(key string) string {
		t.Helper()
		return out[key].(map[string]any)["id"].(string)
	}

	// People and roles.
	structured(t, callTool(t, cs, "save_role", map[string]any{"name": "Members", "permissions": []string{"assets.read", "assets.borrow"}}), &out)
	members := idOf("role")
	structured(t, callTool(t, cs, "save_role", map[string]any{"id": members, "description": "Everyone who pays"}), &out)
	if r := out["role"].(map[string]any); r["name"] != "Members" || r["description"] != "Everyone who pays" || len(r["permissions"].([]any)) != 2 {
		t.Errorf("changed role: %v", r)
	}
	structured(t, callTool(t, cs, "create_user", map[string]any{"email": "grace@hackspace.org", "name": "Grace", "password": "first-password"}), &out)
	grace := idOf("user")
	for _, c := range []struct {
		tool string
		args map[string]any
	}{
		{"set_user_roles", map[string]any{"id": grace, "role_ids": []string{members}}},
		{"set_user_name", map[string]any{"id": grace, "name": "Grace Hopper"}},
		{"set_user_password", map[string]any{"id": grace, "password": "another-password"}},
		{"set_user_active", map[string]any{"id": grace, "is_active": false}},
		{"set_user_active", map[string]any{"id": grace, "is_active": true}},
		{"list_users", nil},
		{"list_roles", nil},
	} {
		if res := callTool(t, cs, c.tool, c.args); res.IsError {
			t.Errorf("%s: %s", c.tool, toolText(res))
		}
	}
	if u, _ := srv.store.GetUserByID(grace); u.Name != "Grace Hopper" {
		t.Errorf("renamed: %+v", u)
	}
	// The anti-lockout rule holds over MCP as anywhere.
	if msg := toolError(t, cs, "archive_user", map[string]any{"id": admin.ID}); !strings.Contains(msg, "anti-lockout") {
		t.Errorf("archiving the last admin: %s", msg)
	}
	if res := callTool(t, cs, "archive_user", map[string]any{"id": grace}); res.IsError {
		t.Errorf("archive_user: %s", toolText(res))
	}
	if res := callTool(t, cs, "delete_role", map[string]any{"id": members}); res.IsError {
		t.Errorf("delete_role: %s", toolText(res))
	}

	// Scopes on a category.
	structured(t, callTool(t, cs, "save_scope", map[string]any{"name": "woodshop"}), &out)
	scope := idOf("scope")
	structured(t, callTool(t, cs, "save_scope", map[string]any{"id": scope, "name": "wood-shop"}), &out)
	cat := mustID(t, doJSON(t, srv, adminCookie, http.MethodPost, "/api/v1/categories", `{"name":"Saws"}`).Body.Bytes(), "category")
	if res := callTool(t, cs, "set_category_scopes", map[string]any{"category_id": cat, "scope_ids": []string{scope}}); res.IsError {
		t.Errorf("set_category_scopes: %s", toolText(res))
	}
	if res := callTool(t, cs, "list_scopes", nil); !strings.Contains(toolText(res), "scopes") {
		t.Errorf("list_scopes: %s", toolText(res))
	}
	if msg := toolError(t, cs, "delete_scope", map[string]any{"id": scope}); !strings.Contains(msg, "409") {
		t.Errorf("deleting a scope in use: %s", msg)
	}

	// Channels: secrets are kept, and never read back.
	webhook := "https://discord.com/api/webhooks/1/secret-part"
	structured(t, callTool(t, cs, "save_channel", map[string]any{"name": "Stores", "driver": "discord", "options": map[string]string{"webhook_url": webhook}, "events": []string{"category.running_low"}}), &out)
	channel := idOf("channel")
	structured(t, callTool(t, cs, "save_channel", map[string]any{"id": channel, "name": "Stores desk"}), &out)
	res := callTool(t, cs, "get_channel", map[string]any{"id": channel})
	if strings.Contains(toolText(res), "secret-part") {
		t.Errorf("a secret was read back: %s", toolText(res))
	}
	structured(t, res, &out)
	if c := out["channel"].(map[string]any); c["name"] != "Stores desk" || c["secrets_set"].(map[string]any)["webhook_url"] != true {
		t.Errorf("changed channel: %v", c)
	}
	for _, name := range []string{"list_channels", "list_channel_drivers", "list_deliveries"} {
		if res := callTool(t, cs, name, nil); res.IsError {
			t.Errorf("%s: %s", name, toolText(res))
		}
	}
	if res := callTool(t, cs, "delete_channel", map[string]any{"id": channel}); res.IsError {
		t.Errorf("delete_channel: %s", toolText(res))
	}

	names := toolNames(t, mcpSession(t, srv, tokenWith(t, srv, admin.ID, "Curator", rbac.PermCategoriesManage)))
	if !slices.Contains(names, "list_scopes") || slices.Contains(names, "save_scope") || slices.Contains(names, "list_users") {
		t.Errorf("a curator lists %v", names)
	}
}
