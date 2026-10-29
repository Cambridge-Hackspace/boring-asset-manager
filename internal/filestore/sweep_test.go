package filestore_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/filestore"
)

// TestSweep: a file no row names is removed once older than the grace
// period, and not before; a row's file stays; a row with no file is
// reported; a dry run removes nothing, and says what it would.
func TestSweep(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	b := filestore.NewLocal(root)
	now := time.Now()
	old := now.Add(-2 * filestore.SweepGrace)
	for key, at := range map[string]time.Time{
		"photos/kept": old, "photos/orphan": old, "photos/young": now.Add(-time.Minute),
		"images/kept": old, "images/orphan": old, "elsewhere/not-ours": old,
	} {
		if err := b.Put(ctx, key, strings.NewReader("x"), 1, ""); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(filepath.Join(root, filepath.FromSlash(key)), at, at); err != nil {
			t.Fatal(err)
		}
	}
	named := map[string]bool{"photos/kept": true, "images/kept": true, "photos/gone": true}

	dry, err := filestore.Sweep(ctx, b, named, filestore.SweepGrace, now, true)
	if err != nil {
		t.Fatal(err)
	}
	want := filestore.SweepResult{Removed: []string{"images/orphan", "photos/orphan"}, Kept: []string{"photos/young"}, Missing: []string{"photos/gone"}}
	if !reflect.DeepEqual(dry, want) {
		t.Errorf("dry run: %+v", dry)
	}
	if _, err := b.Get(ctx, "photos/orphan"); err != nil {
		t.Errorf("a dry run removed a file: %v", err)
	}

	res, err := filestore.Sweep(ctx, b, named, filestore.SweepGrace, now, false)
	if err != nil || !reflect.DeepEqual(res, want) {
		t.Fatalf("sweep: %+v %v", res, err)
	}
	for key, gone := range map[string]bool{"photos/orphan": true, "images/orphan": true, "photos/young": false, "photos/kept": false, "images/kept": false, "elsewhere/not-ours": false} {
		if _, err := b.Get(ctx, key); errors.Is(err, filestore.ErrNotFound) != gone {
			t.Errorf("%s: gone %v, want %v", key, err != nil, gone)
		}
	}
}

// TestCopy: every file of bam's goes to the other store, the same; others
// stay where they were.
func TestCopy(t *testing.T) {
	ctx := context.Background()
	from, to := filestore.NewLocal(t.TempDir()), filestore.NewLocal(t.TempDir())
	for key, data := range map[string]string{"photos/a": "a photo", "photos/a-thumb": "small", "images/logo": "png", "elsewhere/x": "not ours"} {
		if err := from.Put(ctx, key, strings.NewReader(data), int64(len(data)), ""); err != nil {
			t.Fatal(err)
		}
	}
	var copied []string
	n, err := filestore.Copy(ctx, from, to, func(k string) { copied = append(copied, k) })
	if err != nil || n != 3 || strings.Join(copied, ",") != "images/logo,photos/a,photos/a-thumb" {
		t.Fatalf("copied %d %v %v", n, copied, err)
	}
	rc, err := to.Get(ctx, "photos/a")
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	buf := make([]byte, 16)
	if k, _ := rc.Read(buf); string(buf[:k]) != "a photo" {
		t.Errorf("copied %q", buf[:k])
	}
	if _, err := to.Get(ctx, "elsewhere/x"); !errors.Is(err, filestore.ErrNotFound) {
		t.Errorf("copied what is not bam's: %v", err)
	}
}
