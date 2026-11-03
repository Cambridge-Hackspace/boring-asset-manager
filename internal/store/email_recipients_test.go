package store

import (
	"strings"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
)

func TestEmailRecipientsFollowRolesScopesAndPreferences(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()

	keepers, err := st.CreateRole(RoleInput{Name: "Keepers", Permissions: []string{"assets.read"}})
	if err != nil {
		t.Fatal(err)
	}
	woodshop, err := st.CreateScope("woodshop")
	if err != nil {
		t.Fatal(err)
	}
	cat, err := st.CreateCategory("Saws", nil, "#000000", false, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.SetCategoryScopes(cat.ID, []string{woodshop.ID}); err != nil {
		t.Fatal(err)
	}
	other, err := st.CreateCategory("Solder", nil, "#000000", false, false)
	if err != nil {
		t.Fatal(err)
	}

	user := func(email string, active bool, roles ...models.RoleAssignment) *models.User {
		u, err := st.CreateUser(email, "Password123!", active)
		if err != nil {
			t.Fatal(err)
		}
		if err := st.SetUserRoleAssignments(u.ID, roles); err != nil {
			t.Fatal(err)
		}
		return u
	}
	user("everywhere@example.org", true, models.RoleAssignment{RoleID: keepers.ID})
	user("woodshop@example.org", true, models.RoleAssignment{RoleID: keepers.ID, ScopeIDs: []string{woodshop.ID}})
	user("inactive@example.org", false, models.RoleAssignment{RoleID: keepers.ID})
	user("norole@example.org", true)
	quiet := user("quiet@example.org", true, models.RoleAssignment{RoleID: keepers.ID})
	if _, err := st.SetUserEmailNotifications(quiet.ID, false); err != nil {
		t.Fatal(err)
	}

	about := func(id string) models.Event { return models.Event{Kind: models.EventRunningLow, CategoryID: &id} }
	recipients := func(e models.Event, everyone bool, roles ...string) string {
		got, err := st.EmailRecipients(roles, e, everyone)
		if err != nil {
			t.Fatal(err)
		}
		return strings.Join(got, ",")
	}

	if got := recipients(about(cat.ID), false, "keepers"); got != "everywhere@example.org,woodshop@example.org" {
		t.Errorf("in the woodshop: %s", got)
	}
	if got := recipients(about(other.ID), false, "KEEPERS "); got != "everywhere@example.org" {
		t.Errorf("elsewhere: %s", got)
	}
	if got := recipients(models.Event{Kind: models.EventAssetOverdue}, false, "keepers"); got != "everywhere@example.org" {
		t.Errorf("about nothing: %s", got)
	}
	if got := recipients(models.Event{Kind: "test"}, true, "keepers"); got != "everywhere@example.org,woodshop@example.org" {
		t.Errorf("a test message: %s", got)
	}
	if got := recipients(about(cat.ID), false, "nobody holds this"); got != "" {
		t.Errorf("an unknown role: %s", got)
	}
	if got := recipients(about(cat.ID), false); got != "" {
		t.Errorf("no roles: %s", got)
	}
}
