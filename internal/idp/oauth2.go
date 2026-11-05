package idp

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
)

// OAuth2Name is plain OAuth2's key.
const OAuth2Name = "oauth2"

// OAuth2 signs people in through a provider that offers OAuth2 without
// OpenID Connect, such as GitHub (docs/access.md#plain-oauth2): the
// authorization code flow, with state always and PKCE unless turned off,
// then who they are read from the provider's user endpoint, by paths into
// its JSON. A preset fills in a known provider's endpoints and paths.
type OAuth2 struct {
	// Client makes the requests to providers; nil is one with a timeout.
	Client *http.Client
}

// oauth2Preset is what a preset fills in: any setting left blank takes its
// value from here.
type oauth2Preset struct {
	label    string
	settings map[string]string
	// extra reads what the user endpoint does not say, such as GitHub's
	// verified emails and its organizations.
	extra func(ctx context.Context, o *OAuth2, s map[string]string, token string, user map[string]any) error
}

var oauth2Presets = map[string]oauth2Preset{
	"github": {
		label: "GitHub",
		settings: map[string]string{
			"authorization_url":   "https://github.com/login/oauth/authorize",
			"token_url":           "https://github.com/login/oauth/access_token",
			"user_url":            "https://api.github.com/user",
			"scopes":              "read:user user:email read:org",
			"id_path":             "id",
			"email_path":          "email",
			"email_verified_path": "email_verified",
			"name_path":           "name,login",
			"groups_path":         "groups",
		},
		extra: githubExtra,
	},
	"discord": {
		label: "Discord",
		settings: map[string]string{
			"authorization_url":   "https://discord.com/oauth2/authorize",
			"token_url":           "https://discord.com/api/oauth2/token",
			"user_url":            "https://discord.com/api/users/@me",
			"scopes":              "identify email",
			"id_path":             "id",
			"email_path":          "email",
			"email_verified_path": "verified",
			"name_path":           "global_name,username",
		},
	},
}

func (*OAuth2) Name() string  { return OAuth2Name }
func (*OAuth2) Label() string { return "OAuth2" }

func (*OAuth2) Fields() []Field {
	return []Field{
		{Name: "preset", Label: "Preset",
			Help: "github or discord fills in the endpoints, scopes and paths; any you set below are used instead. Blank for another provider, whose endpoints and ID path are then required."},
		{Name: "authorization_url", Label: "Authorization endpoint"},
		{Name: "token_url", Label: "Token endpoint"},
		{Name: "user_url", Label: "User endpoint", Help: "Answers JSON about whoever the access token is for."},
		{Name: "client_id", Label: "Client ID", Required: true},
		{Name: "client_secret", Label: "Client secret", Secret: true},
		{Name: "scopes", Label: "Scopes", Help: "Separated by spaces."},
		{Name: "id_path", Label: "ID path",
			Help: "Where the user endpoint's JSON has their stable ID, such as id, or data.id: never a login name, which can change."},
		{Name: "email_path", Label: "Email path", Help: "Blank is email."},
		{Name: "email_verified_path", Label: "Email verified path",
			Help: "Where the JSON says the email is verified. Blank, no email counts as verified, so none links an account by itself."},
		{Name: "name_path", Label: "Name path", Help: "Blank is name. Several, separated by commas, are tried in order."},
		{Name: "groups_path", Label: "Groups path", Help: "Where the JSON lists their groups, for rules. Blank reads none."},
		{Name: "pkce", Label: "PKCE", Help: "Blank or on sends a PKCE challenge; off for a provider that refuses one."},
	}
}

// settings answers a provider's settings with its preset's filled in where
// they are blank.
func (*OAuth2) settings(c Config) map[string]string {
	out := map[string]string{}
	if p, ok := oauth2Presets[strings.ToLower(strings.TrimSpace(c.Settings["preset"]))]; ok {
		for k, v := range p.settings {
			out[k] = v
		}
	}
	for k, v := range c.Settings {
		if v = strings.TrimSpace(v); v != "" {
			out[k] = v
		}
	}
	return out
}

func (o *OAuth2) Check(c Config) error {
	if err := CheckRequired(o, c); err != nil {
		return err
	}
	if p := strings.ToLower(strings.TrimSpace(c.Settings["preset"])); p != "" {
		if _, ok := oauth2Presets[p]; !ok {
			return fmt.Errorf("Preset must be github, discord or blank, not %q", c.Settings["preset"])
		}
	}
	s := o.settings(c)
	for _, f := range []struct{ name, label string }{{"authorization_url", "Authorization endpoint"}, {"token_url", "Token endpoint"}, {"user_url", "User endpoint"}} {
		if s[f.name] == "" {
			return fmt.Errorf("%s is required without a preset", f.label)
		}
		if err := checkEndpoint(s[f.name]); err != nil {
			return fmt.Errorf("%s %s", f.label, err)
		}
	}
	if s["id_path"] == "" {
		return errors.New("ID path is required without a preset")
	}
	if p := strings.ToLower(s["pkce"]); p != "" && p != "on" && p != "off" {
		return errors.New("PKCE must be on, off or blank")
	}
	return nil
}

func (o *OAuth2) Begin(_ context.Context, c Config) (string, map[string]string, error) {
	s := o.settings(c)
	u, err := url.Parse(s["authorization_url"])
	if err != nil {
		return "", nil, err
	}
	q := u.Query()
	q.Set("response_type", "code")
	q.Set("client_id", s["client_id"])
	q.Set("redirect_uri", c.CallbackURL)
	if scopes := strings.Join(strings.Fields(s["scopes"]), " "); scopes != "" {
		q.Set("scope", scopes)
	}
	q.Set("state", c.State)
	kept := map[string]string{"state": c.State}
	if !strings.EqualFold(s["pkce"], "off") {
		verifier, err := randomString()
		if err != nil {
			return "", nil, err
		}
		challenge := sha256.Sum256([]byte(verifier))
		q.Set("code_challenge", base64.RawURLEncoding.EncodeToString(challenge[:]))
		q.Set("code_challenge_method", "S256")
		kept["verifier"] = verifier
	}
	u.RawQuery = q.Encode()
	return u.String(), kept, nil
}

func (o *OAuth2) Finish(ctx context.Context, c Config, r *http.Request, state map[string]string) (*Claims, error) {
	if err := r.ParseForm(); err != nil {
		return nil, fmt.Errorf("reading the answer: %w", err)
	}
	if state["state"] == "" || subtle.ConstantTimeCompare([]byte(r.Form.Get("state")), []byte(state["state"])) != 1 {
		return nil, errors.New("the answer is not for this sign-in")
	}
	if e := r.Form.Get("error"); e != "" {
		if slices.Contains(refusals, e) {
			return nil, fmt.Errorf("%w: %s", ErrRefused, e)
		}
		return nil, fmt.Errorf("the provider answered %.100s: %.200s", e, r.Form.Get("error_description"))
	}
	code := r.Form.Get("code")
	if code == "" {
		return nil, errors.New("the answer has no code")
	}
	token, err := o.exchange(ctx, c, code, state["verifier"])
	if err != nil {
		return nil, err
	}
	s := o.settings(c)
	var user map[string]any
	if err := o.getJSON(ctx, s["user_url"], token, &user); err != nil {
		return nil, fmt.Errorf("reading who signed in: %w", err)
	}
	if p, ok := oauth2Presets[strings.ToLower(strings.TrimSpace(c.Settings["preset"]))]; ok && p.extra != nil {
		if err := p.extra(ctx, o, s, token, user); err != nil {
			return nil, err
		}
	}
	claims := oauth2Claims(s, user)
	if claims.Subject == "" {
		return nil, errors.New("the user endpoint gave no ID at " + s["id_path"])
	}
	return claims, nil
}

// oauth2Claims reads who came back from the user endpoint's JSON.
func oauth2Claims(s map[string]string, user map[string]any) *Claims {
	first := func(paths, fallback string) string {
		if paths == "" {
			paths = fallback
		}
		for _, p := range strings.Split(paths, ",") {
			if v := claimStrings(lookupPath(user, strings.TrimSpace(p))); len(v) > 0 && v[0] != "" {
				return v[0]
			}
		}
		return ""
	}
	values := map[string][]string{}
	for k, v := range user {
		if s := claimStrings(v); len(s) > 0 {
			values[k] = s
		}
	}
	// Rules and the groups allowed read "groups", wherever they were.
	if s["groups_path"] != "" {
		values[defaultGroupsClaim] = claimStrings(lookupPath(user, s["groups_path"]))
	}
	email := first(s["email_path"], defaultEmailClaim)
	return &Claims{
		Subject:       first(s["id_path"], ""),
		Email:         email,
		EmailVerified: email != "" && s["email_verified_path"] != "" && first(s["email_verified_path"], "") == "true",
		Name:          first(s["name_path"], defaultNameClaim),
		Values:        values,
	}
}

// lookupPath follows a path such as data.user.id into JSON.
func lookupPath(v any, path string) any {
	if path == "" {
		return nil
	}
	for _, part := range strings.Split(path, ".") {
		m, ok := v.(map[string]any)
		if !ok {
			return nil
		}
		v = m[part]
	}
	return v
}

// exchange trades the code for an access token, with the client secret in
// the form, as GitHub and most others take it, and the PKCE verifier.
func (o *OAuth2) exchange(ctx context.Context, c Config, code, verifier string) (string, error) {
	s := o.settings(c)
	form := url.Values{
		"grant_type":   {"authorization_code"},
		"code":         {code},
		"redirect_uri": {c.CallbackURL},
		"client_id":    {s["client_id"]},
	}
	if secret := c.Secrets["client_secret"]; secret != "" {
		form.Set("client_secret", secret)
	}
	if verifier != "" {
		form.Set("code_verifier", verifier)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s["token_url"], strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	// GitHub answers JSON only when asked; otherwise a form.
	req.Header.Set("Accept", "application/json")
	res, err := o.client().Do(req)
	if err != nil {
		return "", fmt.Errorf("trading the code: %w", err)
	}
	defer res.Body.Close()
	var out struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
		Description string `json:"error_description"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, maxResponse)).Decode(&out); err != nil && res.StatusCode == http.StatusOK {
		return "", fmt.Errorf("reading the token: %w", err)
	}
	// GitHub answers a bad code with 200 and an error.
	if res.StatusCode != http.StatusOK || out.Error != "" {
		return "", fmt.Errorf("trading the code: %s %.100s %.200s", res.Status, out.Error, out.Description)
	}
	if out.AccessToken == "" {
		return "", errors.New("the provider gave no access token")
	}
	return out.AccessToken, nil
}

func (o *OAuth2) client() *http.Client {
	if o.Client != nil {
		return o.Client
	}
	return &http.Client{Timeout: 10 * time.Second}
}

// getJSON reads JSON from a provider's API as whoever token is for. Numbers
// are kept exact, as IDs are numbers at some providers.
func (o *OAuth2) getJSON(ctx context.Context, u, token string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	// GitHub refuses a request without one.
	req.Header.Set("User-Agent", "BAM!")
	res, err := o.client().Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("%s answered %s", req.URL.Redacted(), res.Status)
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, maxResponse))
	if err != nil {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("%s: %w", u, err)
	}
	return nil
}

// githubAPI is the base of GitHub's API, from the user endpoint: GitHub
// Enterprise's is not api.github.com.
func githubAPI(s map[string]string) string {
	return strings.TrimSuffix(strings.TrimSuffix(s["user_url"], "/"), "/user")
}

// githubEmail is one of the emails GitHub has for someone.
type githubEmail struct {
	Email    string `json:"email"`
	Primary  bool   `json:"primary"`
	Verified bool   `json:"verified"`
}

// githubExtra reads what GitHub's /user does not say: whether the email is
// verified, from /user/emails, and the organizations and teams they belong
// to, as groups: "org" for each organization and "org/team" for each team.
func githubExtra(ctx context.Context, o *OAuth2, s map[string]string, token string, user map[string]any) error {
	api := githubAPI(s)
	var emails []githubEmail
	if err := o.getJSON(ctx, api+"/user/emails", token, &emails); err != nil {
		return fmt.Errorf("reading their emails: %w", err)
	}
	// The primary email, if verified; else any verified one; else the
	// primary, unverified.
	user["email"], user["email_verified"] = nil, false
	for _, pass := range []func(e githubEmail) bool{
		func(e githubEmail) bool { return e.Primary && e.Verified },
		func(e githubEmail) bool { return e.Verified },
		func(e githubEmail) bool { return e.Primary },
	} {
		if i := slices.IndexFunc(emails, pass); i >= 0 {
			user["email"], user["email_verified"] = emails[i].Email, emails[i].Verified
			break
		}
	}

	var orgs []struct {
		Login string `json:"login"`
	}
	if err := o.getJSON(ctx, api+"/user/orgs", token, &orgs); err != nil {
		return fmt.Errorf("reading their organizations: %w", err)
	}
	var teams []struct {
		Slug         string `json:"slug"`
		Organization struct {
			Login string `json:"login"`
		} `json:"organization"`
	}
	if err := o.getJSON(ctx, api+"/user/teams", token, &teams); err != nil {
		return fmt.Errorf("reading their teams: %w", err)
	}
	groups := []any{}
	for _, org := range orgs {
		groups = append(groups, org.Login)
	}
	for _, t := range teams {
		if !slices.Contains(groups, any(t.Organization.Login)) {
			groups = append(groups, t.Organization.Login)
		}
		groups = append(groups, t.Organization.Login+"/"+t.Slug)
	}
	user["groups"] = groups
	return nil
}
