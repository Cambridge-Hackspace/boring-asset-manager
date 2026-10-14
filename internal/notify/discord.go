package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
)

// Discord posts embeds to a Discord webhook.
type Discord struct {
	// Client sends requests; nil uses a client with a 15-second timeout.
	Client *http.Client
	// endpoint, set by tests, replaces the scheme and host requests go to.
	endpoint string
}

var discordClient = &http.Client{Timeout: 15 * time.Second}

// discordHosts are the only hosts webhooks may point at, so a channel cannot
// make the server request arbitrary addresses.
var discordHosts = map[string]bool{
	"discord.com": true, "ptb.discord.com": true, "canary.discord.com": true,
	"discordapp.com": true, "ptb.discordapp.com": true, "canary.discordapp.com": true,
}

var (
	discordWebhookPath = regexp.MustCompile(`^/api(/v[0-9]{1,2})?/webhooks/[0-9]{1,20}/[A-Za-z0-9_-]{1,100}$`)
	discordThreadID    = regexp.MustCompile(`^[0-9]{1,20}$`)
)

func (Discord) Info() DriverInfo {
	return DriverInfo{
		Driver: "discord",
		Label:  "Discord",
		Options: []OptionInfo{{
			Name:        "webhook_url",
			Label:       "Webhook URL",
			Description: "From the channel's settings in Discord: Integrations, Webhooks, Copy Webhook URL.",
			Required:    true,
			Secret:      true,
		}},
	}
}

func (Discord) Validate(options map[string]string) error {
	_, err := discordWebhook(options["webhook_url"])
	return err
}

// discordWebhook parses and checks a webhook URL.
func discordWebhook(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" || !discordHosts[strings.ToLower(u.Hostname())] ||
		!discordWebhookPath.MatchString(u.Path) || u.Fragment != "" {
		return nil, errors.New("webhook URL must be a Discord webhook (https://discord.com/api/webhooks/…)")
	}
	for k, v := range u.Query() {
		if k != "thread_id" || len(v) != 1 || !discordThreadID.MatchString(v[0]) {
			return nil, errors.New("webhook URL may only carry a thread_id")
		}
	}
	return u, nil
}

// discordMessage is the webhook payload.
type discordMessage struct {
	Username        string         `json:"username"`
	AllowedMentions map[string]any `json:"allowed_mentions"`
	Embeds          []discordEmbed `json:"embeds"`
}

type discordEmbed struct {
	Title       string         `json:"title"`
	URL         string         `json:"url,omitempty"`
	Description string         `json:"description,omitempty"`
	Color       int            `json:"color"`
	Timestamp   string         `json:"timestamp,omitempty"`
	Footer      *discordFooter `json:"footer,omitempty"`
}

type discordFooter struct {
	Text string `json:"text"`
}

const (
	discordNeutral = 0x6b7280
	discordTest    = 0x3b82f6
)

func (d Discord) Send(ctx context.Context, options map[string]string, n Notification) error {
	u, err := discordWebhook(options["webhook_url"])
	if err != nil {
		return err
	}
	if d.endpoint != "" {
		e, err := url.Parse(d.endpoint)
		if err != nil {
			return err
		}
		u.Scheme, u.Host = e.Scheme, e.Host
	}
	body, err := json.Marshal(discordPayload(n))
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	client := d.Client
	if client == nil {
		client = discordClient
	}
	resp, err := client.Do(req)
	if err != nil {
		// Never report the URL: it holds the webhook's token.
		var uerr *url.Error
		if errors.As(err, &uerr) {
			err = uerr.Err
		}
		return fmt.Errorf("%w: sending to Discord: %v", ErrTemporary, err)
	}
	defer resp.Body.Close()
	excerpt, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
	return HTTPError(resp, string(excerpt))
}

// discordPayload presents a notification as a Discord embed.
func discordPayload(n Notification) discordMessage {
	e := n.Event
	p := e.Payload
	embed := discordEmbed{Footer: &discordFooter{Text: "BAM! Boring Asset Manager"}}
	if !e.CreatedAt.IsZero() {
		embed.Timestamp = e.CreatedAt.UTC().Format(time.RFC3339)
	}
	name := "a category"
	if p.Category != nil {
		name = escapeMarkdown(truncate(p.Category.Name, 200))
		if base := strings.TrimRight(n.BaseURL, "/"); base != "" {
			embed.URL = base + "/categories/" + url.PathEscape(p.Category.ID)
		}
	}
	by := ""
	if p.Actor != nil && p.Actor.Email != "" {
		by = escapeMarkdown(p.Actor.Email)
	}

	switch {
	case n.Test:
		embed.Title = "Test message from BAM!"
		embed.Description = "This channel is set up correctly."
		embed.Color = discordTest
		embed.URL = ""
	case e.Kind == models.EventRunningLow:
		embed.Title = "Running low: " + name
		embed.Color = categoryColor(p.Category)
		if p.Trigger == models.TriggerThreshold && p.ActiveCount != nil && p.Threshold != nil {
			embed.Description = fmt.Sprintf("Down to %s, below the threshold of %d.", items(*p.ActiveCount), *p.Threshold)
			if by != "" {
				embed.Description += " Last change by " + by + "."
			}
		} else if by != "" {
			embed.Description = "Flagged as running low by " + by + "."
		} else {
			embed.Description = "Flagged as running low."
		}
	case e.Kind == models.EventRestocked:
		embed.Title = "Restocked: " + name
		embed.Color = discordNeutral
		if p.Trigger == models.TriggerThreshold && p.ActiveCount != nil && p.Threshold != nil {
			embed.Description = fmt.Sprintf("Back to %s, at or above the threshold of %d.", items(*p.ActiveCount), *p.Threshold)
			if by != "" {
				embed.Description += " Last change by " + by + "."
			}
		} else if by != "" {
			embed.Description = "Marked as restocked by " + by + "."
		} else {
			embed.Description = "Marked as restocked."
		}
	default:
		embed.Title = truncate(e.Kind, 200)
		embed.Color = discordNeutral
	}
	return discordMessage{
		Username: "BAM!",
		// Names come from users; never let them ping anyone.
		AllowedMentions: map[string]any{"parse": []string{}},
		Embeds:          []discordEmbed{embed},
	}
}

func items(n int) string {
	if n == 1 {
		return "1 active item"
	}
	return strconv.Itoa(n) + " active items"
}

// categoryColor is a category's color as Discord wants it.
func categoryColor(c *models.EventCategory) int {
	if c == nil {
		return discordNeutral
	}
	v, err := strconv.ParseUint(strings.TrimPrefix(c.ColorHex, "#"), 16, 32)
	if err != nil || len(c.ColorHex) != 7 {
		return discordNeutral
	}
	return int(v)
}

func truncate(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max-1]) + "…"
}

var markdownChars = strings.NewReplacer(`\`, `\\`, `*`, `\*`, `_`, `\_`, `~`, `\~`, "`", "\\`", `|`, `\|`, `>`, `\>`, `[`, `\[`, `]`, `\]`)

// escapeMarkdown stops user text being read as Discord formatting.
func escapeMarkdown(s string) string { return markdownChars.Replace(s) }
