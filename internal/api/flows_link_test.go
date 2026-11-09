package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/fields"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/flows"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// kitRoom is the wood shop with kits to put things in (#264): a Kits
// category in the wood shop's scope, two kits, a board to put in them,
// the seeded "contains" type, which holds one thing in one place, and
// "depends on", which links any number.
type kitRoom struct {
	*woodShop
	kits              *models.Category
	board, kit1, kit2 *models.AssetWithDetails
	contains, needs   string
}

func newKitRoom(t *testing.T) *kitRoom {
	t.Helper()
	k := &kitRoom{woodShop: newWoodShop(t)}
	st := k.srv.store
	var err error
	if k.kits, err = st.CreateCategoryFrom(store.CategoryInput{Name: "Kits"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SetCategoryScopes(k.kits.ID, []string{k.scope.ID}); err != nil {
		t.Fatal(err)
	}
	if k.board, err = st.CreateAsset("Board", []string{k.short.ID}, nil, map[string]string{"owner": "Sam"}); err != nil {
		t.Fatal(err)
	}
	if k.kit1, err = st.CreateAsset("Kit 1", []string{k.kits.ID}, nil, nil); err != nil {
		t.Fatal(err)
	}
	if k.kit2, err = st.CreateAsset("Kit 2", []string{k.kits.ID}, nil, nil); err != nil {
		t.Fatal(err)
	}
	types, err := st.ListRelationTypes()
	if err != nil {
		t.Fatal(err)
	}
	for _, rt := range types {
		switch {
		case rt.IsLocation:
			k.contains = rt.ID
		case rt.Name == "depends on":
			k.needs = rt.ID
		}
	}
	if k.contains == "" || k.needs == "" {
		t.Fatalf("relation types: %+v", types)
	}
	return k
}

// linkFlow is a flow over the wood that finds a board, then does these
// steps, then is done, naming what it linked.
func (k *kitRoom) linkFlow(t *testing.T, slug string, steps ...flows.Step) *models.Flow {
	t.Helper()
	d := flows.Definition{Version: flows.Version, CategoryIDs: []string{k.wood.ID}, Steps: append(append([]flows.Step{
		{ID: "find", Kind: flows.KindFind, FindBy: []string{flows.FindList}},
	}, steps...), flows.Step{ID: "done", Kind: flows.KindDone, Text: "{asset}: {linked}"})}
	return k.saveFlow(t, slug, slug, d)
}

// found starts a run of a flow as Sam and finds the board.
func (k *kitRoom) found(t *testing.T, f *models.Flow) (*flowRunner, *models.FlowRun) {
	t.Helper()
	runner := &flowRunner{s: k.srv, r: as(t, k.srv, k.sam.ID), flow: f}
	run, err := runner.startRun()
	if err != nil {
		t.Fatal(err)
	}
	if run, err = runner.answer(run, FlowAnswer{Step: "find", AssetID: k.board.Asset.ID}); err != nil {
		t.Fatal(err)
	}
	return runner, run
}

func (k *kitRoom) linksOf(t *testing.T, id, typeID string) []string {
	t.Helper()
	a, err := k.srv.store.GetAssetByID(id)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, l := range a.Links {
		if l.TypeID == typeID {
			out = append(out, l.Asset.Name)
		}
	}
	return out
}

// A Link step links the asset found to one thing, or to any number, either
// way round; a link of a single type moves it, and says so.
func TestLinkStepLinksOneOrSeveralEitherWay(t *testing.T) {
	k := newKitRoom(t)
	kits := &flows.LinkFind{FindBy: []string{flows.FindList}, Categories: []string{k.kits.ID}}
	into := k.linkFlow(t, "into", flows.Step{ID: "link", Kind: flows.KindLink, Title: "Which kit?", RelationType: k.contains, Required: true, Find: kits})

	runner, run := k.found(t, into)
	p, err := runner.prompt(run, "")
	if err != nil {
		t.Fatal(err)
	}
	if p.Kind != flows.KindLink || p.Relation != "contained in" || !p.Required || p.Repeat || len(p.Assets) != 2 || p.Moves != "" {
		t.Fatalf("the prompt: %+v", p)
	}
	// One, not two; and done with none, when one is required, is refused.
	if _, err := runner.answer(run, FlowAnswer{Step: "link", AssetIDs: []string{k.kit1.Asset.ID, k.kit2.Asset.ID}}); statusOf(err) != http.StatusBadRequest || !strings.Contains(err.Error(), "one thing only") {
		t.Errorf("two for one: %v", err)
	}
	if _, err := runner.answer(run, FlowAnswer{Step: "link", Done: true}); statusOf(err) != http.StatusBadRequest || !strings.Contains(err.Error(), "at least one") {
		t.Errorf("none, required: %v", err)
	}
	run, err = runner.answer(run, FlowAnswer{Step: "link", AssetID: k.kit1.Asset.ID})
	if err != nil || !run.Finished {
		t.Fatalf("linked: %+v %v", run, err)
	}
	if got := k.linksOf(t, k.board.Asset.ID, k.contains); len(got) != 1 || got[0] != "Kit 1" {
		t.Errorf("the board is in %v", got)
	}
	if run.Values[flows.ValueLinked] != "Kit 1" || run.Done[len(run.Done)-1].Said != "Linked Board: contained in Kit 1." {
		t.Errorf("said %+v, linked %q", run.Done, run.Values[flows.ValueLinked])
	}
	// Linked again, into the other kit, it moves out of the first, which
	// the step says before and after.
	runner, run = k.found(t, into)
	if p, _ := runner.prompt(run, ""); p.Moves != "Kit 1" {
		t.Errorf("moves out of %q", p.Moves)
	}
	if run, err = runner.answer(run, FlowAnswer{Step: "link", AssetIDs: []string{k.kit2.Asset.ID}}); err != nil {
		t.Fatal(err)
	}
	if said := run.Done[len(run.Done)-1].Said; said != "Linked Board: contained in Kit 2. Moved Board out of Kit 1." {
		t.Errorf("said %q", said)
	}
	if got := k.linksOf(t, k.board.Asset.ID, k.contains); len(got) != 1 || got[0] != "Kit 2" {
		t.Errorf("the board is in %v", got)
	}

	// Any number, the other way round, from anywhere: each answer links,
	// staying on the step, until done; its progress counts it once.
	several := k.linkFlow(t, "needs", flows.Step{ID: "link", Kind: flows.KindLink, RelationType: k.needs, Outgoing: true, Repeat: true,
		Find: &flows.LinkFind{FindBy: []string{flows.FindList, flows.FindSearch}}})
	runner, run = k.found(t, several)
	if run, err = runner.answer(run, FlowAnswer{Step: "link", AssetIDs: []string{k.kit1.Asset.ID}}); err != nil || stepOf(several, run).ID != "link" {
		t.Fatalf("one of several: on %q, %v", stepOf(several, run).ID, err)
	}
	p, _ = runner.prompt(run, "")
	if p.Relation != "depends on" || len(p.Linked) != 1 || p.Linked[0].Name != "Kit 1" {
		t.Errorf("the prompt after one: %+v", p)
	}
	for _, a := range p.Assets {
		if a.ID == k.kit1.Asset.ID || a.ID == k.board.Asset.ID {
			t.Errorf("offers %s again", a.Name)
		}
	}
	// A search narrows what it offers.
	if p, _ := runner.prompt(run, "Kit 2"); len(p.Assets) != 1 || p.Assets[0].ID != k.kit2.Asset.ID {
		t.Errorf("searched: %+v", p.Assets)
	}
	run, err = runner.answer(run, FlowAnswer{Step: "link", AssetIDs: []string{k.kit2.Asset.ID}, Done: true})
	if err != nil || !run.Finished {
		t.Fatalf("done: %+v %v", run, err)
	}
	if run.Values[flows.ValueLinked] != "Kit 1, Kit 2" || run.Answered != 2 {
		t.Errorf("linked %q, answered %d", run.Values[flows.ValueLinked], run.Answered)
	}
	if got := k.linksOf(t, k.kit1.Asset.ID, k.needs); len(got) != 1 || got[0] != "Board" {
		t.Errorf("Kit 1 is needed by %v", got)
	}
	a, _ := k.srv.store.GetAssetByID(k.board.Asset.ID)
	outgoing := 0
	for _, l := range a.Links {
		if l.TypeID == k.needs && l.Outgoing {
			outgoing++
		}
	}
	if outgoing != 2 {
		t.Errorf("the board needs %d things: %+v", outgoing, a.Links)
	}

	// Not required, done with none passes it over.
	optional := k.linkFlow(t, "optional", flows.Step{ID: "link", Kind: flows.KindLink, RelationType: k.needs, Find: kits})
	runner, run = k.found(t, optional)
	if run, err = runner.answer(run, FlowAnswer{Step: "link", Done: true}); err != nil || !run.Finished {
		t.Errorf("passed over: %+v %v", run, err)
	}
}

// Each link is made through the links endpoint, so each rule links follow
// refuses it as the endpoint does, and leaves the run on the step, saying
// why.
func TestLinkStepRefusesWhatLinkingRefuses(t *testing.T) {
	k := newKitRoom(t)
	st := k.srv.store
	f := k.linkFlow(t, "link", flows.Step{ID: "link", Kind: flows.KindLink, RelationType: k.contains,
		Find: &flows.LinkFind{FindBy: []string{flows.FindList}}})
	// The board holds Kit 1; Kit 2 is archived; and the board already
	// needs a third kit.
	if _, err := st.AddLink(k.board.Asset.ID, k.contains, k.kit1.Asset.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteAsset(k.kit2.Asset.ID); err != nil {
		t.Fatal(err)
	}
	runner, run := k.found(t, f)
	for _, c := range []struct {
		name, id string
		status   int
		says     string
	}{
		{"itself", k.board.Asset.ID, http.StatusBadRequest, "Board: an asset cannot be linked to itself"},
		{"a loop", k.kit1.Asset.ID, http.StatusConflict, "Kit 1: that link would make a loop"},
		{"something archived", k.kit2.Asset.ID, http.StatusConflict, "Kit 2: cannot update an archived asset"},
	} {
		after, err := runner.answer(run, FlowAnswer{Step: "link", AssetIDs: []string{c.id}})
		if statusOf(err) != c.status || err.Error() != c.says {
			t.Errorf("%s: %d %v", c.name, statusOf(err), err)
		}
		if after == nil || stepOf(f, after).ID != "link" {
			t.Fatalf("%s: moved on", c.name)
		}
		run = after
	}

	// Twice the same way: linked already, by hand.
	kit3, _ := st.CreateAsset("Kit 3", []string{k.kits.ID}, nil, nil)
	needs := k.linkFlow(t, "needs", flows.Step{ID: "link", Kind: flows.KindLink, RelationType: k.needs, Outgoing: true, Find: &flows.LinkFind{FindBy: []string{flows.FindList}}})
	if _, err := st.AddLink(k.board.Asset.ID, k.needs, kit3.Asset.ID, true); err != nil {
		t.Fatal(err)
	}
	runner, run = k.found(t, needs)
	if _, err := runner.answer(run, FlowAnswer{Step: "link", AssetID: kit3.Asset.ID}); statusOf(err) != http.StatusConflict || err.Error() != "Kit 3: the assets are already linked that way" {
		t.Errorf("twice: %d %v", statusOf(err), err)
	}
	// Scanning, when the step does not scan, and what it cannot find.
	if _, err := runner.answer(run, FlowAnswer{Step: "link", Codes: []string{"X-1"}}); statusOf(err) != http.StatusBadRequest || !strings.Contains(err.Error(), "does not find by scanning") {
		t.Errorf("a scan: %v", err)
	}
	if _, err := runner.answer(run, FlowAnswer{Step: "link", AssetID: "nothing"}); statusOf(err) != http.StatusBadRequest || !strings.Contains(err.Error(), "not something this step can link to") {
		t.Errorf("nothing: %v", err)
	}
	if _, err := runner.answer(run, FlowAnswer{Step: "link", Create: &FlowNewAsset{Name: "Kit 9"}}); statusOf(err) != http.StatusBadRequest || !strings.Contains(err.Error(), "does not file anything new") {
		t.Errorf("something new: %v", err)
	}
}

// Not listed, the runner files something new to link to, with only a
// name; saving refuses a category it would file into that requires more.
func TestLinkStepFilesSomethingNew(t *testing.T) {
	k := newKitRoom(t)
	st := k.srv.store
	f := k.linkFlow(t, "link", flows.Step{ID: "link", Kind: flows.KindLink, RelationType: k.contains, Required: true,
		Find: &flows.LinkFind{FindBy: []string{flows.FindList}, Categories: []string{k.kits.ID}}, AllowCreate: true, CreateCategory: k.kits.ID})
	runner, run := k.found(t, f)
	if p, _ := runner.prompt(run, ""); p.CreateIn != "Kits" {
		t.Errorf("files into %q", p.CreateIn)
	}
	if _, err := runner.answer(run, FlowAnswer{Step: "link", Create: &FlowNewAsset{Name: "  "}}); statusOf(err) != http.StatusBadRequest {
		t.Errorf("no name: %v", err)
	}
	run, err := runner.answer(run, FlowAnswer{Step: "link", Create: &FlowNewAsset{Name: "Kit 9"}})
	if err != nil || !run.Finished {
		t.Fatalf("filed and linked: %+v %v", run, err)
	}
	made, _ := st.ListAssets(k.kits.ID, "Kit 9")
	if len(made) != 1 {
		t.Fatalf("filed %v", made)
	}
	if got := k.linksOf(t, k.board.Asset.ID, k.contains); len(got) != 1 || got[0] != "Kit 9" {
		t.Errorf("the board is in %v", got)
	}
	if len(run.Done) < 2 || run.Done[len(run.Done)-2].Said != "Filed Kit 9 in Kits." {
		t.Errorf("said %+v", run.Done)
	}

	// A category requiring a field cannot be filed into with only a name.
	boxes, _ := st.CreateCategoryFrom(store.CategoryInput{Name: "Tool boxes"})
	number, _ := st.CreateField("box_number", "Box number", fields.TypeText, nil)
	if err := st.AssignCategoryField(boxes.ID, number.ID, true); err != nil {
		t.Fatal(err)
	}
	d := f.Definition
	d.Steps = append([]flows.Step{}, d.Steps...)
	d.Steps[1].CreateCategory = boxes.ID
	problems, err := k.srv.checkFlow(as(t, k.srv, k.admin.ID), &d)
	if err != nil || len(problems) != 1 || problems[0].Message != "Tool boxes requires Box number, which something new it files, with only a name, would not have" {
		t.Errorf("filing into tool boxes: %v %v", problems, err)
	}
}

// A link that fails part way is tried again from where it failed: what it
// linked, and what it filed, are not linked or filed twice. Confirm lists
// each link to come, and what a link of a single type moves.
func TestLinkStepTriedAgainDoesNotLinkTwice(t *testing.T) {
	k := newKitRoom(t)
	st := k.srv.store
	kit3, _ := st.CreateAsset("Kit 3", []string{k.kits.ID}, nil, nil)
	f := k.linkFlow(t, "named",
		flows.Step{ID: "confirm", Kind: flows.KindConfirm},
		flows.Step{ID: "link", Kind: flows.KindLink, RelationType: k.needs, Outgoing: true, Assets: []string{k.kit1.Asset.ID, k.kit2.Asset.ID}},
		flows.Step{ID: "put", Kind: flows.KindLink, RelationType: k.contains, Assets: []string{kit3.Asset.ID}},
	)
	// The board is in Kit 1, and already depends on Kit 2.
	if _, err := st.AddLink(k.board.Asset.ID, k.contains, k.kit1.Asset.ID, false); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddLink(k.board.Asset.ID, k.needs, k.kit2.Asset.ID, true); err != nil {
		t.Fatal(err)
	}
	runner, run := k.found(t, f)
	p, err := runner.prompt(run, "")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Link Board: depends on Kit 1.", "Link Board: depends on Kit 2.", "Link Board: contained in Kit 3. This moves Board out of Kit 1."}
	if strings.Join(p.Coming, "|") != strings.Join(want, "|") {
		t.Errorf("Confirm says %q", p.Coming)
	}
	run, err = runner.answer(run, FlowAnswer{Step: "confirm"})
	if statusOf(err) != http.StatusConflict || stepOf(f, run).ID != "link" {
		t.Fatalf("Kit 2 twice: %v, on %q", err, stepOf(f, run).ID)
	}
	// Left on the step, it says what is still to do.
	if p, _ := runner.prompt(run, ""); strings.Join(p.Coming, "|") != strings.Join(want[1:], "|") {
		t.Errorf("still to do: %q", p.Coming)
	}
	links, _ := st.GetAssetByID(k.board.Asset.ID)
	var kept string
	for _, l := range links.Links {
		if l.TypeID == k.needs && l.Asset.ID == k.kit2.Asset.ID {
			kept = l.ID
		}
	}
	if _, err := st.RemoveLink(k.board.Asset.ID, kept); err != nil {
		t.Fatal(err)
	}
	run, err = runner.answer(run, FlowAnswer{Step: "link"})
	if err != nil || !run.Finished {
		t.Fatalf("tried again: %+v %v", run, err)
	}
	var said []string
	for _, d := range run.Done {
		said = append(said, d.Said)
	}
	if got := strings.Join(said, "|"); got != "Linked Board: depends on Kit 1.|Linked Board: depends on Kit 2.|Linked Board: contained in Kit 3. Moved Board out of Kit 1." {
		t.Errorf("done: %s", got)
	}

	// Something new filed, and its link refused: tried again, it is linked,
	// not filed again.
	board2, _ := st.CreateAsset("Board 2", []string{k.short.ID}, nil, map[string]string{"owner": "Sam"})
	g := k.linkFlow(t, "new", flows.Step{ID: "link", Kind: flows.KindLink, RelationType: k.contains,
		Find: &flows.LinkFind{FindBy: []string{flows.FindList}}, AllowCreate: true, CreateCategory: k.kits.ID})
	runner = &flowRunner{s: k.srv, r: as(t, k.srv, k.sam.ID), flow: g}
	run, _ = runner.startRun()
	if run, err = runner.answer(run, FlowAnswer{Step: "find", AssetID: board2.Asset.ID}); err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteAsset(board2.Asset.ID); err != nil {
		t.Fatal(err)
	}
	if run, err = runner.answer(run, FlowAnswer{Step: "link", Create: &FlowNewAsset{Name: "Kit 10"}}); err == nil || stepOf(g, run).ID != "link" {
		t.Fatalf("linking something archived: %v", err)
	}
	if _, _, err := st.RestoreAsset(board2.Asset.ID); err != nil {
		t.Fatal(err)
	}
	if run, err = runner.answer(run, FlowAnswer{Step: "link", Create: &FlowNewAsset{Name: "Kit 10"}}); err != nil || !run.Finished {
		t.Fatalf("tried again: %v", err)
	}
	if made, _ := st.ListAssets(k.kits.ID, "Kit 10"); len(made) != 1 {
		t.Errorf("filed %d", len(made))
	}
	if got := k.linksOf(t, board2.Asset.ID, k.contains); len(got) != 1 || got[0] != "Kit 10" {
		t.Errorf("Board 2 is in %v", got)
	}
}

// Saving a Link needs what it uses, where it finds and files; running it,
// the runner's scopes decide what it can find.
func TestLinkStepPermissions(t *testing.T) {
	k := newKitRoom(t)
	st := k.srv.store
	// The kit room is a scope of its own, which Sam is not in.
	room, _ := st.CreateScope("kitroom")
	if _, err := st.SetCategoryScopes(k.kits.ID, []string{room.ID}); err != nil {
		t.Fatal(err)
	}
	link := flows.Step{ID: "link", Kind: flows.KindLink, RelationType: k.contains, AllowCreate: true, CreateCategory: k.kits.ID,
		Find: &flows.LinkFind{FindBy: []string{flows.FindScan, flows.FindList}, Categories: []string{k.kits.ID}}}
	f := k.linkFlow(t, "link", link)

	// A maker who manages flows, and reads and writes in the wood shop
	// only, without scanning.
	makers, _ := st.CreateRole(store.RoleInput{Name: "Makers", Permissions: []string{rbac.PermFlowsManage, rbac.PermAssetsRead, rbac.PermAssetsWrite}})
	maker, _ := st.CreateNamedUser("mo@hackspace.org", "Mo", "UserSecret123!", true)
	if err := st.SetUserRoleAssignments(maker.ID, []models.RoleAssignment{{RoleID: makers.ID, ScopeIDs: []string{k.scope.ID}}}); err != nil {
		t.Fatal(err)
	}
	d := f.Definition
	problems, err := k.srv.checkFlow(as(t, k.srv, maker.ID), &d)
	if err != nil {
		t.Fatal(err)
	}
	var said []string
	for _, p := range problems {
		said = append(said, p.String())
	}
	got := strings.Join(said, "; ")
	for _, want := range []string{"its steps use assets.scan", "step link: it finds in Kits, where you do not hold assets.read", "step link: it files into Kits, where you do not hold assets.write"} {
		if !strings.Contains(got, want) {
			t.Errorf("saving does not say %q: %s", want, got)
		}
	}
	// Nor may it link to a kit named in the flow that the maker may not
	// change.
	named := flows.Definition{Version: flows.Version, CategoryIDs: []string{k.wood.ID}, Steps: []flows.Step{
		{ID: "find", Kind: flows.KindFind, FindBy: []string{flows.FindList}},
		{ID: "link", Kind: flows.KindLink, RelationType: k.contains, Assets: []string{k.kit1.Asset.ID}},
		{ID: "done", Kind: flows.KindDone},
	}}
	if problems, _ := k.srv.checkFlow(as(t, k.srv, maker.ID), &named); len(problems) != 1 || problems[0].Message != "it links to an asset you may not change" {
		t.Errorf("a named kit: %v", problems)
	}

	// Sam finds nothing in the kit room, and cannot link to it.
	runner, run := k.found(t, f)
	if p, _ := runner.prompt(run, ""); len(p.Assets) != 0 {
		t.Errorf("Sam is offered %+v", p.Assets)
	}
	if _, err := runner.answer(run, FlowAnswer{Step: "link", AssetID: k.kit1.Asset.ID}); statusOf(err) != http.StatusBadRequest || !strings.Contains(err.Error(), "not something this step can link to") {
		t.Errorf("a kit in the kit room: %v", err)
	}
	// In it, Sam finds them.
	if err := st.SetUserRoleAssignments(k.sam.ID, []models.RoleAssignment{{RoleID: k.members.ID, ScopeIDs: []string{k.scope.ID, room.ID}}}); err != nil {
		t.Fatal(err)
	}
	runner, run = k.found(t, f)
	if p, _ := runner.prompt(run, ""); len(p.Assets) != 2 {
		t.Errorf("Sam, in the kit room, is offered %+v", p.Assets)
	}
}

// Over the API a Link step is answered with asset_ids and done, and over
// MCP, run_flow answers it the same way; the starter Add to a kit links by
// the type that holds things.
func TestLinkStepOverTheAPIAndMCP(t *testing.T) {
	k := newKitRoom(t)
	srv := k.srv
	admin := sessionCookie(t, srv, k.admin.ID)
	rec := doJSON(t, srv, admin, http.MethodGet, "/api/v1/flow-starters?category_ids="+k.wood.ID, "")
	var starters struct {
		Starters []flows.Starter `json:"starters"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &starters); err != nil || len(starters.Starters) != 3 {
		t.Fatalf("starters: %s", rec.Body)
	}
	kit := starters.Starters[2]
	if kit.Key != "kit" || kit.Definition.Steps[1].RelationType != k.contains {
		t.Fatalf("add to a kit: %+v", kit)
	}
	body, _ := json.Marshal(map[string]any{"slug": "kit", "name": "Add to a kit", "definition": kit.Definition})
	if rec := doJSON(t, srv, admin, http.MethodPost, "/api/v1/flows", string(body)); rec.Code != http.StatusCreated {
		t.Fatalf("saved: %d %s", rec.Code, rec.Body)
	}

	sam := sessionCookie(t, srv, k.sam.ID)
	v := runAPI(t, srv, sam, http.MethodPost, "/api/v1/flows/kit/runs", "", http.StatusCreated)
	runs := "/api/v1/flows/kit/runs/" + v.Run.ID
	v = runAPI(t, srv, sam, http.MethodPost, runs, `{"step":"find","asset_id":"`+k.board.Asset.ID+`"}`, http.StatusOK)
	if v.Prompt.Kind != flows.KindLink || v.Prompt.Relation != "contained in" || len(v.Prompt.Assets) == 0 {
		t.Fatalf("the link: %+v", v.Prompt)
	}
	// Two, for one, are refused as they are.
	v = runAPI(t, srv, sam, http.MethodPost, runs, `{"step":"link","asset_ids":["`+k.kit1.Asset.ID+`","`+k.kit2.Asset.ID+`"]}`, http.StatusBadRequest)
	if v.Prompt.Kind != flows.KindLink || !strings.Contains(v.Error, "one thing only") {
		t.Fatalf("two: %+v %s", v.Prompt, v.Error)
	}
	v = runAPI(t, srv, sam, http.MethodPost, runs, `{"step":"link","asset_ids":["`+k.kit2.Asset.ID+`"]}`, http.StatusOK)
	if !v.Run.Finished || v.Prompt.Text != "Board is in Kit 2." {
		t.Errorf("done: %+v", v.Prompt)
	}
	// Any number, each moving it out of the last, cannot be saved.
	kit.Definition.Steps[1].Repeat = true
	body, _ = json.Marshal(map[string]any{"slug": "kits", "name": "Kits", "definition": kit.Definition})
	if rec := doJSON(t, srv, admin, http.MethodPost, "/api/v1/flows", string(body)); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "links to one thing only") {
		t.Errorf("any number, held in one place: %d %s", rec.Code, rec.Body)
	}

	// Over MCP, as Sam.
	_, secret, err := srv.store.CreatePersonalToken(k.sam.ID, "Sam's assistant", nil)
	if err != nil {
		t.Fatal(err)
	}
	assistant := mcpSession(t, srv, secret)
	var out map[string]any
	structured(t, callTool(t, assistant, "run_flow", map[string]any{"slug": "kit"}), &out)
	runID := out["run"].(map[string]any)["id"].(string)
	res := callTool(t, assistant, "run_flow", map[string]any{"slug": "kit", "run_id": runID, "answer": map[string]any{"step": "find", "asset_id": k.board.Asset.ID}})
	if !strings.Contains(toolText(res), `"asset_ids": [...]`) {
		t.Errorf("the link's summary: %s", toolText(res))
	}
	res = callTool(t, assistant, "run_flow", map[string]any{"slug": "kit", "run_id": runID, "answer": map[string]any{"step": "link", "asset_ids": []string{k.kit1.Asset.ID}, "done": true}})
	structured(t, res, &out)
	if !strings.Contains(toolText(res), "Linked Board: contained in Kit 1.") || out["run"].(map[string]any)["finished"] != true {
		t.Errorf("linked over MCP: %s", toolText(res))
	}
	if got := k.linksOf(t, k.board.Asset.ID, k.contains); len(got) != 1 || got[0] != "Kit 1" {
		t.Errorf("the board is in %v", got)
	}
}
