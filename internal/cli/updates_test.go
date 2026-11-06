package cli

import (
	"path/filepath"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/api"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/config"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// asRelease runs the rest of a test as a build of a release.
func asRelease(t *testing.T, version string) {
	t.Helper()
	was := api.Version
	api.Version = version
	t.Cleanup(func() { api.Version = was })
}

// migratedConfig writes a configuration on a new SQLite database, migrated.
func migratedConfig(t *testing.T) (string, *config.Config) {
	t.Helper()
	dir := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.Database.DSN = filepath.Join(dir, "bam.db")
	cfg.Storage.LocalPath = filepath.Join(dir, "uploads")
	path := filepath.Join(dir, "config.toml")
	if err := cfg.SaveTOML(path); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(path); err != nil {
		t.Fatal(err)
	}
	return path, cfg
}

func kindsRecorded(t *testing.T, cfg *config.Config, kind string) []models.Event {
	t.Helper()
	st, err := store.Open(&cfg.Database)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	all, err := st.ListEvents(100)
	if err != nil {
		t.Fatal(err)
	}
	var out []models.Event
	for _, e := range all {
		if e.Kind == kind {
			out = append(out, e)
		}
	}
	return out
}

func TestServeNotesTheReleaseItRuns(t *testing.T) {
	_, cfg := migratedConfig(t)
	start := func(version string) {
		asRelease(t, version)
		st, err := store.Open(&cfg.Database)
		if err != nil {
			t.Fatal(err)
		}
		defer st.Close()
		noteVersion(st, cfg)
	}
	start("v1.0.0")
	start("dev")
	start("v1.0.0")
	if got := kindsRecorded(t, cfg, models.EventInstanceUpgraded); len(got) != 0 {
		t.Fatalf("before an upgrade: %+v", got)
	}
	start("v1.1.0")
	got := kindsRecorded(t, cfg, models.EventInstanceUpgraded)
	if len(got) != 1 || got[0].Payload.Instance.From != "v1.0.0" || got[0].Payload.Instance.To != "v1.1.0" ||
		got[0].Payload.Instance.NotesURL != "https://github.com/Cambridge-Hackspace/boring-asset-manager/releases/tag/v1.1.0" {
		t.Errorf("the upgrade: %+v", got)
	}
}

func TestUpdateFailedIsRecorded(t *testing.T) {
	path, cfg := migratedConfig(t)
	asRelease(t, "v1.0.0")
	if err := UpdateFailed(path, "", "it did not come up", true); err == nil {
		t.Error("recorded without the release tried")
	}
	if err := UpdateFailed(path, "v1.1.0", "it did not answer its health check within 30 seconds", true); err != nil {
		t.Fatal(err)
	}
	got := kindsRecorded(t, cfg, models.EventInstanceUpdateFailed)
	want := models.EventInstance{From: "v1.0.0", To: "v1.1.0", Reason: "it did not answer its health check within 30 seconds", Restored: true,
		NotesURL: "https://github.com/Cambridge-Hackspace/boring-asset-manager/releases/tag/v1.1.0"}
	if len(got) != 1 || got[0].Payload.Instance == nil || *got[0].Payload.Instance != want {
		t.Errorf("recorded %+v", got)
	}
}
