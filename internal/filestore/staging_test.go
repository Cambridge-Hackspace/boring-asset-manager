package filestore_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/filestore"
)

func TestPartSizes(t *testing.T) {
	for size, want := range map[int64]int64{
		1:         filestore.MinPartSize,
		50 << 30:  filestore.MinPartSize,
		100 << 30: 11 << 20, // 10.24 MiB, rounded up
		5 << 40:   525 << 20,
		filestore.MinPartSize * filestore.MaxParts: filestore.MinPartSize,
	} {
		got := filestore.PartSize(size)
		if got != want {
			t.Errorf("PartSize(%d) = %d, want %d", size, got, want)
		}
		if (size+got-1)/got > filestore.MaxParts {
			t.Errorf("PartSize(%d) makes more than %d parts", size, filestore.MaxParts)
		}
	}
}

// stagings are the stagings to run on: a directory, and an S3 bucket when
// BAM_TEST_S3_ENDPOINT names one.
func stagings(t *testing.T) map[string]filestore.Staging {
	out := map[string]filestore.Staging{"local": filestore.NewLocalStaging(t.TempDir())}
	if s3, ok := testS3(t, fmt.Sprintf("staging-%d/", time.Now().UnixNano())); ok {
		out["s3"] = filestore.NewS3Staging(s3)
	}
	return out
}

// TestStaging stages an upload in parts, one sent twice as after a dropped
// connection, joins them, reads it back, and removes it; and removes one
// never finished.
func TestStaging(t *testing.T) {
	ctx := context.Background()
	for name, st := range stagings(t) {
		t.Run(name, func(t *testing.T) {
			const part = filestore.MinPartSize
			data := make([]byte, 2*part+12345)
			rand.New(rand.NewSource(1)).Read(data)
			id := uuid.NewString()
			token, err := st.Begin(ctx, id, int64(len(data)))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := st.Open(ctx, id); !errors.Is(err, filestore.ErrNotFound) {
				t.Errorf("opened an upload not complete: %v", err)
			}
			var tags []string
			for n, off := 1, 0; off < len(data); n, off = n+1, off+part {
				end := min(off+part, len(data))
				if n == 2 {
					// Cut short, then sent again.
					_, err := st.Part(ctx, id, token, n, int64(off), bytes.NewReader(data[off:off+1000]), int64(end-off))
					if err == nil && name == "local" {
						t.Error("a part cut short was taken")
					}
				}
				tag, err := st.Part(ctx, id, token, n, int64(off), bytes.NewReader(data[off:end]), int64(end-off))
				if err != nil {
					t.Fatalf("part %d: %v", n, err)
				}
				tags = append(tags, tag)
			}
			if err := st.Complete(ctx, id, token, tags); err != nil {
				t.Fatal(err)
			}
			rc, err := st.Open(ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			got, err := io.ReadAll(rc)
			rc.Close()
			if err != nil || !bytes.Equal(got, data) {
				t.Fatalf("read back %d bytes (%v), not the %d staged", len(got), err, len(data))
			}
			if err := st.Remove(ctx, id, token); err != nil {
				t.Fatal(err)
			}
			if _, err := st.Open(ctx, id); !errors.Is(err, filestore.ErrNotFound) {
				t.Errorf("open after removing: %v", err)
			}

			unfinished := uuid.NewString()
			token, err = st.Begin(ctx, unfinished, int64(len(data)))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := st.Part(ctx, unfinished, token, 1, 0, bytes.NewReader(data[:part]), part); err != nil {
				t.Fatal(err)
			}
			if err := st.Remove(ctx, unfinished, token); err != nil {
				t.Errorf("removing an unfinished upload: %v", err)
			}
			if err := st.Remove(ctx, unfinished, token); err != nil {
				t.Errorf("removing it again: %v", err)
			}
		})
	}
}

func TestLocalStagingRefusesWhatWillNotFit(t *testing.T) {
	st := filestore.NewLocalStaging(t.TempDir())
	if _, err := st.Begin(context.Background(), uuid.NewString(), 1<<60); !errors.Is(err, filestore.ErrNoSpace) {
		t.Errorf("began an upload of an exbibyte: %v", err)
	}
	if _, err := st.Begin(context.Background(), "../escape", 1); !errors.Is(err, filestore.ErrInvalidID) {
		t.Errorf("began an upload named ../escape: %v", err)
	}
}
