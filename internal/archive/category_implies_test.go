package archive

import (
	"context"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/filestore"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// linkedCategories makes a drawer and a pool, the drawer implying the pool,
// and a category outside both that implies the pool too.
func linkedCategories(t *testing.T, st *store.Store) (drawer, pool, outside *models.Category) {
	t.Helper()
	var err error
	if drawer, err = st.CreateCategoryFrom(store.CategoryInput{Name: "Workshop drawer"}); err != nil {
		t.Fatal(err)
	}
	if pool, err = st.CreateCategoryFrom(store.CategoryInput{Name: "M5 bolts", IsConsumable: true, Unit: "pcs"}); err != nil {
		t.Fatal(err)
	}
	if outside, err = st.CreateCategoryFrom(store.CategoryInput{Name: "Laser bay"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SetCategoryLinks(pool.ID, store.CategoryLinks{IncludedBy: &[]string{drawer.ID, outside.ID}}); err != nil {
		t.Fatal(err)
	}
	return drawer, pool, outside
}

// TestBackupsCarryCategoryLinks: a backup holds the links between
// categories, which a restore puts back.
func TestBackupsCarryCategoryLinks(t *testing.T) {
	ctx := context.Background()
	src := setupStore(t)
	blobs := filestore.NewLocal(t.TempDir())
	seed(t, src, blobs)
	_, pool, _ := linkedCategories(t, src)

	b := backup(t, src, blobs, Options{})
	dst := emptyStore(t, envDialect())
	if _, err := Restore(ctx, dst, filestore.NewLocal(t.TempDir()), opener(b), RestoreOptions{}); err != nil {
		t.Fatal(err)
	}
	got, err := dst.GetCategoryByID(pool.ID)
	if err != nil || len(got.IncludedBy) != 2 {
		t.Errorf("restored: %+v %v", got, err)
	}
}

// TestPackagesCarryCategoryLinks: a package holds the links between the
// categories it holds, and not those to categories it leaves out;
// imported, they link the new categories.
func TestPackagesCarryCategoryLinks(t *testing.T) {
	ctx := context.Background()
	src := setupStore(t)
	blobs := filestore.NewLocal(t.TempDir())
	seed(t, src, blobs)
	drawer, pool, _ := linkedCategories(t, src)
	sel := Selection{Categories: []string{drawer.ID, pool.ID}}

	r := packageOf(t, src, blobs, sel, nil)
	if rows := r.rows(t, DataDir+"category_implies/"); len(rows) != 1 || rows[0]["category_id"] != drawer.ID || rows[0]["implied_id"] != pool.ID {
		t.Errorf("links packaged: %v", rows)
	}

	pkg := exportPackage(t, src, blobs, sel)
	into := migratedStore(t, envDialect())
	if _, err := Import(ctx, into, filestore.NewLocal(t.TempDir()), opener(pkg), ImportOptions{}); err != nil {
		t.Fatal(err)
	}
	cats, err := into.ListCategories()
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]models.Category{}
	for _, c := range cats {
		byName[c.Name] = c
	}
	d, p := byName["Workshop drawer"], byName["M5 bolts"]
	if d.ID == "" || d.ID == drawer.ID || len(d.Implies) != 1 || d.Implies[0] != p.ID || len(p.IncludedBy) != 1 {
		t.Errorf("imported: drawer %+v, pool %+v", d, p)
	}
}
