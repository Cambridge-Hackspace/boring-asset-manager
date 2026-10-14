package store

import (
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/auth"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
)

var (
	ErrRoleNotFound  = errors.New("role not found")
	ErrWrongPassword = errors.New("current password is incorrect")
)

// ListRoles returns every role with its permission keys, ordered by name.
func (s *Store) ListRoles() ([]models.RoleWithPermissions, error) {
	rows, err := s.Query(`
		SELECT r.id, r.name, r.description, p.permission_key
		FROM roles r
		LEFT JOIN role_permissions rp ON rp.role_id = r.id
		LEFT JOIN permissions p ON p.id = rp.permission_id
		ORDER BY r.name, p.permission_key
	`)
	if err != nil {
		return nil, fmt.Errorf("querying roles: %w", err)
	}
	defer rows.Close()

	var roles []models.RoleWithPermissions
	for rows.Next() {
		var (
			r   models.Role
			key sql.NullString
		)
		if err := rows.Scan(&r.ID, &r.Name, &r.Description, &key); err != nil {
			return nil, fmt.Errorf("scanning role: %w", err)
		}
		if n := len(roles); n == 0 || roles[n-1].ID != r.ID {
			roles = append(roles, models.RoleWithPermissions{Role: r, Permissions: []string{}})
		}
		if key.Valid {
			last := &roles[len(roles)-1]
			last.Permissions = append(last.Permissions, key.String)
		}
	}
	return roles, rows.Err()
}

// UserRoleIDs maps each user with roles to their role IDs, sorted.
func (s *Store) UserRoleIDs() (map[string][]string, error) {
	rows, err := s.Query("SELECT user_id, role_id FROM user_roles")
	if err != nil {
		return nil, fmt.Errorf("querying user roles: %w", err)
	}
	defer rows.Close()
	result := map[string][]string{}
	for rows.Next() {
		var userID, roleID string
		if err := rows.Scan(&userID, &roleID); err != nil {
			return nil, fmt.Errorf("scanning user role: %w", err)
		}
		result[userID] = append(result[userID], roleID)
	}
	for _, ids := range result {
		sort.Strings(ids)
	}
	return result, rows.Err()
}

// SetUserRoles replaces a user's roles with roleIDs, under the anti-lockout
// guard. Unknown roles are refused with ErrRoleNotFound.
func (s *Store) SetUserRoles(userID string, roleIDs []string) error {
	return s.withAdminGuard(func(tx *sql.Tx) error {
		archived, err := s.userArchived(tx, userID)
		if err != nil {
			return err
		}
		if archived {
			return ErrUserNotFound
		}
		seen := map[string]bool{}
		for _, id := range roleIDs {
			if seen[id] {
				continue
			}
			seen[id] = true
			var n int
			if err := s.TxQueryRow(tx, "SELECT COUNT(*) FROM roles WHERE id = ?", id).Scan(&n); err != nil {
				return fmt.Errorf("checking role: %w", err)
			}
			if n == 0 {
				return fmt.Errorf("%w: %s", ErrRoleNotFound, id)
			}
		}
		if _, err := s.TxExec(tx, "DELETE FROM user_roles WHERE user_id = ?", userID); err != nil {
			return fmt.Errorf("clearing roles: %w", err)
		}
		for id := range seen {
			if _, err := s.TxExec(tx, "INSERT INTO user_roles (user_id, role_id) VALUES (?, ?)", userID, id); err != nil {
				return fmt.Errorf("assigning role: %w", err)
			}
		}
		return nil
	})
}

// ChangePassword sets a user's password after checking their current one,
// and ends every other session of theirs; keepToken is the session to keep.
func (s *Store) ChangePassword(userID, current, next, keepToken string) error {
	u, err := s.GetUserByID(userID)
	if err != nil {
		return err
	}
	if !auth.CheckPassword(current, u.PasswordHash) {
		return ErrWrongPassword
	}
	hash, err := auth.HashPassword(next)
	if err != nil {
		return err
	}

	tx, err := s.DB.Begin()
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()
	if _, err := s.TxExec(tx, "UPDATE users SET password_hash = ?, updated_at = ? WHERE id = ?", hash, time.Now().UTC(), userID); err != nil {
		return fmt.Errorf("updating password: %w", err)
	}
	if _, err := s.TxExec(tx, "DELETE FROM sessions WHERE user_id = ? AND id <> ?", userID, sessionKey(keepToken)); err != nil {
		return fmt.Errorf("ending other sessions: %w", err)
	}
	return tx.Commit()
}
