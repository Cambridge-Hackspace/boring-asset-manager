package rbac

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Standard permission keys.
const (
	PermAssetsRead       = "assets.read"
	PermAssetsWrite      = "assets.write"
	PermAssetsDelete     = "assets.delete"
	PermTemplatesManage  = "templates.manage"
	PermCategoriesManage = "categories.manage"
	PermSettingsManage   = "settings.manage"
	PermUsersManage      = "users.manage"
	PermLabelsPrint      = "labels.print"
	// PermStockRecord uses, restocks and flags stock without editing assets.
	PermStockRecord = "stock.record"
	// PermAssetsReport reports an asset needing maintenance or out of
	// service, without editing it.
	PermAssetsReport = "assets.report"
	// PermAssetsBorrow checks an asset out to oneself and back in.
	PermAssetsBorrow = "assets.borrow"
	// PermAssetsLend checks an asset out to anyone, and any loan back in.
	PermAssetsLend = "assets.lend"
	// PermReportsManage creates, edits and deletes saved reports.
	PermReportsManage = "reports.manage"
	// PermAssetsTrack sees the people around assets: who borrowed them, who
	// changed them, and the loans list.
	PermAssetsTrack = "assets.track"
	// PermAssetsScan looks an asset up by its code, and sees its scan page.
	PermAssetsScan = "assets.scan"
)

var (
	// ErrAntiLockoutTriggered is returned when an action would leave zero active users with users.manage.
	ErrAntiLockoutTriggered = errors.New("anti-lockout guard: operation rejected because at least one active user must retain 'users.manage'")
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
		PermAssetsBorrow,
		PermAssetsLend,
		PermReportsManage,
		PermAssetsTrack,
		PermAssetsScan,
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

// HasPermission reports whether effective holds perm in all scopes, or, for
// a scopable permission, in at least one: enough to reach the pages and
// endpoints it opens, which then check each asset with Allows.
func HasPermission(effective map[string]bool, perm string) bool {
	if effective[perm] {
		return true
	}
	if !Scopable(perm) {
		return false
	}
	for k, ok := range effective {
		if _, key, scoped := strings.Cut(k, ScopeSeparator); ok && scoped && key == perm {
			return true
		}
	}
	return false
}

// Allows reports whether effective holds perm for an asset in these scopes:
// in all scopes, or in any one of them, since permissions add up.
func Allows(effective map[string]bool, perm string, scopes []string) bool {
	if effective[perm] {
		return true
	}
	if !Scopable(perm) {
		return false
	}
	for _, s := range scopes {
		if effective[s+ScopeSeparator+perm] {
			return true
		}
	}
	return false
}

// Scoped permissions (README §3.3) are written scope:key, as tools:assets.read;
// a key alone holds in every scope.
const ScopeSeparator = ":"

// DefaultScope is the reserved scope of assets that none of their
// categories, nor any ancestor of theirs, puts in a scope.
const DefaultScope = "default"

// scopable are the permissions a role may hold in some scopes only: those
// acting on assets.
var scopable = map[string]bool{
	PermAssetsRead: true, PermAssetsWrite: true, PermAssetsDelete: true,
	PermAssetsReport: true, PermAssetsBorrow: true, PermAssetsLend: true,
	PermAssetsTrack: true,
	PermAssetsScan:  true,
	PermStockRecord: true,
}

// Scopable reports whether perm may be held in some scopes only.
func Scopable(perm string) bool { return scopable[perm] }

// SplitKey splits a role's permission key into its scope, empty for every
// scope, and its permission.
func SplitKey(key string) (scope, perm string) {
	if s, p, ok := strings.Cut(key, ScopeSeparator); ok {
		return s, p
	}
	return "", key
}

// ValidScopeName reports whether name can name a scope: 1 to 64 lowercase
// letters, digits, - and _, starting with a letter or digit.
func ValidScopeName(name string) bool {
	if name == "" || len(name) > 64 {
		return false
	}
	for i, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
		case (r == '-' || r == '_') && i > 0:
		default:
			return false
		}
	}
	return true
}

// EffectiveScopes are the scopes of an asset in these categories: those of
// the categories and every ancestor of theirs, sorted, or the default scope
// when there are none. parent gives a category's parent, empty at the top,
// and own a category's own scopes.
func EffectiveScopes(categoryIDs []string, parent func(string) string, own func(string) []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, c := range categoryIDs {
		for id, depth := c, 0; id != "" && depth < 100; id, depth = parent(id), depth+1 {
			for _, s := range own(id) {
				if !seen[s] {
					seen[s] = true
					out = append(out, s)
				}
			}
		}
	}
	if len(out) == 0 {
		return []string{DefaultScope}
	}
	sort.Strings(out)
	return out
}

// ValidateAntiLockout enforces the Anti-Lockout Rule:
// System operations must reject any deletion, deactivation, or role removal
// that results in zero active users possessing the `users.manage` permission.
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

// The public role is everyone's, signed in or not (README §3.3), and its
// scope, on no categories until some are put in it, holds what it may see.
const (
	PublicRole  = "public"
	PublicScope = "public"
)

// PublicAllowed reports whether the public role may hold perm: signed-out
// visitors may look, never change anything.
func PublicAllowed(perm string) bool {
	return perm == PermAssetsRead || perm == PermAssetsScan
}
