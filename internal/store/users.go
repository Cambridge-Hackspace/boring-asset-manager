package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/auth"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/textcheck"
	"github.com/google/uuid"
)

var (
	ErrUserNotFound = errors.New("user not found")
	// ErrInvalidUser wraps the reason a user cannot be created as given.
	ErrInvalidUser = errors.New("invalid user")
)

// userColumns are the columns scanUser reads, in order.
const userColumns = "id, email, name, password_hash, totp_secret, is_active, created_at, updated_at, archived_at"

func scanUser(row interface{ Scan(...any) error }) (models.User, error) {
	var u models.User
	err := row.Scan(&u.ID, &u.Email, &u.Name, &u.PasswordHash, &u.TOTPSecret, &u.IsActive, &u.CreatedAt, &u.UpdatedAt, &u.ArchivedAt)
	return u, err
}

// GetUserByID retrieves a user by their UUID.
func (s *Store) GetUserByID(id string) (*models.User, error) {
	query := `
		SELECT ` + userColumns + `
		FROM users
		WHERE id = ?
	`
	u, err := scanUser(s.QueryRow(query, id))
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
		SELECT ` + userColumns + `
		FROM users
		WHERE email = ? AND archived_at IS NULL
	`
	u, err := scanUser(s.QueryRow(query, normalized))
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
		SELECT ` + userColumns + `
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
		u, err := scanUser(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning user row: %w", err)
		}
		users = append(users, u)
	}
	return users, nil
}

// GetUserPermissions returns the effective permission set for a user by computing
// the strict union across all assigned roles: keys held in every scope, and
// those held in some as scope:key.
func (s *Store) GetUserPermissions(userID string) (map[string]bool, error) {
	return grantedKeys(storeQuerier{s}, `SELECT ur.role_id FROM user_roles ur JOIN users u ON u.id = ur.user_id
		WHERE ur.user_id = ? AND u.is_active = TRUE AND u.archived_at IS NULL`, userID)
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
	return s.CreateNamedUser(email, "", password, isActive)
}

// CreateNamedUser creates a user as CreateUser does, with a display name.
func (s *Store) CreateNamedUser(email, name, password string, isActive bool) (*models.User, error) {
	name, err := normalizeUserName(name)
	if err != nil {
		return nil, err
	}
	normalized := auth.NormalizeEmail(email)
	if normalized == "" {
		return nil, fmt.Errorf("%w: email cannot be empty", ErrInvalidUser)
	}
	if err := textcheck.Check("email", normalized, textcheck.MaxName); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidUser, err)
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return nil, fmt.Errorf("hashing password: %w", err)
	}

	now := time.Now().UTC()
	userID := uuid.NewString()

	query := `INSERT INTO users (id, email, name, password_hash, is_active, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)`
	_, err = s.Exec(query, userID, normalized, name, hash, isActive, now, now)
	if err != nil {
		return nil, fmt.Errorf("inserting user: %w", err)
	}

	return &models.User{
		ID:        userID,
		Email:     normalized,
		Name:      name,
		IsActive:  isActive,
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}

// normalizeUserName trims a display name and checks it as stored text.
func normalizeUserName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if err := textcheck.Check("name", name, textcheck.MaxName); err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidUser, err)
	}
	return name, nil
}

// SetUserName changes a user's display name; an empty name shows their
// email instead. Archived users cannot be renamed.
func (s *Store) SetUserName(userID, name string) (*models.User, error) {
	name, err := normalizeUserName(name)
	if err != nil {
		return nil, err
	}
	res, err := s.Exec("UPDATE users SET name = ?, updated_at = ? WHERE id = ? AND archived_at IS NULL", name, time.Now().UTC(), userID)
	if err != nil {
		return nil, fmt.Errorf("renaming user: %w", err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return nil, fmt.Errorf("renaming user: %w", err)
	} else if n == 0 {
		return nil, ErrUserNotFound
	}
	return s.GetUserByID(userID)
}

// userArchived reports whether a user is archived, within tx, or
// ErrUserNotFound.
func (s *Store) userArchived(tx *sql.Tx, userID string) (bool, error) {
	var archivedAt *time.Time
	err := s.TxQueryRow(tx, "SELECT archived_at FROM users WHERE id = ?", userID).Scan(&archivedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return false, ErrUserNotFound
	}
	if err != nil {
		return false, fmt.Errorf("querying user: %w", err)
	}
	return archivedAt != nil, nil
}

// UpdateUserStatus updates an active user's status with Anti-Lockout enforcement.
// Archived (deleted) users cannot be reactivated and are reported as not found.
// Deactivating a user also ends their sessions.
func (s *Store) UpdateUserStatus(userID string, isActive bool) error {
	return s.withAdminGuard(func(tx *sql.Tx) error {
		archived, err := s.userArchived(tx, userID)
		if err != nil {
			return err
		}
		if archived {
			return ErrUserNotFound
		}
		now := time.Now().UTC()
		if _, err := s.TxExec(tx, "UPDATE users SET is_active = ?, updated_at = ? WHERE id = ?", isActive, now, userID); err != nil {
			return fmt.Errorf("updating user status: %w", err)
		}
		if !isActive {
			if _, err := s.TxExec(tx, "DELETE FROM sessions WHERE user_id = ?", userID); err != nil {
				return fmt.Errorf("deleting user sessions: %w", err)
			}
			if err := revokePersonalTokens(s, tx, userID, now); err != nil {
				return err
			}
		}
		return nil
	})
}

// DeleteUser archives a user with Anti-Lockout enforcement and ends their
// sessions.
func (s *Store) DeleteUser(userID string) error {
	return s.withAdminGuard(func(tx *sql.Tx) error {
		if _, err := s.userArchived(tx, userID); err != nil {
			return err
		}
		now := time.Now().UTC()
		// Soft delete: set archived_at and is_active = false
		if _, err := s.TxExec(tx, "UPDATE users SET archived_at = ?, is_active = ?, updated_at = ? WHERE id = ?", now, false, now, userID); err != nil {
			return fmt.Errorf("archiving user: %w", err)
		}
		if _, err := s.TxExec(tx, "DELETE FROM sessions WHERE user_id = ?", userID); err != nil {
			return fmt.Errorf("deleting user sessions: %w", err)
		}
		return revokePersonalTokens(s, tx, userID, now)
	})
}

// revokePersonalTokens revokes a user's personal tokens, as their sessions
// end, so an assistant stops when its person does.
func revokePersonalTokens(s *Store, tx *sql.Tx, userID string, now time.Time) error {
	if _, err := s.TxExec(tx, "UPDATE api_tokens SET revoked_at = ? WHERE user_id = ? AND revoked_at IS NULL", now, userID); err != nil {
		return fmt.Errorf("revoking personal tokens: %w", err)
	}
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

// RemoveUserRole revokes a role from a user with Anti-Lockout enforcement. The
// guard only refuses a revocation that actually leaves no active user with
// users.manage; revoking a role the user does not hold, or one whose
// permission they also get from another role, is always allowed.
func (s *Store) RemoveUserRole(userID, roleID string) error {
	return s.withAdminGuard(func(tx *sql.Tx) error {
		if _, err := s.userArchived(tx, userID); err != nil {
			return err
		}
		_, err := s.TxExec(tx, "DELETE FROM user_roles WHERE user_id = ? AND role_id = ?", userID, roleID)
		return err
	})
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
