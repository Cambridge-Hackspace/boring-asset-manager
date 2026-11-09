package archive

import (
	"context"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/filestore"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// TestBackupsAndPackagesCarryReview: a backup holds assets' review states,
// categories' review settings and changes waiting from assistants; a
// package brings the settings, but its assets are drafts, as who checked
// them is the importing instance's to say.
func TestBackupsAndPackagesCarryReview(t *testing.T) {
	ctx := context.Background()
	src := setupStore(t)
	blobs := filestore.NewLocal(t.TempDir())
	seed(t, src, blobs)
	cat, err := src.CreateCategoryFrom(store.CategoryInput{Name: "Tools"})
	if err != nil {
		t.Fatal(err)
	}
	on, months := true, 12
	if _, err := src.SetCategoryReview(cat.ID, models.CategoryReview{Enabled: &on, Months: &months}); err != nil {
		t.Fatal(err)
	}
	saw, err := src.Confirming().CreateAsset("Saw", []string{cat.ID}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	router, err := src.Via("Claude Desktop").CreateAsset("Router", []string{cat.ID}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	b := backup(t, src, blobs, Options{})
	dst := emptyStore(t, envDialect())
	if _, err := Restore(ctx, dst, filestore.NewLocal(t.TempDir()), opener(b), RestoreOptions{}); err != nil {
		t.Fatal(err)
	}
	got, err := dst.GetAssetByID(saw.Asset.ID)
	if err != nil || got.Review == nil || got.Review.State != models.ReviewConfirmed || got.Review.DueAt == nil || !got.Review.DueAt.Equal(*saw.Review.DueAt) {
		t.Errorf("restored saw: %+v %v", got, err)
	}
	if got, err := dst.GetAssetByID(router.Asset.ID); err != nil || got.Review == nil || got.Review.Proposal == nil || !got.Review.Proposal.New {
		t.Errorf("restored router: %+v %v", got, err)
	}

	pkg := exportPackage(t, src, blobs, Selection{Subtrees: []string{cat.ID}, Assets: &AssetsSelection{}})
	into := migratedStore(t, envDialect())
	if _, err := Import(ctx, into, filestore.NewLocal(t.TempDir()), opener(pkg), ImportOptions{}); err != nil {
		t.Fatal(err)
	}
	assets, err := into.ListAssets("", "")
	if err != nil {
		t.Fatal(err)
	}
	found := 0
	for _, a := range assets {
		if a.Asset.Name != "Saw" && a.Asset.Name != "Router" {
			continue
		}
		found++
		if a.Review == nil || a.Review.State != models.ReviewDraft || a.Review.ConfirmedBy != nil || a.Review.DueAt != nil || a.Review.Proposal != nil {
			t.Errorf("imported %s: %+v", a.Asset.Name, a.Review)
		}
	}
	if found != 2 {
		t.Errorf("imported %d of the assets", found)
	}
}
