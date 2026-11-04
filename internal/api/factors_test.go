package api

import (
	"bytes"
	"context"
	"encoding/base32"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/auth"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/config"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

const testSecretKey = "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=" // 32 bytes

// factorServer is a test server with a secret key, so two-factor sign-in
// can be set up, and its admin.
func factorServer(t *testing.T) (*Server, *models.User, string) {
	t.Helper()
	cfg := config.DefaultConfig()
	testStorage(t, cfg)
	cfg.Auth.SecretKey = testSecretKey
	st := setupTestStore(t)
	password := "AdminSecret123!"
	admin, err := st.CreateAdminUser("admin@test.org", password)
	if err != nil {
		t.Fatal(err)
	}
	return NewServer(cfg, st), admin, password
}

// setUpApp sets up an authenticator app for a user, as the account page
// does, and returns its secret.
func setUpApp(t *testing.T, srv *Server, userID string) []byte {
	t.Helper()
	secret, _ := setUpAppWithCodes(t, srv, userID)
	return secret
}

// setUpAppWithCodes sets up an app, and returns its secret and the
// recovery codes shown with the first factor.
func setUpAppWithCodes(t *testing.T, srv *Server, userID string) ([]byte, []string) {
	t.Helper()
	cookie := sessionCookie(t, srv, userID)
	rec := doJSON(t, srv, cookie, "POST", "/api/v1/auth/me/factors/totp", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("starting the app: %d %s", rec.Code, rec.Body)
	}
	var started struct {
		Secret string `json:"secret"`
		URI    string `json:"uri"`
		QRSVG  string `json:"qr_svg"`
	}
	_ = json.NewDecoder(rec.Body).Decode(&started)
	if !strings.HasPrefix(started.URI, "otpauth://totp/") || !strings.HasPrefix(started.QRSVG, "<svg") {
		t.Errorf("started: %+v", started)
	}
	secret, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ReplaceAll(started.Secret, " ", ""))
	if err != nil {
		t.Fatal(err)
	}
	now := auth.TOTPStep(time.Now())
	if rec := doJSON(t, srv, cookie, "POST", "/api/v1/auth/me/factors/totp/confirm", `{"code":"`+wrongCode(secret, now)+`"}`); rec.Code != http.StatusBadRequest {
		t.Errorf("confirming with a wrong code: %d", rec.Code)
	}
	rec = doJSON(t, srv, cookie, "POST", "/api/v1/auth/me/factors/totp/confirm", `{"code":"`+auth.TOTPCode(secret, now)+`"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("confirming: %d %s", rec.Code, rec.Body)
	}
	var confirmed struct {
		RecoveryCodes []string `json:"recovery_codes"`
	}
	_ = json.NewDecoder(rec.Body).Decode(&confirmed)
	return secret, confirmed.RecoveryCodes
}

// recoveryCodesLeft is how many the account page says are left.
func recoveryCodesLeft(t *testing.T, srv *Server, cookie *http.Cookie) int {
	t.Helper()
	var resp struct {
		Left *int `json:"recovery_codes_left"`
	}
	_ = json.NewDecoder(doJSON(t, srv, cookie, "GET", "/api/v1/auth/me/factors", "").Body).Decode(&resp)
	if resp.Left == nil {
		t.Fatal("no recovery_codes_left")
	}
	return *resp.Left
}

func TestRecoveryCodesSignInOnceEach(t *testing.T) {
	srv, admin, password := factorServer(t)
	secret, codes := setUpAppWithCodes(t, srv, admin.ID)
	if len(codes) != 10 {
		t.Fatalf("with the first factor: %v", codes)
	}
	cookie := sessionCookie(t, srv, admin.ID)
	if n := recoveryCodesLeft(t, srv, cookie); n != 10 {
		t.Errorf("left at first: %d", n)
	}

	// A code, typed loosely, signs in in place of the app's.
	challenge := passwordStep(t, srv, admin.Email, password)
	rec := secondStep(t, srv, challenge, strings.ToUpper(strings.ReplaceAll(codes[0], "-", " ")))
	if rec.Code != http.StatusOK || cookieNamed(rec, SessionCookieName) == nil {
		t.Fatalf("a recovery code: %d %s", rec.Code, rec.Body)
	}
	events, _ := srv.store.ListEvents(1)
	if len(events) != 1 || events[0].Kind != models.EventAuthRecoveryCodeUsed || events[0].Payload.Account == nil || events[0].Payload.Account.ID != admin.ID {
		t.Errorf("history: %+v", events)
	}

	// Used, it is refused; another still works.
	again := passwordStep(t, srv, admin.Email, password)
	if rec := secondStep(t, srv, again, codes[0]); rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), "used already") {
		t.Errorf("a used code: %d %s", rec.Code, rec.Body)
	}
	if rec := secondStep(t, srv, again, codes[1]); rec.Code != http.StatusOK {
		t.Errorf("another code: %d %s", rec.Code, rec.Body)
	}
	if n := recoveryCodesLeft(t, srv, cookie); n != 8 {
		t.Errorf("left after two: %d", n)
	}

	// New ones take a code from the app, and replace them all.
	now := auth.TOTPStep(time.Now())
	if rec := doJSON(t, srv, cookie, "POST", "/api/v1/auth/me/factors/recovery", `{"code":"`+wrongCode(secret, now)+`"}`); rec.Code != http.StatusBadRequest {
		t.Errorf("new codes with a wrong code: %d", rec.Code)
	}
	if rec := doJSON(t, srv, cookie, "POST", "/api/v1/auth/me/factors/recovery", `{"code":"`+codes[2]+`"}`); rec.Code != http.StatusBadRequest {
		t.Errorf("new codes with a recovery code: %d", rec.Code)
	}
	rec = doJSON(t, srv, cookie, "POST", "/api/v1/auth/me/factors/recovery", `{"code":"`+auth.TOTPCode(secret, now+1)+`"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("new codes: %d %s", rec.Code, rec.Body)
	}
	var fresh struct {
		RecoveryCodes []string `json:"recovery_codes"`
	}
	_ = json.NewDecoder(rec.Body).Decode(&fresh)
	if len(fresh.RecoveryCodes) != 10 || fresh.RecoveryCodes[0] == codes[0] {
		t.Errorf("new codes: %v", fresh.RecoveryCodes)
	}
	if n := recoveryCodesLeft(t, srv, cookie); n != 10 {
		t.Errorf("left after new ones: %d", n)
	}
	third := passwordStep(t, srv, admin.Email, password)
	if rec := secondStep(t, srv, third, codes[2]); rec.Code != http.StatusUnauthorized {
		t.Errorf("an old code after new ones: %d", rec.Code)
	}
	if rec := secondStep(t, srv, third, fresh.RecoveryCodes[0]); rec.Code != http.StatusOK {
		t.Errorf("a new code: %d %s", rec.Code, rec.Body)
	}
}

func TestRecoveryCodesNeedAFactor(t *testing.T) {
	srv, admin, _ := factorServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	if rec := doJSON(t, srv, cookie, "POST", "/api/v1/auth/me/factors/recovery", `{"code":"123456"}`); rec.Code != http.StatusConflict {
		t.Errorf("without a factor: %d %s", rec.Code, rec.Body)
	}
	if n := recoveryCodesLeft(t, srv, cookie); n != 0 {
		t.Errorf("left without a factor: %d", n)
	}
}

func TestARecoveryCodeUsedEmailsItsPerson(t *testing.T) {
	cfg := config.DefaultConfig()
	testStorage(t, cfg)
	cfg.Auth.SecretKey = testSecretKey
	cfg.Server.BaseURL = "https://bam.example"
	cfg.Email = config.EmailConfig{Host: "mail.test", TLS: config.EmailSTARTTLS, From: "bam@example.org"}
	st := setupTestStore(t)
	password := "AdminSecret123!"
	admin, err := st.CreateAdminUser("admin@test.org", password)
	if err != nil {
		t.Fatal(err)
	}
	srv := NewServer(cfg, st)
	dialed := make(chan string, 1)
	srv.mailer.Dial = func(_ context.Context, _, address string) (net.Conn, error) {
		dialed <- address
		return nil, errors.New("no route to host")
	}
	_, codes := setUpAppWithCodes(t, srv, admin.ID)
	challenge := passwordStep(t, srv, admin.Email, password)
	if rec := secondStep(t, srv, challenge, codes[0]); rec.Code != http.StatusOK {
		t.Fatalf("a recovery code: %d %s", rec.Code, rec.Body)
	}
	select {
	case address := <-dialed:
		if address != "mail.test:587" {
			t.Errorf("dialed %q", address)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("no email sent")
	}

	m, err := recoveryCodeUsedMessage(admin, 9, cfg.Server.BaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.To) != 1 || m.To[0] != admin.Email || !strings.Contains(m.Text, "You have 9 left.") || !strings.Contains(m.Text, "https://bam.example/account") {
		t.Errorf("message: %+v", m)
	}
}

// wrongCode is a code that is not the secret's anywhere near step.
func wrongCode(secret []byte, step int64) string {
	for _, c := range []string{"000000", "111111", "222222", "333333"} {
		ok := true
		for s := step - 3; s <= step+3; s++ {
			if auth.TOTPCode(secret, s) == c {
				ok = false
			}
		}
		if ok {
			return c
		}
	}
	panic("no wrong code")
}

// secondStep posts a code with the sign-in's challenge cookie.
func secondStep(t *testing.T, srv *Server, challenge *http.Cookie, code string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login/factor", strings.NewReader(`{"code":"`+code+`"}`))
	req.RemoteAddr = "192.0.2.10:1"
	if challenge != nil {
		req.AddCookie(challenge)
	}
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

func cookieNamed(rec *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == name && c.MaxAge >= 0 {
			return c
		}
	}
	return nil
}

// passwordStep signs in with the password, expecting the second step.
func passwordStep(t *testing.T, srv *Server, email, password string) *http.Cookie {
	t.Helper()
	rec := signIn(t, srv, "192.0.2.10:1", "", email, password)
	if rec.Code != http.StatusOK {
		t.Fatalf("password: %d %s", rec.Code, rec.Body)
	}
	var resp struct {
		FactorRequired bool     `json:"factor_required"`
		Factors        []string `json:"factors"`
	}
	_ = json.NewDecoder(rec.Body).Decode(&resp)
	if !resp.FactorRequired || len(resp.Factors) != 1 || resp.Factors[0] != "totp" {
		t.Errorf("after the password: %+v", resp)
	}
	if cookieNamed(rec, SessionCookieName) != nil {
		t.Error("a session after the password alone")
	}
	challenge := cookieNamed(rec, ChallengeCookieName)
	if challenge == nil || !challenge.HttpOnly || challenge.SameSite != http.SameSiteStrictMode || challenge.Path != "/api/v1/auth/login" {
		t.Fatalf("challenge cookie %+v", challenge)
	}
	return challenge
}

func TestSigningInWithAnAuthenticatorApp(t *testing.T) {
	srv, admin, password := factorServer(t)
	secret := setUpApp(t, srv, admin.ID)
	now := auth.TOTPStep(time.Now())

	challenge := passwordStep(t, srv, admin.Email, password)
	if rec := secondStep(t, srv, nil, auth.TOTPCode(secret, now+1)); rec.Code != http.StatusUnauthorized {
		t.Errorf("a code with no challenge: %d", rec.Code)
	}
	if rec := secondStep(t, srv, &http.Cookie{Name: ChallengeCookieName, Value: "made-up"}, auth.TOTPCode(secret, now+1)); rec.Code != http.StatusUnauthorized {
		t.Errorf("a code with a made-up challenge: %d", rec.Code)
	}
	if rec := secondStep(t, srv, challenge, wrongCode(secret, now)); rec.Code != http.StatusUnauthorized {
		t.Errorf("a wrong code: %d", rec.Code)
	}
	// The code that confirmed the app was used already.
	if rec := secondStep(t, srv, challenge, auth.TOTPCode(secret, now)); rec.Code != http.StatusUnauthorized {
		t.Errorf("the code that confirmed the app: %d", rec.Code)
	}
	for _, off := range []int64{-2, 2} {
		if rec := secondStep(t, srv, challenge, auth.TOTPCode(secret, now+off)); rec.Code != http.StatusUnauthorized {
			t.Errorf("a code %d steps off: %d", off, rec.Code)
		}
	}
	rec := secondStep(t, srv, challenge, auth.TOTPCode(secret, now+1))
	if rec.Code != http.StatusOK {
		t.Fatalf("the next code: %d %s", rec.Code, rec.Body)
	}
	session := cookieNamed(rec, SessionCookieName)
	if session == nil {
		t.Fatal("no session after the code")
	}
	if rec := doJSON(t, srv, session, "GET", "/api/v1/auth/me", ""); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), admin.Email) {
		t.Errorf("signed in: %d %s", rec.Code, rec.Body)
	}
	// The challenge is spent, and the code with it.
	if rec := secondStep(t, srv, challenge, auth.TOTPCode(secret, now+1)); rec.Code != http.StatusUnauthorized {
		t.Errorf("the challenge again: %d", rec.Code)
	}
	again := passwordStep(t, srv, admin.Email, password)
	if rec := secondStep(t, srv, again, auth.TOTPCode(secret, now+1)); rec.Code != http.StatusUnauthorized {
		t.Errorf("a code used already: %d", rec.Code)
	}
}

func TestAnExpiredChallengeIsRefused(t *testing.T) {
	srv, admin, password := factorServer(t)
	secret := setUpApp(t, srv, admin.ID)
	challenge := passwordStep(t, srv, admin.Email, password)
	if _, err := srv.store.Exec("UPDATE sign_in_challenges SET expires_at = ?", time.Now().UTC().Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	if rec := secondStep(t, srv, challenge, auth.TOTPCode(secret, auth.TOTPStep(time.Now())+1)); rec.Code != http.StatusUnauthorized ||
		!strings.Contains(rec.Body.String(), "expired") {
		t.Errorf("an expired challenge: %d %s", rec.Code, rec.Body)
	}
}

func TestWrongCodesAreThrottledAndThePasswordDoesNotResetThem(t *testing.T) {
	srv, admin, password := factorServer(t)
	secret := setUpApp(t, srv, admin.ID)
	now := auth.TOTPStep(time.Now())
	challenge := passwordStep(t, srv, admin.Email, password)
	for i := 1; i < store.AccountFailuresAllowed; i++ {
		if rec := secondStep(t, srv, challenge, wrongCode(secret, now)); rec.Code != http.StatusUnauthorized {
			t.Fatalf("wrong code %d: %d", i, rec.Code)
		}
	}
	// The right password again does not start the count again: knowing it
	// is what the codes guard against.
	challenge = passwordStep(t, srv, admin.Email, password)
	rec := secondStep(t, srv, challenge, wrongCode(secret, now))
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("wrong code %d: %d, want 429", store.AccountFailuresAllowed, rec.Code)
	}
	if rec := secondStep(t, srv, challenge, auth.TOTPCode(secret, now+1)); rec.Code != http.StatusTooManyRequests {
		t.Errorf("the right code while held back: %d, want 429", rec.Code)
	}
	events, err := srv.store.ListEvents(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) == 0 || events[0].Kind != models.EventAuthThrottled || events[0].Payload.Throttle.User == nil {
		t.Errorf("not recorded in history: %+v", events)
	}
}

func TestTurningAnAppOff(t *testing.T) {
	srv, admin, password := factorServer(t)
	secret := setUpApp(t, srv, admin.ID)
	cookie := sessionCookie(t, srv, admin.ID)
	var list struct {
		Factors   []store.Factor `json:"factors"`
		Available bool           `json:"available"`
	}
	rec := doJSON(t, srv, cookie, "GET", "/api/v1/auth/me/factors", "")
	_ = json.NewDecoder(rec.Body).Decode(&list)
	if len(list.Factors) != 1 || !list.Available || list.Factors[0].ConfirmedAt == nil {
		t.Fatalf("factors: %+v", list)
	}
	id, now := list.Factors[0].ID, auth.TOTPStep(time.Now())
	// The admin role requires a second factor, so the last stays, and the
	// code is not taken.
	if rec := doJSON(t, srv, cookie, "DELETE", "/api/v1/auth/me/factors/"+id, `{"password":"`+password+`","code":"`+auth.TOTPCode(secret, now+1)+`"}`); rec.Code != http.StatusConflict {
		t.Fatalf("the last one, required: %d %s", rec.Code, rec.Body)
	}
	adminRole, _ := srv.store.GetRoleByName(store.AdminRoleName)
	if _, err := srv.store.SetRoleTwoFactor(adminRole.ID, false); err != nil {
		t.Fatal(err)
	}
	if rec := doJSON(t, srv, cookie, "DELETE", "/api/v1/auth/me/factors/"+id, `{"password":"wrong","code":"`+auth.TOTPCode(secret, now+1)+`"}`); rec.Code != http.StatusBadRequest {
		t.Errorf("a wrong password: %d", rec.Code)
	}
	if rec := doJSON(t, srv, cookie, "DELETE", "/api/v1/auth/me/factors/"+id, `{"password":"`+password+`","code":"`+wrongCode(secret, now)+`"}`); rec.Code != http.StatusBadRequest {
		t.Errorf("a wrong code: %d", rec.Code)
	}
	if rec := doJSON(t, srv, cookie, "DELETE", "/api/v1/auth/me/factors/"+id, `{"password":"`+password+`","code":"`+auth.TOTPCode(secret, now+1)+`"}`); rec.Code != http.StatusNoContent {
		t.Fatalf("turning it off: %d %s", rec.Code, rec.Body)
	}
	if rec := signIn(t, srv, "192.0.2.10:1", "", admin.Email, password); cookieNamed(rec, SessionCookieName) == nil {
		t.Error("the password alone does not sign in after turning the app off")
	}
}

func TestAnAdminResetsSomeonesFactors(t *testing.T) {
	srv, admin, _ := factorServer(t)
	ada, err := srv.store.CreateUser("ada@test.org", "AdaSecret123!", true)
	if err != nil {
		t.Fatal(err)
	}
	setUpApp(t, srv, ada.ID)
	adaSession := sessionCookie(t, srv, ada.ID)
	adminCookie := sessionCookie(t, srv, admin.ID)

	var users struct {
		Users []struct {
			ID        string `json:"id"`
			TwoFactor bool   `json:"two_factor"`
		} `json:"users"`
	}
	_ = json.NewDecoder(doJSON(t, srv, adminCookie, "GET", "/api/v1/users", "").Body).Decode(&users)
	for _, u := range users.Users {
		if (u.ID == ada.ID) != u.TwoFactor {
			t.Errorf("%s two_factor %v", u.ID, u.TwoFactor)
		}
	}

	if rec := doJSON(t, srv, adaSession, "DELETE", "/api/v1/users/"+admin.ID+"/factors", ""); rec.Code != http.StatusForbidden {
		t.Errorf("reset without users.manage: %d", rec.Code)
	}
	rec := doJSON(t, srv, adminCookie, "DELETE", "/api/v1/users/"+ada.ID+"/factors", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"removed":1`) {
		t.Fatalf("reset: %d %s", rec.Code, rec.Body)
	}
	if rec := doJSON(t, srv, adaSession, "GET", "/api/v1/auth/me/factors", ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("still signed in after the reset: %d", rec.Code)
	}
	if rec := signIn(t, srv, "192.0.2.10:1", "", "ada@test.org", "AdaSecret123!"); cookieNamed(rec, SessionCookieName) == nil {
		t.Error("the password alone does not sign in after the reset")
	}
	events, _ := srv.store.ListEvents(5)
	found := false
	for _, e := range events {
		if e.Kind == models.EventAuthFactorsReset && e.Payload.Account != nil && e.Payload.Account.ID == ada.ID && e.Payload.Actor != nil && e.Payload.Actor.ID == admin.ID {
			found = true
		}
	}
	if !found {
		t.Errorf("the reset is not in history: %+v", events)
	}
	if rec := doJSON(t, srv, adminCookie, "DELETE", "/api/v1/users/nobody/factors", ""); rec.Code != http.StatusNotFound {
		t.Errorf("reset of no one: %d", rec.Code)
	}
}

func TestSecretsNeedTheKey(t *testing.T) {
	srv, admin, password := factorServer(t)
	secret := setUpApp(t, srv, admin.ID)
	var sealed string
	if err := srv.store.QueryRow("SELECT secret FROM user_factors").Scan(&sealed); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(sealed, base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(secret)) || bytes.Contains([]byte(sealed), secret) {
		t.Errorf("the secret is readable in the database: %s", sealed)
	}

	// Another server on the same database, with another key, cannot check
	// it, and says so rather than counting a wrong code.
	other := *srv.cfg
	other.Auth.SecretKey = "ZmVkY2JhOTg3NjU0MzIxMGZlZGNiYTk4NzY1NDMyMTA="
	srv2 := NewServer(&other, srv.store)
	challenge := passwordStep(t, srv2, admin.Email, password)
	rec := secondStep(t, srv2, challenge, auth.TOTPCode(secret, auth.TOTPStep(time.Now())+1))
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "secret_key") {
		t.Errorf("another key: %d %s", rec.Code, rec.Body)
	}

	// With no key at all, an app cannot be set up.
	none := *srv.cfg
	none.Auth.SecretKey = ""
	srv3 := NewServer(&none, srv.store)
	if rec := doJSON(t, srv3, sessionCookie(t, srv3, admin.ID), "POST", "/api/v1/auth/me/factors/totp", ""); rec.Code != http.StatusConflict {
		t.Errorf("setting up with no key: %d", rec.Code)
	}
}

func TestTokensCannotSetUpAFactor(t *testing.T) {
	srv, admin, _ := factorServer(t)
	_, token, err := srv.store.CreatePersonalToken(admin.ID, "script", nil)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("POST", "/api/v1/auth/me/factors/totp", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("a token setting up an app: %d", rec.Code)
	}
}
