package cli

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/api"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/archive"
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
	if err := Backup(configPath, "", BackupOptions{}); err == nil {
		t.Error("backed up without --to")
	}
	if err := Backup(configPath, backup, BackupOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := Restore(configPath, "", RestoreOptions{}); err == nil {
		t.Error("restored without --from")
	}
	if err := Restore(configPath, backup, RestoreOptions{}); err != nil {
		t.Fatal(err)
	}

	cfg.Database = config.DatabaseConfig{Type: "postgres", DSN: "postgres://bam@localhost/bam"}
	if err := cfg.SaveTOML(configPath); err != nil {
		t.Fatal(err)
	}
	if err := Backup(configPath, filepath.Join(dir, "pg.db"), BackupOptions{}); !errors.Is(err, store.ErrBackupUnsupported) {
		t.Errorf("a PostgreSQL backup: %v", err)
	}
}

// TestBackupWritesAnArchive: a --to not ending in .db is an archive,
// readable only by its owner, in parts when split; the flags for archives
// are refused for a .db copy.
func TestBackupWritesAnArchive(t *testing.T) {
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

	dest := filepath.Join(dir, "bam.bam.tar.gz")
	if err := Backup(configPath, dest, BackupOptions{}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(dest)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(dest); len(b) < 2 || b[0] != 0x1f || b[1] != 0x8b {
		t.Error("the backup is not gzipped")
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Errorf("the backup's mode is %v", info.Mode().Perm())
	}

	split := filepath.Join(dir, "split.bam.tar.gz")
	if err := Backup(configPath, split, BackupOptions{Split: 1 << 30, WithoutSecrets: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "split.part1.bam.tar.gz")); err != nil {
		t.Error(err)
	}
	if left, _ := filepath.Glob(filepath.Join(dir, "*.partial")); len(left) != 0 {
		t.Errorf("left behind: %v", left)
	}

	if err := Backup(configPath, filepath.Join(dir, "x.db"), BackupOptions{WithoutSecrets: true}); err == nil {
		t.Error("--without-secrets was taken for a .db copy")
	}
}

// TestRestoreAnArchive: an archive restores into a new database, people
// signing in as before; --check changes nothing; and what is neither an
// archive nor a SQLite copy is refused.
func TestRestoreAnArchive(t *testing.T) {
	dir := t.TempDir()
	configFor := func(name string) string {
		path := filepath.Join(dir, name+".toml")
		cfg := config.DefaultConfig()
		cfg.Database.DSN = filepath.Join(dir, name+".db")
		cfg.Storage.LocalPath = filepath.Join(dir, name+"-uploads")
		if err := cfg.SaveTOML(path); err != nil {
			t.Fatal(err)
		}
		return path
	}
	from, to := configFor("from"), configFor("to")
	if err := Migrate(from); err != nil {
		t.Fatal(err)
	}
	if err := AdminCreate(from, "ada@example.com", "correct horse battery", "Ada"); err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(dir, "b.bam.tar.gz")
	if err := Backup(from, backup, BackupOptions{}); err != nil {
		t.Fatal(err)
	}

	if err := Restore(to, backup, RestoreOptions{Check: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "to.db")); err == nil {
		t.Error("--check made a database")
	}
	if err := Restore(to, backup, RestoreOptions{}); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(to)
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(&cfg.Database)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	u, err := st.GetUserByEmail("ada@example.com")
	if err != nil || !auth.CheckPassword("correct horse battery", u.PasswordHash) {
		t.Errorf("Ada cannot sign in after a restore: %v", err)
	}
	if err := Restore(to, backup, RestoreOptions{}); err == nil {
		t.Error("restored into an instance in use")
	}

	junk := filepath.Join(dir, "junk")
	os.WriteFile(junk, []byte("not a backup"), 0o600)
	if err := Restore(to, junk, RestoreOptions{}); err == nil || !strings.Contains(err.Error(), "neither") {
		t.Errorf("restored junk: %v", err)
	}
	if _, err := ParseKinds("assets,catalog"); err != nil {
		t.Error(err)
	}
	if _, err := ParseKinds("assets,gold"); err == nil {
		t.Error("took a kind there is not")
	}
}

// TestRestoreReplacing: --replace puts an instance's assets back as its
// backup had them, after backing them up, and keeps everything else.
func TestRestoreReplacing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bam.toml")
	cfg := config.DefaultConfig()
	cfg.Database.DSN = filepath.Join(dir, "bam.db")
	cfg.Storage.LocalPath = filepath.Join(dir, "uploads")
	if err := cfg.SaveTOML(path); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(path); err != nil {
		t.Fatal(err)
	}
	if err := AdminCreate(path, "ada@example.com", "correct horse battery", "Ada"); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(&cfg.Database)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if _, err := st.CreateAsset("Drill", nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(dir, "b.bam.tar.gz")
	if err := Backup(path, backup, BackupOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateAsset("Saw", nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateUser("bob@example.com", "bobs long password", true); err != nil {
		t.Fatal(err)
	}

	only := []archive.Kind{archive.KindAssets}
	if err := Restore(path, backup, RestoreOptions{Replace: true, Only: only}); err != nil {
		t.Fatal(err)
	}
	var assets, users int
	st.QueryRow("SELECT COUNT(*) FROM assets").Scan(&assets)
	st.QueryRow("SELECT COUNT(*) FROM users").Scan(&users)
	if assets != 1 || users != 2 {
		t.Errorf("%d assets and %d users after replacing assets", assets, users)
	}
	before, _ := filepath.Glob(filepath.Join(cfg.Storage.StagingDir(), "before-replace-*.bam.tar.gz"))
	if len(before) != 1 {
		t.Fatalf("backups taken first: %v", before)
	}
	// Which put the saw back.
	if err := Restore(path, before[0], RestoreOptions{Replace: true, Only: only, BackupTo: filepath.Join(dir, "again.bam.tar.gz")}); err != nil {
		t.Fatal(err)
	}
	st.QueryRow("SELECT COUNT(*) FROM assets").Scan(&assets)
	if assets != 2 {
		t.Errorf("%d assets after putting back what was replaced", assets)
	}
	if _, err := os.Stat(filepath.Join(dir, "again.bam.tar.gz")); err != nil {
		t.Error(err)
	}
}

func TestPartNamesAndSizes(t *testing.T) {
	for _, c := range []struct {
		dest  string
		split bool
		want  string
	}{
		{"b.bam.tar.gz", false, "b.bam.tar.gz"},
		{"b.bam.tar.gz", true, "b.part2.bam.tar.gz"},
		{"b.tar.gz", true, "b.part2.tar.gz"},
		{"b.TGZ", true, "b.part2.TGZ"},
		{"backup", true, "backup.part2"},
	} {
		if got := PartName(c.dest, 2, c.split); got != c.want {
			t.Errorf("PartName(%q, 2, %v) = %q, want %q", c.dest, c.split, got, c.want)
		}
	}
	for in, want := range map[string]int64{"1048576": 1 << 20, "500MB": 500e6, "4 GiB": 4 << 30, "2tb": 2e12, "10KiB": 10 << 10} {
		if got, err := ParseSize(in); err != nil || got != want {
			t.Errorf("ParseSize(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
	for _, in := range []string{"", "0", "-5MB", "5 parsecs", "MB", "9999999TiB"} {
		if _, err := ParseSize(in); err == nil {
			t.Errorf("ParseSize(%q) took it", in)
		}
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
