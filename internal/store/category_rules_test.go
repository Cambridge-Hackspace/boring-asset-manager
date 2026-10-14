package store

import (
	"errors"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/fields"
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

// A field assigned at several levels of the tree is attributed to the nearest
// one. The lookup used to range over a map, so the reported source changed
// from call to call.
func TestEffectiveFieldSourceIsTheNearestAssignment(t *testing.T) {
	st := setupTestStore(t)
	serial, err := st.CreateField("serial_no", "Serial", fields.TypeText, nil)
	if err != nil {
		t.Fatal(err)
	}
	tools, _ := st.CreateCategory("Tools", nil, "#112233", false, false)
	power, _ := st.CreateCategory("Power tools", &tools.ID, "#445566", false, false)
	drills, _ := st.CreateCategory("Drills", &power.ID, "#778899", false, false)
	if err := st.AssignCategoryField(tools.ID, serial.ID, false); err != nil {
		t.Fatal(err)
	}
	if err := st.AssignCategoryField(power.ID, serial.ID, true); err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 30; i++ {
		for cat, want := range map[string]string{drills.ID: power.ID, power.ID: power.ID, tools.ID: tools.ID} {
			got, err := st.GetEffectiveCategoryFields(cat)
			if err != nil || len(got) != 1 {
				t.Fatalf("effective fields of %s: %+v, %v", cat, got, err)
			}
			if got[0].SourceCategoryID != want {
				t.Fatalf("source of serial in %s = %s, want %s", cat, got[0].SourceCategoryID, want)
			}
			if wantRequired := cat != tools.ID; got[0].IsRequired != wantRequired {
				t.Fatalf("serial required in %s = %v", cat, got[0].IsRequired)
			}
		}
	}
}
