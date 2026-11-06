package archive

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/filestore"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// commentFixture is a category with a comment and a deleted one, and an
// asset in it with a comment, as Ada wrote them.
type commentFixture struct {
	cat              *models.Category
	asset            string
	onCat, onAsset   *models.Comment
	deletedOnCatBody string
}

func seedComments(t *testing.T, st *store.Store, by string) commentFixture {
	t.Helper()
	cat, err := st.CreateCategoryFrom(store.CategoryInput{Name: "Lasers"})
	if err != nil {
		t.Fatal(err)
	}
	a, err := st.CreateAsset("Cutter", []string{cat.ID}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	as := st.As(by)
	f := commentFixture{cat: cat, asset: a.Asset.ID, deletedOnCatBody: "a mistake"}
	if f.onCat, err = as.AddComment(store.CommentTarget{On: models.CommentOnCategory, ID: cat.ID}, "Check the **lens** weekly"); err != nil {
		t.Fatal(err)
	}
	gone, err := as.AddComment(store.CommentTarget{On: models.CommentOnCategory, ID: cat.ID}, f.deletedOnCatBody)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := as.DeleteComment(gone.ID); err != nil {
		t.Fatal(err)
	}
	if f.onAsset, err = as.AddComment(store.CommentTarget{On: models.CommentOnAsset, ID: a.Asset.ID}, "Mirror cleaned"); err != nil {
		t.Fatal(err)
	}
	return f
}

// TestBackupsCarryComments: comments go with what they are on, and come
// back as they were, deleted ones and all.
func TestBackupsCarryComments(t *testing.T) {
	src := setupStore(t)
	blobs := filestore.NewLocal(t.TempDir())
	s := seed(t, src, blobs)
	f := seedComments(t, src, s.user.ID)

	b := backup(t, src, blobs, Options{})
	r := readArchive(t, b)
	for dir, want := range map[string]int{"category_comments": 2, "asset_comments": 1, "loan_comments": 0, "flow_filing_comments": 0} {
		if got := len(r.rows(t, DataDir+dir+"/")); got != want {
			t.Errorf("%s: %d rows, want %d", dir, got, want)
		}
	}
	dst := emptyStore(t, envDialect())
	if _, err := Restore(context.Background(), dst, filestore.NewLocal(t.TempDir()), opener(b), RestoreOptions{}); err != nil {
		t.Fatal(err)
	}
	list, err := dst.ListComments(store.CommentTarget{On: models.CommentOnAsset, ID: f.asset}, true)
	if err != nil || len(list) != 3 || list[0].Body != "Mirror cleaned" || list[0].AuthorID != s.user.ID || list[1].Body != f.onCat.Body || list[2].DeletedAt == nil {
		t.Errorf("restored: %+v %v", list, err)
	}
	if events, err := dst.CommentHistory(f.onAsset.ID); err != nil || len(events) != 1 {
		t.Errorf("its history, restored: %+v %v", events, err)
	}
}

// TestPackagesCarryCategoryComments: a package brings its categories'
// comments when asked, without who wrote them or deleted ones.
func TestPackagesCarryCategoryComments(t *testing.T) {
	ctx := context.Background()
	src := setupStore(t)
	blobs := filestore.NewLocal(t.TempDir())
	s := seed(t, src, blobs)
	f := seedComments(t, src, s.user.ID)

	r := packageOf(t, src, blobs, Selection{Categories: []string{f.cat.ID}}, nil)
	if got := r.rows(t, DataDir+"category_comments/"); len(got) != 0 {
		t.Errorf("comments packaged without being asked for: %v", got)
	}
	sel := Selection{Categories: []string{f.cat.ID}, Comments: true}
	r = packageOf(t, src, blobs, sel, nil)
	rows := r.rows(t, DataDir+"category_comments/")
	if len(rows) != 1 || rows[0]["author_id"] != nil || !strings.Contains(rows[0]["body"].(string), "lens") {
		t.Errorf("comments packaged: %v", rows)
	}

	into := migratedStore(t, envDialect())
	if _, err := Import(ctx, into, filestore.NewLocal(t.TempDir()), opener(exportPackage(t, src, blobs, sel)), ImportOptions{}); err != nil {
		t.Fatal(err)
	}
	var catID string
	if err := into.QueryRow("SELECT id FROM categories WHERE name = 'Lasers'").Scan(&catID); err != nil {
		t.Fatal(err)
	}
	list, err := into.ListComments(store.CommentTarget{On: models.CommentOnCategory, ID: catID}, false)
	if err != nil || len(list) != 1 || list[0].Body != f.onCat.Body || list[0].AuthorID != "" || list[0].ID == f.onCat.ID {
		t.Errorf("imported: %+v %v", list, err)
	}
}

// TestArchivesFromBeforeComments: a backup or a package written before
// comments (migration 069) still checks, restores and imports.
func TestArchivesFromBeforeComments(t *testing.T) {
	ctx := context.Background()
	src := setupStore(t)
	blobs := filestore.NewLocal(t.TempDir())
	s := seed(t, src, blobs)
	f := seedComments(t, src, s.user.ID)

	old := olderArchive(t, backup(t, src, blobs, Options{}), 68)
	if _, err := Check(bytes.NewReader(old)); err != nil {
		t.Fatalf("an archive from before comments: %v", err)
	}
	dst := emptyStore(t, envDialect())
	sum, err := Restore(ctx, dst, filestore.NewLocal(t.TempDir()), opener(old), RestoreOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !sum.Migrated {
		t.Errorf("not migrated: %+v", sum)
	}
	if n := count(t, dst, "category_comments"); n != 0 {
		t.Errorf("%d comments", n)
	}
	if _, err := dst.GetCategoryByID(f.cat.ID); err != nil {
		t.Errorf("the category: %v", err)
	}

	pkg := olderArchive(t, exportPackage(t, src, blobs, Selection{Categories: []string{f.cat.ID}, Comments: true}), 68)
	into := migratedStore(t, envDialect())
	if _, err := Import(ctx, into, filestore.NewLocal(t.TempDir()), opener(pkg), ImportOptions{}); err != nil {
		t.Fatal(err)
	}
	if n := countWhere(t, into, "SELECT COUNT(*) FROM categories WHERE name = 'Lasers'"); n != 1 {
		t.Errorf("%d categories imported", n)
	}
}
