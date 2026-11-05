package idp

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

// testGitHub is a test OAuth2 provider shaped as GitHub is: its authorize
// page approves whoever the test says, and its API answers for them.
type testGitHub struct {
	mu     sync.Mutex
	codes  map[string]githubGrant
	tokens map[string]*githubUser
	// user is who the next approval is for.
	user *githubUser
}

type githubGrant struct {
	user      *githubUser
	challenge string
	redirect  string
}

type githubUser struct {
	ID     json.Number
	Login  string
	Name   string
	Emails []githubEmail
	Orgs   []string
	Teams  [][2]string // organization, team
}

func newTestGitHub(t *testing.T) (*testGitHub, *httptest.Server) {
	t.Helper()
	g := &testGitHub{codes: map[string]githubGrant{}, tokens: map[string]*githubUser{}}
	ts := httptest.NewServer(g)
	t.Cleanup(ts.Close)
	return g, ts
}

func (g *testGitHub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	g.mu.Lock()
	defer g.mu.Unlock()
	answer := func(v any) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(v)
	}
	switch r.URL.Path {
	case "/login/oauth/authorize":
		q := r.URL.Query()
		if q.Get("client_id") != "bam" {
			http.Error(w, "unknown client", http.StatusBadRequest)
			return
		}
		code := "code-" + q.Get("state")
		g.codes[code] = githubGrant{user: g.user, challenge: q.Get("code_challenge"), redirect: q.Get("redirect_uri")}
		back, _ := url.Parse(q.Get("redirect_uri"))
		back.RawQuery = url.Values{"code": {code}, "state": {q.Get("state")}}.Encode()
		http.Redirect(w, r, back.String(), http.StatusFound)
	case "/login/oauth/access_token":
		_ = r.ParseForm()
		grant, ok := g.codes[r.Form.Get("code")]
		delete(g.codes, r.Form.Get("code"))
		sum := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
		switch {
		case !ok || r.Form.Get("client_id") != "bam" || r.Form.Get("client_secret") != "s3cret" || r.Form.Get("redirect_uri") != grant.redirect:
			answer(map[string]string{"error": "bad_verification_code"})
		case grant.challenge != "" && base64.RawURLEncoding.EncodeToString(sum[:]) != grant.challenge:
			answer(map[string]string{"error": "invalid_grant", "error_description": "the verifier does not match"})
		default:
			token := "token-" + r.Form.Get("code")
			g.tokens[token] = grant.user
			answer(map[string]string{"access_token": token, "token_type": "bearer"})
		}
	default:
		u := g.tokens[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")]
		if u == nil || r.Header.Get("User-Agent") == "" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/api/v3/user":
			answer(map[string]any{"id": u.ID, "login": u.Login, "name": u.Name, "email": nil})
		case "/api/v3/user/emails":
			answer(u.Emails)
		case "/api/v3/user/orgs":
			orgs := []map[string]string{}
			for _, o := range u.Orgs {
				orgs = append(orgs, map[string]string{"login": o})
			}
			answer(orgs)
		case "/api/v3/user/teams":
			teams := []map[string]any{}
			for _, t := range u.Teams {
				teams = append(teams, map[string]any{"slug": t[1], "organization": map[string]string{"login": t[0]}})
			}
			answer(teams)
		default:
			http.NotFound(w, r)
		}
	}
}

// githubConfig is a provider with the GitHub preset, its endpoints the test
// provider's, as for GitHub Enterprise.
func githubConfig(ts *httptest.Server) Config {
	return Config{
		Key: "github",
		Settings: map[string]string{
			"preset": "github", "client_id": "bam",
			"authorization_url": ts.URL + "/login/oauth/authorize",
			"token_url":         ts.URL + "/login/oauth/access_token",
			"user_url":          ts.URL + "/api/v3/user",
		},
		Secrets:     map[string]string{"client_secret": "s3cret"},
		CallbackURL: "https://bam.example/api/v1/auth/providers/github/callback",
		State:       "the-state",
	}
}

// oauth2SignIn goes through Begin, the provider's approval, and Finish;
// answer, when given, changes the provider's answer first.
func oauth2SignIn(t *testing.T, o *OAuth2, c Config, answer func(url.Values)) (*Claims, error) {
	t.Helper()
	redirect, kept, err := o.Begin(context.Background(), c)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	noFollow := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := noFollow.Get(redirect)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	back, err := url.Parse(res.Header.Get("Location"))
	if err != nil || !strings.HasPrefix(back.String(), c.CallbackURL) {
		t.Fatalf("the provider sent the browser to %q", res.Header.Get("Location"))
	}
	q := back.Query()
	if answer != nil {
		answer(q)
	}
	return o.Finish(context.Background(), c, httptest.NewRequest(http.MethodGet, "/callback?"+q.Encode(), nil), kept)
}

func githubAda() *githubUser {
	return &githubUser{
		// Past what a float64 holds exactly, so it must be read as written.
		ID: "9007199254740993", Login: "ada", Name: "Ada Lovelace",
		Emails: []githubEmail{{Email: "old@example.org", Verified: true}, {Email: "ada@example.org", Primary: true, Verified: true}},
		Orgs:   []string{"hackspace"},
		Teams:  [][2]string{{"hackspace", "woodshop"}, {"lathes", "turners"}},
	}
}

func TestSigningInWithGitHub(t *testing.T) {
	g, ts := newTestGitHub(t)
	g.user = githubAda()
	o := &OAuth2{}
	c := githubConfig(ts)
	if err := o.Check(c); err != nil {
		t.Fatalf("Check: %v", err)
	}
	claims, err := oauth2SignIn(t, o, c, nil)
	if err != nil {
		t.Fatalf("signing in: %v", err)
	}
	if claims.Subject != "9007199254740993" || claims.Email != "ada@example.org" || !claims.EmailVerified || claims.Name != "Ada Lovelace" {
		t.Errorf("claims %+v", claims)
	}
	if got := strings.Join(claims.Values["groups"], ","); got != "hackspace,hackspace/woodshop,lathes,lathes/turners" {
		t.Errorf("groups %q", got)
	}
	if !claims.Has("login", "ada") {
		t.Errorf("values %v", claims.Values)
	}

	// A new login name, and no display name: the same ID, the login as the
	// name.
	g.user.Login, g.user.Name = "ada-l", ""
	again, err := oauth2SignIn(t, o, c, nil)
	if err != nil {
		t.Fatal(err)
	}
	if again.Subject != claims.Subject || again.Name != "ada-l" {
		t.Errorf("after a new login: %+v", again)
	}
}

func TestAnUnverifiedEmailIsNotVerified(t *testing.T) {
	g, ts := newTestGitHub(t)
	g.user = githubAda()
	g.user.Emails = []githubEmail{{Email: "ada@example.org", Primary: true}}
	claims, err := oauth2SignIn(t, &OAuth2{}, githubConfig(ts), nil)
	if err != nil {
		t.Fatal(err)
	}
	if claims.Email != "ada@example.org" || claims.EmailVerified {
		t.Errorf("an unverified email: %+v", claims)
	}

	// A provider that says nothing of verifying never verifies.
	custom := githubConfig(ts)
	custom.Settings["preset"] = ""
	custom.Settings["id_path"] = "id"
	custom.Settings["email_path"] = "login"
	claims, err = oauth2SignIn(t, &OAuth2{}, custom, nil)
	if err != nil {
		t.Fatal(err)
	}
	if claims.Email != "ada" || claims.EmailVerified || claims.Values["groups"] != nil {
		t.Errorf("a provider without a verified path: %+v", claims)
	}
}

func TestAnOAuth2AnswerIsRefused(t *testing.T) {
	g, ts := newTestGitHub(t)
	g.user = githubAda()
	o := &OAuth2{}
	c := githubConfig(ts)
	if _, err := oauth2SignIn(t, o, c, func(q url.Values) { q.Set("state", "another") }); err == nil || !strings.Contains(err.Error(), "not for this sign-in") {
		t.Errorf("a mismatched state: %v", err)
	}
	if _, err := oauth2SignIn(t, o, c, func(q url.Values) { q.Set("code", "made-up") }); err == nil || !strings.Contains(err.Error(), "bad_verification_code") {
		t.Errorf("a made-up code: %v", err)
	}
	if _, err := oauth2SignIn(t, o, c, func(q url.Values) { q.Del("code"); q.Set("error", "access_denied") }); !errors.Is(err, ErrRefused) {
		t.Errorf("someone cancelling: %v", err)
	}

	// The PKCE verifier proves the code is BAM!'s: another one is refused.
	redirect, kept, _ := o.Begin(context.Background(), c)
	u, _ := url.Parse(redirect)
	if u.Query().Get("code_challenge") == "" || u.Query().Get("code_challenge_method") != "S256" || u.Query().Get("scope") != "read:user user:email read:org" {
		t.Errorf("authorization request %s", redirect)
	}
	noFollow := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, _ := noFollow.Get(redirect)
	res.Body.Close()
	back, _ := url.Parse(res.Header.Get("Location"))
	kept["verifier"] = "another verifier"
	if _, err := o.Finish(context.Background(), c, httptest.NewRequest(http.MethodGet, "/callback?"+back.RawQuery, nil), kept); err == nil || !strings.Contains(err.Error(), "invalid_grant") {
		t.Errorf("another verifier: %v", err)
	}

	// With PKCE off, none is sent.
	c.Settings["pkce"] = "off"
	redirect, kept, _ = o.Begin(context.Background(), c)
	if strings.Contains(redirect, "code_challenge") || kept["verifier"] != "" {
		t.Errorf("PKCE off: %s %v", redirect, kept)
	}
}

func TestAnOAuth2ProviderReadsPathsIntoItsJSON(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "t"})
		case "/me":
			_, _ = w.Write([]byte(`{"data": {"user": {"id": 42, "mail": "bo@example.org", "checked": true, "display": "Bo", "teams": ["lathe", "mill"]}}}`))
		}
	}))
	defer api.Close()
	c := Config{
		Settings: map[string]string{
			"authorization_url": api.URL + "/authorize", "token_url": api.URL + "/token", "user_url": api.URL + "/me", "client_id": "bam",
			"id_path": "data.user.id", "email_path": "data.user.mail", "email_verified_path": "data.user.checked",
			"name_path": "data.user.nickname, data.user.display", "groups_path": "data.user.teams",
		},
		CallbackURL: "https://bam.example/callback", State: "s",
	}
	o := &OAuth2{}
	if err := o.Check(c); err != nil {
		t.Fatal(err)
	}
	_, kept, _ := o.Begin(context.Background(), c)
	claims, err := o.Finish(context.Background(), c, httptest.NewRequest(http.MethodGet, "/callback?code=x&state=s", nil), kept)
	if err != nil {
		t.Fatal(err)
	}
	if claims.Subject != "42" || claims.Email != "bo@example.org" || !claims.EmailVerified || claims.Name != "Bo" || !claims.Has("groups", "mill") {
		t.Errorf("claims %+v", claims)
	}
}

func TestOAuth2Settings(t *testing.T) {
	o := &OAuth2{}
	bad := map[string]map[string]string{
		"an unknown preset":       {"preset": "myspace", "client_id": "bam"},
		"no endpoints":            {"client_id": "bam", "id_path": "id"},
		"no ID path":              {"client_id": "bam", "authorization_url": "https://p/a", "token_url": "https://p/t", "user_url": "https://p/u"},
		"an http endpoint":        {"client_id": "bam", "authorization_url": "http://p.example/a", "token_url": "https://p/t", "user_url": "https://p/u", "id_path": "id"},
		"no client ID":            {"preset": "github"},
		"PKCE neither on nor off": {"preset": "github", "client_id": "bam", "pkce": "maybe"},
	}
	for name, s := range bad {
		if err := o.Check(Config{Settings: s}); err == nil {
			t.Errorf("%s was taken", name)
		}
	}
	for _, preset := range []string{"github", "Discord"} {
		if err := o.Check(Config{Settings: map[string]string{"preset": preset, "client_id": "bam"}}); err != nil {
			t.Errorf("the %s preset alone: %v", preset, err)
		}
	}
}
