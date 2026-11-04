package idp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// oidcFixture is a test provider served over HTTP, and BAM!'s side of it.
type oidcFixture struct {
	provider *TestOIDCProvider
	oidc     *OIDC
	config   Config
}

func newOIDCFixture(t *testing.T) *oidcFixture {
	t.Helper()
	p := NewTestOIDCProvider("", "bam", "s3cret")
	ts := httptest.NewServer(p)
	t.Cleanup(ts.Close)
	p.Issuer = ts.URL
	return &oidcFixture{provider: p, oidc: &OIDC{}, config: Config{
		Key:         "hackspace",
		Settings:    map[string]string{"issuer": ts.URL, "client_id": "bam", "scopes": "openid email profile groups"},
		Secrets:     map[string]string{"client_secret": "s3cret"},
		CallbackURL: "https://bam.example/api/v1/auth/providers/hackspace/callback",
		State:       "the-state",
	}}
}

// signIn goes through Begin, the provider's form filled in with who, and
// Finish; answer, when given, changes the provider's answer first.
func (f *oidcFixture) signIn(t *testing.T, who url.Values, answer func(url.Values)) (*Claims, error) {
	t.Helper()
	redirect, kept, err := f.oidc.Begin(context.Background(), f.config)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	u, _ := url.Parse(redirect)
	form := u.Query()
	for k, v := range who {
		form[k] = v
	}
	noFollow := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := noFollow.PostForm(f.provider.Issuer+"/authorize", form)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	back, err := url.Parse(res.Header.Get("Location"))
	if err != nil || !strings.HasPrefix(back.String(), f.config.CallbackURL) {
		t.Fatalf("the provider sent the browser to %q", res.Header.Get("Location"))
	}
	if answer != nil {
		q := back.Query()
		answer(q)
		back.RawQuery = q.Encode()
	}
	return f.oidc.Finish(context.Background(), f.config, httptest.NewRequest(http.MethodGet, back.String(), nil), kept)
}

var ada = url.Values{"sub": {"ada-1"}, "email": {"ada@hackspace.org"}, "name": {"Ada"}, "groups": {"members, woodshop"}, "mfa": {"yes"}}

func TestOIDCSignsInWithPKCEStateAndNonce(t *testing.T) {
	f := newOIDCFixture(t)
	redirect, kept, err := f.oidc.Begin(context.Background(), f.config)
	if err != nil {
		t.Fatal(err)
	}
	q, _ := url.Parse(redirect)
	ask := q.Query()
	if ask.Get("state") != "the-state" || ask.Get("nonce") != kept["nonce"] || ask.Get("nonce") == "" ||
		ask.Get("code_challenge_method") != "S256" || ask.Get("code_challenge") == "" || kept["verifier"] == "" ||
		ask.Get("scope") != "openid email profile groups" || ask.Get("redirect_uri") != f.config.CallbackURL || ask.Get("client_id") != "bam" {
		t.Errorf("asked %v, kept %v", ask, kept)
	}

	claims, err := f.signIn(t, ada, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Groups come from userinfo, the rest from the ID token.
	if claims.Subject != "ada-1" || claims.Email != "ada@hackspace.org" || !claims.EmailVerified || claims.Name != "Ada" ||
		!claims.Has("groups", "woodshop") || !claims.Has("groups", "members") {
		t.Errorf("claims: %+v", claims)
	}
	if len(claims.AMR) != 2 || claims.AMR[1] != "mfa" {
		t.Errorf("amr: %v", claims.AMR)
	}
}

func TestOIDCReadsTheClaimsItIsTold(t *testing.T) {
	f := newOIDCFixture(t)
	f.config.Settings["email_claim"] = "name"
	f.config.Settings["name_claim"] = "sub"
	f.config.Settings["groups_claim"] = "amr"
	f.provider.Tamper = func(_, c map[string]any) { c["acr"] = "urn:mfa" }
	claims, err := f.signIn(t, ada, nil)
	if err != nil {
		t.Fatal(err)
	}
	if claims.Email != "Ada" || claims.Name != "ada-1" || !claims.Has("groups", "mfa") || claims.ACR != "urn:mfa" {
		t.Errorf("claims: %+v", claims)
	}
}

func TestOIDCRefusesWhatDoesNotCheck(t *testing.T) {
	cases := []struct {
		what   string
		tamper func(t *testing.T, f *oidcFixture)
		answer func(url.Values)
	}{
		{"a bad signature", func(t *testing.T, f *oidcFixture) { f.provider.BreakSignature = true }, nil},
		{"the wrong audience", func(t *testing.T, f *oidcFixture) {
			f.provider.Tamper = func(_, c map[string]any) { c["aud"] = "someone-else" }
		}, nil},
		{"another authorized party", func(t *testing.T, f *oidcFixture) {
			f.provider.Tamper = func(_, c map[string]any) { c["aud"], c["azp"] = []string{"bam", "other"}, "other" }
		}, nil},
		{"an expired token", func(t *testing.T, f *oidcFixture) {
			f.provider.Tamper = func(_, c map[string]any) { c["exp"] = time.Now().Add(-time.Hour).Unix() }
		}, nil},
		{"a token issued in the future", func(t *testing.T, f *oidcFixture) {
			f.provider.Tamper = func(_, c map[string]any) { c["iat"] = time.Now().Add(time.Hour).Unix() }
		}, nil},
		{"another issuer", func(t *testing.T, f *oidcFixture) {
			f.provider.Tamper = func(_, c map[string]any) { c["iss"] = "https://evil.example" }
		}, nil},
		{"a replayed nonce", func(t *testing.T, f *oidcFixture) {
			// A token from an earlier sign-in carries that one's nonce.
			if _, err := f.signIn(t, ada, nil); err != nil {
				t.Fatal(err)
			}
			earlier := f.provider.Nonces()[0]
			f.provider.Tamper = func(_, c map[string]any) { c["nonce"] = earlier }
		}, nil},
		{"no nonce", func(t *testing.T, f *oidcFixture) {
			f.provider.Tamper = func(_, c map[string]any) { delete(c, "nonce") }
		}, nil},
		{"alg none", func(t *testing.T, f *oidcFixture) {
			f.provider.Tamper = func(h, _ map[string]any) { h["alg"] = "none" }
		}, nil},
		{"HS256", func(t *testing.T, f *oidcFixture) {
			f.provider.Tamper = func(h, _ map[string]any) { h["alg"] = "HS256" }
		}, nil},
		{"a mismatched state", nil, func(q url.Values) { q.Set("state", "another-state") }},
		{"a code not given", nil, func(q url.Values) { q.Set("code", "made-up") }},
	}
	// Each is refused for its own reason.
	why := map[string]string{
		"a bad signature": "signature does not check", "the wrong audience": "for another client",
		"another authorized party": "given to another client", "an expired token": "expired",
		"a token issued in the future": "in the future", "another issuer": "is from",
		"a replayed nonce": "not for this sign-in", "no nonce": "not for this sign-in",
		"alg none": "not taken", "HS256": "not taken",
		"a mismatched state": "answer is not for this sign-in", "a code not given": "invalid_grant",
	}
	for _, c := range cases {
		t.Run(c.what, func(t *testing.T) {
			f := newOIDCFixture(t)
			if c.tamper != nil {
				c.tamper(t, f)
			}
			claims, err := f.signIn(t, ada, c.answer)
			if err == nil || errors.Is(err, ErrRefused) || !strings.Contains(err.Error(), why[c.what]) {
				t.Errorf("signed in as %+v, err %v", claims, err)
			}
		})
	}
}

func TestOIDCSaysWhenSomeoneCancels(t *testing.T) {
	f := newOIDCFixture(t)
	_, err := f.signIn(t, url.Values{"action": {"deny"}}, nil)
	if !errors.Is(err, ErrRefused) {
		t.Errorf("err: %v", err)
	}
}

func TestOIDCFollowsKeyRotation(t *testing.T) {
	f := newOIDCFixture(t)
	if _, err := f.signIn(t, ada, nil); err != nil {
		t.Fatal(err)
	}
	// A new kid fetches the keys again, though they were fetched just now,
	// once the least gap between fetches has passed.
	f.provider.Rotate()
	later := time.Now().Add(keysRefetchGap + time.Second)
	f.oidc.Now = func() time.Time { return later }
	if _, err := f.signIn(t, ada, nil); err != nil {
		t.Errorf("after rotation: %v", err)
	}
	// Within the gap, a made-up kid does not fetch them again.
	f.provider.Rotate()
	if _, err := f.signIn(t, ada, nil); err == nil {
		t.Error("the keys were fetched again within the gap")
	}
}

func TestOIDCChecksDiscovery(t *testing.T) {
	f := newOIDCFixture(t)
	f.config.Settings["issuer"] = f.provider.Issuer + "/"
	if _, _, err := f.oidc.Begin(context.Background(), f.config); err != nil {
		t.Errorf("a trailing slash: %v", err)
	}
	f = newOIDCFixture(t)
	real := f.provider.Issuer
	f.provider.Issuer = "https://elsewhere.example"
	f.config.Settings["issuer"] = real
	if _, _, err := f.oidc.Begin(context.Background(), f.config); err == nil || !strings.Contains(err.Error(), "elsewhere") {
		t.Errorf("another issuer's document: %v", err)
	}
}

func TestOIDCCheck(t *testing.T) {
	o := &OIDC{}
	ok := map[string]string{"issuer": "https://id.example", "client_id": "bam"}
	if err := o.Check(Config{Settings: ok}); err != nil {
		t.Errorf("fine settings: %v", err)
	}
	for what, change := range map[string]map[string]string{
		"no issuer":             {"issuer": ""},
		"no client ID":          {"client_id": ""},
		"http elsewhere":        {"issuer": "http://id.example"},
		"not a URL":             {"issuer": "id.example"},
		"a query":               {"issuer": "https://id.example/?x=1"},
		"scopes without openid": {"scopes": "email profile"},
	} {
		s := map[string]string{}
		for k, v := range ok {
			s[k] = v
		}
		for k, v := range change {
			s[k] = v
		}
		if err := o.Check(Config{Settings: s}); err == nil {
			t.Errorf("%s: no error", what)
		}
	}
	if err := o.Check(Config{Settings: map[string]string{"issuer": "http://127.0.0.1:9000", "client_id": "bam"}}); err != nil {
		t.Errorf("http on this machine: %v", err)
	}
}
