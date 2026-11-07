package store

import (
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/markdown"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
)

// TestMentionsAreRecordedOnce: a comment records each person it mentions
// once, by ID, leaving out its author and IDs naming no one; an edit
// records only those it mentions first, and taking a mention out and
// putting it back records no one again. The comment's event names them.
func TestMentionsAreRecordedOnce(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	ada, _ := st.CreateNamedUser("ada@hackspace.org", "Ada", "a long enough password", true)
	bob, _ := st.CreateNamedUser("bob@hackspace.org", "Bob", "a long enough password", true)
	cy, _ := st.CreateNamedUser("cy@hackspace.org", "Cy", "a long enough password", true)
	drill := asset(t, st, "Drill")
	as := st.As(ada.ID)
	nobody := "00000000-0000-4000-8000-000000000000"

	c, err := as.AddComment(CommentTarget{On: models.CommentOnAsset, ID: drill},
		"Hi "+markdown.Mention(bob.ID)+", "+markdown.Mention(ada.ID)+" and "+markdown.Mention(nobody)+"; `"+markdown.Mention(cy.ID)+"`")
	if err != nil {
		t.Fatal(err)
	}
	mentioned := func(want ...string) {
		t.Helper()
		got, err := st.CommentMentions(c.ID)
		if err != nil {
			t.Fatal(err)
		}
		sort.Strings(want)
		if !reflect.DeepEqual(got, append([]string{}, want...)) {
			t.Errorf("mentioned %v, want %v", got, want)
		}
	}
	mentioned(bob.ID)

	events, err := st.CommentHistory(c.ID)
	if err != nil || len(events) != 1 {
		t.Fatalf("history: %v %v", events, err)
	}
	if got := events[0].Payload.Comment.Mentions; !reflect.DeepEqual(got, []models.EventMention{{ID: bob.ID, Name: "Bob"}, {ID: ada.ID, Name: "Ada"}}) {
		t.Errorf("the event's mentions: %+v", got)
	}

	if _, err := as.EditComment(c.ID, markdown.Mention(bob.ID)+" and now "+markdown.Mention(cy.ID)); err != nil {
		t.Fatal(err)
	}
	mentioned(bob.ID, cy.ID)
	if _, err := as.EditComment(c.ID, "no one"); err != nil {
		t.Fatal(err)
	}
	if _, err := as.EditComment(c.ID, "back to "+markdown.Mention(bob.ID)); err != nil {
		t.Fatal(err)
	}
	mentioned(bob.ID, cy.ID)

	due, err := st.DueMentions(time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(due) != 2 || !due[0].Emailed || due[0].Person.Email == "" {
		t.Fatalf("due: %+v", due)
	}
	if ok, err := st.ClaimMention(c.ID, bob.ID, time.Now()); err != nil || !ok {
		t.Fatalf("claiming: %v %v", ok, err)
	}
	if ok, _ := st.ClaimMention(c.ID, bob.ID, time.Now()); ok {
		t.Error("claimed twice")
	}
	if err := st.ReleaseMention(c.ID, bob.ID); err != nil {
		t.Fatal(err)
	}
	if ok, _ := st.ClaimMention(c.ID, bob.ID, time.Now()); !ok {
		t.Error("not claimed once given back")
	}

	// Turned off, a person is still due, to be passed over.
	if u, err := st.SetUserMentionEmails(cy.ID, false); err != nil || u.MentionEmails {
		t.Fatalf("turning mentions off: %+v %v", u, err)
	}
	due, _ = st.DueMentions(time.Now().Add(-time.Hour))
	if len(due) != 1 || due[0].Person.ID != cy.ID || due[0].Emailed {
		t.Errorf("due once Cy turned them off: %+v", due)
	}
	if due, _ := st.DueMentions(time.Now().Add(time.Hour)); len(due) != 0 {
		t.Errorf("mentions older than asked for: %+v", due)
	}
}

// TestRenamingKeepsMentions: mentions are by ID, so a renamed person's
// name now is the one they read with.
func TestRenamingKeepsMentions(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	bob, _ := st.CreateNamedUser("bob@hackspace.org", "Bob", "a long enough password", true)
	nameless, _ := st.CreateUser("nameless@hackspace.org", "a long enough password", true)
	c, err := st.AddComment(CommentTarget{On: models.CommentOnAsset, ID: asset(t, st, "Drill")}, "ask "+markdown.Mention(bob.ID)+" or "+markdown.Mention(nameless.ID))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.SetUserName(bob.ID, "Robert"); err != nil {
		t.Fatal(err)
	}
	names, err := st.MentionNames(markdown.Mentions(c.Body))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(names, map[string]string{bob.ID: "Robert", nameless.ID: UnnamedUser}) {
		t.Errorf("names: %v", names)
	}
	if got, _ := st.CommentMentions(c.ID); len(got) != 2 {
		t.Errorf("mentions after renaming: %v", got)
	}
}

// TestPeopleWhoReadFollowScopes: the people a comment may mention are
// those who may read what it is on: an asset's, by its scopes; a
// category's, those who read the catalog anywhere; never someone who has
// left.
func TestPeopleWhoReadFollowScopes(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	readers, err := st.CreateRole(RoleInput{Name: "Readers", Permissions: []string{"assets.read"}})
	if err != nil {
		t.Fatal(err)
	}
	woodshop, _ := st.CreateScope("woodshop")
	saws, _ := st.CreateCategory("Saws", nil, "#000000", false, false)
	if _, err := st.SetCategoryScopes(saws.ID, []string{woodshop.ID}); err != nil {
		t.Fatal(err)
	}
	solder, _ := st.CreateCategory("Solder", nil, "#000000", false, false)
	person := func(name string, roles ...models.RoleAssignment) *models.User {
		u, err := st.CreateNamedUser(name+"@hackspace.org", name, "a long enough password", true)
		if err != nil {
			t.Fatal(err)
		}
		if err := st.SetUserRoleAssignments(u.ID, roles); err != nil {
			t.Fatal(err)
		}
		return u
	}
	person("Everywhere", models.RoleAssignment{RoleID: readers.ID})
	wood := person("Wood", models.RoleAssignment{RoleID: readers.ID, ScopeIDs: []string{woodshop.ID}})
	person("Nobody")
	gone := person("Gone", models.RoleAssignment{RoleID: readers.ID})
	if err := st.DeleteUser(gone.ID); err != nil {
		t.Fatal(err)
	}

	names := func(on string, scopes []string) []string {
		t.Helper()
		people, err := st.PeopleWhoRead(on, scopes)
		if err != nil {
			t.Fatal(err)
		}
		out := []string{}
		for _, p := range people {
			out = append(out, p.Name)
		}
		sort.Strings(out)
		return out
	}
	sawScopes, _ := st.ScopesForCategories([]string{saws.ID})
	solderScopes, _ := st.ScopesForCategories([]string{solder.ID})
	if got := names(models.CommentOnAsset, sawScopes); !reflect.DeepEqual(got, []string{"Everywhere", "Wood"}) {
		t.Errorf("a saw's: %v", got)
	}
	if got := names(models.CommentOnAsset, solderScopes); !reflect.DeepEqual(got, []string{"Everywhere"}) {
		t.Errorf("solder's: %v", got)
	}
	// A category's go with the catalog, which the public role's
	// public:assets.read opens to everyone signed in, as the API reads it.
	if got := names(models.CommentOnCategory, solderScopes); !reflect.DeepEqual(got, []string{"Everywhere", "Nobody", "Wood"}) {
		t.Errorf("a category's: %v", got)
	}
	if r, err := st.CommentReader(wood.ID, models.CommentOnAsset, solderScopes); err != nil || r.Reads {
		t.Errorf("Wood reads solder: %+v %v", r, err)
	}
	if r, _ := st.CommentReader(wood.ID, models.CommentOnAsset, sawScopes); !r.Reads || r.SeesWho {
		t.Errorf("Wood on a saw: %+v", r)
	}
}
