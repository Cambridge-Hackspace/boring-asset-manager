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
	Name      string     `json:"name"`
	RoleIDs   []string   `json:"role_ids"`
	ExpiresAt *time.Time `json:"expires_at"`
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
	t, secret, err := s.store.CreateToken(req.Name, req.RoleIDs, req.ExpiresAt, CurrentUser(r.Context()).ID)
	switch {
	case errors.Is(err, store.ErrInvalidToken), errors.Is(err, store.ErrRoleNotFound):
		writeJSONError(w, http.StatusBadRequest, err.Error())
	case err != nil:
		writeStoreError(w, err, "create the token")
	default:
		writeJSON(w, http.StatusCreated, map[string]any{"token": t, "secret": secret})
	}
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
