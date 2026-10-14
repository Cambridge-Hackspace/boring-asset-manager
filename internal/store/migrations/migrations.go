package migrations

import (
	"context"
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

var (
	timestampTypeRegex = regexp.MustCompile(`\bTIMESTAMP\b`)
	textTypeRegex      = regexp.MustCompile(`\bTEXT\b`)
	createTableRegex   = regexp.MustCompile(`(?is)^\s*CREATE\s+TABLE\b.*\)\s*;?\s*$`)
)

// adaptDDL rewrites portable DDL for dialect quirks. On MariaDB/MySQL:
//
//   - TIMESTAMP columns are limited to 2038, are converted through the session
//     time zone, and (with explicit_defaults_for_timestamp off, the default
//     before MariaDB 10.10) the first TIMESTAMP NOT NULL column of a table
//     silently gains DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
//     which would rewrite created_at on every update. DATETIME(6) has none of
//     those behaviors.
//   - TEXT holds 65,535 bytes, where SQLite and PostgreSQL have no limit;
//     LONGTEXT matches them.
//   - New tables otherwise take the database's character set and collation,
//     often latin1 with case-insensitive comparison. Tables are created in
//     utf8mb4 with collation, an exact binary one, so text compares as it does
//     on SQLite and PostgreSQL.
func adaptDDL(stmt, dialect, collation string) string {
	switch dialect {
	case "mariadb", "mysql":
		stmt = timestampTypeRegex.ReplaceAllString(stmt, "DATETIME(6)")
		stmt = textTypeRegex.ReplaceAllString(stmt, "LONGTEXT")
		if createTableRegex.MatchString(stmt) {
			stmt = strings.TrimRight(strings.TrimSpace(stmt), ";") + " DEFAULT CHARACTER SET utf8mb4 COLLATE " + collation
		}
		return stmt
	default:
		return stmt
	}
}

// binaryCollations are exact utf8mb4 collations, best first: MariaDB's and
// MySQL's that also count trailing spaces, then one that ignores them.
var binaryCollations = []string{"utf8mb4_nopad_bin", "utf8mb4_0900_bin", "utf8mb4_bin"}

// textCollation returns the collation tables are created with on dialect.
func textCollation(db *sql.DB, dialect string) (string, error) {
	if dialect != "mariadb" && dialect != "mysql" {
		return "", nil
	}
	for _, c := range binaryCollations {
		var n int
		if err := db.QueryRow("SELECT COUNT(*) FROM information_schema.COLLATIONS WHERE COLLATION_NAME = ?", c).Scan(&n); err != nil {
			return "", fmt.Errorf("looking up collation %s: %w", c, err)
		}
		if n > 0 {
			return c, nil
		}
	}
	return "", fmt.Errorf("the server has none of the collations %s", strings.Join(binaryCollations, ", "))
}

// codeMigration is a migration written in Go, for changes that differ by
// dialect or depend on what is in the database.
type codeMigration struct {
	version int
	name    string
	run     func(db *sql.DB, dialect, collation string) error
}

var codeMigrations = []codeMigration{
	{8, "mariadb_text", convertMariaDBText},
}

// convertMariaDBText brings tables created before adaptDDL set their
// character set, collation and text types up to date on MariaDB/MySQL: each
// table is converted to utf8mb4 with the binary collation, and TEXT columns
// become LONGTEXT. Tables already right are left alone. Other dialects have
// nothing to convert.
//
// Converting from latin1 keeps every character latin1 could hold. Uniqueness
// only becomes stricter about what is equal, so no existing row conflicts.
func convertMariaDBText(db *sql.DB, dialect, collation string) error {
	if dialect != "mariadb" && dialect != "mysql" {
		return nil
	}
	rows, err := db.Query(`
		SELECT c.TABLE_NAME, c.COLUMN_NAME, c.DATA_TYPE, c.IS_NULLABLE, c.COLLATION_NAME
		FROM information_schema.COLUMNS c
		JOIN information_schema.TABLES t ON t.TABLE_SCHEMA = c.TABLE_SCHEMA AND t.TABLE_NAME = c.TABLE_NAME
		WHERE c.TABLE_SCHEMA = DATABASE() AND t.TABLE_TYPE = 'BASE TABLE' AND c.COLLATION_NAME IS NOT NULL
		ORDER BY c.TABLE_NAME, c.ORDINAL_POSITION`)
	if err != nil {
		return fmt.Errorf("listing text columns: %w", err)
	}
	type column struct{ name, dataType, nullable, collation string }
	tables := map[string][]column{}
	var order []string
	for rows.Next() {
		var table string
		var c column
		if err := rows.Scan(&table, &c.name, &c.dataType, &c.nullable, &c.collation); err != nil {
			rows.Close()
			return err
		}
		if _, seen := tables[table]; !seen {
			order = append(order, table)
		}
		tables[table] = append(tables[table], c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	var alters []string
	for _, table := range order {
		var changes []string
		convert := false
		for _, c := range tables[table] {
			if c.collation != collation {
				convert = true
			}
			if c.dataType == "tinytext" || c.dataType == "text" || c.dataType == "mediumtext" {
				null := " NOT NULL"
				if c.nullable == "YES" {
					null = " NULL"
				}
				changes = append(changes, "MODIFY `"+c.name+"` LONGTEXT CHARACTER SET utf8mb4 COLLATE "+collation+null)
			}
		}
		if convert {
			// Converting first may itself widen TEXT to MEDIUMTEXT; the
			// MODIFY clauses after it settle every one on LONGTEXT.
			changes = append([]string{"CONVERT TO CHARACTER SET utf8mb4 COLLATE " + collation}, changes...)
		}
		if len(changes) > 0 {
			alters = append(alters, "ALTER TABLE `"+table+"` "+strings.Join(changes, ", "))
		}
	}

	ctx := context.Background()
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	var pending int
	if err := conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.TABLES WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ?", pendingForeignKeys).Scan(&pending); err != nil {
		return err
	}
	if len(alters) == 0 && pending == 0 {
		return nil
	}

	// MariaDB will not change the character set of a column a foreign key
	// uses, even with foreign key checks off, so the keys are dropped and
	// added back around the conversion. The statements that add them back
	// are saved first, so a run that fails part way leaves them for the next
	// run to finish rather than losing the keys.
	if _, err := conn.ExecContext(ctx, "SET FOREIGN_KEY_CHECKS = 0"); err != nil {
		return err
	}
	defer conn.ExecContext(ctx, "SET FOREIGN_KEY_CHECKS = 1") //nolint:errcheck
	if _, err := conn.ExecContext(ctx, "CREATE TABLE IF NOT EXISTS "+pendingForeignKeys+" (name VARCHAR(64) PRIMARY KEY, stmt LONGTEXT NOT NULL) DEFAULT CHARACTER SET utf8mb4 COLLATE "+collation); err != nil {
		return err
	}
	keys, err := foreignKeys(ctx, conn)
	if err != nil {
		return err
	}
	for _, k := range keys {
		if _, err := conn.ExecContext(ctx, "INSERT IGNORE INTO "+pendingForeignKeys+" (name, stmt) VALUES (?, ?)", k.name, k.add); err != nil {
			return fmt.Errorf("saving foreign key %s: %w", k.name, err)
		}
	}
	for _, k := range keys {
		if _, err := conn.ExecContext(ctx, "ALTER TABLE `"+k.table+"` DROP FOREIGN KEY `"+k.name+"`"); err != nil {
			return fmt.Errorf("dropping foreign key %s: %w", k.name, err)
		}
	}
	for _, stmt := range alters {
		if _, err := conn.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("converting: %w\nSQL: %s", err, stmt)
		}
	}
	rows, err = conn.QueryContext(ctx, "SELECT name, stmt FROM "+pendingForeignKeys+" ORDER BY name")
	if err != nil {
		return err
	}
	var adds [][2]string
	for rows.Next() {
		var name, stmt string
		if err := rows.Scan(&name, &stmt); err != nil {
			rows.Close()
			return err
		}
		adds = append(adds, [2]string{name, stmt})
	}
	rows.Close()
	for _, a := range adds {
		if _, err := conn.ExecContext(ctx, a[1]); err != nil {
			return fmt.Errorf("adding foreign key %s back: %w\nSQL: %s", a[0], err, a[1])
		}
		if _, err := conn.ExecContext(ctx, "DELETE FROM "+pendingForeignKeys+" WHERE name = ?", a[0]); err != nil {
			return err
		}
	}
	_, err = conn.ExecContext(ctx, "DROP TABLE "+pendingForeignKeys)
	return err
}

// pendingForeignKeys holds, while convertMariaDBText runs, the statements
// that add back the foreign keys it dropped.
const pendingForeignKeys = "schema_pending_foreign_keys"

// foreignKey is a foreign key and the statement that adds it.
type foreignKey struct{ table, name, add string }

// foreignKeys lists the foreign keys in the current MariaDB database.
func foreignKeys(ctx context.Context, conn *sql.Conn) ([]foreignKey, error) {
	rows, err := conn.QueryContext(ctx, `
		SELECT k.TABLE_NAME, k.CONSTRAINT_NAME, k.COLUMN_NAME, k.REFERENCED_TABLE_NAME, k.REFERENCED_COLUMN_NAME, rc.UPDATE_RULE, rc.DELETE_RULE
		FROM information_schema.KEY_COLUMN_USAGE k
		JOIN information_schema.REFERENTIAL_CONSTRAINTS rc
			ON rc.CONSTRAINT_SCHEMA = k.CONSTRAINT_SCHEMA AND rc.CONSTRAINT_NAME = k.CONSTRAINT_NAME AND rc.TABLE_NAME = k.TABLE_NAME
		WHERE k.TABLE_SCHEMA = DATABASE() AND k.REFERENCED_TABLE_NAME IS NOT NULL
		ORDER BY k.TABLE_NAME, k.CONSTRAINT_NAME, k.ORDINAL_POSITION`)
	if err != nil {
		return nil, fmt.Errorf("listing foreign keys: %w", err)
	}
	defer rows.Close()
	type key struct {
		table, name, refTable, onUpdate, onDelete string
		cols, refCols                             []string
	}
	var list []*key
	for rows.Next() {
		var table, name, col, refTable, refCol, onUpdate, onDelete string
		if err := rows.Scan(&table, &name, &col, &refTable, &refCol, &onUpdate, &onDelete); err != nil {
			return nil, err
		}
		if n := len(list); n == 0 || list[n-1].table != table || list[n-1].name != name {
			list = append(list, &key{table: table, name: name, refTable: refTable, onUpdate: onUpdate, onDelete: onDelete})
		}
		k := list[len(list)-1]
		k.cols = append(k.cols, "`"+col+"`")
		k.refCols = append(k.refCols, "`"+refCol+"`")
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	keys := make([]foreignKey, len(list))
	for i, k := range list {
		keys[i] = foreignKey{table: k.table, name: k.name, add: fmt.Sprintf(
			"ALTER TABLE `%s` ADD CONSTRAINT `%s` FOREIGN KEY (%s) REFERENCES `%s` (%s) ON DELETE %s ON UPDATE %s",
			k.table, k.name, strings.Join(k.cols, ", "), k.refTable, strings.Join(k.refCols, ", "), k.onDelete, k.onUpdate)}
	}
	return keys, nil
}

// Migrate executes all pending embedded SQL migrations idempotently inside a transaction.
func Migrate(db *sql.DB, dialect string) error {
	entries, err := MigrationFiles.ReadDir(".")
	if err != nil {
		return fmt.Errorf("reading embedded migrations: %w", err)
	}

	collation, err := textCollation(db, dialect)
	if err != nil {
		return err
	}

	type migrationEntry struct {
		version  int
		filename string
		code     *codeMigration
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

	for i := range codeMigrations {
		m := &codeMigrations[i]
		migrationList = append(migrationList, migrationEntry{version: m.version, filename: fmt.Sprintf("%03d_%s (Go)", m.version, m.name), code: m})
	}

	sort.Slice(migrationList, func(i, j int) bool {
		return migrationList[i].version < migrationList[j].version
	})

	// Ensure tracking table exists
	_, err = db.Exec(adaptDDL(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY,
		applied_at TIMESTAMP NOT NULL
	);`, dialect, collation))
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

		if m.code != nil {
			// DDL commits implicitly on MariaDB, so a Go migration runs
			// outside a transaction and must be safe to run again.
			if err := m.code.run(db, dialect, collation); err != nil {
				return fmt.Errorf("running migration %s: %w", m.filename, err)
			}
			recordQuery := rebind("INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)", dialect)
			if _, err := db.Exec(recordQuery, m.version, time.Now().UTC()); err != nil {
				return fmt.Errorf("recording migration %s: %w", m.filename, err)
			}
			continue
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
			if _, err := tx.Exec(adaptDDL(stmt, dialect, collation)); err != nil {
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
