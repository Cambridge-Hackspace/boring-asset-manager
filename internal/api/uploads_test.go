package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/filestore"
)

func sha256Of(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

type uploadState struct {
	ID       string `json:"id"`
	PartSize int64  `json:"part_size"`
	Received int64  `json:"received"`
	Complete bool   `json:"complete"`
}

func beginUpload(t *testing.T, srv *Server, cookie *http.Cookie, size int64, sum string) (*httptest.ResponseRecorder, uploadState) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"filename": "bam.bam.tar.gz", "size": size, "sha256": sum})
	rec := do(t, srv, cookie, http.MethodPost, "/api/v1/uploads", "application/json", bytes.NewReader(body))
	var u uploadState
	_ = json.Unmarshal(rec.Body.Bytes(), &u)
	return rec, u
}

// putPart sends bytes as the part from first, of an upload of total bytes,
// declaring length bytes (more than it sends, to cut it short).
func putPart(t *testing.T, srv *Server, cookie *http.Cookie, id string, data []byte, first, length, total int64) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/uploads/"+id, bytes.NewReader(data))
	req.ContentLength = length
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", first, first+length-1, total))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

// TestAnUploadInParts: an archive uploaded in parts, one cut short and
// sent again after asking where to resume, arrives whole; parts out of
// order, again, overlapping, of the wrong length or past the end are
// refused, changing nothing.
func TestAnUploadInParts(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	data := make([]byte, 2*filestore.MinPartSize+777)
	rand.New(rand.NewSource(7)).Read(data)
	size := int64(len(data))

	rec, u := beginUpload(t, srv, cookie, size, sha256Of(data))
	if rec.Code != http.StatusCreated || u.PartSize != filestore.MinPartSize || u.Received != 0 {
		t.Fatalf("begin: %d %s", rec.Code, rec.Body)
	}
	part := u.PartSize
	refused := func(name string, rec *httptest.ResponseRecorder, code int) {
		t.Helper()
		if rec.Code != code {
			t.Errorf("%s: %d %s, want %d", name, rec.Code, rec.Body, code)
		}
	}

	refused("the second part first", putPart(t, srv, cookie, u.ID, data[part:2*part], part, part, size), http.StatusConflict)
	refused("a part too short", putPart(t, srv, cookie, u.ID, data[:part-1], 0, part-1, size), http.StatusBadRequest)
	refused("a part too long", putPart(t, srv, cookie, u.ID, data[:part+1], 0, part+1, size), http.StatusBadRequest)
	refused("another size", putPart(t, srv, cookie, u.ID, data[:part], 0, part, size+1), http.StatusBadRequest)
	noRange := httptest.NewRequest(http.MethodPut, "/api/v1/uploads/"+u.ID, bytes.NewReader(data[:part]))
	noRange.AddCookie(cookie)
	noRange.Header.Set("Content-Type", "application/octet-stream")
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, noRange)
	refused("no Content-Range", rec, http.StatusBadRequest)

	// The connection drops part way through the first part.
	refused("a part cut short", putPart(t, srv, cookie, u.ID, data[:1000], 0, part, size), http.StatusBadRequest)
	head := do(t, srv, cookie, http.MethodHead, "/api/v1/uploads/"+u.ID, "", nil)
	if head.Code != http.StatusOK || head.Header().Get("Upload-Offset") != "0" || head.Header().Get("Upload-Length") != strconv.FormatInt(size, 10) {
		t.Fatalf("HEAD: %d %v", head.Code, head.Header())
	}
	if rec := putPart(t, srv, cookie, u.ID, data[:part], 0, part, size); rec.Code != http.StatusOK {
		t.Fatalf("first part: %d %s", rec.Code, rec.Body)
	}
	refused("the first part again", putPart(t, srv, cookie, u.ID, data[:part], 0, part, size), http.StatusConflict)
	refused("overlapping", putPart(t, srv, cookie, u.ID, data[part-10:2*part-10], part-10, part, size), http.StatusConflict)
	if rec := putPart(t, srv, cookie, u.ID, data[part:2*part], part, part, size); rec.Code != http.StatusOK {
		t.Fatalf("second part: %d %s", rec.Code, rec.Body)
	}
	refused("past the end", putPart(t, srv, cookie, u.ID, data[2*part:], 2*part, size-2*part+1, size), http.StatusBadRequest)
	rec = putPart(t, srv, cookie, u.ID, data[2*part:], 2*part, size-2*part, size)
	if rec.Code != http.StatusOK {
		t.Fatalf("last part: %d %s", rec.Code, rec.Body)
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &u)
	if !u.Complete || u.Received != size {
		t.Fatalf("after the last part: %s", rec.Body)
	}
	refused("a part once complete", putPart(t, srv, cookie, u.ID, data[:part], 0, part, size), http.StatusConflict)

	rc, err := srv.staging.Open(context.Background(), u.ID)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(rc)
	rc.Close()
	if !bytes.Equal(got, data) {
		t.Errorf("staged %d bytes, not the %d uploaded", len(got), len(data))
	}

	if rec := do(t, srv, cookie, http.MethodDelete, "/api/v1/uploads/"+u.ID, "", nil); rec.Code != http.StatusNoContent {
		t.Errorf("delete: %d", rec.Code)
	}
	if _, err := srv.staging.Open(context.Background(), u.ID); err == nil {
		t.Error("the staged upload is still there")
	}
}

// TestUploadsAreTheirOwners: someone else's upload is not found, and one
// needs settings.manage.
func TestUploadsAreTheirOwners(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	data := []byte("a small archive")
	_, u := beginUpload(t, srv, cookie, int64(len(data)), sha256Of(data))

	other, err := srv.store.CreateAdminUser("other@test.org", "AnotherSecret123!")
	if err != nil {
		t.Fatal(err)
	}
	theirs := sessionCookie(t, srv, other.ID)
	for _, rec := range []*httptest.ResponseRecorder{
		do(t, srv, theirs, http.MethodGet, "/api/v1/uploads/"+u.ID, "", nil),
		putPart(t, srv, theirs, u.ID, data, 0, int64(len(data)), int64(len(data))),
		do(t, srv, theirs, http.MethodDelete, "/api/v1/uploads/"+u.ID, "", nil),
	} {
		if rec.Code != http.StatusNotFound {
			t.Errorf("someone else's upload: %d %s", rec.Code, rec.Body)
		}
	}

	plain, err := srv.store.CreateUser("plain@test.org", "PlainSecret123!", true)
	if err != nil {
		t.Fatal(err)
	}
	if rec, _ := beginUpload(t, srv, sessionCookie(t, srv, plain.ID), 10, sha256Of(data)); rec.Code != http.StatusForbidden {
		t.Errorf("an upload without settings.manage: %d", rec.Code)
	}
	if rec, _ := beginUpload(t, srv, nil, 10, sha256Of(data)); rec.Code != http.StatusUnauthorized {
		t.Errorf("an upload signed out: %d", rec.Code)
	}
}

func TestUploadsAreChecked(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	data := []byte("what arrives")
	for name, c := range map[string]struct {
		size int64
		sum  string
	}{
		"no size":       {0, sha256Of(data)},
		"too large":     {MaxUploadSize + 1, sha256Of(data)},
		"no hash":       {10, ""},
		"not a hash":    {10, "ABC"},
		"an upper hash": {10, "A" + sha256Of(data)[1:]},
	} {
		if rec, _ := beginUpload(t, srv, cookie, c.size, c.sum); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: %d", name, rec.Code)
		}
	}

	// Bytes other than the file's: refused at the end, and removed.
	_, u := beginUpload(t, srv, cookie, int64(len(data)), sha256Of([]byte("what was sent")))
	if rec := putPart(t, srv, cookie, u.ID, data, 0, int64(len(data)), int64(len(data))); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("a file not matching its hash: %d %s", rec.Code, rec.Body)
	}
	if rec := do(t, srv, cookie, http.MethodGet, "/api/v1/uploads/"+u.ID, "", nil); rec.Code != http.StatusNotFound {
		t.Errorf("the mismatched upload is still there: %d", rec.Code)
	}

	if os.Getenv("BAM_TEST_S3_ENDPOINT") == "" {
		if rec, _ := beginUpload(t, srv, cookie, 4<<40, sha256Of(data)); rec.Code != http.StatusInsufficientStorage {
			t.Errorf("an upload larger than the disk: %d %s", rec.Code, rec.Body)
		}
	}
}

// TestUnusedUploadsGoAfterADay: an upload neither added to nor used for a
// day is removed, staged bytes and all; a newer one stays.
func TestUnusedUploadsGoAfterADay(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	data := []byte("left behind")
	_, old := beginUpload(t, srv, cookie, int64(len(data)), sha256Of(data))
	if rec := putPart(t, srv, cookie, old.ID, data, 0, int64(len(data)), int64(len(data))); rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if _, err := srv.store.Exec("UPDATE uploads SET updated_at = ? WHERE id = ?", time.Now().Add(-25*time.Hour).UTC(), old.ID); err != nil {
		t.Fatal(err)
	}
	_, fresh := beginUpload(t, srv, cookie, 100, sha256Of(data))

	n, err := srv.SweepUploads(context.Background(), time.Now())
	if err != nil || n != 1 {
		t.Fatalf("swept %d: %v", n, err)
	}
	if rec := do(t, srv, cookie, http.MethodGet, "/api/v1/uploads/"+old.ID, "", nil); rec.Code != http.StatusNotFound {
		t.Errorf("an upload unused for a day: %d", rec.Code)
	}
	if _, err := srv.staging.Open(context.Background(), old.ID); err == nil {
		t.Error("its staged bytes are still there")
	}
	if rec := do(t, srv, cookie, http.MethodGet, "/api/v1/uploads/"+fresh.ID, "", nil); rec.Code != http.StatusOK {
		t.Errorf("a fresh upload: %d", rec.Code)
	}
}

// slowReader gives its bytes a few at a time, pausing between.
type slowReader struct {
	data  []byte
	pause time.Duration
}

func (s *slowReader) Read(p []byte) (int, error) {
	if len(s.data) == 0 {
		return 0, io.EOF
	}
	time.Sleep(s.pause)
	n := copy(p[:min(len(p), 64<<10)], s.data)
	s.data = s.data[n:]
	return n, nil
}

// TestAPartTakesAsLongAsItMoves, over HTTP: a part arriving slower than
// the server's deadline for requests is taken while its bytes keep moving,
// and given up once they stop.
func TestAPartTakesAsLongAsItMoves(t *testing.T) {
	oldTimeout, oldIdle := requestTimeout, transferIdle
	requestTimeout, transferIdle = 300*time.Millisecond, 400*time.Millisecond
	t.Cleanup(func() { requestTimeout, transferIdle = oldTimeout, oldIdle })
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	ts := httptest.NewUnstartedServer(srv.Handler())
	ts.Config.ReadTimeout, ts.Config.WriteTimeout = requestTimeout, requestTimeout
	ts.Start()
	defer ts.Close()

	data := make([]byte, 1<<20)
	rand.New(rand.NewSource(3)).Read(data)
	_, u := beginUpload(t, srv, cookie, int64(len(data)), sha256Of(data))
	send := func(r io.Reader) (*http.Response, error) {
		req, _ := http.NewRequest(http.MethodPut, ts.URL+"/api/v1/uploads/"+u.ID, r)
		req.ContentLength = int64(len(data))
		req.Header.Set("Content-Type", "application/octet-stream")
		req.Header.Set("Content-Range", fmt.Sprintf("bytes 0-%d/%d", len(data)-1, len(data)))
		req.AddCookie(cookie)
		return http.DefaultClient.Do(req)
	}

	// Stalls longer than the idle allowance: given up.
	res, err := send(io.MultiReader(bytes.NewReader(data[:1000]), &slowReader{data: data[1000:], pause: time.Second}))
	if err == nil {
		if res.StatusCode == http.StatusOK {
			t.Error("a part that stopped moving was taken")
		}
		res.Body.Close()
	}

	// 16 pieces 100 ms apart: 1.6 s, past the 300 ms deadline.
	start := time.Now()
	res, err = send(&slowReader{data: data, pause: 100 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("a slow part: %d %s", res.StatusCode, body)
	}
	if took := time.Since(start); took < time.Second {
		t.Errorf("the part took only %v", took)
	}
}

// uploadBytesEnv sets how many bytes TestALargeUpload sends: tens of
// gigabytes takes minutes, so the usual suites send less.
const uploadBytesEnv = "BAM_TEST_UPLOAD_BYTES"

// noise is n bytes, the same for a seed, made as they are read.
type noise struct{ x uint64 }

func (r *noise) Read(b []byte) (int, error) {
	for i := range b {
		r.x ^= r.x >> 12
		r.x ^= r.x << 25
		r.x ^= r.x >> 27
		b[i] = byte((r.x * 0x2545F4914F6CDD1D) >> 56)
	}
	return len(b), nil
}

// TestALargeUpload sends an upload over HTTP, made as it is sent, and
// checks the server's memory does not grow with it: 256 MiB, or
// BAM_TEST_UPLOAD_BYTES. With BAM_TEST_S3_ENDPOINT, it is staged in the
// bucket.
func TestALargeUpload(t *testing.T) {
	size := int64(256 << 20)
	if s := os.Getenv(uploadBytesEnv); s != "" {
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			t.Fatal(err)
		}
		size = n
	}
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	h := sha256.New()
	if _, err := io.CopyN(h, &noise{x: 42}, size); err != nil {
		t.Fatal(err)
	}
	_, u := beginUpload(t, srv, cookie, size, hex.EncodeToString(h.Sum(nil)))
	if u.ID == "" {
		t.Fatal("the upload was not begun")
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
	src := &noise{x: 42}
	start := time.Now()
	for first := int64(0); first < size; first += u.PartSize {
		n := min(u.PartSize, size-first)
		req, _ := http.NewRequest(http.MethodPut, ts.URL+"/api/v1/uploads/"+u.ID, io.LimitReader(src, n))
		req.ContentLength = n
		req.Header.Set("Content-Type", "application/octet-stream")
		req.Header.Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", first, first+n-1, size))
		req.AddCookie(cookie)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Fatalf("part at %d: %d %s", first, res.StatusCode, body)
		}
		if first+n == size {
			_ = json.Unmarshal(body, &u)
		}
	}
	close(stop)
	<-sampled
	if !u.Complete {
		t.Fatal("the upload is not complete")
	}
	grew := int64(peak.Load()) - int64(before.HeapInuse)
	t.Logf("%d bytes uploaded in parts of %d in %v; the heap grew by at most %d bytes", size, u.PartSize, time.Since(start), grew)
	if grew > 96<<20 {
		t.Errorf("the heap grew by %d bytes uploading %d", grew, size)
	}

	// Read back, every byte checked.
	rc, err := srv.staging.Open(context.Background(), u.ID)
	if err != nil {
		t.Fatal(err)
	}
	back := sha256.New()
	n, err := io.Copy(back, rc)
	rc.Close()
	if err != nil || n != size || !bytes.Equal(back.Sum(nil), h.Sum(nil)) {
		t.Errorf("read back %d bytes (%v), not what was sent", n, err)
	}
	staged, err := srv.store.GetUpload(u.ID, admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	srv.removeUpload(context.Background(), staged)
}
