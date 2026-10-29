package archive

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// Replacing an instance in use (#142, README §2.3) with a backup of itself:
// each table of the kinds chosen ends as the archive has it. Rows are
// matched by key, so what still names them (in kinds not chosen) is left
// alone: those the archive has are updated or added, and those it lacks
// are removed, taking with them what named only them. People are never
// removed: the archive's users and roles are added and updated, and the
// instance's others kept, with their roles.

// seenTable is the temporary table holding the keys of a table's rows the
// archive has.
func seenTable(t *Table) string { return "bam_seen_" + t.Name }

// storeKey is a table's key as the database names its columns.
func storeKey(t *Table) []string {
	out := make([]string, len(t.Key))
	for i, k := range t.Key {
		if t.ByPermissionKey && k == "permission_key" {
			k = "permission_id"
		}
		out[i] = k
	}
	return out
}

// dropTemporary drops a temporary table, if there is one.
func dropTemporary(ctx context.Context, tx *sql.Tx, dialect, name string) error {
	q := "DROP TABLE IF EXISTS " + name
	if dialect == "mariadb" || dialect == "mysql" {
		q = "DROP TEMPORARY TABLE IF EXISTS " + name
	}
	_, err := tx.ExecContext(ctx, q)
	return err
}

// makeSeen makes the temporary tables of keys seen, one for each table
// being replaced, with the key's columns as the table has them (their
// collation too, which MariaDB compares by).
func makeSeen(ctx context.Context, tx *sql.Tx, dialect string, tables []*Table) error {
	for _, t := range tables {
		name := seenTable(t)
		if err := dropTemporary(ctx, tx, dialect, name); err != nil {
			return err
		}
		key := strings.Join(storeKey(t), ", ")
		if dialect == "mariadb" || dialect == "mysql" {
			// CREATE INDEX commits the transaction there, even on a
			// temporary table; an index made with the table does not.
			if _, err := tx.ExecContext(ctx, "CREATE TEMPORARY TABLE "+name+" (INDEX "+name+"_key ("+key+")) AS SELECT "+key+" FROM "+t.Name+" WHERE 1 = 0"); err != nil {
				return fmt.Errorf("preparing to replace %s: %w", t.Name, err)
			}
			continue
		}
		if _, err := tx.ExecContext(ctx, "CREATE TEMPORARY TABLE "+name+" AS SELECT "+key+" FROM "+t.Name+" WHERE 1 = 0"); err != nil {
			return fmt.Errorf("preparing to replace %s: %w", t.Name, err)
		}
		if _, err := tx.ExecContext(ctx, "CREATE INDEX "+name+"_key ON "+name+" ("+key+")"); err != nil {
			return fmt.Errorf("preparing to replace %s: %w", t.Name, err)
		}
	}
	return nil
}

func dropSeen(ctx context.Context, tx *sql.Tx, dialect string, tables []*Table) error {
	for _, t := range tables {
		if err := dropTemporary(ctx, tx, dialect, seenTable(t)); err != nil {
			return err
		}
	}
	return nil
}

// keyString is a row's key as one string, to compare.
func keyString(vals []any) string {
	var b strings.Builder
	for _, v := range vals {
		fmt.Fprint(&b, v)
		b.WriteByte(0)
	}
	return b.String()
}

// keyIn is "(k1, k2) IN ((?, ?), ...)" for n keys, or "k IN (?, ...)".
func keyIn(cols []string, n int) string {
	one := strings.TrimSuffix(strings.Repeat("?, ", len(cols)), ", ")
	if len(cols) > 1 {
		one = "(" + one + ")"
	}
	list := strings.TrimSuffix(strings.Repeat(one+", ", n), ", ")
	lhs := cols[0]
	if len(cols) > 1 {
		lhs = "(" + strings.Join(cols, ", ") + ")"
	}
	return lhs + " IN (" + list + ")"
}

// upsert updates the rows of a batch the table has, by key, adds the rest,
// and notes every key as seen.
func (in *inserter) upsert(t *Table, cols []Column, batch [][]any) error {
	key := storeKey(t)
	at := make([]int, len(t.Key))
	for i, k := range t.Key {
		at[i] = colIndexIn(cols, k)
	}
	keyOf := func(row []any) []any {
		out := make([]any, len(at))
		for i, j := range at {
			out[i] = row[j]
		}
		return out
	}

	// Which the table has already.
	var args []any
	for _, row := range batch {
		args = append(args, keyOf(row)...)
	}
	rows, err := in.tx.QueryContext(in.ctx, in.st.Rebind("SELECT "+strings.Join(key, ", ")+" FROM "+t.Name+" WHERE "+keyIn(key, len(batch))), args...)
	if err != nil {
		return err
	}
	have := map[string]bool{}
	for rows.Next() {
		vals := make([]string, len(key))
		ptrs := make([]any, len(key))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			rows.Close()
			return err
		}
		k := make([]any, len(vals))
		for i, v := range vals {
			k[i] = v
		}
		have[keyString(k)] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	var set []string
	var setAt []int
	for i, c := range cols {
		if !hasString(t.Key, c.Name) {
			set = append(set, storeName(t, c.Name)+" = ?")
			setAt = append(setAt, i)
		}
	}
	where := make([]string, len(key))
	for i, k := range key {
		where[i] = k + " = ?"
	}
	update := in.st.Rebind("UPDATE " + t.Name + " SET " + strings.Join(set, ", ") + " WHERE " + strings.Join(where, " AND "))
	var adding [][]any
	for _, row := range batch {
		if !have[keyString(keyOf(row))] {
			adding = append(adding, row)
			continue
		}
		if len(set) == 0 {
			continue
		}
		args := make([]any, 0, len(setAt)+len(at))
		for _, i := range setAt {
			args = append(args, row[i])
		}
		if _, err := in.tx.ExecContext(in.ctx, update, append(args, keyOf(row)...)...); err != nil {
			return fmt.Errorf("%w (a row of the archive clashes with one here: replace with a backup of this instance)", err)
		}
	}
	if len(adding) > 0 {
		if err := in.insert(t, cols, adding); err != nil {
			return err
		}
	}

	one := "(" + strings.TrimSuffix(strings.Repeat("?, ", len(key)), ", ") + ")"
	seen := "INSERT INTO " + seenTable(t) + " (" + strings.Join(key, ", ") + ") VALUES " + strings.TrimSuffix(strings.Repeat(one+", ", len(batch)), ", ")
	_, err = in.tx.ExecContext(in.ctx, in.st.Rebind(seen), args...)
	return err
}

// storeName is a column's name in the database.
func storeName(t *Table, col string) string {
	if t.ByPermissionKey && col == "permission_key" {
		return "permission_id"
	}
	return col
}

// peopleScope keeps people's rows the archive does not name: a user's
// roles, or a role's grants, are replaced only for those it has.
var peopleScope = map[string]struct{ col, of string }{
	"user_roles":             {"user_id", "users"},
	"user_role_scopes":       {"user_id", "users"},
	"role_permissions":       {"role_id", "roles"},
	"role_permission_scopes": {"role_id", "roles"},
}

// removeMissing removes the rows of the tables replaced that the archive
// lacks, last table first, and counts them.
func removeMissing(ctx context.Context, tx *sql.Tx, tables []*Table) (map[string]int, error) {
	removed := map[string]int{}
	for i := len(tables) - 1; i >= 0; i-- {
		t := tables[i]
		if t.Name == "users" || t.Name == "roles" {
			continue
		}
		var match []string
		for _, k := range storeKey(t) {
			match = append(match, "s."+k+" = "+t.Name+"."+k)
		}
		q := "DELETE FROM " + t.Name + " WHERE NOT EXISTS (SELECT 1 FROM " + seenTable(t) + " s WHERE " + strings.Join(match, " AND ") + ")"
		if scope, ok := peopleScope[t.Name]; ok {
			q += " AND " + scope.col + " IN (SELECT id FROM bam_seen_" + scope.of + ")"
		}
		res, err := tx.ExecContext(ctx, q)
		if err != nil {
			return nil, fmt.Errorf("removing %s the archive does not have: %w", t.Name, err)
		}
		if n, _ := res.RowsAffected(); n > 0 {
			removed[t.Name] = int(n)
		}
	}
	return removed, nil
}
