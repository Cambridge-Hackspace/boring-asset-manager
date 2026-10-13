// Package filestore keeps uploaded files under the configured storage directory.
package filestore

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/google/uuid"
)

// ErrInvalidID is returned for IDs that are not UUIDs, which also keeps paths
// inside the storage directory.
var ErrInvalidID = errors.New("filestore: id must be a UUID")

// Images stores image bytes as storage.local_path/images/<uuid>.
type Images struct {
	dir string
}

// NewImages returns an image store rooted at root/images. The directory is
// created on first write.
func NewImages(root string) *Images {
	return &Images{dir: filepath.Join(root, "images")}
}

func (s *Images) path(id string) (string, error) {
	parsed, err := uuid.Parse(id)
	if err != nil {
		return "", ErrInvalidID
	}
	return filepath.Join(s.dir, parsed.String()), nil
}

// Save writes data atomically: readers never observe a partial file.
func (s *Images) Save(id string, data []byte) error {
	dest, err := s.path(id)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return fmt.Errorf("creating image directory: %w", err)
	}
	tmp, err := os.CreateTemp(s.dir, ".upload-*")
	if err != nil {
		return fmt.Errorf("creating temporary image file: %w", err)
	}
	defer os.Remove(tmp.Name()) // no-op once renamed

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("writing image: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("writing image: %w", err)
	}
	if err := os.Rename(tmp.Name(), dest); err != nil {
		return fmt.Errorf("storing image: %w", err)
	}
	return nil
}

// Read returns an image's bytes.
func (s *Images) Read(id string) ([]byte, error) {
	p, err := s.path(id)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(p)
}

// Remove deletes an image's bytes. Removing a missing image is not an error.
func (s *Images) Remove(id string) error {
	p, err := s.path(id)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
