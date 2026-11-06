package api

import (
	"encoding/base64"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
)

// TestVendorsOverMCP: an assistant makes, finds, changes and deletes a
// vendor, reads its history, attaches a file to it, and names it for a
// category; without vendors.manage, it can only read them.
func TestVendorsOverMCP(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	secret := tokenWith(t, srv, admin.ID, "assistant", rbac.PermAssetsRead, rbac.PermVendorsManage, rbac.PermCategoriesManage)
	cs := mcpSession(t, srv, secret)

	var made struct {
		Vendor struct {
			ID       string
			Name     string
			Notes    string
			Contacts []struct{ ID, Name, Role, Email string }
		}
	}
	structured(t, callTool(t, cs, "save_vendor", map[string]any{"name": "Acme", "website": "https://acme.example", "notes": "Net 30",
		"contacts": []map[string]string{{"name": "Wile", "role": "sales", "email": "wile@acme.example"}}}), &made)
	if made.Vendor.Name != "Acme" || len(made.Vendor.Contacts) != 1 {
		t.Fatalf("made: %+v", made)
	}
	id := made.Vendor.ID
	if res := callTool(t, cs, "save_vendor", map[string]any{"website": "https://nameless.example"}); !res.IsError {
		t.Errorf("a vendor with no name was made: %s", toolText(res))
	}

	// Only what is given changes.
	structured(t, callTool(t, cs, "save_vendor", map[string]any{"id": id, "name": "Acme Ltd"}), &made)
	if made.Vendor.Name != "Acme Ltd" || made.Vendor.Notes != "Net 30" || len(made.Vendor.Contacts) != 1 {
		t.Errorf("saved: %+v", made)
	}

	var list struct {
		Vendors []struct{ ID, Name string }
	}
	structured(t, callTool(t, cs, "list_vendors", map[string]any{"query": "wile"}), &list)
	if len(list.Vendors) != 1 || list.Vendors[0].ID != id {
		t.Errorf("found: %+v", list)
	}
	structured(t, callTool(t, cs, "list_vendors", map[string]any{"query": "nobody"}), &list)
	if len(list.Vendors) != 0 {
		t.Errorf("found what is not there: %+v", list)
	}
	var got struct {
		Vendor    struct{ Name string }
		CanManage bool `json:"can_manage"`
	}
	structured(t, callTool(t, cs, "get_vendor", map[string]any{"id": id}), &got)
	if got.Vendor.Name != "Acme Ltd" || !got.CanManage {
		t.Errorf("got: %+v", got)
	}

	var added struct {
		Attachment struct{ ID, Kind string }
	}
	structured(t, callTool(t, cs, "add_attachment", map[string]any{"vendor_id": id, "data": base64.StdEncoding.EncodeToString([]byte("part,price\n")), "filename": "prices.csv"}), &added)
	if added.Attachment.Kind != "document" {
		t.Errorf("attached: %+v", added)
	}
	var files struct {
		Attachments []struct{ ID string }
	}
	structured(t, callTool(t, cs, "list_attachments", map[string]any{"vendor_id": id}), &files)
	if len(files.Attachments) != 1 {
		t.Errorf("its files: %+v", files)
	}
	if res := callTool(t, cs, "list_attachments", map[string]any{"vendor_id": id, "category_id": "x"}); !res.IsError {
		t.Errorf("two owners were taken: %s", toolText(res))
	}

	var history struct {
		Events []struct{ Kind string }
	}
	structured(t, callTool(t, cs, "vendor_history", map[string]any{"id": id}), &history)
	if len(history.Events) != 3 || history.Events[len(history.Events)-1].Kind != "vendor.created" {
		t.Errorf("history: %+v", history)
	}

	cat, _ := srv.store.CreateCategory("Screws", nil, "#123456", true, false)
	if res := callTool(t, cs, "set_category_vendors", map[string]any{"category_id": cat.ID, "vendor_ids": []string{id}}); res.IsError {
		t.Errorf("naming the category's vendors: %s", toolText(res))
	}
	if ids, _ := srv.store.CategoryVendorIDs(cat.ID); len(ids) != 1 || ids[0] != id {
		t.Errorf("the category's vendors: %+v", ids)
	}

	// An assistant that reads only is not offered the tools that change
	// vendors, and reads them.
	reading := mcpSession(t, srv, tokenWith(t, srv, admin.ID, "reader", rbac.PermAssetsRead))
	structured(t, callTool(t, reading, "list_vendors", map[string]any{}), &list)
	if len(list.Vendors) != 1 {
		t.Errorf("a reader's list: %+v", list)
	}

	if res := callTool(t, cs, "delete_vendor", map[string]any{"id": id}); res.IsError {
		t.Errorf("deleting: %s", toolText(res))
	}
	if _, err := srv.store.GetVendor(id); err == nil {
		t.Error("not deleted")
	}
}
