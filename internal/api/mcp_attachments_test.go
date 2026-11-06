package api

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestAttachmentsOverMCP: an assistant lists, adds, reads, renames, orders
// and removes the files on assets and categories, and copies them with an
// asset.
func TestAttachmentsOverMCP(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	secret := tokenWith(t, srv, admin.ID, "assistant", rbac.PermAssetsRead, rbac.PermAssetsWrite, rbac.PermCategoriesManage)
	cs := mcpSession(t, srv, secret)
	a, _ := srv.store.CreateAsset("Drill", nil, nil, nil)
	cat, _ := srv.store.CreateCategory("Drills", nil, "#123456", false, false)
	b64 := func(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

	var added struct {
		Attachment struct{ ID, Kind, Name string }
	}
	structured(t, callTool(t, cs, "add_attachment", map[string]any{"asset": a.Asset.ID, "data": b64("Oil it monthly."), "filename": "care.txt", "name": "Care"}), &added)
	if added.Attachment.Kind != "document" || added.Attachment.Name != "Care" {
		t.Errorf("added: %+v", added)
	}
	care := added.Attachment.ID
	structured(t, callTool(t, cs, "add_attachment", map[string]any{"asset": a.Asset.ID, "data": base64.StdEncoding.EncodeToString(testPDF), "filename": "manual.pdf"}), &added)
	manual := added.Attachment.ID
	if res := callTool(t, cs, "add_attachment", map[string]any{"asset": a.Asset.ID, "data": b64("<html></html>"), "filename": "page.html"}); !res.IsError {
		t.Errorf("an HTML page was attached: %s", toolText(res))
	}
	if res := callTool(t, cs, "add_attachment", map[string]any{"data": b64("x"), "filename": "x.txt"}); !res.IsError {
		t.Errorf("attached to nothing: %s", toolText(res))
	}

	var list struct {
		Attachments []struct{ ID, Kind, Name, Description string }
	}
	structured(t, callTool(t, cs, "list_attachments", map[string]any{"asset": a.Asset.ID}), &list)
	if len(list.Attachments) != 2 || list.Attachments[0].ID != care {
		t.Errorf("listed: %+v", list)
	}
	res := callTool(t, cs, "read_attachment", map[string]any{"asset": a.Asset.ID, "attachment_id": care})
	text := ""
	for _, c := range res.Content {
		if r, ok := c.(*mcp.EmbeddedResource); ok {
			text = r.Resource.Text
		}
	}
	if res.IsError || text != "Oil it monthly." {
		t.Errorf("read: %s %q", toolText(res), text)
	}
	structured(t, callTool(t, cs, "update_attachment", map[string]any{"asset": a.Asset.ID, "attachment_id": care, "description": "Weekly in summer"}), &added)
	structured(t, callTool(t, cs, "reorder_attachments", map[string]any{"asset": a.Asset.ID, "attachment_ids": []string{manual, care}}), &list)
	if list.Attachments[0].ID != manual || list.Attachments[1].Description != "Weekly in summer" {
		t.Errorf("reordered: %+v", list)
	}

	// A copy of the asset, with its attachments.
	var copied struct {
		Attachments int
		Asset       struct{ ID string }
	}
	structured(t, callTool(t, cs, "copy_asset", map[string]any{"asset": a.Asset.ID, "name": "Drill 2", "attachments": true}), &copied)
	if copied.Attachments != 2 {
		t.Errorf("copied: %+v", copied)
	}

	// A category's.
	structured(t, callTool(t, cs, "add_attachment", map[string]any{"category_id": cat.ID, "data": b64("# Guide\n"), "filename": "guide.md"}), &added)
	structured(t, callTool(t, cs, "list_attachments", map[string]any{"category_id": cat.ID}), &list)
	if len(list.Attachments) != 1 || list.Attachments[0].Name != "guide.md" {
		t.Errorf("the category's: %+v", list)
	}
	structured(t, callTool(t, cs, "remove_attachment", map[string]any{"category_id": cat.ID, "attachment_id": added.Attachment.ID}), &list)
	if len(list.Attachments) != 0 {
		t.Errorf("after removing: %+v", list)
	}

	// A reader lists, but is not offered the tools that change them.
	reader := mcpSession(t, srv, tokenWith(t, srv, admin.ID, "reader", rbac.PermAssetsRead))
	tools, err := reader.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tl := range tools.Tools {
		names = append(names, tl.Name)
	}
	all := " " + strings.Join(names, " ") + " "
	if !strings.Contains(all, " list_attachments ") || !strings.Contains(all, " read_attachment ") || strings.Contains(all, " add_attachment ") || strings.Contains(all, " remove_attachment ") {
		t.Errorf("a reader's tools: %v", names)
	}
}
