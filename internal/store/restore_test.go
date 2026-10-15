package store

import (
	"errors"
	"reflect"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
)

func TestRestoreGivesBackTheCodesNobodyTook(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	a, err := st.CreateAsset("Saw", nil, []models.AssetGeneratorKey{
		{GeneratorType: "qr", AssignedCode: "S-1"},
		{GeneratorType: "code128", AssignedCode: "C-1"},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	id := a.Asset.ID
	if _, _, err := st.RestoreAsset(id); !errors.Is(err, ErrAssetNotArchived) {
		t.Errorf("restoring an active asset: %v", err)
	}
	if _, _, err := st.RestoreAsset("no-such-asset"); !errors.Is(err, ErrAssetNotFound) {
		t.Errorf("restoring a missing asset: %v", err)
	}
	archive(t, st, id)

	// Archived assets are listed apart from active ones, by name only, since
	// their codes are released.
	if list, err := st.ListAssets("", "Saw"); err != nil || len(list) != 0 {
		t.Errorf("active list: %d, %v", len(list), err)
	}
	if list, err := st.ListArchivedAssets("", "saw"); err != nil || len(list) != 1 || list[0].Asset.ID != id {
		t.Errorf("archived list: %+v, %v", list, err)
	}

	// Another asset takes one of its codes meanwhile.
	if _, err := st.CreateAsset("Other saw", nil, []models.AssetGeneratorKey{{GeneratorType: "qr", AssignedCode: "S-1"}}, nil); err != nil {
		t.Fatal(err)
	}
	restored, skipped, err := st.RestoreAsset(id)
	if err != nil {
		t.Fatal(err)
	}
	if restored.Asset.ArchivedAt != nil || !reflect.DeepEqual(restored.GeneratorKeys, []models.AssetGeneratorKey{{AssetID: id, GeneratorType: "code128", AssignedCode: "C-1"}}) {
		t.Errorf("restored %+v with %+v", restored.Asset, restored.GeneratorKeys)
	}
	if !reflect.DeepEqual(skipped, []models.AssetGeneratorKey{{AssetID: id, GeneratorType: "qr", AssignedCode: "S-1"}}) {
		t.Errorf("skipped %+v", skipped)
	}
	if list, _ := st.ListArchivedAssets("", ""); len(list) != 0 {
		t.Errorf("still listed as archived: %d", len(list))
	}

	// The history says which codes came back and which were taken.
	expectChanges(t, "restored", history(t, st, id)[0], models.EventAssetRestored,
		models.EventChange{What: "code", Key: "code128", Label: "Code 128", To: "C-1"},
		models.EventChange{What: "code", Key: "qr", Label: "QR Code", From: "S-1"},
	)

	// Archived again, it releases only the code it got back.
	archive(t, st, id)
	if _, skipped, err := st.RestoreAsset(id); err != nil || len(skipped) != 0 {
		t.Errorf("restoring again: %+v, %v", skipped, err)
	}
}

func TestRestoringAnAssetArchivedBeforeHistoryHasNoCodes(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	a, err := st.CreateAsset("Saw", nil, []models.AssetGeneratorKey{{GeneratorType: "qr", AssignedCode: "S-1"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	archive(t, st, a.Asset.ID)
	// As if archived before archiving recorded its codes.
	if _, err := st.Exec("DELETE FROM events WHERE asset_id = ?", a.Asset.ID); err != nil {
		t.Fatal(err)
	}
	restored, skipped, err := st.RestoreAsset(a.Asset.ID)
	if err != nil || len(restored.GeneratorKeys) != 0 || len(skipped) != 0 {
		t.Fatalf("restored %+v, skipped %+v, %v", restored, skipped, err)
	}
	expectChanges(t, "restored", history(t, st, a.Asset.ID)[0], models.EventAssetRestored)
}

func TestRestoringCrossesThresholds(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	spools := consumable(t, st, "Spools", nil, intp(2))
	first := asset(t, st, "Spool 1", spools.ID)
	asset(t, st, "Spool 2", spools.ID)
	archive(t, st, first)
	if !flag(t, st, spools.ID) {
		t.Fatal("archiving did not flag the category")
	}
	if _, _, err := st.RestoreAsset(first); err != nil {
		t.Fatal(err)
	}
	if flag(t, st, spools.ID) {
		t.Error("restoring did not clear the category")
	}
	if list := events(t, st); list[len(list)-1].Kind != models.EventRestocked {
		t.Errorf("events: %s", kinds(list))
	}

	// An asset below its own threshold is flagged as it comes back, even if
	// someone had cleared its flag by hand.
	screws := stocked(t, st, "Screws", 1, "pcs", intp(5))
	if _, _, err := st.SetAssetRunningLow(screws, false); err != nil {
		t.Fatal(err)
	}
	archive(t, st, screws)
	if _, _, err := st.RestoreAsset(screws); err != nil {
		t.Fatal(err)
	}
	if !stockOf(t, st, screws).IsRunningLow {
		t.Error("a restored asset below its threshold was not flagged")
	}
}
