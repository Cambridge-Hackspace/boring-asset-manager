package store

import (
	"errors"
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
