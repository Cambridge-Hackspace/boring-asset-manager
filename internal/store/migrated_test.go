package store

import (
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/config"
)

// A copy of the database migrated once is what migrating would make: the
// same schema, migrations recorded and rows; in memory or in a file; and
// each copy is its own.
func TestAMigratedCopyIsAFreshlyMigratedDatabase(t *testing.T) {
	fresh, err := Open(&config.DatabaseConfig{Type: "sqlite", DSN: ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	defer fresh.Close()
	if err := fresh.Migrate(); err != nil {
		t.Fatal(err)
	}
	want := contents(t, fresh)
	if len(want["schema"]) < 40 {
		t.Fatalf("the migrated schema has %d objects", len(want["schema"]))
	}

	var copies []*Store
	for _, dsn := range []string{":memory:", ":memory:", filepath.Join(t.TempDir(), "a.db"), filepath.Join(t.TempDir(), "b.db") + "?_journal=WAL&_busy_timeout=5000"} {
		st, err := OpenMigratedSQLite(dsn)
		if err != nil {
			t.Fatalf("%s: %v", dsn, err)
		}
		defer st.Close()
		if got := contents(t, st); !reflect.DeepEqual(got, want) {
			t.Errorf("%s: %v\nwant %v", dsn, got, want)
		}
		// a file keeps the journal it was opened with
		var journal string
		if err := st.QueryRow("PRAGMA journal_mode").Scan(&journal); err != nil || (strings.Contains(dsn, "WAL") && journal != "wal") {
			t.Errorf("%s: journal %q %v", dsn, journal, err)
		}
		// and nothing is left to migrate
		if err := st.Migrate(); err != nil {
			t.Errorf("%s: migrating again: %v", dsn, err)
		}
		copies = append(copies, st)
	}

	if _, err := copies[0].CreateField("mine", "Mine", "text", nil); err != nil {
		t.Fatal(err)
	}
	for i, st := range copies[1:] {
		var n int
		if err := st.QueryRow("SELECT COUNT(*) FROM fields WHERE field_key = 'mine'").Scan(&n); err != nil || n != 0 {
			t.Errorf("copy %d sees another's field: %d %v", i+1, n, err)
		}
	}
}

// contents lists a database's schema, the migrations it records and its
// rows' counts, the times migrations were applied aside.
func contents(t *testing.T, st *Store) map[string][]string {
	t.Helper()
	out := map[string][]string{}
	add := func(key, query string) {
		rows, err := st.Query(query)
		if err != nil {
			t.Fatalf("%s: %v", query, err)
		}
		defer rows.Close()
		for rows.Next() {
			var s string
			if err := rows.Scan(&s); err != nil {
				t.Fatal(err)
			}
			out[key] = append(out[key], s)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
	}
	add("schema", "SELECT type || ' ' || name || ': ' || COALESCE(sql, '') FROM sqlite_master ORDER BY type, name")
	add("migrations", "SELECT CAST(version AS TEXT) FROM schema_migrations ORDER BY version")
	add("tables", "SELECT name FROM sqlite_master WHERE type = 'table' ORDER BY name")
	for _, table := range out["tables"] {
		var n int
		if err := st.QueryRow(`SELECT COUNT(*) FROM "` + table + `"`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		out["rows"] = append(out["rows"], fmt.Sprintf("%s %d", table, n))
	}
	return out
}
