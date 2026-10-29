package filestore_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/filestore"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/filestore/blobtest"
	"github.com/google/uuid"
)

func TestLocalConforms(t *testing.T) {
	blobtest.Run(t, filestore.NewLocal(t.TempDir()))
}

// TestLocalKeepsTheLayout: files stay where they always were, so an
// existing installation needs no migration, and a write cut short leaves no
// file listed.
func TestLocalKeepsTheLayout(t *testing.T) {
	root := t.TempDir()
	ctx := context.Background()
	images := filestore.NewImages(filestore.NewLocal(root))
	id := uuid.NewString()
	if err := images.Save(ctx, id, []byte("png bytes")); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(filepath.Join(root, "images", id)); err != nil || string(data) != "png bytes" {
		t.Errorf("on disk: %q %v", data, err)
	}
	if err := os.WriteFile(filepath.Join(root, "images", ".upload-123"), []byte("half"), 0o644); err != nil {
		t.Fatal(err)
	}
	var keys []string
	_ = filestore.NewLocal(root).List(ctx, "images/", func(o filestore.Object) error { keys = append(keys, o.Key); return nil })
	if len(keys) != 1 || keys[0] != "images/"+id {
		t.Errorf("listed: %v", keys)
	}
}

func TestImagesRoundTrip(t *testing.T) {
	ctx := context.Background()
	s := filestore.NewPhotos(filestore.NewLocal(t.TempDir()))
	id := uuid.NewString()

	if err := s.Save(ctx, id, []byte("jpeg bytes")); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := s.Read(ctx, id)
	if err != nil || !bytes.Equal(got, []byte("jpeg bytes")) {
		t.Fatalf("Read() = %q, %v", got, err)
	}
	if err := s.Remove(ctx, id); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if err := s.Remove(ctx, id); err != nil {
		t.Errorf("removing twice: %v", err)
	}
	if _, err := s.Read(ctx, id); !errors.Is(err, filestore.ErrNotFound) {
		t.Errorf("Read after Remove: %v", err)
	}
}

func TestImagesRejectPathsOutsideTheStore(t *testing.T) {
	ctx := context.Background()
	s := filestore.NewImages(filestore.NewLocal(t.TempDir()))
	for _, id := range []string{"../config.toml", "", "a/b", `..\x`} {
		if err := s.Save(ctx, id, nil); !errors.Is(err, filestore.ErrInvalidID) {
			t.Errorf("Save(%q): got %v, want ErrInvalidID", id, err)
		}
		if _, err := s.Read(ctx, id); !errors.Is(err, filestore.ErrInvalidID) {
			t.Errorf("Read(%q): got %v, want ErrInvalidID", id, err)
		}
	}
}
