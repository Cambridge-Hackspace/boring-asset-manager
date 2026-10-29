package filestore

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// S3Options reach a bucket in an S3-compatible store: AWS, MinIO, Garage,
// Ceph, Backblaze B2, Cloudflare R2.
type S3Options struct {
	// Endpoint is the store's address, such as https://s3.eu-west-2.amazonaws.com
	// or http://minio:9000.
	Endpoint string
	Region   string
	Bucket   string
	// Prefix starts the name of every object bam keeps, so one bucket can
	// hold more than bam's.
	Prefix string
	// PathStyle addresses the bucket in the path (endpoint/bucket/key), as
	// MinIO and most stores but AWS expect; otherwise as a host name
	// (bucket.endpoint/key).
	PathStyle bool
	// AccessKeyID and SecretAccessKey sign every request; never logged.
	AccessKeyID     string
	SecretAccessKey string
	// Client sends the requests; nil uses one with a two-minute timeout.
	Client *http.Client
	// Now is the clock requests are signed with; nil is time.Now.
	Now func() time.Time
	// ListPage is how many keys to ask for at once when listing; 0 leaves
	// it to the store (1,000 on most).
	ListPage int
}

// S3 keeps files as objects in a bucket, each key an object named the
// prefix and the key. Requests are signed with AWS Signature Version 4.
type S3 struct {
	opts S3Options
	base *url.URL
}

// MaxS3Object is the largest object bam writes in one request: far over any
// upload it takes (photos are 10 MB at most), and under every store's limit
// for one.
const MaxS3Object = 1 << 30

// NewS3 returns the store of objects the options reach. The options are
// checked by the configuration; an endpoint that does not parse is an
// error.
func NewS3(opts S3Options) (*S3, error) {
	u, err := url.Parse(strings.TrimRight(opts.Endpoint, "/"))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("filestore: the S3 endpoint %q is not an http or https address", opts.Endpoint)
	}
	if opts.Client == nil {
		opts.Client = &http.Client{Timeout: 2 * time.Minute}
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Region == "" {
		opts.Region = "us-east-1"
	}
	return &S3{opts: opts, base: u}, nil
}

// String says which store this is, for the log, without its keys.
func (s *S3) String() string {
	where := s.opts.Bucket
	if s.opts.Prefix != "" {
		where += "/" + s.opts.Prefix
	}
	return fmt.Sprintf("S3 bucket %s at %s", where, s.base)
}

// objectURL is the address of an object, by its full name, or of the
// bucket, for an empty name.
func (s *S3) objectURL(name string, query url.Values) *url.URL {
	u := *s.base
	if s.opts.PathStyle {
		u.Path = strings.TrimRight(u.Path, "/") + "/" + s.opts.Bucket + "/" + name
	} else {
		u.Host = s.opts.Bucket + "." + u.Host
		u.Path = strings.TrimRight(u.Path, "/") + "/" + name
	}
	u.RawQuery = canonicalQuery(query)
	return &u
}

func (s *S3) name(key string) (string, error) {
	if !ValidKey(key) {
		return "", ErrInvalidKey
	}
	return s.opts.Prefix + key, nil
}

// Put stores the object, read whole first so that its length and hash are
// known, and so that a body other than size long is refused before
// anything is sent.
func (s *S3) Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error {
	name, err := s.name(key)
	if err != nil {
		return err
	}
	limit := int64(MaxS3Object)
	if size >= 0 && size < limit {
		limit = size
	}
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return fmt.Errorf("reading %s: %w", key, err)
	}
	switch {
	case int64(len(data)) > limit && size >= 0:
		return fmt.Errorf("writing %s: more than its %d bytes", key, size)
	case int64(len(data)) > limit:
		return fmt.Errorf("writing %s: over %d bytes", key, int64(MaxS3Object))
	case size >= 0 && int64(len(data)) != size:
		return fmt.Errorf("writing %s: %d bytes, expected %d", key, len(data), size)
	}
	header := http.Header{}
	if contentType != "" {
		header.Set("Content-Type", contentType)
	}
	res, err := s.do(ctx, http.MethodPut, s.objectURL(name, nil), header, data)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode/100 != 2 {
		return s3Error(res, "storing "+key)
	}
	return nil
}

// Get opens an object.
func (s *S3) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	name, err := s.name(key)
	if err != nil {
		return nil, err
	}
	res, err := s.do(ctx, http.MethodGet, s.objectURL(name, nil), nil, nil)
	if err != nil {
		return nil, err
	}
	if res.StatusCode == http.StatusNotFound {
		res.Body.Close()
		return nil, ErrNotFound
	}
	if res.StatusCode/100 != 2 {
		defer res.Body.Close()
		return nil, s3Error(res, "reading "+key)
	}
	return res.Body, nil
}

// Delete removes an object; one not there is not an error.
func (s *S3) Delete(ctx context.Context, key string) error {
	name, err := s.name(key)
	if err != nil {
		return err
	}
	res, err := s.do(ctx, http.MethodDelete, s.objectURL(name, nil), nil, nil)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode/100 != 2 && res.StatusCode != http.StatusNotFound {
		return s3Error(res, "deleting "+key)
	}
	return nil
}

// listResult is the part of a ListObjectsV2 answer List reads.
type listResult struct {
	Contents []struct {
		Key          string    `xml:"Key"`
		Size         int64     `xml:"Size"`
		LastModified time.Time `xml:"LastModified"`
	} `xml:"Contents"`
	IsTruncated           bool   `xml:"IsTruncated"`
	NextContinuationToken string `xml:"NextContinuationToken"`
}

// List pages through ListObjectsV2, whose keys come in order.
func (s *S3) List(ctx context.Context, prefix string, fn func(Object) error) error {
	token := ""
	for {
		q := url.Values{"list-type": {"2"}, "prefix": {s.opts.Prefix + prefix}}
		if s.opts.ListPage > 0 {
			q.Set("max-keys", fmt.Sprint(s.opts.ListPage))
		}
		if token != "" {
			q.Set("continuation-token", token)
		}
		res, err := s.do(ctx, http.MethodGet, s.objectURL("", q), nil, nil)
		if err != nil {
			return err
		}
		if res.StatusCode/100 != 2 {
			err := s3Error(res, "listing "+prefix)
			res.Body.Close()
			return err
		}
		var page listResult
		err = xml.NewDecoder(res.Body).Decode(&page)
		res.Body.Close()
		if err != nil {
			return fmt.Errorf("reading the list of %s: %w", prefix, err)
		}
		for _, c := range page.Contents {
			if err := fn(Object{Key: strings.TrimPrefix(c.Key, s.opts.Prefix), Size: c.Size, Modified: c.LastModified}); err != nil {
				return err
			}
		}
		if !page.IsTruncated || page.NextContinuationToken == "" {
			return nil
		}
		token = page.NextContinuationToken
	}
}

// s3Error is a refusal in the store's own words: its code and message, and
// never the request's credentials.
func s3Error(res *http.Response, action string) error {
	var e struct {
		Code    string `xml:"Code"`
		Message string `xml:"Message"`
	}
	body, _ := io.ReadAll(io.LimitReader(res.Body, 64<<10))
	_ = xml.Unmarshal(body, &e)
	if e.Code == "" {
		return fmt.Errorf("%s: the store answered %s", action, res.Status)
	}
	return fmt.Errorf("%s: the store answered %s: %s: %s", action, res.Status, e.Code, e.Message)
}

// do sends a request signed with Signature Version 4.
func (s *S3) do(ctx context.Context, method string, u *url.URL, header http.Header, body []byte) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, u.String(), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	for k, v := range header {
		req.Header[k] = v
	}
	req.ContentLength = int64(len(body))
	if len(body) == 0 {
		// Sent with a length of 0, not chunked, which S3 refuses.
		req.Body = http.NoBody
	}
	sum := sha256.Sum256(body)
	s.sign(req, hex.EncodeToString(sum[:]), s.opts.Now().UTC())
	res, err := s.opts.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("reaching the store: %w", err)
	}
	return res, nil
}

// sign adds AWS Signature Version 4 headers to a request whose body has the
// SHA-256 payloadHash (hex): x-amz-date, x-amz-content-sha256 and
// Authorization, over the host, those and any Content-Type or Range.
func (s *S3) sign(req *http.Request, payloadHash string, now time.Time) {
	amzDate := now.Format("20060102T150405Z")
	day := now.Format("20060102")
	req.Header.Set("X-Amz-Date", amzDate)
	req.Header.Set("X-Amz-Content-Sha256", payloadHash)

	signed := map[string]string{"host": req.URL.Host}
	for _, h := range []string{"Content-Type", "Range", "X-Amz-Date", "X-Amz-Content-Sha256"} {
		if v := req.Header.Get(h); v != "" {
			signed[strings.ToLower(h)] = strings.TrimSpace(v)
		}
	}
	names := make([]string, 0, len(signed))
	for k := range signed {
		names = append(names, k)
	}
	sort.Strings(names)
	var canonHeaders strings.Builder
	for _, k := range names {
		canonHeaders.WriteString(k + ":" + signed[k] + "\n")
	}
	signedHeaders := strings.Join(names, ";")

	path := req.URL.EscapedPath()
	if path == "" {
		path = "/"
	}
	canonical := strings.Join([]string{req.Method, path, req.URL.RawQuery, canonHeaders.String(), signedHeaders, payloadHash}, "\n")
	scope := day + "/" + s.opts.Region + "/s3/aws4_request"
	sum := sha256.Sum256([]byte(canonical))
	toSign := "AWS4-HMAC-SHA256\n" + amzDate + "\n" + scope + "\n" + hex.EncodeToString(sum[:])

	key := hmacSHA256([]byte("AWS4"+s.opts.SecretAccessKey), day)
	key = hmacSHA256(key, s.opts.Region)
	key = hmacSHA256(key, "s3")
	key = hmacSHA256(key, "aws4_request")
	signature := hex.EncodeToString(hmacSHA256(key, toSign))
	req.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential="+s.opts.AccessKeyID+"/"+scope+
		", SignedHeaders="+signedHeaders+", Signature="+signature)
}

func hmacSHA256(key []byte, data string) []byte {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(data))
	return m.Sum(nil)
}

// canonicalQuery is a query string as Signature Version 4 signs it, and as
// it is sent: sorted by name, every byte but A-Z a-z 0-9 - _ . ~ encoded as
// %XX.
func canonicalQuery(q url.Values) string {
	if len(q) == 0 {
		return ""
	}
	names := make([]string, 0, len(q))
	for k := range q {
		names = append(names, k)
	}
	sort.Strings(names)
	var parts []string
	for _, k := range names {
		vals := append([]string(nil), q[k]...)
		sort.Strings(vals)
		for _, v := range vals {
			parts = append(parts, awsEscape(k)+"="+awsEscape(v))
		}
	}
	return strings.Join(parts, "&")
}

func awsEscape(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if 'A' <= c && c <= 'Z' || 'a' <= c && c <= 'z' || '0' <= c && c <= '9' || c == '-' || c == '_' || c == '.' || c == '~' {
			b.WriteByte(c)
		} else {
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

// Check writes, reads back and deletes a small object, so a store that
// cannot be reached, or written, is found at startup rather than at the
// first upload.
func Check(ctx context.Context, b Blobs) error {
	key := fmt.Sprintf("bam-check/%d", time.Now().UnixNano())
	want := []byte("bam storage check")
	if err := b.Put(ctx, key, bytes.NewReader(want), int64(len(want)), "text/plain"); err != nil {
		return fmt.Errorf("writing: %w", err)
	}
	defer func() { _ = b.Delete(context.WithoutCancel(ctx), key) }()
	rc, err := b.Get(ctx, key)
	if err != nil {
		return fmt.Errorf("reading back: %w", err)
	}
	got, err := io.ReadAll(rc)
	rc.Close()
	if err != nil {
		return fmt.Errorf("reading back: %w", err)
	}
	if !bytes.Equal(got, want) {
		return errors.New("reading back: what came back was not what was written")
	}
	return nil
}
