package notify

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/config"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// fakeReleases stands in for GitHub's releases API, answering what it is
// given and counting the requests.
type fakeReleases struct {
	srv *httptest.Server

	mu     sync.Mutex
	status int
	body   string
	asked  int
	agents []string
}

func newFakeReleases(t *testing.T, body string) *fakeReleases {
	t.Helper()
	f := &fakeReleases{status: http.StatusOK, body: body}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.asked++
		f.agents = append(f.agents, r.Header.Get("User-Agent"))
		status, body := f.status, f.body
		f.mu.Unlock()
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeReleases) answer(status int, body string) {
	f.mu.Lock()
	f.status, f.body = status, body
	f.mu.Unlock()
}

func (f *fakeReleases) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.asked
}

// latestRelease is a release as GitHub's releases/latest answers it.
func latestRelease(tag string) string {
	return `{"tag_name":"` + tag + `","html_url":"https://releases.example/` + tag + `","draft":false,"prerelease":false}`
}

func updateWorker(st *store.Store, c *clock, url, version string) *Worker {
	return &Worker{Store: st, Now: c.Now, Updates: &UpdateCheck{URL: url, Version: version}}
}

func updateEvents(t *testing.T, st *store.Store) []models.Event {
	t.Helper()
	all, err := st.ListEvents(100)
	if err != nil {
		t.Fatal(err)
	}
	var out []models.Event
	for _, e := range all {
		if e.Kind == models.EventInstanceUpdateAvailable {
			out = append(out, e)
		}
	}
	return out
}

func TestTheUpdateCheckIsOffUnlessTurnedOn(t *testing.T) {
	if c := NewUpdateCheck(config.DefaultConfig().Updates, "v1.0.0"); c != nil {
		t.Fatalf("on by default: %+v", c)
	}
	f := newFakeReleases(t, latestRelease("v2.0.0"))
	st := openStore(t)
	w := &Worker{Store: st, Updates: NewUpdateCheck(config.UpdatesConfig{URL: f.srv.URL}, "v1.0.0")}
	if tag, err := w.CheckForUpdates(context.Background()); tag != "" || err != nil || f.count() != 0 {
		t.Errorf("off: %q, %v, asked %d times", tag, err, f.count())
	}
	if c := NewUpdateCheck(config.UpdatesConfig{Check: true, URL: f.srv.URL}, "v1.0.0"); c == nil || c.URL != f.srv.URL || c.Version != "v1.0.0" {
		t.Errorf("on: %+v", c)
	}
}

func TestTheUpdateCheckAnnouncesANewerReleaseOnce(t *testing.T) {
	f := newFakeReleases(t, latestRelease("v1.1.0"))
	st := openStore(t)
	c := &clock{now: time.Date(2026, 11, 6, 9, 0, 0, 0, time.UTC)}
	w := updateWorker(st, c, f.srv.URL, "v1.0.0")
	tag, err := w.CheckForUpdates(context.Background())
	if err != nil || tag != "v1.1.0" {
		t.Fatalf("first check: %q, %v", tag, err)
	}
	events := updateEvents(t, st)
	want := models.EventInstance{From: "v1.0.0", To: "v1.1.0", NotesURL: "https://releases.example/v1.1.0"}
	if len(events) != 1 || events[0].Payload.Instance == nil || *events[0].Payload.Instance != want {
		t.Fatalf("recorded %+v", events)
	}
	if f.agents[0] != "BAM!/v1.0.0" {
		t.Errorf("asked as %q", f.agents[0])
	}
	// The next day, the same release is not announced again.
	c.advance(24 * time.Hour)
	if tag, err := w.CheckForUpdates(context.Background()); tag != "" || err != nil || f.count() != 2 {
		t.Errorf("the next day: %q, %v, asked %d times", tag, err, f.count())
	}
	// A newer one is.
	f.answer(http.StatusOK, latestRelease("v1.2.0"))
	c.advance(24 * time.Hour)
	if tag, err := w.CheckForUpdates(context.Background()); tag != "v1.2.0" || err != nil {
		t.Errorf("a newer release: %q, %v", tag, err)
	}
	if n := len(updateEvents(t, st)); n != 2 {
		t.Errorf("%d announced", n)
	}
}

func TestTheUpdateCheckIgnoresWhatIsNotANewerFullRelease(t *testing.T) {
	for name, c := range map[string]struct {
		running, answer string
		want            string
	}{
		"the same release":           {"v1.2.0", latestRelease("v1.2.0"), ""},
		"an older release":           {"v1.2.0", latestRelease("v1.1.9"), ""},
		"a pre-release, by its tag":  {"v1.2.0", latestRelease("v1.3.0-rc.1"), ""},
		"a pre-release, by its flag": {"v1.2.0", `{"tag_name":"v1.3.0","prerelease":true}`, ""},
		"a draft":                    {"v1.2.0", `{"tag_name":"v1.3.0","draft":true}`, ""},
		"a tag that is no release":   {"v1.2.0", latestRelease("nightly"), ""},
		"a list, newest full release first or not": {"v1.2.0",
			`[{"tag_name":"v1.4.0-rc.1","prerelease":true},{"tag_name":"v1.2.1"},{"tag_name":"v1.3.0","html_url":"https://releases.example/v1.3.0"},{"tag_name":"v9.0.0","draft":true},{"tag_name":"v1.0.0"}]`,
			"v1.3.0"},
		"the full release of a pre-release running": {"v1.3.0-rc.1", latestRelease("v1.3.0"), "v1.3.0"},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFakeReleases(t, c.answer)
			st := openStore(t)
			w := updateWorker(st, &clock{now: time.Now()}, f.srv.URL, c.running)
			tag, err := w.CheckForUpdates(context.Background())
			if err != nil || tag != c.want || f.count() != 1 {
				t.Errorf("%q, %v, asked %d times", tag, err, f.count())
			}
			if n := len(updateEvents(t, st)); (n == 1) != (c.want != "") || n > 1 {
				t.Errorf("%d announced", n)
			}
		})
	}
}

func TestBuildsThatAreNotReleasesNeverCheck(t *testing.T) {
	f := newFakeReleases(t, latestRelease("v9.0.0"))
	st := openStore(t)
	for _, build := range []string{"dev", "v1.2.0-4-gabc1234", "v1.2.0-dirty", ""} {
		w := updateWorker(st, &clock{now: time.Now()}, f.srv.URL, build)
		if tag, err := w.CheckForUpdates(context.Background()); tag != "" || err != nil {
			t.Errorf("%q: %q, %v", build, tag, err)
		}
	}
	if f.count() != 0 {
		t.Errorf("asked %d times", f.count())
	}
}

func TestTheUpdateCheckRunsAtMostDailyOnOneServer(t *testing.T) {
	f := newFakeReleases(t, latestRelease("v1.0.0"))
	st := openStore(t)
	c := &clock{now: time.Date(2026, 11, 6, 9, 0, 0, 0, time.UTC)}
	servers := func() {
		var wg sync.WaitGroup
		for range 4 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if _, err := updateWorker(st, c, f.srv.URL, "v1.0.0").CheckForUpdates(context.Background()); err != nil {
					t.Error(err)
				}
			}()
		}
		wg.Wait()
	}
	servers()
	if f.count() != 1 {
		t.Fatalf("four servers asked %d times", f.count())
	}
	c.advance(23 * time.Hour)
	servers()
	if f.count() != 1 {
		t.Errorf("asked again within the day: %d", f.count())
	}
	c.advance(time.Hour)
	servers()
	if f.count() != 2 {
		t.Errorf("a day on, asked %d times in all", f.count())
	}
}

func TestAFailedUpdateCheckRecordsNothingAndIsTriedTheNextDay(t *testing.T) {
	f := newFakeReleases(t, `{"message":"API rate limit exceeded"}`)
	f.answer(http.StatusForbidden, `{"message":"API rate limit exceeded"}`)
	st := openStore(t)
	c := &clock{now: time.Date(2026, 11, 6, 9, 0, 0, 0, time.UTC)}
	w := updateWorker(st, c, f.srv.URL, "v1.0.0")
	_, err := w.CheckForUpdates(context.Background())
	if err == nil || !strings.Contains(err.Error(), "HTTP 403") || !strings.Contains(err.Error(), "rate limit") {
		t.Errorf("rate limited: %v", err)
	}
	f.answer(http.StatusOK, "not JSON")
	c.advance(24 * time.Hour)
	if _, err := w.CheckForUpdates(context.Background()); err == nil {
		t.Error("an answer that is not JSON was taken")
	}
	// No network at all.
	gone := httptest.NewServer(http.NotFoundHandler())
	gone.Close()
	c.advance(24 * time.Hour)
	if _, err := updateWorker(st, c, gone.URL, "v1.0.0").CheckForUpdates(context.Background()); err == nil {
		t.Error("no answer was taken")
	}
	if n := len(updateEvents(t, st)); n != 0 {
		t.Fatalf("%d recorded by failed checks", n)
	}
	// Within the day it is not tried again; the next day it is.
	f.answer(http.StatusOK, latestRelease("v1.1.0"))
	c.advance(time.Hour)
	if tag, _ := w.CheckForUpdates(context.Background()); tag != "" {
		t.Errorf("tried again within the day: %q", tag)
	}
	c.advance(23 * time.Hour)
	if tag, err := w.CheckForUpdates(context.Background()); tag != "v1.1.0" || err != nil {
		t.Errorf("the next day: %q, %v", tag, err)
	}
}

func TestTheUpdateCheckLinksToGitHubsPages(t *testing.T) {
	// GitHub's API, as the check's URL names it, gives the pages; the
	// request itself goes to the fake.
	f := newFakeReleases(t, latestRelease("v1.1.0"))
	st := openStore(t)
	w := updateWorker(st, &clock{now: time.Now()}, "https://api.github.com/repos/someone/fork/releases/latest", "v1.0.0")
	w.Updates.Client = &http.Client{Transport: rewriteTo(f.srv.URL)}
	if tag, err := w.CheckForUpdates(context.Background()); tag != "v1.1.0" || err != nil {
		t.Fatalf("%q, %v", tag, err)
	}
	got := updateEvents(t, st)[0].Payload.Instance
	if got.NotesURL != "https://github.com/someone/fork/releases/tag/v1.1.0" ||
		got.UpdatingURL != "https://github.com/someone/fork/blob/v1.1.0/docs/installing.md#updating-automatically" {
		t.Errorf("links: %+v", got)
	}
}

// rewriteTo sends every request to a test server instead.
type rewriteTo string

func (to rewriteTo) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.URL.Scheme, r.URL.Host = "http", strings.TrimPrefix(string(to), "http://")
	return http.DefaultTransport.RoundTrip(r)
}

func TestInstanceEventsAreFormattedByEachDriver(t *testing.T) {
	upgraded := Notification{BaseURL: "https://bam.example", DeliveryID: "d1", Event: models.Event{ID: "e1", Kind: models.EventInstanceUpgraded,
		Payload: models.EventPayload{Trigger: models.TriggerSchedule, Instance: &models.EventInstance{
			From: "v1.2.0", To: "v1.3.0", SchemaFrom: 60, SchemaTo: 67, NotesURL: "https://github.com/o/r/releases/tag/v1.3.0"}}}}

	// Discord.
	e := discordPayload(upgraded).Embeds[0]
	if e.Title != "BAM! upgraded to v1.3.0" || e.Color != discordNeutral || e.URL != "https://github.com/o/r/releases/tag/v1.3.0" ||
		e.Description != "It ran v1.2.0 before. Its database went from schema 60 to 67.\nRelease notes: https://github.com/o/r/releases/tag/v1.3.0\nThe instance: https://bam.example" {
		t.Errorf("upgraded: %+v", e)
	}
	down := upgraded
	down.Event.Payload.Instance = &models.EventInstance{From: "v1.3.0", To: "v1.2.0", SchemaFrom: 67, SchemaTo: 67, Downgrade: true}
	e = discordPayload(down).Embeds[0]
	if e.Title != "BAM! downgraded to v1.2.0" || e.Color != discordWarning || e.URL != "https://bam.example" ||
		!strings.Contains(e.Description, "An older release may not work") || !strings.Contains(e.Description, "Its database's schema did not change.") {
		t.Errorf("downgraded: %+v", e)
	}
	failed := Notification{BaseURL: "https://bam.example", Event: models.Event{Kind: models.EventInstanceUpdateFailed,
		Payload: models.EventPayload{Instance: &models.EventInstance{From: "v1.2.0", To: "v1.3.0",
			Reason: "it did not answer its health check within 30 seconds", Restored: true}}}}
	e = discordPayload(failed).Embeds[0]
	if e.Title != "Update to v1.3.0 rolled back" || e.Color != discordDanger ||
		!strings.HasPrefix(e.Description, "bam-update installed v1.3.0, but it did not answer its health check within 30 seconds, so it put v1.2.0 back. The database was restored from the backup taken before the update.") {
		t.Errorf("failed: %+v", e)
	}
	failed.Event.Payload.Instance.Restored = false
	if e = discordPayload(failed).Embeds[0]; !strings.Contains(e.Description, "was not restored from a backup, so any changes v1.3.0 made to it remain") {
		t.Errorf("failed, not restored: %+v", e)
	}
	available := Notification{BaseURL: "https://bam.example", Event: models.Event{Kind: models.EventInstanceUpdateAvailable,
		Payload: models.EventPayload{Instance: &models.EventInstance{From: "v1.2.0", To: "v1.4.0",
			NotesURL: "https://notes.example/v1.4.0", UpdatingURL: "https://docs.example/updating"}}}}
	e = discordPayload(available).Embeds[0]
	if e.Title != "BAM! v1.4.0 is available" || e.URL != "https://notes.example/v1.4.0" ||
		e.Description != "This instance runs v1.2.0.\nRelease notes: https://notes.example/v1.4.0\nUpdating: https://docs.example/updating\nThe instance: https://bam.example" {
		t.Errorf("available: %+v", e)
	}

	// Email says the same.
	m, err := emailMessage(upgraded)
	if err != nil || m.Subject != "BAM! upgraded to v1.3.0" || !strings.Contains(m.Text, "Its database went from schema 60 to 67.") ||
		!strings.Contains(m.Text, "https://github.com/o/r/releases/tag/v1.3.0") {
		t.Errorf("email: %+v, %v", m, err)
	}

	// MQTT publishes it under its kind alone, linking to the instance.
	msg, err := mqttMessageFor("bam", upgraded)
	if err != nil || msg.Topic != "bam/instance.upgraded" {
		t.Fatalf("mqtt: %+v, %v", msg, err)
	}
	var out mqttEvent
	if err := json.Unmarshal(msg.Payload, &out); err != nil {
		t.Fatal(err)
	}
	if out.URL != "https://bam.example" || out.Payload.Instance == nil || *out.Payload.Instance != *upgraded.Event.Payload.Instance {
		t.Errorf("mqtt: %s", msg.Payload)
	}

	// A webhook posts it, with its kind in the header.
	r := newReceiver(t)
	opts := map[string]string{"url": "http://hooks.example/bam", "allow_private_networks": "true"}
	if err := webhookTo(r).Send(context.Background(), opts, failed); err != nil {
		t.Fatal(err)
	}
	var body struct {
		Event struct {
			Kind    string              `json:"kind"`
			URL     string              `json:"url"`
			Payload models.EventPayload `json:"payload"`
		} `json:"event"`
	}
	if err := json.Unmarshal(r.raw[0], &body); err != nil {
		t.Fatal(err)
	}
	if r.got[0].Header.Get("BAM-Event") != "instance.update_failed" || body.Event.Kind != "instance.update_failed" ||
		body.Event.URL != "https://bam.example" || body.Event.Payload.Instance == nil || body.Event.Payload.Instance.To != "v1.3.0" {
		t.Errorf("webhook: %s", r.raw[0])
	}
}
