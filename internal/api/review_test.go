package api

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/reports"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// reviewFixture is a wood shop whose Tools category, in the shop scope,
// uses review states, and a Laser category in another scope that does
// too.
type reviewFixture struct {
	srv          *Server
	admin        *http.Cookie
	adminUser    *models.User
	tools, laser *models.Category
}

func newReviewFixture(t *testing.T) *reviewFixture {
	t.Helper()
	srv, admin, _ := setupTestServer(t)
	st := srv.store
	f := &reviewFixture{srv: srv, admin: sessionCookie(t, srv, admin.ID), adminUser: admin}
	for _, c := range []struct {
		into  **models.Category
		name  string
		scope string
	}{{&f.tools, "Tools", "shop"}, {&f.laser, "Laser", "laser"}} {
		cat, err := st.CreateCategoryFrom(store.CategoryInput{Name: c.name})
		if err != nil {
			t.Fatal(err)
		}
		scope, err := st.CreateScope(c.scope)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := st.SetCategoryScopes(cat.ID, []string{scope.ID}); err != nil {
			t.Fatal(err)
		}
		*c.into = cat
	}
	for _, c := range []*models.Category{f.tools, f.laser} {
		if rec := doJSON(t, srv, f.admin, http.MethodPut, "/api/v1/categories/"+c.ID+"/review", `{"enabled":true,"months":12}`); rec.Code != http.StatusOK {
			t.Fatalf("turning review on: %d %s", rec.Code, rec.Body)
		}
	}
	return f
}

func (f *reviewFixture) asset(t *testing.T, name string, cat *models.Category) string {
	t.Helper()
	a, err := f.srv.store.CreateAsset(name, []string{cat.ID}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	return a.Asset.ID
}

func reviewOf(t *testing.T, srv *Server, who *http.Cookie, id string) *models.AssetReview {
	t.Helper()
	rec := doJSON(t, srv, who, http.MethodGet, "/api/v1/assets/"+id, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("reading %s: %d %s", id, rec.Code, rec.Body)
	}
	var out struct{ Asset models.AssetWithDetails }
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out.Asset.Review
}

// Each move needs its permission in the asset's scopes, and verifying
// one's own confirmation is refused.
func TestReviewMovesNeedTheirPermissionInTheAssetsScopes(t *testing.T) {
	f := newReviewFixture(t)
	srv := f.srv
	saw, cutter := f.asset(t, "Saw", f.tools), f.asset(t, "Cutter", f.laser)
	_, lead := namedHolder(t, srv, "Lead", nil, "shop:"+rbac.PermAssetsRead, "laser:"+rbac.PermAssetsRead, "shop:"+rbac.PermAssetsConfirm, "shop:"+rbac.PermAssetsVerify)
	_, keeper := namedHolder(t, srv, "Keeper", nil, rbac.PermAssetsRead, rbac.PermAssetsVerify)
	_, reader := namedHolder(t, srv, "Reader", nil, rbac.PermAssetsRead)

	move := func(who *http.Cookie, id, body string) int {
		return doJSON(t, srv, who, http.MethodPost, "/api/v1/assets/"+id+"/review", body).Code
	}
	if code := move(reader, saw, `{"move":"confirm"}`); code != http.StatusForbidden {
		t.Errorf("a reader confirmed: %d", code)
	}
	if code := move(lead, cutter, `{"move":"confirm"}`); code != http.StatusForbidden {
		t.Errorf("confirming outside one's scope: %d", code)
	}
	if code := move(keeper, saw, `{"move":"confirm"}`); code != http.StatusForbidden {
		t.Errorf("verify alone confirmed: %d", code)
	}
	if code := move(lead, saw, `{"move":"confirm","note":"checked"}`); code != http.StatusOK {
		t.Fatalf("confirming in one's scope: %d", code)
	}
	if code := move(lead, saw, `{"move":"verify"}`); code != http.StatusForbidden {
		t.Errorf("verifying one's own confirmation: %d", code)
	}
	if code := move(keeper, saw, `{"move":"verify"}`); code != http.StatusOK {
		t.Errorf("someone else verifying: %d", code)
	}
	if code := move(keeper, saw, `{"move":"return"}`); code != http.StatusBadRequest {
		t.Errorf("returning without a note: %d", code)
	}
	if code := move(keeper, saw, `{"move":"return","note":"blade missing"}`); code != http.StatusOK {
		t.Errorf("returning: %d", code)
	}
	if code := move(lead, saw, `{"move":"sideways"}`); code != http.StatusBadRequest {
		t.Errorf("an unknown move: %d", code)
	}

	// Who confirmed it shows to those who track it, and to themselves.
	if code := move(lead, saw, `{"move":"confirm"}`); code != http.StatusOK {
		t.Fatal(code)
	}
	if r := reviewOf(t, srv, reader, saw); r.ConfirmedBy != nil || r.SetBy != nil {
		t.Errorf("a reader sees who: %+v", r)
	}
	if r := reviewOf(t, srv, lead, saw); r.ConfirmedBy == nil || r.ConfirmedBy.Name != "Lead" || r.ConfirmedBy.Email != "" {
		t.Errorf("the lead sees their own confirmation as %+v", r.ConfirmedBy)
	}
	if r := reviewOf(t, srv, f.admin, saw); r.ConfirmedBy == nil || r.ConfirmedBy.Name != "Lead" {
		t.Errorf("an admin sees %+v", r.ConfirmedBy)
	}

	// The list filters by state, and the summary counts what waits.
	rec := doJSON(t, srv, f.admin, http.MethodGet, "/api/v1/assets?review=draft", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Cutter") || strings.Contains(rec.Body.String(), `"Saw"`) {
		t.Errorf("the draft filter: %d %s", rec.Code, rec.Body)
	}
	if rec := doJSON(t, srv, f.admin, http.MethodGet, "/api/v1/assets?review=nope", ""); rec.Code != http.StatusBadRequest {
		t.Errorf("an unknown filter: %d", rec.Code)
	}
	var sum ReviewSummary
	rec = doJSON(t, srv, lead, http.MethodGet, "/api/v1/review", "")
	if err := json.Unmarshal(rec.Body.Bytes(), &sum); err != nil || sum.Drafts != 0 || sum.ToVerify != 1 {
		t.Errorf("the lead's summary: %+v %v", sum, err)
	}
	rec = doJSON(t, srv, f.admin, http.MethodGet, "/api/v1/review", "")
	if err := json.Unmarshal(rec.Body.Bytes(), &sum); err != nil || sum.Drafts != 1 || sum.ToVerify != 1 {
		t.Errorf("the admin's summary: %+v %v", sum, err)
	}
}

// Creating as confirmed needs assets.confirm in the categories' scopes;
// categories' settings need categories.manage; the instance's need
// settings.manage.
func TestFilingAsConfirmedAndSettings(t *testing.T) {
	f := newReviewFixture(t)
	srv := f.srv
	_, writer := namedHolder(t, srv, "Writer", nil, rbac.PermAssetsRead, rbac.PermAssetsWrite)
	body := `{"name":"Plane","category_ids":["` + f.tools.ID + `"],"confirm":true}`
	if rec := doJSON(t, srv, writer, http.MethodPost, "/api/v1/assets", body); rec.Code != http.StatusForbidden {
		t.Errorf("confirming without assets.confirm: %d", rec.Code)
	}
	rec := doJSON(t, srv, f.admin, http.MethodPost, "/api/v1/assets", body)
	var made struct{ Asset models.AssetWithDetails }
	if err := json.Unmarshal(rec.Body.Bytes(), &made); err != nil || rec.Code != http.StatusCreated || made.Asset.Review.State != models.ReviewConfirmed {
		t.Errorf("an admin filing as confirmed: %d %s", rec.Code, rec.Body)
	}
	rec = doJSON(t, srv, writer, http.MethodPost, "/api/v1/assets", `{"name":"Chisel","category_ids":["`+f.tools.ID+`"]}`)
	if err := json.Unmarshal(rec.Body.Bytes(), &made); err != nil || made.Asset.Review.State != models.ReviewDraft {
		t.Errorf("a writer's asset: %d %s", rec.Code, rec.Body)
	}

	if rec := doJSON(t, srv, writer, http.MethodPut, "/api/v1/categories/"+f.tools.ID+"/review", `{"enabled":false}`); rec.Code != http.StatusForbidden {
		t.Errorf("a writer changed review settings: %d", rec.Code)
	}
	if rec := doJSON(t, srv, f.admin, http.MethodPut, "/api/v1/categories/"+f.tools.ID+"/review", `{"months":-1}`); rec.Code != http.StatusBadRequest {
		t.Errorf("negative months: %d", rec.Code)
	}
	if rec := doJSON(t, srv, f.admin, http.MethodPut, "/api/v1/categories/"+f.tools.ID+"/review", `{"on_edit":"shrug"}`); rec.Code != http.StatusBadRequest {
		t.Errorf("an unknown on_edit: %d", rec.Code)
	}
	if rec := doJSON(t, srv, writer, http.MethodGet, "/api/v1/settings/review", ""); rec.Code != http.StatusForbidden {
		t.Errorf("a writer read the instance's settings: %d", rec.Code)
	}
	rec = doJSON(t, srv, f.admin, http.MethodPut, "/api/v1/settings/review", `{"assistant_drafts":false,"due_days":30}`)
	var set store.ReviewSettings
	if err := json.Unmarshal(rec.Body.Bytes(), &set); err != nil || rec.Code != http.StatusOK || set.AssistantDrafts || set.DueDays != 30 {
		t.Errorf("saving the instance's settings: %d %s", rec.Code, rec.Body)
	}
	if rec := doJSON(t, srv, f.admin, http.MethodPut, "/api/v1/settings/review", `{"assistant_drafts":true,"due_days":400}`); rec.Code != http.StatusBadRequest {
		t.Errorf("400 days: %d", rec.Code)
	}
}

// Through MCP, an assistant's changes land as drafts for a person to
// accept or reject; its own confirmations are refused; the review tools
// and the search filter work.
func TestAssistantChangesOverMCPLandAsDrafts(t *testing.T) {
	f := newReviewFixture(t)
	srv := f.srv
	saw := f.asset(t, "Saw", f.tools)
	if rec := doJSON(t, srv, f.admin, http.MethodPost, "/api/v1/assets/"+saw+"/review", `{"move":"confirm"}`); rec.Code != http.StatusOK {
		t.Fatal(rec.Code)
	}
	secret := tokenWith(t, srv, f.adminUser.ID, "assistant", rbac.PermAssetsRead, rbac.PermAssetsWrite, rbac.PermAssetsConfirm, rbac.PermAssetsVerify)
	cs := mcpSession(t, srv, secret)

	if res := callTool(t, cs, "confirm_asset", map[string]any{"asset": saw}); !res.IsError {
		t.Errorf("an assistant confirmed: %s", toolText(res))
	}
	if res := callTool(t, cs, "update_asset", map[string]any{"asset": saw, "name": "Band saw"}); res.IsError {
		t.Fatalf("updating: %s", toolText(res))
	}
	r := reviewOf(t, srv, f.admin, saw)
	if r.State != models.ReviewDraft || r.Proposal == nil || r.Proposal.Via != "test client" || r.Proposal.PriorState != models.ReviewConfirmed {
		t.Fatalf("after the assistant's change: %+v %+v", r, r.Proposal)
	}
	var found struct {
		Assets []struct {
			Name   string
			Review *struct {
				State    string
				Proposed bool
			}
		}
	}
	structured(t, callTool(t, cs, "search_assets", map[string]any{"review": "proposed"}), &found)
	if len(found.Assets) != 1 || found.Assets[0].Name != "Band saw" || found.Assets[0].Review == nil || !found.Assets[0].Review.Proposed {
		t.Errorf("searching for proposals: %+v", found)
	}

	// A reader cannot decide; someone who edits it can.
	_, reader := namedHolder(t, srv, "Reader", nil, rbac.PermAssetsRead)
	if rec := doJSON(t, srv, reader, http.MethodPost, "/api/v1/assets/"+saw+"/proposal/reject", ""); rec.Code != http.StatusForbidden {
		t.Errorf("a reader rejected: %d", rec.Code)
	}
	rec := doJSON(t, srv, f.admin, http.MethodPost, "/api/v1/assets/"+saw+"/proposal/reject", `{"note":"keep the old name"}`)
	var got struct{ Asset models.AssetWithDetails }
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || rec.Code != http.StatusOK || got.Asset.Asset.Name != "Saw" || got.Asset.Review.State != models.ReviewConfirmed {
		t.Errorf("rejecting: %d %s", rec.Code, rec.Body)
	}
	if rec := doJSON(t, srv, f.admin, http.MethodPost, "/api/v1/assets/"+saw+"/proposal/accept", ""); rec.Code != http.StatusConflict {
		t.Errorf("accepting with nothing waiting: %d", rec.Code)
	}

	// An asset it adds is a draft, accepted and confirmed by a person.
	res := callTool(t, cs, "create_asset", map[string]any{"assets": []map[string]any{{"name": "Router", "category_ids": []string{f.tools.ID}, "confirm": true}}})
	if res.IsError {
		t.Fatalf("creating: %s", toolText(res))
	}
	var made struct{ Created []struct{ ID string } }
	structured(t, res, &made)
	router := made.Created[0].ID
	if r := reviewOf(t, srv, f.admin, router); r.State != models.ReviewDraft || r.Proposal == nil || !r.Proposal.New {
		t.Fatalf("made through the assistant: %+v", r)
	}
	rec = doJSON(t, srv, f.admin, http.MethodPost, "/api/v1/assets/"+router+"/proposal/accept", `{"confirm":true}`)
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || rec.Code != http.StatusOK || got.Asset.Review.State != models.ReviewConfirmed || got.Asset.Review.Proposal != nil {
		t.Errorf("accepting and confirming: %d %s", rec.Code, rec.Body)
	}

	// return_to_draft works through an assistant, with a note.
	if res := callTool(t, cs, "return_to_draft", map[string]any{"asset": router}); !res.IsError {
		t.Errorf("returned without a note: %s", toolText(res))
	}
	if res := callTool(t, cs, "return_to_draft", map[string]any{"asset": router, "note": "wrong bit"}); res.IsError || !strings.Contains(toolText(res), "back to draft") {
		t.Errorf("returning: %s", toolText(res))
	}

	// With assistants' changes taken as the person's, it confirms.
	if rec := doJSON(t, srv, f.admin, http.MethodPut, "/api/v1/settings/review", `{"assistant_drafts":false,"due_days":14}`); rec.Code != http.StatusOK {
		t.Fatal(rec.Code)
	}
	if res := callTool(t, cs, "confirm_asset", map[string]any{"asset": router}); res.IsError {
		t.Errorf("confirming with drafts off: %s", toolText(res))
	}
	if res := callTool(t, cs, "verify_asset", map[string]any{"asset": router}); !res.IsError {
		t.Errorf("the assistant's token verified its own confirmation: %s", toolText(res))
	}
}

// Imports land as drafts, unless the importer may confirm and chooses to;
// reports bind the review as columns.
func TestImportsLandAsDraftsAndReportsBindReview(t *testing.T) {
	f := newReviewFixture(t)
	srv := f.srv
	_, writer := namedHolder(t, srv, "Writer", nil, rbac.PermAssetsRead, rbac.PermAssetsWrite)
	importWith := func(who *http.Cookie, file string, confirm bool) *httptest.ResponseRecorder {
		t.Helper()
		var body bytes.Buffer
		mw := multipart.NewWriter(&body)
		fw, err := mw.CreateFormFile("file", "assets.csv")
		if err != nil {
			t.Fatal(err)
		}
		_, _ = fw.Write([]byte(file))
		_ = mw.WriteField("mapping", `["name","categories"]`)
		if confirm {
			_ = mw.WriteField("confirm", "true")
		}
		_ = mw.Close()
		return do(t, srv, who, http.MethodPost, "/api/v1/assets/import", mw.FormDataContentType(), &body)
	}
	if rec := importWith(writer, "name,categories\nMallet,Tools\n", true); rec.Code != http.StatusForbidden {
		t.Errorf("confirming without assets.confirm: %d %s", rec.Code, rec.Body)
	}
	if rec := importWith(writer, "name,categories\nMallet,Tools\n", false); rec.Code != http.StatusOK {
		t.Fatalf("importing: %d %s", rec.Code, rec.Body)
	}
	if rec := importWith(f.admin, "name,categories\nClamp,Tools\n", true); rec.Code != http.StatusOK {
		t.Fatalf("importing as confirmed: %d %s", rec.Code, rec.Body)
	}

	columns := `[{"heading":"Name","binding":"{{asset.name}}"},{"heading":"Review","binding":"{{asset.review_state}}|{{asset.review_due}}|{{asset.confirmed_by}}"}]`
	rec := doJSON(t, srv, f.admin, http.MethodPost, "/api/v1/reports/preview", `{"spec":{"filter":{},"sections":[{"type":"table","columns":`+columns+`,"sort_by":0}]}}`)
	var out struct {
		Result reports.Result `json:"result"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("report: %d %s", rec.Code, rec.Body)
	}
	var rows []string
	for _, g := range out.Result.Sections[0].Groups {
		for _, r := range g.Rows {
			rows = append(rows, strings.Join(r.Cells, "="))
		}
	}
	due := time.Now().UTC().AddDate(1, 0, 0).Format("2006-01-02")
	// Confirmed by the admin, who has no name: shown as unnamed.
	if got, want := strings.Join(rows, ";"), "Clamp=Confirmed|"+due+"|"+UnnamedUser+";Mallet=Draft||"; got != want {
		t.Errorf("report rows\n got %q\nwant %q", got, want)
	}
}

// A hook can fire on an asset's review falling due.
func TestHooksFireOnReviewsFallingDue(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/hooks", `{"key":"due","label":"Due","triggers":[{"type":"event","kinds":["asset.review_due"]}]}`)
	if rec.Code != http.StatusCreated {
		t.Errorf("a hook on reviews falling due: %d %s", rec.Code, rec.Body)
	}
}
