package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/fields"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/flows"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
)

// withGrain gives all wood a grain to choose, oak or pine, not required.
func (w *woodShop) withGrain(t *testing.T) {
	t.Helper()
	grain, err := w.srv.store.CreateField("grain", "Grain", fields.TypeSelect, json.RawMessage(`{"choices":[{"label":"Oak","value":"oak"},{"label":"Pine","value":"pine"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := w.srv.store.AssignCategoryField(w.wood.ID, grain.ID, false); err != nil {
		t.Fatal(err)
	}
}

// runAPI runs a flow through the API as a cookie's person, expecting a
// status, and reads the run's view.
func runAPI(t *testing.T, srv *Server, cookie *http.Cookie, method, path, body string, want int) FlowRunView {
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

// A form asks each type of question, its answers become the run's values,
// and a Set fields step before Create files the new asset with fields set
// from them, its photo for the page to add; one after Create updates it.
// Confirm says what each will set.
func TestSetFieldsFilesAndUpdatesTheAsset(t *testing.T) {
	w := newWoodShop(t)
	w.withGrain(t)
	srv := w.srv
	choices := []flows.Option{{Value: "oak", Label: "Oak"}, {Value: "pine", Label: "Pine"}}
	w.saveFlow(t, "leave", "Leave", flows.Definition{Version: flows.Version, CategoryIDs: []string{w.short.ID}, Steps: []flows.Step{
		{ID: "about", Kind: flows.KindForm, Questions: []flows.Question{
			{Key: "name", Label: "What is it?", Required: true},
			{Key: "owner", Label: "Whose?", Default: "{user}"},
			{Key: "note", Label: "Anything else?", Type: flows.QuestionLongText},
			{Key: "boards", Label: "How many?", Type: flows.QuestionNumber},
			{Key: "back", Label: "Back by", Type: flows.QuestionDate},
			{Key: "fragile", Label: "Fragile?", Type: flows.QuestionYesNo, Default: "no"},
			{Key: "wood", Label: "Which wood?", Type: flows.QuestionChoice, Options: choices},
			{Key: "photo", Label: "A photo", Type: flows.QuestionPhoto},
		}},
		{ID: "set", Kind: flows.KindSetFields, Fields: []flows.FieldValue{{Field: "owner", Value: "{owner}"}, {Field: "grain", Value: "{wood}"}}, Photo: "{photo}"},
		{ID: "confirm", Kind: flows.KindConfirm},
		{ID: "file", Kind: flows.KindCreate, Name: "{name}"},
		{ID: "set_2", Kind: flows.KindSetFields, Fields: []flows.FieldValue{{Field: "owner", Value: "{owner} ({boards}, fragile: {fragile})"}}},
		{ID: "done", Kind: flows.KindDone},
	}})
	sam := sessionCookie(t, srv, w.sam.ID)
	v := runAPI(t, srv, sam, http.MethodPost, "/api/v1/flows/leave/runs", "", http.StatusCreated)
	if q := v.Prompt.Questions; len(q) != 8 || q[1].Default != "Sam" || q[6].Type != flows.QuestionChoice || len(q[6].Options) != 2 || q[7].Type != flows.QuestionPhoto {
		t.Fatalf("the form: %+v", v.Prompt.Questions)
	}
	path := "/api/v1/flows/leave/runs/" + v.Run.ID
	for answers, why := range map[string]string{
		`{"name":"Oak","boards":"two"}`:    "How many? is a number",
		`{"name":"Oak","back":"Friday"}`:   "Back by is a date",
		`{"name":"Oak","fragile":"maybe"}`: "Fragile? is yes or no",
		`{"name":"Oak","wood":"teak"}`:     "Which wood? is one of its choices",
		`{"owner":"Sam"}`:                  "What is it? is required",
	} {
		bad := runAPI(t, srv, sam, http.MethodPost, path, `{"step":"about","answers":`+answers+`}`, http.StatusBadRequest)
		if !strings.Contains(bad.Error, why) || bad.Prompt.Step != "about" {
			t.Errorf("%s: %q, want %q", answers, bad.Error, why)
		}
	}
	v = runAPI(t, srv, sam, http.MethodPost, path, `{"step":"about","answers":{"name":"Oak","owner":"Sam","note":"one\ntwo","boards":"2","back":"2026-11-06","fragile":"yes","wood":"oak","photo":"oak.jpg"}}`, http.StatusOK)
	if v.Prompt.Kind != flows.KindConfirm {
		t.Fatalf("confirm: %+v", v.Prompt)
	}
	if want := []string{"File Oak in Short boards.", "Update it: Owner: Sam (2, fragile: yes)."}; len(v.Prompt.Coming) != 2 || v.Prompt.Coming[0] != want[0] || v.Prompt.Coming[1] != want[1] {
		t.Errorf("coming: %q", v.Prompt.Coming)
	}
	if v.Run.Values["note"] != "one\ntwo" || v.Run.Values["back"] != "2026-11-06" || v.Run.Values["grain"] != "oak" {
		t.Errorf("values: %v", v.Run.Values)
	}
	v = runAPI(t, srv, sam, http.MethodPost, path, `{"step":"confirm"}`, http.StatusOK)
	if !v.Run.Finished || v.Prompt.Asset == nil {
		t.Fatalf("done: %+v", v)
	}
	oak, _ := srv.store.GetAssetByID(v.Prompt.Asset.ID)
	if oak.Asset.Name != "Oak" || oak.FieldValues["grain"] != "oak" || oak.FieldValues["owner"] != "Sam (2, fragile: yes)" {
		t.Errorf("filed: %s %v", oak.Asset.Name, oak.FieldValues)
	}
	if len(v.Run.Photos) != 1 || v.Run.Photos[0] != (models.FlowPhoto{Question: "photo", AssetID: oak.Asset.ID}) {
		t.Errorf("its photo: %+v", v.Run.Photos)
	}
	if said := v.Prompt.Done; len(said) != 2 || said[0].Said != "Filed Oak in Short boards." || said[1].Said != "Updated Oak." {
		t.Errorf("done: %+v", said)
	}

	// Set before Confirm, it says what it will give the asset.
	w.saveFlow(t, "give", "Give", flows.Definition{Version: flows.Version, CategoryIDs: []string{w.short.ID}, Steps: []flows.Step{
		{ID: "confirm", Kind: flows.KindConfirm},
		{ID: "set", Kind: flows.KindSetFields, Fields: []flows.FieldValue{{Field: "owner", Value: "{user}"}, {Field: "grain", Value: "pine"}}},
		{ID: "file", Kind: flows.KindCreate, Name: "Offcut"},
		{ID: "done", Kind: flows.KindDone},
	}})
	v = runAPI(t, srv, sam, http.MethodPost, "/api/v1/flows/give/runs", "", http.StatusCreated)
	if want := "Give it Owner: Sam; Grain: pine."; len(v.Prompt.Coming) != 2 || v.Prompt.Coming[0] != want {
		t.Errorf("coming: %q", v.Prompt.Coming)
	}
}

// After a Find, Set fields updates the asset found; a field its category
// does not record is passed over.
func TestSetFieldsUpdatesTheAssetFound(t *testing.T) {
	w := newWoodShop(t)
	w.withGrain(t)
	srv, st := w.srv, w.srv.store
	board, _ := st.CreateAsset("Ash", []string{w.short.ID}, nil, map[string]string{"owner": "Alex"})
	w.saveFlow(t, "claim", "Claim it", flows.Definition{Version: flows.Version, CategoryIDs: []string{w.short.ID, w.long.ID}, Steps: []flows.Step{
		{ID: "find", Kind: flows.KindFind, FindBy: []string{flows.FindList}},
		{ID: "set", Kind: flows.KindSetFields, Fields: []flows.FieldValue{{Field: "owner", Value: "{user}, from {owner}"}, {Field: "grain", Value: "pine"}, {Field: "until", Value: "{today}"}}},
		{ID: "done", Kind: flows.KindDone, Text: "{asset} is {owner}'s"},
	}})
	sam := sessionCookie(t, srv, w.sam.ID)
	v := runAPI(t, srv, sam, http.MethodPost, "/api/v1/flows/claim/runs", `{"asset_id":"`+board.Asset.ID+`"}`, http.StatusCreated)
	if !v.Run.Finished {
		t.Fatalf("not finished: %+v", v)
	}
	a, _ := st.GetAssetByID(board.Asset.ID)
	if a.FieldValues["owner"] != "Sam, from Alex" || a.FieldValues["grain"] != "pine" || a.FieldValues["until"] != "" {
		t.Errorf("updated: %v", a.FieldValues)
	}
	// Later steps see the asset as it now is.
	if v.Prompt.Text != "Ash is Sam, from Alex's" {
		t.Errorf("done says %q", v.Prompt.Text)
	}
}

// A value that does not suit its field fails the step: the run stays on
// it, saying why, and trying again fails again.
func TestSetFieldsRefusesAValueThatDoesNotSuit(t *testing.T) {
	w := newWoodShop(t)
	srv := w.srv
	w.saveFlow(t, "leave", "Leave", flows.Definition{Version: flows.Version, CategoryIDs: []string{w.long.ID}, Steps: []flows.Step{
		{ID: "about", Kind: flows.KindForm, Questions: []flows.Question{{Key: "when", Label: "Until when?"}}},
		{ID: "set", Kind: flows.KindSetFields, Fields: []flows.FieldValue{{Field: "owner", Value: "{user}"}, {Field: "until", Value: "{when}"}}},
		{ID: "file", Kind: flows.KindCreate, Name: "Board"},
		{ID: "done", Kind: flows.KindDone},
	}})
	sam := sessionCookie(t, srv, w.sam.ID)
	v := runAPI(t, srv, sam, http.MethodPost, "/api/v1/flows/leave/runs", "", http.StatusCreated)
	path := "/api/v1/flows/leave/runs/" + v.Run.ID
	v = runAPI(t, srv, sam, http.MethodPost, path, `{"step":"about","answers":{"when":"Friday"}}`, http.StatusBadRequest)
	if v.Prompt.Step != "set" || v.Prompt.Kind != flows.KindSetFields || !strings.Contains(v.Error, `Until cannot be set to "Friday"`) || v.Progress != nil {
		t.Errorf("refused: %+v", v)
	}
	again := runAPI(t, srv, sam, http.MethodPost, path, `{"step":"set"}`, http.StatusBadRequest)
	if again.Prompt.Step != "set" || again.Error != v.Error {
		t.Errorf("again: %+v", again)
	}
	if list, _ := srv.store.ListAssets(w.long.ID, "Board"); len(list) != 0 {
		t.Errorf("filed anyway: %v", list)
	}
}

// Saving checks Set fields: a Create that leaves a field its category
// requires unset is refused, naming it, as is a fixed value that does not
// suit its field.
func TestSavingChecksSetFields(t *testing.T) {
	w := newWoodShop(t)
	w.withGrain(t)
	admin := sessionCookie(t, w.srv, w.admin.ID)
	save := func(steps ...flows.Step) string {
		body, _ := json.Marshal(map[string]any{"slug": "x", "name": "X", "definition": flows.Definition{Version: flows.Version, CategoryIDs: []string{w.short.ID, w.long.ID}, Steps: steps}})
		rec := doJSON(t, w.srv, admin, http.MethodPost, "/api/v1/flows", string(body))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("saved: %d %s", rec.Code, rec.Body)
		}
		return rec.Body.String()
	}
	choose := flows.Step{ID: "kind", Kind: flows.KindChooseCategory}
	file := flows.Step{ID: "file", Kind: flows.KindCreate, Name: "Board"}
	done := flows.Step{ID: "done", Kind: flows.KindDone}
	if body := save(choose, file, done); !strings.Contains(body, "Short boards requires Owner") || !strings.Contains(body, "Long boards requires Owner") {
		t.Errorf("the owner unset: %s", body)
	}
	body := save(choose, flows.Step{ID: "set", Kind: flows.KindSetFields, Fields: []flows.FieldValue{{Field: "owner", Value: "{user}"}, {Field: "grain", Value: "teak"}, {Field: "until", Value: "soon"}}}, file, done)
	if !strings.Contains(body, `Grain cannot be \"teak\"`) || !strings.Contains(body, `Until cannot be \"soon\"`) || strings.Contains(body, "requires") {
		t.Errorf("unsuitable values: %s", body)
	}
}
