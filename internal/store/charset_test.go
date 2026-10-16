package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/config"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store/migrations"
)

func onlyMariaDB(t *testing.T, st *Store) {
	t.Helper()
	if st.Dialect != "mariadb" && st.Dialect != "mysql" {
		t.Skip("MariaDB only; set BAM_TEST_DIALECT=mariadb")
	}
}

// TestMariaDBSessionIgnoresServerDefaults checks that the settings BAM needs
// hold although the test database starts each session with Latin-1, no SQL
// modes and a time zone away from UTC (see OpenEphemeral).
func TestMariaDBSessionIgnoresServerDefaults(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	onlyMariaDB(t, st)

	var client, conn, mode, zone string
	if err := st.DB.QueryRow("SELECT @@character_set_client, @@character_set_connection, @@sql_mode, @@time_zone").Scan(&client, &conn, &mode, &zone); err != nil {
		t.Fatal(err)
	}
	if client != "utf8mb4" || conn != "utf8mb4" {
		t.Errorf("connection character set %s/%s, want utf8mb4", client, conn)
	}
	if !strings.Contains(mode, "STRICT_ALL_TABLES") || strings.Contains(mode, "ANSI_QUOTES") || strings.Contains(mode, "NO_BACKSLASH_ESCAPES") {
		t.Errorf("sql_mode %q", mode)
	}
	if zone != "+00:00" {
		t.Errorf("time_zone %q, want +00:00", zone)
	}
}

// textColumnProblems lists text columns that are not utf8mb4 with the exact
// collation, or whose type has a byte limit SQLite and PostgreSQL lack.
func textColumnProblems(t *testing.T, st *Store) []string {
	t.Helper()
	rows, err := st.DB.Query(`
		SELECT TABLE_NAME, COLUMN_NAME, DATA_TYPE, COLLATION_NAME
		FROM information_schema.COLUMNS
		WHERE TABLE_SCHEMA = DATABASE() AND COLLATION_NAME IS NOT NULL`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var problems []string
	n := 0
	for rows.Next() {
		var table, column, dataType, collation string
		if err := rows.Scan(&table, &column, &dataType, &collation); err != nil {
			t.Fatal(err)
		}
		n++
		if collation != "utf8mb4_nopad_bin" || strings.HasSuffix(dataType, "text") && dataType != "longtext" {
			problems = append(problems, table+"."+column+" "+dataType+" "+collation)
		}
	}
	if n < 20 {
		t.Fatalf("only %d text columns found", n)
	}
	return problems
}

// TestMariaDBTablesHoldTextExactly checks every text column, in a database
// whose default is latin1 with a case-insensitive collation.
func TestMariaDBTablesHoldTextExactly(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	onlyMariaDB(t, st)
	if p := textColumnProblems(t, st); len(p) > 0 {
		t.Errorf("columns not converted:\n%s", strings.Join(p, "\n"))
	}
}

// oldMariaDBSchema replaces the store's tables with those BAM created before
// migration 8: migrations 1 to 7, with TIMESTAMP rewritten but nothing else,
// so tables take the database's latin1 and TEXT stays TEXT.
func oldMariaDBSchema(t *testing.T, st *Store) {
	t.Helper()
	ctx := context.Background()
	conn, err := st.DB.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	exec := func(stmt string) {
		t.Helper()
		if _, err := conn.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("%v\nSQL: %s", err, stmt)
		}
	}
	exec("SET FOREIGN_KEY_CHECKS = 0")
	rows, err := conn.QueryContext(ctx, "SELECT TABLE_NAME FROM information_schema.TABLES WHERE TABLE_SCHEMA = DATABASE()")
	if err != nil {
		t.Fatal(err)
	}
	var tables []string
	for rows.Next() {
		var name string
		_ = rows.Scan(&name)
		tables = append(tables, name)
	}
	rows.Close()
	for _, name := range tables {
		exec("DROP TABLE `" + name + "`")
	}
	exec("SET FOREIGN_KEY_CHECKS = 1")

	timestamp := regexp.MustCompile(`\bTIMESTAMP\b`)
	// Migrations 9, 10 and 13 too, though they came later: they add columns
	// the store reads, so the test can write through the store. Their
	// columns are latin1 as well.
	for _, v := range []int{1, 2, 3, 4, 5, 6, 7, 9, 10, 13} {
		matches, _ := fs.Glob(migrations.MigrationFiles, fmt.Sprintf("%03d_*.sql", v))
		if len(matches) != 1 {
			t.Fatalf("migration %d: %v", v, matches)
		}
		content, _ := migrations.MigrationFiles.ReadFile(matches[0])
		var b strings.Builder
		for _, line := range strings.Split(strings.ReplaceAll(string(content), "\r", ""), "\n") {
			if !strings.HasPrefix(strings.TrimSpace(line), "--") {
				b.WriteString(line + "\n")
			}
		}
		for _, stmt := range strings.Split(b.String(), ";\n") {
			if strings.TrimSpace(stmt) != "" {
				exec(timestamp.ReplaceAllString(stmt, "DATETIME(6)"))
			}
		}
		exec(fmt.Sprintf("INSERT INTO schema_migrations (version, applied_at) VALUES (%d, NOW())", v))
	}
	if err := st.SeedDefaultPermissionsAndRoles(); err != nil {
		t.Fatal(err)
	}
}

// foreignKeyCount counts the foreign keys in the store's database.
func foreignKeyCount(t *testing.T, st *Store) int {
	t.Helper()
	var n int
	if err := st.DB.QueryRow("SELECT COUNT(*) FROM information_schema.REFERENTIAL_CONSTRAINTS WHERE CONSTRAINT_SCHEMA = DATABASE()").Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// TestMariaDBUpgradeConvertsText upgrades a database BAM created before
// migration 8, with data in it.
func TestMariaDBUpgradeConvertsText(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	onlyMariaDB(t, st)
	keys := foreignKeyCount(t, st)
	oldMariaDBSchema(t, st)
	if p := textColumnProblems(t, st); len(p) == 0 {
		t.Fatal("the old schema is already converted")
	}

	f, err := st.CreateField("notes", "Notes", "text", nil)
	if err != nil {
		t.Fatal(err)
	}
	cat, err := st.CreateCategory("Café tools", nil, "", false, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AssignCategoryField(cat.ID, f.ID, false); err != nil {
		t.Fatal(err)
	}
	a, err := st.CreateAsset("Crème brûlée torch", []string{cat.ID}, []models.AssetGeneratorKey{{GeneratorType: "qr", AssignedCode: "Torch"}}, map[string]string{"notes": "Gas: ½ full"})
	if err != nil {
		t.Fatal(err)
	}
	// latin1_swedish_ci calls these the same code.
	if _, err := st.CreateAsset("Other", nil, []models.AssetGeneratorKey{{GeneratorType: "qr", AssignedCode: "torch"}}, nil); err == nil {
		t.Fatal("the old schema took a code differing only in case")
	}

	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	if p := textColumnProblems(t, st); len(p) > 0 {
		t.Errorf("columns not converted:\n%s", strings.Join(p, "\n"))
	}
	if n := foreignKeyCount(t, st); n != keys {
		t.Errorf("%d foreign keys after converting, want %d", n, keys)
	}
	got, err := st.GetAssetByID(a.Asset.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Asset.Name != "Crème brûlée torch" || got.Categories[0].Name != "Café tools" || got.FieldValues["notes"] != "Gas: ½ full" {
		t.Errorf("data changed: %q in %q, notes %q", got.Asset.Name, got.Categories[0].Name, got.FieldValues["notes"])
	}
	if _, err := st.CreateAsset("Other", nil, []models.AssetGeneratorKey{{GeneratorType: "qr", AssignedCode: "torch"}}, nil); err != nil {
		t.Errorf("a code differing only in case: %v", err)
	}
	// The keys still hold.
	if _, err := st.DB.Exec("INSERT INTO asset_categories (asset_id, category_id) VALUES (?, ?)", "no-such-asset", cat.ID); err == nil {
		t.Error("a row referring to no asset was accepted")
	}
}

// TestMariaDBUpgradeResumes finishes a conversion that stopped after
// dropping foreign keys, from the statements it saved.
func TestMariaDBUpgradeResumes(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	onlyMariaDB(t, st)
	// Later migrations add keys of their own, so the old schema has fewer.
	keys := foreignKeyCount(t, st)
	oldMariaDBSchema(t, st)
	oldKeys := foreignKeyCount(t, st)

	// As a run that saved one key, dropped it, and stopped.
	var table, name string
	if err := st.DB.QueryRow("SELECT TABLE_NAME, CONSTRAINT_NAME FROM information_schema.REFERENTIAL_CONSTRAINTS WHERE CONSTRAINT_SCHEMA = DATABASE() ORDER BY CONSTRAINT_NAME LIMIT 1").Scan(&table, &name); err != nil {
		t.Fatal(err)
	}
	var create string
	if err := st.DB.QueryRow("SHOW CREATE TABLE `"+table+"`").Scan(&table, &create); err != nil {
		t.Fatal(err)
	}
	clause := regexp.MustCompile("CONSTRAINT `" + name + "` FOREIGN KEY [^\n]*").FindString(create)
	clause = strings.TrimSuffix(strings.TrimSpace(clause), ",")
	for _, stmt := range []string{
		"CREATE TABLE schema_pending_foreign_keys (name VARCHAR(64) PRIMARY KEY, stmt LONGTEXT NOT NULL)",
		"ALTER TABLE `" + table + "` DROP FOREIGN KEY `" + name + "`",
	} {
		if _, err := st.DB.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := st.DB.Exec("INSERT INTO schema_pending_foreign_keys (name, stmt) VALUES (?, ?)", name, "ALTER TABLE `"+table+"` ADD "+clause); err != nil {
		t.Fatal(err)
	}
	if n := foreignKeyCount(t, st); n != oldKeys-1 {
		t.Fatalf("%d foreign keys before resuming, want %d", n, oldKeys-1)
	}

	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	if n := foreignKeyCount(t, st); n != keys {
		t.Errorf("%d foreign keys after resuming, want %d", n, keys)
	}
	var left int
	if err := st.DB.QueryRow("SELECT COUNT(*) FROM information_schema.TABLES WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'schema_pending_foreign_keys'").Scan(&left); err != nil || left != 0 {
		t.Errorf("the saved statements remain (%d, %v)", left, err)
	}
	if p := textColumnProblems(t, st); len(p) > 0 {
		t.Errorf("columns not converted:\n%s", strings.Join(p, "\n"))
	}
}

// TestPostgresRefusesOtherEncodings creates databases in an encoding that
// cannot hold every character and in one that checks nothing, and expects
// Open to refuse both with the reason.
func TestPostgresRefusesOtherEncodings(t *testing.T) {
	if os.Getenv(TestDialectEnv) != "postgres" {
		t.Skip("PostgreSQL only; set BAM_TEST_DIALECT=postgres")
	}
	dsn := os.Getenv(TestDSNEnv)
	admin, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	for _, encoding := range []string{"LATIN1", "SQL_ASCII"} {
		suffix := make([]byte, 6)
		_, _ = rand.Read(suffix)
		name := "bam_enc_" + hex.EncodeToString(suffix)
		if _, err := admin.Exec("CREATE DATABASE " + name + " ENCODING '" + encoding + "' LC_COLLATE 'C' LC_CTYPE 'C' TEMPLATE template0"); err != nil {
			t.Fatalf("creating a %s database: %v", encoding, err)
		}
		t.Cleanup(func() { _, _ = admin.Exec("DROP DATABASE IF EXISTS " + name) })

		target := dsn + " dbname=" + name
		if strings.Contains(dsn, "://") {
			u, err := url.Parse(dsn)
			if err != nil {
				t.Fatal(err)
			}
			u.Path = "/" + name
			target = u.String()
		}
		st, err := Open(&config.DatabaseConfig{Type: "postgres", DSN: target})
		if err == nil {
			st.Close()
			t.Errorf("%s: opened", encoding)
			continue
		}
		if !errors.Is(err, ErrUnsupportedDatabase) || !strings.Contains(err.Error(), encoding) {
			t.Errorf("%s: %v", encoding, err)
		}
	}
}
