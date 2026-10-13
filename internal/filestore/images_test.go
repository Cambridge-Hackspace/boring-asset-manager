package filestore

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
)

func TestImagesRoundTrip(t *testing.T) {
	root := t.TempDir()
	s := NewImages(root)
	id := uuid.NewString()

	if err := s.Save(id, []byte("png bytes")); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := s.Read(id)
	if err != nil || !bytes.Equal(got, []byte("png bytes")) {
		t.Fatalf("Read() = %q, %v", got, err)
	}
	if err := s.Save(id, []byte("replaced")); err != nil {
		t.Fatalf("overwriting: %v", err)
	}

	entries, _ := os.ReadDir(filepath.Join(root, "images"))
	if len(entries) != 1 || entries[0].Name() != id {
		t.Errorf("unexpected files left behind: %v", entries)
	}

	if err := s.Remove(id); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if err := s.Remove(id); err != nil {
		t.Errorf("removing twice: %v", err)
	}
	if _, err := s.Read(id); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Read after Remove: %v", err)
	}
}

func TestImagesRejectPathsOutsideTheStore(t *testing.T) {
	s := NewImages(t.TempDir())
	for _, id := range []string{"../config.toml", "", "a/b", `..\x`} {
		if err := s.Save(id, nil); !errors.Is(err, ErrInvalidID) {
			t.Errorf("Save(%q): got %v, want ErrInvalidID", id, err)
		}
		if _, err := s.Read(id); !errors.Is(err, ErrInvalidID) {
			t.Errorf("Read(%q): got %v, want ErrInvalidID", id, err)
		}
	}
}
