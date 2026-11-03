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
	discordWarning = 0xf59e0b
	discordDanger  = 0xdc2626
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
	sum := summarize(n, escapeMarkdown)
	embed := discordEmbed{
		Title: sum.Title, URL: sum.URL, Description: sum.Description, Color: sum.Color,
		Footer: &discordFooter{Text: "BAM! Boring Asset Manager"},
	}
	if !n.Event.CreatedAt.IsZero() {
		embed.Timestamp = n.Event.CreatedAt.UTC().Format(time.RFC3339)
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

// assetDescription describes an asset's threshold event with crossed, given
// the quantity and threshold, or its manual one with manual, given who.
func assetDescription(p models.EventPayload, by string, esc func(string) string, crossed, manual string) string {
	if p.Trigger == models.TriggerThreshold && p.Quantity != nil && p.Threshold != nil {
		d := fmt.Sprintf(crossed, amount(*p.Quantity, p.Unit, esc), amount(*p.Threshold, p.Unit, esc))
		if by != "" {
			d += " Last change by " + by + "."
		}
		return d
	}
	d := manual
	if by != "" {
		d += " by " + by
	}
	d += "."
	if p.Quantity != nil {
		d += " " + amount(*p.Quantity, p.Unit, esc) + " left."
	}
	return d
}

// amount is a quantity with its unit, if it has one.
func amount(n int, unit string, esc func(string) string) string {
	if unit == "" {
		return strconv.Itoa(n)
	}
	return strconv.Itoa(n) + " " + esc(truncate(unit, 32))
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

// categoryLevel gives a category's level and threshold: counts of items, or
// for a category with a unit, amounts in it.
func categoryLevel(p models.EventPayload, esc func(string) string) (level, threshold string) {
	if p.Unit != "" {
		return amount(*p.ActiveCount, p.Unit, esc), amount(*p.Threshold, p.Unit, esc)
	}
	return items(*p.ActiveCount), strconv.Itoa(*p.Threshold)
}
