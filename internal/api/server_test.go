package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/config"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

func TestHealthAndInfoEndpoints(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "api_test.db")

	cfg := config.DefaultConfig()
	cfg.Database.DSN = dbPath + "?_journal=WAL&_busy_timeout=5000"

	st, err := store.Open(&cfg.Database)
	if err != nil {
		t.Fatalf("opening store: %v", err)
	}
	defer st.Close()

	if err := st.Migrate(); err != nil {
		t.Fatalf("migrating store: %v", err)
	}

	srv := NewServer(cfg, st)
	handler := srv.Handler()

	// 1. Test /healthz
	reqHealth := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rrHealth := httptest.NewRecorder()
	handler.ServeHTTP(rrHealth, reqHealth)

	if rrHealth.Code != http.StatusOK {
		t.Errorf("expected 200 OK for /healthz, got %d", rrHealth.Code)
	}

	var healthResp map[string]any
	if err := json.Unmarshal(rrHealth.Body.Bytes(), &healthResp); err != nil {
		t.Fatalf("decoding healthz response: %v", err)
	}
	if healthResp["status"] != "healthy" {
		t.Errorf("expected status 'healthy', got %v", healthResp["status"])
	}

	// 2. Test /readyz
	reqReady := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rrReady := httptest.NewRecorder()
	handler.ServeHTTP(rrReady, reqReady)

	if rrReady.Code != http.StatusOK {
		t.Errorf("expected 200 OK for /readyz, got %d", rrReady.Code)
	}

	// 3. Test /api/v1/info
	reqInfo := httptest.NewRequest(http.MethodGet, "/api/v1/info", nil)
	rrInfo := httptest.NewRecorder()
	handler.ServeHTTP(rrInfo, reqInfo)

	if rrInfo.Code != http.StatusOK {
		t.Errorf("expected 200 OK for /api/v1/info, got %d", rrInfo.Code)
	}

	var infoResp map[string]any
	if err := json.Unmarshal(rrInfo.Body.Bytes(), &infoResp); err != nil {
		t.Fatalf("decoding info response: %v", err)
	}
	if infoResp["version"] != Version {
		t.Errorf("expected version %s, got %v", Version, infoResp["version"])
	}
	// The commit is what the binary records, empty in a test binary, which
	// go test builds without it.
	commit, modified := buildCommit()
	if c, ok := infoResp["commit"].(string); !ok || c != commit {
		t.Errorf("commit %v, want %q", infoResp["commit"], commit)
	}
	if m, ok := infoResp["modified"].(bool); !ok || m != modified {
		t.Errorf("modified %v, want %v", infoResp["modified"], modified)
	}
}
