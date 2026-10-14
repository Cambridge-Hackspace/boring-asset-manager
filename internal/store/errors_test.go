package store

import (
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"
)

// TestClassifyingErrors provokes real errors from the database under test
// and checks how they are classified, beside errors that are about a
// request rather than the database.
func TestClassifyingErrors(t *testing.T) {
	st := setupTestStore(t)

	insert := func() error {
		now := time.Now().UTC()
		_, err := st.Exec(`INSERT INTO fields (id, field_key, label, validation_type, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
			fmt.Sprintf("id-%d", now.UnixNano()), "serial", "Serial", "text", now, now)
		return err
	}
	if err := insert(); err != nil {
		t.Fatal(err)
	}
	duplicate := insert()
	missing := st.QueryRow("SELECT COUNT(*) FROM no_such_table").Scan(new(int))
	var noRows string
	notFound := st.QueryRow("SELECT field_key FROM fields WHERE id = ?", "nothing").Scan(&noRows)

	for _, c := range []struct {
		name             string
		err              error
		failure, already bool
	}{
		{"a duplicate key", duplicate, true, true},
		{"a duplicate key, wrapped", fmt.Errorf("creating field: %w", duplicate), true, true},
		{"a missing table", missing, true, false},
		{"no rows", notFound, false, false},
		{"no rows, wrapped", fmt.Errorf("finding: %w", sql.ErrNoRows), false, false},
		{"a validation error", errors.New("asset name cannot be empty"), false, false},
		{"a sentinel", ErrAssetNotFound, false, false},
		{"nil", nil, false, false},
	} {
		if c.err == nil && c.name != "nil" {
			t.Fatalf("%s: the database returned no error", c.name)
		}
		if got := IsDatabaseFailure(c.err); got != c.failure {
			t.Errorf("%s (%v): IsDatabaseFailure = %v, want %v", c.name, c.err, got, c.failure)
		}
		if got := IsUniqueViolation(c.err); got != c.already {
			t.Errorf("%s (%v): IsUniqueViolation = %v, want %v", c.name, c.err, got, c.already)
		}
	}

	st.Close()
	if err := st.QueryRow("SELECT 1").Scan(new(int)); !IsDatabaseFailure(err) {
		t.Errorf("a closed database (%v) is not a failure", err)
	}
}
