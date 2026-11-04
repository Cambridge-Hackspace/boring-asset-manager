package idp

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// OIDCName is OpenID Connect's key.
const OIDCName = "oidc"

const (
	// discoveryTTL is how long a provider's discovery document is kept.
	discoveryTTL = time.Hour
	// keysTTL is how long its signing keys are kept, unless a token names
	// one not among them first.
	keysTTL = 24 * time.Hour
	// keysRefetchGap is the least time between fetches of the keys for an
	// unknown kid, so tokens naming made-up keys cannot set BAM! fetching.
	keysRefetchGap = time.Minute
	// clockSkew is how far a token's times may be off BAM!'s clock.
	clockSkew = time.Minute
	// maxResponse is the most read of any answer from a provider.
	maxResponse = 1 << 20
)

// The default scopes, and the claims read for the email, name and groups.
const (
	defaultScopes      = "openid email profile"
	defaultEmailClaim  = "email"
	defaultNameClaim   = "name"
	defaultGroupsClaim = "groups"
)

// refusals are the errors a provider answers with when someone did not
// sign in, rather than the provider failing.
var refusals = []string{"access_denied", "login_required", "consent_required", "interaction_required", "account_selection_required"}

// OIDC signs people in through an OpenID Connect provider
// (docs/access.md#openid-connect): the authorization code flow with PKCE,
// state and nonce, its ID token checked against the provider's keys.
type OIDC struct {
	// Client makes the requests to providers; nil is one with a timeout.
	Client *http.Client
	// Now is the clock, for tests; nil is time.Now.
	Now func() time.Time

	mu      sync.Mutex
	issuers map[string]*issuerCache
}

// issuerCache is what is kept of one provider between sign-ins.
type issuerCache struct {
	doc    *discovery
	docAt  time.Time
	keys   []jwk
	keysAt time.Time
}

type discovery struct {
	Issuer                string   `json:"issuer"`
	AuthorizationEndpoint string   `json:"authorization_endpoint"`
	TokenEndpoint         string   `json:"token_endpoint"`
	UserinfoEndpoint      string   `json:"userinfo_endpoint"`
	JWKSURI               string   `json:"jwks_uri"`
	TokenAuthMethods      []string `json:"token_endpoint_auth_methods_supported"`
}

func (*OIDC) Name() string  { return OIDCName }
func (*OIDC) Label() string { return "OpenID Connect" }

func (*OIDC) Fields() []Field {
	return []Field{
		{Name: "issuer", Label: "Issuer", Required: true,
			Help: "The provider's issuer URL: its discovery document is this followed by /.well-known/openid-configuration."},
		{Name: "client_id", Label: "Client ID", Required: true},
		{Name: "client_secret", Label: "Client secret", Secret: true,
			Help: "Blank for a public client, which PKCE alone protects."},
		{Name: "scopes", Label: "Scopes",
			Help: "Separated by spaces. Blank is " + defaultScopes + "; add whichever carries groups, such as groups."},
		{Name: "email_claim", Label: "Email claim", Help: "Blank is " + defaultEmailClaim + "."},
		{Name: "name_claim", Label: "Name claim", Help: "Blank is " + defaultNameClaim + "."},
		{Name: "groups_claim", Label: "Groups claim", Help: "Blank is " + defaultGroupsClaim + "."},
	}
}

func (o *OIDC) Check(c Config) error {
	if err := CheckRequired(o, c); err != nil {
		return err
	}
	if err := checkEndpoint(c.Settings["issuer"]); err != nil {
		return fmt.Errorf("Issuer %s", err)
	}
	if u, _ := url.Parse(c.Settings["issuer"]); u.RawQuery != "" || u.Fragment != "" {
		return errors.New("Issuer cannot have a query or fragment")
	}
	if !slices.Contains(scopes(c), "openid") {
		return errors.New("Scopes must include openid")
	}
	return nil
}

// checkEndpoint reports what is wrong with a provider's URL: it must be
// https, but for a provider on this machine.
func checkEndpoint(raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return errors.New("must be a URL")
	}
	if u.Scheme == "https" {
		return nil
	}
	if u.Scheme == "http" {
		host := u.Hostname()
		if ip := net.ParseIP(host); host == "localhost" || (ip != nil && ip.IsLoopback()) {
			return nil
		}
	}
	return errors.New("must be https")
}

func scopes(c Config) []string {
	s := strings.Fields(c.Settings["scopes"])
	if len(s) == 0 {
		return strings.Fields(defaultScopes)
	}
	return s
}

func setting(c Config, name, fallback string) string {
	if v := strings.TrimSpace(c.Settings[name]); v != "" {
		return v
	}
	return fallback
}

func randomString() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func (o *OIDC) Begin(ctx context.Context, c Config) (string, map[string]string, error) {
	doc, err := o.discover(ctx, c)
	if err != nil {
		return "", nil, err
	}
	verifier, err := randomString()
	if err != nil {
		return "", nil, err
	}
	nonce, err := randomString()
	if err != nil {
		return "", nil, err
	}
	challenge := sha256.Sum256([]byte(verifier))
	u, err := url.Parse(doc.AuthorizationEndpoint)
	if err != nil {
		return "", nil, err
	}
	q := u.Query()
	q.Set("response_type", "code")
	q.Set("client_id", c.Settings["client_id"])
	q.Set("redirect_uri", c.CallbackURL)
	q.Set("scope", strings.Join(scopes(c), " "))
	q.Set("state", c.State)
	q.Set("nonce", nonce)
	q.Set("code_challenge", base64.RawURLEncoding.EncodeToString(challenge[:]))
	q.Set("code_challenge_method", "S256")
	u.RawQuery = q.Encode()
	return u.String(), map[string]string{"state": c.State, "nonce": nonce, "verifier": verifier}, nil
}

func (o *OIDC) Finish(ctx context.Context, c Config, r *http.Request, state map[string]string) (*Claims, error) {
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
	doc, err := o.discover(ctx, c)
	if err != nil {
		return nil, err
	}
	tokens, err := o.exchange(ctx, c, doc, code, state["verifier"])
	if err != nil {
		return nil, err
	}
	raw, err := o.checkIDToken(ctx, c, doc, tokens.IDToken, state["nonce"])
	if err != nil {
		return nil, err
	}

	// Claims the ID token does not carry are read from userinfo, which must
	// be about the same subject.
	wanted := []string{setting(c, "email_claim", defaultEmailClaim), setting(c, "name_claim", defaultNameClaim), setting(c, "groups_claim", defaultGroupsClaim)}
	missing := slices.ContainsFunc(wanted, func(k string) bool { _, ok := raw[k]; return !ok })
	if missing && doc.UserinfoEndpoint != "" && tokens.AccessToken != "" {
		info, err := o.userinfo(ctx, doc, tokens.AccessToken)
		if err != nil {
			return nil, err
		}
		if sub, _ := info["sub"].(string); sub != raw["sub"].(string) {
			return nil, errors.New("userinfo is about someone else")
		}
		for k, v := range info {
			if _, ok := raw[k]; !ok {
				raw[k] = v
			}
		}
	}
	return claimsOf(c, raw), nil
}

// claimsOf reads who came back from their claims.
func claimsOf(c Config, raw map[string]any) *Claims {
	values := map[string][]string{}
	for k, v := range raw {
		if s := claimStrings(v); len(s) > 0 {
			values[k] = s
		}
	}
	first := func(k string) string {
		if v := values[k]; len(v) > 0 {
			return v[0]
		}
		return ""
	}
	// Rules and the groups allowed read "groups", whichever claim has them.
	if g := setting(c, "groups_claim", defaultGroupsClaim); g != defaultGroupsClaim {
		values[defaultGroupsClaim] = values[g]
	}
	return &Claims{
		Subject:       first("sub"),
		Email:         first(setting(c, "email_claim", defaultEmailClaim)),
		EmailVerified: first("email_verified") == "true",
		Name:          first(setting(c, "name_claim", defaultNameClaim)),
		Values:        values,
		AMR:           values["amr"],
		ACR:           first("acr"),
	}
}

// claimStrings is a claim's value as strings: a string, number or boolean
// is one, a list of them is each.
func claimStrings(v any) []string {
	switch v := v.(type) {
	case string:
		return []string{v}
	case bool:
		return []string{strconv.FormatBool(v)}
	case float64:
		return []string{strconv.FormatFloat(v, 'f', -1, 64)}
	case []any:
		var out []string
		for _, e := range v {
			if s := claimStrings(e); len(s) == 1 {
				out = append(out, s[0])
			}
		}
		return out
	}
	return nil
}

func (o *OIDC) now() time.Time {
	if o.Now != nil {
		return o.Now()
	}
	return time.Now()
}

func (o *OIDC) client() *http.Client {
	if o.Client != nil {
		return o.Client
	}
	return &http.Client{Timeout: 10 * time.Second}
}

func (o *OIDC) cacheFor(issuer string) *issuerCache {
	if o.issuers == nil {
		o.issuers = map[string]*issuerCache{}
	}
	if o.issuers[issuer] == nil {
		o.issuers[issuer] = &issuerCache{}
	}
	return o.issuers[issuer]
}

// discover reads the provider's discovery document, or the one kept.
func (o *OIDC) discover(ctx context.Context, c Config) (*discovery, error) {
	issuer := strings.TrimSpace(c.Settings["issuer"])
	o.mu.Lock()
	cached := o.cacheFor(issuer)
	if cached.doc != nil && o.now().Sub(cached.docAt) < discoveryTTL {
		doc := cached.doc
		o.mu.Unlock()
		return doc, nil
	}
	o.mu.Unlock()

	var doc discovery
	if err := o.getJSON(ctx, strings.TrimSuffix(issuer, "/")+"/.well-known/openid-configuration", "", &doc); err != nil {
		return nil, fmt.Errorf("reading the discovery document: %w", err)
	}
	// It must be the issuer asked for; a trailing slash aside, as some
	// issuers have one and some admins leave it off.
	if strings.TrimSuffix(doc.Issuer, "/") != strings.TrimSuffix(issuer, "/") {
		return nil, fmt.Errorf("the discovery document is for %q", doc.Issuer)
	}
	for name, u := range map[string]string{"authorization_endpoint": doc.AuthorizationEndpoint, "token_endpoint": doc.TokenEndpoint, "jwks_uri": doc.JWKSURI} {
		if err := checkEndpoint(u); err != nil {
			return nil, fmt.Errorf("the discovery document's %s %s", name, err)
		}
	}
	if doc.UserinfoEndpoint != "" && checkEndpoint(doc.UserinfoEndpoint) != nil {
		doc.UserinfoEndpoint = ""
	}
	o.mu.Lock()
	cached.doc, cached.docAt = &doc, o.now()
	o.mu.Unlock()
	return &doc, nil
}

// keys are the provider's signing keys, fetched again when kid is not
// among them, at most once a keysRefetchGap.
func (o *OIDC) keys(ctx context.Context, doc *discovery, kid string) ([]jwk, error) {
	o.mu.Lock()
	cached := o.cacheFor(doc.Issuer)
	keys, at := cached.keys, cached.keysAt
	o.mu.Unlock()
	known := kid == "" || slices.ContainsFunc(keys, func(k jwk) bool { return k.Kid == kid })
	age := o.now().Sub(at)
	if keys != nil && age < keysTTL && (known || age < keysRefetchGap) {
		return keys, nil
	}
	data, err := o.get(ctx, doc.JWKSURI, "")
	if err != nil {
		return nil, fmt.Errorf("reading the signing keys: %w", err)
	}
	keys, err = parseJWKS(data)
	if err != nil {
		return nil, err
	}
	o.mu.Lock()
	cached.keys, cached.keysAt = keys, o.now()
	o.mu.Unlock()
	return keys, nil
}

type tokenResponse struct {
	IDToken     string `json:"id_token"`
	AccessToken string `json:"access_token"`
	Error       string `json:"error"`
}

// exchange trades the code for tokens, proving with the PKCE verifier and
// the client secret, if there is one, that BAM! asked for it.
func (o *OIDC) exchange(ctx context.Context, c Config, doc *discovery, code, verifier string) (*tokenResponse, error) {
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {c.CallbackURL},
		"code_verifier": {verifier},
	}
	id, secret := c.Settings["client_id"], c.Secrets["client_secret"]
	// The secret goes by Basic authentication, the default, unless the
	// provider takes it only in the form.
	basic := secret != "" && (len(doc.TokenAuthMethods) == 0 || slices.Contains(doc.TokenAuthMethods, "client_secret_basic") ||
		!slices.Contains(doc.TokenAuthMethods, "client_secret_post"))
	if !basic {
		form.Set("client_id", id)
		if secret != "" {
			form.Set("client_secret", secret)
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, doc.TokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	if basic {
		req.SetBasicAuth(url.QueryEscape(id), url.QueryEscape(secret))
	}
	res, err := o.client().Do(req)
	if err != nil {
		return nil, fmt.Errorf("trading the code: %w", err)
	}
	defer res.Body.Close()
	var out tokenResponse
	if err := json.NewDecoder(io.LimitReader(res.Body, maxResponse)).Decode(&out); err != nil && res.StatusCode == http.StatusOK {
		return nil, fmt.Errorf("reading the tokens: %w", err)
	}
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("trading the code: %s %.100s", res.Status, out.Error)
	}
	if out.IDToken == "" {
		return nil, errors.New("the provider gave no ID token")
	}
	return &out, nil
}

// checkIDToken checks the ID token fully and answers its claims: its
// signature by one of the provider's keys, its issuer, audience,
// authorized party, times and nonce.
func (o *OIDC) checkIDToken(ctx context.Context, c Config, doc *discovery, token, nonce string) (map[string]any, error) {
	t, err := parseJWS(token)
	if err != nil {
		return nil, err
	}
	keys, err := o.keys(ctx, doc, t.Kid)
	if err != nil {
		return nil, err
	}
	signed := false
	for _, k := range keys {
		if (t.Kid == "" || k.Kid == t.Kid) && (k.Alg == "" || k.Alg == t.Alg) && t.verify(k.Key) {
			signed = true
			break
		}
	}
	if !signed {
		return nil, errors.New("the ID token's signature does not check")
	}

	var claims map[string]any
	dec := json.NewDecoder(strings.NewReader(string(t.Payload)))
	if err := dec.Decode(&claims); err != nil {
		return nil, errors.New("bad ID token claims")
	}
	if iss, _ := claims["iss"].(string); iss != doc.Issuer {
		return nil, fmt.Errorf("the ID token is from %q", iss)
	}
	clientID := c.Settings["client_id"]
	aud := claimStrings(claims["aud"])
	if !slices.Contains(aud, clientID) {
		return nil, errors.New("the ID token is for another client")
	}
	azp, hasAZP := claims["azp"].(string)
	if (len(aud) > 1 || hasAZP) && azp != clientID {
		return nil, errors.New("the ID token was given to another client")
	}
	now := o.now()
	exp, ok := claims["exp"].(float64)
	if !ok || now.After(time.Unix(int64(exp), 0).Add(clockSkew)) {
		return nil, errors.New("the ID token has expired")
	}
	iat, ok := claims["iat"].(float64)
	if !ok || time.Unix(int64(iat), 0).After(now.Add(clockSkew)) {
		return nil, errors.New("the ID token is issued in the future")
	}
	if nbf, ok := claims["nbf"].(float64); ok && time.Unix(int64(nbf), 0).After(now.Add(clockSkew)) {
		return nil, errors.New("the ID token is not valid yet")
	}
	got, _ := claims["nonce"].(string)
	if nonce == "" || subtle.ConstantTimeCompare([]byte(got), []byte(nonce)) != 1 {
		return nil, errors.New("the ID token is not for this sign-in")
	}
	if sub, _ := claims["sub"].(string); sub == "" {
		return nil, errors.New("the ID token names no subject")
	}
	return claims, nil
}

func (o *OIDC) userinfo(ctx context.Context, doc *discovery, accessToken string) (map[string]any, error) {
	var info map[string]any
	if err := o.getJSON(ctx, doc.UserinfoEndpoint, accessToken, &info); err != nil {
		return nil, fmt.Errorf("reading userinfo: %w", err)
	}
	return info, nil
}

func (o *OIDC) get(ctx context.Context, u, bearer string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	res, err := o.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s answered %s", req.URL.Redacted(), res.Status)
	}
	return io.ReadAll(io.LimitReader(res.Body, maxResponse))
}

func (o *OIDC) getJSON(ctx context.Context, u, bearer string, v any) error {
	data, err := o.get(ctx, u, bearer)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("%s: %w", u, err)
	}
	return nil
}
