package store

import (
	"errors"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/flows"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
)

func TestSavingFlows(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	cat, err := st.CreateCategory("Wood", nil, "", false, false)
	if err != nil {
		t.Fatal(err)
	}
	def := flows.Starters([]string{cat.ID})[0].Definition
	f, err := st.CreateFlow(FlowInput{Slug: "leave", Name: " Leave something ", Enabled: true, Definition: def}, "")
	if err != nil {
		t.Fatal(err)
	}
	if f.Name != "Leave something" || f.Slug != "leave" || !f.Enabled || len(f.Definition.Steps) != len(def.Steps) || f.Definition.CategoryIDs[0] != cat.ID {
		t.Errorf("saved: %+v", f)
	}
	if got, err := st.GetFlow("leave"); err != nil || got.ID != f.ID {
		t.Errorf("by its address: %+v %v", got, err)
	}
	if _, err := st.CreateFlow(FlowInput{Slug: "leave", Name: "Again", Definition: def}, ""); !IsUniqueViolation(err) {
		t.Errorf("an address in use: %v", err)
	}
	for _, slug := range []string{"", "Leave", "leave it", "-leave"} {
		if _, err := st.CreateFlow(FlowInput{Slug: slug, Name: "x", Definition: def}, ""); !errors.Is(err, ErrInvalidFlow) {
			t.Errorf("address %q: %v", slug, err)
		}
	}
	if _, err := st.CreateFlow(FlowInput{Slug: "x", Name: "  ", Definition: def}, ""); !errors.Is(err, ErrInvalidFlow) {
		t.Errorf("no name: %v", err)
	}

	f, err = st.UpdateFlow(f.ID, FlowInput{Slug: "drop-off", Name: "Drop off", Enabled: false, Definition: def})
	if err != nil || f.Slug != "drop-off" || f.Enabled {
		t.Fatalf("changed: %+v %v", f, err)
	}
	if _, err := st.GetFlow("leave"); !errors.Is(err, ErrFlowNotFound) {
		t.Errorf("the old address: %v", err)
	}
	if _, err := st.UpdateFlow("missing", FlowInput{Slug: "y", Name: "y", Definition: def}); !errors.Is(err, ErrFlowNotFound) {
		t.Errorf("changing a missing flow: %v", err)
	}

	// The catalog holds what flows may name.
	c, err := st.FlowCatalog([]string{cat.ID})
	if err != nil || !c.Categories[cat.ID] || !c.Flows[f.ID] {
		t.Errorf("catalog: %+v %v", c, err)
	}

	// Deleting it takes its runs and filings, not the assets.
	user, _ := st.CreateUser("sam@hackspace.org", "Pass1234!", true)
	a, _ := st.CreateAsset("Oak", []string{cat.ID}, nil, nil)
	if err := st.RecordFiling(f.ID, a.Asset.ID, user.ID); err != nil {
		t.Fatal(err)
	}
	if err := st.RecordFiling(f.ID, a.Asset.ID, user.ID); err != nil {
		t.Errorf("filing twice: %v", err)
	}
	if filed, _ := st.FiledBy(f.ID); !filed[a.Asset.ID] || len(filed) != 1 {
		t.Errorf("filed: %v", filed)
	}
	run, err := st.CreateFlowRun(f.ID, user.ID, FlowRunState{Values: flows.Values{"user": "Sam"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteFlow(f.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetFlowRun(run.ID); !errors.Is(err, ErrFlowRunNotFound) {
		t.Errorf("its run: %v", err)
	}
	if filed, _ := st.FiledBy(f.ID); len(filed) != 0 {
		t.Errorf("its filings: %v", filed)
	}
	if _, err := st.GetAssetByID(a.Asset.ID); err != nil {
		t.Errorf("the asset went: %v", err)
	}
	if err := st.DeleteFlow(f.ID); !errors.Is(err, ErrFlowNotFound) {
		t.Errorf("deleting it twice: %v", err)
	}
}

func TestFlowRunsMoveOnOnce(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	cat, _ := st.CreateCategory("Wood", nil, "", false, false)
	f, err := st.CreateFlow(FlowInput{Slug: "leave", Name: "Leave", Enabled: true, Definition: flows.Starters([]string{cat.ID})[0].Definition}, "")
	if err != nil {
		t.Fatal(err)
	}
	user, _ := st.CreateUser("sam@hackspace.org", "Pass1234!", true)
	run, err := st.CreateFlowRun(f.ID, user.ID, FlowRunState{Values: flows.Values{"user": "Sam"}})
	if err != nil {
		t.Fatal(err)
	}
	if run.Step != 0 || run.Values["user"] != "Sam" || run.Finished || len(run.Done) != 0 {
		t.Errorf("started: %+v", run)
	}

	// Of two answers to the same step, one claims it; the other is refused
	// until the first is saved, and then it is on another step.
	if err := st.ClaimFlowRun(run.ID, 0); err != nil {
		t.Fatal(err)
	}
	if err := st.ClaimFlowRun(run.ID, 0); !errors.Is(err, ErrFlowRunMoved) {
		t.Errorf("claimed twice: %v", err)
	}
	state := FlowRunState{Step: 1, Values: flows.Values{"user": "Sam", "name": "Oak"},
		Done: []models.FlowDone{{Step: "file", Kind: flows.KindCreate, Said: "Filed Oak."}}, Fields: map[string]string{"owner": "Sam"}, Progress: map[string]int{"file": 1}}
	if err := st.SaveFlowRun(run.ID, state); err != nil {
		t.Fatal(err)
	}
	if err := st.ClaimFlowRun(run.ID, 0); !errors.Is(err, ErrFlowRunMoved) {
		t.Errorf("replaying a step: %v", err)
	}
	run, _ = st.GetFlowRun(run.ID)
	if run.Step != 1 || run.Values["name"] != "Oak" || len(run.Done) != 1 || run.Fields["owner"] != "Sam" || run.Progress["file"] != 1 {
		t.Errorf("saved: %+v", run)
	}
	if err := st.ClaimFlowRun(run.ID, 1); err != nil {
		t.Errorf("claiming the step it is on: %v", err)
	}
	state.Finished = true
	_ = st.SaveFlowRun(run.ID, state)
	if err := st.ClaimFlowRun(run.ID, 1); !errors.Is(err, ErrFlowRunMoved) {
		t.Errorf("claiming a finished run: %v", err)
	}

	// A run left too long is gone, and goes when the next starts.
	if _, err := st.Exec("UPDATE flow_runs SET updated_at = ? WHERE id = ?", time.Now().UTC().Add(-FlowRunLifetime-time.Minute), run.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetFlowRun(run.ID); !errors.Is(err, ErrFlowRunNotFound) {
		t.Errorf("a run left too long: %v", err)
	}
	if _, err := st.CreateFlowRun(f.ID, user.ID, FlowRunState{}); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := st.QueryRow("SELECT COUNT(*) FROM flow_runs").Scan(&n); err != nil || n != 1 {
		t.Errorf("runs kept: %d %v", n, err)
	}
}

// Migration 043 grants flows.manage to every role that manages categories,
// and flows.run to every role that writes assets, in the same scopes; once
// however often it runs.
func TestFlowPermissionsAreGrantedByMigration(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	if _, err := st.CreateScope("tools"); err != nil {
		t.Fatal(err)
	}
	curators, _ := st.CreateRole(RoleInput{Name: "Curators", Permissions: []string{rbac.PermCategoriesManage}})
	writers, _ := st.CreateRole(RoleInput{Name: "Writers", Permissions: []string{rbac.PermAssetsWrite}})
	keepers, _ := st.CreateRole(RoleInput{Name: "Keepers", Permissions: []string{"tools:" + rbac.PermAssetsWrite, rbac.PermAssetsRead}})
	readers, _ := st.CreateRole(RoleInput{Name: "Readers", Permissions: []string{rbac.PermAssetsRead}})
	for _, stmt := range []string{
		"DELETE FROM permissions WHERE permission_key IN ('flows.manage', 'flows.run')",
		"DELETE FROM schema_migrations WHERE version = 43",
	} {
		if _, err := st.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 2; i++ {
		if err := st.Migrate(); err != nil {
			t.Fatal(err)
		}
		if _, err := st.Exec("DELETE FROM schema_migrations WHERE version = 43"); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		id   string
		want string
	}{
		{curators.ID, "categories.manage,flows.manage"},
		{writers.ID, "assets.write,flows.run"},
		{keepers.ID, "assets.read,tools:assets.write,tools:flows.run"},
		{readers.ID, "assets.read"},
	} {
		role, err := st.GetRole(c.id)
		if err != nil {
			t.Fatal(err)
		}
		got := append([]string(nil), role.Permissions...)
		sort.Strings(got)
		if strings.Join(got, ",") != c.want {
			t.Errorf("%s: %v, want %s", role.Name, got, c.want)
		}
	}
}

// TestThingsLeftPastTheirDate: what a flow with a date to collect by filed
// is overdue the day after that date, announced once; moving the date on
// clears it, and collecting it ends it.
func TestThingsLeftPastTheirDate(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	cat, _ := st.CreateCategory("Wood", nil, "", false, false)
	until, err := st.CreateField("until", "Until", "date", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AssignCategoryField(cat.ID, until.ID, false); err != nil {
		t.Fatal(err)
	}
	def := flows.Starters([]string{cat.ID})[0].Definition
	def.LeaveUntil = "until"
	c, _ := st.FlowCatalog([]string{cat.ID})
	if problems := def.Check(c); len(problems) != 0 {
		t.Fatalf("checked: %v", problems)
	}
	if bad := (flows.Definition{Version: 1, CategoryIDs: []string{cat.ID}, LeaveUntil: "colour", Steps: []flows.Step{{ID: "done", Kind: flows.KindDone}}}); len(bad.Check(c)) != 1 {
		t.Errorf("a date to collect by that is not a date field: %v", bad.Check(c))
	}
	f, err := st.CreateFlow(FlowInput{Slug: "leave", Name: "Leave something", Enabled: true, Definition: def}, "")
	if err != nil {
		t.Fatal(err)
	}
	oak, _ := st.CreateAsset("Sam's oak", []string{cat.ID}, nil, map[string]string{"until": "2026-11-06"})
	pine, _ := st.CreateAsset("Pine", []string{cat.ID}, nil, map[string]string{"until": "2026-12-01"})
	undated, _ := st.CreateAsset("Undated", []string{cat.ID}, nil, nil)
	for _, a := range []*models.AssetWithDetails{oak, pine, undated} {
		if err := st.RecordFiling(f.ID, a.Asset.ID, ""); err != nil {
			t.Fatal(err)
		}
	}
	events := func() int {
		var n int
		if err := st.QueryRow("SELECT COUNT(*) FROM events WHERE kind = ?", models.EventFlowOverdue).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	day := func(s string) time.Time {
		at, _ := time.Parse("2006-01-02 15:04", s)
		return at
	}

	// On its date it is not yet overdue.
	if n, err := st.RecordOverdueFilings(day("2026-11-06 23:59")); err != nil || n != 0 {
		t.Errorf("on its date: %d %v", n, err)
	}
	// The day after, it is, once.
	for i := 0; i < 2; i++ {
		if n, err := st.RecordOverdueFilings(day("2026-11-07 00:00")); err != nil || n != 1-i {
			t.Errorf("the day after, sweep %d: %d %v", i, n, err)
		}
	}
	if events() != 1 {
		t.Errorf("events: %d", events())
	}
	listed, err := st.OverdueFilings(day("2026-11-07 09:00"))
	if err != nil || len(listed[f.ID]) != 1 || listed[f.ID][0].Name != "Sam's oak" || listed[f.ID][0].Until != "2026-11-06" {
		t.Errorf("listed: %v %v", listed, err)
	}
	var payload string
	_ = st.QueryRow("SELECT payload FROM events WHERE kind = ?", models.EventFlowOverdue).Scan(&payload)
	if !strings.Contains(payload, `"flow":{"slug":"leave","name":"Leave something"}`) || !strings.Contains(payload, `"due_at":"2026-11-06T00:00:00Z"`) {
		t.Errorf("payload: %s", payload)
	}

	// Moved on a week, it is no longer overdue; when that passes too, it
	// is announced again.
	if _, err := st.UpdateAsset(oak.Asset.ID, "", nil, nil, map[string]string{"until": "2026-11-13"}); err != nil {
		t.Fatal(err)
	}
	if n, _ := st.RecordOverdueFilings(day("2026-11-08 00:00")); n != 0 {
		t.Errorf("moved on: %d", n)
	}
	if n, _ := st.RecordOverdueFilings(day("2026-11-14 00:00")); n != 1 {
		t.Errorf("past the new date: %d", n)
	}
	// Collected (archived), it is no longer listed.
	if err := st.DeleteAsset(oak.Asset.ID); err != nil {
		t.Fatal(err)
	}
	if listed, _ := st.OverdueFilings(day("2026-11-14 00:00")); len(listed[f.ID]) != 0 {
		t.Errorf("collected, still listed: %v", listed)
	}
}
