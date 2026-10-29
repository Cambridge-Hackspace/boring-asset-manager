package filestore

import (
	"bytes"
	"context"
	"errors"
	"io"

	"github.com/google/uuid"
)

// ErrInvalidID is returned for IDs that are not UUIDs, which also keeps keys
// to the one kind of file.
var ErrInvalidID = errors.New("filestore: id must be a UUID")

// Images stores files of one kind by UUID, as <kind>/<uuid> in a Blobs:
// images/<uuid> for label images, photos/<uuid> for asset photos and their
// thumbnails. In a directory, that is storage.local_path/images/<uuid>, as
// it always was.
type Images struct {
	blobs  Blobs
	prefix string
}

// NewImages returns the store of label images in blobs.
func NewImages(blobs Blobs) *Images {
	return &Images{blobs: blobs, prefix: "images/"}
}

// NewPhotos returns the store of asset photos in blobs.
func NewPhotos(blobs Blobs) *Images {
	return &Images{blobs: blobs, prefix: "photos/"}
}

func (s *Images) key(id string) (string, error) {
	parsed, err := uuid.Parse(id)
	if err != nil {
		return "", ErrInvalidID
	}
	return s.prefix + parsed.String(), nil
}

// Save stores data whole: readers never see it part written.
func (s *Images) Save(ctx context.Context, id string, data []byte) error {
	key, err := s.key(id)
	if err != nil {
		return err
	}
	return s.blobs.Put(ctx, key, bytes.NewReader(data), int64(len(data)), "application/octet-stream")
}

// Read returns a file's bytes; ErrNotFound when there is none.
func (s *Images) Read(ctx context.Context, id string) ([]byte, error) {
	key, err := s.key(id)
	if err != nil {
		return nil, err
	}
	rc, err := s.blobs.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(rc)
}

// Remove deletes a file. Removing a missing one is not an error.
func (s *Images) Remove(ctx context.Context, id string) error {
	key, err := s.key(id)
	if err != nil {
		return err
	}
	return s.blobs.Delete(ctx, key)
}
