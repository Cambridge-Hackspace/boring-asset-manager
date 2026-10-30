package archive

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/filestore"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store/migrations"
)

// ErrNotEmpty is returned for a restore into an instance already in use.
var ErrNotEmpty = errors.New("archive: the instance is in use")

// ErrNotBackup is returned for restoring, or replacing with, a package.
var ErrNotBackup = errors.New("archive: not a backup")

// ErrLockout is returned for a replace that would leave no one able to
// manage users, or the person replacing unable to.
var ErrLockout = errors.New("archive: refused, as it would lock people out")

// RestoreOptions are what Restore restores.
type RestoreOptions struct {
	// Kinds are the kinds of information to restore, of those the archive
	// holds; nil for all of them.
	Kinds []Kind
	// Secrets lets authentication be restored: only from the CLI. Without
	// it, an archive's authentication is skipped, with a warning.
	Secrets bool
	// Replace replaces the kinds chosen in an instance in use (replace.go),
	// with a backup of it at this schema, rather than restoring into one not
	// yet in use.
	Replace bool
	// Keep, with Replace, is a user who must still be active, and able to
	// manage users, after: the person replacing.
	Keep string
	// Wipe drops every table of the database once the archive checks out,
	// and restores into it empty: an instance put back as it was, whatever
	// was done to it since (README §2.2).
	Wipe bool
}

// RestoreSummary is what Restore did.
type RestoreSummary struct {
	Header Header `json:"header"`
	// Kinds are the kinds restored.
	Kinds []Kind `json:"kinds"`
	// Tables counts the rows restored into each table.
	Tables map[string]int `json:"tables"`
	// Files counts the files put in the file store, and their bytes.
	Files     int   `json:"files"`
	FileBytes int64 `json:"file_bytes"`
	// Already counts a later part's files the store already had, and
	// Unnamed those no row names, which are not stored.
	Already int `json:"already"`
	Unnamed int `json:"unnamed"`
	// Migrated is set when the schema was migrated after the rows went in.
	Migrated bool `json:"migrated"`
	// Removed counts, with Replace, the rows of each table replaced that
	// the archive did not have, and Lost the rows of tables not replaced
	// that went with them, as they named only those.
	Removed map[string]int `json:"removed"`
	Lost    map[string]int `json:"lost"`
	// Warnings say what was left out, and why.
	Warnings []string `json:"warnings"`
}

// placeholders are the values secret columns that may not be null take
// when their secrets are not restored: a password hash nothing matches,
// and a channel's options empty.
var placeholders = map[string]any{
	"users.password_hash":           "!",
	"notification_channels.options": "{}",
}

// selfRefs are columns naming a row of their own table, which a row may
// name before it is inserted: set once every row of it is.
var selfRefs = map[string]string{"categories": "parent_id"}

// fileKinds are the kinds whose files are kept under each prefix.
var fileKinds = map[string]Kind{"photos/": KindAssets, "images/": KindLabels}

// Restore restores an archive into st and blobs, reading it twice from
// open: once to check it (Check), and nothing is changed if it does not
// check out, then to apply it.
//
// The first part of an archive restores its rows, into an instance not
// yet in use (no users and no assets): every row of each kind restored
// is replaced, in one transaction, with every ID kept. An archive from an
// older schema is restored into a database at that schema (an empty one,
// which is migrated to it first) and migrated after. Files are put in the
// store as they are read, before the rows naming them are committed.
//
// A later part of a set puts its files in the store, those that the rows
// name and the store does not yet have.
func Restore(ctx context.Context, st *store.Store, blobs filestore.Blobs, open func() (io.ReadCloser, error), opts RestoreOptions) (*RestoreSummary, error) {
	rc, err := open()
	if err != nil {
		return nil, err
	}
	checked, err := Check(rc)
	rc.Close()
	if err != nil {
		return nil, err
	}
	sum := &RestoreSummary{Header: checked.Header, Tables: map[string]int{}}
	if checked.Header.Type == "package" {
		// part of an instance, merged into one rather than put back
		return nil, fmt.Errorf("%w: it is a package, part of an instance, which is imported into one rather than restored", ErrNotBackup)
	}
	if checked.Header.Set.Part > 1 {
		return sum, restoreFiles(ctx, st, blobs, open, sum)
	}

	for _, k := range checked.Header.Kinds {
		if opts.Kinds != nil && !hasKind(opts.Kinds, k) {
			continue
		}
		if k == KindAuth && !opts.Secrets {
			sum.Warnings = append(sum.Warnings, "authentication is not restored here: only bam restore, from the command line, restores it")
			continue
		}
		if k == KindAuth && opts.Replace && !hasKind(opts.Kinds, KindAuth) {
			// Replacing it signs people out, so it is only done when chosen.
			sum.Warnings = append(sum.Warnings, "authentication is kept as it is: name auth among the kinds to replace it")
			continue
		}
		sum.Kinds = append(sum.Kinds, k)
	}
	for _, k := range opts.Kinds {
		if !hasKind(checked.Header.Kinds, k) {
			return sum, fmt.Errorf("the archive holds no %s", k)
		}
	}
	if hasKind(sum.Kinds, KindPeople) && !hasKind(sum.Kinds, KindAuth) {
		sum.Warnings = append(sum.Warnings, "people are restored without their passwords: set one with bam admin password")
	}

	if opts.Wipe {
		if opts.Replace || opts.Kinds != nil {
			return sum, errors.New("a restore that wipes the database restores everything, replacing nothing")
		}
		if err := st.DropAllTables(ctx); err != nil {
			return sum, fmt.Errorf("emptying the database: %w", err)
		}
	}
	version := checked.Header.SchemaVersion
	current, err := schemaVersion(st)
	if err != nil {
		return sum, err
	}
	if opts.Replace && current != version {
		return sum, fmt.Errorf("%w: the archive's schema is at version %d and this instance's at %d; replace with a backup made by this version of bam, or restore it into a new database", ErrSchema, version, current)
	}
	if current > version {
		return sum, fmt.Errorf("%w: the archive's schema is at version %d and the database's at %d; restore it into an empty database, which is migrated to its version first", ErrSchema, version, current)
	}
	if current > 0 && !opts.Replace {
		inUse, err := inUse(st)
		if err != nil {
			return sum, err
		}
		if inUse != "" {
			return sum, fmt.Errorf("%w: it has %s; restore into a new database", ErrNotEmpty, inUse)
		}
	}
	if current < version {
		if err := migrations.MigrateTo(st.DB, st.Dialect, version); err != nil {
			return sum, fmt.Errorf("migrating the database to the archive's schema: %w", err)
		}
	}
	if current == 0 {
		// Grants are restored by permission key, so the permissions must
		// be there first, as bam migrate leaves them.
		if err := st.SeedDefaultPermissionsAndRoles(); err != nil {
			return sum, err
		}
	}

	rc, err = open()
	if err != nil {
		return sum, err
	}
	defer rc.Close()
	if err := applyRows(ctx, st, blobs, rc, sum, opts); err != nil {
		return sum, err
	}
	if version < migrations.Latest() || current == 0 {
		if err := st.Migrate(); err != nil {
			return sum, fmt.Errorf("migrating the restored database: %w", err)
		}
		if err := st.SeedDefaultPermissionsAndRoles(); err != nil {
			return sum, err
		}
		sum.Migrated = version < migrations.Latest()
	}
	return sum, nil
}

// schemaVersion is the database's schema version; 0 when it has none.
func schemaVersion(st *store.Store) (int, error) {
	var v sql.NullInt64
	if err := st.QueryRow("SELECT MAX(version) FROM schema_migrations").Scan(&v); err != nil {
		// No table: an empty database.
		var n int
		if st.QueryRow("SELECT COUNT(*) FROM users").Scan(&n) == nil {
			return 0, fmt.Errorf("reading the schema's version: %w", err)
		}
		return 0, nil
	}
	return int(v.Int64), nil
}

// inUse says what an instance has that a restore would replace; "" when
// nothing.
func inUse(st *store.Store) (string, error) {
	for _, t := range []struct{ table, what string }{{"users", "users"}, {"assets", "assets"}} {
		var n int
		if err := st.QueryRow("SELECT COUNT(*) FROM " + t.table).Scan(&n); err != nil {
			return "", err
		}
		if n > 0 {
			return fmt.Sprintf("%d %s", n, t.what), nil
		}
	}
	return "", nil
}

// applyRows replaces the rows of the kinds chosen with the archive's, in
// one transaction, putting its files in the store as it goes.
func applyRows(ctx context.Context, st *store.Store, blobs filestore.Blobs, r io.Reader, sum *RestoreSummary, opts RestoreOptions) error {
	m, err := openMembers(bufio.NewReader(r))
	if err != nil {
		return err
	}
	// Listed first: SQLite's one connection is the transaction's after.
	exist, err := existingTables(ctx, st)
	if err != nil {
		return err
	}
	tx, err := st.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // after Commit, a no-op

	kept, err := countKept(ctx, tx, sum.Kinds, exist)
	if err != nil {
		return err
	}
	var replacing []*Table
	for i := range Tables {
		if t := &Tables[i]; hasKind(sum.Kinds, t.Kind) && exist[t.Name] {
			replacing = append(replacing, t)
		}
	}
	adminsBefore := 0
	if opts.Replace {
		if adminsBefore, err = st.ActiveAdminsTx(tx); err != nil {
			return err
		}
		if err := makeSeen(ctx, tx, st.Dialect, replacing); err != nil {
			return err
		}
	}
	// Restoring into an instance not in use, its kinds are emptied first.
	for i := len(Tables) - 1; i >= 0 && !opts.Replace; i-- {
		t := &Tables[i]
		if !hasKind(sum.Kinds, t.Kind) || !exist[t.Name] {
			continue
		}
		if col, ok := selfRefs[t.Name]; ok {
			if _, err := tx.ExecContext(ctx, "UPDATE "+t.Name+" SET "+col+" = NULL"); err != nil {
				return fmt.Errorf("emptying %s: %w", t.Name, err)
			}
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM "+t.Name); err != nil {
			return fmt.Errorf("emptying %s: %w", t.Name, err)
		}
	}
	perms, err := permissionIDs(ctx, st, tx)
	if err != nil {
		return err
	}

	ins := &inserter{ctx: ctx, st: st, tx: tx, perms: perms, parents: map[string][]parentRef{}, replace: opts.Replace}
	for {
		h, err := m.next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		dir, rest, _ := strings.Cut(h.Name, "/")
		switch dir + "/" {
		case DataDir, AuthDir:
			table, _, _ := strings.Cut(rest, "/")
			t := tableByName(table)
			if !hasKind(sum.Kinds, t.Kind) || dir+"/" == AuthDir && !hasKind(sum.Kinds, KindAuth) {
				continue
			}
			secrets := dir+"/" == AuthDir && t.Kind != KindAuth
			n, err := ins.member(t, m.tr, secrets)
			if err != nil {
				return fmt.Errorf("restoring %s: %w", table, err)
			}
			if !secrets {
				sum.Tables[table] += n
			}
		case FilesDir:
			if k := fileKinds[rest[:strings.Index(rest, "/")+1]]; !hasKind(sum.Kinds, k) {
				continue
			}
			if err := blobs.Put(ctx, rest, m.tr, h.Size, "application/octet-stream"); err != nil {
				return fmt.Errorf("storing %s: %w", rest, err)
			}
			sum.Files++
			sum.FileBytes += h.Size
		}
	}
	if err := ins.setParents(); err != nil {
		return err
	}
	if !opts.Replace {
		if err := checkKept(ctx, tx, sum.Kinds, exist, kept); err != nil {
			return err
		}
		return tx.Commit()
	}

	if sum.Removed, err = removeMissing(ctx, tx, replacing); err != nil {
		return err
	}
	if err := dropSeen(ctx, tx, st.Dialect, replacing); err != nil {
		return err
	}
	now, err := countKept(ctx, tx, sum.Kinds, exist)
	if err != nil {
		return err
	}
	sum.Lost = map[string]int{}
	for name, n := range kept {
		if now[name] < n {
			sum.Lost[name] = n - now[name]
		}
	}
	if after, err := st.ActiveAdminsTx(tx); err != nil {
		return err
	} else if adminsBefore > 0 && after == 0 {
		return fmt.Errorf("%w: it would leave no one active who can manage users", ErrLockout)
	}
	if opts.Keep != "" {
		if ok, err := st.CanManageUsersTx(tx, opts.Keep); err != nil {
			return err
		} else if !ok {
			return fmt.Errorf("%w: the backup would leave you unable to sign in or manage users", ErrLockout)
		}
	}
	return tx.Commit()
}

// countKept counts the rows of the tables not being restored, which a
// restore must leave as they are.
func countKept(ctx context.Context, tx *sql.Tx, kinds []Kind, exist map[string]bool) (map[string]int, error) {
	out := map[string]int{}
	for _, t := range Tables {
		if hasKind(kinds, t.Kind) || !exist[t.Name] {
			continue
		}
		var n int
		if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+t.Name).Scan(&n); err != nil {
			return nil, err
		}
		out[t.Name] = n
	}
	return out, nil
}

// checkKept refuses a restore that changed a table not being restored,
// as one whose rows named those being replaced.
func checkKept(ctx context.Context, tx *sql.Tx, kinds []Kind, exist map[string]bool, kept map[string]int) error {
	now, err := countKept(ctx, tx, kinds, exist)
	if err != nil {
		return err
	}
	var changed []string
	for name, n := range kept {
		if now[name] != n {
			changed = append(changed, name)
		}
	}
	if len(changed) > 0 {
		sort.Strings(changed)
		return fmt.Errorf("restoring these kinds would change %s, which is not being restored; restore its kind too", strings.Join(changed, ", "))
	}
	return nil
}

// permissionIDs maps each permission's key to its ID on this instance.
func permissionIDs(ctx context.Context, st *store.Store, tx *sql.Tx) (map[string]string, error) {
	rows, err := tx.QueryContext(ctx, "SELECT permission_key, id FROM permissions")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var key, id string
		if err := rows.Scan(&key, &id); err != nil {
			return nil, err
		}
		out[key] = id
	}
	return out, rows.Err()
}

type parentRef struct{ id, parent string }

// inserter inserts an archive's rows in batches.
type inserter struct {
	ctx     context.Context
	st      *store.Store
	tx      *sql.Tx
	perms   map[string]string
	parents map[string][]parentRef
	// replace upserts rows (replace.go), rather than adding them to tables
	// emptied first.
	replace bool
}

// batchArgs is the most values one INSERT binds: within every dialect's
// limit.
const batchArgs = 4000

// member inserts a member's rows, or with secrets, sets the secret columns
// of rows already inserted. It returns how many rows it read.
func (in *inserter) member(t *Table, r io.Reader, secrets bool) (int, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), RowChunk+1)
	var cols []Column
	var batch [][]any
	n := 0
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		var err error
		if in.replace {
			err = in.upsert(t, cols, batch)
		} else {
			err = in.insert(t, cols, batch)
		}
		batch = batch[:0]
		return err
	}
	for sc.Scan() {
		n++
		var row map[string]json.RawMessage
		if err := json.Unmarshal(sc.Bytes(), &row); err != nil {
			return n, fmt.Errorf("row %d: %w", n, err)
		}
		if cols == nil {
			var err error
			if cols, err = rowColumns(t, row, secrets); err != nil {
				return n, err
			}
		}
		if len(row) != len(cols) {
			return n, fmt.Errorf("row %d has other columns than the first", n)
		}
		vals := make([]any, 0, len(cols))
		for _, c := range cols {
			raw, ok := row[c.Name]
			if !ok {
				return n, fmt.Errorf("row %d has no %s", n, c.Name)
			}
			v, err := decodeValue(c, raw)
			if err != nil {
				return n, fmt.Errorf("row %d, %s: %w", n, c.Name, err)
			}
			vals = append(vals, v)
		}
		if secrets {
			if err := in.setSecrets(t, cols, vals); err != nil {
				return n, err
			}
			continue
		}
		if skip := in.adapt(t, cols, vals); skip {
			continue
		}
		batch = append(batch, vals)
		if (len(batch)+1)*len(vals) > batchArgs {
			if err := flush(); err != nil {
				return n, err
			}
		}
	}
	if err := sc.Err(); err != nil {
		return n, err
	}
	return n, flush()
}

// rowColumns are the columns a member's rows have, in the registry's
// order: an archive from an older schema has fewer.
func rowColumns(t *Table, row map[string]json.RawMessage, secrets bool) ([]Column, error) {
	var cols []Column
	for _, c := range t.Columns {
		if _, ok := row[c.Name]; ok {
			cols = append(cols, c)
		}
	}
	if len(cols) != len(row) {
		for name := range row {
			if colIndexOK(t, name) < 0 {
				return nil, fmt.Errorf("it has a column, %s, that this bam does not know", name)
			}
		}
	}
	for _, k := range t.Key {
		if _, ok := row[k]; !ok {
			return nil, fmt.Errorf("its rows have no %s", k)
		}
	}
	if secrets {
		for _, c := range cols {
			if !t.secret(c.Name) && !hasString(t.Key, c.Name) {
				return nil, fmt.Errorf("its secrets hold %s, which is not secret", c.Name)
			}
		}
	}
	return cols, nil
}

func colIndexOK(t *Table, name string) int {
	for i, c := range t.Columns {
		if c.Name == name {
			return i
		}
	}
	return -1
}

// adapt turns a row as archived into a row as stored: a permission's key
// into this instance's ID for it, a secret column's placeholder in place
// of a secret not restored, and a row's parent left to be set after. It
// reports a row to skip: a grant of a permission this bam does not have.
func (in *inserter) adapt(t *Table, cols []Column, vals []any) bool {
	for i, c := range cols {
		if t.ByPermissionKey && c.Name == "permission_key" {
			key, _ := vals[i].(string)
			id, ok := in.perms[key]
			if !ok {
				return true
			}
			vals[i] = id
		}
		if col, ok := selfRefs[t.Name]; ok && c.Name == col && vals[i] != nil {
			in.parents[t.Name] = append(in.parents[t.Name], parentRef{id: fmt.Sprint(vals[colIndexIn(cols, t.Key[0])]), parent: vals[i].(string)})
			vals[i] = nil
		}
	}
	return false
}

func colIndexIn(cols []Column, name string) int {
	for i, c := range cols {
		if c.Name == name {
			return i
		}
	}
	return -1
}

// insert inserts a batch of rows.
func (in *inserter) insert(t *Table, cols []Column, batch [][]any) error {
	names := make([]string, 0, len(cols)+len(t.Secret))
	for _, c := range cols {
		name := c.Name
		if t.ByPermissionKey && name == "permission_key" {
			name = "permission_id"
		}
		names = append(names, name)
	}
	// Secret columns not in the rows take their placeholders.
	var extra []any
	for _, s := range t.Secret {
		if colIndexIn(cols, s) < 0 {
			if v, ok := placeholders[t.Name+"."+s]; ok {
				names = append(names, s)
				extra = append(extra, v)
			}
		}
	}
	one := "(" + strings.TrimSuffix(strings.Repeat("?, ", len(names)), ", ") + ")"
	var q strings.Builder
	q.WriteString("INSERT INTO " + t.Name + " (" + strings.Join(names, ", ") + ") VALUES ")
	args := make([]any, 0, len(batch)*len(names))
	for i, row := range batch {
		if i > 0 {
			q.WriteString(", ")
		}
		q.WriteString(one)
		args = append(append(args, row...), extra...)
	}
	if _, err := in.tx.ExecContext(in.ctx, in.st.Rebind(q.String()), args...); err != nil {
		return fmt.Errorf("%w (its rows clash with what this instance has, as a backup of another instance's would, or name what it does not have: restore that kind too)", err)
	}
	return nil
}

// setSecrets sets a row's secret columns, by its key.
func (in *inserter) setSecrets(t *Table, cols []Column, vals []any) error {
	var set, where []string
	var setArgs, whereArgs []any
	for i, c := range cols {
		if hasString(t.Key, c.Name) {
			where = append(where, c.Name+" = ?")
			whereArgs = append(whereArgs, vals[i])
		} else {
			set = append(set, c.Name+" = ?")
			setArgs = append(setArgs, vals[i])
		}
	}
	if len(set) == 0 {
		return nil
	}
	res, err := in.tx.ExecContext(in.ctx, in.st.Rebind("UPDATE "+t.Name+" SET "+strings.Join(set, ", ")+" WHERE "+strings.Join(where, " AND ")), append(setArgs, whereArgs...)...)
	if err != nil {
		return err
	}
	// MariaDB counts the rows changed, not those found, so a secret the
	// same as its placeholder would count none there.
	if n, _ := res.RowsAffected(); n != 1 && in.st.Dialect != "mariadb" && in.st.Dialect != "mysql" {
		return fmt.Errorf("secrets for a row that is not there: %v", whereArgs)
	}
	return nil
}

// setParents sets the parents of rows that name another of their table.
func (in *inserter) setParents() error {
	for table, refs := range in.parents {
		col := selfRefs[table]
		for _, ref := range refs {
			if _, err := in.tx.ExecContext(in.ctx, in.st.Rebind("UPDATE "+table+" SET "+col+" = ? WHERE id = ?"), ref.parent, ref.id); err != nil {
				return fmt.Errorf("restoring %s's %s: %w", table, col, err)
			}
		}
	}
	return nil
}

// decodeValue reads a value as Write wrote it.
func decodeValue(c Column, raw json.RawMessage) (any, error) {
	if string(raw) == "null" {
		return nil, nil
	}
	switch c.Type {
	case Text:
		var s string
		err := json.Unmarshal(raw, &s)
		return s, err
	case Int:
		var n int64
		err := json.Unmarshal(raw, &n)
		return n, err
	case Float:
		var f float64
		err := json.Unmarshal(raw, &f)
		return f, err
	case Bool:
		var b bool
		err := json.Unmarshal(raw, &b)
		return b, err
	case Time:
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return nil, err
		}
		t, err := time.Parse(time.RFC3339Nano, s)
		if err != nil {
			return nil, err
		}
		return t.UTC(), nil
	case JSON:
		var buf bytes.Buffer
		if err := json.Compact(&buf, raw); err != nil {
			return nil, err
		}
		return buf.String(), nil
	}
	return nil, fmt.Errorf("a column of type %v", c.Type)
}

// restoreFiles applies a later part of a set: its files that the rows
// name and the store does not have.
func restoreFiles(ctx context.Context, st *store.Store, blobs filestore.Blobs, open func() (io.ReadCloser, error), sum *RestoreSummary) error {
	named, err := st.StoredFiles()
	if err != nil {
		return err
	}
	rc, err := open()
	if err != nil {
		return err
	}
	defer rc.Close()
	m, err := openMembers(bufio.NewReader(rc))
	if err != nil {
		return err
	}
	for {
		h, err := m.next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		key, ok := strings.CutPrefix(h.Name, FilesDir)
		if !ok {
			continue
		}
		if !named[key] {
			sum.Unnamed++
			continue
		}
		if existing, err := blobs.Get(ctx, key); err == nil {
			existing.Close()
			sum.Already++
			continue
		} else if !errors.Is(err, filestore.ErrNotFound) {
			return err
		}
		if err := blobs.Put(ctx, key, m.tr, h.Size, "application/octet-stream"); err != nil {
			return fmt.Errorf("storing %s: %w", key, err)
		}
		sum.Files++
		sum.FileBytes += h.Size
	}
	if sum.Files == 0 && sum.Already == 0 && sum.Unnamed > 0 {
		return fmt.Errorf("none of part %d's files is named by a row here: it is of another set, or its first part has not been restored", sum.Header.Set.Part)
	}
	return nil
}

// existingTables are the tables the database has: at an older schema,
// not every one archived.
func existingTables(ctx context.Context, st *store.Store) (map[string]bool, error) {
	q := "SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%'"
	switch st.Dialect {
	case "postgres":
		q = "SELECT table_name FROM information_schema.tables WHERE table_schema = current_schema() AND table_type = 'BASE TABLE'"
	case "mariadb", "mysql":
		q = "SELECT table_name FROM information_schema.tables WHERE table_schema = DATABASE() AND table_type = 'BASE TABLE'"
	}
	rows, err := st.DB.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("listing tables: %w", err)
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out[name] = true
	}
	return out, rows.Err()
}
