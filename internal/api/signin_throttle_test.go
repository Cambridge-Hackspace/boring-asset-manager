package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/auth"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/config"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// signIn posts a sign-in from remote, with an X-Forwarded-For if given.
func signIn(t *testing.T, srv *Server, remote, forwarded, email, password string) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(LoginRequest{Email: email, Password: password})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(body))
	req.RemoteAddr = remote
	if forwarded != "" {
		req.Header.Set("X-Forwarded-For", forwarded)
	}
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

func TestWrongPasswordsHoldAnAccountBackWithoutCheckingMore(t *testing.T) {
	srv, admin, password := setupTestServer(t)
	for i := 1; i < store.AccountFailuresAllowed; i++ {
		if rec := signIn(t, srv, "192.0.2.10:1", "", admin.Email, "wrong"); rec.Code != http.StatusUnauthorized {
			t.Fatalf("wrong password %d: %d, want 401", i, rec.Code)
		}
	}
	rec := signIn(t, srv, "192.0.2.10:1", "", admin.Email, "wrong")
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") != "1" {
		t.Fatalf("5th wrong password: %d, Retry-After %q; want 429 and 1", rec.Code, rec.Header().Get("Retry-After"))
	}
	var resp ErrorResponse
	_ = json.NewDecoder(rec.Body).Decode(&resp)
	if resp.Error != "Too many failed sign-ins. Try again in 1 second." {
		t.Errorf("message %q", resp.Error)
	}

	// While it waits, even the right password, from anywhere, is not checked.
	before := auth.Comparisons()
	if rec := signIn(t, srv, "198.51.100.3:1", "", admin.Email, password); rec.Code != http.StatusTooManyRequests {
		t.Errorf("right password while waiting: %d, want 429", rec.Code)
	}
	if auth.Comparisons() != before {
		t.Error("a password was checked while the account waits")
	}
}

func TestTheRightPasswordStartsTheCountAgain(t *testing.T) {
	srv, admin, password := setupTestServer(t)
	for range store.AccountFailuresAllowed - 1 {
		signIn(t, srv, "192.0.2.10:1", "", admin.Email, "wrong")
	}
	if rec := signIn(t, srv, "192.0.2.10:1", "", admin.Email, password); rec.Code != http.StatusOK {
		t.Fatalf("right password: %d", rec.Code)
	}
	for i := range store.AccountFailuresAllowed - 1 {
		if rec := signIn(t, srv, "192.0.2.10:1", "", admin.Email, "wrong"); rec.Code != http.StatusUnauthorized {
			t.Fatalf("wrong password %d after the right one: %d, want 401", i+1, rec.Code)
		}
	}
}

func TestAnEmailWithNoAccountIsAnsweredAsAWrongPassword(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	work := func(email string) (int, int64) {
		before := auth.Comparisons()
		rec := signIn(t, srv, "192.0.2.10:1", "", email, "wrong")
		return rec.Code, auth.Comparisons() - before
	}
	knownCode, knownWork := work(admin.Email)
	unknownCode, unknownWork := work("nobody@test.org")
	if knownCode != unknownCode || knownCode != http.StatusUnauthorized {
		t.Errorf("answers %d and %d, want 401 for both", knownCode, unknownCode)
	}
	if knownWork != 1 || unknownWork != 1 {
		t.Errorf("checked %d hashes for an account and %d for none, want 1 each", knownWork, unknownWork)
	}
	// And it is held back the same way.
	for range store.AccountFailuresAllowed - 2 {
		signIn(t, srv, "192.0.2.10:1", "", "nobody@test.org", "wrong")
	}
	if rec := signIn(t, srv, "192.0.2.10:1", "", "nobody@test.org", "wrong"); rec.Code != http.StatusTooManyRequests {
		t.Errorf("5th wrong password for no account: %d, want 429", rec.Code)
	}
}

// proxiedServer is a test server behind a proxy at 10.0.0.2.
func proxiedServer(t *testing.T) *Server {
	t.Helper()
	cfg := config.DefaultConfig()
	testStorage(t, cfg)
	cfg.Server.TrustedProxies = []string{"10.0.0.2"}
	return NewServer(cfg, setupTestStore(t))
}

func TestAnAddressIsHeldBackAfterTwentyWrongPasswordsBehindATrustedProxy(t *testing.T) {
	srv := proxiedServer(t)
	for i := range store.AddressFailuresAllowed - 1 {
		// Each an email of its own, so only the address counts; the client
		// claims addresses of its own to the left, which are ignored.
		rec := signIn(t, srv, "10.0.0.2:5000", fmt.Sprintf("1.2.3.%d, 203.0.113.5", i), fmt.Sprintf("u%d@test.org", i), "wrong")
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("wrong password %d: %d, want 401", i+1, rec.Code)
		}
	}
	rec := signIn(t, srv, "10.0.0.2:5000", "203.0.113.5", "last@test.org", "wrong")
	want := strconv.Itoa(int(store.ThrottleWindow.Seconds()))
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") != want {
		t.Fatalf("20th wrong password: %d, Retry-After %q; want 429 and %s", rec.Code, rec.Header().Get("Retry-After"), want)
	}
	// Another client behind the same proxy is not held back.
	if rec := signIn(t, srv, "10.0.0.2:5000", "198.51.100.8", "other@test.org", "wrong"); rec.Code != http.StatusUnauthorized {
		t.Errorf("another client behind the proxy: %d, want 401", rec.Code)
	}
}

func TestAClientNotBehindATrustedProxyCannotClaimAnotherAddress(t *testing.T) {
	srv := proxiedServer(t)
	// The same client, claiming a new address each time: all counted to it.
	for i := range store.AddressFailuresAllowed {
		signIn(t, srv, "198.51.100.66:1", fmt.Sprintf("203.0.113.%d", i), fmt.Sprintf("u%d@test.org", i), "wrong")
	}
	if rec := signIn(t, srv, "198.51.100.66:1", "203.0.113.200", "new@test.org", "wrong"); rec.Code != http.StatusTooManyRequests {
		t.Errorf("after 20 failures with forged headers: %d, want 429", rec.Code)
	}
}

func TestAdminsSeeAndClearSignInDelays(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	user, err := srv.store.CreateUser("bo@test.org", "BoPassword123!", true)
	if err != nil {
		t.Fatal(err)
	}
	sess, err := srv.store.CreateSession(admin.ID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	cookie := &http.Cookie{Name: SessionCookieName, Value: sess.ID}
	holdBack := func() {
		for range store.AccountFailuresAllowed {
			signIn(t, srv, "192.0.2.10:1", "", "bo@test.org", "wrong")
		}
	}
	delayed := func() bool {
		rec := doJSON(t, srv, cookie, http.MethodGet, "/api/v1/users", "")
		var resp struct {
			Users []struct {
				models.User
				SignInDelayedUntil *time.Time `json:"sign_in_delayed_until"`
			} `json:"users"`
		}
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatal(err)
		}
		for _, u := range resp.Users {
			if u.ID == user.ID {
				return u.SignInDelayedUntil != nil
			}
		}
		t.Fatal("bo not listed")
		return false
	}

	holdBack()
	if !delayed() {
		t.Fatal("the user list does not show bo held back")
	}
	if rec := doJSON(t, srv, cookie, http.MethodDelete, "/api/v1/users/"+user.ID+"/sign-in-delay", ""); rec.Code != http.StatusOK {
		t.Fatalf("clearing: %d %s", rec.Code, rec.Body)
	}
	if delayed() {
		t.Error("still held back after clearing")
	}
	if rec := signIn(t, srv, "192.0.2.10:1", "", "bo@test.org", "BoPassword123!"); rec.Code != http.StatusOK {
		t.Errorf("signing in after clearing: %d", rec.Code)
	}

	// Setting their password clears it too.
	holdBack()
	if rec := doJSON(t, srv, cookie, http.MethodPut, "/api/v1/users/"+user.ID+"/password", `{"password": "NewPassword123!"}`); rec.Code != http.StatusOK {
		t.Fatalf("setting the password: %d %s", rec.Code, rec.Body)
	}
	if delayed() {
		t.Error("still held back after a new password")
	}

	if rec := doJSON(t, srv, cookie, http.MethodDelete, "/api/v1/users/no-such-user/sign-in-delay", ""); rec.Code != http.StatusNotFound {
		t.Errorf("clearing for no one: %d, want 404", rec.Code)
	}
}
