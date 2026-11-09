package store

import (
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/fields"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
)

// personField makes a person field, naming several people when multiple,
// on a new category, and answers both.
func personField(t *testing.T, st *Store, key string, multiple bool) (*models.Field, string) {
	t.Helper()
	opts := json.RawMessage(`{}`)
	if multiple {
		opts = json.RawMessage(`{"multiple":true}`)
	}
	f, err := st.CreateField(key, strings.ToUpper(key[:1])+key[1:], fields.TypePerson, opts)
	if err != nil {
		t.Fatal(err)
	}
	cat, err := st.CreateCategory("Things with "+key, nil, "", false, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AssignCategoryField(cat.ID, f.ID, false); err != nil {
		t.Fatal(err)
	}
	return f, cat.ID
}

// fieldPeopleRows are the user IDs asset_field_people holds for an asset's
// field, sorted.
func fieldPeopleRows(t *testing.T, st *Store, assetID, fieldID string) []string {
	t.Helper()
	rows, err := st.Query("SELECT user_id FROM asset_field_people WHERE asset_id = ? AND field_id = ? ORDER BY user_id", assetID, fieldID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		out = append(out, id)
	}
	return out
}

func sortedIDs(ids ...string) []string {
	out := append([]string{}, ids...)
	sort.Strings(out)
	return out
}

// TestPeopleWithoutAccounts: a person without an account has a name, an
// email or none, no password and no roles; they cannot sign in, borrow, be
// given roles or be mentioned, and their missing email reads as none and
// finds no one.
func TestPeopleWithoutAccounts(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	if _, err := st.CreatePerson("  ", ""); !errors.Is(err, ErrInvalidUser) {
		t.Errorf("no name: %v", err)
	}
	cy, err := st.CreatePerson("Cy", "")
	if err != nil {
		t.Fatal(err)
	}
	if cy.CanSignIn || cy.Email != "" || HasPassword(cy) || !cy.IsActive {
		t.Errorf("a person without an account: %+v", cy)
	}
	dee, err := st.CreatePerson("Dee", "Dee@Example.org")
	if err != nil || dee.Email != "dee@example.org" {
		t.Fatalf("with an email: %+v %v", dee, err)
	}
	if _, err := st.CreatePerson("Another Dee", "dee@example.org"); !errors.Is(err, ErrEmailTaken) {
		t.Errorf("an email taken: %v", err)
	}
	if _, err := st.CreatePerson("Bang", "!"+cy.ID); !errors.Is(err, ErrInvalidUser) {
		t.Errorf("an email like none: %v", err)
	}
	// Two people without emails are both kept.
	if _, err := st.CreatePerson("Eve", ""); err != nil {
		t.Errorf("a second without an email: %v", err)
	}
	if _, err := st.GetUserByEmail(""); !errors.Is(err, ErrUserNotFound) {
		t.Errorf("found by no email: %v", err)
	}
	if _, err := st.GetUserByEmail("!" + cy.ID); !errors.Is(err, ErrUserNotFound) {
		t.Errorf("found by the email stored for none: %v", err)
	}
	if u, _ := st.GetUserByID(cy.ID); u.Email != "" {
		t.Errorf("reads with an email: %q", u.Email)
	}

	// No session of theirs works, and they hold no roles.
	sess, err := st.CreateSession(cy.ID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.GetSession(sess.ID); !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("a session of theirs: %v", err)
	}
	role, err := st.CreateRole(RoleInput{Name: "Readers", Permissions: []string{"assets.read"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetUserRoles(cy.ID, []string{role.ID}); !errors.Is(err, ErrNoAccount) {
		t.Errorf("given a role: %v", err)
	}
	people, err := st.ListPeople()
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range people {
		if p.ID == cy.ID {
			t.Error("lendable to a person without an account")
		}
	}

	// A password gives them an account.
	if err := st.SetPassword(cy.ID, "a long enough password", ""); err != nil {
		t.Fatal(err)
	}
	if u, _ := st.GetUserByID(cy.ID); !u.CanSignIn {
		t.Error("a password did not let them sign in")
	}
	if err := st.SetUserRoles(cy.ID, []string{role.ID}); err != nil {
		t.Errorf("roles once they can sign in: %v", err)
	}
}

// TestPersonFieldsStoreUserIDs: a person field holds user IDs, normalized,
// indexed in asset_field_people, naming only people who exist and one
// person unless it names several; renaming someone changes what every
// field shows; assets are found by whom a field names; and a field
// changes to or from a person field only while it has no values.
func TestPersonFieldsStoreUserIDs(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	keeper, cat := personField(t, st, "keeper", false)
	trained, _ := personField(t, st, "trained", true)
	if err := st.AssignCategoryField(cat, trained.ID, false); err != nil {
		t.Fatal(err)
	}
	ada, _ := st.CreateNamedUser("ada@hackspace.org", "Ada", "a long enough password", true)
	bob, _ := st.CreateNamedUser("bob@hackspace.org", "Bob", "a long enough password", true)
	cy, _ := st.CreatePerson("Cy", "")

	if _, err := st.CreateAsset("Lathe", []string{cat}, nil, map[string]string{"keeper": ada.ID + "," + bob.ID}); !errors.Is(err, fields.ErrInvalidFieldValue) {
		t.Errorf("two people in a field naming one: %v", err)
	}
	if _, err := st.CreateAsset("Lathe", []string{cat}, nil, map[string]string{"keeper": "00000000-0000-4000-8000-000000000000"}); !errors.Is(err, fields.ErrInvalidFieldValue) {
		t.Errorf("an ID naming no one: %v", err)
	}
	if _, err := st.CreateAsset("Lathe", []string{cat}, nil, map[string]string{"keeper": "Ada Lovelace"}); !errors.Is(err, fields.ErrInvalidFieldValue) {
		t.Errorf("a name, not an ID: %v", err)
	}
	lathe, err := st.CreateAsset("Lathe", []string{cat}, nil, map[string]string{"keeper": ada.ID, "trained": " " + bob.ID + ", " + cy.ID + "," + bob.ID})
	if err != nil {
		t.Fatal(err)
	}
	if got := lathe.FieldValues["trained"]; got != bob.ID+","+cy.ID {
		t.Errorf("stored as %q", got)
	}
	if got := fieldPeopleRows(t, st, lathe.Asset.ID, trained.ID); !reflect.DeepEqual(got, sortedIDs(bob.ID, cy.ID)) {
		t.Errorf("indexed as %v", got)
	}
	if _, ok := lathe.PersonFields["keeper"]; !ok || lathe.People[ada.ID].Name != "Ada" || !lathe.People[cy.ID].NoAccount {
		t.Errorf("people read: %+v %+v", lathe.PersonFields, lathe.People)
	}
	if got := lathe.FieldText("trained"); got != "Bob, Cy" {
		t.Errorf("as text: %q", got)
	}

	// Renamed, Bob shows by his new name wherever he is named.
	if _, err := st.SetUserName(bob.ID, "Robert"); err != nil {
		t.Fatal(err)
	}
	if a, _ := st.GetAssetByID(lathe.Asset.ID); a.FieldText("trained") != "Robert, Cy" {
		t.Errorf("after the rename: %q", a.FieldText("trained"))
	}

	// Found by whom a field names, in that field or any.
	drill := asset(t, st, "Drill", cat)
	if _, err := st.UpdateAsset(drill, "", nil, nil, map[string]string{"keeper": bob.ID}); err != nil {
		t.Fatal(err)
	}
	found := func(f AssetFilter) []string {
		t.Helper()
		list, err := st.FilterAssets(f)
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, a := range list {
			names = append(names, a.Asset.Name)
		}
		return names
	}
	if got := found(AssetFilter{Person: bob.ID}); !reflect.DeepEqual(got, []string{"Drill", "Lathe"}) {
		t.Errorf("Bob in any field: %v", got)
	}
	if got := found(AssetFilter{Person: bob.ID, PersonField: "keeper"}); !reflect.DeepEqual(got, []string{"Drill"}) {
		t.Errorf("Bob as keeper: %v", got)
	}
	// Clearing a value takes them out.
	if _, err := st.UpdateAsset(drill, "", nil, nil, map[string]string{"keeper": ""}); err != nil {
		t.Fatal(err)
	}
	if got := fieldPeopleRows(t, st, drill, keeper.ID); len(got) != 0 {
		t.Errorf("cleared, still indexed: %v", got)
	}

	// History records whom it named by name, as a person change.
	events, _, err := st.ListAssetEvents(lathe.Asset.ID, "", 10)
	if err != nil {
		t.Fatal(err)
	}
	var created []models.EventChange
	for _, e := range events {
		if e.Kind == models.EventAssetCreated {
			created = e.Payload.Changes
		}
	}
	saw := false
	for _, c := range created {
		if c.What == PersonChange && c.Key == "keeper" && c.To == "Ada" {
			saw = true
		}
		if strings.Contains(c.To, "@") || strings.Contains(c.To, ada.ID) {
			t.Errorf("history shows an email or an ID: %+v", c)
		}
	}
	if !saw {
		t.Errorf("history: %+v", created)
	}

	// A person field with values stays one; an empty one changes freely.
	if _, err := st.UpdateField(keeper.ID, "Keeper", fields.TypeText, nil); !errors.Is(err, ErrFieldInUse) {
		t.Errorf("made text while naming people: %v", err)
	}
	if _, err := st.UpdateField(trained.ID, "Trained", fields.TypePerson, nil); !errors.Is(err, ErrFieldInUse) {
		t.Errorf("made to name one while naming two: %v", err)
	}
	notes, _ := st.CreateField("notes", "Notes", fields.TypeText, nil)
	if _, err := st.UpdateField(notes.ID, "Notes", fields.TypePerson, nil); err != nil {
		t.Errorf("an unused field made a person field: %v", err)
	}
	if _, err := st.UpdateField(keeper.ID, "Keeper", fields.TypePerson, json.RawMessage(`{"public":true}`)); err != nil {
		t.Errorf("made public: %v", err)
	}
	if _, err := st.UpdateField(keeper.ID, "Keeper", fields.TypePerson, json.RawMessage(`{"public":"yes"}`)); err == nil {
		t.Error("options that are not true or false")
	}
}

// TestChoosablePeople are the readers of assets there and every active
// person without an account, by name; never the archived.
func TestChoosablePeople(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	role, _ := st.CreateRole(RoleInput{Name: "Readers", Permissions: []string{"assets.read"}})
	ada, _ := st.CreateNamedUser("ada@hackspace.org", "Ada", "a long enough password", true)
	_ = st.SetUserRoles(ada.ID, []string{role.ID})
	st.CreateNamedUser("nobody@hackspace.org", "Nobody", "a long enough password", true)
	cy, _ := st.CreatePerson("Cy", "")
	gone, _ := st.CreatePerson("Gone", "")
	if err := st.DeleteUser(gone.ID); err != nil {
		t.Fatal(err)
	}
	got, err := st.ChoosablePeople([]string{"default"})
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, p := range got {
		names = append(names, p.Name)
		if p.ID == cy.ID && !p.NoAccount {
			t.Error("Cy not marked as without an account")
		}
	}
	if !reflect.DeepEqual(names, []string{"Ada", "Cy"}) {
		t.Errorf("choosable: %v", names)
	}
	if m := MatchPeople(got, "c"); len(m) != 1 || m[0].Name != "Cy" {
		t.Errorf("matching c: %+v", m)
	}

	// Found by ID, email or a name only one has; a name two have is not.
	if u, err := st.FindPerson("ADA@hackspace.org"); err != nil || u.ID != ada.ID {
		t.Errorf("by email: %v %v", u, err)
	}
	if u, err := st.FindPerson("cy"); err != nil || u.ID != cy.ID {
		t.Errorf("by name: %v %v", u, err)
	}
	st.CreatePerson("Cy", "")
	if _, err := st.FindPerson("Cy"); !errors.Is(err, ErrPersonAmbiguous) {
		t.Errorf("a name two have: %v", err)
	}
	if _, err := st.FindPerson("Gone"); !errors.Is(err, ErrPersonNotFound) {
		t.Errorf("the archived: %v", err)
	}
}

// TestMergingAPerson: merging a person without an account into another
// moves every field naming them, records it in each asset's history, and
// archives them; someone who can sign in is never merged.
func TestMergingAPerson(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	keeper, cat := personField(t, st, "keeper", false)
	trained, _ := personField(t, st, "trained", true)
	_ = st.AssignCategoryField(cat, trained.ID, false)
	admin, _ := st.CreateNamedUser("admin@hackspace.org", "Admin", "a long enough password", true)
	ada, _ := st.CreateNamedUser("ada@hackspace.org", "Ada", "a long enough password", true)
	cy, _ := st.CreatePerson("Cy", "")
	lathe, _ := st.CreateAsset("Lathe", []string{cat}, nil, map[string]string{"keeper": cy.ID, "trained": cy.ID + "," + ada.ID})
	drill, _ := st.CreateAsset("Drill", []string{cat}, nil, map[string]string{"keeper": ada.ID})

	if _, err := st.MergePerson(ada.ID, cy.ID); !errors.Is(err, ErrHasAccount) {
		t.Errorf("merged someone who can sign in: %v", err)
	}
	if _, err := st.MergePerson(cy.ID, cy.ID); !errors.Is(err, ErrInvalidUser) {
		t.Errorf("merged into themselves: %v", err)
	}
	n, err := st.As(admin.ID).MergePerson(cy.ID, ada.ID)
	if err != nil || n != 1 {
		t.Fatalf("merged %d: %v", n, err)
	}
	a, _ := st.GetAssetByID(lathe.Asset.ID)
	if a.FieldValues["keeper"] != ada.ID || a.FieldValues["trained"] != ada.ID {
		t.Errorf("after merging: %v", a.FieldValues)
	}
	if got := fieldPeopleRows(t, st, lathe.Asset.ID, keeper.ID); !reflect.DeepEqual(got, []string{ada.ID}) {
		t.Errorf("indexed: %v", got)
	}
	if got := fieldPeopleRows(t, st, lathe.Asset.ID, trained.ID); !reflect.DeepEqual(got, []string{ada.ID}) {
		t.Errorf("indexed: %v", got)
	}
	events, _, _ := st.ListAssetEvents(lathe.Asset.ID, "", 1)
	if len(events) != 1 || events[0].Payload.Note != "Merged Cy into Ada." || events[0].ActorUserID == nil || *events[0].ActorUserID != admin.ID {
		t.Fatalf("history: %+v", events)
	}
	if c := events[0].Payload.Changes; len(c) != 2 || c[0].What != PersonChange || c[0].From != "Cy" || c[0].To != "Ada" {
		t.Errorf("changes: %+v", c)
	}
	if others, _, _ := st.ListAssetEvents(drill.Asset.ID, "", 10); len(others) != 1 {
		t.Errorf("an asset not naming them changed: %+v", others)
	}
	if u, _ := st.GetUserByID(cy.ID); u.ArchivedAt == nil {
		t.Error("not archived")
	}
	if _, err := st.MergePerson(cy.ID, ada.ID); !errors.Is(err, ErrUserNotFound) {
		t.Errorf("merged twice: %v", err)
	}
}

// TestJoiningAPerson: a provider's first sign-in joins only a person who
// cannot sign in, with no password and no identity, and lets them sign in.
func TestJoiningAPerson(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	p := testProvider(t, st, "hackspace")
	cy, _ := st.CreatePerson("Cy", "cy@hackspace.org")
	u, err := st.JoinPerson(cy.ID, p.ID, "cy-subject", "cy@hackspace.org")
	if err != nil || !u.CanSignIn {
		t.Fatalf("joined: %+v %v", u, err)
	}
	if _, err := st.JoinPerson(cy.ID, p.ID, "another", "cy@hackspace.org"); !errors.Is(err, ErrHasAccount) {
		t.Errorf("joined twice: %v", err)
	}
	ada, _ := st.CreateNamedUser("ada@hackspace.org", "Ada", "a long enough password", true)
	if _, err := st.JoinPerson(ada.ID, p.ID, "ada-subject", "ada@hackspace.org"); !errors.Is(err, ErrHasAccount) {
		t.Errorf("joined an account: %v", err)
	}
	// Someone made to look like a person without an account, but holding
	// a password or an identity, is not joined either.
	dee, _ := st.CreatePerson("Dee", "dee@hackspace.org")
	if _, err := st.Exec("UPDATE users SET password_hash = 'x' WHERE id = ?", dee.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.JoinPerson(dee.ID, p.ID, "dee-subject", "dee@hackspace.org"); !errors.Is(err, ErrHasAccount) {
		t.Errorf("joined someone with a password: %v", err)
	}
	eve, _ := st.CreatePerson("Eve", "eve@hackspace.org")
	if _, err := st.Exec("INSERT INTO user_identities (id, user_id, provider_id, subject, email, created_at) VALUES (?, ?, ?, ?, ?, ?)",
		"eve-identity", eve.ID, p.ID, "eve-old", "eve@hackspace.org", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if _, err := st.JoinPerson(eve.ID, p.ID, "eve-subject", "eve@hackspace.org"); !errors.Is(err, ErrHasAccount) {
		t.Errorf("joined someone with an identity: %v", err)
	}
	if ids, _ := st.UserIdentities(eve.ID); len(ids) != 1 {
		t.Errorf("an identity linked anyway: %+v", ids)
	}
}

// TestImportedPersonFieldsNameOnlyPeopleHere: an import's person fields
// keep only the people this instance has.
func TestImportedPersonFieldsNameOnlyPeopleHere(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	trained, cat := personField(t, st, "trained", true)
	ada, _ := st.CreateNamedUser("ada@hackspace.org", "Ada", "a long enough password", true)
	lathe := asset(t, st, "Lathe", cat)
	elsewhere := "11111111-1111-4111-8111-111111111111"
	if _, err := st.Exec("INSERT INTO asset_field_values (asset_id, field_id, value, updated_at) VALUES (?, ?, ?, ?)", lathe, trained.ID, elsewhere+","+ada.ID, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	drill := asset(t, st, "Drill", cat)
	if _, err := st.Exec("INSERT INTO asset_field_values (asset_id, field_id, value, updated_at) VALUES (?, ?, ?, ?)", drill, trained.ID, elsewhere, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	tx, err := st.DB.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := st.RecordImportedTx(tx, []string{lathe, drill}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if a, _ := st.GetAssetByID(lathe); a.FieldValues["trained"] != ada.ID {
		t.Errorf("kept %q", a.FieldValues["trained"])
	}
	if got := fieldPeopleRows(t, st, lathe, trained.ID); !reflect.DeepEqual(got, []string{ada.ID}) {
		t.Errorf("indexed %v", got)
	}
	if a, _ := st.GetAssetByID(drill); a.FieldValues["trained"] != "" {
		t.Errorf("naming no one here, kept %q", a.FieldValues["trained"])
	}
}

// TestPeopleWhoLeaveStayInFields: deactivating or archiving someone leaves
// the fields naming them set, marked as no longer active.
func TestPeopleWhoLeaveStayInFields(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	_, cat := personField(t, st, "keeper", false)
	st.CreateNamedUser("admin@hackspace.org", "Admin", "a long enough password", true)
	admin, _ := st.GetUserByEmail("admin@hackspace.org")
	role, _ := st.GetRoleByName(AdminRoleName)
	_ = st.AssignUserRole(admin.ID, role.ID)
	ada, _ := st.CreateNamedUser("ada@hackspace.org", "Ada", "a long enough password", true)
	lathe, _ := st.CreateAsset("Lathe", []string{cat}, nil, map[string]string{"keeper": ada.ID})
	if err := st.UpdateUserStatus(ada.ID, false); err != nil {
		t.Fatal(err)
	}
	a, _ := st.GetAssetByID(lathe.Asset.ID)
	if a.FieldValues["keeper"] != ada.ID || !a.People[ada.ID].Inactive {
		t.Errorf("deactivated: %v %+v", a.FieldValues, a.People)
	}
	if err := st.DeleteUser(ada.ID); err != nil {
		t.Fatal(err)
	}
	if a, _ := st.GetAssetByID(lathe.Asset.ID); a.People[ada.ID].Name != "Ada" || !a.People[ada.ID].Inactive {
		t.Errorf("archived: %+v", a.People)
	}
}
