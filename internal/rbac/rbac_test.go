package rbac

import (
	"errors"
	"testing"
)

func TestResolveUnionPermissions(t *testing.T) {
	role1 := []string{PermAssetsRead, PermCategoriesManage}
	role2 := []string{PermAssetsWrite, PermTemplatesManage}
	role3 := []string{PermAssetsRead} // Duplicate in role1

	effective := ResolveUnionPermissions(role1, role2, role3)

	expected := []string{
		PermAssetsRead,
		PermCategoriesManage,
		PermAssetsWrite,
		PermTemplatesManage,
	}

	for _, exp := range expected {
		if !HasPermission(effective, exp) {
			t.Errorf("expected permission %s to be present in union", exp)
		}
	}

	if HasPermission(effective, PermUsersManage) {
		t.Errorf("expected permission %s NOT to be present", PermUsersManage)
	}
}

func TestAntiLockoutGuard(t *testing.T) {
	// Case 1: Removing an admin when 2 active admins exist -> Allowed
	if err := ValidateAntiLockout(2, true); err != nil {
		t.Errorf("expected operation to succeed when 2 admins exist, got %v", err)
	}

	// Case 2: Removing an admin when only 1 active admin exists -> Blocked
	err := ValidateAntiLockout(1, true)
	if err == nil {
		t.Fatal("expected anti-lockout error when removing sole active admin, got nil")
	}
	if !errors.Is(err, ErrAntiLockoutTriggered) {
		t.Errorf("expected ErrAntiLockoutTriggered, got %v", err)
	}

	// Case 3: Zero active admins (corrupted state or bootstrap) -> Blocked
	if err := ValidateAntiLockout(0, true); err == nil {
		t.Error("expected error when zero active admins exist")
	}

	// Case 4: Not removing an admin (e.g., editing a regular user) -> Allowed
	if err := ValidateAntiLockout(1, false); err != nil {
		t.Errorf("expected operation to succeed when not removing admin, got %v", err)
	}
}
