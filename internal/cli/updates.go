package cli

import (
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/api"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/config"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/release"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// noteVersion records, as bam serve starts, the release it runs, and an
// upgrade or a downgrade from the one recorded before (docs/installing.md),
// by whatever route it came. A failure is logged: it is no reason not to
// serve.
func noteVersion(st *store.Store, cfg *config.Config) {
	notes, _ := release.Pages(cfg.Updates.URL, api.Version)
	inst, err := st.NoteVersion(api.Version, notes)
	switch {
	case err != nil:
		log.Printf("Recording the release running: %v", err)
	case inst != nil && inst.Downgrade:
		log.Printf("Downgraded from %s to %s", inst.From, inst.To)
	case inst != nil:
		log.Printf("Upgraded from %s to %s", inst.From, inst.To)
	}
}

// UpdateFailed records that an update to the release tried failed, why,
// and whether the database was restored, as instance.update_failed: for
// bam-update, once it has put this release back and it is running again,
// since restoring the database takes with it anything recorded before.
func UpdateFailed(configPath, tried, reason string, restored bool) error {
	tried, reason = strings.TrimSpace(tried), strings.TrimSpace(reason)
	if tried == "" {
		return errors.New("--tried is required")
	}
	if len(tried) > 64 || len(reason) > 500 {
		return errors.New("--tried is at most 64 characters, and --reason 500")
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("loading configuration: %w", err)
	}
	st, err := store.Open(&cfg.Database)
	if err != nil {
		return fmt.Errorf("opening database (%s): %w", cfg.Database.Type, err)
	}
	defer st.Close()
	notes, _ := release.Pages(cfg.Updates.URL, tried)
	inst := models.EventInstance{From: api.Version, To: tried, Reason: reason, Restored: restored, NotesURL: notes}
	if err := st.RecordUpdateFailed(inst); err != nil {
		return fmt.Errorf("recording the failed update: %w", err)
	}
	fmt.Printf("Recorded that the update to %s failed.\n", tried)
	return nil
}
