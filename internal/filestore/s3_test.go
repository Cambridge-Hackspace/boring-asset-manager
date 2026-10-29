package filestore_test

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/filestore"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/filestore/blobtest"
)

// TestS3Conforms runs the conformance suite on a real S3-compatible store,
// MinIO as README §8.3 starts it, when BAM_TEST_S3_ENDPOINT names one; each
// run under a prefix of its own.
func TestS3Conforms(t *testing.T) {
	s, ok := testS3(t, fmt.Sprintf("conformance-%d/", time.Now().UnixNano()))
	if !ok {
		t.Skip("BAM_TEST_S3_ENDPOINT is not set; start MinIO as README §8.3 says to run this")
	}
	blobtest.Run(t, s)
}

// TestS3ListsPageByPage: a list longer than a page is read page by page,
// in order, every key once.
func TestS3ListsPageByPage(t *testing.T) {
	s, ok := testS3(t, fmt.Sprintf("pages-%d/", time.Now().UnixNano()), 2)
	if !ok {
		t.Skip("BAM_TEST_S3_ENDPOINT is not set")
	}
	ctx := context.Background()
	var want []string
	for i := 0; i < 7; i++ {
		k := fmt.Sprintf("photos/%02d", i)
		want = append(want, k)
		if err := s.Put(ctx, k, strings.NewReader("x"), 1, ""); err != nil {
			t.Fatal(err)
		}
	}
	var got []string
	if err := s.List(ctx, "photos/", func(k string) error { got = append(got, k); return nil }); err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("listed %v", got)
	}
}

func testS3(t *testing.T, prefix string, page ...int) (*filestore.S3, bool) {
	t.Helper()
	endpoint := os.Getenv("BAM_TEST_S3_ENDPOINT")
	if endpoint == "" {
		return nil, false
	}
	s, err := filestore.NewS3(filestore.S3Options{
		Endpoint: endpoint, Bucket: os.Getenv("BAM_TEST_S3_BUCKET"), Prefix: prefix, PathStyle: true,
		AccessKeyID: os.Getenv("BAM_TEST_S3_ACCESS_KEY_ID"), SecretAccessKey: os.Getenv("BAM_TEST_S3_SECRET_ACCESS_KEY"),
		ListPage: append(page, 0)[0],
	})
	if err != nil {
		t.Fatal(err)
	}
	return s, true
}

// TestS3SignsAsAWSDocumentsIt checks the signature against the worked
// examples of AWS's Signature Version 4 documentation for S3 ("Signature
// calculations for the Authorization header: transferring payload in a
// single chunk"), with its example keys.
func TestS3SignsAsAWSDocumentsIt(t *testing.T) {
	at := time.Date(2013, 5, 24, 0, 0, 0, 0, time.UTC)
	s, err := filestore.NewS3(filestore.S3Options{
		Endpoint: "https://s3.amazonaws.com", Bucket: "examplebucket", Region: "us-east-1",
		AccessKeyID: "AKIAIOSFODNN7EXAMPLE", SecretAccessKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
		Now: func() time.Time { return at }, Client: &http.Client{Transport: capture{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	const empty = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	cases := []struct {
		name, method, url string
		header            map[string]string
		want              string
	}{
		{"GET Object", http.MethodGet, "https://examplebucket.s3.amazonaws.com/test.txt", map[string]string{"Range": "bytes=0-9"},
			"AWS4-HMAC-SHA256 Credential=AKIAIOSFODNN7EXAMPLE/20130524/us-east-1/s3/aws4_request, SignedHeaders=host;range;x-amz-content-sha256;x-amz-date, Signature=f0e8bdb87c964420e857bd35b5d6ed310bd44f0170aba48dd91039c6036bdb41"},
		{"GET Bucket (list objects)", http.MethodGet, "https://examplebucket.s3.amazonaws.com/?max-keys=2&prefix=J", nil,
			"AWS4-HMAC-SHA256 Credential=AKIAIOSFODNN7EXAMPLE/20130524/us-east-1/s3/aws4_request, SignedHeaders=host;x-amz-content-sha256;x-amz-date, Signature=34b48302e7b5fa45bde8084f4b7868a86f0a534bc59db6670ed5711ef69dc6f7"},
	}
	for _, c := range cases {
		req, _ := http.NewRequest(c.method, c.url, nil)
		for k, v := range c.header {
			req.Header.Set(k, v)
		}
		filestore.SignForTest(s, req, empty)
		if got := req.Header.Get("Authorization"); got != c.want {
			t.Errorf("%s:\n got %s\nwant %s", c.name, got, c.want)
		}
	}
}

// capture answers every request with 200 and nothing, for tests that look
// at the request only.
type capture struct{}

func (capture) RoundTrip(r *http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: 200, Body: http.NoBody, Header: http.Header{}, Request: r}, nil
}

// TestS3NeverSaysItsSecret: neither the store's description nor its
// refusals carry the secret key.
func TestS3NeverSaysItsSecret(t *testing.T) {
	s, _ := filestore.NewS3(filestore.S3Options{Endpoint: "http://minio:9000", Bucket: "bam", Prefix: "site/", PathStyle: true,
		AccessKeyID: "AK", SecretAccessKey: "very-secret"})
	if got := s.String(); strings.Contains(got, "very-secret") || got != "S3 bucket bam/site/ at http://minio:9000" {
		t.Errorf("String: %s", got)
	}
}
