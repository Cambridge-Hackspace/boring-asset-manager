package store

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/config"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
)

// twoServers opens a store and a second one on the same database, as two
// servers sharing it would: on SQLite a second connection pool on the same
// file, and on PostgreSQL and MariaDB the first store's pool, whose
// connections are as separate as two servers' are.
func twoServers(t *testing.T) (*Store, *Store) {
	t.Helper()
	if dialect := os.Getenv(TestDialectEnv); dialect != "" && dialect != "sqlite" {
		st := setupTestStore(t)
		return st, st
	}
	path := filepath.Join(t.TempDir(), "test.db")
	a, err := OpenMigratedSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	b, err := Open(&config.DatabaseConfig{Type: "sqlite", DSN: path})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = b.Close() })
	return a, b
}

// instanceEvents lists the events of a kind, newest first.
func instanceEvents(t *testing.T, st *Store, kind string) []models.Event {
	t.Helper()
	all, err := st.ListEvents(1000)
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

func TestAnUpgradeIsRecordedOnce(t *testing.T) {
	st := setupTestStore(t)
	channel, err := st.CreateChannel(models.NotificationChannel{Name: "Admins", Driver: "discord", Enabled: true,
		Options: map[string]string{}, Events: []string{models.EventInstanceUpgraded}})
	if err != nil {
		t.Fatal(err)
	}

	// The first start records its release, with no event.
	if got, err := st.NoteVersion("v1.2.0", ""); got != nil || err != nil {
		t.Fatalf("first start: %+v, %v", got, err)
	}
	if v, _ := st.Setting(versionSetting, ""); v != "v1.2.0" {
		t.Errorf("recorded %q", v)
	}
	// Starting again on it records nothing.
	if got, err := st.NoteVersion("v1.2.0", ""); got != nil || err != nil {
		t.Fatalf("restart: %+v, %v", got, err)
	}
	// Builds that are not releases record nothing, and leave the release
	// recorded as it was.
	for _, build := range []string{"dev", "v1.2.0-3-gabc1234", "v1.3.0-dirty", "abc1234"} {
		if got, err := st.NoteVersion(build, ""); got != nil || err != nil {
			t.Fatalf("%s: %+v, %v", build, got, err)
		}
	}
	if v, _ := st.Setting(versionSetting, ""); v != "v1.2.0" {
		t.Errorf("recorded %q after builds that are not releases", v)
	}
	if n := len(instanceEvents(t, st, models.EventInstanceUpgraded)); n != 0 {
		t.Fatalf("%d upgrades recorded before any", n)
	}

	schema, err := st.SchemaVersion()
	if err != nil {
		t.Fatal(err)
	}
	// The schema before is the one recorded with the last release.
	if err := st.SetSetting(schemaSetting, "40"); err != nil {
		t.Fatal(err)
	}
	got, err := st.NoteVersion("v1.3.0", "https://notes.example/v1.3.0")
	if err != nil {
		t.Fatal(err)
	}
	want := models.EventInstance{From: "v1.2.0", To: "v1.3.0", SchemaFrom: 40, SchemaTo: schema, NotesURL: "https://notes.example/v1.3.0"}
	if got == nil || *got != want {
		t.Fatalf("upgrade: %+v, want %+v", got, want)
	}
	events := instanceEvents(t, st, models.EventInstanceUpgraded)
	if len(events) != 1 || events[0].Payload.Instance == nil || *events[0].Payload.Instance != want ||
		events[0].AssetID != nil || events[0].CategoryID != nil || events[0].Payload.Actor != nil {
		t.Fatalf("recorded %+v", events)
	}
	deliveries, err := st.ListChannelDeliveries(channel.ID, 10)
	if err != nil || len(deliveries) != 1 {
		t.Errorf("deliveries: %+v, %v", deliveries, err)
	}
	if got, err := st.NoteVersion("v1.3.0", ""); got != nil || err != nil {
		t.Errorf("restart after the upgrade: %+v, %v", got, err)
	}

	// A downgrade is recorded as one.
	got, err = st.NoteVersion("v1.2.0", "")
	if err != nil || got == nil || !got.Downgrade || got.From != "v1.3.0" || got.To != "v1.2.0" || got.SchemaFrom != schema {
		t.Fatalf("downgrade: %+v, %v", got, err)
	}
	if n := len(instanceEvents(t, st, models.EventInstanceUpgraded)); n != 2 {
		t.Errorf("%d recorded after the downgrade", n)
	}
	// A pre-release is a release.
	if got, err := st.NoteVersion("v1.4.0-rc.1", ""); err != nil || got == nil || got.Downgrade {
		t.Errorf("pre-release: %+v, %v", got, err)
	}
}

func TestServersStartingAtOnceRecordAnUpgradeOnce(t *testing.T) {
	a, b := twoServers(t)
	start := func(version string) int {
		var wg sync.WaitGroup
		var mu sync.Mutex
		recorded := 0
		for i := range 6 {
			st := a
			if i%2 == 1 {
				st = b
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				got, err := st.NoteVersion(version, "")
				if err != nil {
					t.Error(err)
				}
				if got != nil {
					mu.Lock()
					recorded++
					mu.Unlock()
				}
			}()
		}
		wg.Wait()
		return recorded
	}
	// A new instance's servers record its release, and nothing else.
	if n := start("v1.0.0"); n != 0 {
		t.Errorf("first start: %d recorded", n)
	}
	if v, _ := b.Setting(versionSetting, ""); v != "v1.0.0" {
		t.Errorf("recorded %q", v)
	}
	if n := start("v1.1.0"); n != 1 {
		t.Errorf("upgrade: %d answered as recorded", n)
	}
	if n := len(instanceEvents(t, a, models.EventInstanceUpgraded)); n != 1 {
		t.Errorf("upgrade: %d recorded", n)
	}
}

func TestAFailedUpdateIsRecorded(t *testing.T) {
	st := setupTestStore(t)
	inst := models.EventInstance{From: "v1.2.0", To: "v1.3.0", Reason: "it did not answer its health check", Restored: true}
	if err := st.RecordUpdateFailed(inst); err != nil {
		t.Fatal(err)
	}
	events := instanceEvents(t, st, models.EventInstanceUpdateFailed)
	if len(events) != 1 || events[0].Payload.Instance == nil || *events[0].Payload.Instance != inst {
		t.Errorf("recorded %+v", events)
	}
}

func TestTheUpdateCheckIsClaimedOnceADay(t *testing.T) {
	a, b := twoServers(t)
	day := 24 * time.Hour
	now := time.Date(2026, 11, 6, 9, 0, 0, 0, time.UTC)
	claim := func(at time.Time) int {
		var wg sync.WaitGroup
		var mu sync.Mutex
		claimed := 0
		for i := range 6 {
			st := a
			if i%2 == 1 {
				st = b
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				ok, err := st.ClaimUpdateCheck(at, day)
				if err != nil {
					t.Error(err)
				}
				if ok {
					mu.Lock()
					claimed++
					mu.Unlock()
				}
			}()
		}
		wg.Wait()
		return claimed
	}
	if n := claim(now); n != 1 {
		t.Errorf("first check: claimed %d times", n)
	}
	if n := claim(now.Add(23 * time.Hour)); n != 0 {
		t.Errorf("within the day: claimed %d times", n)
	}
	if n := claim(now.Add(day)); n != 1 {
		t.Errorf("a day later: claimed %d times", n)
	}
}

func TestEachReleaseIsAnnouncedOnce(t *testing.T) {
	st := setupTestStore(t)
	announce := func(to string) bool {
		t.Helper()
		ok, err := st.AnnounceRelease(models.EventInstance{From: "v1.0.0", To: to})
		if err != nil {
			t.Fatal(err)
		}
		return ok
	}
	if !announce("v1.1.0") {
		t.Error("v1.1.0 was not announced")
	}
	if announce("v1.1.0") {
		t.Error("v1.1.0 was announced twice")
	}
	if announce("v1.0.5") {
		t.Error("an older release than one announced was announced")
	}
	if !announce("v1.3.0") {
		t.Error("v1.3.0 was not announced")
	}
	events := instanceEvents(t, st, models.EventInstanceUpdateAvailable)
	announced := map[string]bool{}
	for _, e := range events {
		announced[e.Payload.Instance.To] = true
	}
	if len(events) != 2 || !announced["v1.1.0"] || !announced["v1.3.0"] {
		t.Errorf("recorded %+v", events)
	}
}
