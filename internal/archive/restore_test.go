package archive

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/auth"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/config"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/filestore"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store/migrations"
)

// envDialect is the dialect the suite runs against.
func envDialect() string {
	if d := os.Getenv(store.TestDialectEnv); d != "" {
		return d
	}
	return "sqlite"
}

// emptyStore is a database with no tables at all.
func emptyStore(t *testing.T, dialect string) *store.Store {
	t.Helper()
	if dialect == "sqlite" {
		st, err := store.Open(&config.DatabaseConfig{Type: "sqlite", DSN: filepath.Join(t.TempDir(), "bam.db")})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = st.Close() })
		return st
	}
	st, cleanup, err := store.OpenEphemeral(dialect, os.Getenv(store.TestDSNEnv))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	return st
}

// migratedStore is a database as bam migrate leaves it: not yet in use.
func migratedStore(t *testing.T, dialect string) *store.Store {
	t.Helper()
	st := emptyStore(t, dialect)
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	if err := st.SeedDefaultPermissionsAndRoles(); err != nil {
		t.Fatal(err)
	}
	return st
}

// awkwardText is text databases are known to mangle.
var awkwardText = []string{
	"Ångström · naïve café",
	"工具箱 ドリル",
	"🔧 Toolbox 🪛",
	"e" + string(rune(0x301)) + " / é",
	`C:\path\'quoted' "double" %_ \`,
	"مثقاب 👩" + string(rune(0x200d)) + "🔧",
}

// richSeed adds to seed every kind of value: awkward text, a category
// with a parent, floats, JSON, NULLs and a role assignment.
func richSeed(t *testing.T, st *store.Store, blobs filestore.Blobs) seeded {
	t.Helper()
	s := seed(t, st, blobs)
	parent, err := st.CreateCategory(awkwardText[0], nil, "#112233", true, false)
	if err != nil {
		t.Fatal(err)
	}
	for i, name := range awkwardText[1:] {
		if _, err := st.CreateCategory(name, &parent.ID, "#445566", i%2 == 0, i%2 == 0); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := st.Exec("UPDATE users SET name = ? WHERE id = ?", awkwardText[5], s.user.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SetUserTheme(s.user.ID, "dark"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Exec("INSERT INTO templates (id, name, units, width, height, dpi, layout_json, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
		uuid.NewString(), awkwardText[2], "mm", 62.5, 29.125, 300, layoutJSON(awkwardText[4]),
		time.Date(2026, 1, 2, 3, 4, 5, 123456000, time.UTC), time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	var admin string
	if err := st.QueryRow("SELECT id FROM roles WHERE name = 'admin'").Scan(&admin); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Exec("INSERT INTO user_roles (user_id, role_id) VALUES (?, ?)", s.user.ID, admin); err != nil {
		t.Fatal(err)
	}
	return s
}

// backup writes an archive of st in one part.
func backup(t *testing.T, st *store.Store, blobs filestore.Blobs, opts Options) []byte {
	t.Helper()
	var parts memParts
	if _, err := Write(context.Background(), st, blobs, parts.open, opts); err != nil {
		t.Fatal(err)
	}
	return parts.parts[1].Bytes()
}

func opener(b []byte) func() (io.ReadCloser, error) {
	return func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(b)), nil }
}

// contents is what an archive holds, apart from when and where it was
// written: each table's rows, in no order, and each file.
func contents(t *testing.T, b []byte) map[string]string {
	t.Helper()
	r := readArchive(t, b)
	out := map[string]string{}
	lines := map[string][]string{}
	for path, data := range r.members {
		if strings.HasPrefix(path, DataDir) || strings.HasPrefix(path, AuthDir) {
			dir := path[:strings.LastIndex(path, "/")]
			for _, l := range strings.Split(strings.TrimSuffix(string(data), "\n"), "\n") {
				lines[dir] = append(lines[dir], l)
			}
		}
		if strings.HasPrefix(path, FilesDir) {
			out[path] = string(data)
		}
	}
	for dir, ls := range lines {
		sort.Strings(ls)
		out[dir] = strings.Join(ls, "\n")
	}
	return out
}

func sameContents(t *testing.T, want, got map[string]string) {
	t.Helper()
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s differs:\nwant %.600s\ngot  %.600s", k, v, got[k])
		}
	}
	for k := range got {
		if _, ok := want[k]; !ok {
			t.Errorf("%s was not in the archive restored", k)
		}
	}
}

// TestARestoreMakesTheSameInstance restores backups from this dialect and
// from SQLite into each, and backs the result up again: every row and
// file is as it was.
func TestARestoreMakesTheSameInstance(t *testing.T) {
	ctx := context.Background()
	dialect := envDialect()
	for _, pair := range [][2]string{{dialect, dialect}, {"sqlite", dialect}, {dialect, "sqlite"}} {
		t.Run(pair[0]+" to "+pair[1], func(t *testing.T) {
			src := migratedStore(t, pair[0])
			srcBlobs := filestore.NewLocal(t.TempDir())
			s := richSeed(t, src, srcBlobs)
			b := backup(t, src, srcBlobs, Options{Secrets: true})

			dst := emptyStore(t, pair[1])
			dstBlobs := filestore.NewLocal(t.TempDir())
			sum, err := Restore(ctx, dst, dstBlobs, opener(b), RestoreOptions{Secrets: true})
			if err != nil {
				t.Fatal(err)
			}
			if len(sum.Warnings) != 0 || sum.Files != 3 || sum.Tables["users"] != 1 {
				t.Errorf("summary: %+v", sum)
			}
			sameContents(t, contents(t, b), contents(t, backup(t, dst, dstBlobs, Options{Secrets: true})))

			// Restored people sign in with their own passwords, and their
			// sessions hold.
			if !signsIn(t, dst, "ada@example.com", "correct horse battery") {
				t.Error("cannot sign in after a restore")
			}
			if n := count(t, dst, "sessions"); n != 1 {
				t.Errorf("%d sessions restored", n)
			}
			if _, err := dstBlobs.Get(ctx, s.missing); !errors.Is(err, filestore.ErrNotFound) {
				t.Errorf("a missing file was made: %v", err)
			}
		})
	}
}

// TestAuthenticationIsRestoredOnlyWhenAllowed: without Secrets, as through
// the web, authentication is skipped with a warning, and people have no
// password anyone can sign in with.
func TestAuthenticationIsRestoredOnlyWhenAllowed(t *testing.T) {
	src := setupStore(t)
	blobs := filestore.NewLocal(t.TempDir())
	seed(t, src, blobs)
	b := backup(t, src, blobs, Options{Secrets: true})
	dst := emptyStore(t, envDialect())
	sum, err := Restore(context.Background(), dst, filestore.NewLocal(t.TempDir()), opener(b), RestoreOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if hasKind(sum.Kinds, KindAuth) || len(sum.Warnings) != 2 {
		t.Errorf("kinds %v, warnings %q", sum.Kinds, sum.Warnings)
	}
	if n := count(t, dst, "sessions"); n != 0 {
		t.Errorf("%d sessions restored", n)
	}
	var hash, options string
	if err := dst.QueryRow("SELECT password_hash FROM users").Scan(&hash); err != nil || hash != "!" {
		t.Errorf("password hash %q, %v", hash, err)
	}
	if err := dst.QueryRow("SELECT options FROM notification_channels").Scan(&options); err != nil || options != "{}" {
		t.Errorf("channel options %q, %v", options, err)
	}
	if signsIn(t, dst, "ada@example.com", "correct horse battery") {
		t.Error("signed in with a password not restored")
	}
}

// member is a member of an archive being rewritten.
type member struct {
	name string
	data []byte
}

func unpack(t *testing.T, b []byte) []member {
	t.Helper()
	gz, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(gz)
	var out []member
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return out
		}
		if err != nil {
			t.Fatal(err)
		}
		data, _ := io.ReadAll(tr)
		out = append(out, member{h.Name, data})
	}
}

func pack(t *testing.T, ms []member) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, m := range ms {
		if err := tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: m.name, Size: int64(len(m.data)), Mode: 0o600}); err != nil {
			t.Fatal(err)
		}
		tw.Write(m.data)
	}
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

// reseal writes the manifest again for members changed, as someone
// altering an archive with care would.
func reseal(t *testing.T, ms []member, change func(h *Header, man *Manifest)) []member {
	t.Helper()
	var hdr Header
	var man Manifest
	json.Unmarshal(ms[0].data, &hdr)
	json.Unmarshal(ms[len(ms)-1].data, &man)
	if change != nil {
		change(&hdr, &man)
	}
	ms[0].data, _ = json.Marshal(hdr)
	man.Header.FormatVersion, man.Header.SchemaVersion = hdr.FormatVersion, hdr.SchemaVersion
	man.Members = map[string]Member{}
	for _, m := range ms[:len(ms)-1] {
		sum := sha256.Sum256(m.data)
		man.Members[m.name] = Member{Size: int64(len(m.data)), SHA256: hex.EncodeToString(sum[:])}
	}
	ms[len(ms)-1].data, _ = json.Marshal(man)
	return ms
}

// TestRestoreChecksFirst: an archive that does not check out is refused
// before anything is changed.
func TestRestoreChecksFirst(t *testing.T) {
	src := setupStore(t)
	blobs := filestore.NewLocal(t.TempDir())
	seed(t, src, blobs)
	good := backup(t, src, blobs, Options{Secrets: true})
	if _, err := Check(bytes.NewReader(good)); err != nil {
		t.Fatalf("a good archive: %v", err)
	}
	find := func(ms []member, prefix string) int {
		for i, m := range ms {
			if strings.HasPrefix(m.name, prefix) {
				return i
			}
		}
		t.Fatalf("no member %s", prefix)
		return -1
	}
	cases := map[string]func() []byte{
		"empty":     func() []byte { return nil },
		"not gzip":  func() []byte { return []byte("SQLite format 3\x00") },
		"truncated": func() []byte { return good[:len(good)-200] },
		"padded":    func() []byte { return append(append([]byte{}, good...), "more"...) },
		"two archives": func() []byte {
			return append(append([]byte{}, good...), good...)
		},
		"a byte flipped": func() []byte {
			// Some bits of a deflate stream decode to the same bytes either
			// way, so flipping one changes nothing: flip one that does.
			for i := len(good) / 2; i < len(good); i++ {
				b := append([]byte{}, good...)
				b[i] ^= 0x40
				if !sameInflated(good, b) {
					return b
				}
			}
			t.Fatal("no byte changes what the archive holds")
			return nil
		},
		"a row changed": func() []byte {
			ms := unpack(t, good)
			i := find(ms, DataDir+"users/")
			ms[i].data = bytes.Replace(ms[i].data, []byte("ada@"), []byte("eve@"), 1)
			return pack(t, ms)
		},
		"a row added, the manifest made again": func() []byte {
			ms := unpack(t, good)
			i := find(ms, DataDir+"scopes/")
			ms[i].data = append(ms[i].data, ms[i].data...)
			return pack(t, reseal(t, ms, nil))
		},
		"reordered": func() []byte {
			ms := unpack(t, good)
			ms[0], ms[1] = ms[1], ms[0]
			return pack(t, ms)
		},
		"the manifest not last": func() []byte {
			ms := unpack(t, good)
			last := ms[len(ms)-1]
			copy(ms[3:], ms[2:len(ms)-1])
			ms[2] = last
			return pack(t, ms)
		},
		"a member not in the manifest": func() []byte {
			ms := unpack(t, good)
			ms = append(ms[:len(ms)-1], member{FilesDir + "images/" + uuid.NewString(), []byte("x")}, ms[len(ms)-1])
			return pack(t, ms)
		},
		"a member outside it": func() []byte {
			ms := unpack(t, good)
			ms = append(ms[:len(ms)-1], member{"../evil", []byte("x")}, ms[len(ms)-1])
			return pack(t, reseal(t, ms, nil))
		},
		"no manifest": func() []byte {
			ms := unpack(t, good)
			return pack(t, ms[:len(ms)-1])
		},
		"a newer schema": func() []byte {
			return pack(t, reseal(t, unpack(t, good), func(h *Header, _ *Manifest) { h.SchemaVersion = migrations.Latest() + 1 }))
		},
		"a newer format": func() []byte {
			return pack(t, reseal(t, unpack(t, good), func(h *Header, _ *Manifest) { h.FormatVersion = FormatVersion + 1 }))
		},
	}
	for name, make := range cases {
		t.Run(name, func(t *testing.T) {
			dst := emptyStore(t, envDialect())
			dstBlobs := filestore.NewLocal(t.TempDir())
			_, err := Restore(context.Background(), dst, dstBlobs, opener(make()), RestoreOptions{Secrets: true})
			if !errors.Is(err, ErrInvalid) && !errors.Is(err, ErrNewer) {
				t.Fatalf("restored: %v", err)
			}
			if strings.HasPrefix(name, "a newer") != errors.Is(err, ErrNewer) {
				t.Errorf("refused with %v", err)
			}
			if v, err := schemaVersion(dst); v != 0 || err != nil {
				t.Errorf("the database was changed: version %d, %v", v, err)
			}
			n := 0
			dstBlobs.List(context.Background(), "", func(filestore.Object) error { n++; return nil })
			if n != 0 {
				t.Errorf("%d files were stored", n)
			}
		})
	}
}

// TestAnOlderSchemaIsRestoredThenMigrated: an archive of schema 40 goes
// into a database migrated to 40, and is migrated after.
func TestAnOlderSchemaIsRestoredThenMigrated(t *testing.T) {
	src := setupStore(t)
	blobs := filestore.NewLocal(t.TempDir())
	richSeed(t, src, blobs)
	// No migration after 40 changes the tables of labels.
	b := pack(t, reseal(t, unpack(t, backup(t, src, blobs, Options{Kinds: []Kind{KindLabels}})), func(h *Header, _ *Manifest) { h.SchemaVersion = 40 }))
	dst := emptyStore(t, envDialect())
	dstBlobs := filestore.NewLocal(t.TempDir())
	sum, err := Restore(context.Background(), dst, dstBlobs, opener(b), RestoreOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := schemaVersion(dst); v != migrations.Latest() || !sum.Migrated {
		t.Errorf("schema %d, migrated %v", v, sum.Migrated)
	}
	if count(t, dst, "templates") != count(t, src, "templates") || count(t, dst, "images") != 2 {
		t.Errorf("%d templates, %d images restored", count(t, dst, "templates"), count(t, dst, "images"))
	}

	// One into a database already past it is refused.
	_, err = Restore(context.Background(), migratedStore(t, envDialect()), dstBlobs, opener(b), RestoreOptions{})
	if !errors.Is(err, ErrSchema) {
		t.Errorf("into a newer schema: %v", err)
	}
}

// TestAThemeIsRestored: a person's theme comes back with them, and from an
// archive written before people chose one (schema 47, bam v0.16.0), whose
// users have no theme column, they follow their system's.
func TestAThemeIsRestored(t *testing.T) {
	src := setupStore(t)
	blobs := filestore.NewLocal(t.TempDir())
	s := richSeed(t, src, blobs)
	b := backup(t, src, blobs, Options{Kinds: []Kind{KindPeople}})
	themeOf := func(st *store.Store) string {
		t.Helper()
		u, err := st.GetUserByID(s.user.ID)
		if err != nil {
			t.Fatal(err)
		}
		return u.Theme
	}

	dst := emptyStore(t, envDialect())
	if _, err := Restore(context.Background(), dst, filestore.NewLocal(t.TempDir()), opener(b), RestoreOptions{}); err != nil {
		t.Fatal(err)
	}
	if got := themeOf(dst); got != "dark" {
		t.Errorf("restored with %q", got)
	}

	// The same archive as v0.16.0 wrote it: no theme in its users' rows.
	ms := unpack(t, b)
	stripped := 0
	for i, m := range ms {
		if !strings.HasPrefix(m.name, DataDir+"users/") {
			continue
		}
		var out bytes.Buffer
		for _, line := range strings.Split(strings.TrimSuffix(string(m.data), "\n"), "\n") {
			var row map[string]json.RawMessage
			if err := json.Unmarshal([]byte(line), &row); err != nil {
				t.Fatal(err)
			}
			if _, ok := row["theme"]; !ok {
				t.Fatalf("a users row has no theme: %s", line)
			}
			delete(row, "theme")
			stripped++
			data, _ := json.Marshal(row)
			out.Write(append(data, '\n'))
		}
		ms[i].data = out.Bytes()
	}
	if stripped == 0 {
		t.Fatal("the archive has no users rows")
	}
	old := pack(t, reseal(t, ms, func(h *Header, _ *Manifest) { h.SchemaVersion = 47 }))
	dst = emptyStore(t, envDialect())
	sum, err := Restore(context.Background(), dst, filestore.NewLocal(t.TempDir()), opener(old), RestoreOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := schemaVersion(dst); v != migrations.Latest() || !sum.Migrated || sum.Tables["users"] != 1 {
		t.Errorf("schema %d, summary %+v", v, sum)
	}
	if got := themeOf(dst); got != "system" {
		t.Errorf("restored from before themes with %q", got)
	}

	// One at this schema whose rows lack it is restored the same way.
	at := pack(t, reseal(t, unpack(t, old), func(h *Header, _ *Manifest) { h.SchemaVersion = migrations.Latest() }))
	dst = emptyStore(t, envDialect())
	if _, err := Restore(context.Background(), dst, filestore.NewLocal(t.TempDir()), opener(at), RestoreOptions{}); err != nil {
		t.Fatal(err)
	}
	if got := themeOf(dst); got != "system" {
		t.Errorf("restored without a theme with %q", got)
	}
}

func TestARestoreRefusesAnInstanceInUse(t *testing.T) {
	src := setupStore(t)
	blobs := filestore.NewLocal(t.TempDir())
	seed(t, src, blobs)
	b := backup(t, src, blobs, Options{Secrets: true})
	dst := migratedStore(t, envDialect())
	if _, err := dst.CreateUser("bob@example.com", "another long password", true); err != nil {
		t.Fatal(err)
	}
	if _, err := Restore(context.Background(), dst, filestore.NewLocal(t.TempDir()), opener(b), RestoreOptions{Secrets: true}); !errors.Is(err, ErrNotEmpty) {
		t.Fatalf("restored into an instance in use: %v", err)
	}
	if n := count(t, dst, "users"); n != 1 {
		t.Errorf("%d users", n)
	}
}

// TestSomeKindsAreRestored: the kinds chosen are restored and the rest
// left as they were; a kind naming what the instance does not have is
// refused, changing nothing.
func TestSomeKindsAreRestored(t *testing.T) {
	ctx := context.Background()
	src := setupStore(t)
	blobs := filestore.NewLocal(t.TempDir())
	s := richSeed(t, src, blobs)
	b := backup(t, src, blobs, Options{Secrets: true})

	dst := migratedStore(t, envDialect())
	dstBlobs := filestore.NewLocal(t.TempDir())
	before := contents(t, backup(t, dst, dstBlobs, Options{Secrets: true}))
	sum, err := Restore(ctx, dst, dstBlobs, opener(b), RestoreOptions{Kinds: []Kind{KindLabels}, Secrets: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(sum.Kinds) != 1 || sum.Files != 1 {
		t.Errorf("summary: %+v", sum)
	}
	after := contents(t, backup(t, dst, dstBlobs, Options{Secrets: true}))
	want := contents(t, b)
	for k, v := range after {
		labels := strings.HasSuffix(k, "/templates") || strings.HasSuffix(k, "/images") || strings.HasPrefix(k, FilesDir+"images/")
		if labels && v != want[k] || !labels && v != before[k] {
			t.Errorf("%s is not as it should be", k)
		}
	}
	if _, err := dstBlobs.Get(ctx, "photos/"+s.photo.ID); err == nil {
		t.Error("a photo was restored with labels")
	}

	// Assets name categories this instance does not have.
	_, err = Restore(ctx, dst, dstBlobs, opener(b), RestoreOptions{Kinds: []Kind{KindAssets}, Secrets: true})
	if err == nil {
		t.Fatal("restored assets without their categories")
	}
	t.Logf("refused: %v", err)
	sameContents(t, after, contents(t, backup(t, dst, dstBlobs, Options{Secrets: true})))

	if _, err := Restore(ctx, dst, dstBlobs, opener(backup(t, src, blobs, Options{Kinds: []Kind{KindReports}})), RestoreOptions{Kinds: []Kind{KindFlows}}); err == nil {
		t.Error("restored a kind the archive does not hold")
	}
}

// TestASplitSetIsRestoredPartByPart: the first part restores the rows, and
// the others, in any order, the files; the sweep lists those still to come.
func TestASplitSetIsRestoredPartByPart(t *testing.T) {
	ctx := context.Background()
	src := setupStore(t)
	blobs := filestore.NewLocal(t.TempDir())
	cat, err := src.CreateCategory("Tools", nil, "#336699", false, false)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		asset, err := src.CreateAsset("Drill "+strconv.Itoa(i), []string{cat.ID}, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		p := models.AssetPhoto{ID: uuid.NewString(), ThumbID: uuid.NewString(), Filename: "x.jpg", ContentType: "image/jpeg", WidthPx: 1, HeightPx: 1, ByteSize: 200 << 10, CreatedAt: time.Now()}
		for j, id := range []string{p.ID, p.ThumbID} {
			if err := blobs.Put(ctx, "photos/"+id, noise(int64(i*2+j), 200<<10), 200<<10, ""); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := src.AddPhoto(asset.Asset.ID, p); err != nil {
			t.Fatal(err)
		}
	}
	var set memParts
	sum, err := Write(ctx, src, blobs, set.open, Options{Split: 700 << 10, Secrets: true})
	if err != nil {
		t.Fatal(err)
	}
	if sum.Parts < 3 {
		t.Fatalf("%d parts", sum.Parts)
	}
	whole := contents(t, backup(t, src, blobs, Options{Secrets: true}))

	dst := emptyStore(t, envDialect())
	dstBlobs := filestore.NewLocal(t.TempDir())
	sweepMissing := func() int {
		named, err := dst.StoredFiles()
		if err != nil {
			t.Fatal(err)
		}
		res, err := filestore.Sweep(ctx, dstBlobs, named, filestore.SweepGrace, time.Now(), true)
		if err != nil {
			t.Fatal(err)
		}
		return len(res.Missing)
	}
	if _, err := Restore(ctx, dst, dstBlobs, opener(set.parts[2].Bytes()), RestoreOptions{Secrets: true}); err == nil {
		t.Error("a later part was applied before the first")
	}
	if _, err := Restore(ctx, dst, dstBlobs, opener(set.parts[1].Bytes()), RestoreOptions{Secrets: true}); err != nil {
		t.Fatal(err)
	}
	missing := sweepMissing()
	if missing == 0 {
		t.Fatal("the sweep lists no files to come")
	}
	order := rand.Perm(sum.Parts - 1)
	for _, i := range order {
		res, err := Restore(ctx, dst, dstBlobs, opener(set.parts[i+2].Bytes()), RestoreOptions{})
		if err != nil {
			t.Fatalf("part %d: %v", i+2, err)
		}
		if now := sweepMissing(); now != missing-res.Files {
			t.Errorf("after part %d, the sweep lists %d missing, not %d", i+2, now, missing-res.Files)
		} else {
			missing = now
		}
	}
	if missing != 0 {
		t.Errorf("%d files still missing", missing)
	}
	again, err := Restore(ctx, dst, dstBlobs, opener(set.parts[2].Bytes()), RestoreOptions{})
	if err != nil || again.Files != 0 || again.Already == 0 {
		t.Errorf("a part applied twice: %+v, %v", again, err)
	}
	sameContents(t, whole, contents(t, backup(t, dst, dstBlobs, Options{Secrets: true})))

	// A part of another set names no file here.
	var other memParts
	if _, err := Write(ctx, src, blobs, other.open, Options{Split: 700 << 10}); err != nil {
		t.Fatal(err)
	}
	if _, err := dst.Exec("DELETE FROM asset_photos"); err != nil {
		t.Fatal(err)
	}
	if _, err := Restore(ctx, dst, dstBlobs, opener(other.parts[2].Bytes()), RestoreOptions{}); err == nil {
		t.Error("a part of another set was applied")
	}
}

// sinkBlobs stores nothing, reading what is put whole.
type sinkBlobs struct{ n atomic.Int64 }

func (s *sinkBlobs) Put(_ context.Context, _ string, r io.Reader, _ int64, _ string) error {
	n, err := io.Copy(io.Discard, r)
	s.n.Add(n)
	return err
}
func (s *sinkBlobs) Get(context.Context, string) (io.ReadCloser, error) {
	return nil, filestore.ErrNotFound
}
func (s *sinkBlobs) Delete(context.Context, string) error { return nil }
func (s *sinkBlobs) List(context.Context, string, func(filestore.Object) error) error {
	return nil
}

type pipeCloser struct{ *io.PipeWriter }

// TestRestoringDoesNotGrowMemory restores an archive written as it is
// read, of files made on the fly: BAM_TEST_ARCHIVE_BYTES of them.
func TestRestoringDoesNotGrowMemory(t *testing.T) {
	total := int64(256 << 20)
	if s := os.Getenv(archiveBytesEnv); s != "" {
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			t.Fatal(err)
		}
		total = n
	}
	src := setupStore(t)
	cat, err := src.CreateCategory("Tools", nil, "#336699", false, false)
	if err != nil {
		t.Fatal(err)
	}
	g := &genBlobs{sizes: map[string]int64{}}
	size := total / 20
	for i := 0; i < 10; i++ {
		asset, err := src.CreateAsset("Drill "+strconv.Itoa(i), []string{cat.ID}, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		p := models.AssetPhoto{ID: uuid.NewString(), ThumbID: uuid.NewString(), Filename: "x.jpg", ContentType: "image/jpeg", WidthPx: 1, HeightPx: 1, ByteSize: size, CreatedAt: time.Now()}
		g.sizes["photos/"+p.ID], g.sizes["photos/"+p.ThumbID] = size, size
		if _, err := src.AddPhoto(asset.Asset.ID, p); err != nil {
			t.Fatal(err)
		}
	}
	// The same archive each time it is opened: the set's ID and the time
	// are fixed.
	setID := uuid.NewString()
	open := func() (io.ReadCloser, error) {
		pr, pw := io.Pipe()
		go func() {
			_, err := Write(context.Background(), src, g, func(int) (io.WriteCloser, error) { return pipeCloser{pw}, nil },
				Options{Now: time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC), setID: setID})
			pw.CloseWithError(err)
		}()
		return pr, nil
	}

	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)
	var peak atomic.Uint64
	stop, sampled := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(sampled)
		var m runtime.MemStats
		for {
			select {
			case <-stop:
				return
			case <-time.After(20 * time.Millisecond):
			}
			runtime.ReadMemStats(&m)
			if m.HeapInuse > peak.Load() {
				peak.Store(m.HeapInuse)
			}
		}
	}()
	dst := emptyStore(t, envDialect())
	sink := &sinkBlobs{}
	start := time.Now()
	sum, err := Restore(context.Background(), dst, sink, open, RestoreOptions{})
	close(stop)
	<-sampled
	if err != nil {
		t.Fatal(err)
	}
	if sum.FileBytes != size*20 || sink.n.Load() != size*20 {
		t.Fatalf("restored %d bytes of files; want %d", sum.FileBytes, size*20)
	}
	grew := int64(peak.Load()) - int64(before.HeapInuse)
	t.Logf("%d bytes of files checked and restored in %v; the heap grew by at most %d bytes", sum.FileBytes, time.Since(start), grew)
	if grew > 64<<20 {
		t.Errorf("the heap grew by %d bytes restoring %d", grew, sum.FileBytes)
	}
}

// signsIn reports whether a password matches a person's stored hash.
func signsIn(t *testing.T, st *store.Store, email, password string) bool {
	t.Helper()
	var hash string
	if err := st.QueryRow("SELECT password_hash FROM users WHERE email = ?", email).Scan(&hash); err != nil {
		t.Fatal(err)
	}
	return auth.CheckPassword(password, hash)
}

// layoutJSON is a template's layout holding text.
func layoutJSON(text string) string {
	b, _ := json.Marshal(map[string]any{"layers": []any{map[string]string{"text": text}}})
	return string(b)
}

// sameInflated reports whether two gzip streams inflate to the same bytes,
// whole and checked.
func sameInflated(a, b []byte) bool {
	inflate := func(x []byte) ([]byte, error) {
		gz, err := gzip.NewReader(bytes.NewReader(x))
		if err != nil {
			return nil, err
		}
		return io.ReadAll(gz)
	}
	x, err := inflate(a)
	if err != nil {
		return false
	}
	y, err := inflate(b)
	return err == nil && bytes.Equal(x, y)
}
