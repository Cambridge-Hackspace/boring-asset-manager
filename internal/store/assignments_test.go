package store

import (
	"errors"
	"reflect"
	"sort"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
)

// keysOf lists a permission set, sorted.
func keysOf(perms map[string]bool) []string {
	out := []string{}
	for k, ok := range perms {
		if ok {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

func TestNarrowedAssignments(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	if _, err := st.CreateAdminUser("admin@hackspace.org", "Pass1234!"); err != nil {
		t.Fatal(err)
	}
	tools, err := st.CreateScope("tools")
	if err != nil {
		t.Fatal(err)
	}
	kitchen, err := st.CreateScope("kitchen")
	if err != nil {
		t.Fatal(err)
	}
	editor, err := st.CreateRole(RoleInput{Name: "Editor", Permissions: []string{"assets.read", "kitchen:assets.write", "tools:assets.write", "labels.print"}})
	if err != nil {
		t.Fatal(err)
	}
	reader, err := st.CreateRole(RoleInput{Name: "Reader", Permissions: []string{"assets.read"}})
	if err != nil {
		t.Fatal(err)
	}
	grace, err := st.CreateUser("grace@hackspace.org", "Pass1234!", true)
	if err != nil {
		t.Fatal(err)
	}
	permsOf := func() []string {
		t.Helper()
		perms, err := st.GetUserPermissions(grace.ID)
		if err != nil {
			t.Fatal(err)
		}
		return keysOf(perms)
	}
	set := func(assignments ...models.RoleAssignment) error {
		return st.SetUserRoleAssignments(grace.ID, assignments)
	}

	// Unrestricted works as it always has.
	if err := set(models.RoleAssignment{RoleID: editor.ID}); err != nil {
		t.Fatal(err)
	}
	if got, want := permsOf(), []string{"assets.read", "kitchen:assets.write", "labels.print", "tools:assets.write"}; !reflect.DeepEqual(got, want) {
		t.Errorf("unrestricted: %v, want %v", got, want)
	}

	// Narrowed to tools: what it holds everywhere, in tools; what it holds
	// in some scopes, where both name; and labels.print, not at all.
	if err := set(models.RoleAssignment{RoleID: editor.ID, ScopeIDs: []string{tools.ID}}); err != nil {
		t.Fatal(err)
	}
	if got, want := permsOf(), []string{"tools:assets.read", "tools:assets.write"}; !reflect.DeepEqual(got, want) {
		t.Errorf("in tools: %v, want %v", got, want)
	}
	all, err := st.UserRoleAssignments()
	if err != nil {
		t.Fatal(err)
	}
	if want := []models.RoleAssignment{{RoleID: editor.ID, ScopeIDs: []string{tools.ID}}}; !reflect.DeepEqual(all[grace.ID], want) {
		t.Errorf("assignments: %+v", all[grace.ID])
	}
	if names, err := st.RoleScopesOf(grace.ID); err != nil || !reflect.DeepEqual(names, map[string][]string{"Editor": {"tools"}}) {
		t.Errorf("RoleScopesOf = %v, %v", names, err)
	}

	// The same role twice merges over the union of its scopes, and an
	// unrestricted one beside it wins. Other roles add up as ever.
	if err := set(models.RoleAssignment{RoleID: editor.ID, ScopeIDs: []string{tools.ID}}, models.RoleAssignment{RoleID: editor.ID, ScopeIDs: []string{kitchen.ID, tools.ID}}); err != nil {
		t.Fatal(err)
	}
	all, _ = st.UserRoleAssignments()
	if got := all[grace.ID]; len(got) != 1 || len(got[0].ScopeIDs) != 2 {
		t.Errorf("merged: %+v", got)
	}
	if err := set(models.RoleAssignment{RoleID: editor.ID, ScopeIDs: []string{tools.ID}}, models.RoleAssignment{RoleID: editor.ID},
		models.RoleAssignment{RoleID: reader.ID, ScopeIDs: []string{kitchen.ID}}); err != nil {
		t.Fatal(err)
	}
	if got, want := permsOf(), []string{"assets.read", "kitchen:assets.read", "kitchen:assets.write", "labels.print", "tools:assets.write"}; !reflect.DeepEqual(got, want) {
		t.Errorf("unrestricted beside narrowed: %v, want %v", got, want)
	}

	// Refused: an unknown scope or role, and narrowing the public role.
	// Nothing changes.
	before, _ := st.UserRoleAssignments()
	if err := set(models.RoleAssignment{RoleID: editor.ID, ScopeIDs: []string{"no-such-scope"}}); !errors.Is(err, ErrScopeNotFound) {
		t.Errorf("unknown scope: %v", err)
	}
	if err := set(models.RoleAssignment{RoleID: "no-such-role"}); !errors.Is(err, ErrRoleNotFound) {
		t.Errorf("unknown role: %v", err)
	}
	public, err := st.GetRoleByName(rbac.PublicRole)
	if err != nil {
		t.Fatal(err)
	}
	if err := set(models.RoleAssignment{RoleID: public.ID, ScopeIDs: []string{tools.ID}}); !errors.Is(err, ErrInvalidAssignment) {
		t.Errorf("narrowing the public role: %v", err)
	}
	if after, _ := st.UserRoleAssignments(); !reflect.DeepEqual(before, after) {
		t.Errorf("a refused change altered the roles: %+v", after[grace.ID])
	}

	// A scope an assignment uses is counted and can't be deleted; renamed,
	// the assignment follows it.
	if err := set(models.RoleAssignment{RoleID: reader.ID, ScopeIDs: []string{kitchen.ID}}); err != nil {
		t.Fatal(err)
	}
	if sc, _ := st.GetScope(kitchen.ID); sc.AssignmentCount != 1 {
		t.Errorf("assignment count: %+v", sc)
	}
	if err := st.DeleteScope(kitchen.ID); !errors.Is(err, ErrScopeInUse) {
		t.Errorf("deleting a scope an assignment uses: %v", err)
	}
	if _, err := st.RenameScope(kitchen.ID, "galley"); err != nil {
		t.Fatal(err)
	}
	if got, want := permsOf(), []string{"galley:assets.read"}; !reflect.DeepEqual(got, want) {
		t.Errorf("after renaming: %v, want %v", got, want)
	}

	// Someone who has left holds nothing, so their assignment no longer
	// keeps the scope; it goes with it.
	shelf, err := st.CreateScope("shelf")
	if err != nil {
		t.Fatal(err)
	}
	if err := set(models.RoleAssignment{RoleID: reader.ID, ScopeIDs: []string{shelf.ID}}); err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteUser(grace.ID); err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteScope(shelf.ID); err != nil {
		t.Errorf("deleting a scope only an archived user's role used: %v", err)
	}
}

func TestNarrowedAdminAndAntiLockout(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	admin, err := st.CreateAdminUser("admin@hackspace.org", "Pass1234!")
	if err != nil {
		t.Fatal(err)
	}
	adminRole, err := st.GetRoleByName(AdminRoleName)
	if err != nil {
		t.Fatal(err)
	}
	tools, err := st.CreateScope("tools")
	if err != nil {
		t.Fatal(err)
	}
	narrowed := models.RoleAssignment{RoleID: adminRole.ID, ScopeIDs: []string{tools.ID}}

	// Narrowing the last admin's role takes users.manage from them: refused,
	// as removing it is.
	if err := st.SetUserRoleAssignments(admin.ID, []models.RoleAssignment{narrowed}); !errors.Is(err, rbac.ErrAntiLockoutTriggered) {
		t.Errorf("narrowing the last admin: %v", err)
	}

	// "Admin, in tools" acts on tools' assets, and manages no one.
	ada, err := st.CreateUser("ada@hackspace.org", "Pass1234!", true)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetUserRoleAssignments(ada.ID, []models.RoleAssignment{narrowed}); err != nil {
		t.Fatal(err)
	}
	perms, err := st.GetUserPermissions(ada.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !rbac.Allows(perms, rbac.PermAssetsDelete, []string{"tools"}) || rbac.Allows(perms, rbac.PermAssetsRead, []string{rbac.DefaultScope}) {
		t.Errorf("admin in tools: %v", keysOf(perms))
	}
	for _, p := range []string{rbac.PermUsersManage, rbac.PermSettingsManage, rbac.PermLabelsPrint} {
		if rbac.HasPermission(perms, p) {
			t.Errorf("admin in tools holds %s", p)
		}
	}

	// They don't count as someone who manages users: the real admin still
	// can't give theirs up.
	if n, err := st.CountActiveAdmins(); err != nil || n != 1 {
		t.Errorf("active admins = %d, %v", n, err)
	}
	if err := st.SetUserRoles(admin.ID, nil); !errors.Is(err, rbac.ErrAntiLockoutTriggered) {
		t.Errorf("dropping the only unrestricted admin: %v", err)
	}
}

func TestNarrowedTokens(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	admin, err := st.CreateAdminUser("admin@hackspace.org", "Pass1234!")
	if err != nil {
		t.Fatal(err)
	}
	cabinet, err := st.CreateScope("cabinet-3")
	if err != nil {
		t.Fatal(err)
	}
	reader, err := st.CreateRole(RoleInput{Name: "Reader", Permissions: []string{"assets.read", "labels.print"}})
	if err != nil {
		t.Fatal(err)
	}
	tok, secret, err := st.CreateTokenWithRoles("Dashboard", []models.RoleAssignment{{RoleID: reader.ID, ScopeIDs: []string{cabinet.ID}}}, nil, admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	if want := []models.RoleAssignment{{RoleID: reader.ID, ScopeIDs: []string{cabinet.ID}}}; !reflect.DeepEqual(tok.Roles, want) || !reflect.DeepEqual(tok.RoleIDs, []string{reader.ID}) {
		t.Errorf("token roles: %+v %v", tok.Roles, tok.RoleIDs)
	}
	_, perms, err := st.AuthenticateToken(secret)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := keysOf(perms), []string{"cabinet-3:assets.read"}; !reflect.DeepEqual(got, want) {
		t.Errorf("token permissions: %v, want %v", got, want)
	}
	if _, perms, err := st.TokenPermissions(tok.ID); err != nil || !reflect.DeepEqual(keysOf(perms), []string{"cabinet-3:assets.read"}) {
		t.Errorf("TokenPermissions = %v, %v", keysOf(perms), err)
	}

	// The scope is in use while the token works, and free once it's revoked.
	if err := st.DeleteScope(cabinet.ID); !errors.Is(err, ErrScopeInUse) {
		t.Errorf("deleting a scope a token's role uses: %v", err)
	}
	if _, err := st.RevokeToken(tok.ID); err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteScope(cabinet.ID); err != nil {
		t.Errorf("deleting a scope only a revoked token used: %v", err)
	}

	if _, _, err := st.CreateTokenWithRoles("Bad", []models.RoleAssignment{{RoleID: reader.ID, ScopeIDs: []string{"nope"}}}, nil, admin.ID); !errors.Is(err, ErrScopeNotFound) {
		t.Errorf("a token narrowed to an unknown scope: %v", err)
	}
}

// TestAssignmentsByRoleName lists a holder's roles by name, as the account
// page does, not by their random IDs.
func TestAssignmentsByRoleName(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	admin, err := st.CreateAdminUser("admin@hackspace.org", "Pass1234!")
	if err != nil {
		t.Fatal(err)
	}
	ada, err := st.CreateUser("ada@hackspace.org", "Pass1234!", true)
	if err != nil {
		t.Fatal(err)
	}
	// eight, given out of order, so their IDs falling in name order by chance
	// is one in 40,320
	names := []string{"Fixers", "Borrowers", "Hosts", "Admins of bins", "Editors", "Greeters", "Cleaners", "Drivers"}
	var given []models.RoleAssignment
	byID := map[string]string{}
	for _, name := range names {
		role, err := st.CreateRole(RoleInput{Name: name, Permissions: []string{"assets.read"}})
		if err != nil {
			t.Fatal(err)
		}
		given = append(given, models.RoleAssignment{RoleID: role.ID})
		byID[role.ID] = name
	}
	want := append([]string(nil), names...)
	sort.Strings(want)
	named := func(list []models.RoleAssignment) []string {
		out := []string{}
		for _, a := range list {
			out = append(out, byID[a.RoleID])
		}
		return out
	}

	if err := st.SetUserRoleAssignments(ada.ID, given); err != nil {
		t.Fatal(err)
	}
	all, err := st.UserRoleAssignments()
	if err != nil {
		t.Fatal(err)
	}
	if got := named(all[ada.ID]); !reflect.DeepEqual(got, want) {
		t.Errorf("a user's roles: %v, want %v", got, want)
	}

	tok, _, err := st.CreateTokenWithRoles("Dashboard", given, nil, admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	read, err := st.GetToken(tok.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := named(read.Roles); !reflect.DeepEqual(got, want) {
		t.Errorf("a token's roles: %v, want %v", got, want)
	}
}
