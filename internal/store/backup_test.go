package store

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/config"
)

// TestBackupAndRestore backs up a database in use, changes it, and restores
// the backup over it, as the updater does around a failed update.
func TestBackupAndRestore(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.DatabaseConfig{Type: "sqlite", DSN: filepath.Join(dir, "bam.db") + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)"}
	path := filepath.Join(dir, "bam.db")
	st, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	kept, err := st.CreateAsset("Before the update", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	// While BAM still has it open, with changes in its write-ahead log.
	backup := filepath.Join(dir, "backup.db")
	if err := BackupSQLite(cfg, backup); err != nil {
		t.Fatal(err)
	}
	if err := BackupSQLite(cfg, backup); err == nil {
		t.Error("overwrote an existing backup")
	}
	if runtime.GOOS != "windows" {
		if info, _ := os.Stat(backup); info.Mode().Perm() != 0o600 {
			t.Errorf("backup mode %v, want 0600", info.Mode().Perm())
		}
	}

	// The new version changes the database, and then fails.
	lost, err := st.CreateAsset("After the update", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path + "-wal"); err != nil {
		t.Fatalf("expected a write-ahead log while open: %v", err)
	}
	// Close without checkpointing, as a crash would: copy the files aside
	// first, since closing cleanly folds the log into the database.
	walCopy, _ := os.ReadFile(path + "-wal")
	st.Close()
	if err := os.WriteFile(path+"-wal", walCopy, 0o600); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(path, 0o640); err != nil {
			t.Fatal(err)
		}
	}

	if err := RestoreSQLite(cfg, backup); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path + "-wal"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the old write-ahead log is still there: %v", err)
	}
	if runtime.GOOS != "windows" {
		if info, _ := os.Stat(path); info.Mode().Perm() != 0o640 {
			t.Errorf("restored mode %v, want the database's 0640", info.Mode().Perm())
		}
	}
	st, err = Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if _, err := st.GetAssetByID(kept.Asset.ID); err != nil {
		t.Errorf("the asset from before the backup: %v", err)
	}
	if _, err := st.GetAssetByID(lost.Asset.ID); !errors.Is(err, ErrAssetNotFound) {
		t.Errorf("the asset from after the backup survived the restore: %v", err)
	}
}

func TestBackupRefusals(t *testing.T) {
	dir := t.TempDir()
	for _, cfg := range []*config.DatabaseConfig{
		{Type: "postgres", DSN: "postgres://bam@localhost/bam"},
		{Type: "mariadb", DSN: "bam@tcp(localhost:3306)/bam"},
	} {
		if err := BackupSQLite(cfg, filepath.Join(dir, "x.db")); !errors.Is(err, ErrBackupUnsupported) {
			t.Errorf("%s backup: %v", cfg.Type, err)
		}
		if err := RestoreSQLite(cfg, filepath.Join(dir, "x.db")); !errors.Is(err, ErrBackupUnsupported) {
			t.Errorf("%s restore: %v", cfg.Type, err)
		}
	}
	if err := BackupSQLite(&config.DatabaseConfig{Type: "sqlite", DSN: ":memory:"}, filepath.Join(dir, "x.db")); err == nil {
		t.Error("backed up an in-memory database")
	}

	// A database that is not there is not created by looking for it.
	missing := &config.DatabaseConfig{Type: "sqlite", DSN: filepath.Join(dir, "missing.db")}
	if err := BackupSQLite(missing, filepath.Join(dir, "x.db")); err == nil {
		t.Error("backed up a database that does not exist")
	}
	if _, err := os.Stat(filepath.Join(dir, "missing.db")); !errors.Is(err, os.ErrNotExist) {
		t.Error("looking for a missing database created it")
	}

	// Restoring something that is not a backup leaves the database alone.
	path := filepath.Join(dir, "bam.db")
	if err := os.WriteFile(path, []byte("SQLite format 3\x00the real one"), 0o600); err != nil {
		t.Fatal(err)
	}
	junk := filepath.Join(dir, "junk")
	if err := os.WriteFile(junk, []byte("not a database at all"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := RestoreSQLite(&config.DatabaseConfig{Type: "sqlite", DSN: "file:" + path}, junk); err == nil {
		t.Error("restored a file that is not a database")
	}
	if got, _ := os.ReadFile(path); string(got) != "SQLite format 3\x00the real one" {
		t.Error("a refused restore changed the database")
	}
}
