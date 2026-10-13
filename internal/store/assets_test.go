package store

import (
	"errors"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/fields"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/generators"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
)

func TestAssetUncategorizedFallbackAndDataRetention(t *testing.T) {
	st := setupTestStore(t)

	// Fields
	fSerial, err := st.CreateField("serial_number", "Serial Number", fields.TypeText, nil)
	if err != nil {
		t.Fatalf("creating fSerial: %v", err)
	}

	// Categories
	catA, err := st.CreateCategory("Category A", nil, "", false, false)
	if err != nil {
		t.Fatalf("creating catA: %v", err)
	}
	_ = st.AssignCategoryField(catA.ID, fSerial.ID, true) // Required in Cat A

	catB, err := st.CreateCategory("Category B", nil, "", false, false)
	if err != nil {
		t.Fatalf("creating catB: %v", err)
	}

	// 1. Create asset in Category A with serial_number
	assetA, err := st.CreateAsset(
		"Test Asset 1",
		[]string{catA.ID},
		[]models.AssetGeneratorKey{
			{GeneratorType: generators.TypeMicroQR, AssignedCode: "MQR-001"},
			{GeneratorType: generators.TypeQR, AssignedCode: "https://bam.local/a/001"},
		},
		map[string]string{"serial_number": "SN-998877"},
	)
	if err != nil {
		t.Fatalf("creating asset in Cat A: %v", err)
	}

	if len(assetA.Categories) != 1 || assetA.Categories[0].ID != catA.ID {
		t.Errorf("expected Category A assignment")
	}
	if assetA.FieldValues["serial_number"] != "SN-998877" {
		t.Errorf("expected serial_number SN-998877, got %q", assetA.FieldValues["serial_number"])
	}

	// 2. Data Retention Rule: Move asset from Category A to Category B (Category B does NOT have serial_number)
	updated, err := st.UpdateAsset(
		assetA.Asset.ID,
		"Test Asset 1 (Moved)",
		[]string{catB.ID},
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("moving asset to Cat B: %v", err)
	}

	if len(updated.Categories) != 1 || updated.Categories[0].ID != catB.ID {
		t.Errorf("expected Category B assignment")
	}
	// Verify Data Retention Rule: serial_number field value MUST STILL EXIST!
	if updated.FieldValues["serial_number"] != "SN-998877" {
		t.Fatalf("data retention failed: serial_number was discarded upon category change! got: %v", updated.FieldValues)
	}

	// 3. Uncategorized Fallback: Detach asset from all categories -> automatically attaches to 'uncategorized'
	detached, err := st.UpdateAsset(
		assetA.Asset.ID,
		"Test Asset 1 (Uncategorized)",
		[]string{}, // Empty categories
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("detaching asset: %v", err)
	}
	if len(detached.Categories) != 1 || detached.Categories[0].ID != UncategorizedCategoryID {
		t.Errorf("expected automatic fallback to Uncategorized category, got: %+v", detached.Categories)
	}
	// Field value still preserved
	if detached.FieldValues["serial_number"] != "SN-998877" {
		t.Errorf("data retention failed on uncategorized fallback")
	}
}

func TestAssetGeneratorKeysUniquenessAndConstraints(t *testing.T) {
	st := setupTestStore(t)

	// 1. Invalid micro_qr format should fail
	_, err := st.CreateAsset(
		"Asset Bad Code",
		nil,
		[]models.AssetGeneratorKey{
			{GeneratorType: generators.TypeMicroQR, AssignedCode: "lowercase-bad"},
		},
		nil,
	)
	if err == nil {
		t.Fatalf("expected error for invalid micro_qr code, got nil")
	}

	// 2. Valid creation
	a1, err := st.CreateAsset(
		"Asset 1",
		nil,
		[]models.AssetGeneratorKey{
			{GeneratorType: generators.TypeMicroQR, AssignedCode: "MQR-TEST-01"},
		},
		nil,
	)
	if err != nil {
		t.Fatalf("creating asset 1: %v", err)
	}
	if a1 == nil {
		t.Fatalf("asset 1 is nil")
	}

	// 3. Compound uniqueness violation: assigning SAME generator_type and code to Asset 2 must fail!
	_, err = st.CreateAsset(
		"Asset 2",
		nil,
		[]models.AssetGeneratorKey{
			{GeneratorType: generators.TypeMicroQR, AssignedCode: "MQR-TEST-01"},
		},
		nil,
	)
	if !errors.Is(err, ErrGeneratorCodeExists) {
		t.Fatalf("expected ErrGeneratorCodeExists for duplicate generator code, got: %v", err)
	}
}

func TestAssetRequiredFieldEnforcement(t *testing.T) {
	st := setupTestStore(t)

	fReq, _ := st.CreateField("mac_addr", "MAC Address", fields.TypeText, nil)
	cat, _ := st.CreateCategory("Network Devices", nil, "", false, false)
	_ = st.AssignCategoryField(cat.ID, fReq.ID, true) // Required

	// Attempt to create asset in Network Devices WITHOUT mac_addr -> should fail
	_, err := st.CreateAsset("Switch", []string{cat.ID}, nil, nil)
	if err == nil {
		t.Fatalf("expected error for missing required field, got nil")
	}

	// Provide invalid format if number field
	fNum, _ := st.CreateField("port_count", "Port Count", fields.TypeNumber, nil)
	_ = st.AssignCategoryField(cat.ID, fNum.ID, false)

	_, err = st.CreateAsset(
		"Switch",
		[]string{cat.ID},
		nil,
		map[string]string{
			"mac_addr":   "00:11:22:33:44:55",
			"port_count": "not-a-number",
		},
	)
	if err == nil {
		t.Fatalf("expected error for invalid number value, got nil")
	}

	// Provide valid values -> succeeds
	asset, err := st.CreateAsset(
		"Switch",
		[]string{cat.ID},
		nil,
		map[string]string{
			"mac_addr":   "00:11:22:33:44:55",
			"port_count": "24",
		},
	)
	if err != nil {
		t.Fatalf("expected success with valid required fields, got: %v", err)
	}
	if asset.FieldValues["port_count"] != "24" {
		t.Errorf("port_count mismatch: %s", asset.FieldValues["port_count"])
	}
}
