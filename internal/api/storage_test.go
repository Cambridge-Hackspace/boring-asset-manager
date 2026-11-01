package api

import (
	"bytes"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/config"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/filestore"
)

// testStorage gives a test server a store of its own for uploaded files: a
// temporary directory, or, when BAM_TEST_S3_ENDPOINT names an S3-compatible
// store (docs/testing.md), a prefix of its own in that bucket, so the photo,
// image and label suites run on either.
func testStorage(tb testing.TB, cfg *config.Config) {
	tb.Helper()
	cfg.Storage.LocalPath = tb.TempDir()
	endpoint := os.Getenv("BAM_TEST_S3_ENDPOINT")
	if endpoint == "" {
		return
	}
	cfg.Storage.Backend = config.StorageS3
	cfg.Storage.S3 = config.S3Config{
		Endpoint: endpoint, Bucket: os.Getenv("BAM_TEST_S3_BUCKET"), PathStyle: true,
		Prefix:      fmt.Sprintf("api-%d/", time.Now().UnixNano()),
		AccessKeyID: os.Getenv("BAM_TEST_S3_ACCESS_KEY_ID"), SecretAccessKey: os.Getenv("BAM_TEST_S3_SECRET_ACCESS_KEY"),
	}
}

// storedFiles counts the files a test server keeps under a prefix, in
// whichever store it has.
func storedFiles(t *testing.T, srv *Server, prefix string) int {
	t.Helper()
	n := 0
	if err := srv.blobs.List(t.Context(), prefix, func(filestore.Object) error { n++; return nil }); err != nil {
		t.Fatalf("listing %s: %v", prefix, err)
	}
	return n
}

// TestTheStorageIsChecked: a test server's store, whichever it is, can be
// reached and written, as bam serve checks at startup.
func TestTheStorageIsChecked(t *testing.T) {
	srv, _, _ := setupTestServer(t)
	where, err := srv.CheckStorage(t.Context())
	if err != nil {
		t.Fatalf("%s: %v", where, err)
	}
	want := "the directory "
	if os.Getenv("BAM_TEST_S3_ENDPOINT") != "" {
		want = "S3 bucket "
	}
	if len(where) < len(want) || where[:len(want)] != want {
		t.Errorf("described as %q", where)
	}
}

// TestSweepingTheServersStore: the server's store and its rows kept in
// step, on whichever store it has: an orphan goes, a photo's and an image's
// files stay, and a row whose file went is reported.
func TestSweepingTheServersStore(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	a, _ := srv.store.CreateAsset("Drill", nil, nil, nil)
	body, ct := multipartBody(nil, false, map[string][]byte{"file": testJPEG(t, 40, 30)}, nil)
	if rec := do(t, srv, cookie, http.MethodPost, "/api/v1/assets/"+a.Asset.ID+"/photos", ct, bytes.NewReader(body)); rec.Code != http.StatusCreated {
		t.Fatalf("upload: %d %s", rec.Code, rec.Body)
	}
	ctx := t.Context()
	if err := srv.blobs.Put(ctx, "photos/00000000-0000-4000-8000-000000000001", strings.NewReader("orphan"), 6, ""); err != nil {
		t.Fatal(err)
	}
	named, err := srv.store.StoredFiles()
	if err != nil || len(named) != 2 {
		t.Fatalf("named: %v %v", named, err)
	}
	// With no grace, the orphan goes at once, whatever its store's clock.
	res, err := filestore.Sweep(ctx, srv.blobs, named, 0, time.Now().Add(time.Minute), false)
	if err != nil || len(res.Removed) != 1 || res.Removed[0] != "photos/00000000-0000-4000-8000-000000000001" || len(res.Missing) != 0 {
		t.Fatalf("swept: %+v %v", res, err)
	}
	if n := storedFiles(t, srv, "photos/"); n != 2 {
		t.Errorf("%d photo files left, want the photo and its thumbnail", n)
	}
	// A row whose file is gone is reported, and the server's own sweep,
	// with its day of grace, removes nothing young.
	for k := range named {
		if err := srv.blobs.Delete(ctx, k); err != nil {
			t.Fatal(err)
		}
		break
	}
	res, err = srv.SweepStorage(ctx, false)
	if err != nil || len(res.Missing) != 1 || len(res.Removed) != 0 {
		t.Errorf("after losing a file: %+v %v", res, err)
	}
}
