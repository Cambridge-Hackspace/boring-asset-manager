package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/config"
	"github.com/go-sql-driver/mysql"
)

// Environment variables selecting the database used by test harnesses. When
// TestDialectEnv is unset (or "sqlite"), tests use SQLite.
const (
	TestDialectEnv = "BAM_TEST_DIALECT"
	TestDSNEnv     = "BAM_TEST_DSN"
)

// OpenEphemeral creates a uniquely named, empty database namespace on a PostgreSQL
// (schema) or MariaDB (database) server reachable through adminDSN and opens a Store
// confined to it. The returned cleanup function drops the namespace. It exists so
// test suites can run in isolation against shared database servers.
//
// The namespace is deliberately unfriendly, as a server set up without BAM in
// mind may be: on MariaDB the database defaults to latin1 with a
// case-insensitive collation, and the session starts with no SQL modes (so
// overlong text would be cut short silently), a Latin-1 connection and a time
// zone far from UTC; on PostgreSQL the session time zone is far from UTC and
// backslashes in string literals are escapes. Everything BAM needs, it must
// set for itself.
func OpenEphemeral(dialect, adminDSN string) (*Store, func(), error) {
	suffix := make([]byte, 8)
	if _, err := rand.Read(suffix); err != nil {
		return nil, nil, fmt.Errorf("generating namespace name: %w", err)
	}
	name := "bam_test_" + hex.EncodeToString(suffix)

	var (
		driver     string
		createSQL  string
		dropSQL    string
		isolateDSN string
	)

	switch dialect {
	case "postgres":
		driver = "postgres"
		createSQL = "CREATE SCHEMA " + name
		dropSQL = "DROP SCHEMA IF EXISTS " + name + " CASCADE"
		if strings.Contains(adminDSN, "://") {
			u, err := url.Parse(adminDSN)
			if err != nil {
				return nil, nil, fmt.Errorf("parsing postgres dsn: %w", err)
			}
			q := u.Query()
			q.Set("search_path", name)
			u.RawQuery = q.Encode()
			isolateDSN = u.String()
		} else {
			isolateDSN = adminDSN + " search_path=" + name
		}
		isolateDSN = withPostgresParams(isolateDSN, hostilePostgres)

	case "mariadb", "mysql":
		driver = "mysql"
		createSQL = "CREATE DATABASE " + name + " CHARACTER SET latin1 COLLATE latin1_swedish_ci"
		dropSQL = "DROP DATABASE IF EXISTS " + name
		mcfg, err := mysql.ParseDSN(adminDSN)
		if err != nil {
			return nil, nil, fmt.Errorf("parsing mariadb dsn: %w", err)
		}
		mcfg.DBName = name
		if mcfg.Params == nil {
			mcfg.Params = map[string]string{}
		}
		for k, v := range hostileMariaDB {
			mcfg.Params[k] = v
		}
		isolateDSN = mcfg.FormatDSN()

	default:
		return nil, nil, fmt.Errorf("ephemeral databases are not supported for dialect %q", dialect)
	}

	admin, err := sql.Open(driver, adminDSN)
	if err != nil {
		return nil, nil, fmt.Errorf("opening admin connection: %w", err)
	}
	if _, err := admin.Exec(createSQL); err != nil {
		_ = admin.Close()
		return nil, nil, fmt.Errorf("creating ephemeral namespace %s: %w", name, err)
	}

	cleanup := func() {
		_, _ = admin.Exec(dropSQL)
		_ = admin.Close()
	}

	st, err := Open(&config.DatabaseConfig{Type: dialect, DSN: isolateDSN})
	if err != nil {
		cleanup()
		return nil, nil, err
	}

	return st, func() {
		_ = st.Close()
		cleanup()
	}, nil
}

// Session settings that OpenEphemeral starts each connection with.
var (
	hostilePostgres = map[string]string{
		"TimeZone":                    "Pacific/Chatham",
		"standard_conforming_strings": "off",
	}
	hostileMariaDB = map[string]string{
		"charset":   "latin1",
		"sql_mode":  "''",
		"time_zone": "'+05:45'",
	}
)

// withPostgresParams adds run-time parameters to a URL or key=value DSN.
func withPostgresParams(dsn string, params map[string]string) string {
	if strings.Contains(dsn, "://") {
		u, err := url.Parse(dsn)
		if err != nil {
			return dsn
		}
		q := u.Query()
		for k, v := range params {
			q.Set(k, v)
		}
		u.RawQuery = q.Encode()
		return u.String()
	}
	for k, v := range params {
		dsn += " " + k + "=" + v
	}
	return dsn
}
