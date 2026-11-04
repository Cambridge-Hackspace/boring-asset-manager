package idp

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"html/template"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// TestOIDCProvider is an OpenID Connect provider for tests: discovery,
// a sign-in form that signs in as whoever is typed into it, the token
// endpoint, its keys and userinfo. The Go tests serve it with httptest;
// with [auth] test_protocol on, BAM! serves one at /test-oidc for the
// browser tests. Like the test protocol, it lets anyone sign in as anyone.
type TestOIDCProvider struct {
	Issuer       string
	ClientID     string
	ClientSecret string
	// Tamper changes an ID token's header and claims before it is signed,
	// to make bad ones.
	Tamper func(header, claims map[string]any)
	// BreakSignature signs ID tokens with a key the provider does not
	// publish.
	BreakSignature bool

	mu     sync.Mutex
	key    *ecdsa.PrivateKey
	kid    string
	codes  map[string]testGrant
	tokens map[string]map[string]any
	nonces []string
}

// testGrant is a code given, and what it was given for.
type testGrant struct {
	clientID, redirectURI, challenge, nonce string
	claims                                  map[string]any
}

// NewTestOIDCProvider makes one with a signing key of its own.
func NewTestOIDCProvider(issuer, clientID, clientSecret string) *TestOIDCProvider {
	p := &TestOIDCProvider{Issuer: issuer, ClientID: clientID, ClientSecret: clientSecret,
		codes: map[string]testGrant{}, tokens: map[string]map[string]any{}}
	p.Rotate()
	return p
}

// Rotate replaces its signing key with a new one, under a new kid, and
// publishes only the new one.
func (p *TestOIDCProvider) Rotate() {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		panic(err)
	}
	kid, _ := randomString()
	p.mu.Lock()
	defer p.mu.Unlock()
	p.key, p.kid = key, kid[:12]
}

// Nonces are the nonces of the ID tokens it has given, oldest first.
func (p *TestOIDCProvider) Nonces() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.nonces...)
}

func (p *TestOIDCProvider) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/.well-known/openid-configuration":
		writeTestJSON(w, http.StatusOK, map[string]any{
			"issuer":                                p.Issuer,
			"authorization_endpoint":                p.Issuer + "/authorize",
			"token_endpoint":                        p.Issuer + "/token",
			"userinfo_endpoint":                     p.Issuer + "/userinfo",
			"jwks_uri":                              p.Issuer + "/jwks",
			"response_types_supported":              []string{"code"},
			"id_token_signing_alg_values_supported": []string{"ES256"},
			"code_challenge_methods_supported":      []string{"S256"},
		})
	case "/jwks":
		p.mu.Lock()
		pub := p.key.PublicKey
		kid := p.kid
		p.mu.Unlock()
		raw, _ := pub.Bytes()
		writeTestJSON(w, http.StatusOK, map[string]any{"keys": []map[string]string{{
			"kty": "EC", "crv": "P-256", "use": "sig", "alg": "ES256", "kid": kid,
			"x": base64.RawURLEncoding.EncodeToString(raw[1:33]), "y": base64.RawURLEncoding.EncodeToString(raw[33:]),
		}}})
	case "/authorize":
		if r.Method == http.MethodPost {
			p.authorize(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = testSignInForm.Execute(w, r.URL.Query())
	case "/token":
		p.token(w, r)
	case "/userinfo":
		p.mu.Lock()
		info, ok := p.tokens[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")]
		p.mu.Unlock()
		if !ok {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		writeTestJSON(w, http.StatusOK, info)
	default:
		http.NotFound(w, r)
	}
}

var testSignInForm = template.Must(template.New("").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><title>Test provider</title></head>
<body><main><h1>Sign in to the test provider</h1>
<form method="post" action="authorize">
{{range $k, $v := .}}<input type="hidden" name="{{$k}}" value="{{index $v 0}}">
{{end}}<p><label>Subject <input name="sub" required></label></p>
<p><label>Email <input name="email" type="email"></label></p>
<p><label>Name <input name="name"></label></p>
<p><label>Groups <input name="groups"></label></p>
<p><label><input type="checkbox" name="mfa" value="yes"> Signed in with a second factor</label></p>
<p><button name="action" value="allow">Sign in</button> <button name="action" value="deny" formnovalidate>Cancel</button></p>
</form></main></body></html>`))

// authorize signs in as whoever the form names, sending the browser back
// with a code, or with access_denied when they cancel.
func (p *TestOIDCProvider) authorize(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	back, err := url.Parse(r.Form.Get("redirect_uri"))
	if err != nil || back.Host == "" || r.Form.Get("client_id") != p.ClientID ||
		r.Form.Get("response_type") != "code" || r.Form.Get("code_challenge_method") != "S256" {
		http.Error(w, "bad sign-in request", http.StatusBadRequest)
		return
	}
	q := back.Query()
	q.Set("state", r.Form.Get("state"))
	if r.Form.Get("action") == "deny" {
		q.Set("error", "access_denied")
		back.RawQuery = q.Encode()
		http.Redirect(w, r, back.String(), http.StatusSeeOther)
		return
	}
	claims := map[string]any{"sub": r.Form.Get("sub"), "name": r.Form.Get("name")}
	if e := r.Form.Get("email"); e != "" {
		claims["email"], claims["email_verified"] = e, true
	}
	groups := []string{}
	for _, g := range strings.Split(r.Form.Get("groups"), ",") {
		if g = strings.TrimSpace(g); g != "" {
			groups = append(groups, g)
		}
	}
	claims["groups"] = groups
	claims["amr"] = []string{"pwd"}
	if r.Form.Get("mfa") == "yes" {
		claims["amr"] = []string{"pwd", "mfa"}
	}
	code, _ := randomString()
	p.mu.Lock()
	p.codes[code] = testGrant{clientID: p.ClientID, redirectURI: r.Form.Get("redirect_uri"),
		challenge: r.Form.Get("code_challenge"), nonce: r.Form.Get("nonce"), claims: claims}
	p.mu.Unlock()
	q.Set("code", code)
	back.RawQuery = q.Encode()
	http.Redirect(w, r, back.String(), http.StatusSeeOther)
}

// token trades a code for an ID token and an access token. Groups are left
// to userinfo, as many providers do.
func (p *TestOIDCProvider) token(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	id, secret, basic := r.BasicAuth()
	if basic {
		id, _ = url.QueryUnescape(id)
		secret, _ = url.QueryUnescape(secret)
	} else {
		id, secret = r.Form.Get("client_id"), r.Form.Get("client_secret")
	}
	if id != p.ClientID || (p.ClientSecret != "" && subtle.ConstantTimeCompare([]byte(secret), []byte(p.ClientSecret)) != 1) {
		writeTestJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid_client"})
		return
	}
	p.mu.Lock()
	grant, ok := p.codes[r.Form.Get("code")]
	delete(p.codes, r.Form.Get("code"))
	p.mu.Unlock()
	sum := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
	if !ok || r.Form.Get("grant_type") != "authorization_code" || grant.redirectURI != r.Form.Get("redirect_uri") ||
		base64.RawURLEncoding.EncodeToString(sum[:]) != grant.challenge {
		writeTestJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_grant"})
		return
	}
	now := time.Now()
	claims := map[string]any{"iss": p.Issuer, "aud": p.ClientID, "iat": now.Unix(), "exp": now.Add(5 * time.Minute).Unix(), "nonce": grant.nonce}
	for k, v := range grant.claims {
		if k != "groups" {
			claims[k] = v
		}
	}
	access, _ := randomString()
	p.mu.Lock()
	p.tokens[access] = grant.claims
	p.nonces = append(p.nonces, grant.nonce)
	p.mu.Unlock()
	writeTestJSON(w, http.StatusOK, map[string]any{"id_token": p.sign(claims), "access_token": access, "token_type": "Bearer", "expires_in": 300})
}

// sign makes a compact JWS of claims, ES256.
func (p *TestOIDCProvider) sign(claims map[string]any) string {
	p.mu.Lock()
	key, kid := p.key, p.kid
	p.mu.Unlock()
	header := map[string]any{"alg": "ES256", "typ": "JWT", "kid": kid}
	if p.Tamper != nil {
		p.Tamper(header, claims)
	}
	if p.BreakSignature {
		key, _ = ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	}
	h, _ := json.Marshal(header)
	c, _ := json.Marshal(claims)
	signed := base64.RawURLEncoding.EncodeToString(h) + "." + base64.RawURLEncoding.EncodeToString(c)
	digest := sha256.Sum256([]byte(signed))
	r, s, err := ecdsa.Sign(rand.Reader, key, digest[:])
	if err != nil {
		panic(err)
	}
	sig := make([]byte, 64)
	r.FillBytes(sig[:32])
	s.FillBytes(sig[32:])
	return signed + "." + base64.RawURLEncoding.EncodeToString(sig)
}

func writeTestJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
