package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// CreateTokenRequest makes an API token. ExpiresAt is optional; without it
// the token works until it is revoked.
type CreateTokenRequest struct {
	Name string `json:"name"`
	// Roles are its roles, each in every scope or narrowed to some; RoleIDs,
	// as before, each in every scope.
	Roles     []models.RoleAssignment `json:"roles"`
	RoleIDs   []string                `json:"role_ids"`
	ExpiresAt *time.Time              `json:"expires_at"`
}

// handleListTokens lists every token, revoked ones included:
// GET /api/v1/tokens
func (s *Server) handleListTokens(w http.ResponseWriter, r *http.Request) {
	tokens, err := s.store.ListTokens()
	if err != nil {
		writeServerError(w, err, "list tokens")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tokens": tokens})
}

// handleCreateToken makes a token and returns its secret, the only time it
// is shown: POST /api/v1/tokens
func (s *Server) handleCreateToken(w http.ResponseWriter, r *http.Request) {
	var req CreateTokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid request body (expires_at, if given, is an RFC 3339 time)")
		return
	}
	roles := req.Roles
	if roles == nil {
		roles = models.Unrestricted(req.RoleIDs)
	}
	t, secret, err := s.store.CreateTokenWithRoles(req.Name, roles, req.ExpiresAt, CurrentUser(r.Context()).ID)
	switch {
	case errors.Is(err, store.ErrInvalidToken), errors.Is(err, store.ErrRoleNotFound),
		errors.Is(err, store.ErrScopeNotFound), errors.Is(err, store.ErrInvalidAssignment):
		writeJSONError(w, http.StatusBadRequest, err.Error())
	case err != nil:
		writeStoreError(w, err, "create the token")
	default:
		writeJSON(w, http.StatusCreated, map[string]any{"token": t, "secret": secret})
	}
}

// CreateOwnTokenRequest makes a personal token, for an assistant.
type CreateOwnTokenRequest struct {
	Name      string     `json:"name"`
	ExpiresAt *time.Time `json:"expires_at"`
}

// handleListOwnTokens lists the signed-in user's personal tokens:
// GET /api/v1/auth/tokens
func (s *Server) handleListOwnTokens(w http.ResponseWriter, r *http.Request) {
	tokens, err := s.store.ListPersonalTokens(CurrentUser(r.Context()).ID)
	if err != nil {
		writeServerError(w, err, "list tokens")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tokens": tokens})
}

// handleCreateOwnToken makes the signed-in user a personal token, acting as
// them, and returns its secret, the only time it is shown:
// POST /api/v1/auth/tokens
func (s *Server) handleCreateOwnToken(w http.ResponseWriter, r *http.Request) {
	var req CreateOwnTokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid request body (expires_at, if given, is an RFC 3339 time)")
		return
	}
	t, secret, err := s.store.CreatePersonalToken(CurrentUser(r.Context()).ID, req.Name, req.ExpiresAt)
	switch {
	case errors.Is(err, store.ErrInvalidToken):
		writeJSONError(w, http.StatusBadRequest, err.Error())
	case err != nil:
		writeStoreError(w, err, "create the token")
	default:
		writeJSON(w, http.StatusCreated, map[string]any{"token": t, "secret": secret})
	}
}

// handleRevokeOwnToken revokes one of the signed-in user's personal tokens;
// anyone else's is not found: DELETE /api/v1/auth/tokens/{id}
func (s *Server) handleRevokeOwnToken(w http.ResponseWriter, r *http.Request) {
	t, err := s.store.GetToken(r.PathValue("id"))
	if err == nil && t.UserID != CurrentUser(r.Context()).ID {
		err = store.ErrTokenNotFound
	}
	if errors.Is(err, store.ErrTokenNotFound) {
		writeJSONError(w, http.StatusNotFound, "Token not found")
		return
	}
	if err != nil {
		writeServerError(w, err, "read the token")
		return
	}
	s.handleRevokeToken(w, r)
}

// handleRevokeToken stops a token working for good:
// DELETE /api/v1/tokens/{id}
func (s *Server) handleRevokeToken(w http.ResponseWriter, r *http.Request) {
	t, err := s.store.RevokeToken(r.PathValue("id"))
	switch {
	case errors.Is(err, store.ErrTokenNotFound):
		writeJSONError(w, http.StatusNotFound, "Token not found")
	case err != nil:
		writeStoreError(w, err, "revoke the token")
	default:
		// Its live connections end now, here; other servers notice soon.
		s.live.closeToken(t.ID, "the token was revoked")
		writeJSON(w, http.StatusOK, map[string]*models.APIToken{"token": t})
	}
}
