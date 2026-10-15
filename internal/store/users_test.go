package store

import (
	"errors"
	"strings"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
)

func TestUserCreationAndPermissionsUnion(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()

	// 1. Create a normal user
	user, err := st.CreateUser("dev@hackspace.org", "UserPassword123!", true)
	if err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}

	// Normal user has 0 roles, so 0 permissions
	perms, err := st.GetUserPermissions(user.ID)
	if err != nil {
		t.Fatalf("GetUserPermissions failed: %v", err)
	}
	if len(perms) != 0 {
		t.Errorf("expected 0 permissions for unassigned user, got %d", len(perms))
	}

	// 2. Assign admin role
	adminRole, err := st.GetRoleByName("admin")
	if err != nil {
		t.Fatalf("GetRoleByName(admin) failed: %v", err)
	}

	if err := st.AssignUserRole(user.ID, adminRole.ID); err != nil {
		t.Fatalf("AssignUserRole failed: %v", err)
	}

	// User now inherits full admin permissions union
	permsAfter, err := st.GetUserPermissions(user.ID)
	if err != nil {
		t.Fatalf("GetUserPermissions after role assign failed: %v", err)
	}
	if !permsAfter[rbac.PermUsersManage] {
		t.Errorf("expected user to possess %s", rbac.PermUsersManage)
	}
	if !permsAfter[rbac.PermAssetsRead] {
		t.Errorf("expected user to possess %s", rbac.PermAssetsRead)
	}
}

func TestStoreAntiLockoutEnforcement(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()

	// 1. Create the primary admin
	admin1, err := st.CreateAdminUser("admin1@hackspace.org", "Pass1234!")
	if err != nil {
		t.Fatalf("CreateAdminUser 1 failed: %v", err)
	}

	adminRole, err := st.GetRoleByName("admin")
	if err != nil {
		t.Fatalf("GetRoleByName(admin): %v", err)
	}

	// Verify exactly 1 active admin
	count, err := st.CountActiveAdmins()
	if err != nil || count != 1 {
		t.Fatalf("expected 1 active admin, got count=%d, err=%v", count, err)
	}

	// 2. Test Anti-Lockout on Status Deactivation: must fail
	err = st.UpdateUserStatus(admin1.ID, false)
	if err == nil {
		t.Fatal("expected deactivating sole admin to fail with anti-lockout error")
	}
	if !errors.Is(err, rbac.ErrAntiLockoutTriggered) {
		t.Errorf("expected ErrAntiLockoutTriggered, got %v", err)
	}

	// 3. Test Anti-Lockout on User Deletion: must fail
	err = st.DeleteUser(admin1.ID)
	if err == nil {
		t.Fatal("expected deleting sole admin to fail with anti-lockout error")
	}
	if !errors.Is(err, rbac.ErrAntiLockoutTriggered) {
		t.Errorf("expected ErrAntiLockoutTriggered, got %v", err)
	}

	// 4. Test Anti-Lockout on Admin Role Removal: must fail
	err = st.RemoveUserRole(admin1.ID, adminRole.ID)
	if err == nil {
		t.Fatal("expected revoking admin role from sole admin to fail with anti-lockout error")
	}
	if !errors.Is(err, rbac.ErrAntiLockoutTriggered) {
		t.Errorf("expected ErrAntiLockoutTriggered, got %v", err)
	}

	// 5. Create a second admin
	admin2, err := st.CreateAdminUser("admin2@hackspace.org", "Pass5678!")
	if err != nil {
		t.Fatalf("CreateAdminUser 2 failed: %v", err)
	}

	count2, err := st.CountActiveAdmins()
	if err != nil || count2 != 2 {
		t.Fatalf("expected 2 active admins, got count=%d, err=%v", count2, err)
	}

	// 6. Deactivating admin1 now succeeds because admin2 remains active
	if err := st.UpdateUserStatus(admin1.ID, false); err != nil {
		t.Fatalf("expected deactivating admin1 to succeed with 2 active admins, got %v", err)
	}

	// Verify active admins dropped back to 1
	countAfter, err := st.CountActiveAdmins()
	if err != nil || countAfter != 1 {
		t.Fatalf("expected 1 active admin remaining, got count=%d", countAfter)
	}

	// 7. Now attempting to deactivate or delete admin2 must be blocked!
	err = st.UpdateUserStatus(admin2.ID, false)
	if !errors.Is(err, rbac.ErrAntiLockoutTriggered) {
		t.Errorf("expected ErrAntiLockoutTriggered on admin2, got %v", err)
	}

	err = st.DeleteUser(admin2.ID)
	if !errors.Is(err, rbac.ErrAntiLockoutTriggered) {
		t.Errorf("expected ErrAntiLockoutTriggered on deleting admin2, got %v", err)
	}
}

func TestUserNames(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	u, err := st.CreateNamedUser("ada@hackspace.org", "  Ada Lovelace ", "a long enough password", true)
	if err != nil || u.Name != "Ada Lovelace" {
		t.Fatalf("created %+v, %v", u, err)
	}
	if _, err := st.CreateNamedUser("long@hackspace.org", strings.Repeat("x", 256), "a long enough password", true); !errors.Is(err, ErrInvalidUser) {
		t.Errorf("an overlong name: %v", err)
	}
	if got, _ := st.GetUserByEmail("ADA@hackspace.org"); got == nil || got.Name != "Ada Lovelace" {
		t.Errorf("read back %+v", got)
	}

	// History keeps the name as it read when the change was made.
	id := asset(t, st.As(u.ID), "Drill")
	renamed, err := st.SetUserName(u.ID, "Ada King")
	if err != nil || renamed.Name != "Ada King" {
		t.Fatalf("renamed %+v, %v", renamed, err)
	}
	archive(t, st.As(u.ID), id)
	list := history(t, st, id)
	if a := list[0].Payload.Actor; a == nil || a.Name != "Ada King" {
		t.Errorf("the archiving's actor: %+v", a)
	}
	if a := list[1].Payload.Actor; a == nil || a.Name != "Ada Lovelace" || a.Email != "ada@hackspace.org" {
		t.Errorf("the creation's actor: %+v", a)
	}
	if list, _ := st.ListUsers(); len(list) != 1 || list[0].Name != "Ada King" {
		t.Errorf("listed %+v", list)
	}

	// Clearing it shows the email again; archived and missing users are not renamed.
	if cleared, err := st.SetUserName(u.ID, ""); err != nil || cleared.Name != "" {
		t.Errorf("cleared %+v, %v", cleared, err)
	}
	if _, err := st.SetUserName("no-such-user", "Nobody"); !errors.Is(err, ErrUserNotFound) {
		t.Errorf("a missing user: %v", err)
	}
	other, err := st.CreateUser("grace@hackspace.org", "a long enough password", true)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteUser(other.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SetUserName(other.ID, "Grace"); !errors.Is(err, ErrUserNotFound) {
		t.Errorf("an archived user: %v", err)
	}
}
