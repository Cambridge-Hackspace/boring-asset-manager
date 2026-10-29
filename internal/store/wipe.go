package store

import (
	"context"
	"fmt"
)

// DropAllTables drops every table of the database, bam's and any other,
// leaving it empty: for putting an instance back from an archive as it was
// (bam restore --wipe, which bam-update uses when an update fails, README
// §2.2). It cannot be undone.
func (s *Store) DropAllTables(ctx context.Context) error {
	conn, err := s.DB.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	var list string
	switch s.Dialect {
	case "postgres":
		list = "SELECT table_name FROM information_schema.tables WHERE table_schema = current_schema() AND table_type = 'BASE TABLE'"
	case "mariadb", "mysql":
		list = "SELECT table_name FROM information_schema.tables WHERE table_schema = DATABASE() AND table_type = 'BASE TABLE'"
	default:
		list = "SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%'"
	}
	rows, err := conn.QueryContext(ctx, list)
	if err != nil {
		return fmt.Errorf("listing tables: %w", err)
	}
	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return err
		}
		tables = append(tables, name)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	// Each table goes whatever names it.
	before, after, drop := "", "", "DROP TABLE IF EXISTS %s"
	switch s.Dialect {
	case "postgres":
		drop = `DROP TABLE IF EXISTS "%s" CASCADE`
	case "mariadb", "mysql":
		before, after, drop = "SET FOREIGN_KEY_CHECKS = 0", "SET FOREIGN_KEY_CHECKS = 1", "DROP TABLE IF EXISTS `%s`"
	default:
		before, after, drop = "PRAGMA foreign_keys = OFF", "PRAGMA foreign_keys = ON", `DROP TABLE IF EXISTS "%s"`
	}
	if before != "" {
		if _, err := conn.ExecContext(ctx, before); err != nil {
			return err
		}
		defer conn.ExecContext(context.WithoutCancel(ctx), after) //nolint:errcheck // the connection's own setting, restored
	}
	for _, t := range tables {
		if _, err := conn.ExecContext(ctx, fmt.Sprintf(drop, t)); err != nil {
			return fmt.Errorf("dropping %s: %w", t, err)
		}
	}
	return nil
}
