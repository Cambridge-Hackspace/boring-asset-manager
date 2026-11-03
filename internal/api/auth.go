package api

import (
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/auth"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// LoginRequest defines the JSON body for authenticating a session.
type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// handleLogin authenticates user credentials, creates a session, and sets the session cookie.
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.Email == "" || req.Password == "" {
		writeJSONError(w, http.StatusBadRequest, "Email and password are required")
		return
	}

	// Too many failed passwords for this account, or from this address,
	// hold sign-ins back before any password is checked (#246).
	address := ClientAddress(r)
	wait, err := s.store.SignInWait(req.Email, address, time.Now())
	if err != nil {
		writeServerError(w, err, "check the sign-in throttle")
		return
	}
	if wait > 0 {
		writeThrottled(w, wait)
		return
	}

	user, err := s.store.GetUserByEmail(req.Email)
	if err != nil && !errors.Is(err, store.ErrUserNotFound) {
		writeServerError(w, err, "read the user")
		return
	}
	// Verify the password before revealing account state, so the disabled-account
	// message cannot be used to probe which emails exist. An email with no
	// account checks a password too, so it takes as long (#246).
	var ok bool
	if user == nil {
		ok = auth.CheckNoPassword(req.Password)
	} else {
		ok = auth.CheckPassword(req.Password, user.PasswordHash)
	}
	if !ok {
		wait, err := s.store.RecordSignInFailure(req.Email, address, time.Now())
		if err != nil {
			writeServerError(w, err, "count the failed sign-in")
			return
		}
		if wait > 0 {
			writeThrottled(w, wait)
			return
		}
		writeJSONError(w, http.StatusUnauthorized, "Invalid email or password")
		return
	}
	if err := s.store.ClearSignInFailures(req.Email); err != nil {
		writeServerError(w, err, "clear the sign-in throttle")
		return
	}

	if !user.IsActive {
		writeJSONError(w, http.StatusUnauthorized, "Account is disabled")
		return
	}

	sess, err := s.store.CreateSession(user.ID, DefaultSessionTTL)
	if err != nil {
		writeServerError(w, err, "create session")
		return
	}

	SetSessionCookie(w, r, sess.ID, DefaultSessionTTL, s.cfg.Server.BaseURL)
	writeJSON(w, http.StatusOK, map[string]any{
		"user": user,
	})
}

// handleLogout terminates the active session and clears the session cookie.
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	sess := CurrentSession(r.Context())
	if sess != nil {
		_ = s.store.DeleteSession(sess.ID)
	}

	ClearSessionCookie(w, r, s.cfg.Server.BaseURL)
	writeJSON(w, http.StatusOK, map[string]string{
		"message": "Logged out successfully",
	})
}

// handleAuthMe returns current session details and effective user permissions.
// Signed out, the user is null and the permissions are the public role's.
// A personal token is answered as its user; other API tokens are refused,
// being no one.
func (s *Server) handleAuthMe(w http.ResponseWriter, r *http.Request) {
	if CurrentToken(r.Context()) != nil && CurrentUser(r.Context()) == nil {
		writeJSONError(w, http.StatusForbidden, "This needs a person: a signed-in session, or their personal token")
		return
	}
	user := CurrentUser(r.Context())

	permsMap := UserPermissions(r.Context())
	if user == nil {
		permsMap = PublicPermissions(r.Context())
	}
	var permsList []string
	for k, v := range permsMap {
		if v {
			permsList = append(permsList, k)
		}
	}
	if permsList == nil {
		permsList = []string{}
	}
	sort.Strings(permsList)

	// The roles they hold, by name, for their account page, and the scopes
	// of those narrowed to some.
	roles, roleScopes := []string{}, map[string][]string{}
	if user != nil {
		names, err := s.store.RoleNamesOf(user.ID)
		if err != nil {
			writeServerError(w, err, "read the user's roles")
			return
		}
		if roleScopes, err = s.store.RoleScopesOf(user.ID); err != nil {
			writeServerError(w, err, "read the user's roles")
			return
		}
		roles = names
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"user":        user,
		"permissions": permsList,
		"roles":       roles,
		"role_scopes": roleScopes,
	})
}

// ChangePasswordRequest changes the signed-in user's own password.
type ChangePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

// handleChangePassword changes the signed-in user's password and ends their
// other sessions: PUT /api/v1/auth/password
func (s *Server) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	var req ChangePasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	user, sess := CurrentUser(r.Context()), CurrentSession(r.Context())
	if user == nil || sess == nil {
		writeJSONError(w, http.StatusUnauthorized, "Authentication required")
		return
	}
	err := s.store.ChangePassword(user.ID, req.CurrentPassword, req.NewPassword, sess.ID)
	switch {
	case errors.Is(err, store.ErrWrongPassword), errors.Is(err, auth.ErrPasswordPolicy):
		writeJSONError(w, http.StatusBadRequest, err.Error())
	case err != nil:
		writeServerError(w, err, "change password")
	default:
		writeJSON(w, http.StatusOK, map[string]string{"message": "Password changed"})
	}
}

// writeThrottled answers a sign-in held back for wait: 429, with
// Retry-After, saying how long in words the sign-in page shows.
func writeThrottled(w http.ResponseWriter, wait time.Duration) {
	seconds := int(math.Ceil(wait.Seconds()))
	w.Header().Set("Retry-After", strconv.Itoa(seconds))
	writeJSONError(w, http.StatusTooManyRequests, "Too many failed sign-ins. Try again in "+waitWords(seconds)+".")
}

// waitWords says a wait in seconds as people read it: "1 second", "40
// seconds", "3 minutes".
func waitWords(seconds int) string {
	switch {
	case seconds <= 1:
		return "1 second"
	case seconds < 60:
		return strconv.Itoa(seconds) + " seconds"
	case seconds <= 60:
		return "1 minute"
	default:
		return strconv.Itoa((seconds+59)/60) + " minutes"
	}
}
