package store

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/fields"
)

func TestFieldStoreCRUD(t *testing.T) {
	st := setupTestStore(t)

	// 1. Create text field
	f1, err := st.CreateField("asset_tag", "Asset Tag Number", fields.TypeText, nil)
	if err != nil {
		t.Fatalf("creating field 1: %v", err)
	}
	if f1.ID == "" || f1.Key != "asset_tag" {
		t.Errorf("field 1 attributes mismatch: %+v", f1)
	}

	// 2. Create select field with valid choices
	selectJSON := json.RawMessage(`{
		"choices": [
			{"label": "Operational", "value": "operational"},
			{"label": "Needs Repair", "value": "needs_repair"}
		],
		"default": "operational"
	}`)
	f2, err := st.CreateField("condition", "Physical Condition", fields.TypeSelect, selectJSON)
	if err != nil {
		t.Fatalf("creating field 2: %v", err)
	}
	if f2.ValidationType != fields.TypeSelect || len(f2.Options) == 0 {
		t.Errorf("field 2 options mismatch: %+v", f2)
	}

	// 3. Duplicate key should fail
	_, err = st.CreateField("asset_tag", "Duplicate Tag", fields.TypeText, nil)
	if !errors.Is(err, ErrFieldKeyExists) {
		t.Fatalf("expected ErrFieldKeyExists, got: %v", err)
	}

	// 4. Invalid key format should fail
	_, err = st.CreateField("Invalid-Key!", "Bad", fields.TypeText, nil)
	if err == nil {
		t.Fatalf("expected error for bad key format, got nil")
	}

	// 5. Select field missing choices should fail
	_, err = st.CreateField("bad_select", "Bad Select", fields.TypeSelect, nil)
	if err == nil {
		t.Fatalf("expected error for select field without options, got nil")
	}

	// 6. GetFieldByID and GetFieldByKey
	byID, err := st.GetFieldByID(f1.ID)
	if err != nil || byID.Key != "asset_tag" {
		t.Fatalf("GetFieldByID failed: %v", err)
	}
	byKey, err := st.GetFieldByKey("condition")
	if err != nil || byKey.ID != f2.ID {
		t.Fatalf("GetFieldByKey failed: %v", err)
	}

	// 7. ListFields
	all, err := st.ListFields()
	if err != nil {
		t.Fatalf("ListFields failed: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2 fields, got %d", len(all))
	}

	// 8. UpdateField
	updatedSelectJSON := json.RawMessage(`{
		"choices": [
			{"label": "Operational", "value": "operational"},
			{"label": "Needs Repair", "value": "needs_repair"},
			{"label": "Decommissioned", "value": "decommissioned"}
		],
		"default": "operational"
	}`)
	updated, err := st.UpdateField(f2.ID, "Asset Condition Status", fields.TypeSelect, updatedSelectJSON)
	if err != nil {
		t.Fatalf("UpdateField failed: %v", err)
	}
	if updated.Label != "Asset Condition Status" {
		t.Errorf("updated label mismatch: %s", updated.Label)
	}

	// 9. DeleteField
	if err := st.DeleteField(f1.ID); err != nil {
		t.Fatalf("DeleteField failed: %v", err)
	}
	_, err = st.GetFieldByID(f1.ID)
	if !errors.Is(err, ErrFieldNotFound) {
		t.Fatalf("expected ErrFieldNotFound, got: %v", err)
	}
}
