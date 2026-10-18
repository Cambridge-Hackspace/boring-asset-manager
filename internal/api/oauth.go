package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// OAuth 2.1, as the MCP authorization spec has it (README §6.6), so that an
// assistant signs a person in rather than being handed a token: protected
// resource and authorization server metadata, dynamic client registration,
// the authorization code flow with PKCE (S256 only) and refresh tokens. A
// grant is a personal token, acting as the person who approved it.

// mcpResource is the MCP endpoint as a protected resource.
func (s *Server) mcpResource() string {
	return strings.TrimRight(s.cfg.Server.BaseURL, "/") + "/api/v1/mcp"
}

// resourceMetadataURL is where a client reads how to sign in to MCP.
func (s *Server) resourceMetadataURL() string {
	return strings.TrimRight(s.cfg.Server.BaseURL, "/") + "/.well-known/oauth-protected-resource/api/v1/mcp"
}

// oauthRoutes serves OAuth, while MCP is turned on. These routes are the
// way in, not the API, so the MCP coverage test does not list them; the
// consent page's two API routes are listed as refused to tools.
func (s *Server) oauthRoutes(mux *http.ServeMux, handle func(string, http.Handler)) {
	on := func(h http.HandlerFunc) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !s.cfg.MCP.Enabled {
				writeJSONError(w, http.StatusNotFound, "MCP, and signing assistants in, are turned off on this server")
				return
			}
			h(w, r)
		})
	}
	// Assistants' own pages and servers call these, from any origin; none
	// reads a cookie, so allowing every origin gives nothing away.
	open := func(h http.HandlerFunc) http.Handler {
		return on(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, MCP-Protocol-Version")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			h(w, r)
		})
	}
	for _, p := range []string{"/.well-known/oauth-protected-resource", "/.well-known/oauth-protected-resource/api/v1/mcp"} {
		mux.Handle("GET "+p, open(s.handleResourceMetadata))
		mux.Handle("OPTIONS "+p, open(s.handleResourceMetadata))
	}
	mux.Handle("GET /.well-known/oauth-authorization-server", open(s.handleAuthServerMetadata))
	mux.Handle("OPTIONS /.well-known/oauth-authorization-server", open(s.handleAuthServerMetadata))
	mux.Handle("POST /oauth/register", open(s.handleRegisterClient))
	mux.Handle("OPTIONS /oauth/register", open(s.handleRegisterClient))
	mux.Handle("POST /oauth/token", open(s.handleOAuthToken))
	mux.Handle("OPTIONS /oauth/token", open(s.handleOAuthToken))
	mux.Handle("GET /oauth/authorize", on(s.handleAuthorize))
	// The consent page's: a person, signed in, approves an assistant.
	handle("GET /api/v1/oauth/clients/{id}", on(RequireAuth(http.HandlerFunc(s.handleConsentClient)).ServeHTTP))
	handle("POST /api/v1/oauth/authorize", on(RequireAuth(http.HandlerFunc(s.handleConsent)).ServeHTTP))
}

func (s *Server) handleResourceMetadata(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"resource":                 s.mcpResource(),
		"authorization_servers":    []string{strings.TrimRight(s.cfg.Server.BaseURL, "/")},
		"bearer_methods_supported": []string{"header"},
		"resource_name":            "BAM! (Boring Asset Manager)",
	})
}

func (s *Server) handleAuthServerMetadata(w http.ResponseWriter, r *http.Request) {
	base := strings.TrimRight(s.cfg.Server.BaseURL, "/")
	writeJSON(w, http.StatusOK, map[string]any{
		"issuer":                                base,
		"authorization_endpoint":                base + "/oauth/authorize",
		"token_endpoint":                        base + "/oauth/token",
		"registration_endpoint":                 base + "/oauth/register",
		"response_types_supported":              []string{"code"},
		"grant_types_supported":                 []string{"authorization_code", "refresh_token"},
		"code_challenge_methods_supported":      []string{"S256"},
		"token_endpoint_auth_methods_supported": []string{"none"},
	})
}

// oauthError answers as RFC 6749 says, for a client to read.
func oauthError(w http.ResponseWriter, status int, code, description string) {
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, status, map[string]string{"error": code, "error_description": description})
}

// registrations limits how many clients register on each server an hour,
// since anyone may.
type registrations struct {
	mu   sync.Mutex
	seen []time.Time
}

const registrationsPerHour = 30

func (l *registrations) take() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	kept := l.seen[:0]
	for _, t := range l.seen {
		if now.Sub(t) < time.Hour {
			kept = append(kept, t)
		}
	}
	l.seen = kept
	if len(l.seen) >= registrationsPerHour {
		return false
	}
	l.seen = append(l.seen, now)
	return true
}

// handleRegisterClient registers an assistant (RFC 7591): POST /oauth/register
// with {"client_name", "redirect_uris"}. Clients are public: no secret.
func (s *Server) handleRegisterClient(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ClientName   string   `json:"client_name"`
		RedirectURIs []string `json:"redirect_uris"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		oauthError(w, http.StatusBadRequest, "invalid_client_metadata", "the body must be JSON")
		return
	}
	if !s.registrations.take() {
		w.Header().Set("Retry-After", "3600")
		oauthError(w, http.StatusTooManyRequests, "invalid_client_metadata", "too many clients registered lately; try again later")
		return
	}
	c, err := s.store.RegisterOAuthClient(req.ClientName, req.RedirectURIs)
	if errors.Is(err, store.ErrInvalidOAuthClient) {
		oauthError(w, http.StatusBadRequest, "invalid_redirect_uri", err.Error())
		return
	}
	if err != nil {
		writeServerError(w, err, "register the client")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"client_id":                  c.ID,
		"client_name":                c.Name,
		"redirect_uris":              c.RedirectURIs,
		"client_id_issued_at":        c.CreatedAt.Unix(),
		"token_endpoint_auth_method": "none",
		"grant_types":                []string{"authorization_code", "refresh_token"},
		"response_types":             []string{"code"},
	})
}

// authRequest is an authorization request, as the authorization endpoint
// and the consent page carry it.
type authRequest struct {
	ClientID            string `json:"client_id"`
	RedirectURI         string `json:"redirect_uri"`
	CodeChallenge       string `json:"code_challenge"`
	CodeChallengeMethod string `json:"code_challenge_method"`
	State               string `json:"state"`
	Resource            string `json:"resource"`
}

// client checks the client and its redirect URI, which must be one it
// registered. Without both, there is nowhere safe to send an answer.
func (s *Server) client(req authRequest) (string, error) {
	c, err := s.store.GetOAuthClient(req.ClientID)
	if err != nil {
		return "", err
	}
	if !slices.Contains(c.RedirectURIs, req.RedirectURI) {
		return "", errors.New("redirect_uri is not one the client registered")
	}
	return c.Name, nil
}

// problem is what is wrong with the rest of a request, to send back to
// the client as an error.
func (s *Server) problem(req authRequest) string {
	switch {
	case req.CodeChallenge == "":
		return "code_challenge is required (PKCE)"
	case req.CodeChallengeMethod != "S256":
		return "code_challenge_method must be S256"
	case req.Resource != "" && strings.TrimRight(req.Resource, "/") != s.mcpResource():
		return "resource must be " + s.mcpResource()
	}
	return ""
}

// redirect is where an answer goes: the client's redirect URI with params.
func redirect(to string, params url.Values) string {
	u, _ := url.Parse(to)
	q := u.Query()
	for k, v := range params {
		q[k] = v
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// handleAuthorize starts a sign-in: GET /oauth/authorize. A request whose
// client and redirect URI check out is sent on to the consent page, where
// the person signs in if they have not and approves or denies it; one
// otherwise wrong goes back to the client with the error.
func (s *Server) handleAuthorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	req := authRequest{ClientID: q.Get("client_id"), RedirectURI: q.Get("redirect_uri"), CodeChallenge: q.Get("code_challenge"),
		CodeChallengeMethod: q.Get("code_challenge_method"), State: q.Get("state"), Resource: q.Get("resource")}
	if _, err := s.client(req); err != nil {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("This assistant's sign-in request cannot be used: " + err.Error() + ".\n"))
		return
	}
	if q.Get("response_type") != "code" {
		http.Redirect(w, r, redirect(req.RedirectURI, url.Values{"error": {"unsupported_response_type"}, "state": {req.State}}), http.StatusFound)
		return
	}
	if p := s.problem(req); p != "" {
		http.Redirect(w, r, redirect(req.RedirectURI, url.Values{"error": {"invalid_request"}, "error_description": {p}, "state": {req.State}}), http.StatusFound)
		return
	}
	http.Redirect(w, r, "/oauth/consent?"+q.Encode(), http.StatusFound)
}

// handleConsentClient names a client for the consent page, checking its
// redirect URI: GET /api/v1/oauth/clients/{id}?redirect_uri=
func (s *Server) handleConsentClient(w http.ResponseWriter, r *http.Request) {
	req := authRequest{ClientID: r.PathValue("id"), RedirectURI: r.URL.Query().Get("redirect_uri")}
	name, err := s.client(req)
	if errors.Is(err, store.ErrOAuthClientNotFound) {
		writeJSONError(w, http.StatusNotFound, "This assistant is not registered here")
		return
	}
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	u, _ := url.Parse(req.RedirectURI)
	writeJSON(w, http.StatusOK, map[string]any{"client": map[string]string{"id": req.ClientID, "name": name}, "redirect_host": u.Host})
}

// handleConsent records the person's answer and says where to send them:
// POST /api/v1/oauth/authorize, with the request and {"approve"}.
func (s *Server) handleConsent(w http.ResponseWriter, r *http.Request) {
	var req struct {
		authRequest
		Approve bool `json:"approve"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if _, err := s.client(req.authRequest); err != nil {
		writeJSONError(w, http.StatusBadRequest, "This assistant's sign-in request cannot be used: "+err.Error())
		return
	}
	if p := s.problem(req.authRequest); p != "" {
		writeJSON(w, http.StatusOK, map[string]string{"redirect": redirect(req.RedirectURI, url.Values{"error": {"invalid_request"}, "error_description": {p}, "state": {req.State}})})
		return
	}
	if !req.Approve {
		writeJSON(w, http.StatusOK, map[string]string{"redirect": redirect(req.RedirectURI, url.Values{"error": {"access_denied"}, "state": {req.State}})})
		return
	}
	code, err := s.store.CreateOAuthCode(req.ClientID, CurrentUser(r.Context()).ID, req.RedirectURI, req.CodeChallenge)
	if err != nil {
		writeServerError(w, err, "approve the assistant")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"redirect": redirect(req.RedirectURI, url.Values{"code": {code}, "state": {req.State}})})
}

// handleOAuthToken trades a code, or a refresh token, for a grant's
// secrets: POST /oauth/token, form-encoded (RFC 6749).
func (s *Server) handleOAuthToken(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		oauthError(w, http.StatusBadRequest, "invalid_request", "the body must be form-encoded")
		return
	}
	f := r.PostForm
	clientID := f.Get("client_id")
	if clientID == "" {
		oauthError(w, http.StatusBadRequest, "invalid_request", "client_id is required")
		return
	}
	if res := f.Get("resource"); res != "" && strings.TrimRight(res, "/") != s.mcpResource() {
		oauthError(w, http.StatusBadRequest, "invalid_target", "resource must be "+s.mcpResource())
		return
	}
	var (
		access, refresh string
		err             error
	)
	switch f.Get("grant_type") {
	case "authorization_code":
		_, access, refresh, err = s.store.RedeemOAuthCode(f.Get("code"), clientID, f.Get("redirect_uri"), f.Get("code_verifier"))
	case "refresh_token":
		_, access, refresh, err = s.store.RefreshOAuthGrant(f.Get("refresh_token"), clientID)
	default:
		oauthError(w, http.StatusBadRequest, "unsupported_grant_type", "grant_type must be authorization_code or refresh_token")
		return
	}
	if errors.Is(err, store.ErrInvalidGrant) {
		oauthError(w, http.StatusBadRequest, "invalid_grant", err.Error())
		return
	}
	if err != nil {
		writeServerError(w, err, "issue the token")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{
		"access_token":  access,
		"token_type":    "Bearer",
		"expires_in":    int(store.OAuthAccessLife.Seconds()),
		"refresh_token": refresh,
	})
}
