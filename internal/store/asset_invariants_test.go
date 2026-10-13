package store

import (
	"errors"
	"testing"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/fields"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/generators"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
)

// runWithin fails the test if fn does not return within d (guards against pool deadlocks).
func runWithin(t *testing.T, d time.Duration, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()
	select {
	case <-done:
	case <-time.After(d):
		t.Fatalf("operation did not complete within %s (deadlock?)", d)
	}
}

func TestAssetCorpusFieldOutsideCategoriesDoesNotDeadlock(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()

	if _, err := st.CreateField("notes", "Notes", fields.TypeText, nil); err != nil {
		t.Fatalf("creating field: %v", err)
	}
	cat, err := st.CreateCategory("Tools", nil, "", false, false)
	if err != nil {
		t.Fatalf("creating category: %v", err)
	}

	var (
		created *models.AssetWithDetails
		updated *models.AssetWithDetails
		errC    error
		errU    error
	)
	runWithin(t, 15*time.Second, func() {
		created, errC = st.CreateAsset("Drill", []string{cat.ID}, nil, map[string]string{"notes": "left bench"})
		if errC != nil {
			return
		}
		updated, errU = st.UpdateAsset(created.Asset.ID, "", []string{cat.ID}, nil, map[string]string{"notes": "right bench"})
	})
	if errC != nil {
		t.Fatalf("CreateAsset: %v", errC)
	}
	if errU != nil {
		t.Fatalf("UpdateAsset: %v", errU)
	}
	if created.FieldValues["notes"] != "left bench" {
		t.Errorf("created notes = %q", created.FieldValues["notes"])
	}
	if updated.FieldValues["notes"] != "right bench" {
		t.Errorf("updated notes = %q", updated.FieldValues["notes"])
	}
}

func TestGeneratorCodesTrimmedAndReleasedOnArchive(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()

	first, err := st.CreateAsset("First", nil, []models.AssetGeneratorKey{
		{GeneratorType: generators.TypeCode128, AssignedCode: "  BAM-0001  "},
	}, nil)
	if err != nil {
		t.Fatalf("creating first asset: %v", err)
	}
	if got := first.GeneratorKeys[0].AssignedCode; got != "BAM-0001" {
		t.Fatalf("expected stored code to be trimmed, got %q", got)
	}

	// Whitespace variants must collide with the trimmed code.
	_, err = st.CreateAsset("Clash", nil, []models.AssetGeneratorKey{
		{GeneratorType: generators.TypeCode128, AssignedCode: "BAM-0001 "},
	}, nil)
	if !errors.Is(err, ErrGeneratorCodeExists) {
		t.Fatalf("expected ErrGeneratorCodeExists for whitespace variant, got %v", err)
	}

	// Archiving releases the code: uniqueness only holds across active assets.
	if err := st.DeleteAsset(first.Asset.ID); err != nil {
		t.Fatalf("archiving first asset: %v", err)
	}
	second, err := st.CreateAsset("Second", nil, []models.AssetGeneratorKey{
		{GeneratorType: generators.TypeCode128, AssignedCode: "BAM-0001"},
	}, nil)
	if err != nil {
		t.Fatalf("expected archived asset's code to be reusable, got %v", err)
	}
	if len(second.GeneratorKeys) != 1 {
		t.Fatalf("expected one generator key on second asset, got %d", len(second.GeneratorKeys))
	}
}

func TestDeleteCategoryReattachesOrphanedAssetsToUncategorized(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()

	only, err := st.CreateCategory("Only", nil, "", false, false)
	if err != nil {
		t.Fatalf("creating category: %v", err)
	}
	other, err := st.CreateCategory("Other", nil, "", false, false)
	if err != nil {
		t.Fatalf("creating category: %v", err)
	}

	orphan, err := st.CreateAsset("Orphan", []string{only.ID}, nil, nil)
	if err != nil {
		t.Fatalf("creating orphan asset: %v", err)
	}
	multi, err := st.CreateAsset("Multi", []string{only.ID, other.ID}, nil, nil)
	if err != nil {
		t.Fatalf("creating multi asset: %v", err)
	}

	if err := st.DeleteCategory(only.ID); err != nil {
		t.Fatalf("deleting category: %v", err)
	}

	reloaded, err := st.GetAssetByID(orphan.Asset.ID)
	if err != nil {
		t.Fatalf("reloading orphan: %v", err)
	}
	if len(reloaded.Categories) != 1 || reloaded.Categories[0].ID != UncategorizedCategoryID {
		t.Errorf("expected orphan to fall back to Uncategorized, got %+v", reloaded.Categories)
	}

	reloadedMulti, err := st.GetAssetByID(multi.Asset.ID)
	if err != nil {
		t.Fatalf("reloading multi: %v", err)
	}
	if len(reloadedMulti.Categories) != 1 || reloadedMulti.Categories[0].ID != other.ID {
		t.Errorf("expected multi-category asset to keep only Other, got %+v", reloadedMulti.Categories)
	}
}

func TestUncategorizedHiddenWhenOnlyArchivedAssets(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()

	asset, err := st.CreateAsset("Loose", nil, nil, nil)
	if err != nil {
		t.Fatalf("creating asset: %v", err)
	}
	if !listsCategory(t, st, UncategorizedCategoryID) {
		t.Fatalf("expected Uncategorized to be listed while it holds an active asset")
	}

	if err := st.DeleteAsset(asset.Asset.ID); err != nil {
		t.Fatalf("archiving asset: %v", err)
	}
	if listsCategory(t, st, UncategorizedCategoryID) {
		t.Errorf("expected Uncategorized to be hidden when it only holds archived assets")
	}
}

func listsCategory(t *testing.T, st *Store, id string) bool {
	t.Helper()
	cats, err := st.ListCategories()
	if err != nil {
		t.Fatalf("listing categories: %v", err)
	}
	for _, c := range cats {
		if c.ID == id {
			return true
		}
	}
	return false
}

func TestEffectiveFieldsSortedByKey(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()

	root, _ := st.CreateCategory("Root", nil, "", false, false)
	child, _ := st.CreateCategory("Child", &root.ID, "", false, false)
	for i, key := range []string{"zulu", "alpha", "mike", "bravo", "yankee", "charlie"} {
		f, err := st.CreateField(key, key, fields.TypeText, nil)
		if err != nil {
			t.Fatalf("creating field %s: %v", key, err)
		}
		target := root.ID
		if i%2 == 0 {
			target = child.ID
		}
		if err := st.AssignCategoryField(target, f.ID, false); err != nil {
			t.Fatalf("assigning field %s: %v", key, err)
		}
	}

	for attempt := 0; attempt < 5; attempt++ {
		effective, err := st.GetEffectiveCategoryFields(child.ID)
		if err != nil {
			t.Fatalf("resolving effective fields: %v", err)
		}
		for i := 1; i < len(effective); i++ {
			if effective[i-1].Field.Key > effective[i].Field.Key {
				t.Fatalf("effective fields not sorted by key: %q before %q", effective[i-1].Field.Key, effective[i].Field.Key)
			}
		}
	}
}

func TestListAssetsSearchIsCaseInsensitive(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()

	if _, err := st.CreateAsset("ThinkPad LAPTOP", nil, []models.AssetGeneratorKey{
		{GeneratorType: generators.TypeCode128, AssignedCode: "Lab-Kit-7"},
	}, nil); err != nil {
		t.Fatalf("creating asset: %v", err)
	}

	for _, term := range []string{"laptop", "THINKPAD", "lab-kit", "LAB-KIT-7"} {
		results, err := st.ListAssets("", term)
		if err != nil {
			t.Fatalf("searching %q: %v", term, err)
		}
		if len(results) != 1 {
			t.Errorf("search %q: expected 1 result, got %d", term, len(results))
		}
	}
}
