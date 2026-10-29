package filestore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"sort"
	"time"
)

// Kinds are the prefixes of the files bam keeps: what a sweep looks
// through, and a copy copies.
var Kinds = []string{"images/", "photos/"}

// SweepGrace is how old a file no row names must be before a sweep removes
// it: a file is written a moment before its row, so one younger may be an
// upload under way.
const SweepGrace = 24 * time.Hour

// SweepResult is what a sweep found: files no row names that it removed
// (or, in a dry run, would remove), and those it kept, younger than the
// grace period; and keys rows name that have no file.
type SweepResult struct {
	Removed []string `json:"removed"`
	Kept    []string `json:"kept"`
	Missing []string `json:"missing"`
}

// Sweep keeps a store in step with the database (#116), which a file and
// its row cannot be written in one transaction with: it removes each file
// of Kinds that no row names (named holds the keys rows name) once it is
// older than grace, and reports the keys rows name with no file. A dry run
// removes nothing.
func Sweep(ctx context.Context, b Blobs, named map[string]bool, grace time.Duration, now time.Time, dryRun bool) (SweepResult, error) {
	res := SweepResult{Removed: []string{}, Kept: []string{}, Missing: []string{}}
	seen := map[string]bool{}
	var orphans []Object
	for _, prefix := range Kinds {
		err := b.List(ctx, prefix, func(o Object) error {
			seen[o.Key] = true
			if !named[o.Key] {
				orphans = append(orphans, o)
			}
			return nil
		})
		if err != nil {
			return res, fmt.Errorf("listing %s: %w", prefix, err)
		}
	}
	for _, o := range orphans {
		if now.Sub(o.Modified) < grace {
			res.Kept = append(res.Kept, o.Key)
			continue
		}
		if !dryRun {
			if err := b.Delete(ctx, o.Key); err != nil {
				return res, fmt.Errorf("removing %s: %w", o.Key, err)
			}
		}
		res.Removed = append(res.Removed, o.Key)
	}
	for k := range named {
		if !seen[k] {
			res.Missing = append(res.Missing, k)
		}
	}
	sort.Strings(res.Missing)
	return res, nil
}

// Copy copies every file of Kinds from one store to another, reading each
// back from where it went to check it is the same, and says each key it
// copied to progress, which may be nil. It returns how many it copied.
func Copy(ctx context.Context, from, to Blobs, progress func(key string)) (int, error) {
	n := 0
	for _, prefix := range Kinds {
		var keys []Object
		if err := from.List(ctx, prefix, func(o Object) error { keys = append(keys, o); return nil }); err != nil {
			return n, fmt.Errorf("listing %s: %w", prefix, err)
		}
		for _, o := range keys {
			data, err := readAll(ctx, from, o.Key)
			if err != nil {
				return n, fmt.Errorf("reading %s: %w", o.Key, err)
			}
			if err := to.Put(ctx, o.Key, bytes.NewReader(data), int64(len(data)), "application/octet-stream"); err != nil {
				return n, fmt.Errorf("writing %s: %w", o.Key, err)
			}
			back, err := readAll(ctx, to, o.Key)
			if err != nil {
				return n, fmt.Errorf("reading %s back: %w", o.Key, err)
			}
			if sha256.Sum256(back) != sha256.Sum256(data) {
				return n, fmt.Errorf("%s came back different from what was written", o.Key)
			}
			n++
			if progress != nil {
				progress(o.Key)
			}
		}
	}
	return n, nil
}

func readAll(ctx context.Context, b Blobs, key string) ([]byte, error) {
	rc, err := b.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(rc)
}
