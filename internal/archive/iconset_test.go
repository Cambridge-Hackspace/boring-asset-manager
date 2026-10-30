package archive

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/filestore"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/isolex"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

func compiler(t *testing.T) *isolex.Compiler {
	t.Helper()
	c := isolex.Default()
	if !c.Available() {
		if os.Getenv("BAM_REQUIRE_ISOLEX") != "" {
			t.Fatal("isolex is not available")
		}
		t.Skip("isolex is not available")
	}
	return c
}

func sourceOf(t *testing.T, st *store.Store, query string, args ...any) string {
	t.Helper()
	var s *string
	if err := st.QueryRow(query, args...).Scan(&s); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	if s == nil {
		return "<null>"
	}
	return *s
}

// A backup keeps the icons' sources as an isolex library folder, icons/,
// with icons.json, and not in their rows; restored, every source is back,
// drafts and all.
func TestIconsTravelAsALibraryFolder(t *testing.T) {
	src := setupStore(t)
	blobs := filestore.NewLocal(t.TempDir())
	p := seedPackaged(t, src, blobs)
	if _, err := src.Exec("UPDATE assets SET icon_name = 'toolbox' WHERE id = ?", p.drawer); err != nil {
		t.Fatal(err)
	}
	b := backup(t, src, blobs, Options{Secrets: true})
	r := readArchive(t, b)
	if r.header.FormatVersion != 2 {
		t.Errorf("format %d", r.header.FormatVersion)
	}
	for path, want := range map[string]string{
		IconsDir + "library/shared/crates.ilx": `base "crate-base" { category "storage"; footprint 1 1 1; part "box" { insert "lid" } }`,
		IconsDir + "library/shared/lids.ilx":   `fragment "lid" { tile 0 0 1 1 1 }`,
		IconsDir + "categories/toolbox.ilx":    `icon "toolbox" { extends "crate-base" }`,
	} {
		if got := string(r.members[path]); got != want {
			t.Errorf("%s: %q", path, got)
		}
	}
	if lex := string(r.members[IconsDir+LexiconFile]); !strings.Contains(lex, "lexicon {") || !strings.Contains(lex, "palette {") {
		t.Errorf("lexicon.ilx: %s", lex)
	}
	var index IconsIndexFile
	if err := json.Unmarshal(r.members[IconsIndex], &index); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(index.Categories, []IndexedIcon{{Icon: "toolbox", CategoryID: p.tools.ID, Category: "Workshop tools"}}) ||
		len(index.Assets) != 1 || index.Assets[0].AssetID != p.drawer || index.Assets[0].Icon != "toolbox" {
		t.Errorf("icons.json %+v", index)
	}
	for _, table := range []string{"icon_library", "category_icons"} {
		for _, row := range r.rows(t, DataDir+table+"/") {
			if _, ok := row["source"]; ok {
				t.Errorf("%s's row holds its source: %v", table, row)
			}
			if row["draft"] != "a draft" {
				t.Errorf("%s's draft %v", table, row["draft"])
			}
		}
	}

	dst := emptyStore(t, envDialect())
	if _, err := Restore(context.Background(), dst, filestore.NewLocal(t.TempDir()), opener(b), RestoreOptions{Secrets: true}); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{"SELECT source FROM icon_library WHERE path = 'shared/crates.ilx'", "SELECT source FROM category_icons WHERE icon_name = 'toolbox'", "SELECT draft FROM icon_library WHERE path = 'shared/lids.ilx'"} {
		if got, want := sourceOf(t, dst, q), sourceOf(t, src, q); got != want {
			t.Errorf("%s: %q, want %q", q, got, want)
		}
	}

	// replacing the icons with the backup puts the sources back as it has
	// them, and one the backup has none of is none
	if _, err := src.Exec("UPDATE icon_library SET source = 'changed' WHERE path = 'shared/crates.ilx'"); err != nil {
		t.Fatal(err)
	}
	if _, err := src.Exec("UPDATE icon_library SET source = NULL WHERE path = 'shared/lids.ilx'"); err != nil {
		t.Fatal(err)
	}
	unpublished := backup(t, src, blobs, Options{Kinds: []Kind{KindIcons}})
	if _, err := src.Exec("UPDATE icon_library SET source = 'published since' WHERE path = 'shared/lids.ilx'"); err != nil {
		t.Fatal(err)
	}
	if _, err := Restore(context.Background(), src, blobs, opener(b), RestoreOptions{Kinds: []Kind{KindIcons}, Replace: true}); err != nil {
		t.Fatal(err)
	}
	if got := sourceOf(t, src, "SELECT source FROM icon_library WHERE path = 'shared/crates.ilx'"); !strings.Contains(got, "crate-base") {
		t.Errorf("replaced: %q", got)
	}
	if _, err := Restore(context.Background(), src, blobs, opener(unpublished), RestoreOptions{Kinds: []Kind{KindIcons}, Replace: true}); err != nil {
		t.Fatal(err)
	}
	if got := sourceOf(t, src, "SELECT source FROM icon_library WHERE path = 'shared/lids.ilx'"); got != "<null>" {
		t.Errorf("a source the backup has none of: %q", got)
	}
}

// An archive of format 1, its icons' sources in their rows, still
// restores.
func TestAFormat1ArchiveStillRestores(t *testing.T) {
	writeFormat = 1
	t.Cleanup(func() { writeFormat = FormatVersion })
	src := setupStore(t)
	blobs := filestore.NewLocal(t.TempDir())
	seedPackaged(t, src, blobs)
	b := backup(t, src, blobs, Options{Secrets: true})
	r := readArchive(t, b)
	for path := range r.members {
		if strings.HasPrefix(path, IconsDir) || path == IconsIndex {
			t.Errorf("a format 1 archive holds %s", path)
		}
	}
	dst := emptyStore(t, envDialect())
	if _, err := Restore(context.Background(), dst, filestore.NewLocal(t.TempDir()), opener(b), RestoreOptions{Secrets: true}); err != nil {
		t.Fatal(err)
	}
	if got := sourceOf(t, dst, "SELECT source FROM category_icons WHERE icon_name = 'toolbox'"); got != `icon "toolbox" { extends "crate-base" }` {
		t.Errorf("restored %q", got)
	}
	// and a folder in a format 1 archive is refused
	ms := unpack(t, b)
	ms = append(ms[:len(ms)-1], member{IconsDir + "library/x.ilx", []byte("x")}, ms[len(ms)-1])
	if _, err := Check(bytes.NewReader(pack(t, reseal(t, ms, nil)))); !errors.Is(err, ErrInvalid) {
		t.Errorf("icons/ in format 1: %v", err)
	}
}

// zipOf zips files.
func zipOf(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		w, err := zw.Create(n)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.WriteString(w, files[n])
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func unzipped(t *testing.T, b []byte) map[string]string {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, _ := io.ReadAll(rc)
		rc.Close()
		out[f.Name] = string(data)
	}
	return out
}

const crateBase = `base "crate-base" { category "storage"; footprint 1 1 1; part "box" { box 0 0 0 1 1 1 } }`

// iconInstance has a library file, a category's icon and a palette.
func iconInstance(t *testing.T, c *isolex.Compiler) (*store.Store, *models.Category) {
	t.Helper()
	ctx := context.Background()
	st := setupStore(t)
	if _, _, err := st.CreateIconLibraryFile(ctx, c, "shared/crates.ilx", crateBase); err != nil {
		t.Fatal(err)
	}
	cat := must[*models.Category](t)(st.CreateCategory("Storage", nil, "#123456", false, false))
	if save, err := st.SaveCategoryIcon(ctx, c, cat.ID, "crate", `icon "crate" { extends "crate-base" }`); err != nil || !save.Published {
		t.Fatalf("%+v %v", save, err)
	}
	return st, cat
}

func revisions(t *testing.T, st *store.Store) int { return count(t, st, "icon_revisions") }

// The icons export as a zipped library folder, and come back from it: the
// palette through its block, and what changed in the studio replaced, as
// one icon change, the rest left alone.
func TestAnIconSetRoundTrips(t *testing.T) {
	c := compiler(t)
	ctx := context.Background()
	st, cat := iconInstance(t, c)
	palette, err := st.GetIconPalette()
	if err != nil || len(palette) == 0 {
		t.Fatalf("palette %v %v", palette, err)
	}
	var buf bytes.Buffer
	if err := WriteIconSet(ctx, st, &buf); err != nil {
		t.Fatal(err)
	}
	files := unzipped(t, buf.Bytes())
	if files["library/shared/crates.ilx"] != crateBase || files["categories/crate.ilx"] == "" || !strings.Contains(files[LexiconFile], "palette {") {
		t.Fatalf("the set: %v", files)
	}
	var index IconsIndexFile
	if err := json.Unmarshal([]byte(files[IconsIndex]), &index); err != nil || index.Categories[0].CategoryID != cat.ID {
		t.Fatalf("icons.json %s %v", files[IconsIndex], err)
	}

	// unchanged, it changes nothing
	plan, err := CheckImport(ctx, st, opener(buf.Bytes()), ImportOptions{Icons: c})
	if err != nil || len(plan.Problems) > 0 {
		t.Fatalf("%v %+v", err, plan)
	}
	for _, it := range plan.Items {
		if it.Action != ChoiceExisting {
			t.Errorf("unchanged, %s %s: %s", it.Kind, it.Name, it.Action)
		}
	}

	// edited in the studio: a file and a color changed, one added
	before := revisions(t, st)
	first := palette[0]
	files["library/shared/crates.ilx"] = strings.Replace(crateBase, "footprint 1 1 1", "footprint 1 1 2", 1)
	files[LexiconFile] = strings.Replace(files[LexiconFile], first.Light, "#123456", 1)
	files["library/shared/bins.ilx"] = `base "bin-base" { category "storage"; footprint 1 1 1; part "box" { box 0 0 0 1 1 1 } }`
	edited := zipOf(t, files)
	plan, err = CheckImport(ctx, st, opener(edited), ImportOptions{Icons: c})
	if err != nil || len(plan.Problems) > 0 {
		t.Fatalf("%v %+v", err, plan)
	}
	actions := map[string]Choice{}
	for _, it := range plan.Items {
		actions[it.Kind+":"+it.Name] = it.Action
	}
	want := map[string]Choice{
		ItemIconFile + ":shared/crates.ilx": ChoiceReplace, ItemIconFile + ":shared/bins.ilx": ChoiceCreate,
		ItemCategoryIcon + ":crate": ChoiceExisting, ItemPaletteToken + ":" + first.Token: ChoiceReplace,
	}
	for k, v := range want {
		if actions[k] != v {
			t.Errorf("%s: %s, want %s", k, actions[k], v)
		}
	}
	if _, err := Import(ctx, st, nil, opener(edited), ImportOptions{Icons: c, Fingerprint: plan.Fingerprint}); err != nil {
		t.Fatal(err)
	}
	if got := sourceOf(t, st, "SELECT source FROM icon_library WHERE path = 'shared/crates.ilx'"); !strings.Contains(got, "footprint 1 1 2") {
		t.Errorf("replaced: %s", got)
	}
	if n := revisions(t, st) - before; n != 2 {
		t.Errorf("%d revisions: one for each file changed or added, none for what was not", n)
	}
	after, _ := st.GetIconPalette()
	if after[0].Token != first.Token || after[0].Light != "#123456" || after[0].Dark != first.Dark || len(after) != len(palette) {
		t.Errorf("the palette after: %+v", after[0])
	}
	build, err := st.CurrentIconBuild()
	if err != nil || build == nil {
		t.Fatalf("no build: %v", err)
	}
}

// A library made in the studio (a folder zipped, with no icons.json)
// imports as an icon set; one that clashes with what is here, or would
// not compile here, is refused at check, before anything changes.
func TestAStudioLibraryImports(t *testing.T) {
	c := compiler(t)
	ctx := context.Background()
	st, _ := iconInstance(t, c)
	studio := zipOf(t, map[string]string{
		"My shapes/lexicon.ilx":       "palette {\n  sky light=\"#88ccff\" dark=\"#224466\"\n}\n",
		"My shapes/shapes/pallet.ilx": `base "pallet-base" { category "storage"; footprint 2 2 1; part "deck" { box 0 0 0 2 2 1 } }`,
		"My shapes/notes.txt":         "not a source",
	})
	plan, err := CheckImport(ctx, st, opener(studio), ImportOptions{Icons: c})
	if err != nil || len(plan.Problems) > 0 {
		t.Fatalf("%v %+v", err, plan)
	}
	if plan.Header.Package.Name != "My shapes" || plan.Header.Type != "icons" {
		t.Errorf("header %+v", plan.Header)
	}
	if !strings.Contains(strings.Join(plan.Warnings, " "), "notes.txt") {
		t.Errorf("warnings %v", plan.Warnings)
	}
	if _, err := Import(ctx, st, nil, opener(studio), ImportOptions{Icons: c}); err != nil {
		t.Fatal(err)
	}
	if got := sourceOf(t, st, "SELECT source FROM icon_library WHERE path = 'shapes/pallet.ilx'"); !strings.Contains(got, "pallet-base") {
		t.Errorf("imported %s", got)
	}
	if palette, _ := st.GetIconPalette(); palette[len(palette)-1].Token != "sky" {
		t.Errorf("the new color: %+v", palette)
	}

	before := revisions(t, st)
	for name, files := range map[string]map[string]string{
		"a definition here already": {"more.ilx": crateBase},
		"what would not compile":    {"broken.ilx": `icon "broken" { extends "nowhere" }`},
	} {
		plan, err := CheckImport(ctx, st, opener(zipOf(t, files)), ImportOptions{Icons: c})
		if err != nil || len(plan.Problems) == 0 {
			t.Errorf("%s: %v %+v", name, err, plan)
		}
		if _, err := Import(ctx, st, nil, opener(zipOf(t, files)), ImportOptions{Icons: c}); !errors.Is(err, ErrImport) {
			t.Errorf("%s imported: %v", name, err)
		}
	}
	plan, err = CheckImport(ctx, st, opener(zipOf(t, map[string]string{"categories/barrel.ilx": `icon "barrel" { extends "crate-base" }`})), ImportOptions{Icons: c})
	if err != nil || len(plan.Items) != 1 || plan.Items[0].Action != ChoiceSkip {
		t.Errorf("a category's icon nothing here has: %v %+v", err, plan)
	}
	if revisions(t, st) != before {
		t.Error("a refused icon set changed the icons")
	}
	if _, err := CheckImport(ctx, st, opener(zipOf(t, map[string]string{"x.ilx": crateBase})), ImportOptions{Icons: c, Anchor: "x"}); !errors.Is(err, ErrImport) {
		t.Errorf("an anchor for an icon set: %v", err)
	}
}
