package store

import (
	"errors"
	"testing"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/fields"
	"github.com/google/uuid"
)

func TestCategoryHierarchyAndCycleDetection(t *testing.T) {
	st := setupTestStore(t)

	// 1. Create Root Category A
	catA, err := st.CreateCategory("Electronics", nil, "#ff0000", false, false)
	if err != nil {
		t.Fatalf("creating root category: %v", err)
	}

	// 2. Create Child Category B under A
	catB, err := st.CreateCategory("Computers", &catA.ID, "#00ff00", false, false)
	if err != nil {
		t.Fatalf("creating child category B: %v", err)
	}

	// 3. Create Child Category C under B
	catC, err := st.CreateCategory("Laptops", &catB.ID, "#0000ff", false, false)
	if err != nil {
		t.Fatalf("creating child category C: %v", err)
	}

	// 4. Test self-parent error
	_, err = st.UpdateCategory(catA.ID, "Electronics", &catA.ID, "#ff0000", false, false)
	if !errors.Is(err, ErrSelfParent) {
		t.Fatalf("expected ErrSelfParent, got: %v", err)
	}

	// 5. Test cycle detection: Setting A's parent to C (A -> B -> C -> A)
	_, err = st.UpdateCategory(catA.ID, "Electronics", &catC.ID, "#ff0000", false, false)
	if !errors.Is(err, ErrCycleDetected) {
		t.Fatalf("expected ErrCycleDetected, got: %v", err)
	}

	// 6. Test cycle detection: Setting B's parent to C (B -> C -> B)
	_, err = st.UpdateCategory(catB.ID, "Computers", &catC.ID, "#00ff00", false, false)
	if !errors.Is(err, ErrCycleDetected) {
		t.Fatalf("expected ErrCycleDetected, got: %v", err)
	}
}

func TestCategoryFieldInheritanceAndConflictResolution(t *testing.T) {
	st := setupTestStore(t)

	// Fields
	fSerial, err := st.CreateField("serial_number", "Serial Number", fields.TypeText, nil)
	if err != nil {
		t.Fatalf("creating fSerial: %v", err)
	}
	fWattage, err := st.CreateField("wattage", "Power Wattage", fields.TypeNumber, nil)
	if err != nil {
		t.Fatalf("creating fWattage: %v", err)
	}
	fBrand, err := st.CreateField("brand", "Manufacturer Brand", fields.TypeText, nil)
	if err != nil {
		t.Fatalf("creating fBrand: %v", err)
	}

	// Category Tree:
	// Equipment (Root): has fSerial (Optional)
	//   -> Power Tools (Child of Equipment): has fWattage (Required)
	// Hand Tools (Root): has fSerial (Required), fBrand (Optional)
	catEquipment, _ := st.CreateCategory("Equipment", nil, "", false, false)
	catPowerTools, _ := st.CreateCategory("Power Tools", &catEquipment.ID, "", false, false)
	catHandTools, _ := st.CreateCategory("Hand Tools", nil, "", false, false)

	// Bindings:
	// Equipment has serial_number (Optional)
	if err := st.AssignCategoryField(catEquipment.ID, fSerial.ID, false); err != nil {
		t.Fatalf("assigning field: %v", err)
	}
	// Power Tools has wattage (Required)
	if err := st.AssignCategoryField(catPowerTools.ID, fWattage.ID, true); err != nil {
		t.Fatalf("assigning field: %v", err)
	}
	// Hand Tools has serial_number (Required) and brand (Optional)
	if err := st.AssignCategoryField(catHandTools.ID, fSerial.ID, true); err != nil {
		t.Fatalf("assigning field: %v", err)
	}
	if err := st.AssignCategoryField(catHandTools.ID, fBrand.ID, false); err != nil {
		t.Fatalf("assigning field: %v", err)
	}

	// 1. Single category inheritance: Power Tools should inherit fSerial (Optional) from Equipment + fWattage (Required)
	ptFields, err := st.GetEffectiveCategoryFields(catPowerTools.ID)
	if err != nil {
		t.Fatalf("getting effective fields: %v", err)
	}
	if len(ptFields) != 2 {
		t.Fatalf("expected 2 inherited fields for Power Tools, got %d", len(ptFields))
	}

	ptFieldMap := make(map[string]bool)
	for _, ef := range ptFields {
		ptFieldMap[ef.Field.Key] = ef.IsRequired
	}
	if req, ok := ptFieldMap["serial_number"]; !ok || req {
		t.Errorf("expected serial_number to be optional in Power Tools, got req=%v", req)
	}
	if req, ok := ptFieldMap["wattage"]; !ok || !req {
		t.Errorf("expected wattage to be required in Power Tools, got req=%v", req)
	}

	// 2. Multi-category inheritance with Conflict Resolution:
	// An asset belongs to BOTH [Power Tools] and [Hand Tools].
	// fSerial is Optional in Power Tools' ancestor (Equipment), but Required in Hand Tools.
	// Conflict resolution: fSerial MUST be Required!
	multiFields, err := st.GetEffectiveFieldsForCategories([]string{catPowerTools.ID, catHandTools.ID})
	if err != nil {
		t.Fatalf("getting multi-category effective fields: %v", err)
	}
	if len(multiFields) != 3 {
		t.Fatalf("expected 3 union fields, got %d", len(multiFields))
	}

	multiMap := make(map[string]bool)
	for _, ef := range multiFields {
		multiMap[ef.Field.Key] = ef.IsRequired
	}

	if !multiMap["serial_number"] {
		t.Errorf("conflict resolution failed: serial_number should be Required in union")
	}
	if !multiMap["wattage"] {
		t.Errorf("wattage should be Required")
	}
	if multiMap["brand"] {
		t.Errorf("brand should be Optional")
	}
}

func TestUncategorizedCategoryHiding(t *testing.T) {
	st := setupTestStore(t)
	if err := st.EnsureUncategorizedCategory(); err != nil {
		t.Fatalf("ensuring uncategorized: %v", err)
	}

	// Initially, uncategorized has 0 assets -> should NOT appear in ListCategories
	list, err := st.ListCategories()
	if err != nil {
		t.Fatalf("listing categories: %v", err)
	}
	for _, c := range list {
		if c.ID == UncategorizedCategoryID {
			t.Errorf("uncategorized category should be hidden when it has 0 assets")
		}
	}

	// Attach an asset to uncategorized
	dummyAssetID := uuid.NewString()
	now := time.Now().UTC()
	_, _ = st.Exec("INSERT INTO assets (id, name, created_at, updated_at) VALUES (?, 'Test Asset', ?, ?)", dummyAssetID, now, now)
	_, _ = st.Exec("INSERT INTO asset_categories (asset_id, category_id) VALUES (?, ?)", dummyAssetID, UncategorizedCategoryID)

	// Now uncategorized has 1 asset -> should appear in ListCategories
	listWithAsset, err := st.ListCategories()
	if err != nil {
		t.Fatalf("listing categories with asset: %v", err)
	}
	found := false
	for _, c := range listWithAsset {
		if c.ID == UncategorizedCategoryID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("uncategorized category should be visible when it has >= 1 asset")
	}
}
