package cli

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/api"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/auth"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/config"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/filestore"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

func TestFullCLILifecycle(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.toml")
	dbPath := filepath.Join(tempDir, "bam.db")

	// 1. Test Init
	if err := Init(configPath); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	// Verify refusing to overwrite
	if err := Init(configPath); err == nil {
		t.Fatal("expected Init to fail when config file already exists")
	}

	// Override DB path in config file to use temp directory
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("Load config failed: %v", err)
	}
	cfg.Database.DSN = dbPath + "?_journal=WAL&_busy_timeout=5000"
	cfg.Server.Port = 18080 // Unique test port
	if err := cfg.SaveTOML(configPath); err != nil {
		t.Fatalf("SaveTOML failed: %v", err)
	}

	// 2. Test Migrate
	if err := Migrate(configPath); err != nil {
		t.Fatalf("Migrate failed: %v", err)
	}

	// 3. Test AdminCreate
	if err := AdminCreate(configPath, "superadmin@example.com", "SuperSecret123!", "Ada Admin"); err != nil {
		t.Fatalf("AdminCreate failed: %v", err)
	}

	// 4. Verify Admin was created in DB
	activeCfg, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("Loading active config: %v", err)
	}
	st, err := store.Open(&activeCfg.Database)
	if err != nil {
		t.Fatalf("Opening DB: %v", err)
	}
	defer st.Close()

	count, err := st.CountActiveAdmins()
	if err != nil {
		t.Fatalf("CountActiveAdmins: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 active admin, got %d", count)
	}
	admin, err := st.GetUserByEmail("superadmin@example.com")
	if err != nil || admin.Name != "Ada Admin" {
		t.Errorf("the admin as created: %+v, %v", admin, err)
	}

	// 4a. A password set from the command line, for someone locked out,
	// signs them out everywhere.
	if _, err := st.CreateSession(admin.ID, time.Hour); err != nil {
		t.Fatal(err)
	}
	if err := AdminPassword(configPath, "superadmin@example.com", "BrandNew456!"); err != nil {
		t.Fatalf("AdminPassword: %v", err)
	}
	admin, _ = st.GetUserByEmail("superadmin@example.com")
	if !auth.CheckPassword("BrandNew456!", admin.PasswordHash) {
		t.Error("the new password does not sign in")
	}
	var sessions int
	if err := st.QueryRow("SELECT COUNT(*) FROM sessions WHERE user_id = ?", admin.ID).Scan(&sessions); err != nil || sessions != 0 {
		t.Errorf("sessions left after setting the password: %d, %v", sessions, err)
	}
	if err := AdminPassword(configPath, "nobody@example.com", "BrandNew456!"); err == nil {
		t.Error("setting the password of nobody worked")
	}

	// 5. Test Server Lifecycle
	srv := api.NewServer(activeCfg, st)
	serverErr := make(chan error, 1)

	go func() {
		if err := srv.Start(); err != nil && err != http.ErrServerClosed {
			serverErr <- err
		}
	}()

	// Wait briefly for server to bind
	baseURL := fmt.Sprintf("http://127.0.0.1:%d", activeCfg.Server.Port)
	client := &http.Client{Timeout: 2 * time.Second}

	var healthy bool
	for i := 0; i < 10; i++ {
		time.Sleep(50 * time.Millisecond)
		resp, err := client.Get(baseURL + "/healthz")
		if err == nil && resp.StatusCode == http.StatusOK {
			healthy = true
			resp.Body.Close()
			break
		}
		if resp != nil {
			resp.Body.Close()
		}
	}

	if !healthy {
		t.Errorf("server at %s did not become healthy in time", baseURL)
	}

	// Graceful shutdown
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		t.Errorf("Shutdown error: %v", err)
	}
}

func TestAdminCreateValidation(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.toml")
	_ = Init(configPath)

	if err := AdminCreate(configPath, "", "pwd", ""); err == nil {
		t.Error("expected error for empty email")
	}
	if err := AdminCreate(configPath, "user@test.org", "", ""); err == nil {
		t.Error("expected error for empty password")
	}
}

func TestBackupAndRestoreCommands(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")
	cfg := config.DefaultConfig()
	cfg.Database.DSN = filepath.Join(dir, "bam.db")
	if err := cfg.SaveTOML(configPath); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(configPath); err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(dir, "backup.db")
	if err := Backup(configPath, ""); err == nil {
		t.Error("backed up without --to")
	}
	if err := Backup(configPath, backup); err != nil {
		t.Fatal(err)
	}
	if err := Restore(configPath, ""); err == nil {
		t.Error("restored without --from")
	}
	if err := Restore(configPath, backup); err != nil {
		t.Fatal(err)
	}

	cfg.Database = config.DatabaseConfig{Type: "postgres", DSN: "postgres://bam@localhost/bam"}
	if err := cfg.SaveTOML(configPath); err != nil {
		t.Fatal(err)
	}
	if err := Backup(configPath, filepath.Join(dir, "pg.db")); !errors.Is(err, store.ErrBackupUnsupported) {
		t.Errorf("a PostgreSQL backup: %v", err)
	}
}

// TestStorageCommands: bam storage copy moves every file to the store
// another configuration names, and bam storage sweep reports and removes
// what no row names.
func TestStorageCommands(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")
	cfg := config.DefaultConfig()
	cfg.Database.DSN = filepath.Join(dir, "bam.db")
	cfg.Storage.LocalPath = filepath.Join(dir, "uploads")
	if err := cfg.SaveTOML(configPath); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(configPath); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	from := filestore.NewLocal(cfg.Storage.LocalPath)
	old := time.Now().Add(-2 * filestore.SweepGrace)
	for _, key := range []string{"photos/00000000-0000-4000-8000-000000000001", "images/00000000-0000-4000-8000-000000000002"} {
		if err := from.Put(ctx, key, strings.NewReader("x"), 1, ""); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(filepath.Join(cfg.Storage.LocalPath, filepath.FromSlash(key)), old, old); err != nil {
			t.Fatal(err)
		}
	}

	toPath := filepath.Join(dir, "to.toml")
	toCfg := *cfg
	toCfg.Storage.LocalPath = filepath.Join(dir, "elsewhere")
	if err := toCfg.SaveTOML(toPath); err != nil {
		t.Fatal(err)
	}
	if err := StorageCopy(configPath, ""); err == nil {
		t.Error("copied without --to")
	}
	if err := StorageCopy(configPath, configPath); err == nil || !strings.Contains(err.Error(), "both configurations") {
		t.Errorf("copying onto itself: %v", err)
	}
	if err := StorageCopy(configPath, toPath); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "elsewhere", "images", "00000000-0000-4000-8000-000000000002")); err != nil {
		t.Errorf("not copied: %v", err)
	}

	// No row names either file: a dry run keeps them, a sweep removes them.
	if err := StorageSweep(configPath, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(cfg.Storage.LocalPath, "photos", "00000000-0000-4000-8000-000000000001")); err != nil {
		t.Errorf("a dry run removed a file: %v", err)
	}
	if err := StorageSweep(configPath, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(cfg.Storage.LocalPath, "photos", "00000000-0000-4000-8000-000000000001")); !os.IsNotExist(err) {
		t.Errorf("a sweep kept a file no row names: %v", err)
	}
}
