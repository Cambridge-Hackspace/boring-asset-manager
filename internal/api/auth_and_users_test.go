package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/config"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
)

func setupTestServer(t *testing.T) (*Server, *models.User, string) {
	t.Helper()
	cfg := config.DefaultConfig()
	cfg.Storage.LocalPath = t.TempDir()

	st := setupTestStore(t)

	adminPassword := "AdminSecret123!"
	admin, err := st.CreateAdminUser("admin@test.org", adminPassword)
	if err != nil {
		t.Fatalf("creating test admin user: %v", err)
	}

	srv := NewServer(cfg, st)
	return srv, admin, adminPassword
}

func TestAuthLoginLogoutMeFlow(t *testing.T) {
	srv, admin, adminPassword := setupTestServer(t)

	// 1. Invalid login attempt
	badBody, _ := json.Marshal(LoginRequest{Email: admin.Email, Password: "wrong-password"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(badBody))
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for bad password, got %d", rec.Code)
	}

	// 2. Successful login
	goodBody, _ := json.Marshal(LoginRequest{Email: admin.Email, Password: adminPassword})
	req = httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(goodBody))
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on login, got %d", rec.Code)
	}

	cookies := rec.Result().Cookies()
	var sessionCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == SessionCookieName {
			sessionCookie = c
			break
		}
	}
	if sessionCookie == nil || sessionCookie.Value == "" {
		t.Fatalf("expected session cookie in response")
	}

	// 3. GET /api/v1/auth/me with session
	req = httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	req.AddCookie(sessionCookie)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on auth/me, got %d: %s", rec.Code, rec.Body.String())
	}

	var meResp struct {
		User        models.User `json:"user"`
		Permissions []string    `json:"permissions"`
		Roles       []string    `json:"roles"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&meResp); err != nil {
		t.Fatalf("decoding me response: %v", err)
	}
	if meResp.User.Email != admin.Email {
		t.Errorf("expected user email %s, got %s", admin.Email, meResp.User.Email)
	}
	if len(meResp.Permissions) == 0 {
		t.Errorf("expected admin permissions to be populated")
	}
	if len(meResp.Roles) != 1 || meResp.Roles[0] != "admin" {
		t.Errorf("the admin's roles: %v", meResp.Roles)
	}

	// 4. POST /api/v1/auth/logout
	req = httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	req.AddCookie(sessionCookie)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on logout, got %d", rec.Code)
	}

	// Verify cookie cleared
	logoutCookies := rec.Result().Cookies()
	var clearedCookie *http.Cookie
	for _, c := range logoutCookies {
		if c.Name == SessionCookieName {
			clearedCookie = c
			break
		}
	}
	if clearedCookie == nil || clearedCookie.MaxAge != -1 {
		t.Errorf("expected cookie to be cleared with MaxAge -1")
	}

	// 5. GET /api/v1/auth/me after logout answers as signed out: no user
	req = httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	req.AddCookie(sessionCookie)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"user":null`) {
		t.Fatalf("expected no user after session revoked, got %d %s", rec.Code, rec.Body)
	}
}

func TestUserAdministrationAndAntiLockout(t *testing.T) {
	srv, admin, _ := setupTestServer(t)

	// Create session for admin
	sess, err := srv.store.CreateSession(admin.ID, time.Hour)
	if err != nil {
		t.Fatalf("creating admin session: %v", err)
	}
	adminCookie := &http.Cookie{Name: SessionCookieName, Value: sess.ID}

	// 1. GET /api/v1/users
	req := httptest.NewRequest(http.MethodGet, "/api/v1/users", nil)
	req.AddCookie(adminCookie)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", rec.Code)
	}
	var usersResp struct {
		Users []models.User `json:"users"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&usersResp); err != nil {
		t.Fatalf("decoding users response: %v", err)
	}
	if len(usersResp.Users) != 1 {
		t.Fatalf("expected 1 user initially, got %d", len(usersResp.Users))
	}

	// 2. POST /api/v1/users (create regular user)
	createBody, _ := json.Marshal(CreateUserRequest{
		Email:    "regular@test.org",
		Name:     "Regular",
		Password: "UserPassword123!",
	})
	req = httptest.NewRequest(http.MethodPost, "/api/v1/users", bytes.NewReader(createBody))
	req.AddCookie(adminCookie)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d: %s", rec.Code, rec.Body.String())
	}
	var createdResp struct {
		User models.User `json:"user"`
	}
	_ = json.NewDecoder(rec.Body).Decode(&createdResp)
	regularUserID := createdResp.User.ID

	// 3. Duplicate user creation -> 409 Conflict
	req = httptest.NewRequest(http.MethodPost, "/api/v1/users", bytes.NewReader(createBody))
	req.AddCookie(adminCookie)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 Conflict on duplicate email, got %d", rec.Code)
	}

	// 4. Anti-Lockout Test: Attempt to deactivate the ONLY admin -> 400 Bad Request
	deactBody, _ := json.Marshal(UpdateUserStatusRequest{IsActive: false})
	req = httptest.NewRequest(http.MethodPatch, fmt.Sprintf("/api/v1/users/%s/status", admin.ID), bytes.NewReader(deactBody))
	req.AddCookie(adminCookie)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request due to anti-lockout protection, got %d: %s", rec.Code, rec.Body.String())
	}

	// 5. Anti-Lockout Test: Attempt to delete the ONLY admin -> 400 Bad Request
	req = httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/api/v1/users/%s", admin.ID), nil)
	req.AddCookie(adminCookie)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request due to anti-lockout protection on delete, got %d: %s", rec.Code, rec.Body.String())
	}

	// 6. Deactivate the regular user -> 200 OK
	req = httptest.NewRequest(http.MethodPatch, fmt.Sprintf("/api/v1/users/%s/status", regularUserID), bytes.NewReader(deactBody))
	req.AddCookie(adminCookie)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK deactivating regular user, got %d: %s", rec.Code, rec.Body.String())
	}

	// 7. Delete the regular user -> 200 OK
	req = httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/api/v1/users/%s", regularUserID), nil)
	req.AddCookie(adminCookie)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK deleting regular user, got %d: %s", rec.Code, rec.Body.String())
	}
}
