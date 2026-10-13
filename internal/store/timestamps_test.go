package store

import (
	"testing"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/fields"
)

// TestUpdatesPreserveCreatedAt guards against MariaDB's implicit
// ON UPDATE CURRENT_TIMESTAMP on the first TIMESTAMP NOT NULL column of a table,
// which would silently rewrite created_at whenever a row is updated. Run it with
// BAM_TEST_DIALECT=mariadb to exercise that dialect.
func TestUpdatesPreserveCreatedAt(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()

	f, err := st.CreateField("location", "Location", fields.TypeText, nil)
	if err != nil {
		t.Fatalf("creating field: %v", err)
	}
	cat, err := st.CreateCategory("Shelving", nil, "", false, false)
	if err != nil {
		t.Fatalf("creating category: %v", err)
	}
	asset, err := st.CreateAsset("Rack", []string{cat.ID}, nil, nil)
	if err != nil {
		t.Fatalf("creating asset: %v", err)
	}

	// Compare against the persisted values (not the in-memory ones) so column
	// precision differences between dialects do not matter.
	fieldBefore, err := st.GetFieldByID(f.ID)
	if err != nil {
		t.Fatalf("reloading field: %v", err)
	}
	catBefore, err := st.GetCategoryByID(cat.ID)
	if err != nil {
		t.Fatalf("reloading category: %v", err)
	}
	assetBefore, err := st.GetAssetByID(asset.Asset.ID)
	if err != nil {
		t.Fatalf("reloading asset: %v", err)
	}

	// Let the wall clock move past whole-second precision before updating.
	time.Sleep(1100 * time.Millisecond)

	if _, err := st.UpdateField(f.ID, "Storage Location", fields.TypeText, nil); err != nil {
		t.Fatalf("updating field: %v", err)
	}
	if _, err := st.UpdateCategory(cat.ID, "Shelves", nil, "", false, false); err != nil {
		t.Fatalf("updating category: %v", err)
	}
	if _, err := st.UpdateAsset(asset.Asset.ID, "Big Rack", []string{cat.ID}, nil, nil); err != nil {
		t.Fatalf("updating asset: %v", err)
	}

	reloadedField, err := st.GetFieldByID(f.ID)
	if err != nil {
		t.Fatalf("reloading field: %v", err)
	}
	reloadedCat, err := st.GetCategoryByID(cat.ID)
	if err != nil {
		t.Fatalf("reloading category: %v", err)
	}
	reloadedAsset, err := st.GetAssetByID(asset.Asset.ID)
	if err != nil {
		t.Fatalf("reloading asset: %v", err)
	}

	checks := []struct {
		name      string
		original  time.Time
		persisted time.Time
	}{
		{"field", fieldBefore.CreatedAt, reloadedField.CreatedAt},
		{"category", catBefore.CreatedAt, reloadedCat.CreatedAt},
		{"asset", assetBefore.Asset.CreatedAt, reloadedAsset.Asset.CreatedAt},
	}
	for _, c := range checks {
		if !c.persisted.Equal(c.original) {
			t.Errorf("%s created_at changed on update: was %s, now %s", c.name, c.original, c.persisted)
		}
	}
}
