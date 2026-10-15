package store

import (
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/auth"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/textcheck"
	"github.com/google/uuid"
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
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()

	counts := map[string]int{}
	countRows, err := s.Query(`SELECT ur.role_id, COUNT(*) FROM user_roles ur JOIN users u ON u.id = ur.user_id
		WHERE u.archived_at IS NULL GROUP BY ur.role_id`)
	if err != nil {
		return nil, fmt.Errorf("counting role holders: %w", err)
	}
	defer countRows.Close()
	for countRows.Next() {
		var id string
		var n int
		if err := countRows.Scan(&id, &n); err != nil {
			return nil, fmt.Errorf("scanning role holders: %w", err)
		}
		counts[id] = n
	}
	for i := range roles {
		roles[i].UserCount = counts[roles[i].ID]
		roles[i].Protected = roles[i].Name == AdminRoleName
	}
	return roles, countRows.Err()
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

// AdminRoleName is the seeded role holding every permission. Seeding
// recreates it and grants it every permission at each start, so it cannot be
// renamed, edited or deleted.
const AdminRoleName = "admin"

var (
	// ErrRoleProtected is returned when changing or deleting the admin role.
	ErrRoleProtected = errors.New("the admin role always has every permission and cannot be changed or deleted")
	// ErrInvalidRole wraps the reason a role cannot be saved as given.
	ErrInvalidRole = errors.New("invalid role")
)

// MaxRoleName is the longest role name, in characters.
const MaxRoleName = 64

// RoleInput is a role as created or edited.
type RoleInput struct {
	Name        string
	Description string
	Permissions []string
}

// normalize trims the role and checks its name, description and
// permissions, removing repeated permissions.
func (in *RoleInput) normalize() error {
	in.Name = strings.TrimSpace(in.Name)
	in.Description = strings.TrimSpace(in.Description)
	if in.Name == "" {
		return fmt.Errorf("%w: a name is required", ErrInvalidRole)
	}
	if err := textcheck.Check("role name", in.Name, MaxRoleName); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidRole, err)
	}
	if err := textcheck.Check("description", in.Description, textcheck.MaxName); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidRole, err)
	}
	known := map[string]bool{}
	for _, p := range rbac.AllPermissions() {
		known[p] = true
	}
	seen := map[string]bool{}
	perms := make([]string, 0, len(in.Permissions))
	for _, p := range in.Permissions {
		if !known[p] {
			return fmt.Errorf("%w: unknown permission %q", ErrInvalidRole, p)
		}
		if !seen[p] {
			seen[p] = true
			perms = append(perms, p)
		}
	}
	sort.Strings(perms)
	in.Permissions = perms
	return nil
}

// GetRole returns a role with its permissions, or ErrRoleNotFound.
func (s *Store) GetRole(id string) (*models.RoleWithPermissions, error) {
	roles, err := s.ListRoles()
	if err != nil {
		return nil, err
	}
	for i := range roles {
		if roles[i].ID == id {
			return &roles[i], nil
		}
	}
	return nil, ErrRoleNotFound
}

// CreateRole creates a role granting the permissions given. A name already
// in use fails as a unique violation.
func (s *Store) CreateRole(in RoleInput) (*models.RoleWithPermissions, error) {
	if err := in.normalize(); err != nil {
		return nil, err
	}
	id := uuid.NewString()
	tx, err := s.DB.Begin()
	if err != nil {
		return nil, fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()
	if _, err := s.TxExec(tx, "INSERT INTO roles (id, name, description) VALUES (?, ?, ?)", id, in.Name, in.Description); err != nil {
		return nil, fmt.Errorf("inserting role: %w", err)
	}
	if err := s.setRolePermissionsTx(tx, id, in.Permissions); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("committing: %w", err)
	}
	return s.GetRole(id)
}

// UpdateRole renames a role and replaces its permissions, under the
// anti-lockout guard: taking users:manage from the role of the last active
// user who has it is refused.
func (s *Store) UpdateRole(id string, in RoleInput) (*models.RoleWithPermissions, error) {
	if err := in.normalize(); err != nil {
		return nil, err
	}
	err := s.withAdminGuard(func(tx *sql.Tx) error {
		if err := s.editableRoleTx(tx, id); err != nil {
			return err
		}
		if _, err := s.TxExec(tx, "UPDATE roles SET name = ?, description = ? WHERE id = ?", in.Name, in.Description, id); err != nil {
			return fmt.Errorf("updating role: %w", err)
		}
		return s.setRolePermissionsTx(tx, id, in.Permissions)
	})
	if err != nil {
		return nil, err
	}
	return s.GetRole(id)
}

// DeleteRole deletes a role, taking it from everyone who holds it, under the
// anti-lockout guard.
func (s *Store) DeleteRole(id string) error {
	return s.withAdminGuard(func(tx *sql.Tx) error {
		if err := s.editableRoleTx(tx, id); err != nil {
			return err
		}
		// Its grants and assignments go with it (ON DELETE CASCADE).
		if _, err := s.TxExec(tx, "DELETE FROM roles WHERE id = ?", id); err != nil {
			return fmt.Errorf("deleting role: %w", err)
		}
		return nil
	})
}

// editableRoleTx checks that a role exists and is not the admin role.
func (s *Store) editableRoleTx(tx *sql.Tx, id string) error {
	var name string
	err := s.TxQueryRow(tx, "SELECT name FROM roles WHERE id = ?", id).Scan(&name)
	if err == sql.ErrNoRows {
		return ErrRoleNotFound
	}
	if err != nil {
		return fmt.Errorf("reading role: %w", err)
	}
	if name == AdminRoleName {
		return ErrRoleProtected
	}
	return nil
}

// setRolePermissionsTx replaces a role's permissions with keys, which must
// be known.
func (s *Store) setRolePermissionsTx(tx *sql.Tx, roleID string, keys []string) error {
	if _, err := s.TxExec(tx, "DELETE FROM role_permissions WHERE role_id = ?", roleID); err != nil {
		return fmt.Errorf("clearing permissions: %w", err)
	}
	for _, key := range keys {
		var permID string
		if err := s.TxQueryRow(tx, "SELECT id FROM permissions WHERE permission_key = ?", key).Scan(&permID); err != nil {
			return fmt.Errorf("finding permission %s: %w", key, err)
		}
		if _, err := s.TxExec(tx, "INSERT INTO role_permissions (role_id, permission_id) VALUES (?, ?)", roleID, permID); err != nil {
			return fmt.Errorf("granting %s: %w", key, err)
		}
	}
	return nil
}
