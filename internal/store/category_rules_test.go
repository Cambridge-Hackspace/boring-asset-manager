package store

import (
	"errors"
	"testing"
)

func TestCategoryRunningLowRequiresConsumable(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()

	if _, err := st.CreateCategory("Tools", nil, "", false, true); !errors.Is(err, ErrRunningLowRequiresConsumable) {
		t.Fatalf("expected ErrRunningLowRequiresConsumable on create, got %v", err)
	}

	bin, err := st.CreateCategory("Resistors", nil, "", true, true)
	if err != nil {
		t.Fatalf("creating consumable running-low category: %v", err)
	}

	if _, err := st.UpdateCategory(bin.ID, bin.Name, nil, "", false, true); !errors.Is(err, ErrRunningLowRequiresConsumable) {
		t.Fatalf("expected ErrRunningLowRequiresConsumable on update, got %v", err)
	}
}

func TestUncategorizedStaysOutsideCategoryTree(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()

	if err := st.EnsureUncategorizedCategory(); err != nil {
		t.Fatalf("ensuring uncategorized: %v", err)
	}
	root, err := st.CreateCategory("Root", nil, "", false, false)
	if err != nil {
		t.Fatalf("creating root: %v", err)
	}

	uncategorized := UncategorizedCategoryID
	if _, err := st.CreateCategory("Child", &uncategorized, "", false, false); !errors.Is(err, ErrSystemCategoryInTree) {
		t.Errorf("expected ErrSystemCategoryInTree when parenting under Uncategorized, got %v", err)
	}
	if _, err := st.UpdateCategory(UncategorizedCategoryID, UncategorizedCategoryName, &root.ID, "", false, false); !errors.Is(err, ErrSystemCategoryInTree) {
		t.Errorf("expected ErrSystemCategoryInTree when giving Uncategorized a parent, got %v", err)
	}
}

func TestMissingParentIsNotReportedAsMissingCategory(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()

	cat, err := st.CreateCategory("Existing", nil, "", false, false)
	if err != nil {
		t.Fatalf("creating category: %v", err)
	}

	missing := "00000000-0000-0000-0000-000000000000"
	_, err = st.UpdateCategory(cat.ID, cat.Name, &missing, "", false, false)
	if !errors.Is(err, ErrParentNotFound) {
		t.Fatalf("expected ErrParentNotFound, got %v", err)
	}
	if errors.Is(err, ErrCategoryNotFound) {
		t.Errorf("a missing parent must not be reported as the target category being missing")
	}
}
