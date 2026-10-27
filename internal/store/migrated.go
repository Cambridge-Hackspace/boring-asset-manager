package store

import (
	"context"
	"fmt"
	"sync"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/config"
	"modernc.org/sqlite"
)

// migratedURI names the in-memory database OpenMigratedSQLite migrates once
// per process and copies from. Connections share it, and it lives while the
// template store holds one open.
const migratedURI = "file:bam-migrated?mode=memory&cache=shared"

var migrated struct {
	once     sync.Once
	template *Store
	err      error
	// copying makes one copy at a time: two would contend for the shared
	// database's locks
	copying sync.Mutex
}

// OpenMigratedSQLite opens a SQLite store at dsn, ":memory:" or the path of
// a file that does not exist yet, holding a copy of a database migrated
// once per process. It exists for test suites, which open hundreds of
// stores: running every migration takes most of a second under the race
// detector, and a package's tests run one after another within the time
// go test allows it.
func OpenMigratedSQLite(dsn string) (*Store, error) {
	migrated.once.Do(func() {
		migrated.template, migrated.err = Open(&config.DatabaseConfig{Type: "sqlite", DSN: migratedURI})
		if migrated.err == nil {
			migrated.err = migrated.template.Migrate()
		}
	})
	if migrated.err != nil {
		return nil, fmt.Errorf("migrating the database copied from: %w", migrated.err)
	}

	st, err := Open(&config.DatabaseConfig{Type: "sqlite", DSN: dsn})
	if err != nil {
		return nil, err
	}
	if err := restoreMigrated(st); err != nil {
		_ = st.Close()
		return nil, fmt.Errorf("copying in the migrated database: %w", err)
	}
	return st, nil
}

// restoreMigrated replaces the store's database with the migrated one, on
// the pool's one connection, which an in-memory database lives in.
func restoreMigrated(st *Store) error {
	migrated.copying.Lock()
	defer migrated.copying.Unlock()
	conn, err := st.DB.Conn(context.Background())
	if err != nil {
		return err
	}
	defer conn.Close()
	return conn.Raw(func(dc any) error {
		c, ok := dc.(interface {
			NewRestore(srcURI string) (*sqlite.Backup, error)
		})
		if !ok {
			return fmt.Errorf("the SQLite driver cannot copy a database (%T)", dc)
		}
		restore, err := c.NewRestore(migratedURI)
		if err != nil {
			return err
		}
		for more := true; more && err == nil; {
			more, err = restore.Step(-1)
		}
		if finished := restore.Finish(); err == nil {
			err = finished
		}
		return err
	})
}
