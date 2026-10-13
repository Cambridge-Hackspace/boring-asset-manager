package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/auth"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/google/uuid"
)

var (
	ErrUserNotFound = errors.New("user not found")
)

// GetUserByID retrieves a user by their UUID.
func (s *Store) GetUserByID(id string) (*models.User, error) {
	query := `
		SELECT id, email, password_hash, totp_secret, is_active, created_at, updated_at, archived_at
		FROM users
		WHERE id = ?
	`
	var u models.User
	err := s.QueryRow(query, id).Scan(
		&u.ID, &u.Email, &u.PasswordHash, &u.TOTPSecret, &u.IsActive, &u.CreatedAt, &u.UpdatedAt, &u.ArchivedAt,
	)
	if err == sql.ErrNoRows {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("querying user by id: %w", err)
	}
	return &u, nil
}

// GetUserByEmail retrieves a user by their email address (case-insensitive).
func (s *Store) GetUserByEmail(email string) (*models.User, error) {
	normalized := auth.NormalizeEmail(email)
	query := `
		SELECT id, email, password_hash, totp_secret, is_active, created_at, updated_at, archived_at
		FROM users
		WHERE email = ? AND archived_at IS NULL
	`
	var u models.User
	err := s.QueryRow(query, normalized).Scan(
		&u.ID, &u.Email, &u.PasswordHash, &u.TOTPSecret, &u.IsActive, &u.CreatedAt, &u.UpdatedAt, &u.ArchivedAt,
	)
	if err == sql.ErrNoRows {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("querying user by email: %w", err)
	}
	return &u, nil
}

// ListUsers returns all non-archived users.
func (s *Store) ListUsers() ([]models.User, error) {
	query := `
		SELECT id, email, password_hash, totp_secret, is_active, created_at, updated_at, archived_at
		FROM users
		WHERE archived_at IS NULL
		ORDER BY created_at ASC
	`
	rows, err := s.Query(query)
	if err != nil {
		return nil, fmt.Errorf("querying users: %w", err)
	}
	defer rows.Close()

	var users []models.User
	for rows.Next() {
		var u models.User
		if err := rows.Scan(
			&u.ID, &u.Email, &u.PasswordHash, &u.TOTPSecret, &u.IsActive, &u.CreatedAt, &u.UpdatedAt, &u.ArchivedAt,
		); err != nil {
			return nil, fmt.Errorf("scanning user row: %w", err)
		}
		users = append(users, u)
	}
	return users, nil
}

// GetUserPermissions returns the effective permission set for a user by computing
// the strict union across all assigned roles.
func (s *Store) GetUserPermissions(userID string) (map[string]bool, error) {
	query := `
		SELECT DISTINCT p.key
		FROM user_roles ur
		JOIN role_permissions rp ON ur.role_id = rp.role_id
		JOIN permissions p ON rp.permission_id = p.id
		JOIN users u ON ur.user_id = u.id
		WHERE ur.user_id = ? AND u.is_active = TRUE AND u.archived_at IS NULL
	`
	rows, err := s.Query(query, userID)
	if err != nil {
		return nil, fmt.Errorf("querying user permissions: %w", err)
	}
	defer rows.Close()

	effective := make(map[string]bool)
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, fmt.Errorf("scanning permission key: %w", err)
		}
		effective[key] = true
	}
	return effective, nil
}

// UserHasPermission checks whether an active user has a specific permission.
func (s *Store) UserHasPermission(userID string, permKey string) (bool, error) {
	perms, err := s.GetUserPermissions(userID)
	if err != nil {
		return false, err
	}
	return perms[permKey], nil
}

// CreateUser creates a new user account with a hashed password.
func (s *Store) CreateUser(email, password string, isActive bool) (*models.User, error) {
	normalized := auth.NormalizeEmail(email)
	if normalized == "" {
		return nil, errors.New("email cannot be empty")
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return nil, fmt.Errorf("hashing password: %w", err)
	}

	now := time.Now().UTC()
	userID := uuid.NewString()

	query := `INSERT INTO users (id, email, password_hash, is_active, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`
	_, err = s.Exec(query, userID, normalized, hash, isActive, now, now)
	if err != nil {
		return nil, fmt.Errorf("inserting user: %w", err)
	}

	return &models.User{
		ID:        userID,
		Email:     normalized,
		IsActive:  isActive,
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}

// UpdateUserStatus updates an active user's status with Anti-Lockout enforcement.
func (s *Store) UpdateUserStatus(userID string, isActive bool) error {
	u, err := s.GetUserByID(userID)
	if err != nil {
		return err
	}

	// If deactivating an active user, check anti-lockout invariant
	if u.IsActive && !isActive {
		hasManage, err := s.UserHasPermission(userID, rbac.PermUsersManage)
		if err != nil {
			return fmt.Errorf("checking user permissions: %w", err)
		}
		if hasManage {
			activeAdmins, err := s.CountActiveAdmins()
			if err != nil {
				return err
			}
			if err := rbac.ValidateAntiLockout(activeAdmins, true); err != nil {
				return err
			}
		}
	}

	now := time.Now().UTC()
	_, err = s.Exec("UPDATE users SET is_active = ?, updated_at = ? WHERE id = ?", isActive, now, userID)
	if err != nil {
		return fmt.Errorf("updating user status: %w", err)
	}

	// If deactivating, also invalidate active sessions
	if !isActive {
		_ = s.DeleteUserSessions(userID)
	}

	return nil
}

// DeleteUser archives/deletes a user with Anti-Lockout enforcement.
func (s *Store) DeleteUser(userID string) error {
	u, err := s.GetUserByID(userID)
	if err != nil {
		return err
	}

	if u.IsActive {
		hasManage, err := s.UserHasPermission(userID, rbac.PermUsersManage)
		if err != nil {
			return fmt.Errorf("checking user permissions: %w", err)
		}
		if hasManage {
			activeAdmins, err := s.CountActiveAdmins()
			if err != nil {
				return err
			}
			if err := rbac.ValidateAntiLockout(activeAdmins, true); err != nil {
				return err
			}
		}
	}

	now := time.Now().UTC()
	// Soft delete: set archived_at and is_active = false
	_, err = s.Exec("UPDATE users SET archived_at = ?, is_active = ?, updated_at = ? WHERE id = ?", now, false, now, userID)
	if err != nil {
		return fmt.Errorf("archiving user: %w", err)
	}

	_ = s.DeleteUserSessions(userID)
	return nil
}

// AssignUserRole assigns a role to a user.
func (s *Store) AssignUserRole(userID, roleID string) error {
	var count int
	err := s.QueryRow("SELECT COUNT(*) FROM user_roles WHERE user_id = ? AND role_id = ?", userID, roleID).Scan(&count)
	if err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	_, err = s.Exec("INSERT INTO user_roles (user_id, role_id) VALUES (?, ?)", userID, roleID)
	return err
}

// RemoveUserRole revokes a role from a user with Anti-Lockout enforcement.
func (s *Store) RemoveUserRole(userID, roleID string) error {
	var roleHasManage int
	err := s.QueryRow(`
		SELECT COUNT(*) 
		FROM role_permissions rp
		JOIN permissions p ON rp.permission_id = p.id
		WHERE rp.role_id = ? AND p.key = ?
	`, roleID, rbac.PermUsersManage).Scan(&roleHasManage)
	if err != nil {
		return err
	}

	if roleHasManage > 0 {
		activeAdmins, err := s.CountActiveAdmins()
		if err != nil {
			return err
		}
		if err := rbac.ValidateAntiLockout(activeAdmins, true); err != nil {
			return err
		}
	}

	_, err = s.Exec("DELETE FROM user_roles WHERE user_id = ? AND role_id = ?", userID, roleID)
	return err
}

// GetRoleByName retrieves a role record by name.
func (s *Store) GetRoleByName(name string) (*models.Role, error) {
	var r models.Role
	err := s.QueryRow("SELECT id, name, description FROM roles WHERE name = ?", name).Scan(&r.ID, &r.Name, &r.Description)
	if err != nil {
		return nil, err
	}
	return &r, nil
}
