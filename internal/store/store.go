package store

import (
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/auth"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/config"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store/migrations"
	"github.com/go-sql-driver/mysql"
	"github.com/google/uuid"

	_ "github.com/lib/pq"
	_ "modernc.org/sqlite"
)

// Store wraps the database connection pool.
type Store struct {
	DB      *sql.DB
	Dialect string

	// beforeAdminCommit, when set by tests, runs inside withAdminGuard just
	// before a guarded change commits.
	beforeAdminCommit func()
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
		db, err = sql.Open("sqlite", sqliteDSN(dsn))
		if err != nil {
			return nil, fmt.Errorf("opening sqlite database: %w", err)
		}

		// Configure pool for SQLite WAL mode
		db.SetMaxOpenConns(1) // Single writer for SQLite to avoid lock contention
		db.SetMaxIdleConns(1)
		db.SetConnMaxLifetime(0)

	case "postgres":
		db, err = sql.Open("postgres", cfg.DSN)
		if err != nil {
			return nil, fmt.Errorf("opening postgres database: %w", err)
		}
		db.SetMaxOpenConns(25)
		db.SetMaxIdleConns(5)
		db.SetConnMaxLifetime(5 * time.Minute)

	case "mariadb", "mysql":
		dsn, dsnErr := mariaDBDSN(cfg.DSN)
		if dsnErr != nil {
			return nil, fmt.Errorf("parsing mariadb dsn: %w", dsnErr)
		}
		db, err = sql.Open("mysql", dsn)
		if err != nil {
			return nil, fmt.Errorf("opening mariadb/mysql database: %w", err)
		}
		db.SetMaxOpenConns(25)
		db.SetMaxIdleConns(5)
		db.SetConnMaxLifetime(5 * time.Minute)

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

// sqliteDSN rewrites a SQLite DSN into the form understood by modernc.org/sqlite,
// which only honors `_pragma`, `_time_format` and `_txlock` query parameters.
// Legacy mattn-style `_journal`/`_journal_mode` and `_busy_timeout` parameters are
// translated to their pragma equivalents, WAL mode and a 5s busy timeout are applied
// when not otherwise specified, and foreign key enforcement is always enabled so that
// the schema's ON DELETE CASCADE / SET NULL clauses take effect.
func sqliteDSN(dsn string) string {
	path, rawQuery, _ := strings.Cut(dsn, "?")
	q, err := url.ParseQuery(rawQuery)
	if err != nil {
		q = url.Values{}
	}

	pragmas := q["_pragma"]
	hasPragma := func(name string) bool {
		for _, p := range pragmas {
			if strings.HasPrefix(strings.ToLower(strings.TrimSpace(p)), name) {
				return true
			}
		}
		return false
	}

	for _, legacy := range []string{"_journal", "_journal_mode"} {
		if v := q.Get(legacy); v != "" && !hasPragma("journal_mode") {
			pragmas = append(pragmas, "journal_mode("+v+")")
		}
		q.Del(legacy)
	}
	if v := q.Get("_busy_timeout"); v != "" && !hasPragma("busy_timeout") {
		pragmas = append(pragmas, "busy_timeout("+v+")")
	}
	q.Del("_busy_timeout")
	q.Del("_foreign_keys")
	q.Del("_fk")

	if !hasPragma("journal_mode") {
		pragmas = append(pragmas, "journal_mode(WAL)")
	}
	if !hasPragma("busy_timeout") {
		pragmas = append(pragmas, "busy_timeout(5000)")
	}
	filtered := pragmas[:0]
	for _, p := range pragmas {
		if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(p)), "foreign_keys") {
			filtered = append(filtered, p)
		}
	}
	q["_pragma"] = append(filtered, "foreign_keys(1)")

	return path + "?" + q.Encode()
}

// mariaDBDSN forces the driver options the store depends on: parseTime so DATETIME
// columns scan into time.Time, and UTC as the connection location so timestamps
// round-trip unchanged.
func mariaDBDSN(dsn string) (string, error) {
	mcfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		return "", err
	}
	mcfg.ParseTime = true
	mcfg.Loc = time.UTC
	return mcfg.FormatDSN(), nil
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
	return migrations.Migrate(s.DB, s.Dialect)
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
		err := s.TxQueryRow(tx, "SELECT COUNT(*) FROM permissions WHERE permission_key = ?", permKey).Scan(&exists)
		if err != nil {
			return fmt.Errorf("checking permission %s: %w", permKey, err)
		}
		if exists == 0 {
			permID := uuid.NewString()
			_, err = s.TxExec(tx, "INSERT INTO permissions (id, permission_key, description) VALUES (?, ?, ?)", permID, permKey, "Core permission: "+permKey)
			if err != nil {
				return fmt.Errorf("inserting permission %s: %w", permKey, err)
			}
		}
	}

	// 2. Seed Admin Role
	var adminRoleID string
	err = s.TxQueryRow(tx, "SELECT id FROM roles WHERE name = 'admin'").Scan(&adminRoleID)
	if err == sql.ErrNoRows {
		adminRoleID = uuid.NewString()
		_, err = s.TxExec(tx, "INSERT INTO roles (id, name, description) VALUES (?, 'admin', 'System Administrator with full access')", adminRoleID)
		if err != nil {
			return fmt.Errorf("inserting admin role: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("querying admin role: %w", err)
	}

	// 3. Grant all permissions to admin role
	rows, err := s.TxQuery(tx, "SELECT id FROM permissions")
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
		err := s.TxQueryRow(tx, "SELECT COUNT(*) FROM role_permissions WHERE role_id = ? AND permission_id = ?", adminRoleID, pid).Scan(&hasRolePerm)
		if err != nil {
			return err
		}
		if hasRolePerm == 0 {
			_, err = s.TxExec(tx, "INSERT INTO role_permissions (role_id, permission_id) VALUES (?, ?)", adminRoleID, pid)
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
	err = s.TxQueryRow(tx, "SELECT id FROM users WHERE email = ?", normalized).Scan(&existingID)
	if err == nil {
		// Update existing user password and activate
		_, err = s.TxExec(tx, "UPDATE users SET password_hash = ?, is_active = ?, updated_at = ? WHERE id = ?", hash, true, now, existingID)
		if err != nil {
			return nil, fmt.Errorf("updating existing admin user: %w", err)
		}
		userID = existingID
	} else if err == sql.ErrNoRows {
		// Create new user
		_, err = s.TxExec(tx, "INSERT INTO users (id, email, password_hash, is_active, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)",
			userID, normalized, hash, true, now, now)
		if err != nil {
			return nil, fmt.Errorf("inserting admin user: %w", err)
		}
	} else {
		return nil, fmt.Errorf("checking existing user: %w", err)
	}

	// Ensure admin role exists and is assigned
	var adminRoleID string
	err = s.TxQueryRow(tx, "SELECT id FROM roles WHERE name = 'admin'").Scan(&adminRoleID)
	if err != nil {
		return nil, fmt.Errorf("finding admin role (run migrations and seed first): %w", err)
	}

	var hasUserRole int
	err = s.TxQueryRow(tx, "SELECT COUNT(*) FROM user_roles WHERE user_id = ? AND role_id = ?", userID, adminRoleID).Scan(&hasUserRole)
	if err != nil {
		return nil, err
	}
	if hasUserRole == 0 {
		_, err = s.TxExec(tx, "INSERT INTO user_roles (user_id, role_id) VALUES (?, ?)", userID, adminRoleID)
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
	var count int
	err := s.QueryRow(activeAdminsQuery, rbac.PermUsersManage).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("counting active admins: %w", err)
	}
	return count, nil
}
