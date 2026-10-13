package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/auth"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/config"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store/migrations"
	"github.com/google/uuid"

	_ "modernc.org/sqlite"
)

// Store wraps the database connection pool.
type Store struct {
	DB      *sql.DB
	Dialect string
}

// Open initializes and connects to the database based on configuration.
func Open(cfg *config.DatabaseConfig) (*Store, error) {
	var (
		db  *sql.DB
		err error
	)

	switch cfg.Type {
	case "sqlite":
		dsn := cfg.DSN
		// Ensure parent directory exists for file-based SQLite
		if !strings.HasPrefix(dsn, ":memory:") && !strings.Contains(dsn, "mode=memory") {
			filePath := strings.Split(dsn, "?")[0]
			if dir := filepath.Dir(filePath); dir != "" && dir != "." {
				if err := os.MkdirAll(dir, 0o755); err != nil {
					return nil, fmt.Errorf("creating database directory %s: %w", dir, err)
				}
			}
		}
		db, err = sql.Open("sqlite", dsn)
		if err != nil {
			return nil, fmt.Errorf("opening sqlite database: %w", err)
		}

		// Configure pool for SQLite WAL mode
		db.SetMaxOpenConns(1) // Single writer for SQLite to avoid lock contention
		db.SetMaxIdleConns(1)
		db.SetConnMaxLifetime(0)

	default:
		return nil, fmt.Errorf("unsupported database dialect: %s", cfg.Type)
	}

	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("pinging database: %w", err)
	}

	return &Store{
		DB:      db,
		Dialect: cfg.Type,
	}, nil
}

// Close closes the database connection pool.
func (s *Store) Close() error {
	if s.DB != nil {
		return s.DB.Close()
	}
	return nil
}

// Ping checks database liveness.
func (s *Store) Ping() error {
	if s.DB == nil {
		return fmt.Errorf("database connection is nil")
	}
	return s.DB.Ping()
}

// Migrate executes schema migrations.
func (s *Store) Migrate() error {
	return migrations.Migrate(s.DB)
}

// SeedDefaultPermissionsAndRoles creates the standard permission set and default Administrator role.
func (s *Store) SeedDefaultPermissionsAndRoles() error {
	tx, err := s.DB.Begin()
	if err != nil {
		return fmt.Errorf("beginning seed transaction: %w", err)
	}
	defer tx.Rollback()

	// 1. Seed Permissions
	allPerms := rbac.AllPermissions()
	for _, permKey := range allPerms {
		var exists int
		err := tx.QueryRow("SELECT COUNT(*) FROM permissions WHERE key = ?", permKey).Scan(&exists)
		if err != nil {
			return fmt.Errorf("checking permission %s: %w", permKey, err)
		}
		if exists == 0 {
			permID := uuid.NewString()
			_, err = tx.Exec("INSERT INTO permissions (id, key, description) VALUES (?, ?, ?)", permID, permKey, "Core permission: "+permKey)
			if err != nil {
				return fmt.Errorf("inserting permission %s: %w", permKey, err)
			}
		}
	}

	// 2. Seed Admin Role
	var adminRoleID string
	err = tx.QueryRow("SELECT id FROM roles WHERE name = 'admin'").Scan(&adminRoleID)
	if err == sql.ErrNoRows {
		adminRoleID = uuid.NewString()
		_, err = tx.Exec("INSERT INTO roles (id, name, description) VALUES (?, 'admin', 'System Administrator with full access')", adminRoleID)
		if err != nil {
			return fmt.Errorf("inserting admin role: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("querying admin role: %w", err)
	}

	// 3. Grant all permissions to admin role
	rows, err := tx.Query("SELECT id FROM permissions")
	if err != nil {
		return fmt.Errorf("querying all permissions: %w", err)
	}
	defer rows.Close()

	var permIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return err
		}
		permIDs = append(permIDs, id)
	}

	for _, pid := range permIDs {
		var hasRolePerm int
		err := tx.QueryRow("SELECT COUNT(*) FROM role_permissions WHERE role_id = ? AND permission_id = ?", adminRoleID, pid).Scan(&hasRolePerm)
		if err != nil {
			return err
		}
		if hasRolePerm == 0 {
			_, err = tx.Exec("INSERT INTO role_permissions (role_id, permission_id) VALUES (?, ?)", adminRoleID, pid)
			if err != nil {
				return fmt.Errorf("assigning permission %s to admin role: %w", pid, err)
			}
		}
	}

	return tx.Commit()
}

// CreateAdminUser creates or updates a superuser account with the admin role.
func (s *Store) CreateAdminUser(email, password string) (*models.User, error) {
	normalized := auth.NormalizeEmail(email)
	if normalized == "" {
		return nil, fmt.Errorf("email cannot be empty")
	}
	if password == "" {
		return nil, fmt.Errorf("password cannot be empty")
	}

	hash, err := auth.HashPassword(password)
	if err != nil {
		return nil, fmt.Errorf("hashing password: %w", err)
	}

	now := time.Now().UTC()
	userID := uuid.NewString()

	tx, err := s.DB.Begin()
	if err != nil {
		return nil, fmt.Errorf("beginning user transaction: %w", err)
	}
	defer tx.Rollback()

	var existingID string
	err = tx.QueryRow("SELECT id FROM users WHERE email = ?", normalized).Scan(&existingID)
	if err == nil {
		// Update existing user password and activate
		_, err = tx.Exec("UPDATE users SET password_hash = ?, is_active = 1, updated_at = ? WHERE id = ?", hash, now, existingID)
		if err != nil {
			return nil, fmt.Errorf("updating existing admin user: %w", err)
		}
		userID = existingID
	} else if err == sql.ErrNoRows {
		// Create new user
		_, err = tx.Exec("INSERT INTO users (id, email, password_hash, is_active, created_at, updated_at) VALUES (?, ?, ?, 1, ?, ?)",
			userID, normalized, hash, now, now)
		if err != nil {
			return nil, fmt.Errorf("inserting admin user: %w", err)
		}
	} else {
		return nil, fmt.Errorf("checking existing user: %w", err)
	}

	// Ensure admin role exists and is assigned
	var adminRoleID string
	err = tx.QueryRow("SELECT id FROM roles WHERE name = 'admin'").Scan(&adminRoleID)
	if err != nil {
		return nil, fmt.Errorf("finding admin role (run migrations and seed first): %w", err)
	}

	var hasUserRole int
	err = tx.QueryRow("SELECT COUNT(*) FROM user_roles WHERE user_id = ? AND role_id = ?", userID, adminRoleID).Scan(&hasUserRole)
	if err != nil {
		return nil, err
	}
	if hasUserRole == 0 {
		_, err = tx.Exec("INSERT INTO user_roles (user_id, role_id) VALUES (?, ?)", userID, adminRoleID)
		if err != nil {
			return nil, fmt.Errorf("assigning admin role to user: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("committing admin user: %w", err)
	}

	return &models.User{
		ID:        userID,
		Email:     normalized,
		IsActive:  true,
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}

// CountActiveAdmins returns the number of active users possessing users:manage permission.
func (s *Store) CountActiveAdmins() (int, error) {
	query := `
		SELECT COUNT(DISTINCT u.id)
		FROM users u
		JOIN user_roles ur ON u.id = ur.user_id
		JOIN role_permissions rp ON ur.role_id = rp.role_id
		JOIN permissions p ON rp.permission_id = p.id
		WHERE u.is_active = 1 AND p.key = ?
	`
	var count int
	err := s.DB.QueryRow(query, rbac.PermUsersManage).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("counting active admins: %w", err)
	}
	return count, nil
}
