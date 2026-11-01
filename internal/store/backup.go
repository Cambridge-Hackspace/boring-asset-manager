package store

import (
	"bytes"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/config"
)

// ErrBackupUnsupported is returned for databases BAM does not back up
// itself: PostgreSQL and MariaDB have their own tools (docs/installing.md).
var ErrBackupUnsupported = errors.New("BAM backs up only SQLite databases; back up PostgreSQL and MariaDB with their own tools")

// sqliteHeader starts every SQLite database file.
const sqliteHeader = "SQLite format 3\x00"

// sqliteFile returns the file a SQLite configuration names.
func sqliteFile(cfg *config.DatabaseConfig) (string, error) {
	if cfg.Type != "sqlite" {
		return "", ErrBackupUnsupported
	}
	path, _, _ := strings.Cut(cfg.DSN, "?")
	path = strings.TrimPrefix(path, "file:")
	if path == "" || strings.HasPrefix(path, ":memory:") || strings.Contains(cfg.DSN, "mode=memory") {
		return "", errors.New("an in-memory database cannot be backed up")
	}
	return path, nil
}

// BackupSQLite writes a consistent copy of a SQLite database to dest, which
// must not exist, readable only by its owner. It reads the database as it
// is, without migrating it, and is safe while BAM is running.
func BackupSQLite(cfg *config.DatabaseConfig, dest string) error {
	path, err := sqliteFile(cfg)
	if err != nil {
		return err
	}
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("the database %s: %w", path, err)
	}
	if _, err := os.Lstat(dest); err == nil {
		return fmt.Errorf("%s already exists", dest)
	}
	db, err := sql.Open("sqlite", sqliteDSN(cfg.DSN))
	if err != nil {
		return fmt.Errorf("opening the database: %w", err)
	}
	defer db.Close()

	// VACUUM INTO writes a new, compact database from one read transaction.
	// Write beside dest and rename, so dest is never half written.
	tmp := dest + ".partial"
	_ = os.Remove(tmp)
	if _, err := db.Exec("VACUUM INTO ?", tmp); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("copying the database: %w", err)
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, dest); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// RestoreSQLite replaces a SQLite database with a backup made by
// BackupSQLite. BAM must not be running. The restored file keeps the
// database file's owner and mode, and the old write-ahead log is removed
// first, so it cannot be applied to the restored database.
func RestoreSQLite(cfg *config.DatabaseConfig, from string) error {
	path, err := sqliteFile(cfg)
	if err != nil {
		return err
	}
	src, err := os.Open(from)
	if err != nil {
		return err
	}
	defer src.Close()
	header := make([]byte, len(sqliteHeader))
	if _, err := io.ReadFull(src, header); err != nil || !bytes.Equal(header, []byte(sqliteHeader)) {
		return fmt.Errorf("%s is not a SQLite database", from)
	}
	if _, err := src.Seek(0, io.SeekStart); err != nil {
		return err
	}

	mode := os.FileMode(0o600)
	old, statErr := os.Stat(path)
	if statErr == nil {
		mode = old.Mode().Perm()
	}
	tmp := filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+".restoring")
	out, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, src)
	if err == nil {
		err = out.Sync()
	}
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err == nil && statErr == nil {
		err = sameOwner(tmp, old)
	}
	if err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("copying the backup: %w", err)
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		if err := os.Remove(path + suffix); err != nil && !errors.Is(err, os.ErrNotExist) {
			_ = os.Remove(tmp)
			return err
		}
	}
	return os.Rename(tmp, path)
}

// SQLiteFile is the file a SQLite database is kept in; ErrBackupUnsupported
// for another database.
func SQLiteFile(cfg *config.DatabaseConfig) (string, error) { return sqliteFile(cfg) }
