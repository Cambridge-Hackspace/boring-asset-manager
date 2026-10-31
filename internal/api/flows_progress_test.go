package api

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/flows"
)

// "Step 2 of 3" (#166): the steps that ask something, answered and still
// ahead, as a run goes; a conditional step counts once it holds.
func TestFlowRunProgress(t *testing.T) {
	w := newWoodShop(t)
	leave := flows.Starters([]string{w.short.ID, w.long.ID})[0].Definition
	leave.Steps[1].Fields = []flows.FieldSetting{{Key: "owner", Default: "{user}"}}
	// Asked of long boards only, after the form.
	leave.Steps = append(leave.Steps[:2], append([]flows.Step{
		{ID: "long_only", Kind: flows.KindConfirm, When: &flows.Condition{Value: flows.ValueCategoryID, Op: flows.OpIs, To: w.long.ID}},
	}, leave.Steps[2:]...)...)
	flow := w.saveFlow(t, "leave", "Leave something", leave)
	runner := &flowRunner{s: w.srv, r: as(t, w.srv, w.sam.ID), flow: flow}
	at := func(want, got *FlowProgress) {
		t.Helper()
		if (got == nil) != (want == nil) || (got != nil && *got != *want) {
			t.Errorf("progress %+v, want %+v", got, want)
		}
	}

	// Choose and the form; not the long boards' step, nor Create, Label
	// or Done, which ask nothing.
	run, err := runner.startRun()
	if err != nil {
		t.Fatal(err)
	}
	at(&FlowProgress{Step: 1, Of: 2}, progressOf(&flow.Definition, run))
	// A refused answer moves nothing.
	if _, err := runner.answer(run, FlowAnswer{Step: "kind", CategoryID: w.kitchen.ID}); err == nil {
		t.Fatal("a category not offered was taken")
	}
	run, _ = w.srv.store.GetFlowRun(run.ID)
	at(&FlowProgress{Step: 1, Of: 2}, progressOf(&flow.Definition, run))
	// Long boards bring their step in.
	run, err = runner.answer(run, FlowAnswer{Step: "kind", CategoryID: w.long.ID})
	if err != nil {
		t.Fatal(err)
	}
	at(&FlowProgress{Step: 2, Of: 3}, progressOf(&flow.Definition, run))
	run, err = runner.answer(run, FlowAnswer{Step: "about", Name: "Ash plank", Fields: map[string]string{"owner": "Sam"}})
	if err != nil {
		t.Fatal(err)
	}
	at(&FlowProgress{Step: 3, Of: 3}, progressOf(&flow.Definition, run))
	run, err = runner.answer(run, FlowAnswer{Step: "long_only"})
	if err != nil {
		t.Fatal(err)
	}
	if !run.Finished {
		t.Fatalf("not done: %+v", stepOf(flow, run))
	}
	at(nil, progressOf(&flow.Definition, run))

	// An answer the server gives, as it does a Find for an asset a run is
	// started from, is not a step the person took.
	run, err = runner.startRun()
	if err != nil {
		t.Fatal(err)
	}
	run, err = runner.answer(run, FlowAnswer{Step: "kind", CategoryID: w.short.ID, given: true})
	if err != nil {
		t.Fatal(err)
	}
	at(&FlowProgress{Step: 1, Of: 1}, progressOf(&flow.Definition, run))

	// In the run view's JSON, and left out where there is none.
	body, _ := json.Marshal(FlowRunView{Progress: &FlowProgress{Step: 2, Of: 3}})
	if want := `"progress":{"step":2,"of":3}`; !strings.Contains(string(body), want) {
		t.Errorf("view %s, want %s", body, want)
	}
	body, _ = json.Marshal(FlowRunView{})
	if strings.Contains(string(body), "progress") {
		t.Errorf("view without progress: %s", body)
	}
}
