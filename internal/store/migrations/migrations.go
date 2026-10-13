package migrations

import (
	"database/sql"
	"embed"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

//go:embed *.sql
var MigrationFiles embed.FS

// Migrate executes all pending embedded SQL migrations idempotently inside a transaction.
func Migrate(db *sql.DB) error {
	entries, err := MigrationFiles.ReadDir(".")
	if err != nil {
		return fmt.Errorf("reading embedded migrations: %w", err)
	}

	type migrationEntry struct {
		version  int
		filename string
	}

	var migrationList []migrationEntry
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		parts := strings.SplitN(entry.Name(), "_", 2)
		if len(parts) < 2 {
			continue
		}
		version, err := strconv.Atoi(parts[0])
		if err != nil {
			continue
		}
		migrationList = append(migrationList, migrationEntry{
			version:  version,
			filename: entry.Name(),
		})
	}

	sort.Slice(migrationList, func(i, j int) bool {
		return migrationList[i].version < migrationList[j].version
	})

	// Ensure tracking table exists
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY,
		applied_at TIMESTAMP NOT NULL
	);`)
	if err != nil {
		return fmt.Errorf("creating schema_migrations table: %w", err)
	}

	for _, m := range migrationList {
		var exists int
		err := db.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE version = ?", m.version).Scan(&exists)
		if err != nil {
			return fmt.Errorf("checking migration version %d: %w", m.version, err)
		}
		if exists > 0 {
			continue // Already applied
		}

		content, err := MigrationFiles.ReadFile(m.filename)
		if err != nil {
			return fmt.Errorf("reading migration file %s: %w", m.filename, err)
		}

		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("beginning transaction for migration %s: %w", m.filename, err)
		}

		// Execute raw SQL statements
		statements := splitSQLStatements(string(content))
		for _, stmt := range statements {
			stmt = strings.TrimSpace(stmt)
			if stmt == "" {
				continue
			}
			if _, err := tx.Exec(stmt); err != nil {
				_ = tx.Rollback()
				return fmt.Errorf("executing statement in %s: %w\nSQL: %s", m.filename, err, stmt)
			}
		}

		// Record migration version
		_, err = tx.Exec("INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)", m.version, time.Now().UTC())
		if err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("recording migration %s: %w", m.filename, err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("committing migration %s: %w", m.filename, err)
		}
	}

	return nil
}

func splitSQLStatements(sqlScript string) []string {
	var statements []string
	lines := strings.Split(sqlScript, "\n")
	var current strings.Builder

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "--") {
			continue
		}
		current.WriteString(line)
		current.WriteString("\n")
		if strings.HasSuffix(trimmed, ";") {
			statements = append(statements, current.String())
			current.Reset()
		}
	}
	if current.Len() > 0 && strings.TrimSpace(current.String()) != "" {
		statements = append(statements, current.String())
	}
	return statements
}
