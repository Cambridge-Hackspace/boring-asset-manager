package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/config"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// setupTestStore opens a migrated, seeded store: in-memory SQLite by default, or an
// ephemeral PostgreSQL/MariaDB namespace when BAM_TEST_DIALECT/BAM_TEST_DSN are set.
func setupTestStore(t *testing.T) *store.Store {
	t.Helper()

	var st *store.Store
	if dialect := os.Getenv(store.TestDialectEnv); dialect != "" && dialect != "sqlite" {
		ephemeral, cleanup, err := store.OpenEphemeral(dialect, os.Getenv(store.TestDSNEnv))
		if err != nil {
			t.Fatalf("opening ephemeral %s store: %v", dialect, err)
		}
		t.Cleanup(cleanup)
		st = ephemeral
	} else {
		opened, err := store.Open(&config.DatabaseConfig{Type: "sqlite", DSN: ":memory:"})
		if err != nil {
			t.Fatalf("opening test store: %v", err)
		}
		t.Cleanup(func() { _ = opened.Close() })
		st = opened
	}

	if err := st.Migrate(); err != nil {
		t.Fatalf("migrating test store: %v", err)
	}
	if err := st.SeedDefaultPermissionsAndRoles(); err != nil {
		t.Fatalf("seeding permissions: %v", err)
	}
	return st
}

func TestCookieHelpers(t *testing.T) {
	// 1. Plain HTTP request & baseURL
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://localhost:8080/test", nil)
	SetSessionCookie(rec, req, "test-token-123", time.Hour, "http://localhost:8080")

	cookies := rec.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("expected 1 cookie, got %d", len(cookies))
	}
	c := cookies[0]
	if c.Name != SessionCookieName || c.Value != "test-token-123" {
		t.Errorf("cookie mismatch: %s=%s", c.Name, c.Value)
	}
	if !c.HttpOnly {
		t.Errorf("expected HttpOnly to be true")
	}
	if c.SameSite != http.SameSiteLaxMode {
		t.Errorf("expected SameSite Lax, got %v", c.SameSite)
	}
	if c.Secure {
		t.Errorf("expected Secure=false for plain HTTP")
	}

	// 2. HTTPS baseURL
	rec = httptest.NewRecorder()
	SetSessionCookie(rec, req, "test-token-456", time.Hour, "https://bam.example.org")
	cookies = rec.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].Secure {
		t.Errorf("expected Secure=true for https baseURL")
	}

	// 3. Clear cookie
	rec = httptest.NewRecorder()
	ClearSessionCookie(rec, req, "http://localhost:8080")
	cookies = rec.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("expected 1 cookie, got %d", len(cookies))
	}
	if cookies[0].MaxAge != -1 || cookies[0].Value != "" {
		t.Errorf("expected cookie cleared with MaxAge -1, got MaxAge=%d, Value=%q", cookies[0].MaxAge, cookies[0].Value)
	}

	// 4. GetSessionCookie
	req.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "incoming-token"})
	if token := GetSessionCookie(req); token != "incoming-token" {
		t.Errorf("expected incoming-token, got %s", token)
	}
}

func TestAuthenticateAndContext(t *testing.T) {
	st := setupTestStore(t)

	admin, err := st.CreateAdminUser("admin@example.org", "SuperSecretPass123!")
	if err != nil {
		t.Fatalf("creating admin: %v", err)
	}

	sess, err := st.CreateSession(admin.ID, time.Hour)
	if err != nil {
		t.Fatalf("creating session: %v", err)
	}

	handler := Authenticate(st)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := CurrentUser(r.Context())
		if user == nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if !HasPermission(r.Context(), rbac.PermUsersManage) {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(user.Email))
	}))

	// Case 1: Anonymous request
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected status 404 for unauthenticated, got %d", rec.Code)
	}

	// Case 2: Authenticated request with valid session cookie
	req = httptest.NewRequest(http.MethodGet, "/test", nil)
	req.AddCookie(&http.Cookie{Name: SessionCookieName, Value: sess.ID})
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
	if rec.Body.String() != "admin@example.org" {
		t.Errorf("expected admin email, got %s", rec.Body.String())
	}

	// Case 3: Invalid session cookie
	req = httptest.NewRequest(http.MethodGet, "/test", nil)
	req.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "invalid-token"})
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected status 404 for invalid token, got %d", rec.Code)
	}
}

func TestRequireAuthMiddleware(t *testing.T) {
	st := setupTestStore(t)
	dummyHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	protected := Authenticate(st)(RequireAuth(dummyHandler))

	// Request without session
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	rec := httptest.NewRecorder()
	protected.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized, got %d", rec.Code)
	}

	var errResp ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&errResp); err != nil {
		t.Fatalf("decoding error response: %v", err)
	}
	if errResp.Error == "" {
		t.Errorf("expected error message in response")
	}

	// Request with valid session
	user, err := st.CreateUser("user@example.org", "Pass12345!", true)
	if err != nil {
		t.Fatalf("creating user: %v", err)
	}
	sess, err := st.CreateSession(user.ID, time.Hour)
	if err != nil {
		t.Fatalf("creating session: %v", err)
	}

	req = httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.AddCookie(&http.Cookie{Name: SessionCookieName, Value: sess.ID})
	rec = httptest.NewRecorder()
	protected.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", rec.Code)
	}
}

func TestRequirePermissionMiddleware(t *testing.T) {
	st := setupTestStore(t)
	dummyHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	guarded := Authenticate(st)(RequirePermission(rbac.PermUsersManage)(dummyHandler))

	// 1. Unauthenticated -> 401
	req := httptest.NewRequest(http.MethodGet, "/users", nil)
	rec := httptest.NewRecorder()
	guarded.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}

	// 2. Regular user (no users:manage permission) -> 403 Forbidden
	regUser, err := st.CreateUser("viewer@example.org", "Pass12345!", true)
	if err != nil {
		t.Fatalf("creating regular user: %v", err)
	}
	regSess, err := st.CreateSession(regUser.ID, time.Hour)
	if err != nil {
		t.Fatalf("creating session: %v", err)
	}

	req = httptest.NewRequest(http.MethodGet, "/users", nil)
	req.AddCookie(&http.Cookie{Name: SessionCookieName, Value: regSess.ID})
	rec = httptest.NewRecorder()
	guarded.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 Forbidden, got %d", rec.Code)
	}

	var errResp ErrorResponse
	_ = json.NewDecoder(rec.Body).Decode(&errResp)
	if errResp.Error == "" {
		t.Errorf("expected error string in 403 response")
	}

	// 3. Admin user (holds users:manage) -> 200 OK
	admin, err := st.CreateAdminUser("admin2@example.org", "Pass123456!")
	if err != nil {
		t.Fatalf("creating admin: %v", err)
	}
	adminSess, err := st.CreateSession(admin.ID, time.Hour)
	if err != nil {
		t.Fatalf("creating admin session: %v", err)
	}

	req = httptest.NewRequest(http.MethodGet, "/users", nil)
	req.AddCookie(&http.Cookie{Name: SessionCookieName, Value: adminSess.ID})
	rec = httptest.NewRecorder()
	guarded.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", rec.Code)
	}
}
