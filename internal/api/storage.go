package api

import (
	"context"
	"log"
	"strings"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/filestore"
)

// SweepInterval is how often RunSweep sweeps the file store.
const SweepInterval = 6 * time.Hour

// SweepStorage keeps the file store in step with the database (README §2):
// it removes the files no row names, once older than a day, and reports the
// rows whose file is missing. A dry run removes nothing.
func (s *Server) SweepStorage(ctx context.Context, dryRun bool) (filestore.SweepResult, error) {
	named, err := s.store.StoredFiles()
	if err != nil {
		return filestore.SweepResult{}, err
	}
	return filestore.Sweep(ctx, s.blobs, named, filestore.SweepGrace, time.Now(), dryRun)
}

// RunSweep sweeps the file store at startup and every SweepInterval after,
// until ctx ends, logging what it finds.
func (s *Server) RunSweep(ctx context.Context) {
	for {
		res, err := s.SweepStorage(ctx, false)
		switch {
		case err != nil && ctx.Err() == nil:
			log.Printf("storage sweep: %v", err)
		case err == nil:
			if len(res.Removed) > 0 {
				log.Printf("storage sweep: removed %d files no row named", len(res.Removed))
			}
			if len(res.Missing) > 0 {
				shown := res.Missing
				if len(shown) > 10 {
					shown = shown[:10]
				}
				log.Printf("storage sweep: %d files rows name are missing from the store: %s", len(res.Missing), strings.Join(shown, ", "))
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(SweepInterval):
		}
	}
}
