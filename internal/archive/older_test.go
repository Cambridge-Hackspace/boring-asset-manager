package archive

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/filestore"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store/migrations"
)

// TestEveryTableSaysSince: each table's Since is the migration from which
// every schema has had it, as bam's migrations make them one at a time,
// and each retired table's runs until it was retired. A table a migration
// adds is in the registry with its Since only once tools/archive/tables.py
// has been run again.
func TestEveryTableSaysSince(t *testing.T) {
	st := emptyStore(t, "sqlite")
	latest := migrations.Latest()
	present := map[string][]bool{}
	for v := 1; v <= latest; v++ {
		if err := migrations.MigrateTo(st.DB, st.Dialect, v); err != nil {
			t.Fatal(err)
		}
		for _, name := range dbTables(t, st) {
			if present[name] == nil {
				present[name] = make([]bool, latest+1)
			}
			present[name][v] = true
		}
	}
	for _, table := range tablesAt(0) {
		until := latest + 1
		if r := retiredTable(table.Name); r != nil {
			until = r.Until
		}
		// The migration from which it has been in every schema until
		// retired: 0 when it never has.
		since := 0
		for v := until - 1; v >= 1 && present[table.Name] != nil && present[table.Name][v]; v-- {
			since = v
		}
		if table.Since != since {
			t.Errorf("%s has been in every schema since migration %d, and the registry says %d: run tools/archive/tables.py", table.Name, since, table.Since)
		}
	}
}

var (
	schemasMu sync.Mutex
	schemas   = map[int]map[string]map[string]bool{}
)

// schemaAt is each table's columns at schema version v, as bam's
// migrations make them on SQLite.
func schemaAt(t *testing.T, v int) map[string]map[string]bool {
	t.Helper()
	schemasMu.Lock()
	defer schemasMu.Unlock()
	if s, ok := schemas[v]; ok {
		return s
	}
	st := emptyStore(t, "sqlite")
	if err := migrations.MigrateTo(st.DB, st.Dialect, v); err != nil {
		t.Fatal(err)
	}
	s := map[string]map[string]bool{}
	for _, name := range dbTables(t, st) {
		rows, err := st.Query("SELECT * FROM " + name + " WHERE 1 = 0")
		if err != nil {
			t.Fatal(err)
		}
		cols, _ := rows.Columns()
		rows.Close()
		s[name] = map[string]bool{}
		for _, c := range cols {
			s[name][c] = true
		}
	}
	schemas[v] = s
	return s
}

// olderArchive rewrites an archive written now as one written at schema
// version v, as nearly as rows written now allow: without the tables and
// columns its schema did not have yet, and, before attachments (#220),
// with assets' images as asset_photos and no other attachments. Each
// table's rows are counted again for the manifest.
func olderArchive(t *testing.T, b []byte, v int) []byte {
	t.Helper()
	schema := schemaAt(t, v)
	var out []member
	rows := map[string]int{}
	for _, m := range unpack(t, b) {
		dir, rest, _ := strings.Cut(m.name, "/")
		if dir+"/" != DataDir && dir+"/" != AuthDir {
			out = append(out, m)
			continue
		}
		table, file, _ := strings.Cut(rest, "/")
		if r := retiredTable("asset_photos"); table == r.As && v < r.Until {
			table = r.Name
		}
		cols, ok := schema[table]
		if !ok {
			continue
		}
		var buf bytes.Buffer
		for _, line := range strings.Split(string(m.data), "\n") {
			if line == "" {
				continue
			}
			var row map[string]json.RawMessage
			if err := json.Unmarshal([]byte(line), &row); err != nil {
				t.Fatal(err)
			}
			if kind, ok := row["kind"]; table == "asset_photos" && ok && string(kind) != `"image"` {
				continue
			}
			for c := range row {
				if !cols[c] && !(c == "permission_key" && cols["permission_id"]) {
					delete(row, c)
				}
			}
			data, err := json.Marshal(row)
			if err != nil {
				t.Fatal(err)
			}
			buf.Write(append(data, '\n'))
			if dir+"/" == DataDir || tableByName(table).Kind == KindAuth {
				rows[table]++
			}
		}
		if buf.Len() == 0 {
			continue
		}
		m.name = dir + "/" + table + "/" + file
		m.data = buf.Bytes()
		out = append(out, m)
	}
	return pack(t, reseal(t, out, func(h *Header, man *Manifest) {
		h.SchemaVersion = v
		tables := map[string]int{}
		for _, table := range tablesAt(v) {
			if _, ok := schema[table.Name]; ok && hasKind(h.Kinds, table.Kind) {
				tables[table.Name] = rows[table.Name]
			}
		}
		man.Tables = tables
	}))
}

// olderVersions are the schemas an archive may have been written at that
// lack a table this one has: the first archives', and the one before each
// migration that has added a table since.
func olderVersions() []int {
	seen := map[int]bool{FirstSchemaVersion: true}
	for _, table := range Tables {
		if table.Since > FirstSchemaVersion {
			seen[table.Since-1] = true
		}
	}
	var vs []int
	for v := range seen {
		vs = append(vs, v)
	}
	sort.Ints(vs)
	return vs
}

// TestArchivesFromEveryOlderSchema (#288): a backup and a package written
// at the first archives' schema, and at the one before each migration that
// has added a table since, check, restore and import into this schema.
func TestArchivesFromEveryOlderSchema(t *testing.T) {
	ctx := context.Background()
	src := setupStore(t)
	blobs := filestore.NewLocal(t.TempDir())
	s := richSeed(t, src, blobs)
	tools := idOf(t, src, "SELECT id FROM categories WHERE name = 'Tools'")
	bought := idOf(t, src, "SELECT category_id FROM category_vendors")
	// A screen icon with no source yet: before renditions (047), its row
	// has none.
	if _, err := src.Exec("INSERT INTO category_icons (category_id, rendition, icon_name, source, draft, draft_errors, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)",
		tools, "screen", "drill", nil, nil, nil, time.Now()); err != nil {
		t.Fatal(err)
	}
	now := backup(t, src, blobs, Options{Secrets: true})
	pkg := exportPackage(t, src, blobs, Selection{Subtrees: []string{tools, bought}, Assets: &AssetsSelection{Photos: true}})

	for _, v := range olderVersions() {
		t.Run(strconv.Itoa(v), func(t *testing.T) {
			old := olderArchive(t, now, v)
			checked, err := Check(bytes.NewReader(old))
			if err != nil {
				t.Fatalf("checking: %v", err)
			}
			dst := emptyStore(t, envDialect())
			sum, err := Restore(ctx, dst, filestore.NewLocal(t.TempDir()), opener(old), RestoreOptions{Secrets: true})
			if err != nil {
				t.Fatalf("restoring: %v", err)
			}
			if v, _ := schemaVersion(dst); v != migrations.Latest() || !sum.Migrated {
				t.Errorf("schema %d, migrated %v", v, sum.Migrated)
			}
			for table, n := range checked.Manifest.Tables {
				as := tableByName(table)
				if r := retiredTable(table); r != nil {
					as = tableByName(r.As)
				}
				// Grants are restored by permission key, and a key renamed
				// since, which an archive written then would have under its
				// old name, is skipped.
				if as.ByPermissionKey {
					continue
				}
				if got := count(t, dst, as.Name); got < n {
					t.Errorf("%s: %d rows restored of %d", as.Name, got, n)
				}
			}
			if u, err := dst.GetUserByID(s.user.ID); err != nil || u.Email != s.user.Email {
				t.Errorf("the user: %+v %v", u, err)
			}
			if icon, err := dst.GetCategoryIcon(tools, models.RenditionScreen); err != nil || icon.IconName != "drill" {
				t.Errorf("the icon: %+v %v", icon, err)
			}

			into := migratedStore(t, envDialect())
			plan, err := Import(ctx, into, filestore.NewLocal(t.TempDir()), opener(olderArchive(t, pkg, v)), ImportOptions{})
			if err != nil {
				t.Fatalf("importing: %v", err)
			}
			if len(plan.Problems) > 0 {
				t.Errorf("problems: %v", plan.Problems)
			}
			for _, q := range []string{"SELECT COUNT(*) FROM categories WHERE name = 'Tools'", "SELECT COUNT(*) FROM assets WHERE name = 'Drill'",
				"SELECT COUNT(*) FROM asset_attachments WHERE kind = 'image'", "SELECT COUNT(*) FROM category_icons WHERE rendition = 'screen'"} {
				if n := countWhere(t, into, q); n != 1 {
					t.Errorf("%s: %d", q, n)
				}
			}
		})
	}
}

// TestAnArchiveLackingATableItsSchemaHadIsRefused (#288): at this schema
// and at each older one, an archive missing a table of its kinds that its
// schema had is refused, if only one with no rows.
func TestAnArchiveLackingATableItsSchemaHadIsRefused(t *testing.T) {
	src := setupStore(t)
	blobs := filestore.NewLocal(t.TempDir())
	richSeed(t, src, blobs)
	now := backup(t, src, blobs, Options{Secrets: true, Conversations: true})
	for _, v := range append(olderVersions(), migrations.Latest()) {
		b := now
		if v < migrations.Latest() {
			b = olderArchive(t, now, v)
		}
		for _, table := range tablesAt(v) {
			if table.Since > v {
				continue
			}
			var kept []member
			for _, m := range unpack(t, b) {
				if !strings.HasPrefix(m.name, DataDir+table.Name+"/") && !strings.HasPrefix(m.name, AuthDir+table.Name+"/") {
					kept = append(kept, m)
				}
			}
			lacking := pack(t, reseal(t, kept, func(_ *Header, man *Manifest) { delete(man.Tables, table.Name) }))
			if _, err := Check(bytes.NewReader(lacking)); !errors.Is(err, ErrInvalid) {
				t.Errorf("at schema %d, an archive without %s: %v", v, table.Name, err)
			}
		}
	}
}
