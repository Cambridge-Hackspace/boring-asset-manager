package notify

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
)

const hook = "https://discord.com/api/webhooks/123456789012345678/abcDEF_ghi-JKL"

func TestDiscordAcceptsOnlyDiscordWebhooks(t *testing.T) {
	good := []string{
		hook,
		"https://discordapp.com/api/webhooks/1/token",
		"https://ptb.discord.com/api/v10/webhooks/1/token",
		"https://canary.discord.com/api/webhooks/1/token?thread_id=42",
		"  " + hook + "  ",
	}
	for _, u := range good {
		if err := (Discord{}).Validate(map[string]string{"webhook_url": u}); err != nil {
			t.Errorf("%s: %v", u, err)
		}
	}
	bad := []string{
		"",
		"http://discord.com/api/webhooks/1/token",
		"https://discord.com.evil.example/api/webhooks/1/token",
		"https://evil.example/api/webhooks/1/token",
		"https://discord.com:8443/api/webhooks/1/token",
		"https://user:pass@discord.com/api/webhooks/1/token",
		"https://discord.com/api/webhooks/1",
		"https://discord.com/api/webhooks/x/token",
		"https://discord.com/api/webhooks/1/token/extra",
		"https://discord.com/api/webhooks/1/token?wait=true",
		"https://discord.com/api/webhooks/1/token?thread_id=x",
		"https://discord.com/api/webhooks/1/token#frag",
		"https://169.254.169.254/api/webhooks/1/token",
	}
	for _, u := range bad {
		if err := (Discord{}).Validate(map[string]string{"webhook_url": u}); err == nil {
			t.Errorf("%q was accepted", u)
		}
	}
}

func lowEvent() models.Event {
	count, threshold := 1, 3
	return models.Event{
		ID:        "e1",
		Kind:      models.EventRunningLow,
		CreatedAt: time.Date(2026, 10, 14, 9, 30, 0, 0, time.UTC),
		Payload: models.EventPayload{
			Category:    &models.EventCategory{ID: "c1", Name: "Tape *and* glue", ColorHex: "#ff8800"},
			Actor:       &models.EventActor{ID: "u1", Email: "stores_team@hackspace.org"},
			Trigger:     models.TriggerThreshold,
			ActiveCount: &count,
			Threshold:   &threshold,
		},
	}
}

func TestDiscordEmbeds(t *testing.T) {
	got, err := json.Marshal(discordPayload(Notification{Event: lowEvent(), BaseURL: "https://bam.example/"}))
	if err != nil {
		t.Fatal(err)
	}
	want := `{"username":"BAM!","allowed_mentions":{"parse":[]},"embeds":[{"title":"Running low: Tape \\*and\\* glue",` +
		`"url":"https://bam.example/categories/c1","description":"Down to 1 active item, below the threshold of 3. Last change by stores\\_team@hackspace.org.",` +
		`"color":16746496,"timestamp":"2026-10-14T09:30:00Z","footer":{"text":"BAM! Boring Asset Manager"}}]}`
	if string(got) != want {
		t.Errorf("running low:\n got %s\nwant %s", got, want)
	}

	cases := []struct {
		name  string
		edit  func(*Notification)
		title string
		desc  string
		color int
	}{
		{"manual low", func(n *Notification) { n.Event.Payload.Trigger = models.TriggerManual }, "Running low: Tape \\*and\\* glue", "Flagged as running low by stores\\_team@hackspace.org.", 0xff8800},
		{"manual low, by name", func(n *Notification) {
			n.Event.Payload.Trigger = models.TriggerManual
			n.Event.Payload.Actor.Name = "Ada *the* stores lead"
		}, "Running low: Tape \\*and\\* glue", "Flagged as running low by Ada \\*the\\* stores lead.", 0xff8800},
		{"manual low, nobody", func(n *Notification) { n.Event.Payload.Trigger = models.TriggerManual; n.Event.Payload.Actor = nil }, "Running low: Tape \\*and\\* glue", "Flagged as running low.", 0xff8800},
		{"restocked", func(n *Notification) {
			n.Event.Kind = models.EventRestocked
			*n.Event.Payload.ActiveCount = 3
			n.Event.Payload.Actor = nil
		}, "Restocked: Tape \\*and\\* glue", "Back to 3 active items, at or above the threshold of 3.", discordNeutral},
		{"manual restock", func(n *Notification) {
			n.Event.Kind = models.EventRestocked
			n.Event.Payload.Trigger = models.TriggerManual
		}, "Restocked: Tape \\*and\\* glue", "Marked as restocked by stores\\_team@hackspace.org.", discordNeutral},
		{"test", func(n *Notification) { n.Test = true }, "Test message from BAM!", "This channel is set up correctly.", discordTest},
	}
	for _, c := range cases {
		n := Notification{Event: lowEvent()}
		c.edit(&n)
		e := discordPayload(n).Embeds[0]
		if e.Title != c.title || e.Description != c.desc || e.Color != c.color {
			t.Errorf("%s: %+v", c.name, e)
		}
	}

	// Long names are cut to fit Discord's title limit.
	n := Notification{Event: lowEvent()}
	n.Event.Payload.Category.Name = strings.Repeat("é", 300)
	if title := discordPayload(n).Embeds[0].Title; len([]rune(title)) > 256 {
		t.Errorf("title of %d characters", len([]rune(title)))
	}
}

func TestDiscordAssetEmbeds(t *testing.T) {
	quantity, threshold := 40, 50
	event := func() models.Event {
		return models.Event{
			Kind: models.EventAssetRunningLow,
			Payload: models.EventPayload{
				Asset:     &models.EventAsset{ID: "a/1", Name: "M3_screws"},
				Actor:     &models.EventActor{Email: "ada@hackspace.org"},
				Trigger:   models.TriggerThreshold,
				Quantity:  &quantity,
				Threshold: &threshold,
				Unit:      "pcs",
			},
		}
	}
	e := discordPayload(Notification{Event: event(), BaseURL: "https://bam.example"}).Embeds[0]
	if e.Title != `Running low: M3\_screws` || e.URL != "https://bam.example/assets/a%2F1" || e.Color != discordWarning ||
		e.Description != "Down to 40 pcs, below the threshold of 50 pcs. Last change by ada@hackspace.org." {
		t.Errorf("running low: %+v", e)
	}

	cases := []struct {
		name, title, desc string
		edit              func(*models.Event)
	}{
		{"manual", `Running low: M3\_screws`, "Flagged as running low by ada@hackspace.org. 40 pcs left.", func(e *models.Event) { e.Payload.Trigger = models.TriggerManual; e.Payload.Threshold = nil }},
		{"restocked, no unit", `Restocked: M3\_screws`, "Back to 40, at or above the threshold of 50.", func(e *models.Event) {
			e.Kind = models.EventAssetRestocked
			e.Payload.Unit = ""
			e.Payload.Actor = nil
		}},
		{"manual restock, nobody", `Restocked: M3\_screws`, "Marked as restocked. 40 pcs left.", func(e *models.Event) {
			e.Kind = models.EventAssetRestocked
			e.Payload.Trigger = models.TriggerManual
			e.Payload.Actor = nil
		}},
	}
	for _, c := range cases {
		ev := event()
		c.edit(&ev)
		e := discordPayload(Notification{Event: ev}).Embeds[0]
		if e.Title != c.title || e.Description != c.desc || e.URL != "" {
			t.Errorf("%s: %+v", c.name, e)
		}
	}
}

func TestDiscordSend(t *testing.T) {
	var (
		status  = http.StatusNoContent
		headers = map[string]string{}
		gotPath string
		gotBody []byte
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.RequestURI()
		gotBody, _ = io.ReadAll(r.Body)
		for k, v := range headers {
			w.Header().Set(k, v)
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{"message": "nope"}`))
	}))
	defer srv.Close()
	d := Discord{endpoint: srv.URL}
	opts := map[string]string{"webhook_url": hook + "?thread_id=7"}
	send := func() error { return d.Send(context.Background(), opts, Notification{Event: lowEvent()}) }

	if err := send(); err != nil {
		t.Fatalf("204: %v", err)
	}
	if gotPath != "/api/webhooks/123456789012345678/abcDEF_ghi-JKL?thread_id=7" {
		t.Errorf("path %s", gotPath)
	}
	var msg discordMessage
	if err := json.Unmarshal(gotBody, &msg); err != nil || len(msg.Embeds) != 1 {
		t.Errorf("body %s (%v)", gotBody, err)
	}

	status, headers = http.StatusTooManyRequests, map[string]string{"Retry-After": "2.5"}
	var after *RetryAfterError
	if err := send(); !errors.As(err, &after) || after.After != 2500*time.Millisecond {
		t.Errorf("429: %v", err)
	}
	status, headers = http.StatusBadGateway, nil
	if err := send(); !errors.Is(err, ErrTemporary) {
		t.Errorf("502: %v", err)
	}
	status = http.StatusNotFound
	err := send()
	if err == nil || errors.Is(err, ErrTemporary) || errors.As(err, &after) || !strings.Contains(err.Error(), "nope") {
		t.Errorf("404: %v", err)
	}

	// Network failures are temporary and never reveal the webhook's token.
	srv.Close()
	err = send()
	if !errors.Is(err, ErrTemporary) || strings.Contains(err.Error(), "abcDEF") {
		t.Errorf("unreachable: %v", err)
	}
}

func TestValidateChannels(t *testing.T) {
	ok := models.NotificationChannel{Name: "Stores", Driver: "discord", Options: map[string]string{"webhook_url": hook}, Events: []string{models.EventRunningLow}}
	if err := Validate(ok); err != nil {
		t.Fatal(err)
	}
	cases := map[string]func(*models.NotificationChannel){
		"no name":        func(c *models.NotificationChannel) { c.Name = " " },
		"long name":      func(c *models.NotificationChannel) { c.Name = strings.Repeat("x", 256) },
		"unknown driver": func(c *models.NotificationChannel) { c.Driver = "pager" },
		"missing secret": func(c *models.NotificationChannel) { c.Options = map[string]string{} },
		"unknown option": func(c *models.NotificationChannel) {
			c.Options = map[string]string{"webhook_url": hook, "color": "red"}
		},
		"unknown event": func(c *models.NotificationChannel) { c.Events = []string{"asset.created"} },
		"twice": func(c *models.NotificationChannel) {
			c.Events = []string{models.EventRunningLow, models.EventRunningLow}
		},
		"bad url": func(c *models.NotificationChannel) {
			c.Options = map[string]string{"webhook_url": "https://example.com/x"}
		},
	}
	for name, edit := range cases {
		c := ok
		c.Options = map[string]string{"webhook_url": hook}
		edit(&c)
		var verr *ValidationError
		if err := Validate(c); !errors.As(err, &verr) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestSecretsAreRedactedAndKept(t *testing.T) {
	stored := models.NotificationChannel{Driver: "discord", Options: map[string]string{"webhook_url": hook}}
	options, set := Redact(stored)
	if len(options) != 0 || !set["webhook_url"] {
		t.Errorf("redacted %v, set %v", options, set)
	}
	update := models.NotificationChannel{Driver: "discord", Options: map[string]string{}}
	if got := MergeSecrets(update, stored); got["webhook_url"] != hook {
		t.Errorf("blank secret not kept: %v", got)
	}
	update.Options["webhook_url"] = "https://discord.com/api/webhooks/2/new"
	if got := MergeSecrets(update, stored); got["webhook_url"] != update.Options["webhook_url"] {
		t.Errorf("new secret not used: %v", got)
	}
	// A secret never follows a channel to another driver.
	other := models.NotificationChannel{Driver: "other", Options: map[string]string{}}
	if got := MergeSecrets(other, stored); got["webhook_url"] != "" {
		t.Errorf("secret carried across drivers: %v", got)
	}
}

func TestHTTPErrorDefaultsRetryAfter(t *testing.T) {
	var after *RetryAfterError
	err := HTTPError(&http.Response{StatusCode: 429, Header: http.Header{}}, "slow down")
	if !errors.As(err, &after) || after.After != time.Minute {
		t.Errorf("got %v", err)
	}
	if HTTPError(&http.Response{StatusCode: 200}, "") != nil {
		t.Error("200 is not an error")
	}
}

func TestBackoff(t *testing.T) {
	want := []time.Duration{30 * time.Second, time.Minute, 2 * time.Minute, 4 * time.Minute, 8 * time.Minute, 16 * time.Minute, 32 * time.Minute, time.Hour, time.Hour}
	for i, w := range want {
		if got := backoff(i); got != w {
			t.Errorf("backoff(%d) = %s, want %s", i, got, w)
		}
	}
	if backoff(1000) != time.Hour {
		t.Error("backoff overflows")
	}
}

func TestDiscordConditionEmbeds(t *testing.T) {
	event := func(to, note string) Notification {
		return Notification{BaseURL: "https://bam.example", Event: models.Event{
			Kind: models.EventAssetConditionChanged,
			Payload: models.EventPayload{
				Asset:   &models.EventAsset{ID: "a1", Name: "Pillar_drill"},
				Actor:   &models.EventActor{Email: "bo@hackspace.org", Name: "Bo"},
				Trigger: models.TriggerManual,
				Changes: []models.EventChange{{What: "condition", From: "ok", To: to}},
				Note:    note,
			},
		}}
	}
	for _, c := range []struct {
		to, note, title, desc string
		color                 int
	}{
		{"out_of_service", "Chuck *slips*", `Out of service: Pillar\_drill`, `Chuck \*slips\*` + "\nReported by Bo.", discordDanger},
		{"needs_maintenance", "", `Needs maintenance: Pillar\_drill`, "Reported by Bo.", discordWarning},
		{"ok", "", `Working again: Pillar\_drill`, "Reported by Bo.", discordNeutral},
	} {
		e := discordPayload(event(c.to, c.note)).Embeds[0]
		if e.Title != c.title || e.Description != c.desc || e.Color != c.color || e.URL != "https://bam.example/assets/a1" {
			t.Errorf("%s: %+v", c.to, e)
		}
	}
}

func TestDiscordOverdueEmbed(t *testing.T) {
	due := time.Date(2026, 10, 14, 22, 59, 0, 0, time.UTC)
	n := Notification{BaseURL: "https://bam.example", Event: models.Event{
		Kind: models.EventAssetOverdue,
		Payload: models.EventPayload{
			Asset:    &models.EventAsset{ID: "a1", Name: "Drill"},
			Trigger:  models.TriggerSchedule,
			Borrower: &models.EventActor{ID: "u1", Email: "bo@hackspace.org", Name: "Bo_Brown"},
			DueAt:    &due,
		},
	}}
	e := discordPayload(n).Embeds[0]
	if e.Title != "Overdue: Drill" || e.Description != `Lent to Bo\_Brown, due back 14 October 2026 (UTC).` || e.Color != discordWarning || e.URL != "https://bam.example/assets/a1" {
		t.Errorf("overdue: %+v", e)
	}
}

func TestDiscordActionEmbed(t *testing.T) {
	n := Notification{BaseURL: "https://bam.example", Event: models.Event{
		Kind: models.EventAssetAction,
		Payload: models.EventPayload{
			Asset:   &models.EventAsset{ID: "a1", Name: "Drawer A3"},
			Trigger: models.TriggerManual,
			Actor:   &models.EventActor{ID: "u1", Name: "Ada"},
			Action:  &models.EventAction{Key: "identify", Label: "*Identify*"},
		},
	}}
	e := discordPayload(n).Embeds[0]
	if e.Title != `\*Identify\*: Drawer A3` || e.Description != "Pressed by Ada." || e.URL != "https://bam.example/assets/a1" {
		t.Errorf("an action: %+v", e)
	}
}
