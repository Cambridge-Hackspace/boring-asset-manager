// Package filestore keeps uploaded files (asset photos and their
// thumbnails, label images) through one interface, Blobs, so the rest of
// bam treats a file in a directory and an object in a bucket the same
// (README §2).
package filestore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Blobs stores files by key, a path of segments separated by "/", such as
// photos/<id>. A key names one file; putting it again replaces it whole, and
// a reader never sees a file part written.
type Blobs interface {
	// Put stores what r holds under key. size is its length when known,
	// or -1.
	Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error
	// Get opens what key holds; ErrNotFound when it holds nothing.
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	// Delete removes what key holds. A key holding nothing is not an error.
	Delete(ctx context.Context, key string) error
	// List calls fn with each key starting with prefix, in order, until fn
	// returns an error, which List returns.
	List(ctx context.Context, prefix string, fn func(key string) error) error
}

var (
	// ErrNotFound is returned for a key that holds nothing.
	ErrNotFound = errors.New("filestore: not found")
	// ErrInvalidKey is returned for a key that cannot name a file: empty,
	// too long, or with a segment that is empty, . or .., longer than
	// MaxSegment, or holds other than letters, digits, ., - and _.
	ErrInvalidKey = errors.New("filestore: invalid key")
)

// MaxKey is the longest key, in bytes: well inside every store's limit.
// MaxSegment is the longest segment of one, as most filesystems allow a
// file's name.
const (
	MaxKey     = 512
	MaxSegment = 255
)

var keySegment = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// ValidKey reports whether key can name a file in any store.
func ValidKey(key string) bool {
	if key == "" || len(key) > MaxKey {
		return false
	}
	for _, seg := range strings.Split(key, "/") {
		if len(seg) > MaxSegment || !keySegment.MatchString(seg) || seg == "." || seg == ".." {
			return false
		}
	}
	return true
}

// Local keeps files in a directory: each key a file at that path below it,
// written to a temporary file and renamed into place.
type Local struct {
	root string
}

// NewLocal returns the store of files under root, made as files are first
// written.
func NewLocal(root string) *Local {
	return &Local{root: root}
}

// String says which store this is, for the log.
func (l *Local) String() string {
	return "the directory " + l.root
}

// tempPrefix starts the temporary files a write leaves when it is cut
// short; List does not list them.
const tempPrefix = ".upload-"

func (l *Local) path(key string) (string, error) {
	if !ValidKey(key) {
		return "", ErrInvalidKey
	}
	return filepath.Join(l.root, filepath.FromSlash(key)), nil
}

// Put writes the file atomically: readers never see it part written.
func (l *Local) Put(ctx context.Context, key string, r io.Reader, size int64, _ string) error {
	dest, err := l.path(key)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	dir := filepath.Dir(dest)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, tempPrefix+"*")
	if err != nil {
		return fmt.Errorf("creating a temporary file: %w", err)
	}
	defer os.Remove(tmp.Name()) // no-op once renamed

	n, err := io.Copy(tmp, r)
	if err == nil && size >= 0 && n != size {
		err = fmt.Errorf("wrote %d bytes, expected %d", n, size)
	}
	if err != nil {
		tmp.Close()
		return fmt.Errorf("writing %s: %w", key, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("writing %s: %w", key, err)
	}
	if err := os.Rename(tmp.Name(), dest); err != nil {
		return fmt.Errorf("storing %s: %w", key, err)
	}
	return nil
}

// Get opens a file.
func (l *Local) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	p, err := l.path(key)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f, err := os.Open(p)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotFound
	}
	return f, err
}

// Delete removes a file.
func (l *Local) Delete(ctx context.Context, key string) error {
	p, err := l.path(key)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// List walks the files below the directory prefix names, or, for a prefix
// ending part way through a segment, below its parent, keeping those that
// start with it.
func (l *Local) List(ctx context.Context, prefix string, fn func(key string) error) error {
	start := l.root
	if dir := prefix[:strings.LastIndex(prefix, "/")+1]; dir != "" {
		if !ValidKey(strings.TrimSuffix(dir, "/")) {
			return ErrInvalidKey
		}
		start = filepath.Join(l.root, filepath.FromSlash(dir))
	}
	var keys []string
	err := filepath.WalkDir(start, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if d.IsDir() || strings.HasPrefix(d.Name(), tempPrefix) {
			return nil
		}
		rel, err := filepath.Rel(l.root, p)
		if err != nil {
			return err
		}
		if key := filepath.ToSlash(rel); strings.HasPrefix(key, prefix) {
			keys = append(keys, key)
		}
		return nil
	})
	if err != nil {
		return err
	}
	sort.Strings(keys)
	for _, k := range keys {
		if err := fn(k); err != nil {
			return err
		}
	}
	return nil
}
