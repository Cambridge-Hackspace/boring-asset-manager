package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/fields"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/flows"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// woodShop is the wood shop of #121: wood, with an owner, in two kinds,
// the long boards also recording until when they are left; and a kitchen
// besides, in no scope.
type woodShop struct {
	srv                        *Server
	admin                      *models.User
	scope                      *models.Scope
	wood, short, long, kitchen *models.Category
	members                    *models.RoleWithPermissions
	sam                        *models.User
}

func newWoodShop(t *testing.T) *woodShop {
	t.Helper()
	srv, admin, _ := setupTestServer(t)
	st := srv.store
	w := &woodShop{srv: srv, admin: admin}
	var err error
	if w.scope, err = st.CreateScope("woodshop"); err != nil {
		t.Fatal(err)
	}
	if w.wood, err = st.CreateCategoryFrom(store.CategoryInput{Name: "Wood", IsLendable: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SetCategoryScopes(w.wood.ID, []string{w.scope.ID}); err != nil {
		t.Fatal(err)
	}
	if w.short, err = st.CreateCategoryFrom(store.CategoryInput{Name: "Short boards", ParentID: &w.wood.ID}); err != nil {
		t.Fatal(err)
	}
	if w.long, err = st.CreateCategoryFrom(store.CategoryInput{Name: "Long boards", ParentID: &w.wood.ID}); err != nil {
		t.Fatal(err)
	}
	if w.kitchen, err = st.CreateCategoryFrom(store.CategoryInput{Name: "Kitchen"}); err != nil {
		t.Fatal(err)
	}
	owner, err := st.CreateField("owner", "Owner", fields.TypeText, nil)
	if err != nil {
		t.Fatal(err)
	}
	until, err := st.CreateField("until", "Until", fields.TypeDate, nil)
	if err != nil {
		t.Fatal(err)
	}
	// The owner is recorded for all wood, inherited; until, for long boards.
	if err := st.AssignCategoryField(w.wood.ID, owner.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := st.AssignCategoryField(w.long.ID, until.ID, false); err != nil {
		t.Fatal(err)
	}
	if w.members, err = st.CreateRole(store.RoleInput{Name: "Members", Permissions: []string{
		rbac.PermFlowsRun, rbac.PermAssetsRead, rbac.PermAssetsWrite, rbac.PermAssetsDelete, rbac.PermAssetsScan,
	}}); err != nil {
		t.Fatal(err)
	}
	if w.sam, err = st.CreateNamedUser("sam@hackspace.org", "Sam", "UserSecret123!", true); err != nil {
		t.Fatal(err)
	}
	// Members of the wood shop, there only (#118).
	if err := st.SetUserRoleAssignments(w.sam.ID, []models.RoleAssignment{{RoleID: w.members.ID, ScopeIDs: []string{w.scope.ID}}}); err != nil {
		t.Fatal(err)
	}
	return w
}

// as is a request made by a user, as the API sees one once it has
// authenticated them.
func as(t *testing.T, srv *Server, userID string) *http.Request {
	t.Helper()
	u, err := srv.store.GetUserByID(userID)
	if err != nil {
		t.Fatal(err)
	}
	perms, err := srv.store.GetUserPermissions(userID)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	return r.WithContext(WithUser(WithPermissions(r.Context(), perms), u))
}

// saveFlow saves a flow as the admin, checked as the API checks it.
func (w *woodShop) saveFlow(t *testing.T, slug, name string, d flows.Definition) *models.Flow {
	t.Helper()
	problems, err := w.srv.checkFlow(as(t, w.srv, w.admin.ID), &d)
	if err != nil || len(problems) > 0 {
		t.Fatalf("%s: %v %v", slug, problems, err)
	}
	f, err := w.srv.store.CreateFlow(store.FlowInput{Slug: slug, Name: name, Enabled: true, Definition: d}, w.admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func stepOf(f *models.Flow, run *models.FlowRun) flows.Step {
	if run.Step >= len(f.Definition.Steps) {
		return flows.Step{}
	}
	return f.Definition.Steps[run.Step]
}

func statusOf(err error) int {
	var fe *flowError
	if errors.As(err, &fe) {
		return fe.Status
	}
	return 0
}

func TestFlowsLeaveAndCollect(t *testing.T) {
	w := newWoodShop(t)
	srv := w.srv
	leave := flows.Starters([]string{w.short.ID, w.long.ID})[0].Definition
	// The owner defaults to whoever runs it; the long boards ask whether
	// to leave them out, and only they.
	leave.Steps[1].Fields = []flows.FieldSetting{{Key: "until", Label: "Leave until", Help: "When you'll take it home"}, {Key: "owner", Default: "{user}"}}
	leave.Steps = append(leave.Steps[:2], append([]flows.Step{
		{ID: "long_only", Kind: flows.KindConfirm, Text: "{owner}'s {name} goes on the long rack.", When: &flows.Condition{Value: flows.ValueCategoryID, Op: flows.OpIs, To: w.long.ID}},
	}, leave.Steps[2:]...)...)
	flow := w.saveFlow(t, "leave", "Leave something", leave)

	sam := as(t, srv, w.sam.ID)
	runner := &flowRunner{s: srv, r: sam, flow: flow}
	run, err := runner.startRun()
	if err != nil {
		t.Fatal(err)
	}
	if s := stepOf(flow, run); s.Kind != flows.KindChooseCategory {
		t.Fatalf("starts on %+v", s)
	}
	offered, _ := runner.offered(&flow.Definition.Steps[0])
	if len(offered) != 2 {
		t.Errorf("offered: %v", offered)
	}

	// Answering another step than the one it is on, or skipping ahead, is
	// refused.
	if _, err := runner.answer(run, FlowAnswer{Step: "file"}); statusOf(err) != http.StatusConflict {
		t.Errorf("skipping ahead: %v", err)
	}
	if _, err := runner.answer(run, FlowAnswer{Step: "kind", CategoryID: w.kitchen.ID}); statusOf(err) != http.StatusBadRequest {
		t.Errorf("a category not offered: %v", err)
	}

	// Short boards: the form asks the owner, inherited and required, with
	// its default, and not until, which only long boards record.
	run, err = runner.answer(run, FlowAnswer{Step: "kind", CategoryID: w.short.ID})
	if err != nil {
		t.Fatal(err)
	}
	asked, err := runner.formFields(&flow.Definition.Steps[run.Step], run.Values)
	if err != nil {
		t.Fatal(err)
	}
	if len(asked) != 1 || asked[0].Key != "owner" || !asked[0].Required || asked[0].Default != "Sam" {
		t.Errorf("short boards ask %+v", asked)
	}
	// Changing the category rebuilds it: long boards ask until too, first,
	// as the creator ordered and named it.
	long := run.Values
	long[flows.ValueCategoryID] = w.long.ID
	asked, _ = runner.formFields(&flow.Definition.Steps[run.Step], long)
	if len(asked) != 2 || asked[0].Key != "until" || asked[0].Label != "Leave until" || asked[0].Help != "When you'll take it home" || asked[1].Key != "owner" {
		t.Errorf("long boards ask %+v", asked)
	}
	long[flows.ValueCategoryID] = w.short.ID

	if _, err := runner.answer(run, FlowAnswer{Step: "about", Name: "Oak offcut"}); statusOf(err) != http.StatusBadRequest || !strings.Contains(err.Error(), "Owner is required") {
		t.Errorf("the owner left out: %v", err)
	}
	run, err = runner.answer(run, FlowAnswer{Step: "about", Name: "Oak offcut", Fields: map[string]string{"owner": "Sam"}})
	if err != nil {
		t.Fatal(err)
	}
	// The long boards' step is skipped; Create and Label ask nothing; so
	// the run is done.
	if !run.Finished {
		t.Fatalf("not finished, on %+v: %+v", stepOf(flow, run), run.Done)
	}
	id := run.Values[flows.ValueAssetID]
	a, err := srv.store.GetAssetByID(id)
	if err != nil {
		t.Fatal(err)
	}
	if a.Asset.Name != "Oak offcut" || a.FieldValues["owner"] != "Sam" || len(a.Categories) != 1 || a.Categories[0].ID != w.short.ID {
		t.Errorf("filed %+v %v", a.Asset, a.FieldValues)
	}
	if filed, _ := srv.store.FiledBy(flow.ID); !filed[id] {
		t.Error("the filing is not recorded")
	}
	// History records the flow and who ran it.
	events, _, err := srv.store.ListAssetEvents(id, "", 10)
	if err != nil || len(events) == 0 {
		t.Fatalf("history: %v %v", events, err)
	}
	b, _ := json.Marshal(events[len(events)-1])
	if !strings.Contains(string(b), `"via":"flow “Leave something”"`) || !strings.Contains(string(b), w.sam.ID) {
		t.Errorf("history: %s", b)
	}
	// A finished run takes no more answers.
	if _, err := runner.answer(run, FlowAnswer{Step: "done"}); statusOf(err) != http.StatusConflict {
		t.Errorf("a finished run: %v", err)
	}

	// Collect it: Sam's own things, filed through Leave something, found
	// and archived.
	collect := flows.Starters([]string{w.wood.ID})[1].Definition
	collect.Steps[0].Match = &flows.Match{Field: "owner", Value: "{user}"}
	collect.Steps[0].FiledBy = flow.ID
	collect.Steps[0].FindBy = []string{flows.FindList}
	cflow := w.saveFlow(t, "collect", "Collect it", collect)
	// Neither someone else's, nor Sam's own that the flow did not file.
	other, _ := srv.store.CreateAsset("Somebody else's pine", []string{w.short.ID}, nil, map[string]string{"owner": "Alex"})
	unfiled, _ := srv.store.CreateAsset("Sam's birch", []string{w.short.ID}, nil, map[string]string{"owner": "Sam"})
	_ = unfiled
	crunner := &flowRunner{s: srv, r: sam, flow: cflow}
	crun, err := crunner.startRun()
	if err != nil {
		t.Fatal(err)
	}
	found, err := crunner.candidates(&cflow.Definition.Steps[0], crun.Values, "")
	if err != nil || len(found) != 1 || found[0].Asset.ID != id {
		t.Fatalf("Sam finds %v, %v", found, err)
	}
	if _, err := crunner.answer(crun, FlowAnswer{Step: "find", AssetID: other.Asset.ID}); statusOf(err) != http.StatusBadRequest {
		t.Errorf("finding someone else's: %v", err)
	}
	crun, err = crunner.answer(crun, FlowAnswer{Step: "find", AssetID: id})
	if err != nil || stepOf(cflow, crun).Kind != flows.KindConfirm {
		t.Fatalf("found: %+v %v", crun, err)
	}
	crun, err = crunner.answer(crun, FlowAnswer{Step: "confirm"})
	if err != nil || !crun.Finished {
		t.Fatalf("collected: %+v %v", crun, err)
	}
	if a, _ := srv.store.GetAssetByID(id); a.Asset.ArchivedAt == nil {
		t.Error("not archived")
	}
	if len(crun.Done) != 1 || crun.Done[0].Said != "Archived Oak offcut." {
		t.Errorf("done: %+v", crun.Done)
	}
}

// TestFlowRunsAreHeldToTheRunnersPermissions: every Change a flow can make
// is refused for a runner who lacks the permission behind it, the run
// staying on that step to try again; and flows.run decides who may run a
// flow at all, scope by scope.
func TestFlowRunsAreHeldToTheRunnersPermissions(t *testing.T) {
	w := newWoodShop(t)
	srv, st := w.srv, w.srv.store
	board, err := st.CreateAsset("Walnut", []string{w.short.ID}, nil, map[string]string{"owner": "Sam"})
	if err != nil {
		t.Fatal(err)
	}
	shelf, _ := st.CreateAsset("Shelf", []string{w.short.ID}, nil, map[string]string{"owner": "Sam"})
	action, err := st.SaveAction("", store.ActionInput{Key: "identify", Label: "Identify"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.SetCategoryActions(w.wood.ID, []models.CategoryAction{{ActionID: action.ID}}); err != nil {
		t.Fatal(err)
	}
	// A reader runs flows and reads, in the wood shop, and nothing else.
	readers, _ := st.CreateRole(store.RoleInput{Name: "Readers", Permissions: []string{rbac.PermFlowsRun, rbac.PermAssetsRead}})
	reader, _ := st.CreateNamedUser("rae@hackspace.org", "Rae", "UserSecret123!", true)
	if err := st.SetUserRoleAssignments(reader.ID, []models.RoleAssignment{{RoleID: readers.ID, ScopeIDs: []string{w.scope.ID}}}); err != nil {
		t.Fatal(err)
	}
	for _, c := range []flows.Change{
		{Kind: flows.ChangeArchive},
		{Kind: flows.ChangeCondition, Condition: "out_of_service", Note: "split"},
		{Kind: flows.ChangeBorrow},
		{Kind: flows.ChangeReturn},
		{Kind: flows.ChangePlace, AssetID: shelf.Asset.ID},
		{Kind: flows.ChangeCategory, CategoryID: w.long.ID},
		{Kind: flows.ChangeFields, Fields: map[string]string{"owner": "{user}"}},
		{Kind: flows.ChangeAction, Action: "identify"},
	} {
		d := flows.Definition{Version: flows.Version, CategoryIDs: []string{w.wood.ID}, Steps: []flows.Step{
			{ID: "find", Kind: flows.KindFind, FindBy: []string{flows.FindList}},
			{ID: "change", Kind: flows.KindChange, Changes: []flows.Change{c}},
			{ID: "done", Kind: flows.KindDone},
		}}
		f := w.saveFlow(t, "change-"+c.Kind, "Change "+c.Kind, d)
		runner := &flowRunner{s: srv, r: as(t, srv, reader.ID), flow: f}
		run, err := runner.startRun()
		if err != nil {
			t.Fatal(err)
		}
		after, err := runner.answer(run, FlowAnswer{Step: "find", AssetID: board.Asset.ID})
		if code := statusOf(err); code != http.StatusForbidden {
			t.Errorf("%s: %d %v, want 403", c.Kind, code, err)
			continue
		}
		if s := stepOf(f, after); s.ID != "change" || after.Finished {
			t.Errorf("%s: left on %q", c.Kind, s.ID)
		}
	}
	if a, _ := st.GetAssetByID(board.Asset.ID); a.Asset.ArchivedAt != nil || a.FieldValues["owner"] != "Sam" || len(a.Categories) != 1 || a.Categories[0].ID != w.short.ID {
		t.Errorf("a refused change changed the board: %+v", a)
	}

	// Someone with flows.run in the kitchen only cannot run a wood shop
	// flow, nor see one; a flow turned off runs for no one.
	cooks, _ := st.CreateRole(store.RoleInput{Name: "Cooks", Permissions: []string{rbac.PermFlowsRun, rbac.PermAssetsRead, rbac.PermAssetsWrite}})
	kitchenScope, _ := st.CreateScope("kitchen")
	if _, err := st.SetCategoryScopes(w.kitchen.ID, []string{kitchenScope.ID}); err != nil {
		t.Fatal(err)
	}
	cook, _ := st.CreateNamedUser("cy@hackspace.org", "Cy", "UserSecret123!", true)
	if err := st.SetUserRoleAssignments(cook.ID, []models.RoleAssignment{{RoleID: cooks.ID, ScopeIDs: []string{kitchenScope.ID}}}); err != nil {
		t.Fatal(err)
	}
	leave := w.saveFlow(t, "leave", "Leave something", flows.Starters([]string{w.short.ID, w.kitchen.ID})[0].Definition)
	if may, _ := srv.mayRun(as(t, srv, cook.ID), leave); !may {
		t.Error("a cook may run a flow that files into the kitchen")
	}
	runner := &flowRunner{s: srv, r: as(t, srv, cook.ID), flow: leave}
	offered, _ := runner.offered(&leave.Definition.Steps[0])
	if len(offered) != 1 || offered[0] != w.kitchen.ID {
		t.Errorf("a cook is offered %v", offered)
	}
	woodOnly := w.saveFlow(t, "wood", "Wood", flows.Starters([]string{w.short.ID})[0].Definition)
	if _, err := (&flowRunner{s: srv, r: as(t, srv, cook.ID), flow: woodOnly}).startRun(); statusOf(err) != http.StatusNotFound {
		t.Errorf("a cook running a wood shop flow: %v", err)
	}
	woodOnly.Enabled = false
	if _, err := (&flowRunner{s: srv, r: as(t, srv, w.sam.ID), flow: woodOnly}).startRun(); statusOf(err) != http.StatusNotFound {
		t.Errorf("a flow turned off: %v", err)
	}
}

// TestSavingAFlowNeedsWhatItsStepsUse: a flow never offers more than its
// creator could do.
func TestSavingAFlowNeedsWhatItsStepsUse(t *testing.T) {
	w := newWoodShop(t)
	st := w.srv.store
	makers, _ := st.CreateRole(store.RoleInput{Name: "Makers", Permissions: []string{rbac.PermFlowsManage, rbac.PermAssetsRead, rbac.PermAssetsWrite}})
	maker, _ := st.CreateNamedUser("mo@hackspace.org", "Mo", "UserSecret123!", true)
	if err := st.SetUserRoles(maker.ID, []string{makers.ID}); err != nil {
		t.Fatal(err)
	}
	collect := flows.Starters([]string{w.wood.ID})[1].Definition
	problems, err := w.srv.checkFlow(as(t, w.srv, maker.ID), &collect)
	if err != nil {
		t.Fatal(err)
	}
	var said []string
	for _, p := range problems {
		said = append(said, p.String())
	}
	if got := strings.Join(said, "; "); !strings.Contains(got, "assets.delete") || !strings.Contains(got, "assets.scan") || strings.Contains(got, "assets.read") {
		t.Errorf("problems: %s", got)
	}
	leave := flows.Starters([]string{w.wood.ID})[0].Definition
	if problems, _ := w.srv.checkFlow(as(t, w.srv, maker.ID), &leave); len(problems) != 0 {
		t.Errorf("leave, which only files: %v", problems)
	}
}

// TestATriedAgainStepDoesNotRepeatItsWork: a Change that fails part way is
// tried again from where it failed.
func TestATriedAgainStepDoesNotRepeatItsWork(t *testing.T) {
	w := newWoodShop(t)
	srv, st := w.srv, w.srv.store
	board, _ := st.CreateAsset("Ash", []string{w.short.ID}, nil, map[string]string{"owner": "Sam"})
	d := flows.Definition{Version: flows.Version, CategoryIDs: []string{w.wood.ID}, Steps: []flows.Step{
		{ID: "find", Kind: flows.KindFind, FindBy: []string{flows.FindList}},
		{ID: "change", Kind: flows.KindChange, Changes: []flows.Change{
			{Kind: flows.ChangeFields, Fields: map[string]string{"owner": "{owner} (left)"}},
			{Kind: flows.ChangeArchive},
		}},
		{ID: "done", Kind: flows.KindDone},
	}}
	f := w.saveFlow(t, "finish", "Finish", d)
	// Sam may edit, but archiving is taken away for now.
	members, _ := st.GetRole(w.members.ID)
	var kept []string
	for _, p := range members.Permissions {
		if p != rbac.PermAssetsDelete {
			kept = append(kept, p)
		}
	}
	if _, err := st.UpdateRole(w.members.ID, store.RoleInput{Name: "Members", Permissions: kept}); err != nil {
		t.Fatal(err)
	}
	runner := &flowRunner{s: srv, r: as(t, srv, w.sam.ID), flow: f}
	run, _ := runner.startRun()
	run, err := runner.answer(run, FlowAnswer{Step: "find", AssetID: board.Asset.ID})
	if statusOf(err) != http.StatusForbidden || stepOf(f, run).ID != "change" {
		t.Fatalf("archiving refused: %v, on %q", err, stepOf(f, run).ID)
	}
	if _, err := st.UpdateRole(w.members.ID, store.RoleInput{Name: "Members", Permissions: members.Permissions}); err != nil {
		t.Fatal(err)
	}
	runner = &flowRunner{s: srv, r: as(t, srv, w.sam.ID), flow: f}
	run, err = runner.answer(run, FlowAnswer{Step: "change"})
	if err != nil || !run.Finished {
		t.Fatalf("tried again: %+v %v", run, err)
	}
	a, _ := st.GetAssetByID(board.Asset.ID)
	if a.FieldValues["owner"] != "Sam (left)" || a.Asset.ArchivedAt == nil {
		t.Errorf("after trying again: %v archived %v", a.FieldValues, a.Asset.ArchivedAt)
	}
}
