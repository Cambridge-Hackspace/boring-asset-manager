package archive

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/isolex"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// An icon set (#146): bam's icons as a zipped isolex library folder, which
// isolex studio opens as it is (lexicon.ilx, library/, categories/), with
// icons.json beside it, and such a folder, made here or in the studio,
// imported back: checked like any import, then applied as one icon change
// that replaces what changed and leaves the rest alone.

// ItemPaletteToken is a palette color, by its token.
const ItemPaletteToken = "palette_token"

// ChoiceReplace replaces what is here with what an icon set holds.
const ChoiceReplace Choice = "replace"

// Limits on an icon set read: its size, what it may expand to, whatever
// its entries claim, and how many sources.
const (
	MaxIconSet        = 16 << 20
	maxIconSetExpand  = 64 << 20
	maxIconSetSources = 4096
)

// iconSetEpoch dates every entry, so the same icons make the same bytes.
var iconSetEpoch = time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC)

// WriteIconSet writes what is published as a zipped library folder:
// lexicon.ilx (the lexicon block and palette), library/<path>,
// categories/<icon>.ilx, and icons.json.
func WriteIconSet(ctx context.Context, st *store.Store, w io.Writer) error {
	files, err := st.IconLexiconFiles()
	if err != nil {
		return err
	}
	zw := zip.NewWriter(w)
	add := func(name string, text []byte) error {
		fw, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate, Modified: iconSetEpoch})
		if err == nil {
			_, err = fw.Write(text)
		}
		return err
	}
	for i, f := range files {
		name := f.Name
		if i == 0 {
			name = LexiconFile
		}
		if strings.HasSuffix(name, ".ilx") {
			if err := add(name, []byte(f.Text)); err != nil {
				return err
			}
		}
	}
	index, err := iconIndex(ctx, st)
	if err != nil {
		return err
	}
	b, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return err
	}
	if err := add(IconsIndex, append(b, '\n')); err != nil {
		return err
	}
	return zw.Close()
}

// iconIndex is icons.json for what is published.
func iconIndex(ctx context.Context, st *store.Store) (IconsIndexFile, error) {
	index := IconsIndexFile{Categories: []IndexedIcon{}, Assets: []IndexedAsset{}}
	rows, err := queryRows(ctx, st, st.DB, "SELECT ci.icon_name, c.id, c.name, ci.rendition FROM category_icons ci JOIN categories c ON c.id = ci.category_id WHERE ci.source IS NOT NULL ORDER BY ci.icon_name")
	if err != nil {
		return index, err
	}
	for _, r := range rows {
		index.Categories = append(index.Categories, IndexedIcon{Icon: r[0], CategoryID: r[1], Category: r[2], Rendition: r[3]})
	}
	if rows, err = queryRows(ctx, st, st.DB, "SELECT id, name, COALESCE(icon_name, ''), COALESCE(label_icon_name, '') FROM assets WHERE (icon_name IS NOT NULL OR label_icon_name IS NOT NULL) AND archived_at IS NULL ORDER BY id"); err != nil {
		return index, err
	}
	for _, r := range rows {
		if r[2] != "" {
			index.Assets = append(index.Assets, IndexedAsset{AssetID: r[0], Asset: r[1], Icon: r[2], Rendition: models.RenditionScreen})
		}
		if r[3] != "" {
			index.Assets = append(index.Assets, IndexedAsset{AssetID: r[0], Asset: r[1], Icon: r[3], Rendition: models.RenditionLabel})
		}
	}
	return index, nil
}

// IsIconSet reports whether an upload's first bytes are a zip's, as an
// icon set is, rather than a gzipped archive.
func IsIconSet(head []byte) bool { return bytes.HasPrefix(head, []byte("PK\x03\x04")) }

// iconSet is a library folder as read: its name, the sources by where
// they go, and icons.json.
type iconSet struct {
	name    string
	lexicon *string
	library map[string]string
	icons   map[string]string
	index   *IconsIndexFile
	skipped []string
}

// readIconSet reads a zipped library folder. A folder holding the whole
// of it is its name, not part of the paths; lexicon.ilx is its lexicon,
// categories/<icon>.ilx categories' icons, and every other .ilx a library
// file at its path, without library/ before it.
func readIconSet(data []byte) (*iconSet, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("%w: it is not a zip of an isolex library folder", ErrImport)
	}
	type entry struct {
		name string
		f    *zip.File
	}
	var entries []entry
	for _, f := range zr.File {
		base := path.Base(f.Name)
		if strings.HasSuffix(f.Name, "/") || strings.HasPrefix(f.Name, "__MACOSX/") || strings.HasPrefix(base, ".") {
			continue
		}
		if f.Name != path.Clean(f.Name) || path.IsAbs(f.Name) || strings.HasPrefix(f.Name, "../") {
			return nil, fmt.Errorf("%w: %s is not a path in a folder", ErrImport, f.Name)
		}
		entries = append(entries, entry{f.Name, f})
	}
	set := &iconSet{name: "icons", library: map[string]string{}, icons: map[string]string{}}
	if len(entries) > 0 {
		if top, _, ok := strings.Cut(entries[0].name, "/"); ok && top != "library" && top != "categories" {
			all := true
			for _, e := range entries {
				all = all && strings.HasPrefix(e.name, top+"/")
			}
			if all {
				set.name = top
				for i := range entries {
					entries[i].name = strings.TrimPrefix(entries[i].name, top+"/")
				}
			}
		}
	}
	var total int64
	read := func(e entry) (string, error) {
		rc, err := e.f.Open()
		if err != nil {
			return "", fmt.Errorf("%w: %s cannot be read: %v", ErrImport, e.name, err)
		}
		defer rc.Close()
		b, err := io.ReadAll(io.LimitReader(rc, maxIconFile+1))
		if err != nil {
			return "", fmt.Errorf("%w: %s cannot be read: %v", ErrImport, e.name, err)
		}
		if len(b) > maxIconFile {
			return "", fmt.Errorf("%w: %s is longer than an icon's source may be", ErrImport, e.name)
		}
		if total += int64(len(b)); total > maxIconSetExpand {
			return "", fmt.Errorf("%w: it expands to more than %d bytes", ErrImport, maxIconSetExpand)
		}
		return string(b), nil
	}
	sources := 0
	for _, e := range entries {
		switch {
		case e.name == IconsIndex:
			text, err := read(e)
			if err != nil {
				return nil, err
			}
			var index IconsIndexFile
			if err := json.Unmarshal([]byte(text), &index); err != nil {
				return nil, fmt.Errorf("%w: %s: %v", ErrImport, IconsIndex, err)
			}
			set.index = &index
			continue
		case !strings.HasSuffix(e.name, ".ilx"):
			set.skipped = append(set.skipped, e.name)
			continue
		}
		if sources++; sources > maxIconSetSources {
			return nil, fmt.Errorf("%w: a library folder may hold at most %d .ilx files", ErrImport, maxIconSetSources)
		}
		text, err := read(e)
		if err != nil {
			return nil, err
		}
		switch {
		case e.name == LexiconFile || e.name == isolex.LexiconFileName:
			set.lexicon = &text
		case strings.HasPrefix(e.name, "categories/"):
			name := strings.TrimSuffix(strings.TrimPrefix(e.name, "categories/"), ".ilx")
			if strings.Contains(name, "/") || store.ValidIconName(name) != nil {
				return nil, fmt.Errorf("%w: %s is not a category's icon bam keeps", ErrImport, e.name)
			}
			set.icons[name] = text
		default:
			p := strings.TrimPrefix(e.name, "library/")
			if err := store.ValidIconLibraryPath(p); err != nil {
				return nil, fmt.Errorf("%w: %s: %v", ErrImport, e.name, err)
			}
			set.library[p] = text
		}
	}
	if sources == 0 {
		return nil, fmt.Errorf("%w: it holds no .ilx files", ErrImport)
	}
	return set, nil
}

// readUpload reads an upload whole, up to max bytes.
func readUpload(open func() (io.ReadCloser, error), max int64) ([]byte, error) {
	rc, err := open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	b, err := io.ReadAll(io.LimitReader(rc, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > max {
		return nil, fmt.Errorf("%w: an icon set is at most %d bytes", ErrImport, max)
	}
	return b, nil
}

// checkIconSetImport works out what importing an icon set does: library
// files and categories' icons created, replaced or left as they are,
// palette colors added or changed, and what would not compile.
func checkIconSetImport(ctx context.Context, st *store.Store, data []byte, opts ImportOptions) (*ImportPlan, store.IconSet, error) {
	var set store.IconSet
	zs, err := readIconSet(data)
	if err != nil {
		return nil, set, err
	}
	if opts.Anchor != "" || len(opts.Choices) > 0 {
		return nil, set, fmt.Errorf("%w: an icon set goes under no category, and has no choices: it replaces what changed", ErrImport)
	}
	if opts.Icons == nil {
		return nil, set, fmt.Errorf("%w: an icon set is checked by compiling it, and isolex is not available here", ErrImport)
	}
	plan := &ImportPlan{
		Header: Header{Format: "isolex-library", Type: "icons", Kinds: []Kind{KindIcons}, Package: &PackageInfo{Name: zs.name}},
		Kinds:  []Kind{KindIcons}, Items: []ImportItem{}, Permissions: []string{rbac.PermIconsManage}, Missing: []string{},
		Warnings: []string{}, Problems: []string{},
	}
	may := opts.May
	if may == nil {
		may = func(string, []string) bool { return true }
	}
	if !may(rbac.PermIconsManage, nil) {
		plan.Missing = append(plan.Missing, rbac.PermIconsManage)
	}
	for _, s := range zs.skipped {
		plan.Warnings = append(plan.Warnings, fmt.Sprintf("%s is not an .ilx file, and is left out.", s))
	}

	library, err := st.ListIconLibrary()
	if err != nil {
		return nil, set, err
	}
	icons, err := st.ListCategoryIcons()
	if err != nil {
		return nil, set, err
	}
	// what defines each name here, but for what the set replaces
	defined := map[string]string{}
	for _, f := range library {
		if f.Source == nil {
			continue
		}
		if _, replaced := zs.library[f.Path]; replaced {
			continue
		}
		for _, d := range ScanIcons(*f.Source).Defines {
			defined[d] = "the library file " + f.Path
		}
	}
	byName := map[string]models.CategoryIcon{}
	for _, i := range icons {
		byName[i.IconName] = i
		if i.Source == nil {
			continue
		}
		if _, replaced := zs.icons[i.IconName]; replaced {
			continue
		}
		for _, d := range ScanIcons(*i.Source).Defines {
			defined[d] = "the icon " + i.IconName
		}
	}

	paths := sortedKeys(zs.library)
	here := map[string]*models.IconLibraryFile{}
	for i := range library {
		here[library[i].Path] = &library[i]
	}
	for _, p := range paths {
		src := zs.library[p]
		it := ImportItem{Ref: Ref{Kind: ItemIconFile, ID: p, Name: p}, Action: ChoiceCreate}
		if f := here[p]; f != nil {
			it.Existing = &Ref{Kind: ItemIconFile, ID: f.ID, Name: p}
			if f.Source != nil && *f.Source == src {
				it.Action, it.Identical = ChoiceExisting, true
			} else {
				it.Action = ChoiceReplace
			}
		}
		for _, d := range ScanIcons(src).Defines {
			if owner, ok := defined[d]; ok {
				plan.Problems = append(plan.Problems, fmt.Sprintf("%s defines %q, which %s defines here", p, d, owner))
			}
		}
		if it.Action != ChoiceExisting {
			set.Library = append(set.Library, store.IconSourceFile{Path: p, Source: src})
		}
		plan.Items = append(plan.Items, it)
	}

	set.Icons = map[string]string{}
	indexed := map[string]string{}
	if zs.index != nil {
		for _, c := range zs.index.Categories {
			indexed[c.Icon] = c.CategoryID
		}
	}
	for _, name := range sortedKeys(zs.icons) {
		src := zs.icons[name]
		it := ImportItem{Ref: Ref{Kind: ItemCategoryIcon, ID: name, Name: name}}
		cur, ok := byName[name]
		switch {
		case !ok:
			it.Action = ChoiceSkip
			it.Note = "No category here has an icon by this name: give a category it on the category's page."
		case indexed[name] != "" && indexed[name] != cur.CategoryID:
			// another instance's, whose category is not this one
			it.Action = ChoiceSkip
			it.Note = "It is another instance's category's icon, whose name this instance gives another category."
		case cur.Source != nil && *cur.Source == src:
			it.Action, it.Identical = ChoiceExisting, true
		default:
			it.Action = ChoiceReplace
			set.Icons[name] = src
		}
		if ok {
			it.Existing = &Ref{Kind: ItemCategoryIcon, ID: cur.CategoryID, Name: name}
		}
		if it.Action != ChoiceSkip {
			for _, d := range ScanIcons(src).Defines {
				if owner, ok := defined[d]; ok {
					plan.Problems = append(plan.Problems, fmt.Sprintf("categories/%s.ilx defines %q, which %s defines here", name, d, owner))
				}
			}
		}
		plan.Items = append(plan.Items, it)
	}

	// the palette, as the folder's own compile reads it
	if err := iconSetPalette(ctx, st, opts.Icons, zs, plan, &set); err != nil {
		return nil, set, err
	}

	check, err := st.CheckIconSet(ctx, opts.Icons, set)
	if errors.Is(err, isolex.ErrUnavailable) {
		return nil, set, fmt.Errorf("%w: an icon set is checked by compiling it, and isolex is not available here", ErrImport)
	}
	if err != nil {
		return nil, set, fmt.Errorf("%w: %v", ErrImport, err)
	}
	for _, d := range check.Errors {
		plan.Problems = append(plan.Problems, fmt.Sprintf("the icons would not compile here: %s:%d:%d: %s", d.File, d.Line, d.Col, d.Message))
	}
	sort.Strings(plan.Problems)
	plan.Fingerprint = fingerprint(plan)
	return plan, set, nil
}

// iconSetPalette adds the set's palette colors that are new or changed to
// the plan, and notes a lexicon block other than bam's.
func iconSetPalette(ctx context.Context, st *store.Store, c store.IconCompiler, zs *iconSet, plan *ImportPlan, set *store.IconSet) error {
	var files []isolex.File
	if zs.lexicon != nil {
		files = append(files, isolex.File{Name: LexiconFile, Text: *zs.lexicon})
	}
	for _, p := range sortedKeys(zs.library) {
		files = append(files, isolex.File{Name: "library/" + p, Text: zs.library[p]})
	}
	res, err := c.Compile(ctx, files)
	if err != nil {
		return fmt.Errorf("%w: reading its palette: %v", ErrImport, err)
	}
	palette, err := st.GetIconPalette()
	if err != nil {
		return err
	}
	here := map[string]models.IconColor{}
	for _, col := range palette {
		here[col.Token] = col
	}
	for _, token := range sortedKeys(res.Palette) {
		col := res.Palette[token]
		it := ImportItem{Ref: Ref{Kind: ItemPaletteToken, ID: token, Name: token}, Action: ChoiceCreate}
		if h, ok := here[token]; ok {
			it.Existing = &Ref{Kind: ItemPaletteToken, ID: token, Name: token}
			if strings.EqualFold(h.Light, col.Light) && strings.EqualFold(h.Dark, col.Dark) {
				it.Action, it.Identical = ChoiceExisting, true
			} else {
				it.Action = ChoiceReplace
			}
		}
		if it.Action != ChoiceExisting {
			set.Palette = append(set.Palette, models.IconColor{Token: token, Light: col.Light, Dark: col.Dark})
		}
		plan.Items = append(plan.Items, it)
	}
	if zs.lexicon != nil {
		block, err := st.IconSettingsBlock()
		if err != nil {
			return err
		}
		if !strings.Contains(*zs.lexicon, strings.TrimSpace(block)) {
			plan.Warnings = append(plan.Warnings, "Only the palette is taken from lexicon.ilx: its tokens, stroke widths, reserved tokens and limits are this instance's icon settings, set on the Icons page.")
		}
	}
	return nil
}
