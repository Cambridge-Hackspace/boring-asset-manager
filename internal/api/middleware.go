package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/textcheck"
)

type contextKey string

const (
	userContextKey        contextKey = "bam_user"
	sessionContextKey     contextKey = "bam_session"
	permissionsContextKey contextKey = "bam_permissions"
	tokenContextKey       contextKey = "bam_token"
)

// WithToken associates the API token a request authenticated with.
func WithToken(ctx context.Context, t *models.APIToken) context.Context {
	return context.WithValue(ctx, tokenContextKey, t)
}

// CurrentToken extracts the API token from the context, or nil when the
// request did not use one.
func CurrentToken(ctx context.Context) *models.APIToken {
	if t, ok := ctx.Value(tokenContextKey).(*models.APIToken); ok {
		return t
	}
	return nil
}

// authenticated reports whether a user or an API token made the request.
func authenticated(ctx context.Context) bool {
	return CurrentUser(ctx) != nil || CurrentToken(ctx) != nil
}

// bearerToken returns the token of an "Authorization: Bearer" header.
func bearerToken(r *http.Request) (string, bool) {
	scheme, token, ok := strings.Cut(r.Header.Get("Authorization"), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return "", false
	}
	return strings.TrimSpace(token), true
}

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

// HasPermission reports whether the request holds a permission: in every
// scope, or, for an asset permission, in at least one. Handlers acting on an
// asset check it with Allows too.
func HasPermission(ctx context.Context, permKey string) bool {
	return rbac.HasPermission(UserPermissions(ctx), permKey)
}

// Allows reports whether the request holds a permission for an asset in
// these scopes.
func Allows(ctx context.Context, permKey string, scopes []string) bool {
	return rbac.Allows(UserPermissions(ctx), permKey, scopes)
}

// authenticatedKey marks a request made in process that already carries
// its caller, for Authenticate to take as it is.
type authenticatedKey struct{}

// authenticatedAs carries from's caller, token or user, and permissions
// onto ctx, for a request made in process on their behalf.
func authenticatedAs(ctx, from context.Context) context.Context {
	ctx = WithPermissions(ctx, UserPermissions(from))
	if t := CurrentToken(from); t != nil {
		ctx = WithToken(ctx, t)
	}
	if u := CurrentUser(from); u != nil {
		ctx = WithUser(ctx, u)
	}
	return context.WithValue(ctx, authenticatedKey{}, true)
}

// Authenticate is an HTTP middleware that extracts and validates an API token
// (Authorization: Bearer) or else the session cookie, populating the request
// context with the token, or the user and session, and their permissions.
func Authenticate(st *store.Store) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Context().Value(authenticatedKey{}) != nil {
				next.ServeHTTP(w, r)
				return
			}
			// Everyone holds the public role, signed in or not; signed out,
			// only where OpenToPublic lets it count.
			public, err := st.PublicPermissions()
			if err != nil {
				writeServerError(w, err, "read the public role")
				return
			}
			signedOut := r.WithContext(context.WithValue(WithPermissions(r.Context(), map[string]bool{}), publicContextKey, public))
			if secret, ok := bearerToken(r); ok {
				t, perms, err := st.AuthenticateToken(secret)
				if errors.Is(err, store.ErrTokenNotFound) {
					// Answered at once rather than as signed out, so a
					// script learns its token no longer works.
					writeJSONError(w, http.StatusUnauthorized, "Invalid, expired or revoked API token")
					return
				}
				if err != nil {
					writeServerError(w, err, "read the API token")
					return
				}
				ctx := WithPermissions(WithToken(r.Context(), t), union(perms, public))
				// A personal token acts as its user.
				if t.UserID != "" {
					u, err := st.GetUserByID(t.UserID)
					if err != nil {
						writeServerError(w, err, "read the token's user")
						return
					}
					ctx = WithUser(ctx, u)
				}
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			token := GetSessionCookie(r)
			if token == "" {
				next.ServeHTTP(w, signedOut)
				return
			}

			sess, u, err := st.GetSession(token)
			if errors.Is(err, store.ErrSessionNotFound) {
				// An unknown or expired session: continue as unauthenticated.
				next.ServeHTTP(w, signedOut)
				return
			}
			// Otherwise the session could not be read. Answering as if
			// signed out would send the user to sign in again, and
			// answering without permissions would say they lack them.
			if err != nil {
				writeServerError(w, err, "read the session")
				return
			}
			perms, err := st.GetUserPermissions(u.ID)
			if err != nil {
				writeServerError(w, err, "read the user's permissions")
				return
			}

			ctx := r.Context()
			ctx = WithUser(ctx, u)
			ctx = WithSession(ctx, sess)
			ctx = WithPermissions(ctx, union(perms, public))

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequirePerson is an HTTP middleware requiring a person: a signed-in user,
// or a personal token acting as one. Other API tokens are refused.
func RequirePerson(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if CurrentUser(r.Context()) == nil {
			if CurrentToken(r.Context()) != nil {
				writeJSONError(w, http.StatusForbidden, "This needs a person: a signed-in session, or their personal token")
				return
			}
			writeJSONError(w, http.StatusUnauthorized, "Authentication required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequireAuth is an HTTP middleware requiring a signed-in user. API tokens,
// personal ones too, are refused: what it guards belongs to a person's
// session, such as their password, or must not be reachable from a script or
// an assistant, such as minting tokens.
func RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if CurrentToken(r.Context()) != nil {
			writeJSONError(w, http.StatusForbidden, "This needs a signed-in session; API tokens cannot use it")
			return
		}
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
			if !HasPermission(r.Context(), permKey) {
				refuse(w, r)
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
			for _, p := range permKeys {
				if HasPermission(r.Context(), p) {
					next.ServeHTTP(w, r)
					return
				}
			}
			refuse(w, r)
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
		if (r.Method == http.MethodPost || r.Method == http.MethodPut) && isIconSourcePath(r.URL.Path) {
			limit = iconBodyLimit
		} else if r.Method == http.MethodPost {
			switch r.URL.Path {
			case "/api/v1/images":
				limit = imageUploadLimit
			case "/api/v1/assets/import", "/api/v1/assets/import/preview":
				limit = importBodyLimit
			case "/api/v1/icon-library/import":
				limit = archiveBodyLimit
			default:
				if strings.HasPrefix(r.URL.Path, "/api/v1/assets/") && strings.HasSuffix(r.URL.Path, "/photos") {
					limit = photoUploadLimit
				}
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

// RefuseUnstorableText answers 400 to a request whose path, query or JSON
// body holds a NUL character or bytes that are not UTF-8. No stored text may
// contain either (README §3.5), so such a request cannot name anything that
// exists; handed on, PostgreSQL would answer a lookup with an error rather
// than "not found". Multipart bodies (uploads and imports) are left to their
// handlers, which report problems in the file itself.
func RefuseUnstorableText(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		refuse := func() {
			writeJSONError(w, http.StatusBadRequest, "The request contains a NUL character or bytes that are not UTF-8")
		}
		if unstorable(r.URL.Path) {
			refuse()
			return
		}
		for k, vs := range r.URL.Query() {
			if unstorable(k) {
				refuse()
				return
			}
			for _, v := range vs {
				if unstorable(v) {
					refuse()
					return
				}
			}
		}
		if r.Body == nil || r.Body == http.NoBody || strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/") {
			next.ServeHTTP(w, r)
			return
		}
		data, err := io.ReadAll(r.Body)
		if err != nil {
			var tooBig *http.MaxBytesError
			if errors.As(err, &tooBig) {
				writeJSONError(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("Request bodies are limited to %d bytes", tooBig.Limit))
				return
			}
			writeJSONError(w, http.StatusBadRequest, "Could not read the request body")
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(data))
		// Every string in the document, keys included. A body that is not
		// JSON stops the scan, and its handler reports it.
		dec := json.NewDecoder(bytes.NewReader(data))
		for {
			tok, err := dec.Token()
			if err != nil {
				break
			}
			if s, ok := tok.(string); ok && unstorable(s) {
				refuse()
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// unstorable reports whether s holds a NUL character or bytes that are not
// UTF-8.
func unstorable(s string) bool {
	return textcheck.Problem(s, math.MaxInt) != ""
}

// refuse answers a request without the permission it needs: signed out,
// it may be allowed once signed in (401); signed in, it is not (403).
func refuse(w http.ResponseWriter, r *http.Request) {
	if !authenticated(r.Context()) {
		writeJSONError(w, http.StatusUnauthorized, "Authentication required")
		return
	}
	writeJSONError(w, http.StatusForbidden, "Forbidden: insufficient permissions")
}

// union is the permissions of either set.
func union(a, b map[string]bool) map[string]bool {
	out := make(map[string]bool, len(a)+len(b))
	for k := range a {
		out[k] = true
	}
	for k := range b {
		out[k] = true
	}
	return out
}

// publicContextKey holds the public role's permissions for a signed-out
// request, which count only where OpenToPublic says.
const publicContextKey contextKey = "public_permissions"

// PublicPermissions are the public role's permissions, for a signed-out
// request.
func PublicPermissions(ctx context.Context) map[string]bool {
	perms, _ := ctx.Value(publicContextKey).(map[string]bool)
	return perms
}

// OpenToPublic lets signed-out visitors use what the public role allows on
// a route: looking at one asset, not the catalog of categories, fields,
// templates and reports, which only those signed in see (README §3.3).
func OpenToPublic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !authenticated(r.Context()) {
			r = r.WithContext(WithPermissions(r.Context(), PublicPermissions(r.Context())))
		}
		next.ServeHTTP(w, r)
	})
}
