package api

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"sort"
	"strings"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/isolex"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// iconZip is a zip of files by name, in name order.
func iconZip(files map[string]string) []byte {
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, n := range names {
		w, _ := zw.Create(n)
		_, _ = io.WriteString(w, files[n])
	}
	_ = zw.Close()
	return buf.Bytes()
}

// unzip reads a zip's files by name.
func unzip(t *testing.T, data []byte) map[string]string {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, f := range zr.File {
		rc, _ := f.Open()
		b, _ := io.ReadAll(rc)
		rc.Close()
		out[f.Name] = string(b)
	}
	return out
}

func importZip(t *testing.T, srv *Server, cookie *http.Cookie, data []byte) (int, string) {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	w, _ := mw.CreateFormFile("file", "icons.zip")
	_, _ = w.Write(data)
	_ = mw.Close()
	rec := do(t, srv, cookie, "POST", "/api/v1/icon-library/import", mw.FormDataContentType(), &body)
	return rec.Code, rec.Body.String()
}

const crateBase = `base "crate-base" { category "storage"; footprint 1 1 1; part "box" { box 0 0 0 1 1 1 } }`

// A library file and a category icon extending it.
func withIcons(t *testing.T, srv *Server) {
	t.Helper()
	ctx := context.Background()
	cat, _ := srv.store.CreateCategoryFrom(store.CategoryInput{Name: "Storage"})
	if _, _, err := srv.store.CreateIconLibraryFile(ctx, srv.icons, "shared/crates.ilx", crateBase); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.store.SaveCategoryIcon(ctx, srv.icons, cat.ID, "crate", `icon "crate" { extends "crate-base" }`); err != nil {
		t.Fatal(err)
	}
}

// The export is the lexicon as a build compiles it: the compiler checks
// it as it is, the same bytes every time.
func TestExportingIconsMakesALexiconIsolexChecks(t *testing.T) {
	requireIcons(t)
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	withIcons(t, srv)

	rec := doJSON(t, srv, cookie, "GET", "/api/v1/icons/export", "")
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "application/zip" || !strings.Contains(rec.Header().Get("Content-Disposition"), "icons.zip") {
		t.Fatalf("%d %v", rec.Code, rec.Header())
	}
	files := unzip(t, rec.Body.Bytes())
	var names []string
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	if strings.Join(names, " ") != "_lexicon.ilx categories/crate.ilx library/shared/crates.ilx" {
		t.Fatalf("exported %v", names)
	}
	if !strings.Contains(files["_lexicon.ilx"], `tokens "body-top"`) || files["categories/crate.ilx"] != `icon "crate" { extends "crate-base" }` {
		t.Errorf("%q", files)
	}
	var lexicon []isolex.File
	for _, n := range names {
		lexicon = append(lexicon, isolex.File{Name: n, Text: files[n]})
	}
	r, err := isolex.Default().Compile(context.Background(), lexicon)
	if err != nil || !r.OK {
		t.Fatalf("the export does not compile: %v %+v", err, r)
	}
	// it carries the palette, every color of it, as a palette block
	palette, _ := srv.store.GetIconPalette()
	if len(palette) == 0 || len(r.Palette) != len(palette) {
		t.Fatalf("the export's palette %v, bam's %v", r.Palette, palette)
	}
	for _, c := range palette {
		if r.Palette[c.Token] != (isolex.Colors{Light: c.Light, Dark: c.Dark}) {
			t.Errorf("%s: the export has %v, bam %+v", c.Token, r.Palette[c.Token], c)
		}
	}
	if again := doJSON(t, srv, cookie, "GET", "/api/v1/icons/export", ""); !bytes.Equal(again.Body.Bytes(), rec.Body.Bytes()) {
		t.Error("two exports of the same icons differ")
	}
}

func TestImportingIconsIntoTheLibrary(t *testing.T) {
	requireIcons(t)
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	withIcons(t, srv)

	// an export comes back in: its library replaced, the rest skipped
	export := doJSON(t, srv, cookie, "GET", "/api/v1/icons/export", "").Body.Bytes()
	code, body := importZip(t, srv, cookie, export)
	var out struct {
		Save             struct{ Published bool }
		Created, Updated int
		Skipped          []skippedEntry
	}
	_ = json.Unmarshal([]byte(body), &out)
	if code != http.StatusOK || !out.Save.Published || out.Created != 0 || out.Updated != 1 || len(out.Skipped) != 2 {
		t.Fatalf("round trip: %d %s", code, body)
	}

	// a folder holding everything is the archive's; what is not .ilx is skipped
	code, body = importZip(t, srv, cookie, iconZip(map[string]string{
		"my-lexicon/racks/rack.ilx": `base "rack" { category "net"; footprint 2 1 1; part "frame" { box 0 0 0 2 1 1 } }`,
		"my-lexicon/README.md":      "# racks",
		"my-lexicon/.hidden.ilx":    "x",
		"__MACOSX/my-lexicon/._x":   "x",
		"my-lexicon/shared/glyphs/": "",
	}))
	if code != http.StatusOK || !strings.Contains(body, `"created":1`) || !strings.Contains(body, `{"path":"README.md","reason":"not an .ilx file"}`) {
		t.Fatalf("a folder of files: %d %s", code, body)
	}
	files, _ := srv.store.ListIconLibrary()
	if len(files) != 2 || files[0].Path != "racks/rack.ilx" {
		t.Errorf("library: %+v", files)
	}
	if revs, _ := srv.store.ListIconLibraryRevisions(files[1].ID); len(revs) != 2 {
		t.Errorf("an imported file is not in its history: %+v", revs)
	}

	// what would break the icons is refused whole, nothing kept
	code, body = importZip(t, srv, cookie, iconZip(map[string]string{
		"shared/crates.ilx": `base "crate-base2" { }`,
		"new.ilx":           `fragment "f" { }`,
	}))
	if code != http.StatusConflict || !strings.Contains(body, "categories/crate.ilx") {
		t.Errorf("a breaking import: %d %s", code, body)
	}
	if after, _ := srv.store.ListIconLibrary(); len(after) != 2 || *after[1].Source != crateBase {
		t.Errorf("a refused import kept something: %+v", after)
	}
}

func TestImportsThatAreRefused(t *testing.T) {
	requireIcons(t)
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	many := map[string]string{}
	for i := 0; i <= archiveMaxFiles; i++ {
		many[fmt.Sprintf("f%05d.ilx", i)] = ""
	}
	for _, c := range []struct {
		what string
		data []byte
		want string
	}{
		{"not a zip", []byte("hello"), "not a zip archive"},
		{"nothing to import", iconZip(map[string]string{"a.txt": "x"}), "holds no .ilx files"},
		{"a path that is not one", iconZip(map[string]string{"a b.ilx": ""}), "a b.ilx"},
		{"the same file twice", iconZip(map[string]string{"a.ilx": "", "library/a.ilx": ""}), "given twice"},
		{"a source too long", iconZip(map[string]string{"a.ilx": strings.Repeat("x", archiveEntryLimit+1)}), "longer than a source may be"},
		{"too many files", iconZip(many), "at most 4096"},
	} {
		code, body := importZip(t, srv, cookie, c.data)
		if code != http.StatusBadRequest || !strings.Contains(body, c.want) {
			t.Errorf("%s: %d %.300s", c.what, code, body)
		}
	}
	// more than the archive limit is refused before it is read
	if code, _ := importZip(t, srv, cookie, bytes.Repeat([]byte("x"), archiveBodyLimit+1)); code != http.StatusRequestEntityTooLarge {
		t.Errorf("an archive over the limit: %d", code)
	}
	// and the library is icons.manage's
	curator := userWithPermissions(t, srv, "curator@test.org", rbac.PermAssetsRead, rbac.PermCategoriesManage)
	if code, _ := importZip(t, srv, curator, iconZip(map[string]string{"a.ilx": ""})); code != http.StatusForbidden {
		t.Errorf("an import without icons.manage: %d", code)
	}
	if rec := doJSON(t, srv, curator, "GET", "/api/v1/icons/export", ""); rec.Code != http.StatusOK {
		t.Errorf("an export with assets.read: %d", rec.Code)
	}
}

// An archive's entries may claim any size: what is read is bounded by
// what they expand to.
func TestAnArchiveThatExpandsTooFarIsRefused(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	chunk := strings.Repeat(" ", archiveEntryLimit)
	for i := 0; i*archiveEntryLimit <= archiveExpandLimit; i++ {
		w, _ := zw.Create(fmt.Sprintf("f%03d.ilx", i))
		_, _ = io.WriteString(w, chunk)
	}
	_ = zw.Close()
	if buf.Len() > archiveBodyLimit {
		t.Fatalf("the archive is %d bytes, over the upload limit; it should compress", buf.Len())
	}
	if _, _, err := readIconArchive(buf.Bytes()); err == nil || !strings.Contains(err.Error(), "expands to more than") {
		t.Errorf("err = %v", err)
	}
}
