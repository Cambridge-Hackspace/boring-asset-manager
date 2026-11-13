package store

import (
	"errors"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/generators"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
)

// An NFC tag's serial number is kept in one form, and found in any.
func TestAnNFCTagFindsItsAssetHoweverItsSerialIsWritten(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()

	drill, err := st.CreateAsset("Drill", nil, []models.AssetGeneratorKey{
		{GeneratorType: generators.TypeCode128, AssignedCode: "HS-12"},
		{GeneratorType: generators.TypeNFC, AssignedCode: " 04a23b1c5d8012 "},
	}, nil)
	if err != nil {
		t.Fatalf("creating the asset: %v", err)
	}
	var stored string
	for _, k := range drill.GeneratorKeys {
		if k.GeneratorType == generators.TypeNFC {
			stored = k.AssignedCode
		}
	}
	if stored != "04:A2:3B:1C:5D:80:12" {
		t.Fatalf("stored serial = %q, want 04:A2:3B:1C:5D:80:12", stored)
	}

	for _, code := range []string{"04:a2:3b:1c:5d:80:12", "04A23B1C5D8012", "04 A2 3B 1C 5D 80 12", "HS-12"} {
		ids, err := st.AssetsByCode(code)
		if err != nil {
			t.Fatalf("AssetsByCode(%q): %v", code, err)
		}
		if len(ids) != 1 || ids[0] != drill.Asset.ID {
			t.Errorf("AssetsByCode(%q) = %v, want the drill", code, ids)
		}
	}

	// The same tag on another asset, however written, is taken.
	_, err = st.CreateAsset("Clash", nil, []models.AssetGeneratorKey{
		{GeneratorType: generators.TypeNFC, AssignedCode: "04-A2-3B-1C-5D-80-12"},
	}, nil)
	if !errors.Is(err, ErrGeneratorCodeExists) {
		t.Fatalf("the same tag again: got %v, want ErrGeneratorCodeExists", err)
	}

	// A serial number is not numbered in a series, though it may end in
	// digits.
	if next, err := st.NextCode(generators.TypeNFC); err != nil || next != "" {
		t.Errorf("NextCode(nfc) = %q, %v; want none", next, err)
	}
	if next, err := st.NextCodeLike(generators.TypeNFC, stored); err != nil || next != "" {
		t.Errorf("NextCodeLike(nfc) = %q, %v; want none", next, err)
	}

	if _, err := st.CreateAsset("Bad", nil, []models.AssetGeneratorKey{
		{GeneratorType: generators.TypeNFC, AssignedCode: "HS-13"},
	}, nil); !errors.Is(err, generators.ErrInvalidCharacterSet) {
		t.Errorf("a serial number that is not one: got %v", err)
	}
}
