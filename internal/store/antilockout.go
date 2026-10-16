package store

import (
	"database/sql"
	"fmt"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
)

// adminLock names the lock row that serializes changes able to remove
// users.manage from an active user.
const adminLock = "users_manage"

// withAdminGuard runs change in a transaction holding the admin lock and
// commits it only if it leaves at least one active user with users.manage
// (when there was one before). Holding the lock means two such changes never
// both see the other's admin still in place: the second waits for the first
// to commit, then counts again.
func (s *Store) withAdminGuard(change func(tx *sql.Tx) error) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()

	if err := s.takeLock(tx, adminLock); err != nil {
		return err
	}

	before, err := s.countActiveAdmins(tx)
	if err != nil {
		return err
	}
	if err := change(tx); err != nil {
		return err
	}
	after, err := s.countActiveAdmins(tx)
	if err != nil {
		return err
	}
	if before > 0 && after == 0 {
		return rbac.ValidateAntiLockout(before, true)
	}
	if s.beforeAdminCommit != nil {
		s.beforeAdminCommit()
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing: %w", err)
	}
	return nil
}

// activeAdminsQuery counts active users holding the permission given.
const activeAdminsQuery = `
	SELECT COUNT(DISTINCT u.id)
	FROM users u
	JOIN user_roles ur ON u.id = ur.user_id
	JOIN role_permissions rp ON ur.role_id = rp.role_id
	JOIN permissions p ON rp.permission_id = p.id
	WHERE u.is_active = TRUE AND u.archived_at IS NULL AND p.permission_key = ?
`

// countActiveAdmins counts active users holding users.manage, within tx.
func (s *Store) countActiveAdmins(tx *sql.Tx) (int, error) {
	var count int
	err := s.TxQueryRow(tx, activeAdminsQuery, rbac.PermUsersManage).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("counting active admins: %w", err)
	}
	return count, nil
}
