package store

import (
	"errors"
	"strings"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
)

func TestAssetCondition(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	user, err := st.CreateUser("bo@hackspace.org", "a long enough password", true)
	if err != nil {
		t.Fatal(err)
	}
	drill := asset(t, st, "Drill")
	saw := asset(t, st, "Saw")
	a, err := st.GetAssetByID(drill)
	if err != nil || a.Asset.Condition != models.ConditionOK || a.Asset.ConditionNote != "" {
		t.Fatalf("a new asset: %+v, %v", a, err)
	}

	got, changed, err := st.As(user.ID).SetAssetCondition(drill, models.ConditionOutOfService, "  Chuck slips ")
	if err != nil || !changed || got.Asset.Condition != models.ConditionOutOfService || got.Asset.ConditionNote != "Chuck slips" {
		t.Fatalf("reported %+v, %v, %v", got, changed, err)
	}
	e := history(t, st, drill)[0]
	expectChanges(t, "reported", e, models.EventAssetConditionChanged, models.EventChange{What: "condition", From: "ok", To: "out_of_service"})
	if e.Payload.Note != "Chuck slips" || e.Payload.Actor == nil || e.Payload.Actor.ID != user.ID {
		t.Errorf("payload %+v", e.Payload)
	}
	// Channels hear of it: it is not history only.
	if models.HistoryOnly(e.Kind) {
		t.Error("condition changes are history only")
	}

	// The same again records nothing; a new note does.
	n := len(history(t, st, drill))
	if _, changed, err := st.SetAssetCondition(drill, models.ConditionOutOfService, "Chuck slips"); err != nil || changed {
		t.Errorf("the same again: %v, %v", changed, err)
	}
	if _, changed, _ := st.SetAssetCondition(drill, models.ConditionOutOfService, "Chuck slips; motor smells"); !changed {
		t.Error("a new note changed nothing")
	}
	if got := len(history(t, st, drill)); got != n+1 {
		t.Errorf("%d events, want %d", got, n+1)
	}

	// The list filters by condition.
	if _, _, err := st.SetAssetCondition(saw, models.ConditionNeedsMaintenance, "Blunt"); err != nil {
		t.Fatal(err)
	}
	for f, want := range map[string]int{"": 2, ConditionProblem: 2, models.ConditionOutOfService: 1, models.ConditionNeedsMaintenance: 1, models.ConditionOK: 0} {
		if list, err := st.FilterAssets(AssetFilter{Condition: f}); err != nil || len(list) != want {
			t.Errorf("condition %q: %d, %v; want %d", f, len(list), err, want)
		}
	}
	if _, err := st.FilterAssets(AssetFilter{Condition: "broken"}); !errors.Is(err, ErrInvalidCondition) {
		t.Errorf("an unknown filter: %v", err)
	}

	// Working again, it keeps no note.
	got, _, err = st.SetAssetCondition(drill, models.ConditionOK, "Fixed it")
	if err != nil || got.Asset.Condition != models.ConditionOK || got.Asset.ConditionNote != "" {
		t.Errorf("fixed %+v, %v", got, err)
	}

	for what, try := range map[string]func() error{
		"an unknown condition": func() error { _, _, err := st.SetAssetCondition(saw, "broken", ""); return err },
		"a long note": func() error {
			_, _, err := st.SetAssetCondition(saw, models.ConditionNeedsMaintenance, strings.Repeat("n", 256))
			return err
		},
	} {
		if err := try(); err == nil {
			t.Errorf("%s was accepted", what)
		}
	}
	if _, _, err := st.SetAssetCondition("no-such-asset", models.ConditionOK, ""); !errors.Is(err, ErrAssetNotFound) {
		t.Errorf("a missing asset: %v", err)
	}
	archive(t, st, saw)
	if _, _, err := st.SetAssetCondition(saw, models.ConditionOK, ""); !errors.Is(err, ErrAssetArchived) {
		t.Errorf("an archived asset: %v", err)
	}
}
