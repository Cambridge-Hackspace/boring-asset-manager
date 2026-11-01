package archive

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/filestore"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store/migrations"
)

func setupStore(t testing.TB) *store.Store {
	t.Helper()
	var st *store.Store
	if dialect := os.Getenv(store.TestDialectEnv); dialect != "" && dialect != "sqlite" {
		ephemeral, cleanup, err := store.OpenEphemeral(dialect, os.Getenv(store.TestDSNEnv))
		if err != nil {
			t.Fatalf("opening ephemeral %s store: %v", dialect, err)
		}
		t.Cleanup(cleanup)
		st = ephemeral
	} else {
		opened, err := store.OpenMigratedSQLite(":memory:")
		if err != nil {
			t.Fatalf("opening test store: %v", err)
		}
		t.Cleanup(func() { _ = opened.Close() })
		st = opened
	}
	if err := st.Migrate(); err != nil {
		t.Fatalf("migrating: %v", err)
	}
	if err := st.SeedDefaultPermissionsAndRoles(); err != nil {
		t.Fatalf("seeding permissions: %v", err)
	}
	return st
}

// dbTables lists the tables the migrations made.
func dbTables(t *testing.T, st *store.Store) []string {
	t.Helper()
	q := "SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%'"
	switch st.Dialect {
	case "postgres":
		q = "SELECT table_name FROM information_schema.tables WHERE table_schema = current_schema() AND table_type = 'BASE TABLE'"
	case "mariadb", "mysql":
		q = "SELECT table_name FROM information_schema.tables WHERE table_schema = DATABASE() AND table_type = 'BASE TABLE'"
	}
	rows, err := st.Query(q)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		out = append(out, name)
	}
	return out
}

func TestTheRegistryMatchesTheSchema(t *testing.T) {
	st := setupStore(t)
	byName := map[string]*Table{}
	for i := range Tables {
		byName[Tables[i].Name] = &Tables[i]
	}
	seen := map[string]bool{}
	for _, name := range dbTables(t, st) {
		seen[name] = true
		if _, ok := Excluded[name]; ok {
			if byName[name] != nil {
				t.Errorf("%s is both archived and excluded", name)
			}
			continue
		}
		table := byName[name]
		if table == nil {
			t.Errorf("%s is neither archived nor excluded: run tools/archive/tables.py, or add it to Excluded", name)
			continue
		}
		rows, err := st.Query("SELECT * FROM " + name + " WHERE 1 = 0")
		if err != nil {
			t.Fatal(err)
		}
		cols, _ := rows.Columns()
		rows.Close()
		var want []string
		for _, c := range table.Columns {
			n := c.Name
			if table.ByPermissionKey && n == "permission_key" {
				n = "permission_id"
			}
			want = append(want, n)
		}
		sort.Strings(cols)
		sort.Strings(want)
		if strings.Join(cols, ",") != strings.Join(want, ",") {
			t.Errorf("%s has columns %v; the registry has %v: run tools/archive/tables.py", name, cols, want)
		}
		for _, s := range table.Secret {
			if colIndexOK(table, s) < 0 {
				t.Errorf("%s's secret column %s is not a column", name, s)
			}
		}
	}
	for name := range byName {
		if !seen[name] {
			t.Errorf("the registry has %s, which the migrations do not make", name)
		}
	}
	for name := range Excluded {
		if !seen[name] {
			t.Errorf("Excluded has %s, which the migrations do not make", name)
		}
	}
}

func TestTheDocsDocumentEveryTable(t *testing.T) {
	b, err := os.ReadFile("../../docs/backups.md")
	if err != nil {
		t.Fatal(err)
	}
	doc := string(b)
	for _, table := range Tables {
		if !strings.Contains(doc, "`"+table.Name+"`") {
			t.Errorf("docs/backups.md does not name %s", table.Name)
		}
	}
	for name := range Excluded {
		if !strings.Contains(doc, "`"+name+"`") {
			t.Errorf("docs/backups.md does not say why %s is left out", name)
		}
	}
}

// read is an archive read back: its members in order, each checked
// against the manifest.
type read struct {
	header   Header
	manifest Manifest
	order    []string
	members  map[string][]byte
}

func readArchive(t *testing.T, b []byte) *read {
	t.Helper()
	gz, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(gz)
	r := &read{members: map[string][]byte{}}
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			t.Fatal(err)
		}
		r.order = append(r.order, h.Name)
		r.members[h.Name] = data
	}
	if len(r.order) < 3 || r.order[0] != HeaderPath || r.order[1] != ReadmePath || r.order[len(r.order)-1] != ManifestPath {
		t.Fatalf("members in the wrong order: %v", r.order)
	}
	if err := json.Unmarshal(r.members[HeaderPath], &r.header); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(r.members[ManifestPath], &r.manifest); err != nil {
		t.Fatal(err)
	}
	if len(r.manifest.Members) != len(r.order)-1 {
		t.Errorf("the manifest lists %d members; the archive has %d besides it", len(r.manifest.Members), len(r.order)-1)
	}
	for path, m := range r.manifest.Members {
		data, ok := r.members[path]
		if !ok {
			t.Errorf("the manifest lists %s, which is not there", path)
			continue
		}
		sum := sha256.Sum256(data)
		if int64(len(data)) != m.Size || hex.EncodeToString(sum[:]) != m.SHA256 {
			t.Errorf("%s does not match the manifest", path)
		}
	}
	return r
}

// rows reads a table's rows from a directory of the archive.
func (r *read) rows(t *testing.T, dir string) []map[string]any {
	t.Helper()
	var paths []string
	for p := range r.members {
		if strings.HasPrefix(p, dir) {
			paths = append(paths, p)
		}
	}
	sort.Strings(paths)
	var out []map[string]any
	for _, p := range paths {
		sc := bufio.NewScanner(bytes.NewReader(r.members[p]))
		sc.Buffer(nil, RowChunk+1)
		for sc.Scan() {
			var row map[string]any
			if err := json.Unmarshal(sc.Bytes(), &row); err != nil {
				t.Fatalf("%s: %v", p, err)
			}
			out = append(out, row)
		}
	}
	return out
}

// memParts collects the parts Write writes.
type memParts struct {
	mu    sync.Mutex
	parts map[int]*closingBuffer
}

type closingBuffer struct {
	bytes.Buffer
	closed bool
}

func (b *closingBuffer) Close() error { b.closed = true; return nil }

func (m *memParts) open(n int) (io.WriteCloser, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.parts == nil {
		m.parts = map[int]*closingBuffer{}
	}
	b := &closingBuffer{}
	m.parts[n] = b
	return b, nil
}

// seed fills a store with a little of most things, and files for it.
type seeded struct {
	user    *models.User
	photo   models.AssetPhoto
	image   models.Image
	missing string
}

func seed(t *testing.T, st *store.Store, blobs filestore.Blobs) seeded {
	t.Helper()
	ctx := context.Background()
	var s seeded
	var err error
	if s.user, err = st.CreateUser("ada@example.com", "correct horse battery", true); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateSession(s.user.ID, time.Hour); err != nil {
		t.Fatal(err)
	}
	cat, err := st.CreateCategory("Tools", nil, "#336699", false, false)
	if err != nil {
		t.Fatal(err)
	}
	asset, err := st.CreateAsset("Drill", []string{cat.ID}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	s.photo = models.AssetPhoto{ID: uuid.NewString(), ThumbID: uuid.NewString(), Filename: "drill.jpg", ContentType: "image/jpeg", WidthPx: 4, HeightPx: 3, ByteSize: 5, CreatedAt: time.Now()}
	for _, id := range []string{s.photo.ID, s.photo.ThumbID} {
		if err := blobs.Put(ctx, "photos/"+id, strings.NewReader("photo "+id), -1, ""); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := st.AddPhoto(asset.Asset.ID, s.photo); err != nil {
		t.Fatal(err)
	}
	s.image = models.Image{ID: uuid.NewString(), Filename: "logo.png", ContentType: "image/png", WidthPx: 1, HeightPx: 1, ByteSize: 3, SHA256: "x", CreatedAt: time.Now()}
	if err := st.CreateImage(&s.image); err != nil {
		t.Fatal(err)
	}
	if err := blobs.Put(ctx, "images/"+s.image.ID, strings.NewReader("png"), -1, ""); err != nil {
		t.Fatal(err)
	}
	// A label image whose file is gone.
	gone := models.Image{ID: uuid.NewString(), Filename: "gone.png", ContentType: "image/png", WidthPx: 1, HeightPx: 1, ByteSize: 3, SHA256: "y", CreatedAt: time.Now()}
	if err := st.CreateImage(&gone); err != nil {
		t.Fatal(err)
	}
	s.missing = "images/" + gone.ID
	// A file no row names, which is not archived.
	if err := blobs.Put(ctx, "images/"+uuid.NewString(), strings.NewReader("orphan"), -1, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateChannel(models.NotificationChannel{Name: "ops", Driver: "fake", Options: map[string]string{"password": "hunter2"}, Enabled: true, Events: []string{"loan.overdue"}}); err != nil {
		t.Fatal(err)
	}
	return s
}

func count(t *testing.T, st *store.Store, table string) int {
	t.Helper()
	var n int
	if err := st.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestABackupHoldsEveryRowAndFile(t *testing.T) {
	st := setupStore(t)
	blobs := filestore.NewLocal(t.TempDir())
	s := seed(t, st, blobs)
	var parts memParts
	sum, err := Write(context.Background(), st, blobs, parts.open, Options{BamVersion: "v9.9.9", Secrets: true, Now: time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	if sum.Parts != 1 || len(parts.parts) != 1 || !parts.parts[1].closed {
		t.Fatalf("wrote %d parts (%+v)", len(parts.parts), sum)
	}
	r := readArchive(t, parts.parts[1].Bytes())

	h := r.header
	if h.Format != Format || h.FormatVersion != FormatVersion || h.Type != "backup" || h.BamVersion != "v9.9.9" ||
		h.CreatedAt != "2026-10-29T12:00:00Z" || h.Dialect != st.Dialect || !h.Secrets || h.SchemaVersion < 45 ||
		h.Set.Part != 1 || h.Set.ID == "" || len(h.Kinds) != len(Kinds) {
		t.Errorf("header: %+v", h)
	}
	if !r.manifest.Last || r.manifest.Set.Parts != 1 || r.manifest.Set.ID != h.Set.ID {
		t.Errorf("manifest: last %v, set %+v", r.manifest.Last, r.manifest.Set)
	}

	for _, table := range Tables {
		dir := DataDir + table.Name + "/"
		if table.Kind == KindAuth {
			dir = AuthDir + table.Name + "/"
		}
		want := count(t, st, table.Name)
		if got := len(r.rows(t, dir)); got != want || r.manifest.Tables[table.Name] != want {
			t.Errorf("%s: %d rows archived, %d in the manifest; the database has %d", table.Name, got, r.manifest.Tables[table.Name], want)
		}
	}

	users := r.rows(t, DataDir+"users/")
	if len(users) != 1 || users[0]["id"] != s.user.ID || users[0]["is_active"] != true {
		t.Fatalf("users: %v", users)
	}
	if _, ok := users[0]["password_hash"]; ok {
		t.Error("a password hash is in data/")
	}
	if at, _ := users[0]["created_at"].(string); !strings.HasSuffix(at, "Z") {
		t.Errorf("created_at is %q, not RFC 3339 in UTC", users[0]["created_at"])
	} else if _, err := time.Parse(time.RFC3339Nano, at); err != nil {
		t.Error(err)
	}
	secrets := r.rows(t, AuthDir+"users/")
	if len(secrets) != 1 || secrets[0]["id"] != s.user.ID || !strings.HasPrefix(fmt.Sprint(secrets[0]["password_hash"]), "$") || len(secrets[0]) != 3 {
		t.Errorf("auth/users: %v", secrets)
	}
	channels := r.rows(t, DataDir+"notification_channels/")
	if len(channels) != 1 || channels[0]["options"] != nil {
		t.Errorf("notification_channels in data/: %v", channels)
	}
	if ev, ok := channels[0]["events"].([]any); !ok || len(ev) != 1 {
		t.Errorf("events is written as %T, not as JSON", channels[0]["events"])
	}
	chSecrets := r.rows(t, AuthDir+"notification_channels/")
	if opts, _ := chSecrets[0]["options"].(map[string]any); opts["password"] != "hunter2" {
		t.Errorf("auth/notification_channels: %v", chSecrets)
	}
	grants := r.rows(t, DataDir+"role_permissions/")
	if len(grants) == 0 || grants[0]["permission_key"] == nil || grants[0]["permission_id"] != nil {
		t.Errorf("role_permissions: %v", grants[:1])
	}

	for _, key := range []string{"photos/" + s.photo.ID, "photos/" + s.photo.ThumbID, "images/" + s.image.ID} {
		if _, ok := r.members[FilesDir+key]; !ok {
			t.Errorf("%s is not archived", key)
		}
	}
	if got := r.members[FilesDir+"images/"+s.image.ID]; string(got) != "png" {
		t.Errorf("the image holds %q", got)
	}
	if r.manifest.Files != 3 || sum.Files != 3 {
		t.Errorf("%d files archived", r.manifest.Files)
	}
	if len(r.manifest.Missing) != 1 || r.manifest.Missing[0] != s.missing {
		t.Errorf("missing: %v", r.manifest.Missing)
	}
}

func TestAWebBackupHoldsNoSecrets(t *testing.T) {
	st := setupStore(t)
	blobs := filestore.NewLocal(t.TempDir())
	seed(t, st, blobs)
	var parts memParts
	if _, err := Write(context.Background(), st, blobs, parts.open, Options{}); err != nil {
		t.Fatal(err)
	}
	r := readArchive(t, parts.parts[1].Bytes())
	if r.header.Secrets || hasKind(r.header.Kinds, KindAuth) {
		t.Errorf("header: %+v", r.header)
	}
	for _, p := range r.order {
		if strings.HasPrefix(p, AuthDir) {
			t.Errorf("%s is archived", p)
		}
	}
	for path, b := range r.members {
		if strings.Contains(string(b), "hunter2") || strings.Contains(string(b), "$argon2") || strings.Contains(string(b), "$2a$") {
			t.Errorf("%s holds a secret", path)
		}
	}
	for _, table := range Tables {
		if _, ok := r.manifest.Tables[table.Name]; ok == (table.Kind == KindAuth) {
			t.Errorf("%s: in the manifest %v", table.Name, ok)
		}
	}
}

func TestSomeKinds(t *testing.T) {
	st := setupStore(t)
	blobs := filestore.NewLocal(t.TempDir())
	s := seed(t, st, blobs)
	var parts memParts
	if _, err := Write(context.Background(), st, blobs, parts.open, Options{Kinds: []Kind{KindLabels, KindReports}}); err != nil {
		t.Fatal(err)
	}
	r := readArchive(t, parts.parts[1].Bytes())
	if fmt.Sprint(r.header.Kinds) != "[labels reports]" || r.header.Secrets {
		t.Errorf("header: %+v", r.header)
	}
	for _, p := range r.order {
		if strings.HasPrefix(p, DataDir) && !strings.HasPrefix(p, DataDir+"images/") && !strings.HasPrefix(p, DataDir+"templates/") &&
			!strings.HasPrefix(p, DataDir+"reports/") && !strings.HasPrefix(p, DataDir+"category_label_templates/") {
			t.Errorf("%s is archived", p)
		}
		if strings.HasPrefix(p, FilesDir+"photos/") {
			t.Errorf("%s is archived without assets", p)
		}
	}
	if _, ok := r.members[FilesDir+"images/"+s.image.ID]; !ok {
		t.Error("the label image is not archived")
	}
}

func TestASplitBackup(t *testing.T) {
	st := setupStore(t)
	blobs := filestore.NewLocal(t.TempDir())
	ctx := context.Background()
	cat, err := st.CreateCategory("Tools", nil, "#336699", false, false)
	if err != nil {
		t.Fatal(err)
	}
	const fileSize = 300 << 10
	want := map[string]bool{}
	for i := 0; i < 4; i++ {
		asset, err := st.CreateAsset("Drill "+strconv.Itoa(i), []string{cat.ID}, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		p := models.AssetPhoto{ID: uuid.NewString(), ThumbID: uuid.NewString(), Filename: "x.jpg", ContentType: "image/jpeg", WidthPx: 1, HeightPx: 1, ByteSize: fileSize, CreatedAt: time.Now()}
		for _, id := range []string{p.ID, p.ThumbID} {
			if err := blobs.Put(ctx, "photos/"+id, noise(int64(len(want)), fileSize), fileSize, ""); err != nil {
				t.Fatal(err)
			}
			want["photos/"+id] = true
		}
		if _, err := st.AddPhoto(asset.Asset.ID, p); err != nil {
			t.Fatal(err)
		}
	}
	const split = 1 << 20
	var parts memParts
	sum, err := Write(ctx, st, blobs, parts.open, Options{Split: split, Secrets: true})
	if err != nil {
		t.Fatal(err)
	}
	if sum.Parts < 3 || len(parts.parts) != sum.Parts {
		t.Fatalf("%d parts, %d written", sum.Parts, len(parts.parts))
	}
	got := map[string]bool{}
	for n := 1; n <= sum.Parts; n++ {
		b := parts.parts[n]
		if !b.closed {
			t.Errorf("part %d is not closed", n)
		}
		if b.Len() > split {
			t.Errorf("part %d is %d bytes, over %d", n, b.Len(), split)
		}
		r := readArchive(t, b.Bytes())
		if r.header.Set.ID != sum.Set || r.header.Set.Part != n || r.manifest.Set.Parts != sum.Parts || r.manifest.Last != (n == sum.Parts) {
			t.Errorf("part %d: header %+v, manifest set %+v last %v", n, r.header.Set, r.manifest.Set, r.manifest.Last)
		}
		if n > 1 && r.header.Set.Parts != sum.Parts {
			t.Errorf("part %d's header does not say how many parts there are", n)
		}
		for _, p := range r.order {
			if strings.HasPrefix(p, DataDir) || strings.HasPrefix(p, AuthDir) {
				if n > 1 {
					t.Errorf("part %d holds %s", n, p)
				}
			}
			if strings.HasPrefix(p, FilesDir) {
				key := strings.TrimPrefix(p, FilesDir)
				if got[key] {
					t.Errorf("%s is in two parts", key)
				}
				got[key] = true
			}
		}
		if n == 1 && r.manifest.Tables["assets"] != 4 {
			t.Errorf("the first part counts %d assets", r.manifest.Tables["assets"])
		}
	}
	if len(got) != len(want) {
		t.Errorf("%d files archived of %d", len(got), len(want))
	}
}

// noise is n bytes that gzip cannot make smaller, the same for a seed.
func noise(seed, n int64) io.Reader {
	return io.LimitReader(&noiseReader{x: uint64(seed)*0x9E3779B97F4A7C15 + 1}, n)
}

type noiseReader struct{ x uint64 }

func (r *noiseReader) Read(b []byte) (int, error) {
	for i := range b {
		// xorshift64*
		r.x ^= r.x >> 12
		r.x ^= r.x << 25
		r.x ^= r.x >> 27
		b[i] = byte((r.x * 0x2545F4914F6CDD1D) >> 56)
	}
	return len(b), nil
}

func TestRowsAreReadAtOneMoment(t *testing.T) {
	st := setupStore(t)
	blobs := filestore.NewLocal(t.TempDir())
	cat, err := st.CreateCategory("Tools", nil, "#336699", false, false)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 200; i++ {
		if _, err := st.CreateAsset("Drill "+strconv.Itoa(i), []string{cat.ID}, nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	// Assets are made while it is written: each, its category and its
	// history in one transaction.
	stop := make(chan struct{})
	var made atomic.Int64
	done := make(chan error, 1)
	go func() {
		for i := 0; ; i++ {
			select {
			case <-stop:
				done <- nil
				return
			default:
			}
			if _, err := st.CreateAsset("Saw "+strconv.Itoa(i), []string{cat.ID}, nil, nil); err != nil {
				done <- err
				return
			}
			made.Add(1)
		}
	}()
	// Between reading assets and their categories, more are made. SQLite
	// has one connection, which the backup holds, so there none can be.
	var during int64
	afterTable = func(table string) {
		if table != "assets" || st.Dialect == "sqlite" {
			return
		}
		from := made.Load()
		for deadline := time.Now().Add(3 * time.Second); made.Load() < from+5 && time.Now().Before(deadline); {
			time.Sleep(5 * time.Millisecond)
		}
		during = made.Load() - from
	}
	t.Cleanup(func() { afterTable = nil })
	var parts memParts
	_, err = Write(context.Background(), st, blobs, parts.open, Options{})
	close(stop)
	if st.Dialect != "sqlite" && during < 5 {
		t.Errorf("only %d assets were made while the backup was read", during)
	}
	if werr := <-done; werr != nil {
		t.Fatal(werr)
	}
	if err != nil {
		t.Fatal(err)
	}
	r := readArchive(t, parts.parts[1].Bytes())
	assets := map[any]bool{}
	for _, a := range r.rows(t, DataDir+"assets/") {
		assets[a["id"]] = true
	}
	inCategory := map[any]bool{}
	for _, ac := range r.rows(t, DataDir+"asset_categories/") {
		if !assets[ac["asset_id"]] {
			t.Errorf("asset_categories names %v, which is not archived", ac["asset_id"])
		}
		inCategory[ac["asset_id"]] = true
	}
	for id := range assets {
		if !inCategory[id] {
			t.Errorf("asset %v is archived without its category", id)
		}
	}
	t.Logf("%d assets archived; %d made while it was read", len(assets), during)
}

// genBlobs serves files made on the fly, of the sizes given, stored
// nowhere.
type genBlobs struct{ sizes map[string]int64 }

func (g *genBlobs) Put(context.Context, string, io.Reader, int64, string) error { panic("read only") }
func (g *genBlobs) Delete(context.Context, string) error                        { panic("read only") }

func (g *genBlobs) Get(_ context.Context, key string) (io.ReadCloser, error) {
	n, ok := g.sizes[key]
	if !ok {
		return nil, filestore.ErrNotFound
	}
	return io.NopCloser(noise(n, n)), nil
}

func (g *genBlobs) List(_ context.Context, prefix string, fn func(filestore.Object) error) error {
	var keys []string
	for k := range g.sizes {
		if strings.HasPrefix(k, prefix) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		if err := fn(filestore.Object{Key: k, Size: g.sizes[k]}); err != nil {
			return err
		}
	}
	return nil
}

// countingDiscard counts what is written to it, and keeps none of it.
type countingDiscard struct{ n int64 }

func (c *countingDiscard) Write(b []byte) (int, error) { c.n += int64(len(b)); return len(b), nil }
func (c *countingDiscard) Close() error                { return nil }

// archiveBytesEnv sets how many bytes of files TestMemoryDoesNotGrow
// archives: tens of gigabytes takes minutes, so the usual suites archive
// less.
const archiveBytesEnv = "BAM_TEST_ARCHIVE_BYTES"

func TestMemoryDoesNotGrowWithTheArchive(t *testing.T) {
	total := int64(256 << 20)
	if s := os.Getenv(archiveBytesEnv); s != "" {
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			t.Fatalf("%s: %v", archiveBytesEnv, err)
		}
		total = n
	}
	st := setupStore(t)
	cat, err := st.CreateCategory("Tools", nil, "#336699", false, false)
	if err != nil {
		t.Fatal(err)
	}
	// Twenty files, a photo and a thumbnail on each of ten assets.
	g := &genBlobs{sizes: map[string]int64{}}
	size := total / 20
	for i := 0; i < 10; i++ {
		asset, err := st.CreateAsset("Drill "+strconv.Itoa(i), []string{cat.ID}, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		p := models.AssetPhoto{ID: uuid.NewString(), ThumbID: uuid.NewString(), Filename: "x.jpg", ContentType: "image/jpeg", WidthPx: 1, HeightPx: 1, ByteSize: size, CreatedAt: time.Now()}
		g.sizes["photos/"+p.ID], g.sizes["photos/"+p.ThumbID] = size, size
		if _, err := st.AddPhoto(asset.Asset.ID, p); err != nil {
			t.Fatal(err)
		}
	}

	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)
	var peak atomic.Uint64
	stop := make(chan struct{})
	sampled := make(chan struct{})
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
	out := &countingDiscard{}
	start := time.Now()
	sum, err := Write(context.Background(), st, g, func(int) (io.WriteCloser, error) { return out, nil }, Options{})
	close(stop)
	<-sampled
	if err != nil {
		t.Fatal(err)
	}
	if sum.FileBytes != size*20 || out.n < sum.FileBytes {
		t.Fatalf("archived %d bytes of files, wrote %d; want %d", sum.FileBytes, out.n, size*20)
	}
	grew := int64(peak.Load()) - int64(before.HeapInuse)
	t.Logf("%d bytes of files in %v; the heap grew by at most %d bytes", sum.FileBytes, time.Since(start), grew)
	if grew > 64<<20 {
		t.Errorf("the heap grew by %d bytes writing %d", grew, sum.FileBytes)
	}
}

func TestASchemaNotCurrentIsRefused(t *testing.T) {
	st := setupStore(t)
	if _, err := st.Exec("DELETE FROM schema_migrations WHERE version = ?", migrations.Latest()); err != nil {
		t.Fatal(err)
	}
	var parts memParts
	if _, err := Write(context.Background(), st, filestore.NewLocal(t.TempDir()), parts.open, Options{}); !errors.Is(err, ErrSchema) {
		t.Fatalf("got %v, want ErrSchema", err)
	}
	if len(parts.parts) != 0 {
		t.Error("a part was begun")
	}
}
