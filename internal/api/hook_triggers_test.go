package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
)

// firedHooks lists the asset.hook events recorded on an asset.
func firedHooks(t *testing.T, srv *Server, assetID string) []models.EventHook {
	t.Helper()
	events, err := srv.store.EventsSince(time.Now().Add(-time.Hour), 1000)
	if err != nil {
		t.Fatal(err)
	}
	var out []models.EventHook
	for _, e := range events {
		if e.Kind == models.EventAssetHook && e.AssetID != nil && *e.AssetID == assetID && e.Payload.Hook != nil {
			out = append(out, *e.Payload.Hook)
		}
	}
	return out
}

func TestHookTriggersAreCheckedWhenSaved(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	for name, triggers := range map[string]string{
		"none":                   `[]`,
		"an unknown type":        `[{"type":"webhook"}]`,
		"two buttons":            `[{"type":"button"},{"type":"button"}]`,
		"a button with kinds":    `[{"type":"button","kinds":["asset.created"]}]`,
		"events without kinds":   `[{"type":"event"}]`,
		"a hook's own events":    `[{"type":"event","kinds":["asset.hook"]}]`,
		"archiving":              `[{"type":"event","kinds":["asset.archived"]}]`,
		"a category's events":    `[{"type":"event","kinds":["category.running_low"]}]`,
		"an unknown field":       `[{"type":"event","kinds":["asset.edited"],"condition":{"field":"nope","equals":"x"}}]`,
		"too long an interval":   `[{"type":"event","kinds":["asset.edited"],"interval_seconds":9999999}]`,
		"a schedule on events":   `[{"type":"event","kinds":["asset.edited"],"cron":"* * * * *"}]`,
		"a bad schedule":         `[{"type":"schedule","cron":"every monday"}]`,
		"an unknown time zone":   `[{"type":"schedule","cron":"0 9 * * 1","time_zone":"Mars/Olympus"}]`,
		"an unknown filter":      `[{"type":"schedule","cron":"0 9 * * 1","filter":{"colour":"red"}}]`,
		"archived assets":        `[{"type":"schedule","cron":"0 9 * * 1","filter":{"archived":true}}]`,
		"a bad loan filter":      `[{"type":"schedule","cron":"0 9 * * 1","filter":{"loan":"lost"}}]`,
		"a schedule with kinds":  `[{"type":"schedule","cron":"0 9 * * 1","kinds":["asset.edited"]}]`,
		"more than ten triggers": `[` + strings.Repeat(`{"type":"event","kinds":["asset.edited"]},`, 10) + `{"type":"button"}]`,
	} {
		rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/hooks", `{"key":"h","label":"H","triggers":`+triggers+`}`)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: %d %s, want 400", name, rec.Code, rec.Body)
		}
	}

	good := `[{"type":"event","kinds":["asset.checked_out"],"condition":{"field":"condition","equals":"ok"},"interval_seconds":60},` +
		`{"type":"schedule","cron":"0 9 * * 1","time_zone":"Europe/London","filter":{"running_low":true,"loan":"overdue"}}]`
	rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/hooks", `{"key":"light","label":"Light","triggers":`+good+`}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("a good hook: %d %s", rec.Code, rec.Body)
	}
	var saved struct{ Hook models.AssetHook }
	_ = json.Unmarshal(rec.Body.Bytes(), &saved)
	if len(saved.Hook.Triggers) != 2 || saved.Hook.Button() || saved.Hook.Triggers[1].TimeZone != "Europe/London" {
		t.Errorf("saved %+v", saved.Hook)
	}

	// Replaced without triggers, it keeps them.
	rec = doJSON(t, srv, cookie, http.MethodPut, "/api/v1/hooks/"+saved.Hook.ID, `{"key":"light","label":"Light it"}`)
	_ = json.Unmarshal(rec.Body.Bytes(), &saved)
	if rec.Code != http.StatusOK || len(saved.Hook.Triggers) != 2 {
		t.Errorf("replaced without triggers: %d %+v", rec.Code, saved.Hook)
	}

	// A hook made before triggers, or without them, is a button.
	rec = doJSON(t, srv, cookie, http.MethodPost, "/api/v1/hooks", `{"key":"identify","label":"Identify"}`)
	_ = json.Unmarshal(rec.Body.Bytes(), &saved)
	if !saved.Hook.Button() || len(saved.Hook.Triggers) != 1 {
		t.Errorf("without triggers: %+v", saved.Hook)
	}
}

// triggeredSetup is a server with a category offering one hook, saved
// with triggers, and an asset in it and one not.
func triggeredSetup(t *testing.T, triggers string) (*Server, models.AssetHook, string, string) {
	t.Helper()
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/hooks", `{"key":"light","label":"Light","triggers":`+triggers+`}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("saving the hook: %d %s", rec.Code, rec.Body)
	}
	var saved struct{ Hook models.AssetHook }
	_ = json.Unmarshal(rec.Body.Bytes(), &saved)
	cat, err := srv.store.CreateCategory("Drawers", nil, "", false, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := srv.store.SetCategoryHooks(cat.ID, []models.CategoryHook{{HookID: saved.Hook.ID, Cascades: true}}); err != nil {
		t.Fatal(err)
	}
	in, err := srv.store.CreateAsset("Resistors", []string{cat.ID}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	out, err := srv.store.CreateAsset("Kettle", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	return srv, saved.Hook, in.Asset.ID, out.Asset.ID
}

func TestEventTriggersFireOnTheirKindWhereOfferedWhenTheConditionHolds(t *testing.T) {
	srv, _, in, out := triggeredSetup(t,
		`[{"type":"event","kinds":["asset.condition_changed"],"condition":{"field":"condition","equals":"out_of_service"},"interval_seconds":120}]`)

	// Not a button: it is not offered for pressing.
	a, err := srv.store.GetAssetByID(in)
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Hooks) != 0 {
		t.Errorf("a hook no one presses is offered as a button: %+v", a.Hooks)
	}

	for _, id := range []string{in, out} {
		if _, _, err := srv.store.SetAssetCondition(id, models.ConditionOutOfService, "smoking"); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now()
	if fired := srv.hookTriggerRound(now); fired != 1 {
		t.Errorf("first round fired %d, want 1", fired)
	}
	got := firedHooks(t, srv, in)
	if len(got) != 1 || got[0].Trigger != models.HookTriggerEvent || got[0].Cause == nil || got[0].Cause.Kind != models.EventAssetConditionChanged || got[0].Key != "light" {
		t.Fatalf("fired %+v", got)
	}
	if len(firedHooks(t, srv, out)) != 0 {
		t.Error("fired on an asset no category of which offers it")
	}

	// Nothing new: nothing fires, here or on another server.
	if fired := srv.hookTriggerRound(now); fired != 0 {
		t.Errorf("again: fired %d", fired)
	}
	other := NewServer(srv.cfg, srv.store)
	if fired := other.hookTriggerRound(now); fired != 0 {
		t.Errorf("another server: fired %d", fired)
	}

	// Within the interval, it holds back; the condition not holding, it
	// does not fire at all.
	if _, _, err := srv.store.SetAssetCondition(in, models.ConditionOK, ""); err != nil {
		t.Fatal(err)
	}
	if _, _, err := srv.store.SetAssetCondition(in, models.ConditionOutOfService, "again"); err != nil {
		t.Fatal(err)
	}
	if fired := srv.hookTriggerRound(now.Add(time.Minute)); fired != 0 {
		t.Errorf("within the interval: fired %d", fired)
	}
	if _, _, err := srv.store.SetAssetCondition(in, models.ConditionOK, ""); err != nil {
		t.Fatal(err)
	}
	if _, _, err := srv.store.SetAssetCondition(in, models.ConditionOutOfService, "and again"); err != nil {
		t.Fatal(err)
	}
	if fired := srv.hookTriggerRound(now.Add(3 * time.Minute)); fired != 1 {
		t.Errorf("after the interval: fired %d, want 1", fired)
	}
	// The hook events it recorded fire nothing themselves.
	if fired := srv.hookTriggerRound(now.Add(6 * time.Minute)); fired != 0 {
		t.Errorf("on its own events: fired %d", fired)
	}
}

func TestSchedulesFireOnceOnTheAssetsTheirFilterFinds(t *testing.T) {
	srv, _, in, _ := triggeredSetup(t, `[{"type":"button"},{"type":"schedule","cron":"0 9 * * *","filter":{"condition":"out_of_service"}}]`)
	broken, err := srv.store.CreateAsset("Broken resistors", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := srv.store.GetAssetByID(in)
	if _, err := srv.store.UpdateAsset(broken.Asset.ID, "Broken resistors", []string{a.Categories[0].ID}, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, _, err := srv.store.SetAssetCondition(broken.Asset.ID, models.ConditionOutOfService, ""); err != nil {
		t.Fatal(err)
	}
	// Still a button too.
	if a, _ := srv.store.GetAssetByID(in); len(a.Hooks) != 1 {
		t.Errorf("its button: %+v", a.Hooks)
	}

	day := time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)
	// A schedule starts when first seen, without catching up.
	if fired := srv.hookTriggerRound(day.Add(8 * time.Hour)); fired != 0 {
		t.Errorf("first seen: fired %d", fired)
	}
	if fired := srv.hookTriggerRound(day.Add(8*time.Hour + 30*time.Minute)); fired != 0 {
		t.Errorf("before 9: fired %d", fired)
	}
	if fired := srv.hookTriggerRound(day.Add(9*time.Hour + 5*time.Minute)); fired != 1 {
		t.Errorf("after 9: fired %d, want 1, on the asset out of service", fired)
	}
	got := firedHooks(t, srv, broken.Asset.ID)
	if len(got) != 1 || got[0].Trigger != models.HookTriggerSchedule || got[0].Cause != nil {
		t.Errorf("fired %+v", got)
	}
	if len(firedHooks(t, srv, in)) != 0 {
		t.Error("fired on an asset the filter leaves out")
	}
	// Once, though another server looks too.
	other := NewServer(srv.cfg, srv.store)
	if fired := other.hookTriggerRound(day.Add(9*time.Hour + 6*time.Minute)); fired != 0 {
		t.Errorf("another server: fired %d", fired)
	}
	if fired := srv.hookTriggerRound(day.Add(33 * time.Hour)); fired != 1 {
		t.Errorf("the next day: fired %d, want 1", fired)
	}
}
