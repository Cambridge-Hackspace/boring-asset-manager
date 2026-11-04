package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/auth"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/idp"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// Signing in through identity providers (docs/access.md#signing-in-with-an-identity-provider).
// A provider's button starts a sign-in, bound to the browser by a cookie,
// and sends it to the provider; its answer comes back to the callback,
// which matches whoever it names to an account by provider and subject,
// never by email alone, makes or links one when the provider allows it,
// applies the provider's rules to their roles, and signs them in.

// ProviderCookieName holds a sign-in sent to a provider. It is sent only
// to the providers' routes.
const ProviderCookieName = "bam_provider_sign_in"

const providerCookiePath = "/api/v1/auth/providers"

// syncConfigProviders makes the providers from the configuration those
// shown under Admin, with their roles and scopes named.
func (s *Server) syncConfigProviders() error {
	roles, err := s.store.ListRoles()
	if err != nil {
		return err
	}
	scopes, err := s.store.ListScopes()
	if err != nil {
		return err
	}
	roleIDs, scopeIDs := map[string]string{}, map[string]string{}
	for _, r := range roles {
		roleIDs[r.Name] = r.ID
	}
	for _, sc := range scopes {
		scopeIDs[sc.Name] = sc.ID
	}
	inputs := make([]store.ProviderInput, 0, len(s.cfg.Auth.Providers))
	for _, p := range s.cfg.Auth.Providers {
		in := store.ProviderInput{
			Key: p.Key, Name: p.Name, Protocol: p.Protocol, Settings: p.Settings, ButtonLabel: p.ButtonLabel, Icon: p.Icon,
			Enabled: !p.Disabled, Provision: p.Provision, AllowedDomains: p.AllowedDomains, AllowedGroups: p.AllowedGroups,
			LinkByEmail: p.LinkByEmail, TrustEmail: p.TrustEmail,
		}
		for _, name := range p.DefaultRoles {
			id, ok := roleIDs[name]
			if !ok {
				return fmt.Errorf("identity provider %q: no role is named %q", p.Key, name)
			}
			in.DefaultRoleIDs = append(in.DefaultRoleIDs, id)
		}
		for _, r := range p.Rules {
			rule := store.ProviderRule{Claim: r.Claim, Value: r.Value, RoleID: roleIDs[r.Role]}
			if rule.RoleID == "" {
				return fmt.Errorf("identity provider %q: no role is named %q", p.Key, r.Role)
			}
			for _, name := range r.Scopes {
				id, ok := scopeIDs[name]
				if !ok {
					return fmt.Errorf("identity provider %q: no scope is named %q", p.Key, name)
				}
				rule.ScopeIDs = append(rule.ScopeIDs, id)
			}
			in.Rules = append(in.Rules, rule)
		}
		inputs = append(inputs, in)
	}
	return s.store.SyncConfigProviders(inputs)
}

// providerSecrets are a provider's secrets: from the configuration for one
// set there, otherwise opened with this server's key. ok is false when
// they are sealed with another key.
func (s *Server) providerSecrets(p *store.Provider) (map[string]string, bool) {
	if p.FromConfig {
		for _, c := range s.cfg.Auth.Providers {
			if c.Key == p.Key && c.Secrets != nil {
				return c.Secrets, true
			}
		}
		return map[string]string{}, true
	}
	out := map[string]string{}
	if p.SealedSecrets == "" || p.SealedSecrets == "{}" {
		return out, true
	}
	plain, err := s.sealer.Open(p.SealedSecrets, store.ProviderPurpose(p.ID))
	if err != nil {
		return out, false
	}
	if err := json.Unmarshal(plain, &out); err != nil {
		return map[string]string{}, false
	}
	return out, true
}

// providerConfig is a provider as its protocol sees it.
func (s *Server) providerConfig(p *store.Provider, secrets map[string]string, state string) idp.Config {
	return idp.Config{
		Key: p.Key, Settings: p.Settings, Secrets: secrets, State: state,
		CallbackURL: strings.TrimRight(s.cfg.Server.BaseURL, "/") + "/api/v1/auth/providers/" + url.PathEscape(p.Key) + "/callback",
	}
}

// providerView is a provider as Admin sees it: which secrets are set, never
// their values.
type providerView struct {
	store.Provider
	SecretsSet        map[string]bool `json:"secrets_set"`
	SecretsUnreadable bool            `json:"secrets_unreadable,omitempty"`
	CallbackURL       string          `json:"callback_url"`
}

func (s *Server) viewProvider(p store.Provider) providerView {
	secrets, ok := s.providerSecrets(&p)
	set := map[string]bool{}
	for k, v := range secrets {
		set[k] = v != ""
	}
	return providerView{Provider: p, SecretsSet: set, SecretsUnreadable: !ok, CallbackURL: s.providerConfig(&p, nil, "").CallbackURL}
}

type protocolView struct {
	Name   string      `json:"name"`
	Label  string      `json:"label"`
	Fields []idp.Field `json:"fields"`
}

// GET /api/v1/auth/providers: every provider, the protocols, and whether
// passwords are on (settings.manage).
func (s *Server) handleListProviders(w http.ResponseWriter, r *http.Request) {
	providers, err := s.store.Providers()
	if err != nil {
		writeServerError(w, err, "list identity providers")
		return
	}
	views := make([]providerView, len(providers))
	for i, p := range providers {
		views[i] = s.viewProvider(p)
	}
	protocols := []protocolView{}
	for _, p := range idp.Protocols() {
		protocols = append(protocols, protocolView{Name: p.Name(), Label: p.Label(), Fields: p.Fields()})
	}
	on, err := s.store.LocalPasswords()
	if err != nil {
		writeServerError(w, err, "read the sign-in settings")
		return
	}
	// The roles and scopes rules can give, by name, for one who manages
	// settings but not users.
	type named struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	roles, scopes := []named{}, []named{}
	rs, err := s.store.ListRoles()
	if err != nil {
		writeServerError(w, err, "list roles")
		return
	}
	for _, r := range rs {
		roles = append(roles, named{r.ID, r.Name})
	}
	ss, err := s.store.ListScopes()
	if err != nil {
		writeServerError(w, err, "list scopes")
		return
	}
	for _, sc := range ss {
		scopes = append(scopes, named{sc.ID, sc.Name})
	}
	writeJSON(w, http.StatusOK, map[string]any{"providers": views, "protocols": protocols, "local_passwords": on, "secret_key": s.sealer.HasKey(),
		"roles": roles, "scopes": scopes})
}

// ProviderRequest is a provider as saved under Admin. A secret left blank
// keeps the one stored.
type ProviderRequest struct {
	Key            string               `json:"key"`
	Name           string               `json:"name"`
	Protocol       string               `json:"protocol"`
	Settings       map[string]string    `json:"settings"`
	Secrets        map[string]string    `json:"secrets"`
	ButtonLabel    string               `json:"button_label"`
	Icon           string               `json:"icon"`
	Enabled        bool                 `json:"enabled"`
	Position       int                  `json:"position"`
	Provision      bool                 `json:"provision"`
	DefaultRoleIDs []string             `json:"default_role_ids"`
	AllowedDomains []string             `json:"allowed_domains"`
	AllowedGroups  []string             `json:"allowed_groups"`
	LinkByEmail    bool                 `json:"link_by_email"`
	TrustEmail     bool                 `json:"trust_email"`
	Rules          []store.ProviderRule `json:"rules"`
}

// providerInput checks a request against its protocol, with the secrets
// stored for those left blank, and seals them.
func (s *Server) providerInput(req ProviderRequest, stored *store.Provider) (store.ProviderInput, int, error) {
	proto, ok := idp.Lookup(req.Protocol)
	if !ok {
		return store.ProviderInput{}, http.StatusBadRequest, fmt.Errorf("no sign-in protocol is called %q", req.Protocol)
	}
	secrets := map[string]string{}
	if stored != nil && stored.Protocol == req.Protocol {
		var readable bool
		if secrets, readable = s.providerSecrets(stored); !readable {
			secrets = map[string]string{}
		}
	}
	changed := false
	for _, f := range proto.Fields() {
		if !f.Secret {
			continue
		}
		if v := strings.TrimSpace(req.Secrets[f.Name]); v != "" {
			secrets[f.Name], changed = v, true
		}
	}
	// Only the protocol's own secrets are kept.
	for k := range secrets {
		keep := false
		for _, f := range proto.Fields() {
			keep = keep || (f.Secret && f.Name == k)
		}
		if !keep {
			delete(secrets, k)
			changed = true
		}
	}
	settings := map[string]string{}
	for _, f := range proto.Fields() {
		if v, ok := req.Settings[f.Name]; ok && !f.Secret {
			settings[f.Name] = strings.TrimSpace(v)
		}
	}
	if err := proto.Check(idp.Config{Key: req.Key, Settings: settings, Secrets: secrets}); err != nil {
		return store.ProviderInput{}, http.StatusBadRequest, err
	}
	in := store.ProviderInput{
		Key: req.Key, Name: req.Name, Protocol: req.Protocol, Settings: settings, ButtonLabel: req.ButtonLabel, Icon: req.Icon,
		Enabled: req.Enabled, Position: req.Position, Provision: req.Provision, DefaultRoleIDs: req.DefaultRoleIDs,
		AllowedDomains: req.AllowedDomains, AllowedGroups: req.AllowedGroups, LinkByEmail: req.LinkByEmail, TrustEmail: req.TrustEmail,
		Rules: req.Rules,
	}
	if changed || stored == nil {
		if len(secrets) > 0 && !s.sealer.HasKey() {
			return store.ProviderInput{}, http.StatusConflict, errors.New("a provider's secrets are kept sealed with [auth] secret_key, and the configuration has none: ask whoever runs BAM! to add one")
		}
		plain, _ := json.Marshal(secrets)
		in.Seal = func(id string) (string, error) {
			if len(secrets) == 0 {
				return "{}", nil
			}
			return s.sealer.Seal(plain, store.ProviderPurpose(id))
		}
	}
	return in, 0, nil
}

func writeProviderError(w http.ResponseWriter, err error, action string) {
	switch {
	case errors.Is(err, store.ErrProviderNotFound):
		writeJSONError(w, http.StatusNotFound, "Identity provider not found")
	case errors.Is(err, store.ErrProviderKeyExists):
		writeJSONError(w, http.StatusConflict, "Another identity provider has that key")
	case errors.Is(err, store.ErrProviderFromConfig):
		writeJSONError(w, http.StatusConflict, "This identity provider is set in the configuration file; change it there")
	case errors.Is(err, store.ErrInvalidProvider), errors.Is(err, store.ErrRoleNotFound), errors.Is(err, store.ErrScopeNotFound),
		errors.Is(err, store.ErrInvalidAssignment):
		writeJSONError(w, http.StatusBadRequest, err.Error())
	default:
		writeServerError(w, err, action)
	}
}

// POST /api/v1/auth/providers (settings.manage).
func (s *Server) handleCreateProvider(w http.ResponseWriter, r *http.Request) {
	var req ProviderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	in, status, err := s.providerInput(req, nil)
	if err != nil {
		writeJSONError(w, status, err.Error())
		return
	}
	p, err := s.store.CreateProvider(in)
	if err != nil {
		writeProviderError(w, err, "save the identity provider")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"provider": s.viewProvider(*p)})
}

// PUT /api/v1/auth/providers/{id} (settings.manage).
func (s *Server) handleUpdateProvider(w http.ResponseWriter, r *http.Request) {
	var req ProviderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	stored, err := s.store.GetProvider(r.PathValue("id"))
	if err != nil {
		writeProviderError(w, err, "read the identity provider")
		return
	}
	if stored.FromConfig {
		writeProviderError(w, store.ErrProviderFromConfig, "")
		return
	}
	in, status, err := s.providerInput(req, stored)
	if err != nil {
		writeJSONError(w, status, err.Error())
		return
	}
	p, err := s.store.UpdateProvider(stored.ID, in)
	if err != nil {
		writeProviderError(w, err, "save the identity provider")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"provider": s.viewProvider(*p)})
}

// DELETE /api/v1/auth/providers/{id} (settings.manage).
func (s *Server) handleDeleteProvider(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteProvider(r.PathValue("id")); err != nil {
		writeProviderError(w, err, "remove the identity provider")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// SignInSettingsRequest turns passwords off or on.
type SignInSettingsRequest struct {
	LocalPasswords *bool `json:"local_passwords"`
}

// PUT /api/v1/auth/settings (settings.manage): turns signing in with a
// password off or on. It is not turned off while no provider is enabled,
// so that the sign-in page is never left with no way in.
func (s *Server) handleSetSignInSettings(w http.ResponseWriter, r *http.Request) {
	var req SignInSettingsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.LocalPasswords == nil {
		writeJSONError(w, http.StatusBadRequest, `Send {"local_passwords": true} or false`)
		return
	}
	if !*req.LocalPasswords {
		providers, err := s.store.Providers()
		if err != nil {
			writeServerError(w, err, "list identity providers")
			return
		}
		enabled := false
		for _, p := range providers {
			enabled = enabled || p.Enabled
		}
		if !enabled {
			writeJSONError(w, http.StatusConflict, "Turn on an identity provider before turning passwords off, so that people have a way to sign in.")
			return
		}
	}
	value := "on"
	if !*req.LocalPasswords {
		value = "off"
	}
	if err := s.store.SetSetting(store.LocalPasswordsSetting, value); err != nil {
		writeServerError(w, err, "save the sign-in settings")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"local_passwords": *req.LocalPasswords})
}

type publicProvider struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Icon  string `json:"icon,omitempty"`
}

// GET /api/v1/auth/providers/public: the sign-in page's buttons, and
// whether it shows the password form.
func (s *Server) handlePublicProviders(w http.ResponseWriter, r *http.Request) {
	providers, err := s.store.Providers()
	if err != nil {
		writeServerError(w, err, "list identity providers")
		return
	}
	out := []publicProvider{}
	for _, p := range providers {
		if _, ok := idp.Lookup(p.Protocol); ok && p.Enabled {
			out = append(out, publicProvider{Key: p.Key, Label: p.ButtonLabel, Icon: p.Icon})
		}
	}
	on, err := s.store.LocalPasswords()
	if err != nil {
		writeServerError(w, err, "read the sign-in settings")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"providers": out, "local_passwords": on})
}

// localPath is next if it is a path on this server, or "/".
func localPath(next string) string {
	if !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") || strings.HasPrefix(next, "/\\") || strings.ContainsAny(next, "\r\n") {
		return "/"
	}
	return next
}

// signInPage sends the browser back to the sign-in page, saying why.
func signInPage(w http.ResponseWriter, r *http.Request, next, message string) {
	q := url.Values{}
	if message != "" {
		q.Set("error", message)
	}
	if next != "" && next != "/" {
		q.Set("next", next)
	}
	target := "/sign-in"
	if len(q) > 0 {
		target += "?" + q.Encode()
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

func (s *Server) setProviderCookie(w http.ResponseWriter, r *http.Request, token string) {
	http.SetCookie(w, &http.Cookie{
		Name: ProviderCookieName, Value: token, Path: providerCookiePath,
		MaxAge: int(store.ProviderSignInTTL.Seconds()), Expires: time.Now().Add(store.ProviderSignInTTL),
		// Lax: the provider sends the browser back by a link from its site.
		HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: isSecureRequest(r, s.cfg.Server.BaseURL),
	})
}

func (s *Server) clearProviderCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name: ProviderCookieName, Value: "", Path: providerCookiePath, MaxAge: -1, Expires: time.Unix(0, 0),
		HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: isSecureRequest(r, s.cfg.Server.BaseURL),
	})
}

// enabledProvider finds a provider people may sign in through now.
func (s *Server) enabledProvider(key string) (*store.Provider, idp.Protocol, error) {
	p, err := s.store.GetProviderByKey(key)
	if err != nil {
		return nil, nil, err
	}
	proto, ok := idp.Lookup(p.Protocol)
	if !p.Enabled || !ok {
		return nil, nil, store.ErrProviderNotFound
	}
	return p, proto, nil
}

// GET /api/v1/auth/providers/{key}/start: sends the browser to the
// provider. With link=1, for someone signed in, it links the identity that
// comes back to their account rather than signing in.
func (s *Server) handleProviderStart(w http.ResponseWriter, r *http.Request) {
	next := localPath(r.URL.Query().Get("next"))
	address := ClientAddress(r)
	if wait, err := s.store.SignInWait("", address, time.Now()); err != nil {
		writeServerError(w, err, "check the sign-in throttle")
		return
	} else if wait > 0 {
		signInPage(w, r, next, fmt.Sprintf("Too many failed sign-ins from here. Try again in %s.", wait.Round(time.Second)))
		return
	}
	p, proto, err := s.enabledProvider(r.PathValue("key"))
	if err != nil {
		signInPage(w, r, next, "That sign-in button is not available.")
		return
	}
	link := ""
	if r.URL.Query().Get("link") == "1" {
		user := CurrentUser(r.Context())
		if user == nil || CurrentToken(r.Context()) != nil {
			signInPage(w, r, "/account", "Sign in first to link an account.")
			return
		}
		link, next = user.ID, "/account"
	}
	secrets, ok := s.providerSecrets(p)
	if !ok {
		signInPage(w, r, next, "This provider cannot be used: its secrets are sealed with another key. Ask whoever manages BAM! to set them again.")
		return
	}
	state, err := auth.GenerateSessionToken()
	if err != nil {
		writeServerError(w, err, "start the sign-in")
		return
	}
	redirect, kept, err := proto.Begin(r.Context(), s.providerConfig(p, secrets, state))
	if err != nil {
		log.Printf("starting a sign-in through %s: %v", p.Key, err)
		signInPage(w, r, next, "Could not reach "+p.Name+". Try again later.")
		return
	}
	token, err := s.store.CreateProviderSignIn(store.ProviderSignIn{ProviderID: p.ID, State: kept, Next: next, LinkUserID: link})
	if err != nil {
		writeServerError(w, err, "start the sign-in")
		return
	}
	s.setProviderCookie(w, r, token)
	http.Redirect(w, r, redirect, http.StatusSeeOther)
}

// GET or POST /api/v1/auth/providers/{key}/callback: the provider's answer.
func (s *Server) handleProviderCallback(w http.ResponseWriter, r *http.Request) {
	address, now := ClientAddress(r), time.Now()
	token := ""
	if c, err := r.Cookie(ProviderCookieName); err == nil {
		token = c.Value
	}
	s.clearProviderCookie(w, r)
	// A failure counts against the address, as a wrong password does.
	fail := func(next, message string) {
		if _, err := s.store.RecordSignInFailure("", address, now); err != nil {
			log.Printf("counting a failed sign-in: %v", err)
		}
		signInPage(w, r, next, message)
	}
	if wait, err := s.store.SignInWait("", address, now); err != nil {
		writeServerError(w, err, "check the sign-in throttle")
		return
	} else if wait > 0 {
		signInPage(w, r, "", fmt.Sprintf("Too many failed sign-ins from here. Try again in %s.", wait.Round(time.Second)))
		return
	}
	pending, err := s.store.TakeProviderSignIn(token)
	if err != nil {
		if errors.Is(err, store.ErrSignInNotFound) {
			fail("", "The sign-in has expired, or was not started here. Sign in again.")
			return
		}
		writeServerError(w, err, "read the sign-in")
		return
	}
	next := pending.Next
	p, proto, err := s.enabledProvider(r.PathValue("key"))
	if err != nil || p.ID != pending.ProviderID {
		fail(next, "That sign-in button is not available.")
		return
	}
	secrets, _ := s.providerSecrets(p)
	claims, err := proto.Finish(r.Context(), s.providerConfig(p, secrets, pending.State["state"]), r, pending.State)
	if err != nil {
		if errors.Is(err, idp.ErrRefused) {
			signInPage(w, r, next, p.Name+" did not sign you in.")
			return
		}
		log.Printf("finishing a sign-in through %s: %v", p.Key, err)
		fail(next, "The answer from "+p.Name+" could not be checked. Sign in again.")
		return
	}
	if strings.TrimSpace(claims.Subject) == "" {
		fail(next, p.Name+" did not say who you are.")
		return
	}

	if pending.LinkUserID != "" {
		s.finishLinking(w, r, p, claims, pending.LinkUserID)
		return
	}
	user, message, err := s.accountFor(p, claims)
	if err != nil {
		writeServerError(w, err, "find the account")
		return
	}
	if user == nil {
		fail(next, message)
		return
	}
	if !user.IsActive || user.ArchivedAt != nil {
		fail(next, "Your account is disabled.")
		return
	}
	if err := s.applyRules(p, claims, user); err != nil {
		writeServerError(w, err, "apply the provider's roles")
		return
	}

	// A second factor is still asked for (#192 decides when a provider's
	// own suffices).
	factors, err := s.store.SealedFactors(user.ID)
	if err != nil {
		writeServerError(w, err, "read the second factors")
		return
	}
	if len(factors) > 0 {
		challenge, err := s.store.CreateSignInChallenge(user.ID)
		if err != nil {
			writeServerError(w, err, "start the second step")
			return
		}
		s.setChallengeCookie(w, r, challenge)
		q := url.Values{"factor": {"required"}}
		if next != "/" {
			q.Set("next", next)
		}
		http.Redirect(w, r, "/sign-in?"+q.Encode(), http.StatusSeeOther)
		return
	}
	sess, err := s.store.CreateSession(user.ID, DefaultSessionTTL)
	if err != nil {
		writeServerError(w, err, "create session")
		return
	}
	SetSessionCookie(w, r, sess.ID, DefaultSessionTTL, s.cfg.Server.BaseURL)
	http.Redirect(w, r, next, http.StatusSeeOther)
}

// accountFor finds the account claims name, by provider and subject, or
// links or makes one as the provider allows. Without one, it says why.
func (s *Server) accountFor(p *store.Provider, c *idp.Claims) (*models.User, string, error) {
	identity, err := s.store.IdentityBySubject(p.ID, c.Subject)
	if err == nil {
		if err := s.store.TouchIdentity(identity.ID, c.Email); err != nil {
			return nil, "", err
		}
		user, err := s.store.GetUserByID(identity.UserID)
		if errors.Is(err, store.ErrUserNotFound) {
			return nil, "Your account is disabled.", nil
		}
		return user, "", err
	}
	if !errors.Is(err, store.ErrIdentityNotFound) {
		return nil, "", err
	}

	// A provider's email counts only when it vouches for it: OIDC's
	// email_verified, or the admin's word for the provider.
	verified := c.Email != "" && (c.EmailVerified || p.TrustEmail)
	existing, err := s.store.GetUserByEmail(c.Email)
	if err != nil && !errors.Is(err, store.ErrUserNotFound) {
		return nil, "", err
	}
	if existing != nil {
		if !p.LinkByEmail || !verified {
			return nil, "An account with your email exists, but is not linked to " + p.Name + ". Sign in another way and link it under Your account, or ask whoever manages users.", nil
		}
		if _, err := s.store.LinkIdentity(existing.ID, p.ID, c.Subject, c.Email); err != nil {
			return nil, "", err
		}
		return existing, "", nil
	}

	if !p.Provision {
		return nil, "You have no account here. Ask whoever manages users to make one.", nil
	}
	if len(p.AllowedDomains) > 0 && !contains(p.AllowedDomains, c.EmailDomain()) {
		return nil, "Accounts are made only for emails at " + strings.Join(p.AllowedDomains, ", ") + ".", nil
	}
	if len(p.AllowedGroups) > 0 {
		member := false
		for _, g := range p.AllowedGroups {
			member = member || c.Has("groups", g)
		}
		if !member {
			return nil, "Accounts are made only for members of " + strings.Join(p.AllowedGroups, ", ") + ".", nil
		}
	}
	if !verified {
		return nil, p.Name + " did not vouch for your email, so no account can be made with it.", nil
	}
	user, err := s.store.CreateProvisionedUser(c.Email, c.Name, p.DefaultRoleIDs)
	if errors.Is(err, store.ErrEmailTaken) {
		return nil, "An account with your email exists. Sign in another way and link it under Your account.", nil
	}
	if errors.Is(err, store.ErrInvalidUser) {
		return nil, "No account can be made: " + err.Error() + ".", nil
	}
	if err != nil {
		return nil, "", err
	}
	if _, err := s.store.LinkIdentity(user.ID, p.ID, c.Subject, c.Email); err != nil {
		return nil, "", err
	}
	return user, "", nil
}

// applyRules gives the roles the provider's rules give for these claims,
// and takes away those its rules gave that they no longer give.
func (s *Server) applyRules(p *store.Provider, c *idp.Claims, user *models.User) error {
	wanted := []models.RoleAssignment{}
	for _, rule := range p.Rules {
		if c.Has(rule.Claim, rule.Value) {
			wanted = append(wanted, models.RoleAssignment{RoleID: rule.RoleID, ScopeIDs: rule.ScopeIDs})
		}
	}
	kept, err := s.store.ApplyProviderRoles(user.ID, p.ID, wanted)
	if len(kept) > 0 {
		log.Printf("signing %s in through %s: kept %s, which holds the last users.manage", user.Email, p.Key, strings.Join(kept, ", "))
	}
	return err
}

// finishLinking links the identity that came back to the account that
// asked, and returns to Your account.
func (s *Server) finishLinking(w http.ResponseWriter, r *http.Request, p *store.Provider, c *idp.Claims, userID string) {
	back := func(message string) {
		q := url.Values{}
		if message != "" {
			q.Set("identity_error", message)
		} else {
			q.Set("linked", p.Name)
		}
		http.Redirect(w, r, "/account?"+q.Encode(), http.StatusSeeOther)
	}
	// Linking is for the person still signed in who started it.
	if user := CurrentUser(r.Context()); user == nil || user.ID != userID {
		back("Sign in, then link the account again.")
		return
	}
	_, err := s.store.LinkIdentity(userID, p.ID, c.Subject, c.Email)
	switch {
	case errors.Is(err, store.ErrIdentityTaken):
		back("That " + p.Name + " account is linked to someone else here.")
	case err != nil:
		writeServerError(w, err, "link the identity")
	default:
		back("")
	}
}

func writeIdentityError(w http.ResponseWriter, err error, action string) {
	switch {
	case errors.Is(err, store.ErrIdentityNotFound):
		writeJSONError(w, http.StatusNotFound, "Identity not found")
	case errors.Is(err, store.ErrUserNotFound):
		writeJSONError(w, http.StatusNotFound, "User not found")
	case errors.Is(err, store.ErrProviderNotFound):
		writeJSONError(w, http.StatusNotFound, "Identity provider not found")
	case errors.Is(err, store.ErrIdentityTaken):
		writeJSONError(w, http.StatusConflict, "That identity is linked to another account")
	case errors.Is(err, store.ErrLastWayIn):
		writeJSONError(w, http.StatusConflict, "That would leave the account with no way to sign in. Link another identity, or set a password, first.")
	case errors.Is(err, store.ErrInvalidProvider):
		writeJSONError(w, http.StatusBadRequest, err.Error())
	default:
		writeServerError(w, err, action)
	}
}

// identities lists someone's identities, with who gave which of their
// roles.
func (s *Server) writeIdentities(w http.ResponseWriter, userID string) {
	identities, err := s.store.UserIdentities(userID)
	if err != nil {
		writeIdentityError(w, err, "list identities")
		return
	}
	grants, err := s.store.RoleGrants(userID)
	if err != nil {
		writeServerError(w, err, "read who gave roles")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"identities": identities, "role_grants": grants})
}

// GET /api/v1/users/{id}/identities (users.manage).
func (s *Server) handleListUserIdentities(w http.ResponseWriter, r *http.Request) {
	if _, err := s.store.GetUserByID(r.PathValue("id")); err != nil {
		writeIdentityError(w, err, "read the user")
		return
	}
	s.writeIdentities(w, r.PathValue("id"))
}

// LinkIdentityRequest links an identity by hand, naming the provider by
// ID or by key.
type LinkIdentityRequest struct {
	ProviderID  string `json:"provider_id"`
	ProviderKey string `json:"provider_key"`
	Subject     string `json:"subject"`
}

// POST /api/v1/users/{id}/identities (users.manage): links an identity by
// hand, given the provider and the subject it names them by.
func (s *Server) handleLinkUserIdentity(w http.ResponseWriter, r *http.Request) {
	var req LinkIdentityRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if req.ProviderID == "" && req.ProviderKey != "" {
		p, err := s.store.GetProviderByKey(req.ProviderKey)
		if err != nil {
			writeIdentityError(w, err, "read the identity provider")
			return
		}
		req.ProviderID = p.ID
	}
	identity, err := s.store.LinkIdentity(r.PathValue("id"), req.ProviderID, req.Subject, "")
	if err != nil {
		writeIdentityError(w, err, "link the identity")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"identity": identity})
}

func (s *Server) unlink(w http.ResponseWriter, userID, identityID string) {
	on, err := s.store.LocalPasswords()
	if err != nil {
		writeServerError(w, err, "read the sign-in settings")
		return
	}
	if err := s.store.UnlinkIdentity(userID, identityID, on); err != nil {
		writeIdentityError(w, err, "unlink the identity")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// DELETE /api/v1/users/{id}/identities/{identity} (users.manage).
func (s *Server) handleUnlinkUserIdentity(w http.ResponseWriter, r *http.Request) {
	s.unlink(w, r.PathValue("id"), r.PathValue("identity"))
}

// GET /api/v1/auth/me/identities: one's own identities.
func (s *Server) handleListOwnIdentities(w http.ResponseWriter, r *http.Request) {
	s.writeIdentities(w, CurrentUser(r.Context()).ID)
}

// DELETE /api/v1/auth/me/identities/{identity}: unlinks one of one's own.
func (s *Server) handleUnlinkOwnIdentity(w http.ResponseWriter, r *http.Request) {
	s.unlink(w, CurrentUser(r.Context()).ID, r.PathValue("identity"))
}

// manageSignIn guards the providers' administrative routes.
var manageSignIn = RequirePermission(rbac.PermSettingsManage)
