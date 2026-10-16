// Package notify sends events to the outside world through pluggable
// notifier drivers (README §5.2). Events are neutral; each driver decides how
// to present them. Drivers are registered by name and chosen per channel, so
// adding a destination means adding a driver, not changing the API.
package notify

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/textcheck"
)

// Driver delivers events to one kind of destination.
type Driver interface {
	// Info describes the driver for clients configuring channels.
	Info() DriverInfo
	// Validate checks a channel's options. Every option the driver declares
	// as required is present by the time it is called.
	Validate(options map[string]string) error
	// Send delivers one notification. Errors that are worth retrying wrap
	// ErrTemporary or are a *RetryAfterError; anything else fails at once.
	Send(ctx context.Context, options map[string]string, n Notification) error
}

// DriverInfo describes a driver.
type DriverInfo struct {
	Driver  string       `json:"driver"`
	Label   string       `json:"label"`
	Options []OptionInfo `json:"options"`
}

// OptionInfo describes one driver option. Every option is a string.
type OptionInfo struct {
	Name        string `json:"name"`
	Label       string `json:"label"`
	Description string `json:"description"`
	Required    bool   `json:"required"`
	// Secret options can be written but are never returned.
	Secret bool `json:"secret"`
}

// Notification is an event as a driver receives it.
type Notification struct {
	Event models.Event
	// BaseURL is the server's public address, for links.
	BaseURL string
	// Test marks the message sent by a channel's test button.
	Test bool
}

// EventKinds lists the events channels can subscribe to, with labels.
var EventKinds = []EventKind{
	{Kind: models.EventRunningLow, Label: "A category is running low"},
	{Kind: models.EventRestocked, Label: "A category is restocked"},
	{Kind: models.EventAssetRunningLow, Label: "An asset counted by quantity is running low"},
	{Kind: models.EventAssetRestocked, Label: "An asset counted by quantity is restocked"},
	{Kind: models.EventAssetConditionChanged, Label: "An asset needs maintenance, is out of service, or is working again"},
}

// EventKind is an event channels can subscribe to.
type EventKind struct {
	Kind  string `json:"kind"`
	Label string `json:"label"`
}

// ErrTemporary is wrapped by errors worth retrying: network failures and
// server errors.
var ErrTemporary = errors.New("temporary failure")

// RetryAfterError asks for a retry no sooner than After, as a rate limit
// does.
type RetryAfterError struct {
	After time.Duration
	Err   error
}

func (e *RetryAfterError) Error() string {
	return fmt.Sprintf("%v (retry after %s)", e.Err, e.After)
}

func (e *RetryAfterError) Unwrap() error { return e.Err }

// HTTPError turns a webhook response into an error: nil for 2xx, a
// RetryAfterError for 429, ErrTemporary for 5xx and a permanent error
// otherwise. body is a short excerpt of the response for the delivery log.
func HTTPError(resp *http.Response, body string) error {
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	err := fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(body))
	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
		after := time.Minute
		if v, perr := strconv.ParseFloat(resp.Header.Get("Retry-After"), 64); perr == nil && v >= 0 {
			after = time.Duration(v * float64(time.Second))
		}
		return &RetryAfterError{After: after, Err: err}
	case resp.StatusCode >= 500:
		return fmt.Errorf("%w: %v", ErrTemporary, err)
	default:
		return err
	}
}

var (
	registryMu sync.RWMutex
	registry   = map[string]Driver{}
)

func init() {
	if err := Register(Discord{}); err != nil {
		panic(err)
	}
}

// Register adds a driver under its Info().Driver name.
func Register(d Driver) error {
	name := d.Info().Driver
	if name == "" {
		return errors.New("driver has no name")
	}
	registryMu.Lock()
	defer registryMu.Unlock()
	if _, ok := registry[name]; ok {
		return fmt.Errorf("driver %q is already registered", name)
	}
	registry[name] = d
	return nil
}

// Lookup finds a driver by name.
func Lookup(name string) (Driver, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	d, ok := registry[name]
	return d, ok
}

// Drivers describes every registered driver, by name.
func Drivers() []DriverInfo {
	registryMu.RLock()
	defer registryMu.RUnlock()
	list := make([]DriverInfo, 0, len(registry))
	for _, d := range registry {
		list = append(list, d.Info())
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Driver < list[j].Driver })
	return list
}

// ValidationError is a problem with a channel definition.
type ValidationError struct{ msg string }

func (e *ValidationError) Error() string { return e.msg }

func invalid(format string, args ...any) error {
	return &ValidationError{msg: fmt.Sprintf(format, args...)}
}

// Validate checks a channel definition: a name, a known driver, only that
// driver's options with the required ones present, and known event kinds.
func Validate(c models.NotificationChannel) error {
	if strings.TrimSpace(c.Name) == "" {
		return invalid("name is required")
	}
	if p := textcheck.Problem(c.Name, textcheck.MaxName); p != "" {
		return invalid("name %s", p)
	}
	d, ok := Lookup(c.Driver)
	if !ok {
		return invalid("unknown driver %q", c.Driver)
	}
	declared := map[string]OptionInfo{}
	for _, o := range d.Info().Options {
		declared[o.Name] = o
		if o.Required && strings.TrimSpace(c.Options[o.Name]) == "" {
			return invalid("%s is required", o.Label)
		}
	}
	for name := range c.Options {
		if _, ok := declared[name]; !ok {
			return invalid("unknown option %q for driver %s", name, c.Driver)
		}
	}
	known := map[string]bool{}
	for _, k := range EventKinds {
		known[k.Kind] = true
	}
	seen := map[string]bool{}
	for _, k := range c.Events {
		if !known[k] {
			return invalid("unknown event %q", k)
		}
		if seen[k] {
			return invalid("event %q is listed twice", k)
		}
		seen[k] = true
	}
	if err := d.Validate(c.Options); err != nil {
		return &ValidationError{msg: err.Error()}
	}
	return nil
}

// Redact returns a channel's options without secret values, and which
// secret options are set.
func Redact(c models.NotificationChannel) (options map[string]string, secretsSet map[string]bool) {
	options, secretsSet = map[string]string{}, map[string]bool{}
	secret := map[string]bool{}
	if d, ok := Lookup(c.Driver); ok {
		for _, o := range d.Info().Options {
			if o.Secret {
				secret[o.Name] = true
				secretsSet[o.Name] = c.Options[o.Name] != ""
			}
		}
	}
	for k, v := range c.Options {
		if !secret[k] {
			options[k] = v
		}
	}
	return options, secretsSet
}

// MergeSecrets fills secret options left blank in an update with their
// stored values, so clients can change a channel without resending secrets.
func MergeSecrets(update, stored models.NotificationChannel) map[string]string {
	merged := map[string]string{}
	for k, v := range update.Options {
		merged[k] = v
	}
	if update.Driver != stored.Driver {
		return merged
	}
	if d, ok := Lookup(update.Driver); ok {
		for _, o := range d.Info().Options {
			if o.Secret && strings.TrimSpace(merged[o.Name]) == "" && stored.Options[o.Name] != "" {
				merged[o.Name] = stored.Options[o.Name]
			}
		}
	}
	return merged
}
