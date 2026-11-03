package notify

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"sort"
	"strings"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/email"
)

// Email sends events by email (docs/notifications.md), through the SMTP
// server in the configuration's [email] table: to the addresses a channel
// lists, and to the people holding the roles it names, in the place the
// event is about.
type Email struct{}

func (Email) Info() DriverInfo {
	return DriverInfo{
		Driver: "email",
		Label:  "Email",
		Options: []OptionInfo{
			{Name: "to", Label: "Addresses", Multiline: true,
				Description: "Email addresses to send to, one per line or separated by commas."},
			{Name: "roles", Label: "Roles",
				Description: "Send to everyone holding these roles, by name, separated by commas: where a role is narrowed to scopes, only events in them. Each person can turn these off under Your account."},
		},
	}
}

// emailOptions are a channel's addresses and role names.
func emailOptions(options map[string]string) (to, roles []string, err error) {
	for _, a := range strings.FieldsFunc(options["to"], func(r rune) bool { return r == ',' || r == '\n' || r == '\r' }) {
		if a = strings.TrimSpace(a); a == "" {
			continue
		}
		addr, err := mail.ParseAddress(a)
		if err != nil {
			return nil, nil, fmt.Errorf("%q is not an email address", a)
		}
		to = append(to, addr.Address)
	}
	for _, r := range strings.Split(options["roles"], ",") {
		if r = strings.TrimSpace(r); r != "" {
			roles = append(roles, r)
		}
	}
	if len(to) == 0 && len(roles) == 0 {
		return nil, nil, errors.New("give addresses, roles or both")
	}
	if len(to) > 100 {
		return nil, nil, errors.New("at most 100 addresses; to reach more, name a role")
	}
	return to, roles, nil
}

func (Email) Validate(options map[string]string) error {
	_, _, err := emailOptions(options)
	return err
}

func (Email) Send(ctx context.Context, options map[string]string, n Notification) error {
	if n.Mailer == nil {
		return email.ErrNotSetUp
	}
	to, roles, err := emailOptions(options)
	if err != nil {
		return err
	}
	if len(roles) > 0 && n.Recipients != nil {
		holders, err := n.Recipients(roles)
		if err != nil {
			return fmt.Errorf("%w: finding who holds %s: %v", ErrTemporary, strings.Join(roles, ", "), err)
		}
		to = append(to, holders...)
	}
	to = unique(to)
	if len(to) == 0 {
		// Everyone the roles reach has turned email off, or none hold them.
		return nil
	}
	m, err := emailMessage(n)
	if err != nil {
		return err
	}
	m.To = to
	if err := n.Mailer.Send(ctx, m); err != nil {
		if errors.Is(err, email.ErrTemporary) {
			return fmt.Errorf("%w: %v", ErrTemporary, err)
		}
		return err
	}
	return nil
}

// emailMessage lays a notification out as an email, saying what Discord
// would.
func emailMessage(n Notification) (email.Message, error) {
	sum := summarize(n, func(s string) string { return s })
	c := email.Content{
		Title:  sum.Title,
		Link:   sum.URL,
		Accent: fmt.Sprintf("#%06x", sum.Color),
		Footer: "Sent by BAM! because a notification channel sends this event to you. " +
			"If it reached you through a role, you can turn such emails off under Your account.",
	}
	if base := strings.TrimRight(n.BaseURL, "/"); base != "" {
		c.Footer = "Sent by BAM! at " + base + " because a notification channel sends this event to you. " +
			"If it reached you through a role, you can turn such emails off under Your account: " + base + "/account"
	}
	for _, para := range strings.Split(sum.Description, "\n") {
		if para = strings.TrimSpace(para); para != "" {
			c.Paragraphs = append(c.Paragraphs, para)
		}
	}
	html, text, err := email.Render(c)
	if err != nil {
		return email.Message{}, err
	}
	return email.Message{Subject: sum.Title, Text: text, HTML: html}, nil
}

func unique(addresses []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, a := range addresses {
		k := strings.ToLower(a)
		if !seen[k] {
			seen[k] = true
			out = append(out, a)
		}
	}
	sort.Strings(out)
	return out
}
