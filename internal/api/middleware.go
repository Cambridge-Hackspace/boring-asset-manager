package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

type contextKey string

const (
	userContextKey        contextKey = "bam_user"
	sessionContextKey     contextKey = "bam_session"
	permissionsContextKey contextKey = "bam_permissions"
)

// WithUser associates a user model with the context.
func WithUser(ctx context.Context, u *models.User) context.Context {
	return context.WithValue(ctx, userContextKey, u)
}

// CurrentUser extracts the authenticated user from the context, or nil.
func CurrentUser(ctx context.Context) *models.User {
	if u, ok := ctx.Value(userContextKey).(*models.User); ok {
		return u
	}
	return nil
}

// WithSession associates a session model with the context.
func WithSession(ctx context.Context, s *models.Session) context.Context {
	return context.WithValue(ctx, sessionContextKey, s)
}

// CurrentSession extracts the active session from the context, or nil.
func CurrentSession(ctx context.Context) *models.Session {
	if s, ok := ctx.Value(sessionContextKey).(*models.Session); ok {
		return s
	}
	return nil
}

// WithPermissions associates a permissions set with the context.
func WithPermissions(ctx context.Context, perms map[string]bool) context.Context {
	return context.WithValue(ctx, permissionsContextKey, perms)
}

// UserPermissions extracts the user's effective permissions map from context.
func UserPermissions(ctx context.Context) map[string]bool {
	if perms, ok := ctx.Value(permissionsContextKey).(map[string]bool); ok {
		return perms
	}
	return nil
}

// HasPermission checks if the request context contains a specific permission.
func HasPermission(ctx context.Context, permKey string) bool {
	perms := UserPermissions(ctx)
	if perms == nil {
		return false
	}
	return perms[permKey]
}

// Authenticate is an HTTP middleware that extracts and validates the session cookie,
// populating the request context with the user, session, and permissions.
func Authenticate(st *store.Store) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := GetSessionCookie(r)
			if token == "" {
				next.ServeHTTP(w, r)
				return
			}

			sess, u, err := st.GetSession(token)
			if err != nil || sess == nil || u == nil {
				// Invalid or expired session - continue as unauthenticated
				next.ServeHTTP(w, r)
				return
			}

			perms, err := st.GetUserPermissions(u.ID)
			if err != nil {
				perms = make(map[string]bool)
			}

			ctx := r.Context()
			ctx = WithUser(ctx, u)
			ctx = WithSession(ctx, sess)
			ctx = WithPermissions(ctx, perms)

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireAuth is an HTTP middleware requiring an authenticated user in context.
func RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if CurrentUser(r.Context()) == nil {
			writeJSONError(w, http.StatusUnauthorized, "Authentication required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequirePermission is an HTTP middleware requiring the user to hold a specific permission.
func RequirePermission(permKey string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if CurrentUser(r.Context()) == nil {
				writeJSONError(w, http.StatusUnauthorized, "Authentication required")
				return
			}
			if !HasPermission(r.Context(), permKey) {
				writeJSONError(w, http.StatusForbidden, "Forbidden: insufficient permissions")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RequireAnyPermission allows the request when the user holds at least one of
// the listed permissions.
func RequireAnyPermission(permKeys ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if CurrentUser(r.Context()) == nil {
				writeJSONError(w, http.StatusUnauthorized, "Authentication required")
				return
			}
			for _, p := range permKeys {
				if HasPermission(r.Context(), p) {
					next.ServeHTTP(w, r)
					return
				}
			}
			writeJSONError(w, http.StatusForbidden, "Forbidden: insufficient permissions")
		})
	}
}

// ErrorResponse represents a standardized JSON API error structure.
type ErrorResponse struct {
	Error string `json:"error"`
	// Details carries structured context, such as template validation problems.
	Details any `json:"details,omitempty"`
}

func writeJSONError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(ErrorResponse{Error: msg})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// MaxRequestBytes caps request bodies on every endpoint except image uploads
// and asset imports, which set their own, larger limits.
const MaxRequestBytes = 1 << 20

// LimitRequestBodies rejects bodies over the limit with 413 before a handler
// runs when the client declares the length, and otherwise stops reading at
// the limit so a handler's decode fails.
func LimitRequestBodies(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		limit := int64(MaxRequestBytes)
		if r.Method == http.MethodPost {
			switch r.URL.Path {
			case "/api/v1/images":
				limit = imageUploadLimit
			case "/api/v1/assets/import", "/api/v1/assets/import/preview":
				limit = importBodyLimit
			}
		}
		if r.ContentLength > limit {
			writeJSONError(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("Request bodies are limited to %d bytes", limit))
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, limit)
		next.ServeHTTP(w, r)
	})
}
