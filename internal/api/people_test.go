package api

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/assetcsv"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/fields"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/xlsx"
)

// personFixture is a category of machines with a keeper, naming one
// person, the people trained on it, several, and whom to ask about it, a
// public field.
type personFixture struct {
	srv                  *Server
	cat                  string
	keeper, trained, ask *models.Field
	admin                *models.User
	adminC               *http.Cookie
	ada, bob             *models.User
	adaC, bobC, readerC  *http.Cookie
	cy                   *models.User // without an account
	adaEmail, bobEmail   string
}

func newPersonFixture(t *testing.T) *personFixture {
	t.Helper()
	srv, admin, _ := setupTestServer(t)
	f := &personFixture{srv: srv, admin: admin, adminC: sessionCookie(t, srv, admin.ID)}
	var err error
	if f.keeper, err = srv.store.CreateField("keeper", "Keeper", fields.TypePerson, nil); err != nil {
		t.Fatal(err)
	}
	if f.trained, err = srv.store.CreateField("trained", "Trained", fields.TypePerson, json.RawMessage(`{"multiple":true}`)); err != nil {
		t.Fatal(err)
	}
	if f.ask, err = srv.store.CreateField("ask", "Ask about it", fields.TypePerson, json.RawMessage(`{"public":true}`)); err != nil {
		t.Fatal(err)
	}
	cat, err := srv.store.CreateCategory("Machines", nil, "", false, false)
	if err != nil {
		t.Fatal(err)
	}
	f.cat = cat.ID
	for _, fd := range []*models.Field{f.keeper, f.trained, f.ask} {
		if err := srv.store.AssignCategoryField(cat.ID, fd.ID, false); err != nil {
			t.Fatal(err)
		}
	}
	member := []string{rbac.PermAssetsRead, rbac.PermAssetsWrite, rbac.PermAssetsTrack}
	f.ada, f.adaC = namedHolder(t, srv, "Ada", nil, member...)
	f.bob, f.bobC = namedHolder(t, srv, "Bob", nil, member...)
	_, f.readerC = namedHolder(t, srv, "Reader", nil, rbac.PermAssetsRead, rbac.PermAssetsWrite)
	f.adaEmail, f.bobEmail = f.ada.Email, f.bob.Email
	if f.cy, err = srv.store.CreatePerson("Cy", "cy@elsewhere.org"); err != nil {
		t.Fatal(err)
	}
	return f
}

// create adds an asset through the API as who, answering it as shown.
func (f *personFixture) create(t *testing.T, who *http.Cookie, name string, values map[string]string) (int, models.AssetWithDetails) {
	t.Helper()
	body, _ := json.Marshal(CreateAssetRequest{Name: name, CategoryIDs: []string{f.cat}, FieldValues: values})
	rec := doJSON(t, f.srv, who, http.MethodPost, "/api/v1/assets", string(body))
	var out struct{ Asset models.AssetWithDetails }
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out.Asset
}

func (f *personFixture) get(t *testing.T, who *http.Cookie, id string) (models.AssetWithDetails, string) {
	t.Helper()
	rec := doJSON(t, f.srv, who, http.MethodGet, "/api/v1/assets/"+id, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("get: %d %s", rec.Code, rec.Body)
	}
	var out struct{ Asset models.AssetWithDetails }
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return out.Asset, rec.Body.String()
}

// TestChoosingPeople: suggestions are only people the chooser may see,
// only to those who see who, with themselves always; adding someone
// without an account takes people.add, which users.manage includes; and a
// value naming someone the request may not choose is refused.
func TestChoosingPeople(t *testing.T) {
	f := newPersonFixture(t)
	srv := f.srv
	woodshop, _ := srv.store.CreateScope("woodshop")
	saws, _ := srv.store.CreateCategoryFrom(store.CategoryInput{Name: "Saws"})
	if _, err := srv.store.SetCategoryScopes(saws.ID, []string{woodshop.ID}); err != nil {
		t.Fatal(err)
	}
	_, woodC := namedHolder(t, srv, "Wood", []string{woodshop.ID}, rbac.PermAssetsRead, rbac.PermAssetsWrite, rbac.PermAssetsTrack)

	choices := func(who *http.Cookie, query string) ([]string, bool) {
		t.Helper()
		rec := doJSON(t, srv, who, http.MethodGet, "/api/v1/people/choices?"+query, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("choices %s: %d %s", query, rec.Code, rec.Body)
		}
		if strings.Contains(rec.Body.String(), "@") {
			t.Errorf("choices show an email: %s", rec.Body)
		}
		var out struct {
			People []models.FieldPerson
			CanAdd bool `json:"can_add"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		var names []string
		for _, p := range out.People {
			names = append(names, p.Name)
		}
		return names, out.CanAdd
	}
	got, canAdd := choices(f.adaC, "category_id="+f.cat)
	if !holds(got, "Bob") || !holds(got, "Cy") || !holds(got, "Ada") || holds(got, "Wood") || canAdd {
		t.Errorf("Ada's choices in the default scope: %v, can add %v", got, canAdd)
	}
	if got, _ := choices(f.adaC, "category_id="+f.cat+"&q=b"); len(got) != 1 || got[0] != "Bob" {
		t.Errorf("matching b: %v", got)
	}
	// Who may not see who is offered only themselves.
	if got, _ := choices(f.readerC, "category_id="+f.cat); len(got) != 1 || got[0] != "Reader" {
		t.Errorf("without assets.track: %v", got)
	}
	// Wood sees who only in the woodshop.
	if got, _ := choices(woodC, "category_id="+f.cat); len(got) != 1 || got[0] != "Wood" {
		t.Errorf("Wood outside the woodshop: %v", got)
	}
	if got, _ := choices(woodC, "category_id="+saws.ID); !holds(got, "Wood") || !holds(got, "Cy") {
		t.Errorf("Wood in the woodshop: %v", got)
	}
	if _, canAdd := choices(f.adminC, "category_id="+f.cat); !canAdd {
		t.Error("users.manage may not add people")
	}

	// Adding a person without an account takes people.add.
	if rec := doJSON(t, srv, f.adaC, http.MethodPost, "/api/v1/people", `{"name":"Dee"}`); rec.Code != http.StatusForbidden {
		t.Errorf("added without people.add: %d %s", rec.Code, rec.Body)
	}
	_, adderC := namedHolder(t, srv, "Adder", nil, rbac.PermAssetsRead, rbac.PermAssetsTrack, rbac.PermPeopleAdd)
	rec := doJSON(t, srv, adderC, http.MethodPost, "/api/v1/people", `{"name":"Dee","email":"dee@elsewhere.org"}`)
	if rec.Code != http.StatusCreated || strings.Contains(rec.Body.String(), "dee@") || !strings.Contains(rec.Body.String(), `"no_account":true`) {
		t.Fatalf("added: %d %s", rec.Code, rec.Body)
	}
	if rec := doJSON(t, srv, f.adminC, http.MethodPost, "/api/v1/people", `{"name":"Eve"}`); rec.Code != http.StatusCreated {
		t.Errorf("added with users.manage: %d %s", rec.Code, rec.Body)
	}
	if rec := doJSON(t, srv, adderC, http.MethodPost, "/api/v1/people", `{"name":"  "}`); rec.Code != http.StatusBadRequest {
		t.Errorf("added with no name: %d %s", rec.Code, rec.Body)
	}

	// Naming someone one may choose works; anyone else is refused.
	if code, _ := f.create(t, f.adaC, "Lathe", map[string]string{"keeper": f.cy.ID}); code != http.StatusCreated {
		t.Errorf("Ada naming Cy: %d", code)
	}
	if code, _ := f.create(t, f.readerC, "Drill", map[string]string{"keeper": f.bob.ID}); code != http.StatusForbidden {
		t.Errorf("naming Bob without assets.track: %d", code)
	}
	reader := userByName(t, srv, "Reader")
	if code, _ := f.create(t, f.readerC, "Drill", map[string]string{"keeper": reader.ID}); code != http.StatusCreated {
		t.Errorf("naming themselves: %d", code)
	}
	if code, _ := f.create(t, woodC, "Router", map[string]string{"keeper": f.ada.ID}); code != http.StatusForbidden {
		t.Errorf("Wood naming Ada outside the woodshop: %d", code)
	}
	// An archived person is never chosen again.
	gone, _ := srv.store.CreatePerson("Gone", "")
	_ = srv.store.DeleteUser(gone.ID)
	if code, _ := f.create(t, f.adaC, "Mill", map[string]string{"keeper": gone.ID}); code != http.StatusForbidden {
		t.Errorf("naming the archived: %d", code)
	}
}

// userByName finds a user by name.
func userByName(t *testing.T, srv *Server, name string) *models.User {
	t.Helper()
	users, err := srv.store.ListUsers()
	if err != nil {
		t.Fatal(err)
	}
	for i := range users {
		if users[i].Name == name {
			return &users[i]
		}
	}
	t.Fatalf("no user %s", name)
	return nil
}

// TestSeeingPeopleInFields: names show with assets.track, or in a public
// field; without, a field shows that someone is set, but not who, except
// the requester; emails never show on asset pages, lists, history or
// labels; an edit keeps whom the requester does not see; a rename shows
// everywhere; and assets are found by person only as the requester sees
// them.
func TestSeeingPeopleInFields(t *testing.T) {
	f := newPersonFixture(t)
	srv := f.srv
	reader := userByName(t, srv, "Reader")
	code, lathe := f.create(t, f.adaC, "Lathe", map[string]string{"keeper": f.bob.ID, "trained": f.cy.ID + "," + reader.ID, "ask": f.ada.ID})
	if code != http.StatusCreated {
		t.Fatalf("created: %d", code)
	}
	id := lathe.Asset.ID

	seen, raw := f.get(t, f.adaC, id)
	if seen.People[f.bob.ID].Name != "Bob" || seen.People[f.cy.ID].Name != "Cy" || !seen.People[f.cy.ID].NoAccount || len(seen.HiddenPeople) != 0 {
		t.Errorf("with assets.track: %+v %+v", seen.People, seen.HiddenPeople)
	}
	for _, email := range []string{f.adaEmail, f.bobEmail, "cy@elsewhere.org"} {
		if strings.Contains(raw, email) {
			t.Errorf("the asset shows %s: %s", email, raw)
		}
	}

	blind, raw := f.get(t, f.readerC, id)
	if _, ok := blind.FieldValues["keeper"]; ok || blind.HiddenPeople["keeper"] != 1 {
		t.Errorf("keeper without assets.track: %v %v", blind.FieldValues, blind.HiddenPeople)
	}
	if blind.FieldValues["trained"] != reader.ID || blind.HiddenPeople["trained"] != 1 || blind.People[reader.ID].Name != "Reader" {
		t.Errorf("trained without assets.track, the reader among them: %v %v %v", blind.FieldValues, blind.HiddenPeople, blind.People)
	}
	if blind.FieldValues["ask"] != f.ada.ID || blind.People[f.ada.ID].Name != "Ada" {
		t.Errorf("the public field: %v %v", blind.FieldValues, blind.People)
	}
	if strings.Contains(raw, f.bob.ID) || strings.Contains(raw, f.cy.ID) || strings.Contains(raw, "Bob") {
		t.Errorf("hidden people show: %s", raw)
	}
	// The public role, signed out, reads it too, with only public names.
	scopes, _ := srv.store.ListScopes()
	for _, sc := range scopes {
		if sc.Name == rbac.PublicScope {
			if _, err := srv.store.SetCategoryScopes(f.cat, []string{sc.ID}); err != nil {
				t.Fatal(err)
			}
		}
	}
	if out, raw := f.get(t, nil, id); out.FieldValues["ask"] != f.ada.ID || out.HiddenPeople["keeper"] != 1 || strings.Contains(raw, "Bob") {
		t.Errorf("signed out: %v %v", out.FieldValues, out.HiddenPeople)
	}

	// Lists, history and label data name people as the asset does.
	rec := doJSON(t, srv, f.readerC, http.MethodGet, "/api/v1/assets", "")
	if strings.Contains(rec.Body.String(), "Bob") || strings.Contains(rec.Body.String(), f.bob.ID) {
		t.Errorf("the list shows Bob: %s", rec.Body)
	}
	rec = doJSON(t, srv, f.readerC, http.MethodGet, "/api/v1/assets/"+id+"/history", "")
	if strings.Contains(rec.Body.String(), "Bob") || strings.Contains(rec.Body.String(), `"person"`) {
		t.Errorf("history shows whom the fields named: %s", rec.Body)
	}
	rec = doJSON(t, srv, f.adaC, http.MethodGet, "/api/v1/assets/"+id+"/history", "")
	if !strings.Contains(rec.Body.String(), `"what":"person"`) || !strings.Contains(rec.Body.String(), `"to":"Bob"`) || strings.Contains(rec.Body.String(), f.bobEmail) {
		t.Errorf("history with assets.track: %s", rec.Body)
	}
	designer := func(perms ...string) *http.Cookie {
		_, c := namedHolder(t, srv, "Designer "+strings.Join(perms, " "), nil, append(perms, rbac.PermTemplatesManage)...)
		return c
	}
	var data PreviewDataResponse
	rec = doJSON(t, srv, designer(rbac.PermAssetsRead, rbac.PermAssetsTrack), http.MethodPost, "/api/v1/templates/preview-data", `{"asset_id":"`+id+`"}`)
	_ = json.Unmarshal(rec.Body.Bytes(), &data)
	if data.Data.Fields["keeper"] != "Bob" || data.Data.Fields["trained"] != "Cy, Reader" || strings.Contains(rec.Body.String(), "@") {
		t.Errorf("label data with assets.track: %+v %s", data.Data.Fields, rec.Body)
	}
	data = PreviewDataResponse{}
	rec = doJSON(t, srv, designer(rbac.PermAssetsRead), http.MethodPost, "/api/v1/templates/preview-data", `{"asset_id":"`+id+`"}`)
	_ = json.Unmarshal(rec.Body.Bytes(), &data)
	if data.Data.Fields["keeper"] != "someone" || data.Data.Fields["ask"] != "Ada" || strings.Contains(rec.Body.String(), "Bob") {
		t.Errorf("label data without assets.track: %+v", data.Data.Fields)
	}

	// Renamed, Bob shows by his new name.
	if _, err := srv.store.SetUserName(f.bob.ID, "Robert"); err != nil {
		t.Fatal(err)
	}
	if a, _ := f.get(t, f.adaC, id); a.People[f.bob.ID].Name != "Robert" {
		t.Errorf("after the rename: %+v", a.People)
	}

	// The reader edits the trained, seeing only themselves: those hidden
	// stay. Taking themselves out leaves the hidden.
	rec = doJSON(t, srv, f.readerC, http.MethodPut, "/api/v1/assets/"+id, `{"field_values":{"trained":""}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("the reader's edit: %d %s", rec.Code, rec.Body)
	}
	if a, _ := srv.store.GetAssetByID(id); a.FieldValues["trained"] != f.cy.ID || a.FieldValues["keeper"] != f.bob.ID {
		t.Errorf("after the reader's edit: %v", a.FieldValues)
	}

	// Found by person, as the requester sees them.
	found := func(who *http.Cookie, query string) []string {
		t.Helper()
		rec := doJSON(t, srv, who, http.MethodGet, "/api/v1/assets?"+query, "")
		var out struct{ Assets []models.AssetWithDetails }
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		var names []string
		for _, a := range out.Assets {
			names = append(names, a.Asset.Name)
		}
		return names
	}
	if got := found(f.bobC, "person=me"); len(got) != 1 || got[0] != "Lathe" {
		t.Errorf("Bob's assets: %v", got)
	}
	if got := found(f.bobC, "person=me&person_field=trained"); len(got) != 0 {
		t.Errorf("Bob as trained: %v", got)
	}
	if got := found(f.readerC, "person="+f.bob.ID); len(got) != 0 {
		t.Errorf("found by someone hidden from the requester: %v", got)
	}
	if got := found(f.adaC, "person="+f.bob.ID+"&person_field=keeper"); len(got) != 1 {
		t.Errorf("found by keeper: %v", got)
	}
	if rec := doJSON(t, srv, nil, http.MethodGet, "/api/v1/assets?person=me", ""); rec.Code == http.StatusOK {
		t.Errorf("me, signed out: %d", rec.Code)
	}

	// A saved report finds whoever runs it.
	spec := `{"filter":{"person":"me"},"sections":[{"type":"table","columns":[{"heading":"Asset","binding":"{{asset.name}}"},{"heading":"Keeper","binding":"{{field.keeper}}"}]}]}`
	rec = doJSON(t, srv, f.adminC, http.MethodPost, "/api/v1/reports", `{"name":"Mine","spec":`+spec+`}`)
	var saved struct{ Report struct{ ID string } }
	if rec.Code != http.StatusCreated || json.Unmarshal(rec.Body.Bytes(), &saved) != nil {
		t.Fatalf("saving the report: %d %s", rec.Code, rec.Body)
	}
	run := "/api/v1/reports/" + saved.Report.ID + "/run"
	rec = doJSON(t, srv, f.bobC, http.MethodGet, run, "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Lathe") || !strings.Contains(rec.Body.String(), "Robert") {
		t.Errorf("Bob's run: %d %s", rec.Code, rec.Body)
	}
	// The reader took themselves out of it, and sees no one else.
	if rec := doJSON(t, srv, f.readerC, http.MethodGet, run, ""); rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "Lathe") {
		t.Errorf("the reader's run: %d %s", rec.Code, rec.Body)
	}
}

// TestMergingPeopleThroughTheAPI: merging takes users.manage and a person
// without an account, and moves every field.
func TestMergingPeopleThroughTheAPI(t *testing.T) {
	f := newPersonFixture(t)
	srv := f.srv
	_, lathe := f.create(t, f.adaC, "Lathe", map[string]string{"keeper": f.cy.ID})
	path := "/api/v1/users/" + f.cy.ID + "/merge"
	if rec := doJSON(t, srv, f.adaC, http.MethodPost, path, `{"into":"`+f.bob.ID+`"}`); rec.Code != http.StatusForbidden {
		t.Errorf("merged without users.manage: %d", rec.Code)
	}
	if rec := doJSON(t, srv, f.adminC, http.MethodPost, "/api/v1/users/"+f.ada.ID+"/merge", `{"into":"`+f.bob.ID+`"}`); rec.Code != http.StatusConflict {
		t.Errorf("merged someone who can sign in: %d %s", rec.Code, rec.Body)
	}
	rec := doJSON(t, srv, f.adminC, http.MethodPost, path, `{"into":"`+f.bob.ID+`"}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"assets":1`) {
		t.Fatalf("merged: %d %s", rec.Code, rec.Body)
	}
	if a, _ := srv.store.GetAssetByID(lathe.Asset.ID); a.FieldValues["keeper"] != f.bob.ID {
		t.Errorf("after merging: %v", a.FieldValues)
	}
	rec = doJSON(t, srv, f.adaC, http.MethodGet, "/api/v1/assets/"+lathe.Asset.ID+"/history", "")
	if !strings.Contains(rec.Body.String(), "Merged Cy into Bob.") {
		t.Errorf("history: %s", rec.Body)
	}
	rec = doJSON(t, srv, f.readerC, http.MethodGet, "/api/v1/assets/"+lathe.Asset.ID+"/history", "")
	if strings.Contains(rec.Body.String(), "Merged") {
		t.Errorf("history without assets.track: %s", rec.Body)
	}
	// The user list marks people without accounts.
	dee, _ := srv.store.CreatePerson("Dee", "")
	rec = doJSON(t, srv, f.adminC, http.MethodGet, "/api/v1/users", "")
	if !strings.Contains(rec.Body.String(), `"id":"`+dee.ID+`","email":"","name":"Dee"`) || !strings.Contains(rec.Body.String(), `"can_sign_in":false`) {
		t.Errorf("the user list: %s", rec.Body)
	}
}

// TestPersonFieldsInCSVAndExcel: exports hold names, never emails or IDs;
// imports take emails, or names only one person has; a name several
// people have, or no one, is a problem, offered to add with people.add;
// and an export imports back unchanged.
func TestPersonFieldsInCSVAndExcel(t *testing.T) {
	f := newPersonFixture(t)
	srv := f.srv
	_, lathe := f.create(t, f.adaC, "Lathe", map[string]string{"keeper": f.bob.ID, "trained": f.cy.ID + "," + f.ada.ID})

	rec := doJSON(t, srv, f.adaC, http.MethodGet, "/api/v1/assets/export.csv", "")
	rows := readCSV(t, rec.Body.String())
	col := func(name string) int {
		for i, h := range rows[0] {
			if h == name {
				return i
			}
		}
		t.Fatalf("no column %s in %v", name, rows[0])
		return -1
	}
	if rows[1][col("field.keeper")] != "Bob" || rows[1][col("field.trained")] != "Cy; Ada" {
		t.Errorf("exported: %v", rows[1])
	}
	if strings.Contains(rec.Body.String(), "@") || strings.Contains(rec.Body.String(), f.bob.ID) {
		t.Errorf("the export shows an email or an ID: %s", rec.Body)
	}
	// Without assets.track, the hidden are left out.
	rec = doJSON(t, srv, f.readerC, http.MethodGet, "/api/v1/assets/export.csv", "")
	if strings.Contains(rec.Body.String(), "Bob") {
		t.Errorf("the reader's export shows Bob: %s", rec.Body)
	}

	// The export imports back unchanged.
	plan := previewPlan(t, srv, f.adaC, rec0(t, srv, f.adaC))
	if plan.ProblemCount != 0 || plan.Unchanged != 1 {
		t.Errorf("an export imported back: %+v", plan)
	}

	mapping := []string{"id", "field.keeper", "field.trained"}
	file := "id,keeper,trained\n" + lathe.Asset.ID + "," + f.cy.Email + ",ada; BOB\n"
	plan = previewPlanWith(t, srv, f.adaC, file, mapping)
	if plan.ProblemCount != 0 || plan.Update != 1 {
		t.Fatalf("by email and name: %+v", plan)
	}
	if rec := postImport(t, srv, f.adaC, "/api/v1/assets/import", file, mapping); rec.Code != http.StatusOK {
		t.Fatalf("imported: %d %s", rec.Code, rec.Body)
	}
	if a, _ := srv.store.GetAssetByID(lathe.Asset.ID); a.FieldValues["keeper"] != f.cy.ID || a.FieldValues["trained"] != f.ada.ID+","+f.bob.ID {
		t.Errorf("after importing: %v", a.FieldValues)
	}

	// An Excel export imports back unchanged too.
	rec = doJSON(t, srv, f.adaC, http.MethodGet, "/api/v1/assets/export.xlsx", "")
	book, err := xlsx.Open(rec.Body.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	sheetRows, _ := book.Rows(book.Sheets[0])
	if strings.Contains(strings.Join(sheetRows[1], ","), "@") {
		t.Errorf("the workbook shows an email: %v", sheetRows)
	}
	rec = postWorkbook(t, srv, f.adaC, "/api/v1/assets/import/preview", rec.Body.Bytes(), book.Sheets[0], nil)
	var bookPlan assetcsv.Plan
	_ = json.Unmarshal(rec.Body.Bytes(), &bookPlan)
	if rec.Code != http.StatusOK || bookPlan.ProblemCount != 0 || bookPlan.Unchanged != 1 {
		t.Errorf("the workbook imported back: %d %+v", rec.Code, bookPlan)
	}

	// Two named Bob: the name is a problem, the email is not.
	if _, err := srv.store.CreatePerson("Bob", ""); err != nil {
		t.Fatal(err)
	}
	plan = previewPlanWith(t, srv, f.adaC, "id,keeper,trained\n"+lathe.Asset.ID+",Bob,\n", mapping)
	if plan.ProblemCount != 1 || !strings.Contains(plan.Problems[0].Message, "several people") {
		t.Errorf("an ambiguous name: %+v", plan.Problems)
	}
	plan = previewPlanWith(t, srv, f.adaC, "id,keeper,trained\n"+lathe.Asset.ID+","+f.bobEmail+",\n", mapping)
	if plan.ProblemCount != 0 {
		t.Errorf("Bob by email: %+v", plan.Problems)
	}
	// No one named Zed: a problem, offered to add only with people.add.
	file = "id,keeper,trained\n" + lathe.Asset.ID + ",,Zed;zed\n"
	plan = previewPlanWith(t, srv, f.adaC, file, mapping)
	if plan.ProblemCount != 2 || len(plan.NewPeople) != 1 || plan.NewPeople[0] != "Zed" || plan.CanAddPeople {
		t.Errorf("no one named Zed: %+v", plan)
	}
	plan = previewPlanWith(t, srv, f.adminC, file, mapping)
	if !plan.CanAddPeople {
		t.Error("users.manage may not add people")
	}
	// The reader, without assets.track, may name only themselves.
	plan = previewPlanWith(t, srv, f.readerC, "id,keeper,trained\n"+lathe.Asset.ID+","+f.adaEmail+",\n", mapping)
	if plan.ProblemCount != 1 || !strings.Contains(plan.Problems[0].Message, "no one you may choose") {
		t.Errorf("the reader naming Ada: %+v", plan.Problems)
	}

}

// rec0 is the CSV export as Ada sees it.
func rec0(t *testing.T, srv *Server, who *http.Cookie) string {
	t.Helper()
	return doJSON(t, srv, who, http.MethodGet, "/api/v1/assets/export.csv", "").Body.String()
}

func previewPlan(t *testing.T, srv *Server, who *http.Cookie, file string) assetcsv.Plan {
	t.Helper()
	return previewPlanWith(t, srv, who, file, nil)
}

func previewPlanWith(t *testing.T, srv *Server, who *http.Cookie, file string, mapping []string) assetcsv.Plan {
	t.Helper()
	rec := postImport(t, srv, who, "/api/v1/assets/import/preview", file, mapping)
	if rec.Code != http.StatusOK {
		t.Fatalf("preview: %d %s", rec.Code, rec.Body)
	}
	var plan assetcsv.Plan
	if err := json.Unmarshal(rec.Body.Bytes(), &plan); err != nil {
		t.Fatal(err)
	}
	return plan
}

// TestPersonRefs: ?ref= finds the one person a reference names among
// those the request may choose.
func TestPersonRefs(t *testing.T) {
	f := newPersonFixture(t)
	ref := func(who *http.Cookie, r string) int {
		return doJSON(t, f.srv, who, http.MethodGet, "/api/v1/people/choices?category_id="+f.cat+"&ref="+url.QueryEscape(r), "").Code
	}
	for _, r := range []string{f.bob.ID, f.bobEmail, "bob", "cy@elsewhere.org"} {
		if got := ref(f.adaC, r); got != http.StatusOK {
			t.Errorf("%s: %d", r, got)
		}
	}
	if got := ref(f.adaC, "nobody@nowhere.org"); got != http.StatusNotFound {
		t.Errorf("no one: %d", got)
	}
	if got := ref(f.readerC, f.bobEmail); got != http.StatusNotFound {
		t.Errorf("Bob, to the reader: %d", got)
	}
	_, _ = f.srv.store.CreatePerson("Bob", "")
	if got := ref(f.adaC, "Bob"); got != http.StatusConflict {
		t.Errorf("two Bobs: %d", got)
	}
}

// TestFirstSignInJoinsAPersonWithoutAnAccount: a first sign-in through a
// provider joins a person without an account by verified email, when the
// provider links by email, and never by an unverified one; their fields
// are already theirs.
func TestFirstSignInJoinsAPersonWithoutAnAccount(t *testing.T) {
	srv, ts, admin := providerServer(t)
	keeper, _ := srv.store.CreateField("keeper", "Keeper", fields.TypePerson, nil)
	cat, _ := srv.store.CreateCategory("Machines", nil, "", false, false)
	_ = srv.store.AssignCategoryField(cat.ID, keeper.ID, false)
	cy, _ := srv.store.CreatePerson("Cy", "cy@hackspace.org")
	lathe, err := srv.store.CreateAsset("Lathe", []string{cat.ID}, nil, map[string]string{"keeper": cy.ID})
	if err != nil {
		t.Fatal(err)
	}
	settings := map[string]string{"subject": "cy-at-idp", "email": "Cy@Hackspace.org", "email_verified": "no", "name": "Cy Young"}
	req := testProviderRequest(settings)
	req.Provision = true
	p := saveProvider(t, srv, admin, "", req)

	for _, c := range []struct {
		what     string
		link     bool
		verified string
	}{{"linking off, verified", false, "yes"}, {"linking on, unverified", true, "no"}} {
		what := c.what
		req.LinkByEmail, settings["email_verified"] = c.link, c.verified
		saveProvider(t, srv, admin, p.ID, req)
		b := browser(t)
		if end := signInThrough(t, ts, b, "hackspace", "/"); end.Query().Get("error") != noAccount(&store.Provider{Name: "Hackspace"}) {
			t.Errorf("%s: ended at %s", what, end)
		}
		if got := me(t, ts, b); got != "" {
			t.Errorf("%s: signed in as %q", what, got)
		}
		if u, _ := srv.store.GetUserByID(cy.ID); u.CanSignIn {
			t.Fatalf("%s: joined", what)
		}
	}
	// Linking on and verified, they join: the same person, their fields
	// theirs.
	settings["email_verified"] = "yes"
	saveProvider(t, srv, admin, p.ID, req)
	b := browser(t)
	signInThrough(t, ts, b, "hackspace", "/")
	if got := me(t, ts, b); got != "cy@hackspace.org" {
		t.Fatalf("joined: signed in as %q", got)
	}
	if u, _ := srv.store.GetUserByID(cy.ID); !u.CanSignIn || u.Name != "Cy" {
		t.Errorf("after joining: %+v", u)
	}
	if ids, _ := srv.store.UserIdentities(cy.ID); len(ids) != 1 || ids[0].Subject != "cy-at-idp" {
		t.Errorf("identities: %+v", ids)
	}
	if users, _ := srv.store.ListUsers(); len(users) != 2 {
		t.Errorf("an account made beside them: %d users", len(users))
	}
	// They hold no roles, so they read nothing yet; the field is theirs.
	if a, _ := srv.store.FilterAssets(store.AssetFilter{Person: cy.ID}); len(a) != 1 || a[0].Asset.ID != lathe.Asset.ID {
		t.Errorf("their assets: %d", len(a))
	}
}
