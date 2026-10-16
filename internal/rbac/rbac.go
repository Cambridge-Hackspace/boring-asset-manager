package rbac

import (
	"errors"
	"fmt"
)

// Standard permission keys.
const (
	PermAssetsRead       = "assets:read"
	PermAssetsWrite      = "assets:write"
	PermAssetsDelete     = "assets:delete"
	PermTemplatesManage  = "templates:manage"
	PermCategoriesManage = "categories:manage"
	PermSettingsManage   = "settings:manage"
	PermUsersManage      = "users:manage"
	PermLabelsPrint      = "labels:print"
	// PermStockRecord uses, restocks and flags stock without editing assets.
	PermStockRecord = "stock:record"
	// PermAssetsReport reports an asset needing maintenance or out of
	// service, without editing it.
	PermAssetsReport = "assets:report"
)

var (
	// ErrAntiLockoutTriggered is returned when an action would leave zero active users with users:manage.
	ErrAntiLockoutTriggered = errors.New("anti-lockout guard: operation rejected because at least one active user must retain 'users:manage'")
)

// AllPermissions returns the comprehensive list of core system permissions.
func AllPermissions() []string {
	return []string{
		PermAssetsRead,
		PermAssetsWrite,
		PermAssetsDelete,
		PermTemplatesManage,
		PermCategoriesManage,
		PermSettingsManage,
		PermUsersManage,
		PermLabelsPrint,
		PermStockRecord,
		PermAssetsReport,
	}
}

// ResolveUnionPermissions implements the Union Principle:
// When a user is assigned multiple roles, their effective permissions are the
// strict union of all granted rights across all assigned roles.
func ResolveUnionPermissions(rolePermissionSets ...[]string) map[string]bool {
	effective := make(map[string]bool)
	for _, perms := range rolePermissionSets {
		for _, p := range perms {
			if p != "" {
				effective[p] = true
			}
		}
	}
	return effective
}

// HasPermission checks if the effective permissions map contains the target permission.
func HasPermission(effective map[string]bool, perm string) bool {
	return effective[perm]
}

// ValidateAntiLockout enforces the Anti-Lockout Rule:
// System operations must reject any deletion, deactivation, or role removal
// that results in zero active users possessing the `users:manage` permission.
//
// Parameters:
//   - activeAdminsCount: current number of active users possessing PermUsersManage
//   - willRemoveAdmin: whether this specific operation will deactivate, delete,
//     or revoke PermUsersManage from an active user who currently has it.
func ValidateAntiLockout(activeAdminsCount int, willRemoveAdmin bool) error {
	if willRemoveAdmin && activeAdminsCount <= 1 {
		return fmt.Errorf("%w (currently %d active user(s) with %s)", ErrAntiLockoutTriggered, activeAdminsCount, PermUsersManage)
	}
	return nil
}
