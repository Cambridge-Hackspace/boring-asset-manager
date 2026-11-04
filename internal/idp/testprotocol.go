package idp

import (
	"context"
	"crypto/subtle"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// TestProtocol stands in for a provider in tests: it sends the browser
// straight back, signed in as its settings say. It is registered only when
// the configuration's [auth] test_protocol is on, which nothing but a test
// should ever turn on: anyone could then sign in as anyone it names.
type TestProtocol struct{}

// TestProtocolName is its key.
const TestProtocolName = "test"

func (TestProtocol) Name() string  { return TestProtocolName }
func (TestProtocol) Label() string { return "Test (signs in as its settings say)" }

func (TestProtocol) Fields() []Field {
	return []Field{
		{Name: "subject", Label: "Subject", Required: true},
		{Name: "email", Label: "Email"},
		{Name: "email_verified", Label: "Email verified (yes or no)"},
		{Name: "name", Label: "Name"},
		{Name: "groups", Label: "Groups, separated by commas"},
		{Name: "refuse", Label: "Refuse (yes or no)"},
		{Name: "secret", Label: "A secret, unused", Secret: true},
	}
}

func (p TestProtocol) Check(c Config) error { return CheckRequired(p, c) }

func (TestProtocol) Begin(_ context.Context, c Config) (string, map[string]string, error) {
	u, err := url.Parse(c.CallbackURL)
	if err != nil {
		return "", nil, err
	}
	q := u.Query()
	q.Set("state", c.State)
	u.RawQuery = q.Encode()
	return u.String(), map[string]string{"state": c.State}, nil
}

func (TestProtocol) Finish(_ context.Context, c Config, r *http.Request, state map[string]string) (*Claims, error) {
	if subtle.ConstantTimeCompare([]byte(r.URL.Query().Get("state")), []byte(state["state"])) != 1 || state["state"] == "" {
		return nil, fmt.Errorf("the answer is not for this sign-in")
	}
	if c.Settings["refuse"] == "yes" {
		return nil, fmt.Errorf("%w: refused by its settings", ErrRefused)
	}
	claims := &Claims{
		Subject:       c.Settings["subject"],
		Email:         c.Settings["email"],
		EmailVerified: c.Settings["email_verified"] == "yes",
		Name:          c.Settings["name"],
		Values:        map[string][]string{"sub": {c.Settings["subject"]}},
	}
	for _, g := range strings.Split(c.Settings["groups"], ",") {
		if g = strings.TrimSpace(g); g != "" {
			claims.Values["groups"] = append(claims.Values["groups"], g)
		}
	}
	if claims.Email != "" {
		claims.Values["email"] = []string{claims.Email}
	}
	return claims, nil
}
