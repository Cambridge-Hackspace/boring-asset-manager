package cli

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/api"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/config"
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
	if err := AdminCreate(configPath, "superadmin@example.com", "SuperSecret123!"); err != nil {
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

	if err := AdminCreate(configPath, "", "pwd"); err == nil {
		t.Error("expected error for empty email")
	}
	if err := AdminCreate(configPath, "user@test.org", ""); err == nil {
		t.Error("expected error for empty password")
	}
}
