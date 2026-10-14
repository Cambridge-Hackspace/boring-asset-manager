package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Server.Port != 8080 {
		t.Errorf("expected default port 8080, got %d", cfg.Server.Port)
	}
	if cfg.Database.Type != "sqlite" {
		t.Errorf("expected default database type sqlite, got %s", cfg.Database.Type)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("default config failed validation: %v", err)
	}
}

func TestLoadWithFileAndEnvOverrides(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.toml")

	tomlContent := `
[server]
host = "127.0.0.1"
port = 9090
base_url = "http://bam.test:9090"
session_secret = "test-secret-at-least-something-long"

[database]
type = "sqlite"
dsn = "custom.db"

[storage]
local_path = "./custom_uploads"
`
	if err := os.WriteFile(configPath, []byte(tomlContent), 0o600); err != nil {
		t.Fatalf("writing temp config: %v", err)
	}

	// 1. Verify file load
	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if cfg.Server.Port != 9090 {
		t.Errorf("expected port 9090, got %d", cfg.Server.Port)
	}
	if cfg.Server.Host != "127.0.0.1" {
		t.Errorf("expected host 127.0.0.1, got %s", cfg.Server.Host)
	}

	// 2. Verify env overrides take precedence
	t.Setenv("BAM_SERVER_PORT", "9999")
	t.Setenv("BAM_DB_PATH", "/tmp/reaper_state/test.db")

	cfg2, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load with env overrides failed: %v", err)
	}
	if cfg2.Server.Port != 9999 {
		t.Errorf("expected overridden port 9999, got %d", cfg2.Server.Port)
	}
	if cfg2.Database.DSN != "/tmp/reaper_state/test.db?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)" {
		t.Errorf("expected overridden DSN, got %s", cfg2.Database.DSN)
	}
}

func TestValidationErrors(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Server.Port = -1
	if err := cfg.Validate(); err == nil {
		t.Error("expected validation error for negative port")
	}

	cfg = DefaultConfig()
	cfg.Database.Type = "oracle"
	if err := cfg.Validate(); err == nil {
		t.Error("expected validation error for unsupported db type")
	}

	cfg = DefaultConfig()
	cfg.Server.SessionSecret = ""
	if err := cfg.Validate(); err == nil {
		t.Error("expected validation error for empty session_secret")
	}
}

func TestPrinterDevices(t *testing.T) {
	if cfg := DefaultConfig(); len(cfg.Printers.Devices) != 0 {
		t.Errorf("device connections allowed by default: %v", cfg.Printers.Devices)
	}

	configPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(configPath, []byte("[printers]\ndevices = [\"/dev/usb/lp*\"]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(configPath)
	if err != nil || len(cfg.Printers.Devices) != 1 || cfg.Printers.Devices[0] != "/dev/usb/lp*" {
		t.Fatalf("loaded %v, %v", cfg, err)
	}
	t.Setenv("BAM_PRINTERS_DEVICES", "/dev/usb/lp0,/dev/ulpt0")
	if cfg, err := Load(configPath); err != nil || len(cfg.Printers.Devices) != 2 || cfg.Printers.Devices[1] != "/dev/ulpt0" {
		t.Errorf("environment override gave %v, %v", cfg.Printers.Devices, err)
	}

	for _, bad := range []string{"dev/usb/lp*", "/dev/usb/lp["} {
		cfg := DefaultConfig()
		cfg.Printers.Devices = []string{bad}
		if err := cfg.Validate(); err == nil {
			t.Errorf("pattern %q accepted", bad)
		}
	}
}
