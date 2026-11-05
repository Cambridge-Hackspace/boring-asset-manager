// Package idp is what every sign-in protocol shares (docs/access.md#signing-in-with-an-identity-provider):
// a protocol sends the browser to the provider and reads who came back, as
// Claims; BAM! does the rest, the same for each. OpenID Connect, SAML and
// plain OAuth2 each register a Protocol.
package idp

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// Field is one of a protocol's settings, shown in the provider's form.
// A secret one can be written but is never returned.
type Field struct {
	Name     string `json:"name"`
	Label    string `json:"label"`
	Help     string `json:"help,omitempty"`
	Required bool   `json:"required"`
	Secret   bool   `json:"secret"`
	// Multiline is one long enough to want several lines, such as pasted
	// XML.
	Multiline bool `json:"multiline,omitempty"`
}

// Config is a provider as a protocol sees it: its settings and secrets,
// and where the provider sends the browser back to.
type Config struct {
	Key         string
	Settings    map[string]string
	Secrets     map[string]string
	CallbackURL string
	// BaseURL is where BAM! is served, for what a protocol publishes about
	// itself, such as SAML's metadata.
	BaseURL string
	// UseOnce records that an ID the provider gave, such as a SAML
	// assertion's, was used, until it expires; it answers ErrReplayed for
	// one used already. Nil accepts any.
	UseOnce func(id string, until time.Time) error
	// State is a random value for this sign-in, which the protocol sends to
	// the provider, and checks comes back, against forged answers.
	State string
}

// Claims are who the provider says came back.
type Claims struct {
	// Subject is the provider's stable ID for them: OIDC's sub, or SAML's
	// persistent NameID. It never changes, unlike their email.
	Subject string
	Email   string
	// EmailVerified is the provider's word that the email is theirs.
	EmailVerified bool
	Name          string
	// Values are every claim or attribute, by name, each as its values,
	// for the provider's rules.
	Values map[string][]string
	// AMR and ACR are how the provider says they signed in, such as "mfa"
	// for a second factor: OIDC's amr and acr, kept for #192.
	AMR []string
	ACR string
}

// Protocol talks to providers of one kind.
type Protocol interface {
	// Name is the protocol's key, such as "oidc".
	Name() string
	// Label is its name for people, such as "OpenID Connect".
	Label() string
	Fields() []Field
	// Check reports what is wrong with a provider's settings, as a
	// sentence, or nil.
	Check(c Config) error
	// Begin answers where to send the browser, and what to keep until it
	// comes back.
	Begin(ctx context.Context, c Config) (redirect string, state map[string]string, err error)
	// Finish reads the provider's answer to Begin, given what it kept.
	Finish(ctx context.Context, c Config, r *http.Request, state map[string]string) (*Claims, error)
}

// Preparer is a protocol that makes some of a provider's secrets itself,
// such as a key of its own, when the provider is saved.
type Preparer interface {
	// Prepare fills in the secrets it makes that are missing, or makes
	// them all again when renew is set. It answers whether it changed any.
	Prepare(c *Config, renew bool) (bool, error)
}

// Unsolicited is a protocol that can take an answer no sign-in was
// started for, such as SAML's sign-ins started at the identity provider.
type Unsolicited interface {
	// AcceptsUnsolicited reports whether this provider takes them.
	AcceptsUnsolicited(c Config) bool
}

// Publisher is a protocol that publishes a document about BAM! for the
// provider, such as SAML's service provider metadata.
type Publisher interface {
	// Publish answers the document and its content type.
	Publish(c Config) ([]byte, string, error)
}

// ErrReplayed is UseOnce's answer for an ID used already.
var ErrReplayed = errors.New("the answer was used already")

// ErrRefused is wrapped by a provider's answer that someone did not sign
// in, such as a person cancelling, as opposed to the provider failing.
var ErrRefused = errors.New("the provider did not sign them in")

var (
	mu        sync.RWMutex
	protocols = map[string]Protocol{}
)

// Register makes a protocol available.
func Register(p Protocol) {
	mu.Lock()
	defer mu.Unlock()
	protocols[p.Name()] = p
}

// Unregister removes one, for tests.
func Unregister(name string) {
	mu.Lock()
	defer mu.Unlock()
	delete(protocols, name)
}

// Lookup finds a protocol by its key.
func Lookup(name string) (Protocol, bool) {
	mu.RLock()
	defer mu.RUnlock()
	p, ok := protocols[name]
	return p, ok
}

// Protocols lists those available, by key.
func Protocols() []Protocol {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]Protocol, 0, len(protocols))
	for _, p := range protocols {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	return out
}

// CheckRequired reports the first required field left blank.
func CheckRequired(p Protocol, c Config) error {
	for _, f := range p.Fields() {
		v := c.Settings[f.Name]
		if f.Secret {
			v = c.Secrets[f.Name]
		}
		if f.Required && strings.TrimSpace(v) == "" {
			return fmt.Errorf("%s is required", f.Label)
		}
	}
	return nil
}

// Has reports whether a claim holds value: equal to it, ignoring case, or
// among its values.
func (c *Claims) Has(claim, value string) bool {
	for _, v := range c.Values[claim] {
		if strings.EqualFold(v, value) {
			return true
		}
	}
	return false
}

// EmailDomain is the part of the email after its @, lowercased.
func (c *Claims) EmailDomain() string {
	at := strings.LastIndex(c.Email, "@")
	if at < 0 {
		return ""
	}
	return strings.ToLower(c.Email[at+1:])
}
