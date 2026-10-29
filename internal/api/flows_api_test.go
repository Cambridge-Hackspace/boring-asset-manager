package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/flows"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
)

func TestManagingFlowsThroughTheAPI(t *testing.T) {
	w := newWoodShop(t)
	srv := w.srv
	admin := sessionCookie(t, srv, w.admin.ID)
	sam := sessionCookie(t, srv, w.sam.ID)
	expect := func(what string, cookie *http.Cookie, method, path, body string, want int) string {
		t.Helper()
		rec := doJSON(t, srv, cookie, method, path, body)
		if rec.Code != want {
			t.Errorf("%s: %d, want %d: %s", what, rec.Code, want, rec.Body)
		}
		return rec.Body.String()
	}

	var starters struct {
		Starters []flows.Starter `json:"starters"`
	}
	_ = json.Unmarshal([]byte(expect("starters", admin, http.MethodGet, "/api/v1/flow-starters?category_ids="+w.short.ID+","+w.long.ID, "", http.StatusOK)), &starters)
	if len(starters.Starters) != 2 || starters.Starters[0].Definition.CategoryIDs[1] != w.long.ID {
		t.Fatalf("starters: %+v", starters)
	}
	expect("starters, as a member", sam, http.MethodGet, "/api/v1/flow-starters", "", http.StatusForbidden)

	leave, _ := json.Marshal(map[string]any{"slug": "leave", "name": "Leave something", "definition": starters.Starters[0].Definition})
	body := expect("making it", admin, http.MethodPost, "/api/v1/flows", string(leave), http.StatusCreated)
	if !strings.Contains(body, `"slug":"leave"`) || !strings.Contains(body, `"enabled":true`) {
		t.Errorf("made: %s", body)
	}
	expect("the address again", admin, http.MethodPost, "/api/v1/flows", string(leave), http.StatusConflict)
	expect("by a member", sam, http.MethodPost, "/api/v1/flows", string(leave), http.StatusForbidden)

	// Refused with what stops it, by step.
	broken := strings.Replace(string(leave), `"name":"{name}"`, `"name":"{nothing}"`, 1)
	broken = strings.Replace(broken, `"slug":"leave"`, `"slug":"broken"`, 1)
	body = expect("a broken flow", admin, http.MethodPost, "/api/v1/flows", broken, http.StatusBadRequest)
	if !strings.Contains(body, `"step":"file"`) || !strings.Contains(body, "{nothing}") {
		t.Errorf("a broken flow: %s", body)
	}

	// Members see the flows they may run; whoever manages flows sees all.
	if body := expect("listed, as a member", sam, http.MethodGet, "/api/v1/flows", "", http.StatusOK); !strings.Contains(body, `"leave"`) {
		t.Errorf("a member's list: %s", body)
	}
	expect("read, as a member", sam, http.MethodGet, "/api/v1/flows/leave", "", http.StatusOK)
	expect("changed: turned off", admin, http.MethodPut, "/api/v1/flows/leave", `{"slug":"leave","name":"Leave something","enabled":false,"definition":`+mustJSON(starters.Starters[0].Definition)+`}`, http.StatusOK)
	if body := expect("listed, turned off", sam, http.MethodGet, "/api/v1/flows", "", http.StatusOK); strings.Contains(body, `"leave"`) {
		t.Errorf("a member sees a flow turned off: %s", body)
	}
	expect("read, turned off", sam, http.MethodGet, "/api/v1/flows/leave", "", http.StatusNotFound)
	expect("run, turned off", sam, http.MethodPost, "/api/v1/flows/leave/runs", "", http.StatusNotFound)
	// Left out, enabled keeps what it was.
	expect("turned on, renamed", admin, http.MethodPut, "/api/v1/flows/leave", `{"slug":"leave","name":"Leave it","enabled":true,"definition":`+mustJSON(starters.Starters[0].Definition)+`}`, http.StatusOK)
	if body := expect("renamed", admin, http.MethodPut, "/api/v1/flows/leave", `{"slug":"drop","name":"Drop it","definition":`+mustJSON(starters.Starters[0].Definition)+`}`, http.StatusOK); !strings.Contains(body, `"enabled":true`) || !strings.Contains(body, `"slug":"drop"`) {
		t.Errorf("moved: %s", body)
	}
	expect("the old address", admin, http.MethodGet, "/api/v1/flows/leave", "", http.StatusNotFound)
	expect("changing one missing", admin, http.MethodPut, "/api/v1/flows/leave", string(leave), http.StatusNotFound)
	expect("deleting, as a member", sam, http.MethodDelete, "/api/v1/flows/drop", "", http.StatusForbidden)
	expect("deleting", admin, http.MethodDelete, "/api/v1/flows/drop", "", http.StatusOK)
	expect("deleting again", admin, http.MethodDelete, "/api/v1/flows/drop", "", http.StatusNotFound)
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// TestRunningFlowsThroughTheAPI files wood into one of two kinds, so the
// form changes to fit, and collects it by its code.
func TestRunningFlowsThroughTheAPI(t *testing.T) {
	w := newWoodShop(t)
	srv, st := w.srv, w.srv.store
	sam := sessionCookie(t, srv, w.sam.ID)
	leave := w.saveFlow(t, "leave", "Leave something", flows.Starters([]string{w.short.ID, w.long.ID})[0].Definition)
	run := func(cookie *http.Cookie, method, path, body string, want int) FlowRunView {
		t.Helper()
		rec := doJSON(t, srv, cookie, method, path, body)
		if rec.Code != want {
			t.Fatalf("%s %s: %d, want %d: %s", method, path, rec.Code, want, rec.Body)
		}
		var v FlowRunView
		if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
			t.Fatal(err)
		}
		return v
	}

	v := run(sam, http.MethodPost, "/api/v1/flows/leave/runs", "", http.StatusCreated)
	if v.Prompt.Kind != flows.KindChooseCategory || len(v.Prompt.Categories) != 2 || v.Prompt.Categories[0].Name != "Short boards" {
		t.Fatalf("starts with: %+v", v.Prompt)
	}
	path := "/api/v1/flows/leave/runs/" + v.Run.ID
	// Someone else's run is not theirs to see or answer.
	other := userWithPermissions(t, srv, "mo@hackspace.org", rbac.PermFlowsRun, rbac.PermAssetsRead, rbac.PermAssetsWrite)
	if rec := doJSON(t, srv, other, http.MethodGet, path, ""); rec.Code != http.StatusNotFound {
		t.Errorf("another's run: %d", rec.Code)
	}

	// Long boards: the form asks the owner and until.
	v = run(sam, http.MethodPost, path, `{"step":"kind","category_id":"`+w.long.ID+`"}`, http.StatusOK)
	if v.Prompt.Kind != flows.KindForm || len(v.Prompt.Fields) != 2 || !v.Prompt.AskName {
		t.Fatalf("the form: %+v", v.Prompt)
	}
	// An answer to a step it is no longer on is refused, with the run as
	// it is.
	stale := run(sam, http.MethodPost, path, `{"step":"kind","category_id":"`+w.short.ID+`"}`, http.StatusConflict)
	if stale.Error == "" || stale.Prompt.Kind != "" && stale.Prompt.Step != "about" {
		t.Errorf("answered twice: %+v", stale)
	}
	bad := run(sam, http.MethodPost, path, `{"step":"about","name":"Oak","fields":{"owner":"Sam","until":"Friday"}}`, http.StatusBadRequest)
	if !strings.Contains(bad.Error, "Until") || bad.Prompt.Step != "about" {
		t.Errorf("a bad date: %+v", bad)
	}
	v = run(sam, http.MethodPost, path, `{"step":"about","name":"Oak","fields":{"owner":"Sam","until":"2026-11-06"}}`, http.StatusOK)
	if v.Prompt.Kind != flows.KindDone || !v.Run.Finished || v.Prompt.Asset == nil || v.Prompt.Asset.Name != "Oak" || !v.Prompt.ShowLabel {
		t.Fatalf("done: %+v", v)
	}
	if len(v.Prompt.Done) != 2 || v.Prompt.Done[0].Said != "Filed Oak in Long boards." {
		t.Errorf("what it did: %+v", v.Prompt.Done)
	}
	// Finished, the run is over.
	if rec := doJSON(t, srv, sam, http.MethodGet, path, ""); rec.Code != http.StatusNotFound {
		t.Errorf("a finished run: %d", rec.Code)
	}
	oak, _ := st.GetAssetByID(v.Prompt.Asset.ID)
	if oak.FieldValues["until"] != "2026-11-06" {
		t.Errorf("filed: %v", oak.FieldValues)
	}

	// Collect it by scanning its code.
	if _, err := st.UpdateAsset(oak.Asset.ID, "", nil, []models.AssetGeneratorKey{{GeneratorType: "qr", AssignedCode: "WOOD-1"}}, nil); err != nil {
		t.Fatal(err)
	}
	collect := flows.Starters([]string{w.wood.ID})[1].Definition
	collect.Steps[0].FiledBy = leave.ID
	w.saveFlow(t, "collect", "Collect it", collect)
	v = run(sam, http.MethodPost, "/api/v1/flows/collect/runs", "", http.StatusCreated)
	if v.Prompt.Kind != flows.KindFind || len(v.Prompt.Assets) != 1 {
		t.Fatalf("find: %+v", v.Prompt)
	}
	path = "/api/v1/flows/collect/runs/" + v.Run.ID
	if v := run(sam, http.MethodGet, path+"?search=pine", "", http.StatusOK); len(v.Prompt.Assets) != 0 {
		t.Errorf("searched: %+v", v.Prompt.Assets)
	}
	if v := run(sam, http.MethodPost, path, `{"step":"find","code":"NOPE"}`, http.StatusBadRequest); !strings.Contains(v.Error, "NOPE") {
		t.Errorf("an unknown code: %+v", v)
	}
	v = run(sam, http.MethodPost, path, `{"step":"find","code":"WOOD-1"}`, http.StatusOK)
	if v.Prompt.Kind != flows.KindConfirm || len(v.Prompt.Coming) != 1 || !strings.Contains(v.Prompt.Coming[0], "Archive Oak") {
		t.Fatalf("confirm: %+v", v.Prompt)
	}
	v = run(sam, http.MethodPost, path, `{"step":"confirm"}`, http.StatusOK)
	if !v.Run.Finished {
		t.Fatalf("collected: %+v", v)
	}
	if a, _ := st.GetAssetByID(oak.Asset.ID); a.Asset.ArchivedAt == nil {
		t.Error("not archived")
	}

	// Giving a run up.
	v = run(sam, http.MethodPost, "/api/v1/flows/collect/runs", "", http.StatusCreated)
	if rec := doJSON(t, srv, sam, http.MethodDelete, "/api/v1/flows/collect/runs/"+v.Run.ID, ""); rec.Code != http.StatusOK {
		t.Errorf("giving up: %d", rec.Code)
	}

	// An API token holding roles is no one, and runs nothing.
	secret := tokenWith(t, srv, w.admin.ID, "Script", rbac.PermFlowsRun, rbac.PermAssetsWrite)
	rec := doBearer(t, srv, secret, http.MethodPost, "/api/v1/flows/collect/runs")
	if rec.Code != http.StatusForbidden {
		t.Errorf("a token: %d %s", rec.Code, rec.Body)
	}
}

// TestFlowsOverMCP: an assistant manages a flow, and runs one as its
// person.
func TestFlowsOverMCP(t *testing.T) {
	w := newWoodShop(t)
	srv := w.srv
	admin := mcpSession(t, srv, tokenWith(t, srv, w.admin.ID, "Admin assistant", rbac.PermFlowsManage, rbac.PermAssetsRead, rbac.PermAssetsWrite))
	var out map[string]any
	structured(t, callTool(t, admin, "flow_starters", map[string]any{"category_ids": []string{w.short.ID}}), &out)
	starters, _ := out["starters"].([]any)
	if len(starters) != 2 {
		t.Fatalf("starters: %v", out)
	}
	def := starters[0].(map[string]any)["definition"]
	structured(t, callTool(t, admin, "save_flow", map[string]any{"new_slug": "leave", "name": "Leave something", "definition": def}), &out)
	structured(t, callTool(t, admin, "save_flow", map[string]any{"slug": "leave", "name": "Leave it here"}), &out)
	if f := out["flow"].(map[string]any); f["name"] != "Leave it here" || f["slug"] != "leave" {
		t.Errorf("changed only the name: %v", f)
	}
	if msg := toolError(t, admin, "save_flow", map[string]any{"new_slug": "bad", "name": "Bad", "definition": map[string]any{"version": 1, "category_ids": []string{w.short.ID}, "steps": []any{}}}); !strings.Contains(msg, "at least one step") {
		t.Errorf("a flow with no steps: %s", msg)
	}
	structured(t, callTool(t, admin, "list_flows", nil), &out)
	if flows, _ := out["flows"].([]any); len(flows) != 1 {
		t.Errorf("listed: %v", out)
	}

	// Sam's assistant runs it as Sam.
	_, secret, err := srv.store.CreatePersonalToken(w.sam.ID, "Sam's assistant", nil)
	if err != nil {
		t.Fatal(err)
	}
	assistant := mcpSession(t, srv, secret)
	structured(t, callTool(t, assistant, "run_flow", map[string]any{"slug": "leave"}), &out)
	runID := out["run"].(map[string]any)["id"].(string)
	res := callTool(t, assistant, "run_flow", map[string]any{"slug": "leave", "run_id": runID, "answer": map[string]any{"step": "about", "name": "Beech", "fields": map[string]string{"owner": "Sam"}}})
	structured(t, res, &out)
	if !strings.Contains(toolText(res), "Filed Beech in Short boards.") || out["run"].(map[string]any)["finished"] != true {
		t.Errorf("run over MCP: %s", toolText(res))
	}
	found, _ := srv.store.ListAssets(w.short.ID, "Beech")
	if len(found) != 1 {
		t.Fatalf("filed over MCP: %v", found)
	}
	events, _, _ := srv.store.ListAssetEvents(found[0].Asset.ID, "", 5)
	if b, _ := json.Marshal(events); !strings.Contains(string(b), "flow “Leave it here”") {
		t.Errorf("history: %s", b)
	}
	if res := callTool(t, admin, "delete_flow", map[string]any{"slug": "leave"}); res.IsError {
		t.Errorf("delete_flow: %s", toolText(res))
	}
}

// TestAFlowsPosterCode: the QR code on a flow's poster opens /f/<slug>, on
// the server's own address.
func TestAFlowsPosterCode(t *testing.T) {
	w := newWoodShop(t)
	w.saveFlow(t, "leave", "Leave something", flows.Starters([]string{w.short.ID})[0].Definition)
	w.srv.cfg.Server.BaseURL = "https://bam.example.org/"
	if got := w.srv.flowURL(httptest.NewRequest(http.MethodGet, "/", nil), "leave"); got != "https://bam.example.org/f/leave" {
		t.Errorf("its address: %s", got)
	}
	rec := doJSON(t, w.srv, sessionCookie(t, w.srv, w.sam.ID), http.MethodGet, "/api/v1/flows/leave/qr.svg", "")
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/svg+xml" || !strings.HasPrefix(rec.Body.String(), "<svg") || !strings.Contains(rec.Body.String(), "h1v1h-1z") {
		t.Errorf("its code: %d %s %.80s", rec.Code, rec.Header().Get("Content-Type"), rec.Body)
	}
	if rec := doJSON(t, w.srv, sessionCookie(t, w.srv, w.sam.ID), http.MethodGet, "/api/v1/flows/nope/qr.svg", ""); rec.Code != http.StatusNotFound {
		t.Errorf("no such flow: %d", rec.Code)
	}
}

// TestThingsPastTheirDateAreListed: what a flow filed and left past its
// date to collect by comes back with the flow, and is marked when found.
func TestThingsPastTheirDateAreListed(t *testing.T) {
	w := newWoodShop(t)
	srv, st := w.srv, w.srv.store
	leave := flows.Starters([]string{w.long.ID})[0].Definition
	leave.LeaveUntil = "until"
	lf := w.saveFlow(t, "leave", "Leave something", leave)
	oak, _ := st.CreateAsset("Oak", []string{w.long.ID}, nil, map[string]string{"owner": "Sam", "until": "2020-01-01"})
	pine, _ := st.CreateAsset("Pine", []string{w.long.ID}, nil, map[string]string{"owner": "Sam", "until": "2999-01-01"})
	for _, a := range []*models.AssetWithDetails{oak, pine} {
		if err := st.RecordFiling(lf.ID, a.Asset.ID, w.sam.ID); err != nil {
			t.Fatal(err)
		}
	}
	sam := sessionCookie(t, srv, w.sam.ID)
	body := doJSON(t, srv, sam, http.MethodGet, "/api/v1/flows/leave", "").Body.String()
	if !strings.Contains(body, `"overdue":[{"asset_id":"`+oak.Asset.ID+`","name":"Oak","until":"2020-01-01"}]`) {
		t.Errorf("the flow: %s", body)
	}
	if body := doJSON(t, srv, sam, http.MethodGet, "/api/v1/flows", "").Body.String(); !strings.Contains(body, `"`+lf.ID+`":[{"asset_id":"`+oak.Asset.ID+`"`) {
		t.Errorf("the list: %s", body)
	}
	collect := flows.Starters([]string{w.wood.ID})[1].Definition
	collect.Steps[0].FiledBy = lf.ID
	w.saveFlow(t, "collect", "Collect it", collect)
	rec := doJSON(t, srv, sam, http.MethodPost, "/api/v1/flows/collect/runs", "")
	var v FlowRunView
	_ = json.Unmarshal(rec.Body.Bytes(), &v)
	marked := map[string]string{}
	for _, a := range v.Prompt.Assets {
		marked[a.Name] = a.Overdue
	}
	if marked["Oak"] != "2020-01-01" || marked["Pine"] != "" || len(marked) != 2 {
		t.Errorf("found: %v", marked)
	}
}

// TestStartingAFlowFromAnAsset: scanning what Leave something filed offers
// Collect it, to someone who may run it, and starting it there skips the
// finding.
func TestStartingAFlowFromAnAsset(t *testing.T) {
	w := newWoodShop(t)
	srv, st := w.srv, w.srv.store
	lf := w.saveFlow(t, "leave", "Leave something", flows.Starters([]string{w.short.ID})[0].Definition)
	collect := flows.Starters([]string{w.wood.ID})[1].Definition
	collect.Steps[0].FiledBy = lf.ID
	w.saveFlow(t, "collect", "Collect it", collect)
	oak, _ := st.CreateAsset("Oak", []string{w.short.ID}, []models.AssetGeneratorKey{{GeneratorType: "qr", AssignedCode: "WOOD-9"}}, map[string]string{"owner": "Sam"})
	unfiled, _ := st.CreateAsset("Birch", []string{w.short.ID}, nil, map[string]string{"owner": "Sam"})
	if err := st.RecordFiling(lf.ID, oak.Asset.ID, w.sam.ID); err != nil {
		t.Fatal(err)
	}
	sam := sessionCookie(t, srv, w.sam.ID)

	var scanned ScanResponse
	_ = json.Unmarshal(doJSON(t, srv, sam, http.MethodGet, "/api/v1/scan/WOOD-9", "").Body.Bytes(), &scanned)
	if len(scanned.Asset.Flows) != 1 || scanned.Asset.Flows[0].Slug != "collect" {
		t.Errorf("scanned: %+v", scanned.Asset.Flows)
	}
	if body := doJSON(t, srv, sam, http.MethodGet, "/api/v1/assets/"+unfiled.Asset.ID+"/flows", "").Body.String(); body != `{"flows":[]}`+"\n" {
		t.Errorf("what Leave something did not file: %s", body)
	}
	// Someone who may not run it is offered nothing.
	reader := userWithPermissions(t, srv, "rae@hackspace.org", rbac.PermAssetsRead, rbac.PermAssetsScan)
	if body := doJSON(t, srv, reader, http.MethodGet, "/api/v1/scan/WOOD-9", "").Body.String(); strings.Contains(body, `"flows"`) {
		t.Errorf("a reader is offered flows: %s", body)
	}

	rec := doJSON(t, srv, sam, http.MethodPost, "/api/v1/flows/collect/runs", `{"asset_id":"`+oak.Asset.ID+`"}`)
	var v FlowRunView
	_ = json.Unmarshal(rec.Body.Bytes(), &v)
	if rec.Code != http.StatusCreated || v.Prompt.Kind != flows.KindConfirm || v.Prompt.Asset == nil || v.Prompt.Asset.ID != oak.Asset.ID {
		t.Fatalf("started from the oak: %d %+v", rec.Code, v)
	}
	// From what it would not find, it starts, and says so.
	rec = doJSON(t, srv, sam, http.MethodPost, "/api/v1/flows/collect/runs", `{"asset_id":"`+unfiled.Asset.ID+`"}`)
	if _ = json.Unmarshal(rec.Body.Bytes(), &v); rec.Code != http.StatusBadRequest || v.Prompt.Kind != flows.KindFind {
		t.Errorf("started from the birch: %d %+v", rec.Code, v)
	}
	if rec := doJSON(t, srv, sam, http.MethodPost, "/api/v1/flows/leave/runs", `{"asset_id":"`+oak.Asset.ID+`"}`); rec.Code != http.StatusBadRequest {
		t.Errorf("a flow that does not start by finding: %d", rec.Code)
	}
}
