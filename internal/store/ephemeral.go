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

	case "mariadb", "mysql":
		driver = "mysql"
		createSQL = "CREATE DATABASE " + name + " CHARACTER SET utf8mb4"
		dropSQL = "DROP DATABASE IF EXISTS " + name
		mcfg, err := mysql.ParseDSN(adminDSN)
		if err != nil {
			return nil, nil, fmt.Errorf("parsing mariadb dsn: %w", err)
		}
		mcfg.DBName = name
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
