package store

import (
	"errors"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/google/uuid"
)

func TestRemoveUserRoleOnlyGuardsEffectiveRevocations(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()

	admin, err := st.CreateAdminUser("sole-admin@hackspace.org", "Pass1234!")
	if err != nil {
		t.Fatalf("creating sole admin: %v", err)
	}
	adminRole, err := st.GetRoleByName("admin")
	if err != nil {
		t.Fatalf("GetRoleByName(admin): %v", err)
	}

	// An inactive user holding the admin role does not count towards active admins,
	// so revoking it cannot cause a lockout.
	inactive, err := st.CreateUser("inactive@hackspace.org", "Pass1234!", false)
	if err != nil {
		t.Fatalf("creating inactive user: %v", err)
	}
	if err := st.AssignUserRole(inactive.ID, adminRole.ID); err != nil {
		t.Fatalf("assigning admin role: %v", err)
	}
	if err := st.RemoveUserRole(inactive.ID, adminRole.ID); err != nil {
		t.Errorf("revoking admin role from inactive user should succeed, got %v", err)
	}

	// Revoking a role the user never held is a no-op.
	plain, err := st.CreateUser("plain@hackspace.org", "Pass1234!", true)
	if err != nil {
		t.Fatalf("creating plain user: %v", err)
	}
	if err := st.RemoveUserRole(plain.ID, adminRole.ID); err != nil {
		t.Errorf("revoking an unheld role should succeed, got %v", err)
	}

	// The sole admin also holds users:manage through a second role, so dropping the
	// admin role leaves them able to manage users.
	managerRoleID := uuid.NewString()
	if _, err := st.Exec("INSERT INTO roles (id, name, description) VALUES (?, ?, ?)", managerRoleID, "user-manager", "Manages users"); err != nil {
		t.Fatalf("creating manager role: %v", err)
	}
	var manageID string
	if err := st.QueryRow("SELECT id FROM permissions WHERE permission_key = ?", rbac.PermUsersManage).Scan(&manageID); err != nil {
		t.Fatalf("looking up users:manage: %v", err)
	}
	if _, err := st.Exec("INSERT INTO role_permissions (role_id, permission_id) VALUES (?, ?)", managerRoleID, manageID); err != nil {
		t.Fatalf("granting users:manage: %v", err)
	}
	if err := st.AssignUserRole(admin.ID, managerRoleID); err != nil {
		t.Fatalf("assigning manager role: %v", err)
	}
	if err := st.RemoveUserRole(admin.ID, adminRole.ID); err != nil {
		t.Errorf("revoking admin role while users:manage is retained via another role should succeed, got %v", err)
	}

	// Revoking the last role that grants users:manage must still be blocked.
	err = st.RemoveUserRole(admin.ID, managerRoleID)
	if !errors.Is(err, rbac.ErrAntiLockoutTriggered) {
		t.Errorf("expected ErrAntiLockoutTriggered when revoking the last users:manage grant, got %v", err)
	}
}

func TestArchivedAdminCannotBeReactivatedToBypassAntiLockout(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()

	admin1, err := st.CreateAdminUser("admin1@hackspace.org", "Pass1234!")
	if err != nil {
		t.Fatalf("creating admin1: %v", err)
	}
	admin2, err := st.CreateAdminUser("admin2@hackspace.org", "Pass1234!")
	if err != nil {
		t.Fatalf("creating admin2: %v", err)
	}

	if err := st.DeleteUser(admin2.ID); err != nil {
		t.Fatalf("deleting admin2: %v", err)
	}

	if err := st.UpdateUserStatus(admin2.ID, true); !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("expected reactivating an archived user to return ErrUserNotFound, got %v", err)
	}

	// Even if an archived row were flagged active, it must not count as an admin:
	// archived users cannot log in.
	if _, err := st.Exec("UPDATE users SET is_active = ? WHERE id = ?", true, admin2.ID); err != nil {
		t.Fatalf("forcing archived user active: %v", err)
	}
	count, err := st.CountActiveAdmins()
	if err != nil {
		t.Fatalf("CountActiveAdmins: %v", err)
	}
	if count != 1 {
		t.Errorf("expected archived users to be excluded from active admin count, got %d", count)
	}

	if err := st.UpdateUserStatus(admin1.ID, false); !errors.Is(err, rbac.ErrAntiLockoutTriggered) {
		t.Errorf("expected deactivating the last loginable admin to be blocked, got %v", err)
	}
}
