package api

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/config"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/webauthn"
)

const (
	passkeyHost   = "bam.example.org"
	passkeyOrigin = "https://bam.example.org"
)

// passkeyServer is a test server whose base_url passkeys are bound to, with
// no secret key, and its admin.
func passkeyServer(t *testing.T) (*Server, *models.User, string) {
	t.Helper()
	cfg := config.DefaultConfig()
	testStorage(t, cfg)
	cfg.Server.BaseURL = passkeyOrigin + "/"
	st := setupTestStore(t)
	password := "AdminSecret123!"
	admin, err := st.CreateAdminUser("admin@test.org", password)
	if err != nil {
		t.Fatal(err)
	}
	return NewServer(cfg, st), admin, password
}

// publicKeyOptions reads options for navigator.credentials, and their
// challenge.
func publicKeyOptions(t *testing.T, rec *httptest.ResponseRecorder) (map[string]any, []byte) {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("options: %d %s", rec.Code, rec.Body)
	}
	var resp struct {
		PublicKey map[string]any `json:"public_key"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	challenge, err := base64.RawURLEncoding.DecodeString(resp.PublicKey["challenge"].(string))
	if err != nil || len(challenge) != webauthn.ChallengeSize {
		t.Fatalf("challenge %v: %v", resp.PublicKey["challenge"], err)
	}
	return resp.PublicKey, challenge
}

func enc(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

// registerPasskey registers a software passkey for userID, as the account
// page does, and answers the recovery codes shown with a first factor.
func registerPasskey(t *testing.T, srv *Server, cookie *http.Cookie, a *webauthn.SoftAuthenticator, name string) []string {
	t.Helper()
	_, challenge := publicKeyOptions(t, doJSON(t, srv, cookie, "POST", "/api/v1/auth/me/factors/passkey", ""))
	cd, att := a.Create(passkeyHost, passkeyOrigin, challenge)
	body, _ := json.Marshal(map[string]string{"name": name, "client_data_json": enc(cd), "attestation_object": enc(att)})
	rec := doJSON(t, srv, cookie, "POST", "/api/v1/auth/me/factors/passkey/confirm", string(body))
	if rec.Code != http.StatusOK {
		t.Fatalf("registering %s: %d %s", name, rec.Code, rec.Body)
	}
	var resp struct {
		Factor        store.Factor `json:"factor"`
		RecoveryCodes []string     `json:"recovery_codes"`
	}
	_ = json.NewDecoder(rec.Body).Decode(&resp)
	if resp.Factor.Kind != store.FactorPasskey || resp.Factor.Name != name || resp.Factor.RPID != passkeyHost {
		t.Errorf("registered %+v", resp.Factor)
	}
	return resp.RecoveryCodes
}

// assertion is a passkey's answer, as the browser sends it.
func assertion(a *webauthn.SoftAuthenticator, origin string, challenge []byte) string {
	cd, ad, sig := a.Get(passkeyHost, origin, challenge)
	b, _ := json.Marshal(PasskeyAssertion{ID: enc(a.ID), ClientDataJSON: enc(cd), AuthenticatorData: enc(ad), Signature: enc(sig)})
	return string(b)
}

// passkeyStep asks for a passkey at the second step of a sign-in.
func passkeyStep(t *testing.T, srv *Server, challenge *http.Cookie) (map[string]any, []byte) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login/factor/passkey", nil)
	req.AddCookie(challenge)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return publicKeyOptions(t, rec)
}

// passkeySignIn posts a passkey's answer with the sign-in's challenge
// cookie.
func passkeySignIn(srv *Server, challenge *http.Cookie, answer string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login/factor", strings.NewReader(`{"passkey":`+answer+`}`))
	req.RemoteAddr = "192.0.2.10:1"
	req.AddCookie(challenge)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

// passwordStepFor signs in with the password, expecting a second step that
// offers kinds.
func passwordStepFor(t *testing.T, srv *Server, email, password string, kinds ...string) *http.Cookie {
	t.Helper()
	rec := signIn(t, srv, "192.0.2.10:1", "", email, password)
	var resp struct {
		FactorRequired bool     `json:"factor_required"`
		Factors        []string `json:"factors"`
	}
	_ = json.NewDecoder(rec.Body).Decode(&resp)
	if rec.Code != http.StatusOK || !resp.FactorRequired || strings.Join(resp.Factors, ",") != strings.Join(kinds, ",") {
		t.Fatalf("after the password: %d %+v, want %v", rec.Code, resp, kinds)
	}
	return cookieNamed(rec, ChallengeCookieName)
}

func TestRegisteringAndSigningInWithAPasskey(t *testing.T) {
	srv, admin, password := passkeyServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	key := webauthn.NewSoftAuthenticator()

	options, challenge := publicKeyOptions(t, doJSON(t, srv, cookie, "POST", "/api/v1/auth/me/factors/passkey", ""))
	rp := options["rp"].(map[string]any)
	user := options["user"].(map[string]any)
	if rp["id"] != passkeyHost || user["name"] != admin.Email || user["id"] != enc([]byte(admin.ID)) || options["attestation"] != "none" ||
		len(options["excludeCredentials"].([]any)) != 0 || len(options["pubKeyCredParams"].([]any)) != 3 {
		t.Errorf("creation options %+v", options)
	}
	cd, att := key.Create(passkeyHost, passkeyOrigin, challenge)
	body := func(name string) string {
		b, _ := json.Marshal(map[string]string{"name": name, "client_data_json": enc(cd), "attestation_object": enc(att)})
		return string(b)
	}
	if rec := doJSON(t, srv, cookie, "POST", "/api/v1/auth/me/factors/passkey/confirm", body("")); rec.Code != http.StatusBadRequest {
		t.Errorf("with no name: %d", rec.Code)
	}
	rec := doJSON(t, srv, cookie, "POST", "/api/v1/auth/me/factors/passkey/confirm", body("YubiKey"))
	if rec.Code != http.StatusOK {
		t.Fatalf("registering: %d %s", rec.Code, rec.Body)
	}
	var registered struct {
		RecoveryCodes []string `json:"recovery_codes"`
	}
	_ = json.NewDecoder(rec.Body).Decode(&registered)
	if len(registered.RecoveryCodes) != 10 {
		t.Errorf("recovery codes with the first factor: %v", registered.RecoveryCodes)
	}
	// Its challenge is spent.
	if rec := doJSON(t, srv, cookie, "POST", "/api/v1/auth/me/factors/passkey/confirm", body("Again")); rec.Code != http.StatusBadRequest {
		t.Errorf("registering with the same answer again: %d %s", rec.Code, rec.Body)
	}
	// A second is offered as excluded, and comes with no new codes.
	options, _ = publicKeyOptions(t, doJSON(t, srv, cookie, "POST", "/api/v1/auth/me/factors/passkey", ""))
	if ex := options["excludeCredentials"].([]any); len(ex) != 1 || ex[0].(map[string]any)["id"] != enc(key.ID) {
		t.Errorf("excluded %v", ex)
	}
	if codes := registerPasskey(t, srv, cookie, webauthn.NewSoftAuthenticator(), "Phone"); codes != nil {
		t.Errorf("codes with a second factor: %v", codes)
	}
	var listed struct {
		Factors  []store.Factor `json:"factors"`
		Passkeys bool           `json:"passkeys"`
	}
	_ = json.NewDecoder(doJSON(t, srv, cookie, "GET", "/api/v1/auth/me/factors", "").Body).Decode(&listed)
	if len(listed.Factors) != 2 || listed.Factors[0].Name != "YubiKey" || listed.Factors[1].Name != "Phone" || listed.Passkeys {
		t.Errorf("listed, from another host %+v", listed)
	}
	// Passkeys can be registered only from base_url's host.
	req := httptest.NewRequest(http.MethodGet, passkeyOrigin+"/api/v1/auth/me/factors", nil)
	req.AddCookie(cookie)
	here := httptest.NewRecorder()
	srv.Handler().ServeHTTP(here, req)
	if !strings.Contains(here.Body.String(), `"passkeys":true`) {
		t.Errorf("listed at base_url's host: %s", here.Body)
	}

	// Signing in.
	signInCookie := passwordStepFor(t, srv, admin.Email, password, "passkey")
	req = httptest.NewRequest(http.MethodGet, "/api/v1/auth/login/factor", nil)
	req.AddCookie(signInCookie)
	kinds := httptest.NewRecorder()
	srv.Handler().ServeHTTP(kinds, req)
	if kinds.Code != http.StatusOK || !strings.Contains(kinds.Body.String(), `"factors":["passkey"]`) {
		t.Errorf("the kinds: %d %s", kinds.Code, kinds.Body)
	}
	options, challenge = passkeyStep(t, srv, signInCookie)
	if allowed := options["allowCredentials"].([]any); len(allowed) != 2 || options["rpId"] != passkeyHost || options["userVerification"] != "preferred" {
		t.Errorf("request options %+v", options)
	}
	answer := assertion(key, passkeyOrigin, challenge)
	rec = passkeySignIn(srv, signInCookie, answer)
	if rec.Code != http.StatusOK || cookieNamed(rec, SessionCookieName) == nil {
		t.Fatalf("signing in with the passkey: %d %s", rec.Code, rec.Body)
	}

	// The same answer again, in another sign-in, answers a challenge
	// spent.
	again := passwordStepFor(t, srv, admin.Email, password, "passkey")
	passkeyStep(t, srv, again)
	if rec := passkeySignIn(srv, again, answer); rec.Code != http.StatusUnauthorized {
		t.Errorf("a replayed answer: %d %s", rec.Code, rec.Body)
	}
}

func TestAPasskeyIsRefused(t *testing.T) {
	srv, admin, password := passkeyServer(t)
	key := webauthn.NewSoftAuthenticator()
	registerPasskey(t, srv, sessionCookie(t, srv, admin.ID), key, "YubiKey")
	signInCookie := passwordStepFor(t, srv, admin.Email, password, "passkey")

	_, challenge := passkeyStep(t, srv, signInCookie)
	if rec := passkeySignIn(srv, signInCookie, assertion(key, "https://evil.example.org", challenge)); rec.Code != http.StatusUnauthorized {
		t.Errorf("from another origin: %d %s", rec.Code, rec.Body)
	}
	_, challenge = passkeyStep(t, srv, signInCookie)
	if rec := passkeySignIn(srv, signInCookie, assertion(webauthn.NewSoftAuthenticator(), passkeyOrigin, challenge)); rec.Code != http.StatusUnauthorized ||
		!strings.Contains(rec.Body.String(), "not one of yours") {
		t.Errorf("someone else's passkey: %d %s", rec.Code, rec.Body)
	}
	// A challenge made up, not sent.
	if rec := passkeySignIn(srv, signInCookie, assertion(key, passkeyOrigin, []byte("thirty-two bytes, never sent....."))); rec.Code != http.StatusUnauthorized {
		t.Errorf("an answer to a challenge not sent: %d %s", rec.Code, rec.Body)
	}
	// A counter going backward: a copy of the key.
	_, challenge = passkeyStep(t, srv, signInCookie)
	if rec := passkeySignIn(srv, signInCookie, assertion(key, passkeyOrigin, challenge)); rec.Code != http.StatusOK {
		t.Fatalf("signing in: %d %s", rec.Code, rec.Body)
	}
	key.Count = 0
	signInCookie = passwordStepFor(t, srv, admin.Email, password, "passkey")
	_, challenge = passkeyStep(t, srv, signInCookie)
	if rec := passkeySignIn(srv, signInCookie, assertion(key, passkeyOrigin, challenge)); rec.Code != http.StatusUnauthorized ||
		!strings.Contains(rec.Body.String(), "copy") {
		t.Errorf("a counter going backward: %d %s", rec.Code, rec.Body)
	}
}

func TestRemovingAPasskeyTakesACurrentFactor(t *testing.T) {
	srv, admin, password := passkeyServer(t)
	mail := catchMail(srv)
	cookie := sessionCookie(t, srv, admin.ID)
	yubikey, phone := webauthn.NewSoftAuthenticator(), webauthn.NewSoftAuthenticator()
	registerPasskey(t, srv, cookie, yubikey, "YubiKey")
	registerPasskey(t, srv, cookie, phone, "Phone")
	// Each passkey added is told of by email.
	if sent := mail.take(); len(sent) != 2 || sent[0].Subject != "A passkey was added to your BAM! account" || sent[0].To[0] != admin.Email {
		t.Errorf("emails on adding passkeys: %+v", sent)
	}
	factors, _ := srv.store.Factors(admin.ID)
	confirm := func() []byte {
		_, c := publicKeyOptions(t, doJSON(t, srv, cookie, "POST", "/api/v1/auth/me/factors/passkey/challenge", ""))
		return c
	}
	remove := func(id, password, answer string) *httptest.ResponseRecorder {
		return doJSON(t, srv, cookie, "DELETE", "/api/v1/auth/me/factors/"+id, `{"password":"`+password+`","passkey":`+answer+`}`)
	}

	if rec := remove(factors[0].ID, "wrong", assertion(phone, passkeyOrigin, confirm())); rec.Code != http.StatusBadRequest {
		t.Errorf("with a wrong password: %d", rec.Code)
	}
	// An answer to a sign-in's challenge does not confirm it is them.
	signInCookie := passwordStepFor(t, srv, admin.Email, password, "passkey")
	_, signInChallenge := passkeyStep(t, srv, signInCookie)
	if rec := remove(factors[0].ID, password, assertion(phone, passkeyOrigin, signInChallenge)); rec.Code != http.StatusBadRequest {
		t.Errorf("with a sign-in's challenge: %d %s", rec.Code, rec.Body)
	}
	// One passkey removes another.
	if rec := remove(factors[0].ID, password, assertion(phone, passkeyOrigin, confirm())); rec.Code != http.StatusNoContent {
		t.Fatalf("removing the YubiKey with the phone: %d %s", rec.Code, rec.Body)
	}
	if sent := mail.take(); len(sent) != 1 || sent[0].Subject != "A second factor was removed from your BAM! account" {
		t.Errorf("emails on removing one: %+v", sent)
	}
	// The admin role requires a second factor, so the last stays.
	if rec := remove(factors[1].ID, password, assertion(phone, passkeyOrigin, confirm())); rec.Code != http.StatusConflict {
		t.Errorf("removing the last: %d %s", rec.Code, rec.Body)
	}

	// New recovery codes take a passkey too.
	rec := doJSON(t, srv, cookie, "POST", "/api/v1/auth/me/factors/recovery", `{"passkey":`+assertion(phone, passkeyOrigin, confirm())+`}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "recovery_codes") {
		t.Errorf("new codes with a passkey: %d %s", rec.Code, rec.Body)
	}
}

func TestPasskeysAreBoundToBaseURLsHost(t *testing.T) {
	srv, admin, password := passkeyServer(t)
	registerPasskey(t, srv, sessionCookie(t, srv, admin.ID), webauthn.NewSoftAuthenticator(), "YubiKey")

	// The host changes: the passkey cannot be used, and Admin says so.
	srv.cfg.Server.BaseURL = "https://inventory.example.org"
	signInCookie := passwordStepFor(t, srv, admin.Email, password)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login/factor/passkey", nil)
	req.AddCookie(signInCookie)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("asking for a passkey for another host: %d %s", rec.Code, rec.Body)
	}
	var settings struct {
		Host      string `json:"passkey_host"`
		Elsewhere int    `json:"passkeys_elsewhere"`
	}
	_ = json.NewDecoder(doJSON(t, srv, sessionCookie(t, srv, admin.ID), "GET", "/api/v1/auth/providers", "").Body).Decode(&settings)
	if settings.Host != "inventory.example.org" || settings.Elsewhere != 1 {
		t.Errorf("Admin's sign-in settings: %+v", settings)
	}

	// Without base_url, none can be registered.
	srv.cfg.Server.BaseURL = ""
	if rec := doJSON(t, srv, sessionCookie(t, srv, admin.ID), "POST", "/api/v1/auth/me/factors/passkey", ""); rec.Code != http.StatusConflict ||
		!strings.Contains(rec.Body.String(), "base_url") {
		t.Errorf("registering without base_url: %d %s", rec.Code, rec.Body)
	}
}

func TestAPasskeyMeetsARoleRequiringTwoFactor(t *testing.T) {
	// No secret key, so no app: a passkey is the way to set one up.
	srv, admin, _ := passkeyServer(t)
	req := httptest.NewRequest(http.MethodPost, passkeyOrigin+"/api/v1/auth/login", nil)
	sess, err := srv.startSession(req, admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	if held, _ := srv.startSession(httptest.NewRequest(http.MethodPost, "http://elsewhere.example.org/api/v1/auth/login", nil), admin.ID); held.FactorSetup {
		t.Error("held to setting up a passkey from another host, where none can be")
	}
	if !sess.FactorSetup {
		t.Fatal("the admin's session is not held to setting up a factor, though a passkey can be")
	}
	cookie := &http.Cookie{Name: SessionCookieName, Value: sess.ID}
	if rec := doJSON(t, srv, cookie, "GET", "/api/v1/assets", ""); rec.Code != http.StatusForbidden {
		t.Errorf("before a factor: %d", rec.Code)
	}
	registerPasskey(t, srv, cookie, webauthn.NewSoftAuthenticator(), "YubiKey")
	if rec := doJSON(t, srv, cookie, "GET", "/api/v1/assets", ""); rec.Code != http.StatusOK {
		t.Errorf("after a passkey: %d %s", rec.Code, rec.Body)
	}
}
