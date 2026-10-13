package store

import (
	"path/filepath"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/config"
)

func TestSQLiteDSNAppliesPragmas(t *testing.T) {
	cases := []struct {
		name        string
		dsn         string
		journalMode string
		busyTimeout int
	}{
		{"bare path", "bam.db", "wal", 5000},
		{"legacy mattn params", "bam.db?_journal=WAL&_busy_timeout=7000&_foreign_keys=0", "wal", 7000},
		{"explicit pragmas", "bam.db?_pragma=journal_mode(DELETE)&_pragma=busy_timeout(1234)&_pragma=foreign_keys(0)", "delete", 1234},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dsn := filepath.Join(t.TempDir(), tc.dsn)
			st, err := Open(&config.DatabaseConfig{Type: "sqlite", DSN: dsn})
			if err != nil {
				t.Fatalf("Open(%q): %v", dsn, err)
			}
			defer st.Close()

			var journalMode string
			if err := st.QueryRow("PRAGMA journal_mode").Scan(&journalMode); err != nil {
				t.Fatalf("reading journal_mode: %v", err)
			}
			if journalMode != tc.journalMode {
				t.Errorf("journal_mode = %q, want %q", journalMode, tc.journalMode)
			}

			var busyTimeout int
			if err := st.QueryRow("PRAGMA busy_timeout").Scan(&busyTimeout); err != nil {
				t.Fatalf("reading busy_timeout: %v", err)
			}
			if busyTimeout != tc.busyTimeout {
				t.Errorf("busy_timeout = %d, want %d", busyTimeout, tc.busyTimeout)
			}

			var foreignKeys int
			if err := st.QueryRow("PRAGMA foreign_keys").Scan(&foreignKeys); err != nil {
				t.Fatalf("reading foreign_keys: %v", err)
			}
			if foreignKeys != 1 {
				t.Errorf("foreign_keys = %d, want 1 (enforcement must always be on)", foreignKeys)
			}
		})
	}
}

func TestSQLiteForeignKeyCascades(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()

	parent, err := st.CreateCategory("Parent", nil, "", false, false)
	if err != nil {
		t.Fatalf("creating parent: %v", err)
	}
	child, err := st.CreateCategory("Child", &parent.ID, "", false, false)
	if err != nil {
		t.Fatalf("creating child: %v", err)
	}

	if err := st.DeleteCategory(parent.ID); err != nil {
		t.Fatalf("deleting parent: %v", err)
	}

	reloaded, err := st.GetCategoryByID(child.ID)
	if err != nil {
		t.Fatalf("reloading child: %v", err)
	}
	if reloaded.ParentID != nil {
		t.Errorf("expected ON DELETE SET NULL to clear child's parent_id, got %q", *reloaded.ParentID)
	}
}
