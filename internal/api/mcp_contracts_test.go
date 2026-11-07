package api

import (
	"encoding/base64"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// TestContractsOverMCP: an assistant makes, finds, changes and deletes a
// contract, reads its history and attaches a file to it; without
// vendors.manage, it can only read them.
func TestContractsOverMCP(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	acme, err := srv.store.CreateVendor(store.VendorInput{Name: "Acme"})
	if err != nil {
		t.Fatal(err)
	}
	drill, err := srv.store.CreateAsset("Drill", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	secret := tokenWith(t, srv, admin.ID, "assistant", rbac.PermAssetsRead, rbac.PermVendorsManage, rbac.PermPurchasesRead)
	cs := mcpSession(t, srv, secret)

	var made struct {
		Contract struct {
			ID, Name, Kind, Cost, Currency, EndDate string
			AssetIDs                                []string `json:"asset_ids"`
			Notes                                   string
		}
	}
	structured(t, callTool(t, cs, "save_contract", map[string]any{"vendor_id": acme.ID, "kind": "warranty", "name": "Drill warranty",
		"end_date": "2028-01-31", "cost": "49.99", "currency": "GBP", "notes": "Extended", "asset_ids": []string{drill.Asset.ID}}), &made)
	if made.Contract.Name != "Drill warranty" || made.Contract.Cost != "49.99" || len(made.Contract.AssetIDs) != 1 {
		t.Fatalf("made: %+v", made)
	}
	id := made.Contract.ID
	if res := callTool(t, cs, "save_contract", map[string]any{"name": "Nameless vendor"}); !res.IsError {
		t.Errorf("a contract with no vendor was made: %s", toolText(res))
	}
	// Only what is given changes.
	structured(t, callTool(t, cs, "save_contract", map[string]any{"id": id, "name": "Drill cover"}), &made)
	if made.Contract.Name != "Drill cover" || made.Contract.Notes != "Extended" || made.Contract.Cost != "49.99" || len(made.Contract.AssetIDs) != 1 {
		t.Errorf("saved: %+v", made)
	}

	var list struct {
		Contracts []struct{ ID, Name string }
	}
	structured(t, callTool(t, cs, "list_contracts", map[string]any{"asset": drill.Asset.ID}), &list)
	if len(list.Contracts) != 1 || list.Contracts[0].ID != id {
		t.Errorf("found: %+v", list)
	}
	structured(t, callTool(t, cs, "list_contracts", map[string]any{"query": "nobody"}), &list)
	if len(list.Contracts) != 0 {
		t.Errorf("found what is not there: %+v", list)
	}
	var got struct {
		Contract  struct{ Name string }
		CanManage bool `json:"can_manage"`
	}
	structured(t, callTool(t, cs, "get_contract", map[string]any{"id": id}), &got)
	if got.Contract.Name != "Drill cover" || !got.CanManage {
		t.Errorf("got: %+v", got)
	}
	var history struct {
		Events []struct{ Kind string }
	}
	structured(t, callTool(t, cs, "contract_history", map[string]any{"id": id}), &history)
	if len(history.Events) != 2 || history.Events[0].Kind != "contract.edited" {
		t.Errorf("history: %+v", history)
	}
	var attached struct {
		Attachment struct{ Name, Kind string }
	}
	structured(t, callTool(t, cs, "add_attachment", map[string]any{"contract_id": id, "filename": "terms.txt",
		"data": base64.StdEncoding.EncodeToString([]byte("Terms and conditions"))}), &attached)
	if attached.Attachment.Kind != "document" {
		t.Errorf("attached: %+v", attached)
	}
	var files struct {
		Attachments []struct{ Name string }
	}
	structured(t, callTool(t, cs, "list_attachments", map[string]any{"contract_id": id}), &files)
	if len(files.Attachments) != 1 {
		t.Errorf("its files: %+v", files)
	}

	// An assistant that reads only is not offered the tools that change
	// contracts, and reads them.
	reading := mcpSession(t, srv, tokenWith(t, srv, admin.ID, "reader", rbac.PermAssetsRead))
	structured(t, callTool(t, reading, "list_contracts", map[string]any{}), &list)
	if len(list.Contracts) != 1 {
		t.Errorf("a reader lists: %+v", list)
	}
	if res := callTool(t, cs, "delete_contract", map[string]any{"id": id}); res.IsError {
		t.Errorf("deleting: %s", toolText(res))
	}
	structured(t, callTool(t, cs, "list_contracts", map[string]any{}), &list)
	if len(list.Contracts) != 0 {
		t.Errorf("after deleting: %+v", list)
	}
}
