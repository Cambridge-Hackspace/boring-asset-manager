package api

import (
	"strings"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
)

// TestCategoryLinksOverMCP: an assistant links categories into a pool from
// either side, is refused a cycle and warned of a link adding nothing,
// sees where else a bin's stock is, and moves stock between bins.
func TestCategoryLinksOverMCP(t *testing.T) {
	f := newPoolFixture(t)
	srv := f.srv
	secret := tokenWith(t, srv, f.adminUser.ID, "assistant", rbac.PermAssetsRead, rbac.PermCategoriesManage, rbac.PermStockRecord)
	cs := mcpSession(t, srv, secret)

	if res := callTool(t, cs, "set_category_links", map[string]any{"category_id": f.drawer.ID, "implies": []string{f.bolts.ID}}); res.IsError {
		t.Fatalf("linking from the drawer: %s", toolText(res))
	}
	if res := callTool(t, cs, "set_category_links", map[string]any{"category_id": f.bolts.ID, "included_by": []string{f.drawer.ID, f.laser.ID}}); res.IsError {
		t.Fatalf("linking from the pool: %s", toolText(res))
	}
	var got struct{ Category models.Category }
	structured(t, callTool(t, cs, "get_category", map[string]any{"id": f.bolts.ID}), &got)
	if len(got.Category.IncludedBy) != 2 {
		t.Errorf("the pool is included by %v", got.Category.IncludedBy)
	}
	if res := callTool(t, cs, "set_category_links", map[string]any{"category_id": f.bolts.ID, "implies": []string{f.laser.ID}}); !res.IsError {
		t.Errorf("a cycle was made: %s", toolText(res))
	}

	var detail struct {
		CountsIn []models.CountsIn  `json:"counts_in"`
		Pools    []models.AssetPool `json:"pools"`
	}
	structured(t, callTool(t, cs, "get_asset", map[string]any{"asset": f.drawerBin}), &detail)
	if len(detail.CountsIn) != 1 || detail.CountsIn[0].CategoryName != "M5 bolts" || detail.CountsIn[0].ThroughName != "Workshop drawer" {
		t.Errorf("counts in %+v", detail.CountsIn)
	}
	if len(detail.Pools) != 1 || detail.Pools[0].Total != 440 || len(detail.Pools[0].Places) != 2 {
		t.Errorf("pools %+v", detail.Pools)
	}

	res := callTool(t, cs, "move_stock", map[string]any{"from": f.laserBin, "to": f.drawerBin, "amount": 60})
	if res.IsError || !strings.Contains(toolText(res), "Moved 60") {
		t.Fatalf("moving: %s", toolText(res))
	}
	if res := callTool(t, cs, "move_stock", map[string]any{"from": f.laserBin, "to": f.drawerBin, "amount": 1000}); !res.IsError {
		t.Errorf("moved more than there is: %s", toolText(res))
	}

	tape, err := srv.store.CreateCategory("Tape", nil, "", false, false)
	if err != nil {
		t.Fatal(err)
	}
	res = callTool(t, cs, "set_category_links", map[string]any{"category_id": tape.ID, "implies": []string{f.bolts.ID}})
	if res.IsError || !strings.Contains(toolText(res), "add nothing to M5 bolts' total") {
		t.Errorf("an empty link: %s", toolText(res))
	}
}
