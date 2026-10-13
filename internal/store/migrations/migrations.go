package migrations

import (
	"database/sql"
	"embed"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

//go:embed *.sql
var MigrationFiles embed.FS

var timestampTypeRegex = regexp.MustCompile(`\bTIMESTAMP\b`)

// adaptDDL rewrites portable DDL for dialect quirks. On MariaDB/MySQL, TIMESTAMP
// columns are limited to 2038, are converted through the session time zone, and
// (with explicit_defaults_for_timestamp off, the default before MariaDB 10.10) the
// first TIMESTAMP NOT NULL column of a table silently gains DEFAULT CURRENT_TIMESTAMP
// ON UPDATE CURRENT_TIMESTAMP, which would rewrite created_at on every update.
// DATETIME(6) has none of those behaviors.
func adaptDDL(stmt, dialect string) string {
	switch dialect {
	case "mariadb", "mysql":
		return timestampTypeRegex.ReplaceAllString(stmt, "DATETIME(6)")
	default:
		return stmt
	}
}

// Migrate executes all pending embedded SQL migrations idempotently inside a transaction.
func Migrate(db *sql.DB, dialect string) error {
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
	_, err = db.Exec(adaptDDL(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY,
		applied_at TIMESTAMP NOT NULL
	);`, dialect))
	if err != nil {
		return fmt.Errorf("creating schema_migrations table: %w", err)
	}

	for _, m := range migrationList {
		var exists int
		checkQuery := rebind("SELECT COUNT(*) FROM schema_migrations WHERE version = ?", dialect)
		err := db.QueryRow(checkQuery, m.version).Scan(&exists)
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
			if _, err := tx.Exec(adaptDDL(stmt, dialect)); err != nil {
				_ = tx.Rollback()
				return fmt.Errorf("executing statement in %s: %w\nSQL: %s", m.filename, err, stmt)
			}
		}

		// Record migration version
		recordQuery := rebind("INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)", dialect)
		_, err = tx.Exec(recordQuery, m.version, time.Now().UTC())
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

func rebind(query, dialect string) string {
	if dialect != "postgres" {
		return query
	}

	var b strings.Builder
	b.Grow(len(query) + 16)
	paramIdx := 1
	inString := false

	for i := 0; i < len(query); i++ {
		ch := query[i]
		if ch == '\'' {
			inString = !inString
			b.WriteByte(ch)
			continue
		}
		if ch == '?' && !inString {
			b.WriteByte('$')
			b.WriteString(strconv.Itoa(paramIdx))
			paramIdx++
		} else {
			b.WriteByte(ch)
		}
	}
	return b.String()
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
