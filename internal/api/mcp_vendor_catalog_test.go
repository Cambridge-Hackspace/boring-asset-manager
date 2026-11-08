package api

import (
	"strings"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
)

// TestVendorCatalogOverMCP: an assistant records what a vendor sells,
// changes only what it gives, finds the preferred entry, exports and
// imports the vendor's catalog, and deletes an entry.
func TestVendorCatalogOverMCP(t *testing.T) {
	sc := newCatalogScene(t)
	srv := sc.srv
	secret := tokenWith(t, srv, sc.adminID, "assistant", rbac.PermAssetsRead, rbac.PermVendorsManage, rbac.PermPurchasesRead)
	cs := mcpSession(t, srv, secret)

	var made struct {
		Entry struct {
			ID          string
			PartNumber  string `json:"part_number"`
			Description string
			Price       string
			Preferred   bool
		}
	}
	structured(t, callTool(t, cs, "save_catalog_entry", map[string]any{"vendor_id": sc.acme.ID, "category_id": sc.screws.ID, "part_number": "SC-100",
		"description": "M3 screws", "price": "4.50", "currency": "GBP", "preferred": true}), &made)
	if made.Entry.PartNumber != "SC-100" || made.Entry.Price != "4.50" || !made.Entry.Preferred {
		t.Fatalf("made: %+v", made)
	}
	id := made.Entry.ID
	structured(t, callTool(t, cs, "save_catalog_entry", map[string]any{"id": id, "price": "5"}), &made)
	if made.Entry.Price != "5.00" || made.Entry.Description != "M3 screws" || !made.Entry.Preferred {
		t.Errorf("saved: %+v", made)
	}
	if res := callTool(t, cs, "save_catalog_entry", map[string]any{"vendor_id": sc.acme.ID, "part_number": "X"}); !res.IsError {
		t.Errorf("an entry for nothing was made: %s", toolText(res))
	}

	var list struct {
		Entries []struct{ ID string }
	}
	structured(t, callTool(t, cs, "list_catalog", map[string]any{"preferred": true}), &list)
	if len(list.Entries) != 1 || list.Entries[0].ID != id {
		t.Errorf("preferred: %+v", list)
	}
	var got struct{ Entry struct{ ID string } }
	structured(t, callTool(t, cs, "get_catalog_entry", map[string]any{"id": id}), &got)
	if got.Entry.ID != id {
		t.Errorf("got: %+v", got)
	}

	res := callTool(t, cs, "export_vendor_catalog", map[string]any{"id": sc.acme.ID})
	if res.IsError || !strings.Contains(toolText(res), "1 catalog entries") {
		t.Fatalf("exported: %s", toolText(res))
	}
	file := "category,part_number,description\nScrews,SC-200,M4 screws\n"
	if res := callTool(t, cs, "import_vendor_catalog", map[string]any{"vendor_id": sc.acme.ID, "csv": file}); res.IsError || !strings.Contains(toolText(res), "make 1 entries") {
		t.Fatalf("checked: %s", toolText(res))
	}
	structured(t, callTool(t, cs, "list_catalog", map[string]any{"vendor_id": sc.acme.ID}), &list)
	if len(list.Entries) != 1 {
		t.Errorf("checking imported: %+v", list)
	}
	if res := callTool(t, cs, "import_vendor_catalog", map[string]any{"vendor_id": sc.acme.ID, "csv": file, "confirm": true}); res.IsError {
		t.Fatalf("imported: %s", toolText(res))
	}
	structured(t, callTool(t, cs, "list_catalog", map[string]any{"vendor_id": sc.acme.ID}), &list)
	if len(list.Entries) != 2 {
		t.Errorf("imported: %+v", list)
	}

	if res := callTool(t, cs, "delete_catalog_entry", map[string]any{"id": id}); res.IsError {
		t.Fatalf("deleted: %s", toolText(res))
	}
	structured(t, callTool(t, cs, "list_catalog", map[string]any{}), &list)
	if len(list.Entries) != 1 {
		t.Errorf("after deleting: %+v", list)
	}
}
