package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPackagedConfigsMatchTheirServices loads the example configurations
// shipped for each service manager and checks they agree with the service
// files: the configuration where the service looks for it, and the database
// and uploads inside the directory the service gives BAM to write to.
func TestPackagedConfigsMatchTheirServices(t *testing.T) {
	// Environment overrides would hide what the files say.
	for _, name := range []string{"BAM_SERVER_HOST", "BAM_SERVER_PORT", "BAM_SERVER_BASE_URL", "BAM_DATABASE_TYPE", "BAM_DATABASE_DSN", "BAM_DB_PATH", "BAM_STORAGE_LOCAL_PATH", "BAM_STORAGE_PATH", "BAM_PRINTERS_DEVICES"} {
		t.Setenv(name, "")
	}
	for _, c := range []struct {
		dir, service, configPath, dataDir string
	}{
		{"linux", "bam.service", "/etc/bam/config.toml", "/var/lib/bam/"},
		{"freebsd", "bam", "/usr/local/etc/bam/config.toml", "/var/db/bam/"},
	} {
		root := filepath.Join("..", "..", "packaging", c.dir)
		cfg, err := Load(filepath.Join(root, "config.example.toml"))
		if err != nil {
			t.Fatalf("%s: %v", c.dir, err)
		}
		if !strings.HasPrefix(cfg.Database.DSN, c.dataDir) || !strings.HasPrefix(cfg.Storage.LocalPath, c.dataDir) {
			t.Errorf("%s: database %q and uploads %q must be under %s", c.dir, cfg.Database.DSN, cfg.Storage.LocalPath, c.dataDir)
		}
		if cfg.Server.Host != "127.0.0.1" || !strings.HasPrefix(cfg.Server.BaseURL, "https://") {
			t.Errorf("%s: expected loopback behind an HTTPS proxy, got %s and %s", c.dir, cfg.Server.Host, cfg.Server.BaseURL)
		}
		service, err := os.ReadFile(filepath.Join(root, c.service))
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{c.configPath, strings.TrimSuffix(c.dataDir, "/"), "/usr/local/bin/bam serve --config"} {
			if !strings.Contains(string(service), want) {
				t.Errorf("%s/%s does not mention %s", c.dir, c.service, want)
			}
		}
	}
}
