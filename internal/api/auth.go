package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"sort"

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

	user, err := s.store.GetUserByEmail(req.Email)
	if err != nil || user == nil {
		writeJSONError(w, http.StatusUnauthorized, "Invalid email or password")
		return
	}

	// Verify the password before revealing account state, so the disabled-account
	// message cannot be used to probe which emails exist.
	if !auth.CheckPassword(req.Password, user.PasswordHash) {
		writeJSONError(w, http.StatusUnauthorized, "Invalid email or password")
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
func (s *Server) handleAuthMe(w http.ResponseWriter, r *http.Request) {
	user := CurrentUser(r.Context())
	if user == nil {
		writeJSONError(w, http.StatusUnauthorized, "Authentication required")
		return
	}

	permsMap := UserPermissions(r.Context())
	var permsList []string
	for k, v := range permsMap {
		if v {
			permsList = append(permsList, k)
		}
	}
	sort.Strings(permsList)

	writeJSON(w, http.StatusOK, map[string]any{
		"user":        user,
		"permissions": permsList,
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
