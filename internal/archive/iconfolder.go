package archive

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"sort"
	"strings"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// Icons as an isolex library folder (#146): an archive of format 2 keeps
// the icons' published sources as files in icons/, a folder isolex check
// and isolex studio open as it is, rather than in their rows; the rows
// keep the rest (IDs, drafts, when). lexicon.ilx is written for the folder
// to compile on its own, from the palette and icon settings, which keep
// their rows; icons.json says whose each category's icon is.

// iconSourceTables are the tables whose sources are in the folder, and the
// column each file is named by.
var iconSourceTables = map[string]string{"icon_library": "path", "category_icons": "icon_name"}

// iconFolder collects the folder as the rows are written.
type iconFolder struct {
	// library and icons are sources by path and by icon name.
	library, icons map[string]string
	// categories are categories' icons' categories, by icon name.
	categories map[string]string
	assets     []IndexedAsset
}

// take notes what of a row the folder holds, reporting whether its source
// is the folder's.
func (f *iconFolder) take(t *Table, vals []any) bool {
	get := func(col string) string {
		s, _ := text(vals[colIndex(t, col)])
		return s
	}
	switch t.Name {
	case "icon_library":
		if src, ok := text(vals[colIndex(t, "source")]); ok {
			f.library[get("path")] = src
		}
		return true
	case "category_icons":
		if src, ok := text(vals[colIndex(t, "source")]); ok {
			f.icons[get("icon_name")] = src
		}
		f.categories[get("icon_name")] = get("category_id")
		return true
	case "assets":
		if icon := get("icon_name"); icon != "" {
			f.assets = append(f.assets, IndexedAsset{AssetID: get("id"), Asset: get("name"), Icon: icon})
		}
	}
	return false
}

// write adds the folder to the archive: lexicon.ilx, the library at its
// paths, categories' icons by name, and icons.json.
func (f *iconFolder) write(ctx context.Context, st *store.Store, tx *sql.Tx, p *part) error {
	lexicon, err := st.IconLexiconTx(tx)
	if err != nil {
		return err
	}
	if err := p.addBytes(IconsDir+LexiconFile, []byte(lexicon)); err != nil {
		return err
	}
	for _, path := range sortedKeys(f.library) {
		if err := p.addBytes(IconsDir+"library/"+path, []byte(f.library[path])); err != nil {
			return err
		}
	}
	for _, name := range sortedKeys(f.icons) {
		if err := p.addBytes(IconsDir+"categories/"+name+".ilx", []byte(f.icons[name])); err != nil {
			return err
		}
	}
	index := IconsIndexFile{Categories: []IndexedIcon{}, Assets: f.assets}
	if index.Assets == nil {
		index.Assets = []IndexedAsset{}
	}
	names, err := categoryNames(ctx, st, tx)
	if err != nil {
		return err
	}
	for _, icon := range sortedKeys(f.categories) {
		id := f.categories[icon]
		index.Categories = append(index.Categories, IndexedIcon{Icon: icon, CategoryID: id, Category: names[id]})
	}
	b, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return err
	}
	return p.addBytes(IconsIndex, append(b, '\n'))
}

func categoryNames(ctx context.Context, st *store.Store, q querier) (map[string]string, error) {
	rows, err := q.QueryContext(ctx, st.Rebind("SELECT id, name FROM categories"))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var id, name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		out[id] = name
	}
	return out, rows.Err()
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// iconMember reports whether a path in icons/ is one the folder holds:
// lexicon.ilx, a library file at a path bam takes, or a category's icon
// by a name bam takes.
func iconMember(rest string) bool {
	if rest == LexiconFile {
		return true
	}
	if path, ok := strings.CutPrefix(rest, "library/"); ok {
		return store.ValidIconLibraryPath(path) == nil
	}
	if file, ok := strings.CutPrefix(rest, "categories/"); ok {
		name, ok := strings.CutSuffix(file, ".ilx")
		return ok && !strings.Contains(name, "/") && store.ValidIconName(name) == nil
	}
	return false
}

// maxIconFile is the most bytes a source in the folder may be.
const maxIconFile = store.MaxIconSource * 4

// readIconFile reads a source in the folder.
func readIconFile(name string, r io.Reader) (string, error) {
	b, err := io.ReadAll(io.LimitReader(r, maxIconFile+1))
	if err != nil {
		return "", invalid("%s: %v", name, err)
	}
	if len(b) > maxIconFile {
		return "", invalid("%s is longer than an icon's source may be", name)
	}
	return string(b), nil
}

// iconSourceKey is where in icons/ a row's source is: library/<path> or
// categories/<icon>.ilx.
func iconSourceKey(table, name string) string {
	if table == "icon_library" {
		return "library/" + name
	}
	return "categories/" + name + ".ilx"
}

// fillIconSource sets a source the folder holds, in tx: of a library file
// by its path, or a category's icon by its name.
func fillIconSource(ctx context.Context, st *store.Store, tx *sql.Tx, rest, source string) error {
	if path, ok := strings.CutPrefix(rest, "library/"); ok {
		_, err := tx.ExecContext(ctx, st.Rebind("UPDATE icon_library SET source = ? WHERE path = ?"), source, path)
		return err
	}
	if file, ok := strings.CutPrefix(rest, "categories/"); ok {
		_, err := tx.ExecContext(ctx, st.Rebind("UPDATE category_icons SET source = ? WHERE icon_name = ?"), source, strings.TrimSuffix(file, ".ilx"))
		return err
	}
	return nil
}
