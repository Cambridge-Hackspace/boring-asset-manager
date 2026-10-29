// Package blobtest is the conformance suite every filestore.Blobs adapter
// passes (#116): what one store does, every store does.
package blobtest

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/filestore"
)

// Run checks a store, which must start empty, or hold nothing under the
// prefix run/.
func Run(t *testing.T, b filestore.Blobs) {
	t.Helper()
	ctx := context.Background()
	put := func(t *testing.T, key string, data []byte) {
		t.Helper()
		if err := b.Put(ctx, key, bytes.NewReader(data), int64(len(data)), "application/octet-stream"); err != nil {
			t.Fatalf("Put(%s): %v", key, err)
		}
	}
	get := func(t *testing.T, key string) []byte {
		t.Helper()
		rc, err := b.Get(ctx, key)
		if err != nil {
			t.Fatalf("Get(%s): %v", key, err)
		}
		defer rc.Close()
		data, err := io.ReadAll(rc)
		if err != nil {
			t.Fatalf("reading %s: %v", key, err)
		}
		return data
	}
	list := func(t *testing.T, prefix string) []string {
		t.Helper()
		var keys []string
		if err := b.List(ctx, prefix, func(k string) error { keys = append(keys, k); return nil }); err != nil {
			t.Fatalf("List(%q): %v", prefix, err)
		}
		return keys
	}

	t.Run("put and get", func(t *testing.T) {
		put(t, "run/photos/a", []byte("a photo"))
		if got := get(t, "run/photos/a"); string(got) != "a photo" {
			t.Errorf("got %q", got)
		}
	})

	t.Run("overwrite", func(t *testing.T) {
		put(t, "run/photos/b", []byte("first, and longer"))
		put(t, "run/photos/b", []byte("second"))
		if got := get(t, "run/photos/b"); string(got) != "second" {
			t.Errorf("got %q", got)
		}
	})

	t.Run("a missing key", func(t *testing.T) {
		if _, err := b.Get(ctx, "run/photos/missing"); !errors.Is(err, filestore.ErrNotFound) {
			t.Errorf("Get: %v, want ErrNotFound", err)
		}
		if err := b.Delete(ctx, "run/photos/missing"); err != nil {
			t.Errorf("Delete: %v, want nothing", err)
		}
	})

	t.Run("delete", func(t *testing.T) {
		put(t, "run/photos/c", []byte("gone soon"))
		if err := b.Delete(ctx, "run/photos/c"); err != nil {
			t.Fatal(err)
		}
		if _, err := b.Get(ctx, "run/photos/c"); !errors.Is(err, filestore.ErrNotFound) {
			t.Errorf("Get after Delete: %v", err)
		}
	})

	t.Run("list by prefix", func(t *testing.T) {
		put(t, "run/list/images/x", []byte("x"))
		put(t, "run/list/images/y", []byte("y"))
		put(t, "run/list/photos/z", []byte("z"))
		put(t, "run/list/photosynthesis", []byte("not a photo"))
		if got := strings.Join(list(t, "run/list/images/"), ","); got != "run/list/images/x,run/list/images/y" {
			t.Errorf("images/: %s", got)
		}
		if got := strings.Join(list(t, "run/list/photos/"), ","); got != "run/list/photos/z" {
			t.Errorf("photos/: %s", got)
		}
		if got := strings.Join(list(t, "run/list/photo"), ","); got != "run/list/photos/z,run/list/photosynthesis" {
			t.Errorf("photo: %s", got)
		}
		if got := list(t, "run/list/nothing/"); len(got) != 0 {
			t.Errorf("nothing/: %v", got)
		}
		stop := errors.New("stop")
		n := 0
		if err := b.List(ctx, "run/list/", func(string) error { n++; return stop }); !errors.Is(err, stop) || n != 1 {
			t.Errorf("stopping: %v after %d", err, n)
		}
	})

	t.Run("a large object", func(t *testing.T) {
		data := make([]byte, 12<<20+17)
		if _, err := rand.Read(data); err != nil {
			t.Fatal(err)
		}
		put(t, "run/large", data)
		if got := get(t, "run/large"); !bytes.Equal(got, data) {
			t.Errorf("got %d bytes back of %d, or different ones", len(got), len(data))
		}
		// A size not given is read to the end.
		if err := b.Put(ctx, "run/large-unsized", bytes.NewReader(data), -1, ""); err != nil {
			t.Fatal(err)
		}
		if got := get(t, "run/large-unsized"); !bytes.Equal(got, data) {
			t.Errorf("unsized: %d bytes back", len(got))
		}
	})

	t.Run("keys at the length limit", func(t *testing.T) {
		long := "run/" + strings.Repeat("k", filestore.MaxSegment-2) + "/" + strings.Repeat("e", filestore.MaxSegment-1)
		if len(long) != filestore.MaxKey {
			t.Fatalf("made a key of %d", len(long))
		}
		put(t, long, []byte("long"))
		if got := get(t, long); string(got) != "long" {
			t.Errorf("got %q", got)
		}
		for _, bad := range []string{long + "x", "run/" + strings.Repeat("s", filestore.MaxSegment+1), "", "run//x", "run/../x", "/run/x", "run/x/", "run/a b", `run\x`} {
			if err := b.Put(ctx, bad, strings.NewReader("x"), 1, ""); !errors.Is(err, filestore.ErrInvalidKey) {
				t.Errorf("Put(%q): %v, want ErrInvalidKey", bad, err)
			}
			if _, err := b.Get(ctx, bad); !errors.Is(err, filestore.ErrInvalidKey) {
				t.Errorf("Get(%q): %v, want ErrInvalidKey", bad, err)
			}
		}
	})

	t.Run("a wrong size is refused, and leaves nothing", func(t *testing.T) {
		if err := b.Put(ctx, "run/short", strings.NewReader("abc"), 10, ""); err == nil {
			t.Error("a body shorter than its size was stored")
		}
		if _, err := b.Get(ctx, "run/short"); !errors.Is(err, filestore.ErrNotFound) {
			t.Errorf("Get: %v, want ErrNotFound", err)
		}
	})

	t.Run("writes at once", func(t *testing.T) {
		var wg sync.WaitGroup
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				data := bytes.Repeat([]byte{byte('a' + i)}, 64<<10)
				if err := b.Put(ctx, "run/race", bytes.NewReader(data), int64(len(data)), ""); err != nil {
					t.Errorf("Put %d: %v", i, err)
				}
			}(i)
		}
		wg.Wait()
		got := get(t, "run/race")
		if len(got) != 64<<10 || !bytes.Equal(got, bytes.Repeat(got[:1], len(got))) {
			t.Errorf("one write's bytes, whole, should win: %d bytes", len(got))
		}
	})
}
