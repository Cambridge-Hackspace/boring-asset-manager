package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/config"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/fields"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/flows"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store/migrations"
)

// openBlankStore opens a store of the test dialect with no schema, to
// migrate step by step.
func openBlankStore(t *testing.T) *store.Store {
	t.Helper()
	if dialect := os.Getenv(store.TestDialectEnv); dialect != "" && dialect != "sqlite" {
		st, cleanup, err := store.OpenEphemeral(dialect, os.Getenv(store.TestDSNEnv))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(cleanup)
		return st
	}
	st, err := store.Open(&config.DatabaseConfig{Type: "sqlite", DSN: filepath.Join(t.TempDir(), "blank.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

// Migration 050 converts every flow saved at version 1 to version 2 (#174),
// and each runs as it did: filing the same asset with the same fields
// (and, now, the fields it hid with a default), updating as its Change
// did, in the same order. Runs under way are dropped. Run again, it
// changes nothing.
func TestMigratingFlowsToVersion2(t *testing.T) {
	st := openBlankStore(t)
	if err := migrations.MigrateTo(st.DB, st.Dialect, migrations.FormQuestionsVersion-1); err != nil {
		t.Fatal(err)
	}
	if err := st.SeedDefaultPermissionsAndRoles(); err != nil {
		t.Fatal(err)
	}
	admin, err := st.CreateAdminUser("admin@test.org", "AdminSecret123!")
	if err != nil {
		t.Fatal(err)
	}
	w := woodShopIn(t, st)
	note, err := st.CreateField("note", "Note", fields.TypeText, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AssignCategoryField(w.wood.ID, note.ID, false); err != nil {
		t.Fatal(err)
	}
	starter := `{"version":1,"description":"Scan to leave something here, with your name on it.","category_ids":["SHORT"],"steps":[` +
		`{"id":"about","kind":"form","title":"About it","ask_name":true,"name_label":"What is it?"},` +
		`{"id":"file","kind":"create","name":"{name}"},{"id":"label","kind":"label","label":{"mode":"show"}},` +
		`{"id":"done","kind":"done","title":"Filed","text":"{asset} is filed. Put its label on it."}]}`
	saved := map[string]string{
		// Asked fields, relabeled and defaulted, one hidden with a
		// default, its name and a photo.
		"leave": `{"version":1,"description":"Leave it","category_ids":["SHORT","LONG"],"steps":[` +
			`{"id":"kind","kind":"choose_category","title":"What are you leaving?"},` +
			`{"id":"about","kind":"form","ask_name":true,"name_label":"What is it?","ask_photo":true,"fields":[` +
			`{"key":"until","label":"Leave until"},{"key":"owner","default":"{user}"},{"key":"note","hidden":true,"default":"Left by {user}"}]},` +
			`{"id":"file","kind":"create","name":"{owner}: {name}"},{"id":"label","kind":"label","label":{"mode":"show"}},` +
			`{"id":"done","kind":"done","text":"{asset} is filed."}]}`,
		// Change's fields amid other changes.
		"finish": `{"version":1,"category_ids":["WOOD"],"steps":[{"id":"find","kind":"find","find_by":["list"]},` +
			`{"id":"finish","kind":"change","changes":[{"kind":"category","category_id":"LONG"},{"kind":"fields","fields":{"owner":"{owner} (done)"}},{"kind":"archive"}]},` +
			`{"id":"done","kind":"done"}]}`,
		// The starters, as version 1 made them.
		"starter": starter,
		"collect": `{"version":1,"description":"Scan to collect something you left here.","category_ids":["WOOD"],"steps":[` +
			`{"id":"find","kind":"find","title":"What are you collecting?","find_by":["scan","search","list"]},` +
			`{"id":"confirm","kind":"confirm","title":"Collect it?","text":"{asset} will be taken off the list."},` +
			`{"id":"finish","kind":"change","changes":[{"kind":"archive"}]},{"id":"done","kind":"done","title":"Collected","text":"{asset} is collected."}]}`,
	}
	now := time.Now().UTC()
	ids := strings.NewReplacer("SHORT", w.short.ID, "LONG", w.long.ID, "WOOD", w.wood.ID)
	for slug, def := range saved {
		if _, err := st.Exec("INSERT INTO flows (id, slug, name, definition, enabled, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)",
			"f-"+slug, slug, strings.ToUpper(slug[:1])+slug[1:], ids.Replace(def), true, now, now); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := st.Exec("INSERT INTO flow_runs (id, flow_id, user_id, state, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)",
		"r1", "f-leave", w.sam.ID, `{"step":1,"values":{"user":"Sam"},"done":[],"finished":false,"fields":{"owner":"Sam"}}`, now, now); err != nil {
		t.Fatal(err)
	}

	definitions := func() map[string]string {
		t.Helper()
		out := map[string]string{}
		for slug := range saved {
			var def string
			if err := st.QueryRow("SELECT definition FROM flows WHERE slug = ?", slug).Scan(&def); err != nil {
				t.Fatal(err)
			}
			out[slug] = def
		}
		return out
	}
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	after := definitions()
	// Run again, it changes nothing.
	if _, err := st.Exec("DELETE FROM schema_migrations WHERE version = ?", migrations.FormQuestionsVersion); err != nil {
		t.Fatal(err)
	}
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	if again := definitions(); !reflect.DeepEqual(again, after) {
		t.Errorf("run again:\n%v\nwas\n%v", again, after)
	}
	var runs int
	if err := st.QueryRow("SELECT COUNT(*) FROM flow_runs").Scan(&runs); err != nil || runs != 0 {
		t.Errorf("runs under way: %d %v", runs, err)
	}

	cfg := config.DefaultConfig()
	testStorage(t, cfg)
	w.srv, w.admin = NewServer(cfg, st), admin
	read := map[string]*models.Flow{}
	for slug := range saved {
		f, err := st.GetFlow(slug)
		if err != nil {
			t.Fatal(err)
		}
		read[slug] = f
		if f.Definition.Version != flows.Version {
			t.Errorf("%s is version %d", slug, f.Definition.Version)
		}
		// Each saves as it is.
		if problems, err := w.srv.checkFlow(as(t, w.srv, admin.ID), &f.Definition); err != nil || len(problems) > 0 {
			t.Errorf("%s: %v %v", slug, problems, err)
		}
	}
	steps := func(slug string) string {
		var out []string
		for _, s := range read[slug].Definition.Steps {
			out = append(out, s.ID+":"+s.Kind)
		}
		return strings.Join(out, " ")
	}
	if got := steps("leave"); got != "kind:choose_category about:form set:set_fields file:create label:label done:done" {
		t.Errorf("leave: %s", got)
	}
	if got := steps("finish"); got != "find:find finish:change set:set_fields change:change done:done" {
		t.Errorf("finish: %s", got)
	}
	// The starter is today's starter.
	today, err := flows.Starters([]string{w.short.ID}, st.FlowFields)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(read["starter"].Definition, today[0].Definition) {
		t.Errorf("the starter:\n%+v\ntoday's\n%+v", read["starter"].Definition, today[0].Definition)
	}
	if !reflect.DeepEqual(read["collect"].Definition, func() flows.Definition { all, _ := flows.Starters([]string{w.wood.ID}, nil); return all[1].Definition }()) {
		t.Errorf("collect: %+v", read["collect"].Definition)
	}

	sam := sessionCookie(t, w.srv, w.sam.ID)
	// Leave: long boards, with until, the owner, and the photo.
	v := runAPI(t, w.srv, sam, http.MethodPost, "/api/v1/flows/leave/runs", "", http.StatusCreated)
	path := "/api/v1/flows/leave/runs/" + v.Run.ID
	v = runAPI(t, w.srv, sam, http.MethodPost, path, `{"step":"kind","category_id":"`+w.long.ID+`"}`, http.StatusOK)
	var asked []string
	for _, q := range v.Prompt.Questions {
		asked = append(asked, fmt.Sprintf("%s:%s:%s:%s:%v", q.Key, q.Type, q.Label, q.Default, q.Required))
	}
	if want := []string{"name:text:What is it?::true", "until:date:Leave until::false", "owner:text:Owner:Sam:true", "photo:photo:A photo::false"}; !reflect.DeepEqual(asked, want) {
		t.Errorf("leave asks %v", asked)
	}
	v = runAPI(t, w.srv, sam, http.MethodPost, path, `{"step":"about","answers":{"name":"Oak","until":"2026-11-06","owner":"Sam","photo":"oak.jpg"}}`, http.StatusOK)
	if !v.Run.Finished || v.Prompt.Asset == nil || v.Prompt.Text != "Sam: Oak is filed." || !v.Prompt.ShowLabel {
		t.Fatalf("left: %+v", v)
	}
	oak, _ := st.GetAssetByID(v.Prompt.Asset.ID)
	want := map[string]string{"owner": "Sam", "until": "2026-11-06", "note": "Left by Sam"}
	if oak.Asset.Name != "Sam: Oak" || len(oak.Categories) != 1 || oak.Categories[0].ID != w.long.ID || !reflect.DeepEqual(oak.FieldValues, want) {
		t.Errorf("left: %s %v %v", oak.Asset.Name, oak.Categories, oak.FieldValues)
	}
	if len(v.Run.Photos) != 1 || v.Run.Photos[0].AssetID != oak.Asset.ID {
		t.Errorf("its photo: %+v", v.Run.Photos)
	}

	// The starter: short boards, which record no date.
	v = runAPI(t, w.srv, sam, http.MethodPost, "/api/v1/flows/starter/runs", "", http.StatusCreated)
	v = runAPI(t, w.srv, sam, http.MethodPost, "/api/v1/flows/starter/runs/"+v.Run.ID, `{"step":"about","answers":{"name":"Pine","owner":"Sam"}}`, http.StatusOK)
	pine, _ := st.GetAssetByID(v.Prompt.Asset.ID)
	if !v.Run.Finished || pine.Asset.Name != "Pine" || !reflect.DeepEqual(pine.FieldValues, map[string]string{"owner": "Sam"}) {
		t.Errorf("the starter: %+v %v", v.Run, pine.FieldValues)
	}

	// Finish: moved, updated, then archived, in that order.
	v = runAPI(t, w.srv, sam, http.MethodPost, "/api/v1/flows/finish/runs", `{"asset_id":"`+pine.Asset.ID+`"}`, http.StatusCreated)
	if !v.Run.Finished {
		t.Fatalf("finish: %+v", v)
	}
	var said []string
	for _, d := range v.Prompt.Done {
		said = append(said, d.Said)
	}
	if want := []string{"Moved Pine to Long boards.", "Updated Pine.", "Archived Pine."}; !reflect.DeepEqual(said, want) {
		t.Errorf("finish did %v", said)
	}
	pine, _ = st.GetAssetByID(pine.Asset.ID)
	if pine.Asset.ArchivedAt == nil || pine.FieldValues["owner"] != "Sam (done)" || pine.Categories[0].ID != w.long.ID {
		t.Errorf("finished: %+v %v", pine.Asset, pine.FieldValues)
	}

	// Collect it, from what Leave filed.
	v = runAPI(t, w.srv, sam, http.MethodPost, "/api/v1/flows/collect/runs", `{"asset_id":"`+oak.Asset.ID+`"}`, http.StatusCreated)
	v = runAPI(t, w.srv, sam, http.MethodPost, "/api/v1/flows/collect/runs/"+v.Run.ID, `{"step":"confirm"}`, http.StatusOK)
	if oak, _ = st.GetAssetByID(oak.Asset.ID); !v.Run.Finished || oak.Asset.ArchivedAt == nil {
		t.Errorf("collected: %+v", v.Run)
	}

	// A definition as the store keeps it, for the record.
	if b, _ := json.Marshal(read["leave"].Definition.Steps[2]); !strings.Contains(string(b), `"photo":"{photo}"`) || !strings.Contains(string(b), `{"field":"note","value":"Left by {user}"}`) {
		t.Errorf("leave's Set fields: %s", b)
	}
}
