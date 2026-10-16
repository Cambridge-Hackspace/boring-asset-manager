package store

import (
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/fields"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
)

// history lists an asset's events, newest first.
func history(t *testing.T, st *Store, id string) []models.Event {
	t.Helper()
	list, _, err := st.ListAssetEvents(id, "", 1000)
	if err != nil {
		t.Fatal(err)
	}
	return list
}

// expectChanges checks an event's kind and the changes it lists.
func expectChanges(t *testing.T, step string, e models.Event, kind string, want ...models.EventChange) {
	t.Helper()
	if e.Kind != kind || !reflect.DeepEqual(e.Payload.Changes, want) {
		t.Errorf("%s: %s %+v\nwant %s %+v", step, e.Kind, e.Payload.Changes, kind, want)
	}
}

func TestHistoryRecordsEveryEdit(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	user, err := st.CreateUser("stores@hackspace.org", "a long enough password", true)
	if err != nil {
		t.Fatal(err)
	}
	st = st.As(user.ID)
	bench := consumable(t, st, "Bench", nil, nil)
	tools := consumable(t, st, "Tools", nil, nil)
	serial, err := st.CreateField("serial", "Serial", fields.TypeText, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Created: everything it started with, then its quantity.
	a, err := st.CreateAssetWithStock("Drill", []string{tools.ID, bench.ID},
		[]models.AssetGeneratorKey{{GeneratorType: "qr", AssignedCode: "D-1"}},
		map[string]string{"serial": "SN-1"}, &StockInput{Quantity: intp(2), Unit: "pcs"})
	if err != nil {
		t.Fatal(err)
	}
	id := a.Asset.ID
	list := history(t, st, id)
	if len(list) != 2 || list[0].Kind != models.EventQuantityChanged {
		t.Fatalf("created: %s", kinds(list))
	}
	created := list[1]
	expectChanges(t, "created", created, models.EventAssetCreated,
		models.EventChange{What: "name", To: "Drill"},
		models.EventChange{What: "category", Key: bench.ID, To: "Bench"},
		models.EventChange{What: "category", Key: tools.ID, To: "Tools"},
		models.EventChange{What: "field", Key: "serial", Label: "Serial", To: "SN-1"},
		models.EventChange{What: "code", Key: "qr", Label: "QR Code", To: "D-1"},
		models.EventChange{What: "stock", To: "counted"},
		models.EventChange{What: "unit", To: "pcs"},
	)
	if p := created.Payload; p.Trigger != models.TriggerManual || p.Actor == nil || p.Actor.Email != "stores@hackspace.org" || p.Asset.Name != "Drill" {
		t.Errorf("created by: %+v", p)
	}

	// Edited: one event listing each change, old and new.
	_, err = st.UpdateAssetWithStock(id, "Cordless drill", []string{tools.ID},
		[]models.AssetGeneratorKey{{GeneratorType: "qr", AssignedCode: "D-2"}, {GeneratorType: "code128", AssignedCode: "C-2"}},
		map[string]string{"serial": "SN-2"}, &StockUpdate{Stock: &StockInput{Unit: "drills", LowStockThreshold: intp(1)}})
	if err != nil {
		t.Fatal(err)
	}
	expectChanges(t, "edited", history(t, st, id)[0], models.EventAssetEdited,
		models.EventChange{What: "name", From: "Drill", To: "Cordless drill"},
		models.EventChange{What: "category", Key: bench.ID, From: "Bench"},
		models.EventChange{What: "field", Key: "serial", Label: "Serial", From: "SN-1", To: "SN-2"},
		models.EventChange{What: "code", Key: "code128", Label: "Code 128", To: "C-2"},
		models.EventChange{What: "code", Key: "qr", Label: "QR Code", From: "D-1", To: "D-2"},
		models.EventChange{What: "unit", From: "pcs", To: "drills"},
		models.EventChange{What: "threshold", To: "1"},
	)

	// A save that changes nothing records nothing.
	n := len(history(t, st, id))
	if _, err := st.UpdateAsset(id, "Cordless drill", nil, nil, map[string]string{"serial": "SN-2"}); err != nil {
		t.Fatal(err)
	}
	if got := len(history(t, st, id)); got != n {
		t.Errorf("a save changing nothing recorded %d events", got-n)
	}

	// A label as it read when the change was made; a cleared value; stopping counting.
	if _, err := st.UpdateField(serial.ID, "Serial number", fields.TypeText, nil); err != nil {
		t.Fatal(err)
	}
	_, err = st.UpdateAssetWithStock(id, "", nil, []models.AssetGeneratorKey{}, map[string]string{"serial": ""}, &StockUpdate{})
	if err != nil {
		t.Fatal(err)
	}
	list = history(t, st, id)
	expectChanges(t, "cleared", list[0], models.EventAssetEdited,
		models.EventChange{What: "field", Key: "serial", Label: "Serial number", From: "SN-2"},
		models.EventChange{What: "code", Key: "code128", Label: "Code 128", From: "C-2"},
		models.EventChange{What: "code", Key: "qr", Label: "QR Code", From: "D-2"},
		models.EventChange{What: "stock", From: "counted"},
	)
	if list[1].Payload.Changes[2].Label != "Serial" {
		t.Errorf("an earlier event's label changed with the field: %+v", list[1].Payload.Changes)
	}
}

func TestHistoryRecordsArchivingAndItsCodes(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	a, err := st.CreateAsset("Saw", nil, []models.AssetGeneratorKey{{GeneratorType: "qr", AssignedCode: "S-1"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	archive(t, st, a.Asset.ID)
	archive(t, st, a.Asset.ID)
	list := history(t, st, a.Asset.ID)
	if len(list) != 2 {
		t.Fatalf("archiving twice: %s", kinds(list))
	}
	// The codes it released, which a restore needs.
	expectChanges(t, "archived", list[0], models.EventAssetArchived,
		models.EventChange{What: "code", Key: "qr", Label: "QR Code", From: "S-1"})
}

func TestHistoryOfAFailedEditIsRolledBack(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	if _, err := st.CreateAsset("Saw", nil, []models.AssetGeneratorKey{{GeneratorType: "qr", AssignedCode: "TAKEN"}}, nil); err != nil {
		t.Fatal(err)
	}
	id := asset(t, st, "Drill")
	_, err := st.UpdateAsset(id, "Renamed", nil, []models.AssetGeneratorKey{{GeneratorType: "qr", AssignedCode: "TAKEN"}}, nil)
	if !errors.Is(err, ErrGeneratorCodeExists) {
		t.Fatalf("taking a code in use: %v", err)
	}
	if list := history(t, st, id); len(list) != 1 || list[0].Kind != models.EventAssetCreated {
		t.Errorf("a failed edit left %s", kinds(list))
	}
}

func TestHistoryRecordsImports(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	id := asset(t, st, "Drill")
	note, err := st.CreateField("note", "Note", fields.TypeText, nil)
	if err != nil {
		t.Fatal(err)
	}
	created, err := st.ImportAssets([]ImportRow{
		{Name: "Saw", Codes: map[string]string{"qr": "S-1"}, Stock: &StockInput{Quantity: intp(3)}},
		{ID: id, Name: "Drill", FieldValues: map[string]string{note.ID: "Blunt"}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	// The quantity comes after the creation it belongs to.
	saw := history(t, st, created[0])
	if len(saw) != 2 || saw[0].Kind != models.EventQuantityChanged || saw[1].Payload.Trigger != models.TriggerImport {
		t.Fatalf("the created asset: %+v", saw)
	}
	expectChanges(t, "imported", saw[1], models.EventAssetCreated,
		models.EventChange{What: "name", To: "Saw"},
		models.EventChange{What: "category", Key: UncategorizedCategoryID, To: "Uncategorized"},
		models.EventChange{What: "code", Key: "qr", Label: "QR Code", To: "S-1"},
		models.EventChange{What: "stock", To: "counted"},
	)
	drill := history(t, st, id)
	expectChanges(t, "updated", drill[0], models.EventAssetEdited,
		models.EventChange{What: "field", Key: "note", Label: "Note", To: "Blunt"})
	if drill[0].Payload.Trigger != models.TriggerImport {
		t.Errorf("trigger %q", drill[0].Payload.Trigger)
	}

	// Importing the same again changes, and records, nothing.
	if _, err := st.ImportAssets([]ImportRow{{ID: id, Name: "Drill", FieldValues: map[string]string{note.ID: "Blunt"}}}, nil); err != nil {
		t.Fatal(err)
	}
	if got := len(history(t, st, id)); got != len(drill) {
		t.Errorf("an unchanged row recorded %d events", got-len(drill))
	}
}

func TestHistoryPages(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	id := stocked(t, st, "Screws", 0, "", nil)
	for i := 0; i < 11; i++ {
		adjust(t, st, id, 1)
	}
	// 13 events: created, started counting, and 11 restocks.
	var seen []string
	before := ""
	for page := 1; ; page++ {
		list, more, err := st.ListAssetEvents(id, before, 5)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range list {
			seen = append(seen, e.ID)
		}
		if want := page < 3; more != want || len(list) != map[bool]int{true: 5, false: 3}[want] {
			t.Fatalf("page %d: %d events, more %v", page, len(list), more)
		}
		if !more {
			break
		}
		before = list[len(list)-1].ID
	}
	all := history(t, st, id)
	if len(all) != 13 || fmt.Sprint(seen) != fmt.Sprint(ids(all)) {
		t.Errorf("pages %v\nwant %v", seen, ids(all))
	}

	if _, _, err := st.ListAssetEvents(id, "no-such-event", 5); !errors.Is(err, ErrEventNotFound) {
		t.Errorf("an unknown event: %v", err)
	}
	other := asset(t, st, "Drill")
	if _, _, err := st.ListAssetEvents(other, all[0].ID, 5); !errors.Is(err, ErrEventNotFound) {
		t.Errorf("another asset's event: %v", err)
	}
}

func ids(list []models.Event) []string {
	out := make([]string, len(list))
	for i, e := range list {
		out[i] = e.ID
	}
	return out
}

func TestAssetEventsSince(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	start := time.Now().Add(-time.Second)
	screws := stocked(t, st, "Screws", 10, "pcs", nil)
	adjust(t, st, screws, -3)
	asset(t, st, "Drill")
	list, err := st.AssetEventsSince([]string{models.EventQuantityChanged}, start)
	if err != nil || len(list) != 2 || list[1].Payload.Change == nil || *list[1].Payload.Change != -3 {
		t.Fatalf("since the start: %+v, %v", list, err)
	}
	if list, _ := st.AssetEventsSince([]string{models.EventQuantityChanged}, time.Now().Add(time.Hour)); len(list) != 0 {
		t.Errorf("from the future: %+v", list)
	}
	if list, _ := st.AssetEventsSince(nil, start); len(list) != 0 {
		t.Errorf("no kinds: %+v", list)
	}
	if list, _ := st.AssetEventsSince([]string{models.EventAssetCreated, models.EventQuantityChanged}, start); len(list) != 4 {
		t.Errorf("two kinds: %d", len(list))
	}
}
