package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLoginDoesNotRevealDisabledAccountWithoutPassword(t *testing.T) {
	srv, _, _ := setupTestServer(t)

	if _, err := srv.store.CreateUser("disabled@test.org", "RightPassword1!", false); err != nil {
		t.Fatalf("creating disabled user: %v", err)
	}

	login := func(password string) (int, string) {
		body, _ := json.Marshal(LoginRequest{Email: "disabled@test.org", Password: password})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(body))
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		var resp ErrorResponse
		_ = json.NewDecoder(rec.Body).Decode(&resp)
		return rec.Code, resp.Error
	}

	code, msg := login("WrongPassword1!")
	if code != http.StatusUnauthorized || msg != "Invalid email or password" {
		t.Errorf("wrong password for disabled account: got %d %q, want 401 with the generic credentials error", code, msg)
	}

	code, msg = login("RightPassword1!")
	if code != http.StatusUnauthorized || msg != "Account is disabled" {
		t.Errorf("correct password for disabled account: got %d %q, want 401 \"Account is disabled\"", code, msg)
	}
}
