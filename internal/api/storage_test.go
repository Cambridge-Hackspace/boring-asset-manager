package api

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/config"
)

// testStorage gives a test server a store of its own for uploaded files: a
// temporary directory, or, when BAM_TEST_S3_ENDPOINT names an S3-compatible
// store (README §8.3), a prefix of its own in that bucket, so the photo,
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
	if err := srv.blobs.List(t.Context(), prefix, func(string) error { n++; return nil }); err != nil {
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
