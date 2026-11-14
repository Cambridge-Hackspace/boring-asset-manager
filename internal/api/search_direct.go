package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// Going straight to a search's one result (#389, docs/data-model.md#search):
// the instance's setting, and each person's own choice.

// SearchSettings are the instance's settings for searching.
type SearchSettings struct {
	// Direct opens a search's one result straight away, for those who
	// have not chosen for themselves.
	Direct bool `json:"direct"`
}

// handleSearchSettings reads them: GET /api/v1/settings/search, for anyone
// who reads assets, whose searches follow them.
func (s *Server) handleSearchSettings(w http.ResponseWriter, r *http.Request) {
	direct, err := s.store.SearchDirect()
	if err != nil {
		writeServerError(w, err, "read the search settings")
		return
	}
	writeJSON(w, http.StatusOK, SearchSettings{Direct: direct})
}

// handleSetSearchSettings sets them: PUT /api/v1/settings/search with
// {"direct"}.
func (s *Server) handleSetSearchSettings(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Direct *bool `json:"direct"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Direct == nil {
		writeJSONError(w, http.StatusBadRequest, "direct is required: true or false")
		return
	}
	if err := s.store.SetSearchDirect(*req.Direct); err != nil {
		writeServerError(w, err, "save the search settings")
		return
	}
	s.handleSearchSettings(w, r)
}

// handleSetOwnSearchDirect sets whether a search that finds exactly one
// asset opens it straight away for the signed-in person: PUT
// /api/v1/auth/search-direct with {"direct": true}, false, or null to
// follow the instance's setting. It answers {"user"}.
func (s *Server) handleSetOwnSearchDirect(w http.ResponseWriter, r *http.Request) {
	var req map[string]*bool
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "direct is required: true, false, or null to follow the instance's setting")
		return
	}
	direct, ok := req["direct"]
	if !ok {
		writeJSONError(w, http.StatusBadRequest, "direct is required: true, false, or null to follow the instance's setting")
		return
	}
	user, err := s.store.SetUserSearchDirect(CurrentUser(r.Context()).ID, direct)
	switch {
	case errors.Is(err, store.ErrUserNotFound):
		writeJSONError(w, http.StatusNotFound, "User not found")
	case err != nil:
		writeStoreError(w, err, "set the search preference")
	default:
		writeJSON(w, http.StatusOK, map[string]any{"user": user})
	}
}
