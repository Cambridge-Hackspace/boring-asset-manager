package store

import (
	"path/filepath"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/config"
)

func TestStoreMigrationIdempotenceAndAdminCreation(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test_bam.db")

	cfg := &config.DatabaseConfig{
		Type: "sqlite",
		DSN:  dbPath + "?_journal=WAL&_busy_timeout=5000",
	}

	st, err := Open(cfg)
	if err != nil {
		t.Fatalf("Open store failed: %v", err)
	}
	defer st.Close()

	// 1. Initial Migration
	if err := st.Migrate(); err != nil {
		t.Fatalf("Initial migration failed: %v", err)
	}

	// 2. Idempotence Check: running migration second time must not fail
	if err := st.Migrate(); err != nil {
		t.Fatalf("Second migration pass failed: %v", err)
	}

	// 3. Seed Default Permissions and Roles
	if err := st.SeedDefaultPermissionsAndRoles(); err != nil {
		t.Fatalf("SeedDefaultPermissionsAndRoles failed: %v", err)
	}
	// Run seed second time to verify seed idempotence
	if err := st.SeedDefaultPermissionsAndRoles(); err != nil {
		t.Fatalf("Second seed pass failed: %v", err)
	}

	// 4. Create Admin User
	admin, err := st.CreateAdminUser("admin@hackspace.org", "SecurePassword123!")
	if err != nil {
		t.Fatalf("CreateAdminUser failed: %v", err)
	}
	if admin.Email != "admin@hackspace.org" {
		t.Errorf("expected email admin@hackspace.org, got %s", admin.Email)
	}

	// 5. Anti-lockout count: exactly 1 active admin
	count, err := st.CountActiveAdmins()
	if err != nil {
		t.Fatalf("CountActiveAdmins failed: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 active admin, got %d", count)
	}

	// 6. Update password for existing admin
	admin2, err := st.CreateAdminUser("admin@hackspace.org", "UpdatedPassword456!")
	if err != nil {
		t.Fatalf("Updating admin password failed: %v", err)
	}
	if admin2.ID != admin.ID {
		t.Errorf("expected updated admin to preserve ID %s, got %s", admin.ID, admin2.ID)
	}

	countAfterUpdate, err := st.CountActiveAdmins()
	if err != nil {
		t.Fatalf("CountActiveAdmins failed: %v", err)
	}
	if countAfterUpdate != 1 {
		t.Errorf("expected still 1 active admin, got %d", countAfterUpdate)
	}
}
