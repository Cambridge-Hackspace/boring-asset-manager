package store

import (
	"errors"
	"reflect"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
)

func testRole(t *testing.T, st *Store, name string, perms ...string) string {
	t.Helper()
	r, err := st.CreateRole(RoleInput{Name: name, Permissions: perms})
	if err != nil {
		t.Fatal(err)
	}
	return r.ID
}

func testProvider(t *testing.T, st *Store, key string, rules ...ProviderRule) *Provider {
	t.Helper()
	p, err := st.CreateProvider(ProviderInput{Key: key, Name: "Hackspace " + key, Protocol: "test", Enabled: true, Rules: rules,
		Settings: map[string]string{"subject": "s"}})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// assignmentsOf is a user's roles, each with its scopes.
func assignmentsOf(t *testing.T, st *Store, userID string) map[string][]string {
	t.Helper()
	all, err := st.UserRoleAssignments()
	if err != nil {
		t.Fatal(err)
	}
	out := map[string][]string{}
	for _, a := range all[userID] {
		out[a.RoleID] = a.ScopeIDs
	}
	return out
}

func TestProvidersAreSavedWithTheirRules(t *testing.T) {
	st := setupTestStore(t)
	readers := testRole(t, st, "Readers", rbac.PermAssetsRead)
	wood, err := st.CreateScope("woodshop")
	if err != nil {
		t.Fatal(err)
	}
	p, err := st.CreateProvider(ProviderInput{
		Key: "hackspace", Name: "Hackspace", Protocol: "test", Enabled: true,
		Settings:       map[string]string{"subject": "s"},
		Seal:           func(id string) (string, error) { return "sealed-for-" + id, nil },
		AllowedDomains: []string{" @Hackspace.org ", ""},
		Rules:          []ProviderRule{{Claim: "groups", Value: "wood", RoleID: readers, ScopeIDs: []string{wood.ID}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if p.ButtonLabel != "Sign in with Hackspace" || p.SealedSecrets != "sealed-for-"+p.ID || !reflect.DeepEqual(p.AllowedDomains, []string{"hackspace.org"}) {
		t.Errorf("saved: %+v", p)
	}
	if len(p.Rules) != 1 || p.Rules[0].RoleID != readers || !reflect.DeepEqual(p.Rules[0].ScopeIDs, []string{wood.ID}) {
		t.Errorf("rules: %+v", p.Rules)
	}

	// Saved again without new secrets, the old ones stay.
	p, err = st.UpdateProvider(p.ID, ProviderInput{Key: "hackspace", Name: "Hackspace", Protocol: "test", ButtonLabel: "Members"})
	if err != nil {
		t.Fatal(err)
	}
	if p.SealedSecrets != "sealed-for-"+p.ID || p.ButtonLabel != "Members" || len(p.Rules) != 0 || p.Enabled {
		t.Errorf("updated: %+v", p)
	}

	for what, in := range map[string]ProviderInput{
		"a bad key":       {Key: "Hack Space", Name: "x", Protocol: "test"},
		"no name":         {Key: "x", Protocol: "test"},
		"a rule's role":   {Key: "x", Name: "x", Protocol: "test", Rules: []ProviderRule{{Claim: "groups", Value: "a", RoleID: "nope"}}},
		"a rule's claim":  {Key: "x", Name: "x", Protocol: "test", Rules: []ProviderRule{{Value: "a", RoleID: readers}}},
		"a default role":  {Key: "x", Name: "x", Protocol: "test", DefaultRoleIDs: []string{"nope"}},
		"the same key":    {Key: "hackspace", Name: "Another", Protocol: "test"},
		"a scope missing": {Key: "x", Name: "x", Protocol: "test", Rules: []ProviderRule{{Claim: "g", Value: "a", RoleID: readers, ScopeIDs: []string{"nope"}}}},
	} {
		if _, err := st.CreateProvider(in); err == nil {
			t.Errorf("%s: saved", what)
		}
	}
	if _, err := st.CreateProvider(ProviderInput{Key: "hackspace", Name: "Another", Protocol: "test"}); !errors.Is(err, ErrProviderKeyExists) {
		t.Errorf("the same key: %v", err)
	}
}

func TestProvidersFromTheConfiguration(t *testing.T) {
	st := setupTestStore(t)
	if err := st.SyncConfigProviders([]ProviderInput{{Key: "corp", Name: "Corp", Protocol: "test", Enabled: true}}); err != nil {
		t.Fatal(err)
	}
	p, err := st.GetProviderByKey("corp")
	if err != nil || !p.FromConfig {
		t.Fatalf("synced: %+v, %v", p, err)
	}
	if _, err := st.UpdateProvider(p.ID, ProviderInput{Key: "corp", Name: "Changed", Protocol: "test"}); !errors.Is(err, ErrProviderFromConfig) {
		t.Errorf("changed under Admin: %v", err)
	}
	if err := st.DeleteProvider(p.ID); !errors.Is(err, ErrProviderFromConfig) {
		t.Errorf("deleted under Admin: %v", err)
	}
	// Changed in the file, it changes here; gone from it, it goes.
	if err := st.SyncConfigProviders([]ProviderInput{{Key: "corp", Name: "Corporate", Protocol: "test"}}); err != nil {
		t.Fatal(err)
	}
	if p, _ := st.GetProviderByKey("corp"); p == nil || p.Name != "Corporate" {
		t.Errorf("changed in the file: %+v", p)
	}
	if err := st.SyncConfigProviders(nil); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetProviderByKey("corp"); !errors.Is(err, ErrProviderNotFound) {
		t.Errorf("gone from the file: %v", err)
	}
	// One of the same key under Admin is not taken over.
	testProvider(t, st, "local")
	if err := st.SyncConfigProviders([]ProviderInput{{Key: "local", Name: "Local", Protocol: "test"}}); err == nil {
		t.Error("took over a provider set up under Admin")
	}
}

func TestIdentitiesAndTheLastWayIn(t *testing.T) {
	st := setupTestStore(t)
	p := testProvider(t, st, "hackspace")
	other := testProvider(t, st, "other")
	ada, err := st.CreateProvisionedUser("Ada@Hackspace.org", "Ada", nil)
	if err != nil {
		t.Fatal(err)
	}
	if ada.Email != "ada@hackspace.org" || HasPassword(ada) {
		t.Errorf("made: %+v", ada)
	}
	if _, err := st.CreateProvisionedUser("ada@hackspace.org", "Ada again", nil); !errors.Is(err, ErrEmailTaken) {
		t.Errorf("the same email: %v", err)
	}
	bob, err := st.CreateUser("bob@hackspace.org", "BobSecret123!", true)
	if err != nil {
		t.Fatal(err)
	}

	i, err := st.LinkIdentity(ada.ID, p.ID, "sub-ada", "ada@hackspace.org")
	if err != nil {
		t.Fatal(err)
	}
	if found, err := st.IdentityBySubject(p.ID, "sub-ada"); err != nil || found.UserID != ada.ID || found.ProviderKey != "hackspace" {
		t.Errorf("by subject: %+v, %v", found, err)
	}
	// A subject is one account's, at one provider.
	if _, err := st.LinkIdentity(bob.ID, p.ID, "sub-ada", ""); !errors.Is(err, ErrIdentityTaken) {
		t.Errorf("another's subject: %v", err)
	}
	if _, err := st.IdentityBySubject(other.ID, "sub-ada"); !errors.Is(err, ErrIdentityNotFound) {
		t.Errorf("at another provider: %v", err)
	}

	// Ada has no password: her only identity is her way in.
	if err := st.UnlinkIdentity(ada.ID, i.ID, true); !errors.Is(err, ErrLastWayIn) {
		t.Errorf("Ada's last way in: %v", err)
	}
	j, err := st.LinkIdentity(ada.ID, other.ID, "ada-elsewhere", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.UnlinkIdentity(ada.ID, i.ID, true); err != nil {
		t.Errorf("with another: %v", err)
	}
	if list, _ := st.UserIdentities(ada.ID); len(list) != 1 || list[0].ID != j.ID {
		t.Errorf("left: %+v", list)
	}

	// Bob's password counts only while passwords are on.
	k, _ := st.LinkIdentity(bob.ID, p.ID, "sub-bob", "")
	if err := st.UnlinkIdentity(bob.ID, k.ID, false); !errors.Is(err, ErrLastWayIn) {
		t.Errorf("passwords off: %v", err)
	}
	if err := st.UnlinkIdentity(bob.ID, k.ID, true); err != nil {
		t.Errorf("passwords on: %v", err)
	}
	if err := st.UnlinkIdentity(bob.ID, k.ID, true); !errors.Is(err, ErrIdentityNotFound) {
		t.Errorf("again: %v", err)
	}
}

func TestProviderRolesLeaveHandGivenOnes(t *testing.T) {
	st := setupTestStore(t)
	readers := testRole(t, st, "Readers", rbac.PermAssetsRead)
	writers := testRole(t, st, "Writers", rbac.PermAssetsWrite)
	wood, _ := st.CreateScope("woodshop")
	p := testProvider(t, st, "hackspace")
	other := testProvider(t, st, "other")
	u, _ := st.CreateUser("ada@hackspace.org", "AdaSecret123!", true)
	if err := st.SetUserRoles(u.ID, []string{readers}); err != nil {
		t.Fatal(err)
	}

	// Readers is Ada's by hand; Writers comes from the rule, narrowed.
	wanted := []models.RoleAssignment{{RoleID: readers}, {RoleID: writers, ScopeIDs: []string{wood.ID}}}
	if _, err := st.ApplyProviderRoles(u.ID, p.ID, wanted); err != nil {
		t.Fatal(err)
	}
	if got := assignmentsOf(t, st, u.ID); !reflect.DeepEqual(got, map[string][]string{readers: {}, writers: {wood.ID}}) {
		t.Errorf("given: %v", got)
	}
	if grants, _ := st.RoleGrants(u.ID); !reflect.DeepEqual(grants, map[string]string{writers: p.ID}) {
		t.Errorf("who gave them: %v", grants)
	}

	// An admin saving her roles keeps who gave each.
	if err := st.SetUserRoleAssignments(u.ID, []models.RoleAssignment{{RoleID: readers}, {RoleID: writers, ScopeIDs: []string{wood.ID}}}); err != nil {
		t.Fatal(err)
	}
	if grants, _ := st.RoleGrants(u.ID); grants[writers] != p.ID {
		t.Errorf("after an admin saved them: %v", grants)
	}

	// Another provider takes away nothing this one gave.
	if _, err := st.ApplyProviderRoles(u.ID, other.ID, nil); err != nil {
		t.Fatal(err)
	}
	if got := assignmentsOf(t, st, u.ID); len(got) != 2 {
		t.Errorf("after another provider: %v", got)
	}

	// The claim gone, the rule's role goes; the hand-given one stays.
	if _, err := st.ApplyProviderRoles(u.ID, p.ID, nil); err != nil {
		t.Fatal(err)
	}
	if got := assignmentsOf(t, st, u.ID); !reflect.DeepEqual(got, map[string][]string{readers: {}}) {
		t.Errorf("after the claim went: %v", got)
	}

	// Deleting the provider leaves its roles, as given by hand.
	if _, err := st.ApplyProviderRoles(u.ID, p.ID, []models.RoleAssignment{{RoleID: writers}}); err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteProvider(p.ID); err != nil {
		t.Fatal(err)
	}
	if got := assignmentsOf(t, st, u.ID); len(got) != 2 {
		t.Errorf("after deleting the provider: %v", got)
	}
	if grants, _ := st.RoleGrants(u.ID); len(grants) != 0 {
		t.Errorf("grants after deleting the provider: %v", grants)
	}
}

func TestProviderRolesKeepTheLastAdmin(t *testing.T) {
	st := setupTestStore(t)
	admins := testRole(t, st, "Keepers", rbac.PermUsersManage)
	p := testProvider(t, st, "hackspace")
	u, err := st.CreateProvisionedUser("ada@hackspace.org", "Ada", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.ApplyProviderRoles(u.ID, p.ID, []models.RoleAssignment{{RoleID: admins}}); err != nil {
		t.Fatal(err)
	}
	// She holds the last users.manage: losing the claim keeps it.
	kept, err := st.ApplyProviderRoles(u.ID, p.ID, nil)
	if err != nil || !reflect.DeepEqual(kept, []string{"Keepers"}) {
		t.Fatalf("kept %v, %v", kept, err)
	}
	if got := assignmentsOf(t, st, u.ID); len(got) != 1 {
		t.Errorf("roles: %v", got)
	}
	events, _ := st.ListEvents(1)
	if len(events) != 1 || events[0].Kind != models.EventAuthRoleKept || events[0].Payload.Account == nil || events[0].Payload.Account.ID != u.ID {
		t.Errorf("history: %+v", events)
	}

	// With another admin, it goes.
	bob, _ := st.CreateUser("bob@hackspace.org", "BobSecret123!", true)
	if err := st.SetUserRoles(bob.ID, []string{admins}); err != nil {
		t.Fatal(err)
	}
	if kept, err := st.ApplyProviderRoles(u.ID, p.ID, nil); err != nil || len(kept) != 0 {
		t.Errorf("with another admin: %v, %v", kept, err)
	}
	if got := assignmentsOf(t, st, u.ID); len(got) != 0 {
		t.Errorf("roles with another admin: %v", got)
	}
}

func TestSettingsAndSignInsAtProviders(t *testing.T) {
	st := setupTestStore(t)
	if on, err := st.LocalPasswords(); err != nil || !on {
		t.Errorf("by default: %v, %v", on, err)
	}
	for _, v := range []string{"off", "off", "on", "off"} {
		if err := st.SetSetting(LocalPasswordsSetting, v); err != nil {
			t.Fatal(err)
		}
	}
	if on, _ := st.LocalPasswords(); on {
		t.Error("still on")
	}

	p := testProvider(t, st, "hackspace")
	token, err := st.CreateProviderSignIn(ProviderSignIn{ProviderID: p.ID, State: map[string]string{"state": "abc"}, Next: "/assets"})
	if err != nil {
		t.Fatal(err)
	}
	in, err := st.TakeProviderSignIn(token)
	if err != nil || in.ProviderID != p.ID || in.State["state"] != "abc" || in.Next != "/assets" || in.LinkUserID != "" {
		t.Fatalf("taken: %+v, %v", in, err)
	}
	// Once only.
	if _, err := st.TakeProviderSignIn(token); !errors.Is(err, ErrSignInNotFound) {
		t.Errorf("again: %v", err)
	}
	if _, err := st.TakeProviderSignIn("made-up"); !errors.Is(err, ErrSignInNotFound) {
		t.Errorf("made up: %v", err)
	}
}
