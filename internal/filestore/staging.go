package filestore

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/google/uuid"
)

// Staging keeps uploads under way, and archives uploaded, until they are
// used (#141): in a directory beside the files, or in their bucket as an
// S3 multipart upload, so that with S3 nothing is kept on bam's own disk.
// An upload arrives in parts, numbered from 1, all PartSize long but the
// last, in order.
type Staging interface {
	// Begin starts staging an upload of size bytes, refusing one that will
	// not fit (ErrNoSpace). It returns a token the other calls take: S3's
	// upload ID, or "".
	Begin(ctx context.Context, id string, size int64) (string, error)
	// Part stores part n, size bytes read from r, at offset in the upload,
	// replacing any part n stored before. It returns the part's tag, which
	// Complete takes.
	Part(ctx context.Context, id, token string, n int, offset int64, r io.Reader, size int64) (string, error)
	// Complete joins the parts stored, given their tags in order.
	Complete(ctx context.Context, id, token string, tags []string) error
	// Open reads an upload once complete.
	Open(ctx context.Context, id string) (io.ReadCloser, error)
	// Remove removes an upload, complete or not; one not there is not an
	// error.
	Remove(ctx context.Context, id, token string) error
}

// ErrNoSpace is returned when an upload will not fit where it is staged.
var ErrNoSpace = errors.New("filestore: not enough space to stage the upload")

// MinPartSize is the size of an upload's parts, unless the upload is so
// large that more would be needed than MaxParts: S3 takes at most 10,000
// parts, each but the last at least 5 MiB.
const (
	MinPartSize = 8 << 20
	MaxParts    = 10000
)

// PartSize is the size of the parts of an upload of size bytes: a whole
// number of MiB, at least MinPartSize, and few enough for MaxParts.
func PartSize(size int64) int64 {
	p := (size + MaxParts - 1) / MaxParts
	p = (p + 1<<20 - 1) &^ (1<<20 - 1)
	if p < MinPartSize {
		p = MinPartSize
	}
	return p
}

func checkStagingID(id string) error {
	if _, err := uuid.Parse(id); err != nil {
		return ErrInvalidID
	}
	return nil
}

// LocalStaging stages uploads as files in a directory: <id>.partial while
// parts arrive, <id> once complete.
type LocalStaging struct{ dir string }

// NewLocalStaging stages uploads in dir, made when first needed.
func NewLocalStaging(dir string) *LocalStaging { return &LocalStaging{dir: dir} }

func (l *LocalStaging) String() string { return "the directory " + l.dir }

func (l *LocalStaging) path(id string, partial bool) (string, error) {
	if err := checkStagingID(id); err != nil {
		return "", err
	}
	if partial {
		return filepath.Join(l.dir, id+".partial"), nil
	}
	return filepath.Join(l.dir, id), nil
}

// Begin makes the upload's file, once there is room for it.
func (l *LocalStaging) Begin(_ context.Context, id string, size int64) (string, error) {
	path, err := l.path(id, true)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(l.dir, 0o700); err != nil {
		return "", err
	}
	if free, err := freeSpace(l.dir); err == nil && free >= 0 && free < size {
		return "", fmt.Errorf("%w: %d bytes are free in %s, and it is %d", ErrNoSpace, free, l.dir, size)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return "", err
	}
	return "", f.Close()
}

// Part writes the part at its offset in the file.
func (l *LocalStaging) Part(_ context.Context, id, _ string, _ int, offset int64, r io.Reader, size int64) (string, error) {
	path, err := l.path(id, true)
	if err != nil {
		return "", err
	}
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return "", err
	}
	n, err := io.CopyN(f, r, size)
	if err != nil {
		return "", fmt.Errorf("part of %d bytes ended at %d: %w", size, n, err)
	}
	return "", f.Close()
}

// Complete names the file as complete.
func (l *LocalStaging) Complete(_ context.Context, id, _ string, _ []string) error {
	from, err := l.path(id, true)
	if err != nil {
		return err
	}
	to, _ := l.path(id, false)
	return os.Rename(from, to)
}

// Open opens a complete upload.
func (l *LocalStaging) Open(_ context.Context, id string) (io.ReadCloser, error) {
	path, err := l.path(id, false)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotFound
	}
	return f, err
}

// Remove removes the upload's file, complete or not.
func (l *LocalStaging) Remove(_ context.Context, id, _ string) error {
	for _, partial := range []bool{true, false} {
		path, err := l.path(id, partial)
		if err != nil {
			return err
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

// S3Staging stages uploads in a bucket, as staging/<id>: each a multipart
// upload, one part for each of the upload's, completed into one object.
type S3Staging struct{ s3 *S3 }

// NewS3Staging stages uploads in the bucket s3 keeps files in.
func NewS3Staging(s3 *S3) *S3Staging { return &S3Staging{s3: s3} }

func (s *S3Staging) String() string { return "staging/ in " + s.s3.String() }

func (s *S3Staging) name(id string) (string, error) {
	if err := checkStagingID(id); err != nil {
		return "", err
	}
	return s.s3.name("staging/" + id)
}

// Begin starts a multipart upload (CreateMultipartUpload).
func (s *S3Staging) Begin(ctx context.Context, id string, size int64) (string, error) {
	name, err := s.name(id)
	if err != nil {
		return "", err
	}
	if size > MaxParts*int64(5<<30) {
		return "", fmt.Errorf("%w: S3 keeps objects of at most 5 TiB", ErrNoSpace)
	}
	res, err := s.s3.do(ctx, http.MethodPost, s.s3.objectURL(name, url.Values{"uploads": {""}}), nil, nil)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode/100 != 2 {
		return "", s3Error(res, "staging an upload")
	}
	var out struct {
		UploadID string `xml:"UploadId"`
	}
	if err := xml.NewDecoder(res.Body).Decode(&out); err != nil || out.UploadID == "" {
		return "", fmt.Errorf("staging an upload: the store gave no upload ID (%v)", err)
	}
	return out.UploadID, nil
}

// Part sends the part (UploadPart) as it is read: its payload is not
// signed, so it need not be held to be hashed first.
func (s *S3Staging) Part(ctx context.Context, id, token string, n int, _ int64, r io.Reader, size int64) (string, error) {
	name, err := s.name(id)
	if err != nil {
		return "", err
	}
	u := s.s3.objectURL(name, url.Values{"partNumber": {strconv.Itoa(n)}, "uploadId": {token}})
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, u.String(), io.LimitReader(r, size))
	if err != nil {
		return "", err
	}
	req.ContentLength = size
	s.s3.sign(req, "UNSIGNED-PAYLOAD", s.s3.opts.Now().UTC())
	res, err := s.s3.stream.Do(req)
	if err != nil {
		return "", fmt.Errorf("reaching the store: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode/100 != 2 {
		return "", s3Error(res, fmt.Sprintf("staging part %d", n))
	}
	tag := res.Header.Get("ETag")
	if tag == "" {
		return "", fmt.Errorf("staging part %d: the store gave no ETag", n)
	}
	return tag, nil
}

// Complete joins the parts (CompleteMultipartUpload).
func (s *S3Staging) Complete(ctx context.Context, id, token string, tags []string) error {
	name, err := s.name(id)
	if err != nil {
		return err
	}
	type part struct {
		PartNumber int    `xml:"PartNumber"`
		ETag       string `xml:"ETag"`
	}
	body := struct {
		XMLName xml.Name `xml:"CompleteMultipartUpload"`
		Parts   []part   `xml:"Part"`
	}{}
	for i, t := range tags {
		body.Parts = append(body.Parts, part{i + 1, t})
	}
	data, err := xml.Marshal(body)
	if err != nil {
		return err
	}
	header := http.Header{"Content-Type": {"application/xml"}}
	res, err := s.s3.do(ctx, http.MethodPost, s.s3.objectURL(name, url.Values{"uploadId": {token}}), header, data)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode/100 != 2 {
		return s3Error(res, "completing a staged upload")
	}
	// S3 may answer 200 and say in the body that it failed.
	answer, _ := io.ReadAll(io.LimitReader(res.Body, 64<<10))
	if bytes.Contains(answer, []byte("<Error>")) {
		return fmt.Errorf("completing a staged upload: %s", strings.TrimSpace(string(answer)))
	}
	return nil
}

// Open reads the completed object as it streams, however long that takes.
func (s *S3Staging) Open(ctx context.Context, id string) (io.ReadCloser, error) {
	name, err := s.name(id)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.s3.objectURL(name, nil).String(), http.NoBody)
	if err != nil {
		return nil, err
	}
	s.s3.sign(req, emptyHash, s.s3.opts.Now().UTC())
	res, err := s.s3.stream.Do(req)
	if err != nil {
		return nil, fmt.Errorf("reaching the store: %w", err)
	}
	if res.StatusCode == http.StatusNotFound {
		res.Body.Close()
		return nil, ErrNotFound
	}
	if res.StatusCode/100 != 2 {
		defer res.Body.Close()
		return nil, s3Error(res, "reading a staged upload")
	}
	return res.Body, nil
}

// Remove aborts the multipart upload, if it is not complete, and deletes
// the object, if it is.
func (s *S3Staging) Remove(ctx context.Context, id, token string) error {
	name, err := s.name(id)
	if err != nil {
		return err
	}
	if token != "" {
		res, err := s.s3.do(ctx, http.MethodDelete, s.s3.objectURL(name, url.Values{"uploadId": {token}}), nil, nil)
		if err != nil {
			return err
		}
		res.Body.Close()
		if res.StatusCode/100 != 2 && res.StatusCode != http.StatusNotFound {
			return s3Error(res, "removing a staged upload")
		}
	}
	return s.s3.Delete(ctx, "staging/"+id)
}

// emptyHash is the SHA-256 of nothing, a body-less request's payload hash.
const emptyHash = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
