package api

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/email"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// outbox catches the account emails a server sends.
type outbox struct {
	mu    sync.Mutex
	mails []email.Message
}

// catchMail has srv's account emails caught, as if email were set up, with
// links leading to https://bam.example.
func catchMail(srv *Server) *outbox {
	o := &outbox{}
	if srv.cfg.Server.BaseURL == "" {
		srv.cfg.Server.BaseURL = "https://bam.example"
	}
	srv.sendMail = func(m email.Message) {
		o.mu.Lock()
		defer o.mu.Unlock()
		o.mails = append(o.mails, m)
	}
	return o
}

// take answers the emails sent since it was last called.
func (o *outbox) take() []email.Message {
	o.mu.Lock()
	defer o.mu.Unlock()
	out := o.mails
	o.mails = nil
	return out
}

// link is the token of the link to path in the one email sent to to.
func (o *outbox) link(t *testing.T, to, path string) string {
	t.Helper()
	mails := o.take()
	if len(mails) != 1 || mails[0].To[0] != to {
		t.Fatalf("emails: %+v", mails)
	}
	m := regexp.MustCompile(regexp.QuoteMeta(path) + `\?token=([0-9a-f]{64})`).FindStringSubmatch(mails[0].Text)
	if m == nil || !strings.Contains(mails[0].HTML, m[1]) {
		t.Fatalf("no %s link in: %s", path, mails[0].Text)
	}
	return m[1]
}

func expireLinks(t *testing.T, srv *Server) {
	t.Helper()
	if _, err := srv.store.Exec("UPDATE account_links SET expires_at = ?", time.Now().UTC().Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
}

func TestInvitationsWorkOnceAndExpire(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	// Without email, nobody can be invited.
	if rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/users", `{"email":"ada@test.org","name":"Ada","invite":true}`); rec.Code != http.StatusConflict {
		t.Fatalf("without email: %d %s", rec.Code, rec.Body)
	}
	mail := catchMail(srv)
	if rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/users", `{"email":"ada@test.org","name":"Ada","invite":true,"password":"Secret123!"}`); rec.Code != http.StatusBadRequest {
		t.Errorf("both: %d", rec.Code)
	}
	rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/users", `{"email":"ada@test.org","name":"Ada","invite":true}`)
	if rec.Code != http.StatusCreated || !strings.Contains(rec.Body.String(), `"invited":true`) {
		t.Fatalf("inviting: %d %s", rec.Code, rec.Body)
	}
	token := mail.link(t, "ada@test.org", "/invite")
	ada, _ := srv.store.GetUserByEmail("ada@test.org")
	if store.HasPassword(ada) {
		t.Error("an invited account has a password")
	}

	if rec := doJSON(t, srv, nil, http.MethodGet, "/api/v1/auth/invite?token="+token, ""); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"email":"ada@test.org"`) {
		t.Errorf("reading it: %d %s", rec.Code, rec.Body)
	}
	if rec := doJSON(t, srv, nil, http.MethodPost, "/api/v1/auth/invite", `{"token":"`+token+`","password":"short"}`); rec.Code != http.StatusBadRequest {
		t.Errorf("a weak password: %d", rec.Code)
	}
	rec = doJSON(t, srv, nil, http.MethodPost, "/api/v1/auth/invite", `{"token":"`+token+`","password":"AdaSecret123!"}`)
	if rec.Code != http.StatusOK || cookieNamed(rec, SessionCookieName) == nil {
		t.Fatalf("accepting it: %d %s", rec.Code, rec.Body)
	}
	if rec := signIn(t, srv, "192.0.2.20:1", "", "ada@test.org", "AdaSecret123!"); rec.Code != http.StatusOK {
		t.Errorf("signing in with the password set: %d", rec.Code)
	}
	// Once only.
	if rec := doJSON(t, srv, nil, http.MethodPost, "/api/v1/auth/invite", `{"token":"`+token+`","password":"AdaSecret456!"}`); rec.Code != http.StatusNotFound {
		t.Errorf("again: %d", rec.Code)
	}

	// Sent again, the one before stops working; and each expires.
	if rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/users", `{"email":"bob@test.org","name":"Bob","invite":true}`); rec.Code != http.StatusCreated {
		t.Fatal(rec.Body)
	}
	first := mail.link(t, "bob@test.org", "/invite")
	bob, _ := srv.store.GetUserByEmail("bob@test.org")
	if rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/users/"+bob.ID+"/invite", ""); rec.Code != http.StatusOK {
		t.Fatalf("again: %d %s", rec.Code, rec.Body)
	}
	second := mail.link(t, "bob@test.org", "/invite")
	if rec := doJSON(t, srv, nil, http.MethodGet, "/api/v1/auth/invite?token="+first, ""); rec.Code != http.StatusNotFound {
		t.Errorf("the one before: %d", rec.Code)
	}
	expireLinks(t, srv)
	if rec := doJSON(t, srv, nil, http.MethodPost, "/api/v1/auth/invite", `{"token":"`+second+`","password":"BobSecret123!"}`); rec.Code != http.StatusNotFound {
		t.Errorf("expired: %d", rec.Code)
	}
}

func TestAnInvitationAcceptedThroughAProvider(t *testing.T) {
	srv, ts, admin := providerServer(t)
	mail := catchMail(srv)
	saveProvider(t, srv, admin, "", testProviderRequest(map[string]string{"subject": "grace-at-idp", "email": "grace@elsewhere.org"}))
	cookie := sessionCookie(t, srv, admin.ID)
	if rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/users", `{"email":"grace@hackspace.org","name":"Grace","invite":true}`); rec.Code != http.StatusCreated {
		t.Fatal(rec.Body)
	}
	token := mail.link(t, "grace@hackspace.org", "/invite")

	b := browser(t)
	resp, err := b.Get(ts.URL + "/api/v1/auth/providers/hackspace/start?invite=" + token)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.Request.URL.Path != "/" || me(t, ts, b) != "grace@hackspace.org" {
		t.Fatalf("ended at %s as %q", resp.Request.URL, me(t, ts, b))
	}
	grace, _ := srv.store.GetUserByEmail("grace@hackspace.org")
	if ids, _ := srv.store.UserIdentities(grace.ID); len(ids) != 1 || ids[0].Subject != "grace-at-idp" {
		t.Errorf("identities: %+v", ids)
	}
	// Used: it does not start another.
	b = browser(t)
	if end := signInThrough(t, ts, b, "hackspace", "/"); end.Path != "/" {
		t.Errorf("signing in again through it: %s", end)
	}
	resp, _ = browser(t).Get(ts.URL + "/api/v1/auth/providers/hackspace/start?invite=" + token)
	if resp.Body.Close(); resp.Request.URL.Path != "/sign-in" || !strings.Contains(resp.Request.URL.Query().Get("error"), "invitation has expired") {
		t.Errorf("used again: %s", resp.Request.URL)
	}
}

func TestPasswordResetByEmail(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	ada, _ := srv.store.CreateNamedUser("ada@test.org", "Ada", "AdaSecret123!", true)
	adaSession := sessionCookie(t, srv, ada.ID)
	ask := func(addr, email string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/password-reset", strings.NewReader(`{"email":"`+email+`"}`))
		req.RemoteAddr = addr
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		return rec
	}
	// Off unless turned on, and only where links can be emailed.
	if rec := ask("192.0.2.30:1", ada.Email); rec.Code != http.StatusConflict {
		t.Errorf("off: %d", rec.Code)
	}
	if rec := doJSON(t, srv, cookie, http.MethodPut, "/api/v1/auth/settings", `{"password_reset":true}`); rec.Code != http.StatusConflict {
		t.Errorf("turned on without email: %d", rec.Code)
	}
	mail := catchMail(srv)
	if rec := doJSON(t, srv, cookie, http.MethodPut, "/api/v1/auth/settings", `{"password_reset":true}`); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"password_reset":true`) {
		t.Fatalf("turning it on: %d %s", rec.Code, rec.Body)
	}
	if rec := doJSON(t, srv, nil, http.MethodGet, "/api/v1/auth/providers/public", ""); !strings.Contains(rec.Body.String(), `"password_reset":true`) {
		t.Errorf("the sign-in page is not told: %s", rec.Body)
	}

	// The same answer whether or not the email has an account.
	unknown := ask("192.0.2.31:1", "nobody@test.org")
	known := ask("192.0.2.32:1", "ADA@test.org")
	if unknown.Code != http.StatusAccepted || known.Code != http.StatusAccepted || unknown.Body.String() != known.Body.String() {
		t.Errorf("answers: %d %s / %d %s", unknown.Code, unknown.Body, known.Code, known.Body)
	}
	token := mail.link(t, ada.Email, "/reset-password")
	if rec := doJSON(t, srv, nil, http.MethodGet, "/api/v1/auth/password-reset?token="+token, ""); rec.Code != http.StatusOK {
		t.Errorf("reading it: %d", rec.Code)
	}
	if rec := doJSON(t, srv, nil, http.MethodPost, "/api/v1/auth/password-reset/confirm", `{"token":"`+token+`","password":"weak"}`); rec.Code != http.StatusBadRequest {
		t.Errorf("a weak password: %d", rec.Code)
	}
	if rec := doJSON(t, srv, nil, http.MethodPost, "/api/v1/auth/password-reset/confirm", `{"token":"`+token+`","password":"AdaNewSecret123!"}`); rec.Code != http.StatusOK {
		t.Fatalf("setting it: %d %s", rec.Code, rec.Body)
	}
	// Signed out everywhere, told by email, the new password signs in.
	if rec := doJSON(t, srv, adaSession, http.MethodGet, "/api/v1/auth/me", ""); strings.Contains(rec.Body.String(), ada.Email) {
		t.Error("still signed in")
	}
	if mails := mail.take(); len(mails) != 1 || !strings.Contains(mails[0].Subject, "password was changed") {
		t.Errorf("the notice: %+v", mails)
	}
	if rec := signIn(t, srv, "192.0.2.33:1", "", ada.Email, "AdaNewSecret123!"); rec.Code != http.StatusOK {
		t.Errorf("the new password: %d", rec.Code)
	}
	if rec := doJSON(t, srv, nil, http.MethodPost, "/api/v1/auth/password-reset/confirm", `{"token":"`+token+`","password":"AdaOther123!"}`); rec.Code != http.StatusNotFound {
		t.Errorf("again: %d", rec.Code)
	}
	ask("192.0.2.34:1", ada.Email)
	late := mail.link(t, ada.Email, "/reset-password")
	expireLinks(t, srv)
	if rec := doJSON(t, srv, nil, http.MethodPost, "/api/v1/auth/password-reset/confirm", `{"token":"`+late+`","password":"AdaOther123!"}`); rec.Code != http.StatusNotFound {
		t.Errorf("expired: %d", rec.Code)
	}

	// Held back from one address, as failed sign-ins are.
	held := false
	for i := 0; i < 30 && !held; i++ {
		held = ask("192.0.2.35:1", "someone"+string(rune('a'+i))+"@test.org").Code == http.StatusTooManyRequests
	}
	if !held {
		t.Error("never held back")
	}
	mail.take()

	// Never while passwords are off.
	if err := srv.store.SetSetting(store.LocalPasswordsSetting, "off"); err != nil {
		t.Fatal(err)
	}
	if rec := ask("192.0.2.36:1", ada.Email); rec.Code != http.StatusConflict || len(mail.take()) != 0 {
		t.Errorf("passwords off: %d", rec.Code)
	}
}

func TestASecondFactorRemovedByEmail(t *testing.T) {
	srv, admin, adminPassword := factorServer(t)
	mail := catchMail(srv)
	ada, _ := srv.store.CreateNamedUser("ada@test.org", "Ada", "AdaSecret123!", true)
	setUpApp(t, srv, ada.ID)
	setUpApp(t, srv, admin.ID)
	mail.take()
	recover := func(challenge *http.Cookie) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login/factor/recover", nil)
		req.AddCookie(challenge)
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		return rec
	}
	// Not without the password first.
	if rec := doJSON(t, srv, nil, http.MethodPost, "/api/v1/auth/login/factor/recover", ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("without the password: %d", rec.Code)
	}
	challenge := passwordStep(t, srv, ada.Email, "AdaSecret123!")
	if rec := recover(challenge); rec.Code != http.StatusForbidden {
		t.Errorf("off: %d %s", rec.Code, rec.Body)
	}
	cookie := sessionCookie(t, srv, admin.ID)
	if rec := doJSON(t, srv, cookie, http.MethodPut, "/api/v1/auth/settings", `{"factor_recovery_email":true}`); rec.Code != http.StatusOK {
		t.Fatalf("turning it on: %d %s", rec.Code, rec.Body)
	}
	if rec := recover(challenge); rec.Code != http.StatusAccepted {
		t.Fatalf("asking: %d %s", rec.Code, rec.Body)
	}
	token := mail.link(t, ada.Email, "/recover-factor")
	if rec := doJSON(t, srv, nil, http.MethodPost, "/api/v1/auth/factor-recovery", `{"token":"`+token+`"}`); rec.Code != http.StatusOK {
		t.Fatalf("removing it: %d %s", rec.Code, rec.Body)
	}
	if f, _ := srv.store.Factors(ada.ID); len(f) != 0 {
		t.Errorf("factors left: %+v", f)
	}
	events, _ := srv.store.ListEvents(1)
	if len(events) != 1 || events[0].Kind != models.EventAuthFactorsReset || events[0].Payload.Actor == nil || events[0].Payload.Actor.ID != ada.ID {
		t.Errorf("in history: %+v", events)
	}
	if mails := mail.take(); len(mails) != 1 || !strings.Contains(mails[0].Subject, "second factor was removed") {
		t.Errorf("the notice: %+v", mails)
	}
	if rec := doJSON(t, srv, nil, http.MethodPost, "/api/v1/auth/factor-recovery", `{"token":"`+token+`"}`); rec.Code != http.StatusNotFound {
		t.Errorf("again: %d", rec.Code)
	}

	// Not for a role that requires two-factor sign-in, unless allowed.
	adminChallenge := passwordStep(t, srv, admin.Email, adminPassword)
	if rec := recover(adminChallenge); rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "requires two-factor") || len(mail.take()) != 0 {
		t.Errorf("the admin: %d %s", rec.Code, rec.Body)
	}
	doJSON(t, srv, cookie, http.MethodPut, "/api/v1/auth/settings", `{"factor_recovery_email_required":true}`)
	if rec := recover(adminChallenge); rec.Code != http.StatusAccepted {
		t.Errorf("the admin, allowed: %d %s", rec.Code, rec.Body)
	}
	adminToken := mail.link(t, admin.Email, "/recover-factor")
	// Disallowed again before the link is used, it no longer works.
	doJSON(t, srv, cookie, http.MethodPut, "/api/v1/auth/settings", `{"factor_recovery_email_required":false}`)
	if rec := doJSON(t, srv, nil, http.MethodPost, "/api/v1/auth/factor-recovery", `{"token":"`+adminToken+`"}`); rec.Code != http.StatusForbidden {
		t.Errorf("disallowed since: %d", rec.Code)
	}
}

func TestChangesToAnAccountAreToldByEmail(t *testing.T) {
	srv, admin, password := factorServer(t)
	mail := catchMail(srv)
	cookie := sessionCookie(t, srv, admin.ID)
	subjects := func() []string {
		var out []string
		for _, m := range mail.take() {
			out = append(out, m.Subject)
		}
		return out
	}
	doJSON(t, srv, cookie, http.MethodPut, "/api/v1/auth/password", `{"current_password":"`+password+`","new_password":"AdminNew123!"}`)
	if got := subjects(); len(got) != 1 || got[0] != "Your BAM! password was changed" {
		t.Errorf("changing it: %v", got)
	}
	ada, _ := srv.store.CreateNamedUser("ada@test.org", "Ada", "AdaSecret123!", true)
	doJSON(t, srv, cookie, http.MethodPut, "/api/v1/users/"+ada.ID+"/password", `{"password":"AdaNew123456!"}`)
	if got := subjects(); len(got) != 1 || got[0] != "Your BAM! password was changed" {
		t.Errorf("set for them: %v", got)
	}
	setUpApp(t, srv, ada.ID)
	if got := subjects(); len(got) != 1 || got[0] != "A second factor was added to your BAM! account" {
		t.Errorf("a factor added: %v", got)
	}
	doJSON(t, srv, cookie, http.MethodDelete, "/api/v1/users/"+ada.ID+"/factors", "")
	if got := subjects(); len(got) != 1 || got[0] != "A second factor was removed from your BAM! account" {
		t.Errorf("removed for them: %v", got)
	}
}
